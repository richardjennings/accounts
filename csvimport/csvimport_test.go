package csvimport

import (
	"strings"
	"testing"

	"github.com/richardjennings/accounts/money"
)

func TestParseInvoices(t *testing.T) {
	csv := "Issue date,Client,Description,Invoice Total,Includes VAT?,Payment Date,Bank Account/Director\n" +
		"15/05/2026,Acme Ltd,Consulting,\"1,200.00\",Yes,20/05/2026,Current\n" +
		"16/05/2026,Beta,Ad-hoc work,500.00,No,,\n"
	rows, skipped, err := ParseInvoices(strings.NewReader(csv), money.GBP)
	if err != nil {
		t.Fatal(err)
	}
	if len(skipped) != 0 {
		t.Fatalf("unexpected skipped rows: %v", skipped)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	a := rows[0]
	if a.Client != "Acme Ltd" || a.Gross.String() != "GBP 1200.00" || a.Net.String() != "GBP 1000.00" || a.VAT.String() != "GBP 200.00" {
		t.Errorf("invoice 1 = %+v", a)
	}
	if !a.Paid || a.PaymentDate.String() != "2026-05-20" {
		t.Errorf("invoice 1 payment = paid:%v date:%s", a.Paid, a.PaymentDate)
	}
	if a.IssueDate.String() != "2026-05-15" {
		t.Errorf("issue date = %s, want 2026-05-15", a.IssueDate)
	}
	b := rows[1]
	if b.Net.String() != "GBP 500.00" || b.VAT.String() != "GBP 0.00" || b.Paid {
		t.Errorf("invoice 2 = %+v (want net 500, no VAT, unpaid)", b)
	}
}

func TestParseExpenses(t *testing.T) {
	csv := "Date,Supplier,Description,Amount,Payment method,Director\n" +
		"10/05/2026,Staples,Stationery,60.00,Current,Alex\n" +
		"bad,Nope,,notanumber,,\n"
	rows, skipped, err := ParseExpenses(strings.NewReader(csv), money.GBP)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if len(skipped) != 1 {
		t.Fatalf("skipped = %d, want 1 (the bad row)", len(skipped))
	}
	e := rows[0]
	if e.Supplier != "Staples" || e.Gross.String() != "GBP 60.00" || e.Net.String() != "GBP 60.00" {
		t.Errorf("expense = %+v", e)
	}
}

func TestParseStatement(t *testing.T) {
	csv := "Date,Description,Paid In,Paid Out,Balance\n" +
		"01/06/2026,Opening balance,,,1000.00\n" +
		"02/06/2026,Customer receipt,\"1,200.00\",,2200.00\n" +
		"03/06/2026,Rent,,500.00,1700.00\n"
	rows, skipped, err := ParseStatement(strings.NewReader(csv), money.GBP)
	if err != nil || len(skipped) != 0 {
		t.Fatalf("err=%v skipped=%v", err, skipped)
	}
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(rows))
	}
	if rows[0].Amount.String() != "GBP 0.00" || !rows[0].HasBalance || rows[0].Balance.String() != "GBP 1000.00" {
		t.Errorf("opening = %+v", rows[0])
	}
	if rows[1].Amount.String() != "GBP 1200.00" { // paid in
		t.Errorf("receipt amount = %s, want GBP 1200.00", rows[1].Amount)
	}
	if rows[2].Amount.String() != "GBP -500.00" { // paid out → negative
		t.Errorf("rent amount = %s, want GBP -500.00", rows[2].Amount)
	}
}

// TestVATInclusiveExtractionExact: extracting VAT from a gross that does not divide
// cleanly still re-sums to the gross to the penny.
func TestVATInclusiveExtractionExact(t *testing.T) {
	csv := "Issue date,Client,Invoice Total,Includes VAT?\n07/07/2026,Odd,100.00,Yes\n"
	rows, _, err := ParseInvoices(strings.NewReader(csv), money.GBP)
	if err != nil || len(rows) != 1 {
		t.Fatalf("parse: %v rows=%d", err, len(rows))
	}
	r := rows[0]
	// £100 gross incl 20% → VAT £16.67 (100/6 = 16.666… → 16.67), net £83.33.
	if r.VAT.String() != "GBP 16.67" || r.Net.String() != "GBP 83.33" {
		t.Errorf("VAT=%s net=%s, want 16.67 / 83.33", r.VAT, r.Net)
	}
	sum, _ := r.Net.Add(r.VAT)
	if sum.String() != "GBP 100.00" {
		t.Errorf("net+VAT = %s, want GBP 100.00", sum)
	}
}
