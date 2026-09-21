package central

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type carPlanAnswer struct {
	Plate string  `json:"plate"`
	Plan  *string `json:"plan"`
}

type customerAnswer struct {
	ID   string          `json:"id"`
	Name string          `json:"name"`
	Cars []carPlanAnswer `json:"cars"`
}

type customerCarAnswer struct {
	Plate              string            `json:"plate"`
	Plan               *string           `json:"plan"`
	WashesUsed         int               `json:"washes_used"`
	MonthlyCap         int               `json:"monthly_cap"`
	ResetsOn           string            `json:"resets_on"`
	PrepaidWashesReady int               `json:"prepaid_washes_ready"`
	Washes             []plateWashAnswer `json:"washes"`
}

func postCar(t *testing.T, router http.Handler, customerID, body string) *httptest.ResponseRecorder {
	t.Helper()
	return serve(t, router, http.MethodPost, "/customers/"+customerID+"/cars", body)
}

func TestGetCustomers(t *testing.T) {
	store, database := newTestStore(t)
	addCustomer(t, database, testCustomerID)
	if _, err := database.SQL.ExecContext(t.Context(), "UPDATE customers SET name = 'Anna Lindqvist' WHERE id = ?", testCustomerID); err != nil {
		t.Fatalf("name customer: %v", err)
	}
	subscribePremium(t, store, "ABC123")

	answer := decodeOK[[]customerAnswer](t, serve(t, NewRouter(store, nil), http.MethodGet, "/customers", ""), http.StatusOK)

	if len(answer) != 1 || answer[0].ID != testCustomerID || answer[0].Name != "Anna Lindqvist" {
		t.Fatalf("customers = %+v, want %s named Anna Lindqvist", answer, testCustomerID)
	}
	if len(answer[0].Cars) != 1 || answer[0].Cars[0].Plate != "ABC123" || answer[0].Cars[0].Plan == nil || *answer[0].Cars[0].Plan != "premium" {
		t.Fatalf("cars = %+v, want ABC123 on premium", answer[0].Cars)
	}
}

func TestGetCustomerCars(t *testing.T) {
	t.Run("answers each car with its washes used, the cap, and the reset date", func(t *testing.T) {
		store, database := newTestStore(t)
		addCustomer(t, database, testCustomerID)
		addSite(t, database, testSiteID)
		subscribePremium(t, store, "ABC123")
		addWashAt(t, store, "w1", "ABC123", time.Now().UTC())

		answer := decodeOK[[]customerCarAnswer](t, serve(t, NewRouter(store, nil), http.MethodGet, "/customers/"+testCustomerID+"/cars", ""), http.StatusOK)

		if len(answer) != 1 || answer[0].Plan == nil || *answer[0].Plan != "premium" || answer[0].WashesUsed != 1 || answer[0].MonthlyCap != 8 || answer[0].ResetsOn == "" {
			t.Fatalf("cars = %+v, want one premium car with one of 8 washes used and a reset date", answer)
		}
	})

	t.Run("a car with no plan answers a null plan", func(t *testing.T) {
		store, database := newTestStore(t)
		addCustomer(t, database, testCustomerID)
		registerPlate(t, store, testCustomerID, "ABC123")

		answer := decodeOK[[]customerCarAnswer](t, serve(t, NewRouter(store, nil), http.MethodGet, "/customers/"+testCustomerID+"/cars", ""), http.StatusOK)

		if len(answer) != 1 || answer[0].Plan != nil {
			t.Fatalf("cars = %+v, want one car with a null plan", answer)
		}
	})

	t.Run("an unknown customer answers 404", func(t *testing.T) {
		store, _ := newTestStore(t)

		recorder := serve(t, NewRouter(store, nil), http.MethodGet, "/customers/customer-nobody/cars", "")

		if recorder.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
		}
	})

	t.Run("resets on the first of next month in Stockholm and counts from the first of this one", func(t *testing.T) {
		store, database := newTestStore(t)
		addCustomer(t, database, testCustomerID)
		addSite(t, database, testSiteID)
		subscribePremium(t, store, "ABC123")
		// 23:00 on 30 September in Stockholm, the last hour of September.
		addWashAt(t, store, "w-september", "ABC123", time.Date(2026, 9, 30, 21, 0, 0, 0, time.UTC))
		api := newCustomerAPI(store)
		// 00:30 on 1 October in Stockholm, still 30 September in UTC.
		api.month.now = func() time.Time { return time.Date(2026, 9, 30, 22, 30, 0, 0, time.UTC) }
		request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/customers/"+testCustomerID+"/cars", nil)
		request.SetPathValue("id", testCustomerID)
		recorder := httptest.NewRecorder()

		api.handleGetCars(recorder, request)

		answer := decodeOK[[]customerCarAnswer](t, recorder, http.StatusOK)
		if len(answer) != 1 || answer[0].ResetsOn != "2026-11-01" || answer[0].WashesUsed != 0 {
			t.Fatalf("cars = %+v, want a reset on 2026-11-01 and no October wash used", answer)
		}
	})
}

