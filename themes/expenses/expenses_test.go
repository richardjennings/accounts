package expenses

import (
	"testing"
	"time"

	"github.com/richardjennings/accounts/chart"
	"github.com/richardjennings/accounts/ledger"
	"github.com/richardjennings/accounts/money"
)

func gbp(s string) money.Money { return money.MustParse(money.GBP, s) }

func TestBillWithInputVAT(t *testing.T) {
	book, err := chart.NewUKMicroLtdBook(money.GBP)
	if err != nil {
		t.Fatal(err)
	}
	bill := Bill{Date: ledger.NewDate(2026, time.April, 1), Ref: "BILL-9", Supplier: "Sup", Amount: gbp("500.00"), VAT: gbp("100.00"), Expense: chart.OfficeAdmin}
	if err := post(book, bill); err != nil {
		t.Fatal(err)
	}
	assertBalance(t, book, chart.OfficeAdmin, "GBP 500.00")    // net expense
	assertBalance(t, book, chart.VAT, "GBP -100.00")           // input VAT reduces the VAT liability
	assertBalance(t, book, chart.TradeCreditors, "GBP 600.00") // gross owed
}

// TestSupplierCreditNote: a £500 + £100 VAT bill, then a £200 + £40 VAT credit note
// against creditors, leaves the expense at £300, input VAT at −£60 and creditors at £360.
func TestSupplierCreditNote(t *testing.T) {
	book, _ := chart.NewUKMicroLtdBook(money.GBP)
	if err := post(book, Bill{Date: ledger.NewDate(2026, time.April, 1), Ref: "BILL-1", Supplier: "Sup", Amount: gbp("500.00"), VAT: gbp("100.00"), Expense: chart.OfficeAdmin}); err != nil {
		t.Fatal(err)
	}
	if err := post(book, CreditNote{Date: ledger.NewDate(2026, time.April, 5), Ref: "PCN-1", Supplier: "Sup", Amount: gbp("200.00"), VAT: gbp("40.00"), Expense: chart.OfficeAdmin}); err != nil {
		t.Fatal(err)
	}
	assertBalance(t, book, chart.OfficeAdmin, "GBP 300.00")    // 500 − 200
	assertBalance(t, book, chart.VAT, "GBP -60.00")            // input VAT reduced from −100 to −60
	assertBalance(t, book, chart.TradeCreditors, "GBP 360.00") // 600 − 240
}

func TestBillThenPayment(t *testing.T) {
	book, err := chart.NewUKMicroLtdBook(money.GBP)
	if err != nil {
		t.Fatal(err)
	}

	bill := Bill{Date: ledger.NewDate(2026, time.April, 1), Ref: "BILL-1", Supplier: "Supplies Ltd", Amount: gbp("300.00")}
	if err := post(book, bill); err != nil {
		t.Fatal(err)
	}
	assertBalance(t, book, chart.TradeCreditors, "GBP 300.00") // now owed
	assertBalance(t, book, chart.OfficeAdmin, "GBP 300.00")    // default expense account

	pay := Payment{Date: ledger.NewDate(2026, time.April, 20), Ref: "BILL-1", Amount: gbp("300.00")}
	if err := post(book, pay); err != nil {
		t.Fatal(err)
	}
	assertBalance(t, book, chart.TradeCreditors, "GBP 0.00") // debt cleared
	assertBalance(t, book, chart.Bank, "GBP -300.00")        // paid with no funds yet: overdrawn
}

func TestDirectExpenseWithChosenAccount(t *testing.T) {
	book, _ := chart.NewUKMicroLtdBook(money.GBP)
	exp := DirectExpense{Date: ledger.NewDate(2026, time.April, 1), Ref: "EX-1", Payee: "Accountant", Amount: gbp("600.00"), Expense: chart.Accountancy}
	if err := post(book, exp); err != nil {
		t.Fatal(err)
	}
	assertBalance(t, book, chart.Accountancy, "GBP 600.00")
	assertBalance(t, book, chart.Bank, "GBP -600.00")
}

func post(book *ledger.Book, op interface {
	Journal() (ledger.Journal, error)
}) error {
	j, err := op.Journal()
	if err != nil {
		return err
	}
	return book.Post(j)
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
