package supervisor

import (
	"context"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var rootAbsoluteReference = regexp.MustCompile(
	`(?i)(?:src|href)\s*=\s*["']/[^/]` +
		`|url\(\s*["']?/[^/]` +
		`|fetch\(\s*["']/[^/]`,
)

func probeRootAbsoluteReferences(target string) bool {
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, target, nil)
	if err != nil {
		return false
	}
	client := http.Client{Timeout: 2 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return false
	}
	defer func() {
		_ = response.Body.Close()
	}()
	if !strings.HasPrefix(strings.ToLower(response.Header.Get("Content-Type")), "text/html") {
		return false
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maximumHTMLRewriteBytes+1))
	if err != nil || len(body) > maximumHTMLRewriteBytes {
		return false
	}
	return rootAbsoluteReference.Match(body)
}