func TestGetCustomerCar(t *testing.T) {
	t.Run("answers the car with this month's washes", func(t *testing.T) {
		store, database := newTestStore(t)
		addCustomer(t, database, testCustomerID)
		addSite(t, database, testSiteID)
		subscribePremium(t, store, "ABC123")
		admittedAt := time.Now().UTC().Truncate(time.Microsecond)
		addWashAt(t, store, "w1", "ABC123", admittedAt)

		answer := decodeOK[customerCarAnswer](t, serve(t, NewRouter(store, nil), http.MethodGet, "/customers/"+testCustomerID+"/cars/abc-123", ""), http.StatusOK)

		if answer.Plate != "ABC123" || answer.WashesUsed != 1 || len(answer.Washes) != 1 {
			t.Fatalf("car = %+v, want ABC123 with one wash", answer)
		}
		if !answer.Washes[0].AdmittedAt.Equal(admittedAt) || answer.Washes[0].SiteID != testSiteID {
			t.Fatalf("wash = %+v, want %v at %s", answer.Washes[0], admittedAt, testSiteID)
		}
	})

	t.Run("a malformed plate answers 400", func(t *testing.T) {
		store, _ := newTestStore(t)

		recorder := serve(t, NewRouter(store, nil), http.MethodGet, "/customers/"+testCustomerID+"/cars/NOT-A-PLATE", "")

		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
		}
	})

	t.Run("another customer's plate answers 404", func(t *testing.T) {
		store, database := newTestStore(t)
		addCustomer(t, database, testCustomerID)
		addCustomer(t, database, testOtherCustomerID)
		registerPlate(t, store, testOtherCustomerID, "ABC123")

		recorder := serve(t, NewRouter(store, nil), http.MethodGet, "/customers/"+testCustomerID+"/cars/ABC123", "")

		if recorder.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
		}
	})
}

func TestPostCustomerCar(t *testing.T) {
	t.Run("a new plate answers 201 with the car under its normalized form", func(t *testing.T) {
		store, database := newTestStore(t)
		addCustomer(t, database, testCustomerID)

		answer := decodeOK[customerCarAnswer](t, postCar(t, NewRouter(store, nil), testCustomerID, `{"plate": "abc 123"}`), http.StatusCreated)

		if answer.Plate != "ABC123" || answer.Plan != nil || answer.Washes == nil {
			t.Fatalf("car = %+v, want ABC123 with no plan and an empty wash list", answer)
		}
	})

	t.Run("a plate the customer already holds answers 200", func(t *testing.T) {
		store, database := newTestStore(t)
		addCustomer(t, database, testCustomerID)
		router := NewRouter(store, nil)
		postCar(t, router, testCustomerID, `{"plate": "ABC123"}`)

		answer := decodeOK[customerCarAnswer](t, postCar(t, router, testCustomerID, `{"plate": "ABC-123"}`), http.StatusOK)

		if answer.Plate != "ABC123" || countRows(t, database, "vehicles") != 1 {
			t.Fatalf("car = %+v, want ABC123 and one vehicle", answer)
		}
	})

	t.Run("a plate another customer holds answers 409", func(t *testing.T) {
		store, database := newTestStore(t)
		addCustomer(t, database, testCustomerID)
		addCustomer(t, database, testOtherCustomerID)
		registerPlate(t, store, testOtherCustomerID, "ABC123")

		recorder := postCar(t, NewRouter(store, nil), testCustomerID, `{"plate": "ABC123"}`)

		if recorder.Code != http.StatusConflict {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusConflict)
		}
	})

	t.Run("an unknown customer answers 404", func(t *testing.T) {
		store, _ := newTestStore(t)

		recorder := postCar(t, NewRouter(store, nil), "customer-nobody", `{"plate": "ABC123"}`)

		if recorder.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
		}
	})

	cases := []struct {
		name string
		body string
	}{
		{name: "a body that is not JSON answers 400", body: `plate=ABC123`},
		{name: "a missing plate answers 400", body: `{}`},
		{name: "a plate in no known shape answers 400", body: `{"plate": "NOT-A-PLATE"}`},
		{name: "a body over 4 KiB answers 400", body: `{"plate": "ABC123", "note": "` + strings.Repeat("a", 5000) + `"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store, database := newTestStore(t)
			addCustomer(t, database, testCustomerID)

			recorder := postCar(t, NewRouter(store, nil), testCustomerID, tc.body)

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
			}
		})
	}
}
