package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

var errSSEWrite = errors.New("write failed")

type failingSSEWriter struct {
	header http.Header
}

func (w *failingSSEWriter) Header() http.Header {
	return w.header
}

func (w *failingSSEWriter) Write([]byte) (int, error) {
	return 0, errSSEWrite
}

func (w *failingSSEWriter) WriteHeader(int) {}

func (w *failingSSEWriter) Flush() {}

func TestEventSubscriberOverflowRequestsFullResync(t *testing.T) {
	subscriber := newEventSubscriber(2)
	subscriber.enqueue(serverEvent{name: "first", data: []byte("null")})
	subscriber.enqueue(serverEvent{name: "second", data: []byte("null")})
	subscriber.enqueue(serverEvent{name: "latest", data: []byte("null")})

	resync, ok := subscriber.next()
	if !ok || resync.name != resyncEventName {
		t.Fatalf("first event after overflow = %#v, want %q", resync, resyncEventName)
	}
	var payload struct {
		Missed uint64 `json:"missed"`
	}
	if err := json.Unmarshal(resync.data, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Missed != 2 {
		t.Fatalf("missed count = %d, want 2", payload.Missed)
	}
	latest, ok := subscriber.next()
	if !ok || latest.name != "latest" {
		t.Fatalf("latest event was not retained: %#v", latest)
	}
}

func TestEventHubCloseStopsActiveSSEHandler(t *testing.T) {
	hub := newEventHub()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/events", nil)
	done := make(chan struct{})
	go func() {
		defer close(done)
		hub.ServeHTTP(recorder, request)
	}()

	deadline := time.Now().Add(time.Second)
	for {
		hub.mu.RLock()
		count := len(hub.subscribers)
		hub.mu.RUnlock()
		if count == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("SSE subscriber was not registered")
		}
		time.Sleep(time.Millisecond)
	}

	hub.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("SSE handler did not stop when the hub closed")
	}
}

func TestWriteSSEPayloadPropagatesWriteFailure(t *testing.T) {
	writer := &failingSSEWriter{header: make(http.Header)}
	err := writeSSEPayload(writer, http.NewResponseController(writer), []byte("data: null\n\n"))
	if !errors.Is(err, errSSEWrite) {
		t.Fatalf("write error = %v", err)
	}
}
