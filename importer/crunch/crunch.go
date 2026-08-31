// Package crunch reads the "Export - Complete" archive of the Crunch accounting
// platform: one .xls workbook per record type, each a header row and one row per
// record. It knows Crunch's column names and vocabulary and turns them into an
// importer.Batch; nothing about Crunch leaves this package.
//
// Two of Crunch's conventions shape the reading. Amounts on a client payment are
// in the currency paid, so a payment is matched to the customer's open invoices
// in currency units and posted at the invoice's value in the company currency.
// An expense is split across two workbooks — the totals and payment in
// "Expenses", the category and VAT rate per line in "Expense Line Items" — with
// no shared key, so lines are joined to their expense by date, description and
// gross amount. A director's salary is money the company owes the director:
// Crunch credits it to the director's account and the cash leaves later as a
// "Director withdrawal", so a salary is marked Owed and withdrawals settle it.
package crunch

import (
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"

	"github.com/richardjennings/accounts/importer"
	"github.com/richardjennings/accounts/ledger"
	"github.com/richardjennings/accounts/money"
	"github.com/richardjennings/decimal"
)

// Profile reads a Crunch export.
type Profile struct{}

func (Profile) Name() string { return "Crunch" }

// Workbook names in the export.
const (
	tClients      = "Clients"
	tSuppliers    = "Suppliers"
	tInvoices     = "Sales invoices"
	tCreditNotes  = "Sales invoice credit notes"
	tPayments     = "Client payments"
	tExpenses     = "Expenses"
	tLineItems    = "Expense Line Items"
	tTransfers    = "Money transfers"
	tDeposits     = "Bank deposits"
	tInterest     = "Interest received"
	tDirectorPay  = "Director salaries"
	tEmployeePay  = "Employee salaries"
	tPayrollRuns  = "Payroll runs"
	tDividends    = "Dividends"
	tWithdrawals  = "Director withdrawals"
	tTaxPayments  = "Tax payments"
	tTaxRebates   = "Tax rebates"
	tRefunds      = "Client refunds"
	tIncentives   = "Incentive payments"
	tLoans        = "Loan repayments"
	tP35          = "P35 PAYE documents"
	tVATReturns   = "VAT returns"
	pettyCashName = "Company Petty Cash"
	none          = "None"
)

type reader struct {
	src    importer.Source
	cur    money.Currency
	b      *importer.Batch
	issues []importer.Issue
	banks  map[string]bool
	// invoices by ref, in issue order, for matching credit notes and receipts
	invoices []*openInvoice
	byRef    map[string]*openInvoice
	// default expense category per supplier
	supplierCategory map[string]string
	// expense line items by date and description, consumed as expenses claim them
	lines map[string][]*lineItem
}

// openInvoice tracks how much of an invoice is still unpaid, in the currency it
// was issued in and in the company currency.
type openInvoice struct {
	ref, customer string
	date          ledger.Date
	foreign       bool        // issued in another currency
	ccyCode       string      // ISO code of that currency, when known
	ccyGross      *big.Rat    // gross in the invoice currency
	gross         money.Money // gross in the company currency
	openCcy       *big.Rat
	openGross     money.Money
}

// Read reads the export.
func (Profile) Read(src importer.Source, cur money.Currency) (*importer.Batch, []importer.Issue, error) {
	r := &reader{src: src, cur: cur, b: &importer.Batch{}, banks: map[string]bool{}, byRef: map[string]*openInvoice{}, supplierCategory: map[string]string{}}
	if _, ok := src.Table(tInvoices); !ok {
		if _, ok := src.Table(tExpenses); !ok {
			return nil, nil, fmt.Errorf("crunch: neither %q nor %q found; is this a Crunch export?", tInvoices, tExpenses)
		}
	}
	r.parties()
	r.invoicesAndCredits()
	sort.SliceStable(r.invoices, func(i, j int) bool { return r.invoices[i].date.Before(r.invoices[j].date) })
	r.receipts()
	r.expenses()
	r.transfers()
	r.deposits()
	r.interest()
	r.salaries()
	r.dividends()
	r.withdrawals()
	r.tax()
	r.unsupported()
	sort.SliceStable(r.b.Invoices, func(i, j int) bool { return r.b.Invoices[i].Date.Before(r.b.Invoices[j].Date) })
	sort.SliceStable(r.b.Bills, func(i, j int) bool { return r.b.Bills[i].Date.Before(r.b.Bills[j].Date) })
	sort.SliceStable(r.b.Transfers, func(i, j int) bool { return r.b.Transfers[i].Date.Before(r.b.Transfers[j].Date) })
	for name := range r.banks {
		r.b.Banks = append(r.b.Banks, name)
		// Crunch account names carry the currency ("USD Wise"); the company
		// currency and unknown codes mean a company-currency account.
		for _, tok := range strings.Fields(name) {
			code := currencyCode(tok)
			if _, ok := money.Lookup(code); ok && code != cur.Code {
				if r.b.BankCurrency == nil {
					r.b.BankCurrency = map[string]string{}
				}
				r.b.BankCurrency[name] = code
			}
		}
	}
	sort.Strings(r.b.Banks)
	return r.b, r.issues, nil
}

