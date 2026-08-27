// Package dashboard serves Dropserve's embedded, build-free web interface.
package dashboard

import (
	"embed"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/tanzir71/dropserve/internal/app"
	"github.com/tanzir71/dropserve/internal/indexer"
)

//go:embed assets/*
var assets embed.FS

type handler struct {
	index      []byte
	stylesheet []byte
	script     []byte
	apps       []indexer.Entry
}

// New returns the embedded dashboard handler.
func New(applications []app.App) http.Handler {
	index, _ := assets.ReadFile("assets/index.html")
	stylesheet, _ := assets.ReadFile("assets/app.css")
	script, _ := assets.ReadFile("assets/app.js")
	return &handler{
		index:      index,
		stylesheet: stylesheet,
		script:     script,
		apps:       indexer.Build(applications),
	}
}

func (dashboard *handler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		response.Header().Set("Allow", "GET, HEAD")
		http.Error(response, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var content []byte
	switch request.URL.Path {
	case "/":
		response.Header().Set("Content-Type", "text/html; charset=utf-8")
		response.Header().Set("Cache-Control", "no-store")
		content = dashboard.index
	case "/_dropserve/app.css":
		response.Header().Set("Content-Type", "text/css; charset=utf-8")
		response.Header().Set("Cache-Control", "public, max-age=300")
		content = dashboard.stylesheet
	case "/_dropserve/app.js":
		response.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		response.Header().Set("Cache-Control", "public, max-age=300")
		content = dashboard.script
	case "/_dropserve/api/apps":
		dashboard.serveJSON(response, request, dashboard.apps)
		return
	case "/_dropserve/api/search":
		dashboard.serveJSON(response, request, indexer.Search(dashboard.apps, request.URL.Query().Get("q")))
		return
	case "/_dropserve/api/urls":
		dashboard.serveAdvertisedURLs(response, request)
		return
	default:
		http.NotFound(response, request)
		return
	}
	response.Header().Set("X-Content-Type-Options", "nosniff")
	response.Header().Set(
		"Content-Security-Policy",
		"default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; connect-src 'self'",
	)
	response.Header().Set("Content-Length", strconv.Itoa(len(content)))
	response.WriteHeader(http.StatusOK)
	if request.Method == http.MethodHead {
		return
	}
	_, _ = response.Write(content)
}

type advertisedURL struct {
	Kind string `json:"kind"`
	URL  string `json:"url"`
}

func (dashboard *handler) serveAdvertisedURLs(response http.ResponseWriter, request *http.Request) {
	scheme := "http"
	if request.TLS != nil {
		scheme = "https"
	}
	currentURL := scheme + "://" + request.Host + "/"
	dashboard.serveJSON(response, request, []advertisedURL{{Kind: "current", URL: currentURL}})
}

func (dashboard *handler) serveJSON(response http.ResponseWriter, request *http.Request, value any) {
	content, err := json.Marshal(value)
	if err != nil {
		http.Error(response, "Dropserve could not encode its app index.", http.StatusInternalServerError)
		return
	}
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("Content-Length", strconv.Itoa(len(content)))
	response.WriteHeader(http.StatusOK)
	if request.Method == http.MethodHead {
		return
	}
	_, _ = response.Write(content)
}
