package game

import (
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/jkong7/cooked/internal/ai"
	"github.com/jkong7/cooked/internal/core"
	"github.com/jkong7/cooked/internal/hub"
	"github.com/jkong7/cooked/internal/prompts"
)

var ErrBlocked = errors.New("blocked by moderation")

type Engine struct {
	Store     *core.Store
	Brain     ai.Brain
	Hub       *hub.Hub
	Log       *slog.Logger
	QueueWait time.Duration
	BotDelay  func() time.Duration

	mu       sync.Mutex
	waiting  map[int]map[string][]*ticket
	inflight map[string]bool
}

type ticket struct {
	userID int64
	match  chan string
}

func New(store *core.Store, brain ai.Brain, h *hub.Hub, log *slog.Logger) *Engine {
	return &Engine{
		Store: store, Brain: brain, Hub: h, Log: log, QueueWait: 12 * time.Second,
		BotDelay: func() time.Duration { return time.Duration(2000+rand.IntN(2500)) * time.Millisecond },
		waiting:  map[int]map[string][]*ticket{}, inflight: map[string]bool{},
	}
}

func Topic(code string) string { return "m:" + code }

const Lobby = "lobby"

func other(side string) string {
	if side == "a" {
		return "b"
	}
	return "a"
}

func (e *Engine) publish(m core.Match, typ string) {
	e.Hub.Publish(Topic(m.Code), typ, m)
	if !m.Private {
		lite := m
		lite.Turns = nil
		e.Hub.Publish(Lobby, typ, lite)
	}
}

func (e *Engine) Queue(ctx context.Context, userID int64, promptID int, side string) (string, error) {
	p, ok := prompts.ByID(promptID)
	if !ok || (side != "a" && side != "b") {
		return "", core.ErrInvalid
	}
	e.mu.Lock()
	if e.waiting[promptID] == nil {
		e.waiting[promptID] = map[string][]*ticket{}
	}
	opp := e.waiting[promptID][other(side)]
	for i, t := range opp {
		if t.userID == userID {
			continue
		}
		e.waiting[promptID][other(side)] = append(opp[:i:i], opp[i+1:]...)
		e.mu.Unlock()
		a, b := userID, t.userID
		if side == "b" {
			a, b = b, a
		}
		m, err := e.Store.CreateMatch(ctx, core.NewMatch{Prompt: p.Text, PromptID: p.ID, A: core.Seat{UserID: &a},
			B: &core.Seat{UserID: &b}})
		if err != nil {
			close(t.match)
			return "", err
		}
		t.match <- m.Code
		e.publish(m, "start")
		return m.Code, nil
	}
	t := &ticket{userID: userID, match: make(chan string, 1)}
	e.waiting[promptID][side] = append(e.waiting[promptID][side], t)
	e.mu.Unlock()

	timer := time.NewTimer(e.QueueWait)
	defer timer.Stop()
	select {
	case code, ok := <-t.match:
		if !ok {
			return "", core.ErrInvalid
		}
		return code, nil
	case <-ctx.Done():
		if e.dequeue(promptID, side, t) {
			return "", ctx.Err()
		}
		return <-t.match, nil
	case <-timer.C:
	}
	if !e.dequeue(promptID, side, t) {
		return <-t.match, nil
	}
	human := core.Seat{UserID: &userID}
	bot := core.Seat{Bot: true}
	nm := core.NewMatch{Prompt: p.Text, PromptID: p.ID, A: human, B: &bot}
	if side == "b" {
		nm.A, nm.B = bot, &human
	}
	m, err := e.Store.CreateMatch(ctx, nm)
	if err != nil {
		return "", err
	}
	e.publish(m, "start")
	e.kick(m)
	return m.Code, nil
}

func (e *Engine) dequeue(promptID int, side string, t *ticket) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	list := e.waiting[promptID][side]
	for i, x := range list {
		if x == t {
			e.waiting[promptID][side] = append(list[:i:i], list[i+1:]...)
			return true
		}
	}
	return false
}

func (e *Engine) Challenge(ctx context.Context, userID int64, prompt string) (core.Match, error) {
	text, err := core.Clean(prompt, 140)
	if err != nil {
		return core.Match{}, err
	}
	if ok, _ := e.Brain.Moderate(ctx, text); !ok {
		return core.Match{}, ErrBlocked
	}
	return e.Store.CreateMatch(ctx, core.NewMatch{Prompt: text, A: core.Seat{UserID: &userID}, Private: true})
}

