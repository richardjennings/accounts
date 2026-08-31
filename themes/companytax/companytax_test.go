package companytax

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

func TestCorporationTax(t *testing.T) {
	book, err := chart.NewUKMicroLtdBook(money.GBP)
	if err != nil {
		t.Fatal(err)
	}
	post(t, book, Provision{Date: date(1), Ref: "CT-2026", Amount: gbp("646.00")})
	assertBalance(t, book, chart.CorpTaxCharge, "GBP 646.00")
	assertBalance(t, book, chart.CorpTaxPayable, "GBP 646.00")

	post(t, book, Payment{Date: date(20), Ref: "CT-2026", Amount: gbp("646.00")})
	assertBalance(t, book, chart.CorpTaxPayable, "GBP 0.00")
	assertBalance(t, book, chart.Bank, "GBP -646.00")
}
