// Package server composes scanning, static handlers, and the atomic router.
package server

import (
	"net/http"
	"strings"

	"github.com/tanzir71/dropserve/internal/dashboard"
	"github.com/tanzir71/dropserve/internal/router"
	"github.com/tanzir71/dropserve/internal/scanner"
	staticserver "github.com/tanzir71/dropserve/internal/static"
)

// Server is one immutable scan snapshot behind an atomically swappable router.
type Server struct {
	router *router.Router
	scan   scanner.Result
	http   http.Handler
}

// New scans the configured roots and registered apps, then mounts every app.
func New(options scanner.Options) (*Server, error) {
	result, err := scanner.Scan(options)
	if err != nil {
		return nil, err
	}
	mounts := make([]router.Mount, 0, len(result.Apps))
	for _, application := range result.Apps {
		mounts = append(mounts, router.Mount{
			App:     application,
			Handler: staticserver.New(application),
		})
	}
	appRouter := router.New(mounts)
	dashboardHandler := dashboard.New()
	handler := http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/" || strings.HasPrefix(request.URL.Path, "/_dropserve/") {
			dashboardHandler.ServeHTTP(response, request)
			return
		}
		appRouter.ServeHTTP(response, request)
	})
	return &Server{router: appRouter, scan: result, http: handler}, nil
}

// Handler returns the live HTTP handler.
func (server *Server) Handler() http.Handler {
	return server.http
}

// Scan returns the discovery snapshot used to build the current mount table.
func (server *Server) Scan() scanner.Result {
	return server.scan
}
