package siteagent

import (
	"context"
	"crypto/rand"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

// timeLayout is fixed width so admitted_at compares correctly as text.
const timeLayout = "2006-01-02T15:04:05.000000000Z"

const schemaVersion = 1

//go:embed schema/*.sql
var schemaFiles embed.FS

//go:embed fixtures/entitlements.sql
var entitlementFixtures string

// Store is the site's local SQLite copy of entitlements and its wash ledger.
type Store struct {
	db *sql.DB
}

// Open opens the SQLite file at path and brings its schema up to date.
func Open(ctx context.Context, path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("open site database: %w", err)
	}
	// One connection serializes every write, so two reads of one plate cannot both insert a wash.
	db.SetMaxOpenConns(1)
	store := &Store{db: db}
	if err := store.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

// Close releases the database file.
func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate(ctx context.Context) error {
	var version int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if version >= schemaVersion {
		return nil
	}
	up, err := schemaFiles.ReadFile("schema/001_init.up.sql")
	if err != nil {
		return fmt.Errorf("read schema: %w", err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, string(up)); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", schemaVersion)); err != nil {
		return fmt.Errorf("record schema version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration: %w", err)
	}
	return nil
}

// SeedFixtures loads the embedded entitlements into a copy holding no vehicle or company, reporting whether it did.
func (s *Store) SeedFixtures(ctx context.Context) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin seed: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var entitlementRows int
	err = tx.QueryRowContext(ctx,
		"SELECT (SELECT COUNT(*) FROM vehicles) + (SELECT COUNT(*) FROM companies)",
	).Scan(&entitlementRows)
	if err != nil {
		return false, fmt.Errorf("count entitlements: %w", err)
	}
	if entitlementRows > 0 {
		return false, nil
	}
	if _, err := tx.ExecContext(ctx, entitlementFixtures); err != nil {
		return false, fmt.Errorf("load fixtures: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit seed: %w", err)
	}
	return true, nil
}

// Facts reads a plate's entitlement, its washes since monthStart, and its most recent wash.
func (s *Store) Facts(ctx context.Context, plate string, monthStart time.Time) (Facts, error) {
	var facts Facts
	var companyID sql.NullString
	err := s.db.QueryRowContext(ctx,
		"SELECT plan, company_id FROM vehicles WHERE plate = ?", plate,
	).Scan(&facts.Entitlement.Plan, &companyID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Facts{}, fmt.Errorf("read entitlement: %w", err)
	}
	facts.Entitlement.CompanyID = companyID.String

	err = s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM washes WHERE plate = ? AND admitted_at >= ?", plate, formatTime(monthStart),
	).Scan(&facts.WashesThisMonth)
	if err != nil {
		return Facts{}, fmt.Errorf("count washes this month: %w", err)
	}

	lastWash, err := latestWash(ctx, s.db, plate)
	if err != nil {
		return Facts{}, err
	}
	facts.LastWash = lastWash
	return facts, nil
}

// RecordWash writes an admitted wash, unless one for the plate already sits inside the
// dedup window, in which case it returns that wash and reports it as a duplicate.
func (s *Store) RecordWash(ctx context.Context, plate string, entitlement Entitlement, admittedAt time.Time, window time.Duration) (Wash, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Wash{}, false, fmt.Errorf("begin wash: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	previous, err := latestWash(ctx, tx, plate)
	if err != nil {
		return Wash{}, false, err
	}
	if isInsideWindow(previous, window, admittedAt) {
		return previous, true, nil
	}

	wash := Wash{ID: rand.Text(), AdmittedAt: admittedAt}
	companyID := sql.NullString{String: entitlement.CompanyID, Valid: entitlement.CompanyID != ""}
	_, err = tx.ExecContext(ctx,
		"INSERT INTO washes (id, plate, company_id, plan, admitted_at) VALUES (?, ?, ?, ?, ?)",
		wash.ID, plate, companyID, entitlement.Plan, formatTime(admittedAt),
	)
	if err != nil {
		return Wash{}, false, fmt.Errorf("insert wash: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Wash{}, false, fmt.Errorf("commit wash: %w", err)
	}
	return wash, false, nil
}

type rowQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func latestWash(ctx context.Context, db rowQuerier, plate string) (Wash, error) {
	var wash Wash
	var admittedAt string
	err := db.QueryRowContext(ctx,
		"SELECT id, admitted_at FROM washes WHERE plate = ? ORDER BY admitted_at DESC LIMIT 1", plate,
	).Scan(&wash.ID, &admittedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Wash{}, nil
	}
	if err != nil {
		return Wash{}, fmt.Errorf("read latest wash: %w", err)
	}
	wash.AdmittedAt, err = time.Parse(timeLayout, admittedAt)
	if err != nil {
		return Wash{}, fmt.Errorf("parse wash time: %w", err)
	}
	return wash, nil
}

func formatTime(t time.Time) string {
	return t.UTC().Format(timeLayout)
}
