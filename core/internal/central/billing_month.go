package central

import (
	"time"
	_ "time/tzdata" // embeds the time zone database so a slim image can load the billing zone
)

// billingTimeZone is where a calendar month, and so the monthly cap, begins and ends.
const billingTimeZone = "Europe/Stockholm"

// billingMonth is central's one clock for "this month in Stockholm", shared by every handler that counts
// washes against the monthly cap, so the operator and the customer read the same month.
type billingMonth struct {
	location *time.Location
	now      func() time.Time
}

func newBillingMonth() billingMonth {
	location, err := time.LoadLocation(billingTimeZone)
	if err != nil {
		// The zone database is embedded above, so a failure here is a build defect rather than a runtime condition.
		panic("load billing time zone: " + err.Error())
	}
	return billingMonth{location: location, now: time.Now}
}

// start is midnight on the first of the current month in the billing zone.
func (m billingMonth) start() time.Time {
	local := m.now().In(m.location)
	return time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, m.location)
}

// nextStart is midnight on the first of next month in the billing zone, when the monthly cap resets.
func (m billingMonth) nextStart() time.Time {
	return m.start().AddDate(0, 1, 0)
}
