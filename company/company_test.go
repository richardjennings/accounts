package company

import (
	"testing"
	"time"

	"github.com/richardjennings/accounts/ledger"
)

func d(y int, m time.Month, day int) ledger.Date { return ledger.NewDate(y, m, day) }

func TestFirstYearFromIncorporation(t *testing.T) {
	c := Default() // incorporated 1 Apr 2026, year-end 31 March
	fy := c.YearContaining(d(2026, time.June, 1))
	if fy.Number != 1 || fy.Start != d(2026, time.April, 1) || fy.End != d(2027, time.March, 31) {
		t.Fatalf("FY1 = %d %s..%s, want 1 2026-04-01..2027-03-31", fy.Number, fy.Start, fy.End)
	}
}

func TestSecondYear(t *testing.T) {
	fy := Default().YearContaining(d(2027, time.June, 1))
	if fy.Number != 2 || fy.Start != d(2027, time.April, 1) || fy.End != d(2028, time.March, 31) {
		t.Fatalf("FY2 = %d %s..%s, want 2 2027-04-01..2028-03-31", fy.Number, fy.Start, fy.End)
	}
}

func TestYearEndDayBelongsToThatYear(t *testing.T) {
	// The year-end date itself is the last day of that period, not the next.
	fy := Default().YearContaining(d(2027, time.March, 31))
	if fy.Number != 1 {
		t.Fatalf("2027-03-31 should be in FY1, got FY%d", fy.Number)
	}
}

func TestIrregularFirstPeriod(t *testing.T) {
	c := Default()
	c.Incorporated = d(2026, time.June, 15) // mid-year: first period is short
	fy := c.YearContaining(d(2026, time.September, 1))
	if fy.Start != d(2026, time.June, 15) || fy.End != d(2027, time.March, 31) {
		t.Fatalf("irregular FY1 = %s..%s, want 2026-06-15..2027-03-31", fy.Start, fy.End)
	}
}

func TestNextYearStart(t *testing.T) {
	if got := Default().NextYearStart(d(2026, time.June, 1)); got != d(2027, time.April, 1) {
		t.Fatalf("next year starts %s, want 2027-04-01", got)
	}
}
