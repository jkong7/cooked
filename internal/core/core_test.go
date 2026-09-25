package core

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/jkong7/cooked/internal/ai"
)

type fx struct {
	s   *Store
	ctx context.Context
	now time.Time
}

func setup(t *testing.T) *fx {
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	f := &fx{s: s, ctx: context.Background(), now: time.Date(2026, 9, 25, 20, 0, 0, 0, time.UTC)}
	s.Now = func() time.Time { return f.now }
	return f
}

func (f *fx) user(t *testing.T, name string) User {
	u, _, err := f.s.CreateUser(f.ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func (f *fx) live(t *testing.T, a, b User) Match {
	m, err := f.s.CreateMatch(f.ctx, NewMatch{Prompt: "5'10 is short.", PromptID: 1, A: Seat{UserID: &a.ID}, B: &Seat{UserID: &b.ID}})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func (f *fx) playOut(t *testing.T, m Match) Match {
	var err error
	for i := range Turns {
		m, err = f.s.Post(f.ctx, m.Code, SideOfTurn(i), i, "point number "+string(rune('1'+i)))
		if err != nil {
			t.Fatalf("turn %d: %v", i, err)
		}
	}
	return m
}

func TestUsers(t *testing.T) {
	f := setup(t)
	u, token, err := f.s.CreateUser(f.ctx, "  Marcus  ")
	if err != nil || u.Name != "Marcus" || u.Tier != "NPC" || u.Rating != 1000 {
		t.Fatalf("user = %+v %v", u, err)
	}
	if back, err := f.s.UserByToken(f.ctx, token); err != nil || back.ID != u.ID {
		t.Fatalf("token lookup = %+v %v", back, err)
	}
	if _, _, err := f.s.CreateUser(f.ctx, "CookBot"); !errors.Is(err, ErrInvalid) {
		t.Fatal("bot name should be reserved")
	}
}

func TestTurnOrderAndVoting(t *testing.T) {
	f := setup(t)
	a, b, fan := f.user(t, "A"), f.user(t, "B"), f.user(t, "Fan")
	m := f.live(t, a, b)
	if m.Status != "live" || !m.Deadline.Equal(f.now.Add(TurnTime)) {
		t.Fatalf("new match = %+v", m)
	}
	if _, err := f.s.Post(f.ctx, m.Code, "b", 0, "jumping the gun"); !errors.Is(err, ErrNotYours) {
		t.Fatalf("out of turn: %v", err)
	}
	if _, err := f.s.Post(f.ctx, m.Code, "a", 0, "first"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.Post(f.ctx, m.Code, "a", 0, "double post"); !errors.Is(err, ErrNotYours) {
		t.Fatalf("replayed turn: %v", err)
	}
	if _, err := f.s.Vote(f.ctx, m.Code, a.ID, "a"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("self vote: %v", err)
	}
	f.s.Vote(f.ctx, m.Code, fan.ID, "a")
	got, _ := f.s.Vote(f.ctx, m.Code, fan.ID, "b")
	if got.CrowdA != 0 || got.CrowdB != 1 {
		t.Fatalf("vote switch: %+v", got)
	}
	for i := 1; i < Turns; i++ {
		got, _ = f.s.Post(f.ctx, m.Code, SideOfTurn(i), i, "x")
	}
	if got.Status != "voting" || len(got.Turns) != Turns || !got.Deadline.Equal(f.now.Add(VoteTime)) {
		t.Fatalf("after last turn: %+v", got)
	}
}

func TestDueAndJudging(t *testing.T) {
	f := setup(t)
	a, b := f.user(t, "A"), f.user(t, "B")
	m := f.live(t, a, b)
	if d, _ := f.s.Due(f.ctx); len(d) != 0 {
		t.Fatalf("nothing should be due yet: %+v", d)
	}
	f.now = f.now.Add(TurnTime)
	d, _ := f.s.Due(f.ctx)
	if len(d) != 1 || d[0].Code != m.Code || d[0].Turn != 0 {
		t.Fatalf("due = %+v", d)
	}
	f.playOut(t, m)
	f.now = f.now.Add(VoteTime)
	ok, _ := f.s.BeginJudging(f.ctx, m.Code)
	again, _ := f.s.BeginJudging(f.ctx, m.Code)
	if !ok || again {
		t.Fatalf("begin judging should succeed exactly once: %v %v", ok, again)
	}
	if codes, _ := f.s.Judging(f.ctx); len(codes) != 1 {
		t.Fatalf("judging = %v", codes)
	}
}

func TestFinishJudgeDecidesWithoutQuorum(t *testing.T) {
	f := setup(t)
	a, b, fan := f.user(t, "A"), f.user(t, "B"), f.user(t, "Fan")
	m := f.playOut(t, f.live(t, a, b))
	f.s.Vote(f.ctx, m.Code, fan.ID, "a")
	f.s.BeginJudging(f.ctx, m.Code)
	done, err := f.s.Finish(f.ctx, m.Code, ai.Verdict{Winner: "b", ScoreA: 30, Headline: "B cooked A"})
	if err != nil || done.Winner != "b" || done.DecidedBy != "judge" || done.CrowdA != 1 || done.Delta != 16 {
		t.Fatalf("finish = %+v %v", done, err)
	}
	ua, _ := f.s.User(f.ctx, a.ID)
	ub, _ := f.s.User(f.ctx, b.ID)
	if ua.Rating != 984 || ua.Losses != 1 || ub.Rating != 1016 || ub.Wins != 1 {
		t.Fatalf("ratings: a=%+v b=%+v", ua, ub)
	}
	if _, err := f.s.Finish(f.ctx, m.Code, ai.Verdict{Winner: "a"}); !errors.Is(err, ErrState) {
		t.Fatalf("double finish: %v", err)
	}
	board, _ := f.s.Leaderboard(f.ctx, 10)
	if len(board) != 2 || board[0].ID != b.ID {
		t.Fatalf("leaderboard = %+v", board)
	}
}

func TestCrowdOverridesJudge(t *testing.T) {
	if w, by := Decide(ai.Verdict{Winner: "b"}, 3, 1); w != "a" || by != "crowd" {
		t.Fatalf("crowd quorum: %s %s", w, by)
	}
	if w, by := Decide(ai.Verdict{Winner: "b"}, 2, 2); w != "b" || by != "judge" {
		t.Fatalf("tie goes to judge: %s %s", w, by)
	}
}

func TestBotMatchUsesSmallerK(t *testing.T) {
	f := setup(t)
	a := f.user(t, "A")
	m, _ := f.s.CreateMatch(f.ctx, NewMatch{Prompt: "p", A: Seat{UserID: &a.ID}, B: &Seat{Bot: true}})
	if m.B == nil || !m.B.Bot || m.B.Name != BotName {
		t.Fatalf("bot seat = %+v", m.B)
	}
	f.now = f.now.Add(time.Second)
	m, _ = f.s.Post(f.ctx, m.Code, "a", 0, "go")
	if !m.BotTurn() {
		t.Fatal("turn 1 should be the bot's")
	}
	m = f.finishFrom(t, m, 1)
	f.s.BeginJudging(f.ctx, m.Code)
	done, _ := f.s.Finish(f.ctx, m.Code, ai.Verdict{Winner: "a"})
	if done.Delta != 8 {
		t.Fatalf("bot match delta = %v", done.Delta)
	}
}

func (f *fx) finishFrom(t *testing.T, m Match, from int) Match {
	var err error
	for i := from; i < Turns; i++ {
		if m, err = f.s.Post(f.ctx, m.Code, SideOfTurn(i), i, "x"); err != nil {
			t.Fatal(err)
		}
	}
	return m
}

func TestChallengeAccept(t *testing.T) {
	f := setup(t)
	a, b := f.user(t, "A"), f.user(t, "B")
	m, _ := f.s.CreateMatch(f.ctx, NewMatch{Prompt: "custom take", A: Seat{UserID: &a.ID}, Private: true})
	if m.Status != "waiting" || m.B != nil {
		t.Fatalf("challenge = %+v", m)
	}
	if _, err := f.s.Accept(f.ctx, m.Code, a.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("self accept: %v", err)
	}
	got, err := f.s.Accept(f.ctx, m.Code, b.ID)
	if err != nil || got.Status != "live" || got.SideOf(b.ID) != "b" {
		t.Fatalf("accept = %+v %v", got, err)
	}
	if _, err := f.s.Accept(f.ctx, m.Code, b.ID); !errors.Is(err, ErrState) {
		t.Fatalf("double accept: %v", err)
	}
	live, _ := f.s.Live(f.ctx, 10)
	if len(live) != 0 {
		t.Fatal("private matches must not be listed")
	}
	f.now = f.now.Add(2 * time.Hour)
	stale, _ := f.s.CreateMatch(f.ctx, NewMatch{Prompt: "old", A: Seat{UserID: &a.ID}})
	f.now = f.now.Add(2 * time.Hour)
	if n, _ := f.s.ExpireWaiting(f.ctx, time.Hour); n != 1 {
		t.Fatalf("expired %d (stale %s)", n, stale.Code)
	}
}

func TestReport(t *testing.T) {
	f := setup(t)
	a, b := f.user(t, "A"), f.user(t, "B")
	m := f.live(t, a, b)
	if err := f.s.Report(f.ctx, m.Code, b.ID, "slurs"); err != nil {
		t.Fatal(err)
	}
	if err := f.s.Report(f.ctx, m.Code, b.ID, "again"); err != nil {
		t.Fatal("duplicate report should be ignored, not fail")
	}
}
