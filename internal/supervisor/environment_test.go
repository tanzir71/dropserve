package supervisor

import (
	"strings"
	"testing"

	"github.com/tanzir71/dropserve/internal/app"
)

func TestCommandEnvironmentIncludesFrameworkBasePathConventions(t *testing.T) {
	t.Setenv("PUBLIC_URL", "/application-owned/")

	environment := environmentMap(commandEnvironment(app.App{Slug: "field-notes"}, 7451))
	want := map[string]string{
		"DROPSERVE_BASE_PATH":   "/field-notes/",
		"DROPSERVE_BASE_URL":    "http://127.0.0.1/field-notes/",
		"BASE_PATH":             "/field-notes/",
		"PUBLIC_URL":            "/application-owned/",
		"VITE_BASE":             "/field-notes/",
		"NEXT_PUBLIC_BASE_PATH": "/field-notes/",
	}
	for name, value := range want {
		if environment[name] != value {
			t.Errorf("%s = %q, want %q", name, environment[name], value)
		}
	}
}

func environmentMap(environment []string) map[string]string {
	values := make(map[string]string, len(environment))
	for _, item := range environment {
		name, value, found := strings.Cut(item, "=")
		if found {
			values[strings.ToUpper(name)] = value
		}
	}
	return values
}
