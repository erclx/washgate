package central

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/erclx/washgate/core/internal/testdb"
)

const (
	testSiteID      = "site-arlanda"
	testCompanyID   = "company-nordfrakt"
	testCompanyName = "Nordfrakt AB"
)

var testAdmittedAt = time.Date(2026, 9, 21, 7, 30, 0, 0, time.UTC)

func newTestStore(t *testing.T) (*Store, *testdb.Database) {
	t.Helper()
	database := testdb.New(t)
	store, err := Open(t.Context(), Config{
		Host:     database.Host,
		Port:     database.Port,
		Name:     database.Name,
		User:     database.User,
		Password: database.Password,
	})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store, database
}

func addSite(t *testing.T, database *testdb.Database, id string) {
	t.Helper()
	if _, err := database.SQL.ExecContext(t.Context(), "INSERT INTO sites (id, name) VALUES (?, ?)", id, "Test site"); err != nil {
		t.Fatalf("add site: %v", err)
	}
}

func addCompany(t *testing.T, database *testdb.Database, id, name string) {
	t.Helper()
	if _, err := database.SQL.ExecContext(t.Context(), "INSERT INTO companies (id, name) VALUES (?, ?)", id, name); err != nil {
		t.Fatalf("add company: %v", err)
	}
}

func premiumWash(id, plate string) Wash {
	return Wash{ID: id, Plate: plate, Plan: PlanPremium, AdmittedAt: testAdmittedAt}
}

func countWashes(t *testing.T, database *testdb.Database) int {
	t.Helper()
	var count int
	if err := database.SQL.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM washes").Scan(&count); err != nil {
		t.Fatalf("count washes: %v", err)
	}
	return count
}

func putEntitlement(t *testing.T, store *Store, entitlement Entitlement) {
	t.Helper()
	if err := store.PutEntitlement(t.Context(), entitlement); err != nil {
		t.Fatalf("put entitlement: %v", err)
	}
}

func readChanges(t *testing.T, store *Store, after int64, limit int) []EntitlementChange {
	t.Helper()
	changes, err := store.EntitlementChanges(t.Context(), after, limit)
	if err != nil {
		t.Fatalf("read entitlement changes: %v", err)
	}
	return changes
}

func changePlates(changes []EntitlementChange) []string {
	plates := make([]string, 0, len(changes))
	for _, change := range changes {
		plates = append(plates, change.Plate)
	}
	return plates
}

