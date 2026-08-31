// Command web serves a local, self-contained UI for the accounts engine. It is
// organised as the product is: a left-hand menu of the sections — Company, Sales,
// Expenses, Banking, Pay Yourself, Company Tax and Accounting — each expanding to
// its own sub-sections. The books belong to a Company with a real financial year;
// every transaction is dated within it. One in-memory company; no external
// integrations.
package main

import (
	"bytes"
	"embed"
	"flag"
	"fmt"
	"html/template"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/richardjennings/accounts/chart"
	"github.com/richardjennings/accounts/company"
	"github.com/richardjennings/accounts/csvimport"
	"github.com/richardjennings/accounts/dividends"
	"github.com/richardjennings/accounts/explain"
	"github.com/richardjennings/accounts/fixedassets"
	"github.com/richardjennings/accounts/frs105"
	"github.com/richardjennings/accounts/ledger"
	"github.com/richardjennings/accounts/mileage"
	"github.com/richardjennings/accounts/money"
	"github.com/richardjennings/accounts/purchaseledger"
	"github.com/richardjennings/accounts/register"
	"github.com/richardjennings/accounts/report"
	"github.com/richardjennings/accounts/salesledger"
	"github.com/richardjennings/accounts/tax/capitalallowances"
	"github.com/richardjennings/accounts/tax/corporationtax"
	"github.com/richardjennings/accounts/tax/payroll"
	"github.com/richardjennings/accounts/tax/vat"
	"github.com/richardjennings/accounts/themes"
	"github.com/richardjennings/accounts/themes/adjustments"
	"github.com/richardjennings/accounts/themes/banking"
	"github.com/richardjennings/accounts/themes/capital"
	"github.com/richardjennings/accounts/themes/companytax"
	"github.com/richardjennings/accounts/themes/expenses"
	"github.com/richardjennings/accounts/themes/payyourself"
	"github.com/richardjennings/accounts/themes/sales"
	"github.com/richardjennings/accounts/vatreturn"
	"github.com/richardjennings/accounts/yearend"

	"github.com/richardjennings/decimal"
)

//go:embed templates/app.html
var templatesFS embed.FS

// --- navigation model (drives the sidebar) ---

type navItem struct{ ID, Label, Href string }
type navSection struct {
	ID, Label, Href string
	Items           []navItem
}

var nav = []navSection{
	{ID: "overview", Label: "Overview", Href: "/"},
	{ID: "learn", Label: "Learn", Href: "/learn"},
	{ID: "company", Label: "Company", Href: "/company", Items: []navItem{
		{"company", "Details", "/company"},
		{"company.year", "Financial year", "/company/financial-year"},
		{"company.people", "Directors & shares", "/company/people"},
		{"company.import", "Import (CSV)", "/company/import"},
	}},
	{ID: "sales", Label: "Sales", Href: "/sales", Items: []navItem{
		{"sales", "Invoices", "/sales"},
		{"sales.receipts", "Receipts", "/sales/receipts"},
		{"sales.cash", "Cash sales", "/sales/cash"},
		{"sales.credit", "Credit notes", "/sales/credit-notes"},
	}},
	{ID: "expenses", Label: "Expenses", Href: "/expenses", Items: []navItem{
		{"expenses", "Bills", "/expenses"},
		{"expenses.payments", "Payments", "/expenses/payments"},
		{"expenses.direct", "Direct expenses", "/expenses/direct"},
		{"expenses.credit", "Credit notes", "/expenses/credit-notes"},
		{"expenses.mileage", "Mileage", "/expenses/mileage"},
	}},
	{ID: "banking", Label: "Banking", Href: "/banking", Items: []navItem{
		{"banking", "Accounts", "/banking"},
		{"banking.transfers", "Transfers", "/banking/transfers"},
		{"banking.interest", "Interest & charges", "/banking/interest"},
		{"banking.reconcile", "Reconcile", "/banking/reconcile"},
	}},
	{ID: "pay-yourself", Label: "Pay Yourself", Href: "/pay-yourself", Items: []navItem{
		{"pay-yourself", "Salary", "/pay-yourself"},
		{"pay-yourself.employees", "Employees", "/pay-yourself/employees"},
		{"pay-yourself.dividends", "Dividends", "/pay-yourself/dividends"},
		{"pay-yourself.loan", "Director's loan", "/pay-yourself/loan"},
	}},
	{ID: "company-tax", Label: "Company Tax", Href: "/company-tax", Items: []navItem{
		{"company-tax", "Corporation tax", "/company-tax"},
		{"company-tax.vat", "VAT return", "/company-tax/vat"},
	}},
	{ID: "accounting", Label: "Accounting", Href: "/accounting", Items: []navItem{
		{"accounting", "Trial balance", "/accounting"},
		{"accounting.pl", "Profit & Loss", "/accounting/profit-loss"},
		{"accounting.bs", "Balance sheet", "/accounting/balance-sheet"},
		{"accounting.chart", "Chart of accounts", "/accounting/chart"},
		{"accounting.journals", "Journals", "/accounting/journals"},
		{"accounting.assets", "Fixed assets", "/accounting/fixed-assets"},
		{"accounting.adjust", "Adjustments", "/accounting/adjustments"},
		{"accounting.accounts", "Year-end accounts", "/accounting/accounts"},
	}},
}

func sectionOf(page string) string { return strings.SplitN(page, ".", 2)[0] }

// --- app state ---

type journalOp struct {
	j   ledger.Journal
	err error
}

func (o journalOp) Journal() (ledger.Journal, error) { return o.j, o.err }

type entry struct {
	section   string
	j         ledger.Journal
	principle string // op-level accounting principle (for the learner), when known
}

// assetHolding is one fixed asset in the register with the depreciation posted so far.
type assetHolding struct {
	Asset       fixedassets.Asset
	Accumulated money.Money
}

// employee is one person on the payroll.
type employee struct {
	Name        string
	TaxCode     string
	StudentLoan string // payroll student-loan plan name, or "" for none
	Salary      money.Money
	BIK         money.Money // annual benefits in kind (P11D value)
	AutoEnrol   bool        // enrolled in the workplace pension
}

// bankAcct is a bank account the company holds; cash moves through one of these.
type bankAcct struct{ Code, Name string }

func defaultBanks() []bankAcct { return []bankAcct{{chart.Bank, "Bank current account"}} }

// invoiceDoc is the itemised detail behind a raised invoice — its lines and totals
// — kept so the invoice can be listed and printed. The sales ledger holds only the
// gross total that makes up the trade-debtors control account.
type invoiceDoc struct {
	Ref, Customer   string
	Date            ledger.Date
	Lines           []sales.InvoiceLine
	Net, VAT, Gross money.Money
}

// costRecord is a recorded cost (a supplier bill or a direct expense) that the
// company might later recover from a customer. Recharging one links it to the
// invoice that recovered it, so the recharge reconciles to a real expense rather
// than to a hopeful free-text description.
type costRecord struct {
	Ref, Desc   string
	Date        ledger.Date
	Net         money.Money // the recoverable (VAT-exclusive) amount
	Recharged   bool
	RechargedOn string // invoice ref that recovered it
}

// stmtLine is one imported bank-statement line, tied to a bank account, that the user
// ticks off during reconciliation.
type stmtLine struct {
	BankCode   string
	Date       ledger.Date
	Desc       string
	Amount     money.Money // signed: in positive, out negative
	Balance    money.Money
	HasBalance bool
	Reconciled bool
}

type app struct {
	mu            sync.Mutex
	co            company.Company
	today         ledger.Date
	book          *ledger.Book
	sl            *salesledger.Ledger
	purch         *purchaseledger.Ledger
	assets        []*assetHolding
	employees     []*employee
	banks         []bankAcct
	mainBank      string // code of the primary bank account; the default for cash flows
	reg           register.Register
	invoiceDocs   map[string]*invoiceDoc
	invoiceOrder  []string
	costs         []*costRecord
	stmtLines     []*stmtLine
	entries       []entry
	seq           int
	flash         string
	lastPayroll   *payroll.Result
	lastDividend  *dividendRun
	closedThrough ledger.Date // periods on/before this date are closed (locked)
	lastImport    *importReport
	dataPath      string // save file; empty = in-memory only
	tmpl          *template.Template
}

// inClosedPeriod reports whether a date falls in a locked (closed) accounting period
// — i.e. on or before the closed-through date.
func (a *app) inClosedPeriod(d ledger.Date) bool {
	return !a.closedThrough.IsZero() && !a.closedThrough.Before(d)
}

// dividendRun is the most recent dividend declaration, kept for the voucher display.
type dividendRun struct {
	Ref      string
	Date     ledger.Date
	Total    money.Money
	PerShare string
	Awards   []register.Award
}

