package central

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var (
	// ErrUnknownPlate reports a plate central holds no vehicle for.
	ErrUnknownPlate = errors.New("unknown plate")
	// ErrNotSubscribed reports a plate that holds no active subscription.
	ErrNotSubscribed = errors.New("no active subscription")
	// ErrNotPremium reports a quota reset for a plate whose active plan has no cap to reset.
	ErrNotPremium = errors.New("active plan is not premium")
	// ErrResetIDTaken reports a quota reset id already stored for another plate.
	ErrResetIDTaken = errors.New("quota reset id belongs to another plate")
)

// OwnerType names who a vehicle is registered to.
type OwnerType string

// The owners a vehicle can have. OwnerNone is a vehicle registered to nobody central knows.
const (
	OwnerNone     OwnerType = ""
	OwnerCustomer OwnerType = "customer"
	OwnerCompany  OwnerType = "company"
)

// Subscription is a plate's active plan as a lookup reports it.
type Subscription struct {
	Plan   Plan
	Status string
}

// PlateWash is one of a plate's washes as a lookup reports it.
type PlateWash struct {
	AdmittedAt time.Time
	SiteID     string
}

// PlateLookup is what central knows about one plate this month. WashesSinceReset counts the washes
// the monthly cap is measured against, which are those since the later of the month start and the latest reset.
type PlateLookup struct {
	Plate            string
	OwnerType        OwnerType
	OwnerName        string
	LeasingCompany   string
	Subscription     *Subscription
	QuotaResetAt     time.Time
	Washes           []PlateWash
	WashesSinceReset int
}

// QuotaReset makes every site count a plate's monthly cap from ResetAt.
type QuotaReset struct {
	ID      string
	Plate   string
	ResetAt time.Time
	Note    string
}

// Site is one wash site and when it last pushed washes to central.
type Site struct {
	ID           string
	Name         string
	LastSyncedAt time.Time
}

