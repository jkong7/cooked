package game

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jkong7/cooked/internal/ai"
	"github.com/jkong7/cooked/internal/core"
	"github.com/jkong7/cooked/internal/hub"
)

type fakeBrain struct {
	judged atomic.Int32
}

func (*fakeBrain) Moderate(_ context.Context, text string) (bool, string) {
	return text != "banned words", "nope"
}
func (*fakeBrain) Argue(context.Context, ai.Debate, string) (string, error) { return "bot point", nil }
func (f *fakeBrain) Judge(context.Context, ai.Debate) (ai.Verdict, error) {
	f.judged.Add(1)
	return ai.Verdict{Winner: "a", ScoreA: 70, Headline: "A cooked B"}, nil
}

type env struct {
	e     *Engine
	s     *core.Store
	brain *fakeBrain
	now   time.Time
}

func setup(t *testing.T) *env {
	s, err := core.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	v := &env{s: s, brain: &fakeBrain{}, now: time.Now()}
	s.Now = func() time.Time { return v.now }
	v.e = New(s, v.brain, hub.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	v.e.QueueWait = 150 * time.Millisecond
	v.e.BotDelay = func() time.Duration { return 0 }
	return v
}

func (v *env) user(t *testing.T, name string) core.User {
	u, _, err := v.s.CreateUser(context.Background(), name)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestQueuePairsOppositeSides(t *testing.T) {
	v := setup(t)
	a, b := v.user(t, "A"), v.user(t, "B")
	v.e.QueueWait = 2 * time.Second
	got := make(chan string, 1)
	go func() {
		code, _ := v.e.Queue(context.Background(), a.ID, 1, "a")
		got <- code
	}()
	time.Sleep(50 * time.Millisecond)
	code, err := v.e.Queue(context.Background(), b.ID, 1, "b")
	if err != nil || code == "" || <-got != code {
		t.Fatalf("pairing failed: %q %v", code, err)
	}
	m, _ := v.s.Match(context.Background(), code)
	if m.SideOf(a.ID) != "a" || m.SideOf(b.ID) != "b" || m.B.Bot {
		t.Fatalf("seats wrong: %+v", m)
	}
}

func TestQueueFallsBackToBotAndBotPlays(t *testing.T) {
	v := setup(t)
	a := v.user(t, "A")
	code, err := v.e.Queue(context.Background(), a.ID, 1, "b")
	if err != nil {
		t.Fatal(err)
	}
	m, _ := v.s.Match(context.Background(), code)
	if !m.A.Bot || m.SideOf(a.ID) != "b" {
		t.Fatalf("bot should take side a: %+v", m)
	}
	waitFor(t, "bot opening", func() bool {
		m, _ = v.s.Match(context.Background(), code)
		return len(m.Turns) == 1 && m.Turns[0].Text == "bot point"
	})
	if _, err := v.e.Say(context.Background(), code, a.ID, "banned words"); !errors.Is(err, ErrBlocked) {
		t.Fatalf("moderation: %v", err)
	}
	if _, err := v.e.Say(context.Background(), code, a.ID, "my rebuttal"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "bot reply", func() bool {
		m, _ = v.s.Match(context.Background(), code)
		return len(m.Turns) == 3
	})
}

func TestTickChokesJudgesOnce(t *testing.T) {
	v := setup(t)
	a, b := v.user(t, "A"), v.user(t, "B")
	ctx := context.Background()
	m, _ := v.s.CreateMatch(ctx, core.NewMatch{Prompt: "p", A: core.Seat{UserID: &a.ID}, B: &core.Seat{UserID: &b.ID}})
	for i := range core.Turns {
		v.now = v.now.Add(core.TurnTime)
		v.e.Tick(ctx)
		m, _ = v.s.Match(ctx, m.Code)
		if len(m.Turns) != i+1 || m.Turns[i].Text != "" {
			t.Fatalf("tick %d: turns=%+v", i, m.Turns)
		}
	}
	if m.Status != "voting" {
		t.Fatalf("status = %s", m.Status)
	}
	v.now = v.now.Add(core.VoteTime)
	v.e.Tick(ctx)
	v.e.Tick(ctx)
	waitFor(t, "verdict", func() bool {
		m, _ = v.s.Match(ctx, m.Code)
		return m.Status == "done"
	})
	if v.brain.judged.Load() != 1 || m.Winner != "a" || m.Headline != "A cooked B" {
		t.Fatalf("judged %d times, match %+v", v.brain.judged.Load(), m)
	}
}

func TestSayRejectsOutOfTurnAndOutsiders(t *testing.T) {
	v := setup(t)
	a, b, c := v.user(t, "A"), v.user(t, "B"), v.user(t, "C")
	ctx := context.Background()
	m, _ := v.s.CreateMatch(ctx, core.NewMatch{Prompt: "p", A: core.Seat{UserID: &a.ID}, B: &core.Seat{UserID: &b.ID}})
	if _, err := v.e.Say(ctx, m.Code, b.ID, "me first"); !errors.Is(err, core.ErrNotYours) {
		t.Fatalf("out of turn: %v", err)
	}
	if _, err := v.e.Say(ctx, m.Code, c.ID, "hi"); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("outsider: %v", err)
	}
}

func TestChallengeFlow(t *testing.T) {
	v := setup(t)
	a, b := v.user(t, "A"), v.user(t, "B")
	ctx := context.Background()
	if _, err := v.e.Challenge(ctx, a.ID, "banned words"); !errors.Is(err, ErrBlocked) {
		t.Fatalf("moderated challenge: %v", err)
	}
	m, err := v.e.Challenge(ctx, a.ID, "Marcus can't cook")
	if err != nil || !m.Private || m.Status != "waiting" {
		t.Fatalf("challenge = %+v %v", m, err)
	}
	if m, err = v.e.Accept(ctx, m.Code, b.ID); err != nil || m.Status != "live" {
		t.Fatalf("accept = %+v %v", m, err)
	}
}

func TestQueueCancelledContextLeavesNoGhost(t *testing.T) {
	v := setup(t)
	a, b := v.user(t, "A"), v.user(t, "B")
	v.e.QueueWait = 5 * time.Second
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := v.e.Queue(ctx, a.ID, 2, "a")
		done <- err
	}()
	time.Sleep(30 * time.Millisecond)
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled queue: %v", err)
	}
	v.e.QueueWait = 100 * time.Millisecond
	code, _ := v.e.Queue(context.Background(), b.ID, 2, "b")
	m, _ := v.s.Match(context.Background(), code)
	if !m.A.Bot {
		t.Fatal("cancelled ticket should not have been matched")
	}
}