// defaultRegister is the starter statutory register: one director who is also the
// sole shareholder, holding 100 ordinary £1 shares.
func defaultRegister(cur money.Currency, inc ledger.Date) register.Register {
	nominal, _ := money.Parse(cur, "1.00")
	return register.Register{
		Officers: []register.Officer{{Name: "Alex Director", Role: register.Director, Appointed: inc}},
		Members:  []register.Member{{Name: "Alex Director", Class: "Ordinary", Shares: 100, Since: inc}},
		Nominal:  nominal,
	}
}

func newApp(dataPath string) (*app, error) {
	co := company.Default()
	book, err := chart.NewUKMicroLtdBook(co.Currency)
	if err != nil {
		return nil, err
	}
	tmpl, err := template.New("app").Funcs(template.FuncMap{
		"money": fmtMoney,
		"pos":   func(m money.Money) bool { return m.IsPositive() },
		"seq": func(n int) []int {
			s := make([]int, n)
			for i := range s {
				s[i] = i
			}
			return s
		},
		"pct": func(part, total int) string {
			if total == 0 {
				return "—"
			}
			return strconv.FormatFloat(float64(part)*100/float64(total), 'f', 1, 64) + "%"
		},
	}).ParseFS(templatesFS, "templates/app.html")
	if err != nil {
		return nil, err
	}
	a := &app{co: co, today: ledger.NewDate(2026, time.June, 1), book: book, sl: salesledger.New(), purch: purchaseledger.New(), banks: defaultBanks(), mainBank: chart.Bank, reg: defaultRegister(co.Currency, co.Incorporated), invoiceDocs: map[string]*invoiceDoc{}, dataPath: dataPath, tmpl: tmpl}
	switch {
	case dataPath == "":
		a.seedShareCapital() // in-memory only
	default:
		if s, err := loadSnapshot(dataPath); err == nil {
			if rerr := a.restore(s); rerr != nil {
				log.Printf("could not restore %s (%v); starting a fresh company", dataPath, rerr)
				a.seedShareCapital()
			} else {
				log.Printf("restored company from %s", dataPath)
			}
		} else {
			a.seedShareCapital() // no save yet; this becomes the first
			a.save()
		}
	}
	return a, nil
}

// seedShareCapital posts the opening share issuance so the ledger agrees with the
// statutory register: the founder subscribed for their shares in cash on
// incorporation (debit bank, credit share capital).
func (a *app) seedShareCapital() {
	if !a.reg.IssuedCapital().IsPositive() {
		return
	}
	op := capital.IssueShares{Date: a.co.Incorporated, Ref: "SC-001", Amount: a.reg.IssuedCapital()}
	if j, err := op.Journal(); err == nil && a.book.Post(j) == nil {
		a.entries = append(a.entries, entry{section: "company", j: j})
	}
}

// --- helpers ---

func (a *app) bal(code string) money.Money { v, _ := a.book.Balance(code); return v }

func (a *app) ref(prefix string) string { a.seq++; return fmt.Sprintf("%s-%03d", prefix, a.seq) }

func (a *app) fy() company.FinancialYear { return a.co.YearContaining(a.today) }

// signer is the director who approves the accounts — the first in-office director.
func (a *app) signer() string {
	if d := a.reg.Directors(); len(d) > 0 {
		return d[0].Name
	}
	return "A Director"
}

// accounts builds the statutory year-end accounts for the current financial year.
func (a *app) accounts() (frs105.Accounts, error) {
	return frs105.Build(a.book, a.co, a.fy(), a.signer())
}

// vatReturn computes the VAT return for the current financial year. Wages, pension,
// depreciation and the corporation-tax charge are not Box 7 purchases.
func (a *app) vatReturn() vatreturn.Return {
	fy := a.fy()
	r, _ := vatreturn.Compute(a.book, fy.Start, fy.End, vatreturn.Options{
		VATControl:      chart.VAT,
		PurchaseExclude: map[string]bool{chart.Salaries: true, chart.EmployerNIC: true, chart.PensionCosts: true, chart.Depreciation: true, chart.CorpTaxCharge: true},
		CapitalCodes:    []string{chart.PlantEquipment},
	})
	return r
}

// importInvoices applies parsed CSV invoice rows (Crunch-style) as credit sales,
// recording a receipt for any that arrived already paid. The caller holds a.mu.
func (a *app) importInvoices(text string) string {
	rows, skipped, err := csvimport.ParseInvoices(strings.NewReader(text), a.co.Currency)
	if err != nil {
		return "⚠ could not read CSV: " + err.Error()
	}
	imported, closed := 0, 0
	for _, iv := range rows {
		if a.inClosedPeriod(iv.IssueDate) {
			closed++
			continue
		}
		ref := a.ref("INV")
		j, jerr := sales.Invoice{Date: iv.IssueDate, Ref: ref, Customer: iv.Client, Amount: iv.Net, VAT: iv.VAT}.Journal()
		if jerr != nil || a.book.Post(j) != nil {
			continue
		}
		a.entries = append(a.entries, entry{section: "sales", j: j})
		_, _ = a.sl.Raise(ref, iv.Client, iv.IssueDate, iv.Gross)
		rate := vat.None.Fraction
		if iv.VAT.IsPositive() {
			rate = vat.Standard.Fraction
		}
		desc := iv.Description
		if desc == "" {
			desc = "Imported invoice"
		}
		a.invoiceDocs[ref] = &invoiceDoc{Ref: ref, Customer: iv.Client, Date: iv.IssueDate,
			Lines: []sales.InvoiceLine{{Description: desc, Quantity: decimal.MustParse("1"), UnitPrice: iv.Net, VATRate: rate}},
			Net:   iv.Net, VAT: iv.VAT, Gross: iv.Gross}
		a.invoiceOrder = append(a.invoiceOrder, ref)
		if iv.Paid && !a.inClosedPeriod(iv.PaymentDate) {
			if rj, err := (sales.Receipt{Date: iv.PaymentDate, Ref: a.ref("REC"), Amount: iv.Gross, Bank: a.main()}).Journal(); err == nil && a.book.Post(rj) == nil {
				a.entries = append(a.entries, entry{section: "sales", j: rj})
				_ = a.sl.Allocate(ref, iv.Gross)
			}
		}
		imported++
	}
	return fmt.Sprintf("✓ Imported %d invoice(s); %d bad row(s) skipped, %d in closed periods", imported, len(skipped), closed)
}

// importExpenses applies parsed CSV expense rows as direct expenses paid from the
// main account, and records each as a recoverable cost. The caller holds a.mu.
func (a *app) importExpenses(text string) string {
	rows, skipped, err := csvimport.ParseExpenses(strings.NewReader(text), a.co.Currency)
	if err != nil {
		return "⚠ could not read CSV: " + err.Error()
	}
	imported, closed := 0, 0
	for _, ex := range rows {
		if a.inClosedPeriod(ex.Date) {
			closed++
			continue
		}
		ref := a.ref("EXP")
		desc := ex.Supplier
		if ex.Description != "" {
			desc = ex.Supplier + " — " + ex.Description
		}
		j, jerr := expenses.DirectExpense{Date: ex.Date, Ref: ref, Payee: desc, Amount: ex.Net, VAT: ex.VAT, Expense: chart.OfficeAdmin, Bank: a.main()}.Journal()
		if jerr != nil || a.book.Post(j) != nil {
			continue
		}
		a.entries = append(a.entries, entry{section: "expenses", j: j})
		a.costs = append(a.costs, &costRecord{Ref: ref, Desc: ex.Supplier, Date: ex.Date, Net: ex.Net})
		imported++
	}
	return fmt.Sprintf("✓ Imported %d expense(s); %d bad row(s) skipped, %d in closed periods", imported, len(skipped), closed)
}

// importStatement stores parsed bank-statement lines against a bank account for
// reconciliation. It posts nothing — a statement is evidence to tick the books
// against, not a source of journals. The caller holds a.mu.
func (a *app) importStatement(text, bankCode string) string {
	rows, skipped, err := csvimport.ParseStatement(strings.NewReader(text), a.co.Currency)
	if err != nil {
		return "⚠ could not read CSV: " + err.Error()
	}
	for _, sr := range rows {
		a.stmtLines = append(a.stmtLines, &stmtLine{BankCode: bankCode, Date: sr.Date, Desc: sr.Description, Amount: sr.Amount, Balance: sr.Balance, HasBalance: sr.HasBalance})
	}
	name := bankCode
	for _, b := range a.banks {
		if b.Code == bankCode {
			name = b.Name
		}
	}
	return fmt.Sprintf("✓ Imported %d statement line(s) for %s; %d bad row(s) skipped. Reconcile them under Banking → Reconcile.", len(rows), name, len(skipped))
}

func (a *app) date(r *http.Request) ledger.Date { return parseDate(r.FormValue("date"), a.today) }

