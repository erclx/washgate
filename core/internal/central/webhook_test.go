package central

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func readStripeFixture(t *testing.T, name string) string {
	t.Helper()
	payload, err := os.ReadFile("stripe/testdata/" + name) //nolint:gosec // the name is a fixture the test itself chose
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(payload)
}

func stripeSignatureHeader(payload, secret string) string {
	signedAt := time.Now().Unix()
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = fmt.Fprintf(mac, "%d.%s", signedAt, payload)
	return fmt.Sprintf("t=%d,v1=%s", signedAt, hex.EncodeToString(mac.Sum(nil)))
}

func postWebhook(t *testing.T, router http.Handler, payload, signatureHeader string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/stripe/webhook", strings.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Stripe-Signature", signatureHeader)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

func newWebhookRouter(t *testing.T, store *Store) http.Handler {
	t.Helper()
	server, _ := fakeStripe(t, http.StatusOK, `{}`)
	return NewRouter(store, newPayments(t, server.URL))
}

func TestPostStripeWebhook(t *testing.T) {
	t.Run("the same signed paid invoice posted twice answers 200 both times and applies once", func(t *testing.T) {
		store, database := newTestStore(t)
		addCustomer(t, database, testCustomerID)
		router := newWebhookRouter(t, store)
		payload := readStripeFixture(t, "invoice_paid.json")

		first := postWebhook(t, router, payload, stripeSignatureHeader(payload, testWebhookSecret))
		second := postWebhook(t, router, payload, stripeSignatureHeader(payload, testWebhookSecret))

		if first.Code != http.StatusOK || second.Code != http.StatusOK {
			t.Fatalf("statuses = %d and %d, want 200 both times", first.Code, second.Code)
		}
		want := []storedSubscription{{Plate: "ABC123", Plan: "premium", Status: "active", PeriodEnd: time.Unix(1792654200, 0).UTC()}}
		if got := readSubscriptions(t, database); len(got) != 1 || got[0] != want[0] {
			t.Fatalf("subscriptions = %+v, want %+v", got, want)
		}
		if changes := readChanges(t, store, 0, 10); len(changes) != 1 {
			t.Fatalf("changes = %d, want 1", len(changes))
		}
	})

	t.Run("a body signed with the wrong secret answers 400 and writes nothing", func(t *testing.T) {
		store, database := newTestStore(t)
		addCustomer(t, database, testCustomerID)
		payload := readStripeFixture(t, "invoice_paid.json")

		recorder := postWebhook(t, newWebhookRouter(t, store), payload, stripeSignatureHeader(payload, "whsec_someone_else"))

		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
		}
		if got := countStripeEvents(t, database); got != 0 {
			t.Fatalf("stripe events = %d, want 0", got)
		}
		if got := readSubscriptions(t, database); len(got) != 0 {
			t.Fatalf("subscriptions = %+v, want none", got)
		}
	})

	t.Run("an unhandled type answers 200 and writes only its event id", func(t *testing.T) {
		store, database := newTestStore(t)
		payload := readStripeFixture(t, "checkout_session_completed.json")

		recorder := postWebhook(t, newWebhookRouter(t, store), payload, stripeSignatureHeader(payload, testWebhookSecret))

		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
		}
		if got := countStripeEvents(t, database); got != 1 {
			t.Fatalf("stripe events = %d, want 1", got)
		}
		if got := readSubscriptions(t, database); len(got) != 0 {
			t.Fatalf("subscriptions = %+v, want none", got)
		}
	})

	t.Run("a deleted subscription answers 200 and revokes the plate", func(t *testing.T) {
		store, database := newTestStore(t)
		addCustomer(t, database, testCustomerID)
		router := newWebhookRouter(t, store)
		paid := readStripeFixture(t, "invoice_paid.json")
		deleted := readStripeFixture(t, "customer_subscription_deleted.json")
		postWebhook(t, router, paid, stripeSignatureHeader(paid, testWebhookSecret))

		recorder := postWebhook(t, router, deleted, stripeSignatureHeader(deleted, testWebhookSecret))

		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
		}
		if got := readSubscriptions(t, database); len(got) != 1 || got[0].Status != "canceled" {
			t.Fatalf("subscriptions = %+v, want one canceled", got)
		}
	})

	t.Run("a paid invoice naming no subscription answers 400 and writes nothing", func(t *testing.T) {
		store, database := newTestStore(t)
		payload := `{"id":"evt_1","type":"invoice.paid","data":{"object":{"id":"in_1"}}}`

		recorder := postWebhook(t, newWebhookRouter(t, store), payload, stripeSignatureHeader(payload, testWebhookSecret))

		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
		}
		if got := countStripeEvents(t, database); got != 0 {
			t.Fatalf("stripe events = %d, want 0", got)
		}
	})

	t.Run("Stripe not configured answers 503", func(t *testing.T) {
		store, _ := newTestStore(t)
		payload := readStripeFixture(t, "invoice_paid.json")

		recorder := postWebhook(t, NewRouter(store, nil), payload, stripeSignatureHeader(payload, testWebhookSecret))

		if recorder.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
		}
	})

	t.Run("a store failure answers 500 so Stripe retries", func(t *testing.T) {
		store, database := newTestStore(t)
		router := newWebhookRouter(t, store)
		if _, err := database.SQL.ExecContext(t.Context(), "DROP TABLE stripe_events"); err != nil {
			t.Fatalf("drop stripe events: %v", err)
		}
		payload := readStripeFixture(t, "invoice_paid.json")

		recorder := postWebhook(t, router, payload, stripeSignatureHeader(payload, testWebhookSecret))

		if recorder.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
		}
	})
}