func (r *reader) issue(table string, row int, format string, args ...any) {
	r.issues = append(r.issues, importer.Issue{Table: table, Row: row, Msg: fmt.Sprintf(format, args...)})
}

// table returns a table and checks its columns; a missing table is not an
// error (an export omits nothing, but a partial one may).
func (r *reader) table(name string, cols ...string) (*importer.Table, bool) {
	t, ok := r.src.Table(name)
	if !ok {
		return nil, false
	}
	if err := t.Require(cols...); err != nil {
		r.issue(name, 0, "%v; skipped", err)
		return nil, false
	}
	return t, true
}

// bank records a bank account name from the export. Petty cash and the "None"
// placeholder are not bank accounts: they map to "".
func (r *reader) bank(name string) string {
	name = strings.TrimSpace(name)
	if name == "" || name == none || name == pettyCashName {
		return ""
	}
	r.banks[name] = true
	return name
}

func (r *reader) parties() {
	if t, ok := r.table(tClients, "Company name"); ok {
		t.Each(func(row importer.Row) {
			if n := row.Text("Company name"); n != "" {
				r.b.Customers = append(r.b.Customers, importer.Party{Name: n, Address: row.Text("Billing address"), VATNumber: row.Text("VAT registration number")})
			}
		})
	}
	if t, ok := r.table(tSuppliers, "Company name"); ok {
		t.Each(func(row importer.Row) {
			n := row.Text("Company name")
			if n == "" {
				return
			}
			r.b.Suppliers = append(r.b.Suppliers, importer.Party{Name: n, Address: row.Text("Billing address"), VATNumber: row.Text("VAT registration number")})
			if c := row.Text("Default expense type"); c != "" && c != none {
				r.supplierCategory[n] = c
			}
		})
	}
}

