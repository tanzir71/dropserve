package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAddonActionsRequireCSRFAndRemainExplicit(t *testing.T) {
	var actions []string
	handler, err := NewWithOptions(nil, Options{
		Addons: func() []AddonStatus {
			return []AddonStatus{{Name: "php", Title: "PHP", Version: "8.3.33", Available: true}}
		},
		ChangeAddon: func(_ context.Context, name, action string) error {
			actions = append(actions, name+":"+action)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("create add-ons dashboard: %v", err)
	}
	statusRequest := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "http://dropserve.test/_dropserve/api/status", nil)
	statusResponse := httptest.NewRecorder()
	handler.ServeHTTP(statusResponse, statusRequest)
	var status struct {
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.NewDecoder(statusResponse.Body).Decode(&status); err != nil || status.CSRFToken == "" {
		t.Fatalf("read dashboard CSRF token: status=%#v err=%v", status, err)
	}

	body := []byte(`{"action":"install"}`)
	unauthenticated := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "http://dropserve.test/_dropserve/api/addons/php", bytes.NewReader(body))
	unauthenticatedResponse := httptest.NewRecorder()
	handler.ServeHTTP(unauthenticatedResponse, unauthenticated)
	if unauthenticatedResponse.Code != http.StatusForbidden || len(actions) != 0 {
		t.Fatalf("unauthenticated add-on action = %d actions=%v", unauthenticatedResponse.Code, actions)
	}

	request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "http://dropserve.test/_dropserve/api/addons/php", bytes.NewReader(body))
	authorizeTestMutation(request, status.CSRFToken)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || len(actions) != 1 || actions[0] != "php:install" {
		t.Fatalf("explicit add-on action = %d actions=%v", response.Code, actions)
	}
}
