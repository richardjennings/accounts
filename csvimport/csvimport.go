// Package csvimport reads accounting data from CSV — the only inbound channel this
// product has. It targets the export/import layouts of common UK small-company
// software (Crunch's importer templates in particular): one row per invoice, one row
// per expense, gross (VAT-inclusive) totals, and UK day-first dates. Parsing is
// tolerant: columns are matched by header name (case- and space-insensitive) so
// re-ordered or slightly-renamed headers still load.
//
// It parses and validates into typed rows; applying them to a ledger is the caller's
// job, so import stays separate from posting.
package csvimport

import (
	"encoding/csv"
	"fmt"
	"io"
	"math/big"
	"strings"
	"time"

	"github.com/richardjennings/accounts/ledger"
	"github.com/richardjennings/accounts/money"
	"github.com/richardjennings/accounts/tax/vat"
	"github.com/richardjennings/decimal"
)

// RowError notes a row that could not be parsed, so a partial import can report what
// it skipped rather than failing wholesale.
type RowError struct {
	Line   int
	Reason string
}

func (e RowError) Error() string { return fmt.Sprintf("line %d: %s", e.Line, e.Reason) }

// InvoiceRow is one imported sales invoice.
type InvoiceRow struct {
	IssueDate   ledger.Date
	Client      string
	Description string
	Gross       money.Money
	Net         money.Money
	VAT         money.Money
	Paid        bool
	PaymentDate ledger.Date // zero unless Paid
}

// ExpenseRow is one imported cost.
type ExpenseRow struct {
	Date        ledger.Date
	Supplier    string
	Description string
	Gross       money.Money
	Net         money.Money
	VAT         money.Money
}

// StatementRow is one line of an imported bank statement. Amount is signed: money in
// is positive, money out negative. Balance is the running balance if the statement
// carries one.
type StatementRow struct {
	Date        ledger.Date
	Description string
	Amount      money.Money
	Balance     money.Money
	HasBalance  bool
}

// ParseStatement reads a bank statement: Date, Reference/Description, and either a
// single signed Amount column or separate Paid In / Paid Out columns, with an
// optional running Balance.
func ParseStatement(r io.Reader, cur money.Currency) ([]StatementRow, []RowError, error) {
	records, err := readCSV(r)
	if err != nil || len(records) == 0 {
		return nil, nil, err
	}
	h := newHeader(records[0])
	var out []StatementRow
	var skipped []RowError
	for i, row := range records[1:] {
		line := i + 2
		dateStr := h.get(row, "Date")
		if dateStr == "" {
			continue
		}
		date, err := parseDate(dateStr)
		if err != nil {
			skipped = append(skipped, RowError{line, "invalid date"})
			continue
		}
		amount, err := statementAmount(h, row, cur)
		if err != nil {
			skipped = append(skipped, RowError{line, err.Error()})
			continue
		}
		sr := StatementRow{Date: date, Description: h.get(row, "Description", "Reference", "Details"), Amount: amount}
		if bs := h.get(row, "Balance"); bs != "" {
			if bal, err := money.Parse(cur, cleanNumber(bs)); err == nil {
				sr.Balance, sr.HasBalance = bal, true
			}
		}
		out = append(out, sr)
	}
	return out, skipped, nil
}

// statementAmount reads a signed movement from either a single Amount column or a
// Paid In / Paid Out pair.
func statementAmount(h header, row []string, cur money.Currency) (money.Money, error) {
	if in, out := h.get(row, "Paid In", "Money In", "Credit"), h.get(row, "Paid Out", "Money Out", "Debit"); in != "" || out != "" {
		amt := money.Zero(cur)
		if in != "" {
			v, err := money.Parse(cur, cleanNumber(in))
			if err != nil {
				return money.Money{}, fmt.Errorf("invalid paid-in %q", in)
			}
			amt = v
		}
		if out != "" {
			v, err := money.Parse(cur, cleanNumber(out))
			if err != nil {
				return money.Money{}, fmt.Errorf("invalid paid-out %q", out)
			}
			amt, _ = amt.Sub(v)
		}
		return amt, nil
	}
	s := h.get(row, "Amount")
	if s == "" {
		return money.Zero(cur), nil // balance-only row (e.g. an opening balance) — no movement
	}
	v, err := money.Parse(cur, cleanNumber(s))
	if err != nil {
		return money.Money{}, fmt.Errorf("invalid amount %q", s)
	}
	return v, nil
}

// header maps normalised column names to their index.
type header map[string]int

func newHeader(cols []string) header {
	h := header{}
	for i, c := range cols {
		h[normalise(c)] = i
	}
	return h
}

func normalise(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "?", "")
	return strings.Join(strings.Fields(s), " ")
}

// get returns the value for the first matching column name, or "".
func (h header) get(row []string, names ...string) string {
	for _, n := range names {
		if i, ok := h[normalise(n)]; ok && i < len(row) {
			return strings.TrimSpace(row[i])
		}
	}
	return ""
}

