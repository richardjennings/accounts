package ledger

import (
	"fmt"
	"time"
)

// Date is a calendar date without time-of-day or zone — the right granularity for
// a posting or journal date. Callers pass dates in explicitly; the ledger never
// reads the wall clock.
type Date struct {
	Year  int
	Month time.Month
	Day   int
}

// NewDate returns the given calendar date.
func NewDate(year int, month time.Month, day int) Date {
	return Date{Year: year, Month: month, Day: day}
}

// String renders the date in ISO 8601 (YYYY-MM-DD).
func (d Date) String() string {
	return fmt.Sprintf("%04d-%02d-%02d", d.Year, int(d.Month), d.Day)
}

// IsZero reports whether d is the zero value.
func (d Date) IsZero() bool { return d == Date{} }

// Before reports whether d falls before o.
func (d Date) Before(o Date) bool {
	if d.Year != o.Year {
		return d.Year < o.Year
	}
	if d.Month != o.Month {
		return d.Month < o.Month
	}
	return d.Day < o.Day
}
