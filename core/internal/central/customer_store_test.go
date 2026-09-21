package central

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/erclx/washgate/core/internal/testdb"
)

const testOtherCustomerID = "customer-ben"

var testMonthStart = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

func registerPlate(t *testing.T, store *Store, customerID, plate string) bool {
	t.Helper()
	isNew, err := store.RegisterPlate(t.Context(), customerID, plate)
	if err != nil {
		t.Fatalf("register plate %s: %v", plate, err)
	}
	return isNew
}

func readCustomerCars(t *testing.T, store *Store, customerID string) []CustomerCar {
	t.Helper()
	cars, err := store.CustomerCars(t.Context(), customerID, testMonthStart)
	if err != nil {
		t.Fatalf("read customer cars: %v", err)
	}
	return cars
}

func applySeed(t *testing.T, database *testdb.Database) {
	t.Helper()
	statements, err := os.ReadFile("../../seed/demo.sql")
	if err != nil {
		t.Fatalf("read demo seed: %v", err)
	}
	if _, err := database.SQL.ExecContext(t.Context(), string(statements)); err != nil {
		t.Fatalf("apply demo seed: %v", err)
	}
}

func countRows(t *testing.T, database *testdb.Database, table string) int {
	t.Helper()
	var count int
	if err := database.SQL.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return count
}

func TestRegisterPlate(t *testing.T) {
	t.Run("stores one vehicle for the customer and appends no entitlement change", func(t *testing.T) {
		store, database := newTestStore(t)
		addCustomer(t, database, testCustomerID)

		isNew := registerPlate(t, store, testCustomerID, "ABC123")

		cars := readCustomerCars(t, store, testCustomerID)
		if !isNew || len(cars) != 1 || cars[0].Plate != "ABC123" || cars[0].Plan != PlanNone {
			t.Fatalf("isNew = %v, cars = %+v, want one new car with no plan", isNew, cars)
		}
		if changes := countRows(t, database, "entitlement_changes"); changes != 0 {
			t.Fatalf("entitlement changes = %d, want 0", changes)
		}
	})

	t.Run("the same registration again reports the car as not new", func(t *testing.T) {
		store, database := newTestStore(t)
		addCustomer(t, database, testCustomerID)
		registerPlate(t, store, testCustomerID, "ABC123")

		isNew := registerPlate(t, store, testCustomerID, "ABC123")

		if isNew || countRows(t, database, "vehicles") != 1 {
			t.Fatalf("isNew = %v, want false and one vehicle", isNew)
		}
	})

	t.Run("a plate another customer holds is refused", func(t *testing.T) {
		store, database := newTestStore(t)
		addCustomer(t, database, testCustomerID)
		addCustomer(t, database, testOtherCustomerID)
		registerPlate(t, store, testOtherCustomerID, "ABC123")

		_, err := store.RegisterPlate(t.Context(), testCustomerID, "ABC123")

		if !errors.Is(err, ErrPlateTaken) {
			t.Fatalf("err = %v, want %v", err, ErrPlateTaken)
		}
	})

	t.Run("a fleet plate is refused", func(t *testing.T) {
		store, database := newTestStore(t)
		addCustomer(t, database, testCustomerID)
		addCompany(t, database, testCompanyID, testCompanyName)
		putEntitlement(t, store, Entitlement{Plate: "FLT001", Plan: PlanFleet, CompanyID: testCompanyID})

		_, err := store.RegisterPlate(t.Context(), testCustomerID, "FLT001")

		if !errors.Is(err, ErrPlateTaken) {
			t.Fatalf("err = %v, want %v", err, ErrPlateTaken)
		}
	})

	t.Run("an unknown customer is refused", func(t *testing.T) {
		store, database := newTestStore(t)

		_, err := store.RegisterPlate(t.Context(), "customer-nobody", "ABC123")

		if !errors.Is(err, ErrUnknownCustomer) || countRows(t, database, "vehicles") != 0 {
			t.Fatalf("err = %v, want %v and no vehicle", err, ErrUnknownCustomer)
		}
	})

	t.Run("an unknown customer is refused as unknown even for a plate another customer holds", func(t *testing.T) {
		store, database := newTestStore(t)
		addCustomer(t, database, testOtherCustomerID)
		registerPlate(t, store, testOtherCustomerID, "ABC123")

		_, err := store.RegisterPlate(t.Context(), "customer-nobody", "ABC123")

		if !errors.Is(err, ErrUnknownCustomer) {
			t.Fatalf("err = %v, want %v", err, ErrUnknownCustomer)
		}
	})
}

