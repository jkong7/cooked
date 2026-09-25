package web

import (
	"bytes"
	"embed"
	"html/template"
	"io/fs"
	"net/http"

	"github.com/jkong7/cooked/internal/card"
	"github.com/jkong7/cooked/internal/core"
	"github.com/jkong7/cooked/internal/prompts"
)

//go:embed static
var static embed.FS

var page = template.Must(template.ParseFS(static, "static/index.html"))

type meta struct {
	Title       string
	Description string
	Image       string
	URL         string
}

func (s *Server) origin(r *http.Request) string {
	scheme := "http"
	if s.Secure || r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, m meta) {
	if m.Title == "" {
		m.Title = "cooked: say it with your chest"
	}
	if m.Description == "" {
		m.Description = "Live 1v1 hot-take debates. The crowd votes, an AI judges, and someone gets cooked."
	}
	if m.Image == "" {
		m.Image = "/og/default"
	}
	o := s.origin(r)
	m.Image, m.URL = o+m.Image, o+r.URL.Path
	var buf bytes.Buffer
	if err := page.Execute(&buf, m); err != nil {
		s.fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(buf.Bytes())
}

func (s *Server) pages(mux *http.ServeMux) {
	assets, _ := fs.Sub(static, "static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(assets)))
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) { s.render(w, r, meta{}) })
	mux.HandleFunc("GET /u/{id}", func(w http.ResponseWriter, r *http.Request) { s.render(w, r, meta{}) })
	mux.HandleFunc("GET /m/{code}", s.matchPage)
	mux.HandleFunc("GET /og/m/{code}", s.matchCard)
	mux.HandleFunc("GET /og/default", s.defaultCard)
}

func cardOf(m core.Match) card.Match {
	c := card.Match{Prompt: m.Prompt, NameA: m.A.Name, TierA: m.A.Tier, Status: m.Status, Winner: m.Winner,
		Headline: m.Headline, CrowdA: m.CrowdA, CrowdB: m.CrowdB, ScoreA: m.ScoreA}
	if m.B != nil {
		c.NameB, c.TierB = m.B.Name, m.B.Tier
	}
	return c
}

func (s *Server) matchPage(w http.ResponseWriter, r *http.Request) {
	m, err := s.Store.Match(r.Context(), r.PathValue("code"))
	if err != nil {
		s.render(w, r, meta{})
		return
	}
	title := "“" + m.Prompt + "”"
	desc := m.A.Name + " is live right now. Watch and vote."
	switch {
	case m.Status == "waiting":
		title = m.A.Name + " challenged you: “" + m.Prompt + "”"
		desc = "Take the other side. Three rounds, the crowd votes, an AI judges."
	case m.Status == "done" && m.Headline != "":
		desc = m.Headline + ". " + m.RoastA
	case m.B != nil:
		desc = m.A.Name + " vs " + m.B.Name + " is live right now. Watch and vote."
	}
	s.render(w, r, meta{Title: title, Description: desc, Image: "/og/m/" + m.Code})
}

func pngOut(w http.ResponseWriter, fn func(*bytes.Buffer) error) {
	var buf bytes.Buffer
	if err := fn(&buf); err != nil {
		http.Error(w, "render failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=30")
	w.Write(buf.Bytes())
}

func (s *Server) matchCard(w http.ResponseWriter, r *http.Request) {
	m, err := s.Store.Match(r.Context(), r.PathValue("code"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	pngOut(w, func(b *bytes.Buffer) error { return card.Render(b, cardOf(m)) })
}

func (s *Server) defaultCard(w http.ResponseWriter, r *http.Request) {
	p := prompts.Featured(s.Store.Now())
	pngOut(w, func(b *bytes.Buffer) error {
		return card.Render(b, card.Match{Prompt: p.Text, NameA: "you", TierA: "NPC", Status: "waiting"})
	})
}
