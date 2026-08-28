package server

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"
)

// HTTPSRuntime serves the same live handler as HTTP while loading the current
// leaf for each new TLS handshake.
type HTTPSRuntime struct {
	mu          sync.RWMutex
	server      *http.Server
	certificate func() (tls.Certificate, error)
	listener    net.Listener
	address     string
	generation  uint64
	active      bool
	closing     bool
	lastError   error
}

// NewHTTPSRuntime creates an additive TLS serving boundary. It performs no
// listener or trust-store action until its caller explicitly supplies one.
func NewHTTPSRuntime(handler http.Handler, certificate func() (tls.Certificate, error)) *HTTPSRuntime {
	return &HTTPSRuntime{
		server: &http.Server{
			Handler:           handler,
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       30 * time.Second,
			WriteTimeout:      30 * time.Second,
			IdleTimeout:       60 * time.Second,
			MaxHeaderBytes:    1 << 20,
		},
		certificate: certificate,
	}
}

// Start begins serving TLS on listener without changing the HTTP runtime.
func (runtime *HTTPSRuntime) Start(listener net.Listener) {
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

	configuration := &tls.Config{
		MinVersion: tls.VersionTLS12,
		GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
			certificate, err := runtime.certificate()
			if err != nil {
				return nil, err
			}
			return &certificate, nil
		},
	}
	tlsListener := tls.NewListener(listener, configuration)
	go func() {
		err := runtime.server.Serve(tlsListener)
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

// Address returns the current or most recently served TLS listener address.
func (runtime *HTTPSRuntime) Address() string {
	runtime.mu.RLock()
	defer runtime.mu.RUnlock()
	return runtime.address
}

// Healthy reports whether the TLS listener is active.
func (runtime *HTTPSRuntime) Healthy() bool {
	runtime.mu.RLock()
	defer runtime.mu.RUnlock()
	return runtime.active
}

// LastError returns the last listener failure.
func (runtime *HTTPSRuntime) LastError() error {
	runtime.mu.RLock()
	defer runtime.mu.RUnlock()
	return runtime.lastError
}

// Shutdown permanently stops the TLS listener.
func (runtime *HTTPSRuntime) Shutdown(ctx context.Context) error {
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
