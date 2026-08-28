// Package access implements Dropserve's optional non-loopback PIN gate.
package access

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

const sessionCookie = "dropserve_session"

const sessionLifetime = 30 * 24 * time.Hour

type settings struct {
	enabled bool
	pinHash [sha256.Size]byte
	key     [sha256.Size]byte
}

// Gate protects all non-loopback routes except the health endpoint.
type Gate struct {
	next http.Handler
	now  func() time.Time
	live atomic.Pointer[settings]
}

// New creates a gate and validates the configured PIN digest.
func New(next http.Handler, enabled bool, pinHash string) (*Gate, error) {
	gate := &Gate{next: next, now: time.Now}
	if err := gate.Update(enabled, pinHash); err != nil {
		return nil, err
	}
	return gate, nil
}

// Update atomically applies a hot-reloaded PIN policy. Existing sessions are
// invalidated when the digest changes and remain valid across process restarts.
func (gate *Gate) Update(enabled bool, pinHash string) error {
	current := &settings{enabled: enabled}
	if enabled {
		decoded, err := hex.DecodeString(strings.TrimSpace(pinHash))
		if err != nil || len(decoded) != sha256.Size {
			return fmt.Errorf("PIN hash must be a 64-character SHA-256 hex digest")
		}
		copy(current.pinHash[:], decoded)
		current.key = sha256.Sum256(append([]byte("dropserve-session-v1:"), decoded...))
	}
	gate.live.Store(current)
	return nil
}

// ServeHTTP enforces the live policy.
func (gate *Gate) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	current := gate.live.Load()
	if current == nil || !current.enabled || request.URL.Path == "/_dropserve/healthz" || request.URL.Path == "/_dropserve/login" || isLoopback(request.RemoteAddr) {
		if current != nil && current.enabled && request.URL.Path == "/_dropserve/login" && !isLoopback(request.RemoteAddr) {
			gate.serveLogin(response, request, current)
			return
		}
		gate.next.ServeHTTP(response, request)
		return
	}
	if gate.hasValidSession(request, current) {
		gate.next.ServeHTTP(response, request)
		return
	}
	gate.writeLogin(response, request, request.URL.RequestURI(), "", http.StatusUnauthorized)
}

func (gate *Gate) serveLogin(response http.ResponseWriter, request *http.Request, current *settings) {
	if request.Method == http.MethodGet || request.Method == http.MethodHead {
		gate.writeLogin(response, request, safeReturnTo(request.URL.Query().Get("return_to")), "", http.StatusOK)
		return
	}
	if request.Method != http.MethodPost {
		response.Header().Set("Allow", "GET, HEAD, POST")
		http.Error(response, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	request.Body = http.MaxBytesReader(response, request.Body, 4<<10)
	if err := request.ParseForm(); err != nil {
		gate.writeLogin(response, request, "/", "Enter the six-digit PIN.", http.StatusBadRequest)
		return
	}
	pin := request.PostForm.Get("pin")
	digest := sha256.Sum256([]byte(pin))
	if !sixDigits(pin) || subtle.ConstantTimeCompare(digest[:], current.pinHash[:]) != 1 {
		gate.writeLogin(response, request, safeReturnTo(request.PostForm.Get("return_to")), "That PIN did not match.", http.StatusUnauthorized)
		return
	}
	expires := gate.now().Add(sessionLifetime).UTC()
	payload := strconv.FormatInt(expires.Unix(), 10)
	signature := sign(current.key[:], payload)
	// #nosec G124 -- HttpOnly and SameSite are fixed; Secure follows whether the local listener is HTTPS so PIN lock also works on the documented LAN HTTP default.
	http.SetCookie(response, &http.Cookie{
		Name:     sessionCookie,
		Value:    payload + "." + signature,
		Path:     "/",
		Expires:  expires,
		MaxAge:   int(sessionLifetime.Seconds()),
		HttpOnly: true,
		Secure:   request.TLS != nil,
		SameSite: http.SameSiteStrictMode,
	})
	// #nosec G710 -- safeReturnTo rejects absolute, authority-bearing, and protocol-relative values.
	http.Redirect(response, request, safeReturnTo(request.PostForm.Get("return_to")), http.StatusSeeOther)
}

func (gate *Gate) hasValidSession(request *http.Request, current *settings) bool {
	cookie, err := request.Cookie(sessionCookie)
	if err != nil {
		return false
	}
	payload, signature, found := strings.Cut(cookie.Value, ".")
	if !found || subtle.ConstantTimeCompare([]byte(signature), []byte(sign(current.key[:], payload))) != 1 {
		return false
	}
	expires, err := strconv.ParseInt(payload, 10, 64)
	return err == nil && gate.now().Before(time.Unix(expires, 0))
}

func sign(key []byte, payload string) string {
	mac := hmac.New(sha256.New, key)
	_, _ = io.WriteString(mac, payload)
	return hex.EncodeToString(mac.Sum(nil))
}

func isLoopback(remoteAddress string) bool {
	host, _, err := net.SplitHostPort(remoteAddress)
	if err != nil {
		host = strings.Trim(remoteAddress, "[]")
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}

func sixDigits(value string) bool {
	if len(value) != 6 {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func safeReturnTo(value string) string {
	if value == "" {
		return "/"
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || !strings.HasPrefix(parsed.Path, "/") || strings.HasPrefix(value, "//") {
		return "/"
	}
	return value
}

func (gate *Gate) writeLogin(response http.ResponseWriter, request *http.Request, returnTo, message string, status int) {
	returnTo = safeReturnTo(returnTo)
	messageHTML := ""
	if message != "" {
		messageHTML = `<p role="alert" class="error">` + html.EscapeString(message) + `</p>`
	}
	content := `<!doctype html><html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>Unlock Dropserve</title><style>html{color-scheme:light dark;font-family:system-ui,sans-serif}body{min-height:100vh;margin:0;display:grid;place-items:center;background:#f3f6f4;color:#18201e}main{width:min(360px,calc(100% - 32px));padding:28px;border:1px solid #dce3df;border-radius:20px;background:white;box-shadow:0 18px 50px #16302718}h1{margin-top:0}p{color:#66716d;line-height:1.5}.error{color:#9b2c2c}label{display:block;font-weight:700}input{box-sizing:border-box;width:100%;margin:8px 0 18px;padding:12px;border:1px solid #9baba4;border-radius:10px;font:22px ui-monospace,monospace;letter-spacing:.3em}button{width:100%;padding:11px;border:0;border-radius:999px;color:white;background:#156b50;font-weight:750}@media(prefers-color-scheme:dark){body{background:#111815;color:#edf3f0}main{border-color:#33413c;background:#1b2622}p{color:#aab7b1}}</style><main><h1>Unlock Dropserve</h1><p>This computer asks for its six-digit PIN before sharing apps over the network.</p>` + messageHTML + `<form method="post" action="/_dropserve/login"><input type="hidden" name="return_to" value="` + html.EscapeString(returnTo) + `"><label for="pin">PIN</label><input id="pin" name="pin" type="password" inputmode="numeric" pattern="[0-9]{6}" maxlength="6" autocomplete="one-time-code" autofocus required><button type="submit">Unlock for 30 days</button></form></main></html>`
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
	response.Header().Set("Content-Length", strconv.Itoa(len(content)))
	response.WriteHeader(status)
	if request.Method != http.MethodHead {
		_, _ = io.WriteString(response, content)
	}
}
