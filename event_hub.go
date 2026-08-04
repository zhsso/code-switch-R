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

const (
	eventSubscriberBuffer = 32
	resyncEventName       = "system:resync"
)

type eventSubscriber struct {
	mu       sync.Mutex
	queue    []serverEvent
	wake     chan struct{}
	capacity int
	missed   uint64
}

func newEventSubscriber(capacity int) *eventSubscriber {
	if capacity < 2 {
		capacity = 2
	}
	return &eventSubscriber{
		queue:    make([]serverEvent, 0, capacity),
		wake:     make(chan struct{}, 1),
		capacity: capacity,
	}
}

func (s *eventSubscriber) enqueue(event serverEvent) {
	s.mu.Lock()
	if len(s.queue) >= s.capacity {
		dropped := len(s.queue)
		s.queue = s.queue[:0]
		s.missed += uint64(dropped)
		data, _ := json.Marshal(map[string]uint64{"missed": s.missed})
		s.queue = append(s.queue, serverEvent{name: resyncEventName, data: data})
	}
	s.queue = append(s.queue, event)
	s.mu.Unlock()
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *eventSubscriber) next() (serverEvent, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.queue) == 0 {
		return serverEvent{}, false
	}
	event := s.queue[0]
	s.queue[0] = serverEvent{}
	s.queue = s.queue[1:]
	if len(s.queue) == 0 {
		s.queue = s.queue[:0]
	}
	return event, true
}

type eventHub struct {
	mu          sync.RWMutex
	subscribers map[*eventSubscriber]struct{}
	done        chan struct{}
	closeOnce   sync.Once
}

func newEventHub() *eventHub {
	return &eventHub{
		subscribers: make(map[*eventSubscriber]struct{}),
		done:        make(chan struct{}),
	}
}

func (h *eventHub) Close() {
	h.closeOnce.Do(func() {
		close(h.done)
	})
}

func (h *eventHub) Emit(name string, value any) {
	select {
	case <-h.done:
		return
	default:
	}
	data, err := json.Marshal(value)
	if err != nil {
		return
	}
	event := serverEvent{name: name, data: data}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for subscriber := range h.subscribers {
		subscriber.enqueue(event)
	}
}

func (h *eventHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	select {
	case <-h.done:
		http.Error(w, "event stream is shutting down", http.StatusServiceUnavailable)
		return
	default:
	}
	_, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unavailable", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	subscriber := newEventSubscriber(eventSubscriberBuffer)
	h.mu.Lock()
	h.subscribers[subscriber] = struct{}{}
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.subscribers, subscriber)
		h.mu.Unlock()
	}()

	controller := http.NewResponseController(w)
	if err := writeSSEPayload(w, controller, []byte(": connected\n\n")); err != nil {
		return
	}
	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-h.done:
			return
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			if err := writeSSEPayload(w, controller, []byte(": heartbeat\n\n")); err != nil {
				return
			}
		case <-subscriber.wake:
			for {
				event, ok := subscriber.next()
				if !ok {
					break
				}
				frame := make([]byte, 0, len(event.name)+len(event.data)+16)
				frame = append(frame, "event: "...)
				frame = append(frame, event.name...)
				frame = append(frame, "\ndata: "...)
				frame = append(frame, event.data...)
				frame = append(frame, '\n', '\n')
				if err := writeSSEPayload(w, controller, frame); err != nil {
					return
				}
			}
		}
	}
}

func writeSSEPayload(w http.ResponseWriter, controller *http.ResponseController, payload []byte) error {
	if _, err := w.Write(payload); err != nil {
		return err
	}
	return controller.Flush()
}
