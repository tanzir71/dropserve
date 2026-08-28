package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/tanzir71/dropserve/internal/dashboard"
	dropserver "github.com/tanzir71/dropserve/internal/server"
	"github.com/tanzir71/dropserve/internal/state"
	"github.com/tanzir71/dropserve/internal/tlsca"
)

type localHTTPSOptions struct {
	Bind           string
	PreferredPort  int
	StateDirectory string
	Hostname       string
	Addresses      func() []netip.Addr
	Handler        func() http.Handler
	PersistPort    func(int) error
	TrustStore     tlsca.TrustStore
}

type localHTTPSController struct {
	mu             sync.RWMutex
	options        localHTTPSOptions
	authority      *tlsca.Authority
	runtime        *dropserver.HTTPSRuntime
	listener       net.Listener
	port           int
	trustInstalled bool
	warning        string
}

func newLocalHTTPSController(options localHTTPSOptions) *localHTTPSController {
	return &localHTTPSController{options: options}
}

func (controller *localHTTPSController) Status() dashboard.LocalHTTPSStatus {
	controller.mu.RLock()
	defer controller.mu.RUnlock()
	enabled := controller.runtime != nil && controller.runtime.Healthy()
	warning := controller.warning
	if controller.runtime != nil && !enabled && controller.runtime.LastError() != nil {
		warning = "The local HTTPS listener stopped: " + controller.runtime.LastError().Error()
	}
	rootAvailable := false
	if controller.options.StateDirectory != "" {
		_, rootErr := os.Stat(controller.rootPath())
		rootAvailable = rootErr == nil
	}
	return dashboard.LocalHTTPSStatus{
		Enabled:        enabled,
		Port:           controller.port,
		TrustInstalled: controller.trustInstalled,
		RootAvailable:  rootAvailable,
		Warning:        warning,
	}
}

func (controller *localHTTPSController) SetEnabled(ctx context.Context, enabled bool) error {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	if enabled {
		return controller.enableLocked(ctx)
	}
	return controller.disableLocked()
}

func (controller *localHTTPSController) enableLocked(ctx context.Context) error {
	if controller.runtime != nil && controller.runtime.Healthy() {
		return nil
	}
	if controller.options.StateDirectory == "" {
		return controller.failLocked(errors.New("a state path is required to store local HTTPS certificates"))
	}
	if controller.options.Handler == nil || controller.options.Handler() == nil {
		return controller.failLocked(errors.New("the Dropserve handler is not ready"))
	}
	port := controller.options.PreferredPort
	if port == 0 {
		port = 443
	}
	if port < 1 || port > 65_535 {
		return controller.failLocked(fmt.Errorf("HTTPS port %d is invalid", port))
	}
	listener, err := (&net.ListenConfig{}).Listen(
		ctx,
		"tcp",
		net.JoinHostPort(controller.options.Bind, strconv.Itoa(port)),
	)
	if err != nil {
		return controller.failLocked(fmt.Errorf("open HTTPS port %d: %w", port, err))
	}
	addresses := []netip.Addr{}
	if controller.options.Addresses != nil {
		addresses = controller.options.Addresses()
	}
	authority, err := tlsca.New(tlsca.Options{
		Directory: filepath.Join(controller.options.StateDirectory, "ca"),
		Hostname:  controller.options.Hostname,
		Addresses: addresses,
	})
	if err != nil {
		_ = listener.Close()
		return controller.failLocked(err)
	}
	if controller.options.PersistPort != nil {
		if err := controller.options.PersistPort(port); err != nil {
			_ = listener.Close()
			return controller.failLocked(fmt.Errorf("save HTTPS setting: %w", err))
		}
	}
	runtime := dropserver.NewHTTPSRuntime(controller.options.Handler(), authority.TLSCertificate)
	runtime.Start(listener)
	controller.authority = authority
	controller.runtime = runtime
	controller.listener = listener
	controller.port = port
	controller.options.PreferredPort = port
	controller.warning = ""
	return nil
}

func (controller *localHTTPSController) disableLocked() error {
	if controller.options.PersistPort != nil {
		if err := controller.options.PersistPort(0); err != nil {
			return controller.failLocked(fmt.Errorf("save HTTPS setting: %w", err))
		}
	}
	runtime := controller.runtime
	listener := controller.listener
	controller.runtime = nil
	controller.listener = nil
	controller.port = 0
	controller.options.PreferredPort = 0
	controller.warning = ""
	if listener != nil {
		_ = listener.Close()
	}
	if runtime != nil {
		go func() {
			shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = runtime.Shutdown(shutdownContext)
		}()
	}
	return nil
}

