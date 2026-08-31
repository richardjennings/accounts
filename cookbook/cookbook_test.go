package cookbook

import (
	"testing"
	"time"

	"github.com/richardjennings/accounts/chart"
	"github.com/richardjennings/accounts/ledger"
	"github.com/richardjennings/accounts/money"
)

func gbp(s string) money.Money { return money.MustParse(money.GBP, s) }

// TestFirstYear walks a virtual company's first year, exercising a posting from
// every theme in the cookbook, then checks the books balance and key accounts hold
// the expected figures — the executable proof of docs/posting-cookbook.md.
func TestFirstYear(t *testing.T) {
	book, err := chart.NewUKMicroLtdBook(money.GBP)
	if err != nil {
		t.Fatal(err)
	}

	day := 1
	post := func(narrative string, ps ...ledger.Posting) {
		t.Helper()
		j, err := ledger.NewJournal(ledger.NewDate(2026, time.April, day), narrative, ps...)
		if err != nil {
			t.Fatalf("%s: %v", narrative, err)
		}
		if err := book.Post(j); err != nil {
			t.Fatalf("%s: %v", narrative, err)
		}
		day++
	}
	dr := func(code, amt string) ledger.Posting {
		return ledger.Posting{Account: code, Side: ledger.Debit, Amount: gbp(amt)}
	}
	cr := func(code, amt string) ledger.Posting {
		return ledger.Posting{Account: code, Side: ledger.Credit, Amount: gbp(amt)}
	}

	// Accounting: incorporate the company.
	post("Issue share capital", dr("1200", "1000.00"), cr("3000", "1000.00"))
	// Pay Yourself: the director lends the company some money.
	post("Director's loan in", dr("1200", "500.00"), cr("2300", "500.00"))
	// Sales: a credit sale, then the customer pays.
	post("Sales invoice INV-001", dr("1100", "4000.00"), cr("4000", "4000.00"))
	post("Receipt INV-001", dr("1200", "4000.00"), cr("1100", "4000.00"))
	// Expenses: a cost-of-sales bill, then paying the supplier.
	post("Supplier bill", dr("5000", "1000.00"), cr("2100", "1000.00"))
	post("Pay supplier", dr("2100", "1000.00"), cr("1200", "1000.00"))
	// Expenses: accountancy fee paid straight from the bank.
	post("Accountancy fee", dr("7500", "600.00"), cr("1200", "600.00"))
	// Banking: a little bank interest.
	post("Bank interest", dr("1200", "5.00"), cr("4900", "5.00"))
	// Pay Yourself: salary of 700 gross = 150 PAYE/NIC withheld + 550 net paid.
	post("Director salary", dr("7000", "700.00"), cr("2210", "150.00"), cr("1200", "550.00"))
	// Pay Yourself: pay the PAYE/NIC over to HMRC.
	post("Pay PAYE/NIC", dr("2210", "150.00"), cr("1200", "150.00"))
	// Company Tax: provide for corporation tax (profit 1705 @ 19% = 323.95).
	post("Corporation tax provision", dr("8200", "323.95"), cr("2320", "323.95"))
	// Pay Yourself: declare a dividend, then pay it.
	post("Declare dividend", dr("3100", "500.00"), cr("2300", "500.00"))
	post("Pay dividend", dr("2300", "500.00"), cr("1200", "500.00"))

	tb, err := book.TrialBalance()
	if err != nil {
		t.Fatal(err)
	}
	if !tb.InBalance() {
		t.Fatalf("trial balance out: Dr %s vs Cr %s", tb.TotalDebit, tb.TotalCredit)
	}

	want := map[string]string{
		"1200": "GBP 2705.00", // bank
		"1100": "GBP 0.00",    // debtors settled
		"2100": "GBP 0.00",    // creditors settled
		"2210": "GBP 0.00",    // PAYE/NIC paid over
		"2300": "GBP 500.00",  // director's loan still outstanding
		"2320": "GBP 323.95",  // corporation tax payable
		"4000": "GBP 4000.00", // sales
		"4900": "GBP 5.00",    // other income
	}
	for code, exp := range want {
		got, err := book.Balance(code)
		if err != nil {
			t.Fatal(err)
		}
		if got.String() != exp {
			t.Errorf("%s balance = %s, want %s", code, got, exp)
		}
	}
}
