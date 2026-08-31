package vatreturn

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

func TestVATReturnBoxes(t *testing.T) {
	book, err := chart.NewUKMicroLtdBook(money.GBP)
	if err != nil {
		t.Fatal(err)
	}
	d := func(day int) ledger.Date { return ledger.NewDate(2026, time.May, day) }
	post(t, book, sales.CashSale{Date: d(1), Ref: "CS-1", Amount: gbp("2000.00"), VAT: gbp("400.00")})                               // output VAT 400
	post(t, book, expenses.DirectExpense{Date: d(2), Ref: "EX-1", Amount: gbp("500.00"), VAT: gbp("100.00"), Expense: chart.Travel}) // input VAT 100
	post(t, book, payyourself.Salary{Date: d(3), Ref: "SAL-1", Gross: gbp("1000.00")})                                               // wages: excluded from Box 7

	opt := Options{
		VATControl:      chart.VAT,
		PurchaseExclude: map[string]bool{chart.Salaries: true, chart.EmployerNIC: true, chart.PensionCosts: true, chart.Depreciation: true, chart.CorpTaxCharge: true},
		CapitalCodes:    []string{chart.PlantEquipment},
	}
	r, err := Compute(book, ledger.NewDate(2026, time.April, 1), ledger.NewDate(2027, time.March, 31), opt)
	if err != nil {
		t.Fatal(err)
	}
	checks := []struct{ label, got, want string }{
		{"Box 1 (output VAT)", r.Box1.String(), "GBP 400.00"},
		{"Box 3 (total due)", r.Box3.String(), "GBP 400.00"},
		{"Box 4 (input VAT)", r.Box4.String(), "GBP 100.00"},
		{"Box 5 (net to pay)", r.Box5.String(), "GBP 300.00"},
		{"Box 6 (sales)", r.Box6.String(), "GBP 2000.00"},
		{"Box 7 (purchases, ex wages)", r.Box7.String(), "GBP 500.00"},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %s, want %s", c.label, c.got, c.want)
		}
	}
	if r.Box5Reclaim {
		t.Error("Box 5 should be payable, not a reclaim")
	}
}

// TestReclaimPosition: more input VAT than output VAT means a reclaim.
func TestReclaimPosition(t *testing.T) {
	book, _ := chart.NewUKMicroLtdBook(money.GBP)
	post(t, book, sales.CashSale{Date: ledger.NewDate(2026, time.May, 1), Ref: "CS-1", Amount: gbp("100.00"), VAT: gbp("20.00")})
	post(t, book, expenses.DirectExpense{Date: ledger.NewDate(2026, time.May, 2), Ref: "EX-1", Amount: gbp("500.00"), VAT: gbp("100.00"), Expense: chart.Travel})
	r, err := Compute(book, ledger.NewDate(2026, time.April, 1), ledger.NewDate(2027, time.March, 31), Options{VATControl: chart.VAT})
	if err != nil {
		t.Fatal(err)
	}
	if r.Box5.String() != "GBP 80.00" || !r.Box5Reclaim { // 100 input − 20 output = 80 reclaim
		t.Errorf("Box5 = %s reclaim=%v, want GBP 80.00 reclaim", r.Box5, r.Box5Reclaim)
	}
}
