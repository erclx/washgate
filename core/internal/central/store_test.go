package central

import (
	"crypto/rand"
	"database/sql"
	"errors"
	"os"
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

// provisionSite provisions siteID as the only site holding a token, the way central's startup does
// with a one-entry list, and returns the token it now answers to.
func provisionSite(t *testing.T, store *Store, siteID string) string {
	t.Helper()
	token := rand.Text() + rand.Text()
	if err := store.ProvisionSites(t.Context(), []SiteToken{{SiteID: siteID, Token: token}}); err != nil {
		t.Fatalf("provision site: %v", err)
	}
	return token
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

	for _, plate := range []string{"abc123", "ABC 123"} {
		t.Run("rejects the plate "+plate+" that no site would look up", func(t *testing.T) {
			store, _ := newTestStore(t)

			err := store.PutEntitlement(t.Context(), Entitlement{Plate: plate, Plan: PlanPremium})

			if !errors.Is(err, ErrInvalidEntitlement) {
				t.Fatalf("error = %v, want ErrInvalidEntitlement", err)
			}
			if changes := readChanges(t, store, 0, 10); len(changes) != 0 {
				t.Fatalf("changes = %+v, want none", changes)
			}
		})
	}

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

func TestProvisionSites(t *testing.T) {
	t.Run("provisioning twice leaves one row per site answering to the latest token", func(t *testing.T) {
		store, database := newTestStore(t)
		firstToken := provisionSite(t, store, testSiteID)
		latestToken := provisionSite(t, store, testSiteID)

		siteID, err := store.SiteForToken(t.Context(), hashSiteToken(latestToken))

		if err != nil || siteID != testSiteID {
			t.Fatalf("site for latest token = %q, error %v, want %q", siteID, err, testSiteID)
		}
		if _, err := store.SiteForToken(t.Context(), hashSiteToken(firstToken)); !errors.Is(err, ErrUnknownToken) {
			t.Fatalf("error for replaced token = %v, want ErrUnknownToken", err)
		}
		var sites int
		if err := database.SQL.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM sites").Scan(&sites); err != nil {
			t.Fatalf("count sites: %v", err)
		}
		if sites != 1 {
			t.Fatalf("sites = %d, want 1", sites)
		}
	})

	t.Run("a token a dropped site still holds moves to the site now named with it", func(t *testing.T) {
		store, _ := newTestStore(t)
		token := provisionSite(t, store, "site-kista")

		err := store.ProvisionSites(t.Context(), []SiteToken{{SiteID: testSiteID, Token: token}})

		if err != nil {
			t.Fatalf("provision site: %v", err)
		}
		if siteID, err := store.SiteForToken(t.Context(), hashSiteToken(token)); err != nil || siteID != testSiteID {
			t.Fatalf("site for token = %q, error %v, want %q", siteID, err, testSiteID)
		}
	})

	t.Run("a site left out of the list keeps its row and loses its token", func(t *testing.T) {
		store, database := newTestStore(t)
		droppedToken := provisionSite(t, store, "site-kista")

		keptToken := provisionSite(t, store, testSiteID)

		if _, err := store.SiteForToken(t.Context(), hashSiteToken(droppedToken)); !errors.Is(err, ErrUnknownToken) {
			t.Fatalf("error for dropped site's token = %v, want ErrUnknownToken", err)
		}
		if siteID, err := store.SiteForToken(t.Context(), hashSiteToken(keptToken)); err != nil || siteID != testSiteID {
			t.Fatalf("site for kept token = %q, error %v, want %q", siteID, err, testSiteID)
		}
		var sites int
		if err := database.SQL.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM sites").Scan(&sites); err != nil {
			t.Fatalf("count sites: %v", err)
		}
		if sites != 2 {
			t.Fatalf("sites = %d, want 2", sites)
		}
	})

	t.Run("an empty list revokes every token", func(t *testing.T) {
		store, _ := newTestStore(t)
		token := provisionSite(t, store, testSiteID)

		err := store.ProvisionSites(t.Context(), nil)

		if err != nil {
			t.Fatalf("provision no sites: %v", err)
		}
		if _, err := store.SiteForToken(t.Context(), hashSiteToken(token)); !errors.Is(err, ErrUnknownToken) {
			t.Fatalf("error = %v, want ErrUnknownToken", err)
		}
	})

	t.Run("keeps the name and sync time of a site central already holds", func(t *testing.T) {
		store, database := newTestStore(t)
		addSite(t, database, testSiteID)
		if _, err := store.RecordWashes(t.Context(), testSiteID, []Wash{premiumWash("w1", "ABC123")}); err != nil {
			t.Fatalf("record washes: %v", err)
		}

		provisionSite(t, store, testSiteID)

		var name string
		var isSynced bool
		err := database.SQL.QueryRowContext(t.Context(),
			"SELECT name, last_synced_at IS NOT NULL FROM sites WHERE id = ?", testSiteID,
		).Scan(&name, &isSynced)
		if err != nil {
			t.Fatalf("read site: %v", err)
		}
		if name != "Test site" || !isSynced {
			t.Fatalf("name %q synced %v, want the original name and a sync time", name, isSynced)
		}
	})
}

