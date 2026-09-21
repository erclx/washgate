package central

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrPlateTaken reports a plate registered to another customer or to a company.
var ErrPlateTaken = errors.New("plate registered to another owner")

// Customer is one customer with the plan each of their cars holds.
type Customer struct {
	ID   string
	Name string
	Cars []CarPlan
}

// CarPlan is a customer's car and its active plan, PlanNone when it has none.
type CarPlan struct {
	Plate string
	Plan  Plan
}

// CustomerCar is one of a customer's cars this month. WashesUsed counts what the monthly cap is measured
// against, the Premium washes since the later of the month start and the latest quota reset, the rule
// LookupPlate applies. Washes is this month's washes, oldest first, and is read only for a single car.
type CustomerCar struct {
	Plate              string
	Plan               Plan
	WashesUsed         int
	PrepaidWashesReady int
	Washes             []PlateWash
}

// customerCarsQuery reads a customer's cars with their plan, washes used, and unspent prepaid washes in one
// query. Its arguments are the month start twice, the customer id, and a plate to narrow to, or NULL for all.
const customerCarsQuery = `SELECT vehicles.plate, COALESCE(subscriptions.plan, ''),
	(SELECT COUNT(*) FROM washes
		WHERE washes.plate = vehicles.plate AND washes.plan = 'premium'
		AND washes.admitted_at >= GREATEST(?, COALESCE((SELECT MAX(reset_at) FROM quota_resets WHERE quota_resets.plate = vehicles.plate), ?))),
	(SELECT COUNT(*) FROM prepaid_washes WHERE prepaid_washes.plate = vehicles.plate AND prepaid_washes.spent_wash_id IS NULL)
	FROM vehicles
	LEFT JOIN subscriptions ON subscriptions.plate = vehicles.plate AND subscriptions.status = 'active'
	WHERE vehicles.customer_id = ? AND (? IS NULL OR vehicles.plate = ?)
	ORDER BY vehicles.plate`

// Customers reads every customer, ordered by name, with each of their cars and its active plan.
func (s *Store) Customers(ctx context.Context) ([]Customer, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT customers.id, customers.name, vehicles.plate, subscriptions.plan FROM customers
		LEFT JOIN vehicles ON vehicles.customer_id = customers.id
		LEFT JOIN subscriptions ON subscriptions.plate = vehicles.plate AND subscriptions.status = 'active'
		ORDER BY customers.name, customers.id, vehicles.plate`)
	if err != nil {
		return nil, fmt.Errorf("read customers: %w", err)
	}
	defer func() { _ = rows.Close() }()

	customers := []Customer{}
	for rows.Next() {
		var id, name string
		var plate, plan sql.NullString
		if err := rows.Scan(&id, &name, &plate, &plan); err != nil {
			return nil, fmt.Errorf("scan customer: %w", err)
		}
		if len(customers) == 0 || customers[len(customers)-1].ID != id {
			customers = append(customers, Customer{ID: id, Name: name, Cars: []CarPlan{}})
		}
		if plate.Valid {
			current := &customers[len(customers)-1]
			current.Cars = append(current.Cars, CarPlan{Plate: plate.String, Plan: Plan(plan.String)})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate customers: %w", err)
	}
	return customers, nil
}

// CustomerCars reads each of the customer's cars this month, ordered by plate, without their washes.
func (s *Store) CustomerCars(ctx context.Context, customerID string, monthStart time.Time) ([]CustomerCar, error) {
	exists, err := s.CustomerExists(ctx, customerID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrUnknownCustomer
	}
	return s.customerCars(ctx, customerID, sql.NullString{}, monthStart)
}

// CustomerCar reads one of the customer's cars with this month's washes. A plate the customer does not
// hold answers ErrUnknownPlate, whoever else holds it.
func (s *Store) CustomerCar(ctx context.Context, customerID, plate string, monthStart time.Time) (CustomerCar, error) {
	cars, err := s.customerCars(ctx, customerID, sql.NullString{String: plate, Valid: true}, monthStart)
	if err != nil {
		return CustomerCar{}, err
	}
	if len(cars) == 0 {
		return CustomerCar{}, ErrUnknownPlate
	}
	car := cars[0]
	car.Washes, err = washesSince(ctx, s.db, plate, monthStart)
	if err != nil {
		return CustomerCar{}, err
	}
	return car, nil
}

func (s *Store) customerCars(ctx context.Context, customerID string, plate sql.NullString, monthStart time.Time) ([]CustomerCar, error) {
	rows, err := s.db.QueryContext(ctx, customerCarsQuery, monthStart.UTC(), monthStart.UTC(), customerID, plate, plate)
	if err != nil {
		return nil, fmt.Errorf("read customer cars: %w", err)
	}
	defer func() { _ = rows.Close() }()

	cars := []CustomerCar{}
	for rows.Next() {
		var car CustomerCar
		if err := rows.Scan(&car.Plate, &car.Plan, &car.WashesUsed, &car.PrepaidWashesReady); err != nil {
			return nil, fmt.Errorf("scan customer car: %w", err)
		}
		cars = append(cars, car)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate customer cars: %w", err)
	}
	return cars, nil
}

// RegisterPlate registers plate to the customer and reports whether it was new to them. A plate nobody holds
// is taken over, and one held by another customer or a company answers ErrPlateTaken. It appends no
// entitlement change, since a plate with no plan changes nothing a site holds.
func (s *Store) RegisterPlate(ctx context.Context, customerID, plate string) (bool, error) {
	written, err := s.db.ExecContext(ctx,
		`INSERT INTO vehicles (plate, customer_id, company_id) VALUES (?, ?, NULL)
		ON DUPLICATE KEY UPDATE customer_id = IF(customer_id IS NULL AND company_id IS NULL, VALUES(customer_id), customer_id)`,
		plate, customerID,
	)
	if isForeignKeyMissing(err) {
		return false, ErrUnknownCustomer
	}
	if err != nil {
		return false, fmt.Errorf("write vehicle: %w", err)
	}
	rows, err := written.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("count written vehicle: %w", err)
	}
	if rows > 0 {
		return true, nil
	}

	// Nothing was written, so the foreign key never checked the customer. Read both before refusing.
	var isHolder, isKnownCustomer bool
	if err := s.db.QueryRowContext(ctx,
		"SELECT customer_id <=> ?, EXISTS (SELECT 1 FROM customers WHERE id = ?) FROM vehicles WHERE plate = ?",
		customerID, customerID, plate,
	).Scan(&isHolder, &isKnownCustomer); err != nil {
		return false, fmt.Errorf("read plate holder: %w", err)
	}
	switch {
	case !isKnownCustomer:
		return false, ErrUnknownCustomer
	case !isHolder:
		return false, ErrPlateTaken
	}
	return false, nil
}
