// Package vat computes UK VAT for accounting and invoicing — output VAT charged on
// sales and input VAT paid on purchases — using standard VAT accounting. It does
// NOT produce or file a VAT return; it records the VAT amount on each transaction
// and in the VAT control account. VAT is rounded to the penny half-up, HMRC's
// standard method (VAT Notice 700 §17.6: below half a penny down, half or more up).
package vat

import (
	"github.com/richardjennings/accounts/money"
	"github.com/richardjennings/decimal"
)

// Rate is a VAT rate.
type Rate struct {
	Code     string
	Label    string
	Fraction decimal.Decimal
}

var (
	Standard = Rate{"standard", "Standard 20%", decimal.MustParse("0.20")}
	Reduced  = Rate{"reduced", "Reduced 5%", decimal.MustParse("0.05")}
	Zero     = Rate{"zero", "Zero 0%", decimal.MustParse("0")}
	None     = Rate{"none", "No VAT", decimal.MustParse("0")}
)

// Rates lists the selectable rates.
var Rates = []Rate{Standard, Reduced, Zero, None}

// ByCode returns the rate for a code, defaulting to None (no VAT).
func ByCode(code string) Rate {
	for _, r := range Rates {
		if r.Code == code {
			return r
		}
	}
	return None
}

// On returns the VAT on a net (VAT-exclusive) amount, rounded to the penny half-up.
func (r Rate) On(net money.Money) money.Money {
	v, err := net.Mul(r.Fraction, money.HalfUp)
	if err != nil {
		return money.Zero(net.Currency())
	}
	return v
}