func (r *reader) invoicesAndCredits() {
	t, ok := r.table(tInvoices, "Issued date", "Client", "Invoice # - Ref", "Net amount", "VAT amount charged", "Gross amount")
	if !ok {
		return
	}
	t.Each(func(row importer.Row) {
		date, err := row.Date("Issued date")
		if err != nil {
			r.issue(tInvoices, row.N(), "%v; skipped", err)
			return
		}
		net, err1 := row.Money(r.cur, "Net amount")
		vat, err2 := row.Money(r.cur, "VAT amount charged")
		gross, err3 := row.Money(r.cur, "Gross amount")
		if err := firstErr(err1, err2, err3); err != nil {
			r.issue(tInvoices, row.N(), "%v; skipped", err)
			return
		}
		ref, customer := row.Text("Invoice # - Ref"), row.Text("Client")
		inv := importer.Invoice{Date: date, Ref: ref, Customer: customer}
		line := importer.InvoiceLine{Description: "Invoice", Net: net, VAT: vat, VATRate: rateOf(net, vat)}
		if vat.IsPositive() {
			r.b.VATCharged = true
		}
		oi := &openInvoice{ref: ref, customer: customer, date: date, gross: gross, openGross: gross, ccyGross: gross.Amount().Rat(), openCcy: gross.Amount().Rat()}
		if ccy := row.Text("Currency"); ccy != "" && ccy != r.cur.Code && ccy != "£" {
			if cg, err := row.Money(r.cur, "Currency gross amount"); err == nil && cg.IsPositive() {
				oi.foreign, oi.ccyGross, oi.openCcy = true, cg.Amount().Rat(), cg.Amount().Rat()
				oi.ccyCode = currencyCode(ccy)
				inv.Memo = fmt.Sprintf("issued in %s: %s gross", ccy, cg.Amount().String())
				line.Description = "Invoiced in " + ccy + ": " + ccy + " " + cg.Amount().String() + " gross"
			}
		}
		inv.Lines = []importer.InvoiceLine{line}
		r.b.Invoices = append(r.b.Invoices, inv)
		r.invoices = append(r.invoices, oi)
		r.byRef[ref] = oi
	})
	if t, ok := r.table(tCreditNotes, "Date", "Credit note number", "Sales invoice number", "Gross amount"); ok {
		t.Each(func(row importer.Row) {
			date, err := row.Date("Date")
			if err != nil {
				r.issue(tCreditNotes, row.N(), "%v; skipped", err)
				return
			}
			amount, err := row.Money(r.cur, "Gross amount")
			if err != nil {
				r.issue(tCreditNotes, row.N(), "%v; skipped", err)
				return
			}
			cn := importer.CreditNote{Date: date, Ref: row.Text("Credit note number"), Invoice: row.Text("Sales invoice number"), Gross: amount}
			if oi, ok := r.byRef[cn.Invoice]; ok {
				// The gross may be in the invoice currency or the company currency; a
				// credit for the whole open amount settles it in both units.
				switch {
				case ratEq(amount.Amount().Rat(), oi.openCcy) || amount.Equal(oi.openGross):
					cn.Gross = oi.openGross
					r.settle(oi, oi.openGross, oi.openCcy)
				default:
					units := new(big.Rat).Mul(amount.Amount().Rat(), new(big.Rat).Quo(oi.ccyGross, oi.gross.Amount().Rat()))
					r.settle(oi, amount, units)
				}
			} else {
				r.issue(tCreditNotes, row.N(), "credit note %s refers to unknown invoice %q; posted against sales without allocation", cn.Ref, cn.Invoice)
			}
			r.b.CreditNotes = append(r.b.CreditNotes, cn)
		})
	}
}

// receipts matches each client payment to the customer's open invoices, oldest
// first, in the units the payment is expressed in.
func (r *reader) receipts() {
	t, ok := r.table(tPayments, "Date", "Client", "Amount")
	if !ok {
		return
	}
	type pay struct {
		row  importer.Row
		date ledger.Date
	}
	var pays []pay
	t.Each(func(row importer.Row) {
		date, err := row.Date("Date")
		if err != nil {
			r.issue(tPayments, row.N(), "%v; skipped", err)
			return
		}
		pays = append(pays, pay{row, date})
	})
	sort.SliceStable(pays, func(i, j int) bool { return pays[i].date.Before(pays[j].date) })
	for _, p := range pays {
		row := p.row
		amount, err := row.Money(r.cur, "Amount")
		if err != nil {
			r.issue(tPayments, row.N(), "%v; skipped", err)
			continue
		}
		customer := row.Text("Client")
		bank := r.bank(row.Text("Payment account"))
		if strings.Contains(strings.ToLower(row.Text("Payment method")), "petty cash") {
			bank = ""
		}
		remaining := new(big.Rat).Set(amount.Amount().Rat())
		var memo []string
		// A payment equal to what one invoice still owes settles that invoice,
		// whatever its age; otherwise the oldest open invoices are settled first.
		order := r.openInvoices(customer)
		for i, oi := range order {
			if ratEq(remaining, oi.openCcy) || oi.openGross.Equal(amount) {
				order = append([]*openInvoice{oi}, append(order[:i:i], order[i+1:]...)...)
				break
			}
		}
		for _, oi := range order {
			if remaining.Sign() <= 0 {
				break
			}
			// Take the invoice in full when the payment covers it, else part of it.
			var settledGross money.Money
			var units *big.Rat
			if remaining.Cmp(oi.openCcy) >= 0 {
				settledGross, units = oi.openGross, new(big.Rat).Set(oi.openCcy)
			} else {
				units = new(big.Rat).Set(remaining)
				rate := new(big.Rat).Quo(oi.gross.Amount().Rat(), oi.ccyGross)
				settledGross = money.FromRat(r.cur, new(big.Rat).Mul(units, rate), money.HalfUp)
				if c, _ := settledGross.Cmp(oi.openGross); c > 0 {
					settledGross = oi.openGross
				}
			}
			r.settle(oi, settledGross, units)
			remaining.Sub(remaining, units)
			if !settledGross.IsPositive() {
				continue // too small to be a penny in the company currency
			}
			rec := importer.Receipt{Date: p.date, Ref: row.Text("Ref"), Customer: customer, Invoice: oi.ref, Bank: bank, Amount: settledGross,
				Memo: fmt.Sprintf("%s of the payment against %s", ratText(units), oi.ref)}
			if oi.foreign && oi.ccyCode != "" {
				if cur, ok := money.Lookup(oi.ccyCode); ok {
					rec.CcyAmount = money.FromRat(cur, units, money.HalfUp)
				}
			}
			r.b.Receipts = append(r.b.Receipts, rec)
			memo = append(memo, oi.ref)
		}
		if remaining.Sign() > 0 {
			left := money.FromRat(r.cur, remaining, money.HalfUp)
			if len(memo) == 0 {
				r.issue(tPayments, row.N(), "no open invoice for %s matches %s; posted as an unallocated receipt at face value", customer, left)
			} else {
				r.issue(tPayments, row.N(), "%s of the payment exceeds the open invoices of %s; posted unallocated at face value", left, customer)
			}
			r.b.Receipts = append(r.b.Receipts, importer.Receipt{Date: p.date, Ref: row.Text("Ref"), Customer: customer, Bank: bank, Amount: left, Memo: "unallocated"})
		}
	}
}

