// Package mileage computes tax-free business-mileage claims at HMRC's Approved
// Mileage Allowance Payment (AMAP) rates, and posts the reimbursement. Cars and
// vans get the higher rate for the first 10,000 business miles in the tax year and
// the lower rate after; motorcycles and bicycles are flat. Rates are configuration:
// Year2026_27 holds the verified figures (the car rate rose to 55p from 6 April 2026).
package mileage

import (
	"math/big"

	"github.com/richardjennings/accounts/chart"
	"github.com/richardjennings/accounts/ledger"
	"github.com/richardjennings/accounts/money"
	"github.com/richardjennings/decimal"
)

func acct(override, def string) string {
	if override == "" {
		return def
	}
	return override
}

// Vehicle is the kind of vehicle used.
type Vehicle uint8

const (
	Car        Vehicle = iota // cars and vans (two-tier)
	Motorcycle                // flat rate
	Bicycle                   // flat rate
)

// RateTable holds AMAP rates — the unit of configuration.
type RateTable struct {
	Name           string
	FirstMiles     int             // higher-rate threshold for cars/vans (10,000)
	CarFirst       decimal.Decimal // car/van rate up to the threshold
	CarAfter       decimal.Decimal // car/van rate beyond it
	MotorcycleRate decimal.Decimal
	BicycleRate    decimal.Decimal
}

// Year2026_27 holds the AMAP rates for 2026/27, verified against HMRC (the car/van
// first-10,000-mile rate increased from 45p to 55p with effect from 6 April 2026).
var Year2026_27 = RateTable{
	Name:           "2026/27",
	FirstMiles:     10000,
	CarFirst:       decimal.MustParse("0.55"),
	CarAfter:       decimal.MustParse("0.25"),
	MotorcycleRate: decimal.MustParse("0.24"),
	BicycleRate:    decimal.MustParse("0.20"),
}

// Claim returns the approved allowance for miles driven in v, given priorMiles
// already claimed this tax year (which decides how much of a car/van journey falls
// under the 10,000-mile threshold). A zero-value RateTable uses Year2026_27.
func Claim(miles, priorMiles int, v Vehicle, rt RateTable) money.Money {
	if rt.Name == "" {
		rt = Year2026_27
	}
	if miles < 0 {
		miles = 0
	}
	perMile := func(rate decimal.Decimal, n int) *big.Rat {
		return new(big.Rat).Mul(rate.Rat(), new(big.Rat).SetInt64(int64(n)))
	}
	switch v {
	case Motorcycle:
		return money.FromRat(money.GBP, perMile(rt.MotorcycleRate, miles), money.HalfUp)
	case Bicycle:
		return money.FromRat(money.GBP, perMile(rt.BicycleRate, miles), money.HalfUp)
	default: // car/van: split across the threshold
		first := 0
		if priorMiles < rt.FirstMiles {
			first = rt.FirstMiles - priorMiles
			if first > miles {
				first = miles
			}
		}
		after := miles - first
		total := new(big.Rat).Add(perMile(rt.CarFirst, first), perMile(rt.CarAfter, after))
		return money.FromRat(money.GBP, total, money.HalfUp)
	}
}

// Reimbursement records paying a mileage claim to the director: debit travel
// costs, credit the director's loan account (the company now owes them).
type Reimbursement struct {
	Date    ledger.Date
	Ref     string
	Amount  money.Money
	Expense string // defaults to chart.Travel
	Owed    string // defaults to chart.DirectorsLoan
}

func (r Reimbursement) Journal() (ledger.Journal, error) {
	j, err := ledger.NewJournal(r.Date, "Mileage claim "+r.Ref,
		ledger.Posting{Account: acct(r.Expense, chart.Travel), Side: ledger.Debit, Amount: r.Amount},
		ledger.Posting{Account: acct(r.Owed, chart.DirectorsLoan), Side: ledger.Credit, Amount: r.Amount},
	)
	if err != nil {
		return ledger.Journal{}, err
	}
	return j.WithRef(r.Ref), nil
}
