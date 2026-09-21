package central

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const testLeasingCompany = "Nordfrakt Leasing"

type plateWashAnswer struct {
	AdmittedAt time.Time `json:"admitted_at"`
	SiteID     string    `json:"site_id"`
}

type subscriptionAnswer struct {
	Plan   string `json:"plan"`
	Status string `json:"status"`
}

type plateLookupAnswer struct {
	Plate            string              `json:"plate"`
	OwnerType        *string             `json:"owner_type"`
	OwnerName        *string             `json:"owner_name"`
	LeasingCompany   *string             `json:"leasing_company"`
	Subscription     *subscriptionAnswer `json:"subscription"`
	WashesThisMonth  int                 `json:"washes_this_month"`
	WashesSinceReset int                 `json:"washes_since_reset"`
	QuotaResetAt     *time.Time          `json:"quota_reset_at"`
	Washes           []plateWashAnswer   `json:"washes"`
}

type quotaResetAnswer struct {
	ID      string    `json:"id"`
	Plate   string    `json:"plate"`
	ResetAt time.Time `json:"reset_at"`
}

type siteAnswer struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	LastSyncedAt *time.Time `json:"last_synced_at"`
}

type sitesAnswer struct {
	Sites []siteAnswer `json:"sites"`
}

func serve(t *testing.T, router http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

func decodeOK[T any](t *testing.T, recorder *httptest.ResponseRecorder, wantStatus int) T {
	t.Helper()
	if recorder.Code != wantStatus {
		t.Fatalf("status = %d, want %d, body %q", recorder.Code, wantStatus, recorder.Body.String())
	}
	var answer T
	if err := json.NewDecoder(recorder.Body).Decode(&answer); err != nil {
		t.Fatalf("decode answer: %v", err)
	}
	return answer
}

// subscribePremium registers plate to a new customer on an active Premium subscription, the way a paid checkout does.
func subscribePremium(t *testing.T, store *Store, plate string) {
	t.Helper()
	applyEvent(t, store, SubscriptionEvent{
		EventID:              "evt_" + plate,
		EventType:            "invoice.paid",
		Action:               SubscriptionPaid,
		StripeSubscriptionID: "sub_" + plate,
		CustomerID:           testCustomerID,
		Plate:                plate,
		PeriodEnd:            testPeriodEnd,
	})
}

func addWashAt(t *testing.T, store *Store, id, plate string, admittedAt time.Time) {
	t.Helper()
	wash := Wash{ID: id, Plate: plate, Plan: PlanPremium, AdmittedAt: admittedAt}
	if _, err := store.RecordWashes(t.Context(), testSiteID, []Wash{wash}); err != nil {
		t.Fatalf("record wash: %v", err)
	}
}

func countQuotaResets(t *testing.T, store *Store) int {
	t.Helper()
	var count int
	if err := store.db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM quota_resets").Scan(&count); err != nil {
		t.Fatalf("count quota resets: %v", err)
	}
	return count
}

