package washctl

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const plateBody = `{"plate":"ABC123","owner_type":"private","owner_name":"Anna Berg","leasing_company":null,` +
	`"subscription":{"plan":"premium","status":"active"},"washes_this_month":3,"washes_since_reset":1,` +
	`"quota_reset_at":"2026-09-10T12:00:00Z","washes":[]}`

const invoiceCSV = "company,plate,washes\nAcme,ABC123,4\n"

func newConfig(centralURL, invoicingURL string, sites ...Site) Config {
	return Config{
		CentralURL:    centralURL,
		InvoicingURL:  invoicingURL,
		Sites:         sites,
		Timeout:       2 * time.Second,
		SiteTimeout:   500 * time.Millisecond,
		RetryAttempts: 3,
		RetryBackoff:  time.Millisecond,
	}
}

func newServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

func run(t *testing.T, config Config, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = Run(t.Context(), args, &out, &errOut, config)
	return code, out.String(), errOut.String()
}

func respond(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

func TestRunPrintsUsageWithoutArguments(t *testing.T) {
	code, stdout, stderr := run(t, newConfig("http://unused", "http://unused"))

	if code != 2 || stdout != "" || !strings.Contains(stderr, "usage: washctl") {
		t.Fatalf("got code %d stdout %q stderr %q, want code 2, empty stdout, usage on stderr", code, stdout, stderr)
	}
}

func TestRunRefusesUnknownSubcommand(t *testing.T) {
	code, stdout, stderr := run(t, newConfig("http://unused", "http://unused"), "polish")

	if code != 2 || stdout != "" || !strings.Contains(stderr, `unknown command "polish"`) {
		t.Fatalf("got code %d stdout %q stderr %q, want code 2 naming the command", code, stdout, stderr)
	}
}

func TestPlateRequiresExactlyOnePlate(t *testing.T) {
	code, _, stderr := run(t, newConfig("http://unused", "http://unused"), "plate")

	if code != 2 || !strings.Contains(stderr, "usage: washctl plate") {
		t.Fatalf("got code %d stderr %q, want code 2 with plate usage", code, stderr)
	}
}

func TestPlatePrintsOwnerPlanAndWashCounts(t *testing.T) {
	central := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/plates/ABC123" {
			t.Errorf("got path %q, want /plates/ABC123", r.URL.Path)
		}
		_, _ = w.Write([]byte(plateBody))
	})

	code, stdout, stderr := run(t, newConfig(central.URL, "http://unused"), "plate", "ABC123")

	if code != 0 || stderr != "" {
		t.Fatalf("got code %d stderr %q, want success and empty stderr", code, stderr)
	}
	for _, want := range []string{"Anna Berg", "premium", "active", "3", "1", "2026-09-10"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout %q lacks %q", stdout, want)
		}
	}
}

func TestPlateJSONPrintsCentralBody(t *testing.T) {
	central := newServer(t, respond(http.StatusOK, plateBody))

	code, stdout, _ := run(t, newConfig(central.URL, "http://unused"), "plate", "--json", "ABC123")

	var decoded map[string]any
	if err := json.Unmarshal([]byte(stdout), &decoded); code != 0 || err != nil || decoded["plate"] != "ABC123" {
		t.Fatalf("got code %d stdout %q (%v), want JSON carrying the plate", code, stdout, err)
	}
}

func TestPlateAcceptsFlagAfterPlate(t *testing.T) {
	central := newServer(t, respond(http.StatusOK, plateBody))

	code, stdout, _ := run(t, newConfig(central.URL, "http://unused"), "plate", "ABC123", "--json")

	if code != 0 || !strings.HasPrefix(stdout, "{") {
		t.Fatalf("got code %d stdout %q, want JSON output", code, stdout)
	}
}

func TestPlateReportsUnknownPlateOnStderr(t *testing.T) {
	central := newServer(t, respond(http.StatusNotFound, "central holds no vehicle with this plate\n"))

	code, stdout, stderr := run(t, newConfig(central.URL, "http://unused"), "plate", "ZZZ999")

	if code != 1 || stdout != "" || !strings.Contains(stderr, "no vehicle with this plate") {
		t.Fatalf("got code %d stdout %q stderr %q, want code 1 and the reason on stderr", code, stdout, stderr)
	}
}

