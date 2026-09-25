package ai

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

type Turn struct {
	Side string `json:"side"`
	Name string `json:"name"`
	Text string `json:"text"`
}

type Debate struct {
	Prompt string
	NameA  string
	NameB  string
	Turns  []Turn
	VotesA int
	VotesB int
}

type Verdict struct {
	Winner   string `json:"winner"`
	ScoreA   int    `json:"score_a"`
	Headline string `json:"headline"`
	Reason   string `json:"reason"`
	RoastA   string `json:"roast_a"`
	RoastB   string `json:"roast_b"`
}

type Brain interface {
	Moderate(ctx context.Context, text string) (bool, string)
	Argue(ctx context.Context, d Debate, side string) (string, error)
	Judge(ctx context.Context, d Debate) (Verdict, error)
}

func SideLabel(side string) string {
	if side == "a" {
		return "AGREE"
	}
	return "DISAGREE"
}

func (d Debate) Transcript() string {
	var b strings.Builder
	b.WriteString("Hot take: \"" + d.Prompt + "\"\n")
	b.WriteString("A (" + d.NameA + ") argues AGREE. B (" + d.NameB + ") argues DISAGREE.\n\n")
	for i, t := range d.Turns {
		text := t.Text
		if text == "" {
			text = "(ran out of time and said nothing)"
		}
		b.WriteString(strings.ToUpper(t.Side) + " turn " + itoa(i/2+1) + ": " + text + "\n")
	}
	return b.String()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var s []byte
	for n > 0 {
		s = append([]byte{byte('0' + n%10)}, s...)
		n /= 10
	}
	return string(s)
}

var ErrBadJSON = errors.New("ai: no JSON object in response")

func extractJSON(s string, v any) error {
	start, end := strings.Index(s, "{"), strings.LastIndex(s, "}")
	if start < 0 || end <= start {
		return ErrBadJSON
	}
	return json.Unmarshal([]byte(s[start:end+1]), v)
}

func (v *Verdict) normalize(d Debate) {
	v.Winner = strings.ToLower(strings.TrimSpace(v.Winner))
	if v.Winner != "a" && v.Winner != "b" {
		if v.ScoreA >= 50 {
			v.Winner = "a"
		} else {
			v.Winner = "b"
		}
	}
	if v.ScoreA < 0 {
		v.ScoreA = 0
	}
	if v.ScoreA > 100 {
		v.ScoreA = 100
	}
	if v.Winner == "a" && v.ScoreA < 50 {
		v.ScoreA = 100 - v.ScoreA
	}
	if v.Winner == "b" && v.ScoreA > 50 {
		v.ScoreA = 100 - v.ScoreA
	}
	v.Headline = clip(v.Headline, 90)
	v.Reason = clip(v.Reason, 240)
	v.RoastA = clip(v.RoastA, 140)
	v.RoastB = clip(v.RoastB, 140)
}

func clip(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}
