// Package central serves the HQ API that holds customers, subscriptions, and washes.
package central

import "net/http"

// NewRouter returns the central API's HTTP handler over store. A nil payments leaves the Stripe routes answering 503.
func NewRouter(store *Store, payments *Payments) http.Handler {
	ledger := &ledger{store: store}
	billing := &billing{store: store, payments: payments}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", handleHealth)
	mux.Handle("POST /washes", requireSite(store, ledger.handlePostWashes))
	mux.Handle("GET /entitlements", requireSite(store, ledger.handleGetEntitlements))
	mux.HandleFunc("POST /checkout", billing.handlePostCheckout)
	mux.HandleFunc("POST /stripe/webhook", billing.handlePostStripeWebhook)
	return mux
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok"))
}
