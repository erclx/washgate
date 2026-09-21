package siteagent

import "time"

// PremiumMonthlyCap is how many washes a Premium subscription includes each calendar month.
const PremiumMonthlyCap = 8

// Plan names what a registered vehicle is entitled to.
type Plan string

// Plans a vehicle can hold. The zero value means the plate holds none.
const (
	PlanPremium Plan = "premium"
	PlanFleet   Plan = "fleet"
	// PlanPrepaid is never held by a vehicle. It marks a wash admitted on a prepaid wash.
	PlanPrepaid Plan = "prepaid"
)

// Outcome is what the lane does with the car.
type Outcome string

// Outcomes the lane can act on.
const (
	OutcomeAdmit Outcome = "admit"
	OutcomePay   Outcome = "pay"
	OutcomeStaff Outcome = "staff"
)

// Reason explains an outcome to the staff on the lane.
type Reason string

// Reasons behind each outcome.
const (
	ReasonLowConfidence  Reason = "low_confidence"
	ReasonMalformedPlate Reason = "malformed_plate"
	ReasonDuplicateRead  Reason = "duplicate_read"
	ReasonFleet          Reason = "fleet"
	ReasonWithinCap      Reason = "within_cap"
	ReasonCapReached     Reason = "cap_reached"
	ReasonPrepaidWash    Reason = "prepaid_wash"
	ReasonUnknownPlate   Reason = "unknown_plate"
	// ReasonUnknownPlateOffline sends an unknown plate to staff because the copy is too old to say it holds no subscription.
	ReasonUnknownPlateOffline Reason = "unknown_plate_offline"
)

// Read is one plate read from the lane camera.
type Read struct {
	Plate      string
	Confidence float64
}

// Entitlement is what the site's local copy holds for a plate.
type Entitlement struct {
	Plan      Plan
	CompanyID string
}

// Wash is one admitted wash in the site ledger.
type Wash struct {
	ID         string
	AdmittedAt time.Time
}

// Facts is what the store knows about a plate at the moment of a read.
type Facts struct {
	Entitlement     Entitlement
	WashesThisMonth int
	LastWash        Wash
	// LastPulledAt is when the copy last pulled from central, zero if it never has.
	LastPulledAt time.Time
	// PrepaidWashID is the plate's oldest unspent prepaid wash, empty if it holds none.
	PrepaidWashID string
}

// Policy holds the site's tunable decision thresholds.
type Policy struct {
	MinConfidence float64
	DedupWindow   time.Duration
	// MaxOffline is how old the copy can grow before an unknown plate goes to staff rather than to payment.
	MaxOffline time.Duration
}

// Decision is the answer the lane acts on. WashID is set only when an earlier wash is reused.
type Decision struct {
	Outcome Outcome
	Reason  Reason
	WashID  string
}

// Decide turns a read and what the store knows about its plate into admit, pay, or staff.
func Decide(read Read, facts Facts, policy Policy, now time.Time) Decision {
	if read.Confidence < policy.MinConfidence {
		return Decision{Outcome: OutcomeStaff, Reason: ReasonLowConfidence}
	}
	if _, ok := NormalizePlate(read.Plate); !ok {
		return Decision{Outcome: OutcomeStaff, Reason: ReasonMalformedPlate}
	}
	// Dedup runs ahead of the cap, so a camera firing twice on the eighth wash does not charge the second read.
	if isInsideWindow(facts.LastWash, policy.DedupWindow, now) {
		return Decision{Outcome: OutcomeAdmit, Reason: ReasonDuplicateRead, WashID: facts.LastWash.ID}
	}
	isPremium := facts.Entitlement.Plan == PlanPremium
	if facts.Entitlement.Plan == PlanFleet {
		return Decision{Outcome: OutcomeAdmit, Reason: ReasonFleet}
	}
	if isPremium && facts.WashesThisMonth < PremiumMonthlyCap {
		return Decision{Outcome: OutcomeAdmit, Reason: ReasonWithinCap}
	}
	// Included washes go first, so a paid wash is never spent on a car the plan admits anyway,
	// and a driver who paid is admitted even when the copy is too old to vouch for an unknown plate.
	if facts.PrepaidWashID != "" {
		return Decision{Outcome: OutcomeAdmit, Reason: ReasonPrepaidWash}
	}
	if isPremium {
		return Decision{Outcome: OutcomePay, Reason: ReasonCapReached}
	}
	if isStale(facts.LastPulledAt, policy.MaxOffline, now) {
		return Decision{Outcome: OutcomeStaff, Reason: ReasonUnknownPlateOffline}
	}
	return Decision{Outcome: OutcomePay, Reason: ReasonUnknownPlate}
}

// MonthStart returns the first instant of now's calendar month in the site's time zone.
func MonthStart(now time.Time, location *time.Location) time.Time {
	local := now.In(location)
	return time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, location)
}

func isInsideWindow(wash Wash, window time.Duration, now time.Time) bool {
	return wash.ID != "" && now.Sub(wash.AdmittedAt) < window
}

// isStale reports a copy never pulled, or last pulled longer than maxOffline before now.
func isStale(lastPulledAt time.Time, maxOffline time.Duration, now time.Time) bool {
	return lastPulledAt.IsZero() || now.Sub(lastPulledAt) > maxOffline
}
