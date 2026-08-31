package fixedassets

import (
	"testing"
	"time"

	"github.com/richardjennings/accounts/chart"
	"github.com/richardjennings/accounts/ledger"
	"github.com/richardjennings/accounts/money"
	"github.com/richardjennings/accounts/report"
	"github.com/richardjennings/decimal"
)

func gbp(s string) money.Money { return money.MustParse(money.GBP, s) }
func date(d int) ledger.Date   { return ledger.NewDate(2026, time.April, d) }

func TestStraightLineCharge(t *testing.T) {
	a := Asset{Ref: "PC-1", Cost: gbp("10000.00"), Method: StraightLine, UsefulLifeYears: 4}
	c1, err := a.Charge(gbp("0.00"))
	if err != nil {
		t.Fatal(err)
	}
	if c1.String() != "GBP 2500.00" {
		t.Errorf("year 1 charge = %s, want GBP 2500.00", c1)
	}
	// Fully depreciated: no further charge.
	last, _ := a.Charge(gbp("10000.00"))
	if !last.IsZero() {
		t.Errorf("charge once fully depreciated = %s, want zero", last)
	}
}

func TestStraightLineSchedule(t *testing.T) {
	a := Asset{Ref: "PC-1", Cost: gbp("10000.00"), Method: StraightLine, UsefulLifeYears: 4}
	sch, err := a.Schedule()
	if err != nil {
		t.Fatal(err)
	}
	if len(sch) != 4 {
		t.Fatalf("schedule length = %d, want 4", len(sch))
	}
	for i, e := range sch {
		if e.Charge.String() != "GBP 2500.00" {
			t.Errorf("year %d charge = %s, want GBP 2500.00", i+1, e.Charge)
		}
	}
	if sch[3].CarryingValue.String() != "GBP 0.00" {
		t.Errorf("final carrying value = %s, want GBP 0.00", sch[3].CarryingValue)
	}
}

func TestReducingBalanceCharge(t *testing.T) {
	a := Asset{Ref: "VAN", Cost: gbp("10000.00"), Method: ReducingBalance, Rate: decimal.MustParse("0.25")}
	c1, _ := a.Charge(gbp("0.00"))
	if c1.String() != "GBP 2500.00" { // 25% × 10000
		t.Errorf("year 1 = %s, want GBP 2500.00", c1)
	}
	c2, _ := a.Charge(gbp("2500.00"))
	if c2.String() != "GBP 1875.00" { // 25% × 7500
		t.Errorf("year 2 = %s, want GBP 1875.00", c2)
	}
}

func TestResidualFloor(t *testing.T) {
	a := Asset{Ref: "TOOL", Cost: gbp("1000.00"), Residual: gbp("100.00"), Method: StraightLine, UsefulLifeYears: 3}
	c, _ := a.Charge(gbp("0.00"))
	if c.String() != "GBP 300.00" { // (1000 − 100) / 3
		t.Errorf("charge = %s, want GBP 300.00", c)
	}
	sch, _ := a.Schedule()
	if sch[2].CarryingValue.String() != "GBP 100.00" { // ends at the residual
		t.Errorf("final carrying value = %s, want GBP 100.00", sch[2].CarryingValue)
	}
}

func TestAcquireAndDepreciatePosts(t *testing.T) {
	book, err := chart.NewUKMicroLtdBook(money.GBP)
	if err != nil {
		t.Fatal(err)
	}
	post := func(j ledger.Journal, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		if err := book.Post(j); err != nil {
			t.Fatal(err)
		}
	}

	a := Asset{Ref: "PC-1", Cost: gbp("10000.00"), Acquired: date(1), Method: StraightLine, UsefulLifeYears: 4}
	post(Acquisition{Date: date(1), Ref: a.Ref, Amount: a.Cost}.Journal())
	charge, _ := a.Charge(gbp("0.00"))
	post(DepreciationEntry{Date: date(20), Ref: a.Ref, Amount: charge}.Journal())

	bal := func(code string) string {
		v, _ := book.Balance(code)
		return v.String()
	}
	if bal(chart.PlantEquipment) != "GBP 10000.00" {
		t.Errorf("cost = %s", bal(chart.PlantEquipment))
	}
	if bal(chart.AccumulatedDepreciation) != "GBP -2500.00" { // contra-asset: credit balance
		t.Errorf("accumulated depreciation = %s, want GBP -2500.00", bal(chart.AccumulatedDepreciation))
	}
	if bal(chart.Depreciation) != "GBP 2500.00" {
		t.Errorf("depreciation expense = %s", bal(chart.Depreciation))
	}

	bs, err := report.NewBalanceSheet(book, date(30))
	if err != nil {
		t.Fatal(err)
	}
	if !bs.Balances() {
		t.Fatalf("balance sheet does not balance: A %s vs L %s + E %s", bs.TotalAssets, bs.TotalLiabilities, bs.TotalEquity)
	}
}