func (a *app) vatOn(r *http.Request, net money.Money) money.Money {
	if !a.co.VATRegistered {
		return money.Zero(a.co.Currency)
	}
	return vat.ByCode(r.FormValue("vat")).On(net)
}

// bankCode returns the chosen bank account, defaulting to the main account.
func (a *app) bankCode(r *http.Request) string {
	if b := strings.TrimSpace(r.FormValue("bank")); b != "" {
		return b
	}
	return a.main()
}

// main returns the main bank account code, falling back to the base bank account.
func (a *app) main() string {
	if a.mainBank != "" {
		return a.mainBank
	}
	return chart.Bank
}

// nextBankCode finds a free code in the bank range (12xx, skipping Cash at 1210).
func (a *app) nextBankCode() string {
	for i := 1; i < 100; i++ {
		code := fmt.Sprintf("12%02d", i)
		if code == chart.Cash {
			continue
		}
		if _, ok := a.book.Account(code); !ok {
			return code
		}
	}
	return ""
}

func parseDate(s string, def ledger.Date) ledger.Date {
	t, err := time.Parse("2006-01-02", strings.TrimSpace(s))
	if err != nil {
		return def
	}
	return ledger.NewDate(t.Year(), t.Month(), t.Day())
}

func (a *app) amount(r *http.Request) (money.Money, error) {
	s := strings.TrimSpace(r.FormValue("amount"))
	m, err := money.Parse(a.co.Currency, s)
	if err != nil {
		return money.Money{}, fmt.Errorf("enter a valid amount (got %q)", s)
	}
	if !m.IsPositive() {
		return money.Money{}, fmt.Errorf("amount must be greater than zero")
	}
	return m, nil
}

// invoiceLines parses the itemised invoice form — up to six rows of description,
// quantity, unit price, VAT rate and a recharge flag. Empty rows are skipped; at
// least one valid line is required.
func (a *app) invoiceLines(r *http.Request) ([]sales.InvoiceLine, error) {
	cur := a.co.Currency
	var lines []sales.InvoiceLine
	for i := 0; i < 6; i++ {
		s := strconv.Itoa(i)
		desc := strings.TrimSpace(r.FormValue("desc" + s))
		priceStr := strings.TrimSpace(r.FormValue("price" + s))
		if desc == "" && priceStr == "" {
			continue
		}
		price, err := money.Parse(cur, priceStr)
		if err != nil || !price.IsPositive() {
			return nil, fmt.Errorf("line %d: enter a valid unit price", i+1)
		}
		qty := decimal.MustParse("1")
		if q := strings.TrimSpace(r.FormValue("qty" + s)); q != "" {
			qd, err := decimal.NewFromString(q)
			if err != nil || !qd.IsFinite() || qd.Rat().Sign() <= 0 {
				return nil, fmt.Errorf("line %d: enter a valid quantity", i+1)
			}
			qty = qd
		}
		if desc == "" {
			desc = "Item"
		}
		rate := vat.None.Fraction
		if a.co.VATRegistered {
			rate = vat.ByCode(r.FormValue("vat" + s)).Fraction
		}
		lines = append(lines, sales.InvoiceLine{
			Description: desc,
			Quantity:    qty,
			UnitPrice:   price,
			VATRate:     rate,
		})
	}
	return lines, nil
}

// rechargeLines turns the recorded costs ticked on the invoice form into recharge
// lines — each at the cost's net amount, standard-rated when the company is VAT
// registered (recharging a cost is a standard-rated supply). It returns the lines
// and the cost records to mark as recovered once the invoice is raised.
func (a *app) rechargeLines(r *http.Request) ([]sales.InvoiceLine, []*costRecord) {
	_ = r.ParseForm()
	rate := vat.None.Fraction
	if a.co.VATRegistered {
		rate = vat.Standard.Fraction
	}
	var lines []sales.InvoiceLine
	var recs []*costRecord
	for _, ref := range r.Form["recharge"] {
		for _, c := range a.costs {
			if c.Ref == ref && !c.Recharged {
				lines = append(lines, sales.InvoiceLine{
					Description: "Recharge: " + c.Desc + " (" + c.Ref + ")",
					UnitPrice:   c.Net,
					VATRate:     rate,
					Recharge:    true,
				})
				recs = append(recs, c)
			}
		}
	}
	return lines, recs
}

// addShares gives shares to a member, increasing an existing ordinary holding or
// registering a new member.
func (a *app) addShares(name string, shares int, since ledger.Date) {
	for i := range a.reg.Members {
		if a.reg.Members[i].Name == name && a.reg.Members[i].Class == "Ordinary" {
			a.reg.Members[i].Shares += shares
			return
		}
	}
	a.reg.Members = append(a.reg.Members, register.Member{Name: name, Class: "Ordinary", Shares: shares, Since: since})
}

// transferShares moves shares between members (no cash, no ledger entry — a transfer
// changes who owns the shares, not the company's capital).
func (a *app) transferShares(from, to string, n int) string {
	fromIdx := -1
	for i := range a.reg.Members {
		if a.reg.Members[i].Name == from && a.reg.Members[i].Class == "Ordinary" {
			fromIdx = i
			break
		}
	}
	if fromIdx < 0 {
		return "⚠ " + from + " is not a shareholder"
	}
	if a.reg.Members[fromIdx].Shares < n {
		return fmt.Sprintf("⚠ %s only holds %d shares", from, a.reg.Members[fromIdx].Shares)
	}
	a.reg.Members[fromIdx].Shares -= n
	a.addShares(to, n, a.today)
	kept := a.reg.Members[:0] // drop anyone left holding nothing
	for _, m := range a.reg.Members {
		if m.Shares > 0 {
			kept = append(kept, m)
		}
	}
	a.reg.Members = kept
	return fmt.Sprintf("✓ Transferred %d shares from %s to %s", n, from, to)
}

func (a *app) whole(r *http.Request, field string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(r.FormValue(field)))
	if err != nil || n < 0 {
		return 0, fmt.Errorf("enter a whole number for %s", field)
	}
	return n, nil
}

func (a *app) profitBeforeTax() money.Money {
	fy := a.fy()
	pl, _ := report.NewProfitAndLoss(a.book, fy.Start, fy.End)
	pbt, _ := pl.Profit.Add(a.bal(chart.CorpTaxCharge))
	return pbt
}

// capitalAllowances claims AIA on the assets acquired in the current financial year.
func (a *app) capitalAllowances() money.Money {
	fy := a.fy()
	additions := money.Zero(a.co.Currency)
	for _, h := range a.assets {
		if !h.Asset.Acquired.Before(fy.Start) && !fy.End.Before(h.Asset.Acquired) {
			additions, _ = additions.Add(h.Asset.Cost)
		}
	}
	res, _ := capitalallowances.Compute(capitalallowances.Input{MainAdditions: additions})
	return res.TotalAllowance
}

// taxableProfit adjusts accounting profit for tax: add back depreciation
// (disallowable), deduct capital allowances.
func (a *app) taxableProfit() money.Money {
	taxable, _ := corporationtax.AdjustProfit(a.profitBeforeTax(), a.bal(chart.Depreciation), a.capitalAllowances())
	return taxable
}

func (a *app) estimateCT() corporationtax.Result {
	res, _ := corporationtax.Compute(corporationtax.Input{FinancialYear: a.fy().Start.Year, TaxableProfit: a.taxableProfit()})
	return res
}

// --- view model ---

type postingView struct{ Account, Side, Amount, Effect string }
type journalView struct {
	Date, Ref, Narrative, Principle string
	Index                           int  // position in a.entries, for reversal
	Reversible                      bool // false when reversing would desync a subsidiary ledger
	Postings                        []postingView
}

// touchesSubsidiary reports whether a journal moves a control account backed by a
// subsidiary ledger (debtors/creditors) — such journals must be corrected through
// the sales/purchase ledger (e.g. a credit note), not a raw GL reversal.
func touchesSubsidiary(j ledger.Journal) bool {
	for _, p := range j.Postings() {
		if p.Account == chart.TradeDebtors || p.Account == chart.TradeCreditors {
			return true
		}
	}
	return false
}

type assetView struct {
	Ref, Name, Method      string
	Cost, Accumulated, NBV money.Money
}

type employeeView struct {
	Index               int
	Name, TaxCode, Plan string
	Salary              money.Money
}

type bankRow struct {
	Code, Name string
	Balance    money.Money
	Main       bool
}

type reconLine struct {
	Index      int
	Date, Desc string
	Amount     money.Money
	Reconciled bool
}

type reconView struct {
	BankCode, BankName                 string
	Ledger, StatementClose, Difference money.Money
	ReconciledTotal                    money.Money
	Agrees                             bool
	Lines                              []reconLine
}

