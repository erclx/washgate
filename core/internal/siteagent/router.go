package siteagent

import (
	"context"
	"encoding/json"
	"log/slog"
	"mime"
	"net/http"
	"time"
)

const maxReadBodyBytes = 4 << 10

// Config holds what the router needs beyond the store: the site's id, thresholds, the site's time zone, and a clock.
type Config struct {
	SiteID   string
	Policy   Policy
	Location *time.Location
	Now      func() time.Time
}

// NewRouter returns the site agent's HTTP handler.
func NewRouter(store *Store, config Config) http.Handler {
	lane := &lane{store: store, config: config}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", handleHealth)
	mux.HandleFunc("POST /reads", lane.handleRead)
	mux.HandleFunc("GET /status", lane.handleStatus)
	return mux
}

type statusResponse struct {
	SiteID       string     `json:"site_id"`
	OutboxDepth  int        `json:"outbox_depth"`
	LastSyncedAt *time.Time `json:"last_synced_at"`
}

// handleStatus reports what only the site knows about its link to central: the washes central has not
// acknowledged and when the copy last pulled.
func (l *lane) handleStatus(w http.ResponseWriter, r *http.Request) {
	depth, err := l.store.OutboxDepth(r.Context())
	if err != nil {
		slog.Error("site status failed", "site_id", l.config.SiteID, "error", err)
		http.Error(w, "the site could not read its status", http.StatusInternalServerError)
		return
	}
	lastPulledAt, err := l.store.LastPulledAt(r.Context())
	if err != nil {
		slog.Error("site status failed", "site_id", l.config.SiteID, "error", err)
		http.Error(w, "the site could not read its status", http.StatusInternalServerError)
		return
	}
	response := statusResponse{SiteID: l.config.SiteID, OutboxDepth: depth}
	if !lastPulledAt.IsZero() {
		response.LastSyncedAt = &lastPulledAt
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

type lane struct {
	store  *Store
	config Config
}

type readRequest struct {
	Plate      *string  `json:"plate"`
	Confidence *float64 `json:"confidence"`
}

type readResponse struct {
	Decision Outcome `json:"decision"`
	Reason   Reason  `json:"reason"`
	WashID   string  `json:"wash_id,omitempty"`
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok"))
}

func (l *lane) handleRead(w http.ResponseWriter, r *http.Request) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		http.Error(w, "body must be application/json", http.StatusUnsupportedMediaType)
		return
	}
	var body readRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxReadBodyBytes))
	if err := decoder.Decode(&body); err != nil || decoder.More() {
		http.Error(w, "body is not a valid read", http.StatusBadRequest)
		return
	}
	if body.Plate == nil || body.Confidence == nil || *body.Confidence < 0 || *body.Confidence > 1 {
		http.Error(w, "a read needs a plate and a confidence from 0 to 1", http.StatusBadRequest)
		return
	}

	decision, err := l.decide(r.Context(), Read{Plate: *body.Plate, Confidence: *body.Confidence})
	if err != nil {
		slog.Error("lane decision failed", "error", err)
		http.Error(w, "the site could not decide this read", http.StatusInternalServerError)
		return
	}
	slog.Info("lane decision", "decision", decision.Outcome, "reason", decision.Reason, "wash_id", decision.WashID)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(readResponse{Decision: decision.Outcome, Reason: decision.Reason, WashID: decision.WashID})
}

func (l *lane) decide(ctx context.Context, read Read) (Decision, error) {
	now := l.config.Now()
	plate, isWellFormed := NormalizePlate(read.Plate)
	var facts Facts
	if isWellFormed {
		var err error
		facts, err = l.store.Facts(ctx, plate, MonthStart(now, l.config.Location))
		if err != nil {
			return Decision{}, err
		}
	}

	decision := Decide(read, facts, l.config.Policy, now)
	if decision.Outcome != OutcomeAdmit || decision.Reason == ReasonDuplicateRead {
		return decision, nil
	}

	wash, isDuplicate, err := l.store.RecordWash(ctx, plate, facts.Entitlement, now, l.config.Policy.DedupWindow)
	if err != nil {
		return Decision{}, err
	}
	decision.WashID = wash.ID
	if isDuplicate {
		decision.Reason = ReasonDuplicateRead
	}
	return decision, nil
}
