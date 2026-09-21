// Command central runs the HQ API.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/erclx/washgate/core/internal/central"
	"github.com/erclx/washgate/core/internal/central/stripe"
)

const provisionTimeout = 10 * time.Second

func main() {
	address := ":8080"
	if port := os.Getenv("PORT"); port != "" {
		address = ":" + port
	}
	siteTokens, err := central.ParseSiteTokens(os.Getenv("CENTRAL_SITE_TOKENS"))
	if err != nil {
		slog.Error("central could not read CENTRAL_SITE_TOKENS", "error", err)
		os.Exit(1)
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
	if err := provisionSites(store, siteTokens); err != nil {
		slog.Error("central could not provision its sites", "error", err)
		_ = store.Close()
		os.Exit(1)
	}
	payments, err := paymentsFromEnv()
	if err != nil {
		slog.Error("central could not configure Stripe", "error", err)
		_ = store.Close()
		os.Exit(1)
	}

	server := &http.Server{
		Addr:              address,
		Handler:           central.NewRouter(store, payments),
		ReadHeaderTimeout: 5 * time.Second,
	}
	slog.Info("central listening", "address", address)
	if err := server.ListenAndServe(); err != nil {
		slog.Error("central stopped", "error", err)
		_ = store.Close()
		os.Exit(1)
	}
}

func provisionSites(store *central.Store, siteTokens []central.SiteToken) error {
	ctx, cancel := context.WithTimeout(context.Background(), provisionTimeout)
	defer cancel()
	if err := store.ProvisionSites(ctx, siteTokens); err != nil {
		return err
	}
	siteIDs := make([]string, 0, len(siteTokens))
	for _, site := range siteTokens {
		siteIDs = append(siteIDs, site.SiteID)
	}
	slog.Info("sites provisioned", "site_ids", siteIDs)
	return nil
}

// paymentsFromEnv builds the Stripe configuration, or returns nil when the key or webhook secret is unset,
// so the stack still starts with no Stripe account.
func paymentsFromEnv() (*central.Payments, error) {
	secretKey := os.Getenv("STRIPE_SECRET_KEY")
	webhookSecret := os.Getenv("STRIPE_WEBHOOK_SECRET")
	if secretKey == "" || webhookSecret == "" {
		slog.Info("stripe is off", "reason", "STRIPE_SECRET_KEY or STRIPE_WEBHOOK_SECRET is unset")
		return nil, nil
	}
	client, err := stripe.NewClient(stripe.DefaultBaseURL, secretKey)
	if err != nil {
		return nil, err
	}
	slog.Info("stripe is on", "api_version", stripe.APIVersion)
	return &central.Payments{
		Client:        client,
		WebhookSecret: webhookSecret,
		SuccessURL:    envOr("CHECKOUT_SUCCESS_URL", "http://localhost:5173/"),
		CancelURL:     envOr("CHECKOUT_CANCEL_URL", "http://localhost:5173/"),
	}, nil
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
