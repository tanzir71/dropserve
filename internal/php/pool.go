package php

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

const defaultStartTimeout = 10 * time.Second

// PoolOptions configures a local php-cgi worker pool.
type PoolOptions struct {
	Executable   string
	INIPath      string
	Workers      int
	StartTimeout time.Duration
	Output       io.Writer
	Command      func(context.Context, string) *exec.Cmd
}

// Pool owns a bounded set of loopback php-cgi worker processes.
type Pool struct {
	mu      sync.Mutex
	workers []*worker
	next    atomic.Uint64
	closed  bool
}

type worker struct {
	address string
	command *exec.Cmd
	done    chan error
}

// StartPool launches php-cgi workers and waits until each loopback socket is ready.
func StartPool(ctx context.Context, options PoolOptions) (*Pool, error) {
	if options.Command == nil && options.Executable == "" {
		return nil, errors.New("start PHP pool: php-cgi executable is required")
	}
	workerCount := options.Workers
	if workerCount <= 0 {
		workerCount = runtime.NumCPU()
		if workerCount < 2 {
			workerCount = 2
		}
		if workerCount > 4 {
			workerCount = 4
		}
	}
	if options.StartTimeout <= 0 {
		options.StartTimeout = defaultStartTimeout
	}
	if options.Output == nil {
		options.Output = io.Discard
	}

	pool := &Pool{}
	for index := 0; index < workerCount; index++ {
		address, err := availableAddress(ctx)
		if err != nil {
			_ = pool.Close()
			return nil, fmt.Errorf("reserve PHP worker address: %w", err)
		}
		command := options.command(ctx, address)
		command.Stdout = options.Output
		command.Stderr = options.Output
		configureWorkerCommand(command)
		if err := command.Start(); err != nil {
			_ = pool.Close()
			return nil, fmt.Errorf("start PHP worker %d: %w", index+1, err)
		}
		current := &worker{address: address, command: command, done: make(chan error, 1)}
		pool.workers = append(pool.workers, current)
		go func() {
			current.done <- command.Wait()
		}()
		if err := waitForWorker(current, options.StartTimeout); err != nil {
			_ = pool.Close()
			return nil, fmt.Errorf("start PHP worker %d: %w", index+1, err)
		}
	}
	return pool, nil
}

func (options PoolOptions) command(ctx context.Context, address string) *exec.Cmd {
	if options.Command != nil {
		return options.Command(ctx, address)
	}
	arguments := []string{"-b", address}
	if options.INIPath != "" {
		arguments = append(arguments, "-c", options.INIPath)
	}
	return exec.CommandContext(ctx, options.Executable, arguments...) // #nosec G204 -- executable is the verified runtime pack path.
}

func availableAddress(ctx context.Context) (string, error) {
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		return "", err
	}
	return address, nil
}

func waitForWorker(current *worker, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		dialer := &net.Dialer{Timeout: 100 * time.Millisecond}
		connection, err := dialer.DialContext(context.Background(), "tcp", current.address)
		if err == nil {
			_ = connection.Close()
			return nil
		}
		select {
		case waitErr := <-current.done:
			current.done <- waitErr
			return fmt.Errorf("php-cgi exited before accepting requests: %w", waitErr)
		default:
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("php-cgi did not listen on %s within %s", current.address, timeout)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// Handler returns an app-specific FastCGI handler balanced across the pool.
func (pool *Pool) Handler(documentRoot, slug string) http.Handler {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	handlers := make([]http.Handler, 0, len(pool.workers))
	for _, current := range pool.workers {
		handlers = append(handlers, New(documentRoot, slug, "tcp", current.address))
	}
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if len(handlers) == 0 {
			http.Error(response, "The PHP runtime is not running.", http.StatusBadGateway)
			return
		}
		index := (pool.next.Add(1) - 1) % uint64(len(handlers))
		handlers[index].ServeHTTP(response, request)
	})
}

// Close stops every worker and waits for process handles to be released.
func (pool *Pool) Close() error {
	pool.mu.Lock()
	if pool.closed {
		pool.mu.Unlock()
		return nil
	}
	pool.closed = true
	workers := append([]*worker(nil), pool.workers...)
	pool.workers = nil
	pool.mu.Unlock()

	var closeErrors []error
	for _, current := range workers {
		if current.command.Process != nil {
			if err := current.command.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
				closeErrors = append(closeErrors, fmt.Errorf("stop PHP worker at %s: %w", current.address, err))
			}
		}
	}
	for _, current := range workers {
		select {
		case <-current.done:
		case <-time.After(5 * time.Second):
			closeErrors = append(closeErrors, fmt.Errorf("PHP worker at %s did not exit within 5s", current.address))
		}
	}
	return errors.Join(closeErrors...)
}

// WorkerCount reports the configured process count.
func (pool *Pool) WorkerCount() int {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	return len(pool.workers)
}