// openInvoices lists a customer's invoices that still owe something, oldest first.
func (r *reader) openInvoices(customer string) []*openInvoice {
	var out []*openInvoice
	for _, oi := range r.invoices {
		if oi.customer == customer && oi.openCcy.Sign() > 0 && oi.openGross.IsPositive() {
			out = append(out, oi)
		}
	}
	return out
}

func (r *reader) settle(oi *openInvoice, gross money.Money, units *big.Rat) {
	if g, err := oi.openGross.Sub(gross); err == nil {
		oi.openGross = g
	}
	oi.openCcy = new(big.Rat).Sub(oi.openCcy, units)
}

// lineItem is one row of "Expense Line Items".
type lineItem struct {
	row      int
	category string
	rate     decimal.Decimal
	desc     string
	gross    money.Money
	used     bool
}

func (r *reader) expenses() {
	t, ok := r.table(tExpenses, "Date", "Supplier - Ref", "Net amount", "VAT amount", "Gross amount")
	if !ok {
		return
	}
	// Index the line items by date and description.
	lines := map[string][]*lineItem{}
	r.lines = lines
	if lt, ok := r.table(tLineItems, "Invoice Date", "Expense Type", "VAT", "Description", "Gross"); ok {
		lt.Each(func(row importer.Row) {
			date, err1 := row.Date("Invoice Date")
			gross, err2 := row.Money(r.cur, "Gross")
			if err := firstErr(err1, err2); err != nil {
				r.issue(tLineItems, row.N(), "%v; skipped", err)
				return
			}
			key := date.String() + "|" + row.Text("Description")
			lines[key] = append(lines[key], &lineItem{row: row.N(), category: row.Text("Expense Type"), rate: vatRate(row.Text("VAT")), desc: row.Text("Description"), gross: gross})
		})
	}
	t.Each(func(row importer.Row) {
		date, err := row.Date("Date")
		if err != nil {
			r.issue(tExpenses, row.N(), "%v; skipped", err)
			return
		}
		net, err1 := row.Money(r.cur, "Net amount")
		vat, err2 := row.Money(r.cur, "VAT amount")
		gross, err3 := row.Money(r.cur, "Gross amount")
		if err := firstErr(err1, err2, err3); err != nil {
			r.issue(tExpenses, row.N(), "%v; skipped", err)
			return
		}
		supplier := row.Text("Supplier - Ref")
		desc := row.Text("Line item(s) description")
		recharge := row.Text("Recharge to")
		if recharge == none {
			recharge = ""
		}
		paid, _ := row.Money(r.cur, "Payment(s) amount")
		credited, _ := row.Money(r.cur, "Credit note(s) amount")
		paidBy := importer.Bank
		switch method := strings.ToLower(row.Text("Payment method(s)")); {
		case strings.Contains(method, "director"):
			paidBy = importer.Director
		case strings.Contains(method, "petty cash"):
			paidBy = importer.PettyCash
		}
		if !paid.IsPositive() {
			paidBy = importer.Unpaid
		}
		bills := r.splitExpense(row.N(), date, desc, gross, vat, net)
		if len(bills) == 0 {
			bills = []importer.Bill{{Description: desc, Category: r.supplierCategory[supplier], Net: net, VAT: vat}}
		}
		// The payment and any credit settle the lines in order.
		leftPaid, leftCredit := paid, credited
		for i := range bills {
			b := &bills[i]
			b.Date, b.Supplier, b.Recharge, b.PaidBy = date, supplier, recharge, paidBy
			lineGross, _ := b.Net.Add(b.VAT)
			b.Credited, leftCredit = take(leftCredit, lineGross)
			owed, _ := lineGross.Sub(b.Credited)
			b.Paid, leftPaid = take(leftPaid, owed)
			if !b.Paid.IsPositive() {
				b.PaidBy = importer.Unpaid
			}
			r.b.Bills = append(r.b.Bills, *b)
		}
	})
}

