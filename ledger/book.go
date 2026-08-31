package ledger

import (
	"fmt"
	"sort"

	"github.com/richardjennings/accounts/money"
)

// Book is a set of books of account: a chart of accounts plus an append-only list
// of posted journals, all in one base currency. Journals are only ever appended;
// balances and the trial balance are derived from them on demand.
type Book struct {
	base     money.Currency
	accounts map[string]Account
	journals []Journal
}

// NewBook returns an empty book keeping accounts in the given base currency.
func NewBook(base money.Currency) *Book {
	return &Book{base: base, accounts: map[string]Account{}}
}

// Base returns the book's base currency.
func (b *Book) Base() money.Currency { return b.base }

// AddAccount adds an account to the chart, rejecting an empty or duplicate code.
func (b *Book) AddAccount(a Account) error {
	if a.Code == "" {
		return ErrEmptyCode
	}
	if _, ok := b.accounts[a.Code]; ok {
		return fmt.Errorf("%w: %s", ErrDuplicateAccount, a.Code)
	}
	b.accounts[a.Code] = a
	return nil
}

// AddAccounts adds several accounts, stopping at the first error.
func (b *Book) AddAccounts(accts ...Account) error {
	for _, a := range accts {
		if err := b.AddAccount(a); err != nil {
			return err
		}
	}
	return nil
}

// Account returns the account with the given code.
func (b *Book) Account(code string) (Account, bool) {
	a, ok := b.accounts[code]
	return a, ok
}

// Post appends a journal after checking its currency matches the book and every
// posting references a known account. The journal is already balanced (NewJournal
// guarantees it), so nothing here can put the books out of balance.
func (b *Book) Post(j Journal) error {
	if len(j.postings) < 2 {
		return ErrTooFewPostings
	}
	if cur := j.Currency(); cur != b.base {
		return fmt.Errorf("%w: book is %s, journal is %s", ErrWrongCurrency, b.base.Code, cur.Code)
	}
	for _, p := range j.postings {
		if _, ok := b.accounts[p.Account]; !ok {
			return fmt.Errorf("%w: %s", ErrUnknownAccount, p.Account)
		}
	}
	b.journals = append(b.journals, j)
	return nil
}

// Journals returns a copy of the posted journals in posting order.
func (b *Book) Journals() []Journal {
	out := make([]Journal, len(b.journals))
	copy(out, b.journals)
	return out
}

// netDebit sums an account's postings as debits-minus-credits (a debit-positive
// net), the raw ledger balance before any presentation convention is applied.
func (b *Book) netDebit(code string) (money.Money, error) {
	return b.netDebitFiltered(code, func(Date) bool { return true })
}

// netDebitFiltered is netDebit restricted to journals whose date satisfies keep.
func (b *Book) netDebitFiltered(code string, keep func(Date) bool) (money.Money, error) {
	net := money.Zero(b.base)
	for _, j := range b.journals {
		if !keep(j.date) {
			continue
		}
		for _, p := range j.postings {
			if p.Account != code {
				continue
			}
			var err error
			if p.Side == Debit {
				net, err = net.Add(p.Amount)
			} else {
				net, err = net.Sub(p.Amount)
			}
			if err != nil {
				return money.Money{}, err
			}
		}
	}
	return net, nil
}

// normalBalance converts a debit-positive net into the account's natural sense:
// positive on its normal side.
func normalBalance(t AccountType, netDebit money.Money) money.Money {
	if t.NormalSide() == Credit {
		return netDebit.Neg()
	}
	return netDebit
}

// Balance returns an account's balance in its natural sense: positive on the
// account's normal side. A bank asset reads positive when in funds (negative when
// overdrawn); sales income and share capital read positive.
func (b *Book) Balance(code string) (money.Money, error) {
	acct, ok := b.accounts[code]
	if !ok {
		return money.Money{}, fmt.Errorf("%w: %s", ErrUnknownAccount, code)
	}
	net, err := b.netDebit(code)
	if err != nil {
		return money.Money{}, err
	}
	return normalBalance(acct.Type, net), nil
}

// BalanceAsAt returns an account's balance including only postings dated on or
// before on — the figure a balance sheet needs.
func (b *Book) BalanceAsAt(code string, on Date) (money.Money, error) {
	acct, ok := b.accounts[code]
	if !ok {
		return money.Money{}, fmt.Errorf("%w: %s", ErrUnknownAccount, code)
	}
	net, err := b.netDebitFiltered(code, func(d Date) bool { return !on.Before(d) })
	if err != nil {
		return money.Money{}, err
	}
	return normalBalance(acct.Type, net), nil
}

// MovementBetween returns an account's net movement over [from, to] inclusive —
// the figure a profit & loss account needs for a period.
func (b *Book) MovementBetween(code string, from, to Date) (money.Money, error) {
	acct, ok := b.accounts[code]
	if !ok {
		return money.Money{}, fmt.Errorf("%w: %s", ErrUnknownAccount, code)
	}
	net, err := b.netDebitFiltered(code, func(d Date) bool { return !d.Before(from) && !to.Before(d) })
	if err != nil {
		return money.Money{}, err
	}
	return normalBalance(acct.Type, net), nil
}

// Accounts returns the chart of accounts, ordered by code.
func (b *Book) Accounts() []Account {
	out := make([]Account, 0, len(b.accounts))
	for _, a := range b.accounts {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out
}

// TrialBalanceLine is one account's entry in the trial balance: its net balance
// placed in the debit or credit column (the other column is zero).
type TrialBalanceLine struct {
	Account Account
	Debit   money.Money
	Credit  money.Money
}

// TrialBalance is every non-zero account balance sorted into debit and credit
// columns. Because every journal balances, the column totals are always equal.
type TrialBalance struct {
	Lines       []TrialBalanceLine
	TotalDebit  money.Money
	TotalCredit money.Money
}

// InBalance reports whether the debit and credit totals match (they always should).
func (tb TrialBalance) InBalance() bool { return tb.TotalDebit.Equal(tb.TotalCredit) }

// TrialBalance builds the trial balance over all accounts with a non-zero balance,
// ordered by account code.
func (b *Book) TrialBalance() (TrialBalance, error) {
	codes := make([]string, 0, len(b.accounts))
	for code := range b.accounts {
		codes = append(codes, code)
	}
	sort.Strings(codes)

	tb := TrialBalance{TotalDebit: money.Zero(b.base), TotalCredit: money.Zero(b.base)}
	for _, code := range codes {
		net, err := b.netDebit(code)
		if err != nil {
			return TrialBalance{}, err
		}
		line := TrialBalanceLine{Account: b.accounts[code], Debit: money.Zero(b.base), Credit: money.Zero(b.base)}
		switch net.Sign() {
		case 1:
			line.Debit = net
			tb.TotalDebit, _ = tb.TotalDebit.Add(net)
		case -1:
			line.Credit = net.Neg()
			tb.TotalCredit, _ = tb.TotalCredit.Add(net.Neg())
		default:
			continue // zero balance: omit
		}
		tb.Lines = append(tb.Lines, line)
	}
	return tb, nil
}
