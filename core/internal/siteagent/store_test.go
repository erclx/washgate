package siteagent

import (
	"path/filepath"
	"testing"
	"time"
)

const testWindow = 120 * time.Second

func openSeededStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(t.Context(), filepath.Join(t.TempDir(), "site.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if _, err := store.SeedFixtures(t.Context()); err != nil {
		t.Fatalf("seed fixtures: %v", err)
	}
	return store
}

func recordWash(t *testing.T, store *Store, plate string, admittedAt time.Time) (Wash, bool) {
	t.Helper()
	facts, err := store.Facts(t.Context(), plate, admittedAt)
	if err != nil {
		t.Fatalf("read facts: %v", err)
	}
	wash, isDuplicate, err := store.RecordWash(t.Context(), plate, facts.Entitlement, admittedAt, testWindow)
	if err != nil {
		t.Fatalf("record wash: %v", err)
	}
	return wash, isDuplicate
}

func TestStoreRecordsAWashOnce(t *testing.T) {
	store := openSeededStore(t)

	wash, isDuplicate := recordWash(t, store, "ABC123", testNow)

	facts, err := store.Facts(t.Context(), "ABC123", MonthStart(testNow, time.UTC))
	if err != nil {
		t.Fatalf("read facts: %v", err)
	}
	if isDuplicate {
		t.Errorf("isDuplicate = true, want false")
	}
	if facts.WashesThisMonth != 1 {
		t.Errorf("washes this month = %d, want 1", facts.WashesThisMonth)
	}
	if facts.LastWash.ID != wash.ID || !facts.LastWash.AdmittedAt.Equal(testNow) {
		t.Errorf("last wash = %+v, want %+v", facts.LastWash, wash)
	}
}

func TestStoreReturnsTheFirstWashForARecordInsideTheWindow(t *testing.T) {
	store := openSeededStore(t)
	first, _ := recordWash(t, store, "ABC123", testNow)

	second, isDuplicate := recordWash(t, store, "ABC123", testNow.Add(30*time.Second))

	facts, err := store.Facts(t.Context(), "ABC123", MonthStart(testNow, time.UTC))
	if err != nil {
		t.Fatalf("read facts: %v", err)
	}
	if !isDuplicate || second.ID != first.ID {
		t.Errorf("second record = %+v duplicate %v, want %+v duplicate true", second, isDuplicate, first)
	}
	if facts.WashesThisMonth != 1 {
		t.Errorf("washes this month = %d, want 1", facts.WashesThisMonth)
	}
}

func TestStoreLogsAFleetWashAgainstItsCompany(t *testing.T) {
	store := openSeededStore(t)

	wash, _ := recordWash(t, store, "KLM456", testNow)

	var companyID string
	err := store.db.QueryRowContext(t.Context(), "SELECT company_id FROM washes WHERE id = ?", wash.ID).Scan(&companyID)
	if err != nil {
		t.Fatalf("read wash: %v", err)
	}
	if companyID != "nordfrakt" {
		t.Errorf("company id = %q, want %q", companyID, "nordfrakt")
	}
}

func TestStoreMonthCountIgnoresLastMonthsWashes(t *testing.T) {
	store := openSeededStore(t)
	monthStart := MonthStart(testNow, time.UTC)
	recordWash(t, store, "ABC123", monthStart.Add(-time.Hour))

	recordWash(t, store, "ABC123", monthStart.Add(time.Hour))

	facts, err := store.Facts(t.Context(), "ABC123", monthStart)
	if err != nil {
		t.Fatalf("read facts: %v", err)
	}
	if facts.WashesThisMonth != 1 {
		t.Errorf("washes this month = %d, want 1", facts.WashesThisMonth)
	}
}

func TestStoreFactsForAnUnknownPlateHoldNoEntitlement(t *testing.T) {
	store := openSeededStore(t)

	facts, err := store.Facts(t.Context(), "XYZ789", MonthStart(testNow, time.UTC))
	if err != nil {
		t.Fatalf("read facts: %v", err)
	}
	if facts != (Facts{}) {
		t.Errorf("facts = %+v, want none", facts)
	}
}

func TestStoreSeedsFixturesOnlyIntoAnEmptyCopy(t *testing.T) {
	store := openSeededStore(t)

	isSeeded, err := store.SeedFixtures(t.Context())
	if err != nil {
		t.Fatalf("seed fixtures: %v", err)
	}
	if isSeeded {
		t.Errorf("isSeeded = true on a filled copy, want false")
	}
}

func TestStoreSkipsSeedingACopyHoldingOnlyCompanies(t *testing.T) {
	store, err := Open(t.Context(), filepath.Join(t.TempDir(), "site.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if _, err := store.db.ExecContext(t.Context(), "INSERT INTO companies (id, name) VALUES (?, ?)", "nordfrakt", "Nordfrakt AB"); err != nil {
		t.Fatalf("insert company: %v", err)
	}

	isSeeded, err := store.SeedFixtures(t.Context())
	if err != nil {
		t.Fatalf("seed fixtures: %v", err)
	}
	if isSeeded {
		t.Errorf("isSeeded = true on a copy holding a company, want false")
	}
}

func TestSchemaDownMigrationDropsEveryTable(t *testing.T) {
	store := openSeededStore(t)
	down, err := schemaFiles.ReadFile("schema/001_init.down.sql")
	if err != nil {
		t.Fatalf("read down migration: %v", err)
	}

	if _, err := store.db.ExecContext(t.Context(), string(down)); err != nil {
		t.Fatalf("run down migration: %v", err)
	}

	var remaining int
	err = store.db.QueryRowContext(t.Context(),
		"SELECT COUNT(*) FROM sqlite_master WHERE type IN ('table', 'index') AND name NOT LIKE 'sqlite_%'").Scan(&remaining)
	if err != nil {
		t.Fatalf("count schema objects: %v", err)
	}
	if remaining != 0 {
		t.Errorf("schema objects left = %d, want 0", remaining)
	}
}
