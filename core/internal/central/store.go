package central

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"regexp"
	"time"

	"github.com/go-sql-driver/mysql"
)

const (
	pingTimeout = 5 * time.Second
	dialTimeout = 5 * time.Second
	// mysqlForeignKeyMissing is the server's error number for a row whose foreign key matches no parent.
	mysqlForeignKeyMissing = 1452
)

// normalizedPlate is the shape a site's lane looks plates up in once it has normalized a read.
var normalizedPlate = regexp.MustCompile(`^[A-Z0-9]{2,7}$`)

var (
	// ErrUnknownSite reports a batch from a site central does not know.
	ErrUnknownSite = errors.New("unknown site")
	// ErrUnknownToken reports a bearer token no site holds.
	ErrUnknownToken = errors.New("unknown site token")
	// ErrUnknownCompany reports a wash or an entitlement naming a company central does not know.
	ErrUnknownCompany = errors.New("unknown company")
	// ErrInvalidEntitlement reports an entitlement whose plan and owner do not pair, which no site could apply.
	ErrInvalidEntitlement = errors.New("invalid entitlement")
	// ErrUnknownCustomer reports a subscription naming a customer central does not know.
	ErrUnknownCustomer = errors.New("unknown customer")
	// ErrNoPremiumPrice reports that no Premium price is valid yet.
	ErrNoPremiumPrice = errors.New("no premium price")
)

// SubscriptionAction is what a payment event does to a subscription.
type SubscriptionAction int

// The actions a payment event can carry.
const (
	SubscriptionUnchanged SubscriptionAction = iota
	SubscriptionPaid
	SubscriptionEnded
)

// EventOutcome is what applying a payment event did.
type EventOutcome string

// The outcomes of applying a payment event.
const (
	EventApplied   EventOutcome = "applied"
	EventDuplicate EventOutcome = "duplicate"
	EventIgnored   EventOutcome = "ignored"
)

// SubscriptionEvent is a payment provider's event as central applies it. A paid event starts or extends
// the subscription to PeriodEnd, and an ended one cancels it.
type SubscriptionEvent struct {
	EventID              string
	EventType            string
	Action               SubscriptionAction
	StripeSubscriptionID string
	CustomerID           string
	Plate                string
	PeriodEnd            time.Time
}

// Plan is what entitles a plate to wash. PlanNone is a revoked or absent entitlement.
type Plan string

// The plans a plate can hold.
const (
	PlanNone    Plan = ""
	PlanPremium Plan = "premium"
	PlanFleet   Plan = "fleet"
)

// Config names the MariaDB database central connects to.
type Config struct {
	Host     string
	Port     string
	Name     string
	User     string
	Password string
}

// Wash is one admitted wash as a site reports it.
type Wash struct {
	ID         string
	Plate      string
	Plan       Plan
	CompanyID  string
	AdmittedAt time.Time
}

// RecordResult splits a batch's wash ids into those stored now and those already stored.
type RecordResult struct {
	Stored     []string
	Duplicates []string
}

// Entitlement is the plan a plate holds from now on. A fleet plan names its company, and PlanNone revokes.
type Entitlement struct {
	Plate      string
	Plan       Plan
	CustomerID string
	CompanyID  string
}

// EntitlementChange is one entry in the log sites pull their copy from.
type EntitlementChange struct {
	Seq         int64
	Plate       string
	Plan        Plan
	CompanyID   string
	CompanyName string
}

// Store is central's MariaDB ledger of sites, entitlements, and washes.
type Store struct {
	db *sql.DB
}

// Open connects to the database config names and confirms it answers.
func Open(ctx context.Context, config Config) (*Store, error) {
	driverConfig := mysql.NewConfig()
	driverConfig.Net = "tcp"
	driverConfig.Addr = net.JoinHostPort(config.Host, config.Port)
	driverConfig.DBName = config.Name
	driverConfig.User = config.User
	driverConfig.Passwd = config.Password
	driverConfig.Timeout = dialTimeout
	driverConfig.ParseTime = true
	driverConfig.Loc = time.UTC
	driverConfig.Params = map[string]string{"time_zone": "'+00:00'"}
	connector, err := mysql.NewConnector(driverConfig)
	if err != nil {
		return nil, fmt.Errorf("configure database connection: %w", err)
	}
	db := sql.OpenDB(connector)
	db.SetConnMaxLifetime(3 * time.Minute)

	pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("reach database %s at %s: %w", config.Name, driverConfig.Addr, err)
	}
	return &Store{db: db}, nil
}

