package central

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/erclx/washgate/core/internal/central/stripe"
)

const (
	testSuccessURL    = "http://localhost:5173/?checkout=success"
	testCancelURL     = "http://localhost:5173/?checkout=cancel"
	testWebhookSecret = "whsec_test_washgate" //nolint:gosec // a fixed secret the tests sign with, never a real one
	testCheckoutURL   = "https://checkout.stripe.com/c/pay/cs_test_1"
)

// fakeStripe answers every call with status and body, and hands back the form of the last call it received.
func fakeStripe(t *testing.T, status int, body string) (*httptest.Server, *url.Values) {
	t.Helper()
	received := &url.Values{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read stripe request: %v", err)
		}
		form, err := url.ParseQuery(string(raw))
		if err != nil {
			t.Errorf("parse stripe request: %v", err)
		}
		*received = form
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)
	return server, received
}

func newPayments(t *testing.T, stripeURL string) *Payments {
	t.Helper()
	client, err := stripe.NewClient(stripeURL, "sk_test_washgate")
	if err != nil {
		t.Fatalf("new stripe client: %v", err)
	}
	return &Payments{Client: client, WebhookSecret: testWebhookSecret, SuccessURL: testSuccessURL, CancelURL: testCancelURL}
}

func postCheckout(t *testing.T, router http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/checkout", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

func TestPostCheckout(t *testing.T) {
	t.Run("answers 201 with the checkout url, priced from the premium row and carrying the metadata", func(t *testing.T) {
		store, database := newTestStore(t)
		addCustomer(t, database, testCustomerID)
		addPrice(t, database, "premium", 29900, testAdmittedAt.AddDate(-1, 0, 0))
		server, received := fakeStripe(t, http.StatusOK, `{"id":"cs_test_1","url":"`+testCheckoutURL+`"}`)

		recorder := postCheckout(t, NewRouter(store, newPayments(t, server.URL)), `{"customer_id": "customer-anna", "plate": " abc123 "}`)

		if recorder.Code != http.StatusCreated {
			t.Fatalf("status = %d, want %d, body %q", recorder.Code, http.StatusCreated, recorder.Body.String())
		}
		var answer struct {
			CheckoutURL string `json:"checkout_url"`
		}
		if err := json.NewDecoder(recorder.Body).Decode(&answer); err != nil {
			t.Fatalf("decode answer: %v", err)
		}
		if answer.CheckoutURL != testCheckoutURL {
			t.Fatalf("checkout url = %q, want %q", answer.CheckoutURL, testCheckoutURL)
		}
		want := map[string]string{
			"line_items[0][price_data][unit_amount]":   "29900",
			"subscription_data[metadata][customer_id]": testCustomerID,
			"subscription_data[metadata][plate]":       "ABC123",
			"success_url":                              testSuccessURL,
			"cancel_url":                               testCancelURL,
		}
		for field, value := range want {
			if got := received.Get(field); got != value {
				t.Errorf("stripe form %s = %q, want %q", field, got, value)
			}
		}
	})

	t.Run("a taxi's trailing T is dropped the way the lane drops it", func(t *testing.T) {
		store, database := newTestStore(t)
		addCustomer(t, database, testCustomerID)
		addPrice(t, database, "premium", 29900, testAdmittedAt.AddDate(-1, 0, 0))
		server, received := fakeStripe(t, http.StatusOK, `{"id":"cs_test_1","url":"`+testCheckoutURL+`"}`)

		recorder := postCheckout(t, NewRouter(store, newPayments(t, server.URL)), `{"customer_id": "customer-anna", "plate": "abc123t"}`)

		if recorder.Code != http.StatusCreated {
			t.Fatalf("status = %d, want %d, body %q", recorder.Code, http.StatusCreated, recorder.Body.String())
		}
		if got := received.Get("subscription_data[metadata][plate]"); got != "ABC123" {
			t.Fatalf("stripe metadata plate = %q, want %q", got, "ABC123")
		}
	})

	cases := []struct {
		name string
		body string
	}{
		{name: "a body that is not JSON answers 400", body: `customer_id=customer-anna`},
		{name: "a plate of one character answers 400", body: `{"customer_id": "customer-anna", "plate": "A"}`},
		{name: "a plate with a space inside answers 400", body: `{"customer_id": "customer-anna", "plate": "ABC 123"}`},
		{name: "a missing customer id answers 400", body: `{"plate": "ABC123"}`},
		{name: "a body over 4 KiB answers 400", body: `{"customer_id": "` + strings.Repeat("a", 5000) + `", "plate": "ABC123"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store, _ := newTestStore(t)
			server, _ := fakeStripe(t, http.StatusOK, `{}`)

			recorder := postCheckout(t, NewRouter(store, newPayments(t, server.URL)), tc.body)

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
			}
		})
	}

	t.Run("an unknown customer answers 422", func(t *testing.T) {
		store, database := newTestStore(t)
		addPrice(t, database, "premium", 29900, testAdmittedAt.AddDate(-1, 0, 0))
		server, _ := fakeStripe(t, http.StatusOK, `{}`)

		recorder := postCheckout(t, NewRouter(store, newPayments(t, server.URL)), `{"customer_id": "customer-nobody", "plate": "ABC123"}`)

		if recorder.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnprocessableEntity)
		}
	})

	t.Run("a plate registered to another owner answers 409", func(t *testing.T) {
		store, database := newTestStore(t)
		addCustomer(t, database, testCustomerID)
		addCustomer(t, database, "customer-bo")
		addPrice(t, database, "premium", 29900, testAdmittedAt.AddDate(-1, 0, 0))
		putEntitlement(t, store, Entitlement{Plate: "ABC123", Plan: PlanPremium, CustomerID: "customer-bo"})
		server, _ := fakeStripe(t, http.StatusOK, `{}`)

		recorder := postCheckout(t, NewRouter(store, newPayments(t, server.URL)), `{"customer_id": "customer-anna", "plate": "ABC123"}`)

		if recorder.Code != http.StatusConflict {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusConflict)
		}
	})

	t.Run("no premium price answers 503", func(t *testing.T) {
		store, database := newTestStore(t)
		addCustomer(t, database, testCustomerID)
		server, _ := fakeStripe(t, http.StatusOK, `{}`)

		recorder := postCheckout(t, NewRouter(store, newPayments(t, server.URL)), `{"customer_id": "customer-anna", "plate": "ABC123"}`)

		if recorder.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
		}
	})

	t.Run("Stripe not configured answers 503", func(t *testing.T) {
		store, _ := newTestStore(t)

		recorder := postCheckout(t, NewRouter(store, nil), `{"customer_id": "customer-anna", "plate": "ABC123"}`)

		if recorder.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
		}
	})

	t.Run("a Stripe failure answers 502", func(t *testing.T) {
		store, database := newTestStore(t)
		addCustomer(t, database, testCustomerID)
		addPrice(t, database, "premium", 29900, testAdmittedAt.AddDate(-1, 0, 0))
		server, _ := fakeStripe(t, http.StatusInternalServerError, `{"error":{"type":"api_error","message":"boom"}}`)

		recorder := postCheckout(t, NewRouter(store, newPayments(t, server.URL)), `{"customer_id": "customer-anna", "plate": "ABC123"}`)

		if recorder.Code != http.StatusBadGateway {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadGateway)
		}
	})
}