// ParseInvoices reads Crunch-style invoice rows: Issue date, Client, Description,
// Invoice Total (gross when "Includes VAT?" is yes), Payment Date. VAT is extracted
// at the standard rate from the gross total.
func ParseInvoices(r io.Reader, cur money.Currency) ([]InvoiceRow, []RowError, error) {
	records, err := readCSV(r)
	if err != nil || len(records) == 0 {
		return nil, nil, err
	}
	h := newHeader(records[0])
	var out []InvoiceRow
	var skipped []RowError
	for i, row := range records[1:] {
		line := i + 2
		client := h.get(row, "Client", "Customer")
		totalStr := h.get(row, "Invoice Total", "Total", "Amount")
		if client == "" && totalStr == "" {
			continue // blank row
		}
		gross, err := money.Parse(cur, cleanNumber(totalStr))
		if err != nil {
			skipped = append(skipped, RowError{line, "invalid invoice total " + fmt.Sprintf("%q", totalStr)})
			continue
		}
		date, err := parseDate(h.get(row, "Issue date", "Date"))
		if err != nil {
			skipped = append(skipped, RowError{line, "invalid issue date"})
			continue
		}
		inc := yes(h.get(row, "Includes VAT", "Includes VAT?", "VAT"))
		net, vatAmt := gross, money.Zero(cur)
		if inc {
			net, vatAmt = extractVAT(gross, vat.Standard.Fraction)
		}
		inv := InvoiceRow{IssueDate: date, Client: client, Description: h.get(row, "Description"), Gross: gross, Net: net, VAT: vatAmt}
		if pd := h.get(row, "Payment Date", "Paid Date"); pd != "" {
			if d, err := parseDate(pd); err == nil {
				inv.Paid, inv.PaymentDate = true, d
			}
		}
		out = append(out, inv)
	}
	return out, skipped, nil
}

// ParseExpenses reads Crunch-style expense rows: Date, Supplier, Description, Amount.
// The amount is taken as the gross cost; VAT is extracted at the standard rate only
// when a "VAT" column marks the row as VAT-inclusive, otherwise the cost is treated
// as outside the scope of VAT (net = gross).
func ParseExpenses(r io.Reader, cur money.Currency) ([]ExpenseRow, []RowError, error) {
	records, err := readCSV(r)
	if err != nil || len(records) == 0 {
		return nil, nil, err
	}
	h := newHeader(records[0])
	var out []ExpenseRow
	var skipped []RowError
	for i, row := range records[1:] {
		line := i + 2
		supplier := h.get(row, "Supplier", "Payee")
		amountStr := h.get(row, "Amount", "Total", "Gross")
		if supplier == "" && amountStr == "" {
			continue
		}
		gross, err := money.Parse(cur, cleanNumber(amountStr))
		if err != nil {
			skipped = append(skipped, RowError{line, "invalid amount " + fmt.Sprintf("%q", amountStr)})
			continue
		}
		date, err := parseDate(h.get(row, "Date", "Posting date"))
		if err != nil {
			skipped = append(skipped, RowError{line, "invalid date"})
			continue
		}
		net, vatAmt := gross, money.Zero(cur)
		if yes(h.get(row, "Includes VAT", "VAT")) {
			net, vatAmt = extractVAT(gross, vat.Standard.Fraction)
		}
		out = append(out, ExpenseRow{Date: date, Supplier: supplier, Description: h.get(row, "Description"), Gross: gross, Net: net, VAT: vatAmt})
	}
	return out, skipped, nil
}

func readCSV(r io.Reader) ([][]string, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1 // tolerate ragged rows
	cr.TrimLeadingSpace = true
	return cr.ReadAll()
}

// extractVAT splits a VAT-inclusive gross into net and VAT for a rate: VAT is
// gross × rate/(1+rate), rounded to the penny half-up, and net = gross − VAT so the
// two always re-sum to the gross exactly.
func extractVAT(gross money.Money, rate decimal.Decimal) (net, vatAmt money.Money) {
	cur := gross.Currency()
	r := rate.Rat()
	denom := new(big.Rat).Add(big.NewRat(1, 1), r)
	frac := new(big.Rat).Quo(r, denom)
	vatRat := new(big.Rat).Mul(gross.Amount().Rat(), frac)
	vatAmt = money.FromRat(cur, vatRat, money.HalfUp)
	net, err := gross.Sub(vatAmt)
	if err != nil {
		return gross, money.Zero(cur)
	}
	return net, vatAmt
}

func cleanNumber(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "£")
	s = strings.ReplaceAll(s, ",", "")
	return s
}

func yes(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "yes", "y", "true", "1", "standard":
		return true
	}
	return false
}

// parseDate reads UK day-first dates (dd/mm/yyyy, dd/mm/yy) and ISO dates.
func parseDate(s string) (ledger.Date, error) {
	s = strings.TrimSpace(s)
	for _, layout := range []string{"02/01/2006", "2/1/2006", "02/01/06", "2006-01-02", "02-01-2006"} {
		if t, err := time.Parse(layout, s); err == nil {
			return ledger.NewDate(t.Year(), t.Month(), t.Day()), nil
		}
	}
	return ledger.Date{}, fmt.Errorf("unrecognised date %q", s)
}
