package siteagent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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
