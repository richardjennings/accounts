package payyourself

import (
	"testing"
	"time"

	"github.com/richardjennings/accounts/chart"
	"github.com/richardjennings/accounts/ledger"
	"github.com/richardjennings/accounts/money"
)

func gbp(s string) money.Money { return money.MustParse(money.GBP, s) }
func date(d int) ledger.Date   { return ledger.NewDate(2026, time.April, d) }

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

func assertBalance(t *testing.T, book *ledger.Book, code, want string) {
	t.Helper()
	got, err := book.Balance(code)
	if err != nil {
		t.Fatal(err)
	}
	if got.String() != want {
		t.Errorf("balance %s = %s, want %s", code, got, want)
	}
}

func TestSalaryWithTax(t *testing.T) {
	book, _ := chart.NewUKMicroLtdBook(money.GBP)
	post(t, book, Salary{Date: date(1), Ref: "PAY-1", Gross: gbp("1000.00"), TaxNIC: gbp("200.00")})
	assertBalance(t, book, chart.Salaries, "GBP 1000.00")
	assertBalance(t, book, chart.PAYENIC, "GBP 200.00")
	assertBalance(t, book, chart.Bank, "GBP -800.00") // net paid

	post(t, book, PayPAYE{Date: date(2), Ref: "PAY-1", Amount: gbp("200.00")})
	assertBalance(t, book, chart.PAYENIC, "GBP 0.00")
	assertBalance(t, book, chart.Bank, "GBP -1000.00")
}

func TestSalaryNoTaxOmitsLeg(t *testing.T) {
	book, _ := chart.NewUKMicroLtdBook(money.GBP)
	// TaxNIC left unset (zero value): treated as zero, PAYE/NIC leg omitted.
	post(t, book, Salary{Date: date(1), Ref: "PAY-2", Gross: gbp("500.00")})
	assertBalance(t, book, chart.Salaries, "GBP 500.00")
	assertBalance(t, book, chart.Bank, "GBP -500.00")
	assertBalance(t, book, chart.PAYENIC, "GBP 0.00")
}

func TestSalaryTaxExceedsGrossErrors(t *testing.T) {
	_, err := Salary{Date: date(1), Ref: "PAY-3", Gross: gbp("100.00"), TaxNIC: gbp("200.00")}.Journal()
	if err == nil {
		t.Fatal("expected an error when tax/NIC exceeds gross")
	}
}

func TestDividendsAndLoan(t *testing.T) {
	book, _ := chart.NewUKMicroLtdBook(money.GBP)
	post(t, book, IntroduceFunds{Date: date(1), Ref: "DL-1", Amount: gbp("1000.00")})
	post(t, book, DeclareDividend{Date: date(2), Ref: "DIV-1", Amount: gbp("500.00")})
	post(t, book, PayDividend{Date: date(3), Ref: "DIV-1", Amount: gbp("500.00")})
	post(t, book, DrawFunds{Date: date(4), Ref: "DR-1", Amount: gbp("300.00")})

	assertBalance(t, book, chart.Bank, "GBP 200.00")          // 1000 in - 500 div - 300 draw
	assertBalance(t, book, chart.DirectorsLoan, "GBP 700.00") // 1000 + 500 - 500 - 300
	assertBalance(t, book, chart.Dividends, "GBP -500.00")    // distribution: reduces equity
}