func TestQuotaResetSendsGeneratedIDAndNote(t *testing.T) {
	var received struct{ ID, Note string }
	central := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/plates/ABC123/quota-resets" {
			t.Errorf("got %s %s, want POST /plates/ABC123/quota-resets", r.Method, r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"` + received.ID + `","plate":"ABC123","reset_at":"2026-09-21T08:00:00Z","note":"goodwill"}`))
	})

	code, stdout, stderr := run(t, newConfig(central.URL, "http://unused"), "quota-reset", "--note", "goodwill", "ABC123")

	if code != 0 || stderr != "" || received.ID == "" || received.Note != "goodwill" {
		t.Fatalf("got code %d stderr %q request %+v, want success with an id and the note", code, stderr, received)
	}
	if !strings.Contains(stdout, "next pull") || !strings.Contains(stdout, received.ID) {
		t.Errorf("stdout %q should name the reset id and say sites apply it at their next pull", stdout)
	}
}

func TestQuotaResetRetryAfterDroppedConnectionReusesID(t *testing.T) {
	var ids []string
	var calls atomic.Int32
	central := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		var body struct{ ID string }
		_ = json.NewDecoder(r.Body).Decode(&body)
		ids = append(ids, body.ID)
		if calls.Add(1) == 1 {
			panic(http.ErrAbortHandler)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"` + body.ID + `","plate":"ABC123","reset_at":"2026-09-21T08:00:00Z","note":null}`))
	})

	code, _, stderr := run(t, newConfig(central.URL, "http://unused"), "quota-reset", "ABC123")

	if code != 0 || len(ids) != 2 || ids[0] == "" || ids[0] != ids[1] {
		t.Fatalf("got code %d ids %v stderr %q, want success with one id sent twice", code, ids, stderr)
	}
}

func TestQuotaResetReportsRepeatAsUnchanged(t *testing.T) {
	central := newServer(t, respond(http.StatusOK, `{"id":"x","plate":"ABC123","reset_at":"2026-09-21T08:00:00Z","note":null}`))

	code, stdout, _ := run(t, newConfig(central.URL, "http://unused"), "quota-reset", "ABC123")

	if code != 0 || !strings.Contains(stdout, "already recorded") {
		t.Fatalf("got code %d stdout %q, want success saying the reset was already recorded", code, stdout)
	}
}

func TestQuotaResetNamesWhyCentralRefused(t *testing.T) {
	central := newServer(t, respond(http.StatusConflict, "only a Premium plate has a monthly cap to reset\n"))

	code, stdout, stderr := run(t, newConfig(central.URL, "http://unused"), "quota-reset", "ABC123")

	if code != 1 || stdout != "" || !strings.Contains(stderr, "only a Premium plate") {
		t.Fatalf("got code %d stdout %q stderr %q, want code 1 and the reason on stderr", code, stdout, stderr)
	}
}

func TestInvoiceRejectsMalformedMonthWithoutCallingInvoicing(t *testing.T) {
	var calls atomic.Int32
	invoicing := newServer(t, func(http.ResponseWriter, *http.Request) { calls.Add(1) })

	for _, month := range []string{"2026-13", "2026-00", "202609", "2026-9", "september"} {
		t.Run(month, func(t *testing.T) {
			code, stdout, stderr := run(t, newConfig("http://unused", invoicing.URL), "invoice", month)

			if code != 2 || stdout != "" || !strings.Contains(stderr, "YYYY-MM") {
				t.Fatalf("got code %d stdout %q stderr %q, want code 2 naming YYYY-MM", code, stdout, stderr)
			}
		})
	}
	if calls.Load() != 0 {
		t.Errorf("got %d calls to invoicing, want none", calls.Load())
	}
}

