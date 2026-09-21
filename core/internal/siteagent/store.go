package siteagent

import (
	"context"
	"crypto/rand"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

// timeLayout is fixed width so admitted_at compares correctly as text.
const timeLayout = "2006-01-02T15:04:05.000000000Z"

const upMigrationSuffix = ".up.sql"

//go:embed schema/*.sql
var schemaFiles embed.FS

// Store is the site's local SQLite copy of entitlements and its wash ledger.
type Store struct {
	db *sql.DB
}

// PendingWash is an admitted wash waiting in the outbox for central to acknowledge it.
type PendingWash struct {
	ID            string
	Plate         string
	Plan          Plan
	CompanyID     string
	PrepaidWashID string
	AdmittedAt    time.Time
}

// ChangeKind is what an entry of central's change log changes: the plate's plan, or one prepaid wash.
// The zero value is a plan change, which is how central sent every change before prepaid washes existed.
type ChangeKind string

// The kinds of change central sends.
const (
	ChangeKindPlan           ChangeKind = "plan"
	ChangeKindPrepaidGranted ChangeKind = "prepaid_granted"
	ChangeKindPrepaidSpent   ChangeKind = "prepaid_spent"
)

// EntitlementChange is one entry of central's change log. For a plan change, a zero Plan revokes
// the plate and a non-zero QuotaResetAt makes the monthly cap count from that instant. A prepaid
// change names its prepaid wash and leaves the plan alone.
type EntitlementChange struct {
	Seq           int64
	Plate         string
	Kind          ChangeKind
	Plan          Plan
	CompanyID     string
	CompanyName   string
	QuotaResetAt  time.Time
	PrepaidWashID string
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

type schemaMigration struct {
	version int
	file    string
}

func (s *Store) migrate(ctx context.Context) error {
	var current int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&current); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	migrations, err := upMigrations()
	if err != nil {
		return err
	}
	for _, migration := range migrations {
		if migration.version <= current {
			continue
		}
		if err := s.applyMigration(ctx, migration); err != nil {
			return err
		}
	}
	return nil
}

func upMigrations() ([]schemaMigration, error) {
	files, err := fs.Glob(schemaFiles, "schema/*"+upMigrationSuffix)
	if err != nil {
		return nil, fmt.Errorf("list schema files: %w", err)
	}
	migrations := make([]schemaMigration, 0, len(files))
	for _, file := range files {
		prefix, _, _ := strings.Cut(strings.TrimPrefix(file, "schema/"), "_")
		version, err := strconv.Atoi(prefix)
		if err != nil {
			return nil, fmt.Errorf("schema file %s has no version prefix: %w", file, err)
		}
		migrations = append(migrations, schemaMigration{version: version, file: file})
	}
	slices.SortFunc(migrations, func(a, b schemaMigration) int { return a.version - b.version })
	return migrations, nil
}

func (s *Store) applyMigration(ctx context.Context, migration schemaMigration) error {
	up, err := schemaFiles.ReadFile(migration.file)
	if err != nil {
		return fmt.Errorf("read schema %d: %w", migration.version, err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration %d: %w", migration.version, err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, string(up)); err != nil {
		return fmt.Errorf("apply schema %d: %w", migration.version, err)
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", migration.version)); err != nil {
		return fmt.Errorf("record schema version %d: %w", migration.version, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration %d: %w", migration.version, err)
	}
	return nil
}

// Facts reads a plate's entitlement, its Premium washes since the later of monthStart and its latest
// quota reset, its most recent wash, its oldest unspent prepaid wash, and when the copy last pulled.
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
		`SELECT COUNT(*) FROM washes WHERE plate = ? AND plan = 'premium'
		AND admitted_at >= MAX(?, COALESCE((SELECT reset_at FROM quota_resets WHERE plate = ?), ''))`,
		plate, formatTime(monthStart), plate,
	).Scan(&facts.WashesThisMonth)
	if err != nil {
		return Facts{}, fmt.Errorf("count washes this month: %w", err)
	}

	var prepaidWashID sql.NullString
	err = s.db.QueryRowContext(ctx,
		"SELECT id FROM prepaid_washes WHERE plate = ? AND spent_at IS NULL ORDER BY rowid LIMIT 1", plate,
	).Scan(&prepaidWashID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Facts{}, fmt.Errorf("read prepaid wash: %w", err)
	}
	facts.PrepaidWashID = prepaidWashID.String

	lastWash, err := latestWash(ctx, s.db, plate)
	if err != nil {
		return Facts{}, err
	}
	facts.LastWash = lastWash

	facts.LastPulledAt, err = s.LastPulledAt(ctx)
	if err != nil {
		return Facts{}, err
	}
	return facts, nil
}

// LastPulledAt is when the copy last pulled from central, zero if it never has.
func (s *Store) LastPulledAt(ctx context.Context) (time.Time, error) {
	var lastPulledAt sql.NullString
	if err := s.db.QueryRowContext(ctx, "SELECT last_pulled_at FROM sync_state WHERE id = 1").Scan(&lastPulledAt); err != nil {
		return time.Time{}, fmt.Errorf("read last pull time: %w", err)
	}
	if !lastPulledAt.Valid {
		return time.Time{}, nil
	}
	pulledAt, err := time.Parse(timeLayout, lastPulledAt.String)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse last pull time: %w", err)
	}
	return pulledAt, nil
}

