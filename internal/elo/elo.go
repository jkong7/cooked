package elo

import "math"

const (
	Start = 1000.0
	K     = 32.0
	BotK  = 16.0
)

func Expected(a, b float64) float64 {
	return 1 / (1 + math.Pow(10, (b-a)/400))
}

func Update(winner, loser, k float64) (float64, float64) {
	e := Expected(winner, loser)
	delta := k * (1 - e)
	return winner + delta, loser - delta
}

type Tier struct {
	Name  string  `json:"name"`
	Floor float64 `json:"floor"`
}

var Tiers = []Tier{
	{"Chud", 0},
	{"NPC", 900},
	{"Cooker", 1050},
	{"Menace", 1200},
	{"Goat", 1350},
}

func TierOf(r float64) Tier {
	t := Tiers[0]
	for _, x := range Tiers {
		if r >= x.Floor {
			t = x
		}
	}
	return t
}
