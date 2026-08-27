// Package router maps the first URL path segment to immutable app mounts.
package router

import (
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/tanzir71/dropserve/internal/app"
)

// Mount connects a discovered app with the handler that serves it.
type Mount struct {
	App     app.App
	Handler http.Handler
}

// Table is an immutable snapshot of all active mounts.
type Table struct {
	mounts map[string]Mount
}

// Router atomically swaps immutable mount-table snapshots.
type Router struct {
	table atomic.Pointer[Table]
}

// New creates a router with the supplied mount snapshot.
func New(mounts []Mount) *Router {
	router := &Router{}
	router.Swap(mounts)
	return router
}

// Swap replaces the live table without mutating handlers used by in-flight requests.
func (router *Router) Swap(mounts []Mount) {
	table := &Table{mounts: make(map[string]Mount, len(mounts))}
	for _, mount := range mounts {
		if mount.Handler == nil {
			continue
		}
		table.mounts[mount.App.Slug] = mount
	}
	router.table.Store(table)
}

// ServeHTTP dispatches one request using a single immutable table snapshot.
func (router *Router) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	table := router.table.Load()
	if table == nil {
		http.NotFound(response, request)
		return
	}

	trimmed := strings.TrimPrefix(request.URL.Path, "/")
	slug, remainder, hasSlash := strings.Cut(trimmed, "/")
	mount, found := table.mounts[slug]
	if !found || slug == "" {
		http.NotFound(response, request)
		return
	}
	if !hasSlash {
		target := "/" + slug + "/"
		if request.URL.RawQuery != "" {
			target += "?" + request.URL.RawQuery
		}
		response.Header().Set("Location", target)
		response.WriteHeader(http.StatusMovedPermanently)
		return
	}

	proxied := request.Clone(request.Context())
	proxied.URL.Path = "/" + remainder
	proxied.URL.RawPath = ""
	mount.Handler.ServeHTTP(response, proxied)
}
