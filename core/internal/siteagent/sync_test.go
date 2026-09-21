package siteagent

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"
)

const (
	testSiteID = "site-1"
	testToken  = "test-site-token-that-is-long-enough"
)

type pushedBatch struct {
	SiteID string `json:"site_id"`
	Washes []struct {
		ID            string    `json:"id"`
		Plate         string    `json:"plate"`
		Plan          Plan      `json:"plan"`
		CompanyID     string    `json:"company_id"`
		PrepaidWashID string    `json:"prepaid_wash_id"`
		AdmittedAt    time.Time `json:"admitted_at"`
	} `json:"washes"`
}

// fakeCentral answers pushes and pulls the way central does, from answers a test sets.
type fakeCentral struct {
	mu             sync.Mutex
	pushStatus     int
	acknowledge    func(ids []string) (stored, duplicates []string)
	changes        []map[string]any
	pushes         []pushedBatch
	pullAfters     []string
	authorizations []string
}

func newFakeCentral(t *testing.T) (*fakeCentral, *httptest.Server) {
	t.Helper()
	central := &fakeCentral{
		pushStatus:  http.StatusOK,
		acknowledge: func(ids []string) ([]string, []string) { return ids, nil },
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /washes", central.handlePush)
	mux.HandleFunc("GET /entitlements", central.handlePull)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return central, server
}

func (c *fakeCentral) handlePush(w http.ResponseWriter, r *http.Request) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.authorizations = append(c.authorizations, r.Header.Get("Authorization"))
	var batch pushedBatch
	if err := json.NewDecoder(r.Body).Decode(&batch); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	c.pushes = append(c.pushes, batch)
	if c.pushStatus != http.StatusOK {
		http.Error(w, "central failed", c.pushStatus)
		return
	}
	ids := make([]string, 0, len(batch.Washes))
	for _, wash := range batch.Washes {
		ids = append(ids, wash.ID)
	}
	stored, duplicates := c.acknowledge(ids)
	_ = json.NewEncoder(w).Encode(map[string][]string{"stored": stored, "duplicates": duplicates})
}

func (c *fakeCentral) handlePull(w http.ResponseWriter, r *http.Request) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.authorizations = append(c.authorizations, r.Header.Get("Authorization"))
	after := r.URL.Query().Get("after")
	c.pullAfters = append(c.pullAfters, after)
	afterSeq, _ := strconv.ParseInt(after, 10, 64)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	page := []map[string]any{}
	next := afterSeq
	for _, change := range c.changes {
		seq := change["seq"].(int64)
		if seq > afterSeq && len(page) < limit {
			page = append(page, change)
			next = seq
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"changes": page, "next": next})
}

func premiumWire(seq int64, plate string) map[string]any {
	return map[string]any{"seq": seq, "plate": plate, "plan": "premium", "company_id": nil, "company_name": nil}
}

func fleetWire(seq int64, plate string) map[string]any {
	return map[string]any{"seq": seq, "plate": plate, "plan": "fleet", "company_id": "nordfrakt", "company_name": "Nordfrakt AB"}
}

func revokeWire(seq int64, plate string) map[string]any {
	return map[string]any{"seq": seq, "plate": plate, "plan": nil, "company_id": nil, "company_name": nil}
}

func prepaidWire(seq int64, plate, kind string) map[string]any {
	return map[string]any{
		"seq": seq, "plate": plate, "kind": kind, "prepaid_wash_id": testPrepaidWashID,
		"plan": nil, "company_id": nil, "company_name": nil,
	}
}

func newTestSyncer(store *Store, server *httptest.Server, now time.Time) *Syncer {
	return newLinkedSyncer(store, server, now, NewFeed(), &LinkSwitch{})
}

func newLinkedSyncer(store *Store, server *httptest.Server, now time.Time, feed *Feed, link *LinkSwitch) *Syncer {
	return NewSyncer(store, SyncConfig{
		CentralURL: server.URL,
		SiteID:     testSiteID,
		Token:      testToken,
		Client:     &http.Client{Timeout: 5 * time.Second},
		Now:        func() time.Time { return now },
		Feed:       feed,
		Link:       link,
	})
}

func recordWashes(t *testing.T, store *Store, count int) []string {
	t.Helper()
	ids := make([]string, 0, count)
	for index := range count {
		admittedAt := testNow.Add(time.Duration(index) * time.Second)
		wash, _, err := store.RecordWash(t.Context(), fmt.Sprintf("P%05d", index), Entitlement{Plan: PlanPremium}, "", admittedAt, testWindow)
		if err != nil {
			t.Fatalf("record wash: %v", err)
		}
		ids = append(ids, wash.ID)
	}
	return ids
}

