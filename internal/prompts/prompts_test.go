package prompts

import (
	"testing"
	"time"
)

func TestFeaturedStableWithinWindow(t *testing.T) {
	base := time.Unix(1_790_000_400, 0)
	a, b := Featured(base), Featured(base.Add(Window-time.Second))
	if a != b {
		t.Fatalf("featured changed inside a window: %v vs %v", a, b)
	}
	if next := NextRotation(base); !next.After(base) || next.Sub(base) > Window {
		t.Fatalf("next rotation %v", next)
	}
}

func TestIDsUnique(t *testing.T) {
	seen := map[int]bool{}
	for _, p := range All {
		if seen[p.ID] || p.Text == "" {
			t.Fatalf("bad prompt %+v", p)
		}
		seen[p.ID] = true
	}
	if _, ok := ByID(1); !ok {
		t.Fatal("ByID(1) missing")
	}
}
