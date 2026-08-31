package main

import (
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/richardjennings/accounts/chart"
)

// TestDollarLifecycle runs a USD account through Stage 1 over the real router:
// create the account, invoice in GBP, receive dollars with a realised exchange
// loss, convert part of them to GBP with a realised gain, import a USD
// statement and reconcile it against the currency balance, and restart.
func TestDollarLifecycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	a, err := newApp(path)
	if err != nil {
		t.Fatal(err)
	}
	h := a.persistMiddleware(a.routes())

	drive(t, h, "/banking/accounts/add", url.Values{"name": {"USD Wise"}, "currency": {"usd"}})
	usd := a.banks[len(a.banks)-1]
	if usd.Currency != "USD" {
		t.Fatalf("account currency: %+v", usd)
	}

	// An invoice booked at £750 via the CSV import path.
	drive(t, h, "/company/import/run", url.Values{"kind": {"invoices"}, "csv": {"Issue date,Client,Invoice Total,Includes VAT?\n01/06/2026,Widgets Inc,750.00,No\n"}})
	inv := a.invoiceOrder[0]

	// $1000 arrives, worth £738 today: the £12 shortfall is a realised loss.
	drive(t, h, "/sales/receipts/record", url.Values{"invoice": {inv}, "amount": {"738.00"}, "ccy_amount": {"1000.00"}, "settle": {"1"}, "bank": {usd.Code}, "date": {"2026-06-05"}})
	if got := a.fxBal(usd.Code).String(); got != "USD 1000.00" {
		t.Errorf("USD balance = %s", got)
	}
	if got := a.bal(usd.Code).String(); got != "GBP 738.00" {
		t.Errorf("carrying = %s", got)
	}
	if got := a.bal(chart.ExchangeDiff).String(); got != "GBP -12.00" {
		t.Errorf("FX after receipt = %s (want a £12 loss)", got)
	}
	if inv2, _ := a.sl.Get(inv); !inv2.Settled() {
		t.Error("invoice not settled")
	}

	// Sell $400 of the $1000 for £300: carried out £295.20, a £4.80 gain.
	drive(t, h, "/banking/conversions/record", url.Values{"from": {usd.Code}, "to": {chart.Bank}, "ccy_amount": {"400.00"}, "proceeds": {"300.00"}, "date": {"2026-06-10"}})
	if got := a.fxBal(usd.Code).String(); got != "USD 600.00" {
		t.Errorf("USD after conversion = %s", got)
	}
	if got := a.bal(usd.Code).String(); got != "GBP 442.80" {
		t.Errorf("carrying after conversion = %s", got)
	}
	if got := a.bal(chart.ExchangeDiff).String(); got != "GBP -7.20" {
		t.Errorf("FX after conversion = %s (want -12 + 4.80)", got)
	}
	if got := a.bal(chart.Bank).String(); got != "GBP 400.00" { // £100 seeded share capital + £300 proceeds
		t.Errorf("GBP bank = %s", got)
	}

	// A plain transfer between currencies is refused.
	drive(t, h, "/banking/transfers/record", url.Values{"from": {usd.Code}, "to": {chart.Bank}, "amount": {"10.00"}})
	if !strings.Contains(a.flash, "different currencies") {
		t.Errorf("cross-currency transfer flash: %q", a.flash)
	}

	// The account's statement is in dollars and reconciles against the USD balance.
	uploadStatement(t, h, usd.Code, "wise.csv", []byte("Date,Description,Amount\n05/06/2026,Client payment,1000.00\n10/06/2026,Converted to GBP,-400.00\n"))
	drive(t, h, "/banking/statements/confirm", nil)
	p := page(t, h, "/banking/reconcile")
	for _, want := range []string{"$1,000.00", "−$400.00", "$600.00", "Reconciled — the statement agrees"} {
		if !strings.Contains(p, want) {
			t.Errorf("reconcile page lacks %q", want)
		}
	}

	// The Banking page shows both balances and the conversion form exists.
	pb := page(t, h, "/banking")
	for _, want := range []string{"$600.00 · carried", "£442.80", `<span class="badge">USD</span>`, "Account currency"} {
		if !strings.Contains(pb, want) {
			t.Errorf("banking page lacks %q", want)
		}
	}
	if !strings.Contains(page(t, h, "/banking/transfers"), "Currency conversion") {
		t.Error("transfers page lacks the conversion form")
	}

	// Everything survives a restart.
	b, err := newApp(path)
	if err != nil {
		t.Fatal(err)
	}
	if b.fxBal(usd.Code).String() != "USD 600.00" || b.banks[len(b.banks)-1].Currency != "USD" {
		t.Errorf("restore: fx=%s banks=%+v", b.fxBal(usd.Code), b.banks[len(b.banks)-1])
	}
	if got := b.bal(chart.ExchangeDiff).String(); got != "GBP -7.20" {
		t.Errorf("FX after restore = %s", got)
	}
}
