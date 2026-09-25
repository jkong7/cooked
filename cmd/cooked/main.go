package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jkong7/cooked/internal/ai"
	"github.com/jkong7/cooked/internal/core"
	"github.com/jkong7/cooked/internal/game"
	"github.com/jkong7/cooked/internal/hub"
	"github.com/jkong7/cooked/internal/web"
)

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	addr := flag.String("addr", env("ADDR", ":8095"), "listen address")
	dbPath := flag.String("db", env("DB_PATH", "cooked.db"), "SQLite database path")
	secure := flag.Bool("secure", os.Getenv("SECURE") == "1", "set Secure on cookies (behind HTTPS)")
	trustProxy := flag.Bool("trust-proxy", os.Getenv("TRUST_PROXY") == "1", "read client IPs from proxy headers")
	model := flag.String("model", env("COOKED_MODEL", ai.DefaultModel), "Claude model for the judge, moderator and opponent")
	flag.Parse()
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	store, err := core.Open(*dbPath)
	if err != nil {
		log.Error("open database", "err", err)
		os.Exit(1)
	}
	defer store.Close()

	var brain ai.Brain = ai.Local{}
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		brain = ai.NewClaude(key, *model)
		log.Info("using Claude", "model", *model)
	} else {
		log.Warn("ANTHROPIC_API_KEY not set; using the local judge, moderator and opponent")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	h := hub.New()
	engine := game.New(store, brain, h, log)
	go engine.Run(ctx)

	srv := &http.Server{
		Addr: *addr,
		Handler: (&web.Server{Store: store, Engine: engine, Hub: h, Log: log, Secure: *secure, TrustProxy: *trustProxy,
			Limiter: web.NewLimiter(90, 30)}).Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdown)
	}()
	log.Info("cooked listening", "addr", *addr, "db", *dbPath)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		log.Error("serve", "err", err)
		os.Exit(1)
	}
}
