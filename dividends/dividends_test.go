package dividends

import (
	"testing"
	"time"

	"github.com/richardjennings/accounts/chart"
	"github.com/richardjennings/accounts/ledger"
	"github.com/richardjennings/accounts/money"
	"github.com/richardjennings/accounts/themes"
	"github.com/richardjennings/accounts/themes/expenses"
	"github.com/richardjennings/accounts/themes/payyourself"
	"github.com/richardjennings/accounts/themes/sales"
)

func gbp(s string) money.Money { return money.MustParse(money.GBP, s) }
func date(d int) ledger.Date   { return ledger.NewDate(2026, time.April, d) }

var asAt = date(30)

func newBook(t *testing.T, ops ...themes.Operation) *ledger.Book {
	t.Helper()
	book, err := chart.NewUKMicroLtdBook(money.GBP)
	if err != nil {
		t.Fatal(err)
	}
	if err := themes.Post(book, ops...); err != nil {
		t.Fatal(err)
	}
	return book
}

func TestAvailableIsProfitLessDistributions(t *testing.T) {
	// £1,000 of profit made, no share capital: all £1,000 is distributable.
	book := newBook(t, sales.CashSale{Date: date(1), Ref: "CS-1", Amount: gbp("1000.00")})
	got, err := Available(book, asAt)
	if err != nil {
		t.Fatal(err)
	}
	if got.String() != "GBP 1000.00" {
		t.Fatalf("available = %s, want GBP 1000.00", got)
	}
}

func TestCheckLawfulAndUnlawful(t *testing.T) {
	book := newBook(t, sales.CashSale{Date: date(1), Ref: "CS-1", Amount: gbp("1000.00")})

	lawful, err := Check(book, asAt, gbp("800.00"))
	if err != nil {
		t.Fatal(err)
	}
	if !lawful.Lawful || !lawful.Shortfall.IsZero() {
		t.Errorf("£800 of £1000 should be lawful: %+v", lawful)
	}

	over, err := Check(book, asAt, gbp("1200.00"))
	if err != nil {
		t.Fatal(err)
	}
	if over.Lawful || over.Shortfall.String() != "GBP 200.00" {
		t.Errorf("£1200 of £1000 should be unlawful with £200 shortfall: %+v", over)
	}
}

func TestReservesReducedByDeclaredDividends(t *testing.T) {
	book := newBook(t,
		sales.CashSale{Date: date(1), Ref: "CS-1", Amount: gbp("1000.00")},
		payyourself.DeclareDividend{Date: date(2), Ref: "DIV-1", Amount: gbp("600.00")},
	)
	got, err := Available(book, asAt)
	if err != nil {
		t.Fatal(err)
	}
	if got.String() != "GBP 400.00" { // 1000 profit − 600 already declared
		t.Fatalf("available = %s, want GBP 400.00", got)
	}
	d, _ := Check(book, asAt, gbp("500.00"))
	if d.Lawful || d.Shortfall.String() != "GBP 100.00" {
		t.Errorf("£500 of £400 should be unlawful with £100 shortfall: %+v", d)
	}
}

func TestLossMakesReservesNegative(t *testing.T) {
	book := newBook(t, expenses.DirectExpense{Date: date(1), Ref: "EX-1", Amount: gbp("200.00")})
	d, err := Check(book, asAt, gbp("100.00"))
	if err != nil {
		t.Fatal(err)
	}
	if d.Available.String() != "GBP -200.00" || d.Lawful || d.Shortfall.String() != "GBP 300.00" {
		t.Errorf("a company in loss cannot distribute: %+v", d)
	}
}
