// Package central serves the HQ API that holds customers, subscriptions, and washes.
package central

import "net/http"

// NewRouter returns the central API's HTTP handler over store.
func NewRouter(store *Store) http.Handler {
	ledger := &ledger{store: store}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", handleHealth)
	mux.HandleFunc("POST /washes", ledger.handlePostWashes)
	mux.HandleFunc("GET /entitlements", ledger.handleGetEntitlements)
	return mux
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok"))
}