func TestCustomerCars(t *testing.T) {
	t.Run("counts only the Premium washes since the latest quota reset", func(t *testing.T) {
		store, database := newTestStore(t)
		addCustomer(t, database, testCustomerID)
		addSite(t, database, testSiteID)
		subscribePremium(t, store, "ABC123")
		addWashAt(t, store, "w-last-month", "ABC123", testMonthStart.Add(-time.Hour))
		addWashAt(t, store, "w-before", "ABC123", testMonthStart.Add(time.Hour))
		if _, _, err := store.ResetQuota(t.Context(), QuotaReset{ID: "reset-1", Plate: "ABC123", ResetAt: testMonthStart.Add(2 * time.Hour)}); err != nil {
			t.Fatalf("reset quota: %v", err)
		}
		addWashAt(t, store, "w-after", "ABC123", testMonthStart.Add(3*time.Hour))

		cars := readCustomerCars(t, store, testCustomerID)

		if len(cars) != 1 || cars[0].Plan != PlanPremium || cars[0].WashesUsed != 1 {
			t.Fatalf("cars = %+v, want one Premium car with one wash used", cars)
		}
	})

	t.Run("counts a prepaid wash as ready until it is spent", func(t *testing.T) {
		store, database := newTestStore(t)
		addCustomer(t, database, testCustomerID)
		addSite(t, database, testSiteID)
		grantPrepaidWash(t, store, singleWashGrant("evt_1", "cs_test_1"))
		grantPrepaidWash(t, store, singleWashGrant("evt_2", "cs_test_2"))
		spent := prepaidWash("w1", "cs_test_1")
		if _, err := store.RecordWashes(t.Context(), testSiteID, []Wash{spent}); err != nil {
			t.Fatalf("record prepaid wash: %v", err)
		}

		cars := readCustomerCars(t, store, testCustomerID)

		if len(cars) != 1 || cars[0].Plate != testPrepaidPlate || cars[0].PrepaidWashesReady != 1 || cars[0].WashesUsed != 0 {
			t.Fatalf("cars = %+v, want one car with one prepaid wash ready and none used", cars)
		}
	})

	t.Run("a canceled subscription shows no plan", func(t *testing.T) {
		store, database := newTestStore(t)
		addCustomer(t, database, testCustomerID)
		putEntitlement(t, store, Entitlement{Plate: "ABC123", Plan: PlanPremium, CustomerID: testCustomerID})
		putEntitlement(t, store, Entitlement{Plate: "ABC123"})

		cars := readCustomerCars(t, store, testCustomerID)

		if len(cars) != 1 || cars[0].Plan != PlanNone {
			t.Fatalf("cars = %+v, want one car with no plan", cars)
		}
	})

	t.Run("an unknown customer is refused", func(t *testing.T) {
		store, _ := newTestStore(t)

		_, err := store.CustomerCars(t.Context(), "customer-nobody", testMonthStart)

		if !errors.Is(err, ErrUnknownCustomer) {
			t.Fatalf("err = %v, want %v", err, ErrUnknownCustomer)
		}
	})
}

func TestCustomerCar(t *testing.T) {
	t.Run("answers the car with this month's washes, oldest first", func(t *testing.T) {
		store, database := newTestStore(t)
		addCustomer(t, database, testCustomerID)
		addSite(t, database, testSiteID)
		subscribePremium(t, store, "ABC123")
		addWashAt(t, store, "w-last-month", "ABC123", testMonthStart.Add(-time.Hour))
		addWashAt(t, store, "w2", "ABC123", testMonthStart.Add(2*time.Hour))
		addWashAt(t, store, "w1", "ABC123", testMonthStart.Add(time.Hour))

		car, err := store.CustomerCar(t.Context(), testCustomerID, "ABC123", testMonthStart)
		if err != nil {
			t.Fatalf("read customer car: %v", err)
		}

		if car.WashesUsed != 2 || len(car.Washes) != 2 || !car.Washes[0].AdmittedAt.Equal(testMonthStart.Add(time.Hour)) {
			t.Fatalf("car = %+v, want two washes this month, oldest first", car)
		}
	})

	t.Run("another customer's plate answers as unknown", func(t *testing.T) {
		store, database := newTestStore(t)
		addCustomer(t, database, testCustomerID)
		addCustomer(t, database, testOtherCustomerID)
		registerPlate(t, store, testOtherCustomerID, "ABC123")

		_, err := store.CustomerCar(t.Context(), testCustomerID, "ABC123", testMonthStart)

		if !errors.Is(err, ErrUnknownPlate) {
			t.Fatalf("err = %v, want %v", err, ErrUnknownPlate)
		}
	})
}

func TestCustomersListsEachCustomerWithTheirCarsAndPlans(t *testing.T) {
	store, database := newTestStore(t)
	addCustomer(t, database, testCustomerID)
	addCustomer(t, database, testOtherCustomerID)
	subscribePremium(t, store, "ABC123")
	registerPlate(t, store, testCustomerID, "DEF456")

	customers, err := store.Customers(t.Context())
	if err != nil {
		t.Fatalf("read customers: %v", err)
	}

	if len(customers) != 2 || customers[0].ID != testCustomerID || customers[1].ID != testOtherCustomerID {
		t.Fatalf("customers = %+v, want %s then %s", customers, testCustomerID, testOtherCustomerID)
	}
	wantCars := []CarPlan{{Plate: "ABC123", Plan: PlanPremium}, {Plate: "DEF456", Plan: PlanNone}}
	if len(customers[0].Cars) != 2 || customers[0].Cars[0] != wantCars[0] || customers[0].Cars[1] != wantCars[1] {
		t.Fatalf("cars = %+v, want %+v", customers[0].Cars, wantCars)
	}
	if len(customers[1].Cars) != 0 {
		t.Fatalf("second customer's cars = %+v, want none", customers[1].Cars)
	}
}

func TestDemoSeedAppliesTwiceWithTheSameRows(t *testing.T) {
	_, database := newTestStore(t)
	applySeed(t, database)

	applySeed(t, database)

	if customers, prices := countRows(t, database, "customers"), countRows(t, database, "prices"); customers != 3 || prices != 3 {
		t.Fatalf("customers = %d, prices = %d, want 3 and 3", customers, prices)
	}
	if vehicles := countRows(t, database, "vehicles"); vehicles != 0 {
		t.Fatalf("vehicles = %d, want 0", vehicles)
	}
}
