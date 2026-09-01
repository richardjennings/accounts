package main

import (
	"bytes"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/richardjennings/accounts/chart"
	"github.com/richardjennings/accounts/explain"
	"github.com/richardjennings/accounts/importer"
	"github.com/richardjennings/accounts/importer/crunch"
	"github.com/richardjennings/accounts/ledger"
	"github.com/richardjennings/accounts/money"
	"github.com/richardjennings/accounts/themes"
	"github.com/richardjennings/accounts/themes/banking"
	"github.com/richardjennings/accounts/themes/companytax"
	"github.com/richardjennings/accounts/themes/expenses"
	"github.com/richardjennings/accounts/themes/payyourself"
	"github.com/richardjennings/accounts/themes/sales"
	"github.com/richardjennings/decimal"
)

// maxImportArchive bounds an uploaded export archive.
const maxImportArchive = 64 << 20

// importReport is what an import did, shown on the import page.
type importReport struct {
	Profile string
	Counts  []importCount
	Notes   []string
	Issues  []string
	Skipped int // records in closed periods, not posted
}

type importCount struct {
	Kind string
	N    int
}

// importArchive handles an uploaded export archive for a profile.
func (a *app) importArchive(w http.ResponseWriter, r *http.Request, profile importer.Profile) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/company/import", http.StatusSeeOther)
		return
	}
	f, _, err := r.FormFile("file")
	if err != nil {
		a.mu.Lock()
		a.flash = "⚠ choose the export archive (.zip) to import"
		a.mu.Unlock()
		http.Redirect(w, r, "/company/import", http.StatusSeeOther)
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxImportArchive))
	if err != nil {
		a.mu.Lock()
		a.flash = "⚠ " + err.Error()
		a.mu.Unlock()
		http.Redirect(w, r, "/company/import", http.StatusSeeOther)
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	tables, err := importer.ReadZip(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		a.flash = "⚠ " + err.Error()
		http.Redirect(w, r, "/company/import", http.StatusSeeOther)
		return
	}
	batch, issues, err := profile.Read(tables, a.co.Currency)
	if err != nil {
		a.flash = "⚠ " + err.Error()
		http.Redirect(w, r, "/company/import", http.StatusSeeOther)
		return
	}
	// The export is read in full before anything is cleared, so a bad archive
	// leaves the existing books untouched.
	if r.FormValue("replace") != "" {
		a.clearBooks()
	}
	rep := a.applyBatch(profile.Name(), batch, issues)
	a.lastImport = rep // persistMiddleware saves after the request
	posted := 0
	for _, c := range rep.Counts {
		posted += c.N
	}
	a.flash = fmt.Sprintf("✓ Imported %d record(s) from the %s export; %d need attention — see the report below", posted, profile.Name(), len(rep.Issues))
	http.Redirect(w, r, "/company/import", http.StatusSeeOther)
}

func crunchProfile() importer.Profile { return crunch.Profile{} }

// batchApplier posts a batch into the company. It assigns the app's own
// references and keeps the source's in the narratives and documents.
type batchApplier struct {
	a      *app
	rep    *importReport
	counts map[string]int
	banks  map[string]string // bank name (lower-case) → account code
	refs   map[string]string // source invoice ref → app ref
	vatReg bool
	latest ledger.Date
	cur    money.Currency
	notes  map[string]bool
	mapped map[string]string // source category → account name, for the notes
}

