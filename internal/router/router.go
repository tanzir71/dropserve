// Package router maps the first URL path segment to immutable app mounts.
package router

import (
	"net"
	"net/http"
	"net/netip"
	"strconv"
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
		serveNotFound(response, request)
		return
	}

	trimmed := strings.TrimPrefix(request.URL.Path, "/")
	slug, remainder, hasSlash := strings.Cut(trimmed, "/")
	mount, found := table.mounts[slug]
	if !found || slug == "" {
		serveNotFound(response, request)
		return
	}
	if !visibilityAllows(mount.App.Visibility, request.RemoteAddr) {
		serveVisibilityDenied(response, request, mount.App.Visibility)
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

var tailnetPrefix = netip.MustParsePrefix("100.64.0.0/10")

func visibilityAllows(visibility, remoteAddress string) bool {
	switch strings.ToLower(strings.TrimSpace(visibility)) {
	case "local":
		address, ok := remoteIP(remoteAddress)
		return ok && address.IsLoopback()
	case "tailnet":
		address, ok := remoteIP(remoteAddress)
		return ok && address.Is4() && tailnetPrefix.Contains(address)
	default:
		return true
	}
}

func remoteIP(remoteAddress string) (netip.Addr, bool) {
	host, _, err := net.SplitHostPort(remoteAddress)
	if err != nil {
		host = strings.Trim(remoteAddress, "[]")
	}
	address, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, false
	}
	return address.Unmap(), true
}

func serveVisibilityDenied(response http.ResponseWriter, request *http.Request, visibility string) {
	explanation := "This app is not available from your current network."
	switch strings.ToLower(strings.TrimSpace(visibility)) {
	case "local":
		explanation = "This app is set to local visibility and can only be opened on the Dropserve computer."
	case "tailnet":
		explanation = "This app is set to tailnet visibility and can only be opened through Tailscale."
	}
	content := `<!doctype html><html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>App access blocked · Dropserve</title><body><main><h1>Dropserve blocked this request.</h1><p>` + explanation + `</p><p>Change the app's <code>visibility</code> in <code>dropserve.json</code> if this was not intended.</p></main></body></html>`
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	response.Header().Set("Content-Length", strconv.Itoa(len(content)))
	response.WriteHeader(http.StatusForbidden)
	if request.Method != http.MethodHead {
		_, _ = response.Write([]byte(content))
	}
}

func serveNotFound(response http.ResponseWriter, request *http.Request) {
	const content = `<!doctype html><html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>App not found · Dropserve</title><body><main><h1>Dropserve could not find that app.</h1><p>Check the address, or return to <a href="/">your apps</a>.</p></main></body></html>`
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	response.Header().Set("Content-Length", strconv.Itoa(len(content)))
	response.WriteHeader(http.StatusNotFound)
	if request.Method == http.MethodHead {
		return
	}
	_, _ = response.Write([]byte(content))
}
