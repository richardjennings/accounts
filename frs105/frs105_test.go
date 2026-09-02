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
	a, err := Build(book, co, fy, Options{SignedBy: "Alex Director"})
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
	a, _ := Build(book, co, fy, Options{SignedBy: "Alex Director", ApprovedOn: ledger.NewDate(2027, time.June, 15), AverageEmployees: 1})

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
		`<ix:nonFraction name="uk-core:AverageNumberEmployeesDuringPeriod" contextRef="period" unitRef="pure" decimals="0">1</ix:nonFraction>`,
		`<ix:nonNumeric name="uk-core:DateAuthorisationFinancialStatementsForIssue" contextRef="period-end">2027-06-15</ix:nonNumeric>`,
		`<ix:nonNumeric name="uk-bus:NameEntityOfficer" contextRef="period-end">Alex Director</ix:nonNumeric>`,
		`<xbrli:measure>xbrli:pure</xbrli:measure>`,
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("iXBRL missing %q", want)
		}
	}
	if strings.Contains(doc, "DRAFT") {
		t.Error("approved accounts are marked as a draft")
	}
	if a.Prior != nil || strings.Contains(doc, "prior-period") || strings.Contains(doc, "comparative") {
		t.Error("a first-year set of accounts has a comparative")
	}
}

func TestUnapprovedAccountsAreADraft(t *testing.T) {
	book, _ := chart.NewUKMicroLtdBook(money.GBP)
	co := company.Default()
	a, _ := Build(book, co, co.YearContaining(ledger.NewDate(2026, time.June, 1)), Options{SignedBy: "Alex Director"})
	doc, err := a.IXBRL()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, "DRAFT — the board has not yet approved these accounts.") || !strings.Contains(doc, "(draft)</title>") {
		t.Error("unapproved accounts are not marked as a draft")
	}
	if strings.Contains(doc, "DateAuthorisationFinancialStatementsForIssue") {
		t.Error("a draft carries an approval date")
	}
}

// TestComparatives books two years and checks the second year's accounts carry the
// first year's figures beside them.
func TestComparatives(t *testing.T) {
	book, _ := chart.NewUKMicroLtdBook(money.GBP)
	co := company.Default() // incorporated 1 April 2026, year end 31 March
	post(t, book, capital.IssueShares{Date: ledger.NewDate(2026, time.April, 1), Ref: "SC-1", Amount: gbp("100.00")})
	post(t, book, sales.CashSale{Date: ledger.NewDate(2026, time.May, 2), Ref: "CS-1", Amount: gbp("2000.00")})
	post(t, book, expenses.DirectExpense{Date: ledger.NewDate(2026, time.June, 3), Ref: "EX-1", Amount: gbp("300.00"), Expense: chart.OfficeAdmin})
	post(t, book, sales.CashSale{Date: ledger.NewDate(2027, time.May, 2), Ref: "CS-2", Amount: gbp("5000.00")})
	post(t, book, expenses.DirectExpense{Date: ledger.NewDate(2027, time.June, 3), Ref: "EX-2", Amount: gbp("1000.00"), Expense: chart.OfficeAdmin})

	fy2 := co.YearContaining(ledger.NewDate(2027, time.June, 1))
	a, err := Build(book, co, fy2, Options{SignedBy: "Alex Director"})
	if err != nil {
		t.Fatal(err)
	}
	if a.Prior == nil {
		t.Fatal("second-year accounts have no comparative")
	}
	if a.Prior.FY.Number != 1 || a.Prior.FY.End != ledger.NewDate(2027, time.March, 31) {
		t.Errorf("comparative year = %s, want FY1 to 2027-03-31", a.Prior.FY)
	}
	want := map[string][2]string{
		"turnover":       {a.Turnover.String(), "GBP 5000.00"},
		"prior turnover": {a.Prior.Turnover.String(), "GBP 2000.00"},
		"profit":         {a.ProfitForYear.String(), "GBP 4000.00"},
		"prior profit":   {a.Prior.ProfitForYear.String(), "GBP 1700.00"},
		"net assets":     {a.NetAssets.String(), "GBP 5800.00"}, // 100 + 1700 + 4000
		"prior net":      {a.Prior.NetAssets.String(), "GBP 1800.00"},
		"prior reserve":  {a.Prior.ProfitLossReserve.String(), "GBP 1700.00"},
	}
	for k, v := range want {
		if v[0] != v[1] {
			t.Errorf("%s = %s, want %s", k, v[0], v[1])
		}
	}
	if !a.Balances() || !a.Prior.Balances() {
		t.Error("a year does not balance")
	}

	// The iXBRL carries the comparative column with its own contexts.
	doc, err := a.IXBRL()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`<xbrli:context id="prior-period-end">`, `<xbrli:instant>2027-03-31</xbrli:instant>`,
		`<xbrli:context id="prior-period">`, `<xbrli:startDate>2026-04-01</xbrli:startDate><xbrli:endDate>2027-03-31</xbrli:endDate>`,
		`<tr class="head"><th></th><th class="num">2028-03-31</th><th class="num">2027-03-31</th></tr>`,
		`name="uk-core:TurnoverRevenue" contextRef="period" unitRef="GBP" decimals="2">5000.00</ix:nonFraction></td><td class="num">£<ix:nonFraction name="uk-core:TurnoverRevenue" contextRef="prior-period" unitRef="GBP" decimals="2">2000.00</ix:nonFraction>`,
		`name="uk-core:NetAssetsLiabilities" contextRef="prior-period-end" unitRef="GBP" decimals="2">1800.00<`,
		"with comparative figures for the year ended 2027-03-31",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("iXBRL missing %q", want)
		}
	}
}
