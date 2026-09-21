package central

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

type changeAnswer struct {
	Seq         int64   `json:"seq"`
	Plate       string  `json:"plate"`
	Plan        *string `json:"plan"`
	CompanyID   *string `json:"company_id"`
	CompanyName *string `json:"company_name"`
}

type entitlementsAnswer struct {
	Changes []changeAnswer `json:"changes"`
	Next    int64          `json:"next"`
}

func getEntitlements(t *testing.T, router http.Handler, token, query string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/entitlements"+query, nil)
	request.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

func decodeEntitlementsAnswer(t *testing.T, recorder *httptest.ResponseRecorder) entitlementsAnswer {
	t.Helper()
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body %q", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var answer entitlementsAnswer
	if err := json.NewDecoder(recorder.Body).Decode(&answer); err != nil {
		t.Fatalf("decode answer: %v", err)
	}
	return answer
}

func TestGetEntitlements(t *testing.T) {
	t.Run("an empty log answers the input cursor", func(t *testing.T) {
		store, _ := newTestStore(t)
		token := provisionSite(t, store, testSiteID)

		answer := decodeEntitlementsAnswer(t, getEntitlements(t, NewRouter(store), token, "?after=7"))

		if len(answer.Changes) != 0 || answer.Next != 7 {
			t.Fatalf("answer = %+v, want no changes and next 7", answer)
		}
	})

	t.Run("the cursor pages through the log", func(t *testing.T) {
		store, _ := newTestStore(t)
		putEntitlement(t, store, Entitlement{Plate: "AAA111", Plan: PlanPremium})
		putEntitlement(t, store, Entitlement{Plate: "BBB222", Plan: PlanPremium})
		putEntitlement(t, store, Entitlement{Plate: "CCC333", Plan: PlanPremium})
		token := provisionSite(t, store, testSiteID)
		router := NewRouter(store)

		firstPage := decodeEntitlementsAnswer(t, getEntitlements(t, router, token, "?limit=2"))
		secondPage := decodeEntitlementsAnswer(t, getEntitlements(t, router, token, "?limit=2&after="+strconv.FormatInt(firstPage.Next, 10)))

		if len(firstPage.Changes) != 2 || firstPage.Next != firstPage.Changes[1].Seq {
			t.Fatalf("first page = %+v, want two changes and next at the last seq", firstPage)
		}
		if len(secondPage.Changes) != 1 || secondPage.Changes[0].Plate != "CCC333" || secondPage.Next != secondPage.Changes[0].Seq {
			t.Fatalf("second page = %+v, want CCC333 and next at its seq", secondPage)
		}
	})

	t.Run("a fleet change carries its company and a revoke carries nulls", func(t *testing.T) {
		store, database := newTestStore(t)
		addCompany(t, database, testCompanyID, testCompanyName)
		putEntitlement(t, store, Entitlement{Plate: "FLT001", Plan: PlanFleet, CompanyID: testCompanyID})
		putEntitlement(t, store, Entitlement{Plate: "FLT001"})
		token := provisionSite(t, store, testSiteID)

		answer := decodeEntitlementsAnswer(t, getEntitlements(t, NewRouter(store), token, ""))

		fleet, revoke := answer.Changes[0], answer.Changes[1]
		if fleet.Plan == nil || *fleet.Plan != "fleet" || fleet.CompanyID == nil || *fleet.CompanyID != testCompanyID || fleet.CompanyName == nil || *fleet.CompanyName != testCompanyName {
			t.Fatalf("fleet change = %+v, want the fleet plan and its company", fleet)
		}
		if revoke.Plan != nil || revoke.CompanyID != nil || revoke.CompanyName != nil {
			t.Fatalf("revoke = %+v, want null plan and company", revoke)
		}
	})

	for _, query := range []string{"?after=abc", "?after=-1", "?limit=0", "?limit=ten"} {
		t.Run("rejects "+query+" with 400", func(t *testing.T) {
			store, _ := newTestStore(t)
			token := provisionSite(t, store, testSiteID)

			recorder := getEntitlements(t, NewRouter(store), token, query)

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
			}
		})
	}

	t.Run("rejects a request with no token with 401", func(t *testing.T) {
		store, _ := newTestStore(t)
		putEntitlement(t, store, Entitlement{Plate: "ABC123", Plan: PlanPremium})

		recorder := getEntitlements(t, NewRouter(store), "", "")

		if recorder.Code != http.StatusUnauthorized || strings.Contains(recorder.Body.String(), "ABC123") {
			t.Fatalf("status %d body %q, want 401 carrying no plate", recorder.Code, recorder.Body.String())
		}
	})
}
