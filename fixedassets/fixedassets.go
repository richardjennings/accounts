// Package fixedassets models fixed assets and their depreciation for the accounts.
// Depreciation spreads an asset's cost over its useful life; it is an accounting
// policy (straight-line or reducing-balance), not a tax computation — the tax
// equivalent, capital allowances, is separate. The package computes the yearly
// charge and full schedule, and posts both the purchase and the depreciation.
package fixedassets

import (
	"fmt"

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

// Method is how an asset depreciates.
type Method uint8

const (
	StraightLine    Method = iota // equal charge each year over the useful life
	ReducingBalance               // a fixed percentage of the carrying value each year
)

// Asset is a depreciable fixed asset.
type Asset struct {
	Ref             string
	Name            string
	Cost            money.Money
	Acquired        ledger.Date
	Method          Method
	UsefulLifeYears int             // required for straight-line and for a schedule
	Residual        money.Money     // value at the end of life; zero if unset
	Rate            decimal.Decimal // reducing-balance annual rate, e.g. 0.25 for 25%
}

func (a Asset) residual() money.Money {
	if a.Residual.Currency().Code == "" {
		return money.Zero(a.Cost.Currency())
	}
	return a.Residual
}

// Charge returns the depreciation for the next year given the depreciation already
// accumulated. It never takes the carrying value below the residual.
func (a Asset) Charge(priorAccumulated money.Money) (money.Money, error) {
	cur := a.Cost.Currency()
	depreciable, err := a.Cost.Sub(a.residual())
	if err != nil {
		return money.Money{}, err
	}
	remaining, err := depreciable.Sub(priorAccumulated)
	if err != nil {
		return money.Money{}, err
	}
	if !remaining.IsPositive() {
		return money.Zero(cur), nil
	}

	var annual money.Money
	switch a.Method {
	case StraightLine:
		if a.UsefulLifeYears <= 0 {
			return money.Money{}, fmt.Errorf("fixedassets: straight-line needs a useful life")
		}
		if annual, err = depreciable.Div(decimal.New(int64(a.UsefulLifeYears), 0), money.HalfUp); err != nil {
			return money.Money{}, err
		}
	case ReducingBalance:
		carrying, err := a.Cost.Sub(priorAccumulated)
		if err != nil {
			return money.Money{}, err
		}
		if annual, err = carrying.Mul(a.Rate, money.HalfUp); err != nil {
			return money.Money{}, err
		}
	default:
		return money.Money{}, fmt.Errorf("fixedassets: unknown method")
	}

	if cmp, _ := annual.Cmp(remaining); cmp > 0 { // don't over-depreciate
		annual = remaining
	}
	return annual, nil
}

// Entry is one year of a depreciation schedule.
type Entry struct {
	Year          int
	Charge        money.Money
	Accumulated   money.Money
	CarryingValue money.Money // cost − accumulated
}

// Schedule returns the full depreciation schedule over the useful life. The final
// year writes off any remainder, so the carrying value ends exactly at the residual.
func (a Asset) Schedule() ([]Entry, error) {
	if a.UsefulLifeYears <= 0 {
		return nil, fmt.Errorf("fixedassets: schedule needs a useful life")
	}
	cur := a.Cost.Currency()
	depreciable, err := a.Cost.Sub(a.residual())
	if err != nil {
		return nil, err
	}
	accumulated := money.Zero(cur)
	entries := make([]Entry, 0, a.UsefulLifeYears)
	for y := 1; y <= a.UsefulLifeYears; y++ {
		charge, err := a.Charge(accumulated)
		if err != nil {
			return nil, err
		}
		if y == a.UsefulLifeYears { // final year: clear the remainder exactly
			if charge, err = depreciable.Sub(accumulated); err != nil {
				return nil, err
			}
		}
		if accumulated, err = accumulated.Add(charge); err != nil {
			return nil, err
		}
		carrying, err := a.Cost.Sub(accumulated)
		if err != nil {
			return nil, err
		}
		entries = append(entries, Entry{Year: y, Charge: charge, Accumulated: accumulated, CarryingValue: carrying})
	}
	return entries, nil
}

// Acquisition records buying a fixed asset: debit the fixed-asset cost account,
// credit however it was funded (the bank by default).
type Acquisition struct {
	Date   ledger.Date
	Ref    string
	Amount money.Money
	Asset  string // fixed-asset account; defaults to chart.PlantEquipment
	Funded string // defaults to chart.Bank
}

func (a Acquisition) Journal() (ledger.Journal, error) {
	j, err := ledger.NewJournal(a.Date, "Asset purchase "+a.Ref,
		ledger.Posting{Account: acct(a.Asset, chart.PlantEquipment), Side: ledger.Debit, Amount: a.Amount},
		ledger.Posting{Account: acct(a.Funded, chart.Bank), Side: ledger.Credit, Amount: a.Amount},
	)
	if err != nil {
		return ledger.Journal{}, err
	}
	return j.WithRef(a.Ref), nil
}

// DepreciationEntry records a year's depreciation: debit the depreciation expense,
// credit accumulated depreciation (a contra-asset).
type DepreciationEntry struct {
	Date        ledger.Date
	Ref         string
	Amount      money.Money
	Expense     string // defaults to chart.Depreciation
	Accumulated string // defaults to chart.AccumulatedDepreciation
}

func (d DepreciationEntry) Journal() (ledger.Journal, error) {
	j, err := ledger.NewJournal(d.Date, "Depreciation "+d.Ref,
		ledger.Posting{Account: acct(d.Expense, chart.Depreciation), Side: ledger.Debit, Amount: d.Amount},
		ledger.Posting{Account: acct(d.Accumulated, chart.AccumulatedDepreciation), Side: ledger.Credit, Amount: d.Amount},
	)
	if err != nil {
		return ledger.Journal{}, err
	}
	return j.WithRef(d.Ref), nil
}