func TestSyncOncePush(t *testing.T) {
	t.Run("sends each pending wash with the site id", func(t *testing.T) {
		store := openSeededStore(t)
		wash, _ := recordWash(t, store, "KLM456", testNow)
		central, server := newFakeCentral(t)

		if err := newTestSyncer(store, server, testNow).SyncOnce(t.Context()); err != nil {
			t.Fatalf("sync: %v", err)
		}

		if len(central.pushes) != 1 || central.pushes[0].SiteID != testSiteID || len(central.pushes[0].Washes) != 1 {
			t.Fatalf("pushes = %+v, want one batch of one wash from %s", central.pushes, testSiteID)
		}
		got := central.pushes[0].Washes[0]
		if got.ID != wash.ID || got.Plate != "KLM456" || got.Plan != PlanFleet || got.CompanyID != "nordfrakt" || !got.AdmittedAt.Equal(testNow) {
			t.Errorf("pushed wash = %+v, want %s KLM456 fleet nordfrakt at %v", got, wash.ID, testNow)
		}
	})

	t.Run("a spent prepaid wash carries its id", func(t *testing.T) {
		store := openSeededStore(t)
		applyChanges(t, store, grantedChange(6, "XYZ789", testPrepaidWashID))
		spendPrepaidWash(t, store, "XYZ789", testPrepaidWashID, testNow)
		central, server := newFakeCentral(t)

		if err := newTestSyncer(store, server, testNow).SyncOnce(t.Context()); err != nil {
			t.Fatalf("sync: %v", err)
		}

		got := central.pushes[0].Washes[0]
		if got.Plan != PlanPrepaid || got.PrepaidWashID != testPrepaidWashID || got.CompanyID != "" {
			t.Errorf("pushed wash = %+v, want prepaid %s with no company", got, testPrepaidWashID)
		}
	})

	t.Run("clears exactly the ids central answered as stored or duplicates", func(t *testing.T) {
		store := openSeededStore(t)
		ids := recordWashes(t, store, 3)
		central, server := newFakeCentral(t)
		central.acknowledge = func([]string) ([]string, []string) { return ids[:1], ids[1:2] }

		err := newTestSyncer(store, server, testNow).SyncOnce(t.Context())

		if err == nil {
			t.Error("sync: nil error, want the unanswered wash reported")
		}
		if got := pendingIDs(t, store); !slices.Equal(got, ids[2:]) {
			t.Errorf("pending = %v, want %v", got, ids[2:])
		}
	})

	t.Run("a push answered 500 clears nothing", func(t *testing.T) {
		store := openSeededStore(t)
		recordWashes(t, store, 2)
		central, server := newFakeCentral(t)
		central.pushStatus = http.StatusInternalServerError

		err := newTestSyncer(store, server, testNow).SyncOnce(t.Context())

		if err == nil {
			t.Error("sync: nil error, want the failed push reported")
		}
		if got := outboxDepth(t, store); got != 2 {
			t.Errorf("outbox depth = %d, want 2", got)
		}
	})

	t.Run("a full outbox goes in batches of the most central takes", func(t *testing.T) {
		store := openSeededStore(t)
		recordWashes(t, store, maxPushBatch+1)
		central, server := newFakeCentral(t)

		if err := newTestSyncer(store, server, testNow).SyncOnce(t.Context()); err != nil {
			t.Fatalf("sync: %v", err)
		}

		if len(central.pushes) != 2 || len(central.pushes[0].Washes) != maxPushBatch || len(central.pushes[1].Washes) != 1 {
			t.Errorf("pushes = %d batches, want %d then 1", len(central.pushes), maxPushBatch)
		}
		if got := outboxDepth(t, store); got != 0 {
			t.Errorf("outbox depth = %d, want 0", got)
		}
	})

	t.Run("an empty outbox sends no push", func(t *testing.T) {
		store := openSeededStore(t)
		central, server := newFakeCentral(t)

		if err := newTestSyncer(store, server, testNow).SyncOnce(t.Context()); err != nil {
			t.Fatalf("sync: %v", err)
		}

		if len(central.pushes) != 0 {
			t.Errorf("pushes = %d, want 0", len(central.pushes))
		}
	})
}

