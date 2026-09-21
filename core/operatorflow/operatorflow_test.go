// Package operatorflow runs the washctl CLI against central's real router over a real MariaDB.
// It plays the role a binary plays, importing the CLI and central together, which neither component may do.
package operatorflow

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/erclx/washgate/core/internal/central"
	"github.com/erclx/washgate/core/internal/testdb"
	"github.com/erclx/washgate/core/internal/washctl"
)

const (
	siteID = "site-1"
	plate  = "ABC123"
)

// flakyAnswers lets central handle the first quota reset and then drops the answer, the way a link
// that fails after the write leaves the caller not knowing whether it landed.
type flakyAnswers struct {
	central     http.Handler
	isDropArmed atomic.Bool
}

func (f *flakyAnswers) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !strings.HasSuffix(r.URL.Path, "/quota-resets") || !f.isDropArmed.CompareAndSwap(true, false) {
		f.central.ServeHTTP(w, r)
		return
	}
	f.central.ServeHTTP(httptest.NewRecorder(), r)
	panic(http.ErrAbortHandler)
}

type system struct {
	front  *flakyAnswers
	config washctl.Config
	token  string
	url    string
}

func newSystem(t *testing.T) *system {
	t.Helper()
	database := testdb.New(t)
	store, err := central.Open(t.Context(), central.Config{
		Host:     database.Host,
		Port:     database.Port,
		Name:     database.Name,
		User:     database.User,
		Password: database.Password,
	})
	if err != nil {
		t.Fatalf("open central: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	token := rand.Text() + rand.Text()
	if err := store.ProvisionSites(t.Context(), []central.SiteToken{{SiteID: siteID, Token: token}}); err != nil {
		t.Fatalf("provision site: %v", err)
	}
	if err := store.PutEntitlement(t.Context(), central.Entitlement{Plate: plate, Plan: central.PlanPremium}); err != nil {
		t.Fatalf("grant premium: %v", err)
	}

	front := &flakyAnswers{central: central.NewRouter(store, nil)}
	server := httptest.NewServer(front)
	t.Cleanup(server.Close)
	return &system{
		front: front,
		token: token,
		url:   server.URL,
		config: washctl.Config{
			CentralURL:    server.URL,
			InvoicingURL:  "http://127.0.0.1:1",
			Timeout:       5 * time.Second,
			SiteTimeout:   time.Second,
			RetryAttempts: 3,
			RetryBackoff:  time.Millisecond,
		},
	}
}

func (s *system) washctl(t *testing.T, args ...string) string {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if code := washctl.Run(t.Context(), args, &stdout, &stderr, s.config); code != 0 {
		t.Fatalf("washctl %v exited %d with stderr %q", args, code, stderr.String())
	}
	return stdout.String()
}

// resetChanges is what a site pulling now would see: the entitlement changes for the plate that carry a reset.
func (s *system) resetChanges(t *testing.T) int {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, s.url+"/entitlements", nil)
	if err != nil {
		t.Fatalf("build pull: %v", err)
	}
	request.Header.Set("Authorization", "Bearer "+s.token)
	answer, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("pull entitlements: %v", err)
	}
	defer func() { _ = answer.Body.Close() }()
	var pulled struct {
		Changes []struct {
			Plate        string     `json:"plate"`
			QuotaResetAt *time.Time `json:"quota_reset_at"`
		} `json:"changes"`
	}
	if err := json.NewDecoder(answer.Body).Decode(&pulled); err != nil {
		t.Fatalf("decode pull: %v", err)
	}
	count := 0
	for _, change := range pulled.Changes {
		if change.Plate == plate && change.QuotaResetAt != nil {
			count++
		}
	}
	return count
}

func TestQuotaResetReachesTheEntitlementPull(t *testing.T) {
	system := newSystem(t)

	output := system.washctl(t, "quota-reset", "--note", "goodwill", plate)

	if got := system.resetChanges(t); got != 1 {
		t.Errorf("got %d reset changes in the pull, want 1", got)
	}
	if !strings.Contains(output, "Recorded quota reset") {
		t.Errorf("output %q should say the reset was recorded", output)
	}
	if lookup := system.washctl(t, "plate", plate); !strings.Contains(lookup, "Last quota reset:") || strings.Contains(lookup, "never") {
		t.Errorf("plate lookup %q should show the reset time", lookup)
	}
}

func TestQuotaResetRetriedAfterALostAnswerChangesOnce(t *testing.T) {
	system := newSystem(t)
	system.front.isDropArmed.Store(true)

	output := system.washctl(t, "quota-reset", plate)

	if got := system.resetChanges(t); got != 1 {
		t.Errorf("got %d reset changes in the pull, want 1", got)
	}
	if !strings.Contains(output, "already recorded") {
		t.Errorf("output %q should say the retry found the reset already recorded", output)
	}
}
