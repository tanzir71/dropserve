// Package server composes scanning, static handlers, and the atomic router.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/tanzir71/dropserve/internal/app"
	"github.com/tanzir71/dropserve/internal/dashboard"
	"github.com/tanzir71/dropserve/internal/discovery"
	"github.com/tanzir71/dropserve/internal/indexer"
	"github.com/tanzir71/dropserve/internal/router"
	"github.com/tanzir71/dropserve/internal/scanner"
	"github.com/tanzir71/dropserve/internal/sqlitebrowser"
	staticserver "github.com/tanzir71/dropserve/internal/static"
	"github.com/tanzir71/dropserve/internal/supervisor"
	"github.com/tanzir71/dropserve/internal/watcher"
)

type snapshot struct {
	scan      scanner.Result
	dashboard http.Handler
}

// Server keeps immutable scan and handler snapshots behind atomic swaps.
type Server struct {
	router      *router.Router
	http        http.Handler
	snapshot    atomic.Pointer[snapshot]
	watcher     *watcher.Watcher
	options     Options
	reconcileMu sync.Mutex
	rebuilds    atomic.Uint64
	events      *eventHub
	supervisor  *supervisor.Manager
}

// Options configures scanning and optional machine-state persistence.
type Options struct {
	Scanner              scanner.Options
	IndexPath            string
	Supervisor           supervisor.Options
	Warnings             []string
	Discovery            func() discovery.Snapshot
	Funnel               *discovery.FunnelManager
	SetTailscaleServe    func(context.Context, bool) error
	LocalHTTPSStatus     func() dashboard.LocalHTTPSStatus
	SetLocalHTTPS        func(context.Context, bool) error
	SetLocalTrust        func(bool) error
	RootCertificate      func() ([]byte, error)
	DismissNetworkChange func() error
	PHPHandler           func(app.App) (http.Handler, error)
	Addons               func() []dashboard.AddonStatus
	ChangeAddon          func(context.Context, string, string) error
}

// New scans the configured roots and registered apps, then mounts every app.
func New(options scanner.Options) (*Server, error) {
	return NewWithOptions(Options{Scanner: options})
}

// NewWithOptions creates a server and atomically persists its dashboard index when requested.
func NewWithOptions(options Options) (*Server, error) {
	supervisorOptions := options.Supervisor
	if options.IndexPath != "" {
		stateDirectory := filepath.Dir(options.IndexPath)
		if supervisorOptions.LogDirectory == "" {
			supervisorOptions.LogDirectory = filepath.Join(stateDirectory, "logs")
		}
		if supervisorOptions.PortPath == "" {
			supervisorOptions.PortPath = filepath.Join(stateDirectory, "ports.json")
		}
	}
	server := &Server{
		router:     router.New(nil),
		options:    options,
		events:     newEventHub(),
		supervisor: supervisor.NewManager(supervisorOptions),
	}
	handler := http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/_dropserve/api/events" {
			server.events.serveHTTP(response, request)
			return
		}
		if strings.HasPrefix(request.URL.Path, "/_dropserve/api/logs/") {
			server.serveCommandLogs(response, request)
			return
		}
		if request.URL.Path == "/" || strings.HasPrefix(request.URL.Path, "/_dropserve/") {
			current := server.snapshot.Load()
			if current == nil || current.dashboard == nil {
				http.Error(response, "Dropserve is preparing the dashboard.", http.StatusServiceUnavailable)
				return
			}
			current.dashboard.ServeHTTP(response, request)
			return
		}
		server.router.ServeHTTP(response, request)
	})
	server.http = handler
	if err := server.reconcile(); err != nil {
		return nil, err
	}
	liveWatcher, err := watcher.New(watcher.Options{
		Roots:     options.Scanner.Roots,
		Reconcile: server.reconcile,
	})
	if err != nil {
		return nil, err
	}
	server.watcher = liveWatcher
	return server, nil
}

// Handler returns the live HTTP handler.
func (server *Server) Handler() http.Handler {
	return server.http
}

// Scan returns the discovery snapshot used to build the current mount table.
func (server *Server) Scan() scanner.Result {
	current := server.snapshot.Load()
	if current == nil {
		return scanner.Result{}
	}
	return current.scan
}

// Close stops live filesystem watching.
func (server *Server) Close() error {
	var closeErrors []error
	if server.watcher != nil {
		closeErrors = append(closeErrors, server.watcher.Close())
	}
	if server.supervisor != nil {
		closeErrors = append(closeErrors, server.supervisor.Close())
	}
	return errors.Join(closeErrors...)
}

// RebuildCount returns the number of successfully published mount snapshots.
func (server *Server) RebuildCount() uint64 {
	return server.rebuilds.Load()
}

// Reconcile performs one full read-only scan and publishes the resulting routes.
func (server *Server) Reconcile() error {
	return server.reconcile()
}

