// Package dashboard serves Dropserve's embedded, build-free web interface.
package dashboard

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	qrcode "github.com/skip2/go-qrcode"
	"github.com/tanzir71/dropserve/internal/discovery"
	"github.com/tanzir71/dropserve/internal/indexer"
	"github.com/tanzir71/dropserve/internal/sqlitebrowser"
	"github.com/tanzir71/dropserve/internal/version"
)

//go:embed assets/*
var assets embed.FS

type handler struct {
	index                []byte
	stylesheet           []byte
	script               []byte
	dhcpHelp             []byte
	apps                 []indexer.Entry
	appWarnings          map[string][]string
	started              time.Time
	csrfToken            string
	warnings             []string
	discovery            func() discovery.Snapshot
	funnel               *discovery.FunnelManager
	setTailscaleServe    func(context.Context, bool) error
	localHTTPSStatus     func() LocalHTTPSStatus
	setLocalHTTPS        func(context.Context, bool) error
	setLocalTrust        func(bool) error
	rootCertificate      func() ([]byte, error)
	dismissNetworkChange func() error
	browseDatabase       func(context.Context, string, string) (sqlitebrowser.Snapshot, error)
	addons               func() []AddonStatus
	changeAddon          func(context.Context, string, string) error
	restartApp           func(context.Context, string) error
	openFolder           func(context.Context, string) error
	rescan               func() error
	changeAppSettings    func(context.Context, string, AppSettingsChange) error
	update               func() UpdateNotice
}

// LocalHTTPSStatus is the live opt-in local TLS and trust state.
type LocalHTTPSStatus struct {
	Enabled        bool   `json:"enabled"`
	Port           int    `json:"port,omitempty"`
	TrustInstalled bool   `json:"trust_installed"`
	RootAvailable  bool   `json:"root_available"`
	Warning        string `json:"warning,omitempty"`
}

// AddonStatus is the dashboard-safe state of one optional runtime pack.
type AddonStatus struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Available   bool   `json:"available"`
	Installed   bool   `json:"installed"`
	Running     bool   `json:"running"`
	Busy        bool   `json:"busy"`
	Progress    int    `json:"progress,omitempty"`
	Connection  string `json:"connection,omitempty"`
	Message     string `json:"message,omitempty"`
}

// UpdateNotice is a metadata-only link to a newer release.
type UpdateNotice struct {
	Available bool   `json:"available"`
	Version   string `json:"version,omitempty"`
	URL       string `json:"url,omitempty"`
}

// AppSettingsChange carries only the dashboard preferences the user changed.
type AppSettingsChange struct {
	Pinned *bool `json:"pinned"`
	Hidden *bool `json:"hidden"`
}

// Options supplies runtime information displayed by the dashboard.
type Options struct {
	Title                string
	Theme                string
	Warnings             []string
	AppWarnings          map[string][]string
	Discovery            func() discovery.Snapshot
	Funnel               *discovery.FunnelManager
	SetTailscaleServe    func(context.Context, bool) error
	LocalHTTPSStatus     func() LocalHTTPSStatus
	SetLocalHTTPS        func(context.Context, bool) error
	SetLocalTrust        func(bool) error
	RootCertificate      func() ([]byte, error)
	DismissNetworkChange func() error
	BrowseDatabase       func(context.Context, string, string) (sqlitebrowser.Snapshot, error)
	Addons               func() []AddonStatus
	ChangeAddon          func(context.Context, string, string) error
	RestartApp           func(context.Context, string) error
	OpenFolder           func(context.Context, string) error
	Rescan               func() error
	ChangeAppSettings    func(context.Context, string, AppSettingsChange) error
	Update               func() UpdateNotice
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
	dhcpHelp, _ := assets.ReadFile("assets/dhcp.html")
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, fmt.Errorf("create dashboard security token: %w", err)
	}
	title := strings.TrimSpace(options.Title)
	if title == "" {
		title = "Dropserve"
	}
	theme := strings.ToLower(strings.TrimSpace(options.Theme))
	if theme != "light" && theme != "dark" {
		theme = "auto"
	}
	indexText := string(index)
	indexText = strings.Replace(indexText, "<title>Dropserve</title>", "<title>"+html.EscapeString(title)+"</title>", 1)
	indexText = strings.Replace(indexText, "<strong>Dropserve</strong>", "<strong>"+html.EscapeString(title)+"</strong>", 1)
	indexText = strings.Replace(indexText, `data-dropserve-theme="auto"`, `data-dropserve-theme="`+theme+`"`, 1)
	index = []byte(indexText)
	return &handler{
		index:                index,
		stylesheet:           stylesheet,
		script:               script,
		dhcpHelp:             dhcpHelp,
		apps:                 append([]indexer.Entry{}, applications...),
		appWarnings:          cloneAppWarnings(options.AppWarnings),
		started:              time.Now(),
		csrfToken:            hex.EncodeToString(tokenBytes),
		warnings:             append([]string{}, options.Warnings...),
		discovery:            options.Discovery,
		funnel:               options.Funnel,
		setTailscaleServe:    options.SetTailscaleServe,
		localHTTPSStatus:     options.LocalHTTPSStatus,
		setLocalHTTPS:        options.SetLocalHTTPS,
		setLocalTrust:        options.SetLocalTrust,
		rootCertificate:      options.RootCertificate,
		dismissNetworkChange: options.DismissNetworkChange,
		browseDatabase:       options.BrowseDatabase,
		addons:               options.Addons,
		changeAddon:          options.ChangeAddon,
		restartApp:           options.RestartApp,
		openFolder:           options.OpenFolder,
		rescan:               options.Rescan,
		changeAppSettings:    options.ChangeAppSettings,
		update:               options.Update,
	}, nil
}

