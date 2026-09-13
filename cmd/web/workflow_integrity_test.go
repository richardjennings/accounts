package main

import (
	"bytes"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/richardjennings/accounts/chart"
	"github.com/richardjennings/accounts/importer"
	"github.com/richardjennings/accounts/ledger"
	"github.com/richardjennings/accounts/money"
	"github.com/richardjennings/accounts/report"
)

func savedApp(t *testing.T) *app {
	t.Helper()
	a, err := newApp(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func reloadApp(t *testing.T, a *app) *app {
	t.Helper()
	if err := a.save(); err != nil {
		t.Fatal(err)
	}
	b, err := newApp(a.dataPath)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestClosedReportsAndLockSurviveReload(t *testing.T) {
	for _, expense := range []string{"400.00", "1000.00"} {
		t.Run(expense, func(t *testing.T) {
			a := savedApp(t)
			h := a.routes()
			drive(t, h, "/sales/cash/record", url.Values{"amount": {"1000.00"}})
			drive(t, h, "/expenses/direct/record", url.Values{"amount": {expense}, "account": {chart.OfficeAdmin}})
			fy := a.fy()
			before, _ := report.NewProfitAndLoss(a.book, fy.Start, fy.End)
			drive(t, h, "/company/close-year", nil)
			a = reloadApp(t, a)
			acc, err := a.accounts()
			if err != nil {
				t.Fatal(err)
			}
			if acc.Prior == nil || !acc.Prior.ProfitForYear.Equal(before.Profit) {
				t.Fatal("close/reload changed comparative profit")
			}
			if !a.bal(chart.Sales).IsZero() || !a.bal(chart.OfficeAdmin).IsZero() {
				t.Fatal("close did not clear P&L accounts")
			}
			j, _ := ledger.NewJournal(fy.End, "late posting", ledger.Posting{Account: chart.Bank, Side: ledger.Debit, Amount: money.MustParse(money.GBP, "1.00")}, ledger.Posting{Account: chart.Sales, Side: ledger.Credit, Amount: money.MustParse(money.GBP, "1.00")})
			if err := a.book.Post(j); err == nil {
				t.Fatal("restored book accepted a closed-period posting")
			}
		})
	}
}

func TestCreditsAndSettlementsStayReconciledAfterReload(t *testing.T) {
	a := savedApp(t)
	h := a.routes()
	drive(t, h, "/sales/invoices/raise", url.Values{"price0": {"100.00"}, "vat0": {"standard"}, "desc0": {"Work"}})
	ref := a.sl.Invoices()[0].Ref
	drive(t, h, "/sales/credit-notes/record", url.Values{"invoice": {ref}, "amount": {"25.00"}, "vat": {"standard"}})
	drive(t, h, "/sales/receipts/record", url.Values{"invoice": {ref}, "amount": {"90.00"}})
	drive(t, h, "/expenses/bills/record", url.Values{"amount": {"100.00"}, "vat": {"standard"}, "account": {chart.OfficeAdmin}})
	bill := a.purch.Bills()[0].Ref
	drive(t, h, "/expenses/credit-notes/record", url.Values{"bill": {bill}, "amount": {"25.00"}, "vat": {"standard"}, "account": {chart.OfficeAdmin}})
	drive(t, h, "/expenses/payments/record", url.Values{"bill": {bill}, "amount": {"90.00"}})
	n := len(a.entries)
	drive(t, h, "/sales/credit-notes/record", url.Values{"invoice": {ref}, "amount": {"1.00"}})
	drive(t, h, "/expenses/credit-notes/record", url.Values{"bill": {bill}, "amount": {"1.00"}})
	if len(a.entries) != n {
		t.Fatal("over-credit posted after settlement")
	}
	a = reloadApp(t, a)
	inv, _ := a.sl.Get(ref)
	b, _ := a.purch.Get(bill)
	if inv.Credited().String() != "GBP 30.00" || inv.Paid().String() != "GBP 90.00" || !inv.Outstanding().IsZero() {
		t.Fatalf("invoice: paid=%s credited=%s outstanding=%s", inv.Paid(), inv.Credited(), inv.Outstanding())
	}
	if b.Credited().String() != "GBP 30.00" || b.Paid().String() != "GBP 90.00" || !b.Outstanding().IsZero() {
		t.Fatal("bill credits/payments did not survive reload")
	}
	if !a.bal(chart.TradeDebtors).IsZero() || !a.bal(chart.TradeCreditors).IsZero() {
		t.Fatal("subsidiaries diverged from control accounts")
	}
	vr, err := a.vatReturn()
	if err != nil {
		t.Fatal(err)
	}
	if vr.Box1.String() != "GBP 15.00" || vr.Box4.String() != "GBP 15.00" || vr.Box6.String() != "GBP 75.00" || vr.Box7.String() != "GBP 75.00" {
		t.Fatalf("VAT after credits and cash settlements: %+v", vr)
	}
}

func TestRejectedOperationsLeaveRelatedRecordsUnchanged(t *testing.T) {
	for _, c := range []struct {
		path string
		form url.Values
	}{
		{"/expenses/bills/record", url.Values{"amount": {"100.00"}, "account": {"missing"}}},
		{"/expenses/direct/record", url.Values{"amount": {"100.00"}, "bank": {"missing"}}},
		{"/accounting/fixed-assets/acquire", url.Values{"amount": {"100.00"}, "life": {"3"}, "date": {"2026-05-01"}}},
		{"/sales/invoices/raise", url.Values{"price0": {"100.00"}, "desc0": {"Work"}, "date": {"2026-05-01"}}},
	} {
		t.Run(c.path, func(t *testing.T) {
			a := integrityApp(t)
			a.book.CloseThrough(ledger.NewDate(2026, 5, 31))
			n := len(a.entries)
			drive(t, a.routes(), c.path, c.form)
			if len(a.entries) != n || len(a.sl.Invoices()) != 0 || len(a.purch.Bills()) != 0 || len(a.costs) != 0 || len(a.assets) != 0 {
				t.Fatalf("rejected operation left records behind: %s", a.flash)
			}
		})
	}
	// Payroll must not create a payslip if its journal cannot be posted.
	a := integrityApp(t)
	a.mainBank = "missing"
	drive(t, a.routes(), "/pay-yourself/salary/run", url.Values{"amount": {"30000.00"}})
	if len(a.runs) != 0 {
		t.Fatal("failed payroll created a payslip")
	}
}

func TestDepreciationOncePerYearAcrossReloads(t *testing.T) {
	a := savedApp(t)
	drive(t, a.routes(), "/accounting/fixed-assets/acquire", url.Values{"amount": {"100.00"}, "life": {"3"}})
	for _, date := range []string{"2026-06-01", "2027-06-01", "2028-06-01"} {
		h := a.routes()
		drive(t, h, "/company/date", url.Values{"date": {date}})
		drive(t, h, "/accounting/fixed-assets/depreciate", nil)
		n := len(a.entries)
		a = reloadApp(t, a)
		drive(t, a.routes(), "/accounting/fixed-assets/depreciate", nil)
		if len(a.entries) != n {
			t.Fatal("repeat depreciation after reload posted twice")
		}
	}
	if a.assets[0].Accumulated.String() != "GBP 100.00" || a.assets[0].DepreciationYears != 3 {
		t.Fatal("final-year depreciation did not clear rounding remainder")
	}
}

func TestMileageUsesClaimantJourneyYearAndSavedHistory(t *testing.T) {
	a := savedApp(t)
	for _, c := range []struct{ person, date string }{{"A", "2026-03-01"}, {"A", "2026-06-01"}, {"A", "2026-06-02"}, {"B", "2026-06-02"}} {
		drive(t, a.routes(), "/expenses/mileage/record", url.Values{"person": {c.person}, "date": {c.date}, "miles": {"10000"}})
		a = reloadApp(t, a)
	}
	if a.bal(chart.Travel).String() != "GBP 18000.00" {
		t.Fatalf("mileage = %s", a.bal(chart.Travel))
	}
	n := len(a.entries)
	drive(t, a.routes(), "/expenses/mileage/record", url.Values{"person": {"A"}, "date": {"2026-05-01"}, "miles": {"10000"}})
	if len(a.entries) != n {
		t.Fatal("backdated mileage changed already assessed claims")
	}
}

func TestSaveBackupFailureAndVersionProtection(t *testing.T) {
	a := savedApp(t)
	before, err := os.ReadFile(a.dataPath)
	if err != nil {
		t.Fatal(err)
	}
	a.co.Name = "Changed"
	if err := a.save(); err != nil {
		t.Fatal(err)
	}
	backup, err := os.ReadFile(a.dataPath + ".bak")
	if err != nil || !bytes.Equal(backup, before) {
		t.Fatal("previous save was not preserved as a backup")
	}
	for _, content := range []string{`{"Version":999}`, `{}`, `{"Version":-1}`} {
		p := filepath.Join(t.TempDir(), "state.json")
		if err := os.WriteFile(p, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := newApp(p); err == nil {
			t.Fatalf("accepted invalid save %s", content)
		}
		after, _ := os.ReadFile(p)
		if string(after) != content {
			t.Fatal("invalid save overwritten")
		}
	}
	// A failed save remains visible when the next page renders.
	a.dataPath = filepath.Join(a.dataPath, "impossible.json")
	drive(t, a.persistMiddleware(a.routes()), "/sales/cash/record", url.Values{"amount": {"10.00"}})
	if !strings.Contains(a.flash, "could not be saved") {
		t.Fatalf("save failure hidden: %s", a.flash)
	}
}

func TestConcurrentSavesKeepLatestState(t *testing.T) {
	a := savedApp(t)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a.mu.Lock()
			a.seq++
			a.mu.Unlock()
			if err := a.save(); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	b, err := newApp(a.dataPath)
	if err != nil {
		t.Fatal(err)
	}
	if b.seq != a.seq {
		t.Fatalf("latest save lost updates: saved=%d current=%d", b.seq, a.seq)
	}
}

func TestLegacyCloseMigrationAndRestoreIsAtomic(t *testing.T) {
	a := integrityApp(t)
	drive(t, a.routes(), "/sales/cash/record", url.Values{"amount": {"100.00"}})
	fy := a.fy()
	drive(t, a.routes(), "/company/close-year", nil)
	s := a.buildSnapshot()
	s.Version = 0
	for i := range s.Entries {
		s.Entries[i].Closing = false
	}
	b := integrityApp(t)
	if err := b.restore(&s); err != nil {
		t.Fatal(err)
	}
	pl, _ := report.NewProfitAndLoss(b.book, fy.Start, fy.End)
	if pl.Profit.String() != "GBP 100.00" {
		t.Fatal("legacy closing entry not classified")
	}
	before, _ := json.Marshal(b.buildSnapshot())
	s.SalesInvoices = []invoiceLedgerDTO{{Ref: "broken", Date: fy.Start, Total: money.MustParse(money.GBP, "10.00"), Paid: money.MustParse(money.GBP, "11.00")}}
	if err := b.restore(&s); err == nil {
		t.Fatal("invalid subsidiary was restored")
	}
	after, _ := json.Marshal(b.buildSnapshot())
	if !bytes.Equal(before, after) {
		t.Fatal("failed restore partially changed the company")
	}
}

func TestImportRejectsOversizedAllocationsBeforePosting(t *testing.T) {
	a := integrityApp(t)
	ref := integrityInvoice(t, a)
	ap := batchApplier{a: a, rep: &importReport{}, refs: map[string]string{"source": ref}}
	n := len(a.entries)
	tooMuch := money.MustParse(money.GBP, "150.00")
	ap.receipts([]importer.Receipt{{Invoice: "source", Amount: tooMuch, Date: a.today}})
	ap.creditNotes([]importer.CreditNote{{Invoice: "source", Gross: tooMuch, Date: a.today}})
	if len(a.entries) != n || len(ap.rep.Issues) != 2 {
		t.Fatal("invalid import allocation was posted or not reported")
	}
	inv, _ := a.sl.Get(ref)
	if inv.Outstanding().String() != "GBP 100.00" || a.bal(chart.TradeDebtors).String() != "GBP 100.00" {
		t.Fatal("invalid import changed the receivables")
	}
}

func TestVATUsesConfiguredQuarter(t *testing.T) {
	a := integrityApp(t)
	a.co.VATQuarterEndMonth = 2
	a.today = ledger.NewDate(2026, 3, 1)
	for _, date := range []string{"2026-02-28", "2026-03-01", "2026-06-01"} {
		drive(t, a.routes(), "/sales/cash/record", url.Values{"date": {date}, "amount": {"100.00"}, "vat": {"standard"}})
	}
	r, err := a.vatReturn()
	if err != nil {
		t.Fatal(err)
	}
	if r.From.String() != "2026-03-01" || r.To.String() != "2026-05-31" || r.Box1.String() != "GBP 20.00" || r.Box6.String() != "GBP 100.00" {
		t.Fatalf("wrong staggered VAT return: %+v", r)
	}
}

func TestImportBillCreditAndPaymentValidateIndependently(t *testing.T) {
	for _, paidBy := range []importer.PaidBy{importer.Bank, importer.PettyCash, importer.Director} {
		for _, tc := range []struct {
			name, credited, paid, wantCredit, wantPaid, wantOutstanding string
			issues                                                      int
		}{
			{"rejected credit keeps payment", "121.00", "120.00", "0.00", "120.00", "0.00", 1},
			{"rejected credit and payment", "121.00", "121.00", "0.00", "0.00", "120.00", 2},
			{"valid credit and payment", "30.00", "90.00", "30.00", "90.00", "0.00", 0},
		} {
			t.Run(tc.name+"/"+strconv.Itoa(int(paidBy)), func(t *testing.T) {
				a := integrityApp(t)
				amount := func(s string) money.Money { return money.MustParse(money.GBP, s) }
				cashBefore, bankBefore, loanBefore := a.bal(chart.Cash), a.bal(chart.Bank), a.bal(chart.DirectorsLoan)
				rep := a.applyBatch("test", &importer.Batch{VATCharged: true, Bills: []importer.Bill{{
					Date: a.today, Supplier: "Supplier", Net: amount("100.00"), VAT: amount("20.00"),
					Credited: amount(tc.credited), Paid: amount(tc.paid), PaidBy: paidBy,
				}}}, nil)
				if len(rep.Issues) != tc.issues {
					t.Fatalf("issues = %v, want %d", rep.Issues, tc.issues)
				}
				bills := a.purch.Bills()
				if len(bills) != 1 {
					t.Fatalf("bills = %d, want 1", len(bills))
				}
				bill := bills[0]
				if !bill.Credited().Equal(amount(tc.wantCredit)) || !bill.Paid().Equal(amount(tc.wantPaid)) || !bill.Outstanding().Equal(amount(tc.wantOutstanding)) {
					t.Fatalf("credited=%s paid=%s outstanding=%s", bill.Credited(), bill.Paid(), bill.Outstanding())
				}
				if !bill.Outstanding().Equal(a.bal(chart.TradeCreditors)) {
					t.Fatal("bill outstanding differs from creditors control")
				}
				var movement money.Money
				switch paidBy {
				case importer.Bank:
					movement, _ = bankBefore.Sub(a.bal(chart.Bank))
				case importer.PettyCash:
					movement, _ = cashBefore.Sub(a.bal(chart.Cash))
				case importer.Director:
					movement, _ = a.bal(chart.DirectorsLoan).Sub(loanBefore)
				}
				if !movement.Equal(amount(tc.wantPaid)) {
					t.Fatalf("payment movement = %s, want %s", movement, tc.wantPaid)
				}
			})
		}
	}
}

func TestLegacyDepreciationCannotChargeSameYearAgain(t *testing.T) {
	a := integrityApp(t)
	drive(t, a.routes(), "/accounting/fixed-assets/acquire", url.Values{"amount": {"100.00"}, "life": {"3"}})
	drive(t, a.routes(), "/accounting/fixed-assets/depreciate", nil)
	s := a.buildSnapshot()
	s.Version = 0
	s.Assets[0].DepreciatedThrough = ledger.Date{}
	s.Assets[0].DepreciationYears = 0
	b := integrityApp(t)
	if err := b.restore(&s); err != nil {
		t.Fatal(err)
	}
	n := len(b.entries)
	drive(t, b.routes(), "/accounting/fixed-assets/depreciate", nil)
	if len(b.entries) != n || b.assets[0].Accumulated.String() != "GBP 33.33" {
		t.Fatal("legacy depreciation charged twice")
	}
}
