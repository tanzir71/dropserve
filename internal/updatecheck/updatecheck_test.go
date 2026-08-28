package updatecheck

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type recordingClient struct {
	requests []*http.Request
	body     string
}

func (client *recordingClient) Do(request *http.Request) (*http.Response, error) {
	client.requests = append(client.requests, request)
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(client.body)),
	}, nil
}

func TestCheckUsesOnlyLatestReleaseAPIAndReturnsLink(t *testing.T) {
	client := &recordingClient{body: `{
  "tag_name":"v1.4.0",
  "html_url":"https://github.com/tanzir71/dropserve/releases/tag/v1.4.0",
  "assets":[{"browser_download_url":"https://objects.example/dropserve.exe"}]
}`}
	notice, err := Check(context.Background(), Options{Client: client, CurrentVersion: "1.3.9"})
	if err != nil {
		t.Fatalf("check latest release: %v", err)
	}
	if !notice.Available || notice.Version != "1.4.0" || notice.URL != "https://github.com/tanzir71/dropserve/releases/tag/v1.4.0" {
		t.Fatalf("update notice = %#v", notice)
	}
	if len(client.requests) != 1 {
		t.Fatalf("request count = %d, want exactly one API request and no asset download", len(client.requests))
	}
	request := client.requests[0]
	if request.Method != http.MethodGet || request.URL.String() != LatestReleaseAPI {
		t.Fatalf("update request = %s %s", request.Method, request.URL)
	}
	if request.Header.Get("Accept") != "application/vnd.github+json" || request.Header.Get("User-Agent") == "" {
		t.Fatalf("update request headers = %#v", request.Header)
	}
}

func TestCheckUsesSemanticVersionOrdering(t *testing.T) {
	tests := []struct {
		name      string
		current   string
		latest    string
		available bool
	}{
		{"patch available", "1.2.3", "v1.2.4", true},
		{"numeric ordering", "1.9.9", "v1.10.0", true},
		{"same release", "v2.0.0", "v2.0.0", false},
		{"older release", "2.1.0", "v2.0.9", false},
		{"release after prerelease", "2.0.0-rc.1", "v2.0.0", true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := &recordingClient{body: `{"tag_name":"` + test.latest + `","html_url":"https://github.com/tanzir71/dropserve/releases/tag/` + test.latest + `"}`}
			notice, err := Check(context.Background(), Options{Client: client, CurrentVersion: test.current})
			if err != nil {
				t.Fatalf("check versions: %v", err)
			}
			if notice.Available != test.available {
				t.Fatalf("available = %v for current %s latest %s, want %v", notice.Available, test.current, test.latest, test.available)
			}
		})
	}
}

func TestCheckRejectsNonReleaseLinks(t *testing.T) {
	for _, releaseURL := range []string{
		"http://github.com/tanzir71/dropserve/releases/tag/v1.4.0",
		"https://example.com/tanzir71/dropserve/releases/tag/v1.4.0",
		"https://github.com/tanzir71/dropserve/releases/download/v1.4.0/dropserve.exe",
		"https://github.com/tanzir71/dropserve/releases/tag/v1.4.0/asset",
	} {
		client := &recordingClient{body: `{"tag_name":"v1.4.0","html_url":"` + releaseURL + `"}`}
		if _, err := Check(context.Background(), Options{Client: client, CurrentVersion: "1.3.9"}); err == nil {
			t.Fatalf("accepted non-release link %q", releaseURL)
		}
	}
}