func TestSyncOncePull(t *testing.T) {
	t.Run("applies the changes and moves the cursor and the pull time", func(t *testing.T) {
		store := openStore(t)
		central, server := newFakeCentral(t)
		central.changes = []map[string]any{premiumWire(1, "ABC123"), fleetWire(2, "KLM456"), revokeWire(3, "ABC123")}
		pulledAt := testNow.Add(time.Minute)

		if err := newTestSyncer(store, server, pulledAt).SyncOnce(t.Context()); err != nil {
			t.Fatalf("sync: %v", err)
		}

		facts, err := store.Facts(t.Context(), "KLM456", MonthStart(testNow, time.UTC))
		if err != nil {
			t.Fatalf("read facts: %v", err)
		}
		if facts.Entitlement != (Entitlement{Plan: PlanFleet, CompanyID: "nordfrakt"}) {
			t.Errorf("KLM456 entitlement = %+v, want fleet nordfrakt", facts.Entitlement)
		}
		if got := entitlementOf(t, store, "ABC123"); got != (Entitlement{}) {
			t.Errorf("ABC123 entitlement = %+v, want revoked", got)
		}
		if got := cursorOf(t, store); got != 3 || !facts.LastPulledAt.Equal(pulledAt) {
			t.Errorf("cursor = %d pulled at %v, want 3 at %v", got, facts.LastPulledAt, pulledAt)
		}
	})

	t.Run("a pulled quota reset stops earlier washes counting toward the cap", func(t *testing.T) {
		store := openSeededStore(t)
		recordWash(t, store, "ABC123", testNow.Add(-2*time.Hour))
		central, server := newFakeCentral(t)
		reset := premiumWire(6, "ABC123")
		reset["quota_reset_at"] = testNow.Add(-time.Hour).Format(time.RFC3339Nano)
		central.changes = []map[string]any{reset}

		if err := newTestSyncer(store, server, testNow).SyncOnce(t.Context()); err != nil {
			t.Fatalf("sync: %v", err)
		}

		if got := washesCountedTowardTheCap(t, store, "ABC123"); got != 0 {
			t.Errorf("washes counted = %d, want 0", got)
		}
	})

	t.Run("a pulled grant then spend of a prepaid wash leaves it spent and the plan untouched", func(t *testing.T) {
		store := openSeededStore(t)
		central, server := newFakeCentral(t)
		central.changes = []map[string]any{prepaidWire(6, "ABC123", "prepaid_granted"), prepaidWire(7, "ABC123", "prepaid_spent")}
		applyChanges(t, store, grantedChange(5, "ABC123", "cs_test_kept"))

		if err := newTestSyncer(store, server, testNow).SyncOnce(t.Context()); err != nil {
			t.Fatalf("sync: %v", err)
		}

		if got := prepaidWashOf(t, store, "ABC123"); got != "cs_test_kept" {
			t.Errorf("prepaid wash = %q, want only cs_test_kept left", got)
		}
		if got := entitlementOf(t, store, "ABC123"); got != (Entitlement{Plan: PlanPremium}) {
			t.Errorf("entitlement = %+v, want premium kept", got)
		}
	})

	t.Run("a change with no kind reads as a plan change", func(t *testing.T) {
		store := openStore(t)
		central, server := newFakeCentral(t)
		central.changes = []map[string]any{premiumWire(1, "ABC123")}

		if err := newTestSyncer(store, server, testNow).SyncOnce(t.Context()); err != nil {
			t.Fatalf("sync: %v", err)
		}

		if got := entitlementOf(t, store, "ABC123"); got != (Entitlement{Plan: PlanPremium}) {
			t.Errorf("entitlement = %+v, want premium", got)
		}
	})

	t.Run("asks from the stored cursor", func(t *testing.T) {
		store := openSeededStore(t)
		central, server := newFakeCentral(t)

		if err := newTestSyncer(store, server, testNow).SyncOnce(t.Context()); err != nil {
			t.Fatalf("sync: %v", err)
		}

		if !slices.Equal(central.pullAfters, []string{"5"}) {
			t.Errorf("pulls asked after %v, want [5]", central.pullAfters)
		}
	})

	t.Run("a full page asks again from next", func(t *testing.T) {
		store := openStore(t)
		central, server := newFakeCentral(t)
		for seq := range int64(maxPullPage + 1) {
			central.changes = append(central.changes, premiumWire(seq+1, fmt.Sprintf("P%05d", seq)))
		}

		if err := newTestSyncer(store, server, testNow).SyncOnce(t.Context()); err != nil {
			t.Fatalf("sync: %v", err)
		}

		want := []string{"0", strconv.Itoa(maxPullPage)}
		if !slices.Equal(central.pullAfters, want) {
			t.Errorf("pulls asked after %v, want %v", central.pullAfters, want)
		}
		if got := cursorOf(t, store); got != maxPullPage+1 {
			t.Errorf("cursor = %d, want %d", got, maxPullPage+1)
		}
	})

	t.Run("an empty page still marks the copy as pulled", func(t *testing.T) {
		store := openSeededStore(t)
		_, server := newFakeCentral(t)
		pulledAt := testNow.Add(time.Hour)

		if err := newTestSyncer(store, server, pulledAt).SyncOnce(t.Context()); err != nil {
			t.Fatalf("sync: %v", err)
		}

		facts, err := store.Facts(t.Context(), "XYZ789", MonthStart(testNow, time.UTC))
		if err != nil {
			t.Fatalf("read facts: %v", err)
		}
		if !facts.LastPulledAt.Equal(pulledAt) {
			t.Errorf("last pulled at = %v, want %v", facts.LastPulledAt, pulledAt)
		}
	})

	t.Run("a failed push does not stop the pull", func(t *testing.T) {
		store := openSeededStore(t)
		recordWashes(t, store, 1)
		central, server := newFakeCentral(t)
		central.pushStatus = http.StatusInternalServerError
		central.changes = []map[string]any{premiumWire(6, "NEW001")}

		err := newTestSyncer(store, server, testNow).SyncOnce(t.Context())

		if err == nil {
			t.Error("sync: nil error, want the failed push reported")
		}
		if got := entitlementOf(t, store, "NEW001"); got.Plan != PlanPremium {
			t.Errorf("NEW001 plan = %q, want premium pulled despite the failed push", got.Plan)
		}
	})
}

