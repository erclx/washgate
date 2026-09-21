package central

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/erclx/washgate/core/internal/central/stripe"
)

const maxWebhookBodyBytes = 1 << 20

func (b *billing) handlePostStripeWebhook(w http.ResponseWriter, r *http.Request) {
	if b.payments == nil {
		http.Error(w, "payments are not configured", http.StatusServiceUnavailable)
		return
	}
	payload, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxWebhookBodyBytes))
	if err != nil {
		http.Error(w, "the body is unreadable or over 1 MiB", http.StatusBadRequest)
		return
	}
	if err := stripe.VerifySignature(payload, r.Header.Get("Stripe-Signature"), b.payments.WebhookSecret, time.Now()); err != nil {
		slog.Warn("stripe event refused", "reason", err.Error())
		http.Error(w, "the Stripe signature does not verify", http.StatusBadRequest)
		return
	}
	event, err := stripe.DecodeEvent(payload)
	if err != nil {
		slog.Warn("stripe event rejected", "reason", err.Error())
		http.Error(w, "the event is missing fields central needs", http.StatusBadRequest)
		return
	}

	outcome, err := b.store.ApplySubscriptionEvent(r.Context(), subscriptionEvent(event))
	if errors.Is(err, ErrUnknownCustomer) || errors.Is(err, ErrInvalidEntitlement) {
		slog.Warn("stripe event rejected", "event_id", event.ID, "event_type", event.Type, "reason", err.Error())
		http.Error(w, "the event names a customer or plate central cannot apply", http.StatusUnprocessableEntity)
		return
	}
	if err != nil {
		slog.Error("stripe event failed", "event_id", event.ID, "event_type", event.Type, "error", err)
		http.Error(w, "central could not apply this event", http.StatusInternalServerError)
		return
	}
	slog.Info("stripe event handled", "event_id", event.ID, "event_type", event.Type, "outcome", string(outcome))
	w.WriteHeader(http.StatusOK)
}

func subscriptionEvent(event stripe.Event) SubscriptionEvent {
	converted := SubscriptionEvent{EventID: event.ID, EventType: event.Type, Action: SubscriptionUnchanged}
	switch {
	case event.InvoicePaid != nil:
		converted.Action = SubscriptionPaid
		converted.StripeSubscriptionID = event.InvoicePaid.SubscriptionID
		converted.CustomerID = event.InvoicePaid.CustomerID
		converted.Plate = event.InvoicePaid.Plate
		converted.PeriodEnd = event.InvoicePaid.PeriodEnd
	case event.SubscriptionDeleted != nil:
		converted.Action = SubscriptionEnded
		converted.StripeSubscriptionID = event.SubscriptionDeleted.SubscriptionID
	}
	return converted
}