// Close releases the connection pool.
func (s *Store) Close() error {
	return s.db.Close()
}

// ProvisionSites registers each site and the hash of the token it now answers to, in one transaction.
// A site central already holds keeps its name and sync time and takes the new hash, and a site left out
// of tokens keeps its row but can no longer authenticate.
func (s *Store) ProvisionSites(ctx context.Context, tokens []SiteToken) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin site provisioning: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// The list is the whole set of sites that may authenticate. Clearing every hash first revokes a site left out,
	// and keeps an upsert from landing on a stale row that still holds the same hash through the unique index.
	if _, err := tx.ExecContext(ctx, "UPDATE sites SET token_sha256 = NULL"); err != nil {
		return fmt.Errorf("revoke site tokens: %w", err)
	}
	for _, site := range tokens {
		hash := hashSiteToken(site.Token)
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO sites (id, name, token_sha256) VALUES (?, ?, ?)
			ON DUPLICATE KEY UPDATE token_sha256 = VALUES(token_sha256)`,
			site.SiteID, site.SiteID, hash[:],
		); err != nil {
			return fmt.Errorf("provision site %s: %w", site.SiteID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit site provisioning: %w", err)
	}
	return nil
}

// SiteForToken finds the site whose token hashes to hash.
func (s *Store) SiteForToken(ctx context.Context, hash [sha256.Size]byte) (string, error) {
	var siteID string
	err := s.db.QueryRowContext(ctx, "SELECT id FROM sites WHERE token_sha256 = ?", hash[:]).Scan(&siteID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrUnknownToken
	}
	if err != nil {
		return "", fmt.Errorf("read site for token: %w", err)
	}
	return siteID, nil
}

// RecordWashes stores each wash a site sent that central does not hold yet, in one transaction,
// and marks the site as synced. A wash id already stored is reported as a duplicate and left unchanged.
func (s *Store) RecordWashes(ctx context.Context, siteID string, washes []Wash) (RecordResult, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return RecordResult{}, fmt.Errorf("begin wash batch: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var knownSite string
	err = tx.QueryRowContext(ctx, "SELECT id FROM sites WHERE id = ?", siteID).Scan(&knownSite)
	if errors.Is(err, sql.ErrNoRows) {
		return RecordResult{}, ErrUnknownSite
	}
	if err != nil {
		return RecordResult{}, fmt.Errorf("read site: %w", err)
	}

	result := RecordResult{Stored: []string{}, Duplicates: []string{}}
	for _, wash := range washes {
		// ON DUPLICATE KEY forgives only the repeated id. INSERT IGNORE would also turn a missing company into a warning.
		inserted, err := tx.ExecContext(ctx,
			`INSERT INTO washes (id, site_id, plate, plan, company_id, admitted_at, received_at)
			VALUES (?, ?, ?, ?, ?, ?, UTC_TIMESTAMP(6))
			ON DUPLICATE KEY UPDATE id = id`,
			wash.ID, siteID, wash.Plate, string(wash.Plan), nullableString(wash.CompanyID), wash.AdmittedAt.UTC(),
		)
		if isForeignKeyMissing(err) {
			return RecordResult{}, ErrUnknownCompany
		}
		if err != nil {
			return RecordResult{}, fmt.Errorf("insert wash: %w", err)
		}
		rows, err := inserted.RowsAffected()
		if err != nil {
			return RecordResult{}, fmt.Errorf("count inserted wash: %w", err)
		}
		if rows == 0 {
			result.Duplicates = append(result.Duplicates, wash.ID)
		} else {
			result.Stored = append(result.Stored, wash.ID)
		}
	}

	if _, err := tx.ExecContext(ctx, "UPDATE sites SET last_synced_at = UTC_TIMESTAMP(6) WHERE id = ?", siteID); err != nil {
		return RecordResult{}, fmt.Errorf("mark site synced: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return RecordResult{}, fmt.Errorf("commit wash batch: %w", err)
	}
	return result, nil
}

// PutEntitlement makes entitlement the plate's only active subscription and appends the change
// sites pull, in one transaction. Writers take the cursor row in turn, so changes commit in seq order.
func (s *Store) PutEntitlement(ctx context.Context, entitlement Entitlement) error {
	if !isApplicable(entitlement) {
		return ErrInvalidEntitlement
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin entitlement: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := lockEntitlementCursor(ctx, tx); err != nil {
		return err
	}

	var companyName sql.NullString
	if entitlement.CompanyID != "" {
		err := tx.QueryRowContext(ctx, "SELECT name FROM companies WHERE id = ?", entitlement.CompanyID).Scan(&companyName)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrUnknownCompany
		}
		if err != nil {
			return fmt.Errorf("read company: %w", err)
		}
	}

	if _, err := tx.ExecContext(ctx,
		"UPDATE subscriptions SET status = 'canceled' WHERE plate = ? AND status = 'active'", entitlement.Plate,
	); err != nil {
		return fmt.Errorf("cancel active subscriptions: %w", err)
	}

	if entitlement.Plan != PlanNone {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO vehicles (plate, customer_id, company_id) VALUES (?, ?, ?)
			ON DUPLICATE KEY UPDATE customer_id = VALUES(customer_id), company_id = VALUES(company_id)`,
			entitlement.Plate, nullableString(entitlement.CustomerID), nullableString(entitlement.CompanyID),
		); err != nil {
			return fmt.Errorf("write vehicle: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO subscriptions (id, plate, plan, customer_id, company_id, status)
			VALUES (?, ?, ?, ?, ?, 'active')`,
			rand.Text(), entitlement.Plate, string(entitlement.Plan),
			nullableString(entitlement.CustomerID), nullableString(entitlement.CompanyID),
		); err != nil {
			return fmt.Errorf("write subscription: %w", err)
		}
	}

	if err := appendEntitlementChange(ctx, tx, EntitlementChange{
		Plate:       entitlement.Plate,
		Plan:        entitlement.Plan,
		CompanyID:   entitlement.CompanyID,
		CompanyName: companyName.String,
	}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit entitlement: %w", err)
	}
	return nil
}

// ApplySubscriptionEvent stores the event id and its effect on the subscription in one transaction.
// An id already stored changes nothing and reports a duplicate. A paid event starts the Premium subscription
// or extends its period, never shortening it, and an ended one cancels it. Each change appends to the log sites pull.
func (s *Store) ApplySubscriptionEvent(ctx context.Context, event SubscriptionEvent) (EventOutcome, error) {
	if event.Action == SubscriptionPaid && !normalizedPlate.MatchString(event.Plate) {
		return "", ErrInvalidEntitlement
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("begin subscription event: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// ON DUPLICATE KEY forgives only the repeated id, where INSERT IGNORE would hide any other failure as a warning.
	inserted, err := tx.ExecContext(ctx,
		`INSERT INTO stripe_events (event_id, type, received_at) VALUES (?, ?, UTC_TIMESTAMP(6))
		ON DUPLICATE KEY UPDATE event_id = event_id`,
		event.EventID, event.EventType,
	)
	if err != nil {
		return "", fmt.Errorf("store event id: %w", err)
	}
	rows, err := inserted.RowsAffected()
	if err != nil {
		return "", fmt.Errorf("count stored event id: %w", err)
	}
	if rows == 0 {
		return EventDuplicate, nil
	}

	outcome := EventIgnored
	switch event.Action {
	case SubscriptionPaid:
		outcome, err = applyPaidSubscription(ctx, tx, event)
	case SubscriptionEnded:
		outcome, err = applyEndedSubscription(ctx, tx, event.StripeSubscriptionID)
	}
	if err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit subscription event: %w", err)
	}
	return outcome, nil
}

func applyPaidSubscription(ctx context.Context, tx *sql.Tx, event SubscriptionEvent) (EventOutcome, error) {
	if err := lockEntitlementCursor(ctx, tx); err != nil {
		return "", err
	}
	var plate, status string
	err := tx.QueryRowContext(ctx,
		"SELECT plate, status FROM subscriptions WHERE stripe_subscription_id = ? FOR UPDATE", event.StripeSubscriptionID,
	).Scan(&plate, &status)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return startPaidSubscription(ctx, tx, event)
	case err != nil:
		return "", fmt.Errorf("read subscription: %w", err)
	case status != "active":
		// Stripe never revives a deleted subscription, so a paid invoice arriving after the deletion changes nothing.
		return EventIgnored, nil
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE subscriptions SET current_period_end = GREATEST(COALESCE(current_period_end, ?), ?)
		WHERE stripe_subscription_id = ?`,
		event.PeriodEnd.UTC(), event.PeriodEnd.UTC(), event.StripeSubscriptionID,
	); err != nil {
		return "", fmt.Errorf("extend subscription: %w", err)
	}
	if err := appendEntitlementChange(ctx, tx, EntitlementChange{Plate: plate, Plan: PlanPremium}); err != nil {
		return "", err
	}
	return EventApplied, nil
}

