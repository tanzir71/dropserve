// Package firstrun owns the one-time setup screen and example-app install.
package firstrun

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tanzir71/dropserve/internal/config"
	"github.com/tanzir71/dropserve/internal/state"
)

// ExampleDirectory is the app folder created during successful setup.
const ExampleDirectory = "Welcome to Dropserve"

//go:embed assets/wizard.html assets/example/index.html
var assets embed.FS

var wizardTemplate = template.Must(template.ParseFS(assets, "assets/wizard.html"))

// Options supplies the private paths and platform actions used during setup.
type Options struct {
	StatePath       string
	ConfigPath      string
	DefaultAppsRoot string
	Executable      string
	OpenBrowser     func(string) error
	EnableAutostart func(string) error
}

// Result describes whether setup was shown and the choices that were saved.
type Result struct {
	Shown     bool
	AppsRoot  string
	Autostart bool
}

type completion struct {
	result Result
	err    error
}

// Needed reports true only when the state file is absent.
func Needed(statePath string) (bool, error) {
	info, err := os.Stat(statePath)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect first-run state %q: %w", statePath, err)
	}
	if info.IsDir() {
		return false, fmt.Errorf("first-run state path %q is a directory", statePath)
	}
	return false, nil
}

// Run serves the first-run screen until setup succeeds. A present state file
// returns immediately without inspecting or repairing any setup output.
func Run(ctx context.Context, options Options) (Result, error) {
	needed, err := Needed(options.StatePath)
	if err != nil || !needed {
		return Result{}, err
	}
	if options.OpenBrowser == nil {
		return Result{}, errors.New("first-run browser opener is not configured")
	}

	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(ctx, "tcp4", "127.0.0.1:0")
	if err != nil {
		return Result{}, fmt.Errorf("start first-run screen: %w", err)
	}
	completed := make(chan completion, 1)
	var submitMu sync.Mutex
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", func(response http.ResponseWriter, _ *http.Request) {
		renderWizard(response, options.DefaultAppsRoot, "")
	})
	mux.HandleFunc("POST /start", func(response http.ResponseWriter, request *http.Request) {
		submitMu.Lock()
		defer submitMu.Unlock()
		request.Body = http.MaxBytesReader(response, request.Body, 1<<20)
		if err := request.ParseForm(); err != nil {
			renderWizard(response, options.DefaultAppsRoot, "That form was too large. Choose an Apps folder and try again.")
			return
		}
		appsRoot := strings.TrimSpace(request.FormValue("apps_root"))
		if appsRoot == "" {
			renderWizard(response, options.DefaultAppsRoot, "Choose where Dropserve should keep your apps.")
			return
		}
		absoluteRoot, err := filepath.Abs(appsRoot)
		if err != nil {
			renderWizard(response, appsRoot, "Dropserve could not understand that folder. Choose another location.")
			return
		}
		startAtLogin := request.FormValue("autostart") == "on"
		initialized, err := complete(options, absoluteRoot, startAtLogin)
		if err != nil {
			renderWizard(response, absoluteRoot, fmt.Sprintf("Dropserve could not finish setup: %v", err))
			return
		}
		if !initialized {
			writeSetupComplete(response)
			completed <- completion{result: Result{}}
			return
		}
		writeSetupComplete(response)
		completed <- completion{result: Result{Shown: true, AppsRoot: absoluteRoot, Autostart: startAtLogin}}
	})

	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	serveResult := make(chan error, 1)
	go func() {
		err := server.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		serveResult <- err
	}()
	address := "http://" + listener.Addr().String() + "/"
	if err := options.OpenBrowser(address); err != nil {
		_ = server.Close()
		return Result{}, fmt.Errorf("open first-run screen: %w", err)
	}

	select {
	case outcome := <-completed:
		shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownContext); err != nil {
			return Result{}, fmt.Errorf("close first-run screen: %w", err)
		}
		return outcome.result, outcome.err
	case err := <-serveResult:
		return Result{}, err
	case <-ctx.Done():
		_ = server.Close()
		return Result{}, ctx.Err()
	}
}

func complete(options Options, appsRoot string, startAtLogin bool) (bool, error) {
	needed, err := Needed(options.StatePath)
	if err != nil || !needed {
		return false, err
	}
	if err := os.MkdirAll(appsRoot, 0o750); err != nil {
		return false, fmt.Errorf("create Apps folder: %w", err)
	}
	if err := installExample(appsRoot); err != nil {
		return false, err
	}
	configuration, err := config.Load(options.ConfigPath)
	if err != nil {
		return false, err
	}
	configuration.Server.AppsRoots = []string{appsRoot}
	if err := config.Save(options.ConfigPath, configuration); err != nil {
		return false, err
	}
	if startAtLogin {
		if options.EnableAutostart == nil {
			return false, errors.New("start-at-login support is not configured")
		}
		if err := options.EnableAutostart(options.Executable); err != nil {
			return false, fmt.Errorf("enable start at login: %w", err)
		}
	}
	if err := state.Save(options.StatePath, state.State{}); err != nil {
		return false, err
	}
	return true, nil
}

func installExample(appsRoot string) error {
	target := filepath.Join(appsRoot, ExampleDirectory)
	if _, err := os.Stat(target); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect example app: %w", err)
	}
	if err := os.Mkdir(target, 0o750); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return nil
		}
		return fmt.Errorf("create example app: %w", err)
	}
	created := true
	defer func() {
		if created {
			_ = os.RemoveAll(target)
		}
	}()
	data, err := assets.ReadFile("assets/example/index.html")
	if err != nil {
		return fmt.Errorf("read embedded example app: %w", err)
	}
	if err := os.WriteFile(filepath.Join(target, "index.html"), data, 0o600); err != nil {
		return fmt.Errorf("write example app: %w", err)
	}
	created = false
	return nil
}

func renderWizard(response http.ResponseWriter, appsRoot, message string) {
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := wizardTemplate.Execute(response, struct {
		AppsRoot string
		Message  string
	}{AppsRoot: appsRoot, Message: message}); err != nil {
		http.Error(response, "Dropserve could not draw the setup screen.", http.StatusInternalServerError)
	}
}

func writeSetupComplete(response http.ResponseWriter) {
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	response.Header().Set("Content-Length", strconv.Itoa(len(setupCompletePage)))
	_, _ = fmt.Fprint(response, setupCompletePage)
	_ = http.NewResponseController(response).Flush()
}

const setupCompletePage = `<!doctype html><html lang="en"><meta charset="utf-8"><title>Dropserve is starting</title><body><h1>Dropserve is starting</h1><p>Your dashboard will open in a moment. You can close this tab.</p></body></html>`
