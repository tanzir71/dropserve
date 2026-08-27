// Command gate runs Dropserve's portable build and verification targets.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
)

const modulePath = "github.com/tanzir71/dropserve"

func main() {
	if len(os.Args) != 2 {
		fatalf("usage: go run ./scripts/gate.go <check|build|test|lint|run>")
	}

	var err error
	switch os.Args[1] {
	case "check":
		err = check()
	case "build":
		err = build("bin")
	case "test":
		err = test()
	case "lint":
		err = lint()
	case "run":
		err = runGo("run", "./cmd/dropserve")
	default:
		err = fmt.Errorf("unknown gate target %q", os.Args[1])
	}
	if err != nil {
		fatalf("%v", err)
	}
}

func check() error {
	steps := []struct {
		name string
		fn   func() error
	}{
		{"format", format},
		{"lint", lint},
		{"test", test},
		{"version injection", testVersionInjection},
		{"zero-CGO cross-build", crossBuild},
		{"shipped-file scan", scanShippedFiles},
	}
	for _, step := range steps {
		fmt.Printf("==> %s\n", step.name)
		if err := step.fn(); err != nil {
			return fmt.Errorf("%s: %w", step.name, err)
		}
	}
	fmt.Println("gate: green")
	return nil
}

func format() error {
	files, err := goFiles()
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return errors.New("no Go files found")
	}
	args := append([]string{"-l"}, files...)
	// #nosec G204 -- gofmt is fixed and the arguments are repository Go files discovered above.
	output, err := exec.CommandContext(context.Background(), "gofmt", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("gofmt: %w: %s", err, output)
	}
	if len(bytes.TrimSpace(output)) != 0 {
		return fmt.Errorf("these files need gofmt:\n%s", output)
	}
	return nil
}

func goFiles() ([]string, error) {
	var files []string
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && (entry.Name() == ".git" || entry.Name() == "bin" || entry.Name() == "dist") {
			return fs.SkipDir
		}
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".go") {
			files = append(files, path)
		}
		return nil
	})
	sort.Strings(files)
	return files, err
}

func lint() error {
	if _, err := exec.LookPath("golangci-lint"); err == nil {
		return command("golangci-lint", "run")
	}
	fmt.Println("golangci-lint is not installed; running go vet as the portable baseline")
	return runGo("vet", "./...")
}

func test() error {
	transcript, err := runTestsOnce()
	if err == nil {
		return nil
	}
	if runtime.GOOS != "windows" ||
		!strings.Contains(transcript, "go: unlinkat ") ||
		!strings.Contains(transcript, "used by another process") ||
		strings.Contains(transcript, "--- FAIL:") {
		return err
	}
	fmt.Println("Windows briefly retained a completed Go test executable; retrying the test suite once")
	_, retryErr := runTestsOnce()
	return retryErr
}

func runTestsOnce() (string, error) {
	var transcript bytes.Buffer
	// #nosec G204 -- executable and arguments are fixed by the repository gate.
	cmd := exec.CommandContext(context.Background(), "go", "test", "-race", "./...")
	cmd.Stdout = io.MultiWriter(os.Stdout, &transcript)
	cmd.Stderr = io.MultiWriter(os.Stderr, &transcript)
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return transcript.String(), fmt.Errorf("go test -race ./...: %w", err)
	}
	return transcript.String(), nil
}

func crossBuild() error {
	targets := [][2]string{{"windows", "amd64"}, {"linux", "amd64"}, {"darwin", "arm64"}}
	for _, target := range targets {
		fmt.Printf("    %s/%s\n", target[0], target[1])
		cmd := exec.CommandContext(context.Background(), "go", "build", "./...")
		cmd.Env = withEnv(os.Environ(), map[string]string{
			"CGO_ENABLED": "0",
			"GOOS":        target[0],
			"GOARCH":      target[1],
		})
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("go build for %s/%s: %w", target[0], target[1], err)
		}
	}
	return nil
}

func build(outputDir string) error {
	if err := os.MkdirAll(outputDir, 0o750); err != nil {
		return err
	}
	name := "dropserve"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	version := envOr("VERSION", "0.0.0-dev")
	commit := envOr("COMMIT", gitCommit())
	ldflags := linkFlags(version, commit)
	return runGo("build", "-trimpath", "-ldflags", ldflags, "-o", filepath.Join(outputDir, name), "./cmd/dropserve")
}

func testVersionInjection() error {
	dir, err := os.MkdirTemp("", "dropserve-version-")
	if err != nil {
		return err
	}
	defer func() {
		_ = os.RemoveAll(dir)
	}()
	name := "dropserve"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binary := filepath.Join(dir, name)
	if err := runGo("build", "-ldflags", linkFlags("1.2.3", "abc1234"), "-o", binary, "./cmd/dropserve"); err != nil {
		return err
	}
	// #nosec G204 -- binary is the executable built into the private temporary directory above.
	output, err := exec.CommandContext(context.Background(), binary, "version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("run version command: %w: %s", err, output)
	}
	if got, want := strings.TrimSpace(string(output)), "dropserve 1.2.3 (abc1234)"; got != want {
		return fmt.Errorf("version output %q, want %q", got, want)
	}
	return nil
}

func scanShippedFiles() error {
	patterns := []string{
		"US" + "ER/",
		"YO" + "UR-",
		"TO" + "DO:",
		"FIX" + "ME:",
		"<place" + "holder>",
	}
	var matches []string
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		clean := filepath.ToSlash(path)
		if entry.IsDir() {
			if clean == ".git" || clean == "docs/adr" || clean == "bin" || clean == "dist" {
				return fs.SkipDir
			}
			return nil
		}
		if entry.Name() == "DROPSERVE-HANDOVER.md" || entry.Name() == "STATE.md" {
			return nil
		}
		// #nosec G304,G122 -- the scan intentionally follows the checked-out shipped tree; matches are advisory build failures only.
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for lineNumber, line := range strings.Split(string(data), "\n") {
			for _, pattern := range patterns {
				if strings.Contains(line, pattern) {
					matches = append(matches, fmt.Sprintf("%s:%d: %s", clean, lineNumber+1, strings.TrimSpace(line)))
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(matches) != 0 {
		return fmt.Errorf("unfinished shipped text found:\n%s", strings.Join(matches, "\n"))
	}
	return nil
}

func runGo(args ...string) error {
	return command("go", args...)
}

func command(name string, args ...string) error {
	// #nosec G204 -- callers supply a fixed allowlist of development tools and internally constructed arguments.
	cmd := exec.CommandContext(context.Background(), name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}

func withEnv(base []string, values map[string]string) []string {
	result := make([]string, 0, len(base)+len(values))
	for _, item := range base {
		key := item
		if index := strings.IndexByte(item, '='); index >= 0 {
			key = item[:index]
		}
		if _, replaced := values[key]; !replaced {
			result = append(result, item)
		}
	}
	for key, value := range values {
		result = append(result, key+"="+value)
	}
	return result
}

func linkFlags(version, commit string) string {
	return fmt.Sprintf("-X %s/internal/version.Version=%s -X %s/internal/version.Commit=%s", modulePath, version, modulePath, commit)
}

func gitCommit() string {
	output, err := exec.CommandContext(context.Background(), "git", "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return "unknown"
	}
	commit := strings.TrimSpace(string(output))
	if matched, _ := regexp.MatchString(`^[0-9a-f]+$`, commit); !matched {
		return "unknown"
	}
	return commit
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "gate: "+format+"\n", args...)
	os.Exit(1)
}
