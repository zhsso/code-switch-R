package main

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

type serverEvent struct {
	name string
	data []byte
}

type eventHub struct {
	mu          sync.RWMutex
	subscribers map[chan serverEvent]struct{}
}

func newEventHub() *eventHub {
	return &eventHub{subscribers: make(map[chan serverEvent]struct{})}
}

func (h *eventHub) Emit(name string, value any) {
	data, err := json.Marshal(value)
	if err != nil {
		return
	}
	event := serverEvent{name: name, data: data}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for subscriber := range h.subscribers {
		select {
		case subscriber <- event:
		default:
			// A slow browser must not block relay request handling.
		}
	}
}

func (h *eventHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unavailable", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	subscriber := make(chan serverEvent, 32)
	h.mu.Lock()
	h.subscribers[subscriber] = struct{}{}
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.subscribers, subscriber)
		h.mu.Unlock()
	}()

	_, _ = w.Write([]byte(": connected\n\n"))
	flusher.Flush()
	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			_, _ = w.Write([]byte(": heartbeat\n\n"))
			flusher.Flush()
		case event := <-subscriber:
			_, _ = w.Write([]byte("event: " + event.name + "\n"))
			_, _ = w.Write([]byte("data: "))
			_, _ = w.Write(event.data)
			_, _ = w.Write([]byte("\n\n"))
			flusher.Flush()
		}
	}
}
