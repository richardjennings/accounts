package main

import (
	"bytes"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/richardjennings/accounts/chart"
	"github.com/richardjennings/accounts/frs105"
	"github.com/richardjennings/accounts/ledger"
	"github.com/richardjennings/accounts/money"
	"github.com/richardjennings/accounts/report"
	"github.com/richardjennings/accounts/tax/payroll"
	"github.com/richardjennings/accounts/yearend"
)

func integrityApp(t *testing.T) *app {
	t.Helper()
	a, err := newApp("")
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func integrityInvoice(t *testing.T, a *app) string {
	t.Helper()
	drive(t, a.routes(), "/sales/invoices/raise", url.Values{"date": {"2026-06-01"}, "price0": {"100.00"}, "desc0": {"Work"}})
	if len(a.sl.Invoices()) != 1 {
		t.Fatal("invoice setup failed")
	}
	return a.sl.Invoices()[0].Ref
}

func TestRegressionRejectedReceiptLeavesInvoiceUntouched(t *testing.T) {
	a := integrityApp(t)
	ref := integrityInvoice(t, a)
	a.closedThrough = a.fy().End
	n := len(a.book.Journals())
	drive(t, a.routes(), "/sales/receipts/record", url.Values{"date": {"2026-06-02"}, "invoice": {ref}, "amount": {"100.00"}})
	inv, _ := a.sl.Get(ref)
	if !inv.Paid().IsZero() || len(a.book.Journals()) != n {
		t.Errorf("rejected receipt: invoice paid=%s, ledger journals added=%d, flash=%s", inv.Paid(), len(a.book.Journals())-n, a.flash)
	}
}

func TestRegressionCreditNoteUpdatesInvoice(t *testing.T) {
	a := integrityApp(t)
	ref := integrityInvoice(t, a)
	drive(t, a.routes(), "/sales/credit-notes/record", url.Values{"date": {"2026-06-02"}, "invoice": {ref}, "amount": {"100.00"}})
	inv, _ := a.sl.Get(ref)
	if !inv.Outstanding().Equal(a.bal(chart.TradeDebtors)) {
		t.Errorf("after full credit: invoice outstanding=%s, control account=%s", inv.Outstanding(), a.bal(chart.TradeDebtors))
	}
}

func TestRegressionClosePreservesHistoricalProfit(t *testing.T) {
	a := integrityApp(t)
	integrityInvoice(t, a)
	fy := a.fy()
	drive(t, a.routes(), "/company/close-year", nil)
	pl, err := report.NewProfitAndLoss(a.book, fy.Start, fy.End)
	if err != nil {
		t.Fatal(err)
	}
	acc, err := frs105.Build(a.book, a.co, a.fy(), frs105.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if pl.Profit.String() != "GBP 100.00" || acc.Prior.ProfitForYear.String() != "GBP 100.00" {
		t.Errorf("closed-year profit=%s; next-year comparative profit=%s; both should be GBP 100.00", pl.Profit, acc.Prior.ProfitForYear)
	}
}

func TestRegressionBreakEvenYearCanClose(t *testing.T) {
	a := integrityApp(t)
	h := a.routes()
	drive(t, h, "/sales/cash/record", url.Values{"amount": {"100.00"}})
	drive(t, h, "/expenses/direct/record", url.Values{"amount": {"100.00"}, "account": {chart.OfficeAdmin}})
	_, err := yearend.CloseEntry(a.book, a.fy().End, "YE", chart.RetainedEarnings)
	if err != nil {
		t.Errorf("break-even close failed: %v", err)
	}
}

func TestRegressionVATPaymentIsNotInputVAT(t *testing.T) {
	a := integrityApp(t)
	a.co.VATRegistered = true
	h := a.routes()
	drive(t, h, "/accounting/journals/post", url.Values{"amount": {"20.00"}, "debit": {chart.VAT}, "credit": {chart.Bank}})
	r, err := a.vatReturn()
	if err != nil {
		t.Fatal(err)
	}
	if !r.Box4.IsZero() {
		t.Errorf("VAT payment creates input VAT=%s, reclaim=%s", r.Box4, r.Box5)
	}
}

func TestRegressionPurchaseCreditReducesInputVAT(t *testing.T) {
	a := integrityApp(t)
	a.co.VATRegistered = true
	h := a.routes()
	drive(t, h, "/expenses/bills/record", url.Values{"amount": {"100.00"}, "vat": {"standard"}, "account": {chart.OfficeAdmin}})
	drive(t, h, "/expenses/credit-notes/record", url.Values{"bill": {a.purch.Bills()[0].Ref}, "amount": {"100.00"}, "vat": {"standard"}, "account": {chart.OfficeAdmin}})
	r, err := a.vatReturn()
	if err != nil {
		t.Fatal(err)
	}
	if !r.Box1.IsZero() || !r.Box4.IsZero() {
		t.Errorf("fully credited purchase: Box1=%s, Box4=%s, both should be zero", r.Box1, r.Box4)
	}
}

func TestRegressionCorruptSaveIsPreserved(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.json")
	original := []byte("{broken save")
	if err := os.WriteFile(p, original, 0600); err != nil {
		t.Fatal(err)
	}
	_, err := newApp(p)
	after, readErr := os.ReadFile(p)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if err == nil || !bytes.Equal(original, after) {
		t.Errorf("opening corrupt save: error=%v, original overwritten=%v", err, !bytes.Equal(original, after))
	}
}

func TestRegressionClosedPeriodBlocksDepreciation(t *testing.T) {
	a := integrityApp(t)
	h := a.routes()
	drive(t, h, "/accounting/fixed-assets/acquire", url.Values{"amount": {"100.00"}, "life": {"5"}})
	a.closedThrough = a.fy().End
	n := len(a.book.Journals())
	drive(t, h, "/accounting/fixed-assets/depreciate", nil)
	if len(a.book.Journals()) != n {
		t.Errorf("posted %d depreciation journals dated %s into period closed through %s", len(a.book.Journals())-n, a.today, a.closedThrough)
	}
}

func TestRegressionSpecialTaxCodes(t *testing.T) {
	for _, c := range []struct{ code, gross, want string }{{"BR", "60000.00", "GBP 12000.00"}, {"D0", "30000.00", "GBP 12000.00"}, {"D1", "30000.00", "GBP 13500.00"}} {
		r, err := payroll.Compute(payroll.Input{GrossAnnual: money.MustParse(money.GBP, c.gross), TaxCode: c.code, Rates: payroll.Year2026_27})
		if err != nil {
			t.Fatal(err)
		}
		if r.IncomeTax.String() != c.want {
			t.Errorf("%s on %s: tax=%s, want %s", c.code, c.gross, r.IncomeTax, c.want)
		}
	}
}

func TestRegressionPayrollRunsUseAnnualTotals(t *testing.T) {
	a := integrityApp(t)
	h := a.routes()
	for _, d := range []string{"2026-06-01", "2026-07-01"} {
		drive(t, h, "/pay-yourself/salary/run", url.Values{"amount": {"30000.00"}, "date": {d}})
	}
	got, _ := a.runs[0].Result.IncomeTax.Add(a.runs[1].Result.IncomeTax)
	whole, err := payroll.Compute(payroll.Input{GrossAnnual: money.MustParse(money.GBP, "60000.00"), Rates: payroll.Year2026_27})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(whole.IncomeTax) {
		t.Errorf("two runs tax=%s; annual tax on same gross=%s", got, whole.IncomeTax)
	}
}

func TestRegressionMileageUsesPreviousClaims(t *testing.T) {
	a := integrityApp(t)
	h := a.routes()
	for i := 0; i < 2; i++ {
		drive(t, h, "/expenses/mileage/record", url.Values{"miles": {"10000"}})
	}
	if a.bal(chart.Travel).String() != "GBP 8000.00" {
		t.Errorf("20000 miles in two claims cost %s; 10000 at 55p + 10000 at 25p should be GBP 8000.00", a.bal(chart.Travel))
	}
}

func TestRegressionConcurrentSave(t *testing.T) {
	a := integrityApp(t)
	a.dataPath = filepath.Join(t.TempDir(), "state.json")
	a.costs = []*costRecord{{Ref: "EXP-1", Net: money.MustParse(money.GBP, "100.00"), Date: ledger.NewDate(2026, time.June, 1)}}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 10000; i++ {
			a.mu.Lock()
			a.costs[0].Recharged = !a.costs[0].Recharged
			a.mu.Unlock()
		}
	}()
	for i := 0; i < 20; i++ {
		a.save()
	}
	wg.Wait()
}
