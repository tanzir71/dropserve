package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tanzir71/dropserve/internal/indexer"
)

func TestFolderRestartAndRescanActionsUseExplicitMutationBoundary(t *testing.T) {
	var opened string
	var restarted string
	rescans := 0
	httpHandler, err := NewWithOptions([]indexer.Entry{{Slug: "field-notes", Name: "Field Notes"}}, Options{
		OpenFolder: func(_ context.Context, slug string) error {
			opened = slug
			return nil
		},
		RestartApp: func(_ context.Context, slug string) error {
			restarted = slug
			return nil
		},
		Rescan: func() error {
			rescans++
			return nil
		},
	})
	if err != nil {
		t.Fatalf("create dashboard: %v", err)
	}
	dashboard := httpHandler.(*handler)
	for _, action := range []struct {
		path string
		body string
	}{
		{path: "/_dropserve/api/open-folder", body: `{"slug":"field-notes"}`},
		{path: "/_dropserve/api/apps/field-notes/restart", body: `{}`},
		{path: "/_dropserve/api/rescan", body: `{}`},
	} {
		request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "http://dropserve.test"+action.path, strings.NewReader(action.body))
		authorizeTestMutation(request, dashboard.csrfToken)
		response := httptest.NewRecorder()
		dashboard.ServeHTTP(response, request)
		if response.Code != http.StatusNoContent {
			t.Fatalf("POST %s returned %d: %s", action.path, response.Code, response.Body.String())
		}
	}
	if opened != "field-notes" || restarted != "field-notes" || rescans != 1 {
		t.Fatalf("opened=%q restarted=%q rescans=%d", opened, restarted, rescans)
	}
}

func TestAppSettingsAndHiddenAppFiltering(t *testing.T) {
	hidden := true
	visibleEntry := indexer.Entry{Slug: "field-notes", Name: "Field Notes"}
	hiddenEntry := indexer.Entry{Slug: "secret-notes", Name: "Secret Notes", Hidden: true}
	var changedSlug string
	var changed AppSettingsChange
	httpHandler, err := NewWithOptions([]indexer.Entry{visibleEntry, hiddenEntry}, Options{
		ChangeAppSettings: func(_ context.Context, slug string, change AppSettingsChange) error {
			changedSlug = slug
			changed = change
			return nil
		},
	})
	if err != nil {
		t.Fatalf("create dashboard: %v", err)
	}
	dashboard := httpHandler.(*handler)

	request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "http://dropserve.test/_dropserve/api/apps/field-notes/settings", strings.NewReader(`{"hidden":true}`))
	authorizeTestMutation(request, dashboard.csrfToken)
	response := httptest.NewRecorder()
	dashboard.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("settings response = %d: %s", response.Code, response.Body.String())
	}
	if changedSlug != "field-notes" || changed.Hidden == nil || *changed.Hidden != hidden || changed.Pinned != nil {
		t.Fatalf("settings callback slug=%q change=%+v", changedSlug, changed)
	}

	for _, test := range []struct {
		path string
		want []string
	}{
		{path: "/_dropserve/api/apps", want: []string{"field-notes"}},
		{path: "/_dropserve/api/apps?include_hidden=1", want: []string{"field-notes", "secret-notes"}},
		{path: "/_dropserve/api/search?q=secret", want: []string{}},
	} {
		request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "http://dropserve.test"+test.path, nil)
		response := httptest.NewRecorder()
		dashboard.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s returned %d: %s", test.path, response.Code, response.Body.String())
		}
		var entries []indexer.Entry
		if err := json.Unmarshal(response.Body.Bytes(), &entries); err != nil {
			t.Fatalf("decode GET %s: %v", test.path, err)
		}
		got := make([]string, len(entries))
		for index, entry := range entries {
			got[index] = entry.Slug
		}
		if strings.Join(got, ",") != strings.Join(test.want, ",") {
			t.Fatalf("GET %s slugs=%v, want %v", test.path, got, test.want)
		}
	}
}

func TestAppDetailIncludesItsManifestWarnings(t *testing.T) {
	httpHandler, err := NewWithOptions([]indexer.Entry{{Slug: "notes", Name: "Notes"}}, Options{
		AppWarnings: map[string][]string{"notes": {`dropserve.json ignores unknown key "future"`}},
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "http://dropserve.test/_dropserve/api/apps/notes", nil)
	response := httptest.NewRecorder()
	httpHandler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `unknown key \"future\"`) {
		t.Fatalf("app detail = %d %s", response.Code, response.Body.String())
	}
}