func (e *Engine) Accept(ctx context.Context, code string, userID int64) (core.Match, error) {
	m, err := e.Store.Accept(ctx, code, userID)
	if err != nil {
		return m, err
	}
	e.publish(m, "start")
	return m, nil
}

func (e *Engine) Say(ctx context.Context, code string, userID int64, text string) (core.Match, error) {
	m, err := e.Store.Match(ctx, code)
	if err != nil {
		return m, err
	}
	side := m.SideOf(userID)
	if side == "" {
		return m, core.ErrForbidden
	}
	text, err = core.Clean(text, core.MaxLen)
	if err != nil {
		return m, err
	}
	if m.Status != "live" || core.SideOfTurn(m.Turn) != side {
		return m, core.ErrNotYours
	}
	if ok, _ := e.Brain.Moderate(ctx, text); !ok {
		return m, ErrBlocked
	}
	m, err = e.Store.Post(ctx, code, side, m.Turn, text)
	if err != nil {
		return m, err
	}
	e.after(m)
	return m, nil
}

func (e *Engine) Vote(ctx context.Context, code string, userID int64, side string) (core.Match, error) {
	m, err := e.Store.Vote(ctx, code, userID, side)
	if err != nil {
		return m, err
	}
	e.Hub.Publish(Topic(code), "vote", map[string]int{"crowd_a": m.CrowdA, "crowd_b": m.CrowdB})
	return m, nil
}

func (e *Engine) after(m core.Match) {
	e.publish(m, "turn")
	e.kick(m)
}

func (e *Engine) claim(key string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.inflight[key] {
		return false
	}
	e.inflight[key] = true
	return true
}

func (e *Engine) release(key string) {
	e.mu.Lock()
	delete(e.inflight, key)
	e.mu.Unlock()
}

func (e *Engine) kick(m core.Match) {
	if !m.BotTurn() {
		return
	}
	key := m.Code + ":bot"
	if !e.claim(key) {
		return
	}
	go func() {
		defer e.release(key)
		time.Sleep(e.BotDelay())
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		side := core.SideOfTurn(m.Turn)
		text, err := e.Brain.Argue(ctx, m.Debate(), side)
		if err != nil || text == "" {
			text, _ = ai.Local{}.Argue(ctx, m.Debate(), side)
		}
		next, err := e.Store.Post(ctx, m.Code, side, m.Turn, text)
		if err != nil {
			if !errors.Is(err, core.ErrNotYours) && !errors.Is(err, core.ErrState) {
				e.Log.Warn("bot turn", "match", m.Code, "err", err)
			}
			return
		}
		e.after(next)
	}()
}

func (e *Engine) Tick(ctx context.Context) {
	due, err := e.Store.Due(ctx)
	if err != nil {
		e.Log.Warn("due", "err", err)
		return
	}
	for _, d := range due {
		switch d.Status {
		case "live":
			m, err := e.Store.Post(ctx, d.Code, core.SideOfTurn(d.Turn), d.Turn, "")
			if err == nil {
				e.publish(m, "choke")
				e.kick(m)
			}
		case "voting":
			if ok, err := e.Store.BeginJudging(ctx, d.Code); err == nil && ok {
				e.judge(d.Code)
			}
		}
	}
}

func (e *Engine) judge(code string) {
	key := code + ":judge"
	if !e.claim(key) {
		return
	}
	go func() {
		defer e.release(key)
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		m, err := e.Store.Match(ctx, code)
		if err != nil {
			e.Log.Warn("judge load", "match", code, "err", err)
			return
		}
		e.publish(m, "judging")
		v, err := e.Brain.Judge(ctx, m.Debate())
		if err != nil {
			v, _ = ai.Local{}.Judge(ctx, m.Debate())
		}
		done, err := e.Store.Finish(ctx, code, v)
		if err != nil {
			e.Log.Warn("finish", "match", code, "err", err)
			return
		}
		e.publish(done, "verdict")
	}()
}

func (e *Engine) Recover(ctx context.Context) {
	codes, err := e.Store.Judging(ctx)
	if err != nil {
		e.Log.Warn("recover", "err", err)
		return
	}
	for _, c := range codes {
		e.judge(c)
	}
	live, _ := e.Store.Live(ctx, 500)
	for _, m := range live {
		if full, err := e.Store.Match(ctx, m.Code); err == nil {
			e.kick(full)
		}
	}
}

func (e *Engine) Run(ctx context.Context) {
	e.Recover(ctx)
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()
	sweep := time.NewTicker(10 * time.Minute)
	defer sweep.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			e.Tick(ctx)
		case <-sweep.C:
			e.Store.ExpireWaiting(ctx, 24*time.Hour)
		}
	}
}