// reconciliations groups imported statement lines by bank account and compares each
// account's ledger balance with its statement's closing balance. The caller holds a.mu.
func (a *app) reconciliations() []reconView {
	byBank := map[string][]int{}
	var order []string
	for i, l := range a.stmtLines {
		if _, ok := byBank[l.BankCode]; !ok {
			order = append(order, l.BankCode)
		}
		byBank[l.BankCode] = append(byBank[l.BankCode], i)
	}
	cur := a.co.Currency
	var out []reconView
	for _, code := range order {
		name := code
		for _, b := range a.banks {
			if b.Code == code {
				name = b.Name
			}
		}
		rv := reconView{BankCode: code, BankName: name, Ledger: a.bal(code)}
		close, reconciled, sum := money.Zero(cur), money.Zero(cur), money.Zero(cur)
		anyBalance := false
		for _, idx := range byBank[code] {
			l := a.stmtLines[idx]
			rv.Lines = append(rv.Lines, reconLine{Index: idx, Date: l.Date.String(), Desc: l.Desc, Amount: l.Amount, Reconciled: l.Reconciled})
			sum, _ = sum.Add(l.Amount)
			if l.HasBalance {
				close, anyBalance = l.Balance, true // the last stated running balance is the closing balance
			}
			if l.Reconciled {
				reconciled, _ = reconciled.Add(l.Amount)
			}
		}
		if !anyBalance {
			close = sum // no running balance in the file: use the sum of movements
		}
		rv.StatementClose, rv.ReconciledTotal = close, reconciled
		rv.Difference, _ = rv.Ledger.Sub(close)
		rv.Agrees = rv.Difference.IsZero()
		out = append(out, rv)
	}
	return out
}

func (a *app) toView(j ledger.Journal) journalView {
	ex := explain.ExplainJournal(a.book, j) // plain-language narration for learners
	jv := journalView{Date: j.Date().String(), Ref: j.Ref(), Narrative: j.Narrative(), Principle: ex.Principle, Reversible: !touchesSubsidiary(j)}
	for i, p := range j.Postings() {
		name := p.Account
		if acc, ok := a.book.Account(p.Account); ok {
			name = acc.Name
		}
		side := "Dr"
		if p.Side == ledger.Credit {
			side = "Cr"
		}
		effect := ""
		if i < len(ex.Postings) {
			effect = ex.Postings[i].Effect
		}
		jv.Postings = append(jv.Postings, postingView{Account: name, Side: side, Amount: fmtMoney(p.Amount), Effect: effect})
	}
	return jv
}

func (a *app) activity(section string) []journalView {
	var out []journalView
	for i := len(a.entries) - 1; i >= 0; i-- {
		if section == "" || a.entries[i].section == section {
			jv := a.toView(a.entries[i].j)
			jv.Index = i
			if a.entries[i].principle != "" {
				jv.Principle = a.entries[i].principle // op-level principle overrides the empty journal one
			}
			out = append(out, jv)
		}
	}
	return out
}

type pageData struct {
	LastImport *importReport
	Nav        []navSection
	Active     string
	Section    string
	Flash      string
	Content    template.HTML

	Co            company.Company
	FY            company.FinancialYear
	Today         ledger.Date
	YearEndDate   ledger.Date
	ClosedThrough ledger.Date

	Bank, Cash, Debtors, Creditors, PAYE, CorpTaxDue, DirLoan money.Money
	AccrualsBal, PrepaidBal                                   money.Money
	Profit, Reserves, SalesTotal, ExpensesTotal               money.Money
	VATOwed                                                   money.Money
	VATRates                                                  []vat.Rate

	TB                   ledger.TrialBalance
	PL                   report.ProfitAndLoss
	BS                   report.BalanceSheet
	Accounts             []ledger.Account
	ExpenseAccounts      []ledger.Account
	AllJournals          []journalView
	Invoices             []*salesledger.Invoice
	OpenInvoices         []*salesledger.Invoice
	Bills                []*purchaseledger.Bill
	OpenBills            []*purchaseledger.Bill
	Assets               []assetView
	Employees            []employeeView
	StudentLoanPlanNames []string
	Banks                []bankAcct
	BankRows             []bankRow
	MainBank             string

	Officers      []register.Officer
	Members       []register.Member
	Nominal       money.Money
	TotalShares   int
	IssuedCapital money.Money
	LastDividend  *dividendRun

	Costs            []*costRecord // all recorded costs, for recharge reconciliation
	RecoverableCosts []*costRecord // costs not yet recharged, offered on the invoice form
	Recs             []reconView   // bank reconciliations

	Activity                  []journalView
	Payroll                   *payroll.Result
	CT                        *corporationtax.Result
	PBT, DepAddBack, CapAllow money.Money
	Acc                       *frs105.Accounts
	VATReturn                 *vatreturn.Return
}

func (a *app) render(w http.ResponseWriter, page string) {
	fy := a.fy()
	pl, _ := report.NewProfitAndLoss(a.book, fy.Start, fy.End)
	bs, _ := report.NewBalanceSheet(a.book, fy.End)
	tb, _ := a.book.TrialBalance()
	reserves, _ := dividends.Available(a.book, fy.End)

	d := pageData{
		Nav: nav, Active: page, Section: sectionOf(page), Flash: a.flash,
		Co: a.co, FY: fy, Today: a.today, ClosedThrough: a.closedThrough,
		YearEndDate: ledger.NewDate(a.today.Year, a.co.YearEndMonth, a.co.YearEndDay),
		Bank:        a.bal(chart.Bank), Cash: a.bal(chart.Cash),
		Debtors: a.bal(chart.TradeDebtors), Creditors: a.bal(chart.TradeCreditors),
		PAYE: a.bal(chart.PAYENIC), CorpTaxDue: a.bal(chart.CorpTaxPayable), DirLoan: a.bal(chart.DirectorsLoan),
		AccrualsBal: a.bal(chart.Accruals), PrepaidBal: a.bal(chart.Prepayments),
		Profit: pl.Profit, Reserves: reserves, SalesTotal: a.bal(chart.Sales), ExpensesTotal: pl.TotalExpenses,
		VATOwed: a.bal(chart.VAT), VATRates: vat.Rates,
		TB: tb, PL: pl, BS: bs, Accounts: a.book.Accounts(),
		Invoices: a.sl.Invoices(), OpenInvoices: a.sl.Outstanding(),
		Bills: a.purch.Bills(), OpenBills: a.purch.Outstanding(),
		Activity: a.activity(sectionOf(page)), Payroll: a.lastPayroll,
	}
	for _, ac := range d.Accounts {
		if ac.Type == ledger.Expense {
			d.ExpenseAccounts = append(d.ExpenseAccounts, ac)
		}
	}
	for _, h := range a.assets {
		nbv, _ := h.Asset.Cost.Sub(h.Accumulated)
		method := "Straight-line"
		if h.Asset.Method == fixedassets.ReducingBalance {
			method = "Reducing balance"
		}
		d.Assets = append(d.Assets, assetView{Ref: h.Asset.Ref, Name: h.Asset.Name, Method: method, Cost: h.Asset.Cost, Accumulated: h.Accumulated, NBV: nbv})
	}
	for i, e := range a.employees {
		plan := e.StudentLoan
		if plan == "" {
			plan = "None"
		}
		d.Employees = append(d.Employees, employeeView{Index: i, Name: e.Name, TaxCode: e.TaxCode, Plan: plan, Salary: e.Salary})
	}
	for _, p := range payroll.StudentLoanPlans {
		d.StudentLoanPlanNames = append(d.StudentLoanPlanNames, p.Name)
	}
	d.LastImport = a.lastImport
	d.Banks, d.MainBank = a.banks, a.main()
	for _, b := range a.banks {
		d.BankRows = append(d.BankRows, bankRow{Code: b.Code, Name: b.Name, Balance: a.bal(b.Code), Main: b.Code == a.main()})
	}
	d.Officers, d.Members, d.Nominal = a.reg.Officers, a.reg.Members, a.reg.Nominal
	d.TotalShares, d.IssuedCapital, d.LastDividend = a.reg.TotalShares(), a.reg.IssuedCapital(), a.lastDividend
	d.Costs = a.costs
	for _, c := range a.costs {
		if !c.Recharged {
			d.RecoverableCosts = append(d.RecoverableCosts, c)
		}
	}
	if page == "overview" || strings.HasPrefix(page, "accounting") {
		d.AllJournals = a.activity("")
	}
	if page == "company-tax" {
		ct := a.estimateCT()
		d.CT, d.PBT = &ct, a.profitBeforeTax()
		d.DepAddBack, d.CapAllow = a.bal(chart.Depreciation), a.capitalAllowances()
	}
	if page == "accounting.accounts" {
		if acc, err := a.accounts(); err == nil {
			d.Acc = &acc
		}
	}
	if page == "banking.reconcile" {
		d.Recs = a.reconciliations()
	}
	if page == "company-tax.vat" && a.co.VATRegistered {
		vr := a.vatReturn()
		d.VATReturn = &vr
	}
	a.flash = ""

	var buf bytes.Buffer
	if err := a.tmpl.ExecuteTemplate(&buf, "page:"+page, d); err != nil {
		log.Printf("content %q: %v", page, err)
		http.Error(w, "template error", http.StatusInternalServerError)
		return
	}
	d.Content = template.HTML(buf.String())
	if err := a.tmpl.ExecuteTemplate(w, "layout", d); err != nil {
		log.Printf("layout: %v", err)
	}
}

