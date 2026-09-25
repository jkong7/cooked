package hub

import (
	"encoding/json"
	"sync"
)

type Event struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

type Hub struct {
	mu   sync.Mutex
	subs map[string]map[chan []byte]struct{}
}

func New() *Hub {
	return &Hub{subs: map[string]map[chan []byte]struct{}{}}
}

func (h *Hub) Subscribe(topic string) (<-chan []byte, func()) {
	ch := make(chan []byte, 32)
	h.mu.Lock()
	if h.subs[topic] == nil {
		h.subs[topic] = map[chan []byte]struct{}{}
	}
	h.subs[topic][ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		if _, ok := h.subs[topic][ch]; ok {
			delete(h.subs[topic], ch)
			close(ch)
			if len(h.subs[topic]) == 0 {
				delete(h.subs, topic)
			}
		}
		h.mu.Unlock()
	}
}

func (h *Hub) Publish(topic, typ string, data any) {
	msg, err := json.Marshal(Event{Type: typ, Data: data})
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs[topic] {
		select {
		case ch <- msg:
		default:
		}
	}
}

func (h *Hub) Watching(topic string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs[topic])
}
