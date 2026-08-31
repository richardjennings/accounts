// Package capital provides the equity-financing operations that move money between
// the company and its share capital. Issuing shares for cash brings money in and
// raises share capital by the nominal value of the shares — the one point where the
// statutory share register and the ledger meet.
package capital

import (
	"github.com/richardjennings/accounts/chart"
	"github.com/richardjennings/accounts/ledger"
	"github.com/richardjennings/accounts/money"
)

func acct(override, def string) string {
	if override == "" {
		return def
	}
	return override
}

// IssueShares records issuing shares for cash at their nominal value: debit the
// bank (cash received), credit share capital (the nominal value issued).
type IssueShares struct {
	Date    ledger.Date
	Ref     string
	Amount  money.Money // nominal value issued (number of shares × nominal value)
	Bank    string      // defaults to chart.Bank
	Capital string      // defaults to chart.ShareCapital
}

func (s IssueShares) Journal() (ledger.Journal, error) {
	j, err := ledger.NewJournal(s.Date, "Share capital issued "+s.Ref,
		ledger.Posting{Account: acct(s.Bank, chart.Bank), Side: ledger.Debit, Amount: s.Amount},
		ledger.Posting{Account: acct(s.Capital, chart.ShareCapital), Side: ledger.Credit, Amount: s.Amount},
	)
	if err != nil {
		return ledger.Journal{}, err
	}
	return j.WithRef(s.Ref), nil
}
