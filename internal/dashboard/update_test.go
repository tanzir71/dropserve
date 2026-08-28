package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStatusSurfacesOnlyAnUpdateNotificationLink(t *testing.T) {
	handler, err := NewWithOptions(nil, Options{Update: func() UpdateNotice {
		return UpdateNotice{Available: true, Version: "1.4.0", URL: "https://github.com/tanzir71/dropserve/releases/tag/v1.4.0"}
	}})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "http://dropserve.test/_dropserve/api/status", nil)
	handler.ServeHTTP(response, request)
	var status struct {
		Update UpdateNotice `json:"update"`
	}
	if err := json.NewDecoder(response.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	if !status.Update.Available || status.Update.Version != "1.4.0" || status.Update.URL != "https://github.com/tanzir71/dropserve/releases/tag/v1.4.0" {
		t.Fatalf("dashboard update = %#v", status.Update)
	}
}
