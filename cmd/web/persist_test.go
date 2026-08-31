package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/richardjennings/accounts/chart"
	"github.com/richardjennings/accounts/fixedassets"
	"github.com/richardjennings/accounts/ledger"
	"github.com/richardjennings/accounts/money"
	"github.com/richardjennings/accounts/themes/sales"
	"github.com/richardjennings/decimal"
)

// TestPersistRoundTrip mutates an app, serialises it to JSON and back, restores it
// into a fresh app, and checks the rebuilt state matches — balances (rebuilt by
// replaying journals), the sales ledger, registers, company details, and the bare
// decimals (quantity, VAT rate, depreciation rate) inside invoice lines and assets.
func TestPersistRoundTrip(t *testing.T) {
	a, err := newApp("") // in-memory; seeds £100 share capital
	if err != nil {
		t.Fatal(err)
	}
	a.co.Name = "Persisted Ltd"
	a.co.VATRegistered = true
	a.co.VATNumber = "GB424242"

	// A credit sale of £1,000 + £200 VAT, tracked in the sales ledger too.
	inv := sales.Invoice{Date: ledger.NewDate(2026, time.June, 3), Ref: "INV-100", Customer: "Acme",
		Amount: money.MustParse(money.GBP, "1000.00"), VAT: money.MustParse(money.GBP, "200.00")}
	j, err := inv.Journal()
	if err != nil {
		t.Fatal(err)
	}
	if err := a.book.Post(j); err != nil {
		t.Fatal(err)
	}
	a.entries = append(a.entries, entry{section: "sales", j: j})
	if _, err := a.sl.Raise("INV-100", "Acme", inv.Date, money.MustParse(money.GBP, "1200.00")); err != nil {
		t.Fatal(err)
	}
	if err := a.sl.Allocate("INV-100", money.MustParse(money.GBP, "500.00")); err != nil {
		t.Fatal(err)
	}

	a.invoiceOrder = append(a.invoiceOrder, "INV-100")
	a.invoiceDocs["INV-100"] = &invoiceDoc{Ref: "INV-100", Customer: "Acme", Date: inv.Date,
		Lines: []sales.InvoiceLine{{Description: "Consulting", Quantity: decimal.MustParse("2"),
			UnitPrice: money.MustParse(money.GBP, "500.00"), VATRate: decimal.MustParse("0.20")}},
		Net: inv.Amount, VAT: inv.VAT, Gross: money.MustParse(money.GBP, "1200.00")}
	a.assets = append(a.assets, &assetHolding{Asset: fixedassets.Asset{Ref: "FA-1", Name: "Laptop",
		Cost: money.MustParse(money.GBP, "1200.00"), Acquired: inv.Date, Method: fixedassets.ReducingBalance,
		Rate: decimal.MustParse("0.25")}, Accumulated: money.MustParse(money.GBP, "0.00")})

	// Snapshot → JSON → snapshot (exercises the money/decimal marshalers).
	blob, err := json.Marshal(a.buildSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	var s snapshot
	if err := json.Unmarshal(blob, &s); err != nil {
		t.Fatal(err)
	}

	b, err := newApp("")
	if err != nil {
		t.Fatal(err)
	}
	if err := b.restore(&s); err != nil {
		t.Fatalf("restore: %v", err)
	}

	if b.co.Name != "Persisted Ltd" || b.co.VATNumber != "GB424242" {
		t.Errorf("company not restored: %+v", b.co)
	}
	if got := b.bal(chart.Sales).String(); got != "GBP 1000.00" {
		t.Errorf("sales balance = %s, want GBP 1000.00", got)
	}
	if got := b.bal(chart.VAT).String(); got != "GBP 200.00" {
		t.Errorf("VAT balance = %s, want GBP 200.00", got)
	}
	if got := b.bal(chart.ShareCapital).String(); got != "GBP 100.00" {
		t.Errorf("share capital = %s, want GBP 100.00 (seed replayed)", got)
	}
	inv2, ok := b.sl.Get("INV-100")
	if !ok {
		t.Fatal("sales-ledger invoice not restored")
	}
	if got := inv2.Outstanding().String(); got != "GBP 700.00" {
		t.Errorf("outstanding = %s, want GBP 700.00 (1200 − 500 paid)", got)
	}
	d, ok := b.invoiceDocs["INV-100"]
	if !ok || len(d.Lines) != 1 {
		t.Fatalf("invoice document not restored: %+v", d)
	}
	if q, r := d.Lines[0].Quantity.String(), d.Lines[0].VATRate.String(); q != "2" || r != "0.20" {
		t.Errorf("invoice line decimals = quantity %s, VAT rate %s; want 2, 0.20", q, r)
	}
	if len(b.assets) != 1 || b.assets[0].Asset.Rate.String() != "0.25" {
		t.Errorf("asset depreciation rate not restored: %+v", b.assets)
	}
	tb, err := b.book.TrialBalance()
	if err != nil {
		t.Fatal(err)
	}
	if !tb.InBalance() {
		t.Error("restored trial balance does not balance")
	}
}
