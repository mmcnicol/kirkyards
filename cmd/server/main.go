// Command server serves kirkyards.scot.
//
// Configuration is by environment variable, to suit Cloud Run:
//
//	PORT            listen port (default 8080)
//	BASE_URL        public origin (default http://localhost:PORT)
//	PHOTO_BASE_URL  where photos are served from (default: none, show placeholders)
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mmcnicol/kirkyards"
	"github.com/mmcnicol/kirkyards/internal/archive"
	"github.com/mmcnicol/kirkyards/internal/site"
)

func main() {
	port := cmpEnv("PORT", "8080")
	a, err := archive.Load(kirkyards.Data())
	if err != nil {
		fatal("load archive", err)
	}
	srv, err := site.New(a, kirkyards.Web(), site.Config{
		BaseURL:      cmpEnv("BASE_URL", "http://localhost:"+port),
		PhotoBaseURL: os.Getenv("PHOTO_BASE_URL"),
		RepoURL:      "https://github.com/mmcnicol/kirkyards",
	})
	if err != nil {
		fatal("build site", err)
	}

	hs := &http.Server{
		Addr:              ":" + port,
		Handler:           srv,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		hs.Shutdown(shutdown)
	}()
	slog.Info("listening", "port", port, "places", len(a.Places))
	if err := hs.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fatal("serve", err)
	}
}

func cmpEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func fatal(msg string, err error) {
	slog.Error(msg, "err", err)
	os.Exit(1)
}
