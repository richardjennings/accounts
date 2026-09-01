package main

import (
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/richardjennings/accounts/chart"
	"github.com/richardjennings/accounts/ledger"
	"github.com/richardjennings/accounts/money"
	"github.com/richardjennings/accounts/tax/corporationtax"
)

// TestCTEstimateUsesCurrentYear books a closed year with its own tax charge and
// depreciation, then checks the estimate for the open year ignores both.
func TestCTEstimateUsesCurrentYear(t *testing.T) {
	a, err := newApp(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	a.co.Incorporated = ledger.NewDate(2025, time.April, 1)
	h := a.persistMiddleware(a.routes())
	journal := func(date, debit, credit, amount string) {
		drive(t, h, "/accounting/journals/post", url.Values{"date": {date}, "debit": {debit}, "credit": {credit}, "amount": {amount}, "narrative": {"test"}})
	}
	// Year to 31 March 2026: £10,000 of sales, £300 depreciation, £1,900 tax.
	journal("2025-06-01", chart.Bank, chart.Sales, "10000.00")
	journal("2025-09-01", chart.Depreciation, chart.AccumulatedDepreciation, "300.00")
	journal("2026-03-31", chart.CorpTaxCharge, chart.CorpTaxPayable, "1900.00")
	// Year to 31 March 2027, the open year: £5,000 of sales, £200 depreciation.
	journal("2026-05-01", chart.Bank, chart.Sales, "5000.00")
	journal("2026-05-15", chart.Depreciation, chart.AccumulatedDepreciation, "200.00")

	if got := a.profitBeforeTax().String(); got != "GBP 4800.00" {
		t.Errorf("profit before tax = %s, want GBP 4800.00", got)
	}
	if got := a.taxableProfit().String(); got != "GBP 5000.00" {
		t.Errorf("taxable profit = %s, want GBP 5000.00", got)
	}
	taxable, _ := money.Parse(money.GBP, "5000.00")
	want, err := corporationtax.Compute(corporationtax.Input{FinancialYear: 2026, TaxableProfit: taxable})
	if err != nil {
		t.Fatal(err)
	}
	if got := a.estimateCT().Charge; got.String() != want.Charge.String() {
		t.Errorf("estimated charge = %s, want %s", got, want.Charge)
	}

	// Providing posts the open year's charge; providing again adds nothing.
	for i := 0; i < 2; i++ {
		drive(t, h, "/company-tax/provide", url.Values{"date": {"2026-06-01"}})
		if got := a.fyMovement(chart.CorpTaxCharge); got.String() != want.Charge.String() {
			t.Errorf("provision %d: charge this year = %s, want %s", i+1, got, want.Charge)
		}
	}
	if got := a.profitBeforeTax().String(); got != "GBP 4800.00" {
		t.Errorf("profit before tax after provision = %s, want GBP 4800.00", got)
	}

	body := page(t, h, "/company-tax")
	for _, s := range []string{"£4,800.00", "£5,000.00", "£200.00", "Provided this year"} {
		if !strings.Contains(body, s) {
			t.Errorf("company tax page lacks %q", s)
		}
	}
}
