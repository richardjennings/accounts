package frs105

import (
	"strings"
	"testing"
	"time"

	"github.com/richardjennings/accounts/chart"
	"github.com/richardjennings/accounts/company"
	"github.com/richardjennings/accounts/ledger"
	"github.com/richardjennings/accounts/money"
	"github.com/richardjennings/accounts/themes/capital"
	"github.com/richardjennings/accounts/themes/expenses"
	"github.com/richardjennings/accounts/themes/sales"
)

func gbp(s string) money.Money { return money.MustParse(money.GBP, s) }

func post(t *testing.T, book *ledger.Book, op interface {
	Journal() (ledger.Journal, error)
}) {
	t.Helper()
	j, err := op.Journal()
	if err != nil {
		t.Fatal(err)
	}
	if err := book.Post(j); err != nil {
		t.Fatal(err)
	}
}

func TestBuildAndBalance(t *testing.T) {
	book, err := chart.NewUKMicroLtdBook(money.GBP)
	if err != nil {
		t.Fatal(err)
	}
	d := func(day int) ledger.Date { return ledger.NewDate(2026, time.May, day) }

	post(t, book, capital.IssueShares{Date: d(1), Ref: "SC-1", Amount: gbp("100.00")}) // Dr Bank / Cr Share capital
	post(t, book, sales.CashSale{Date: d(2), Ref: "CS-1", Amount: gbp("2000.00")})     // Dr Bank / Cr Sales
	post(t, book, expenses.DirectExpense{Date: d(3), Ref: "EX-1", Amount: gbp("300.00"), Expense: chart.OfficeAdmin})
	// Buy £500 of plant for cash (a fixed asset).
	plant, err := ledger.NewJournal(d(4), "Plant purchase",
		ledger.Posting{Account: chart.PlantEquipment, Side: ledger.Debit, Amount: gbp("500.00")},
		ledger.Posting{Account: chart.Bank, Side: ledger.Credit, Amount: gbp("500.00")},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := book.Post(plant); err != nil {
		t.Fatal(err)
	}

	co := company.Default()
	fy := co.YearContaining(ledger.NewDate(2026, time.June, 1))
	a, err := Build(book, co, fy, "Alex Director")
	if err != nil {
		t.Fatal(err)
	}

	checks := map[string]string{
		"fixed assets":        a.FixedAssets.String(),
		"current assets":      a.CurrentAssets.String(),
		"net current assets":  a.NetCurrentAssets.String(),
		"total less current":  a.TotalAssetsLessCL.String(),
		"net assets":          a.NetAssets.String(),
		"called-up capital":   a.CalledUpCapital.String(),
		"p&l reserve":         a.ProfitLossReserve.String(),
		"capital & reserves":  a.CapitalAndReserves.String(),
		"turnover":            a.Turnover.String(),
		"gross profit":        a.GrossProfit.String(),
		"admin expenses":      a.AdminExpenses.String(),
		"profit before tax":   a.ProfitBeforeTax.String(),
		"profit for the year": a.ProfitForYear.String(),
	}
	want := map[string]string{
		"fixed assets":        "GBP 500.00",
		"current assets":      "GBP 1300.00", // bank: 100 + 2000 − 300 − 500
		"net current assets":  "GBP 1300.00",
		"total less current":  "GBP 1800.00",
		"net assets":          "GBP 1800.00",
		"called-up capital":   "GBP 100.00",
		"p&l reserve":         "GBP 1700.00",
		"capital & reserves":  "GBP 1800.00",
		"turnover":            "GBP 2000.00",
		"gross profit":        "GBP 2000.00",
		"admin expenses":      "GBP 300.00",
		"profit before tax":   "GBP 1700.00",
		"profit for the year": "GBP 1700.00",
	}
	for k, got := range checks {
		if got != want[k] {
			t.Errorf("%s = %s, want %s", k, got, want[k])
		}
	}
	if !a.Balances() {
		t.Errorf("net assets %s must equal capital & reserves %s", a.NetAssets, a.CapitalAndReserves)
	}
}

func TestIXBRLIsWellFormedAndTagged(t *testing.T) {
	book, _ := chart.NewUKMicroLtdBook(money.GBP)
	post(t, book, capital.IssueShares{Date: ledger.NewDate(2026, time.May, 1), Ref: "SC-1", Amount: gbp("100.00")})
	post(t, book, sales.CashSale{Date: ledger.NewDate(2026, time.May, 2), Ref: "CS-1", Amount: gbp("2000.00")})

	co := company.Default()
	fy := co.YearContaining(ledger.NewDate(2026, time.June, 1))
	a, _ := Build(book, co, fy, "Alex Director")

	doc, err := a.IXBRL()
	if err != nil {
		t.Fatalf("IXBRL render (must be well-formed): %v", err)
	}
	for _, want := range []string{
		`<ix:nonNumeric name="uk-bus:EntityCurrentLegalOrRegisteredName" contextRef="period-end">Your Company Ltd</ix:nonNumeric>`,
		`name="uk-core:NetAssetsLiabilities"`,
		`name="uk-core:TurnoverRevenue"`,
		`unitRef="GBP"`,
		`<xbrli:instant>2027-03-31</xbrli:instant>`,
		`<xbrli:startDate>2026-04-01</xbrli:startDate>`,
		`xmlns:uk-core="http://xbrl.frc.org.uk/fr/2023-01-01/core"`,
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("iXBRL missing %q", want)
		}
	}
}
