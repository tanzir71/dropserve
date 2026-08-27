package scanner

import (
	"path/filepath"
	"strings"

	"github.com/tanzir71/dropserve/internal/app"
)

// Rename records one discovered app whose path casing or name changed.
type Rename struct {
	Before app.App
	After  app.App
}

// Changes is the stable difference between two scan snapshots.
type Changes struct {
	Added   []app.App
	Removed []app.App
	Renamed []Rename
}

// Compare reports changes between scan snapshots. Case-insensitive roots use a
// folded clean path as identity, so a case-only rename remains one app.
func Compare(previous, current Result, caseInsensitive bool) Changes {
	currentByIdentity := make(map[string]app.App, len(current.Apps))
	for _, application := range current.Apps {
		currentByIdentity[pathIdentity(application.Path, caseInsensitive)] = application
	}

	var changes Changes
	matched := make(map[string]struct{}, len(previous.Apps))
	for _, before := range previous.Apps {
		identity := pathIdentity(before.Path, caseInsensitive)
		after, found := currentByIdentity[identity]
		if !found {
			changes.Removed = append(changes.Removed, before)
			continue
		}
		matched[identity] = struct{}{}
		if before.Path != after.Path || before.Name != after.Name || before.Slug != after.Slug {
			changes.Renamed = append(changes.Renamed, Rename{Before: before, After: after})
		}
	}
	for _, application := range current.Apps {
		identity := pathIdentity(application.Path, caseInsensitive)
		if _, found := matched[identity]; !found {
			changes.Added = append(changes.Added, application)
		}
	}
	return changes
}

func pathIdentity(path string, caseInsensitive bool) string {
	identity := filepath.Clean(path)
	if caseInsensitive {
		identity = strings.ToLower(identity)
	}
	return identity
}
