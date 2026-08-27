// Package indexer builds the read-only dashboard view of discovered apps.
package indexer

import (
	"bufio"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

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
	fileNames   []string
}

// Build returns an immutable dashboard snapshot in scanner order.
func Build(applications []app.App) []Entry {
	entries := make([]Entry, 0, len(applications))
	for _, application := range applications {
		entry := Entry{
			Slug:        application.Slug,
			Name:        application.Name,
			Description: readDescription(application.Path, application.LooseFile),
			Type:        string(application.Kind),
			Status:      "ready",
			URLs:        URLs{Path: "/" + strings.Trim(application.Slug, "/") + "/"},
			fileNames:   indexFileNames(application.Path, application.LooseFile),
		}
		entries = append(entries, entry)
	}
	return entries
}

// Search returns matching entries ordered by weighted field relevance.
func Search(entries []Entry, query string) []Entry {
	query = strings.TrimSpace(strings.ToLower(query))
	if query == "" {
		return append([]Entry(nil), entries...)
	}
	type scoredEntry struct {
		entry Entry
		score int
	}
	scored := make([]scoredEntry, 0, len(entries))
	for _, entry := range entries {
		score := 0
		if fieldMatches(entry.Name, query) || fieldMatches(entry.Slug, query) {
			score += 5
		}
		if fieldMatches(entry.Description, query) {
			score += 3
		}
		for _, fileName := range entry.fileNames {
			if fieldMatches(fileName, query) {
				score++
				break
			}
		}
		if score != 0 {
			scored = append(scored, scoredEntry{entry: entry, score: score})
		}
	}
	sort.SliceStable(scored, func(first, second int) bool {
		return scored[first].score > scored[second].score
	})
	results := make([]Entry, len(scored))
	for index, item := range scored {
		results[index] = item.entry
	}
	return results
}

func indexFileNames(appPath string, looseFile bool) []string {
	if looseFile {
		return []string{filepath.Base(appPath)}
	}
	const maximumFiles = 5_000
	fileNames := make([]string, 0, 32)
	_ = filepath.WalkDir(appPath, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		relative, err := filepath.Rel(appPath, path)
		if err != nil || relative == "." {
			return nil
		}
		depth := strings.Count(filepath.ToSlash(relative), "/") + 1
		if entry.IsDir() {
			if ignoredIndexDirectory(entry.Name()) || depth >= 3 {
				return filepath.SkipDir
			}
			return nil
		}
		if depth > 3 {
			return nil
		}
		fileNames = append(fileNames, entry.Name())
		if len(fileNames) >= maximumFiles {
			return filepath.SkipAll
		}
		return nil
	})
	return fileNames
}

func ignoredIndexDirectory(name string) bool {
	switch strings.ToLower(name) {
	case ".git", "node_modules", "venv", ".venv", "__pycache__", "vendor":
		return true
	default:
		return strings.HasPrefix(name, ".")
	}
}

func fieldMatches(value, query string) bool {
	value = strings.ToLower(value)
	if strings.Contains(value, query) {
		return true
	}
	queryTokens := strings.Fields(query)
	valueTokens := strings.FieldsFunc(value, func(character rune) bool {
		return (character < 'a' || character > 'z') && (character < '0' || character > '9')
	})
	for _, queryToken := range queryTokens {
		matched := false
		for _, valueToken := range valueTokens {
			if strings.HasPrefix(valueToken, queryToken) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return len(queryTokens) != 0
}

func readDescription(appPath string, looseFile bool) string {
	if looseFile {
		return ""
	}
	for _, name := range []string{"README.md", "README.txt"} {
		candidate := filepath.Join(appPath, name)
		// #nosec G304,G122 -- appPath is a read-only scanner result and names are fixed.
		file, err := os.Open(candidate)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return ""
		}
		description := firstParagraph(io.LimitReader(file, 64<<10))
		_ = file.Close()
		return description
	}
	return ""
}

func firstParagraph(reader io.Reader) string {
	scanner := bufio.NewScanner(reader)
	paragraph := make([]string, 0, 4)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			if len(paragraph) != 0 {
				break
			}
			continue
		}
		if strings.HasPrefix(line, "#") && len(paragraph) == 0 {
			continue
		}
		paragraph = append(paragraph, line)
	}
	return truncate(strings.Join(paragraph, " "), 200)
}

func truncate(value string, maximum int) string {
	if utf8.RuneCountInString(value) <= maximum {
		return value
	}
	runes := []rune(value)
	return string(runes[:maximum])
}
