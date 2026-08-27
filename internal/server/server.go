// Package server composes scanning, static handlers, and the atomic router.
package server

import (
	"net/http"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/tanzir71/dropserve/internal/dashboard"
	"github.com/tanzir71/dropserve/internal/indexer"
	"github.com/tanzir71/dropserve/internal/router"
	"github.com/tanzir71/dropserve/internal/scanner"
	staticserver "github.com/tanzir71/dropserve/internal/static"
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
}

// Options configures scanning and optional machine-state persistence.
type Options struct {
	Scanner   scanner.Options
	IndexPath string
}

// New scans the configured roots and registered apps, then mounts every app.
func New(options scanner.Options) (*Server, error) {
	return NewWithOptions(Options{Scanner: options})
}

// NewWithOptions creates a server and atomically persists its dashboard index when requested.
func NewWithOptions(options Options) (*Server, error) {
	server := &Server{router: router.New(nil), options: options, events: newEventHub()}
	handler := http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/_dropserve/api/events" {
			server.events.serveHTTP(response, request)
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
	if server.watcher == nil {
		return nil
	}
	return server.watcher.Close()
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
	for _, application := range result.Apps {
		mounts = append(mounts, router.Mount{
			App:     application,
			Handler: staticserver.New(application),
		})
	}
	entries := indexer.Build(result.Apps)
	if server.options.IndexPath != "" {
		if err := indexer.Save(server.options.IndexPath, entries); err != nil {
			return err
		}
	}
	dashboardHandler, err := dashboard.NewWithOptions(entries, dashboard.Options{
		Warnings: warningMessages(result.Warnings),
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

func warningMessages(warnings []scanner.Warning) []string {
	messages := make([]string, 0, len(warnings))
	for _, warning := range warnings {
		messages = append(messages, warning.Message)
	}
	return messages
}