func TestSiteForToken(t *testing.T) {
	t.Run("a token no site holds is unknown", func(t *testing.T) {
		store, _ := newTestStore(t)
		provisionSite(t, store, testSiteID)

		_, err := store.SiteForToken(t.Context(), hashSiteToken(rand.Text()+rand.Text()))

		if !errors.Is(err, ErrUnknownToken) {
			t.Fatalf("error = %v, want ErrUnknownToken", err)
		}
	})

	t.Run("a site with no token answers to no hash", func(t *testing.T) {
		store, database := newTestStore(t)
		addSite(t, database, testSiteID)

		_, err := store.SiteForToken(t.Context(), [32]byte{})

		if !errors.Is(err, ErrUnknownToken) {
			t.Fatalf("error = %v, want ErrUnknownToken", err)
		}
	})
}

func TestSiteTokensDownMigrationRestoresSites(t *testing.T) {
	database := testdb.New(t)
	statements, err := os.ReadFile("../../migrations/0003_site_tokens.down.sql")
	if err != nil {
		t.Fatalf("read down migration: %v", err)
	}

	if _, err := database.SQL.ExecContext(t.Context(), string(statements)); err != nil {
		t.Fatalf("apply down migration: %v", err)
	}

	rows, err := database.SQL.QueryContext(t.Context(),
		"SELECT column_name FROM information_schema.columns WHERE table_schema = ? AND table_name = 'sites' ORDER BY ordinal_position",
		database.Name,
	)
	if err != nil {
		t.Fatalf("read sites columns: %v", err)
	}
	defer func() { _ = rows.Close() }()
	columns := []string{}
	for rows.Next() {
		var column string
		if err := rows.Scan(&column); err != nil {
			t.Fatalf("scan column: %v", err)
		}
		columns = append(columns, column)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate columns: %v", err)
	}
	if want := []string{"id", "name", "last_synced_at"}; !slices.Equal(columns, want) {
		t.Fatalf("sites columns = %v, want %v", columns, want)
	}
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

const (
	testCustomerID           = "customer-anna"
	testStripeSubscriptionID = "sub_test_0001"
)

var testPeriodEnd = time.Date(2026, 10, 21, 7, 30, 0, 0, time.UTC)

type storedSubscription struct {
	Plate     string
	Plan      string
	Status    string
	PeriodEnd time.Time
}

func addCustomer(t *testing.T, database *testdb.Database, id string) {
	t.Helper()
	if _, err := database.SQL.ExecContext(t.Context(),
		"INSERT INTO customers (id, email, created_at) VALUES (?, ?, UTC_TIMESTAMP(6))", id, id+"@example.test",
	); err != nil {
		t.Fatalf("add customer: %v", err)
	}
}

func addPrice(t *testing.T, database *testdb.Database, code string, amountOre int64, validFrom time.Time) {
	t.Helper()
	if _, err := database.SQL.ExecContext(t.Context(),
		"INSERT INTO prices (code, amount_ore, valid_from) VALUES (?, ?, ?)", code, amountOre, validFrom,
	); err != nil {
		t.Fatalf("add price: %v", err)
	}
}

func paidInvoice(eventID string, periodEnd time.Time) SubscriptionEvent {
	return SubscriptionEvent{
		EventID:              eventID,
		EventType:            "invoice.paid",
		Action:               SubscriptionPaid,
		StripeSubscriptionID: testStripeSubscriptionID,
		CustomerID:           testCustomerID,
		Plate:                "ABC123",
		PeriodEnd:            periodEnd,
	}
}

func deletedSubscription(eventID, stripeSubscriptionID string) SubscriptionEvent {
	return SubscriptionEvent{
		EventID:              eventID,
		EventType:            "customer.subscription.deleted",
		Action:               SubscriptionEnded,
		StripeSubscriptionID: stripeSubscriptionID,
	}
}

func applyEvent(t *testing.T, store *Store, event SubscriptionEvent) EventOutcome {
	t.Helper()
	outcome, err := store.ApplySubscriptionEvent(t.Context(), event)
	if err != nil {
		t.Fatalf("apply event %s: %v", event.EventID, err)
	}
	return outcome
}

func readSubscriptions(t *testing.T, database *testdb.Database) []storedSubscription {
	t.Helper()
	rows, err := database.SQL.QueryContext(t.Context(),
		"SELECT plate, plan, status, current_period_end FROM subscriptions ORDER BY id")
	if err != nil {
		t.Fatalf("read subscriptions: %v", err)
	}
	defer func() { _ = rows.Close() }()
	subscriptions := []storedSubscription{}
	for rows.Next() {
		var subscription storedSubscription
		var periodEnd sql.NullTime
		if err := rows.Scan(&subscription.Plate, &subscription.Plan, &subscription.Status, &periodEnd); err != nil {
			t.Fatalf("scan subscription: %v", err)
		}
		subscription.PeriodEnd = periodEnd.Time
		subscriptions = append(subscriptions, subscription)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate subscriptions: %v", err)
	}
	return subscriptions
}

func countStripeEvents(t *testing.T, database *testdb.Database) int {
	t.Helper()
	var count int
	if err := database.SQL.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM stripe_events").Scan(&count); err != nil {
		t.Fatalf("count stripe events: %v", err)
	}
	return count
}