func cloneAppWarnings(source map[string][]string) map[string][]string {
	cloned := make(map[string][]string, len(source))
	for slug, warnings := range source {
		cloned[slug] = append([]string(nil), warnings...)
	}
	return cloned
}

func (dashboard *handler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	if request.Method == http.MethodPost && strings.HasPrefix(request.URL.Path, "/_dropserve/api/apps/") && strings.HasSuffix(request.URL.Path, "/settings") {
		dashboard.serveAppSettings(response, request)
		return
	}
	if request.Method == http.MethodPost && request.URL.Path == "/_dropserve/api/open-folder" {
		dashboard.serveOpenFolder(response, request)
		return
	}
	if request.Method == http.MethodPost && request.URL.Path == "/_dropserve/api/rescan" {
		dashboard.serveRescan(response, request)
		return
	}
	if request.Method == http.MethodPost && strings.HasPrefix(request.URL.Path, "/_dropserve/api/apps/") && strings.HasSuffix(request.URL.Path, "/restart") {
		dashboard.serveAppRestart(response, request)
		return
	}
	if request.Method == http.MethodPost && strings.HasPrefix(request.URL.Path, "/_dropserve/api/addons/") {
		dashboard.serveAddonChange(response, request)
		return
	}
	if request.Method == http.MethodPost && strings.HasPrefix(request.URL.Path, "/_dropserve/api/sharing/funnel/") {
		dashboard.serveFunnelChange(response, request)
		return
	}
	if request.Method == http.MethodPost && request.URL.Path == "/_dropserve/api/sharing/tailscale" {
		dashboard.serveTailscaleServeChange(response, request)
		return
	}
	if request.Method == http.MethodPost && request.URL.Path == "/_dropserve/api/https" {
		dashboard.serveLocalHTTPSChange(response, request)
		return
	}
	if request.Method == http.MethodPost && request.URL.Path == "/_dropserve/api/trust" {
		dashboard.serveLocalTrustChange(response, request)
		return
	}
	if request.Method == http.MethodPost && request.URL.Path == "/_dropserve/api/network-change/dismiss" {
		dashboard.serveNetworkChangeDismiss(response, request)
		return
	}
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		response.Header().Set("Allow", "GET, HEAD")
		http.Error(response, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var content []byte
	switch request.URL.Path {
	case "/", "/_dropserve/":
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
	case "/_dropserve/help/dhcp-reservation":
		response.Header().Set("Content-Type", "text/html; charset=utf-8")
		response.Header().Set("Cache-Control", "public, max-age=3600")
		content = dashboard.dhcpHelp
	case "/_dropserve/api/apps":
		dashboard.serveJSON(response, request, dashboard.visibleApps(request.URL.Query().Get("include_hidden") == "1"))
		return
	case "/_dropserve/api/search":
		dashboard.serveJSON(response, request, indexer.Search(dashboard.visibleApps(false), request.URL.Query().Get("q")))
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
	case "/_dropserve/api/addons":
		if dashboard.addons == nil {
			dashboard.serveJSON(response, request, []AddonStatus{})
		} else {
			dashboard.serveJSON(response, request, dashboard.addons())
		}
		return
	case "/_dropserve/api/https/root.pem":
		dashboard.serveRootCertificate(response, request)
		return
	case "/_dropserve/healthz":
		dashboard.serveHealth(response, request)
		return
	default:
		if strings.HasPrefix(request.URL.Path, "/_dropserve/api/databases/") {
			dashboard.serveDatabase(response, request)
			return
		}
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

func (dashboard *handler) authorizeMutation(response http.ResponseWriter, request *http.Request) bool {
	if request.Header.Get("X-Dropserve-CSRF") != dashboard.csrfToken {
		http.Error(response, "Refresh the dashboard and try again.", http.StatusForbidden)
		return false
	}
	origin, err := url.Parse(request.Header.Get("Origin"))
	scheme := "http"
	if request.TLS != nil {
		scheme = "https"
	}
	if err != nil || origin.Scheme != scheme || !strings.EqualFold(origin.Host, request.Host) {
		http.Error(response, "This change must come from the open Dropserve dashboard or local CLI.", http.StatusForbidden)
		return false
	}
	return true
}

func (dashboard *handler) serveOpenFolder(response http.ResponseWriter, request *http.Request) {
	if !dashboard.authorizeMutation(response, request) {
		return
	}
	request.Body = http.MaxBytesReader(response, request.Body, 4<<10)
	var payload struct {
		Slug string `json:"slug"`
	}
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil && !errors.Is(err, io.EOF) {
		http.Error(response, "Choose an app folder to open.", http.StatusBadRequest)
		return
	}
	if payload.Slug != "" && (strings.Contains(payload.Slug, "/") || !dashboard.hasApp(payload.Slug)) {
		http.NotFound(response, request)
		return
	}
	if dashboard.openFolder == nil {
		http.Error(response, "Opening the Apps folder is not available.", http.StatusServiceUnavailable)
		return
	}
	if err := dashboard.openFolder(request.Context(), payload.Slug); err != nil {
		http.Error(response, "Dropserve could not open this folder: "+err.Error(), http.StatusInternalServerError)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (dashboard *handler) serveRescan(response http.ResponseWriter, request *http.Request) {
	if !dashboard.authorizeMutation(response, request) {
		return
	}
	if dashboard.rescan == nil {
		http.Error(response, "Rescan is not available.", http.StatusServiceUnavailable)
		return
	}
	if err := dashboard.rescan(); err != nil {
		http.Error(response, "Dropserve could not rescan your apps: "+err.Error(), http.StatusInternalServerError)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (dashboard *handler) serveAppSettings(response http.ResponseWriter, request *http.Request) {
	if !dashboard.authorizeMutation(response, request) {
		return
	}
	slug := strings.TrimSuffix(strings.TrimPrefix(request.URL.Path, "/_dropserve/api/apps/"), "/settings")
	if slug == "" || strings.Contains(slug, "/") || !dashboard.hasApp(slug) {
		http.NotFound(response, request)
		return
	}
	request.Body = http.MaxBytesReader(response, request.Body, 4<<10)
	var change AppSettingsChange
	if err := json.NewDecoder(request.Body).Decode(&change); err != nil || (change.Pinned == nil && change.Hidden == nil) {
		http.Error(response, "Choose whether this app is pinned or hidden.", http.StatusBadRequest)
		return
	}
	if dashboard.changeAppSettings == nil {
		http.Error(response, "App display settings are not available.", http.StatusServiceUnavailable)
		return
	}
	if err := dashboard.changeAppSettings(request.Context(), slug, change); err != nil {
		http.Error(response, "Dropserve could not save this app setting: "+err.Error(), http.StatusInternalServerError)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (dashboard *handler) serveAppRestart(response http.ResponseWriter, request *http.Request) {
	if !dashboard.authorizeMutation(response, request) {
		return
	}
	slug := strings.TrimSuffix(strings.TrimPrefix(request.URL.Path, "/_dropserve/api/apps/"), "/restart")
	if slug == "" || strings.Contains(slug, "/") || !dashboard.hasApp(slug) {
		http.NotFound(response, request)
		return
	}
	if dashboard.restartApp == nil {
		http.Error(response, "Restart is not available for this app.", http.StatusServiceUnavailable)
		return
	}
	if err := dashboard.restartApp(request.Context(), slug); err != nil {
		http.Error(response, "Dropserve could not restart this app: "+err.Error(), http.StatusBadGateway)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (dashboard *handler) visibleApps(includeHidden bool) []indexer.Entry {
	if includeHidden {
		return append([]indexer.Entry(nil), dashboard.apps...)
	}
	visible := make([]indexer.Entry, 0, len(dashboard.apps))
	for _, application := range dashboard.apps {
		if !application.Hidden {
			visible = append(visible, application)
		}
	}
	return visible
}

func (dashboard *handler) serveAddonChange(response http.ResponseWriter, request *http.Request) {
	if !dashboard.authorizeMutation(response, request) {
		return
	}
	name := strings.TrimPrefix(request.URL.Path, "/_dropserve/api/addons/")
	if name == "" || strings.Contains(name, "/") {
		http.NotFound(response, request)
		return
	}
	request.Body = http.MaxBytesReader(response, request.Body, 4<<10)
	var payload struct {
		Action string `json:"action"`
	}
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		http.Error(response, "Choose an add-on action.", http.StatusBadRequest)
		return
	}
	switch payload.Action {
	case "install", "remove", "start", "stop":
	default:
		http.Error(response, "Choose install, remove, start, or stop.", http.StatusBadRequest)
		return
	}
	if dashboard.changeAddon == nil {
		http.Error(response, "Add-on changes are not available.", http.StatusServiceUnavailable)
		return
	}
	if err := dashboard.changeAddon(request.Context(), name, payload.Action); err != nil {
		http.Error(response, "Dropserve could not change this add-on: "+err.Error(), http.StatusBadGateway)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (dashboard *handler) serveDatabase(response http.ResponseWriter, request *http.Request) {
	slug := strings.TrimPrefix(request.URL.Path, "/_dropserve/api/databases/")
	if slug == "" || strings.Contains(slug, "/") {
		http.NotFound(response, request)
		return
	}
	file := request.URL.Query().Get("file")
	allowed := false
	for _, entry := range dashboard.apps {
		if entry.Slug != slug {
			continue
		}
		for _, candidate := range entry.Databases {
			if candidate == file {
				allowed = true
				break
			}
		}
		break
	}
	if !allowed {
		http.NotFound(response, request)
		return
	}
	if dashboard.browseDatabase == nil {
		http.Error(response, "Database browsing is not available.", http.StatusServiceUnavailable)
		return
	}
	snapshot, err := dashboard.browseDatabase(request.Context(), slug, file)
	if err != nil {
		http.Error(response, "Dropserve could not read this database: "+err.Error(), http.StatusUnprocessableEntity)
		return
	}
	snapshot.Path = file
	dashboard.serveJSON(response, request, snapshot)
}

func (dashboard *handler) serveLocalHTTPSChange(response http.ResponseWriter, request *http.Request) {
	if !dashboard.authorizeMutation(response, request) {
		return
	}
	request.Body = http.MaxBytesReader(response, request.Body, 4<<10)
	var payload struct {
		Enabled *bool `json:"enabled"`
	}
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil || payload.Enabled == nil {
		http.Error(response, "Choose whether local HTTPS should be on or off.", http.StatusBadRequest)
		return
	}
	if dashboard.setLocalHTTPS == nil {
		http.Error(response, "Local HTTPS is not available.", http.StatusServiceUnavailable)
		return
	}
	if err := dashboard.setLocalHTTPS(request.Context(), *payload.Enabled); err != nil {
		http.Error(response, "Dropserve could not change local HTTPS: "+err.Error(), http.StatusBadGateway)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (dashboard *handler) serveLocalTrustChange(response http.ResponseWriter, request *http.Request) {
	if !dashboard.authorizeMutation(response, request) {
		return
	}
	request.Body = http.MaxBytesReader(response, request.Body, 4<<10)
	var payload struct {
		Installed *bool `json:"installed"`
	}
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil || payload.Installed == nil {
		http.Error(response, "Choose whether this computer should trust the local certificate.", http.StatusBadRequest)
		return
	}
	if dashboard.setLocalTrust == nil {
		http.Error(response, "Local certificate trust is not available.", http.StatusServiceUnavailable)
		return
	}
	if err := dashboard.setLocalTrust(*payload.Installed); err != nil {
		http.Error(response, "Dropserve could not change local certificate trust: "+err.Error(), http.StatusBadGateway)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (dashboard *handler) serveRootCertificate(response http.ResponseWriter, request *http.Request) {
	if dashboard.rootCertificate == nil {
		http.Error(response, "Enable local HTTPS before downloading its certificate.", http.StatusNotFound)
		return
	}
	content, err := dashboard.rootCertificate()
	if err != nil {
		http.Error(response, "Enable local HTTPS before downloading its certificate.", http.StatusNotFound)
		return
	}
	response.Header().Set("Content-Type", "application/x-pem-file")
	response.Header().Set("Content-Disposition", `attachment; filename="dropserve-local-ca.pem"`)
	response.Header().Set("X-Content-Type-Options", "nosniff")
	response.Header().Set("Content-Length", strconv.Itoa(len(content)))
	response.WriteHeader(http.StatusOK)
	if request.Method != http.MethodHead {
		_, _ = response.Write(content)
	}
}

func (dashboard *handler) serveNetworkChangeDismiss(response http.ResponseWriter, request *http.Request) {
	if !dashboard.authorizeMutation(response, request) {
		return
	}
	if dashboard.dismissNetworkChange == nil {
		http.Error(response, "Address-change notices are not available.", http.StatusServiceUnavailable)
		return
	}
	if err := dashboard.dismissNetworkChange(); err != nil {
		http.Error(response, "Dropserve could not dismiss this notice.", http.StatusInternalServerError)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (dashboard *handler) serveFunnelChange(response http.ResponseWriter, request *http.Request) {
	if !dashboard.authorizeMutation(response, request) {
		return
	}
	slug := strings.TrimPrefix(request.URL.Path, "/_dropserve/api/sharing/funnel/")
	if slug == "" || strings.Contains(slug, "/") || !dashboard.hasApp(slug) {
		http.NotFound(response, request)
		return
	}
	request.Body = http.MaxBytesReader(response, request.Body, 4<<10)
	var payload struct {
		Confirmation string `json:"confirmation"`
		Enabled      *bool  `json:"enabled"`
	}
	decoder := json.NewDecoder(request.Body)
	if err := decoder.Decode(&payload); err != nil && !errors.Is(err, io.EOF) {
		http.Error(response, "Type the app name shown in the confirmation box exactly.", http.StatusBadRequest)
		return
	}
	if dashboard.funnel == nil {
		http.Error(response, "Public sharing is not available. Check that Tailscale is running and Funnel is enabled for your tailnet.", http.StatusServiceUnavailable)
		return
	}
	if payload.Enabled != nil && !*payload.Enabled {
		if err := dashboard.funnel.Disable(request.Context(), slug); err != nil {
			http.Error(response, "Dropserve could not stop public sharing. Try again.", http.StatusBadGateway)
			return
		}
		response.WriteHeader(http.StatusNoContent)
		return
	}
	if err := dashboard.funnel.Enable(request.Context(), slug, payload.Confirmation); err != nil {
		if errors.Is(err, discovery.ErrFunnelConfirmation) {
			http.Error(response, err.Error(), http.StatusBadRequest)
			return
		}
		http.Error(response, "Dropserve could not start public sharing. Check Tailscale and try again.", http.StatusBadGateway)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (dashboard *handler) serveTailscaleServeChange(response http.ResponseWriter, request *http.Request) {
	if !dashboard.authorizeMutation(response, request) {
		return
	}
	request.Body = http.MaxBytesReader(response, request.Body, 4<<10)
	var payload struct {
		Enabled *bool `json:"enabled"`
	}
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil || payload.Enabled == nil {
		http.Error(response, "Choose whether tailnet HTTPS should be on or off.", http.StatusBadRequest)
		return
	}
	if dashboard.setTailscaleServe == nil {
		http.Error(response, "Tailnet HTTPS is not available.", http.StatusServiceUnavailable)
		return
	}
	if err := dashboard.setTailscaleServe(request.Context(), *payload.Enabled); err != nil {
		http.Error(response, "Dropserve could not change tailnet HTTPS: "+err.Error(), http.StatusBadGateway)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (dashboard *handler) hasApp(slug string) bool {
	for _, application := range dashboard.apps {
		if application.Slug == slug {
			return true
		}
	}
	return false
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
		HTTP  int `json:"http"`
		HTTPS int `json:"https,omitempty"`
	}
	type networkStatus struct {
		LANIP  string               `json:"lan_ip,omitempty"`
		Change *discovery.LANChange `json:"change,omitempty"`
	}
	type publicSharingStatus struct {
		Slug      string    `json:"slug"`
		URL       string    `json:"url,omitempty"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	type sharingStatus struct {
		TailscaleServeEnabled bool                  `json:"tailscale_serve_enabled"`
		Public                []publicSharingStatus `json:"public"`
	}
	network := networkStatus{}
	sharing := sharingStatus{Public: []publicSharingStatus{}}
	httpsStatus := LocalHTTPSStatus{}
	if dashboard.localHTTPSStatus != nil {
		httpsStatus = dashboard.localHTTPSStatus()
	}
	var discoverySnapshot discovery.Snapshot
	if dashboard.discovery != nil {
		discoverySnapshot = dashboard.discovery()
		if discoverySnapshot.LANIP.IsValid() {
			network.LANIP = discoverySnapshot.LANIP.String()
		}
		network.Change = discoverySnapshot.LANChange
		sharing.TailscaleServeEnabled = discoverySnapshot.Tailscale.ServeEnabled
	}
	warnings := append([]string{}, dashboard.warnings...)
	if dashboard.funnel != nil {
		active := dashboard.funnel.ActiveEntries()
		slugs := make([]string, 0, len(active))
		for slug := range active {
			slugs = append(slugs, slug)
		}
		sort.Strings(slugs)
		for _, slug := range slugs {
			publicURL := ""
			if discoverySnapshot.Tailscale.Host != "" {
				publicURL = tailscalePublicURL(discoverySnapshot.Tailscale.Host, slug)
			}
			sharing.Public = append(sharing.Public, publicSharingStatus{
				Slug:      slug,
				URL:       publicURL,
				ExpiresAt: active[slug].ExpiresAt.UTC(),
			})
			warnings = append(warnings, fmt.Sprintf(
				"public_sharing_active: %s is reachable from the public internet until %s.",
				slug,
				active[slug].ExpiresAt.UTC().Format(time.RFC3339),
			))
		}
	}
	payload := struct {
		Version       string           `json:"version"`
		Commit        string           `json:"commit"`
		UptimeSeconds int64            `json:"uptime_seconds"`
		Ports         ports            `json:"ports"`
		Network       networkStatus    `json:"network"`
		Sharing       sharingStatus    `json:"sharing"`
		HTTPS         LocalHTTPSStatus `json:"https"`
		Warnings      []string         `json:"warnings"`
		CSRFToken     string           `json:"csrf_token"`
		Update        UpdateNotice     `json:"update"`
	}{
		Version:       version.Version,
		Commit:        version.Commit,
		UptimeSeconds: int64(time.Since(dashboard.started).Seconds()),
		Ports:         ports{HTTP: requestHTTPPort(request), HTTPS: httpsStatus.Port},
		Network:       network,
		Sharing:       sharing,
		HTTPS:         httpsStatus,
		Warnings:      warnings,
		CSRFToken:     dashboard.csrfToken,
	}
	if dashboard.update != nil {
		payload.Update = dashboard.update()
	}
	dashboard.serveJSON(response, request, payload)
}

func tailscalePublicURL(host, slug string) string {
	authority := host
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		authority = "[" + host + "]"
	}
	return (&url.URL{Scheme: "https", Host: authority, Path: "/" + slug + "/"}).String()
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
			warnings := append([]string{}, dashboard.appWarnings[slug]...)
			payload := struct {
				indexer.Entry
				Warnings []string `json:"warnings"`
			}{Entry: entry, Warnings: warnings}
			dashboard.serveJSON(response, request, payload)
			return
		}
	}
	http.NotFound(response, request)
}

type advertisedURL struct {
	Kind    string `json:"kind"`
	URL     string `json:"url,omitempty"`
	Message string `json:"message,omitempty"`
}

func (dashboard *handler) serveAdvertisedURLs(response http.ResponseWriter, request *http.Request) {
	scheme := "http"
	if request.TLS != nil {
		scheme = "https"
	}
	if dashboard.discovery != nil {
		snapshot := dashboard.discovery()
		endpoints := snapshot.Endpoints(scheme, requestHTTPPort(request))
		advertised := make([]advertisedURL, 0, len(endpoints)+2)
		for _, endpoint := range endpoints {
			advertised = append(advertised, advertisedURL{Kind: endpoint.Kind, URL: endpoint.URL, Message: endpoint.Message})
		}
		if dashboard.localHTTPSStatus != nil {
			status := dashboard.localHTTPSStatus()
			if status.Enabled && status.Port > 0 && (scheme != "https" || requestHTTPPort(request) != status.Port) {
				for _, endpoint := range snapshot.Endpoints("https", status.Port) {
					if endpoint.Kind == "tailscale" {
						continue
					}
					advertised = append(advertised, advertisedURL{
						Kind:    "https-" + endpoint.Kind,
						URL:     endpoint.URL,
						Message: endpoint.Message,
					})
				}
			}
		}
		dashboard.serveJSON(response, request, advertised)
		return
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