func startPaidSubscription(ctx context.Context, tx *sql.Tx, event SubscriptionEvent) (EventOutcome, error) {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO vehicles (plate, customer_id, company_id) VALUES (?, ?, NULL)
		ON DUPLICATE KEY UPDATE customer_id = VALUES(customer_id), company_id = NULL`,
		event.Plate, event.CustomerID,
	)
	if isForeignKeyMissing(err) {
		return "", ErrUnknownCustomer
	}
	if err != nil {
		return "", fmt.Errorf("write vehicle: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		"UPDATE subscriptions SET status = 'canceled' WHERE plate = ? AND status = 'active'", event.Plate,
	); err != nil {
		return "", fmt.Errorf("cancel active subscriptions: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO subscriptions (id, plate, plan, customer_id, company_id, status, current_period_end, stripe_subscription_id)
		VALUES (?, ?, 'premium', ?, NULL, 'active', ?, ?)`,
		rand.Text(), event.Plate, event.CustomerID, event.PeriodEnd.UTC(), event.StripeSubscriptionID,
	); err != nil {
		return "", fmt.Errorf("write subscription: %w", err)
	}
	if err := appendEntitlementChange(ctx, tx, EntitlementChange{Plate: event.Plate, Plan: PlanPremium}); err != nil {
		return "", err
	}
	return EventApplied, nil
}

