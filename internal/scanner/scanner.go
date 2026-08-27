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
	Roots      []string
	Registered []string
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
	collector := newCollector()
	for _, configuredRoot := range options.Roots {
		root, err := filepath.Abs(configuredRoot)
		if err != nil {
			return Result{}, fmt.Errorf("resolve Apps root %q: %w", configuredRoot, err)
		}
		entries, err := os.ReadDir(root)
		if errors.Is(err, os.ErrNotExist) {
			collector.result.Warnings = append(collector.result.Warnings, Warning{
				Code:    "root_missing",
				Path:    root,
				Message: fmt.Sprintf("Create the Apps root at %s and Dropserve will pick it up automatically", root),
			})
			continue
		}
		if err != nil {
			return Result{}, fmt.Errorf("read Apps root %q: %w", root, err)
		}
		for _, entry := range entries {
			if err := collector.add(root, entry); err != nil {
				return Result{}, err
			}
		}
	}

	for _, registeredPath := range options.Registered {
		absolute, err := filepath.Abs(registeredPath)
		if err != nil {
			return Result{}, fmt.Errorf("resolve registered app %q: %w", registeredPath, err)
		}
		info, err := os.Stat(absolute)
		if err != nil {
			return Result{}, fmt.Errorf("open registered app %q: %w", absolute, err)
		}
		if err := collector.add(filepath.Dir(absolute), fs.FileInfoToDirEntry(info)); err != nil {
			return Result{}, err
		}
	}
	return collector.result, nil
}

type collector struct {
	result     Result
	slugUses   map[string]int
	slugOwners map[string]string
	seenPaths  map[string]struct{}
}

func newCollector() *collector {
	return &collector{
		slugUses:   make(map[string]int),
		slugOwners: make(map[string]string),
		seenPaths:  make(map[string]struct{}),
	}
}

func (collector *collector) add(root string, entry fs.DirEntry) error {
	fullPath := filepath.Join(root, entry.Name())
	if _, seen := collector.seenPaths[filepath.Clean(fullPath)]; seen {
		return nil
	}
	collector.seenPaths[filepath.Clean(fullPath)] = struct{}{}

	if reservedName(entry.Name()) {
		collector.result.Warnings = append(collector.result.Warnings, Warning{
			Code:    "reserved_slug",
			Path:    fullPath,
			Message: fmt.Sprintf("Rename %q because Dropserve reserves that address for its own features", entry.Name()),
		})
		return nil
	}
	if unsafeName(entry.Name()) {
		collector.result.Warnings = append(collector.result.Warnings, Warning{
			Code:    "unsafe_name",
			Path:    fullPath,
			Message: fmt.Sprintf("Rename %q without a leading '..' before Dropserve can serve it", entry.Name()),
		})
		return nil
	}
	if ignored(entry.Name()) {
		return nil
	}
	if !entry.IsDir() && !looseHTML(entry) {
		return nil
	}

	baseSlug := Slug(entry.Name())
	if baseSlug == "" {
		collector.result.Warnings = append(collector.result.Warnings, Warning{
			Code:    "invalid_slug",
			Path:    fullPath,
			Message: fmt.Sprintf("%q does not have a URL-safe name and was not mounted", entry.Name()),
		})
		return nil
	}
	if _, reserved := reservedSlugs[baseSlug]; reserved {
		collector.result.Warnings = append(collector.result.Warnings, Warning{
			Code:    "reserved_slug",
			Path:    fullPath,
			Message: fmt.Sprintf("%q is reserved by Dropserve and was not mounted", baseSlug),
		})
		return nil
	}

	slugKey := strings.ToLower(baseSlug)
	collector.slugUses[slugKey]++
	slug := baseSlug
	if use := collector.slugUses[slugKey]; use > 1 {
		slug = fmt.Sprintf("%s-%d", baseSlug, use)
		collector.result.Warnings = append(collector.result.Warnings, Warning{
			Code: "slug_collision",
			Path: fullPath,
			Message: fmt.Sprintf(
				"Apps at %s and %s share the address %q; the later app is available as %q",
				collector.slugOwners[slugKey],
				fullPath,
				baseSlug,
				slug,
			),
		})
	} else {
		collector.slugOwners[slugKey] = fullPath
	}

	application := app.App{
		Slug:      slug,
		Name:      displayName(entry.Name()),
		Path:      fullPath,
		Kind:      app.KindStatic,
		LooseFile: !entry.IsDir(),
		FileCount: 1,
	}
	var err error
	if entry.IsDir() {
		application.Index, err = findIndex(fullPath)
		if err != nil {
			return err
		}
		application.DirectoryListing = application.Index == ""
		application.FileCount, err = countFiles(fullPath)
		if err != nil {
			return fmt.Errorf("walk app %q: %w", fullPath, err)
		}
	}
	collector.result.Apps = append(collector.result.Apps, application)
	return nil
}

// Slug returns a stable lowercase ASCII URL segment derived from a file name.
func Slug(name string) string {
	if unsafeName(name) {
		return ""
	}
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

func unsafeName(name string) bool {
	return strings.HasPrefix(name, "..")
}

func reservedName(name string) bool {
	extension := filepath.Ext(name)
	if strings.EqualFold(extension, ".html") || strings.EqualFold(extension, ".htm") {
		name = strings.TrimSuffix(name, extension)
	}
	_, reserved := reservedSlugs[strings.ToLower(name)]
	return reserved
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

func countFiles(root string) (int64, error) {
	walkPath, err := pathForWalk(root)
	if err != nil {
		return 0, err
	}
	var count int64
	err = filepath.WalkDir(walkPath, func(_ string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() {
			count++
		}
		return nil
	})
	return count, err
}
