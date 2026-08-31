package adjustments

import (
	"testing"
	"time"

	"github.com/richardjennings/accounts/chart"
	"github.com/richardjennings/accounts/ledger"
	"github.com/richardjennings/accounts/money"
)

func gbp(s string) money.Money { return money.MustParse(money.GBP, s) }

func bal(t *testing.T, book *ledger.Book, code, want string) {
	t.Helper()
	b, err := book.Balance(code)
	if err != nil {
		t.Fatal(err)
	}
	if b.String() != want {
		t.Errorf("%s = %s, want %s", code, b, want)
	}
}

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

func TestAccrualAndPrepayment(t *testing.T) {
	book, _ := chart.NewUKMicroLtdBook(money.GBP)
	post(t, book, Accrual{Date: ledger.NewDate(2027, time.March, 31), Ref: "ACC-1", Note: "March electricity", Amount: gbp("120.00"), Expense: chart.OfficeAdmin})
	post(t, book, Prepayment{Date: ledger.NewDate(2027, time.March, 31), Ref: "PRE-1", Note: "Insurance paid ahead", Amount: gbp("300.00"), Expense: chart.OfficeAdmin})

	bal(t, book, chart.Accruals, "GBP 120.00")     // liability for the unbilled cost
	bal(t, book, chart.Prepayments, "GBP 300.00")  // asset for the future benefit
	bal(t, book, chart.OfficeAdmin, "GBP -180.00") // 120 accrued in − 300 prepaid out
	tb, _ := book.TrialBalance()
	if !tb.InBalance() {
		t.Error("trial balance not in balance")
	}
}