func applyEndedSubscription(ctx context.Context, tx *sql.Tx, stripeSubscriptionID string) (EventOutcome, error) {
	if err := lockEntitlementCursor(ctx, tx); err != nil {
		return "", err
	}
	var plate string
	err := tx.QueryRowContext(ctx,
		"SELECT plate FROM subscriptions WHERE stripe_subscription_id = ? AND status = 'active' FOR UPDATE", stripeSubscriptionID,
	).Scan(&plate)
	if errors.Is(err, sql.ErrNoRows) {
		return EventIgnored, nil
	}
	if err != nil {
		return "", fmt.Errorf("read subscription: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		"UPDATE subscriptions SET status = 'canceled' WHERE stripe_subscription_id = ?", stripeSubscriptionID,
	); err != nil {
		return "", fmt.Errorf("cancel subscription: %w", err)
	}
	if err := appendEntitlementChange(ctx, tx, EntitlementChange{Plate: plate, Plan: PlanNone}); err != nil {
		return "", err
	}
	return EventApplied, nil
}

// PremiumPrice reads the Premium price in öre that is valid at the given time.
func (s *Store) PremiumPrice(ctx context.Context, at time.Time) (int64, error) {
	var amountOre int64
	err := s.db.QueryRowContext(ctx,
		`SELECT amount_ore FROM prices WHERE code = 'premium' AND valid_from <= ?
		ORDER BY valid_from DESC LIMIT 1`,
		at.UTC(),
	).Scan(&amountOre)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNoPremiumPrice
	}
	if err != nil {
		return 0, fmt.Errorf("read premium price: %w", err)
	}
	return amountOre, nil
}