func TestRecordWashes(t *testing.T) {
	t.Run("stores a new batch once and reports every id as stored", func(t *testing.T) {
		store, database := newTestStore(t)
		addSite(t, database, testSiteID)

		result, err := store.RecordWashes(t.Context(), testSiteID, []Wash{premiumWash("w1", "ABC123"), premiumWash("w2", "DEF456")})

		if err != nil {
			t.Fatalf("record washes: %v", err)
		}
		if !slices.Equal(result.Stored, []string{"w1", "w2"}) || len(result.Duplicates) != 0 {
			t.Fatalf("result = %+v, want both stored", result)
		}
		if got := countWashes(t, database); got != 2 {
			t.Fatalf("washes = %d, want 2", got)
		}
	})

	t.Run("a replayed batch inserts nothing and reports every id as a duplicate", func(t *testing.T) {
		store, database := newTestStore(t)
		addSite(t, database, testSiteID)
		batch := []Wash{premiumWash("w1", "ABC123"), premiumWash("w2", "DEF456")}
		if _, err := store.RecordWashes(t.Context(), testSiteID, batch); err != nil {
			t.Fatalf("record first batch: %v", err)
		}

		result, err := store.RecordWashes(t.Context(), testSiteID, batch)

		if err != nil {
			t.Fatalf("replay batch: %v", err)
		}
		if len(result.Stored) != 0 || !slices.Equal(result.Duplicates, []string{"w1", "w2"}) {
			t.Fatalf("result = %+v, want both duplicates", result)
		}
		if got := countWashes(t, database); got != 2 {
			t.Fatalf("washes = %d, want 2", got)
		}
	})

	t.Run("a batch mixing one new and one stored wash stores only the new one", func(t *testing.T) {
		store, database := newTestStore(t)
		addSite(t, database, testSiteID)
		if _, err := store.RecordWashes(t.Context(), testSiteID, []Wash{premiumWash("w1", "ABC123")}); err != nil {
			t.Fatalf("record first batch: %v", err)
		}

		result, err := store.RecordWashes(t.Context(), testSiteID, []Wash{premiumWash("w1", "ABC123"), premiumWash("w2", "DEF456")})

		if err != nil {
			t.Fatalf("record mixed batch: %v", err)
		}
		if !slices.Equal(result.Stored, []string{"w2"}) || !slices.Equal(result.Duplicates, []string{"w1"}) {
			t.Fatalf("result = %+v, want w2 stored and w1 duplicate", result)
		}
		if got := countWashes(t, database); got != 2 {
			t.Fatalf("washes = %d, want 2", got)
		}
	})

	t.Run("an unknown site rolls the whole batch back", func(t *testing.T) {
		store, database := newTestStore(t)

		_, err := store.RecordWashes(t.Context(), "site-nowhere", []Wash{premiumWash("w1", "ABC123")})

		if !errors.Is(err, ErrUnknownSite) {
			t.Fatalf("error = %v, want ErrUnknownSite", err)
		}
		if got := countWashes(t, database); got != 0 {
			t.Fatalf("washes = %d, want 0", got)
		}
	})

	t.Run("an unknown company rolls the whole batch back", func(t *testing.T) {
		store, database := newTestStore(t)
		addSite(t, database, testSiteID)
		fleetWash := Wash{ID: "w2", Plate: "DEF456", Plan: PlanFleet, CompanyID: "company-nowhere", AdmittedAt: testAdmittedAt}

		_, err := store.RecordWashes(t.Context(), testSiteID, []Wash{premiumWash("w1", "ABC123"), fleetWash})

		if !errors.Is(err, ErrUnknownCompany) {
			t.Fatalf("error = %v, want ErrUnknownCompany", err)
		}
		if got := countWashes(t, database); got != 0 {
			t.Fatalf("washes = %d, want 0", got)
		}
	})

	t.Run("marks the site as synced", func(t *testing.T) {
		store, database := newTestStore(t)
		addSite(t, database, testSiteID)

		if _, err := store.RecordWashes(t.Context(), testSiteID, []Wash{premiumWash("w1", "ABC123")}); err != nil {
			t.Fatalf("record washes: %v", err)
		}

		var isSynced bool
		err := database.SQL.QueryRowContext(t.Context(),
			"SELECT last_synced_at IS NOT NULL FROM sites WHERE id = ?", testSiteID,
		).Scan(&isSynced)
		if err != nil {
			t.Fatalf("read site: %v", err)
		}
		if !isSynced {
			t.Fatal("last_synced_at is null, want it set")
		}
	})
}

func TestPutEntitlement(t *testing.T) {
	t.Run("rejects a premium change carrying a company id", func(t *testing.T) {
		store, database := newTestStore(t)
		addCompany(t, database, testCompanyID, testCompanyName)

		err := store.PutEntitlement(t.Context(), Entitlement{Plate: "ABC123", Plan: PlanPremium, CompanyID: testCompanyID})

		if !errors.Is(err, ErrInvalidEntitlement) {
			t.Fatalf("error = %v, want ErrInvalidEntitlement", err)
		}
		if changes := readChanges(t, store, 0, 10); len(changes) != 0 {
			t.Fatalf("changes = %+v, want none", changes)
		}
	})

	t.Run("rejects a fleet change without a company id", func(t *testing.T) {
		store, _ := newTestStore(t)

		err := store.PutEntitlement(t.Context(), Entitlement{Plate: "ABC123", Plan: PlanFleet})

		if !errors.Is(err, ErrInvalidEntitlement) {
			t.Fatalf("error = %v, want ErrInvalidEntitlement", err)
		}
		if changes := readChanges(t, store, 0, 10); len(changes) != 0 {
			t.Fatalf("changes = %+v, want none", changes)
		}
	})

	t.Run("a writer waiting on an uncommitted change cannot commit ahead of it", func(t *testing.T) {
		store, database := newTestStore(t)
		earlier, err := database.SQL.BeginTx(t.Context(), nil)
		if err != nil {
			t.Fatalf("begin earlier writer: %v", err)
		}
		defer func() { _ = earlier.Rollback() }()
		if _, err := earlier.ExecContext(t.Context(), "SELECT id FROM entitlement_cursor WHERE id = 1 FOR UPDATE"); err != nil {
			t.Fatalf("lock cursor: %v", err)
		}
		if _, err := earlier.ExecContext(t.Context(),
			"INSERT INTO entitlement_changes (plate, plan, changed_at) VALUES ('AAA111', 'premium', UTC_TIMESTAMP(6))",
		); err != nil {
			t.Fatalf("append earlier change: %v", err)
		}
		later := make(chan error, 1)
		go func() { later <- store.PutEntitlement(t.Context(), Entitlement{Plate: "BBB222", Plan: PlanPremium}) }()

		time.Sleep(300 * time.Millisecond)
		changesWhileOpen := readChanges(t, store, 0, 10)
		if err := earlier.Commit(); err != nil {
			t.Fatalf("commit earlier writer: %v", err)
		}
		if err := <-later; err != nil {
			t.Fatalf("later writer: %v", err)
		}

		if len(changesWhileOpen) != 0 {
			t.Fatalf("changes while the earlier writer was open = %v, want none", changePlates(changesWhileOpen))
		}
		if got := changePlates(readChanges(t, store, 0, 10)); !slices.Equal(got, []string{"AAA111", "BBB222"}) {
			t.Fatalf("changes = %v, want AAA111 then BBB222", got)
		}
	})
}

