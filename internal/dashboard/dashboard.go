// Package dashboard serves Dropserve's embedded, build-free web interface.
package dashboard

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	qrcode "github.com/skip2/go-qrcode"
	"github.com/tanzir71/dropserve/internal/indexer"
	"github.com/tanzir71/dropserve/internal/version"
)

//go:embed assets/*
var assets embed.FS

type handler struct {
	index      []byte
	stylesheet []byte
	script     []byte
	apps       []indexer.Entry
	started    time.Time
	csrfToken  string
	warnings   []string
}

// Options supplies runtime information displayed by the dashboard.
type Options struct {
	Warnings []string
}

// New returns the embedded dashboard handler.
func New(applications []indexer.Entry) (http.Handler, error) {
	return NewWithOptions(applications, Options{})
}

// NewWithOptions returns the embedded dashboard handler with runtime warnings.
func NewWithOptions(applications []indexer.Entry, options Options) (http.Handler, error) {
	index, _ := assets.ReadFile("assets/index.html")
	stylesheet, _ := assets.ReadFile("assets/app.css")
	script, _ := assets.ReadFile("assets/app.js")
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, fmt.Errorf("create dashboard security token: %w", err)
	}
	return &handler{
		index:      index,
		stylesheet: stylesheet,
		script:     script,
		apps:       append([]indexer.Entry(nil), applications...),
		started:    time.Now(),
		csrfToken:  hex.EncodeToString(tokenBytes),
		warnings:   append([]string{}, options.Warnings...),
	}, nil
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
	case "/_dropserve/api/qr":
		dashboard.serveQR(response, request)
		return
	case "/_dropserve/api/status":
		dashboard.serveStatus(response, request)
		return
	case "/_dropserve/healthz":
		dashboard.serveHealth(response, request)
		return
	default:
		if strings.HasPrefix(request.URL.Path, "/_dropserve/api/apps/") {
			dashboard.serveAppDetail(response, request)
			return
		}
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

func (dashboard *handler) serveHealth(response http.ResponseWriter, request *http.Request) {
	content := []byte("ok\n")
	response.Header().Set("Content-Type", "text/plain; charset=utf-8")
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("Content-Length", strconv.Itoa(len(content)))
	response.WriteHeader(http.StatusOK)
	if request.Method == http.MethodHead {
		return
	}
	_, _ = response.Write(content)
}

func (dashboard *handler) serveStatus(response http.ResponseWriter, request *http.Request) {
	type ports struct {
		HTTP int `json:"http"`
	}
	payload := struct {
		Version       string   `json:"version"`
		Commit        string   `json:"commit"`
		UptimeSeconds int64    `json:"uptime_seconds"`
		Ports         ports    `json:"ports"`
		Warnings      []string `json:"warnings"`
		CSRFToken     string   `json:"csrf_token"`
	}{
		Version:       version.Version,
		Commit:        version.Commit,
		UptimeSeconds: int64(time.Since(dashboard.started).Seconds()),
		Ports:         ports{HTTP: requestHTTPPort(request)},
		Warnings:      append([]string{}, dashboard.warnings...),
		CSRFToken:     dashboard.csrfToken,
	}
	dashboard.serveJSON(response, request, payload)
}

func requestHTTPPort(request *http.Request) int {
	_, portText, err := net.SplitHostPort(request.Host)
	if err == nil {
		port, conversionErr := strconv.Atoi(portText)
		if conversionErr == nil {
			return port
		}
	}
	if request.TLS != nil {
		return 443
	}
	return 80
}

func (dashboard *handler) serveAppDetail(response http.ResponseWriter, request *http.Request) {
	slug := strings.TrimPrefix(request.URL.Path, "/_dropserve/api/apps/")
	if slug == "" || strings.Contains(slug, "/") {
		http.NotFound(response, request)
		return
	}
	for _, entry := range dashboard.apps {
		if entry.Slug == slug {
			payload := struct {
				indexer.Entry
				Warnings []string `json:"warnings"`
			}{Entry: entry, Warnings: []string{}}
			dashboard.serveJSON(response, request, payload)
			return
		}
	}
	http.NotFound(response, request)
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

func (dashboard *handler) serveQR(response http.ResponseWriter, request *http.Request) {
	const maximumURLLength = 2_048
	target := request.URL.Query().Get("url")
	parsed, err := url.ParseRequestURI(target)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || len(target) > maximumURLLength {
		http.Error(response, "Choose a valid HTTP or HTTPS address for the QR code.", http.StatusBadRequest)
		return
	}
	content, err := qrcode.Encode(target, qrcode.Medium, 256)
	if err != nil {
		http.Error(response, "Dropserve could not create that QR code.", http.StatusInternalServerError)
		return
	}
	response.Header().Set("Content-Type", "image/png")
	response.Header().Set("Cache-Control", "private, max-age=300")
	response.Header().Set("Content-Length", strconv.Itoa(len(content)))
	response.WriteHeader(http.StatusOK)
	if request.Method == http.MethodHead {
		return
	}
	// #nosec G705 -- qrcode.Encode returns a binary PNG and the response is explicitly image/png.
	_, _ = response.Write(content)
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
