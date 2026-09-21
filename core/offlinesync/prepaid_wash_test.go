package offlinesync

// A prepaid wash is spent by the first site to admit its car, and the other sites hear of the spend
// only once that site has pushed and they have pulled. Two sites offline at once can each admit on one
// prepaid wash. Central then stores both washes and marks the first as the spend, and the second stays
// a prepaid wash against a spent prepaid wash, which is where an operator finds it. That is the same
// accepted-and-reconciled cost as the offline Premium cap. Refusing the second push would lose a wash
// that happened, so nothing here guards against it.

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/erclx/washgate/core/internal/central"
	"github.com/erclx/washgate/core/internal/testdb"
)

const (
	prepaidPlate      = "XYZ789"
	prepaidWashID     = "cs_test_single_wash_1"
	prepaidCustomerID = "customer-anna"
	webhookSecret     = "whsec_test_washgate" //nolint:gosec // a fixed secret the test signs with, never a real one
)

type prepaidSystem struct {
	central  http.Handler
	database *testdb.Database
	linkA    *link
	linkB    *link
	siteA    site
	siteB    site
}

type laneAnswer struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
}

func newPrepaidSystem(t *testing.T) *prepaidSystem {
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
	if _, err := database.SQL.ExecContext(t.Context(),
		"INSERT INTO customers (id, email, created_at) VALUES (?, 'anna@example.test', UTC_TIMESTAMP(6))", prepaidCustomerID,
	); err != nil {
		t.Fatalf("add customer: %v", err)
	}
	tokenA, tokenB := rand.Text()+rand.Text(), rand.Text()+rand.Text()
	if err := centralStore.ProvisionSites(t.Context(), []central.SiteToken{
		{SiteID: "site-a", Token: tokenA},
		{SiteID: "site-b", Token: tokenB},
	}); err != nil {
		t.Fatalf("provision sites: %v", err)
	}

	router := central.NewRouter(centralStore, &central.Payments{WebhookSecret: webhookSecret})
	linkA, linkB := &link{central: router}, &link{central: router}
	return &prepaidSystem{
		central:  router,
		database: database,
		linkA:    linkA,
		linkB:    linkB,
		siteA:    openSite(t, linkA, "site-a", tokenA),
		siteB:    openSite(t, linkB, "site-b", tokenB),
	}
}

// payForSingleWash posts the signed event Stripe sends once a single wash is paid.
func (s *prepaidSystem) payForSingleWash(t *testing.T) {
	t.Helper()
	payload := fmt.Sprintf(`{"id":"evt_test_single_wash_1","type":"checkout.session.completed","data":{"object":{`+
		`"id":%q,"mode":"payment","payment_status":"paid",`+
		`"metadata":{"kind":"single_wash","customer_id":%q,"plate":%q}}}}`,
		prepaidWashID, prepaidCustomerID, prepaidPlate)
	signedAt := time.Now().Unix()
	mac := hmac.New(sha256.New, []byte(webhookSecret))
	_, _ = fmt.Fprintf(mac, "%d.%s", signedAt, payload)
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/stripe/webhook", strings.NewReader(payload))
	request.Header.Set("Stripe-Signature", fmt.Sprintf("t=%d,v1=%s", signedAt, hex.EncodeToString(mac.Sum(nil))))
	recorder := httptest.NewRecorder()
	s.central.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("webhook answered %d %q, want 200", recorder.Code, recorder.Body.String())
	}
}

func syncSite(t *testing.T, site site) {
	t.Helper()
	if err := site.syncer.SyncOnce(t.Context()); err != nil {
		t.Fatalf("sync: %v", err)
	}
}

func readPlate(t *testing.T, site site) laneAnswer {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/reads",
		strings.NewReader(fmt.Sprintf(`{"plate": %q, "confidence": 1}`, prepaidPlate)))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	site.lane.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("read answered %d %q, want 200", recorder.Code, recorder.Body.String())
	}
	var answer laneAnswer
	if err := json.NewDecoder(recorder.Body).Decode(&answer); err != nil {
		t.Fatalf("decode read answer: %v", err)
	}
	return answer
}

func (s *prepaidSystem) countCentral(t *testing.T, query string) int {
	t.Helper()
	var count int
	if err := s.database.SQL.QueryRowContext(t.Context(), query).Scan(&count); err != nil {
		t.Fatalf("count %q: %v", query, err)
	}
	return count
}

func TestPrepaidWash(t *testing.T) {
	t.Run("the first site to admit the car spends it and the other site is told to pay", func(t *testing.T) {
		system := newPrepaidSystem(t)
		system.payForSingleWash(t)
		syncSite(t, system.siteA)
		syncSite(t, system.siteB)
		first := readPlate(t, system.siteA)
		syncSite(t, system.siteA)
		syncSite(t, system.siteB)

		second := readPlate(t, system.siteB)

		if first != (laneAnswer{Decision: "admit", Reason: "prepaid_wash"}) {
			t.Errorf("site A answered %+v, want admit prepaid_wash", first)
		}
		if second != (laneAnswer{Decision: "pay", Reason: "unknown_plate"}) {
			t.Errorf("site B answered %+v, want pay unknown_plate", second)
		}
	})

	t.Run("two sites offline at once each admit on it, and central keeps both washes and one spend", func(t *testing.T) {
		system := newPrepaidSystem(t)
		system.payForSingleWash(t)
		syncSite(t, system.siteA)
		syncSite(t, system.siteB)
		system.linkA.isCut.Store(true)
		system.linkB.isCut.Store(true)
		atA := readPlate(t, system.siteA)
		atB := readPlate(t, system.siteB)
		system.linkA.isCut.Store(false)
		system.linkB.isCut.Store(false)

		syncSite(t, system.siteA)
		syncSite(t, system.siteB)

		if atA.Decision != "admit" || atB.Decision != "admit" {
			t.Fatalf("offline answers = %+v and %+v, want both admitted", atA, atB)
		}
		if got := system.countCentral(t, "SELECT COUNT(*) FROM washes WHERE plan = 'prepaid'"); got != 2 {
			t.Errorf("central prepaid washes = %d, want 2", got)
		}
		if got := system.countCentral(t, "SELECT COUNT(*) FROM prepaid_washes WHERE spent_wash_id IS NOT NULL"); got != 1 {
			t.Errorf("spent prepaid washes = %d, want 1", got)
		}
		if got := system.countCentral(t, "SELECT COUNT(*) FROM entitlement_changes WHERE kind = 'prepaid_spent'"); got != 1 {
			t.Errorf("spend changes = %d, want 1", got)
		}
	})
}
