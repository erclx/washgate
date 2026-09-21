package siteagent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	stockholm, err := time.LoadLocation("Europe/Stockholm")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	return NewRouter(store, Config{
		SiteID:   testSiteID,
		Policy:   testPolicy(),
		Location: stockholm,
		Now:      func() time.Time { return now },
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

type statusAnswer struct {
	SiteID       string     `json:"site_id"`
	OutboxDepth  int        `json:"outbox_depth"`
	LastSyncedAt *time.Time `json:"last_synced_at"`
}

func getStatus(t *testing.T, router http.Handler) statusAnswer {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/status", nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body %q", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var answer statusAnswer
	if err := json.NewDecoder(recorder.Body).Decode(&answer); err != nil {
		t.Fatalf("decode answer: %v", err)
	}
	return answer
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

func TestStatus(t *testing.T) {
	t.Run("reports the site, its outbox depth, and its last pull", func(t *testing.T) {
		store := openSeededStore(t)
		recordWash(t, store, "ABC123", testNow)
		recordWash(t, store, "KLM456", testNow)

		got := getStatus(t, newTestRouter(t, store, testNow))

		if got.SiteID != testSiteID || got.OutboxDepth != 2 || got.LastSyncedAt == nil || !got.LastSyncedAt.Equal(testNow) {
			t.Errorf("status = %+v, want %s with depth 2 pulled at %v", got, testSiteID, testNow)
		}
	})

	t.Run("a copy never pulled reports no sync time", func(t *testing.T) {
		got := getStatus(t, newTestRouter(t, openStore(t), testNow))

		if got.OutboxDepth != 0 || got.LastSyncedAt != nil {
			t.Errorf("status = %+v, want an empty outbox and no sync time", got)
		}
	})
}
