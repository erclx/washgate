package siteagent

import (
	"errors"
	"log/slog"
	"net/http"
	"sync"
)

const maxStaffBodyBytes = 1 << 10

// staffDesk takes the staff's answer to a staff decision. It re-enters the lane rather than serving a read.
type staffDesk struct {
	lane *lane
	// mu runs one staff answer at a time, so two answers to one decision cannot both resolve it.
	mu sync.Mutex
}

type confirmRequest struct {
	Plate *string `json:"plate"`
}

// handleConfirm re-runs the decision on the plate staff typed, at full confidence. An admit carries
// confirmed_by_staff, a pay keeps its own reason, and a staff answer stays open with the typed plate.
func (s *staffDesk) handleConfirm(w http.ResponseWriter, r *http.Request) {
	var body confirmRequest
	if !decodeJSONBody(w, r, maxStaffBodyBytes, &body) {
		return
	}
	if body.Plate == nil {
		http.Error(w, "a confirm needs the plate staff read", http.StatusBadRequest)
		return
	}
	if _, isWellFormed := NormalizePlate(*body.Plate); !isWellFormed {
		http.Error(w, "the typed plate matches no accepted plate shape", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	open, ok := s.openDecision(w, r.PathValue("id"))
	if !ok {
		return
	}
	read := Read{Plate: *body.Plate, Confidence: 1}
	run, err := s.lane.decide(r.Context(), read)
	if err != nil {
		slog.Error("staff confirm failed", "decision_id", open.ID, "error", err)
		http.Error(w, "the site could not decide the confirmed plate", http.StatusInternalServerError)
		return
	}
	decision := s.lane.laneDecision(open.ID, read, run)
	decision.DecidedAt = open.DecidedAt
	if decision.Outcome == OutcomeStaff {
		s.answer(w, decision, s.lane.config.Feed.Update(decision))
		return
	}
	if decision.Outcome == OutcomeAdmit && decision.Reason != ReasonDuplicateRead {
		decision.Reason = ReasonConfirmedByStaff
	}
	s.answer(w, decision, s.lane.config.Feed.Resolve(decision))
}

// handleSendToPay answers a staff decision as pay without a lookup, writing nothing.
func (s *staffDesk) handleSendToPay(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	open, ok := s.openDecision(w, r.PathValue("id"))
	if !ok {
		return
	}
	decision := sentToPay(open)
	s.answer(w, decision, s.lane.config.Feed.Resolve(decision))
}

func (s *staffDesk) openDecision(w http.ResponseWriter, id string) (LaneDecision, bool) {
	open, err := s.lane.config.Feed.OpenDecision(id)
	switch {
	case errors.Is(err, ErrDecisionNotFound):
		http.Error(w, "no lane decision with that id", http.StatusNotFound)
		return LaneDecision{}, false
	case errors.Is(err, ErrDecisionSettled):
		http.Error(w, "that lane decision is already resolved", http.StatusConflict)
		return LaneDecision{}, false
	}
	return open, true
}

// answer reports the staff answer, or a conflict when the decision was settled while it ran,
// which only the open staff decision cap does.
func (s *staffDesk) answer(w http.ResponseWriter, decision LaneDecision, err error) {
	if err != nil {
		var washID string
		if decision.WashID != nil {
			washID = *decision.WashID
		}
		slog.Warn("staff answer lost to a settled decision", "decision_id", decision.ID, "wash_id", washID)
		http.Error(w, "that lane decision is already resolved", http.StatusConflict)
		return
	}
	slog.Info("staff answer", "decision_id", decision.ID, "decision", decision.Outcome, "reason", decision.Reason)
	writeJSON(w, decision)
}