func TestSyncOnceSendsTheSiteTokenOnEveryRequest(t *testing.T) {
	store := openSeededStore(t)
	recordWashes(t, store, 1)
	central, server := newFakeCentral(t)

	if err := newTestSyncer(store, server, testNow).SyncOnce(t.Context()); err != nil {
		t.Fatalf("sync: %v", err)
	}

	want := []string{"Bearer " + testToken, "Bearer " + testToken}
	if !slices.Equal(central.authorizations, want) {
		t.Errorf("authorizations = %v, want %v", central.authorizations, want)
	}
}

func TestSyncOnceReportsAnUnreachableCentral(t *testing.T) {
	store := openSeededStore(t)
	recordWashes(t, store, 1)
	_, server := newFakeCentral(t)
	server.Close()

	err := newTestSyncer(store, server, testNow.Add(time.Hour)).SyncOnce(t.Context())

	if err == nil {
		t.Error("sync: nil error, want the unreachable central reported")
	}
	facts, factsErr := store.Facts(t.Context(), "XYZ789", MonthStart(testNow, time.UTC))
	if factsErr != nil {
		t.Fatalf("read facts: %v", factsErr)
	}
	if got := outboxDepth(t, store); got != 1 || !facts.LastPulledAt.Equal(testNow) {
		t.Errorf("outbox depth = %d pulled at %v, want 1 at %v", got, facts.LastPulledAt, testNow)
	}
}

func pushCount(central *fakeCentral) int {
	central.mu.Lock()
	defer central.mu.Unlock()
	return len(central.pushes)
}

func TestSyncerObeysTheLinkSwitch(t *testing.T) {
	t.Run("a cut link sends no request", func(t *testing.T) {
		store := openSeededStore(t)
		recordWash(t, store, "ABC123", testNow)
		central, server := newFakeCentral(t)
		link := &LinkSwitch{}
		link.SetCut(true)

		err := newLinkedSyncer(store, server, testNow, NewFeed(), link).tick(t.Context())

		if !errors.Is(err, errLinkCut) || pushCount(central) != 0 || len(central.pullAfters) != 0 {
			t.Errorf("tick = %v with %d pushes and %d pulls, want the cut link and no request", err, pushCount(central), len(central.pullAfters))
		}
	})

	t.Run("restoring the link pushes on the next tick", func(t *testing.T) {
		store := openSeededStore(t)
		recordWash(t, store, "ABC123", testNow)
		central, server := newFakeCentral(t)
		link := &LinkSwitch{}
		syncer := newLinkedSyncer(store, server, testNow, NewFeed(), link)
		link.SetCut(true)
		_ = syncer.tick(t.Context())
		link.SetCut(false)

		err := syncer.tick(t.Context())

		if err != nil || pushCount(central) != 1 || outboxDepth(t, store) != 0 {
			t.Errorf("tick = %v with %d pushes and outbox %d, want one push that empties the outbox", err, pushCount(central), outboxDepth(t, store))
		}
	})
}

func TestSyncOnceReportsExactlyTheClearedWashesToTheFeed(t *testing.T) {
	store := openSeededStore(t)
	ids := recordWashes(t, store, 3)
	central, server := newFakeCentral(t)
	central.acknowledge = func(pushed []string) ([]string, []string) { return pushed[:1], pushed[1:2] }
	feed := NewFeed()
	events := feed.Subscribe(t.Context())

	_ = newLinkedSyncer(store, server, testNow, feed, &LinkSwitch{}).SyncOnce(t.Context())

	got := drain(events)
	if len(got) != 1 || got[0].Type != EventSynced {
		t.Fatalf("events = %+v, want one synced event", got)
	}
	if washIDs := got[0].Body.(SyncedBody).WashIDs; !slices.Equal(washIDs, ids[:2]) {
		t.Errorf("synced washes = %v, want %v", washIDs, ids[:2])
	}
}
