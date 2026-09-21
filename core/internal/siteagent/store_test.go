package siteagent

import (
	"path/filepath"
	"testing"
	"time"
)

const testWindow = 120 * time.Second

func openStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(t.Context(), filepath.Join(t.TempDir(), "site.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func premiumChange(seq int64, plate string) EntitlementChange {
	return EntitlementChange{Seq: seq, Plate: plate, Plan: PlanPremium}
}

func fleetChange(seq int64, plate string) EntitlementChange {
	return EntitlementChange{Seq: seq, Plate: plate, Plan: PlanFleet, CompanyID: "nordfrakt", CompanyName: "Nordfrakt AB"}
}

func revokeChange(seq int64, plate string) EntitlementChange {
	return EntitlementChange{Seq: seq, Plate: plate}
}

func testEntitlements() []EntitlementChange {
	return []EntitlementChange{
		premiumChange(1, "ABC123"),
		fleetChange(2, "KLM456"),
		fleetChange(3, "KLM457"),
		premiumChange(4, "TAX123"),
		premiumChange(5, "CLEAN"),
	}
}

// openSeededStore holds the test entitlements, applied the way a pull from central applies them, pulled at testNow.
func openSeededStore(t *testing.T) *Store {
	t.Helper()
	store := openStore(t)
	changes := testEntitlements()
	if err := store.ApplyChanges(t.Context(), changes, changes[len(changes)-1].Seq, testNow); err != nil {
		t.Fatalf("apply test entitlements: %v", err)
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

func outboxDepth(t *testing.T, store *Store) int {
	t.Helper()
	depth, err := store.OutboxDepth(t.Context())
	if err != nil {
		t.Fatalf("read outbox depth: %v", err)
	}
	return depth
}

func pendingIDs(t *testing.T, store *Store) []string {
	t.Helper()
	pending, err := store.PendingWashes(t.Context(), maxPushBatch)
	if err != nil {
		t.Fatalf("read pending washes: %v", err)
	}
	ids := make([]string, 0, len(pending))
	for _, wash := range pending {
		ids = append(ids, wash.ID)
	}
	return ids
}

func entitlementOf(t *testing.T, store *Store, plate string) Entitlement {
	t.Helper()
	facts, err := store.Facts(t.Context(), plate, MonthStart(testNow, time.UTC))
	if err != nil {
		t.Fatalf("read facts: %v", err)
	}
	return facts.Entitlement
}

func cursorOf(t *testing.T, store *Store) int64 {
	t.Helper()
	cursor, err := store.Cursor(t.Context())
	if err != nil {
		t.Fatalf("read cursor: %v", err)
	}
	return cursor
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
	if facts.Entitlement != (Entitlement{}) || facts.WashesThisMonth != 0 || facts.LastWash != (Wash{}) {
		t.Errorf("facts = %+v, want no entitlement and no washes", facts)
	}
}

func TestStoreFactsCarryTheLastPullTime(t *testing.T) {
	t.Run("a pulled copy answers with its pull time", func(t *testing.T) {
		store := openSeededStore(t)

		facts, err := store.Facts(t.Context(), "XYZ789", MonthStart(testNow, time.UTC))
		if err != nil {
			t.Fatalf("read facts: %v", err)
		}
		if !facts.LastPulledAt.Equal(testNow) {
			t.Errorf("last pulled at = %v, want %v", facts.LastPulledAt, testNow)
		}
	})

	t.Run("a copy never pulled answers with no pull time", func(t *testing.T) {
		store := openStore(t)

		facts, err := store.Facts(t.Context(), "XYZ789", MonthStart(testNow, time.UTC))
		if err != nil {
			t.Fatalf("read facts: %v", err)
		}
		if !facts.LastPulledAt.IsZero() {
			t.Errorf("last pulled at = %v, want zero", facts.LastPulledAt)
		}
	})
}

func TestStoreOutbox(t *testing.T) {
	t.Run("a recorded wash waits in the outbox with what central needs", func(t *testing.T) {
		store := openSeededStore(t)

		wash, _ := recordWash(t, store, "KLM456", testNow)

		pending, err := store.PendingWashes(t.Context(), maxPushBatch)
		if err != nil {
			t.Fatalf("read pending washes: %v", err)
		}
		want := PendingWash{ID: wash.ID, Plate: "KLM456", Plan: PlanFleet, CompanyID: "nordfrakt", AdmittedAt: testNow}
		if len(pending) != 1 || pending[0] != want {
			t.Errorf("pending = %+v, want [%+v]", pending, want)
		}
	})

	t.Run("a duplicate read adds no outbox row", func(t *testing.T) {
		store := openSeededStore(t)
		recordWash(t, store, "ABC123", testNow)

		recordWash(t, store, "ABC123", testNow.Add(30*time.Second))

		if got := outboxDepth(t, store); got != 1 {
			t.Errorf("outbox depth = %d, want 1", got)
		}
	})

	t.Run("pending washes come oldest first up to the limit", func(t *testing.T) {
		store := openSeededStore(t)
		first, _ := recordWash(t, store, "ABC123", testNow)
		second, _ := recordWash(t, store, "TAX123", testNow.Add(time.Second))
		recordWash(t, store, "CLEAN", testNow.Add(2*time.Second))

		pending, err := store.PendingWashes(t.Context(), 2)
		if err != nil {
			t.Fatalf("read pending washes: %v", err)
		}
		if len(pending) != 2 || pending[0].ID != first.ID || pending[1].ID != second.ID {
			t.Errorf("pending = %+v, want the two oldest", pending)
		}
	})

	t.Run("clearing removes only the named washes and keeps the ledger", func(t *testing.T) {
		store := openSeededStore(t)
		first, _ := recordWash(t, store, "ABC123", testNow)
		second, _ := recordWash(t, store, "TAX123", testNow.Add(time.Second))

		if err := store.ClearOutbox(t.Context(), []string{first.ID}); err != nil {
			t.Fatalf("clear outbox: %v", err)
		}

		if got := pendingIDs(t, store); len(got) != 1 || got[0] != second.ID {
			t.Errorf("pending = %v, want [%s]", got, second.ID)
		}
		if got := countWashes(t, store); got != 2 {
			t.Errorf("washes = %d, want 2", got)
		}
	})

	t.Run("clearing twice is the same as clearing once", func(t *testing.T) {
		store := openSeededStore(t)
		wash, _ := recordWash(t, store, "ABC123", testNow)
		if err := store.ClearOutbox(t.Context(), []string{wash.ID}); err != nil {
			t.Fatalf("clear outbox: %v", err)
		}

		err := store.ClearOutbox(t.Context(), []string{wash.ID, "never-recorded"})

		if err != nil {
			t.Errorf("second clear: %v, want nil", err)
		}
		if got := outboxDepth(t, store); got != 0 {
			t.Errorf("outbox depth = %d, want 0", got)
		}
	})
}

func TestStoreApplyChanges(t *testing.T) {
	t.Run("a fleet car arrives with its company", func(t *testing.T) {
		store := openStore(t)

		err := store.ApplyChanges(t.Context(), []EntitlementChange{fleetChange(7, "KLM456")}, 7, testNow)

		if err != nil {
			t.Fatalf("apply changes: %v", err)
		}
		want := Entitlement{Plan: PlanFleet, CompanyID: "nordfrakt"}
		if got := entitlementOf(t, store, "KLM456"); got != want {
			t.Errorf("entitlement = %+v, want %+v", got, want)
		}
	})

	t.Run("a later change replaces the plate's plan", func(t *testing.T) {
		store := openSeededStore(t)

		err := store.ApplyChanges(t.Context(), []EntitlementChange{fleetChange(6, "ABC123")}, 6, testNow)

		if err != nil {
			t.Fatalf("apply changes: %v", err)
		}
		if got := entitlementOf(t, store, "ABC123"); got.Plan != PlanFleet {
			t.Errorf("plan = %q, want fleet", got.Plan)
		}
	})

	t.Run("a revoked plate holds no entitlement", func(t *testing.T) {
		store := openSeededStore(t)

		err := store.ApplyChanges(t.Context(), []EntitlementChange{revokeChange(6, "ABC123")}, 6, testNow)

		if err != nil {
			t.Fatalf("apply changes: %v", err)
		}
		if got := entitlementOf(t, store, "ABC123"); got != (Entitlement{}) {
			t.Errorf("entitlement = %+v, want none", got)
		}
	})

	t.Run("revoking a plate that washed keeps its washes", func(t *testing.T) {
		store := openSeededStore(t)
		recordWash(t, store, "KLM456", testNow)

		err := store.ApplyChanges(t.Context(), []EntitlementChange{revokeChange(6, "KLM456")}, 6, testNow)

		if err != nil {
			t.Fatalf("apply changes: %v", err)
		}
		if got := countWashes(t, store); got != 1 {
			t.Errorf("washes = %d, want 1", got)
		}
	})

	t.Run("a change that alters nothing is a no-op", func(t *testing.T) {
		store := openSeededStore(t)

		err := store.ApplyChanges(t.Context(), []EntitlementChange{premiumChange(6, "ABC123")}, 6, testNow)

		if err != nil {
			t.Fatalf("apply changes: %v", err)
		}
		if got := entitlementOf(t, store, "ABC123"); got != (Entitlement{Plan: PlanPremium}) {
			t.Errorf("entitlement = %+v, want premium", got)
		}
	})

	t.Run("the cursor and the pull time move together", func(t *testing.T) {
		store := openStore(t)
		pulledAt := testNow.Add(time.Minute)

		err := store.ApplyChanges(t.Context(), []EntitlementChange{premiumChange(9, "ABC123")}, 9, pulledAt)

		if err != nil {
			t.Fatalf("apply changes: %v", err)
		}
		facts, err := store.Facts(t.Context(), "ABC123", MonthStart(testNow, time.UTC))
		if err != nil {
			t.Fatalf("read facts: %v", err)
		}
		if got := cursorOf(t, store); got != 9 || !facts.LastPulledAt.Equal(pulledAt) {
			t.Errorf("cursor = %d pulled at %v, want 9 at %v", got, facts.LastPulledAt, pulledAt)
		}
	})

	t.Run("a refused page leaves the cursor and the copy where they were", func(t *testing.T) {
		store := openSeededStore(t)
		fleetWithoutCompany := EntitlementChange{Seq: 7, Plate: "NEW001", Plan: PlanFleet}

		err := store.ApplyChanges(t.Context(), []EntitlementChange{premiumChange(6, "NEW002"), fleetWithoutCompany}, 7, testNow.Add(time.Minute))

		if err == nil {
			t.Fatal("apply changes: nil error, want the page refused")
		}
		if got := cursorOf(t, store); got != 5 {
			t.Errorf("cursor = %d, want 5", got)
		}
		if got := entitlementOf(t, store, "NEW002"); got != (Entitlement{}) {
			t.Errorf("entitlement = %+v, want none from a refused page", got)
		}
	})
}

func TestStoreReopensAnExistingCopyWithoutReapplyingItsSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "site.db")
	first, err := Open(t.Context(), path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := first.ApplyChanges(t.Context(), testEntitlements(), 5, testNow); err != nil {
		t.Fatalf("apply changes: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	second, err := Open(t.Context(), path)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	t.Cleanup(func() { _ = second.Close() })

	if got := cursorOf(t, second); got != 5 {
		t.Errorf("cursor = %d, want 5 kept across a reopen", got)
	}
}

func TestSchemaDownMigrationsDropEveryTable(t *testing.T) {
	store := openSeededStore(t)
	recordWash(t, store, "ABC123", testNow)
	downs := []string{"schema/002_sync.down.sql", "schema/001_init.down.sql"}

	for _, name := range downs {
		down, err := schemaFiles.ReadFile(name)
		if err != nil {
			t.Fatalf("read down migration %s: %v", name, err)
		}
		if _, err := store.db.ExecContext(t.Context(), string(down)); err != nil {
			t.Fatalf("run down migration %s: %v", name, err)
		}
	}

	var remaining int
	err := store.db.QueryRowContext(t.Context(),
		"SELECT COUNT(*) FROM sqlite_master WHERE type IN ('table', 'index') AND name NOT LIKE 'sqlite_%'").Scan(&remaining)
	if err != nil {
		t.Fatalf("count schema objects: %v", err)
	}
	if remaining != 0 {
		t.Errorf("schema objects left = %d, want 0", remaining)
	}
}
