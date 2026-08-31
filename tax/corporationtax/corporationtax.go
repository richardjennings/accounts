// Package corporationtax computes the UK corporation tax charge on a company's
// taxable profit — the small profits rate, the main rate, and Marginal Relief in
// between — using rates keyed by financial year (which starts 1 April). The rates
// and the Marginal Relief formula are verified against HMRC (CTM03925). This
// computes the charge; the companytax theme posts it.
package corporationtax

import (
	"fmt"
	"math/big"

	"github.com/richardjennings/accounts/money"
	"github.com/richardjennings/decimal"
)

func gbp(s string) money.Money     { return money.MustParse(money.GBP, s) }
func dec(s string) decimal.Decimal { return decimal.MustParse(s) }
func toRat(m money.Money) *big.Rat { return m.Amount().Rat() }

// Rates are the corporation tax rates and limits for one financial year.
type Rates struct {
	SmallProfitsRate decimal.Decimal // e.g. 0.19
	MainRate         decimal.Decimal // e.g. 0.25
	LowerLimit       money.Money     // £50,000
	UpperLimit       money.Money     // £250,000
	MRNumerator      int64           // marginal relief fraction numerator (3)
	MRDenominator    int64           // marginal relief fraction denominator (200)
}

// standardRates have applied unchanged since the small profits rate and Marginal
// Relief were reintroduced on 1 April 2023.
var standardRates = Rates{
	SmallProfitsRate: dec("0.19"),
	MainRate:         dec("0.25"),
	LowerLimit:       gbp("50000.00"),
	UpperLimit:       gbp("250000.00"),
	MRNumerator:      3,
	MRDenominator:    200,
}

var ratesByFY = map[int]Rates{
	2023: standardRates,
	2024: standardRates,
	2025: standardRates,
	2026: standardRates,
}

// Input describes an accounting period's position for the corporation tax charge.
type Input struct {
	FinancialYear       int         // the year the financial year starts (1 April YYYY)
	TaxableProfit       money.Money // taxable total profits (after tax adjustments)
	ExemptDistributions money.Money // added to TTP to give augmented profits; often zero
	AssociatedCompanies int         // divides the limits by (1 + this)
	Days                int         // accounting period length; 0 means a full year (365)
}

// Result is a corporation tax computation with enough detail to display or explain.
type Result struct {
	TaxableProfit   money.Money
	AugmentedProfit money.Money
	Charge          money.Money
	MarginalRelief  money.Money
	Band            string          // "small profits" | "marginal" | "main rate" | "none"
	EffectiveRate   decimal.Decimal // Charge / TaxableProfit
}

// AdjustProfit derives taxable total profit from accounting profit before tax: add
// back disallowable expenses (e.g. depreciation, entertaining) and deduct capital
// allowances.
func AdjustProfit(accountingProfitBeforeTax, addBacks, capitalAllowances money.Money) (money.Money, error) {
	p, err := accountingProfitBeforeTax.Add(addBacks)
	if err != nil {
		return money.Money{}, err
	}
	return p.Sub(capitalAllowances)
}

// Compute returns the corporation tax charge for the period.
func Compute(in Input) (Result, error) {
	rates, ok := ratesByFY[in.FinancialYear]
	if !ok {
		return Result{}, fmt.Errorf("corporationtax: no rate table for financial year %d", in.FinancialYear)
	}
	cur := in.TaxableProfit.Currency()

	augmented := in.TaxableProfit
	if in.ExemptDistributions.Currency().Code != "" && !in.ExemptDistributions.IsZero() {
		a, err := in.TaxableProfit.Add(in.ExemptDistributions)
		if err != nil {
			return Result{}, err
		}
		augmented = a
	}

	res := Result{
		TaxableProfit:   in.TaxableProfit,
		AugmentedProfit: augmented,
		Charge:          money.Zero(cur),
		MarginalRelief:  money.Zero(cur),
	}
	if in.TaxableProfit.Sign() <= 0 { // a loss or nil profit bears no tax (losses not modelled)
		res.Band = "none"
		return res, nil
	}

	// The limits shrink by (days / 365) and by the number of associated companies.
	days := in.Days
	if days <= 0 {
		days = 365
	}
	factor := big.NewRat(int64(days), 365)
	factor.Quo(factor, big.NewRat(int64(1+in.AssociatedCompanies), 1))
	lower := new(big.Rat).Mul(toRat(rates.LowerLimit), factor)
	upper := new(big.Rat).Mul(toRat(rates.UpperLimit), factor)

	n := toRat(in.TaxableProfit) // taxable total profits
	a := toRat(augmented)        // augmented profits (tested against the limits)

	var chargeRat, mrRat *big.Rat
	switch {
	case a.Cmp(lower) <= 0:
		res.Band = "small profits"
		chargeRat = new(big.Rat).Mul(n, rates.SmallProfitsRate.Rat())
		mrRat = new(big.Rat)
	case a.Cmp(upper) >= 0:
		res.Band = "main rate"
		chargeRat = new(big.Rat).Mul(n, rates.MainRate.Rat())
		mrRat = new(big.Rat)
	default:
		res.Band = "marginal"
		mainCharge := new(big.Rat).Mul(n, rates.MainRate.Rat())
		// Marginal Relief = (F × (U − A)) × (N ÷ A)
		f := big.NewRat(rates.MRNumerator, rates.MRDenominator)
		mrRat = new(big.Rat).Mul(f, new(big.Rat).Sub(upper, a))
		mrRat.Mul(mrRat, new(big.Rat).Quo(n, a))
		chargeRat = new(big.Rat).Sub(mainCharge, mrRat)
	}

	res.Charge = money.FromRat(cur, chargeRat, money.HalfUp)
	res.MarginalRelief = money.FromRat(cur, mrRat, money.HalfUp)
	if eff, cond := (decimal.Context{Precision: 9, Rounding: decimal.RoundHalfEven}).
		Divide(res.Charge.Amount(), in.TaxableProfit.Amount()); !cond.Has(decimal.DivisionByZero) {
		res.EffectiveRate = eff
	}
	return res, nil
}
