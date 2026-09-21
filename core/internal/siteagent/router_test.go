package siteagent

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

type readAnswer struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
	WashID   string `json:"wash_id"`
}

func newTestRouter(t *testing.T, store *Store, now time.Time) http.Handler {
	t.Helper()
	return newFeedRouter(t, store, now, NewFeed(), &LinkSwitch{})
}

func newFeedRouter(t *testing.T, store *Store, now time.Time, feed *Feed, link *LinkSwitch) http.Handler {
	t.Helper()
	stockholm, err := time.LoadLocation("Europe/Stockholm")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	return NewRouter(store, Config{
		SiteID:   testSiteID,
		Policy:   testPolicy(),
		Location: stockholm,
		Now:      func() time.Time { return now },
		Feed:     feed,
		Link:     link,
	})
}

func postRead(t *testing.T, router http.Handler, contentType, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/reads", strings.NewReader(body))
	request.Header.Set("Content-Type", contentType)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

func decodeAnswer(t *testing.T, recorder *httptest.ResponseRecorder) readAnswer {
	t.Helper()
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body %q", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var answer readAnswer
	if err := json.NewDecoder(recorder.Body).Decode(&answer); err != nil {
		t.Fatalf("decode answer: %v", err)
	}
	return answer
}

func recordFullMonth(t *testing.T, store *Store, plate string, from time.Time) {
	t.Helper()
	for wash := range PremiumMonthlyCap {
		recordWash(t, store, plate, from.Add(time.Duration(wash)*2*testWindow))
	}
}

func TestReadsAnswersEachDecision(t *testing.T) {
	cases := []struct {
		name string
		body string
		want readAnswer
	}{
		{
			name: "unknown plate pays",
			body: `{"plate": "XYZ 789", "confidence": 0.995}`,
			want: readAnswer{Decision: "pay", Reason: "unknown_plate"},
		},
		{
			name: "low confidence goes to staff",
			body: `{"plate": "ABC 123", "confidence": 0.5}`,
			want: readAnswer{Decision: "staff", Reason: "low_confidence"},
		},
		{
			name: "malformed plate goes to staff",
			body: `{"plate": "A", "confidence": 1}`,
			want: readAnswer{Decision: "staff", Reason: "malformed_plate"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router := newTestRouter(t, openSeededStore(t), testNow)

			got := decodeAnswer(t, postRead(t, router, "application/json", tc.body))

			if got != tc.want {
				t.Errorf("answer = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestReadsSendsAnUnknownPlateToStaffUntilTheFirstPull(t *testing.T) {
	router := newTestRouter(t, openStore(t), testNow)

	got := decodeAnswer(t, postRead(t, router, "application/json", `{"plate": "XYZ 789", "confidence": 0.995}`))

	want := readAnswer{Decision: "staff", Reason: "unknown_plate_offline"}
	if got != want {
		t.Errorf("answer = %+v, want %+v", got, want)
	}
}

func TestReadsAdmitsAFleetCarWithAWashID(t *testing.T) {
	router := newTestRouter(t, openSeededStore(t), testNow)

	got := decodeAnswer(t, postRead(t, router, "application/json", `{"plate": "KLM-456", "confidence": 1}`))

	if got.Decision != "admit" || got.Reason != "fleet" || got.WashID == "" {
		t.Errorf("answer = %+v, want admit fleet with a wash id", got)
	}
}

func TestReadsAcceptsFieldsItDoesNotUse(t *testing.T) {
	router := newTestRouter(t, openSeededStore(t), testNow)
	body := `{"plate": "ABC123", "confidence": 1, "box": [10, 20, 110, 60]}`

	got := decodeAnswer(t, postRead(t, router, "application/json; charset=utf-8", body))

	if got.Decision != "admit" || got.Reason != "within_cap" {
		t.Errorf("answer = %+v, want admit within_cap", got)
	}
}

func TestReadsCountsASecondReadInsideTheWindowOnce(t *testing.T) {
	router := newTestRouter(t, openSeededStore(t), testNow)
	body := `{"plate": "ABC123", "confidence": 1}`
	first := decodeAnswer(t, postRead(t, router, "application/json", body))

	second := decodeAnswer(t, postRead(t, router, "application/json", body))

	want := readAnswer{Decision: "admit", Reason: "duplicate_read", WashID: first.WashID}
	if first.WashID == "" || second != want {
		t.Errorf("first = %+v, second = %+v, want second %+v", first, second, want)
	}
}

func postReadsAtOnce(t *testing.T, router http.Handler, body string, count int) []readAnswer {
	t.Helper()
	recorders := make([]*httptest.ResponseRecorder, count)
	var group sync.WaitGroup
	for index := range recorders {
		recorders[index] = httptest.NewRecorder()
		request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/reads", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		group.Go(func() { router.ServeHTTP(recorders[index], request) })
	}
	group.Wait()
	answers := make([]readAnswer, count)
	for index, recorder := range recorders {
		answers[index] = decodeAnswer(t, recorder)
	}
	return answers
}

func distinctWashIDs(answers []readAnswer) map[string]bool {
	washIDs := make(map[string]bool)
	for _, answer := range answers {
		washIDs[answer.WashID] = true
	}
	return washIDs
}

func countWashes(t *testing.T, store *Store) int {
	t.Helper()
	var washes int
	if err := store.db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM washes").Scan(&washes); err != nil {
		t.Fatalf("count washes: %v", err)
	}
	return washes
}

func TestReadsArrivingAtOnceShareOneWash(t *testing.T) {
	store := openSeededStore(t)
	router := newTestRouter(t, store, testNow)

	answers := postReadsAtOnce(t, router, `{"plate": "ABC123", "confidence": 1}`, 8)

	washIDs := distinctWashIDs(answers)
	if len(washIDs) != 1 || washIDs[""] {
		t.Errorf("wash ids = %v, want one shared non-empty id", washIDs)
	}
	if got := countWashes(t, store); got != 1 {
		t.Errorf("washes recorded = %d, want 1", got)
	}
}

func TestReadsCountsTheCapInTheSiteMonth(t *testing.T) {
	store := openSeededStore(t)
	recordFullMonth(t, store, "ABC123", time.Date(2026, time.January, 31, 22, 0, 0, 0, time.UTC))
	justAfterMidnightInStockholm := time.Date(2026, time.January, 31, 23, 30, 0, 0, time.UTC)
	router := newTestRouter(t, store, justAfterMidnightInStockholm)

	got := decodeAnswer(t, postRead(t, router, "application/json", `{"plate": "ABC123", "confidence": 1}`))

	if got.Decision != "admit" || got.Reason != "within_cap" {
		t.Errorf("answer = %+v, want admit within_cap in the new month", got)
	}
}

func TestReadsRejectsABadRequest(t *testing.T) {
	cases := []struct {
		name        string
		contentType string
		body        string
		want        int
	}{
		{name: "missing plate", contentType: "application/json", body: `{"confidence": 1}`, want: http.StatusBadRequest},
		{name: "missing confidence", contentType: "application/json", body: `{"plate": "ABC123"}`, want: http.StatusBadRequest},
		{name: "confidence above one", contentType: "application/json", body: `{"plate": "ABC123", "confidence": 1.5}`, want: http.StatusBadRequest},
		{name: "negative confidence", contentType: "application/json", body: `{"plate": "ABC123", "confidence": -0.1}`, want: http.StatusBadRequest},
		{name: "broken JSON", contentType: "application/json", body: `{"plate": `, want: http.StatusBadRequest},
		{name: "trailing data after the read", contentType: "application/json", body: `{"plate": "ABC123", "confidence": 1} {}`, want: http.StatusBadRequest},
		{name: "not JSON", contentType: "text/plain", body: "ABC123", want: http.StatusUnsupportedMediaType},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router := newTestRouter(t, openSeededStore(t), testNow)

			got := postRead(t, router, tc.contentType, tc.body)

			if got.Code != tc.want {
				t.Errorf("status = %d, want %d", got.Code, tc.want)
			}
		})
	}
}

func TestHealthReportsOK(t *testing.T) {
	router := newTestRouter(t, openSeededStore(t), testNow)
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/health", nil)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
}

func TestReadsAdmitsACappedPlateAgainAfterAReset(t *testing.T) {
	store := openSeededStore(t)
	recordFullMonth(t, store, "ABC123", MonthStart(testNow, time.UTC).Add(time.Hour))
	applyChanges(t, store, resetChange(6, "ABC123", testNow.Add(-time.Minute)))
	router := newTestRouter(t, store, testNow)

	got := decodeAnswer(t, postRead(t, router, "application/json", `{"plate": "ABC123", "confidence": 1}`))

	if got.Decision != "admit" || got.Reason != "within_cap" {
		t.Errorf("answer = %+v, want admit within_cap after the reset", got)
	}
}

func TestReadsSpendsAPrepaidWashOnce(t *testing.T) {
	t.Run("the first read admits on it and a read after the window is told to pay", func(t *testing.T) {
		store := openSeededStore(t)
		applyChanges(t, store, grantedChange(6, "XYZ789", testPrepaidWashID))
		body := `{"plate": "XYZ789", "confidence": 1}`
		first := decodeAnswer(t, postRead(t, newTestRouter(t, store, testNow), "application/json", body))

		second := decodeAnswer(t, postRead(t, newTestRouter(t, store, testNow.Add(2*testWindow)), "application/json", body))

		if first.Decision != "admit" || first.Reason != "prepaid_wash" || first.WashID == "" {
			t.Errorf("first = %+v, want admit prepaid_wash with a wash id", first)
		}
		if want := (readAnswer{Decision: "pay", Reason: "unknown_plate"}); second != want {
			t.Errorf("second = %+v, want %+v", second, want)
		}
	})

	t.Run("a second read inside the window reuses the wash and spends nothing more", func(t *testing.T) {
		store := openSeededStore(t)
		applyChanges(t, store, grantedChange(6, "XYZ789", testPrepaidWashID), grantedChange(7, "XYZ789", "cs_test_second"))
		router := newTestRouter(t, store, testNow)
		body := `{"plate": "XYZ789", "confidence": 1}`
		first := decodeAnswer(t, postRead(t, router, "application/json", body))

		second := decodeAnswer(t, postRead(t, router, "application/json", body))

		if want := (readAnswer{Decision: "admit", Reason: "duplicate_read", WashID: first.WashID}); second != want {
			t.Errorf("second = %+v, want %+v", second, want)
		}
		if got := prepaidWashOf(t, store, "XYZ789"); got != "cs_test_second" {
			t.Errorf("prepaid wash left = %q, want cs_test_second", got)
		}
	})
}

func readWithPhoto(photo string) string {
	return fmt.Sprintf(`{"plate": "ABC123", "confidence": 1, "photo": %q}`, photo)
}

func getPhoto(t *testing.T, router http.Handler, decisionID string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/lane/photos/"+decisionID, nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

func decisionIDOf(t *testing.T, recorder *httptest.ResponseRecorder) string {
	t.Helper()
	var answer struct {
		DecisionID string `json:"decision_id"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&answer); err != nil {
		t.Fatalf("decode answer: %v", err)
	}
	if answer.DecisionID == "" {
		t.Fatal("answer carries no decision id")
	}
	return answer.DecisionID
}

func TestReadsHoldsTheLanePhoto(t *testing.T) {
	t.Run("a read with a photo is decided and its photo served", func(t *testing.T) {
		router := newTestRouter(t, openSeededStore(t), testNow)
		answer := postRead(t, router, "application/json", readWithPhoto(base64.StdEncoding.EncodeToString(testPhoto)))
		if answer.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d, body %q", answer.Code, http.StatusOK, answer.Body.String())
		}

		photo := getPhoto(t, router, decisionIDOf(t, answer))

		if photo.Code != http.StatusOK || photo.Header().Get("Content-Type") != "image/jpeg" || !bytes.Equal(photo.Body.Bytes(), testPhoto) {
			t.Errorf("photo answer = %d %q with %d bytes, want the posted JPEG", photo.Code, photo.Header().Get("Content-Type"), photo.Body.Len())
		}
	})

	t.Run("a photo nobody holds answers 404", func(t *testing.T) {
		got := getPhoto(t, newTestRouter(t, openSeededStore(t), testNow), "dec-unknown")

		if got.Code != http.StatusNotFound {
			t.Errorf("status = %d, want %d", got.Code, http.StatusNotFound)
		}
	})

	t.Run("a photo that is not base64 answers 400", func(t *testing.T) {
		got := postRead(t, newTestRouter(t, openSeededStore(t), testNow), "application/json", readWithPhoto("not base64!"))

		if got.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want %d", got.Code, http.StatusBadRequest)
		}
	})

	t.Run("a body past the cap answers 413", func(t *testing.T) {
		oversized := strings.Repeat("A", maxReadBodyBytes)

		got := postRead(t, newTestRouter(t, openSeededStore(t), testNow), "application/json", readWithPhoto(oversized))

		if got.Code != http.StatusRequestEntityTooLarge {
			t.Errorf("status = %d, want %d", got.Code, http.StatusRequestEntityTooLarge)
		}
	})
}

// nextEvent reads one server-sent event from stream.
func nextEvent(t *testing.T, stream *bufio.Reader) (string, string) {
	t.Helper()
	var name, data string
	for {
		line, err := stream.ReadString('\n')
		if err != nil {
			t.Fatalf("read event stream: %v", err)
		}
		line = strings.TrimSuffix(line, "\n")
		switch {
		case line == "" && name != "":
			return name, data
		case strings.HasPrefix(line, "event: "):
			name = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			data = strings.TrimPrefix(line, "data: ")
		}
	}
}

func openEventStream(t *testing.T, server *httptest.Server) *bufio.Reader {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/lane/events", nil)
	if err != nil {
		t.Fatalf("build stream request: %v", err)
	}
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatalf("open event stream: %v", err)
	}
	t.Cleanup(func() { _ = response.Body.Close() })
	if got := response.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("content type = %q, want text/event-stream", got)
	}
	return bufio.NewReader(response.Body)
}

func traceStatuses(trace []TraceStep) []TraceStatus {
	statuses := make([]TraceStatus, 0, len(trace))
	for _, step := range trace {
		statuses = append(statuses, step.Status)
	}
	return statuses
}

func TestLaneEventsStreamsADecisionPostedAfterItConnects(t *testing.T) {
	feed := NewFeed()
	router := newFeedRouter(t, openSeededStore(t), testNow, feed, &LinkSwitch{})
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	t.Cleanup(feed.Close)
	stream := openEventStream(t, server)

	answer := postRead(t, router, "application/json", `{"plate": "KLM456", "confidence": 1}`)
	name, data := nextEvent(t, stream)

	var decision LaneDecision
	if err := json.Unmarshal([]byte(data), &decision); err != nil {
		t.Fatalf("decode event: %v", err)
	}
	if name != "decision" || decision.ID != decisionIDOf(t, answer) {
		t.Fatalf("event = %s %+v, want the posted decision", name, decision)
	}
	if decision.Outcome != OutcomeAdmit || decision.Reason != ReasonFleet || decision.CompanyName == nil || *decision.CompanyName != "Nordfrakt AB" {
		t.Errorf("decision = %+v, want a fleet admit billed to Nordfrakt AB", decision)
	}
	wantTrace := []TraceStatus{TraceDone, TraceDone, TraceDone, TraceQueued}
	if gotTrace := traceStatuses(decision.Trace); !slices.Equal(gotTrace, wantTrace) {
		t.Errorf("trace = %v, want %v", gotTrace, wantTrace)
	}
}

func TestReadsPublishesAPremiumAdmitWithItsWashNumber(t *testing.T) {
	store := openSeededStore(t)
	recordWash(t, store, "ABC123", testNow.Add(-time.Hour))
	feed := NewFeed()
	events := feed.Subscribe(t.Context())

	decodeAnswer(t, postRead(t, newFeedRouter(t, store, testNow, feed, &LinkSwitch{}), "application/json", `{"plate": "ABC123", "confidence": 1}`))

	got := drain(events)
	if len(got) != 1 {
		t.Fatalf("events = %+v, want one decision", got)
	}
	if decision := got[0].Body.(LaneDecision); decision.WashNumber == nil || *decision.WashNumber != 2 || decision.WashID == nil {
		t.Errorf("decision = %+v, want wash 2 with a wash id", decision)
	}
}
