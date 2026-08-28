package server

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tanzir71/dropserve/internal/supervisor"
)

func TestConnectionLimitWaiterUnblocksWhenListenerCloses(t *testing.T) {
	var listenConfig net.ListenConfig
	base, err := listenConfig.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	slots := make(chan struct{}, 1)
	slots <- struct{}{}
	limited := &connectionLimitedListener{Listener: base, slots: slots, done: make(chan struct{})}
	result := make(chan error, 1)
	go func() {
		_, acceptErr := limited.Accept()
		result <- acceptErr
	}()
	if err := limited.Close(); err != nil {
		t.Fatalf("close limited listener: %v", err)
	}
	select {
	case acceptErr := <-result:
		if acceptErr == nil {
			t.Fatal("blocked Accept returned nil after close")
		}
	case <-time.After(time.Second):
		t.Fatal("connection-limit waiter remained blocked after close")
	}
}

func TestLiveLogStreamHasBoundedClientCount(t *testing.T) {
	server := &Server{logClients: make(chan struct{}, 1)}
	server.logClients <- struct{}{}
	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "http://dropserve.test/_dropserve/api/logs/app", nil)
	response := httptest.NewRecorder()
	server.streamCommandLogs(response, request, "app", supervisor.Snapshot{})
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("full live-log pool returned %d, want 503", response.Code)
	}
}