func TestGetPlate(t *testing.T) {
	t.Run("a Premium plate answers its customer, subscription, and this month's washes", func(t *testing.T) {
		store, database := newTestStore(t)
		addCustomer(t, database, testCustomerID)
		addSite(t, database, testSiteID)
		subscribePremium(t, store, "ABC123")
		admittedAt := time.Now().UTC().Truncate(time.Microsecond)
		addWashAt(t, store, "w1", "ABC123", admittedAt)

		answer := decodeOK[plateLookupAnswer](t, serve(t, NewRouter(store, nil), http.MethodGet, "/plates/ABC123", ""), http.StatusOK)

		if answer.Plate != "ABC123" || answer.OwnerType == nil || *answer.OwnerType != "customer" || answer.OwnerName == nil || *answer.OwnerName != testCustomerID+"@example.test" {
			t.Fatalf("owner = %+v, want the customer's email", answer)
		}
		if answer.Subscription == nil || *answer.Subscription != (subscriptionAnswer{Plan: "premium", Status: "active"}) {
			t.Fatalf("subscription = %+v, want active premium", answer.Subscription)
		}
		if answer.WashesThisMonth != 1 || answer.WashesSinceReset != 1 || len(answer.Washes) != 1 {
			t.Fatalf("answer = %+v, want one wash this month", answer)
		}
		if !answer.Washes[0].AdmittedAt.Equal(admittedAt) || answer.Washes[0].SiteID != testSiteID {
			t.Fatalf("wash = %+v, want %v at %s", answer.Washes[0], admittedAt, testSiteID)
		}
	})

	t.Run("a fleet plate answers its company and leasing company", func(t *testing.T) {
		store, database := newTestStore(t)
		addCompany(t, database, testCompanyID, testCompanyName)
		putEntitlement(t, store, Entitlement{Plate: "FLT001", Plan: PlanFleet, CompanyID: testCompanyID})
		if _, err := database.SQL.ExecContext(t.Context(), "UPDATE vehicles SET leasing_company = ? WHERE plate = 'FLT001'", testLeasingCompany); err != nil {
			t.Fatalf("set leasing company: %v", err)
		}

		answer := decodeOK[plateLookupAnswer](t, serve(t, NewRouter(store, nil), http.MethodGet, "/plates/FLT001", ""), http.StatusOK)

		if answer.OwnerType == nil || *answer.OwnerType != "company" || answer.OwnerName == nil || *answer.OwnerName != testCompanyName {
			t.Fatalf("owner = %+v, want the company", answer)
		}
		if answer.LeasingCompany == nil || *answer.LeasingCompany != testLeasingCompany {
			t.Fatalf("leasing company = %v, want %s", answer.LeasingCompany, testLeasingCompany)
		}
		if answer.Subscription == nil || answer.Subscription.Plan != "fleet" || answer.Washes == nil {
			t.Fatalf("answer = %+v, want an active fleet subscription and an empty wash list", answer)
		}
	})

	t.Run("a plate whose subscription ended answers no subscription", func(t *testing.T) {
		store, _ := newTestStore(t)
		putEntitlement(t, store, Entitlement{Plate: "ABC123", Plan: PlanPremium})
		putEntitlement(t, store, Entitlement{Plate: "ABC123"})

		answer := decodeOK[plateLookupAnswer](t, serve(t, NewRouter(store, nil), http.MethodGet, "/plates/ABC123", ""), http.StatusOK)

		if answer.Subscription != nil {
			t.Fatalf("subscription = %+v, want null", answer.Subscription)
		}
	})

	t.Run("an unknown plate answers 404", func(t *testing.T) {
		store, _ := newTestStore(t)

		recorder := serve(t, NewRouter(store, nil), http.MethodGet, "/plates/XYZ789", "")

		if recorder.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
		}
	})

	t.Run("a lowercase taxi plate is looked up under its registration", func(t *testing.T) {
		store, _ := newTestStore(t)
		putEntitlement(t, store, Entitlement{Plate: "ABC12A", Plan: PlanPremium})

		answer := decodeOK[plateLookupAnswer](t, serve(t, NewRouter(store, nil), http.MethodGet, "/plates/abc12at", ""), http.StatusOK)

		if answer.Plate != "ABC12A" {
			t.Fatalf("plate = %q, want ABC12A", answer.Plate)
		}
	})

	t.Run("a plate typed with separators is looked up without them", func(t *testing.T) {
		store, _ := newTestStore(t)
		putEntitlement(t, store, Entitlement{Plate: "ABC123", Plan: PlanPremium})

		answer := decodeOK[plateLookupAnswer](t, serve(t, NewRouter(store, nil), http.MethodGet, "/plates/abc-123", ""), http.StatusOK)

		if answer.Plate != "ABC123" {
			t.Fatalf("plate = %q, want ABC123", answer.Plate)
		}
	})

	t.Run("a malformed plate answers 400", func(t *testing.T) {
		store, _ := newTestStore(t)

		recorder := serve(t, NewRouter(store, nil), http.MethodGet, "/plates/NOT-A-PLATE", "")

		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
		}
	})
}