// applyBatch posts every record of a batch and returns the report.
func (a *app) applyBatch(profile string, b *importer.Batch, issues []importer.Issue) *importReport {
	ap := &batchApplier{a: a, rep: &importReport{Profile: profile}, counts: map[string]int{}, banks: map[string]string{}, refs: map[string]string{}, cur: a.co.Currency, notes: map[string]bool{}, mapped: map[string]string{}}
	for _, i := range issues {
		ap.rep.Issues = append(ap.rep.Issues, i.String())
	}
	ap.ensureBanks(b.Banks, b.BankCurrency)
	ap.chooseMain(b)
	ap.startFrom(b)
	ap.vatReg = b.VATCharged
	switch {
	case len(b.Invoices) > 0 && !b.VATCharged:
		a.co.VATRegistered = false
		ap.note("No sales invoice charges VAT, so the company is treated as not VAT-registered: costs are posted gross and no input VAT is reclaimed.")
	case b.VATCharged && !a.co.VATRegistered:
		a.co.VATRegistered = true
		ap.note("Sales invoices charge VAT, so the company is marked VAT-registered.")
	}
	ap.invoices(b.Invoices)
	ap.creditNotes(b.CreditNotes)
	ap.receipts(b.Receipts)
	ap.bills(b.Bills)
	for _, t := range b.Transfers {
		ap.transfer(t)
	}
	for _, in := range b.Introduced {
		ap.post("pay-yourself", "Funds introduced", payyourself.IntroduceFunds{Date: in.Date, Ref: a.ref("DLI"), Amount: in.Amount, Bank: ap.code(in.Bank, a.main())})
	}
	for _, i := range b.Interest {
		ap.post("banking", "Interest received", banking.InterestReceived{Date: i.Date, Ref: a.ref("INT"), Amount: i.Amount, Bank: ap.code(i.Bank, a.main())})
	}
	for _, s := range b.Salaries {
		paidFrom := a.main()
		if s.Owed {
			paidFrom = chart.DirectorsLoan // owed to the director until drawn
		}
		ap.post("pay-yourself", "Salaries", payyourself.Salary{Date: s.Date, Ref: a.ref("SAL"), Gross: s.Gross, TaxNIC: s.TaxNIC, EmployerNIC: s.EmployerNIC, Bank: paidFrom})
	}
	if len(b.Dividends) > 0 {
		ap.note("Dividends are posted as declared, without the distributable-reserves check; check the reserves for each year.")
	}
	for _, d := range b.Dividends {
		ap.post("pay-yourself", "Dividends declared", payyourself.DeclareDividend{Date: d.Date, Ref: a.ref("DIV"), Amount: d.Amount})
	}
	for _, d := range b.Drawings {
		ap.post("pay-yourself", "Director withdrawals", payyourself.DrawFunds{Date: d.Date, Ref: a.ref("DLO"), Amount: d.Amount, Bank: ap.code(d.Bank, a.main())})
	}
	ap.tax(b.TaxPayments, b.TaxRebates)
	for kind, n := range ap.counts {
		ap.rep.Counts = append(ap.rep.Counts, importCount{kind, n})
	}
	sort.Slice(ap.rep.Counts, func(i, j int) bool { return ap.rep.Counts[i].Kind < ap.rep.Counts[j].Kind })
	if len(ap.mapped) > 0 {
		var cats []string
		for c, acct := range ap.mapped {
			cats = append(cats, c+" → "+acct)
		}
		sort.Strings(cats)
		ap.note("Cost categories were mapped to accounts: " + strings.Join(cats, "; ") + ".")
	}
	if ap.rep.Skipped > 0 {
		ap.note(fmt.Sprintf("%d record(s) fell in a closed period and were not posted.", ap.rep.Skipped))
	}
	for _, bk := range a.banks {
		if bal := a.bal(bk.Code); bal.IsNegative() {
			ap.note(bk.Name + " ends " + bal.String() + ": money left it that never arrived in the export, so receipts or transfers into it are missing.")
		}
	}
	if !ap.latest.IsZero() && a.today.Before(ap.latest) {
		a.today = ap.latest
		ap.note("The clock moved to " + ap.latest.String() + ", the latest date in the export.")
	}
	return ap.rep
}

func (ap *batchApplier) note(s string) {
	if !ap.notes[s] {
		ap.notes[s] = true
		ap.rep.Notes = append(ap.rep.Notes, s)
	}
}

func (ap *batchApplier) issue(format string, args ...any) {
	ap.rep.Issues = append(ap.rep.Issues, fmt.Sprintf(format, args...))
}

// ensureBanks finds or creates a ledger account for every bank name, and sets
// the currency the source says an account is held in.
func (ap *batchApplier) ensureBanks(names []string, currencies map[string]string) {
	for _, b := range ap.a.banks {
		ap.banks[strings.ToLower(b.Name)] = b.Code
	}
	for _, name := range names {
		key := strings.ToLower(name)
		ccy := currencies[name]
		if code, ok := ap.banks[key]; ok {
			for i := range ap.a.banks {
				if ap.a.banks[i].Code == code && ap.a.banks[i].Currency == "" && ccy != "" {
					ap.a.banks[i].Currency = ccy
					ap.note(name + " is held in " + ccy + " (from its name); correct it under Banking → Accounts if that is wrong.")
				}
			}
			continue
		}
		code := ap.a.nextBankCode()
		if code == "" {
			ap.issue("no free account code for bank account %q; its movements go to the main account", name)
			continue
		}
		if err := ap.a.book.AddAccount(ledger.Account{Code: code, Name: name, Type: ledger.Asset}); err != nil {
			ap.issue("bank account %q: %v", name, err)
			continue
		}
		ap.a.banks = append(ap.a.banks, bankAcct{Code: code, Name: name, Currency: ccy})
		ap.banks[key] = code
		if ccy != "" {
			ap.note("Added bank account: " + name + ", held in " + ccy + " (from its name).")
		} else {
			ap.note("Added bank account: " + name + ".")
		}
	}
}