func TestEntitlementChanges(t *testing.T) {
	t.Run("returns changes after the cursor in seq order", func(t *testing.T) {
		store, _ := newTestStore(t)
		putEntitlement(t, store, Entitlement{Plate: "AAA111", Plan: PlanPremium})
		putEntitlement(t, store, Entitlement{Plate: "BBB222", Plan: PlanPremium})
		putEntitlement(t, store, Entitlement{Plate: "CCC333", Plan: PlanPremium})
		first := readChanges(t, store, 0, 10)[0]

		changes := readChanges(t, store, first.Seq, 10)

		if got := changePlates(changes); !slices.Equal(got, []string{"BBB222", "CCC333"}) {
			t.Fatalf("plates = %v, want BBB222 then CCC333", got)
		}
		if changes[0].Seq >= changes[1].Seq {
			t.Fatalf("seq values = %d, %d, want ascending", changes[0].Seq, changes[1].Seq)
		}
	})

	t.Run("a fleet change carries its company", func(t *testing.T) {
		store, database := newTestStore(t)
		addCompany(t, database, testCompanyID, testCompanyName)
		putEntitlement(t, store, Entitlement{Plate: "FLT001", Plan: PlanFleet, CompanyID: testCompanyID})

		changes := readChanges(t, store, 0, 10)

		want := EntitlementChange{Seq: changes[0].Seq, Plate: "FLT001", Plan: PlanFleet, CompanyID: testCompanyID, CompanyName: testCompanyName}
		if changes[0] != want {
			t.Fatalf("change = %+v, want %+v", changes[0], want)
		}
	})

	t.Run("a revoke comes back with no plan", func(t *testing.T) {
		store, _ := newTestStore(t)
		putEntitlement(t, store, Entitlement{Plate: "ABC123", Plan: PlanPremium})
		putEntitlement(t, store, Entitlement{Plate: "ABC123"})

		changes := readChanges(t, store, 0, 10)

		if len(changes) != 2 || changes[1].Plan != PlanNone {
			t.Fatalf("changes = %+v, want a grant then a revoke", changes)
		}
	})

	t.Run("limit pages the result", func(t *testing.T) {
		store, _ := newTestStore(t)
		putEntitlement(t, store, Entitlement{Plate: "AAA111", Plan: PlanPremium})
		putEntitlement(t, store, Entitlement{Plate: "BBB222", Plan: PlanPremium})
		putEntitlement(t, store, Entitlement{Plate: "CCC333", Plan: PlanPremium})

		firstPage := readChanges(t, store, 0, 2)
		secondPage := readChanges(t, store, firstPage[len(firstPage)-1].Seq, 2)

		if got := changePlates(firstPage); !slices.Equal(got, []string{"AAA111", "BBB222"}) {
			t.Fatalf("first page = %v, want AAA111 and BBB222", got)
		}
		if got := changePlates(secondPage); !slices.Equal(got, []string{"CCC333"}) {
			t.Fatalf("second page = %v, want CCC333", got)
		}
	})
}

func TestDownMigrationDropsEveryTable(t *testing.T) {
	database := testdb.New(t)

	database.RollBack(t)

	var tables int
	err := database.SQL.QueryRowContext(t.Context(),
		"SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = ?", database.Name,
	).Scan(&tables)
	if err != nil {
		t.Fatalf("count tables: %v", err)
	}
	if tables != 0 {
		t.Fatalf("tables left = %d, want 0", tables)
	}
}
