// Command site-agent decides entry at one wash site and syncs with central.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"
	_ "time/tzdata" // embeds the time zone database so a slim image can load the site zone

	"github.com/erclx/washgate/core/internal/siteagent"
)

const (
	centralTimeout  = 10 * time.Second
	shutdownTimeout = 10 * time.Second
)

func main() {
	if err := run(context.Background()); err != nil {
		slog.Error("site-agent stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	address := ":" + envOr("PORT", "8081")
	minConfidence, err := strconv.ParseFloat(envOr("SITE_MIN_CONFIDENCE", "0.99"), 64)
	if err != nil {
		return fmt.Errorf("SITE_MIN_CONFIDENCE: %w", err)
	}
	dedupWindow, err := time.ParseDuration(envOr("SITE_DEDUP_WINDOW", "120s"))
	if err != nil {
		return fmt.Errorf("SITE_DEDUP_WINDOW: %w", err)
	}
	syncInterval, err := time.ParseDuration(envOr("SITE_SYNC_INTERVAL", "5s"))
	if err != nil {
		return fmt.Errorf("SITE_SYNC_INTERVAL: %w", err)
	}
	if syncInterval <= 0 {
		return fmt.Errorf("SITE_SYNC_INTERVAL must be positive, got %s", syncInterval)
	}
	maxOffline, err := time.ParseDuration(envOr("SITE_MAX_OFFLINE", "10m"))
	if err != nil {
		return fmt.Errorf("SITE_MAX_OFFLINE: %w", err)
	}
	location, err := time.LoadLocation(envOr("SITE_TIME_ZONE", "Europe/Stockholm"))
	if err != nil {
		return fmt.Errorf("SITE_TIME_ZONE: %w", err)
	}
	centralURL, siteID, token := os.Getenv("CENTRAL_URL"), os.Getenv("SITE_ID"), os.Getenv("SITE_TOKEN")
	if centralURL == "" || siteID == "" || token == "" {
		return errors.New("CENTRAL_URL, SITE_ID, and SITE_TOKEN must all be set")
	}

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	store, err := siteagent.Open(ctx, envOr("SITE_DB_PATH", "site.db"))
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()

	syncer := siteagent.NewSyncer(store, siteagent.SyncConfig{
		CentralURL: centralURL,
		SiteID:     siteID,
		Token:      token,
		Client:     &http.Client{Timeout: centralTimeout},
		Now:        time.Now,
	})
	var syncing sync.WaitGroup
	syncing.Go(func() { syncer.Run(ctx, syncInterval) })
	defer syncing.Wait()

	server := &http.Server{
		Addr: address,
		Handler: siteagent.NewRouter(store, siteagent.Config{
			Policy:   siteagent.Policy{MinConfidence: minConfidence, DedupWindow: dedupWindow, MaxOffline: maxOffline},
			Location: location,
			Now:      time.Now,
		}),
		ReadHeaderTimeout: 5 * time.Second,
	}
	serving := make(chan error, 1)
	go func() { serving <- server.ListenAndServe() }()
	slog.Info("site-agent listening", "address", address, "site_id", siteID, "sync_interval", syncInterval.String())

	select {
	case err := <-serving:
		stop()
		return err
	case <-ctx.Done():
	}
	slog.Info("site-agent shutting down", "site_id", siteID)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shut down server: %w", err)
	}
	if err := <-serving; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
