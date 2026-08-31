package banking

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

func TestBankingOps(t *testing.T) {
	book, err := chart.NewUKMicroLtdBook(money.GBP)
	if err != nil {
		t.Fatal(err)
	}
	post(t, book, Transfer{Date: date(1), Ref: "T-1", Amount: gbp("100.00")})       // bank -> cash
	post(t, book, InterestReceived{Date: date(2), Ref: "I-1", Amount: gbp("5.00")}) // bank + income
	post(t, book, Charge{Date: date(3), Ref: "C-1", Amount: gbp("2.00")})           // expense - bank

	assertBalance(t, book, chart.Cash, "GBP 100.00")
	assertBalance(t, book, chart.OtherIncome, "GBP 5.00")
	assertBalance(t, book, chart.OfficeAdmin, "GBP 2.00")
	assertBalance(t, book, chart.Bank, "GBP -97.00") // -100 + 5 - 2
}

func TestConversion(t *testing.T) {
	book, err := chart.NewUKMicroLtdBook(money.GBP)
	if err != nil {
		t.Fatal(err)
	}
	if err := book.AddAccount(ledger.Account{Code: "1290", Name: "USD account", Type: ledger.Asset}); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		proceeds, carried string
		wantFX            string // income balance: gains positive
		postings          int
	}{
		{"9366.17", "9500.00", "-133.83", 3}, // realised less than carried: a loss
		{"9700.00", "9500.00", "66.17", 3},   // a gain of 200, net of the earlier loss
		{"500.00", "500.00", "66.17", 2},     // no difference: no FX posting
	}
	for _, c := range cases {
		op := Conversion{Date: date(2), Ref: "FX-1", Proceeds: gbp(c.proceeds), Carried: gbp(c.carried), From: "1290", To: chart.Bank}
		j, err := op.Journal()
		if err != nil {
			t.Fatal(err)
		}
		if len(j.Postings()) != c.postings {
			t.Errorf("%+v: %d postings, want %d", c, len(j.Postings()), c.postings)
		}
		post(t, book, op)
		if got, _ := book.Balance(chart.ExchangeDiff); got.String() != "GBP "+c.wantFX {
			t.Errorf("FX balance after %s/%s = %s, want GBP %s", c.proceeds, c.carried, got, c.wantFX)
		}
	}
	if got, _ := book.Balance("1290"); got.String() != "GBP -19500.00" {
		t.Errorf("currency account carrying = %s", got)
	}
}
