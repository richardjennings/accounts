// Package expenses provides the Expenses theme's operations — supplier bills,
// payments, and costs paid straight from the bank. A cost is always a debit to an
// expense account; the matching credit says how it was funded (a creditor if
// unpaid, the bank if paid).
package expenses

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

func orZero(m money.Money, cur money.Currency) money.Money {
	if m.Currency().Code == "" {
		return money.Zero(cur)
	}
	return m
}

// Bill records a supplier bill to be paid later: debit an expense, credit trade
// creditors (money you now owe).
type Bill struct {
	Date       ledger.Date
	Ref        string
	Supplier   string
	Amount     money.Money // net (VAT-exclusive)
	VAT        money.Money // input VAT reclaimed; zero/unset for no VAT
	Expense    string      // which expense account; defaults to chart.OfficeAdmin
	Creditors  string      // defaults to chart.TradeCreditors
	VATAccount string      // defaults to chart.VAT
}

func (b Bill) Journal() (ledger.Journal, error) {
	cur := b.Amount.Currency()
	vat := orZero(b.VAT, cur)
	gross, err := b.Amount.Add(vat)
	if err != nil {
		return ledger.Journal{}, err
	}
	narr := "Bill " + b.Ref
	if b.Supplier != "" {
		narr += " — " + b.Supplier
	}
	postings := []ledger.Posting{
		{Account: acct(b.Expense, chart.OfficeAdmin), Side: ledger.Debit, Amount: b.Amount},
	}
	if vat.IsPositive() {
		postings = append(postings, ledger.Posting{Account: acct(b.VATAccount, chart.VAT), Side: ledger.Debit, Amount: vat})
	}
	postings = append(postings, ledger.Posting{Account: acct(b.Creditors, chart.TradeCreditors), Side: ledger.Credit, Amount: gross})
	j, err := ledger.NewJournal(b.Date, narr, postings...)
	if err != nil {
		return ledger.Journal{}, err
	}
	return j.WithRef(b.Ref), nil
}

// Payment records paying a supplier: debit trade creditors (the debt is cleared),
// credit the bank.
type Payment struct {
	Date      ledger.Date
	Ref       string
	Amount    money.Money
	Creditors string // defaults to chart.TradeCreditors
	Bank      string // defaults to chart.Bank
}

func (p Payment) Journal() (ledger.Journal, error) {
	j, err := ledger.NewJournal(p.Date, "Supplier payment "+p.Ref,
		ledger.Posting{Account: acct(p.Creditors, chart.TradeCreditors), Side: ledger.Debit, Amount: p.Amount},
		ledger.Posting{Account: acct(p.Bank, chart.Bank), Side: ledger.Credit, Amount: p.Amount},
	)
	if err != nil {
		return ledger.Journal{}, err
	}
	return j.WithRef(p.Ref), nil
}

// DirectExpense records a cost paid straight from the bank, with no bill in
// between: debit an expense, credit the bank.
type DirectExpense struct {
	Date       ledger.Date
	Ref        string
	Payee      string
	Amount     money.Money // net
	VAT        money.Money // input VAT reclaimed; zero/unset for no VAT
	Expense    string      // which expense account; defaults to chart.OfficeAdmin
	Bank       string      // defaults to chart.Bank
	VATAccount string      // defaults to chart.VAT
}

func (d DirectExpense) Journal() (ledger.Journal, error) {
	cur := d.Amount.Currency()
	vat := orZero(d.VAT, cur)
	gross, err := d.Amount.Add(vat)
	if err != nil {
		return ledger.Journal{}, err
	}
	narr := "Expense " + d.Ref
	if d.Payee != "" {
		narr += " — " + d.Payee
	}
	postings := []ledger.Posting{
		{Account: acct(d.Expense, chart.OfficeAdmin), Side: ledger.Debit, Amount: d.Amount},
	}
	if vat.IsPositive() {
		postings = append(postings, ledger.Posting{Account: acct(d.VATAccount, chart.VAT), Side: ledger.Debit, Amount: vat})
	}
	postings = append(postings, ledger.Posting{Account: acct(d.Bank, chart.Bank), Side: ledger.Credit, Amount: gross})
	j, err := ledger.NewJournal(d.Date, narr, postings...)
	if err != nil {
		return ledger.Journal{}, err
	}
	return j.WithRef(d.Ref), nil
}

// CreditNote records a supplier credit note or refund — goods returned or an
// overcharge put right. It reduces the expense (credit) and reverses the input VAT
// reclaimed (credit), against either trade creditors (less to pay) or the bank (a
// refund received). It is the mirror of a sales credit note.
type CreditNote struct {
	Date       ledger.Date
	Ref        string
	Supplier   string
	Amount     money.Money // net
	VAT        money.Money // input VAT being reversed; zero/unset for none
	Expense    string      // expense account to reduce; defaults to chart.OfficeAdmin
	Against    string      // trade creditors or bank; defaults to chart.TradeCreditors
	VATAccount string      // defaults to chart.VAT
}

func (c CreditNote) Journal() (ledger.Journal, error) {
	cur := c.Amount.Currency()
	vat := orZero(c.VAT, cur)
	gross, err := c.Amount.Add(vat)
	if err != nil {
		return ledger.Journal{}, err
	}
	narr := "Supplier credit note " + c.Ref
	if c.Supplier != "" {
		narr += " — " + c.Supplier
	}
	postings := []ledger.Posting{
		{Account: acct(c.Against, chart.TradeCreditors), Side: ledger.Debit, Amount: gross},
		{Account: acct(c.Expense, chart.OfficeAdmin), Side: ledger.Credit, Amount: c.Amount},
	}
	if vat.IsPositive() {
		postings = append(postings, ledger.Posting{Account: acct(c.VATAccount, chart.VAT), Side: ledger.Credit, Amount: vat})
	}
	j, err := ledger.NewJournal(c.Date, narr, postings...)
	if err != nil {
		return ledger.Journal{}, err
	}
	return j.WithRef(c.Ref), nil
}
