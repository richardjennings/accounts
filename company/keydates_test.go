package company

import (
	"testing"
	"time"

	"github.com/richardjennings/accounts/ledger"
)

// jennings mirrors a real company record: incorporated 26 April 2023 with a
// 30 April year end, last confirmation statement made up to 25 April 2026.
func jennings() Company {
	c := Default()
	c.Incorporated = d(2023, time.April, 26)
	c.YearEndDay, c.YearEndMonth = 30, time.April
	c.LastStatementDate = d(2026, time.April, 25)
	c.VATRegistered = false
	return c
}

func TestFirstPeriodRunsMoreThanSixMonths(t *testing.T) {
	c := jennings()
	fy := c.Year(1)
	if fy.Start != d(2023, time.April, 26) || fy.End != d(2024, time.April, 30) {
		t.Fatalf("FY1 = %s..%s, want 2023-04-26..2024-04-30", fy.Start, fy.End)
	}
	if fy2 := c.Year(2); fy2.Start != d(2024, time.May, 1) || fy2.End != d(2025, time.April, 30) {
		t.Fatalf("FY2 = %s..%s, want 2024-05-01..2025-04-30", fy2.Start, fy2.End)
	}
	if got := c.YearContaining(d(2026, time.August, 31)).Number; got != 4 {
		t.Fatalf("2026-08-31 is in FY%d, want FY4", got)
	}
}

func TestAddMonthsClampsToMonthEnd(t *testing.T) {
	if got := addMonths(d(2026, time.May, 31), 9); got != d(2027, time.February, 28) {
		t.Errorf("31 May + 9 months = %s, want 2027-02-28", got)
	}
	if got := addMonths(d(2024, time.February, 29), 12); got != d(2025, time.February, 28) {
		t.Errorf("29 Feb 2024 + 12 months = %s, want 2025-02-28", got)
	}
	if got := addMonths(d(2026, time.April, 30), 9); got != d(2027, time.January, 31) {
		t.Errorf("30 Apr + 9 months = %s, want 2027-01-31 (month end to month end)", got)
	}
	if got := addMonths(d(2026, time.April, 15), 9); got != d(2027, time.January, 15) {
		t.Errorf("15 Apr + 9 months = %s, want 2027-01-15", got)
	}
}

func TestAccountsDue(t *testing.T) {
	c := jennings()
	// A first period longer than twelve months: twenty-one months from incorporation.
	if got := c.AccountsDue(c.Year(1)); got != d(2025, time.January, 26) {
		t.Errorf("first accounts due %s, want 2025-01-26", got)
	}
	if got := c.AccountsDue(c.Year(3)); got != d(2027, time.January, 31) {
		t.Errorf("accounts to 30 Apr 2026 due %s, want 2027-01-31", got)
	}
	// A twelve-month first period follows the ordinary rule.
	if got := Default().AccountsDue(Default().Year(1)); got != d(2027, time.December, 31) {
		t.Errorf("default first accounts due %s, want 2027-12-31", got)
	}
}

func TestCorporationTaxDates(t *testing.T) {
	c := jennings()
	fy3 := c.Year(3)
	periods := TaxPeriods(fy3)
	if len(periods) != 1 {
		t.Fatalf("FY3 tax periods = %v, want one", periods)
	}
	if got := TaxPaymentDue(periods[0]); got != d(2027, time.February, 1) {
		t.Errorf("tax for year to 30 Apr 2026 due %s, want 2027-02-01", got)
	}
	if got := TaxReturnDue(fy3); got != d(2027, time.April, 30) {
		t.Errorf("CT600 for year to 30 Apr 2026 due %s, want 2027-04-30", got)
	}
	// The long first year splits into twelve months and four days.
	first := TaxPeriods(c.Year(1))
	if len(first) != 2 || first[0].End != d(2024, time.April, 25) || first[1].Start != d(2024, time.April, 26) || first[1].End != d(2024, time.April, 30) {
		t.Fatalf("FY1 tax periods = %v, want 2023-04-26..2024-04-25 and 2024-04-26..2024-04-30", first)
	}
}

func TestConfirmationStatement(t *testing.T) {
	c := jennings()
	p := c.ReviewPeriod()
	if p.Start != d(2026, time.April, 26) || p.End != d(2027, time.April, 25) {
		t.Fatalf("review period = %s..%s, want 2026-04-26..2027-04-25", p.Start, p.End)
	}
	sd, due := c.NextStatement()
	if sd != d(2027, time.April, 25) || due != d(2027, time.May, 9) {
		t.Errorf("next statement %s due %s, want 2027-04-25 due 2027-05-09", sd, due)
	}
	// With no statement made yet the first review period begins on incorporation.
	c.LastStatementDate = ledger.Date{}
	if sd, due := c.NextStatement(); sd != d(2024, time.April, 25) || due != d(2024, time.May, 9) {
		t.Errorf("first statement %s due %s, want 2024-04-25 due 2024-05-09", sd, due)
	}
}

