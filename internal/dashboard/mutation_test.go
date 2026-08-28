package dashboard

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func authorizeTestMutation(request *http.Request, token string) {
	request.Header.Set("Origin", "http://"+request.Host)
	request.Header.Set("X-Dropserve-CSRF", token)
}

func TestMutationsRequireSameOriginInAdditionToCSRF(t *testing.T) {
	httpHandler, err := NewWithOptions(nil, Options{})
	if err != nil {
		t.Fatalf("create dashboard: %v", err)
	}
	dashboard := httpHandler.(*handler)
	request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "http://dropserve.test/_dropserve/api/network-change/dismiss", nil)
	request.Header.Set("Origin", "http://attacker.example")
	request.Header.Set("X-Dropserve-CSRF", dashboard.csrfToken)
	response := httptest.NewRecorder()
	dashboard.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("cross-origin mutation returned %d, want 403", response.Code)
	}
}
