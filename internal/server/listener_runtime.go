package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"
)

const maximumConnections = 256

// ListenerRuntime keeps one HTTP handler/server alive while its underlying
// network listener is replaced after sleep or adapter changes.
type ListenerRuntime struct {
	mu          sync.RWMutex
	server      *http.Server
	listener    net.Listener
	address     string
	generation  uint64
	active      bool
	closing     bool
	lastError   error
	connections chan struct{}
}

// NewListenerRuntime creates a recoverable HTTP serving boundary.
func NewListenerRuntime(handler http.Handler) *ListenerRuntime {
	return &ListenerRuntime{connections: make(chan struct{}, maximumConnections), server: &http.Server{
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
	limited := &connectionLimitedListener{
		Listener: listener,
		slots:    runtime.connections,
		done:     make(chan struct{}),
	}
	runtime.mu.Lock()
	if runtime.closing || runtime.active {
		runtime.mu.Unlock()
		_ = limited.Close()
		return
	}
	runtime.generation++
	generation := runtime.generation
	runtime.listener = limited
	runtime.address = listener.Addr().String()
	runtime.active = true
	runtime.lastError = nil
	runtime.mu.Unlock()

	go func() {
		err := runtime.server.Serve(limited)
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

type connectionLimitedListener struct {
	net.Listener
	slots     chan struct{}
	done      chan struct{}
	closeOnce sync.Once
}

func (listener *connectionLimitedListener) Accept() (net.Conn, error) {
	select {
	case listener.slots <- struct{}{}:
	case <-listener.done:
		return nil, net.ErrClosed
	}
	connection, err := listener.Listener.Accept()
	if err != nil {
		<-listener.slots
		return nil, err
	}
	return &limitedConnection{Conn: connection, release: func() { <-listener.slots }}, nil
}

func (listener *connectionLimitedListener) Close() error {
	listener.closeOnce.Do(func() { close(listener.done) })
	return listener.Listener.Close()
}

type limitedConnection struct {
	net.Conn
	once    sync.Once
	release func()
}

func (connection *limitedConnection) Close() error {
	err := connection.Conn.Close()
	connection.once.Do(connection.release)
	return err
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
