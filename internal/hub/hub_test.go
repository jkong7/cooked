package hub

import (
	"encoding/json"
	"testing"
)

func TestTopics(t *testing.T) {
	h := New()
	a, unA := h.Subscribe("m:abc")
	b, unB := h.Subscribe("lobby")
	defer unB()
	h.Publish("m:abc", "turn", 1)
	var e Event
	json.Unmarshal(<-a, &e)
	if e.Type != "turn" || h.Watching("m:abc") != 1 {
		t.Fatalf("event %+v", e)
	}
	select {
	case <-b:
		t.Fatal("lobby got a match event")
	default:
	}
	unA()
	unA()
	for range 100 {
		h.Publish("lobby", "x", nil)
	}
	if h.Watching("m:abc") != 0 {
		t.Fatal("unsubscribe failed")
	}
}
