package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/jkong7/cooked/internal/core"
	"github.com/jkong7/cooked/internal/game"
	"github.com/jkong7/cooked/internal/hub"
	"github.com/jkong7/cooked/internal/prompts"
)

const cookieName = "ck"

type Server struct {
	Store      *core.Store
	Engine     *game.Engine
	Hub        *hub.Hub
	Log        *slog.Logger
	Secure     bool
	TrustProxy bool
	Limiter    *Limiter
}

type userKey struct{}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/me", s.me)
	mux.HandleFunc("POST /api/me", s.signup)
	mux.HandleFunc("GET /api/lobby", s.lobby)
	mux.HandleFunc("GET /api/lobby/events", s.stream(func(r *http.Request) string { return game.Lobby }))
	mux.HandleFunc("GET /api/leaderboard", s.leaderboard)
	mux.HandleFunc("GET /api/users/{id}", s.profile)
	mux.HandleFunc("POST /api/queue", s.auth(s.queue))
	mux.HandleFunc("POST /api/challenges", s.auth(s.challenge))
	mux.HandleFunc("GET /api/matches/{code}", s.match)
	mux.HandleFunc("GET /api/matches/{code}/events", s.stream(func(r *http.Request) string { return game.Topic(r.PathValue("code")) }))
	mux.HandleFunc("POST /api/matches/{code}/accept", s.auth(s.accept))
	mux.HandleFunc("POST /api/matches/{code}/say", s.auth(s.say))
	mux.HandleFunc("POST /api/matches/{code}/vote", s.auth(s.vote))
	mux.HandleFunc("POST /api/matches/{code}/report", s.auth(s.report))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	s.pages(mux)
	var h http.Handler = mux
	if s.Limiter != nil {
		h = s.Limiter.Middleware(s.TrustProxy, h)
	}
	return headers(h)
}

func headers(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("X-Frame-Options", "DENY")
		r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func (s *Server) fail(w http.ResponseWriter, err error) {
	status, msg := http.StatusInternalServerError, "something broke"
	switch {
	case errors.Is(err, core.ErrNotFound):
		status, msg = http.StatusNotFound, "not found"
	case errors.Is(err, core.ErrNotYours):
		status, msg = http.StatusConflict, "not your turn"
	case errors.Is(err, core.ErrState):
		status, msg = http.StatusConflict, "that debate already moved on"
	case errors.Is(err, core.ErrForbidden):
		status, msg = http.StatusForbidden, "you can't do that here"
	case errors.Is(err, game.ErrBlocked):
		status, msg = http.StatusUnprocessableEntity, "that got blocked. argue the take, not the person"
	case errors.Is(err, core.ErrInvalid):
		status, msg = http.StatusBadRequest, "that doesn't look right"
	case errors.Is(err, context.Canceled):
		status, msg = 499, "cancelled"
	default:
		s.Log.Error("request failed", "err", err)
	}
	writeJSON(w, status, map[string]string{"error": msg})
}

func decode(r *http.Request, v any) error {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		return core.ErrInvalid
	}
	return nil
}

func (s *Server) currentUser(r *http.Request) (core.User, bool) {
	c, err := r.Cookie(cookieName)
	if err != nil || c.Value == "" {
		return core.User{}, false
	}
	u, err := s.Store.UserByToken(r.Context(), c.Value)
	return u, err == nil
}

func userFrom(ctx context.Context) core.User {
	u, _ := ctx.Value(userKey{}).(core.User)
	return u
}

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, ok := s.currentUser(r)
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "pick a name first"})
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), userKey{}, u)))
	}
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	u, ok := s.currentUser(r)
	if !ok {
		writeJSON(w, http.StatusOK, nil)
		return
	}
	writeJSON(w, http.StatusOK, u)
}

func (s *Server) signup(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name  string `json:"name"`
		Adult bool   `json:"adult"`
	}
	if err := decode(r, &in); err != nil {
		s.fail(w, err)
		return
	}
	if !in.Adult {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "cooked is 18+"})
		return
	}
	if u, ok := s.currentUser(r); ok {
		writeJSON(w, http.StatusOK, u)
		return
	}
	u, token, err := s.Store.CreateUser(r.Context(), in.Name)
	if err != nil {
		s.fail(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: token, Path: "/", HttpOnly: true, Secure: s.Secure,
		SameSite: http.SameSiteLaxMode, MaxAge: 400 * 24 * 3600})
	writeJSON(w, http.StatusCreated, u)
}