// chooseMain makes the export's most-used bank account the main account when
// the company still has the untouched default one. Records whose account the
// export does not name are posted to the main account, so this matters.
func (ap *batchApplier) chooseMain(b *importer.Batch) {
	a := ap.a
	bal := a.bal(chart.Bank)
	untouched := bal.IsZero() || bal.Equal(a.reg.IssuedCapital()) // nothing, or only the seeded share capital
	if a.main() != chart.Bank || !untouched {
		return
	}
	uses := map[string]int{}
	for _, d := range b.Drawings {
		uses[d.Bank]++
	}
	for _, t := range b.Transfers {
		uses[t.From]++
		uses[t.To]++
	}
	for _, i := range b.Introduced {
		uses[i.Bank]++
	}
	best, n := "", 0
	for name, c := range uses {
		// Never a foreign-currency account: records without an account are
		// posted to the main account at company-currency values.
		if code := ap.code(name, ""); code == "" || ap.a.isForeign(code) {
			continue
		}
		if c > n {
			best, n = name, c
		}
	}
	if code := ap.code(best, ""); code != "" {
		a.mainBank = code
		ap.note(best + " is now the main bank account: the export uses it most, and payments whose account the export does not name are posted to it.")
	}
}

// startFrom moves the incorporation date back to the earliest record when the
// company's date is later, so every year of the history has a financial year.
func (ap *batchApplier) startFrom(b *importer.Batch) {
	earliest := ledger.Date{}
	note := func(d ledger.Date) {
		if !d.IsZero() && (earliest.IsZero() || d.Before(earliest)) {
			earliest = d
		}
	}
	for _, x := range b.Invoices {
		note(x.Date)
	}
	for _, x := range b.Bills {
		note(x.Date)
	}
	for _, x := range b.Receipts {
		note(x.Date)
	}
	for _, x := range b.Transfers {
		note(x.Date)
	}
	for _, x := range b.Salaries {
		note(x.Date)
	}
	for _, x := range b.Introduced {
		note(x.Date)
	}
	for _, x := range b.Interest {
		note(x.Date)
	}
	for _, x := range b.Drawings {
		note(x.Date)
	}
	for _, x := range b.Dividends {
		note(x.Date)
	}
	for _, x := range b.TaxPayments {
		note(x.Date)
	}
	if !earliest.IsZero() && earliest.Before(ap.a.co.Incorporated) {
		ap.a.co.Incorporated = earliest
		ap.note("Incorporation moved to " + earliest.String() + ", the earliest date in the export; set the true date and the year end under Company → Details.")
	}
}

// code resolves a bank name to an account code, or dflt for "" and unknown names.
func (ap *batchApplier) code(name, dflt string) string {
	if name == "" {
		return dflt
	}
	if c, ok := ap.banks[strings.ToLower(name)]; ok {
		return c
	}
	return dflt
}

// transfer posts one movement between accounts. A movement out of a
// foreign-currency account into a company-currency one is a conversion: the
// source does not record the currency amount sold, so the dollars leave at the
// account's average carried rate — no realised difference — and the residual
// waits for a revaluation. When the carrying is already exhausted (receipts
// missing from the source), the movement falls back to a plain transfer and is
// reported, rather than inventing an exchange gain.
func (ap *batchApplier) transfer(t importer.Transfer) {
	a := ap.a
	from, to := ap.code(t.From, chart.Cash), ap.code(t.To, chart.Cash)
	if !a.isForeign(from) || a.isForeign(to) {
		if a.isForeign(to) {
			ap.issue("transfer of %s into %s on %s: the currency amount bought is not in the export; imported at face value", t.Amount, t.To, t.Date)
		}
		ap.post("banking", "Transfers", banking.Transfer{Date: t.Date, Ref: a.ref("TFR"), Amount: t.Amount, From: from, To: to})
		return
	}
	carrying, err := a.book.BalanceAsAt(from, t.Date)
	fxb := a.fxBal(from)
	if err != nil {
		carrying = a.bal(from)
	}
	if c, _ := carrying.Cmp(t.Amount); c < 0 || !fxb.IsPositive() {
		ap.issue("conversion of %s out of %s on %s exceeds its carried balance of %s — receipts into it are missing from the export; imported as a plain transfer", t.Amount, t.From, t.Date, carrying)
		ap.post("banking", "Transfers", banking.Transfer{Date: t.Date, Ref: a.ref("TFR"), Amount: t.Amount, From: from, To: to})
		return
	}
	share := new(big.Rat).Quo(t.Amount.Amount().Rat(), carrying.Amount().Rat())
	sold := money.FromRat(fxb.Currency(), new(big.Rat).Mul(fxb.Amount().Rat(), share), money.HalfUp)
	if c, _ := sold.Cmp(fxb); c > 0 {
		sold = fxb
	}
	if ap.post("banking", "Currency conversions", banking.Conversion{Date: t.Date, Ref: a.ref("FX"), Proceeds: t.Amount, Carried: t.Amount, From: from, To: to}) {
		a.addFX(from, sold.Neg())
	}
}

