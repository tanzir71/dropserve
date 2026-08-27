package server

import (
	"fmt"
	"net/http"
	"sync"
)

const maximumEventClients = 64

type eventHub struct {
	mu      sync.Mutex
	clients map[chan struct{}]struct{}
}

func newEventHub() *eventHub {
	return &eventHub{clients: make(map[chan struct{}]struct{})}
}

func (hub *eventHub) serveHTTP(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		response.Header().Set("Allow", http.MethodGet)
		http.Error(response, "Use GET to open the Dropserve event stream.", http.StatusMethodNotAllowed)
		return
	}
	flusher, supported := response.(http.Flusher)
	if !supported {
		http.Error(response, "Streaming is not supported by this HTTP connection.", http.StatusInternalServerError)
		return
	}
	client, registered := hub.register()
	if !registered {
		http.Error(response, "Dropserve has reached its live-dashboard connection limit.", http.StatusServiceUnavailable)
		return
	}
	defer hub.unregister(client)

	response.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("X-Content-Type-Options", "nosniff")
	response.WriteHeader(http.StatusOK)
	if _, err := fmt.Fprint(response, ": connected\n\n"); err != nil {
		return
	}
	flusher.Flush()

	for {
		select {
		case <-request.Context().Done():
			return
		case <-client:
			if _, err := fmt.Fprint(response, "event: apps-changed\ndata: {}\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (hub *eventHub) publish() {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	for client := range hub.clients {
		select {
		case client <- struct{}{}:
		default:
			// A slow dashboard needs only the newest immutable app snapshot.
		}
	}
}

func (hub *eventHub) register() (chan struct{}, bool) {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	if len(hub.clients) >= maximumEventClients {
		return nil, false
	}
	client := make(chan struct{}, 1)
	hub.clients[client] = struct{}{}
	return client, true
}

func (hub *eventHub) unregister(client chan struct{}) {
	hub.mu.Lock()
	delete(hub.clients, client)
	hub.mu.Unlock()
}
