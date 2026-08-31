package importer

import (
	"testing"

	"github.com/richardjennings/accounts/money"
)

func TestTableFromCSV(t *testing.T) {
	tb := FromCSV("t", [][]string{
		{"Issue Date", " Client ", "Invoice Total", "Includes VAT?"},
		{"15/05/2026", "Acme Ltd", "£1,200.00", "Yes"},
		{"2026-06-01", "Bee", "None", ""},
		{"nonsense", "Cee", "12.345", ""},
	})
	if tb.Col("issue date") != 0 || tb.Col("client") != 1 || tb.Col("Includes VAT") != 3 || tb.Col("missing") != -1 {
		t.Fatalf("columns: %v", tb.Header)
	}
	r := tb.Row(1)
	if d, err := r.Date("Issue Date"); err != nil || d.String() != "2026-05-15" {
		t.Errorf("date: %v %v", d, err)
	}
	if m, err := r.Money(money.GBP, "Invoice Total"); err != nil || m.String() != "GBP 1200.00" {
		t.Errorf("money: %v %v", m, err)
	}
	if r.Text("Client") != "Acme Ltd" || r.N() != 1 {
		t.Errorf("text %q row %d", r.Text("Client"), r.N())
	}
	r = tb.Row(2)
	if d, err := r.Date("Issue Date"); err != nil || d.String() != "2026-06-01" {
		t.Errorf("iso date: %v %v", d, err)
	}
	if m, err := r.Money(money.GBP, "Invoice Total"); err != nil || !m.IsZero() {
		t.Errorf("None should be zero: %v %v", m, err)
	}
	r = tb.Row(3)
	if _, err := r.Date("Issue Date"); err == nil {
		t.Error("bad date accepted")
	}
	if _, err := r.Money(money.GBP, "Invoice Total"); err == nil {
		t.Error("three decimal places accepted for GBP")
	}
	if err := tb.Require("Client", "Total"); err == nil {
		t.Error("Require missed a missing column")
	}
}