func TestLookupPlateCountsFromTheLatestReset(t *testing.T) {
	store, database := newTestStore(t)
	addCustomer(t, database, testCustomerID)
	addSite(t, database, testSiteID)
	subscribePremium(t, store, "ABC123")
	monthStart := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	addWashAt(t, store, "w-last-month", "ABC123", monthStart.Add(-time.Hour))
	addWashAt(t, store, "w-before", "ABC123", monthStart.Add(time.Hour))
	resetAt := monthStart.Add(2 * time.Hour)
	if _, _, err := store.ResetQuota(t.Context(), QuotaReset{ID: "reset-1", Plate: "ABC123", ResetAt: resetAt}); err != nil {
		t.Fatalf("reset quota: %v", err)
	}
	addWashAt(t, store, "w-after", "ABC123", monthStart.Add(3*time.Hour))

	lookup, err := store.LookupPlate(t.Context(), "ABC123", monthStart)
	if err != nil {
		t.Fatalf("look up plate: %v", err)
	}

	if len(lookup.Washes) != 2 || lookup.WashesSinceReset != 1 || !lookup.QuotaResetAt.Equal(resetAt) {
		t.Fatalf("lookup = %+v, want two washes this month, one since the reset at %v", lookup, resetAt)
	}
}

func TestPostQuotaReset(t *testing.T) {
	t.Run("a reset records one change carrying its time", func(t *testing.T) {
		store, database := newTestStore(t)
		addCustomer(t, database, testCustomerID)
		subscribePremium(t, store, "ABC123")
		before := readChanges(t, store, 0, 100)

		answer := decodeOK[quotaResetAnswer](t,
			serve(t, NewRouter(store, nil), http.MethodPost, "/plates/ABC123/quota-resets", `{"id": "reset-1", "note": "goodwill"}`),
			http.StatusCreated)

		changes := readChanges(t, store, before[len(before)-1].Seq, 100)
		if answer.ID != "reset-1" || answer.Plate != "ABC123" || answer.ResetAt.IsZero() {
			t.Fatalf("answer = %+v, want the stored reset", answer)
		}
		if len(changes) != 1 || changes[0].Plan != PlanPremium || !changes[0].QuotaResetAt.Equal(answer.ResetAt) {
			t.Fatalf("changes = %+v, want one premium change at %v", changes, answer.ResetAt)
		}
	})

	t.Run("the same reset id twice appends one change", func(t *testing.T) {
		store, database := newTestStore(t)
		addCustomer(t, database, testCustomerID)
		subscribePremium(t, store, "ABC123")
		router := NewRouter(store, nil)
		first := decodeOK[quotaResetAnswer](t, serve(t, router, http.MethodPost, "/plates/ABC123/quota-resets", `{"id": "reset-1"}`), http.StatusCreated)

		second := decodeOK[quotaResetAnswer](t, serve(t, router, http.MethodPost, "/plates/ABC123/quota-resets", `{"id": "reset-1"}`), http.StatusOK)

		if !second.ResetAt.Equal(first.ResetAt) || countQuotaResets(t, store) != 1 || len(readChanges(t, store, 0, 100)) != 2 {
			t.Fatalf("second = %+v, want the first reset and no second change", second)
		}
	})

	t.Run("a reset id reused for another plate answers 409", func(t *testing.T) {
		store, database := newTestStore(t)
		addCustomer(t, database, testCustomerID)
		subscribePremium(t, store, "ABC123")
		subscribePremium(t, store, "DEF456")
		router := NewRouter(store, nil)
		serve(t, router, http.MethodPost, "/plates/ABC123/quota-resets", `{"id": "reset-1"}`)

		recorder := serve(t, router, http.MethodPost, "/plates/DEF456/quota-resets", `{"id": "reset-1"}`)

		if recorder.Code != http.StatusConflict || countQuotaResets(t, store) != 1 {
			t.Fatalf("status = %d, want %d and one reset", recorder.Code, http.StatusConflict)
		}
	})

	t.Run("a fleet plate answers 409", func(t *testing.T) {
		store, database := newTestStore(t)
		addCompany(t, database, testCompanyID, testCompanyName)
		putEntitlement(t, store, Entitlement{Plate: "FLT001", Plan: PlanFleet, CompanyID: testCompanyID})

		recorder := serve(t, NewRouter(store, nil), http.MethodPost, "/plates/FLT001/quota-resets", `{"id": "reset-1"}`)

		if recorder.Code != http.StatusConflict || countQuotaResets(t, store) != 0 {
			t.Fatalf("status = %d, want %d and no reset", recorder.Code, http.StatusConflict)
		}
	})

	t.Run("a plate with no active subscription answers 404", func(t *testing.T) {
		store, _ := newTestStore(t)
		putEntitlement(t, store, Entitlement{Plate: "ABC123", Plan: PlanPremium})
		putEntitlement(t, store, Entitlement{Plate: "ABC123"})

		recorder := serve(t, NewRouter(store, nil), http.MethodPost, "/plates/ABC123/quota-resets", `{"id": "reset-1"}`)

		if recorder.Code != http.StatusNotFound || countQuotaResets(t, store) != 0 {
			t.Fatalf("status = %d, want %d and no reset", recorder.Code, http.StatusNotFound)
		}
	})

	for _, body := range []string{`{}`, `{"id": ""}`, `{"id": "` + strings.Repeat("x", 65) + `"}`, `not json`} {
		t.Run("rejects "+body+" with 400", func(t *testing.T) {
			store, _ := newTestStore(t)

			recorder := serve(t, NewRouter(store, nil), http.MethodPost, "/plates/ABC123/quota-resets", body)

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
			}
		})
	}
}

