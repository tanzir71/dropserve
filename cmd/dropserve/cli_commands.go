package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/tanzir71/dropserve/internal/config"
	"github.com/tanzir71/dropserve/internal/discovery"
	"github.com/tanzir71/dropserve/internal/launch"
	"github.com/tanzir71/dropserve/internal/state"
	"github.com/tanzir71/dropserve/internal/supervisor"
)

const maximumCLIResponse = 4 << 20

type localAPI struct {
	baseURL string
	client  *http.Client
	token   string
}

func newLocalAPI(statePath string) (*localAPI, error) {
	if statePath == "" {
		var err error
		statePath, err = state.DefaultPath()
		if err != nil {
			return nil, err
		}
	}
	snapshot, err := state.Load(statePath)
	if err != nil {
		return nil, err
	}
	if snapshot.HTTPPort < 1 || snapshot.HTTPPort > 65535 {
		return nil, errors.New("dropserve is not running; no local HTTP port is recorded")
	}
	return localAPIForPort(snapshot.HTTPPort), nil
}

func localAPIForPort(port int) *localAPI {
	transport := &http.Transport{Proxy: nil}
	return &localAPI{
		baseURL: fmt.Sprintf("http://127.0.0.1:%d", port),
		client:  &http.Client{Transport: transport, Timeout: 30 * time.Second},
	}
}

func (api *localAPI) get(ctx context.Context, path string, result any) error {
	// #nosec G704 -- baseURL is fixed to validated loopback state and callers supply only Dropserve-owned API paths.
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, api.baseURL+path, nil)
	if err != nil {
		return err
	}
	// #nosec G704 -- the transport has no proxy and the request authority is validated loopback state.
	response, err := api.client.Do(request)
	if err != nil {
		return fmt.Errorf("reach the running Dropserve server: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return responseError(response)
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, maximumCLIResponse)).Decode(result); err != nil {
		return fmt.Errorf("decode Dropserve response: %w", err)
	}
	return nil
}

func (api *localAPI) post(ctx context.Context, path string, payload any) error {
	if api.token == "" {
		var status struct {
			CSRFToken string `json:"csrf_token"`
		}
		if err := api.get(ctx, "/_dropserve/api/status", &status); err != nil {
			return err
		}
		if status.CSRFToken == "" {
			return errors.New("the running Dropserve server did not provide a security token")
		}
		api.token = status.CSRFToken
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	// #nosec G704 -- baseURL is fixed to validated loopback state and callers supply only Dropserve-owned API paths.
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, api.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", api.baseURL)
	request.Header.Set("X-Dropserve-CSRF", api.token)
	// #nosec G704 -- the transport has no proxy and the request authority is validated loopback state.
	response, err := api.client.Do(request)
	if err != nil {
		return fmt.Errorf("reach the running Dropserve server: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusNoContent {
		return responseError(response)
	}
	return nil
}

func (api *localAPI) health(ctx context.Context) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, api.baseURL+"/_dropserve/healthz", nil)
	if err != nil {
		return err
	}
	response, err := api.client.Do(request)
	if err != nil {
		return fmt.Errorf("reach the running Dropserve server: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	body, readErr := io.ReadAll(io.LimitReader(response.Body, 16))
	if readErr != nil || response.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) != "ok" {
		return fmt.Errorf("local health check returned HTTP %d", response.StatusCode)
	}
	return nil
}

func responseError(response *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(response.Body, 16<<10))
	detail := strings.TrimSpace(string(body))
	if detail == "" {
		detail = response.Status
	}
	return fmt.Errorf("dropserve returned HTTP %d: %s", response.StatusCode, detail)
}

func openCommand(arguments []string, _ io.Writer, stderr io.Writer) int {
	if len(arguments) != 0 {
		_, _ = fmt.Fprintln(stderr, "Use: dropserve open")
		return 2
	}
	api, err := newLocalAPI("")
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "Dropserve could not open the dashboard: %v\n", err)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := api.health(ctx); err != nil {
		_, _ = fmt.Fprintf(stderr, "Dropserve could not open the dashboard: %v\n", err)
		return 1
	}
	if err := launch.OpenURL(api.baseURL + "/"); err != nil {
		_, _ = fmt.Fprintf(stderr, "Dropserve could not open the dashboard: %v\n", err)
		return 1
	}
	return 0
}

type cliApp struct {
	Slug   string `json:"slug"`
	Name   string `json:"name"`
	Type   string `json:"type"`
	Status string `json:"status"`
	URLs   struct {
		Path string `json:"path"`
		Own  string `json:"own"`
	} `json:"urls"`
}

