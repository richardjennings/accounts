package company

import (
	"sort"
	"time"

	"github.com/richardjennings/accounts/ledger"
)

// Recipients of a filing or payment.
const (
	CompaniesHouse = "Companies House"
	HMRC           = "HMRC"
)

// KeyDate is one filing or payment and the date it is due.
type KeyDate struct {
	Due       ledger.Date
	What      string // the filing or payment, e.g. "Confirmation statement"
	Detail    string // what it covers, e.g. "made up to 2027-04-25"
	Recipient string // CompaniesHouse or HMRC
	Overdue   bool   // the due date is before the date the list was made for
}

// KeyDateOptions says which optional obligations apply to the company.
type KeyDateOptions struct {
	Payroll  bool // the company runs a payroll, so PAYE falls due each month
	Benefits bool // the company gives benefits in kind, so P11D(b) and Class 1A NIC fall due
}

// Period is a span of dates, both ends inclusive.
type Period struct{ Start, End ledger.Date }

// addMonths moves a date forward by whole months the way the Companies Act
// (section 443) and HMRC count them: the last day of a month moves to the last day
// of the target month, and a day the target month does not have becomes its last
// day.
func addMonths(d ledger.Date, n int) ledger.Date {
	first := time.Date(d.Year, d.Month+time.Month(n), 1, 0, 0, 0, 0, time.UTC)
	last := first.AddDate(0, 1, -1).Day()
	day := d.Day
	if day > last || d.Day == lastDay(d) {
		day = last
	}
	return ledger.NewDate(first.Year(), first.Month(), day)
}

// lastDay is the number of days in the date's month.
func lastDay(d ledger.Date) int { return time.Date(d.Year, d.Month+1, 0, 0, 0, 0, 0, time.UTC).Day() }

func addDays(d ledger.Date, n int) ledger.Date { return fromTime(toTime(d).AddDate(0, 0, n)) }

// twelveMonthsFrom returns the last day of the twelve months that begin on start.
func twelveMonthsFrom(start ledger.Date) ledger.Date { return addMonths(addDays(start, -1), 12) }

func later(a, b ledger.Date) ledger.Date {
	if a.Before(b) {
		return b
	}
	return a
}

// AccountsDue is the last day to deliver the accounts for a financial year to
// Companies House: nine months after the year end. For a first financial year
// longer than twelve months it is the later of nine months after the first
// anniversary of incorporation and three months after the year end.
func (c Company) AccountsDue(fy FinancialYear) ledger.Date {
	if fy.Number == 1 && twelveMonthsFrom(fy.Start).Before(fy.End) {
		return later(addMonths(c.Incorporated, 21), addMonths(fy.End, 3))
	}
	return addMonths(fy.End, 9)
}

// TaxPeriods splits a financial year into corporation tax accounting periods. An
// accounting period runs for at most twelve months, so a longer first financial
// year gives two: the first twelve months and the remainder.
func TaxPeriods(fy FinancialYear) []Period {
	twelve := twelveMonthsFrom(fy.Start)
	if !twelve.Before(fy.End) {
		return []Period{{fy.Start, fy.End}}
	}
	return []Period{{fy.Start, twelve}, {addDays(twelve, 1), fy.End}}
}

// TaxPaymentDue is the day corporation tax for an accounting period must reach
// HMRC: nine months and one day after the period ends.
func TaxPaymentDue(p Period) ledger.Date { return addDays(addMonths(p.End, 9), 1) }

// TaxReturnDue is the last day to file the CT600 return for each accounting period
// in a financial year: twelve months after the financial year ends.
func TaxReturnDue(fy FinancialYear) ledger.Date { return addMonths(fy.End, 12) }

// ReviewPeriod is the twelve months the next confirmation statement covers. The
// first begins on incorporation; each later one begins the day after the last
// statement date.
func (c Company) ReviewPeriod() Period {
	start := c.Incorporated
	if !c.LastStatementDate.IsZero() {
		start = addDays(c.LastStatementDate, 1)
	}
	return Period{Start: start, End: twelveMonthsFrom(start)}
}