func TestInvoiceWritesCSVToStdoutOnly(t *testing.T) {
	invoicing := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/invoices/2026-09.csv" || r.URL.RawQuery != "" {
			t.Errorf("got %s?%s, want /invoices/2026-09.csv with no query", r.URL.Path, r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(invoiceCSV))
	})

	code, stdout, stderr := run(t, newConfig("http://unused", invoicing.URL), "invoice", "2026-09")

	if code != 0 || stdout != invoiceCSV || stderr != "" {
		t.Fatalf("got code %d stdout %q stderr %q, want the CSV alone on stdout", code, stdout, stderr)
	}
}

func TestInvoiceSplitAsksForLeasingColumn(t *testing.T) {
	var query string
	invoicing := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		_, _ = w.Write([]byte(invoiceCSV))
	})

	code, _, _ := run(t, newConfig("http://unused", invoicing.URL), "invoice", "2026-09", "--split", "leasing")

	if code != 0 || query != "split=leasing" {
		t.Fatalf("got code %d query %q, want split=leasing", code, query)
	}
}

func TestInvoiceRejectsUnknownSplit(t *testing.T) {
	code, _, stderr := run(t, newConfig("http://unused", "http://unused"), "invoice", "2026-09", "--split", "color")

	if code != 2 || !strings.Contains(stderr, "leasing") {
		t.Fatalf("got code %d stderr %q, want code 2 naming leasing as the only split", code, stderr)
	}
}

func TestInvoiceWritesToFileWhenAsked(t *testing.T) {
	invoicing := newServer(t, respond(http.StatusOK, invoiceCSV))
	path := filepath.Join(t.TempDir(), "sept.csv")

	code, stdout, _ := run(t, newConfig("http://unused", invoicing.URL), "invoice", "2026-09", "-o", path)

	written, err := os.ReadFile(path) //nolint:gosec // the path sits under t.TempDir
	if code != 0 || err != nil || string(written) != invoiceCSV || stdout != "" {
		t.Fatalf("got code %d file %q (%v) stdout %q, want the CSV in the file and none on stdout", code, written, err, stdout)
	}
}

func TestInvoiceFailureLeavesStdoutAndFileUntouched(t *testing.T) {
	invoicing := newServer(t, respond(http.StatusUnprocessableEntity, "no fleet_wash price is in force\n"))
	path := filepath.Join(t.TempDir(), "sept.csv")

	code, stdout, stderr := run(t, newConfig("http://unused", invoicing.URL), "invoice", "2026-09", "-o", path)

	_, statErr := os.Stat(path)
	if code != 1 || stdout != "" || !strings.Contains(stderr, "fleet_wash price") || !os.IsNotExist(statErr) {
		t.Fatalf("got code %d stdout %q stderr %q file err %v, want the reason on stderr and no file", code, stdout, stderr, statErr)
	}
}

func healthCentral(t *testing.T) *httptest.Server {
	t.Helper()
	return newServer(t, respond(http.StatusOK, `{"sites":[`+
		`{"id":"site-1","name":"Kista","last_synced_at":"2026-09-21T09:00:00Z"},`+
		`{"id":"site-2","name":"Nord","last_synced_at":null}]}`))
}

func closedURL(t *testing.T) string {
	t.Helper()
	server := httptest.NewServer(http.NotFoundHandler())
	server.Close()
	return server.URL
}

func TestHealthRendersUnreachableSiteWithoutFailing(t *testing.T) {
	site := newServer(t, respond(http.StatusOK, `{"site_id":"site-1","outbox_depth":4,"last_synced_at":"2026-09-21T09:05:00Z"}`))
	config := newConfig(healthCentral(t).URL, "http://unused", Site{ID: "site-1", URL: site.URL}, Site{ID: "site-2", URL: closedURL(t)})

	code, stdout, stderr := run(t, config, "health")

	if code != 0 || stderr != "" {
		t.Fatalf("got code %d stderr %q, want success with an offline site", code, stderr)
	}
	if !strings.Contains(stdout, "2026-09-21T09:05:00Z") || !strings.Contains(stdout, "unreachable") {
		t.Errorf("stdout %q should show site-1's last pull and site-2 as unreachable", stdout)
	}
}

