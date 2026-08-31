package main

import (
	"os"
	"testing"

	"github.com/richardjennings/accounts/chart"
	"github.com/richardjennings/accounts/importer"
	"github.com/richardjennings/accounts/importer/crunch"
)

// TestCrunchImportLocal imports the private Crunch export named by
// CRUNCH_EXPORT_ZIP into a fresh in-memory company and checks the books
// balance. It logs counts and the report; the export is never in the repository.
func TestCrunchImportLocal(t *testing.T) {
	zipPath := os.Getenv("CRUNCH_EXPORT_ZIP")
	if zipPath == "" {
		t.Skip("CRUNCH_EXPORT_ZIP not set")
	}
	tables, err := importer.ReadZipFile(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	a, err := newApp("")
	if err != nil {
		t.Fatal(err)
	}
	b, issues, err := (crunch.Profile{}).Read(tables, a.co.Currency)
	if err != nil {
		t.Fatal(err)
	}
	rep := a.applyBatch("Crunch", b, issues)
	tb, err := a.book.TrialBalance()
	if err != nil {
		t.Fatal(err)
	}
	if !tb.InBalance() {
		t.Errorf("trial balance out of balance: %s vs %s", tb.TotalDebit, tb.TotalCredit)
	}
	for _, c := range rep.Counts {
		t.Logf("%-32s %4d", c.Kind, c.N)
	}
	t.Logf("skipped (closed period): %d; issues: %d; journals: %d; banks: %d", rep.Skipped, len(rep.Issues), len(a.book.Journals()), len(a.banks))
	for _, n := range rep.Notes {
		t.Logf("note: %s", n)
	}
	for i, s := range rep.Issues {
		if i >= 12 {
			t.Logf("... and %d more", len(rep.Issues)-12)
			break
		}
		t.Logf("issue: %s", s)
	}
	open := 0
	for _, inv := range a.sl.Invoices() {
		if !inv.Settled() {
			open++
		}
	}
	t.Logf("sales ledger: %d invoices, %d open; purchase ledger: %d bills, %d open", len(a.sl.Invoices()), open, len(a.purch.Bills()), len(a.purch.Outstanding()))
	for _, code := range []string{chart.Sales, chart.TradeDebtors, chart.TradeCreditors, chart.VAT, chart.DirectorsLoan, chart.CorpTaxPayable, chart.PAYENIC, chart.Cash} {
		ac, _ := a.book.Account(code)
		t.Logf("%-28s %s", ac.Name, a.bal(code))
	}
	for _, bk := range a.banks {
		if bk.Currency != "" {
			t.Logf("bank %-24s %s holding %s", bk.Name, a.bal(bk.Code), a.fxBal(bk.Code))
			continue
		}
		t.Logf("bank %-24s %s", bk.Name, a.bal(bk.Code))
	}
	t.Logf("today: %s; VAT registered: %v", a.today, a.co.VATRegistered)
}