// RecordWash writes an admitted wash and its outbox entry, unless one for the plate already sits
// inside the dedup window, in which case it returns that wash and reports it as a duplicate.
// A non-empty prepaidWashID is spent by the same transaction.
func (s *Store) RecordWash(ctx context.Context, plate string, entitlement Entitlement, prepaidWashID string, admittedAt time.Time, window time.Duration) (Wash, bool, error) {
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
	_, err = tx.ExecContext(ctx,
		"INSERT INTO washes (id, plate, company_id, plan, prepaid_wash_id, admitted_at) VALUES (?, ?, ?, ?, ?, ?)",
		wash.ID, plate, nullableString(entitlement.CompanyID), entitlement.Plan, nullableString(prepaidWashID), formatTime(admittedAt),
	)
	if err != nil {
		return Wash{}, false, fmt.Errorf("insert wash: %w", err)
	}
	if prepaidWashID != "" {
		if err := markPrepaidWashSpent(ctx, tx, prepaidWashID, admittedAt); err != nil {
			return Wash{}, false, err
		}
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO outbox (wash_id) VALUES (?)", wash.ID); err != nil {
		return Wash{}, false, fmt.Errorf("queue wash for central: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Wash{}, false, fmt.Errorf("commit wash: %w", err)
	}
	return wash, false, nil
}

// PendingWashes reads up to limit washes from the outbox, oldest first.
func (s *Store) PendingWashes(ctx context.Context, limit int) ([]PendingWash, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT washes.id, washes.plate, washes.plan, washes.company_id, washes.prepaid_wash_id, washes.admitted_at
		FROM outbox JOIN washes ON washes.id = outbox.wash_id
		ORDER BY washes.admitted_at, washes.id LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("read outbox: %w", err)
	}
	defer func() { _ = rows.Close() }()

	pending := []PendingWash{}
	for rows.Next() {
		var wash PendingWash
		var companyID, prepaidWashID sql.NullString
		var admittedAt string
		if err := rows.Scan(&wash.ID, &wash.Plate, &wash.Plan, &companyID, &prepaidWashID, &admittedAt); err != nil {
			return nil, fmt.Errorf("scan outbox wash: %w", err)
		}
		wash.CompanyID = companyID.String
		wash.PrepaidWashID = prepaidWashID.String
		wash.AdmittedAt, err = time.Parse(timeLayout, admittedAt)
		if err != nil {
			return nil, fmt.Errorf("parse wash time: %w", err)
		}
		pending = append(pending, wash)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate outbox: %w", err)
	}
	return pending, nil
}

// ClearOutbox removes the named washes from the outbox. An id no longer there is skipped.
func (s *Store) ClearOutbox(ctx context.Context, ids []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin outbox clear: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, "DELETE FROM outbox WHERE wash_id = ?", id); err != nil {
			return fmt.Errorf("clear outbox wash: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit outbox clear: %w", err)
	}
	return nil
}

// OutboxDepth counts the washes central has not acknowledged yet.
func (s *Store) OutboxDepth(ctx context.Context) (int, error) {
	var depth int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM outbox").Scan(&depth); err != nil {
		return 0, fmt.Errorf("count outbox: %w", err)
	}
	return depth, nil
}

