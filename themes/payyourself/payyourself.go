// Package payyourself provides the Pay Yourself theme's operations for an owner-
// director: salary (with PAYE/NIC withheld), dividends, and movements on the
// director's loan account. Salary is a company expense; a dividend is not — it is a
// distribution of profit, so it reduces equity rather than the profit & loss. The
// director's loan account is the running tab between company and director: a
// liability when the company owes them, an asset when they owe the company.
package payyourself

import (
	"fmt"

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

// Salary records a director's salary run. Gross is the employee's pay; TaxNIC is
// the employee PAYE + NIC withheld; EmployerNIC is the company's secondary NIC — an
// extra cost on top of gross. It debits salaries (gross) and employer's NIC, and
// credits PAYE/NIC (everything owed to HMRC) and the bank (the net) — omitting any
// leg that is zero.
type Salary struct {
	Date               ledger.Date
	Ref                string
	Gross              money.Money
	TaxNIC             money.Money // employee PAYE + NIC withheld; may be zero/unset
	EmployerNIC        money.Money // employer secondary NIC; may be zero/unset
	EmployeePension    money.Money // employee workplace-pension contribution (withheld); may be zero/unset
	EmployerPension    money.Money // employer workplace-pension contribution; may be zero/unset
	Expense            string      // gross salary account; defaults to chart.Salaries
	EmployerNICExpense string      // defaults to chart.EmployerNIC
	PensionExpense     string      // defaults to chart.PensionCosts
	PAYENIC            string      // defaults to chart.PAYENIC
	PensionPayable     string      // defaults to chart.PensionPayable
	Bank               string      // where the net is paid; defaults to chart.Bank
}

func (s Salary) Journal() (ledger.Journal, error) {
	cur := s.Gross.Currency()
	taxNIC := zeroIfUnset(s.TaxNIC, cur)
	erNIC := zeroIfUnset(s.EmployerNIC, cur)
	eePen := zeroIfUnset(s.EmployeePension, cur)
	erPen := zeroIfUnset(s.EmployerPension, cur)

	net, err := s.Gross.Sub(taxNIC) // net pay: gross less tax/NIC and the employee's pension
	if err != nil {
		return ledger.Journal{}, err
	}
	if net, err = net.Sub(eePen); err != nil {
		return ledger.Journal{}, err
	}
	if net.IsNegative() {
		return ledger.Journal{}, fmt.Errorf("payyourself: deductions exceed gross %s", s.Gross)
	}
	payeOwed, err := taxNIC.Add(erNIC) // employee tax/NIC plus employer NIC, owed to HMRC
	if err != nil {
		return ledger.Journal{}, err
	}
	pensionOwed, err := eePen.Add(erPen) // employee plus employer pension, owed to the provider
	if err != nil {
		return ledger.Journal{}, err
	}
	postings := []ledger.Posting{
		{Account: acct(s.Expense, chart.Salaries), Side: ledger.Debit, Amount: s.Gross},
	}
	if erNIC.IsPositive() {
		postings = append(postings, ledger.Posting{Account: acct(s.EmployerNICExpense, chart.EmployerNIC), Side: ledger.Debit, Amount: erNIC})
	}
	if erPen.IsPositive() {
		postings = append(postings, ledger.Posting{Account: acct(s.PensionExpense, chart.PensionCosts), Side: ledger.Debit, Amount: erPen})
	}
	if payeOwed.IsPositive() {
		postings = append(postings, ledger.Posting{Account: acct(s.PAYENIC, chart.PAYENIC), Side: ledger.Credit, Amount: payeOwed})
	}
	if pensionOwed.IsPositive() {
		postings = append(postings, ledger.Posting{Account: acct(s.PensionPayable, chart.PensionPayable), Side: ledger.Credit, Amount: pensionOwed})
	}
	if net.IsPositive() {
		postings = append(postings, ledger.Posting{Account: acct(s.Bank, chart.Bank), Side: ledger.Credit, Amount: net})
	}
	j, err := ledger.NewJournal(s.Date, "Salary "+s.Ref, postings...)
	if err != nil {
		return ledger.Journal{}, err
	}
	return j.WithRef(s.Ref), nil
}

func zeroIfUnset(m money.Money, cur money.Currency) money.Money {
	if m.Currency().Code == "" {
		return money.Zero(cur)
	}
	return m
}

// PayPAYE records paying withheld PAYE/NIC over to HMRC: debit the PAYE/NIC
// liability, credit the bank.
type PayPAYE struct {
	Date    ledger.Date
	Ref     string
	Amount  money.Money
	PAYENIC string // defaults to chart.PAYENIC
	Bank    string // defaults to chart.Bank
}

func (p PayPAYE) Journal() (ledger.Journal, error) {
	j, err := ledger.NewJournal(p.Date, "PAYE/NIC payment "+p.Ref,
		ledger.Posting{Account: acct(p.PAYENIC, chart.PAYENIC), Side: ledger.Debit, Amount: p.Amount},
		ledger.Posting{Account: acct(p.Bank, chart.Bank), Side: ledger.Credit, Amount: p.Amount},
	)
	if err != nil {
		return ledger.Journal{}, err
	}
	return j.WithRef(p.Ref), nil
}

// DeclareDividend records declaring a dividend to the director-shareholder: debit
// dividends (a distribution of profit, reducing equity), credit the director's loan
// account (the company now owes them).
type DeclareDividend struct {
	Date      ledger.Date
	Ref       string
	Amount    money.Money
	Dividends string // defaults to chart.Dividends
	DLA       string // defaults to chart.DirectorsLoan
}

func (d DeclareDividend) Journal() (ledger.Journal, error) {
	j, err := ledger.NewJournal(d.Date, "Dividend declared "+d.Ref,
		ledger.Posting{Account: acct(d.Dividends, chart.Dividends), Side: ledger.Debit, Amount: d.Amount},
		ledger.Posting{Account: acct(d.DLA, chart.DirectorsLoan), Side: ledger.Credit, Amount: d.Amount},
	)
	if err != nil {
		return ledger.Journal{}, err
	}
	return j.WithRef(d.Ref), nil
}

// PayDividend records paying a declared dividend: debit the director's loan account,
// credit the bank.
type PayDividend struct {
	Date   ledger.Date
	Ref    string
	Amount money.Money
	DLA    string // defaults to chart.DirectorsLoan
	Bank   string // defaults to chart.Bank
}

func (p PayDividend) Journal() (ledger.Journal, error) {
	j, err := ledger.NewJournal(p.Date, "Dividend paid "+p.Ref,
		ledger.Posting{Account: acct(p.DLA, chart.DirectorsLoan), Side: ledger.Debit, Amount: p.Amount},
		ledger.Posting{Account: acct(p.Bank, chart.Bank), Side: ledger.Credit, Amount: p.Amount},
	)
	if err != nil {
		return ledger.Journal{}, err
	}
	return j.WithRef(p.Ref), nil
}

// IntroduceFunds records the director lending money into the company: debit the
// bank, credit the director's loan account (the company now owes them).
type IntroduceFunds struct {
	Date   ledger.Date
	Ref    string
	Amount money.Money
	Bank   string // defaults to chart.Bank
	DLA    string // defaults to chart.DirectorsLoan
}

func (f IntroduceFunds) Journal() (ledger.Journal, error) {
	j, err := ledger.NewJournal(f.Date, "Director funds introduced "+f.Ref,
		ledger.Posting{Account: acct(f.Bank, chart.Bank), Side: ledger.Debit, Amount: f.Amount},
		ledger.Posting{Account: acct(f.DLA, chart.DirectorsLoan), Side: ledger.Credit, Amount: f.Amount},
	)
	if err != nil {
		return ledger.Journal{}, err
	}
	return j.WithRef(f.Ref), nil
}

// DrawFunds records the director taking money out: debit the director's loan
// account, credit the bank.
type DrawFunds struct {
	Date   ledger.Date
	Ref    string
	Amount money.Money
	DLA    string // defaults to chart.DirectorsLoan
	Bank   string // defaults to chart.Bank
}

func (d DrawFunds) Journal() (ledger.Journal, error) {
	j, err := ledger.NewJournal(d.Date, "Director drawings "+d.Ref,
		ledger.Posting{Account: acct(d.DLA, chart.DirectorsLoan), Side: ledger.Debit, Amount: d.Amount},
		ledger.Posting{Account: acct(d.Bank, chart.Bank), Side: ledger.Credit, Amount: d.Amount},
	)
	if err != nil {
		return ledger.Journal{}, err
	}
	return j.WithRef(d.Ref), nil
}