// post builds an operation's journal and posts it, unless it falls in a closed
// period. It returns whether it was posted.
func (ap *batchApplier) post(section, kind string, op themes.Operation) bool {
	j, err := op.Journal()
	if err != nil {
		ap.issue("%s: %v", kind, err)
		return false
	}
	return ap.postJournal(section, kind, j, op)
}

// postJournal posts a ready journal. op, when given, supplies the explanation.
func (ap *batchApplier) postJournal(section, kind string, j ledger.Journal, op themes.Operation) bool {
	if ap.a.inClosedPeriod(j.Date()) {
		ap.rep.Skipped++
		return false
	}
	if err := ap.a.book.Post(j); err != nil {
		ap.issue("%s %s: %v", kind, j.Ref(), err)
		return false
	}
	principle := ""
	if op != nil {
		if ex, err := explain.Explain(ap.a.book, op); err == nil {
			principle = ex.Principle
		}
	} else {
		principle = explain.ExplainJournal(ap.a.book, j).Principle
	}
	ap.a.entries = append(ap.a.entries, entry{section, j, principle})
	ap.counts[kind]++
	if ap.latest.Before(j.Date()) {
		ap.latest = j.Date()
	}
	return true
}

func (ap *batchApplier) raw(section, kind string, date ledger.Date, ref, narrative string, postings ...ledger.Posting) bool {
	j, err := ledger.NewJournal(date, narrative, postings...)
	if err != nil {
		ap.issue("%s: %v", kind, err)
		return false
	}
	return ap.postJournal(section, kind, j.WithRef(ref), nil)
}

func (ap *batchApplier) invoices(invs []importer.Invoice) {
	a := ap.a
	for _, inv := range invs {
		net, vat := money.Zero(ap.cur), money.Zero(ap.cur)
		var lines []sales.InvoiceLine
		for _, l := range inv.Lines {
			net, _ = net.Add(l.Net)
			vat, _ = vat.Add(l.VAT)
			lines = append(lines, sales.InvoiceLine{Description: l.Description, Quantity: decimal.MustParse("1"), UnitPrice: l.Net, VATRate: l.VATRate, Recharge: l.Recharge})
		}
		gross, _ := net.Add(vat)
		// Keep the source's invoice number as the reference; fall back to the
		// app's own when the source has none or it is already in use.
		ref := inv.Ref
		if _, taken := a.invoiceDocs[ref]; ref == "" || taken {
			ref = a.ref("INV")
			if inv.Ref != "" {
				ap.issue("invoice %s: number already in use; posted as %s", inv.Ref, ref)
			}
		}
		op := sales.Invoice{Date: inv.Date, Ref: ref, Customer: inv.Customer, Amount: net, VAT: vat}
		if !ap.post("sales", "Sales invoices", op) {
			continue
		}
		if _, err := a.sl.Raise(ref, inv.Customer, inv.Date, gross); err != nil {
			ap.issue("invoice %s: %v", inv.Ref, err)
		}
		a.invoiceDocs[ref] = &invoiceDoc{Ref: ref, Customer: inv.Customer, Date: inv.Date, Lines: lines, Net: net, VAT: vat, Gross: gross}
		a.invoiceOrder = append(a.invoiceOrder, ref)
		ap.refs[inv.Ref] = ref
	}
}

// ratioContext divides to 12 significant digits: enough for a VAT proportion
// that is then rounded to the penny.
var ratioContext = decimal.Context{Precision: 12, Rounding: decimal.RoundHalfEven}

