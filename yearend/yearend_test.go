package yearend

import (
	"testing"
	"time"

	"github.com/richardjennings/accounts/chart"
	"github.com/richardjennings/accounts/ledger"
	"github.com/richardjennings/accounts/money"
	"github.com/richardjennings/accounts/themes/expenses"
	"github.com/richardjennings/accounts/themes/payyourself"
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

func bal(t *testing.T, book *ledger.Book, code string) string {
	t.Helper()
	b, err := book.Balance(code)
	if err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// TestCloseRollsProfitLessDividendsToRetained: £2,000 income − £300 expenses = £1,700
// profit; a £500 dividend was declared. After the close, the P&L and dividends
// accounts are zero and retained earnings is £1,200.
func TestCloseRollsProfitLessDividendsToRetained(t *testing.T) {
	book, err := chart.NewUKMicroLtdBook(money.GBP)
	if err != nil {
		t.Fatal(err)
	}
	d := func(day int) ledger.Date { return ledger.NewDate(2027, time.March, day) }
	post(t, book, sales.CashSale{Date: d(1), Ref: "CS-1", Amount: gbp("2000.00")})
	post(t, book, expenses.DirectExpense{Date: d(2), Ref: "EX-1", Amount: gbp("300.00"), Expense: chart.OfficeAdmin})
	post(t, book, payyourself.DeclareDividend{Date: d(3), Ref: "DIV-1", Amount: gbp("500.00")})

	yearEnd := ledger.NewDate(2027, time.March, 31)
	j, err := CloseEntry(book, yearEnd, "YE-2027", chart.RetainedEarnings, chart.Dividends)
	if err != nil {
		t.Fatal(err)
	}
	if err := book.Post(j); err != nil {
		t.Fatalf("close journal did not balance: %v", err)
	}

	if got := bal(t, book, chart.Sales); got != "GBP 0.00" {
		t.Errorf("sales after close = %s, want 0", got)
	}
	if got := bal(t, book, chart.OfficeAdmin); got != "GBP 0.00" {
		t.Errorf("expense after close = %s, want 0", got)
	}
	if got := bal(t, book, chart.Dividends); got != "GBP 0.00" {
		t.Errorf("dividends after close = %s, want 0", got)
	}
	if got := bal(t, book, chart.RetainedEarnings); got != "GBP 1200.00" {
		t.Errorf("retained earnings = %s, want GBP 1200.00 (1700 profit − 500 dividend)", got)
	}
	tb, _ := book.TrialBalance()
	if !tb.InBalance() {
		t.Error("trial balance not in balance after close")
	}
}

func TestCloseErrorsWhenNothingToClose(t *testing.T) {
	book, _ := chart.NewUKMicroLtdBook(money.GBP)
	if _, err := CloseEntry(book, ledger.NewDate(2027, time.March, 31), "YE", chart.RetainedEarnings); err == nil {
		t.Error("expected an error closing an empty period")
	}
}