// splitExpense finds the line items of an expense and splits its gross among
// them: each line's net is its gross less VAT at the line's rate, and the VAT
// total is reconciled to the expense's own VAT amount on the last VAT-bearing
// line. It returns nil when no line items match.
func (r *reader) splitExpense(rowN int, date ledger.Date, desc string, gross, vat, net money.Money) []importer.Bill {
	key := date.String() + "|" + desc
	var free []*lineItem
	for _, li := range r.lineIndex(key) {
		if !li.used {
			free = append(free, li)
		}
	}
	var chosen []*lineItem
	for _, li := range free {
		if li.gross.Equal(gross) {
			chosen = []*lineItem{li}
			break
		}
	}
	if chosen == nil && len(free) > 1 {
		sum := money.Zero(r.cur)
		for _, li := range free {
			sum, _ = sum.Add(li.gross)
		}
		if sum.Equal(gross) {
			chosen = free
		}
	}
	if chosen == nil {
		if desc != "" {
			r.issue(tExpenses, rowN, "no line item matches %q %s on %s; category unknown", desc, gross, date)
		}
		return nil
	}
	var bills []importer.Bill
	vatSum := money.Zero(r.cur)
	lastVAT := -1
	for i, li := range chosen {
		li.used = true
		lineNet := li.gross
		lineVAT := money.Zero(r.cur)
		if li.rate.Rat().Sign() > 0 {
			one := big.NewRat(1, 1)
			lineNet = money.FromRat(r.cur, new(big.Rat).Quo(li.gross.Amount().Rat(), new(big.Rat).Add(one, li.rate.Rat())), money.HalfUp)
			lineVAT, _ = li.gross.Sub(lineNet)
			lastVAT = i
		}
		vatSum, _ = vatSum.Add(lineVAT)
		bills = append(bills, importer.Bill{Description: li.desc, Category: li.category, Net: lineNet, VAT: lineVAT})
	}
	if diff, err := vat.Sub(vatSum); err == nil && !diff.IsZero() {
		if lastVAT < 0 {
			lastVAT = len(bills) - 1
		}
		bills[lastVAT].VAT, _ = bills[lastVAT].VAT.Add(diff)
		bills[lastVAT].Net, _ = bills[lastVAT].Net.Sub(diff)
	}
	return bills
}

// lineIndex is the line items under a key; kept as a method so expenses() can
// build the index once.
func (r *reader) lineIndex(key string) []*lineItem { return r.lines[key] }

func (r *reader) transfers() {
	t, ok := r.table(tTransfers, "Date", "Source", "Destination", "Amount")
	if !ok {
		return
	}
	t.Each(func(row importer.Row) {
		date, err1 := row.Date("Date")
		amount, err2 := row.Money(r.cur, "Amount")
		if err := firstErr(err1, err2); err != nil {
			r.issue(tTransfers, row.N(), "%v; skipped", err)
			return
		}
		r.b.Transfers = append(r.b.Transfers, importer.Transfer{Date: date, Ref: row.Text("Reference"), From: r.bank(row.Text("Source")), To: r.bank(row.Text("Destination")), Amount: amount})
	})
}

