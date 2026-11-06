// Command server runs the paywall-lab HTTP API.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/erencivril/paywall-lab/internal/api"
	"github.com/erencivril/paywall-lab/internal/remoteconfig"
	"github.com/erencivril/paywall-lab/internal/service"
	"github.com/erencivril/paywall-lab/internal/store"
)

func main() {
	healthcheck := flag.Bool("healthcheck", false, "call /healthz on the local server and exit 0 or 1")
	flag.Parse()

	port := env("PORT", "8080")
	if *healthcheck {
		os.Exit(runHealthcheck(port))
	}

	setupLogging(env("LOG_LEVEL", "info"))
	if err := run(port); err != nil {
		slog.Error("server stopped with error", "error", err)
		os.Exit(1)
	}
}

func run(port string) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := remoteconfig.Load(env("CONFIG_PATH", "config/remote_config.json"))
	if err != nil {
		return err
	}
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return errors.New("DATABASE_URL is not set")
	}
	st, err := store.Connect(ctx, dbURL, 30*time.Second)
	if err != nil {
		return err
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		return err
	}

	debug := env("ENABLE_DEBUG_ENDPOINTS", "false") == "true"
	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           api.NewHandler(service.New(cfg, st), debug),
		ReadHeaderTimeout: 5 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", srv.Addr, "config_version", cfg.Version, "debug_endpoints", debug)
		serveErr <- srv.ListenAndServe()
	}()

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
		slog.Info("shutting down")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	slog.Info("stopped cleanly")
	return nil
}

func runHealthcheck(port string) int {
	client := http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://127.0.0.1:" + port + "/healthz")
	if err != nil {
		return 1
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}

func setupLogging(level string) {
	var l slog.Level
	if err := l.UnmarshalText([]byte(strings.ToUpper(level))); err != nil {
		l = slog.LevelInfo
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: l})))
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
