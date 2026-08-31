// Package dividends applies the company-law rule that a dividend may only be paid
// out of distributable reserves — broadly, accumulated realised profits less
// distributions already made. It reads those reserves from the ledger (via the
// balance sheet) and tests a proposed dividend against them; declaring more is
// unlawful. It gates the Pay Yourself DeclareDividend operation.
package dividends

import (
	"github.com/richardjennings/accounts/chart"
	"github.com/richardjennings/accounts/ledger"
	"github.com/richardjennings/accounts/money"
	"github.com/richardjennings/accounts/report"
)

// Available returns the distributable reserves as at a date: total equity less the
// non-distributable share capital. Extend nonDistributable with any other reserves
// (share premium, capital redemption) a richer chart introduces.
func Available(book *ledger.Book, asAt ledger.Date, nonDistributable ...string) (money.Money, error) {
	if len(nonDistributable) == 0 {
		nonDistributable = []string{chart.ShareCapital}
	}
	bs, err := report.NewBalanceSheet(book, asAt)
	if err != nil {
		return money.Money{}, err
	}
	available := bs.TotalEquity
	for _, code := range nonDistributable {
		bal, err := book.Balance(code)
		if err != nil {
			return money.Money{}, err
		}
		if available, err = available.Sub(bal); err != nil {
			return money.Money{}, err
		}
	}
	return available, nil
}

// Decision is the outcome of testing a proposed dividend against reserves.
type Decision struct {
	Requested money.Money
	Available money.Money
	Lawful    bool
	Shortfall money.Money // zero when lawful
}

// Check reports whether declaring amount as at asAt is covered by distributable
// reserves.
func Check(book *ledger.Book, asAt ledger.Date, amount money.Money) (Decision, error) {
	available, err := Available(book, asAt)
	if err != nil {
		return Decision{}, err
	}
	cmp, err := amount.Cmp(available)
	if err != nil {
		return Decision{}, err
	}
	d := Decision{
		Requested: amount,
		Available: available,
		Lawful:    cmp <= 0,
		Shortfall: money.Zero(amount.Currency()),
	}
	if !d.Lawful {
		if d.Shortfall, err = amount.Sub(available); err != nil {
			return Decision{}, err
		}
	}
	return d, nil
}
