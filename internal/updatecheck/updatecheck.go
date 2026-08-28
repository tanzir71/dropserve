// Package updatecheck reads release metadata without downloading or executing updates.
package updatecheck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/mod/semver"
)

// LatestReleaseAPI is the only endpoint used by the update checker.
const LatestReleaseAPI = "https://api.github.com/repos/tanzir71/dropserve/releases/latest"

const maximumResponseBytes = 64 << 10

// HTTPClient is the update checker's complete network boundary.
type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

// Options configures one metadata-only update check.
type Options struct {
	Client         HTTPClient
	CurrentVersion string
}

// Notification is an optional link to a newer GitHub release page.
type Notification struct {
	Available bool
	Version   string
	URL       string
}

// Check performs one bounded GitHub API request and compares semantic versions.
// It never requests an asset URL, writes a file, or starts a process.
func Check(ctx context.Context, options Options) (Notification, error) {
	current := normalizeVersion(options.CurrentVersion)
	if !semver.IsValid(current) {
		return Notification{}, fmt.Errorf("current Dropserve version %q is not semantic", options.CurrentVersion)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, LatestReleaseAPI, nil)
	if err != nil {
		return Notification{}, fmt.Errorf("create update request: %w", err)
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	request.Header.Set("User-Agent", "dropserve-update-check/"+strings.TrimPrefix(current, "v"))
	client := options.Client
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return Notification{}, fmt.Errorf("check for a Dropserve release: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
		return Notification{}, fmt.Errorf("check for a Dropserve release: GitHub returned %s", response.Status)
	}
	var latest struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, maximumResponseBytes+1))
	if err := decoder.Decode(&latest); err != nil {
		return Notification{}, fmt.Errorf("read latest Dropserve release: %w", err)
	}
	version := normalizeVersion(latest.TagName)
	if !semver.IsValid(version) {
		return Notification{}, fmt.Errorf("latest Dropserve release tag %q is not semantic", latest.TagName)
	}
	if semver.Compare(version, current) <= 0 {
		return Notification{}, nil
	}
	releaseURL, err := safeReleaseURL(latest.HTMLURL)
	if err != nil {
		return Notification{}, err
	}
	return Notification{Available: true, Version: strings.TrimPrefix(version, "v"), URL: releaseURL}, nil
}

func normalizeVersion(value string) string {
	value = strings.TrimSpace(value)
	if value != "" && !strings.HasPrefix(value, "v") {
		value = "v" + value
	}
	return value
}

func safeReleaseURL(value string) (string, error) {
	parsed, err := url.Parse(value)
	if err != nil {
		return "", fmt.Errorf("parse latest release link: %w", err)
	}
	const releaseTagPrefix = "/tanzir71/dropserve/releases/tag/"
	releaseTag := strings.TrimPrefix(parsed.EscapedPath(), releaseTagPrefix)
	if parsed.Scheme != "https" || !strings.EqualFold(parsed.Host, "github.com") || parsed.User != nil ||
		releaseTag == parsed.EscapedPath() || releaseTag == "" || strings.Contains(releaseTag, "/") ||
		parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("latest release link is not a Dropserve GitHub release page")
	}
	return parsed.String(), nil
}
