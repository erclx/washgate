package siteagent

import (
	"testing"
	"time"
)

var testNow = time.Date(2026, time.March, 15, 10, 0, 0, 0, time.UTC)

func testPolicy() Policy {
	return Policy{MinConfidence: 0.99, DedupWindow: 120 * time.Second}
}

func newRead(plate string, confidence float64) Read {
	return Read{Plate: plate, Confidence: confidence}
}

func premiumFacts(washesThisMonth int) Facts {
	return Facts{Entitlement: Entitlement{Plan: PlanPremium}, WashesThisMonth: washesThisMonth}
}

func fleetFacts() Facts {
	return Facts{Entitlement: Entitlement{Plan: PlanFleet, CompanyID: "nordfrakt"}}
}

func withLastWash(facts Facts, id string, age time.Duration) Facts {
	facts.LastWash = Wash{ID: id, AdmittedAt: testNow.Add(-age)}
	return facts
}

func TestDecideCoversEveryReason(t *testing.T) {
	cases := []struct {
		name  string
		read  Read
		facts Facts
		want  Decision
	}{
		{
			name:  "low confidence goes to staff",
			read:  newRead("ABC123", 0.98),
			facts: premiumFacts(0),
			want:  Decision{Outcome: OutcomeStaff, Reason: ReasonLowConfidence},
		},
		{
			name:  "exactly the cutoff confidence is accepted",
			read:  newRead("ABC123", 0.99),
			facts: premiumFacts(0),
			want:  Decision{Outcome: OutcomeAdmit, Reason: ReasonWithinCap},
		},
		{
			name:  "malformed plate goes to staff",
			read:  newRead("A!", 1),
			facts: Facts{},
			want:  Decision{Outcome: OutcomeStaff, Reason: ReasonMalformedPlate},
		},
		{
			name:  "second read inside the window reuses the first wash",
			read:  newRead("ABC123", 1),
			facts: withLastWash(premiumFacts(3), "wash-1", 30*time.Second),
			want:  Decision{Outcome: OutcomeAdmit, Reason: ReasonDuplicateRead, WashID: "wash-1"},
		},
		{
			name:  "a wash older than the window is a new visit",
			read:  newRead("ABC123", 1),
			facts: withLastWash(premiumFacts(3), "wash-1", 121*time.Second),
			want:  Decision{Outcome: OutcomeAdmit, Reason: ReasonWithinCap},
		},
		{
			name:  "fleet car is admitted against its company",
			read:  newRead("KLM456", 1),
			facts: fleetFacts(),
			want:  Decision{Outcome: OutcomeAdmit, Reason: ReasonFleet},
		},
		{
			name:  "seventh premium wash is admitted",
			read:  newRead("ABC123", 1),
			facts: premiumFacts(6),
			want:  Decision{Outcome: OutcomeAdmit, Reason: ReasonWithinCap},
		},
		{
			name:  "eighth premium wash is admitted",
			read:  newRead("ABC123", 1),
			facts: premiumFacts(7),
			want:  Decision{Outcome: OutcomeAdmit, Reason: ReasonWithinCap},
		},
		{
			name:  "ninth premium wash is offered a single wash",
			read:  newRead("ABC123", 1),
			facts: premiumFacts(8),
			want:  Decision{Outcome: OutcomePay, Reason: ReasonCapReached},
		},
		{
			name:  "duplicate of the eighth wash is still admitted",
			read:  newRead("ABC123", 1),
			facts: withLastWash(premiumFacts(8), "wash-8", 10*time.Second),
			want:  Decision{Outcome: OutcomeAdmit, Reason: ReasonDuplicateRead, WashID: "wash-8"},
		},
		{
			name:  "unknown plate pays per wash",
			read:  newRead("XYZ789", 1),
			facts: Facts{},
			want:  Decision{Outcome: OutcomePay, Reason: ReasonUnknownPlate},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Decide(tc.read, tc.facts, testPolicy(), testNow)

			if got != tc.want {
				t.Errorf("Decide() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestMonthStartUsesTheSiteTimeZone(t *testing.T) {
	stockholm, err := time.LoadLocation("Europe/Stockholm")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	justAfterMidnightOnTheFirst := time.Date(2026, time.January, 31, 23, 30, 0, 0, time.UTC)

	got := MonthStart(justAfterMidnightOnTheFirst, stockholm)

	want := time.Date(2026, time.January, 31, 23, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("MonthStart() = %v, want %v", got.UTC(), want)
	}
}