func (controller *localHTTPSController) SetTrust(installed bool) error {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	if controller.options.StateDirectory == "" {
		return controller.failLocked(errors.New("enable local HTTPS before changing trust"))
	}
	rootPath := controller.rootPath()
	if _, err := os.Stat(rootPath); err != nil {
		return controller.failLocked(fmt.Errorf("enable local HTTPS before changing trust: %w", err))
	}
	trust := tlsca.NewTrustController(rootPath, controller.options.TrustStore)
	var err error
	if installed {
		err = trust.Install()
	} else {
		err = trust.Uninstall()
	}
	if err != nil {
		return controller.failLocked(err)
	}
	controller.trustInstalled = installed
	controller.warning = ""
	return nil
}

func (controller *localHTTPSController) RootCertificate() ([]byte, error) {
	controller.mu.RLock()
	defer controller.mu.RUnlock()
	if controller.options.StateDirectory == "" {
		return nil, errors.New("enable local HTTPS before downloading its certificate")
	}
	content, err := os.ReadFile(controller.rootPath()) // #nosec G304 -- fixed filename under Dropserve's state directory.
	if err != nil {
		return nil, fmt.Errorf("read local root certificate: %w", err)
	}
	return content, nil
}

func (controller *localHTTPSController) UpdateAddresses(addresses []netip.Addr) error {
	controller.mu.RLock()
	authority := controller.authority
	controller.mu.RUnlock()
	if authority == nil {
		return nil
	}
	_, err := authority.UpdateAddresses(addresses)
	return err
}

func (controller *localHTTPSController) Shutdown(ctx context.Context) error {
	controller.mu.Lock()
	runtime := controller.runtime
	listener := controller.listener
	controller.runtime = nil
	controller.listener = nil
	controller.port = 0
	controller.mu.Unlock()
	if listener != nil {
		_ = listener.Close()
	}
	if runtime == nil {
		return nil
	}
	return runtime.Shutdown(ctx)
}

func (controller *localHTTPSController) rootPath() string {
	return filepath.Join(controller.options.StateDirectory, "ca", "root.pem")
}

func (controller *localHTTPSController) failLocked(err error) error {
	controller.warning = "Local HTTPS is off: " + err.Error()
	return err
}

func trustCommand(arguments []string, stdout, stderr io.Writer) int {
	statePath, err := state.DefaultPath()
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "Dropserve could not find its state folder: %v\n", err)
		return 1
	}
	return trustCommandWithStore(arguments, stdout, stderr, statePath, nil)
}

func trustCommandWithStore(
	arguments []string,
	stdout io.Writer,
	stderr io.Writer,
	statePath string,
	store tlsca.TrustStore,
) int {
	if len(arguments) != 1 {
		_, _ = fmt.Fprintln(stderr, "Choose a trust action: dropserve trust --install or dropserve trust --uninstall")
		return 2
	}
	rootPath := filepath.Join(filepath.Dir(statePath), "ca", "root.pem")
	if _, err := os.Stat(rootPath); err != nil {
		_, _ = fmt.Fprintln(stderr, "Enable local HTTPS before changing trust; its certificate files do not exist yet.")
		return 1
	}
	controller := tlsca.NewTrustController(rootPath, store)
	switch arguments[0] {
	case "--install", "install":
		if err := controller.Install(); err != nil {
			_, _ = fmt.Fprintf(stderr, "Dropserve could not add its local HTTPS certificate to this computer: %v\n", err)
			return 1
		}
		_, _ = fmt.Fprintln(stdout, "This computer now trusts Dropserve's local HTTPS certificate. This affects only this computer; run 'dropserve trust --uninstall' to stop trusting it.")
		return 0
	case "--uninstall", "uninstall":
		if err := controller.Uninstall(); err != nil {
			_, _ = fmt.Fprintf(stderr, "Dropserve could not stop this computer from trusting its local HTTPS certificate: %v\n", err)
			return 1
		}
		_, _ = fmt.Fprintln(stdout, "This computer no longer trusts Dropserve's local HTTPS certificate. Dropserve's certificate files and your apps are unchanged.")
		return 0
	default:
		_, _ = fmt.Fprintln(stderr, "Choose a trust action: dropserve trust --install or dropserve trust --uninstall")
		return 2
	}
}
