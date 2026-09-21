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
	if b.payments == nil {
		http.Error(w, "payments are not configured", http.StatusServiceUnavailable)
		return
	}
	var body checkoutRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxCheckoutBodyBytes))
	if err := decoder.Decode(&body); err != nil || decoder.More() {
		http.Error(w, rejectedCheckoutReason, http.StatusBadRequest)
		return
	}
	plate := strings.ToUpper(strings.TrimSpace(body.Plate))
	if !isPresentWithin(body.CustomerID, maxReferenceLength) || !normalizedPlate.MatchString(plate) {
		http.Error(w, rejectedCheckoutReason, http.StatusBadRequest)
		return
	}
	// Central's own copy of the lane's taxi rule, so a taxi's Premium lands under the key the lane looks up.
	if match := taxiPlate.FindStringSubmatch(plate); match != nil {
		plate = match[1]
	}

	exists, err := b.store.CustomerExists(r.Context(), body.CustomerID)
	if err != nil {
		slog.Error("checkout failed", "stage", "read customer", "error", err)
		http.Error(w, "central could not start checkout", http.StatusInternalServerError)
		return
	}
	if !exists {
		http.Error(w, "the customer_id names no customer central knows", http.StatusUnprocessableEntity)
		return
	}
	held, err := b.store.IsPlateHeldByAnother(r.Context(), plate, body.CustomerID)
	if err != nil {
		slog.Error("checkout failed", "stage", "read plate holder", "error", err)
		http.Error(w, "central could not start checkout", http.StatusInternalServerError)
		return
	}
	if held {
		http.Error(w, "this plate is registered to another owner", http.StatusConflict)
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

	session, err := b.payments.Client.CreateCheckoutSession(r.Context(), stripe.CheckoutRequest{
		CustomerID: body.CustomerID,
		Plate:      plate,
		AmountOre:  amountOre,
		SuccessURL: b.payments.SuccessURL,
		CancelURL:  b.payments.CancelURL,
	})
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
