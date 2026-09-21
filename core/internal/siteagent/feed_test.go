package siteagent

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"
)

func buildLaneDecision(id string, outcome Outcome, reason Reason) LaneDecision {
	return LaneDecision{
		ID:         id,
		DecidedAt:  testNow,
		Plate:      "ABC123",
		Confidence: 0.995,
		Outcome:    outcome,
		Reason:     reason,
		Cutoff:     testPolicy().MinConfidence,
		Trace:      skippedTrace(),
	}
}

func admitted(id string) LaneDecision {
	return buildLaneDecision(id, OutcomeAdmit, ReasonWithinCap)
}

func sentToStaff(id string) LaneDecision {
	return buildLaneDecision(id, OutcomeStaff, ReasonLowConfidence)
}

var testPhoto = []byte{0xff, 0xd8, 0xff, 0xe0}

// drain reads every event already buffered on events without waiting for more.
func drain(events <-chan LaneEvent) []LaneEvent {
	var got []LaneEvent
	for {
		select {
		case event, ok := <-events:
			if !ok {
				return got
			}
			got = append(got, event)
		default:
			return got
		}
	}
}

func eventIDs(events []LaneEvent) []string {
	ids := make([]string, 0, len(events))
	for _, event := range events {
		ids = append(ids, fmt.Sprintf("%s:%s", event.Type, event.Body.(LaneDecision).ID))
	}
	return ids
}

func hasPhoto(feed *Feed, id string) bool {
	_, ok := feed.Photo(id)
	return ok
}

func TestFeedSubscribers(t *testing.T) {
	t.Run("a subscriber receives a published decision", func(t *testing.T) {
		feed := NewFeed()
		events := feed.Subscribe(t.Context())

		feed.Publish(admitted("dec-1"), nil)

		if got := eventIDs(drain(events)); !slices.Equal(got, []string{"decision:dec-1"}) {
			t.Errorf("events = %v, want the published decision", got)
		}
	})

	t.Run("a new subscriber gets open staff decisions and no settled ones", func(t *testing.T) {
		feed := NewFeed()
		feed.Publish(admitted("dec-1"), nil)
		feed.Publish(sentToStaff("dec-2"), nil)
		feed.Publish(sentToStaff("dec-3"), nil)
		if err := feed.Resolve(sentToPay(sentToStaff("dec-3"))); err != nil {
			t.Fatalf("resolve: %v", err)
		}

		events := feed.Subscribe(t.Context())

		if got := eventIDs(drain(events)); !slices.Equal(got, []string{"decision:dec-2"}) {
			t.Errorf("events = %v, want only the open staff decision", got)
		}
	})

	t.Run("a subscriber that stops reading does not stall a publish", func(t *testing.T) {
		feed := NewFeed()
		stalled := feed.Subscribe(t.Context())
		published := make(chan struct{})

		go func() {
			for index := range subscriberBuffer * 2 {
				feed.Publish(admitted(fmt.Sprintf("dec-%d", index)), nil)
			}
			close(published)
		}()

		select {
		case <-published:
		case <-time.After(5 * time.Second):
			t.Fatal("publish blocked on a subscriber that stopped reading")
		}
		if got := len(drain(stalled)); got != subscriberBuffer {
			t.Errorf("stalled subscriber got %d events before being dropped, want %d", got, subscriberBuffer)
		}
	})

	t.Run("a subscriber whose context ends is closed", func(t *testing.T) {
		feed := NewFeed()
		ctx, cancel := context.WithCancel(t.Context())
		events := feed.Subscribe(ctx)

		cancel()

		select {
		case _, ok := <-events:
			if ok {
				t.Error("got an event, want the stream closed")
			}
		case <-time.After(5 * time.Second):
			t.Fatal("stream still open after its context ended")
		}
	})

	t.Run("closing the feed ends every stream", func(t *testing.T) {
		feed := NewFeed()
		events := feed.Subscribe(t.Context())

		feed.Close()

		if _, ok := <-events; ok {
			t.Error("got an event, want the stream closed")
		}
	})
}

func TestFeedMarkSynced(t *testing.T) {
	feed := NewFeed()
	events := feed.Subscribe(t.Context())

	feed.MarkSynced([]string{"wash-1", "wash-2"})

	got := drain(events)
	if len(got) != 1 || got[0].Type != EventSynced {
		t.Fatalf("events = %+v, want one synced event", got)
	}
	if body := got[0].Body.(SyncedBody); !slices.Equal(body.WashIDs, []string{"wash-1", "wash-2"}) {
		t.Errorf("synced washes = %v, want exactly wash-1 and wash-2", body.WashIDs)
	}
}

