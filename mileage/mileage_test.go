package mileage

import (
	"testing"
	"time"

	"github.com/richardjennings/accounts/chart"
	"github.com/richardjennings/accounts/ledger"
	"github.com/richardjennings/accounts/money"
	"github.com/richardjennings/decimal"
)

func TestCarWithinThreshold(t *testing.T) {
	if got := Claim(100, 0, Car, RateTable{}); got.String() != "GBP 55.00" { // 100 × 55p
		t.Errorf("100 miles = %s, want GBP 55.00", got)
	}
}

func TestCarCrossingThreshold(t *testing.T) {
	// 12,000 miles from zero: 10,000 @ 55p + 2,000 @ 25p = 5,500 + 500.
	if got := Claim(12000, 0, Car, RateTable{}); got.String() != "GBP 6000.00" {
		t.Errorf("12,000 miles = %s, want GBP 6000.00", got)
	}
}

func TestCarWithPriorMiles(t *testing.T) {
	// 3,000 more after 9,000 already claimed: 1,000 @ 55p + 2,000 @ 25p = 550 + 500.
	if got := Claim(3000, 9000, Car, RateTable{}); got.String() != "GBP 1050.00" {
		t.Errorf("3,000 miles after 9,000 = %s, want GBP 1050.00", got)
	}
}

func TestMotorcycleAndBicycle(t *testing.T) {
	if got := Claim(100, 0, Motorcycle, RateTable{}); got.String() != "GBP 24.00" {
		t.Errorf("motorcycle = %s, want GBP 24.00", got)
	}
	if got := Claim(100, 0, Bicycle, RateTable{}); got.String() != "GBP 20.00" {
		t.Errorf("bicycle = %s, want GBP 20.00", got)
	}
}

func TestConfigurableRate(t *testing.T) {
	old := Year2026_27
	old.Name = "2025/26"
	old.CarFirst = decimal.MustParse("0.45") // the pre-April-2026 car rate
	if got := Claim(100, 0, Car, old); got.String() != "GBP 45.00" {
		t.Errorf("100 miles at 45p = %s, want GBP 45.00", got)
	}
}

func TestReimbursementPosts(t *testing.T) {
	book, err := chart.NewUKMicroLtdBook(money.GBP)
	if err != nil {
		t.Fatal(err)
	}
	claim := Claim(200, 0, Car, RateTable{}) // £110.00
	j, err := Reimbursement{Date: ledger.NewDate(2026, time.April, 30), Ref: "MIL-1", Amount: claim}.Journal()
	if err != nil {
		t.Fatal(err)
	}
	if err := book.Post(j); err != nil {
		t.Fatal(err)
	}
	travel, _ := book.Balance(chart.Travel)
	if travel.String() != "GBP 110.00" {
		t.Errorf("travel expense = %s, want GBP 110.00", travel)
	}
	owed, _ := book.Balance(chart.DirectorsLoan)
	if owed.String() != "GBP 110.00" { // company owes the director
		t.Errorf("director's loan = %s, want GBP 110.00", owed)
	}
}
