package elo

import (
	"math"
	"testing"
)

func TestUpdateIsZeroSum(t *testing.T) {
	w, l := Update(1000, 1000, K)
	if math.Abs((w+l)-2000) > 1e-9 || w != 1016 {
		t.Fatalf("even match: %v %v", w, l)
	}
	w2, _ := Update(900, 1300, K)
	if w2-900 <= 16 {
		t.Fatal("upset should pay more than an even win")
	}
}

func TestTiers(t *testing.T) {
	cases := map[float64]string{0: "Chud", 899: "Chud", 900: "NPC", 1000: "NPC", 1050: "Cooker", 1250: "Menace", 1400: "Goat"}
	for r, want := range cases {
		if got := TierOf(r).Name; got != want {
			t.Errorf("TierOf(%v) = %s, want %s", r, got, want)
		}
	}
}
