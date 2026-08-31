// Package ledger is a framework-neutral double-entry bookkeeping engine.
//
// It owns one invariant — every journal balances, debits equal credits — and the
// structures that rest on it: a chart of accounts, immutable posted journals,
// account balances, and the trial balance. It knows nothing about UK GAAP,
// FRS 105 vs FRS 102, VAT, invoices, or presentation: those concerns generate
// journals (above) or read balances (above). The ledger just keeps the books.
//
// Amounts are money.Money, so balancing is exact: a journal either nets to zero
// or is rejected at construction.
package ledger

import "errors"

var (
	ErrUnbalanced       = errors.New("ledger: journal does not balance")
	ErrTooFewPostings   = errors.New("ledger: journal needs at least two postings")
	ErrNonPositive      = errors.New("ledger: posting amount must be positive")
	ErrMixedCurrency    = errors.New("ledger: mixed-currency journal not supported")
	ErrWrongCurrency    = errors.New("ledger: journal currency does not match the book")
	ErrInvalidSide      = errors.New("ledger: invalid posting side")
	ErrUnknownAccount   = errors.New("ledger: unknown account")
	ErrDuplicateAccount = errors.New("ledger: duplicate account code")
	ErrEmptyCode        = errors.New("ledger: account code must not be empty")
)
