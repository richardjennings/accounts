package ledger

// AccountType classifies an account by its place in the accounting equation and
// fixes its "normal" (increasing) side. Assets and expenses increase on the debit
// side; liabilities, equity, and income increase on the credit side.
type AccountType uint8

const (
	Asset AccountType = iota
	Liability
	Equity
	Income
	Expense
)

func (t AccountType) String() string {
	switch t {
	case Asset:
		return "Asset"
	case Liability:
		return "Liability"
	case Equity:
		return "Equity"
	case Income:
		return "Income"
	case Expense:
		return "Expense"
	default:
		return "Unknown"
	}
}

// NormalSide is the side on which an account of this type increases: debit for
// assets and expenses, credit for liabilities, equity, and income.
func (t AccountType) NormalSide() Side {
	switch t {
	case Asset, Expense:
		return Debit
	default:
		return Credit
	}
}

// Account is a node in the chart of accounts. Code is the stable identifier used
// by postings (e.g. "1200" for a bank account); Name is for display.
type Account struct {
	Code string
	Name string
	Type AccountType
}

// Side is the debit or credit side of a posting.
type Side uint8

const (
	Debit Side = iota
	Credit
)

func (s Side) String() string {
	if s == Credit {
		return "Cr"
	}
	return "Dr"
}

func (s Side) opposite() Side {
	if s == Debit {
		return Credit
	}
	return Debit
}