// Cursor is the last entitlement change the copy applied.
func (s *Store) Cursor(ctx context.Context) (int64, error) {
	var cursor int64
	if err := s.db.QueryRowContext(ctx, "SELECT cursor FROM sync_state WHERE id = 1").Scan(&cursor); err != nil {
		return 0, fmt.Errorf("read cursor: %w", err)
	}
	return cursor, nil
}

// ApplyChanges applies one page of central's change log and moves the cursor to next and the
// pull time to pulledAt, all in one transaction, so a refused change leaves the copy as it was.
func (s *Store) ApplyChanges(ctx context.Context, changes []EntitlementChange, next int64, pulledAt time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin entitlement changes: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, change := range changes {
		if err := applyChange(ctx, tx, change, pulledAt); err != nil {
			return fmt.Errorf("apply entitlement change %d: %w", change.Seq, err)
		}
	}
	if _, err := tx.ExecContext(ctx,
		"UPDATE sync_state SET cursor = ?, last_pulled_at = ? WHERE id = 1", next, formatTime(pulledAt),
	); err != nil {
		return fmt.Errorf("move cursor: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit entitlement changes: %w", err)
	}
	return nil
}

func applyChange(ctx context.Context, tx *sql.Tx, change EntitlementChange, pulledAt time.Time) error {
	switch change.Kind {
	case "", ChangeKindPlan:
		return applyPlanChange(ctx, tx, change)
	case ChangeKindPrepaidGranted:
		// A grant pulled again, after a spend here or anywhere, finds the row and changes nothing.
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO prepaid_washes (id, plate) VALUES (?, ?) ON CONFLICT (id) DO NOTHING",
			change.PrepaidWashID, change.Plate,
		); err != nil {
			return fmt.Errorf("write prepaid wash: %w", err)
		}
		return nil
	case ChangeKindPrepaidSpent:
		return markPrepaidWashSpent(ctx, tx, change.PrepaidWashID, pulledAt)
	default:
		return fmt.Errorf("unknown change kind %q", change.Kind)
	}
}

// markPrepaidWashSpent only ever fills a spend, so neither a replayed pull nor a local spend racing one undoes another.
func markPrepaidWashSpent(ctx context.Context, tx *sql.Tx, prepaidWashID string, spentAt time.Time) error {
	if _, err := tx.ExecContext(ctx,
		"UPDATE prepaid_washes SET spent_at = ? WHERE id = ? AND spent_at IS NULL",
		formatTime(spentAt), prepaidWashID,
	); err != nil {
		return fmt.Errorf("spend prepaid wash: %w", err)
	}
	return nil
}

func applyPlanChange(ctx context.Context, tx *sql.Tx, change EntitlementChange) error {
	if !change.QuotaResetAt.IsZero() {
		// A reset only ever moves forward, so a change replayed out of order cannot undo a later one.
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO quota_resets (plate, reset_at) VALUES (?, ?)
			ON CONFLICT (plate) DO UPDATE SET reset_at = MAX(reset_at, excluded.reset_at)`,
			change.Plate, formatTime(change.QuotaResetAt),
		); err != nil {
			return fmt.Errorf("write quota reset: %w", err)
		}
	}
	if change.Plan == "" {
		if _, err := tx.ExecContext(ctx, "DELETE FROM vehicles WHERE plate = ?", change.Plate); err != nil {
			return fmt.Errorf("revoke vehicle: %w", err)
		}
		return nil
	}
	if change.CompanyID != "" {
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO companies (id, name) VALUES (?, ?) ON CONFLICT (id) DO UPDATE SET name = excluded.name",
			change.CompanyID, change.CompanyName,
		); err != nil {
			return fmt.Errorf("write company: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO vehicles (plate, plan, company_id) VALUES (?, ?, ?)
		ON CONFLICT (plate) DO UPDATE SET plan = excluded.plan, company_id = excluded.company_id`,
		change.Plate, change.Plan, nullableString(change.CompanyID),
	); err != nil {
		return fmt.Errorf("write vehicle: %w", err)
	}
	return nil
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

func nullableString(value string) sql.NullString {
	return sql.NullString{String: value, Valid: value != ""}
}

func formatTime(t time.Time) string {
	return t.UTC().Format(timeLayout)
}
