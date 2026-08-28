// Command accessibility serves a deterministic dashboard fixture for browser checks.
package main

import (
	"errors"
	"log"
	"net/http"
	"path/filepath"
	"time"

	"github.com/tanzir71/dropserve/internal/scanner"
	dropserver "github.com/tanzir71/dropserve/internal/server"
)

func main() {
	appsRoot, err := filepath.Abs("fixtures")
	if err != nil {
		log.Fatalf("resolve accessibility fixtures: %v", err)
	}
	dashboard, err := dropserver.New(scanner.Options{Roots: []string{appsRoot}})
	if err != nil {
		log.Fatalf("create accessibility server: %v", err)
	}
	defer func() {
		_ = dashboard.Close()
	}()

	httpServer := &http.Server{
		Addr:              "127.0.0.1:17431",
		Handler:           dashboard.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	log.Printf("accessibility fixture listening at http://%s", httpServer.Addr)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("serve accessibility fixture: %v", err)
	}
}
