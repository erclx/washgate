package siteagent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"time"
)

const (
	// maxPushBatch is the most washes central takes in one POST /washes.
	maxPushBatch = 500
	// maxPullPage is the page size the site asks GET /entitlements for.
	maxPullPage       = 500
	maxAnswerBodySize = 1 << 20
)

// SyncConfig holds where central is, who this site is to it, and the clock that stamps each pull.
type SyncConfig struct {
	CentralURL string
	SiteID     string
	Token      string
	Client     *http.Client
	Now        func() time.Time
}

// Syncer pushes the outbox to central and pulls central's entitlement changes into the copy.
type Syncer struct {
	store  *Store
	config SyncConfig
}

// NewSyncer returns a Syncer over store. config.Client carries the timeout every call to central runs under.
func NewSyncer(store *Store, config SyncConfig) *Syncer {
	return &Syncer{store: store, config: config}
}

type pushRequest struct {
	SiteID string     `json:"site_id"`
	Washes []pushWash `json:"washes"`
}

type pushWash struct {
	ID         string    `json:"id"`
	Plate      string    `json:"plate"`
	Plan       Plan      `json:"plan"`
	CompanyID  string    `json:"company_id,omitempty"`
	AdmittedAt time.Time `json:"admitted_at"`
}

type pushAnswer struct {
	Stored     []string `json:"stored"`
	Duplicates []string `json:"duplicates"`
}

type pullAnswer struct {
	Changes []pullChange `json:"changes"`
	Next    int64        `json:"next"`
}

type pullChange struct {
	Seq         int64   `json:"seq"`
	Plate       string  `json:"plate"`
	Plan        *string `json:"plan"`
	CompanyID   *string `json:"company_id"`
	CompanyName *string `json:"company_name"`
}

// Run syncs once at start and then every interval until ctx ends, logging only when the link
// to central goes up or down rather than on every failed tick.
func (s *Syncer) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	var wasOnline *bool
	for {
		err := s.SyncOnce(ctx)
		if ctx.Err() != nil {
			return
		}
		isOnline := err == nil
		if wasOnline == nil || *wasOnline != isOnline {
			s.logLinkChange(ctx, err)
		}
		wasOnline = &isOnline
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Syncer) logLinkChange(ctx context.Context, err error) {
	if err != nil {
		slog.Warn("central link down", "site_id", s.config.SiteID, "error", err)
		return
	}
	cursor, cursorErr := s.store.Cursor(ctx)
	if cursorErr != nil {
		slog.Warn("central link up, cursor unreadable", "site_id", s.config.SiteID, "error", cursorErr)
		return
	}
	slog.Info("central link up", "site_id", s.config.SiteID, "cursor", cursor)
}

// SyncOnce pushes the outbox until it is empty, then pulls changes until a page comes back short.
// A failed push does not skip the pull, since a copy kept fresh keeps the lane's answers right.
// It returns the first error either leg met.
func (s *Syncer) SyncOnce(ctx context.Context) error {
	pushErr := s.push(ctx)
	pullErr := s.pull(ctx)
	if pushErr != nil {
		return pushErr
	}
	return pullErr
}

func (s *Syncer) push(ctx context.Context) error {
	for {
		pending, err := s.store.PendingWashes(ctx, maxPushBatch)
		if err != nil {
			return err
		}
		if len(pending) == 0 {
			return nil
		}
		answer, err := s.postWashes(ctx, pending)
		if err != nil {
			return err
		}
		acknowledged := slices.Concat(answer.Stored, answer.Duplicates)
		if err := s.store.ClearOutbox(ctx, acknowledged); err != nil {
			return err
		}
		slog.Info("washes pushed", "site_id", s.config.SiteID, "batch_size", len(pending),
			"stored", len(answer.Stored), "duplicates", len(answer.Duplicates))
		if len(acknowledged) < len(pending) {
			return fmt.Errorf("central acknowledged %d of %d washes", len(acknowledged), len(pending))
		}
		if len(pending) < maxPushBatch {
			return nil
		}
	}
}

func (s *Syncer) postWashes(ctx context.Context, pending []PendingWash) (pushAnswer, error) {
	batch := pushRequest{SiteID: s.config.SiteID, Washes: make([]pushWash, 0, len(pending))}
	for _, wash := range pending {
		batch.Washes = append(batch.Washes, pushWash(wash))
	}
	body, err := json.Marshal(batch)
	if err != nil {
		return pushAnswer{}, fmt.Errorf("encode wash batch: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.config.CentralURL+"/washes", bytes.NewReader(body))
	if err != nil {
		return pushAnswer{}, fmt.Errorf("build wash push: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	var answer pushAnswer
	if err := s.call(request, &answer); err != nil {
		return pushAnswer{}, fmt.Errorf("push washes: %w", err)
	}
	return answer, nil
}

func (s *Syncer) pull(ctx context.Context) error {
	for {
		cursor, err := s.store.Cursor(ctx)
		if err != nil {
			return err
		}
		answer, err := s.getChanges(ctx, cursor)
		if err != nil {
			return err
		}
		changes := make([]EntitlementChange, 0, len(answer.Changes))
		for _, change := range answer.Changes {
			changes = append(changes, EntitlementChange{
				Seq:         change.Seq,
				Plate:       change.Plate,
				Plan:        Plan(valueOf(change.Plan)),
				CompanyID:   valueOf(change.CompanyID),
				CompanyName: valueOf(change.CompanyName),
			})
		}
		if err := s.store.ApplyChanges(ctx, changes, answer.Next, s.config.Now()); err != nil {
			return err
		}
		if len(changes) > 0 {
			slog.Info("entitlements pulled", "site_id", s.config.SiteID, "changes", len(changes), "cursor", answer.Next)
		}
		if len(changes) < maxPullPage {
			return nil
		}
	}
}

func (s *Syncer) getChanges(ctx context.Context, after int64) (pullAnswer, error) {
	query := url.Values{"after": {strconv.FormatInt(after, 10)}, "limit": {strconv.Itoa(maxPullPage)}}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, s.config.CentralURL+"/entitlements?"+query.Encode(), nil)
	if err != nil {
		return pullAnswer{}, fmt.Errorf("build entitlement pull: %w", err)
	}
	var answer pullAnswer
	if err := s.call(request, &answer); err != nil {
		return pullAnswer{}, fmt.Errorf("pull entitlements after %d: %w", after, err)
	}
	return answer, nil
}

// call sends request with the site's token and decodes a 200 answer into answer.
func (s *Syncer) call(request *http.Request, answer any) error {
	request.Header.Set("Authorization", "Bearer "+s.config.Token)
	response, err := s.config.Client.Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	body := io.LimitReader(response.Body, maxAnswerBodySize)
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("central answered %s", response.Status)
	}
	if err := json.NewDecoder(body).Decode(answer); err != nil {
		return fmt.Errorf("decode central's answer: %w", err)
	}
	return nil
}

func valueOf(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