// vatShare splits a gross amount into net and VAT in the same proportion as
// a document's totals.
func vatShare(gross, docNet, docVAT money.Money) (net, vat money.Money) {
	cur := gross.Currency()
	if !docVAT.IsPositive() || !docNet.IsPositive() {
		return gross, money.Zero(cur)
	}
	docGross, _ := docNet.Add(docVAT)
	ratio, cond := ratioContext.Divide(docVAT.Amount(), docGross.Amount())
	if cond.Has(decimal.DivisionByZero) || cond.Has(decimal.InvalidOperation) {
		return gross, money.Zero(cur)
	}
	vat, err := gross.Mul(ratio, money.HalfUp)
	if err != nil {
		return gross, money.Zero(cur)
	}
	net, _ = gross.Sub(vat)
	return net, vat
}

func (ap *batchApplier) creditNotes(cns []importer.CreditNote) {
	a := ap.a
	for _, cn := range cns {
		ref := cn.Ref
		if ref == "" {
			ref = a.ref("CN")
		}
		narr := "Credit note " + ref
		appRef, known := ap.refs[cn.Invoice]
		net, vat := cn.Gross, money.Zero(ap.cur)
		if doc := a.invoiceDocs[appRef]; known && doc != nil {
			net, vat = vatShare(cn.Gross, doc.Net, doc.VAT)
			narr += " against " + appRef
		}
		postings := []ledger.Posting{{Account: chart.Sales, Side: ledger.Debit, Amount: net}}
		if vat.IsPositive() {
			postings = append(postings, ledger.Posting{Account: chart.VAT, Side: ledger.Debit, Amount: vat})
		}
		postings = append(postings, ledger.Posting{Account: chart.TradeDebtors, Side: ledger.Credit, Amount: cn.Gross})
		if ap.raw("sales", "Credit notes", cn.Date, ref, narr, postings...) && known {
			if err := a.sl.Allocate(appRef, cn.Gross); err != nil {
				ap.issue("credit note %s against %s: %v", cn.Ref, cn.Invoice, err)
			}
		}
	}
}

func (ap *batchApplier) receipts(rs []importer.Receipt) {
	a := ap.a
	for _, r := range rs {
		ref := a.ref("REC")
		bank := ap.code(r.Bank, chart.Cash)
		if !ap.post("sales", "Receipts", sales.Receipt{Date: r.Date, Ref: ref, Amount: r.Amount, Bank: bank}) {
			continue
		}
		if r.CcyAmount.IsPositive() && a.bankCurrency(bank).Code == r.CcyAmount.Currency().Code && a.isForeign(bank) {
			a.addFX(bank, r.CcyAmount)
		}
		if appRef, ok := ap.refs[r.Invoice]; ok {
			if err := a.sl.Allocate(appRef, r.Amount); err != nil {
				ap.issue("receipt %s against %s: %v", ref, r.Invoice, err)
			}
		}
	}
}

// categoryAccount maps a source cost category to an expense account.
func categoryAccount(category string) string {
	c := strings.ToLower(category)
	switch {
	case strings.Contains(c, "accountan"):
		return chart.Accountancy
	case strings.Contains(c, "subcontract"):
		return chart.CostOfSales
	case strings.Contains(c, "travel"), strings.Contains(c, "subsistence"), strings.Contains(c, "mileage"), strings.Contains(c, "accommodation"), strings.Contains(c, "hotel"):
		return chart.Travel
	}
	return chart.OfficeAdmin
}

