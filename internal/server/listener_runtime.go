package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"
)

// ListenerRuntime keeps one HTTP handler/server alive while its underlying
// network listener is replaced after sleep or adapter changes.
type ListenerRuntime struct {
	mu         sync.RWMutex
	server     *http.Server
	listener   net.Listener
	address    string
	generation uint64
	active     bool
	closing    bool
	lastError  error
}

// NewListenerRuntime creates a recoverable HTTP serving boundary.
func NewListenerRuntime(handler http.Handler) *ListenerRuntime {
	return &ListenerRuntime{server: &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}}
}

// Start serves through listener without replacing the handler or app state.
func (runtime *ListenerRuntime) Start(listener net.Listener) {
	runtime.mu.Lock()
	if runtime.closing || runtime.active {
		runtime.mu.Unlock()
		_ = listener.Close()
		return
	}
	runtime.generation++
	generation := runtime.generation
	runtime.listener = listener
	runtime.address = listener.Addr().String()
	runtime.active = true
	runtime.lastError = nil
	runtime.mu.Unlock()

	go func() {
		err := runtime.server.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		runtime.mu.Lock()
		if runtime.generation == generation {
			runtime.active = false
			runtime.listener = nil
			runtime.lastError = err
		}
		runtime.mu.Unlock()
	}()
}

// Healthy reports whether an active listener is currently serving.
func (runtime *ListenerRuntime) Healthy() bool {
	runtime.mu.RLock()
	defer runtime.mu.RUnlock()
	return runtime.active
}

// Address returns the current or most recently served address.
func (runtime *ListenerRuntime) Address() string {
	runtime.mu.RLock()
	defer runtime.mu.RUnlock()
	return runtime.address
}

// LastError returns the most recent listener failure.
func (runtime *ListenerRuntime) LastError() error {
	runtime.mu.RLock()
	defer runtime.mu.RUnlock()
	return runtime.lastError
}

// CloseActiveListener simulates or handles an externally invalidated network
// listener without shutting down the HTTP server itself.
func (runtime *ListenerRuntime) CloseActiveListener() error {
	runtime.mu.RLock()
	listener := runtime.listener
	runtime.mu.RUnlock()
	if listener == nil {
		return nil
	}
	return listener.Close()
}

// Shutdown permanently stops the HTTP server and any current listener.
func (runtime *ListenerRuntime) Shutdown(ctx context.Context) error {
	runtime.mu.Lock()
	runtime.closing = true
	runtime.mu.Unlock()
	err := runtime.server.Shutdown(ctx)
	runtime.mu.Lock()
	runtime.active = false
	runtime.listener = nil
	runtime.mu.Unlock()
	return err
}
