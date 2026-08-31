package report

import (
	"strings"
	"testing"
	"time"

	"github.com/richardjennings/accounts/chart"
	"github.com/richardjennings/accounts/ledger"
	"github.com/richardjennings/accounts/money"
)

func gbp(s string) money.Money { return money.MustParse(money.GBP, s) }

// buildBook posts a small first-year scenario: capital in, a sale, an overhead,
// and a corporation-tax provision.
func buildBook(t *testing.T) *ledger.Book {
	t.Helper()
	b, err := chart.NewUKMicroLtdBook(money.GBP)
	if err != nil {
		t.Fatal(err)
	}
	day := 1
	post := func(narr string, ps ...ledger.Posting) {
		t.Helper()
		j, err := ledger.NewJournal(ledger.NewDate(2026, time.April, day), narr, ps...)
		if err != nil {
			t.Fatal(err)
		}
		if err := b.Post(j); err != nil {
			t.Fatal(err)
		}
		day++
	}
	dr := func(c, a string) ledger.Posting {
		return ledger.Posting{Account: c, Side: ledger.Debit, Amount: gbp(a)}
	}
	cr := func(c, a string) ledger.Posting {
		return ledger.Posting{Account: c, Side: ledger.Credit, Amount: gbp(a)}
	}

	post("Share capital", dr("1200", "1000.00"), cr("3000", "1000.00"))
	post("Cash sale", dr("1200", "4000.00"), cr("4000", "4000.00"))
	post("Office costs", dr("7600", "600.00"), cr("1200", "600.00"))
	post("Corporation tax provision", dr("8200", "646.00"), cr("2320", "646.00"))
	return b
}

var (
	yearStart = ledger.NewDate(2026, time.April, 1)
	yearEnd   = ledger.NewDate(2026, time.April, 30)
)

func TestProfitAndLoss(t *testing.T) {
	pl, err := NewProfitAndLoss(buildBook(t), yearStart, yearEnd)
	if err != nil {
		t.Fatal(err)
	}
	if pl.TotalIncome.String() != "GBP 4000.00" {
		t.Errorf("total income = %s, want GBP 4000.00", pl.TotalIncome)
	}
	if pl.TotalExpenses.String() != "GBP 1246.00" { // 600 office + 646 tax
		t.Errorf("total expenses = %s, want GBP 1246.00", pl.TotalExpenses)
	}
	if pl.Profit.String() != "GBP 2754.00" {
		t.Errorf("profit = %s, want GBP 2754.00", pl.Profit)
	}
}

func TestBalanceSheetBalances(t *testing.T) {
	bs, err := NewBalanceSheet(buildBook(t), yearEnd)
	if err != nil {
		t.Fatal(err)
	}
	if !bs.Balances() {
		t.Fatalf("does not balance: assets %s vs liabilities %s + equity %s",
			bs.TotalAssets, bs.TotalLiabilities, bs.TotalEquity)
	}
	checks := map[string]string{
		"assets":      bs.TotalAssets.String(),
		"liabilities": bs.TotalLiabilities.String(),
		"equity":      bs.TotalEquity.String(),
		"profit":      bs.ProfitForPeriod.String(),
	}
	want := map[string]string{
		"assets":      "GBP 4400.00", // bank 1000 + 4000 - 600
		"liabilities": "GBP 646.00",  // corporation tax payable
		"equity":      "GBP 3754.00", // share capital 1000 + profit 2754
		"profit":      "GBP 2754.00",
	}
	for k, got := range checks {
		if got != want[k] {
			t.Errorf("%s = %s, want %s", k, got, want[k])
		}
	}
}

func TestRenderContainsProfit(t *testing.T) {
	book := buildBook(t)
	pl, _ := NewProfitAndLoss(book, yearStart, yearEnd)
	bs, _ := NewBalanceSheet(book, yearEnd)
	t.Logf("\n%s\n%s", pl, bs) // visible with go test -v
	if !strings.Contains(pl.String(), "Profit/(loss) for the period") {
		t.Error("P&L render missing profit line")
	}
	if !strings.Contains(bs.String(), "Profit for the period") {
		t.Error("balance sheet render missing profit line")
	}
}
