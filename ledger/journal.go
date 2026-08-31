package ledger

import (
	"fmt"

	"github.com/richardjennings/accounts/money"
)

// Posting moves an amount to one account on one side. Amount is always positive;
// direction is carried by Side, so a posting never depends on a sign convention.
type Posting struct {
	Account string      // account code
	Side    Side        // Debit or Credit
	Amount  money.Money // must be positive
}

// Journal is a balanced set of postings recorded on a date. It is immutable once
// constructed: NewJournal copies its input, and the accessors hand back copies, so
// a posted journal can never be altered — corrections are made with Reverse.
type Journal struct {
	date      Date
	narrative string
	ref       string // optional source reference, e.g. "INV-1001"
	postings  []Posting
}

// NewJournal validates and constructs a journal. It requires at least two
// postings, each with a positive amount and a non-empty account, all in one
// currency, and debits that equal credits exactly. Anything else is an error —
// an unbalanced journal can never be constructed.
func NewJournal(date Date, narrative string, postings ...Posting) (Journal, error) {
	if len(postings) < 2 {
		return Journal{}, ErrTooFewPostings
	}
	cur := postings[0].Amount.Currency()
	debit := money.Zero(cur)
	credit := money.Zero(cur)
	cp := make([]Posting, len(postings))
	for i, p := range postings {
		if p.Account == "" {
			return Journal{}, ErrEmptyCode
		}
		if p.Amount.Currency() != cur {
			return Journal{}, fmt.Errorf("%w: %s and %s", ErrMixedCurrency, cur.Code, p.Amount.Currency().Code)
		}
		if !p.Amount.IsPositive() {
			return Journal{}, fmt.Errorf("%w: %s on %s", ErrNonPositive, p.Amount, p.Account)
		}
		var err error
		switch p.Side {
		case Debit:
			debit, err = debit.Add(p.Amount)
		case Credit:
			credit, err = credit.Add(p.Amount)
		default:
			return Journal{}, fmt.Errorf("%w: %d", ErrInvalidSide, p.Side)
		}
		if err != nil {
			return Journal{}, err
		}
		cp[i] = p
	}
	if !debit.Equal(credit) {
		return Journal{}, fmt.Errorf("%w: debits %s vs credits %s", ErrUnbalanced, debit, credit)
	}
	return Journal{date: date, narrative: narrative, postings: cp}, nil
}

// WithRef returns a copy of the journal tagged with a source reference.
func (j Journal) WithRef(ref string) Journal {
	j.ref = ref // j is a value copy; the postings slice is shared but never mutated
	return j
}

// Reverse returns the reversing journal: the same postings with debit and credit
// swapped, on a new date. Because the original balances, so does the reversal —
// this is how a posted journal is corrected without mutating it.
func (j Journal) Reverse(date Date, narrative string) Journal {
	rev := make([]Posting, len(j.postings))
	for i, p := range j.postings {
		rev[i] = Posting{Account: p.Account, Side: p.Side.opposite(), Amount: p.Amount}
	}
	return Journal{date: date, narrative: narrative, ref: j.ref, postings: rev}
}

// Date returns the journal date.
func (j Journal) Date() Date { return j.date }

// Narrative returns the journal narrative.
func (j Journal) Narrative() string { return j.narrative }

// Ref returns the source reference, if any.
func (j Journal) Ref() string { return j.ref }

// Currency returns the journal's currency (empty for the zero value).
func (j Journal) Currency() money.Currency {
	if len(j.postings) == 0 {
		return money.Currency{}
	}
	return j.postings[0].Amount.Currency()
}

// Postings returns a copy of the journal's postings.
func (j Journal) Postings() []Posting {
	out := make([]Posting, len(j.postings))
	copy(out, j.postings)
	return out
}
