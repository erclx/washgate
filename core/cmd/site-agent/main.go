// Command site-agent decides entry at one wash site and syncs with central.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"time"
	_ "time/tzdata" // embeds the time zone database so a slim image can load the site zone

	"github.com/erclx/washgate/core/internal/siteagent"
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
	location, err := time.LoadLocation(envOr("SITE_TIME_ZONE", "Europe/Stockholm"))
	if err != nil {
		return fmt.Errorf("SITE_TIME_ZONE: %w", err)
	}
	shouldSeed, err := strconv.ParseBool(envOr("SITE_SEED_FIXTURES", "false"))
	if err != nil {
		return fmt.Errorf("SITE_SEED_FIXTURES: %w", err)
	}

	store, err := siteagent.Open(ctx, envOr("SITE_DB_PATH", "site.db"))
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()
	if shouldSeed {
		isSeeded, err := store.SeedFixtures(ctx)
		if err != nil {
			return err
		}
		slog.Info("site-agent fixtures", "seeded", isSeeded)
	}

	server := &http.Server{
		Addr: address,
		Handler: siteagent.NewRouter(store, siteagent.Config{
			Policy:   siteagent.Policy{MinConfidence: minConfidence, DedupWindow: dedupWindow},
			Location: location,
			Now:      time.Now,
		}),
		ReadHeaderTimeout: 5 * time.Second,
	}
	slog.Info("site-agent listening", "address", address)
	return server.ListenAndServe()
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
