package stripe

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

const testSecretKey = "sk_test_washgate" //nolint:gosec // a made-up key the fake Stripe server accepts, never a real one

type receivedRequest struct {
	method  string
	path    string
	headers http.Header
	form    url.Values
}

// fakeStripe answers every call with status and body, and records the last request it received.
func fakeStripe(t *testing.T, status int, body string) (*httptest.Server, *receivedRequest) {
	t.Helper()
	received := &receivedRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		form, err := url.ParseQuery(string(raw))
		if err != nil {
			t.Errorf("parse request form: %v", err)
		}
		*received = receivedRequest{method: r.Method, path: r.URL.Path, headers: r.Header.Clone(), form: form}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)
	return server, received
}

func newCheckoutRequest() CheckoutRequest {
	return CheckoutRequest{
		CustomerID: "customer-anna",
		Plate:      "ABC123",
		AmountOre:  29900,
		SuccessURL: "http://localhost:5173/?checkout=success",
		CancelURL:  "http://localhost:5173/?checkout=cancel",
	}
}

func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	client, err := NewClient(baseURL, testSecretKey)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	return client
}

func TestCreateCheckoutSession(t *testing.T) {
	t.Run("posts a monthly Premium subscription session and returns its id and url", func(t *testing.T) {
		server, received := fakeStripe(t, http.StatusOK, `{"id":"cs_test_1","url":"https://checkout.stripe.com/c/pay/cs_test_1"}`)

		session, err := newTestClient(t, server.URL).CreateCheckoutSession(t.Context(), newCheckoutRequest())

		if err != nil {
			t.Fatalf("create session: %v", err)
		}
		if session.ID != "cs_test_1" || session.URL != "https://checkout.stripe.com/c/pay/cs_test_1" {
			t.Fatalf("session = %+v, want cs_test_1 and its url", session)
		}
		if received.method != http.MethodPost || received.path != "/v1/checkout/sessions" {
			t.Fatalf("request = %s %s, want POST /v1/checkout/sessions", received.method, received.path)
		}
		want := map[string]string{
			"mode":                                   "subscription",
			"client_reference_id":                    "customer-anna",
			"success_url":                            "http://localhost:5173/?checkout=success",
			"cancel_url":                             "http://localhost:5173/?checkout=cancel",
			"line_items[0][quantity]":                "1",
			"line_items[0][price_data][currency]":    "sek",
			"line_items[0][price_data][unit_amount]": "29900",
			"line_items[0][price_data][recurring][interval]": "month",
			"line_items[0][price_data][product_data][name]":  "Premium",
			"subscription_data[metadata][customer_id]":       "customer-anna",
			"subscription_data[metadata][plate]":             "ABC123",
		}
		for field, value := range want {
			if got := received.form.Get(field); got != value {
				t.Errorf("form %s = %q, want %q", field, got, value)
			}
		}
	})

	t.Run("sends the secret key, the pinned version, and a form body", func(t *testing.T) {
		server, received := fakeStripe(t, http.StatusOK, `{"id":"cs_test_1","url":"https://checkout.stripe.com/c/pay/cs_test_1"}`)

		if _, err := newTestClient(t, server.URL).CreateCheckoutSession(t.Context(), newCheckoutRequest()); err != nil {
			t.Fatalf("create session: %v", err)
		}

		if got := received.headers.Get("Authorization"); got != "Bearer "+testSecretKey {
			t.Errorf("authorization = %q, want the bearer secret key", got)
		}
		if got := received.headers.Get("Stripe-Version"); got != APIVersion {
			t.Errorf("stripe version = %q, want %q", got, APIVersion)
		}
		if got := received.headers.Get("Content-Type"); got != "application/x-www-form-urlencoded" {
			t.Errorf("content type = %q, want a form", got)
		}
	})

	t.Run("a Stripe error body comes back as an APIError", func(t *testing.T) {
		server, _ := fakeStripe(t, http.StatusBadRequest,
			`{"error":{"type":"invalid_request_error","code":"parameter_invalid_integer","message":"Invalid integer: abc"}}`)

		_, err := newTestClient(t, server.URL).CreateCheckoutSession(t.Context(), newCheckoutRequest())

		var apiErr *APIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("error = %v, want an APIError", err)
		}
		if apiErr.Status != http.StatusBadRequest || apiErr.Type != "invalid_request_error" || apiErr.Code != "parameter_invalid_integer" {
			t.Fatalf("api error = %+v, want the 400 invalid_request_error", apiErr)
		}
	})
}

func TestNewClient(t *testing.T) {
	cases := []struct {
		name string
		key  string
		want error
	}{
		{name: "a test secret key is accepted", key: "sk_test_abc"},
		{name: "a restricted test key is accepted", key: "rk_test_abc"},
		{name: "a live secret key is refused", key: "sk_live_abc", want: ErrLiveModeKey},
		{name: "a restricted live key is refused", key: "rk_live_abc", want: ErrLiveModeKey},
		{name: "a key of no known shape is refused", key: "abc", want: ErrLiveModeKey},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewClient("https://api.stripe.com", tc.key)

			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
		})
	}
}
