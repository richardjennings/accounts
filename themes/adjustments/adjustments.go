// Package adjustments provides the period-end adjustments that make profit fall in
// the right period regardless of when cash moves: accruals (costs incurred but not
// yet billed) and prepayments (costs paid in advance). Both are usually reversed in
// the following period when the real invoice arrives or the benefit is used up — the
// Journals page's reverse action does that.
package adjustments

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

// Accrual recognises a cost that belongs to this period but has not been billed yet:
// debit the expense (the cost lands now), credit accruals (a liability for what will
// be invoiced).
type Accrual struct {
	Date     ledger.Date
	Ref      string
	Note     string
	Amount   money.Money
	Expense  string // defaults to chart.OfficeAdmin
	Accruals string // defaults to chart.Accruals
}

func (a Accrual) Journal() (ledger.Journal, error) {
	narr := "Accrual " + a.Ref
	if a.Note != "" {
		narr += " — " + a.Note
	}
	j, err := ledger.NewJournal(a.Date, narr,
		ledger.Posting{Account: acct(a.Expense, chart.OfficeAdmin), Side: ledger.Debit, Amount: a.Amount},
		ledger.Posting{Account: acct(a.Accruals, chart.Accruals), Side: ledger.Credit, Amount: a.Amount},
	)
	if err != nil {
		return ledger.Journal{}, err
	}
	return j.WithRef(a.Ref), nil
}

// Prepayment defers a cost paid in advance into a later period: debit prepayments
// (an asset — future benefit), credit the expense (take it out of this period).
type Prepayment struct {
	Date        ledger.Date
	Ref         string
	Note        string
	Amount      money.Money
	Expense     string // defaults to chart.OfficeAdmin
	Prepayments string // defaults to chart.Prepayments
}

func (p Prepayment) Journal() (ledger.Journal, error) {
	narr := "Prepayment " + p.Ref
	if p.Note != "" {
		narr += " — " + p.Note
	}
	j, err := ledger.NewJournal(p.Date, narr,
		ledger.Posting{Account: acct(p.Prepayments, chart.Prepayments), Side: ledger.Debit, Amount: p.Amount},
		ledger.Posting{Account: acct(p.Expense, chart.OfficeAdmin), Side: ledger.Credit, Amount: p.Amount},
	)
	if err != nil {
		return ledger.Journal{}, err
	}
	return j.WithRef(p.Ref), nil
}