func appsCommand(arguments []string, stdout, stderr io.Writer) int {
	if len(arguments) != 0 {
		_, _ = fmt.Fprintln(stderr, "Use: dropserve apps")
		return 2
	}
	api, err := newLocalAPI("")
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "Dropserve could not list apps: %v\n", err)
		return 1
	}
	var applications []cliApp
	if err := api.get(context.Background(), "/_dropserve/api/apps", &applications); err != nil {
		_, _ = fmt.Fprintf(stderr, "Dropserve could not list apps: %v\n", err)
		return 1
	}
	writer := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(writer, "NAME\tSLUG\tTYPE\tSTATUS\tURL")
	for _, application := range applications {
		address := api.baseURL + application.URLs.Path
		if application.URLs.Own != "" {
			address = application.URLs.Own
		}
		_, _ = fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\n", application.Name, application.Slug, application.Type, application.Status, address)
	}
	if err := writer.Flush(); err != nil {
		return 1
	}
	return 0
}

func logsCommand(arguments []string, stdout, stderr io.Writer) int {
	follow := false
	var slug string
	for _, argument := range arguments {
		switch argument {
		case "-f", "--follow":
			follow = true
		default:
			if slug != "" {
				_, _ = fmt.Fprintln(stderr, "Use: dropserve logs <slug> [-f]")
				return 2
			}
			slug = argument
		}
	}
	if slug == "" {
		_, _ = fmt.Fprintln(stderr, "Use: dropserve logs <slug> [-f]")
		return 2
	}
	api, err := newLocalAPI("")
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "Dropserve could not read %s logs: %v\n", slug, err)
		return 1
	}
	ctx := context.Background()
	previous := ""
	for {
		var snapshot supervisor.Snapshot
		if err := api.get(ctx, "/_dropserve/api/logs/"+url.PathEscape(slug), &snapshot); err != nil {
			_, _ = fmt.Fprintf(stderr, "Dropserve could not read %s logs: %v\n", slug, err)
			return 1
		}
		addition := snapshot.Logs
		if strings.HasPrefix(snapshot.Logs, previous) {
			addition = strings.TrimPrefix(snapshot.Logs, previous)
		}
		if addition != "" {
			_, _ = io.WriteString(stdout, addition)
		}
		previous = snapshot.Logs
		if !follow {
			return 0
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func restartCommand(arguments []string, stdout, stderr io.Writer) int {
	if len(arguments) != 1 {
		_, _ = fmt.Fprintln(stderr, "Use: dropserve restart <slug>")
		return 2
	}
	api, err := newLocalAPI("")
	if err == nil {
		err = api.post(context.Background(), "/_dropserve/api/apps/"+url.PathEscape(arguments[0])+"/restart", struct{}{})
	}
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "Dropserve could not restart %s: %v\n", arguments[0], err)
		return 1
	}
	_, _ = fmt.Fprintf(stdout, "Restarted %s.\n", arguments[0])
	return 0
}

func tailscaleCommand(arguments []string, stdout, stderr io.Writer) int {
	if len(arguments) == 0 || len(arguments) > 2 {
		_, _ = fmt.Fprintln(stderr, "Use: dropserve tailscale status|serve|unserve|funnel <slug>|unfunnel <slug>")
		return 2
	}
	action := arguments[0]
	if len(arguments) == 1 && action == "status" {
		status, err := discovery.ProbeTailscale(context.Background(), discovery.TailscaleProbes{})
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "Dropserve could not read Tailscale status: %v\n", err)
			return 1
		}
		output := struct {
			State   string `json:"state"`
			Host    string `json:"host,omitempty"`
			Message string `json:"message,omitempty"`
		}{State: status.BackendState, Host: status.Host, Message: status.Message}
		if err := json.NewEncoder(stdout).Encode(output); err != nil {
			return 1
		}
		return 0
	}
	api, err := newLocalAPI("")
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "Dropserve could not change Tailscale sharing: %v\n", err)
		return 1
	}
	var path string
	var payload any
	switch action {
	case "serve", "unserve":
		if len(arguments) != 1 {
			_, _ = fmt.Fprintln(stderr, "Use: dropserve tailscale serve|unserve")
			return 2
		}
		path = "/_dropserve/api/sharing/tailscale"
		payload = map[string]bool{"enabled": action == "serve"}
	case "funnel", "unfunnel":
		if len(arguments) != 2 || arguments[1] == "" {
			_, _ = fmt.Fprintln(stderr, "Use: dropserve tailscale funnel|unfunnel <slug>")
			return 2
		}
		path = "/_dropserve/api/sharing/funnel/" + url.PathEscape(arguments[1])
		payload = map[string]any{"enabled": action == "funnel"}
		if action == "funnel" {
			payload.(map[string]any)["confirmation"] = arguments[1]
		}
	default:
		_, _ = fmt.Fprintln(stderr, "Use: dropserve tailscale status|serve|unserve|funnel <slug>|unfunnel <slug>")
		return 2
	}
	if err := api.post(context.Background(), path, payload); err != nil {
		_, _ = fmt.Fprintf(stderr, "Dropserve could not change Tailscale sharing: %v\n", err)
		return 1
	}
	_, _ = fmt.Fprintln(stdout, "Tailscale sharing updated.")
	return 0
}