func (r *reader) deposits() {
	t, ok := r.table(tDeposits, "Date", "Payment account")
	if !ok {
		return
	}
	t.Each(func(row importer.Row) {
		date, err := row.Date("Date")
		if err != nil {
			r.issue(tDeposits, row.N(), "%v; skipped", err)
			return
		}
		bank := r.bank(row.Text("Payment account"))
		if m, err := row.Money(r.cur, "Petty cash amount"); err == nil && m.IsPositive() {
			r.b.Transfers = append(r.b.Transfers, importer.Transfer{Date: date, Ref: row.Text("Memo"), From: "", To: bank, Amount: m})
		}
		if m, err := row.Money(r.cur, "Director deposit amount"); err == nil && m.IsPositive() {
			r.b.Introduced = append(r.b.Introduced, importer.Introduced{Date: date, Bank: bank, Amount: m})
		}
		if m, err := row.Money(r.cur, "Company cheque amount"); err == nil && m.IsPositive() {
			r.issue(tDeposits, row.N(), "company cheque of %s not imported: its origin is not in the export", m)
		}
	})
}

func (r *reader) interest() {
	t, ok := r.table(tInterest, "Date", "Payment account", "Amount")
	if !ok {
		return
	}
	t.Each(func(row importer.Row) {
		date, err1 := row.Date("Date")
		amount, err2 := row.Money(r.cur, "Amount")
		if err := firstErr(err1, err2); err != nil {
			r.issue(tInterest, row.N(), "%v; skipped", err)
			return
		}
		r.b.Interest = append(r.b.Interest, importer.Interest{Date: date, Bank: r.bank(row.Text("Payment account")), Amount: amount})
	})
}

func (r *reader) salaries() {
	if t, ok := r.table(tDirectorPay, "Date", "Director", "Amount"); ok {
		t.Each(func(row importer.Row) {
			date, err1 := row.Date("Date")
			amount, err2 := row.Money(r.cur, "Amount")
			if err := firstErr(err1, err2); err != nil {
				r.issue(tDirectorPay, row.N(), "%v; skipped", err)
				return
			}
			if !amount.IsPositive() {
				return // a month with no pay
			}
			r.b.Salaries = append(r.b.Salaries, importer.Salary{Date: date, Person: row.Text("Director"), Gross: amount, TaxNIC: money.Zero(r.cur), EmployerNIC: money.Zero(r.cur), Owed: true})
		})
	}
	if t, ok := r.table(tEmployeePay, "Date", "Employee", "Gross Amount"); ok {
		t.Each(func(row importer.Row) {
			date, err1 := row.Date("Date")
			amount, err2 := row.Money(r.cur, "Gross Amount")
			if err := firstErr(err1, err2); err != nil {
				r.issue(tEmployeePay, row.N(), "%v; skipped", err)
				return
			}
			if !amount.IsPositive() {
				return
			}
			r.b.Salaries = append(r.b.Salaries, importer.Salary{Date: date, Person: row.Text("Employee"), Gross: amount, TaxNIC: money.Zero(r.cur), EmployerNIC: money.Zero(r.cur)})
		})
	}
	if t, ok := r.table(tPayrollRuns, "Date", "Total"); ok {
		t.Each(func(row importer.Row) {
			if m, err := row.Money(r.cur, "Total"); err == nil && m.IsPositive() {
				r.issue(tPayrollRuns, row.N(), "payroll run total %s not posted: the export does not say how it splits into tax and NI", m)
			}
		})
	}
}

func (r *reader) dividends() {
	t, ok := r.table(tDividends, "Date", "Net amount")
	if !ok {
		return
	}
	t.Each(func(row importer.Row) {
		date, err1 := row.Date("Date")
		amount, err2 := row.Money(r.cur, "Net amount")
		if err := firstErr(err1, err2); err != nil {
			r.issue(tDividends, row.N(), "%v; skipped", err)
			return
		}
		r.b.Dividends = append(r.b.Dividends, importer.Dividend{Date: date, Amount: amount})
	})
}

func (r *reader) withdrawals() {
	t, ok := r.table(tWithdrawals, "Date", "Director", "Amount")
	if !ok {
		return
	}
	t.Each(func(row importer.Row) {
		date, err1 := row.Date("Date")
		amount, err2 := row.Money(r.cur, "Amount")
		if err := firstErr(err1, err2); err != nil {
			r.issue(tWithdrawals, row.N(), "%v; skipped", err)
			return
		}
		r.b.Drawings = append(r.b.Drawings, importer.Drawing{Date: date, Person: row.Text("Director"), Bank: r.bank(row.Text("Payment account")), Amount: amount})
	})
}

