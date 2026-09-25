package ai

import (
	"context"
	"fmt"
	"hash/fnv"
	"regexp"
	"strings"
)

var blocked = regexp.MustCompile(`(?i)\b(n[i1!]gg(a|[e3]r)s?|f[a@4]gg?([o0]t)?s?|r[e3]t[a@4]rd([e3]d|s)?|k[i1!]k[e3]|sp[i1!]c|ch[i1!]nk|tr[a@4]nn(y|[i1!][e3]s)|kys|kill yourself|go die)\b`)
var contact = regexp.MustCompile(`(?i)(\b\d{3}[-.\s]?\d{3}[-.\s]?\d{4}\b|\b[\w.+-]+@[\w-]+\.[\w.]+\b)`)

type Local struct{}

func (Local) Moderate(_ context.Context, text string) (bool, string) {
	switch {
	case blocked.MatchString(text):
		return false, "slurs and self-harm stuff get you removed"
	case contact.MatchString(text):
		return false, "no phone numbers or emails"
	}
	return true, ""
}

var lines = map[string][]string{
	"a": {
		"Look at the evidence. Everyone who pretends otherwise is coping in public.",
		"I'll say it with my chest: this is obviously true and you know it.",
		"Name one person who disagrees with this and is winning at life. I'll wait.",
		"The fact that this makes people mad is the proof.",
		"You're arguing against reality and reality is undefeated.",
		"Every group chat already agrees with me, they're just scared to say it.",
	},
	"b": {
		"That's a take you only have when nobody's ever pushed back on you.",
		"This is the kind of opinion that sounds smart at 2am and dumb at noon.",
		"Correlation isn't causation, and this take is neither.",
		"Respectfully, this is cope wearing a confidence costume.",
		"If this were true we'd see it everywhere. We don't. Next.",
		"You picked the loudest side, not the right one.",
	},
}

func pick(seed string, list []string) string {
	h := fnv.New32a()
	h.Write([]byte(seed))
	return list[int(h.Sum32())%len(list)]
}

func (Local) Argue(_ context.Context, d Debate, side string) (string, error) {
	used := map[string]bool{}
	for _, t := range d.Turns {
		used[t.Text] = true
	}
	h := fnv.New32a()
	h.Write([]byte(d.Prompt + side))
	list := lines[side]
	start := int(h.Sum32()) + len(d.Turns)
	for i := range list {
		if line := list[(start+i)%len(list)]; !used[line] {
			return line, nil
		}
	}
	return list[start%len(list)], nil
}

func effort(d Debate, side string) float64 {
	score := 0.0
	for _, t := range d.Turns {
		if t.Side != side {
			continue
		}
		n := len(strings.Fields(t.Text))
		switch {
		case n == 0:
			score -= 3
		case n < 6:
			score += 1
		default:
			score += 2 + min(float64(n)/20, 2)
		}
	}
	return score
}

func (Local) Judge(_ context.Context, d Debate) (Verdict, error) {
	a, b := effort(d, "a"), effort(d, "b")
	total := a + b
	scoreA := 50
	if total > 0 && a >= 0 && b >= 0 {
		scoreA = int(100 * a / total)
	} else if a != b {
		if a > b {
			scoreA = 70
		} else {
			scoreA = 30
		}
	}
	if scoreA == 50 {
		scoreA = 51
	}
	v := Verdict{ScoreA: scoreA}
	winName, loseName := d.NameA, d.NameB
	if scoreA < 50 {
		winName, loseName = d.NameB, d.NameA
	}
	v.Headline = fmt.Sprintf("%s cooked %s", winName, loseName)
	v.Reason = "Showed up every round and actually made points."
	v.RoastA = pick(d.NameA+"a", []string{"Argued like a LinkedIn post.", "Confidence of a Chad, evidence of an NPC.", "All volume, no receipts."})
	v.RoastB = pick(d.NameB+"b", []string{"Debated like the group chat was watching. It was.", "Brought vibes to a fact fight.", "Said a lot, proved nothing."})
	v.normalize(d)
	return v, nil
}
