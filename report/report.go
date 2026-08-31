// Package report derives financial statements — a profit & loss account and a
// balance sheet — from a ledger book. It is framework-neutral: a plain management
// format built from the five account types, structured so the statutory FRS 105 /
// FRS 102 §1A presentations can be layered on later. Presentation lives here, above
// the ledger, never inside it.
package report

import (
	"fmt"
	"strings"

	"github.com/richardjennings/accounts/ledger"
	"github.com/richardjennings/accounts/money"
)

// Line is one account's contribution to a statement, as a normal-side amount.
type Line struct {
	Code   string
	Name   string
	Amount money.Money
}

// ProfitAndLoss is income less expenses over a period. Profit is after every
// expense booked in the period, including any corporation-tax charge.
type ProfitAndLoss struct {
	From, To      ledger.Date
	Income        []Line
	Expenses      []Line
	TotalIncome   money.Money
	TotalExpenses money.Money
	Profit        money.Money
}

// BalanceSheet is the position as at a date. The unclosed profit for the period is
// folded into equity as a reserve, so assets equal liabilities plus equity without
// requiring year-end closing entries.
type BalanceSheet struct {
	AsAt             ledger.Date
	Assets           []Line
	Liabilities      []Line
	Equity           []Line // equity accounts, then a synthetic "Profit for the period" line
	ProfitForPeriod  money.Money
	TotalAssets      money.Money
	TotalLiabilities money.Money
	TotalEquity      money.Money
}

// NewProfitAndLoss builds the P&L for [from, to] inclusive.
func NewProfitAndLoss(book *ledger.Book, from, to ledger.Date) (ProfitAndLoss, error) {
	pl := ProfitAndLoss{
		From: from, To: to,
		TotalIncome:   money.Zero(book.Base()),
		TotalExpenses: money.Zero(book.Base()),
	}
	for _, a := range book.Accounts() {
		if a.Type != ledger.Income && a.Type != ledger.Expense {
			continue
		}
		amt, err := book.MovementBetween(a.Code, from, to)
		if err != nil {
			return ProfitAndLoss{}, err
		}
		if amt.IsZero() {
			continue
		}
		if a.Type == ledger.Income {
			pl.Income = append(pl.Income, Line{a.Code, a.Name, amt})
			if pl.TotalIncome, err = pl.TotalIncome.Add(amt); err != nil {
				return ProfitAndLoss{}, err
			}
		} else {
			pl.Expenses = append(pl.Expenses, Line{a.Code, a.Name, amt})
			if pl.TotalExpenses, err = pl.TotalExpenses.Add(amt); err != nil {
				return ProfitAndLoss{}, err
			}
		}
	}
	profit, err := pl.TotalIncome.Sub(pl.TotalExpenses)
	if err != nil {
		return ProfitAndLoss{}, err
	}
	pl.Profit = profit
	return pl, nil
}

// NewBalanceSheet builds the balance sheet as at asAt (all postings up to that date).
func NewBalanceSheet(book *ledger.Book, asAt ledger.Date) (BalanceSheet, error) {
	base := book.Base()
	bs := BalanceSheet{
		AsAt:             asAt,
		TotalAssets:      money.Zero(base),
		TotalLiabilities: money.Zero(base),
		TotalEquity:      money.Zero(base),
	}
	income, expense := money.Zero(base), money.Zero(base)
	for _, a := range book.Accounts() {
		bal, err := book.BalanceAsAt(a.Code, asAt)
		if err != nil {
			return BalanceSheet{}, err
		}
		switch a.Type {
		case ledger.Asset:
			if !bal.IsZero() {
				bs.Assets = append(bs.Assets, Line{a.Code, a.Name, bal})
				if bs.TotalAssets, err = bs.TotalAssets.Add(bal); err != nil {
					return BalanceSheet{}, err
				}
			}
		case ledger.Liability:
			if !bal.IsZero() {
				bs.Liabilities = append(bs.Liabilities, Line{a.Code, a.Name, bal})
				if bs.TotalLiabilities, err = bs.TotalLiabilities.Add(bal); err != nil {
					return BalanceSheet{}, err
				}
			}
		case ledger.Equity:
			if !bal.IsZero() {
				bs.Equity = append(bs.Equity, Line{a.Code, a.Name, bal})
				if bs.TotalEquity, err = bs.TotalEquity.Add(bal); err != nil {
					return BalanceSheet{}, err
				}
			}
		case ledger.Income:
			if income, err = income.Add(bal); err != nil {
				return BalanceSheet{}, err
			}
		case ledger.Expense:
			if expense, err = expense.Add(bal); err != nil {
				return BalanceSheet{}, err
			}
		}
	}
	profit, err := income.Sub(expense)
	if err != nil {
		return BalanceSheet{}, err
	}
	bs.ProfitForPeriod = profit
	bs.Equity = append(bs.Equity, Line{"", "Profit for the period", profit})
	if bs.TotalEquity, err = bs.TotalEquity.Add(profit); err != nil {
		return BalanceSheet{}, err
	}
	return bs, nil
}

// Balances reports whether assets equal liabilities plus equity (they always do).
func (bs BalanceSheet) Balances() bool {
	rhs, err := bs.TotalLiabilities.Add(bs.TotalEquity)
	if err != nil {
		return false
	}
	return bs.TotalAssets.Equal(rhs)
}

func bare(m money.Money) string {
	return strings.TrimPrefix(m.String(), m.Currency().Code+" ")
}

func writeLine(b *strings.Builder, code, name string, amt money.Money) {
	fmt.Fprintf(b, "  %-6s %-30s %12s\n", code, name, bare(amt))
}

func writeTotal(b *strings.Builder, label string, amt money.Money) {
	fmt.Fprintf(b, "  %-37s %12s\n", label, bare(amt))
}

// String renders the P&L as a plain-text statement.
func (pl ProfitAndLoss) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Profit & Loss for %s to %s\n\n", pl.From, pl.To)
	b.WriteString("Income\n")
	for _, l := range pl.Income {
		writeLine(&b, l.Code, l.Name, l.Amount)
	}
	writeTotal(&b, "Total income", pl.TotalIncome)
	b.WriteString("\nExpenses\n")
	for _, l := range pl.Expenses {
		writeLine(&b, l.Code, l.Name, l.Amount)
	}
	writeTotal(&b, "Total expenses", pl.TotalExpenses)
	b.WriteString("\n")
	writeTotal(&b, "Profit/(loss) for the period", pl.Profit)
	return b.String()
}

// String renders the balance sheet as a plain-text statement.
func (bs BalanceSheet) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Balance Sheet as at %s\n\n", bs.AsAt)
	b.WriteString("Assets\n")
	for _, l := range bs.Assets {
		writeLine(&b, l.Code, l.Name, l.Amount)
	}
	writeTotal(&b, "Total assets", bs.TotalAssets)
	b.WriteString("\nLiabilities\n")
	for _, l := range bs.Liabilities {
		writeLine(&b, l.Code, l.Name, l.Amount)
	}
	writeTotal(&b, "Total liabilities", bs.TotalLiabilities)
	b.WriteString("\nEquity\n")
	for _, l := range bs.Equity {
		writeLine(&b, l.Code, l.Name, l.Amount)
	}
	writeTotal(&b, "Total equity", bs.TotalEquity)
	return b.String()
}