func TestGetEntitlementsCarriesTheQuotaReset(t *testing.T) {
	store, database := newTestStore(t)
	addCustomer(t, database, testCustomerID)
	subscribePremium(t, store, "ABC123")
	token := provisionSite(t, store, testSiteID)
	router := NewRouter(store, nil)
	reset := decodeOK[quotaResetAnswer](t, serve(t, router, http.MethodPost, "/plates/ABC123/quota-resets", `{"id": "reset-1"}`), http.StatusCreated)

	var answer struct {
		Changes []struct {
			QuotaResetAt *time.Time `json:"quota_reset_at"`
		} `json:"changes"`
	}
	recorder := getEntitlements(t, router, token, "")
	if err := json.NewDecoder(recorder.Body).Decode(&answer); err != nil {
		t.Fatalf("decode answer: %v", err)
	}

	subscribed, resetChange := answer.Changes[0], answer.Changes[1]
	if subscribed.QuotaResetAt != nil {
		t.Fatalf("subscribe change quota_reset_at = %v, want null", subscribed.QuotaResetAt)
	}
	if resetChange.QuotaResetAt == nil || !resetChange.QuotaResetAt.Equal(reset.ResetAt) {
		t.Fatalf("reset change quota_reset_at = %v, want %v", resetChange.QuotaResetAt, reset.ResetAt)
	}
}

func TestGetSites(t *testing.T) {
	store, database := newTestStore(t)
	provisionSite(t, store, testSiteID)
	addWashAt(t, store, "w1", "ABC123", testAdmittedAt)
	addSite(t, database, "site-never-synced")

	answer := decodeOK[sitesAnswer](t, serve(t, NewRouter(store, nil), http.MethodGet, "/sites", ""), http.StatusOK)

	if len(answer.Sites) != 2 || answer.Sites[0].ID != testSiteID || answer.Sites[0].LastSyncedAt == nil {
		t.Fatalf("sites = %+v, want %s first with its sync time", answer.Sites, testSiteID)
	}
	if answer.Sites[1].ID != "site-never-synced" || answer.Sites[1].LastSyncedAt != nil {
		t.Fatalf("second site = %+v, want no sync time", answer.Sites[1])
	}
}
