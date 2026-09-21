package siteagent

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net/http"
	"strings"
	"time"
)

// maxReadBodyBytes holds the plate reader's 10 MiB photo cap after base64, with room for the read beside it.
const maxReadBodyBytes = 14 << 20

// Config holds what the router needs beyond the store: the site's id, thresholds, the site's time zone,
// a clock, the lane feed every decision is published to, and the switch that cuts the link to central.
type Config struct {
	SiteID   string
	Policy   Policy
	Location *time.Location
	Now      func() time.Time
	Feed     *Feed
	Link     *LinkSwitch
}

// NewRouter returns the site agent's HTTP handler.
func NewRouter(store *Store, config Config) http.Handler {
	lane := &lane{store: store, config: config}
	staff := &staffDesk{lane: lane}
	site := &siteStatus{store: store, config: config}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", handleHealth)
	mux.HandleFunc("POST /reads", lane.handleRead)
	mux.HandleFunc("GET /lane/events", lane.handleEvents)
	mux.HandleFunc("GET /lane/photos/{id}", lane.handlePhoto)
	mux.HandleFunc("POST /lane/decisions/{id}/confirm", staff.handleConfirm)
	mux.HandleFunc("POST /lane/decisions/{id}/send-to-pay", staff.handleSendToPay)
	mux.HandleFunc("GET /status", site.handleStatus)
	mux.HandleFunc("PUT /site/link", site.handleLink)
	return mux
}

type lane struct {
	store  *Store
	config Config
}

type readRequest struct {
	Plate      *string  `json:"plate"`
	Confidence *float64 `json:"confidence"`
	// Photo is the lane photo as base64 JPEG. It is held in memory for the dashboard and never stored.
	Photo *string `json:"photo"`
}

type readResponse struct {
	DecisionID string  `json:"decision_id"`
	Decision   Outcome `json:"decision"`
	Reason     Reason  `json:"reason"`
	WashID     string  `json:"wash_id,omitempty"`
}

// laneRun is one pass of the lane over a read: its decision, the plate and facts it decided on, and what the backend did.
type laneRun struct {
	decidedAt time.Time
	decision  Decision
	plate     string
	facts     Facts
	trace     []TraceStep
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok"))
}