func TestApplySubscriptionEvent(t *testing.T) {
	t.Run("the first paid invoice starts an active Premium subscription and appends one change", func(t *testing.T) {
		store, database := newTestStore(t)
		addCustomer(t, database, testCustomerID)

		outcome := applyEvent(t, store, paidInvoice("evt_1", testPeriodEnd))

		if outcome != EventApplied {
			t.Fatalf("outcome = %s, want %s", outcome, EventApplied)
		}
		want := []storedSubscription{{Plate: "ABC123", Plan: "premium", Status: "active", PeriodEnd: testPeriodEnd}}
		if got := readSubscriptions(t, database); !slices.Equal(got, want) {
			t.Fatalf("subscriptions = %+v, want %+v", got, want)
		}
		changes := readChanges(t, store, 0, 10)
		if len(changes) != 1 || changes[0].Plate != "ABC123" || changes[0].Plan != PlanPremium {
			t.Fatalf("changes = %+v, want one premium change for ABC123", changes)
		}
	})

	t.Run("the same event id again changes nothing and reports a duplicate", func(t *testing.T) {
		store, database := newTestStore(t)
		addCustomer(t, database, testCustomerID)
		applyEvent(t, store, paidInvoice("evt_1", testPeriodEnd))

		outcome := applyEvent(t, store, paidInvoice("evt_1", testPeriodEnd.AddDate(0, 1, 0)))

		if outcome != EventDuplicate {
			t.Fatalf("outcome = %s, want %s", outcome, EventDuplicate)
		}
		if got := readSubscriptions(t, database); len(got) != 1 || !got[0].PeriodEnd.Equal(testPeriodEnd) {
			t.Fatalf("subscriptions = %+v, want one ending %s", got, testPeriodEnd)
		}
		if changes := readChanges(t, store, 0, 10); len(changes) != 1 {
			t.Fatalf("changes = %d, want 1", len(changes))
		}
	})

	t.Run("a later paid invoice extends the period once and appends an unchanged plan", func(t *testing.T) {
		store, database := newTestStore(t)
		addCustomer(t, database, testCustomerID)
		applyEvent(t, store, paidInvoice("evt_1", testPeriodEnd))
		nextPeriodEnd := testPeriodEnd.AddDate(0, 1, 0)

		outcome := applyEvent(t, store, paidInvoice("evt_2", nextPeriodEnd))

		if outcome != EventApplied {
			t.Fatalf("outcome = %s, want %s", outcome, EventApplied)
		}
		want := []storedSubscription{{Plate: "ABC123", Plan: "premium", Status: "active", PeriodEnd: nextPeriodEnd}}
		if got := readSubscriptions(t, database); !slices.Equal(got, want) {
			t.Fatalf("subscriptions = %+v, want %+v", got, want)
		}
		changes := readChanges(t, store, 0, 10)
		if len(changes) != 2 || changes[1].Plan != PlanPremium {
			t.Fatalf("changes = %+v, want a second premium change", changes)
		}
	})

	t.Run("an older period never shortens the subscription", func(t *testing.T) {
		store, database := newTestStore(t)
		addCustomer(t, database, testCustomerID)
		applyEvent(t, store, paidInvoice("evt_2", testPeriodEnd.AddDate(0, 1, 0)))

		applyEvent(t, store, paidInvoice("evt_1", testPeriodEnd))

		if got := readSubscriptions(t, database); len(got) != 1 || !got[0].PeriodEnd.Equal(testPeriodEnd.AddDate(0, 1, 0)) {
			t.Fatalf("subscriptions = %+v, want one ending %s", got, testPeriodEnd.AddDate(0, 1, 0))
		}
	})

	t.Run("a paid invoice replaces the plate's other active subscription", func(t *testing.T) {
		store, database := newTestStore(t)
		addCustomer(t, database, testCustomerID)
		putEntitlement(t, store, Entitlement{Plate: "ABC123", Plan: PlanPremium, CustomerID: testCustomerID})

		applyEvent(t, store, paidInvoice("evt_1", testPeriodEnd))

		var active int
		if err := database.SQL.QueryRowContext(t.Context(),
			"SELECT COUNT(*) FROM subscriptions WHERE plate = 'ABC123' AND status = 'active'").Scan(&active); err != nil {
			t.Fatalf("count active subscriptions: %v", err)
		}
		if active != 1 {
			t.Fatalf("active subscriptions = %d, want 1", active)
		}
	})

	t.Run("a deletion cancels the subscription and appends a revoke with no plan", func(t *testing.T) {
		store, database := newTestStore(t)
		addCustomer(t, database, testCustomerID)
		applyEvent(t, store, paidInvoice("evt_1", testPeriodEnd))

		outcome := applyEvent(t, store, deletedSubscription("evt_2", testStripeSubscriptionID))

		if outcome != EventApplied {
			t.Fatalf("outcome = %s, want %s", outcome, EventApplied)
		}
		if got := readSubscriptions(t, database); len(got) != 1 || got[0].Status != "canceled" {
			t.Fatalf("subscriptions = %+v, want one canceled", got)
		}
		changes := readChanges(t, store, 0, 10)
		if len(changes) != 2 || changes[1].Plate != "ABC123" || changes[1].Plan != PlanNone {
			t.Fatalf("changes = %+v, want a revoke for ABC123", changes)
		}
	})

	t.Run("a deletion for an unknown subscription changes nothing but its event id", func(t *testing.T) {
		store, database := newTestStore(t)

		outcome := applyEvent(t, store, deletedSubscription("evt_1", "sub_unknown"))

		if outcome != EventIgnored {
			t.Fatalf("outcome = %s, want %s", outcome, EventIgnored)
		}
		if changes := readChanges(t, store, 0, 10); len(changes) != 0 {
			t.Fatalf("changes = %+v, want none", changes)
		}
		if got := countStripeEvents(t, database); got != 1 {
			t.Fatalf("stripe events = %d, want 1", got)
		}
	})

	t.Run("a paid invoice after the deletion does not bring the subscription back", func(t *testing.T) {
		store, database := newTestStore(t)
		addCustomer(t, database, testCustomerID)
		applyEvent(t, store, paidInvoice("evt_1", testPeriodEnd))
		applyEvent(t, store, deletedSubscription("evt_2", testStripeSubscriptionID))

		outcome := applyEvent(t, store, paidInvoice("evt_3", testPeriodEnd.AddDate(0, 1, 0)))

		if outcome != EventIgnored {
			t.Fatalf("outcome = %s, want %s", outcome, EventIgnored)
		}
		if got := readSubscriptions(t, database); len(got) != 1 || got[0].Status != "canceled" {
			t.Fatalf("subscriptions = %+v, want one canceled", got)
		}
	})

	t.Run("a paid invoice for an unknown customer is rejected and leaves no event id", func(t *testing.T) {
		store, database := newTestStore(t)

		_, err := store.ApplySubscriptionEvent(t.Context(), paidInvoice("evt_1", testPeriodEnd))

		if !errors.Is(err, ErrUnknownCustomer) {
			t.Fatalf("error = %v, want ErrUnknownCustomer", err)
		}
		if got := countStripeEvents(t, database); got != 0 {
			t.Fatalf("stripe events = %d, want 0", got)
		}
	})

	t.Run("a paid invoice for a plate no site would look up is rejected", func(t *testing.T) {
		store, database := newTestStore(t)
		addCustomer(t, database, testCustomerID)
		event := paidInvoice("evt_1", testPeriodEnd)
		event.Plate = "abc 123"

		_, err := store.ApplySubscriptionEvent(t.Context(), event)

		if !errors.Is(err, ErrInvalidEntitlement) {
			t.Fatalf("error = %v, want ErrInvalidEntitlement", err)
		}
	})

	t.Run("an ignored type stores only its event id", func(t *testing.T) {
		store, database := newTestStore(t)

		outcome := applyEvent(t, store, SubscriptionEvent{EventID: "evt_1", EventType: "checkout.session.completed", Action: SubscriptionUnchanged})

		if outcome != EventIgnored {
			t.Fatalf("outcome = %s, want %s", outcome, EventIgnored)
		}
		if got := countStripeEvents(t, database); got != 1 {
			t.Fatalf("stripe events = %d, want 1", got)
		}
		if got := readSubscriptions(t, database); len(got) != 0 {
			t.Fatalf("subscriptions = %+v, want none", got)
		}
	})
}

