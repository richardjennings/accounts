package themes_test

import (
	"testing"
	"time"

	"github.com/richardjennings/accounts/chart"
	"github.com/richardjennings/accounts/ledger"
	"github.com/richardjennings/accounts/money"
	"github.com/richardjennings/accounts/report"
	"github.com/richardjennings/accounts/themes"
	"github.com/richardjennings/accounts/themes/expenses"
	"github.com/richardjennings/accounts/themes/sales"
)

func gbp(s string) money.Money { return money.MustParse(money.GBP, s) }
func date(day int) ledger.Date { return ledger.NewDate(2026, time.April, day) }

// TestPostAcrossThemes drives operations from two themes through themes.Post, then
// reads the result back as a profit & loss account — the whole stack end to end.
func TestPostAcrossThemes(t *testing.T) {
	book, err := chart.NewUKMicroLtdBook(money.GBP)
	if err != nil {
		t.Fatal(err)
	}

	err = themes.Post(book,
		sales.CashSale{Date: date(1), Ref: "CS-1", Amount: gbp("1000.00")},
		expenses.DirectExpense{Date: date(2), Ref: "EX-1", Payee: "Stationers", Amount: gbp("400.00")},
	)
	if err != nil {
		t.Fatal(err)
	}

	pl, err := report.NewProfitAndLoss(book, date(1), date(30))
	if err != nil {
		t.Fatal(err)
	}
	if pl.TotalIncome.String() != "GBP 1000.00" {
		t.Errorf("income = %s, want GBP 1000.00", pl.TotalIncome)
	}
	if pl.TotalExpenses.String() != "GBP 400.00" {
		t.Errorf("expenses = %s, want GBP 400.00", pl.TotalExpenses)
	}
	if pl.Profit.String() != "GBP 600.00" {
		t.Errorf("profit = %s, want GBP 600.00", pl.Profit)
	}
}
