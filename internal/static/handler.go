// Package static serves files from a discovered static app without modifying it.
package static

import (
	"net/http"

	"github.com/tanzir71/dropserve/internal/app"
)

// New returns a handler for a static app.
func New(application app.App) http.Handler {
	if application.LooseFile {
		return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			http.ServeFile(response, request, application.Path)
		})
	}
	return http.FileServer(http.Dir(application.Path))
}
