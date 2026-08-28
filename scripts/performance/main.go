// Command performance enforces Dropserve's M11 loopback performance floor.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tanzir71/dropserve/internal/scanner"
	dropserver "github.com/tanzir71/dropserve/internal/server"
)

const fixtureApps = 200
const dashboardTTFBFloor = 100 * time.Millisecond
const appsAPILatencyFloor = 200 * time.Millisecond
const minimumStaticRPS = 500
const staticPayloadBytes = 10 * 1024
const sampleCount = 20
const loadDuration = 2 * time.Second
const loadWorkers = 8

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "performance floor: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	appsRoot, err := os.MkdirTemp("", "dropserve-performance-")
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
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        32,
			MaxIdleConnsPerHost: 32,
			IdleConnTimeout:     30 * time.Second,
		},
	}
	defer client.CloseIdleConnections()

	dashboardURL := httpServer.URL + "/"
	appsURL := httpServer.URL + "/_dropserve/api/apps"
	staticURL := httpServer.URL + "/app-000/payload.bin"
	for range 3 {
		if _, err := firstByte(client, dashboardURL); err != nil {
			return err
		}
		if _, err := fullResponse(client, appsURL, 0); err != nil {
			return err
		}
	}

	dashboardSamples := make([]time.Duration, 0, sampleCount)
	appsSamples := make([]time.Duration, 0, sampleCount)
	for range sampleCount {
		duration, sampleErr := firstByte(client, dashboardURL)
		if sampleErr != nil {
			return sampleErr
		}
		dashboardSamples = append(dashboardSamples, duration)
		duration, sampleErr = fullResponse(client, appsURL, 0)
		if sampleErr != nil {
			return sampleErr
		}
		appsSamples = append(appsSamples, duration)
	}
	dashboardP95 := percentile95(dashboardSamples)
	appsP95 := percentile95(appsSamples)
	requests, elapsed, err := staticLoad(client, staticURL)
	if err != nil {
		return err
	}
	staticRPS := float64(requests) / elapsed.Seconds()

	fmt.Println("M11 performance transcript")
	fmt.Printf("apps=%d\n", fixtureApps)
	fmt.Printf("dashboard_ttfb_p95=%s threshold=%s\n", dashboardP95, dashboardTTFBFloor)
	fmt.Printf("api_apps_full_p95=%s threshold=%s\n", appsP95, appsAPILatencyFloor)
	fmt.Printf("static_bytes=%d requests=%d elapsed=%s requests_per_second=%.1f threshold=%d\n", staticPayloadBytes, requests, elapsed, staticRPS, minimumStaticRPS)

	if dashboardP95 >= dashboardTTFBFloor {
		return fmt.Errorf("dashboard p95 TTFB %s is not below %s", dashboardP95, dashboardTTFBFloor)
	}
	if appsP95 >= appsAPILatencyFloor {
		return fmt.Errorf("apps API p95 response %s is not below %s", appsP95, appsAPILatencyFloor)
	}
	if staticRPS < minimumStaticRPS {
		return fmt.Errorf("static throughput %.1f req/s is below %d", staticRPS, minimumStaticRPS)
	}
	return nil
}

func createFixtures(root string) error {
	payload := make([]byte, staticPayloadBytes)
	for index := range fixtureApps {
		appRoot := filepath.Join(root, fmt.Sprintf("app-%03d", index))
		if err := os.Mkdir(appRoot, 0o750); err != nil {
			return fmt.Errorf("create fixture app %d: %w", index, err)
		}
		indexHTML := []byte(fmt.Sprintf("<!doctype html><title>App %03d</title><h1>App %03d</h1>", index, index))
		if err := os.WriteFile(filepath.Join(appRoot, "index.html"), indexHTML, 0o600); err != nil {
			return fmt.Errorf("write fixture app %d: %w", index, err)
		}
		if index == 0 {
			if err := os.WriteFile(filepath.Join(appRoot, "payload.bin"), payload, 0o600); err != nil {
				return fmt.Errorf("write static payload: %w", err)
			}
		}
	}
	return nil
}

func firstByte(client *http.Client, address string) (time.Duration, error) {
	start := time.Now()
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, address, nil)
	if err != nil {
		return 0, fmt.Errorf("create request for %s: %w", address, err)
	}
	response, err := client.Do(request)
	if err != nil {
		return 0, fmt.Errorf("request %s: %w", address, err)
	}
	elapsed := time.Since(start)
	_, readErr := io.Copy(io.Discard, response.Body)
	closeErr := response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("request %s returned %d", address, response.StatusCode)
	}
	if readErr != nil || closeErr != nil {
		return 0, errors.Join(readErr, closeErr)
	}
	return elapsed, nil
}

func fullResponse(client *http.Client, address string, expectedBytes int64) (time.Duration, error) {
	start := time.Now()
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, address, nil)
	if err != nil {
		return 0, fmt.Errorf("create request for %s: %w", address, err)
	}
	response, err := client.Do(request)
	if err != nil {
		return 0, fmt.Errorf("request %s: %w", address, err)
	}
	written, readErr := io.Copy(io.Discard, response.Body)
	closeErr := response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("request %s returned %d", address, response.StatusCode)
	}
	if readErr != nil || closeErr != nil {
		return 0, errors.Join(readErr, closeErr)
	}
	if expectedBytes != 0 && written != expectedBytes {
		return 0, fmt.Errorf("request %s returned %d bytes, want %d", address, written, expectedBytes)
	}
	return time.Since(start), nil
}

func percentile95(samples []time.Duration) time.Duration {
	sorted := append([]time.Duration(nil), samples...)
	sort.Slice(sorted, func(first, second int) bool { return sorted[first] < sorted[second] })
	index := (95*len(sorted)+99)/100 - 1
	return sorted[index]
}

func staticLoad(client *http.Client, address string) (int64, time.Duration, error) {
	stop := make(chan struct{})
	errorsFound := make(chan error, loadWorkers)
	var requests atomic.Int64
	var workers sync.WaitGroup
	workers.Add(loadWorkers)
	start := time.Now()
	for range loadWorkers {
		go func() {
			defer workers.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				if _, err := fullResponse(client, address, staticPayloadBytes); err != nil {
					select {
					case errorsFound <- err:
					default:
					}
					return
				}
				requests.Add(1)
			}
		}()
	}
	timer := time.NewTimer(loadDuration)
	<-timer.C
	close(stop)
	workers.Wait()
	elapsed := time.Since(start)
	close(errorsFound)
	for err := range errorsFound {
		return requests.Load(), elapsed, err
	}
	return requests.Load(), elapsed, nil
}