// LookupPlate reads plate's owner, active subscription, latest quota reset, and its washes since monthStart, oldest first.
func (s *Store) LookupPlate(ctx context.Context, plate string, monthStart time.Time) (PlateLookup, error) {
	lookup := PlateLookup{Plate: plate}
	var customerEmail, companyName, leasingCompany sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT customers.email, companies.name, vehicles.leasing_company FROM vehicles
		LEFT JOIN customers ON customers.id = vehicles.customer_id
		LEFT JOIN companies ON companies.id = vehicles.company_id
		WHERE vehicles.plate = ?`,
		plate,
	).Scan(&customerEmail, &companyName, &leasingCompany)
	if errors.Is(err, sql.ErrNoRows) {
		return PlateLookup{}, ErrUnknownPlate
	}
	if err != nil {
		return PlateLookup{}, fmt.Errorf("read vehicle: %w", err)
	}
	switch {
	case customerEmail.Valid:
		lookup.OwnerType, lookup.OwnerName = OwnerCustomer, customerEmail.String
	case companyName.Valid:
		lookup.OwnerType, lookup.OwnerName = OwnerCompany, companyName.String
	}
	lookup.LeasingCompany = leasingCompany.String

	subscription, err := activeSubscription(ctx, s.db, plate)
	if err != nil && !errors.Is(err, ErrNotSubscribed) {
		return PlateLookup{}, err
	}
	lookup.Subscription = subscription

	var quotaResetAt sql.NullTime
	if err := s.db.QueryRowContext(ctx,
		"SELECT MAX(reset_at) FROM quota_resets WHERE plate = ?", plate,
	).Scan(&quotaResetAt); err != nil {
		return PlateLookup{}, fmt.Errorf("read latest quota reset: %w", err)
	}
	lookup.QuotaResetAt = quotaResetAt.Time

	lookup.Washes, err = washesSince(ctx, s.db, plate, monthStart)
	if err != nil {
		return PlateLookup{}, err
	}
	for _, wash := range lookup.Washes {
		if !wash.AdmittedAt.Before(lookup.QuotaResetAt) {
			lookup.WashesSinceReset++
		}
	}
	return lookup, nil
}

// ResetQuota stores reset and appends the change that carries it to sites, in one transaction under the
// cursor lock. A reset id already stored for the same plate changes nothing and returns the stored reset
// with isNew false, so a retried request is safe. Only a plate on an active Premium subscription can be reset.
func (s *Store) ResetQuota(ctx context.Context, reset QuotaReset) (stored QuotaReset, isNew bool, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return QuotaReset{}, false, fmt.Errorf("begin quota reset: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := lockEntitlementCursor(ctx, tx); err != nil {
		return QuotaReset{}, false, err
	}

	existing, err := storedQuotaReset(ctx, tx, reset.ID)
	if err == nil {
		if existing.Plate != reset.Plate {
			return QuotaReset{}, false, ErrResetIDTaken
		}
		return existing, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return QuotaReset{}, false, err
	}

	subscription, err := activeSubscription(ctx, tx, reset.Plate)
	if err != nil {
		return QuotaReset{}, false, err
	}
	if subscription.Plan != PlanPremium {
		return QuotaReset{}, false, ErrNotPremium
	}

	reset.ResetAt = reset.ResetAt.UTC().Truncate(time.Microsecond)
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO quota_resets (id, plate, reset_at, note) VALUES (?, ?, ?, ?)",
		reset.ID, reset.Plate, reset.ResetAt, nullableString(reset.Note),
	); err != nil {
		return QuotaReset{}, false, fmt.Errorf("insert quota reset: %w", err)
	}
	if err := appendEntitlementChange(ctx, tx, EntitlementChange{
		Plate:        reset.Plate,
		Plan:         PlanPremium,
		QuotaResetAt: reset.ResetAt,
	}); err != nil {
		return QuotaReset{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return QuotaReset{}, false, fmt.Errorf("commit quota reset: %w", err)
	}
	return reset, true, nil
}

// Sites reads every site central knows, ordered by id.
func (s *Store) Sites(ctx context.Context) ([]Site, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, name, last_synced_at FROM sites ORDER BY id")
	if err != nil {
		return nil, fmt.Errorf("read sites: %w", err)
	}
	defer func() { _ = rows.Close() }()

	sites := []Site{}
	for rows.Next() {
		var site Site
		var lastSyncedAt sql.NullTime
		if err := rows.Scan(&site.ID, &site.Name, &lastSyncedAt); err != nil {
			return nil, fmt.Errorf("scan site: %w", err)
		}
		site.LastSyncedAt = lastSyncedAt.Time
		sites = append(sites, site)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sites: %w", err)
	}
	return sites, nil
}

type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func activeSubscription(ctx context.Context, db queryer, plate string) (*Subscription, error) {
	subscription := Subscription{Status: "active"}
	err := db.QueryRowContext(ctx,
		"SELECT plan FROM subscriptions WHERE plate = ? AND status = 'active' LIMIT 1", plate,
	).Scan(&subscription.Plan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotSubscribed
	}
	if err != nil {
		return nil, fmt.Errorf("read active subscription: %w", err)
	}
	return &subscription, nil
}

func storedQuotaReset(ctx context.Context, db queryer, id string) (QuotaReset, error) {
	reset := QuotaReset{ID: id}
	var note sql.NullString
	err := db.QueryRowContext(ctx,
		"SELECT plate, reset_at, note FROM quota_resets WHERE id = ?", id,
	).Scan(&reset.Plate, &reset.ResetAt, &note)
	if errors.Is(err, sql.ErrNoRows) {
		return QuotaReset{}, err
	}
	if err != nil {
		return QuotaReset{}, fmt.Errorf("read quota reset: %w", err)
	}
	reset.Note = note.String
	return reset, nil
}

func washesSince(ctx context.Context, db queryer, plate string, since time.Time) ([]PlateWash, error) {
	rows, err := db.QueryContext(ctx,
		"SELECT admitted_at, site_id FROM washes WHERE plate = ? AND admitted_at >= ? ORDER BY admitted_at, id",
		plate, since.UTC(),
	)
	if err != nil {
		return nil, fmt.Errorf("read washes: %w", err)
	}
	defer func() { _ = rows.Close() }()

	washes := []PlateWash{}
	for rows.Next() {
		var wash PlateWash
		if err := rows.Scan(&wash.AdmittedAt, &wash.SiteID); err != nil {
			return nil, fmt.Errorf("scan wash: %w", err)
		}
		washes = append(washes, wash)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate washes: %w", err)
	}
	return washes, nil
}
