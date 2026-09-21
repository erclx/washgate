// Package offlinesync runs central and a site agent against each other with the link between them cut.
// It plays the role a binary plays, importing both components, which neither component may do.
//
// Two sites offline at once can each admit the same Premium car past its monthly cap, and central's
// month count for that plate then exceeds eight. That is accepted and reconciled rather than prevented,
// so nothing here guards against it.
package offlinesync

import (
	"crypto/rand"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/erclx/washgate/core/internal/central"
	"github.com/erclx/washgate/core/internal/siteagent"
	"github.com/erclx/washgate/core/internal/testdb"
)

const siteID = "site-1"

var plates = []string{"ABC123", "DEF456", "GHI789"}

// link joins the site to central and can be cut two ways: before a request reaches central,
// or after central has answered but before the answer reaches the site.
type link struct {
	central     http.Handler
	isCut       atomic.Bool
	isAnswerCut atomic.Bool
}

func (l *link) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if l.isCut.Load() {
		panic(http.ErrAbortHandler)
	}
	recorder := httptest.NewRecorder()
	l.central.ServeHTTP(recorder, r)
	if l.isAnswerCut.Load() {
		panic(http.ErrAbortHandler)
	}
	for key, values := range recorder.Header() {
		w.Header()[key] = values
	}
	w.WriteHeader(recorder.Code)
	_, _ = w.Write(recorder.Body.Bytes())
}

type system struct {
	link     *link
	database *testdb.Database
	store    *siteagent.Store
	lane     http.Handler
	syncer   *siteagent.Syncer
}

func newSystem(t *testing.T) *system {
	t.Helper()
	database := testdb.New(t)
	centralStore, err := central.Open(t.Context(), central.Config{
		Host:     database.Host,
		Port:     database.Port,
		Name:     database.Name,
		User:     database.User,
		Password: database.Password,
	})
	if err != nil {
		t.Fatalf("open central: %v", err)
	}
	t.Cleanup(func() { _ = centralStore.Close() })
	token := rand.Text() + rand.Text()
	if err := centralStore.ProvisionSites(t.Context(), []central.SiteToken{{SiteID: siteID, Token: token}}); err != nil {
		t.Fatalf("provision site: %v", err)
	}
	for _, plate := range plates {
		if err := centralStore.PutEntitlement(t.Context(), central.Entitlement{Plate: plate, Plan: central.PlanPremium}); err != nil {
			t.Fatalf("grant premium to %s: %v", plate, err)
		}
	}

	link := &link{central: central.NewRouter(centralStore, nil)}
	server := httptest.NewServer(link)
	t.Cleanup(server.Close)

	store, err := siteagent.Open(t.Context(), filepath.Join(t.TempDir(), "site.db"))
	if err != nil {
		t.Fatalf("open site: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	now := time.Now().UTC()
	syncer := siteagent.NewSyncer(store, siteagent.SyncConfig{
		CentralURL: server.URL,
		SiteID:     siteID,
		Token:      token,
		Client:     &http.Client{Timeout: 5 * time.Second},
		Now:        func() time.Time { return now },
	})
	if err := syncer.SyncOnce(t.Context()); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	lane := siteagent.NewRouter(store, siteagent.Config{
		Policy:   siteagent.Policy{MinConfidence: 0.99, DedupWindow: 120 * time.Second, MaxOffline: 10 * time.Minute},
		Location: time.UTC,
		Now:      func() time.Time { return now },
	})
	return &system{link: link, database: database, store: store, lane: lane, syncer: syncer}
}

func (s *system) admitEveryPlate(t *testing.T) {
	t.Helper()
	for _, plate := range plates {
		request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/reads",
			strings.NewReader(fmt.Sprintf(`{"plate": %q, "confidence": 1}`, plate)))
		request.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		s.lane.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"decision":"admit"`) {
			t.Fatalf("read of %s answered %d %q, want admit", plate, recorder.Code, recorder.Body.String())
		}
	}
}

func (s *system) centralWashes(t *testing.T) int {
	t.Helper()
	var count int
	if err := s.database.SQL.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM washes").Scan(&count); err != nil {
		t.Fatalf("count central washes: %v", err)
	}
	return count
}

func (s *system) outboxDepth(t *testing.T) int {
	t.Helper()
	depth, err := s.store.OutboxDepth(t.Context())
	if err != nil {
		t.Fatalf("read outbox depth: %v", err)
	}
	return depth
}

func TestOfflineSync(t *testing.T) {
	t.Run("washes admitted with the link cut reach central once it returns", func(t *testing.T) {
		system := newSystem(t)
		system.link.isCut.Store(true)
		system.admitEveryPlate(t)
		if err := system.syncer.SyncOnce(t.Context()); err == nil {
			t.Fatal("sync over a cut link: nil error, want it reported")
		}
		if got := system.centralWashes(t); got != 0 {
			t.Fatalf("central washes while cut = %d, want 0", got)
		}

		system.link.isCut.Store(false)
		err := system.syncer.SyncOnce(t.Context())

		if err != nil {
			t.Fatalf("sync after restore: %v", err)
		}
		if got := system.centralWashes(t); got != len(plates) {
			t.Errorf("central washes = %d, want %d", got, len(plates))
		}
		if got := system.outboxDepth(t); got != 0 {
			t.Errorf("outbox depth = %d, want 0", got)
		}
	})

	t.Run("a batch central stored but whose answer was lost is stored once", func(t *testing.T) {
		system := newSystem(t)
		system.admitEveryPlate(t)
		system.link.isAnswerCut.Store(true)
		if err := system.syncer.SyncOnce(t.Context()); err == nil {
			t.Fatal("sync with the answer cut: nil error, want it reported")
		}
		if got := system.centralWashes(t); got != len(plates) {
			t.Fatalf("central washes after the lost answer = %d, want %d", got, len(plates))
		}

		system.link.isAnswerCut.Store(false)
		err := system.syncer.SyncOnce(t.Context())

		if err != nil {
			t.Fatalf("sync after restore: %v", err)
		}
		if got := system.centralWashes(t); got != len(plates) {
			t.Errorf("central washes = %d, want %d", got, len(plates))
		}
		if got := system.outboxDepth(t); got != 0 {
			t.Errorf("outbox depth = %d, want 0", got)
		}
	})
}