func (a *app) page(id string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		defer a.mu.Unlock()
		a.render(w, id)
	}
}

// run performs a POST action then redirects back to the sub-page it came from.
func (a *app) run(section, redirect string, build func(r *http.Request) (themes.Operation, string, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Redirect(w, r, redirect, http.StatusSeeOther)
			return
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		op, okMsg, err := build(r)
		switch {
		case err != nil:
			a.flash = "⚠ " + err.Error()
		case op == nil:
			a.flash = okMsg
		default:
			j, jerr := op.Journal()
			switch {
			case jerr == nil && a.inClosedPeriod(j.Date()):
				a.flash = "⚠ " + j.Date().String() + " is in a closed period — reopen or use a later date"
			default:
				if perr := themes.Post(a.book, op); perr != nil {
					a.flash = "⚠ " + perr.Error()
				} else {
					if jerr == nil {
						principle := ""
						if ex, e := explain.Explain(a.book, op); e == nil {
							principle = ex.Principle
						}
						a.entries = append(a.entries, entry{section, j, principle})
					}
					a.flash = "✓ " + okMsg
				}
			}
		}
		http.Redirect(w, r, redirect, http.StatusSeeOther)
	}
}

func main() {
	addr := flag.String("addr", envOr("ACCOUNTS_ADDR", "127.0.0.1:8080"),
		"listen address (host:port); use :0 to auto-pick a free port")
	data := flag.String("data", envOr("ACCOUNTS_DATA", defaultDataPath()),
		"save file for the company; empty for in-memory only")
	flag.Parse()

	a, err := newApp(*data)
	if err != nil {
		log.Fatal(err)
	}
	if a.dataPath != "" {
		log.Printf("saving to %s", a.dataPath)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		a.page("overview")(w, r)
	})
	for _, s := range nav {
		if s.ID == "overview" {
			continue
		}
		for _, it := range s.Items {
			mux.HandleFunc(it.Href, a.page(it.ID))
		}
		if len(s.Items) == 0 {
			mux.HandleFunc(s.Href, a.page(s.ID))
		}
	}

	// Company.
	mux.HandleFunc("/company/details", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Redirect(w, r, "/company", http.StatusSeeOther)
			return
		}
		a.mu.Lock()
		a.co.Name = strings.TrimSpace(r.FormValue("name"))
		a.co.Number = strings.TrimSpace(r.FormValue("number"))
		a.co.SICCode = strings.TrimSpace(r.FormValue("sic"))
		a.co.RegisteredOffice = strings.TrimSpace(r.FormValue("office"))
		a.co.VATRegistered = r.FormValue("vatreg") != ""
		a.co.VATNumber = strings.TrimSpace(r.FormValue("vatnumber"))
		a.co.Incorporated = parseDate(r.FormValue("incorporated"), a.co.Incorporated)
		ye := parseDate(r.FormValue("yearend"), ledger.NewDate(a.today.Year, a.co.YearEndMonth, a.co.YearEndDay))
		a.co.YearEndMonth, a.co.YearEndDay = ye.Month, ye.Day
		a.flash = "✓ Company details updated"
		a.mu.Unlock()
		http.Redirect(w, r, "/company", http.StatusSeeOther)
	})
	mux.HandleFunc("/company/date", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Redirect(w, r, "/company/financial-year", http.StatusSeeOther)
			return
		}
		a.mu.Lock()
		if r.FormValue("advance") == "1" {
			a.today = a.co.NextYearStart(a.today)
			a.flash = "✓ Advanced to " + a.today.String()
		} else {
			a.today = parseDate(r.FormValue("date"), a.today)
			a.flash = "✓ Today is now " + a.today.String()
		}
		a.mu.Unlock()
		http.Redirect(w, r, "/company/financial-year", http.StatusSeeOther)
	})
	mux.HandleFunc("/company/close-year", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Redirect(w, r, "/company/financial-year", http.StatusSeeOther)
			return
		}
		a.mu.Lock()
		fy := a.fy()
		if !a.closedThrough.IsZero() && !a.closedThrough.Before(fy.End) {
			a.flash = "⚠ FY" + strconv.Itoa(fy.Number) + " is already closed"
		} else {
			// Post the closing journal (best effort — an empty year has nothing to close),
			// then lock the period and carry the clock into the next year.
			if j, err := yearend.CloseEntry(a.book, fy.End, a.ref("YE"), chart.RetainedEarnings, chart.Dividends); err == nil {
				if perr := a.book.Post(j); perr == nil {
					a.entries = append(a.entries, entry{section: "company", j: j})
				}
			}
			a.closedThrough = fy.End
			a.today = a.co.NextYearStart(a.today)
			a.flash = fmt.Sprintf("✓ Closed FY%d (to %s); profit carried to retained earnings. Now in FY%d.", fy.Number, fy.End, a.fy().Number)
		}
		a.mu.Unlock()
		http.Redirect(w, r, "/company/financial-year", http.StatusSeeOther)
	})
	mux.HandleFunc("/company/import/run", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Redirect(w, r, "/company/import", http.StatusSeeOther)
			return
		}
		text := r.FormValue("csv")
		if strings.TrimSpace(text) == "" {
			if f, _, err := r.FormFile("file"); err == nil {
				defer f.Close()
				if b, err := io.ReadAll(io.LimitReader(f, 4<<20)); err == nil {
					text = string(b)
				}
			}
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		switch {
		case strings.TrimSpace(text) == "":
			a.flash = "⚠ paste some CSV or choose a file to import"
		case r.FormValue("kind") == "expenses":
			a.flash = a.importExpenses(text)
		case r.FormValue("kind") == "statement":
			a.flash = a.importStatement(text, a.bankCode(r))
		default:
			a.flash = a.importInvoices(text)
		}
		http.Redirect(w, r, "/company/import", http.StatusSeeOther)
	})
	mux.HandleFunc("/company/import/crunch", func(w http.ResponseWriter, r *http.Request) { a.importArchive(w, r, crunchProfile()) })
	mux.HandleFunc("/company/officers/add", func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		defer a.mu.Unlock()
		if r.Method == http.MethodPost {
			name := strings.TrimSpace(r.FormValue("name"))
			role := register.Director
			if r.FormValue("role") == string(register.Secretary) {
				role = register.Secretary
			}
			if name == "" {
				a.flash = "⚠ enter the officer's name"
			} else {
				a.reg.Officers = append(a.reg.Officers, register.Officer{Name: name, Role: role, Appointed: parseDate(r.FormValue("date"), a.today)})
				a.flash = "✓ Appointed " + name + " as " + string(role)
			}
		}
		http.Redirect(w, r, "/company/people", http.StatusSeeOther)
	})
	mux.HandleFunc("/company/shares/issue", func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		defer a.mu.Unlock()
		if r.Method == http.MethodPost {
			to := strings.TrimSpace(r.FormValue("to"))
			shares, err := a.whole(r, "shares")
			switch {
			case to == "":
				a.flash = "⚠ choose who the shares are issued to"
			case err != nil || shares <= 0:
				a.flash = "⚠ enter a whole number of shares to issue"
			default:
				amount := a.reg.Nominal.MulInt(int64(shares))
				op := capital.IssueShares{Date: a.date(r), Ref: a.ref("SC"), Amount: amount, Bank: a.bankCode(r)}
				if j, jerr := op.Journal(); jerr != nil || a.book.Post(j) != nil {
					a.flash = "⚠ could not issue shares"
				} else {
					a.entries = append(a.entries, entry{section: "company", j: j})
					a.addShares(to, shares, a.date(r))
					a.flash = fmt.Sprintf("✓ Issued %d ordinary shares to %s for %s", shares, to, fmtMoney(amount))
				}
			}
		}
		http.Redirect(w, r, "/company/people", http.StatusSeeOther)
	})
	mux.HandleFunc("/company/shares/transfer", func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		defer a.mu.Unlock()
		if r.Method == http.MethodPost {
			from, to := strings.TrimSpace(r.FormValue("from")), strings.TrimSpace(r.FormValue("to"))
			shares, err := a.whole(r, "shares")
			switch {
			case from == "" || to == "":
				a.flash = "⚠ choose who is transferring and who receives the shares"
			case err != nil || shares <= 0:
				a.flash = "⚠ enter a whole number of shares to transfer"
			default:
				a.flash = a.transferShares(from, to, shares)
			}
		}
		http.Redirect(w, r, "/company/people", http.StatusSeeOther)
	})

	// Sales.
	mux.HandleFunc("/sales/invoices/raise", a.run("sales", "/sales", func(r *http.Request) (themes.Operation, string, error) {
		customer := strings.TrimSpace(r.FormValue("customer"))
		if customer == "" {
			customer = "Customer"
		}
		lines, err := a.invoiceLines(r)
		if err != nil {
			return nil, "", err
		}
		rlines, recs := a.rechargeLines(r)
		lines = append(lines, rlines...)
		if len(lines) == 0 {
			return nil, "", fmt.Errorf("add at least one line, or tick a cost to recharge")
		}
		when := a.date(r)
		ref := a.ref("INV")
		inv := sales.Invoice{Date: when, Ref: ref, Customer: customer, Lines: lines}
		net, vatAmt, gross, err := inv.Totals()
		if err != nil {
			return nil, "", err
		}
		if _, err := a.sl.Raise(ref, customer, when, gross); err != nil { // customer owes the gross
			return nil, "", err
		}
		for _, c := range recs { // reconcile the recovered costs to this invoice
			c.Recharged, c.RechargedOn = true, ref
		}
		a.invoiceDocs[ref] = &invoiceDoc{Ref: ref, Customer: customer, Date: when, Lines: lines, Net: net, VAT: vatAmt, Gross: gross}
		a.invoiceOrder = append(a.invoiceOrder, ref)
		return inv, fmt.Sprintf("Invoice %s raised — %s to %s", ref, fmtMoney(gross), customer), nil
	}))
	mux.HandleFunc("/sales/invoices/view", func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		defer a.mu.Unlock()
		doc, ok := a.invoiceDocs[strings.TrimSpace(r.URL.Query().Get("ref"))]
		if !ok {
			http.NotFound(w, r)
			return
		}
		slInv, _ := a.sl.Get(doc.Ref)
		data := struct {
			Co      company.Company
			Doc     *invoiceDoc
			Invoice *salesledger.Invoice
		}{a.co, doc, slInv}
		if err := a.tmpl.ExecuteTemplate(w, "invoicedoc", data); err != nil {
			log.Printf("invoicedoc: %v", err)
		}
	})
	mux.HandleFunc("/sales/receipts/record", a.run("sales", "/sales/receipts", func(r *http.Request) (themes.Operation, string, error) {
		ref := strings.TrimSpace(r.FormValue("invoice"))
		inv, ok := a.sl.Get(ref)
		if !ok {
			return nil, "", fmt.Errorf("choose an invoice to record the receipt against")
		}
		m, err := a.amount(r)
		if err != nil {
			return nil, "", err
		}
		if err := a.sl.Allocate(ref, m); err != nil {
			return nil, "", err
		}
		return sales.Receipt{Date: a.date(r), Ref: a.ref("REC"), Amount: m, Bank: a.bankCode(r)},
			fmt.Sprintf("Receipt of %s against %s (%s)", fmtMoney(m), ref, inv.Customer), nil
	}))
	mux.HandleFunc("/sales/cash/record", a.run("sales", "/sales/cash", func(r *http.Request) (themes.Operation, string, error) {
		m, err := a.amount(r)
		if err != nil {
			return nil, "", err
		}
		return sales.CashSale{Date: a.date(r), Ref: a.ref("CS"), Amount: m, VAT: a.vatOn(r, m), Bank: a.bankCode(r)}, "Cash sale recorded", nil
	}))
	mux.HandleFunc("/sales/credit-notes/record", a.run("sales", "/sales/credit-notes", func(r *http.Request) (themes.Operation, string, error) {
		m, err := a.amount(r)
		if err != nil {
			return nil, "", err
		}
		return sales.CreditNote{Date: a.date(r), Ref: a.ref("CN"), Amount: m}, "Credit note issued", nil
	}))

	// Expenses.
	mux.HandleFunc("/expenses/bills/record", a.run("expenses", "/expenses", func(r *http.Request) (themes.Operation, string, error) {
		m, err := a.amount(r)
		if err != nil {
			return nil, "", err
		}
		supplier := strings.TrimSpace(r.FormValue("supplier"))
		if supplier == "" {
			supplier = "Supplier"
		}
		vatAmt := a.vatOn(r, m)
		gross, err := m.Add(vatAmt)
		if err != nil {
			return nil, "", err
		}
		ref := a.ref("BILL")
		if _, err := a.purch.Record(ref, supplier, a.date(r), gross); err != nil { // you owe the gross
			return nil, "", err
		}
		a.costs = append(a.costs, &costRecord{Ref: ref, Desc: supplier, Date: a.date(r), Net: m})
		return expenses.Bill{Date: a.date(r), Ref: ref, Supplier: supplier, Amount: m, VAT: vatAmt, Expense: r.FormValue("account")}, "Bill recorded", nil
	}))
	mux.HandleFunc("/expenses/payments/record", a.run("expenses", "/expenses/payments", func(r *http.Request) (themes.Operation, string, error) {
		ref := strings.TrimSpace(r.FormValue("bill"))
		bill, ok := a.purch.Get(ref)
		if !ok {
			return nil, "", fmt.Errorf("choose a bill to pay")
		}
		m, err := a.amount(r)
		if err != nil {
			return nil, "", err
		}
		if err := a.purch.Allocate(ref, m); err != nil {
			return nil, "", err
		}
		return expenses.Payment{Date: a.date(r), Ref: a.ref("SPAY"), Amount: m, Bank: a.bankCode(r)},
			fmt.Sprintf("Paid %s to %s (%s)", fmtMoney(m), bill.Supplier, ref), nil
	}))
	mux.HandleFunc("/expenses/direct/record", a.run("expenses", "/expenses/direct", func(r *http.Request) (themes.Operation, string, error) {
		m, err := a.amount(r)
		if err != nil {
			return nil, "", err
		}
		desc := strings.TrimSpace(r.FormValue("desc"))
		if desc == "" {
			desc = "Expense"
		}
		ref := a.ref("EXP")
		a.costs = append(a.costs, &costRecord{Ref: ref, Desc: desc, Date: a.date(r), Net: m})
		return expenses.DirectExpense{Date: a.date(r), Ref: ref, Payee: desc, Amount: m, VAT: a.vatOn(r, m), Expense: r.FormValue("account"), Bank: a.bankCode(r)}, "Expense recorded", nil
	}))
	mux.HandleFunc("/expenses/credit-notes/record", a.run("expenses", "/expenses/credit-notes", func(r *http.Request) (themes.Operation, string, error) {
		m, err := a.amount(r)
		if err != nil {
			return nil, "", err
		}
		against := chart.TradeCreditors
		if r.FormValue("refund") != "" {
			against = a.bankCode(r) // a cash refund rather than a reduction of what's owed
		}
		return expenses.CreditNote{Date: a.date(r), Ref: a.ref("PCN"), Supplier: strings.TrimSpace(r.FormValue("supplier")), Amount: m, VAT: a.vatOn(r, m), Expense: r.FormValue("account"), Against: against}, "Supplier credit note recorded", nil
	}))
	mux.HandleFunc("/expenses/mileage/record", a.run("expenses", "/expenses/mileage", func(r *http.Request) (themes.Operation, string, error) {
		miles, err := a.whole(r, "miles")
		if err != nil {
			return nil, "", err
		}
		claim := mileage.Claim(miles, 0, mileage.Car, mileage.RateTable{})
		return mileage.Reimbursement{Date: a.date(r), Ref: a.ref("MIL"), Amount: claim},
			fmt.Sprintf("Mileage claim for %d miles: %s", miles, fmtMoney(claim)), nil
	}))

	// Banking.
	mux.HandleFunc("/banking/transfers/record", a.run("banking", "/banking/transfers", func(r *http.Request) (themes.Operation, string, error) {
		m, err := a.amount(r)
		if err != nil {
			return nil, "", err
		}
		from := strings.TrimSpace(r.FormValue("from"))
		if from == "" {
			from = chart.Bank
		}
		to := strings.TrimSpace(r.FormValue("to"))
		if to == "" {
			to = chart.Cash
		}
		return banking.Transfer{Date: a.date(r), Ref: a.ref("TFR"), Amount: m, From: from, To: to}, "Transfer recorded", nil
	}))
	mux.HandleFunc("/banking/accounts/add", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Redirect(w, r, "/banking", http.StatusSeeOther)
			return
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		name := strings.TrimSpace(r.FormValue("name"))
		if name == "" {
			name = "Bank account"
		}
		code := a.nextBankCode()
		if code == "" {
			a.flash = "⚠ no free bank account code"
		} else if err := a.book.AddAccount(ledger.Account{Code: code, Name: name, Type: ledger.Asset}); err != nil {
			a.flash = "⚠ " + err.Error()
		} else {
			a.banks = append(a.banks, bankAcct{code, name})
			a.flash = "✓ Added bank account: " + name
		}
		http.Redirect(w, r, "/banking", http.StatusSeeOther)
	})
	mux.HandleFunc("/banking/accounts/main", func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		defer a.mu.Unlock()
		code := strings.TrimSpace(r.FormValue("bank"))
		for _, b := range a.banks {
			if b.Code == code {
				a.mainBank = code
				a.flash = "✓ " + b.Name + " is now the main account"
				break
			}
		}
		http.Redirect(w, r, "/banking", http.StatusSeeOther)
	})
	mux.HandleFunc("/banking/reconcile/toggle", func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		defer a.mu.Unlock()
		if idx, err := strconv.Atoi(r.FormValue("i")); err == nil && idx >= 0 && idx < len(a.stmtLines) {
			a.stmtLines[idx].Reconciled = !a.stmtLines[idx].Reconciled
		}
		http.Redirect(w, r, "/banking/reconcile", http.StatusSeeOther)
	})
	mux.HandleFunc("/banking/interest/record", a.run("banking", "/banking/interest", func(r *http.Request) (themes.Operation, string, error) {
		m, err := a.amount(r)
		if err != nil {
			return nil, "", err
		}
		return banking.InterestReceived{Date: a.date(r), Ref: a.ref("INT"), Amount: m, Bank: a.bankCode(r)}, "Interest received", nil
	}))
	mux.HandleFunc("/banking/charges/record", a.run("banking", "/banking/interest", func(r *http.Request) (themes.Operation, string, error) {
		m, err := a.amount(r)
		if err != nil {
			return nil, "", err
		}
		return banking.Charge{Date: a.date(r), Ref: a.ref("CHG"), Amount: m, Bank: a.bankCode(r)}, "Bank charge recorded", nil
	}))

	// Pay Yourself.
	mux.HandleFunc("/pay-yourself/salary/run", a.run("pay-yourself", "/pay-yourself", func(r *http.Request) (themes.Operation, string, error) {
		gross, err := a.amount(r)
		if err != nil {
			return nil, "", err
		}
		res, err := payroll.Compute(payroll.Input{GrossAnnual: gross, AutoEnrol: r.FormValue("pension") != ""})
		if err != nil {
			return nil, "", err
		}
		a.lastPayroll = &res
		taxNIC, _ := res.IncomeTax.Add(res.EmployeeNIC)
		erNIC, _ := res.EmployerNIC.Add(res.Class1A) // secondary Class 1 + Class 1A on benefits
		return payyourself.Salary{Date: a.date(r), Ref: a.ref("SAL"), Gross: gross, TaxNIC: taxNIC, EmployerNIC: erNIC, EmployeePension: res.EmployeePension, EmployerPension: res.EmployerPension, Bank: a.main()}, "Salary run", nil
	}))
	mux.HandleFunc("/pay-yourself/dividends/declare", a.run("pay-yourself", "/pay-yourself/dividends", func(r *http.Request) (themes.Operation, string, error) {
		m, err := a.amount(r)
		if err != nil {
			return nil, "", err
		}
		dec, err := dividends.Check(a.book, a.fy().End, m)
		if err != nil {
			return nil, "", err
		}
		if !dec.Lawful {
			return nil, "", fmt.Errorf("unlawful: %s exceeds distributable reserves of %s (short by %s)", fmtMoney(m), fmtMoney(dec.Available), fmtMoney(dec.Shortfall))
		}
		awards, err := a.reg.AllocateDividend(m)
		if err != nil {
			return nil, "", err
		}
		ref := a.ref("DIV")
		a.lastDividend = &dividendRun{Ref: ref, Date: a.date(r), Total: m, PerShare: a.reg.PerShareLabel(m), Awards: awards}
		return payyourself.DeclareDividend{Date: a.date(r), Ref: ref, Amount: m}, "Dividend declared and allocated to shareholders", nil
	}))
	mux.HandleFunc("/pay-yourself/dividends/pay", a.run("pay-yourself", "/pay-yourself/dividends", func(r *http.Request) (themes.Operation, string, error) {
		m, err := a.amount(r)
		if err != nil {
			return nil, "", err
		}
		return payyourself.PayDividend{Date: a.date(r), Ref: a.ref("DVP"), Amount: m, Bank: a.main()}, "Dividend paid", nil
	}))
	mux.HandleFunc("/pay-yourself/loan/in", a.run("pay-yourself", "/pay-yourself/loan", func(r *http.Request) (themes.Operation, string, error) {
		m, err := a.amount(r)
		if err != nil {
			return nil, "", err
		}
		return payyourself.IntroduceFunds{Date: a.date(r), Ref: a.ref("DLI"), Amount: m, Bank: a.main()}, "Funds introduced", nil
	}))
	mux.HandleFunc("/pay-yourself/loan/out", a.run("pay-yourself", "/pay-yourself/loan", func(r *http.Request) (themes.Operation, string, error) {
		m, err := a.amount(r)
		if err != nil {
			return nil, "", err
		}
		return payyourself.DrawFunds{Date: a.date(r), Ref: a.ref("DLO"), Amount: m, Bank: a.main()}, "Funds drawn", nil
	}))
	mux.HandleFunc("/pay-yourself/employees/add", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Redirect(w, r, "/pay-yourself/employees", http.StatusSeeOther)
			return
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		salary, err := a.amount(r)
		if err != nil {
			a.flash = "⚠ " + err.Error()
			http.Redirect(w, r, "/pay-yourself/employees", http.StatusSeeOther)
			return
		}
		name := strings.TrimSpace(r.FormValue("name"))
		if name == "" {
			name = "Employee"
		}
		taxCode := strings.TrimSpace(r.FormValue("taxcode"))
		if taxCode == "" {
			taxCode = "1257L"
		}
		bik := money.Zero(a.co.Currency)
		if s := strings.TrimSpace(r.FormValue("bik")); s != "" {
			if m, e := money.Parse(a.co.Currency, s); e == nil {
				bik = m
			}
		}
		a.employees = append(a.employees, &employee{Name: name, TaxCode: taxCode, StudentLoan: r.FormValue("plan"), Salary: salary, BIK: bik, AutoEnrol: r.FormValue("pension") != ""})
		a.flash = "✓ Added " + name
		http.Redirect(w, r, "/pay-yourself/employees", http.StatusSeeOther)
	})
	mux.HandleFunc("/pay-yourself/employees/pay", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Redirect(w, r, "/pay-yourself/employees", http.StatusSeeOther)
			return
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		idx, _ := strconv.Atoi(r.FormValue("emp"))
		if idx < 0 || idx >= len(a.employees) {
			a.flash = "⚠ unknown employee"
			http.Redirect(w, r, "/pay-yourself/employees", http.StatusSeeOther)
			return
		}
		e := a.employees[idx]
		res, err := payroll.Compute(payroll.Input{GrossAnnual: e.Salary, TaxCode: e.TaxCode, StudentLoan: payroll.StudentLoanByName(e.StudentLoan), BenefitsInKind: e.BIK, AutoEnrol: e.AutoEnrol})
		if err != nil {
			a.flash = "⚠ " + err.Error()
			http.Redirect(w, r, "/pay-yourself/employees", http.StatusSeeOther)
			return
		}
		taxNIC, _ := res.IncomeTax.Add(res.EmployeeNIC)
		taxNIC, _ = taxNIC.Add(res.StudentLoan)      // income tax + employee NI + student loan, all withheld
		erNIC, _ := res.EmployerNIC.Add(res.Class1A) // secondary Class 1 + Class 1A on benefits
		j, jerr := payyourself.Salary{Date: a.today, Ref: a.ref("SAL"), Gross: e.Salary, TaxNIC: taxNIC, EmployerNIC: erNIC, EmployeePension: res.EmployeePension, EmployerPension: res.EmployerPension, Bank: a.main()}.Journal()
		if jerr == nil {
			jerr = a.book.Post(j)
		}
		if jerr != nil {
			a.flash = "⚠ " + jerr.Error()
			http.Redirect(w, r, "/pay-yourself/employees", http.StatusSeeOther)
			return
		}
		a.entries = append(a.entries, entry{section: "pay-yourself", j: j})
		a.lastPayroll = &res
		a.flash = "✓ Ran payroll for " + e.Name
		http.Redirect(w, r, "/pay-yourself/employees", http.StatusSeeOther)
	})

	// Company Tax.
	mux.HandleFunc("/company-tax/provide", a.run("company-tax", "/company-tax", func(r *http.Request) (themes.Operation, string, error) {
		ct := a.estimateCT()
		delta, err := ct.Charge.Sub(a.bal(chart.CorpTaxCharge))
		if err != nil {
			return nil, "", err
		}
		if !delta.IsPositive() {
			return nil, "Corporation tax already provided for", nil
		}
		return companytax.Provision{Date: a.date(r), Ref: a.ref("CT"), Amount: delta}, fmt.Sprintf("Provided %s for corporation tax", fmtMoney(delta)), nil
	}))
	mux.HandleFunc("/company-tax/pay", a.run("company-tax", "/company-tax", func(r *http.Request) (themes.Operation, string, error) {
		m, err := a.amount(r)
		if err != nil {
			return nil, "", err
		}
		return companytax.Payment{Date: a.date(r), Ref: a.ref("CTP"), Amount: m, Bank: a.main()}, "Corporation tax paid", nil
	}))

	// Accounting.
	mux.HandleFunc("/accounting/fixed-assets/acquire", a.run("accounting", "/accounting/fixed-assets", func(r *http.Request) (themes.Operation, string, error) {
		cost, err := a.amount(r)
		if err != nil {
			return nil, "", err
		}
		name := strings.TrimSpace(r.FormValue("name"))
		if name == "" {
			name = "Asset"
		}
		method := fixedassets.StraightLine
		if r.FormValue("method") == "reducing" {
			method = fixedassets.ReducingBalance
		}
		life, _ := a.whole(r, "life")
		var rate decimal.Decimal
		if s := strings.TrimSpace(r.FormValue("rate")); s != "" {
			if d, err := decimal.NewFromString(s + "E-2"); err == nil { // percent → fraction
				rate = d
			}
		}
		ref := a.ref("FA")
		a.assets = append(a.assets, &assetHolding{
			Asset:       fixedassets.Asset{Ref: ref, Name: name, Cost: cost, Acquired: a.date(r), Method: method, UsefulLifeYears: life, Rate: rate},
			Accumulated: money.Zero(a.co.Currency),
		})
		return fixedassets.Acquisition{Date: a.date(r), Ref: ref, Amount: cost}, "Asset purchased: " + name, nil
	}))
	mux.HandleFunc("/accounting/fixed-assets/depreciate", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Redirect(w, r, "/accounting/fixed-assets", http.StatusSeeOther)
			return
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		total := money.Zero(a.co.Currency)
		posted := 0
		for _, h := range a.assets {
			charge, err := h.Asset.Charge(h.Accumulated)
			if err != nil || !charge.IsPositive() {
				continue
			}
			j, err := fixedassets.DepreciationEntry{Date: a.today, Ref: a.ref("DEP"), Amount: charge}.Journal()
			if err != nil || a.book.Post(j) != nil {
				continue
			}
			a.entries = append(a.entries, entry{section: "accounting", j: j})
			h.Accumulated, _ = h.Accumulated.Add(charge)
			total, _ = total.Add(charge)
			posted++
		}
		if posted == 0 {
			a.flash = "⚠ No depreciation to post — add an asset first"
		} else {
			a.flash = fmt.Sprintf("✓ Posted %s depreciation across %d asset(s)", fmtMoney(total), posted)
		}
		http.Redirect(w, r, "/accounting/fixed-assets", http.StatusSeeOther)
	})
	mux.HandleFunc("/accounting/journals/post", a.run("accounting", "/accounting/journals", func(r *http.Request) (themes.Operation, string, error) {
		m, err := a.amount(r)
		if err != nil {
			return nil, "", err
		}
		narr := strings.TrimSpace(r.FormValue("narrative"))
		if narr == "" {
			narr = "Manual journal"
		}
		j, jerr := ledger.NewJournal(a.date(r), narr,
			ledger.Posting{Account: r.FormValue("debit"), Side: ledger.Debit, Amount: m},
			ledger.Posting{Account: r.FormValue("credit"), Side: ledger.Credit, Amount: m},
		)
		return journalOp{j: j}, "Journal posted", jerr
	}))
	mux.HandleFunc("/accounting/adjustments/accrue", a.run("accounting", "/accounting/adjustments", func(r *http.Request) (themes.Operation, string, error) {
		m, err := a.amount(r)
		if err != nil {
			return nil, "", err
		}
		return adjustments.Accrual{Date: a.date(r), Ref: a.ref("ACR"), Note: strings.TrimSpace(r.FormValue("note")), Amount: m, Expense: r.FormValue("account")}, "Accrual posted", nil
	}))
	mux.HandleFunc("/accounting/adjustments/prepay", a.run("accounting", "/accounting/adjustments", func(r *http.Request) (themes.Operation, string, error) {
		m, err := a.amount(r)
		if err != nil {
			return nil, "", err
		}
		return adjustments.Prepayment{Date: a.date(r), Ref: a.ref("PRE"), Note: strings.TrimSpace(r.FormValue("note")), Amount: m, Expense: r.FormValue("account")}, "Prepayment posted", nil
	}))
	mux.HandleFunc("/accounting/reverse", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Redirect(w, r, "/accounting/journals", http.StatusSeeOther)
			return
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		idx, err := strconv.Atoi(r.FormValue("i"))
		switch {
		case err != nil || idx < 0 || idx >= len(a.entries):
			a.flash = "⚠ unknown entry to reverse"
		case a.inClosedPeriod(a.today):
			a.flash = "⚠ today is in a closed period — a reversal must post to an open period"
		case touchesSubsidiary(a.entries[idx].j):
			a.flash = "⚠ this entry moves trade debtors/creditors — correct it with a credit note so the sales/purchase ledger stays in step"
		default:
			orig := a.entries[idx].j
			rev := orig.Reverse(a.today, "Reversal of "+orig.Narrative())
			if perr := a.book.Post(rev); perr != nil {
				a.flash = "⚠ " + perr.Error()
			} else {
				a.entries = append(a.entries, entry{section: a.entries[idx].section, j: rev, principle: "A mistake is fixed by posting an equal and opposite entry: the original stays on record and the two net to nothing, so the audit trail is never broken."})
				a.flash = "✓ Reversed: " + orig.Narrative()
			}
		}
		http.Redirect(w, r, "/accounting/journals", http.StatusSeeOther)
	})
	mux.HandleFunc("/accounting/accounts/ixbrl", func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		defer a.mu.Unlock()
		acc, err := a.accounts()
		if err != nil {
			http.Error(w, "could not build accounts: "+err.Error(), http.StatusInternalServerError)
			return
		}
		doc, err := acc.IXBRL()
		if err != nil {
			http.Error(w, "could not render iXBRL: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if r.URL.Query().Get("download") == "1" {
			filename := fmt.Sprintf("accounts-%s.html", acc.FY.End)
			w.Header().Set("Content-Type", "application/xhtml+xml; charset=utf-8")
			w.Header().Set("Content-Disposition", "attachment; filename=\""+filename+"\"")
		} else {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
		}
		_, _ = w.Write([]byte(doc))
	})

	mux.HandleFunc("/reset", func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		a.co = company.Default()
		a.today = ledger.NewDate(2026, time.June, 1)
		a.book, _ = chart.NewUKMicroLtdBook(a.co.Currency)
		a.sl = salesledger.New()
		a.purch = purchaseledger.New()
		a.invoiceDocs, a.invoiceOrder, a.costs, a.stmtLines = map[string]*invoiceDoc{}, nil, nil, nil
		a.assets = nil
		a.employees = nil
		a.banks = defaultBanks()
		a.mainBank = chart.Bank
		a.reg = defaultRegister(a.co.Currency, a.co.Incorporated)
		a.entries, a.seq, a.lastPayroll, a.lastDividend = nil, 0, nil, nil
		a.seedShareCapital()
		a.flash = "✓ Started a fresh company"
		a.mu.Unlock()
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("cannot listen on %s: %v", *addr, err)
	}
	log.Printf("Virtual Accounts UI on http://%s", ln.Addr())
	log.Fatal(http.Serve(ln, a.persistMiddleware(mux)))
}

// persistMiddleware saves the company after any state-changing (POST) request.
func (a *app) persistMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
		if r.Method == http.MethodPost {
			a.save()
		}
	})
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func fmtMoney(m money.Money) string {
	cur := m.Currency()
	neg := m.IsNegative()
	digits := strings.TrimPrefix(m.Abs().String(), cur.Code+" ")
	intp, frac := digits, ""
	if i := strings.IndexByte(digits, '.'); i >= 0 {
		intp, frac = digits[:i], digits[i:]
	}
	var b strings.Builder
	n := len(intp)
	for i := 0; i < n; i++ {
		if i > 0 && (n-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteByte(intp[i])
	}
	grouped := b.String() + frac
	out := cur.Code + " " + grouped
	if sym := cur.Symbol(); sym != "" {
		out = sym + grouped
	}
	if neg {
		out = "−" + out
	}
	return out
}
