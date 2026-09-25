package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/anthropics/anthropic-sdk-go/option"
)

var debate = Debate{
	Prompt: "5'10 is short.",
	NameA:  "Marcus",
	NameB:  "Priya",
	Turns: []Turn{
		{Side: "a", Text: "Every dating app filter says so."},
		{Side: "b", Text: "The average American man is 5'9, so 5'10 is literally above average and you know it."},
		{Side: "a", Text: ""},
		{Side: "b", Text: "He went silent because the math won."},
	},
}

func TestLocalModerate(t *testing.T) {
	l := Local{}
	for _, bad := range []string{"you're a f4ggot", "just kys", "text me 312-555-1234", "email me a@b.com"} {
		if ok, _ := l.Moderate(context.Background(), bad); ok {
			t.Errorf("allowed %q", bad)
		}
	}
	for _, fine := range []string{"this take is trash and so are you", "5'10 is tall in Europe", "damn that's cold"} {
		if ok, why := l.Moderate(context.Background(), fine); !ok {
			t.Errorf("blocked %q: %s", fine, why)
		}
	}
}

func TestLocalJudgePunishesSilence(t *testing.T) {
	v, _ := Local{}.Judge(context.Background(), debate)
	if v.Winner != "b" || v.ScoreA >= 50 || !strings.Contains(v.Headline, "Priya") {
		t.Fatalf("verdict = %+v", v)
	}
}

func TestNormalize(t *testing.T) {
	v := Verdict{Winner: " A ", ScoreA: 20}
	v.normalize(debate)
	if v.Winner != "a" || v.ScoreA != 80 {
		t.Fatalf("normalize = %+v", v)
	}
	v = Verdict{Winner: "", ScoreA: 150}
	v.normalize(debate)
	if v.Winner != "a" || v.ScoreA != 100 {
		t.Fatalf("normalize clamp = %+v", v)
	}
}

func TestExtractJSON(t *testing.T) {
	var v Verdict
	if err := extractJSON("Sure!\n```json\n{\"winner\":\"b\",\"score_a\":30}\n```", &v); err != nil || v.Winner != "b" {
		t.Fatalf("extract = %+v %v", v, err)
	}
	if extractJSON("no json here", &v) == nil {
		t.Fatal("expected error")
	}
}

type fakeAPI struct {
	mu     sync.Mutex
	bodies []map[string]any
	betas  []string
	reply  string
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	json.NewDecoder(r.Body).Decode(&body)
	f.mu.Lock()
	f.bodies = append(f.bodies, body)
	f.betas = append(f.betas, r.Header.Get("anthropic-beta"))
	reply := f.reply
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"id": "msg_1", "type": "message", "role": "assistant", "model": body["model"],
		"content":     []map[string]any{{"type": "text", "text": reply}},
		"stop_reason": "end_turn",
		"usage":       map[string]any{"input_tokens": 10, "output_tokens": 10},
	})
}

func TestClaudeJudgeAndFallback(t *testing.T) {
	api := &fakeAPI{reply: `{"winner":"b","score_a":35,"headline":"Priya cooked Marcus","reason":"Math.","roast_a":"Went mute.","roast_b":"Brought a calculator."}`}
	srv := httptest.NewServer(api)
	defer srv.Close()
	c := NewClaude("test-key", "", option.WithBaseURL(srv.URL))

	v, err := c.Judge(context.Background(), debate)
	if err != nil || v.Winner != "b" || v.ScoreA != 35 || v.RoastB != "Brought a calculator." {
		t.Fatalf("judge = %+v %v", v, err)
	}
	body := api.bodies[0]
	if body["model"] != DefaultModel || body["fallbacks"] != "default" || !strings.Contains(api.betas[0], "server-side-fallback-2026-07-01") {
		t.Fatalf("request = %v betas=%v", body, api.betas)
	}

	api.reply = "I refuse to use JSON today"
	v, err = c.Judge(context.Background(), debate)
	if err != nil || v.Winner != "b" || !strings.Contains(v.Headline, "Priya") {
		t.Fatalf("fallback judge = %+v %v", v, err)
	}

	api.reply = `{"allow": false, "reason": "threat"}`
	if ok, why := c.Moderate(context.Background(), "some text"); ok || why != "threat" {
		t.Fatalf("moderate = %v %q", ok, why)
	}
	api.reply = "Height is a number, confidence is a vibe."
	if arg, _ := c.Argue(context.Background(), debate, "a"); arg != api.reply {
		t.Fatalf("argue = %q", arg)
	}
}

func TestClaudeDownFallsBackToLocal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"type":"error","error":{"type":"api_error","message":"down"}}`, 500)
	}))
	defer srv.Close()
	c := NewClaude("k", "", option.WithBaseURL(srv.URL), option.WithMaxRetries(0))
	if ok, _ := c.Moderate(context.Background(), "normal trash talk"); !ok {
		t.Fatal("moderation should fail open when the API is down")
	}
	if arg, err := c.Argue(context.Background(), debate, "b"); err != nil || arg == "" {
		t.Fatalf("argue fallback = %q %v", arg, err)
	}
}

func TestLocalArgueDoesNotRepeat(t *testing.T) {
	d := Debate{Prompt: "p"}
	seen := map[string]bool{}
	for i := range 6 {
		side := "a"
		if i%2 == 1 {
			side = "b"
		}
		line, _ := Local{}.Argue(context.Background(), d, side)
		if seen[line] {
			t.Fatalf("repeated line %q", line)
		}
		seen[line] = true
		d.Turns = append(d.Turns, Turn{Side: side, Text: line})
	}
}