func TestPremiumPrice(t *testing.T) {
	t.Run("reads the latest premium price valid at the time", func(t *testing.T) {
		store, database := newTestStore(t)
		addPrice(t, database, "premium", 24900, testAdmittedAt.AddDate(-1, 0, 0))
		addPrice(t, database, "premium", 29900, testAdmittedAt.AddDate(0, -1, 0))
		addPrice(t, database, "premium", 34900, testAdmittedAt.AddDate(0, 1, 0))
		addPrice(t, database, "single", 14900, testAdmittedAt.AddDate(0, 0, -1))

		amount, err := store.PremiumPrice(t.Context(), testAdmittedAt)

		if err != nil {
			t.Fatalf("premium price: %v", err)
		}
		if amount != 29900 {
			t.Fatalf("amount = %d, want 29900", amount)
		}
	})

	t.Run("reports no price when none is valid yet", func(t *testing.T) {
		store, database := newTestStore(t)
		addPrice(t, database, "premium", 29900, testAdmittedAt.AddDate(0, 1, 0))

		_, err := store.PremiumPrice(t.Context(), testAdmittedAt)

		if !errors.Is(err, ErrNoPremiumPrice) {
			t.Fatalf("error = %v, want ErrNoPremiumPrice", err)
		}
	})
}

