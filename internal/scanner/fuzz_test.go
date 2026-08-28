package scanner_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/tanzir71/dropserve/internal/scanner"
)

func FuzzSlugSanitiser(f *testing.F) {
	for _, seed := range []string{
		"My Notes",
		"Ünïcødé Tool",
		"..evil",
		"_scratch",
		"a---b___c",
		"space\tand\nlines",
		"api.html",
		"\x00/\\:%2e%2e",
	} {
		f.Add(seed)
	}
	valid := regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	f.Fuzz(func(t *testing.T, name string) {
		slug := scanner.Slug(name)
		if slug != scanner.Slug(name) {
			t.Fatalf("Slug(%q) is not deterministic", name)
		}
		if strings.HasPrefix(name, "..") && slug != "" {
			t.Fatalf("Slug(%q) = %q, want rejection for unsafe name", name, slug)
		}
		if slug != "" && !valid.MatchString(slug) {
			t.Fatalf("Slug(%q) = %q, want one lowercase ASCII URL segment", name, slug)
		}
		if slug != "" && scanner.Slug(slug) != slug {
			t.Fatalf("Slug(%q) = %q, which is not idempotent", name, slug)
		}
	})
}
