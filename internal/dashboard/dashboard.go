// Package dashboard serves Dropserve's embedded, build-free web interface.
package dashboard

import (
	"embed"
	"net/http"
)

//go:embed assets/*
var assets embed.FS

type handler struct {
	index      []byte
	stylesheet []byte
	script     []byte
}

// New returns the embedded dashboard handler.
func New() http.Handler {
	index, _ := assets.ReadFile("assets/index.html")
	stylesheet, _ := assets.ReadFile("assets/app.css")
	script, _ := assets.ReadFile("assets/app.js")
	return &handler{index: index, stylesheet: stylesheet, script: script}
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
	default:
		http.NotFound(response, request)
		return
	}
	response.Header().Set("X-Content-Type-Options", "nosniff")
	response.Header().Set(
		"Content-Security-Policy",
		"default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; connect-src 'self'",
	)
	response.Header().Set("Content-Length", contentLength(content))
	response.WriteHeader(http.StatusOK)
	if request.Method == http.MethodHead {
		return
	}
	_, _ = response.Write(content)
}

func contentLength(content []byte) string {
	const digits = "0123456789"
	if len(content) == 0 {
		return "0"
	}
	value := len(content)
	buffer := make([]byte, 0, 20)
	for value > 0 {
		buffer = append(buffer, digits[value%10])
		value /= 10
	}
	for left, right := 0, len(buffer)-1; left < right; left, right = left+1, right-1 {
		buffer[left], buffer[right] = buffer[right], buffer[left]
	}
	return string(buffer)
}
