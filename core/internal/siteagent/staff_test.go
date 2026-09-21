package siteagent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type staffLane struct {
	router http.Handler
	feed   *Feed
	store  *Store
}

func newStaffLane(t *testing.T, store *Store) staffLane {
	t.Helper()
	feed := NewFeed()
	return staffLane{router: newFeedRouter(t, store, testNow, feed, &LinkSwitch{}), feed: feed, store: store}
}

// sendToStaff posts a read below the cutoff and returns the staff decision it opens.
func (l staffLane) sendToStaff(t *testing.T) string {
	t.Helper()
	answer := postRead(t, l.router, "application/json", `{"plate": "ABC 128", "confidence": 0.4}`)
	if answer.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body %q", answer.Code, http.StatusOK, answer.Body.String())
	}
	return decisionIDOf(t, answer)
}

func (l staffLane) post(t *testing.T, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	l.router.ServeHTTP(recorder, request)
	return recorder
}

func (l staffLane) confirm(t *testing.T, decisionID, plate string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]string{"plate": plate})
	if err != nil {
		t.Fatalf("encode confirm: %v", err)
	}
	return l.post(t, "/lane/decisions/"+decisionID+"/confirm", string(body))
}

func (l staffLane) sendToPay(t *testing.T, decisionID string) *httptest.ResponseRecorder {
	t.Helper()
	return l.post(t, "/lane/decisions/"+decisionID+"/send-to-pay", "")
}

func decodeStaffAnswer(t *testing.T, recorder *httptest.ResponseRecorder) LaneDecision {
	t.Helper()
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body %q", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var decision LaneDecision
	if err := json.NewDecoder(recorder.Body).Decode(&decision); err != nil {
		t.Fatalf("decode answer: %v", err)
	}
	return decision
}

func TestStaffConfirm(t *testing.T) {
	t.Run("confirming a Premium plate admits it once as confirmed by staff", func(t *testing.T) {
		lane := newStaffLane(t, openSeededStore(t))
		decisionID := lane.sendToStaff(t)

		got := decodeStaffAnswer(t, lane.confirm(t, decisionID, "ABC 123"))

		if got.ID != decisionID || got.Outcome != OutcomeAdmit || got.Reason != ReasonConfirmedByStaff || got.WashID == nil {
			t.Errorf("answer = %+v, want admit %s with a wash id", got, ReasonConfirmedByStaff)
		}
		if washes := countWashes(t, lane.store); washes != 1 {
			t.Errorf("washes recorded = %d, want 1", washes)
		}
	})

	t.Run("confirming an unknown plate pays with its own reason", func(t *testing.T) {
		lane := newStaffLane(t, openSeededStore(t))
		decisionID := lane.sendToStaff(t)

		got := decodeStaffAnswer(t, lane.confirm(t, decisionID, "XYZ789"))

		if got.Outcome != OutcomePay || got.Reason != ReasonUnknownPlate {
			t.Errorf("answer = %s %s, want pay %s", got.Outcome, got.Reason, ReasonUnknownPlate)
		}
	})

	t.Run("a second confirm answers 409", func(t *testing.T) {
		lane := newStaffLane(t, openSeededStore(t))
		decisionID := lane.sendToStaff(t)
		decodeStaffAnswer(t, lane.confirm(t, decisionID, "ABC123"))

		got := lane.confirm(t, decisionID, "ABC123")

		if got.Code != http.StatusConflict {
			t.Errorf("status = %d, want %d", got.Code, http.StatusConflict)
		}
	})

	t.Run("a malformed typed plate answers 400 and leaves the decision open", func(t *testing.T) {
		lane := newStaffLane(t, openSeededStore(t))
		decisionID := lane.sendToStaff(t)

		got := lane.confirm(t, decisionID, "A")

		if got.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want %d", got.Code, http.StatusBadRequest)
		}
		if _, err := lane.feed.OpenDecision(decisionID); err != nil {
			t.Errorf("open decision: %v, want it still open", err)
		}
	})

	t.Run("an unknown decision answers 404", func(t *testing.T) {
		lane := newStaffLane(t, openSeededStore(t))

		got := lane.confirm(t, "dec-unknown", "ABC123")

		if got.Code != http.StatusNotFound {
			t.Errorf("status = %d, want %d", got.Code, http.StatusNotFound)
		}
	})

	t.Run("confirming an unknown plate on a copy past its offline limit stays open with the typed plate", func(t *testing.T) {
		lane := newStaffLane(t, openStore(t))
		decisionID := lane.sendToStaff(t)

		got := decodeStaffAnswer(t, lane.confirm(t, decisionID, "XYZ 789"))

		if got.Outcome != OutcomeStaff || got.Reason != ReasonUnknownPlateOffline || got.Plate != "XYZ789" {
			t.Errorf("answer = %s %s for %s, want staff %s for XYZ789", got.Outcome, got.Reason, got.Plate, ReasonUnknownPlateOffline)
		}
		if open, err := lane.feed.OpenDecision(decisionID); err != nil || open.Plate != "XYZ789" {
			t.Errorf("open decision = %+v, %v, want it open with XYZ789", open, err)
		}
	})
}

func TestStaffSendToPay(t *testing.T) {
	t.Run("resolves as pay and writes nothing", func(t *testing.T) {
		lane := newStaffLane(t, openSeededStore(t))
		decisionID := lane.sendToStaff(t)

		got := decodeStaffAnswer(t, lane.sendToPay(t, decisionID))

		if got.Outcome != OutcomePay || got.Reason != ReasonSentToPay {
			t.Errorf("answer = %s %s, want pay %s", got.Outcome, got.Reason, ReasonSentToPay)
		}
		if washes := countWashes(t, lane.store); washes != 0 {
			t.Errorf("washes recorded = %d, want 0", washes)
		}
	})

	t.Run("a resolved decision answers 409", func(t *testing.T) {
		lane := newStaffLane(t, openSeededStore(t))
		decisionID := lane.sendToStaff(t)
		decodeStaffAnswer(t, lane.sendToPay(t, decisionID))

		got := lane.sendToPay(t, decisionID)

		if got.Code != http.StatusConflict {
			t.Errorf("status = %d, want %d", got.Code, http.StatusConflict)
		}
	})
}
