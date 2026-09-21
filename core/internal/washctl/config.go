package washctl

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	centralURLVariable   = "WASHCTL_CENTRAL_URL"
	invoicingURLVariable = "WASHCTL_INVOICING_URL"
	siteURLsVariable     = "WASHCTL_SITE_URLS"

	defaultCentralURL   = "http://127.0.0.1:8080"
	defaultInvoicingURL = "http://127.0.0.1:8082"
	defaultSiteURLs     = "site-1=http://127.0.0.1:8081"
)

// Site is one site agent the CLI can ask for its status.
type Site struct {
	ID  string
	URL string
}

// Config says where each service answers and how patient the CLI is with it.
type Config struct {
	CentralURL   string
	InvoicingURL string
	Sites        []Site
	// Timeout bounds one call to central or invoicing.
	Timeout time.Duration
	// SiteTimeout bounds one call to a site agent, short so an offline site reads as offline rather than as a hang.
	SiteTimeout time.Duration
	// RetryAttempts is how many times an idempotent write is tried before giving up.
	RetryAttempts int
	RetryBackoff  time.Duration
}

// LoadConfig reads the WASHCTL_* variables through getenv, falling back to the compose defaults on loopback.
func LoadConfig(getenv func(string) string) (Config, error) {
	centralURL, err := serviceURL(centralURLVariable, valueOr(getenv(centralURLVariable), defaultCentralURL))
	if err != nil {
		return Config{}, err
	}
	invoicingURL, err := serviceURL(invoicingURLVariable, valueOr(getenv(invoicingURLVariable), defaultInvoicingURL))
	if err != nil {
		return Config{}, err
	}
	sites, err := parseSites(valueOr(getenv(siteURLsVariable), defaultSiteURLs))
	if err != nil {
		return Config{}, err
	}
	return Config{
		CentralURL:    centralURL,
		InvoicingURL:  invoicingURL,
		Sites:         sites,
		Timeout:       15 * time.Second,
		SiteTimeout:   3 * time.Second,
		RetryAttempts: 3,
		RetryBackoff:  250 * time.Millisecond,
	}, nil
}

func valueOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func parseSites(raw string) ([]Site, error) {
	var sites []Site
	seen := make(map[string]bool)
	for entry := range strings.SplitSeq(raw, ",") {
		id, rawURL, hasSeparator := strings.Cut(strings.TrimSpace(entry), "=")
		id = strings.TrimSpace(id)
		if !hasSeparator || id == "" {
			return nil, fmt.Errorf("%s entry %q must look like site-id=http://host:port", siteURLsVariable, entry)
		}
		if seen[id] {
			return nil, fmt.Errorf("%s names site %q twice", siteURLsVariable, id)
		}
		seen[id] = true
		siteURL, err := serviceURL(siteURLsVariable, strings.TrimSpace(rawURL))
		if err != nil {
			return nil, err
		}
		sites = append(sites, Site{ID: id, URL: siteURL})
	}
	return sites, nil
}

// serviceURL checks a base URL and drops any trailing slash so paths append cleanly.
func serviceURL(variable, raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", errors.New(variable + " must be an http or https URL with a host, got " + fmt.Sprintf("%q", raw))
	}
	return strings.TrimRight(raw, "/"), nil
}
