package web

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jkong7/cooked/internal/ai"
	"github.com/jkong7/cooked/internal/core"
	"github.com/jkong7/cooked/internal/game"
	"github.com/jkong7/cooked/internal/hub"
)

type client struct {
	t    *testing.T
	base string
	c    *http.Client
}

func newServer(t *testing.T) (*httptest.Server, *game.Engine) {
	st, err := core.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := hub.New()
	e := game.New(st, ai.Local{}, h, log)
	e.QueueWait = 100 * time.Millisecond
	e.BotDelay = func() time.Duration { return 0 }
	srv := httptest.NewServer((&Server{Store: st, Engine: e, Hub: h, Log: log}).Routes())
	t.Cleanup(srv.Close)
	return srv, e
}

func newClient(t *testing.T, srv *httptest.Server) *client {
	jar, _ := cookiejar.New(nil)
	return &client{t: t, base: srv.URL, c: &http.Client{Jar: jar}}
}

func (c *client) do(method, path string, body, out any) int {
	c.t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, c.base+path, r)
	resp, err := c.c.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil {
		json.NewDecoder(resp.Body).Decode(out)
	}
	return resp.StatusCode
}

func TestSignupRequiresAdult(t *testing.T) {
	srv, _ := newServer(t)
	c := newClient(t, srv)
	if code := c.do("POST", "/api/me", map[string]any{"name": "Kid"}, nil); code != 403 {
		t.Fatalf("no adult flag = %d", code)
	}
	var u core.User
	if code := c.do("POST", "/api/me", map[string]any{"name": "Marcus", "adult": true}, &u); code != 201 || u.Tier != "NPC" {
		t.Fatalf("signup = %d %+v", code, u)
	}
	var me *core.User
	c.do("GET", "/api/me", nil, &me)
	if me == nil || me.ID != u.ID {
		t.Fatalf("me = %+v", me)
	}
}

func TestBotMatchOverHTTP(t *testing.T) {
	srv, e := newServer(t)
	player, fan := newClient(t, srv), newClient(t, srv)
	player.do("POST", "/api/me", map[string]any{"name": "Marcus", "adult": true}, nil)
	fan.do("POST", "/api/me", map[string]any{"name": "Fan", "adult": true}, nil)

	var q struct{ Code string }
	if code := player.do("POST", "/api/queue", map[string]any{"prompt_id": 1, "side": "a"}, &q); code != 200 || q.Code == "" {
		t.Fatalf("queue = %d %+v", code, q)
	}
	var m struct {
		core.Match
		You string `json:"you"`
	}
	player.do("GET", "/api/matches/"+q.Code, nil, &m)
	if m.You != "a" || m.B == nil || !m.B.Bot || m.Status != "live" {
		t.Fatalf("match = %+v", m)
	}
	if code := player.do("POST", "/api/matches/"+q.Code+"/say", map[string]string{"text": "kys"}, nil); code != 422 {
		t.Fatalf("moderated say = %d", code)
	}
	if code := player.do("POST", "/api/matches/"+q.Code+"/say", map[string]string{"text": "Every filter on every app agrees with me."}, nil); code != 200 {
		t.Fatalf("say = %d", code)
	}
	if code := player.do("POST", "/api/matches/"+q.Code+"/vote", map[string]string{"side": "a"}, nil); code != 403 {
		t.Fatalf("self vote = %d", code)
	}
	if code := fan.do("POST", "/api/matches/"+q.Code+"/vote", map[string]string{"side": "b"}, &m); code != 200 || m.CrowdB != 1 {
		t.Fatalf("fan vote = %d %+v", code, m)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		player.do("GET", "/api/matches/"+q.Code, nil, &m)
		if len(m.Turns) >= 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(m.Turns) < 2 || m.Turns[1].Side != "b" {
		t.Fatalf("bot never replied: %+v", m.Turns)
	}
	if code := fan.do("POST", "/api/matches/"+q.Code+"/report", map[string]string{"reason": "testing"}, nil); code != 200 {
		t.Fatalf("report = %d", code)
	}
	_ = e
}

func TestLobbyAndPages(t *testing.T) {
	srv, e := newServer(t)
	c := newClient(t, srv)
	c.do("POST", "/api/me", map[string]any{"name": "Sam", "adult": true}, nil)
	var ch core.Match
	if code := c.do("POST", "/api/challenges", map[string]string{"prompt": "Marcus can't cook."}, &ch); code != 201 || ch.Status != "waiting" {
		t.Fatalf("challenge = %d %+v", code, ch)
	}
	var lobby struct {
		Featured struct{ Text string }
		Prompts  []any
		Live     []core.Match
	}
	c.do("GET", "/api/lobby", nil, &lobby)
	if lobby.Featured.Text == "" || len(lobby.Prompts) < 30 || len(lobby.Live) != 0 {
		t.Fatalf("lobby = %+v", lobby)
	}
	resp, _ := http.Get(srv.URL + "/m/" + ch.Code)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), `og:image" content="`+srv.URL+`/og/m/`+ch.Code) || !strings.Contains(string(body), "Sam challenged you") {
		t.Fatalf("match page missing preview tags")
	}
	for _, path := range []string{"/og/m/" + ch.Code, "/og/default"} {
		resp, _ := http.Get(srv.URL + path)
		if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "image/png" {
			t.Fatalf("%s = %d %s", path, resp.StatusCode, resp.Header.Get("Content-Type"))
		}
		resp.Body.Close()
	}
	friend := newClient(t, srv)
	friend.do("POST", "/api/me", map[string]any{"name": "Marcus", "adult": true}, nil)
	var acc core.Match
	if code := friend.do("POST", "/api/matches/"+ch.Code+"/accept", nil, &acc); code != 200 || acc.Status != "live" {
		t.Fatalf("accept = %d %+v", code, acc)
	}
	e.Tick(context.Background())
}
