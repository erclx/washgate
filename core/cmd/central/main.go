// Command central runs the HQ API.
package main

import (
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
	server := &http.Server{
		Addr:              address,
		Handler:           central.NewRouter(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	slog.Info("central listening", "address", address)
	if err := server.ListenAndServe(); err != nil {
		slog.Error("central stopped", "error", err)
		os.Exit(1)
	}
}