func (server *Server) reconcile() error {
	server.reconcileMu.Lock()
	defer server.reconcileMu.Unlock()

	result, err := scanner.Scan(server.options.Scanner)
	if err != nil {
		return err
	}
	mounts := make([]router.Mount, 0, len(result.Apps))
	for applicationIndex := range result.Apps {
		application := result.Apps[applicationIndex]
		var applicationHandler http.Handler
		switch application.Kind {
		case app.KindCommand:
			applicationHandler, err = server.supervisor.Handler(application)
			if commandState, found := server.supervisor.Snapshot(application.Slug); found {
				application.Status = commandState.Status
				application.Port = commandState.Port
				application.PrefersOwnPort = commandState.PrefersOwnPort
				result.Apps[applicationIndex].Status = commandState.Status
				result.Apps[applicationIndex].Port = commandState.Port
				result.Apps[applicationIndex].PrefersOwnPort = commandState.PrefersOwnPort
			}
		case app.KindPHP:
			if server.options.PHPHandler == nil {
				application.Status = "needs-runtime"
				result.Apps[applicationIndex].Status = application.Status
				applicationHandler = needsPHPRuntimeHandler()
			} else {
				applicationHandler, err = server.options.PHPHandler(application)
				if err == nil && applicationHandler == nil {
					application.Status = "needs-runtime"
					result.Apps[applicationIndex].Status = application.Status
					applicationHandler = needsPHPRuntimeHandler()
				}
			}
		default:
			applicationHandler = staticserver.New(application)
		}
		if err != nil {
			return err
		}
		mounts = append(mounts, router.Mount{
			App:     application,
			Handler: applicationHandler,
		})
	}
	if err := server.supervisor.Reconcile(result.Apps); err != nil {
		return err
	}
	entries := indexer.Build(result.Apps)
	databasePaths := make(map[string]map[string]string)
	for _, application := range result.Apps {
		if len(application.Databases) == 0 {
			continue
		}
		files := make(map[string]string, len(application.Databases))
		for _, relative := range application.Databases {
			files[relative] = filepath.Join(application.Path, filepath.FromSlash(relative))
		}
		databasePaths[application.Slug] = files
	}
	if server.options.IndexPath != "" {
		if err := indexer.Save(server.options.IndexPath, entries); err != nil {
			return err
		}
	}
	warnings := append([]string{}, server.options.Warnings...)
	warnings = append(warnings, warningMessages(result.Warnings)...)
	dashboardHandler, err := dashboard.NewWithOptions(entries, dashboard.Options{
		Warnings:             warnings,
		Discovery:            server.options.Discovery,
		Funnel:               server.options.Funnel,
		SetTailscaleServe:    server.options.SetTailscaleServe,
		LocalHTTPSStatus:     server.options.LocalHTTPSStatus,
		SetLocalHTTPS:        server.options.SetLocalHTTPS,
		SetLocalTrust:        server.options.SetLocalTrust,
		RootCertificate:      server.options.RootCertificate,
		DismissNetworkChange: server.options.DismissNetworkChange,
		Addons:               server.options.Addons,
		ChangeAddon:          server.options.ChangeAddon,
		BrowseDatabase: func(ctx context.Context, slug, file string) (sqlitebrowser.Snapshot, error) {
			files, found := databasePaths[slug]
			if !found {
				return sqlitebrowser.Snapshot{}, errors.New("app has no discovered databases")
			}
			path, found := files[file]
			if !found {
				return sqlitebrowser.Snapshot{}, errors.New("database was not discovered in this app")
			}
			return sqlitebrowser.Browse(ctx, path)
		},
	})
	if err != nil {
		return err
	}
	server.router.Swap(mounts)
	server.snapshot.Store(&snapshot{scan: result, dashboard: dashboardHandler})
	server.rebuilds.Add(1)
	server.events.publish()
	return nil
}

func needsPHPRuntimeHandler() http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = response.Write([]byte(`<!doctype html><html lang="en"><meta charset="utf-8"><title>PHP needed · Dropserve</title><body><main><h1>This app needs PHP.</h1><p>Install the optional PHP pack from Dropserve Add-ons, then try again.</p></main></body></html>`))
	})
}

func (server *Server) serveCommandLogs(response http.ResponseWriter, request *http.Request) {
	slug := strings.TrimPrefix(request.URL.Path, "/_dropserve/api/logs/")
	if slug == "" || strings.Contains(slug, "/") {
		http.NotFound(response, request)
		return
	}
	commandState, found := server.supervisor.Snapshot(slug)
	if !found {
		http.NotFound(response, request)
		return
	}
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(response).Encode(commandState); err != nil {
		http.Error(response, "Dropserve could not encode these logs.", http.StatusInternalServerError)
	}
}

func warningMessages(warnings []scanner.Warning) []string {
	messages := make([]string, 0, len(warnings))
	for _, warning := range warnings {
		messages = append(messages, warning.Message)
	}
	return messages
}
