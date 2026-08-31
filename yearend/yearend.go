// Package yearend closes an accounting period: it transfers the profit (or loss) for
// the year into retained earnings and zeroes the profit-and-loss accounts, so the
// next year starts from a clean slate and reserves roll forward correctly. Dividends
// declared in the year are closed to retained earnings too, so retained earnings ends
// the year as prior reserves + profit − distributions.
//
// It builds one balanced journal; posting it and then locking the period is the
// caller's job.
package yearend

import (
	"fmt"

	"github.com/richardjennings/accounts/ledger"
	"github.com/richardjennings/accounts/money"
)

// signedDebit returns an account's balance as at a date as a debit-positive amount
// (a debit balance is positive, a credit balance negative), regardless of the
// account's normal side.
func signedDebit(book *ledger.Book, ac ledger.Account, asAt ledger.Date) (money.Money, error) {
	b, err := book.BalanceAsAt(ac.Code, asAt)
	if err != nil {
		return money.Money{}, err
	}
	if ac.Type.NormalSide() == ledger.Debit {
		return b, nil // debit-normal: BalanceAsAt is already debit-positive
	}
	return b.Neg(), nil // credit-normal: flip so a credit balance is negative
}

// CloseEntry builds the year-end closing journal as at asAt: every income and expense
// account (plus any extra accounts in alsoClose, e.g. dividends) is zeroed, and the
// net is taken to retainedCode. Returns an error if there is nothing to close.
func CloseEntry(book *ledger.Book, asAt ledger.Date, ref, retainedCode string, alsoClose ...string) (ledger.Journal, error) {
	base := book.Base()
	extra := map[string]bool{}
	for _, c := range alsoClose {
		extra[c] = true
	}

	var postings []ledger.Posting
	netDebit := money.Zero(base) // sum of the debit-positive balances being cleared
	for _, ac := range book.Accounts() {
		if ac.Code == retainedCode {
			continue
		}
		if ac.Type != ledger.Income && ac.Type != ledger.Expense && !extra[ac.Code] {
			continue
		}
		d, err := signedDebit(book, ac, asAt)
		if err != nil {
			return ledger.Journal{}, err
		}
		if d.IsZero() {
			continue
		}
		// Zero the account by posting the opposite of its balance.
		if d.IsPositive() {
			postings = append(postings, ledger.Posting{Account: ac.Code, Side: ledger.Credit, Amount: d})
		} else {
			postings = append(postings, ledger.Posting{Account: ac.Code, Side: ledger.Debit, Amount: d.Neg()})
		}
		if netDebit, err = netDebit.Add(d); err != nil {
			return ledger.Journal{}, err
		}
	}
	if len(postings) == 0 {
		return ledger.Journal{}, fmt.Errorf("yearend: nothing to close for %s", asAt)
	}

	// The retained-earnings posting balances the journal: it absorbs the net that was
	// cleared out of the P&L and dividends accounts.
	if netDebit.IsPositive() {
		postings = append(postings, ledger.Posting{Account: retainedCode, Side: ledger.Debit, Amount: netDebit})
	} else {
		postings = append(postings, ledger.Posting{Account: retainedCode, Side: ledger.Credit, Amount: netDebit.Neg()})
	}

	j, err := ledger.NewJournal(asAt, "Year-end close "+ref, postings...)
	if err != nil {
		return ledger.Journal{}, err
	}
	return j.WithRef(ref), nil
}
