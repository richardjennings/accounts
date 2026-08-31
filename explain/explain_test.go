package explain

import (
	"strings"
	"testing"
	"time"

	"github.com/richardjennings/accounts/chart"
	"github.com/richardjennings/accounts/ledger"
	"github.com/richardjennings/accounts/money"
	"github.com/richardjennings/accounts/themes/payyourself"
	"github.com/richardjennings/accounts/themes/sales"
)

func gbp(s string) money.Money { return money.MustParse(money.GBP, s) }
func date(d int) ledger.Date   { return ledger.NewDate(2026, time.April, d) }

func book(t *testing.T) *ledger.Book {
	t.Helper()
	b, err := chart.NewUKMicroLtdBook(money.GBP)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestExplainInvoice(t *testing.T) {
	e, err := Explain(book(t), sales.Invoice{Date: date(1), Ref: "INV-1", Customer: "Acme", Amount: gbp("1200.00")})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(e.Headline, "Acme") || !strings.Contains(e.Headline, "£1200.00") {
		t.Errorf("headline = %q", e.Headline)
	}
	if len(e.Postings) != 2 {
		t.Fatalf("want 2 posting notes, got %d", len(e.Postings))
	}
	if e.Principle == "" {
		t.Error("expected a principle")
	}
	// The debtor leg should read as an asset increasing.
	if !hasNote(e, "Trade debtors", "debit", "this asset goes up") {
		t.Errorf("missing debtor mechanics: %+v", e.Postings)
	}
	if !hasNote(e, "Sales", "credit", "you've earned income") {
		t.Errorf("missing sales mechanics: %+v", e.Postings)
	}
	t.Logf("\n%s", e)
}

func TestExplainSalaryShowsSplit(t *testing.T) {
	e, err := Explain(book(t), payyourself.Salary{Date: date(1), Ref: "PAY-1", Gross: gbp("1000.00"), TaxNIC: gbp("200.00")})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(e.Headline, "£200.00") || !strings.Contains(e.Headline, "£800.00") {
		t.Errorf("salary headline should show the split: %q", e.Headline)
	}
	if len(e.Postings) != 3 {
		t.Errorf("want 3 legs (salary, PAYE, net), got %d", len(e.Postings))
	}
	t.Logf("\n%s", e)
}

func TestExplainDividendPrinciple(t *testing.T) {
	e, _ := Explain(book(t), payyourself.DeclareDividend{Date: date(1), Ref: "DIV-1", Amount: gbp("500.00")})
	if !strings.Contains(strings.ToLower(e.Principle), "not a company expense") {
		t.Errorf("dividend principle should stress it is not an expense: %q", e.Principle)
	}
}

func TestExplainJournalFallback(t *testing.T) {
	j, err := ledger.NewJournal(date(1), "Manual adjustment",
		ledger.Posting{Account: chart.Bank, Side: ledger.Debit, Amount: gbp("10.00")},
		ledger.Posting{Account: chart.Sales, Side: ledger.Credit, Amount: gbp("10.00")})
	if err != nil {
		t.Fatal(err)
	}
	e := ExplainJournal(book(t), j)
	if e.Headline != "Manual adjustment" {
		t.Errorf("headline = %q, want the journal narrative", e.Headline)
	}
	if len(e.Postings) != 2 || e.Postings[0].Effect == "" {
		t.Errorf("expected mechanics on each posting: %+v", e.Postings)
	}
}

func hasNote(e Explanation, account, side, effect string) bool {
	for _, p := range e.Postings {
		if p.Account == account && p.Side == side && p.Effect == effect {
			return true
		}
	}
	return false
}