func (ap *batchApplier) bills(bills []importer.Bill) {
	a := ap.a
	for _, b := range bills {
		net, vat := b.Net, b.VAT
		if !ap.vatReg {
			net, _ = net.Add(vat)
			vat = money.Zero(ap.cur)
		}
		gross, _ := net.Add(vat)
		if !gross.IsPositive() {
			continue
		}
		account := categoryAccount(b.Category)
		if b.Category != "" {
			if ac, ok := a.book.Account(account); ok {
				ap.mapped[b.Category] = ac.Name
			}
		}
		ref := a.ref("BILL")
		desc := b.Supplier
		if b.Description != "" {
			desc += " — " + b.Description
		}
		if !ap.post("expenses", "Bills", expenses.Bill{Date: b.Date, Ref: ref, Supplier: desc, Amount: net, VAT: vat, Expense: account}) {
			continue
		}
		if _, err := a.purch.Record(ref, b.Supplier, b.Date, gross); err != nil {
			ap.issue("bill %s: %v", ref, err)
		}
		a.costs = append(a.costs, &costRecord{Ref: ref, Desc: desc, Date: b.Date, Net: net, Recharged: b.Recharge != ""})
		if b.Credited.IsPositive() {
			cnet, cvat := vatShare(b.Credited, net, vat)
			if ap.post("expenses", "Supplier credit notes", expenses.CreditNote{Date: b.Date, Ref: a.ref("PCN"), Supplier: b.Supplier, Amount: cnet, VAT: cvat, Expense: account}) {
				a.purch.Allocate(ref, b.Credited)
			}
		}
		if !b.Paid.IsPositive() {
			continue
		}
		var paid bool
		switch b.PaidBy {
		case importer.Bank:
			paid = ap.post("expenses", "Supplier payments", expenses.Payment{Date: b.Date, Ref: a.ref("SPAY"), Amount: b.Paid, Bank: ap.code(b.PaidFrom, a.main())})
		case importer.PettyCash:
			paid = ap.post("expenses", "Supplier payments", expenses.Payment{Date: b.Date, Ref: a.ref("SPAY"), Amount: b.Paid, Bank: chart.Cash})
		case importer.Director:
			paid = ap.raw("expenses", "Costs paid by the director", b.Date, a.ref("SPAY"), "Paid personally by the director — "+b.Supplier,
				ledger.Posting{Account: chart.TradeCreditors, Side: ledger.Debit, Amount: b.Paid},
				ledger.Posting{Account: chart.DirectorsLoan, Side: ledger.Credit, Amount: b.Paid})
		}
		if paid {
			a.purch.Allocate(ref, b.Paid)
		}
	}
}

func (ap *batchApplier) tax(payments []importer.TaxPayment, rebates []importer.TaxRebate) {
	a := ap.a
	for _, p := range payments {
		bank := ap.code(p.Bank, a.main())
		switch p.Kind {
		case importer.CorporationTax:
			// The charge itself is not in an export: provide for it at the end of
			// the year the payment settles, the one before the payment.
			fy := a.co.YearContaining(p.Date)
			prevEnd := ledger.NewDate(fy.Start.Year, fy.Start.Month, fy.Start.Day)
			prevEnd = dayBefore(prevEnd)
			if a.co.Incorporated.Before(prevEnd) {
				ap.post("company-tax", "Corporation tax provisions", companytax.Provision{Date: prevEnd, Ref: a.ref("CTC"), Amount: p.Amount})
				ap.note("Each corporation tax payment is matched by a charge of the same amount at the previous year end; adjust if a payment was not the full charge.")
			}
			ap.post("company-tax", "Corporation tax payments", companytax.Payment{Date: p.Date, Ref: a.ref("CTP"), Amount: p.Amount, Bank: bank})
		case importer.PAYE:
			ap.post("pay-yourself", "PAYE/NIC payments", payyourself.PayPAYE{Date: p.Date, Ref: a.ref("PAYE"), Amount: p.Amount, Bank: bank})
		case importer.VATTax:
			ap.raw("company-tax", "VAT payments", p.Date, a.ref("VATP"), "VAT payment to HMRC",
				ledger.Posting{Account: chart.VAT, Side: ledger.Debit, Amount: p.Amount},
				ledger.Posting{Account: bank, Side: ledger.Credit, Amount: p.Amount})
		default:
			ap.issue("tax payment of %s on %s: unknown tax kind, not posted", p.Amount, p.Date)
		}
	}
	for _, r := range rebates {
		liability := ""
		switch r.Kind {
		case importer.PAYE:
			liability = chart.PAYENIC
		case importer.CorporationTax:
			liability = chart.CorpTaxPayable
		case importer.VATTax:
			liability = chart.VAT
		default:
			liability = chart.OtherIncome
		}
		into, narr := ap.code(r.Bank, a.main()), r.Kind.String()+" rebate from HMRC"
		if r.ToDirector {
			into, narr = chart.DirectorsLoan, narr+", received by the director"
		}
		ap.raw("company-tax", "Tax rebates", r.Date, a.ref("REB"), narr,
			ledger.Posting{Account: into, Side: ledger.Debit, Amount: r.Amount},
			ledger.Posting{Account: liability, Side: ledger.Credit, Amount: r.Amount})
	}
}

func dayBefore(d ledger.Date) ledger.Date {
	t := time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, time.UTC).AddDate(0, 0, -1)
	return ledger.NewDate(t.Year(), t.Month(), t.Day())
}
