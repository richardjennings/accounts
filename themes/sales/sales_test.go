package sales

import (
	"testing"
	"time"

	"github.com/richardjennings/accounts/chart"
	"github.com/richardjennings/accounts/ledger"
	"github.com/richardjennings/accounts/money"
	"github.com/richardjennings/decimal"
)

func gbp(s string) money.Money     { return money.MustParse(money.GBP, s) }
func num(s string) decimal.Decimal { return decimal.MustParse(s) }

func TestInvoiceWithVAT(t *testing.T) {
	book, err := chart.NewUKMicroLtdBook(money.GBP)
	if err != nil {
		t.Fatal(err)
	}
	inv := Invoice{Date: ledger.NewDate(2026, time.April, 1), Ref: "INV-9", Customer: "Acme", Amount: gbp("1000.00"), VAT: gbp("200.00")}
	if err := post(book, inv); err != nil {
		t.Fatal(err)
	}
	assertBalance(t, book, chart.TradeDebtors, "GBP 1200.00") // gross owed
	assertBalance(t, book, chart.Sales, "GBP 1000.00")        // net income
	assertBalance(t, book, chart.VAT, "GBP 200.00")           // output VAT owed to HMRC
}

// TestItemisedInvoiceWithRecharge builds an invoice from lines at different VAT
// rates, one of which recovers a cost from the customer. Ordinary sales, the
// recharge and the VAT each land in their own account, and the customer owes the
// gross.
func TestItemisedInvoiceWithRecharge(t *testing.T) {
	book, err := chart.NewUKMicroLtdBook(money.GBP)
	if err != nil {
		t.Fatal(err)
	}
	inv := Invoice{
		Date: ledger.NewDate(2026, time.April, 1), Ref: "INV-10", Customer: "Acme",
		Lines: []InvoiceLine{
			{Description: "Consulting", Quantity: num("10"), UnitPrice: gbp("50.00"), VATRate: num("0.20")},       // 500 net, 100 VAT
			{Description: "Training materials", UnitPrice: gbp("80.00"), VATRate: num("0")},                       // qty defaults to 1: 80 net, no VAT
			{Description: "Train fare (rebilled)", UnitPrice: gbp("45.00"), VATRate: num("0.20"), Recharge: true}, // 45 recharge, 9 VAT
		},
	}

	net, vat, gross, err := inv.Totals()
	if err != nil {
		t.Fatal(err)
	}
	check(t, "net", net.String(), "GBP 625.00")     // 500 + 80 + 45
	check(t, "vat", vat.String(), "GBP 109.00")     // 100 + 0 + 9
	check(t, "gross", gross.String(), "GBP 734.00") // 625 + 109

	if err := post(book, inv); err != nil {
		t.Fatal(err)
	}
	assertBalance(t, book, chart.TradeDebtors, "GBP 734.00")     // gross owed
	assertBalance(t, book, chart.Sales, "GBP 580.00")            // 500 + 80 ordinary sales
	assertBalance(t, book, chart.RechargedExpenses, "GBP 45.00") // recovered cost as income
	assertBalance(t, book, chart.VAT, "GBP 109.00")              // output VAT
}

func check(t *testing.T, label, got, want string) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %s, want %s", label, got, want)
	}
}

func TestInvoiceThenReceipt(t *testing.T) {
	book, err := chart.NewUKMicroLtdBook(money.GBP)
	if err != nil {
		t.Fatal(err)
	}

	inv := Invoice{Date: ledger.NewDate(2026, time.April, 1), Ref: "INV-001", Customer: "Acme", Amount: gbp("1200.00")}
	j, err := inv.Journal()
	if err != nil {
		t.Fatal(err)
	}
	if j.Ref() != "INV-001" {
		t.Errorf("journal ref = %q, want INV-001", j.Ref())
	}
	if err := book.Post(j); err != nil {
		t.Fatal(err)
	}
	assertBalance(t, book, chart.TradeDebtors, "GBP 1200.00")
	assertBalance(t, book, chart.Sales, "GBP 1200.00")

	rec := Receipt{Date: ledger.NewDate(2026, time.April, 5), Ref: "INV-001", Amount: gbp("1200.00")}
	rj, err := rec.Journal()
	if err != nil {
		t.Fatal(err)
	}
	if err := book.Post(rj); err != nil {
		t.Fatal(err)
	}
	assertBalance(t, book, chart.TradeDebtors, "GBP 0.00") // debt cleared
	assertBalance(t, book, chart.Bank, "GBP 1200.00")
}

func TestCashSaleAndCreditNote(t *testing.T) {
	book, _ := chart.NewUKMicroLtdBook(money.GBP)
	if err := post(book, CashSale{Date: ledger.NewDate(2026, time.April, 1), Ref: "CS-1", Amount: gbp("500.00")}); err != nil {
		t.Fatal(err)
	}
	assertBalance(t, book, chart.Bank, "GBP 500.00")
	assertBalance(t, book, chart.Sales, "GBP 500.00")

	// Refund £120 against the bank.
	if err := post(book, CreditNote{Date: ledger.NewDate(2026, time.April, 2), Ref: "CN-1", Amount: gbp("120.00"), Against: chart.Bank}); err != nil {
		t.Fatal(err)
	}
	assertBalance(t, book, chart.Bank, "GBP 380.00")
	assertBalance(t, book, chart.Sales, "GBP 380.00")
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
