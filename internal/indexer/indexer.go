// Package indexer builds the read-only dashboard view of discovered apps.
package indexer

import (
	"strings"

	"github.com/tanzir71/dropserve/internal/app"
)

// URLs contains verified ways to reach an app.
type URLs struct {
	Path string `json:"path"`
}

// Entry is the stable public dashboard representation of one app.
type Entry struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Type        string `json:"type"`
	Status      string `json:"status"`
	URLs        URLs   `json:"urls"`
	Icon        string `json:"icon,omitempty"`
	Size        int64  `json:"size"`
	MTime       int64  `json:"mtime"`
}

// Build returns an immutable dashboard snapshot in scanner order.
func Build(applications []app.App) []Entry {
	entries := make([]Entry, 0, len(applications))
	for _, application := range applications {
		entries = append(entries, Entry{
			Slug:   application.Slug,
			Name:   application.Name,
			Type:   string(application.Kind),
			Status: "ready",
			URLs:   URLs{Path: "/" + strings.Trim(application.Slug, "/") + "/"},
		})
	}
	return entries
}