func (s *Server) lobby(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	now := s.Store.Now()
	live, err := s.Store.Live(ctx, 30)
	if err != nil {
		s.fail(w, err)
		return
	}
	recent, err := s.Store.Recent(ctx, 12)
	if err != nil {
		s.fail(w, err)
		return
	}
	board, err := s.Store.Leaderboard(ctx, 10)
	if err != nil {
		s.fail(w, err)
		return
	}
	watching := map[string]int{}
	for _, m := range live {
		watching[m.Code] = s.Hub.Watching(game.Topic(m.Code))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"featured": prompts.Featured(now), "next_rotation": prompts.NextRotation(now), "prompts": prompts.All,
		"live": live, "recent": recent, "leaderboard": board, "watching": watching, "online": s.Hub.Watching(game.Lobby),
	})
}

func (s *Server) leaderboard(w http.ResponseWriter, r *http.Request) {
	board, err := s.Store.Leaderboard(r.Context(), 50)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, board)
}

func (s *Server) profile(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.fail(w, core.ErrNotFound)
		return
	}
	u, err := s.Store.User(r.Context(), id)
	if err != nil {
		s.fail(w, err)
		return
	}
	history, err := s.Store.History(r.Context(), id, 30)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": u, "history": history})
}

func (s *Server) queue(w http.ResponseWriter, r *http.Request) {
	var in struct {
		PromptID int    `json:"prompt_id"`
		Side     string `json:"side"`
	}
	if err := decode(r, &in); err != nil {
		s.fail(w, err)
		return
	}
	code, err := s.Engine.Queue(r.Context(), userFrom(r.Context()).ID, in.PromptID, in.Side)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"code": code})
}

func (s *Server) challenge(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Prompt string `json:"prompt"`
	}
	if err := decode(r, &in); err != nil {
		s.fail(w, err)
		return
	}
	m, err := s.Engine.Challenge(r.Context(), userFrom(r.Context()).ID, in.Prompt)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, m)
}

type matchView struct {
	core.Match
	You      string `json:"you,omitempty"`
	Watching int    `json:"watching"`
}

func (s *Server) view(r *http.Request, m core.Match) matchView {
	v := matchView{Match: m, Watching: s.Hub.Watching(game.Topic(m.Code))}
	if u, ok := s.currentUser(r); ok {
		v.You = m.SideOf(u.ID)
	}
	return v
}

func (s *Server) match(w http.ResponseWriter, r *http.Request) {
	m, err := s.Store.Match(r.Context(), r.PathValue("code"))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.view(r, m))
}

func (s *Server) accept(w http.ResponseWriter, r *http.Request) {
	m, err := s.Engine.Accept(r.Context(), r.PathValue("code"), userFrom(r.Context()).ID)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.view(r, m))
}

func (s *Server) say(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Text string `json:"text"`
	}
	if err := decode(r, &in); err != nil {
		s.fail(w, err)
		return
	}
	m, err := s.Engine.Say(r.Context(), r.PathValue("code"), userFrom(r.Context()).ID, in.Text)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.view(r, m))
}

func (s *Server) vote(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Side string `json:"side"`
	}
	if err := decode(r, &in); err != nil {
		s.fail(w, err)
		return
	}
	m, err := s.Engine.Vote(r.Context(), r.PathValue("code"), userFrom(r.Context()).ID, in.Side)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.view(r, m))
}

func (s *Server) report(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Reason string `json:"reason"`
	}
	if err := decode(r, &in); err != nil {
		s.fail(w, err)
		return
	}
	if err := s.Store.Report(r.Context(), r.PathValue("code"), userFrom(r.Context()).ID, in.Reason); err != nil {
		s.fail(w, err)
		return
	}
	s.Log.Warn("match reported", "match", r.PathValue("code"), "reason", in.Reason)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) stream(topic func(*http.Request) string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		ch, unsub := s.Hub.Subscribe(topic(r))
		defer unsub()
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Accel-Buffering", "no")
		fmt.Fprint(w, "retry: 2000\n\n")
		flusher.Flush()
		ping := time.NewTicker(20 * time.Second)
		defer ping.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}
				fmt.Fprintf(w, "data: %s\n\n", msg)
				flusher.Flush()
			case <-ping.C:
				fmt.Fprint(w, ": ping\n\n")
				flusher.Flush()
			}
		}
	}
}
