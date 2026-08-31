package importer

import (
	"fmt"
	"strings"
	"time"

	"github.com/richardjennings/accounts/ledger"
	"github.com/richardjennings/accounts/money"
)

// StatementSpec says which column of a bank-statement table supplies each
// field, and how to read them. Column values are header names ("" = unused).
// A statement has either one signed Amount column or a Paid In / Paid Out
// pair. Negate flips the sign of a single amount column, for a bank that
// reports money out as positive.
type StatementSpec struct {
	Date        string
	Description string
	Amount      string
	In          string
	Out         string
	Balance     string
	DateOrder   string // "dmy" (UK, the default), "ymd", or "mdy"
	Negate      bool
}

// StatementLine is one parsed statement movement. Amount is signed: money in
// is positive.
type StatementLine struct {
	Date        ledger.Date
	Description string
	Amount      money.Money
	Balance     money.Money
	HasBalance  bool
}

// DetectStatement proposes a spec from a table's header names, using the
// vocabulary of common UK bank exports.
func DetectStatement(t *Table) StatementSpec {
	pick := func(names ...string) string {
		if i := t.Col(names...); i >= 0 {
			return t.Header[i]
		}
		return ""
	}
	s := StatementSpec{
		Date:        pick("Date", "Transaction date", "Date Time", "Booking Date"),
		Description: pick("Description", "Reference", "Details", "Narrative", "Transaction description", "Payment Reference", "Merchant"),
		In:          pick("Paid In", "Money In", "Credit", "Credit Amount", "In"),
		Out:         pick("Paid Out", "Money Out", "Debit", "Debit Amount", "Out"),
		Balance:     pick("Balance", "Running Balance", "Balance After"),
		DateOrder:   "dmy",
	}
	if s.In == "" && s.Out == "" {
		s.Amount = pick("Amount", "Value", "Transaction Amount")
	}
	return s
}

// Columns returns the header names a spec uses, in field order.
func (s StatementSpec) Columns() []string {
	return []string{s.Date, s.Description, s.Amount, s.In, s.Out, s.Balance}
}

// Validate reports whether the spec names the columns a statement needs and
// they exist in the table.
func (s StatementSpec) Validate(t *Table) error {
	if s.Date == "" {
		return fmt.Errorf("choose the date column")
	}
	if s.Amount == "" && s.In == "" && s.Out == "" {
		return fmt.Errorf("choose an amount column, or a paid-in / paid-out pair")
	}
	if s.Amount != "" && (s.In != "" || s.Out != "") {
		return fmt.Errorf("choose either one amount column or a paid-in / paid-out pair, not both")
	}
	for _, name := range s.Columns() {
		if name != "" && t.Col(name) < 0 {
			return fmt.Errorf("no column %q in the file", name)
		}
	}
	switch s.DateOrder {
	case "", "dmy", "ymd", "mdy":
	default:
		return fmt.Errorf("unknown date order %q", s.DateOrder)
	}
	return nil
}

// ReadStatement parses a table with a spec. Rows without a date or without any
// movement are skipped with an issue; a bad amount or date is an issue too.
func ReadStatement(t *Table, s StatementSpec, cur money.Currency) ([]StatementLine, []Issue) {
	var out []StatementLine
	var issues []Issue
	bad := func(r Row, format string, args ...any) {
		issues = append(issues, Issue{Table: t.Name, Row: r.N(), Msg: fmt.Sprintf(format, args...)})
	}
	t.Each(func(r Row) {
		if blankRow(r, s) {
			return
		}
		date, err := statementDate(r, s)
		if err != nil {
			bad(r, "%v; skipped", err)
			return
		}
		amount, moved, err := statementAmount(r, s, cur)
		if err != nil {
			bad(r, "%v; skipped", err)
			return
		}
		line := StatementLine{Date: date, Description: r.Text(s.Description), Amount: amount}
		if s.Balance != "" {
			if b, err := statementMoney(r, s.Balance, cur); err == nil {
				line.Balance, line.HasBalance = b, true
			}
		}
		if !moved {
			if !line.HasBalance {
				bad(r, "no amount; skipped")
			}
			return // a balance-only row (e.g. an opening balance) is not a movement
		}
		out = append(out, line)
	})
	return out, issues
}