func TestHealthJSONCarriesOneRowPerSite(t *testing.T) {
	site := newServer(t, respond(http.StatusOK, `{"site_id":"site-1","outbox_depth":4,"last_synced_at":null}`))
	config := newConfig(healthCentral(t).URL, "http://unused", Site{ID: "site-1", URL: site.URL})

	code, stdout, _ := run(t, config, "health", "--json")

	var rows []struct {
		SiteID      string `json:"site_id"`
		State       string `json:"state"`
		OutboxDepth *int   `json:"outbox_depth"`
	}
	if err := json.Unmarshal([]byte(stdout), &rows); code != 0 || err != nil || len(rows) != 2 {
		t.Fatalf("got code %d rows %v (%v), want two rows", code, rows, err)
	}
	if rows[0].State != "ok" || rows[0].OutboxDepth == nil || *rows[0].OutboxDepth != 4 || rows[1].State != "not_configured" {
		t.Errorf("got rows %+v, want site-1 ok with depth 4 and site-2 not configured", rows)
	}
}

func TestHealthFailsWhenCentralIsUnreachable(t *testing.T) {
	code, stdout, stderr := run(t, newConfig(closedURL(t), "http://unused"), "health")

	if code != 1 || stdout != "" || stderr == "" {
		t.Fatalf("got code %d stdout %q stderr %q, want code 1 with the reason on stderr", code, stdout, stderr)
	}
}

func TestRunStopsWhenContextIsCanceled(t *testing.T) {
	central := newServer(t, respond(http.StatusOK, plateBody))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var out, errOut bytes.Buffer

	code := Run(ctx, []string{"plate", "ABC123"}, &out, &errOut, newConfig(central.URL, "http://unused"))

	if code != 1 || out.Len() != 0 {
		t.Fatalf("got code %d stdout %q, want code 1 and no output", code, out.String())
	}
}

func TestLoadConfigFallsBackToLocalDefaults(t *testing.T) {
	config, err := LoadConfig(func(string) string { return "" })

	if err != nil || config.CentralURL != "http://127.0.0.1:8080" || config.InvoicingURL != "http://127.0.0.1:8082" ||
		len(config.Sites) != 1 || config.Sites[0] != (Site{ID: "site-1", URL: "http://127.0.0.1:8081"}) {
		t.Fatalf("got %+v (%v), want the loopback defaults with site-1 on 8081", config, err)
	}
}

func TestLoadConfigReadsSiteList(t *testing.T) {
	env := map[string]string{"WASHCTL_SITE_URLS": "site-1=http://a:1, site-2=http://b:2/"}

	config, err := LoadConfig(func(key string) string { return env[key] })

	if err != nil || len(config.Sites) != 2 || config.Sites[1] != (Site{ID: "site-2", URL: "http://b:2"}) {
		t.Fatalf("got %+v (%v), want two sites with the trailing slash trimmed", config.Sites, err)
	}
}

func TestLoadConfigRejectsMalformedEntries(t *testing.T) {
	for _, value := range []string{"site-1", "=http://a:1", "site-1=", "site-1=not a url", "site-1=ftp://a", "site-1=http://a:1,site-1=http://b:2"} {
		t.Run(value, func(t *testing.T) {
			env := map[string]string{"WASHCTL_SITE_URLS": value}

			_, err := LoadConfig(func(key string) string { return env[key] })

			if err == nil || !strings.Contains(err.Error(), "WASHCTL_SITE_URLS") {
				t.Fatalf("got %v, want an error naming WASHCTL_SITE_URLS", err)
			}
		})
	}
}

func TestLoadConfigRejectsBadServiceURL(t *testing.T) {
	env := map[string]string{"WASHCTL_CENTRAL_URL": "central"}

	_, err := LoadConfig(func(key string) string { return env[key] })

	if err == nil || !strings.Contains(err.Error(), "WASHCTL_CENTRAL_URL") {
		t.Fatalf("got %v, want an error naming WASHCTL_CENTRAL_URL", err)
	}
}
