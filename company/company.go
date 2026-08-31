// Package company models the entity the books belong to: its identity (name,
// registered number), when it was incorporated, its accounting reference date
// (year-end), and its base currency. From those it derives the accounting periods
// — the financial years — that every report and transaction is dated within. This
// is the trunk the ledger, subsidiary ledgers and statements hang off.
package company

import (
	"fmt"
	"time"

	"github.com/richardjennings/accounts/ledger"
	"github.com/richardjennings/accounts/money"
)

// Company is the reporting entity.
type Company struct {
	Name             string
	Number           string // Companies House registered number
	SICCode          string // Standard Industrial Classification code
	RegisteredOffice string
	Incorporated     ledger.Date
	YearEndDay       int        // accounting reference date — day
	YearEndMonth     time.Month // accounting reference date — month
	Currency         money.Currency
	VATRegistered    bool   // whether the company charges and reclaims VAT
	VATNumber        string // VAT registration number, when registered
}

// FinancialYear is one accounting period. The first runs from incorporation to the
// first year-end after it; each subsequent one is the following twelve months.
type FinancialYear struct {
	Number int
	Start  ledger.Date
	End    ledger.Date
}

func (fy FinancialYear) String() string {
	return fmt.Sprintf("FY%d: %s to %s", fy.Number, fy.Start, fy.End)
}

// YearEndLabel renders the accounting reference date, e.g. "31 March".
func (c Company) YearEndLabel() string {
	return fmt.Sprintf("%d %s", c.YearEndDay, c.YearEndMonth)
}

func toTime(d ledger.Date) time.Time { return time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, time.UTC) }
func fromTime(t time.Time) ledger.Date {
	return ledger.NewDate(t.Year(), t.Month(), t.Day())
}

func (c Company) yearEnd(year int) ledger.Date {
	return ledger.NewDate(year, c.YearEndMonth, c.YearEndDay)
}

// firstYearEnd is the first accounting reference date strictly after incorporation.
func (c Company) firstYearEnd() ledger.Date {
	ye := c.yearEnd(c.Incorporated.Year)
	if !toTime(c.Incorporated).Before(toTime(ye)) { // incorporated on/after that year-end
		ye = c.yearEnd(c.Incorporated.Year + 1)
	}
	return ye
}

// YearContaining returns the financial year that the given date falls in.
func (c Company) YearContaining(on ledger.Date) FinancialYear {
	end := c.firstYearEnd()
	num := 1
	for toTime(end).Before(toTime(on)) {
		end = c.yearEnd(end.Year + 1)
		num++
	}
	start := c.Incorporated
	if num > 1 {
		start = fromTime(toTime(c.yearEnd(end.Year-1)).AddDate(0, 0, 1)) // day after the previous year-end
	}
	return FinancialYear{Number: num, Start: start, End: end}
}

// NextYearStart returns the first day of the financial year after the one
// containing on — used to "close" a year and move the clock forward.
func (c Company) NextYearStart(on ledger.Date) ledger.Date {
	return fromTime(toTime(c.YearContaining(on).End).AddDate(0, 0, 1))
}

// Default returns a starter company for a fresh game — a placeholder to be edited.
func Default() Company {
	return Company{
		Name:             "Your Company Ltd",
		Number:           "00000000",
		SICCode:          "62012",
		RegisteredOffice: "1 Example Street, London, EC1A 1AA",
		Incorporated:     ledger.NewDate(2026, time.April, 1),
		YearEndDay:       31,
		YearEndMonth:     time.March,
		Currency:         money.GBP,
		VATRegistered:    true,
		VATNumber:        "GB123456789",
	}
}