func TestVATQuarters(t *testing.T) {
	c := Default() // quarters end March, June, September, December
	if got := c.VATQuarterEnd(d(2026, time.August, 31)); got != d(2026, time.September, 30) {
		t.Errorf("quarter containing 31 Aug ends %s, want 2026-09-30", got)
	}
	if got := c.VATQuarterEnd(d(2026, time.September, 30)); got != d(2026, time.September, 30) {
		t.Errorf("quarter containing 30 Sep ends %s, want 2026-09-30", got)
	}
	if got := VATReturnDue(d(2026, time.September, 30)); got != d(2026, time.November, 7) {
		t.Errorf("return for quarter to 30 Sep due %s, want 2026-11-07", got)
	}
	c.VATQuarterEndMonth = time.January
	if got := c.VATQuarterEnd(d(2026, time.December, 15)); got != d(2027, time.January, 31) {
		t.Errorf("quarter containing 15 Dec ends %s, want 2027-01-31", got)
	}
}

func find(list []KeyDate, what string) (KeyDate, bool) {
	for _, k := range list {
		if k.What == what {
			return k, true
		}
	}
	return KeyDate{}, false
}

func TestKeyDatesForTheRealCalendar(t *testing.T) {
	c := jennings()
	on := d(2026, time.September, 2)
	list := c.KeyDates(on, KeyDateOptions{})
	for i := 1; i < len(list); i++ {
		if list[i].Due.Before(list[i-1].Due) {
			t.Fatalf("key dates are not sorted: %v", list)
		}
	}
	want := map[string]ledger.Date{
		"Annual accounts":            d(2027, time.January, 31),
		"Corporation tax payment":    d(2027, time.February, 1),
		"Company tax return (CT600)": d(2027, time.April, 30),
		"Confirmation statement":     d(2027, time.May, 9),
	}
	for what, due := range want {
		k, ok := find(list, what)
		if !ok {
			t.Errorf("no %q in %v", what, list)
			continue
		}
		if k.Due != due || k.Overdue {
			t.Errorf("%s due %s (overdue %v), want %s", what, k.Due, k.Overdue, due)
		}
	}
	if _, ok := find(list, "VAT return and payment"); ok {
		t.Error("a company that is not VAT registered has no VAT return date")
	}
	if _, ok := find(list, "PAYE and NIC payment"); ok {
		t.Error("PAYE is listed without a payroll")
	}
}

func TestKeyDatesFlagOverdueStatement(t *testing.T) {
	c := jennings()
	c.LastStatementDate = d(2025, time.April, 25)
	k, ok := find(c.KeyDates(d(2026, time.September, 2), KeyDateOptions{}), "Confirmation statement")
	if !ok || !k.Overdue || k.Due != d(2026, time.May, 9) {
		t.Fatalf("statement = %+v, want overdue and due 2026-05-09", k)
	}
}

func TestKeyDatesWithVATAndPayroll(t *testing.T) {
	c := Default() // VAT quarters end March, June, September, December
	on := d(2026, time.October, 25)
	list := c.KeyDates(on, KeyDateOptions{Payroll: true, Benefits: true})
	want := map[string]ledger.Date{
		"VAT return and payment": d(2026, time.November, 7), // quarter to 30 September
		"PAYE and NIC payment":   d(2026, time.November, 22),
		"P60 to each employee":   d(2027, time.May, 31),
		"P11D and P11D(b)":       d(2027, time.July, 6),
		"Class 1A NIC payment":   d(2027, time.July, 22),
	}
	for what, due := range want {
		k, ok := find(list, what)
		if !ok || k.Due != due {
			t.Errorf("%s = %+v, want due %s", what, k, due)
		}
	}
	// On the 22nd itself PAYE is due that day.
	k, _ := find(c.KeyDates(d(2026, time.October, 22), KeyDateOptions{Payroll: true}), "PAYE and NIC payment")
	if k.Due != d(2026, time.October, 22) {
		t.Errorf("PAYE on the 22nd due %s, want 2026-10-22", k.Due)
	}
	// The return for the quarter to 30 June is due 7 August, so on 1 August it is still the next one.
	k, _ = find(c.KeyDates(d(2026, time.August, 1), KeyDateOptions{}), "VAT return and payment")
	if k.Due != d(2026, time.August, 7) {
		t.Errorf("VAT return on 1 Aug due %s, want 2026-08-07", k.Due)
	}
}
