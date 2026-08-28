// Command dropserve is the Dropserve command-line entry point.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/tanzir71/dropserve/internal/access"
	"github.com/tanzir71/dropserve/internal/app"
	"github.com/tanzir71/dropserve/internal/autostart"
	"github.com/tanzir71/dropserve/internal/config"
	"github.com/tanzir71/dropserve/internal/dashboard"
	"github.com/tanzir71/dropserve/internal/discovery"
	"github.com/tanzir71/dropserve/internal/doctor"
	"github.com/tanzir71/dropserve/internal/firstrun"
	"github.com/tanzir71/dropserve/internal/launch"
	"github.com/tanzir71/dropserve/internal/ports"
	"github.com/tanzir71/dropserve/internal/runtimes"
	"github.com/tanzir71/dropserve/internal/scanner"
	dropserver "github.com/tanzir71/dropserve/internal/server"
	"github.com/tanzir71/dropserve/internal/state"
	"github.com/tanzir71/dropserve/internal/supervisor"
	"github.com/tanzir71/dropserve/internal/systemmemory"
	"github.com/tanzir71/dropserve/internal/updatecheck"
	"github.com/tanzir71/dropserve/internal/version"
)

const usage = `Dropserve hosts folders as local websites.

Usage:
  dropserve serve                         run in the foreground
  dropserve status                        print live state and apps as JSON
  dropserve open                          open the dashboard
  dropserve apps                          list discovered apps
  dropserve add PATH                      register an app folder without moving it
  dropserve logs SLUG [-f]                print or follow one command app's logs
  dropserve restart SLUG                  restart one command app
  dropserve autostart enable|disable|status
  dropserve trust install|uninstall|status
  dropserve firewall allow                allow private-network access on Windows
  dropserve tailscale status|serve|unserve|funnel SLUG|unfunnel SLUG
  dropserve runtime install php|mariadb|postgres
  dropserve config path|validate|edit
  dropserve healthz                       verify the running local server
  dropserve doctor                        check this computer's setup
  dropserve version                       print the version and build commit
  dropserve help                          show this help
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	return runWithConfigPath(args, stdout, stderr, "")
}

func runWithConfigPath(args []string, stdout, stderr io.Writer, configPath string) int {
	if len(args) == 0 {
		return defaultCommand(stdout, stderr, configPath)
	}
	if args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		if _, err := fmt.Fprint(stdout, usage); err != nil {
			return 1
		}
		return 0
	}
	if args[0] == "--background" {
		if err := writeBackgroundConsoleProbe(); err != nil {
			if _, writeErr := fmt.Fprintf(stderr, "Dropserve could not write its background console probe: %v\n", err); writeErr != nil {
				return 1
			}
			return 1
		}
		return serveCommand(args[1:], stdout, stderr, configPath)
	}

	switch args[0] {
	case "serve":
		return serveCommand(args[1:], stdout, stderr, configPath)
	case "status":
		return statusCommand(args[1:], stdout, stderr)
	case "open":
		return openCommand(args[1:], stdout, stderr)
	case "apps":
		return appsCommand(args[1:], stdout, stderr)
	case "logs":
		return logsCommand(args[1:], stdout, stderr)
	case "restart":
		return restartCommand(args[1:], stdout, stderr)
	case "healthz":
		return healthzCommand(args[1:], stdout, stderr)
	case "doctor":
		return doctorCommand(args[1:], stdout, stderr, configPath)
	case "autostart":
		return autostartCommand(args[1:], stdout, stderr)
	case "trust":
		return trustCommand(args[1:], stdout, stderr)
	case "firewall":
		return firewallCommand(args[1:], stdout, stderr)
	case "tailscale":
		return tailscaleCommand(args[1:], stdout, stderr)
	case "runtime":
		return runtimeCommand(args[1:], stdout, stderr)
	case "config":
		return configCommand(args[1:], stdout, stderr, configPath)
	case "version", "--version", "-v":
		if _, err := fmt.Fprintf(stdout, "dropserve %s (%s)\n", version.Version, version.Commit); err != nil {
			return 1
		}
		return 0
	case "add":
		if len(args) != 2 {
			if _, err := fmt.Fprintln(stderr, "Choose one app folder: dropserve add <path>"); err != nil {
				return 1
			}
			return 2
		}
		if configPath == "" {
			var err error
			configPath, err = config.DefaultPath()
			if err != nil {
				if _, writeErr := fmt.Fprintf(stderr, "Dropserve could not find its config folder: %v\n", err); writeErr != nil {
					return 1
				}
				return 1
			}
		}
		registeredPath, changed, err := config.Register(configPath, args[1])
		if err != nil {
			if _, writeErr := fmt.Fprintf(stderr, "Dropserve could not add that folder: %v\n", err); writeErr != nil {
				return 1
			}
			return 1
		}
		if changed {
			if _, err := fmt.Fprintf(stdout, "Added %s. Dropserve will serve it without moving or changing it.\n", registeredPath); err != nil {
				return 1
			}
		} else if _, err := fmt.Fprintf(stdout, "%s is already registered.\n", registeredPath); err != nil {
			return 1
		}
		return 0
	default:
		if _, err := fmt.Fprintf(stderr, "Unknown command %q. Run 'dropserve help' to see the available commands.\n", args[0]); err != nil {
			return 1
		}
		return 2
	}
}

func defaultCommand(stdout, stderr io.Writer, configPath string) int {
	if configPath == "" {
		var err error
		configPath, err = config.DefaultPath()
		if err != nil {
			if _, writeErr := fmt.Fprintf(stderr, "Dropserve could not find its config folder: %v\n", err); writeErr != nil {
				return 1
			}
			return 1
		}
	}
	statePath, err := state.DefaultPath()
	if err != nil {
		if _, writeErr := fmt.Fprintf(stderr, "Dropserve could not find its state folder: %v\n", err); writeErr != nil {
			return 1
		}
		return 1
	}
	configuration, err := config.Load(configPath)
	if err != nil {
		if _, writeErr := fmt.Fprintf(stderr, "Dropserve could not read its config: %v\n", err); writeErr != nil {
			return 1
		}
		return 1
	}
	appsRoot := config.Default().Server.AppsRoots[0]
	if len(configuration.Server.AppsRoots) != 0 {
		appsRoot = configuration.Server.AppsRoots[0]
	}
	executable, err := os.Executable()
	if err != nil {
		if _, writeErr := fmt.Fprintf(stderr, "Dropserve could not find its executable: %v\n", err); writeErr != nil {
			return 1
		}
		return 1
	}
	executable = backgroundExecutable(executable)
	ctx, stop := signal.NotifyContext(context.Background(), commandSignals()...)
	defer stop()
	setupResult, err := firstrun.Run(ctx, firstrun.Options{
		StatePath:       statePath,
		ConfigPath:      configPath,
		DefaultAppsRoot: appsRoot,
		Executable:      executable,
		OpenBrowser: func(address string) error {
			if _, writeErr := fmt.Fprintf(stdout, "Dropserve setup is at %s\n", address); writeErr != nil {
				return writeErr
			}
			if openErr := launch.OpenURL(address); openErr != nil {
				_, _ = fmt.Fprintf(stderr, "Open %s in a browser to finish setup.\n", address)
			}
			return nil
		},
		EnableAutostart: autostart.Enable,
	})
	if err != nil {
		if _, writeErr := fmt.Fprintf(stderr, "Dropserve could not complete first-run setup: %v\n", err); writeErr != nil {
			return 1
		}
		return 1
	}
	appsRoot = desktopAppsRoot(appsRoot, setupResult)
	return runDefaultMode(ctx, defaultModeOptions{
		ServeArguments: []string{
			"--config", configPath,
			"--state", statePath,
			"--open",
		},
		ConfigPath: configPath,
		StatePath:  statePath,
		AppsRoot:   appsRoot,
		Executable: executable,
		Stdout:     stdout,
		Stderr:     stderr,
	})
}

func desktopAppsRoot(configuredRoot string, setupResult firstrun.Result) string {
	if setupResult.Shown && setupResult.AppsRoot != "" {
		return setupResult.AppsRoot
	}
	return configuredRoot
}

func autostartCommand(arguments []string, stdout, stderr io.Writer) int {
	if len(arguments) != 1 {
		if _, err := fmt.Fprintln(stderr, "Choose an autostart action: dropserve autostart enable|disable|status"); err != nil {
			return 1
		}
		return 2
	}

	switch arguments[0] {
	case "enable":
		executable, err := os.Executable()
		if err == nil {
			err = autostart.Enable(backgroundExecutable(executable))
		}
		if err != nil {
			if _, writeErr := fmt.Fprintf(stderr, "Dropserve could not enable autostart: %v\n", err); writeErr != nil {
				return 1
			}
			return 1
		}
		if _, err := fmt.Fprintln(stdout, "Dropserve will start when you log in."); err != nil {
			return 1
		}
		if note := autostart.EnableNote(); note != "" {
			if _, err := fmt.Fprintln(stdout, note); err != nil {
				return 1
			}
		}
		return 0
	case "disable":
		if err := autostart.Disable(); err != nil {
			if _, writeErr := fmt.Fprintf(stderr, "Dropserve could not disable autostart: %v\n", err); writeErr != nil {
				return 1
			}
			return 1
		}
		if _, err := fmt.Fprintln(stdout, "Dropserve will not start automatically."); err != nil {
			return 1
		}
		return 0
	case "status":
		enabled, err := autostart.Enabled()
		if err != nil {
			if _, writeErr := fmt.Fprintf(stderr, "Dropserve could not read autostart status: %v\n", err); writeErr != nil {
				return 1
			}
			return 1
		}
		status := "disabled"
		if enabled {
			status = "enabled"
		}
		if _, err := fmt.Fprintln(stdout, status); err != nil {
			return 1
		}
		return 0
	default:
		if _, err := fmt.Fprintln(stderr, "Choose an autostart action: dropserve autostart enable|disable|status"); err != nil {
			return 1
		}
		return 2
	}
}

type rootFlags []string

func (roots *rootFlags) String() string {
	return strings.Join(*roots, ",")
}

func (roots *rootFlags) Set(value string) error {
	if value == "" {
		return errors.New("root path cannot be empty")
	}
	*roots = append(*roots, value)
	return nil
}

func serveCommand(arguments []string, stdout, stderr io.Writer, injectedConfigPath string) int {
	ctx, stop := signal.NotifyContext(context.Background(), commandSignals()...)
	defer stop()
	return serveCommandContext(ctx, arguments, stdout, stderr, injectedConfigPath)
}

func commandSignals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGTERM}
}

func serveCommandContext(
	ctx context.Context,
	arguments []string,
	stdout io.Writer,
	stderr io.Writer,
	injectedConfigPath string,
) int {
	return serveCommandContextWithReady(ctx, arguments, stdout, stderr, injectedConfigPath, nil, nil, nil)
}

func serveCommandContextWithReady(
	ctx context.Context,
	arguments []string,
	stdout io.Writer,
	stderr io.Writer,
	injectedConfigPath string,
	ready func(string),
	publicSharingChanged func(bool),
	updateChanged func(dashboard.UpdateNotice),
) int {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	flags.SetOutput(stderr)
	listenAddress := flags.String("listen", "", "listener address; use 127.0.0.1:0 for a random local port")
	bindAddress := flags.String("bind", "", "listener host or IP; defaults to the configured bind address")
	configPath := flags.String("config", injectedConfigPath, "configuration file")
	statePath := flags.String("state", "", "runtime state file")
	openDashboard := flags.Bool("open", false, "open the dashboard after startup")
	var roots rootFlags
	flags.Var(&roots, "root", "Apps folder; repeat to use more than one")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		if _, err := fmt.Fprintln(stderr, "The serve command accepts flags only. Run 'dropserve help' for examples."); err != nil {
			return 1
		}
		return 2
	}

	configuration := config.Default()
	shouldLoadConfig := *configPath != "" || len(roots) == 0
	if shouldLoadConfig {
		if *configPath == "" {
			var err error
			*configPath, err = config.DefaultPath()
			if err != nil {
				if _, writeErr := fmt.Fprintf(stderr, "Dropserve could not find its config folder: %v\n", err); writeErr != nil {
					return 1
				}
				return 1
			}
		}
		loaded, err := config.Load(*configPath)
		if err != nil {
			if _, writeErr := fmt.Fprintf(stderr, "Dropserve could not read its config: %v\n", err); writeErr != nil {
				return 1
			}
			return 1
		}
		configuration = loaded
	}
	if len(roots) != 0 {
		configuration.Server.AppsRoots = append([]string(nil), roots...)
	}
	if *bindAddress == "" {
		*bindAddress = configuration.Server.Bind
	}
	var liveConfiguration atomic.Pointer[config.Config]
	liveConfiguration.Store(&configuration)
	if *statePath == "" && *listenAddress == "" {
		var err error
		*statePath, err = state.DefaultPath()
		if err != nil {
			if _, writeErr := fmt.Fprintf(stderr, "Dropserve could not find its state folder: %v\n", err); writeErr != nil {
				return 1
			}
			return 1
		}
	}
	indexPath := ""
	networkStatePath := ""
	funnelStatePath := ""
	stateDirectory := ""
	if *statePath != "" {
		stateDirectory = filepath.Dir(*statePath)
		indexPath = filepath.Join(stateDirectory, "index.json")
		networkStatePath = filepath.Join(stateDirectory, "network.json")
		funnelStatePath = filepath.Join(stateDirectory, "funnel.json")
	}

	listener, err := acquireMainListener(ctx, *listenAddress, *bindAddress, *statePath, configuration)
	if err != nil {
		if _, writeErr := fmt.Fprintf(stderr, "Dropserve could not start its HTTP listener: %v\n", err); writeErr != nil {
			return 1
		}
		return 1
	}
	mainPort, err := listenerPort(listener.Addr())
	if err != nil {
		_ = listener.Close()
		if _, writeErr := fmt.Fprintf(stderr, "Dropserve could not read its HTTP listener: %v\n", err); writeErr != nil {
			return 1
		}
		return 1
	}
	probeLANIP := discovery.ProbeLANIP
	if listenerExcludesLAN(listener.Addr()) {
		probeLANIP = func() (netip.Addr, error) { return netip.Addr{}, nil }
	}
	lanIP, probeErr := probeLANIP()
	if probeErr != nil {
		_, _ = fmt.Fprintf(stderr, "Dropserve could not select a LAN address; loopback remains available: %v\n", probeErr)
	}
	var runtimeWarnings []string
	var handler *dropserver.Server
	var servedHandler http.Handler
	var updateNotice atomic.Pointer[dashboard.UpdateNotice]
	var addonManager *runtimes.Manager
	if stateDirectory != "" {
		addonManager, err = runtimes.NewManager(runtimes.ManagerOptions{
			Context:        ctx,
			StateDirectory: stateDirectory,
			Output:         stderr,
			OnChange: func() {
				if handler != nil {
					if reconcileErr := handler.Reconcile(); reconcileErr != nil {
						_, _ = fmt.Fprintf(stderr, "Dropserve could not refresh apps after an add-on change: %v\n", reconcileErr)
					}
				}
			},
		})
		if err != nil {
			runtimeWarnings = append(runtimeWarnings, "Dropserve could not prepare optional add-ons: "+err.Error())
			addonManager = nil
		}
	}
	if addonManager != nil {
		defer func() { _ = addonManager.Close() }()
	}
	var phpHandler func(app.App) (http.Handler, error)
	var addons func() []dashboard.AddonStatus
	var changeAddon func(context.Context, string, string) error
	if addonManager != nil {
		phpHandler = func(application app.App) (http.Handler, error) {
			return addonManager.PHPHandler(application.Path, application.Slug)
		}
		addons = func() []dashboard.AddonStatus {
			statuses := addonManager.Statuses()
			result := make([]dashboard.AddonStatus, 0, len(statuses))
			for _, status := range statuses {
				result = append(result, dashboard.AddonStatus{
					Name: status.Name, Title: status.Title, Version: status.Version, Description: status.Description,
					Available: status.Available, Installed: status.Installed, Running: status.Running,
					Busy: status.Busy, Progress: status.Progress, Connection: status.Connection, Message: status.Message,
				})
			}
			return result
		}
		changeAddon = addonManager.ChangeAsync
	}
	discoveryManager := discovery.NewManager(discovery.ManagerOptions{
		LANIP:        lanIP,
		HTTPPort:     mainPort,
		MDNSHostname: configuration.Discovery.MDNSName,
		NoticePath:   networkStatePath,
		Logf: func(format string, arguments ...any) {
			_, _ = fmt.Fprintf(stderr, format+"\n", arguments...)
		},
	})
	discoveryManager.SetTailscaleEnabled(configuration.Discovery.Tailscale)
	defer discoveryManager.Close()
	tailscaleProbes := discovery.TailscaleProbes{}
	probeTailscale := func(probeContext context.Context) (discovery.TailscaleStatus, error) {
		status, statusErr := discovery.ProbeTailscale(probeContext, tailscaleProbes)
		if statusErr != nil || !strings.EqualFold(status.BackendState, "running") {
			return status, statusErr
		}
		serveEnabled, serveErr := discovery.ProbeTailscaleServe(probeContext, mainPort, tailscaleProbes)
		if serveErr != nil {
			status.Message = "Tailscale is running, but Dropserve could not verify tailnet HTTPS."
			_, _ = fmt.Fprintf(stderr, "Dropserve could not read Tailscale Serve status: %v\n", serveErr)
			return status, nil
		}
		status.ServeEnabled = serveEnabled
		return status, nil
	}
	if live := liveConfiguration.Load(); live != nil && live.Discovery.Tailscale {
		tailscaleStatus, tailscaleErr := probeTailscale(ctx)
		if tailscaleErr != nil {
			_, _ = fmt.Fprintf(stderr, "Dropserve could not read Tailscale status: %v\n", tailscaleErr)
		} else {
			discoveryManager.UpdateTailscale(tailscaleStatus)
		}
	}
	funnel, funnelErr := discovery.NewFunnelManager(discovery.FunnelOptions{
		StatePath: funnelStatePath,
		Execute:   discovery.TailscaleFunnelExecutor(mainPort, tailscaleProbes),
		OnChange:  publicSharingChanged,
	})
	if funnelErr != nil {
		_, _ = fmt.Fprintf(stderr, "Dropserve disabled Tailscale Funnel because its state could not be read: %v\n", funnelErr)
		funnel = nil
	}
	httpsConfigPath := *configPath
	var httpsConfigPathErr error
	if httpsConfigPath == "" {
		httpsConfigPath, httpsConfigPathErr = config.DefaultPath()
	}
	hostname, _ := os.Hostname()
	httpsController := newLocalHTTPSController(localHTTPSOptions{
		Bind:           *bindAddress,
		PreferredPort:  configuration.Server.HTTPSPort,
		StateDirectory: stateDirectory,
		Hostname:       hostname,
		Addresses: func() []netip.Addr {
			snapshot := discoveryManager.Snapshot()
			if snapshot.LANIP.IsValid() {
				return []netip.Addr{snapshot.LANIP}
			}
			return nil
		},
		Handler: func() http.Handler {
			return servedHandler
		},
		PersistPort: func(port int) error {
			if httpsConfigPathErr != nil {
				return httpsConfigPathErr
			}
			updated, loadErr := config.Load(httpsConfigPath)
			if loadErr != nil {
				return loadErr
			}
			updated.Server.HTTPSPort = port
			if err := config.Save(httpsConfigPath, updated); err != nil {
				return err
			}
			return nil
		},
	})

	setTailscaleServe := func(changeContext context.Context, enabled bool) error {
		live := liveConfiguration.Load()
		if live == nil || !live.Discovery.Tailscale {
			return errors.New("tailscale integration is disabled in config.toml")
		}
		if err := discovery.SetTailscaleServe(changeContext, mainPort, enabled, tailscaleProbes); err != nil {
			return err
		}
		return discoveryManager.SetTailscaleServeEnabled(enabled)
	}
	handler, err = dropserver.NewWithOptions(dropserver.Options{
		Scanner: scanner.Options{
			Roots:             configuration.Server.AppsRoots,
			Registered:        configuration.Server.RegisteredApps,
			LazyStartCommands: globalLazyStart(configuration.Runtimes.LazyStart),
		},
		IndexPath:      indexPath,
		DashboardTitle: configuration.Dashboard.Title,
		DashboardTheme: configuration.Dashboard.Theme,
		PinToRoot:      configuration.Dashboard.PinToRoot,
		Supervisor: supervisor.Options{
			FirstPort: configuration.Server.AppPortRange[0],
			LastPort:  configuration.Server.AppPortRange[1],
		},
		AsyncCommandStart:    true,
		Warnings:             runtimeWarnings,
		Discovery:            discoveryManager.Snapshot,
		Funnel:               funnel,
		SetTailscaleServe:    setTailscaleServe,
		LocalHTTPSStatus:     httpsController.Status,
		SetLocalHTTPS:        httpsController.SetEnabled,
		SetLocalTrust:        httpsController.SetTrust,
		RootCertificate:      httpsController.RootCertificate,
		DismissNetworkChange: discoveryManager.DismissNetworkChange,
		PHPHandler:           phpHandler,
		Addons:               addons,
		ChangeAddon:          changeAddon,
		OpenFolder: func(_ context.Context, slug string) error {
			path := ""
			live := liveConfiguration.Load()
			if slug == "" && live != nil && len(live.Server.AppsRoots) != 0 {
				path = live.Server.AppsRoots[0]
			}
			if slug != "" && handler != nil {
				for _, application := range handler.Scan().Apps {
					if application.Slug == slug {
						path = application.Path
						break
					}
				}
			}
			if path == "" {
				return errors.New("the requested Apps folder was not found")
			}
			if info, statErr := os.Stat(path); statErr == nil && !info.IsDir() {
				path = filepath.Dir(path)
			}
			return launch.OpenPath(path)
		},
		Update: func() dashboard.UpdateNotice {
			current := updateNotice.Load()
			if current == nil {
				return dashboard.UpdateNotice{}
			}
			return *current
		},
	})
	if err != nil {
		_ = listener.Close()
		if _, writeErr := fmt.Fprintf(stderr, "Dropserve could not scan your app folders: %v\n", err); writeErr != nil {
			return 1
		}
		return 1
	}
	defer func() { _ = handler.Close() }()
	accessGate, err := access.New(handler.Handler(), configuration.Security.PINEnabled, configuration.Security.PINHash)
	if err != nil {
		_ = listener.Close()
		_, _ = fmt.Fprintf(stderr, "Dropserve could not apply its PIN lock: %v\n", err)
		return 1
	}
	servedHandler = accessGate
	updateTriggers := make(chan struct{}, 1)
	if !strings.HasPrefix(version.Version, "0.0.0-") {
		go monitorUpdates(ctx, version.Version, &updateNotice, updateChanged, handler.Reconcile, func() bool {
			live := liveConfiguration.Load()
			return live != nil && live.Updates.Check
		}, updateTriggers, updatecheck.Check)
	}

	listenerRuntime := dropserver.NewListenerRuntime(servedHandler)
	listenerRuntime.Start(listener)
	if configuration.Server.HTTPSPort > 0 {
		if httpsErr := httpsController.SetEnabled(ctx, true); httpsErr != nil {
			warning := fmt.Sprintf(
				"HTTPS port %d is unavailable, so HTTP is still available and HTTPS is off: %v",
				configuration.Server.HTTPSPort,
				httpsErr,
			)
			_, _ = fmt.Fprintln(stderr, warning)
		}
	}
	monitorOptions := discovery.MonitorOptions{
		Manager:         discoveryManager,
		ProbeLANIP:      probeLANIP,
		ListenerHealthy: listenerRuntime.Healthy,
		RecoverListener: func(recoveryContext context.Context) error {
			currentConfiguration := configuration
			if live := liveConfiguration.Load(); live != nil {
				currentConfiguration = *live
			}
			recovered, recoveryErr := acquireMainListener(
				recoveryContext,
				*listenAddress,
				*bindAddress,
				*statePath,
				currentConfiguration,
			)
			if recoveryErr != nil {
				return recoveryErr
			}
			listenerRuntime.Start(recovered)
			return nil
		},
		Logf: func(format string, arguments ...any) {
			_, _ = fmt.Fprintf(stderr, format+"\n", arguments...)
		},
	}
	monitorOptions.ProbeTailscale = func(probeContext context.Context) (discovery.TailscaleStatus, error) {
		live := liveConfiguration.Load()
		if live == nil || !live.Discovery.Tailscale {
			return discovery.TailscaleStatus{}, nil
		}
		return probeTailscale(probeContext)
	}
	if funnel != nil {
		monitorOptions.ExpireFunnels = funnel.Expire
	}
	monitorOptions.UpdateTLSAddresses = httpsController.UpdateAddresses
	monitor := discovery.NewMonitor(monitorOptions)
	go monitor.Run(ctx)

	if *configPath != "" {
		go func() {
			watchErr := config.Watch(ctx, *configPath, func(updated config.Config) {
				if len(roots) != 0 {
					updated.Server.AppsRoots = append([]string(nil), roots...)
				}
				scannerOptions := scanner.Options{
					Roots:             updated.Server.AppsRoots,
					Registered:        updated.Server.RegisteredApps,
					LazyStartCommands: globalLazyStart(updated.Runtimes.LazyStart),
				}
				if updateErr := handler.UpdateConfiguration(
					scannerOptions,
					updated.Dashboard.Title,
					updated.Dashboard.Theme,
					updated.Dashboard.PinToRoot,
					updated.Server.AppPortRange[0],
					updated.Server.AppPortRange[1],
				); updateErr != nil {
					_, _ = fmt.Fprintf(stderr, "Dropserve ignored a config edit and kept the last good settings: %v\n", updateErr)
					return
				}
				if updateErr := accessGate.Update(updated.Security.PINEnabled, updated.Security.PINHash); updateErr != nil {
					_, _ = fmt.Fprintf(stderr, "Dropserve ignored a PIN config edit and kept the last good PIN: %v\n", updateErr)
					return
				}
				previous := liveConfiguration.Load()
				liveConfiguration.Store(&updated)
				if previous != nil && previous.Updates.Check != updated.Updates.Check {
					if updated.Updates.Check {
						select {
						case updateTriggers <- struct{}{}:
						default:
						}
					} else {
						cleared := dashboard.UpdateNotice{}
						updateNotice.Store(&cleared)
						if updateChanged != nil {
							updateChanged(cleared)
						}
						_ = handler.Reconcile()
					}
				}
				discoveryManager.SetTailscaleEnabled(updated.Discovery.Tailscale)
				discoveryManager.ConfigureMDNS(updated.Discovery.MDNS, updated.Discovery.MDNSName)
				if updated.Discovery.Tailscale && (previous == nil || !previous.Discovery.Tailscale) {
					go func() {
						status, probeErr := probeTailscale(ctx)
						if probeErr == nil {
							discoveryManager.UpdateTailscale(status)
						}
					}()
				}
				if previous != nil && (updated.Server.Bind != previous.Server.Bind || updated.Server.HTTPPort != previous.Server.HTTPPort || updated.Server.HTTPSPort != previous.Server.HTTPSPort) {
					_, _ = fmt.Fprintln(stderr, "Dropserve reloaded config.toml; listener bind and port changes take effect after restart.")
				} else {
					_, _ = fmt.Fprintln(stderr, "Dropserve reloaded config.toml.")
				}
			}, func(loadErr error) {
				_, _ = fmt.Fprintf(stderr, "Dropserve ignored a malformed config edit and kept the last good settings: %v\n", loadErr)
			})
			if watchErr != nil && ctx.Err() == nil {
				_, _ = fmt.Fprintf(stderr, "Dropserve could not watch config.toml: %v\n", watchErr)
			}
		}()
	}

	address := listenerURL(listener.Addr())
	if _, err := fmt.Fprintf(stdout, "Dropserve is ready at %s\n", address); err != nil {
		return 1
	}
	if *openDashboard {
		if err := launch.OpenURL(address); err != nil {
			if _, writeErr := fmt.Fprintf(stderr, "Dropserve could not open the dashboard automatically: %v\n", err); writeErr != nil {
				return 1
			}
		}
	}
	if ready != nil {
		ready(address)
	}
	if configuration.Discovery.MDNS {
		discoveryManager.StartMDNS()
	}
	<-ctx.Done()
	shutdownContext, cancelShutdown := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancelShutdown()
	if err := httpsController.Shutdown(shutdownContext); err != nil {
		_, _ = fmt.Fprintf(stderr, "Dropserve could not stop its HTTPS listener cleanly: %v\n", err)
	}
	if err := listenerRuntime.Shutdown(shutdownContext); err != nil {
		_, _ = fmt.Fprintf(stderr, "Dropserve could not stop its HTTP listener cleanly: %v\n", err)
		return 1
	}
	return 0
}

func globalLazyStart(setting string) bool {
	switch strings.ToLower(strings.TrimSpace(setting)) {
	case "always":
		return true
	case "auto":
		return systemmemory.LowMemory()
	default:
		return false
	}
}

func monitorUpdates(
	ctx context.Context,
	currentVersion string,
	notice *atomic.Pointer[dashboard.UpdateNotice],
	notify func(dashboard.UpdateNotice),
	onChange func() error,
	enabled func() bool,
	trigger <-chan struct{},
	checkUpdate func(context.Context, updatecheck.Options) (updatecheck.Notification, error),
) {
	check := func() {
		if enabled != nil && !enabled() {
			return
		}
		checkContext, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		latest, err := checkUpdate(checkContext, updatecheck.Options{CurrentVersion: currentVersion})
		if err != nil {
			return
		}
		current := dashboard.UpdateNotice{Available: latest.Available, Version: latest.Version, URL: latest.URL}
		notice.Store(&current)
		if notify != nil {
			notify(current)
		}
		if onChange != nil {
			_ = onChange()
		}
	}
	check()
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-trigger:
			check()
		case <-ticker.C:
			check()
		}
	}
}

func acquireMainListener(
	ctx context.Context,
	listenAddress string,
	bindAddress string,
	statePath string,
	configuration config.Config,
) (net.Listener, error) {
	if listenAddress != "" {
		var listenConfig net.ListenConfig
		listener, err := listenConfig.Listen(ctx, "tcp", listenAddress)
		if err != nil {
			return nil, fmt.Errorf("use %s: %w", listenAddress, err)
		}
		return listener, nil
	}

	if statePath == "" {
		var err error
		statePath, err = state.DefaultPath()
		if err != nil {
			return nil, err
		}
	}
	persisted, err := state.Load(statePath)
	if err != nil {
		return nil, err
	}
	preferredPort := configuration.Server.HTTPPort
	if preferredPort == 0 {
		preferredPort = persisted.HTTPPort
	}
	listener, selection, err := ports.Acquire(ctx, bindAddress, preferredPort)
	if err != nil {
		return nil, err
	}
	snapshot := state.State{HTTPPort: selection.Port}
	if selection.Fallback {
		snapshot.Warnings = []state.Warning{{Code: "port_fallback", Message: selection.Message}}
	} else if configuration.Server.HTTPPort == 0 && selection.Port == persisted.HTTPPort {
		snapshot.Warnings = persisted.Warnings
	}
	if err := state.Save(statePath, snapshot); err != nil {
		_ = listener.Close()
		return nil, err
	}
	return listener, nil
}

func statusCommand(arguments []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("status", flag.ContinueOnError)
	flags.SetOutput(stderr)
	statePath := flags.String("state", "", "runtime state file")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		if _, err := fmt.Fprintln(stderr, "The status command accepts flags only."); err != nil {
			return 1
		}
		return 2
	}
	if *statePath == "" {
		var err error
		*statePath, err = state.DefaultPath()
		if err != nil {
			if _, writeErr := fmt.Fprintf(stderr, "Dropserve could not find its state folder: %v\n", err); writeErr != nil {
				return 1
			}
			return 1
		}
	}
	snapshot, err := state.Load(*statePath)
	if err != nil {
		if _, writeErr := fmt.Fprintf(stderr, "Dropserve could not read its runtime state: %v\n", err); writeErr != nil {
			return 1
		}
		return 1
	}
	if snapshot.HTTPPort > 0 && snapshot.HTTPPort <= 65535 {
		api := localAPIForPort(snapshot.HTTPPort)
		var live map[string]any
		if liveErr := api.get(context.Background(), "/_dropserve/api/status", &live); liveErr == nil {
			var applications []cliApp
			if appsErr := api.get(context.Background(), "/_dropserve/api/apps", &applications); appsErr != nil {
				_, _ = fmt.Fprintf(stderr, "Dropserve could not read its live app list: %v\n", appsErr)
				return 1
			}
			live["apps"] = applications
			live["port"] = snapshot.HTTPPort
			live["running"] = true
			warnings := append([]state.Warning(nil), snapshot.Warnings...)
			if liveWarnings, ok := live["warnings"].([]any); ok {
				for _, item := range liveWarnings {
					if message, valid := item.(string); valid && message != "" {
						warnings = append(warnings, state.Warning{Code: "runtime_status", Message: message})
					}
				}
			}
			live["warnings"] = warnings
			live["discovery"] = map[string]any{"network": live["network"], "sharing": live["sharing"]}
			delete(live, "csrf_token")
			if err := json.NewEncoder(stdout).Encode(live); err != nil {
				return 1
			}
			return 0
		}
	}
	output := struct {
		Version  string          `json:"version"`
		Commit   string          `json:"commit"`
		Port     int             `json:"port"`
		Warnings []state.Warning `json:"warnings"`
		Apps     []cliApp        `json:"apps"`
		Running  bool            `json:"running"`
	}{
		Version:  version.Version,
		Commit:   version.Commit,
		Port:     snapshot.HTTPPort,
		Warnings: snapshot.Warnings,
	}
	if err := json.NewEncoder(stdout).Encode(output); err != nil {
		return 1
	}
	return 0
}

func healthzCommand(arguments []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("healthz", flag.ContinueOnError)
	flags.SetOutput(stderr)
	statePath := flags.String("state", "", "runtime state file")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		_, _ = fmt.Fprintln(stderr, "The healthz command accepts flags only.")
		return 2
	}
	if *statePath == "" {
		var err error
		*statePath, err = state.DefaultPath()
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "Dropserve could not find its state folder: %v\n", err)
			return 1
		}
	}
	snapshot, err := state.Load(*statePath)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "Dropserve could not read its runtime state: %v\n", err)
		return 1
	}
	if snapshot.HTTPPort < 1 || snapshot.HTTPPort > 65535 {
		_, _ = fmt.Fprintln(stderr, "Dropserve is not healthy: no running HTTP port is recorded.")
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	requestURL := fmt.Sprintf("http://127.0.0.1:%d/_dropserve/healthz", snapshot.HTTPPort)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "Dropserve is not healthy: %v\n", err)
		return 1
	}
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	response, err := (&http.Client{Transport: transport}).Do(request)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "Dropserve is not healthy: %v\n", err)
		return 1
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(response.Body, 16))
	if err != nil || response.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) != "ok" {
		_, _ = fmt.Fprintf(stderr, "Dropserve is not healthy: local health check returned HTTP %d.\n", response.StatusCode)
		return 1
	}
	if _, err := fmt.Fprintln(stdout, "ok"); err != nil {
		return 1
	}
	return 0
}

func doctorCommand(arguments []string, stdout, stderr io.Writer, injectedConfigPath string) int {
	return doctorCommandWithProbes(arguments, stdout, stderr, injectedConfigPath, doctor.Probes{})
}

func doctorCommandWithProbes(
	arguments []string,
	stdout io.Writer,
	stderr io.Writer,
	injectedConfigPath string,
	probes doctor.Probes,
) int {
	flags := flag.NewFlagSet("doctor", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", injectedConfigPath, "configuration file")
	statePath := flags.String("state", "", "runtime state file")
	logDirectory := flags.String("logs", "", "application log directory")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		if _, err := fmt.Fprintln(stderr, "The doctor command accepts flags only."); err != nil {
			return 1
		}
		return 2
	}
	if *configPath == "" {
		var err error
		*configPath, err = config.DefaultPath()
		if err != nil {
			if _, writeErr := fmt.Fprintf(stderr, "Dropserve doctor could not find its config folder: %v\n", err); writeErr != nil {
				return 1
			}
			return 1
		}
	}
	report := doctor.Diagnose(doctor.Options{
		ConfigPath:   *configPath,
		StatePath:    *statePath,
		LogDirectory: *logDirectory,
		Version:      version.Version,
		Commit:       version.Commit,
	}, probes)
	if err := report.Write(stdout); err != nil {
		return 1
	}
	if report.RequiredFailure() {
		return 1
	}
	return 0
}

func listenerURL(address net.Addr) string {
	host, port, err := net.SplitHostPort(address.String())
	if err != nil {
		return "http://" + address.String()
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port)
}

func listenerPort(address net.Addr) (int, error) {
	_, rawPort, err := net.SplitHostPort(address.String())
	if err != nil {
		return 0, fmt.Errorf("parse listener address %q: %w", address.String(), err)
	}
	port, err := strconv.Atoi(rawPort)
	if err != nil {
		return 0, fmt.Errorf("parse listener port %q: %w", rawPort, err)
	}
	return port, nil
}

func listenerExcludesLAN(address net.Addr) bool {
	host, _, err := net.SplitHostPort(address.String())
	if err != nil {
		return false
	}
	parsed := net.ParseIP(host)
	return parsed != nil && parsed.IsLoopback()
}
