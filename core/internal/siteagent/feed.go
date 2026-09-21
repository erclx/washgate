package siteagent

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"sync"
	"time"
)

const (
	// subscriberBuffer is how many events a dashboard may fall behind before it is dropped.
	subscriberBuffer = 64
	// maxOpenStaffDecisions bounds the staff decisions nobody has answered. A lane blocks long before it.
	maxOpenStaffDecisions = 20
	// maxSettledIDs is how many settled decisions the feed remembers, so a late staff answer reads as settled.
	maxSettledIDs = 256
)

// Errors a staff answer can meet.
var (
	ErrDecisionNotFound = errors.New("no lane decision with that id")
	ErrDecisionSettled  = errors.New("the lane decision is no longer open")
)

// TraceStepName names one step the backend took for a lane decision.
type TraceStepName string

// The steps of a lane decision, in the order they run.
const (
	TraceEntitlementLookup TraceStepName = "entitlement_lookup"
	TraceLedgerWrite       TraceStepName = "ledger_write"
	TraceOutboxEntry       TraceStepName = "outbox_entry"
	TraceSyncToHQ          TraceStepName = "sync_to_hq"
)

// TraceStatus says whether a step ran, was not needed, or waits on central.
type TraceStatus string

// Trace step statuses.
const (
	TraceDone    TraceStatus = "done"
	TraceSkipped TraceStatus = "skipped"
	TraceQueued  TraceStatus = "queued"
)

// TraceStep is one step of a decision's trace.
type TraceStep struct {
	Step       TraceStepName `json:"step"`
	Status     TraceStatus   `json:"status"`
	DurationMS float64       `json:"duration_ms"`
}

// LaneDecision is what the dashboard shows for one car. Its JSON is also the replay recording format.
type LaneDecision struct {
	ID          string      `json:"id"`
	DecidedAt   time.Time   `json:"decided_at"`
	Plate       string      `json:"plate"`
	Confidence  float64     `json:"confidence"`
	Outcome     Outcome     `json:"outcome"`
	Reason      Reason      `json:"reason"`
	WashID      *string     `json:"wash_id"`
	WashNumber  *int        `json:"wash_number"`
	CompanyName *string     `json:"company_name"`
	Cutoff      float64     `json:"cutoff"`
	HasPhoto    bool        `json:"has_photo"`
	Trace       []TraceStep `json:"trace"`
}

// LaneEventType names an event on the lane feed.
type LaneEventType string

// Lane feed events.
const (
	EventDecision LaneEventType = "decision"
	EventResolved LaneEventType = "resolved"
	EventSynced   LaneEventType = "synced"
)

// LaneEvent is one event on the lane feed. Body is a LaneDecision, or a SyncedBody for EventSynced.
type LaneEvent struct {
	Type LaneEventType
	Body any
}

// SyncedBody names the washes central has acknowledged.
type SyncedBody struct {
	WashIDs []string `json:"wash_ids"`
}

// Feed publishes the lane's decisions to every open dashboard, holds the staff decisions nobody
// has answered yet, and keeps the photo of the current car and of each open staff decision in memory only.
type Feed struct {
	mu          sync.Mutex
	subscribers map[chan LaneEvent]struct{}
	open        []LaneDecision
	settledIDs  []string
	photos      map[string][]byte
	currentID   string
	isClosed    bool
}

// NewFeed returns an empty feed.
func NewFeed() *Feed {
	return &Feed{subscribers: make(map[chan LaneEvent]struct{}), photos: make(map[string][]byte)}
}

// Publish makes decision the current car, holding photo while the car is current or waiting on staff.
func (f *Feed) Publish(decision LaneDecision, photo []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	previousID := f.currentID
	f.currentID = decision.ID
	decision.HasPhoto = len(photo) > 0
	if decision.HasPhoto {
		f.photos[decision.ID] = photo
	}
	if previousID != "" && f.openIndex(previousID) < 0 {
		delete(f.photos, previousID)
	}
	if decision.Outcome != OutcomeStaff {
		f.settle(decision.ID)
		f.broadcast(LaneEvent{Type: EventDecision, Body: decision})
		return
	}
	f.open = append(f.open, decision)
	f.broadcast(LaneEvent{Type: EventDecision, Body: decision})
	if len(f.open) > maxOpenStaffDecisions {
		oldest := sentToPay(f.open[0])
		slog.Warn("oldest staff decision sent to pay", "decision_id", oldest.ID, "open_limit", maxOpenStaffDecisions)
		f.resolve(0, oldest)
	}
}

