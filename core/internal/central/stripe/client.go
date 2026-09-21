// Package stripe is central's test-mode Stripe client: the checkout call, webhook signatures, and event payloads.
package stripe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// APIVersion is the Stripe API version every request pins and every event payload is decoded as.
// Keep it equal to the account's default version, since events forwarded by the Stripe CLI use that one.
const APIVersion = "2025-03-31.basil"

// DefaultBaseURL is Stripe's API host.
const DefaultBaseURL = "https://api.stripe.com"

const (
	requestTimeout   = 10 * time.Second
	maxResponseBytes = 1 << 20
)

// ErrLiveModeKey reports a key that is not a test-mode key, since washgate never moves real money.
var ErrLiveModeKey = errors.New("stripe key is not a test-mode key")

// APIError is an error Stripe answered with.
type APIError struct {
	Status  int
	Type    string `json:"type"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *APIError) Error() string {
	return fmt.Sprintf("stripe answered %d %s %s: %s", e.Status, e.Type, e.Code, e.Message)
}

// CheckoutRequest is one customer paying for Premium or a single wash on one plate.
type CheckoutRequest struct {
	CustomerID string
	Plate      string
	AmountOre  int64
	SuccessURL string
	CancelURL  string
}

// CheckoutSession is a Stripe Checkout session the customer is sent to.
type CheckoutSession struct {
	ID  string `json:"id"`
	URL string `json:"url"`
}

// Client calls Stripe's API in test mode.
type Client struct {
	baseURL    string
	secretKey  string
	httpClient *http.Client
}

// NewClient returns a client for the API at baseURL, refusing any key that is not a test-mode key.
func NewClient(baseURL, secretKey string) (*Client, error) {
	if !strings.HasPrefix(secretKey, "sk_test_") && !strings.HasPrefix(secretKey, "rk_test_") {
		return nil, ErrLiveModeKey
	}
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		secretKey:  secretKey,
		httpClient: &http.Client{Timeout: requestTimeout},
	}, nil
}

// CreateCheckoutSession opens a Checkout session for a monthly Premium subscription priced inline in SEK.
// The subscription carries the customer and plate as metadata, so its paid invoices name them.
func (c *Client) CreateCheckoutSession(ctx context.Context, request CheckoutRequest) (CheckoutSession, error) {
	form := checkoutForm(request, "subscription", "Premium")
	form.Set("line_items[0][price_data][recurring][interval]", "month")
	form.Set("subscription_data[metadata][customer_id]", request.CustomerID)
	form.Set("subscription_data[metadata][plate]", request.Plate)
	return c.createCheckoutSession(ctx, form)
}

// CreateSingleWashSession opens a Checkout session for one wash paid once, priced inline in SEK.
// The session carries the customer and plate as metadata, so its completed event names them.
func (c *Client) CreateSingleWashSession(ctx context.Context, request CheckoutRequest) (CheckoutSession, error) {
	form := checkoutForm(request, "payment", "Single wash")
	form.Set("metadata[kind]", SessionKindSingleWash)
	form.Set("metadata[customer_id]", request.CustomerID)
	form.Set("metadata[plate]", request.Plate)
	return c.createCheckoutSession(ctx, form)
}

func checkoutForm(request CheckoutRequest, mode, productName string) url.Values {
	return url.Values{
		"mode":                                   {mode},
		"client_reference_id":                    {request.CustomerID},
		"success_url":                            {request.SuccessURL},
		"cancel_url":                             {request.CancelURL},
		"line_items[0][quantity]":                {"1"},
		"line_items[0][price_data][currency]":    {"sek"},
		"line_items[0][price_data][unit_amount]": {strconv.FormatInt(request.AmountOre, 10)},
		"line_items[0][price_data][product_data][name]": {productName},
	}
}

func (c *Client) createCheckoutSession(ctx context.Context, form url.Values) (CheckoutSession, error) {
	var session CheckoutSession
	if err := c.post(ctx, "/v1/checkout/sessions", form, &session); err != nil {
		return CheckoutSession{}, fmt.Errorf("create checkout session: %w", err)
	}
	return session, nil
}

func (c *Client) post(ctx context.Context, path string, form url.Values, result any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+c.secretKey)
	request.Header.Set("Stripe-Version", APIVersion)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("reach stripe: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("read stripe answer: %w", err)
	}

	if response.StatusCode >= http.StatusBadRequest {
		var answer struct {
			Error APIError `json:"error"`
		}
		_ = json.Unmarshal(body, &answer)
		answer.Error.Status = response.StatusCode
		return &answer.Error
	}
	if err := json.Unmarshal(body, result); err != nil {
		return fmt.Errorf("decode stripe answer: %w", err)
	}
	return nil
}