func TestIsPlateHeldByAnother(t *testing.T) {
	t.Run("a plate another customer holds actively is held", func(t *testing.T) {
		store, database := newTestStore(t)
		addCustomer(t, database, "customer-bo")
		putEntitlement(t, store, Entitlement{Plate: "ABC123", Plan: PlanPremium, CustomerID: "customer-bo"})

		held, err := store.IsPlateHeldByAnother(t.Context(), "ABC123", testCustomerID)

		if err != nil || !held {
			t.Fatalf("held = %t, error = %v, want true", held, err)
		}
	})

	t.Run("a plate another customer registered with no subscription is held", func(t *testing.T) {
		store, database := newTestStore(t)
		addCustomer(t, database, "customer-bo")
		if _, err := database.SQL.ExecContext(t.Context(),
			"INSERT INTO vehicles (plate, customer_id) VALUES ('ABC123', 'customer-bo')"); err != nil {
			t.Fatalf("register vehicle: %v", err)
		}

		held, err := store.IsPlateHeldByAnother(t.Context(), "ABC123", testCustomerID)

		if err != nil || !held {
			t.Fatalf("held = %t, error = %v, want true", held, err)
		}
	})

	t.Run("an unregistered plate is not held", func(t *testing.T) {
		store, _ := newTestStore(t)

		held, err := store.IsPlateHeldByAnother(t.Context(), "ABC123", testCustomerID)

		if err != nil || held {
			t.Fatalf("held = %t, error = %v, want false", held, err)
		}
	})

	t.Run("a plate the same customer holds is not held by another", func(t *testing.T) {
		store, database := newTestStore(t)
		addCustomer(t, database, testCustomerID)
		putEntitlement(t, store, Entitlement{Plate: "ABC123", Plan: PlanPremium, CustomerID: testCustomerID})

		held, err := store.IsPlateHeldByAnother(t.Context(), "ABC123", testCustomerID)

		if err != nil || held {
			t.Fatalf("held = %t, error = %v, want false", held, err)
		}
	})

	t.Run("a fleet car is held by its company", func(t *testing.T) {
		store, database := newTestStore(t)
		addCompany(t, database, testCompanyID, testCompanyName)
		putEntitlement(t, store, Entitlement{Plate: "FLT001", Plan: PlanFleet, CompanyID: testCompanyID})

		held, err := store.IsPlateHeldByAnother(t.Context(), "FLT001", testCustomerID)

		if err != nil || !held {
			t.Fatalf("held = %t, error = %v, want true", held, err)
		}
	})
}

func TestCustomerExists(t *testing.T) {
	store, database := newTestStore(t)
	addCustomer(t, database, testCustomerID)

	known, errKnown := store.CustomerExists(t.Context(), testCustomerID)
	unknown, errUnknown := store.CustomerExists(t.Context(), "customer-nobody")

	if errKnown != nil || errUnknown != nil {
		t.Fatalf("errors = %v, %v, want none", errKnown, errUnknown)
	}
	if !known || unknown {
		t.Fatalf("exists = %t for the customer and %t for nobody, want true and false", known, unknown)
	}
}
