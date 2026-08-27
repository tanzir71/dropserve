// Package scanner discovers apps from configured roots without modifying them.
package scanner

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/tanzir71/dropserve/internal/app"
)

var reservedSlugs = map[string]struct{}{
	"_dropserve":  {},
	"api":         {},
	"health":      {},
	".well-known": {},
}

var ignoredNames = map[string]struct{}{
	"node_modules": {},
	"venv":         {},
	".venv":        {},
	"__pycache__":  {},
	"vendor":       {},
	".git":         {},
	"dist":         {},
}

// Options controls one scan.
type Options struct {
	Roots []string
}

// Warning describes an app that could not be mounted exactly as discovered.
type Warning struct {
	Code    string
	Path    string
	Message string
}

// Result is an ordered, immutable snapshot of discovered apps and warnings.
type Result struct {
	Apps     []app.App
	Warnings []Warning
}

// Scan reads each root in order and discovers immediate child directories and
// loose HTML files. It never writes below a root.
func Scan(options Options) (Result, error) {
	var result Result
	slugUses := make(map[string]int)

	for _, configuredRoot := range options.Roots {
		root, err := filepath.Abs(configuredRoot)
		if err != nil {
			return Result{}, fmt.Errorf("resolve Apps root %q: %w", configuredRoot, err)
		}
		entries, err := os.ReadDir(root)
		if err != nil {
			return Result{}, fmt.Errorf("read Apps root %q: %w", root, err)
		}
		for _, entry := range entries {
			if ignored(entry.Name()) {
				continue
			}
			if !entry.IsDir() && !looseHTML(entry) {
				continue
			}

			baseSlug := Slug(entry.Name())
			if baseSlug == "" {
				result.Warnings = append(result.Warnings, Warning{
					Code:    "invalid_slug",
					Path:    filepath.Join(root, entry.Name()),
					Message: fmt.Sprintf("%q does not have a URL-safe name and was not mounted", entry.Name()),
				})
				continue
			}
			if _, reserved := reservedSlugs[baseSlug]; reserved {
				result.Warnings = append(result.Warnings, Warning{
					Code:    "reserved_slug",
					Path:    filepath.Join(root, entry.Name()),
					Message: fmt.Sprintf("%q is reserved by Dropserve and was not mounted", baseSlug),
				})
				continue
			}

			slugUses[strings.ToLower(baseSlug)]++
			slug := baseSlug
			if use := slugUses[strings.ToLower(baseSlug)]; use > 1 {
				slug = fmt.Sprintf("%s-%d", baseSlug, use)
			}

			fullPath := filepath.Join(root, entry.Name())
			application := app.App{
				Slug:      slug,
				Name:      displayName(entry.Name()),
				Path:      fullPath,
				Kind:      app.KindStatic,
				LooseFile: !entry.IsDir(),
			}
			if entry.IsDir() {
				application.Index, err = findIndex(fullPath)
				if err != nil {
					return Result{}, err
				}
				application.DirectoryListing = application.Index == ""
			}
			result.Apps = append(result.Apps, application)
		}
	}
	return result, nil
}

// Slug returns a stable lowercase ASCII URL segment derived from a file name.
func Slug(name string) string {
	extension := filepath.Ext(name)
	if strings.EqualFold(extension, ".html") || strings.EqualFold(extension, ".htm") {
		name = strings.TrimSuffix(name, extension)
	}

	var builder strings.Builder
	separatorPending := false
	for _, character := range strings.ToLower(name) {
		switch {
		case character >= 'a' && character <= 'z', character >= '0' && character <= '9':
			if separatorPending && builder.Len() > 0 {
				builder.WriteByte('-')
			}
			builder.WriteRune(character)
			separatorPending = false
		case character == ' ', character == '_', character == '-', unicode.IsSpace(character):
			separatorPending = true
		default:
			// Non-ASCII and punctuation are deliberately omitted.
		}
	}
	return strings.Trim(builder.String(), "-")
}

func ignored(name string) bool {
	if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
		return true
	}
	_, ignoredName := ignoredNames[strings.ToLower(name)]
	return ignoredName
}

func looseHTML(entry fs.DirEntry) bool {
	if entry.IsDir() {
		return false
	}
	extension := filepath.Ext(entry.Name())
	return strings.EqualFold(extension, ".html") || strings.EqualFold(extension, ".htm")
}

func findIndex(root string) (string, error) {
	for _, candidate := range []string{"index.html", "index.htm"} {
		_, err := os.Stat(filepath.Join(root, candidate))
		switch {
		case err == nil:
			return candidate, nil
		case errors.Is(err, os.ErrNotExist):
			continue
		default:
			return "", fmt.Errorf("inspect index in %q: %w", root, err)
		}
	}
	return "", nil
}

func displayName(name string) string {
	extension := filepath.Ext(name)
	if strings.EqualFold(extension, ".html") || strings.EqualFold(extension, ".htm") {
		name = strings.TrimSuffix(name, extension)
	}
	name = strings.ReplaceAll(name, "_", " ")
	name = strings.ReplaceAll(name, "-", " ")
	return strings.Join(strings.Fields(name), " ")
}