func TestFeedPhotos(t *testing.T) {
	t.Run("the current car's photo is held", func(t *testing.T) {
		feed := NewFeed()

		feed.Publish(admitted("dec-1"), testPhoto)

		if photo, ok := feed.Photo("dec-1"); !ok || !slices.Equal(photo, testPhoto) {
			t.Errorf("photo = %v, %v, want the published photo", photo, ok)
		}
	})

	t.Run("a photo is dropped once a newer decision replaces its car", func(t *testing.T) {
		feed := NewFeed()
		feed.Publish(admitted("dec-1"), testPhoto)

		feed.Publish(admitted("dec-2"), testPhoto)

		if hasPhoto(feed, "dec-1") {
			t.Error("photo of a replaced car is still held")
		}
	})

	t.Run("an open staff decision keeps its photo after a newer decision", func(t *testing.T) {
		feed := NewFeed()
		feed.Publish(sentToStaff("dec-1"), testPhoto)

		feed.Publish(admitted("dec-2"), testPhoto)

		if !hasPhoto(feed, "dec-1") {
			t.Error("photo of an open staff decision was dropped")
		}
	})

	t.Run("a resolved staff decision drops its photo", func(t *testing.T) {
		feed := NewFeed()
		feed.Publish(sentToStaff("dec-1"), testPhoto)
		feed.Publish(admitted("dec-2"), testPhoto)

		if err := feed.Resolve(sentToPay(sentToStaff("dec-1"))); err != nil {
			t.Fatalf("resolve: %v", err)
		}

		if hasPhoto(feed, "dec-1") {
			t.Error("photo of a resolved staff decision is still held")
		}
	})

	t.Run("a decision published with a photo says so", func(t *testing.T) {
		feed := NewFeed()
		events := feed.Subscribe(t.Context())

		feed.Publish(admitted("dec-1"), testPhoto)

		if got := drain(events); len(got) != 1 || !got[0].Body.(LaneDecision).HasPhoto {
			t.Errorf("events = %+v, want one decision with has_photo", got)
		}
	})
}

func TestFeedStaffDecisions(t *testing.T) {
	t.Run("an open staff decision is found by its id", func(t *testing.T) {
		feed := NewFeed()
		feed.Publish(sentToStaff("dec-1"), nil)

		got, err := feed.OpenDecision("dec-1")

		if err != nil || got.ID != "dec-1" {
			t.Errorf("open decision = %+v, %v, want dec-1", got, err)
		}
	})

	t.Run("an unknown id is not found", func(t *testing.T) {
		_, err := NewFeed().OpenDecision("dec-9")

		if !errors.Is(err, ErrDecisionNotFound) {
			t.Errorf("error = %v, want %v", err, ErrDecisionNotFound)
		}
	})

	t.Run("a decision that is not open reports settled", func(t *testing.T) {
		feed := NewFeed()
		feed.Publish(admitted("dec-1"), nil)

		_, err := feed.OpenDecision("dec-1")

		if !errors.Is(err, ErrDecisionSettled) {
			t.Errorf("error = %v, want %v", err, ErrDecisionSettled)
		}
	})

	t.Run("resolving a decision twice reports settled", func(t *testing.T) {
		feed := NewFeed()
		feed.Publish(sentToStaff("dec-1"), nil)
		if err := feed.Resolve(sentToPay(sentToStaff("dec-1"))); err != nil {
			t.Fatalf("first resolve: %v", err)
		}

		err := feed.Resolve(sentToPay(sentToStaff("dec-1")))

		if !errors.Is(err, ErrDecisionSettled) {
			t.Errorf("error = %v, want %v", err, ErrDecisionSettled)
		}
	})

	t.Run("a staff decision past the cap sends the oldest to pay", func(t *testing.T) {
		feed := NewFeed()
		for index := range maxOpenStaffDecisions {
			feed.Publish(sentToStaff(fmt.Sprintf("dec-%d", index)), nil)
		}
		events := feed.Subscribe(t.Context())
		drain(events)

		feed.Publish(sentToStaff("dec-new"), nil)

		got := drain(events)
		if ids := eventIDs(got); !slices.Equal(ids, []string{"decision:dec-new", "resolved:dec-0"}) {
			t.Fatalf("events = %v, want the new decision and the oldest resolved", ids)
		}
		if resolved := got[1].Body.(LaneDecision); resolved.Outcome != OutcomePay || resolved.Reason != ReasonSentToPay {
			t.Errorf("oldest resolved as %s %s, want pay %s", resolved.Outcome, resolved.Reason, ReasonSentToPay)
		}
	})
}