// NextStatement returns the statement date of the next confirmation statement and
// the last day to deliver it: fourteen days after the review period ends.
func (c Company) NextStatement() (statementDate, due ledger.Date) {
	p := c.ReviewPeriod()
	return p.End, addDays(p.End, 14)
}

// VATQuarterEnd is the last day of the VAT quarter that contains the date, or the
// zero date when the company has no VAT quarter end month.
func (c Company) VATQuarterEnd(on ledger.Date) ledger.Date {
	if c.VATQuarterEndMonth == 0 {
		return ledger.Date{}
	}
	for i := 0; i < 3; i++ {
		m := time.Date(on.Year, on.Month+time.Month(i), 1, 0, 0, 0, 0, time.UTC)
		if (int(m.Month())-int(c.VATQuarterEndMonth)+12)%3 == 0 {
			return fromTime(m.AddDate(0, 1, -1))
		}
	}
	return ledger.Date{}
}

// VATReturnDue is the last day to file a VAT return and pay it: one month and
// seven days after the quarter ends.
func VATReturnDue(quarterEnd ledger.Date) ledger.Date { return addDays(addMonths(quarterEnd, 1), 7) }

// nextAnnual returns the first occurrence of a day and month on or after on.
func nextAnnual(on ledger.Date, month time.Month, day int) ledger.Date {
	d := ledger.NewDate(on.Year, month, day)
	if d.Before(on) {
		d = ledger.NewDate(on.Year+1, month, day)
	}
	return d
}

// KeyDates lists the filings and payments due on or after the date, soonest first.
// Annual accounts and corporation tax come from every financial year so far; the
// list keeps those still ahead. The confirmation statement is derived from the
// last statement date, so it stays in the list when overdue.
func (c Company) KeyDates(on ledger.Date, opts KeyDateOptions) []KeyDate {
	var out []KeyDate
	add := func(due ledger.Date, what, detail, to string) {
		if !due.Before(on) {
			out = append(out, KeyDate{Due: due, What: what, Detail: detail, Recipient: to})
		}
	}
	current := c.YearContaining(on)
	for n := 1; n <= current.Number; n++ {
		fy := c.Year(n)
		add(c.AccountsDue(fy), "Annual accounts", "for the year to "+fy.End.String(), CompaniesHouse)
		for _, p := range TaxPeriods(fy) {
			span := "for the period " + p.Start.String() + " to " + p.End.String()
			add(TaxPaymentDue(p), "Corporation tax payment", span, HMRC)
			add(TaxReturnDue(fy), "Company tax return (CT600)", span, HMRC)
		}
	}

	statementDate, due := c.NextStatement()
	out = append(out, KeyDate{Due: due, What: "Confirmation statement", Detail: "made up to " + statementDate.String(), Recipient: CompaniesHouse, Overdue: due.Before(on)})

	if c.VATRegistered && c.VATQuarterEndMonth != 0 {
		q := c.VATQuarterEnd(addMonths(on, -3))
		for VATReturnDue(q).Before(on) {
			q = c.VATQuarterEnd(addDays(q, 1))
		}
		add(VATReturnDue(q), "VAT return and payment", "for the quarter to "+q.String(), HMRC)
	}

	if opts.Payroll {
		paye := ledger.NewDate(on.Year, on.Month, 22)
		if paye.Before(on) {
			paye = addMonths(ledger.NewDate(on.Year, on.Month, 1), 1)
			paye = ledger.NewDate(paye.Year, paye.Month, 22)
		}
		add(paye, "PAYE and NIC payment", "for the tax month to "+ledger.NewDate(paye.Year, paye.Month, 5).String(), HMRC)
		add(nextAnnual(on, time.May, 31), "P60 to each employee", "for the tax year to 5 April", HMRC)
	}
	if opts.Benefits {
		add(nextAnnual(on, time.July, 6), "P11D and P11D(b)", "for the tax year to 5 April", HMRC)
		add(nextAnnual(on, time.July, 22), "Class 1A NIC payment", "on benefits for the tax year to 5 April", HMRC)
	}

	sort.SliceStable(out, func(i, j int) bool { return out[i].Due.Before(out[j].Due) })
	return out
}
