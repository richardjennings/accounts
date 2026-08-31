// Package banking provides the Banking theme's operations — transfers between the
// company's own accounts, interest received, and bank charges. A bank account is an
// asset, so money in is a debit and money out a credit. Reconciliation itself posts
// nothing: it matches imported statement lines to postings that already exist.
package banking

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

// Transfer moves money between two of the company's own accounts (e.g. bank to
// cash): debit the destination, credit the source. No money enters or leaves the
// business.
type Transfer struct {
	Date   ledger.Date
	Ref    string
	Amount money.Money
	From   string // defaults to chart.Bank
	To     string // defaults to chart.Cash
}

func (tr Transfer) Journal() (ledger.Journal, error) {
	j, err := ledger.NewJournal(tr.Date, "Transfer "+tr.Ref,
		ledger.Posting{Account: acct(tr.To, chart.Cash), Side: ledger.Debit, Amount: tr.Amount},
		ledger.Posting{Account: acct(tr.From, chart.Bank), Side: ledger.Credit, Amount: tr.Amount},
	)
	if err != nil {
		return ledger.Journal{}, err
	}
	return j.WithRef(tr.Ref), nil
}

// InterestReceived records bank interest earned: debit the bank, credit other income.
type InterestReceived struct {
	Date   ledger.Date
	Ref    string
	Amount money.Money
	Bank   string // defaults to chart.Bank
	Income string // defaults to chart.OtherIncome
}

func (i InterestReceived) Journal() (ledger.Journal, error) {
	j, err := ledger.NewJournal(i.Date, "Bank interest "+i.Ref,
		ledger.Posting{Account: acct(i.Bank, chart.Bank), Side: ledger.Debit, Amount: i.Amount},
		ledger.Posting{Account: acct(i.Income, chart.OtherIncome), Side: ledger.Credit, Amount: i.Amount},
	)
	if err != nil {
		return ledger.Journal{}, err
	}
	return j.WithRef(i.Ref), nil
}

// Charge records a bank charge or fee: debit an expense, credit the bank.
type Charge struct {
	Date    ledger.Date
	Ref     string
	Amount  money.Money
	Expense string // defaults to chart.OfficeAdmin
	Bank    string // defaults to chart.Bank
}

func (c Charge) Journal() (ledger.Journal, error) {
	j, err := ledger.NewJournal(c.Date, "Bank charge "+c.Ref,
		ledger.Posting{Account: acct(c.Expense, chart.OfficeAdmin), Side: ledger.Debit, Amount: c.Amount},
		ledger.Posting{Account: acct(c.Bank, chart.Bank), Side: ledger.Credit, Amount: c.Amount},
	)
	if err != nil {
		return ledger.Journal{}, err
	}
	return j.WithRef(c.Ref), nil
}
