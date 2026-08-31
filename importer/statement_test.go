package importer

import (
	"strings"
	"testing"

	"github.com/richardjennings/accounts/money"
)

func table(text string) *Table {
	var recs [][]string
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		recs = append(recs, strings.Split(line, ";"))
	}
	return FromCSV("stmt", recs)
}

func TestDetectStatement(t *testing.T) {
	wise := table(`TransferWise ID;Date;Date Time;Amount;Currency;Description;Payment Reference;Running Balance
x;01/02/2025;01/02/2025 09:00;-12.34;GBP;Card payment;;100.00`)
	s := DetectStatement(wise)
	if s.Date != "Date" || s.Amount != "Amount" || s.Description != "Description" || s.Balance != "Running Balance" || s.In != "" {
		t.Errorf("wise spec: %+v", s)
	}
	tide := table(`Date;Transaction description;Paid In;Paid Out;Balance
01/02/2025;Client;100.00;;100.00`)
	s = DetectStatement(tide)
	if s.Date != "Date" || s.In != "Paid In" || s.Out != "Paid Out" || s.Amount != "" || s.Description != "Transaction description" {
		t.Errorf("tide spec: %+v", s)
	}
	if err := s.Validate(tide); err != nil {
		t.Errorf("detected spec invalid: %v", err)
	}
}

func TestValidate(t *testing.T) {
	tb := table("Date;Amount;Description\n01/01/2025;1;x")
	cases := []struct {
		s  StatementSpec
		ok bool
	}{
		{StatementSpec{Date: "Date", Amount: "Amount"}, true},
		{StatementSpec{Amount: "Amount"}, false},                                // no date
		{StatementSpec{Date: "Date"}, false},                                    // no amount
		{StatementSpec{Date: "Date", Amount: "Amount", In: "Amount"}, false},    // both forms
		{StatementSpec{Date: "Date", Amount: "Missing"}, false},                 // unknown column
		{StatementSpec{Date: "Date", Amount: "Amount", DateOrder: "xy"}, false}, // bad order
	}
	for i, c := range cases {
		if err := c.s.Validate(tb); (err == nil) != c.ok {
			t.Errorf("case %d: err=%v want ok=%v", i, err, c.ok)
		}
	}
}

func TestReadStatementSignedAmount(t *testing.T) {
	tb := table(`Date;Amount;Description;Running Balance
01/02/2025;-12.34;Card;87.66
02/02/2025;£1,000.00;Client;1087.66
03/02/2025;(50.00);Fee;1037.66
04/02/2025;;Opening;1037.66
05/02/2025;bad;X;1
;;;`)
	s := StatementSpec{Date: "Date", Amount: "Amount", Description: "Description", Balance: "Running Balance"}
	lines, issues := ReadStatement(tb, s, money.GBP)
	if len(lines) != 3 || len(issues) != 1 {
		t.Fatalf("%d lines, %d issues: %v %v", len(lines), len(issues), lines, issues)
	}
	if lines[0].Amount.String() != "GBP -12.34" || lines[1].Amount.String() != "GBP 1000.00" || lines[2].Amount.String() != "GBP -50.00" {
		t.Errorf("amounts: %v %v %v", lines[0].Amount, lines[1].Amount, lines[2].Amount)
	}
	if lines[0].Date.String() != "2025-02-01" || !lines[0].HasBalance || lines[0].Balance.String() != "GBP 87.66" {
		t.Errorf("line 0: %+v", lines[0])
	}
	if !strings.Contains(issues[0].String(), "not an amount") {
		t.Errorf("issue: %v", issues[0])
	}
	// Negate flips the single-amount convention.
	lines, _ = ReadStatement(tb, StatementSpec{Date: "Date", Amount: "Amount", Negate: true}, money.GBP)
	if lines[0].Amount.String() != "GBP 12.34" {
		t.Errorf("negated: %v", lines[0].Amount)
	}
}

func TestReadStatementInOut(t *testing.T) {
	tb := table(`Date;Paid In;Paid Out;Details
01/02/2025;100.00;;Client
02/02/2025;;40.00;Supplier
03/02/2025;;-40.00;Already negative
04/02/2025;10.00;5.00;Both`)
	s := StatementSpec{Date: "Date", In: "Paid In", Out: "Paid Out", Description: "Details"}
	lines, issues := ReadStatement(tb, s, money.GBP)
	if len(issues) != 0 || len(lines) != 4 {
		t.Fatalf("%v %v", lines, issues)
	}
	for i, want := range []string{"GBP 100.00", "GBP -40.00", "GBP -40.00", "GBP 5.00"} {
		if lines[i].Amount.String() != want {
			t.Errorf("line %d = %s, want %s", i, lines[i].Amount, want)
		}
	}
}

func TestReadStatementDateOrders(t *testing.T) {
	tb := table("Date;Amount\n03/02/2025;1.00\n2025-02-03T09:30:00;2.00")
	for _, c := range []struct{ order, want string }{{"dmy", "2025-02-03"}, {"mdy", "2025-03-02"}, {"ymd", "2025-02-03"}} {
		lines, issues := ReadStatement(tb, StatementSpec{Date: "Date", Amount: "Amount", DateOrder: c.order}, money.GBP)
		if c.order == "ymd" {
			// 03/02/2025 does not parse as ymd; the ISO row still does.
			if len(lines) != 1 || len(issues) != 1 || lines[0].Date.String() != "2025-02-03" {
				t.Errorf("ymd: %v %v", lines, issues)
			}
			continue
		}
		if len(lines) != 2 || lines[0].Date.String() != c.want || lines[1].Date.String() != "2025-02-03" {
			t.Errorf("%s: %v %v", c.order, lines, issues)
		}
	}
}