// OpenDecision returns the open staff decision with id.
func (f *Feed) OpenDecision(id string) (LaneDecision, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if index := f.openIndex(id); index >= 0 {
		return f.open[index], nil
	}
	if slices.Contains(f.settledIDs, id) {
		return LaneDecision{}, ErrDecisionSettled
	}
	return LaneDecision{}, ErrDecisionNotFound
}

// Update replaces an open staff decision that stays open, such as one confirmed to a plate the copy is too old to vouch for.
func (f *Feed) Update(decision LaneDecision) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	index := f.openIndex(decision.ID)
	if index < 0 {
		return ErrDecisionSettled
	}
	decision.HasPhoto = f.photos[decision.ID] != nil
	f.open[index] = decision
	f.broadcast(LaneEvent{Type: EventDecision, Body: decision})
	return nil
}

// Resolve settles an open staff decision with its answer.
func (f *Feed) Resolve(decision LaneDecision) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	index := f.openIndex(decision.ID)
	if index < 0 {
		return ErrDecisionSettled
	}
	f.resolve(index, decision)
	return nil
}

// MarkSynced tells every dashboard that central has acknowledged washIDs.
func (f *Feed) MarkSynced(washIDs []string) {
	if len(washIDs) == 0 {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.broadcast(LaneEvent{Type: EventSynced, Body: SyncedBody{WashIDs: washIDs}})
}

// Subscribe returns a stream that opens with every open staff decision and then carries each event,
// until ctx ends, the subscriber falls subscriberBuffer events behind, or the feed closes.
func (f *Feed) Subscribe(ctx context.Context) <-chan LaneEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	events := make(chan LaneEvent, subscriberBuffer)
	if f.isClosed {
		close(events)
		return events
	}
	for _, decision := range f.open {
		events <- LaneEvent{Type: EventDecision, Body: decision}
	}
	f.subscribers[events] = struct{}{}
	context.AfterFunc(ctx, func() { f.unsubscribe(events) })
	return events
}

// Photo returns the photo held for decision id.
func (f *Feed) Photo(id string) ([]byte, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	photo, ok := f.photos[id]
	return photo, ok
}

// Close ends every stream and refuses new ones, so a server shutdown is not held open by a dashboard.
func (f *Feed) Close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.isClosed = true
	for events := range f.subscribers {
		close(events)
	}
	clear(f.subscribers)
}

func (f *Feed) unsubscribe(events chan LaneEvent) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.subscribers[events]; ok {
		delete(f.subscribers, events)
		close(events)
	}
}

// resolve removes the open decision at index, drops its photo unless it is the current car, and broadcasts the answer.
func (f *Feed) resolve(index int, decision LaneDecision) {
	f.open = slices.Delete(f.open, index, index+1)
	f.settle(decision.ID)
	if decision.ID != f.currentID {
		delete(f.photos, decision.ID)
	}
	decision.HasPhoto = f.photos[decision.ID] != nil
	f.broadcast(LaneEvent{Type: EventResolved, Body: decision})
}

func (f *Feed) settle(id string) {
	f.settledIDs = append(f.settledIDs, id)
	if len(f.settledIDs) > maxSettledIDs {
		f.settledIDs = slices.Delete(f.settledIDs, 0, len(f.settledIDs)-maxSettledIDs)
	}
}

// broadcast never waits on a subscriber. One that has fallen behind is dropped, so a stalled tab cannot stop the lane.
func (f *Feed) broadcast(event LaneEvent) {
	for events := range f.subscribers {
		select {
		case events <- event:
		default:
			delete(f.subscribers, events)
			close(events)
			slog.Warn("lane feed subscriber dropped", "buffer", subscriberBuffer)
		}
	}
}

func (f *Feed) openIndex(id string) int {
	return slices.IndexFunc(f.open, func(decision LaneDecision) bool { return decision.ID == id })
}

// sentToPay answers a staff decision as pay without a lookup.
func sentToPay(decision LaneDecision) LaneDecision {
	decision.Outcome = OutcomePay
	decision.Reason = ReasonSentToPay
	decision.WashID, decision.WashNumber, decision.CompanyName = nil, nil, nil
	decision.Trace = skippedTrace()
	return decision
}

// skippedTrace is the trace of a decision that looked nothing up and wrote nothing.
func skippedTrace() []TraceStep {
	return []TraceStep{
		{Step: TraceEntitlementLookup, Status: TraceSkipped},
		{Step: TraceLedgerWrite, Status: TraceSkipped},
		{Step: TraceOutboxEntry, Status: TraceSkipped},
		{Step: TraceSyncToHQ, Status: TraceSkipped},
	}
}
