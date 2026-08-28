package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestLandingPageHasNoExternalResourceReferences(t *testing.T) {
	t.Parallel()
	content, err := os.ReadFile("../docs/index.html") // #nosec G304 -- fixed checked-in landing page.
	if err != nil {
		t.Fatalf("read landing page: %v", err)
	}
	html := string(content)
	externalSource := regexp.MustCompile(`(?is)<(?:script|img|source|video|audio|iframe)\b[^>]*\bsrc\s*=\s*["']https?://`)
	linkTag := regexp.MustCompile(`(?is)<link\b[^>]*>`)
	externalHref := regexp.MustCompile(`(?is)\bhref\s*=\s*["']https?://`)
	if match := externalSource.FindString(html); match != "" {
		t.Fatalf("landing page loads an external source: %s", match)
	}
	for _, tag := range linkTag.FindAllString(html, -1) {
		if strings.Contains(strings.ToLower(tag), "stylesheet") && externalHref.MatchString(tag) {
			t.Fatalf("landing page loads an external stylesheet: %s", tag)
		}
	}
	if strings.Contains(strings.ToLower(html), "url(") {
		t.Fatal("landing page CSS contains url(); all visuals must be embedded markup")
	}
}
