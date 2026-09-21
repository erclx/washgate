// Command central runs the HQ API.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/erclx/washgate/core/internal/central"
)

func main() {
	address := ":8080"
	if port := os.Getenv("PORT"); port != "" {
		address = ":" + port
	}
	store, err := central.Open(context.Background(), central.Config{
		Host:     os.Getenv("DB_HOST"),
		Port:     envOr("DB_PORT", "3306"),
		Name:     os.Getenv("DB_NAME"),
		User:     os.Getenv("DB_USER"),
		Password: os.Getenv("DB_PASSWORD"),
	})
	if err != nil {
		slog.Error("central could not open its database", "error", err)
		os.Exit(1)
	}

	server := &http.Server{
		Addr:              address,
		Handler:           central.NewRouter(store),
		ReadHeaderTimeout: 5 * time.Second,
	}
	slog.Info("central listening", "address", address)
	if err := server.ListenAndServe(); err != nil {
		slog.Error("central stopped", "error", err)
		_ = store.Close()
		os.Exit(1)
	}
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