// IsPlateHeldByAnother reports whether plate is registered to anyone but customerID, whether another
// customer or a fleet company. Every subscription write registers the vehicle to its owner, so this covers them too.
func (s *Store) IsPlateHeldByAnother(ctx context.Context, plate, customerID string) (bool, error) {
	var held bool
	if err := s.db.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM vehicles
		WHERE plate = ? AND (company_id IS NOT NULL OR (customer_id IS NOT NULL AND customer_id <> ?)))`,
		plate, customerID,
	).Scan(&held); err != nil {
		return false, fmt.Errorf("read plate holder: %w", err)
	}
	return held, nil
}

// CustomerExists reports whether central holds a customer with id.
func (s *Store) CustomerExists(ctx context.Context, id string) (bool, error) {
	var exists bool
	if err := s.db.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM customers WHERE id = ?)", id).Scan(&exists); err != nil {
		return false, fmt.Errorf("read customer: %w", err)
	}
	return exists, nil
}

// lockEntitlementCursor takes the cursor row every entitlement writer holds until it commits, so changes
// commit in seq order. Writers take it before any subscription row, which keeps two writers from deadlocking.
func lockEntitlementCursor(ctx context.Context, tx *sql.Tx) error {
	var cursor int
	if err := tx.QueryRowContext(ctx, "SELECT id FROM entitlement_cursor WHERE id = 1 FOR UPDATE").Scan(&cursor); err != nil {
		return fmt.Errorf("lock entitlement cursor: %w", err)
	}
	return nil
}

// appendEntitlementChange adds change to the log sites pull. The caller holds the cursor lock.
func appendEntitlementChange(ctx context.Context, tx *sql.Tx, change EntitlementChange) error {
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO entitlement_changes (plate, plan, company_id, company_name, changed_at)
		VALUES (?, ?, ?, ?, UTC_TIMESTAMP(6))`,
		change.Plate, nullableString(string(change.Plan)), nullableString(change.CompanyID), nullableString(change.CompanyName),
	); err != nil {
		return fmt.Errorf("append entitlement change: %w", err)
	}
	return nil
}

// EntitlementChanges reads up to limit changes after the cursor, oldest first.
func (s *Store) EntitlementChanges(ctx context.Context, after int64, limit int) ([]EntitlementChange, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT seq, plate, plan, company_id, company_name FROM entitlement_changes
		WHERE seq > ? ORDER BY seq LIMIT ?`,
		after, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("read entitlement changes: %w", err)
	}
	defer func() { _ = rows.Close() }()

	changes := []EntitlementChange{}
	for rows.Next() {
		var change EntitlementChange
		var plan, companyID, companyName sql.NullString
		if err := rows.Scan(&change.Seq, &change.Plate, &plan, &companyID, &companyName); err != nil {
			return nil, fmt.Errorf("scan entitlement change: %w", err)
		}
		change.Plan = Plan(plan.String)
		change.CompanyID = companyID.String
		change.CompanyName = companyName.String
		changes = append(changes, change)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate entitlement changes: %w", err)
	}
	return changes, nil
}

// isApplicable mirrors what a site can apply: a plate in the normalized form its lane looks up,
// and the pairing its vehicles table enforces, where a fleet plan names a company and nothing else does.
func isApplicable(entitlement Entitlement) bool {
	if !normalizedPlate.MatchString(entitlement.Plate) {
		return false
	}
	hasCompany := entitlement.CompanyID != ""
	switch entitlement.Plan {
	case PlanFleet:
		return hasCompany && entitlement.CustomerID == ""
	case PlanPremium, PlanNone:
		return !hasCompany
	default:
		return false
	}
}

func isForeignKeyMissing(err error) bool {
	var mysqlErr *mysql.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == mysqlForeignKeyMissing
}

func nullableString(value string) sql.NullString {
	return sql.NullString{String: value, Valid: value != ""}
}