func runtimeCommand(arguments []string, stdout, stderr io.Writer) int {
	if len(arguments) != 2 || arguments[0] != "install" || !validRuntimeName(arguments[1]) {
		_, _ = fmt.Fprintln(stderr, "Use: dropserve runtime install php|mariadb|postgres")
		return 2
	}
	api, err := newLocalAPI("")
	if err == nil {
		api.client.Timeout = 0
		installContext, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		err = api.post(installContext, "/_dropserve/api/addons/"+arguments[1], map[string]string{"action": "install"})
		if err == nil {
			err = waitForRuntimeInstall(installContext, api, arguments[1])
		}
	}
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "Dropserve could not install %s: %v\n", arguments[1], err)
		return 1
	}
	_, _ = fmt.Fprintf(stdout, "Installed %s.\n", arguments[1])
	return 0
}

type cliAddonStatus struct {
	Name      string `json:"name"`
	Available bool   `json:"available"`
	Installed bool   `json:"installed"`
	Busy      bool   `json:"busy"`
	Message   string `json:"message"`
}

func waitForRuntimeInstall(ctx context.Context, api *localAPI, name string) error {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		var statuses []cliAddonStatus
		if err := api.get(ctx, "/_dropserve/api/addons", &statuses); err != nil {
			return err
		}
		found := false
		for _, status := range statuses {
			if status.Name != name {
				continue
			}
			found = true
			if !status.Busy {
				if status.Message != "" {
					return errors.New(status.Message)
				}
				if status.Installed {
					return nil
				}
				return fmt.Errorf("the %s install stopped before the runtime was registered", name)
			}
			break
		}
		if !found {
			return fmt.Errorf("runtime %s is not available on this computer", name)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for %s installation: %w", name, ctx.Err())
		case <-ticker.C:
		}
	}
}

func validRuntimeName(name string) bool {
	return name == "php" || name == "mariadb" || name == "postgres"
}

func configCommand(arguments []string, stdout, stderr io.Writer, injectedPath string) int {
	if len(arguments) != 1 {
		_, _ = fmt.Fprintln(stderr, "Use: dropserve config path|validate|edit")
		return 2
	}
	configPath := injectedPath
	if configPath == "" {
		var err error
		configPath, err = config.DefaultPath()
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "Dropserve could not find its configuration: %v\n", err)
			return 1
		}
	}
	switch arguments[0] {
	case "path":
		_, _ = fmt.Fprintln(stdout, configPath)
		return 0
	case "validate":
		if _, err := config.Load(configPath); err != nil {
			_, _ = fmt.Fprintf(stderr, "Dropserve configuration is invalid: %v\n", err)
			return 1
		}
		_, _ = fmt.Fprintf(stdout, "Configuration is valid: %s\n", configPath)
		return 0
	case "edit":
		if _, err := os.Stat(configPath); errors.Is(err, os.ErrNotExist) {
			if err := config.Save(configPath, config.Default()); err != nil {
				_, _ = fmt.Fprintf(stderr, "Dropserve could not create its configuration: %v\n", err)
				return 1
			}
		} else if err != nil {
			_, _ = fmt.Fprintf(stderr, "Dropserve could not read its configuration: %v\n", err)
			return 1
		}
		if err := launch.OpenPath(configPath); err != nil {
			_, _ = fmt.Fprintf(stderr, "Dropserve could not open its configuration: %v\n", err)
			return 1
		}
		return 0
	default:
		_, _ = fmt.Fprintln(stderr, "Use: dropserve config path|validate|edit")
		return 2
	}
}

func firewallCommand(arguments []string, stdout, stderr io.Writer) int {
	if len(arguments) != 1 || arguments[0] != "allow" {
		_, _ = fmt.Fprintln(stderr, "Use: dropserve firewall allow")
		return 2
	}
	if runtime.GOOS != "windows" {
		_, _ = fmt.Fprintln(stderr, "The firewall command is only needed on Windows.")
		return 1
	}
	executable, err := os.Executable()
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "Dropserve could not locate its executable: %v\n", err)
		return 1
	}
	desktop := filepath.Join(filepath.Dir(executable), "dropserve.exe")
	if _, err := os.Stat(desktop); err != nil {
		desktop = executable
	}
	commands := [][]string{
		{"advfirewall", "firewall", "delete", "rule", "name=Dropserve"},
		{"advfirewall", "firewall", "add", "rule", "name=Dropserve", "dir=in", "action=allow", "program=" + desktop, "enable=yes", "profile=private"},
	}
	for _, arguments := range commands {
		commandContext, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		// #nosec G204 -- netsh is fixed and the executable path comes from os.Executable.
		output, commandErr := exec.CommandContext(commandContext, "netsh.exe", arguments...).CombinedOutput()
		cancel()
		if commandErr != nil {
			_, _ = fmt.Fprintf(stderr, "Dropserve could not update the Windows firewall rule. Run this command from an Administrator terminal: %v: %s\n", commandErr, strings.TrimSpace(string(output)))
			return 1
		}
	}
	_, _ = fmt.Fprintln(stdout, "Windows now allows Dropserve on private networks.")
	return 0
}
