package siteagent

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type statusAnswer struct {
	SiteID       string     `json:"site_id"`
	Link         LinkState  `json:"link"`
	OutboxDepth  int        `json:"outbox_depth"`
	LastSyncedAt *time.Time `json:"last_synced_at"`
	IsSyncing    bool       `json:"is_syncing"`
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

	t.Run("a cut link reports cut", func(t *testing.T) {
		link := &LinkSwitch{}
		link.endSync(nil)
		link.SetCut(true)

		got := getStatus(t, newFeedRouter(t, openSeededStore(t), testNow, NewFeed(), link))

		if got.Link != LinkCut {
			t.Errorf("link = %q, want %q", got.Link, LinkCut)
		}
	})

	t.Run("a successful last sync reports online", func(t *testing.T) {
		link := &LinkSwitch{}
		link.endSync(nil)

		got := getStatus(t, newFeedRouter(t, openSeededStore(t), testNow, NewFeed(), link))

		if got.Link != LinkOnline || got.IsSyncing {
			t.Errorf("status = %+v, want online and not syncing", got)
		}
	})

	t.Run("a failed last sync attempt reports offline", func(t *testing.T) {
		link := &LinkSwitch{}
		link.endSync(nil)
		link.endSync(errors.New("central answered 503"))

		got := getStatus(t, newFeedRouter(t, openSeededStore(t), testNow, NewFeed(), link))

		if got.Link != LinkOffline {
			t.Errorf("link = %q, want %q", got.Link, LinkOffline)
		}
	})

	t.Run("a sync under way reports syncing", func(t *testing.T) {
		link := &LinkSwitch{}
		link.beginSync()

		got := getStatus(t, newFeedRouter(t, openSeededStore(t), testNow, NewFeed(), link))

		if !got.IsSyncing {
			t.Errorf("status = %+v, want syncing", got)
		}
	})
}

func putLink(t *testing.T, router http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPut, "/site/link", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

func TestLinkSwitch(t *testing.T) {
	t.Run("cutting the link is reported by the status", func(t *testing.T) {
		link := &LinkSwitch{}
		router := newFeedRouter(t, openSeededStore(t), testNow, NewFeed(), link)

		answer := putLink(t, router, `{"cut": true}`)

		if answer.Code != http.StatusNoContent || getStatus(t, router).Link != LinkCut {
			t.Errorf("answer %d, link cut = %v, want 204 and a cut link", answer.Code, link.IsCut())
		}
	})

	t.Run("restoring the link lifts the cut", func(t *testing.T) {
		link := &LinkSwitch{}
		link.SetCut(true)
		router := newFeedRouter(t, openSeededStore(t), testNow, NewFeed(), link)

		answer := putLink(t, router, `{"cut": false}`)

		if answer.Code != http.StatusNoContent || link.IsCut() {
			t.Errorf("answer %d, link cut = %v, want 204 and a restored link", answer.Code, link.IsCut())
		}
	})

	t.Run("a body without cut answers 400", func(t *testing.T) {
		answer := putLink(t, newTestRouter(t, openSeededStore(t), testNow), `{}`)

		if answer.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want %d", answer.Code, http.StatusBadRequest)
		}
	})
}
