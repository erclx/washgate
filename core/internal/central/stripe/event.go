package stripe

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// The event types central acts on. Every other type decodes to its envelope alone.
const (
	EventInvoicePaid         = "invoice.paid"
	EventSubscriptionDeleted = "customer.subscription.deleted"
)

// ErrIncompleteEvent reports an event missing a field central needs, which usually means an API version mismatch.
var ErrIncompleteEvent = errors.New("incomplete Stripe event")

// Event is a webhook event's envelope and, for a handled type, its decoded object.
type Event struct {
	ID                  string
	Type                string
	InvoicePaid         *InvoicePaid
	SubscriptionDeleted *SubscriptionDeleted
}

// InvoicePaid is a paid subscription invoice: the subscription it pays for, the metadata
// checkout attached to it, and the end of the period it covers.
type InvoicePaid struct {
	SubscriptionID string
	CustomerID     string
	Plate          string
	PeriodEnd      time.Time
}

// SubscriptionDeleted is a subscription that has ended.
type SubscriptionDeleted struct {
	SubscriptionID string
}

type envelope struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Data struct {
		Object json.RawMessage `json:"object"`
	} `json:"data"`
}

type subscriptionMetadata struct {
	CustomerID string `json:"customer_id"`
	Plate      string `json:"plate"`
}

type invoiceObject struct {
	Parent struct {
		SubscriptionDetails struct {
			Subscription string               `json:"subscription"`
			Metadata     subscriptionMetadata `json:"metadata"`
		} `json:"subscription_details"`
	} `json:"parent"`
	Lines struct {
		Data []struct {
			Period struct {
				End int64 `json:"end"`
			} `json:"period"`
		} `json:"data"`
	} `json:"lines"`
}

type subscriptionObject struct {
	ID string `json:"id"`
}

// DecodeEvent reads a webhook payload in the shape of APIVersion.
func DecodeEvent(payload []byte) (Event, error) {
	var raw envelope
	if err := json.Unmarshal(payload, &raw); err != nil {
		return Event{}, fmt.Errorf("%w: %w", ErrIncompleteEvent, err)
	}
	if raw.ID == "" || raw.Type == "" {
		return Event{}, fmt.Errorf("%w: no id or type", ErrIncompleteEvent)
	}
	event := Event{ID: raw.ID, Type: raw.Type}

	switch raw.Type {
	case EventInvoicePaid:
		var invoice invoiceObject
		if err := json.Unmarshal(raw.Data.Object, &invoice); err != nil {
			return Event{}, fmt.Errorf("%w: invoice: %w", ErrIncompleteEvent, err)
		}
		details := invoice.Parent.SubscriptionDetails
		if details.Subscription == "" || details.Metadata.CustomerID == "" || details.Metadata.Plate == "" ||
			len(invoice.Lines.Data) == 0 || invoice.Lines.Data[0].Period.End == 0 {
			return Event{}, fmt.Errorf("%w: invoice without subscription details or a line period", ErrIncompleteEvent)
		}
		event.InvoicePaid = &InvoicePaid{
			SubscriptionID: details.Subscription,
			CustomerID:     details.Metadata.CustomerID,
			Plate:          details.Metadata.Plate,
			PeriodEnd:      time.Unix(invoice.Lines.Data[0].Period.End, 0).UTC(),
		}
	case EventSubscriptionDeleted:
		var subscription subscriptionObject
		if err := json.Unmarshal(raw.Data.Object, &subscription); err != nil {
			return Event{}, fmt.Errorf("%w: subscription: %w", ErrIncompleteEvent, err)
		}
		if subscription.ID == "" {
			return Event{}, fmt.Errorf("%w: subscription without an id", ErrIncompleteEvent)
		}
		event.SubscriptionDeleted = &SubscriptionDeleted{SubscriptionID: subscription.ID}
	}
	return event, nil
}
