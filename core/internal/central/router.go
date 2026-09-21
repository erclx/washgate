// Package central serves the HQ API that holds customers, subscriptions, and washes.
package central

import "net/http"

// NewRouter returns the central API's HTTP handler over store. A nil payments leaves the Stripe routes answering 503.
func NewRouter(store *Store, payments *Payments) http.Handler {
	ledger := &ledger{store: store}
	billing := &billing{store: store, payments: payments}
	operator := newOperator(store)
	customers := newCustomerAPI(store)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", handleHealth)
	mux.Handle("POST /washes", requireSite(store, ledger.handlePostWashes))
	mux.Handle("GET /entitlements", requireSite(store, ledger.handleGetEntitlements))
	mux.HandleFunc("POST /checkout", billing.handlePostCheckout)
	mux.HandleFunc("POST /checkout/single-wash", billing.handlePostSingleWashCheckout)
	mux.HandleFunc("POST /stripe/webhook", billing.handlePostStripeWebhook)
	// Operator and customer routes answer without a site token, since a site's token must never open them and
	// central binds to loopback. The customer app's demo picker stands in for a login.
	mux.HandleFunc("GET /plates/{plate}", operator.handleGetPlate)
	mux.HandleFunc("POST /plates/{plate}/quota-resets", operator.handlePostQuotaReset)
	mux.HandleFunc("GET /sites", operator.handleGetSites)
	mux.HandleFunc("GET /customers", customers.handleGetCustomers)
	mux.HandleFunc("GET /customers/{id}/cars", customers.handleGetCars)
	mux.HandleFunc("GET /customers/{id}/cars/{plate}", customers.handleGetCar)
	mux.HandleFunc("POST /customers/{id}/cars", customers.handlePostCar)
	return mux
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok"))
}
