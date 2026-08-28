package access

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestPINGateExemptsLoopbackAndHealthAndIssuesSignedSession(t *testing.T) {
	called := 0
	next := http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		called++
		response.WriteHeader(http.StatusNoContent)
	})
	digest := sha256.Sum256([]byte("123456"))
	gate, err := New(next, true, hex.EncodeToString(digest[:]))
	if err != nil {
		t.Fatalf("create gate: %v", err)
	}
	now := time.Date(2026, time.August, 28, 12, 0, 0, 0, time.UTC)
	gate.now = func() time.Time { return now }

	loopback := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "http://dropserve.test/notes/", nil)
	loopback.RemoteAddr = "127.0.0.1:5000"
	loopbackResponse := httptest.NewRecorder()
	gate.ServeHTTP(loopbackResponse, loopback)
	if loopbackResponse.Code != http.StatusNoContent || called != 1 {
		t.Fatalf("loopback response=%d called=%d", loopbackResponse.Code, called)
	}

	health := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "http://dropserve.test/_dropserve/healthz", nil)
	health.RemoteAddr = "192.168.1.20:5000"
	healthResponse := httptest.NewRecorder()
	gate.ServeHTTP(healthResponse, health)
	if healthResponse.Code != http.StatusNoContent || called != 2 {
		t.Fatalf("health response=%d called=%d", healthResponse.Code, called)
	}

	blocked := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "http://dropserve.test/notes/", nil)
	blocked.RemoteAddr = "192.168.1.20:5000"
	blockedResponse := httptest.NewRecorder()
	gate.ServeHTTP(blockedResponse, blocked)
	if blockedResponse.Code != http.StatusUnauthorized || !strings.Contains(blockedResponse.Body.String(), "six-digit PIN") || called != 2 {
		t.Fatalf("blocked response=%d called=%d body=%q", blockedResponse.Code, called, blockedResponse.Body.String())
	}

	form := url.Values{"pin": {"123456"}, "return_to": {"/notes/"}}
	login := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "http://dropserve.test/_dropserve/login", strings.NewReader(form.Encode()))
	login.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	login.RemoteAddr = "192.168.1.20:5000"
	loginResponse := httptest.NewRecorder()
	gate.ServeHTTP(loginResponse, login)
	if loginResponse.Code != http.StatusSeeOther || loginResponse.Header().Get("Location") != "/notes/" {
		t.Fatalf("login response=%d location=%q body=%q", loginResponse.Code, loginResponse.Header().Get("Location"), loginResponse.Body.String())
	}
	result := loginResponse.Result()
	if len(result.Cookies()) != 1 {
		t.Fatalf("login cookies = %#v", result.Cookies())
	}

	authorized := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "http://dropserve.test/notes/", nil)
	authorized.RemoteAddr = "192.168.1.20:5000"
	authorized.AddCookie(result.Cookies()[0])
	authorizedResponse := httptest.NewRecorder()
	gate.ServeHTTP(authorizedResponse, authorized)
	if authorizedResponse.Code != http.StatusNoContent || called != 3 {
		t.Fatalf("authorized response=%d called=%d", authorizedResponse.Code, called)
	}

	now = now.Add(sessionLifetime + time.Second)
	expiredResponse := httptest.NewRecorder()
	gate.ServeHTTP(expiredResponse, authorized)
	if expiredResponse.Code != http.StatusUnauthorized || called != 3 {
		t.Fatalf("expired response=%d called=%d", expiredResponse.Code, called)
	}
}

func TestPINGateRejectsWrongPINAndUnsafeReturnURL(t *testing.T) {
	digest := sha256.Sum256([]byte("123456"))
	gate, err := New(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), true, hex.EncodeToString(digest[:]))
	if err != nil {
		t.Fatal(err)
	}
	form := url.Values{"pin": {"000000"}, "return_to": {"//attacker.example/"}}
	request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "http://dropserve.test/_dropserve/login", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.RemoteAddr = "192.168.1.20:5000"
	response := httptest.NewRecorder()
	gate.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || !strings.Contains(response.Body.String(), `value="/"`) || len(response.Result().Cookies()) != 0 {
		t.Fatalf("wrong PIN response=%d cookies=%v body=%q", response.Code, response.Result().Cookies(), response.Body.String())
	}
}
