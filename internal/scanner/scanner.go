// Package scanner discovers apps from configured roots without modifying them.
package scanner

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
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
	Roots             []string
	Registered        []string
	LazyStartCommands bool
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
	collector := newCollector(options.LazyStartCommands)
	for _, configuredRoot := range options.Roots {
		root, err := filepath.Abs(configuredRoot)
		if err != nil {
			return Result{}, fmt.Errorf("resolve Apps folder %q: %w", configuredRoot, err)
		}
		if provider := syncProvider(root); provider != "" {
			collector.result.Warnings = append(collector.result.Warnings, Warning{
				Code: "sync_root",
				Path: root,
				Message: fmt.Sprintf(
					"Apps folder %s is inside %s; use %%USERPROFILE%%\\Dropserve for the most reliable file updates",
					root,
					provider,
				),
			})
		}
		entries, err := os.ReadDir(root)
		if errors.Is(err, os.ErrNotExist) {
			collector.result.Warnings = append(collector.result.Warnings, Warning{
				Code:    "root_missing",
				Path:    root,
				Message: fmt.Sprintf("Create the Apps folder at %s and Dropserve will pick it up automatically", root),
			})
			continue
		}
		if err != nil {
			return Result{}, fmt.Errorf("read Apps folder %q: %w", root, err)
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
	lazyStart  bool
}

func newCollector(lazyStart bool) *collector {
	return &collector{
		slugUses:   make(map[string]int),
		slugOwners: make(map[string]string),
		seenPaths:  make(map[string]struct{}),
		lazyStart:  lazyStart,
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
			Message: fmt.Sprintf("Rename %q using letters or numbers before Dropserve can serve it", entry.Name()),
		})
		return nil
	}
	if _, reserved := reservedSlugs[baseSlug]; reserved {
		collector.result.Warnings = append(collector.result.Warnings, Warning{
			Code:    "reserved_slug",
			Path:    fullPath,
			Message: fmt.Sprintf("Rename %q because Dropserve reserves that address for its own features", baseSlug),
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
		Slug:       slug,
		Name:       displayName(entry.Name()),
		Path:       fullPath,
		Kind:       app.KindStatic,
		LooseFile:  !entry.IsDir(),
		FileCount:  1,
		Autostart:  true,
		HealthPath: "/",
		PortEnv:    "PORT",
		Visibility: "lan",
		Status:     "ready",
	}
	var err error
	if entry.IsDir() {
		detection, detectionErr := app.Detect(fullPath)
		if detectionErr != nil {
			return detectionErr
		}
		application.Kind = detection.Kind
		application.Command = detection.Command
		application.Runtime = detection.Runtime
		application.Detection = detection.Reason
		if detection.Name != "" {
			application.Name = detection.Name
		}
		application.Description = detection.Description
		application.Icon = detection.Icon
		application.Tags = append([]string(nil), detection.Tags...)
		application.Environment = detection.Environment
		if detection.HealthPath != "" {
			application.HealthPath = detection.HealthPath
		}
		if detection.PortEnv != "" {
			application.PortEnv = detection.PortEnv
		}
		application.BaseHref = detection.BaseHref
		application.Autostart = detection.Autostart
		if collector.lazyStart && application.Kind == app.KindCommand {
			application.Autostart = false
		}
		application.SPA = detection.SPA
		application.Visibility = detection.Visibility
		application.Pinned = detection.Pinned
		application.Hidden = detection.Hidden
		for _, warning := range detection.Warnings {
			collector.result.Warnings = append(collector.result.Warnings, Warning{
				Code:    warning.Code,
				Path:    filepath.Join(fullPath, "dropserve.json"),
				Message: warning.Message,
			})
		}
		if application.Kind == app.KindStatic {
			if detection.Index != nil {
				candidate := filepath.Join(fullPath, filepath.FromSlash(*detection.Index))
				if info, statErr := os.Stat(candidate); statErr == nil && info.Mode().IsRegular() {
					application.Index = *detection.Index
				} else {
					collector.result.Warnings = append(collector.result.Warnings, Warning{
						Code:    "manifest_index_missing",
						Path:    filepath.Join(fullPath, "dropserve.json"),
						Message: fmt.Sprintf("dropserve.json index %q is not a readable file; using automatic index detection", *detection.Index),
					})
				}
			}
			if application.Index == "" {
				application.Index, err = findIndex(fullPath)
			}
		}
		if err != nil {
			return err
		}
		application.DirectoryListing = application.Kind == app.KindStatic && application.Index == ""
		if detection.DirectoryListing != nil {
			application.DirectoryListing = application.Kind == app.KindStatic && *detection.DirectoryListing
		}
		application.FileCount, err = countFiles(fullPath)
		if err != nil {
			return fmt.Errorf("walk app %q: %w", fullPath, err)
		}
		application.Databases, err = findDatabases(fullPath)
		if err != nil {
			return fmt.Errorf("find databases in app %q: %w", fullPath, err)
		}
	}
	collector.result.Apps = append(collector.result.Apps, application)
	return nil
}

func findDatabases(root string) ([]string, error) {
	var databases []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		switch strings.ToLower(filepath.Ext(entry.Name())) {
		case ".db", ".sqlite", ".sqlite3":
		default:
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		databases = append(databases, filepath.ToSlash(relative))
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(databases)
	return databases, nil
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

func syncProvider(path string) string {
	for _, segment := range strings.Split(filepath.ToSlash(filepath.Clean(path)), "/") {
		folded := strings.ToLower(segment)
		switch {
		case folded == "onedrive" || strings.HasPrefix(folded, "onedrive - "):
			return "OneDrive"
		case folded == "dropbox":
			return "Dropbox"
		case folded == "google drive":
			return "Google Drive"
		case folded == "icloud drive":
			return "iCloud Drive"
		}
	}
	return ""
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
