package central

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/erclx/washgate/core/internal/central/stripe"
)

// taxiPlate matches a taxi's registration followed by the T marking, the same shape the site drops it from.
var taxiPlate = regexp.MustCompile(`^([A-Z]{3}[0-9]{2}[A-Z0-9])T$`)

const (
	maxCheckoutBodyBytes   = 4 << 10
	rejectedCheckoutReason = "the body needs a customer_id and a plate of 2 to 7 letters and digits"
)

// Payments is central's Stripe configuration. A nil *Payments means Stripe is off and its routes answer 503.
type Payments struct {
	Client        *stripe.Client
	WebhookSecret string
	SuccessURL    string
	CancelURL     string
}

type checkoutRequest struct {
	CustomerID string `json:"customer_id"`
	Plate      string `json:"plate"`
}

type checkoutResponse struct {
	CheckoutURL string `json:"checkout_url"`
}

type billing struct {
	store    *Store
	payments *Payments
}

func (b *billing) handlePostCheckout(w http.ResponseWriter, r *http.Request) {
	body, isAccepted := b.acceptCheckout(w, r, http.StatusConflict)
	if !isAccepted {
		return
	}
	amountOre, err := b.store.PremiumPrice(r.Context(), time.Now())
	if errors.Is(err, ErrNoPremiumPrice) {
		slog.Warn("checkout refused", "reason", "no premium price is valid")
		http.Error(w, "Premium has no price yet", http.StatusServiceUnavailable)
		return
	}
	if err != nil {
		slog.Error("checkout failed", "stage", "read price", "error", err)
		http.Error(w, "central could not start checkout", http.StatusInternalServerError)
		return
	}

	session, err := b.payments.Client.CreateCheckoutSession(r.Context(), b.checkoutRequest(body, amountOre))
	b.answerCheckout(w, session, err)
}

// handlePostSingleWashCheckout opens a one-off payment for one wash. A plate held by a company or by another
// customer answers 422 rather than 409, since a fleet car is admitted already and billed on the invoice.
func (b *billing) handlePostSingleWashCheckout(w http.ResponseWriter, r *http.Request) {
	body, isAccepted := b.acceptCheckout(w, r, http.StatusUnprocessableEntity)
	if !isAccepted {
		return
	}
	amountOre, err := b.store.SingleWashPrice(r.Context(), time.Now())
	if errors.Is(err, ErrNoSingleWashPrice) {
		slog.Warn("checkout refused", "reason", "no single wash price is valid")
		http.Error(w, "a single wash has no price yet", http.StatusServiceUnavailable)
		return
	}
	if err != nil {
		slog.Error("checkout failed", "stage", "read price", "error", err)
		http.Error(w, "central could not start checkout", http.StatusInternalServerError)
		return
	}

	session, err := b.payments.Client.CreateSingleWashSession(r.Context(), b.checkoutRequest(body, amountOre))
	b.answerCheckout(w, session, err)
}

// acceptCheckout decodes and checks a checkout body, answering the request itself and reporting false when
// it refuses. A plate registered to anyone but the buyer answers heldStatus.
func (b *billing) acceptCheckout(w http.ResponseWriter, r *http.Request, heldStatus int) (checkoutRequest, bool) {
	if b.payments == nil {
		http.Error(w, "payments are not configured", http.StatusServiceUnavailable)
		return checkoutRequest{}, false
	}
	var body checkoutRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxCheckoutBodyBytes))
	if err := decoder.Decode(&body); err != nil || decoder.More() {
		http.Error(w, rejectedCheckoutReason, http.StatusBadRequest)
		return checkoutRequest{}, false
	}
	body.Plate = strings.ToUpper(strings.TrimSpace(body.Plate))
	if !isPresentWithin(body.CustomerID, maxReferenceLength) || !normalizedPlate.MatchString(body.Plate) {
		http.Error(w, rejectedCheckoutReason, http.StatusBadRequest)
		return checkoutRequest{}, false
	}
	// Central's own copy of the lane's taxi rule, so a taxi's purchase lands under the key the lane looks up.
	if match := taxiPlate.FindStringSubmatch(body.Plate); match != nil {
		body.Plate = match[1]
	}

	exists, err := b.store.CustomerExists(r.Context(), body.CustomerID)
	if err != nil {
		slog.Error("checkout failed", "stage", "read customer", "error", err)
		http.Error(w, "central could not start checkout", http.StatusInternalServerError)
		return checkoutRequest{}, false
	}
	if !exists {
		http.Error(w, "the customer_id names no customer central knows", http.StatusUnprocessableEntity)
		return checkoutRequest{}, false
	}
	held, err := b.store.IsPlateHeldByAnother(r.Context(), body.Plate, body.CustomerID)
	if err != nil {
		slog.Error("checkout failed", "stage", "read plate holder", "error", err)
		http.Error(w, "central could not start checkout", http.StatusInternalServerError)
		return checkoutRequest{}, false
	}
	if held {
		http.Error(w, "this plate is registered to another owner", heldStatus)
		return checkoutRequest{}, false
	}
	return body, true
}

func (b *billing) checkoutRequest(body checkoutRequest, amountOre int64) stripe.CheckoutRequest {
	return stripe.CheckoutRequest{
		CustomerID: body.CustomerID,
		Plate:      body.Plate,
		AmountOre:  amountOre,
		SuccessURL: b.payments.SuccessURL,
		CancelURL:  b.payments.CancelURL,
	}
}

func (b *billing) answerCheckout(w http.ResponseWriter, session stripe.CheckoutSession, err error) {
	if err != nil {
		slog.Error("checkout failed", "stage", "create stripe session", "error", err)
		http.Error(w, "the payment provider could not start checkout", http.StatusBadGateway)
		return
	}
	slog.Info("checkout started", "checkout_session_id", session.ID)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(checkoutResponse{CheckoutURL: session.URL})
}
