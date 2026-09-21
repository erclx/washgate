package central

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

type washesAnswer struct {
	Stored     []string `json:"stored"`
	Duplicates []string `json:"duplicates"`
}

func postWashes(t *testing.T, router http.Handler, token, contentType, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/washes", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", contentType)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

func decodeWashesAnswer(t *testing.T, recorder *httptest.ResponseRecorder) washesAnswer {
	t.Helper()
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body %q", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var answer washesAnswer
	if err := json.NewDecoder(recorder.Body).Decode(&answer); err != nil {
		t.Fatalf("decode answer: %v", err)
	}
	return answer
}

func washBatchBody(siteID string, washes ...string) string {
	return fmt.Sprintf(`{"site_id": %q, "washes": [%s]}`, siteID, strings.Join(washes, ", "))
}

func premiumWashBody(id string) string {
	return fmt.Sprintf(`{"id": %q, "plate": "ABC123", "plan": "premium", "admitted_at": "2026-09-21T07:30:00Z"}`, id)
}

func oversizedBatchBody() string {
	washes := make([]string, maxWashesPerBatch+1)
	for index := range washes {
		washes[index] = premiumWashBody(fmt.Sprintf("w%d", index))
	}
	return washBatchBody(testSiteID, washes...)
}

func TestPostWashes(t *testing.T) {
	t.Run("stores a new batch and answers with its ids", func(t *testing.T) {
		store, database := newTestStore(t)
		token := provisionSite(t, store, testSiteID)
		addCompany(t, database, testCompanyID, testCompanyName)
		fleetWash := fmt.Sprintf(`{"id": "w2", "plate": "FLT001", "plan": "fleet", "company_id": %q, "admitted_at": "2026-09-21T07:31:00Z"}`, testCompanyID)

		answer := decodeWashesAnswer(t, postWashes(t, NewRouter(store), token, "application/json", washBatchBody(testSiteID, premiumWashBody("w1"), fleetWash)))

		if !slices.Equal(answer.Stored, []string{"w1", "w2"}) || len(answer.Duplicates) != 0 {
			t.Fatalf("answer = %+v, want both stored", answer)
		}
	})

	t.Run("the same body posted twice answers 200 both times and leaves one row per wash", func(t *testing.T) {
		store, database := newTestStore(t)
		token := provisionSite(t, store, testSiteID)
		router := NewRouter(store)
		body := washBatchBody(testSiteID, premiumWashBody("w1"), premiumWashBody("w2"))

		first := decodeWashesAnswer(t, postWashes(t, router, token, "application/json", body))
		second := decodeWashesAnswer(t, postWashes(t, router, token, "application/json", body))

		if !slices.Equal(first.Stored, []string{"w1", "w2"}) {
			t.Fatalf("first answer = %+v, want both stored", first)
		}
		if len(second.Stored) != 0 || !slices.Equal(second.Duplicates, []string{"w1", "w2"}) {
			t.Fatalf("second answer = %+v, want both duplicates", second)
		}
		if got := countWashes(t, database); got != 2 {
			t.Fatalf("washes = %d, want 2", got)
		}
	})

	t.Run("rejects a body that is not JSON with 415", func(t *testing.T) {
		store, _ := newTestStore(t)
		token := provisionSite(t, store, testSiteID)

		recorder := postWashes(t, NewRouter(store), token, "text/plain", washBatchBody(testSiteID, premiumWashBody("w1")))

		if recorder.Code != http.StatusUnsupportedMediaType {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnsupportedMediaType)
		}
	})

	t.Run("rejects a request with no token with 401", func(t *testing.T) {
		store, database := newTestStore(t)
		provisionSite(t, store, testSiteID)

		recorder := postWashes(t, NewRouter(store), "", "application/json", washBatchBody(testSiteID, premiumWashBody("w1")))

		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
		}
		if got := countWashes(t, database); got != 0 {
			t.Fatalf("washes = %d, want 0", got)
		}
	})

	t.Run("rejects a site pushing under another site's id with 403", func(t *testing.T) {
		store, database := newTestStore(t)
		provisionSite(t, store, "site-kista")
		token := provisionSite(t, store, testSiteID)

		recorder := postWashes(t, NewRouter(store), token, "application/json", washBatchBody("site-kista", premiumWashBody("w1")))

		if recorder.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
		}
		if got := countWashes(t, database); got != 0 {
			t.Fatalf("washes = %d, want 0", got)
		}
	})

	t.Run("rejects an unknown company with 422", func(t *testing.T) {
		store, _ := newTestStore(t)
		token := provisionSite(t, store, testSiteID)
		fleetWash := `{"id": "w1", "plate": "FLT001", "plan": "fleet", "company_id": "company-nowhere", "admitted_at": "2026-09-21T07:30:00Z"}`

		recorder := postWashes(t, NewRouter(store), token, "application/json", washBatchBody(testSiteID, fleetWash))

		if recorder.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnprocessableEntity)
		}
	})

	cases := []struct {
		name string
		body string
	}{
		{name: "malformed JSON", body: `{"site_id": `},
		{name: "an empty batch", body: washBatchBody(testSiteID)},
		{name: "a batch over the limit", body: oversizedBatchBody()},
		{name: "a missing site", body: washBatchBody("", premiumWashBody("w1"))},
		{name: "a missing wash id", body: washBatchBody(testSiteID, premiumWashBody(""))},
		{name: "a missing plate", body: washBatchBody(testSiteID, `{"id": "w1", "plan": "premium", "admitted_at": "2026-09-21T07:30:00Z"}`)},
		{name: "a missing admitted_at", body: washBatchBody(testSiteID, `{"id": "w1", "plate": "ABC123", "plan": "premium"}`)},
		{name: "an unknown plan", body: washBatchBody(testSiteID, `{"id": "w1", "plate": "ABC123", "plan": "gold", "admitted_at": "2026-09-21T07:30:00Z"}`)},
		{name: "a fleet wash with no company", body: washBatchBody(testSiteID, `{"id": "w1", "plate": "FLT001", "plan": "fleet", "admitted_at": "2026-09-21T07:30:00Z"}`)},
		{name: "a premium wash naming a company", body: washBatchBody(testSiteID, `{"id": "w1", "plate": "ABC123", "plan": "premium", "company_id": "company-nordfrakt", "admitted_at": "2026-09-21T07:30:00Z"}`)},
	}
	for _, testCase := range cases {
		t.Run("rejects "+testCase.name+" with 400", func(t *testing.T) {
			store, _ := newTestStore(t)
			token := provisionSite(t, store, testSiteID)

			recorder := postWashes(t, NewRouter(store), token, "application/json", testCase.body)

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d, body %q", recorder.Code, http.StatusBadRequest, recorder.Body.String())
			}
		})
	}
}
