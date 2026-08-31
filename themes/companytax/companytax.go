// Package companytax provides the Company Tax theme's operations: providing for the
// corporation-tax charge at the year end, and paying it to HMRC. The charge is an
// expense in the profit & loss; the payable is a balance-sheet liability until paid.
// What the charge should be — profit adjusted for disallowables and allowances — is
// a computation for a later layer, not a posting rule.
package companytax

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

// Provision records the year-end corporation-tax charge: debit the tax charge (an
// expense), credit corporation tax payable (a liability).
type Provision struct {
	Date    ledger.Date
	Ref     string
	Amount  money.Money
	Charge  string // defaults to chart.CorpTaxCharge
	Payable string // defaults to chart.CorpTaxPayable
}

func (p Provision) Journal() (ledger.Journal, error) {
	j, err := ledger.NewJournal(p.Date, "Corporation tax provision "+p.Ref,
		ledger.Posting{Account: acct(p.Charge, chart.CorpTaxCharge), Side: ledger.Debit, Amount: p.Amount},
		ledger.Posting{Account: acct(p.Payable, chart.CorpTaxPayable), Side: ledger.Credit, Amount: p.Amount},
	)
	if err != nil {
		return ledger.Journal{}, err
	}
	return j.WithRef(p.Ref), nil
}

// Payment records paying corporation tax to HMRC: debit the payable, credit the bank.
type Payment struct {
	Date    ledger.Date
	Ref     string
	Amount  money.Money
	Payable string // defaults to chart.CorpTaxPayable
	Bank    string // defaults to chart.Bank
}

func (p Payment) Journal() (ledger.Journal, error) {
	j, err := ledger.NewJournal(p.Date, "Corporation tax payment "+p.Ref,
		ledger.Posting{Account: acct(p.Payable, chart.CorpTaxPayable), Side: ledger.Debit, Amount: p.Amount},
		ledger.Posting{Account: acct(p.Bank, chart.Bank), Side: ledger.Credit, Amount: p.Amount},
	)
	if err != nil {
		return ledger.Journal{}, err
	}
	return j.WithRef(p.Ref), nil
}
