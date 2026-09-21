package stripe

import (
	"errors"
	"os"
	"testing"
	"time"
)

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	payload, err := os.ReadFile("testdata/" + name) //nolint:gosec // the name is a fixture the test itself chose
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return payload
}

func TestDecodeEvent(t *testing.T) {
	t.Run("a paid invoice gives its subscription, metadata, and first line's period end", func(t *testing.T) {
		payload := readFixture(t, "invoice_paid.json")

		event, err := DecodeEvent(payload)

		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		want := InvoicePaid{
			SubscriptionID: "sub_test_0001",
			CustomerID:     "customer-anna",
			Plate:          "ABC123",
			PeriodEnd:      time.Unix(1792654200, 0).UTC(),
		}
		if event.ID != "evt_test_invoice_paid_0001" || event.Type != EventInvoicePaid {
			t.Fatalf("envelope = %s %s, want evt_test_invoice_paid_0001 %s", event.ID, event.Type, EventInvoicePaid)
		}
		if event.InvoicePaid == nil || *event.InvoicePaid != want {
			t.Fatalf("invoice paid = %+v, want %+v", event.InvoicePaid, want)
		}
	})

	t.Run("a deleted subscription gives its id", func(t *testing.T) {
		payload := readFixture(t, "customer_subscription_deleted.json")

		event, err := DecodeEvent(payload)

		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if event.Type != EventSubscriptionDeleted {
			t.Fatalf("type = %s, want %s", event.Type, EventSubscriptionDeleted)
		}
		if event.SubscriptionDeleted == nil || event.SubscriptionDeleted.SubscriptionID != "sub_test_0001" {
			t.Fatalf("subscription deleted = %+v, want sub_test_0001", event.SubscriptionDeleted)
		}
	})

	t.Run("an unhandled type decodes to its envelope alone", func(t *testing.T) {
		payload := readFixture(t, "checkout_session_completed.json")

		event, err := DecodeEvent(payload)

		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if event.ID != "evt_test_checkout_completed_0001" || event.Type != "checkout.session.completed" {
			t.Fatalf("envelope = %s %s, want the checkout event", event.ID, event.Type)
		}
		if event.InvoicePaid != nil || event.SubscriptionDeleted != nil {
			t.Fatalf("event = %+v, want the envelope alone", event)
		}
	})

	t.Run("a paid invoice naming no subscription is rejected", func(t *testing.T) {
		payload := []byte(`{"id":"evt_1","type":"invoice.paid","data":{"object":{"id":"in_1","lines":{"data":[{"period":{"end":1792654200}}]}}}}`)

		_, err := DecodeEvent(payload)

		if !errors.Is(err, ErrIncompleteEvent) {
			t.Fatalf("error = %v, want ErrIncompleteEvent", err)
		}
	})

	t.Run("a body that is not an event is rejected", func(t *testing.T) {
		_, err := DecodeEvent([]byte(`{"type":"invoice.paid"}`))

		if !errors.Is(err, ErrIncompleteEvent) {
			t.Fatalf("error = %v, want ErrIncompleteEvent", err)
		}
	})
}
