// Command memory enforces Dropserve's M11 resident-memory floor.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tanzir71/dropserve/internal/scanner"
	dropserver "github.com/tanzir71/dropserve/internal/server"
)

const fixtureApps = 50
const memoryLimitBytes = 60 * 1024 * 1024
const defaultSoakDuration = 5 * time.Minute
const requestPause = 10 * time.Millisecond

func main() {
	soakDuration := flag.Duration("soak", defaultSoakDuration, "duration to exercise the static apps before measuring RSS")
	flag.Parse()
	if err := run(*soakDuration); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "memory floor: %v\n", err)
		os.Exit(1)
	}
}

func run(soakDuration time.Duration) error {
	if soakDuration <= 0 {
		return fmt.Errorf("soak duration must be positive: %s", soakDuration)
	}
	appsRoot, err := os.MkdirTemp("", "dropserve-memory-")
	if err != nil {
		return fmt.Errorf("create fixture root: %w", err)
	}
	defer func() {
		_ = os.RemoveAll(appsRoot)
	}()
	if err := createFixtures(appsRoot); err != nil {
		return err
	}

	server, err := dropserver.New(scanner.Options{Roots: []string{appsRoot}})
	if err != nil {
		return fmt.Errorf("create Dropserve server: %w", err)
	}
	defer func() {
		_ = server.Close()
	}()
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()
	client := &http.Client{Timeout: 5 * time.Second}
	defer client.CloseIdleConnections()

	startedRSS, err := residentBytes()
	if err != nil {
		return err
	}
	maximumRSS := startedRSS
	requests := 0
	deadline := time.Now().Add(soakDuration)
	nextSample := time.Now().Add(time.Minute)
	for time.Now().Before(deadline) {
		appIndex := requests % fixtureApps
		address := fmt.Sprintf("%s/app-%02d/", httpServer.URL, appIndex)
		if err := requestApp(client, address, appIndex); err != nil {
			return err
		}
		requests++
		if time.Now().After(nextSample) {
			current, sampleErr := residentBytes()
			if sampleErr != nil {
				return sampleErr
			}
			if current > maximumRSS {
				maximumRSS = current
			}
			fmt.Printf("memory_sample elapsed=%s rss_bytes=%d\n", soakDuration-time.Until(deadline), current)
			nextSample = nextSample.Add(time.Minute)
		}
		time.Sleep(requestPause)
	}
	finalRSS, err := residentBytes()
	if err != nil {
		return err
	}
	if finalRSS > maximumRSS {
		maximumRSS = finalRSS
	}

	fmt.Println("M11 memory transcript")
	fmt.Printf("apps=%d soak=%s requests=%d\n", fixtureApps, soakDuration, requests)
	fmt.Printf("rss_start_bytes=%d rss_final_bytes=%d rss_max_sample_bytes=%d limit_bytes=%d\n", startedRSS, finalRSS, maximumRSS, memoryLimitBytes)
	fmt.Printf("rss_final_mib=%.2f limit_mib=%.2f\n", mebibytes(finalRSS), mebibytes(memoryLimitBytes))
	if finalRSS >= memoryLimitBytes {
		return fmt.Errorf("final resident memory %.2f MiB is not below %.2f MiB", mebibytes(finalRSS), mebibytes(memoryLimitBytes))
	}
	return nil
}

func createFixtures(root string) error {
	for index := range fixtureApps {
		appRoot := filepath.Join(root, fmt.Sprintf("app-%02d", index))
		if err := os.Mkdir(appRoot, 0o750); err != nil {
			return fmt.Errorf("create fixture app %d: %w", index, err)
		}
		body := fmt.Sprintf("<!doctype html><title>Memory %02d</title><h1>Memory app %02d</h1>", index, index)
		if err := os.WriteFile(filepath.Join(appRoot, "index.html"), []byte(body), 0o600); err != nil {
			return fmt.Errorf("write fixture app %d: %w", index, err)
		}
	}
	return nil
}

func requestApp(client *http.Client, address string, index int) error {
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, address, nil)
	if err != nil {
		return fmt.Errorf("create memory request: %w", err)
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("request memory app %d: %w", index, err)
	}
	body, readErr := io.ReadAll(io.LimitReader(response.Body, 4<<10))
	closeErr := response.Body.Close()
	if readErr != nil {
		return fmt.Errorf("read memory app %d: %w", index, readErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close memory app %d: %w", index, closeErr)
	}
	if response.StatusCode != http.StatusOK || !strings.Contains(string(body), fmt.Sprintf("Memory app %02d", index)) {
		return fmt.Errorf("memory app %d returned %d %q", index, response.StatusCode, body)
	}
	return nil
}

func mebibytes(bytes uint64) float64 {
	return float64(bytes) / (1024 * 1024)
}
