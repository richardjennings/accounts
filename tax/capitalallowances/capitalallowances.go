// Package capitalallowances computes the tax relief a company claims on capital
// expenditure — the Annual Investment Allowance (100% up to a limit) plus
// writing-down allowances on the reducing-balance main and special rate pools,
// including the small-pools write-off. It is the tax counterpart of accounting
// depreciation, and its total feeds corporationtax.AdjustProfit. Rates are
// configuration: the Standard table holds the verified figures and is overridable.
package capitalallowances

import (
	"github.com/richardjennings/accounts/money"
	"github.com/richardjennings/decimal"
)

func gbp(s string) money.Money     { return money.MustParse(money.GBP, s) }
func dec(s string) decimal.Decimal { return decimal.MustParse(s) }

// RateTable is the set of capital-allowances rates — the unit of configuration.
type RateTable struct {
	Name               string
	AIALimit           money.Money     // Annual Investment Allowance cap
	MainRate           decimal.Decimal // main pool writing-down allowance
	SpecialRate        decimal.Decimal // special rate pool writing-down allowance
	SmallPoolThreshold money.Money     // pools at or below this may be written off in full
}

// Standard holds the rates in force since the small-pools and current pool rates
// settled (AIA £1,000,000; main pool 18%; special rate pool 6%; small pools £1,000).
var Standard = RateTable{
	Name:               "standard",
	AIALimit:           gbp("1000000.00"),
	MainRate:           dec("0.18"),
	SpecialRate:        dec("0.06"),
	SmallPoolThreshold: gbp("1000.00"),
}

// Pools are the reducing-balance written-down values.
type Pools struct {
	Main    money.Money
	Special money.Money
}

// Input is one accounting period's capital expenditure position.
type Input struct {
	Rates            RateTable // if the zero value, Standard is used
	BroughtForward   Pools     // pool written-down values brought forward
	MainAdditions    money.Money
	SpecialAdditions money.Money
	AIAClaim         money.Money // AIA to claim; unset means claim the maximum available
}

// Result is the allowances for the year and the pools carried forward.
type Result struct {
	AIA                money.Money
	MainWDA            money.Money
	SpecialWDA         money.Money
	SmallPoolAllowance money.Money
	TotalAllowance     money.Money
	CarriedForward     Pools
}

// Compute works out the year's capital allowances.
func Compute(in Input) (Result, error) {
	rt := in.Rates
	if rt.Name == "" {
		rt = Standard
	}
	cur := rt.AIALimit.Currency()
	zero := money.Zero(cur)

	var ferr error
	add := func(a, b money.Money) money.Money {
		r, e := a.Add(b)
		if e != nil && ferr == nil {
			ferr = e
		}
		return r
	}
	sub := func(a, b money.Money) money.Money {
		r, e := a.Sub(b)
		if e != nil && ferr == nil {
			ferr = e
		}
		return r
	}
	mul := func(a money.Money, r decimal.Decimal) money.Money {
		v, e := a.Mul(r, money.HalfUp)
		if e != nil && ferr == nil {
			ferr = e
		}
		return v
	}
	min := func(a, b money.Money) money.Money {
		c, _ := a.Cmp(b)
		if c <= 0 {
			return a
		}
		return b
	}
	set := func(m money.Money) money.Money {
		if m.Currency().Code == "" {
			return zero
		}
		return m
	}

	mainAdd, specAdd := set(in.MainAdditions), set(in.SpecialAdditions)
	mainBF, specBF := set(in.BroughtForward.Main), set(in.BroughtForward.Special)

	availableAIA := min(rt.AIALimit, add(mainAdd, specAdd))
	aia := availableAIA
	if in.AIAClaim.Currency().Code != "" {
		aia = min(in.AIAClaim, availableAIA)
	}
	// Allocate AIA to special-rate additions first (they attract only 6% otherwise).
	aiaToSpecial := min(aia, specAdd)
	aiaToMain := sub(aia, aiaToSpecial)

	mainPool := add(mainBF, sub(mainAdd, aiaToMain))
	specPool := add(specBF, sub(specAdd, aiaToSpecial))

	writeDown := func(pool money.Money, rate decimal.Decimal) (wda, small, cf money.Money) {
		if !pool.IsPositive() {
			return zero, zero, pool
		}
		if c, _ := pool.Cmp(rt.SmallPoolThreshold); c <= 0 { // small pool: write off in full
			return zero, pool, zero
		}
		wda = mul(pool, rate)
		return wda, zero, sub(pool, wda)
	}

	mainWDA, mainSmall, mainCF := writeDown(mainPool, rt.MainRate)
	specWDA, specSmall, specCF := writeDown(specPool, rt.SpecialRate)

	smallPool := add(mainSmall, specSmall)
	total := add(add(aia, add(mainWDA, specWDA)), smallPool)

	if ferr != nil {
		return Result{}, ferr
	}
	return Result{
		AIA:                aia,
		MainWDA:            mainWDA,
		SpecialWDA:         specWDA,
		SmallPoolAllowance: smallPool,
		TotalAllowance:     total,
		CarriedForward:     Pools{Main: mainCF, Special: specCF},
	}, nil
}