func blankRow(r Row, s StatementSpec) bool {
	for _, name := range s.Columns() {
		if name != "" && r.Text(name) != "" {
			return false
		}
	}
	return true
}

// statementDate reads the date column under the spec's date order. Dates the
// source typed (xls cells) need no format; text tries the chosen order first,
// then ISO.
func statementDate(r Row, s StatementSpec) (ledger.Date, error) {
	if v, ok := r.value(s.Date); ok && !v.Date.IsZero() {
		return v.Date, nil
	}
	text := strings.TrimSpace(r.Text(s.Date))
	if i := strings.IndexAny(text, " T"); i > 0 { // drop a time-of-day part
		text = text[:i]
	}
	var layouts []string
	switch s.DateOrder {
	case "ymd":
		layouts = []string{"2006-01-02", "2006/01/02", "20060102"}
	case "mdy":
		layouts = []string{"01/02/2006", "1/2/2006", "01-02-2006"}
	default:
		layouts = []string{"02/01/2006", "2/1/2006", "02-01-2006", "02.01.2006", "02/01/06", "02 Jan 2006", "02 January 2006"}
	}
	layouts = append(layouts, "2006-01-02")
	for _, l := range layouts {
		if tm, err := time.Parse(l, text); err == nil {
			return ledger.NewDate(tm.Year(), tm.Month(), tm.Day()), nil
		}
	}
	return ledger.Date{}, fmt.Errorf("%q is not a date", text)
}

// statementAmount reads the signed movement. moved is false when the row has
// no amount at all.
func statementAmount(r Row, s StatementSpec, cur money.Currency) (money.Money, bool, error) {
	if s.Amount != "" {
		text := r.Text(s.Amount)
		if text == "" {
			return money.Zero(cur), false, nil
		}
		m, err := statementMoney(r, s.Amount, cur)
		if err != nil {
			return money.Money{}, false, err
		}
		if s.Negate {
			m = m.Neg()
		}
		return m, true, nil
	}
	in, out := r.Text(s.In), r.Text(s.Out)
	if in == "" && out == "" {
		return money.Zero(cur), false, nil
	}
	amount := money.Zero(cur)
	if in != "" {
		v, err := statementMoney(r, s.In, cur)
		if err != nil {
			return money.Money{}, false, err
		}
		amount = v
	}
	if out != "" {
		v, err := statementMoney(r, s.Out, cur)
		if err != nil {
			return money.Money{}, false, err
		}
		amount, _ = amount.Sub(v.Abs()) // some banks list paid-out as negative already
	}
	return amount, true, nil
}

// statementMoney reads an amount cell, tolerating currency signs, thousands
// separators and a bracketed negative.
func statementMoney(r Row, col string, cur money.Currency) (money.Money, error) {
	if v, ok := r.value(col); ok && v.IsNum {
		m, err := money.Parse(cur, v.Num)
		if err != nil {
			return money.Money{}, fmt.Errorf("%s: %q is not an amount at %s scale", col, v.Num, cur.Code)
		}
		return m, nil
	}
	text := strings.TrimSpace(r.Text(col))
	neg := false
	if strings.HasPrefix(text, "(") && strings.HasSuffix(text, ")") {
		neg, text = true, text[1:len(text)-1]
	}
	text = strings.NewReplacer("£", "", "$", "", "€", "", ",", "", " ", "").Replace(text)
	m, err := money.Parse(cur, text)
	if err != nil {
		return money.Money{}, fmt.Errorf("%s: %q is not an amount", col, r.Text(col))
	}
	if neg {
		m = m.Neg()
	}
	return m, nil
}