// decodeJSONBody reads one JSON value of at most maxBytes into into, answering the client itself and
// reporting false when the body is not one.
func decodeJSONBody(w http.ResponseWriter, r *http.Request, maxBytes int64, into any) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		http.Error(w, "body must be application/json", http.StatusUnsupportedMediaType)
		return false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBytes))
	err = decoder.Decode(into)
	if maxBytesErr := (*http.MaxBytesError)(nil); errors.As(err, &maxBytesErr) {
		http.Error(w, fmt.Sprintf("body is over %d bytes", maxBytes), http.StatusRequestEntityTooLarge)
		return false
	}
	if err != nil || decoder.More() {
		http.Error(w, "body is not valid JSON of the expected shape", http.StatusBadRequest)
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

func (l *lane) handleRead(w http.ResponseWriter, r *http.Request) {
	var body readRequest
	if !decodeJSONBody(w, r, maxReadBodyBytes, &body) {
		return
	}
	if body.Plate == nil || body.Confidence == nil || *body.Confidence < 0 || *body.Confidence > 1 {
		http.Error(w, "a read needs a plate and a confidence from 0 to 1", http.StatusBadRequest)
		return
	}
	var photo []byte
	if body.Photo != nil {
		var err error
		photo, err = base64.StdEncoding.DecodeString(*body.Photo)
		if err != nil {
			http.Error(w, "photo must be base64", http.StatusBadRequest)
			return
		}
	}

	read := Read{Plate: *body.Plate, Confidence: *body.Confidence}
	run, err := l.decide(r.Context(), read)
	if err != nil {
		slog.Error("lane decision failed", "error", err)
		http.Error(w, "the site could not decide this read", http.StatusInternalServerError)
		return
	}
	decision := l.laneDecision(rand.Text(), read, run)
	l.config.Feed.Publish(decision, photo)
	slog.Info("lane decision", "decision_id", decision.ID, "decision", decision.Outcome, "reason", decision.Reason,
		"wash_id", run.decision.WashID)

	writeJSON(w, readResponse{
		DecisionID: decision.ID, Decision: run.decision.Outcome, Reason: run.decision.Reason, WashID: run.decision.WashID,
	})
}

func (l *lane) decide(ctx context.Context, read Read) (laneRun, error) {
	now := l.config.Now()
	run := laneRun{decidedAt: now, trace: skippedTrace()}
	plate, isWellFormed := NormalizePlate(read.Plate)
	if isWellFormed {
		run.plate = plate
		startedAt := time.Now()
		facts, err := l.store.Facts(ctx, plate, MonthStart(now, l.config.Location))
		if err != nil {
			return laneRun{}, err
		}
		run.facts = facts
		run.trace[0] = TraceStep{Step: TraceEntitlementLookup, Status: TraceDone, DurationMS: millisecondsSince(startedAt)}
	}

	run.decision = Decide(read, run.facts, l.config.Policy, now)
	if run.decision.Outcome != OutcomeAdmit || run.decision.Reason == ReasonDuplicateRead {
		return run, nil
	}

	entitlement, prepaidWashID := run.facts.Entitlement, ""
	if run.decision.Reason == ReasonPrepaidWash {
		entitlement, prepaidWashID = Entitlement{Plan: PlanPrepaid}, run.facts.PrepaidWashID
	}
	startedAt := time.Now()
	wash, isDuplicate, err := l.store.RecordWash(ctx, plate, entitlement, prepaidWashID, now, l.config.Policy.DedupWindow)
	if err != nil {
		return laneRun{}, err
	}
	if prepaidWashID != "" && !isDuplicate {
		slog.Info("prepaid wash spent", "site_id", l.config.SiteID, "prepaid_wash_id", prepaidWashID, "wash_id", wash.ID)
	}
	run.decision.WashID = wash.ID
	if isDuplicate {
		run.decision.Reason = ReasonDuplicateRead
		return run, nil
	}
	// The outbox entry is written in the ledger's own transaction, so its time is inside the ledger write's.
	run.trace[1] = TraceStep{Step: TraceLedgerWrite, Status: TraceDone, DurationMS: millisecondsSince(startedAt)}
	run.trace[2] = TraceStep{Step: TraceOutboxEntry, Status: TraceDone}
	run.trace[3] = TraceStep{Step: TraceSyncToHQ, Status: TraceQueued}
	return run, nil
}

// laneDecision is what the dashboard shows for run, under id.
func (l *lane) laneDecision(id string, read Read, run laneRun) LaneDecision {
	decision := LaneDecision{
		ID:         id,
		DecidedAt:  run.decidedAt.UTC(),
		Plate:      run.plate,
		Confidence: read.Confidence,
		Outcome:    run.decision.Outcome,
		Reason:     run.decision.Reason,
		Cutoff:     l.config.Policy.MinConfidence,
		Trace:      run.trace,
	}
	if decision.Plate == "" {
		decision.Plate = strings.TrimSpace(read.Plate)
	}
	if run.decision.WashID != "" {
		decision.WashID = &run.decision.WashID
	}
	if run.decision.Reason == ReasonWithinCap {
		washNumber := run.facts.WashesThisMonth + 1
		decision.WashNumber = &washNumber
	}
	if run.facts.Entitlement.Plan == PlanFleet && run.facts.CompanyName != "" {
		decision.CompanyName = &run.facts.CompanyName
	}
	return decision
}

// handleEvents streams the lane feed as server-sent events until the dashboard leaves or the feed closes.
func (l *lane) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "this connection cannot stream", http.StatusInternalServerError)
		return
	}
	events := l.config.Feed.Subscribe(r.Context())
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	// Tells a proxy such as the dashboard's nginx to pass each event on rather than buffer the stream.
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	for event := range events {
		body, err := json.Marshal(event.Body)
		if err != nil {
			slog.Error("lane event not encodable", "event", event.Type, "error", err)
			return
		}
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, body); err != nil {
			return
		}
		flusher.Flush()
	}
}

func (l *lane) handlePhoto(w http.ResponseWriter, r *http.Request) {
	photo, ok := l.config.Feed.Photo(r.PathValue("id"))
	if !ok {
		http.Error(w, "no photo is held for that decision", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(photo)
}

func millisecondsSince(startedAt time.Time) float64 {
	return float64(time.Since(startedAt).Microseconds()) / 1000
}