func (r *reader) tax() {
	if t, ok := r.table(tTaxPayments, "Date", "Payment type", "Amount"); ok {
		t.Each(func(row importer.Row) {
			date, err1 := row.Date("Date")
			amount, err2 := row.Money(r.cur, "Amount")
			if err := firstErr(err1, err2); err != nil {
				r.issue(tTaxPayments, row.N(), "%v; skipped", err)
				return
			}
			r.b.TaxPayments = append(r.b.TaxPayments, importer.TaxPayment{Date: date, Kind: taxKind(row.Text("Payment type")), Bank: r.bank(row.Text("Payment account")), Amount: amount})
		})
	}
	if t, ok := r.table(tTaxRebates, "Date", "Payment type", "Amount"); ok {
		t.Each(func(row importer.Row) {
			date, err1 := row.Date("Date")
			amount, err2 := row.Money(r.cur, "Amount")
			if err := firstErr(err1, err2); err != nil {
				r.issue(tTaxRebates, row.N(), "%v; skipped", err)
				return
			}
			toDirector := strings.Contains(strings.ToLower(row.Text("Payment method")), "director")
			r.b.TaxRebates = append(r.b.TaxRebates, importer.TaxRebate{Date: date, Kind: taxKind(row.Text("Payment type")), Bank: r.bank(row.Text("Payment account")), ToDirector: toDirector, Amount: amount})
		})
	}
}

// unsupported notes the record types this profile does not import, when they
// hold rows.
func (r *reader) unsupported() {
	for _, name := range []string{tRefunds, tIncentives, tLoans, tP35, tVATReturns} {
		if t, ok := r.src.Table(name); ok && len(t.Rows) > 0 {
			r.issue(name, 0, "%d row(s) not imported: not supported yet", len(t.Rows))
		}
	}
}

// ratioContext divides to 12 significant digits, enough for a VAT rate.
var ratioContext = decimal.Context{Precision: 12, Rounding: decimal.RoundHalfEven}

// vatRate reads Crunch's VAT rate names: "Standard - 20%", "Reduced - 5%",
// "Zero", "Exempt", "Outside the Scope".
func vatRate(s string) decimal.Decimal {
	if i := strings.Index(s, "%"); i > 0 {
		j := i
		for j > 0 && (s[j-1] >= '0' && s[j-1] <= '9' || s[j-1] == '.') {
			j--
		}
		if pct, err := decimal.NewFromString(s[j:i]); err == nil {
			r, _ := ratioContext.Divide(pct, decimal.MustParse("100"))
			return r
		}
	}
	return decimal.MustParse("0")
}

// rateOf derives a VAT rate from amounts, snapping to a UK rate when the
// amounts agree with one to the penny.
func rateOf(net, vat money.Money) decimal.Decimal {
	if !vat.IsPositive() || !net.IsPositive() {
		return decimal.MustParse("0")
	}
	for _, rate := range []string{"0.20", "0.05"} {
		d := decimal.MustParse(rate)
		if v, err := net.Mul(d, money.HalfUp); err == nil && v.Equal(vat) {
			return d
		}
	}
	q, _ := ratioContext.Divide(vat.Amount(), net.Amount())
	return q
}

// currencyCode reads a currency symbol or code as an ISO code, or "".
func currencyCode(s string) string {
	switch s = strings.TrimSpace(s); s {
	case "$":
		return "USD"
	case "€":
		return "EUR"
	case "£":
		return "GBP"
	}
	if len(s) == 3 && strings.ToUpper(s) == s {
		return s
	}
	return ""
}

func taxKind(s string) importer.TaxKind {
	switch s = strings.ToLower(s); {
	case strings.Contains(s, "corporation"):
		return importer.CorporationTax
	case strings.Contains(s, "paye"), strings.Contains(s, "nic"):
		return importer.PAYE
	case strings.Contains(s, "vat"):
		return importer.VATTax
	}
	return importer.OtherTax
}

// take returns min(avail, want) and what is left of avail.
func take(avail, want money.Money) (taken, left money.Money) {
	if c, _ := avail.Cmp(want); c <= 0 {
		return avail, money.Zero(avail.Currency())
	}
	left, _ = avail.Sub(want)
	return want, left
}

func firstErr(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}

func ratEq(a, b *big.Rat) bool { return a.Cmp(b) == 0 }

func ratText(r *big.Rat) string {
	f, _ := r.Float64()
	return strconv.FormatFloat(f, 'f', -1, 64)
}
