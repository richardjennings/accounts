// Package explain narrates ledger activity in plain language for a learner. For a
// theme operation it tells two things: the story of the business event (what you
// did and the accounting principle behind it), and — derived uniformly from the
// real accounts touched — what each debit and credit actually does. It is
// pedagogy, so it sits above the themes, which stay focused on posting.
package explain

import (
	"fmt"
	"strings"

	"github.com/richardjennings/accounts/ledger"
	"github.com/richardjennings/accounts/money"
	"github.com/richardjennings/accounts/themes"
	"github.com/richardjennings/accounts/themes/banking"
	"github.com/richardjennings/accounts/themes/companytax"
	"github.com/richardjennings/accounts/themes/expenses"
	"github.com/richardjennings/accounts/themes/payyourself"
	"github.com/richardjennings/accounts/themes/sales"
)

// PostingNote explains one leg of a journal: which account moved, on which side,
// by how much, and what that means in plain language.
type PostingNote struct {
	Account string
	Side    string // "debit" / "credit"
	Amount  string
	Effect  string // "this asset goes up"
}

// Explanation is a learner-facing account of one journal: what happened, the
// mechanics of each posting, and the principle behind it.
type Explanation struct {
	Headline  string
	Postings  []PostingNote
	Principle string
}

// Explain narrates a theme operation: the mechanics of its journal plus the
// operation's own story and accounting principle.
func Explain(book *ledger.Book, op themes.Operation) (Explanation, error) {
	j, err := op.Journal()
	if err != nil {
		return Explanation{}, err
	}
	e := ExplainJournal(book, j)
	if headline, principle, ok := story(op); ok {
		e.Headline, e.Principle = headline, principle
	}
	return e, nil
}

// ExplainJournal narrates any journal from the accounts it touches — the headline
// is the journal narrative, and each posting gets its plain-language mechanics.
// Used for manual journals (the Accounting theme) and as the base for Explain.
func ExplainJournal(book *ledger.Book, j ledger.Journal) Explanation {
	e := Explanation{Headline: j.Narrative()}
	for _, p := range j.Postings() {
		name, effect := p.Account, ""
		if acct, ok := book.Account(p.Account); ok {
			name = acct.Name
			effect = mechanics(acct.Type, p.Side)
		}
		e.Postings = append(e.Postings, PostingNote{
			Account: name,
			Side:    sideWord(p.Side),
			Amount:  formatAmount(p.Amount),
			Effect:  effect,
		})
	}
	return e
}

// String renders the explanation as plain text.
func (e Explanation) String() string {
	var b strings.Builder
	b.WriteString(e.Headline)
	b.WriteByte('\n')
	for _, p := range e.Postings {
		fmt.Fprintf(&b, "  • %-6s %s %s — %s\n", p.Side, p.Account, p.Amount, p.Effect)
	}
	if e.Principle != "" {
		fmt.Fprintf(&b, "Why: %s\n", e.Principle)
	}
	return b.String()
}

// mechanics states, in plain language, what a posting does to an account given its
// type and side — an account moves up on its normal side and down on the other.
func mechanics(t ledger.AccountType, s ledger.Side) string {
	up := s == t.NormalSide()
	switch t {
	case ledger.Asset:
		if up {
			return "this asset goes up"
		}
		return "this asset goes down"
	case ledger.Liability:
		if up {
			return "you now owe more here"
		}
		return "you owe less here"
	case ledger.Income:
		if up {
			return "you've earned income"
		}
		return "income is reduced"
	case ledger.Expense:
		if up {
			return "a cost is recorded"
		}
		return "a cost is reduced"
	case ledger.Equity:
		if up {
			return "the owner's stake goes up"
		}
		return "the owner's stake goes down"
	}
	return ""
}

func sideWord(s ledger.Side) string {
	if s == ledger.Credit {
		return "credit"
	}
	return "debit"
}

func formatAmount(m money.Money) string {
	s := m.String()
	if m.Currency().Code == "GBP" {
		return "£" + strings.TrimPrefix(s, "GBP ")
	}
	return s
}

func suffix(sep, name string) string {
	if name == "" {
		return ""
	}
	return sep + name
}

// story returns the domain headline and accounting principle for a known
// operation. ok is false for an unrecognised operation, leaving the journal
// narrative as the headline.
func story(op themes.Operation) (headline, principle string, ok bool) {
	switch o := op.(type) {
	case sales.Invoice:
		return fmt.Sprintf("You raised sales invoice %s%s for %s.", o.Ref, suffix(" to ", o.Customer), formatAmount(o.Amount)),
			"Income counts when you make the sale, not when you're paid. So the customer becomes a debtor (they owe you) and you record the income straight away.", true
	case sales.Receipt:
		return fmt.Sprintf("You were paid %s for invoice %s.", formatAmount(o.Amount), o.Ref),
			"Getting paid doesn't earn you anything new — you already booked the income on the invoice. This just turns a debt owed to you into cash.", true
	case sales.CashSale:
		return fmt.Sprintf("You made a cash sale %s for %s.", o.Ref, formatAmount(o.Amount)),
			"The money arrives at the moment you make the sale, so there's no debtor — the bank goes straight up and you record the income.", true
	case sales.CreditNote:
		return fmt.Sprintf("You issued credit note %s for %s.", o.Ref, formatAmount(o.Amount)),
			"A credit note is a sale in reverse: it lowers your income and cancels part of what the customer owed (or refunds them).", true
	case expenses.Bill:
		return fmt.Sprintf("You recorded bill %s%s for %s.", o.Ref, suffix(" from ", o.Supplier), formatAmount(o.Amount)),
			"The cost counts when you get the bill, not when you pay it. So you record the expense now and owe the supplier until you pay.", true
	case expenses.Payment:
		return fmt.Sprintf("You paid supplier bill %s, %s.", o.Ref, formatAmount(o.Amount)),
			"Paying a bill isn't a new cost — you already recorded it. This clears what you owed and reduces your bank.", true
	case expenses.DirectExpense:
		return fmt.Sprintf("You paid %s straight from the bank%s (%s).", formatAmount(o.Amount), suffix(" to ", o.Payee), o.Ref),
			"With no bill in between, the cost and the payment happen together: the expense goes up and the bank goes down.", true
	case banking.Transfer:
		return fmt.Sprintf("You moved %s between your own accounts (%s).", formatAmount(o.Amount), o.Ref),
			"Moving your own money around doesn't change what the business is worth — one account goes up by exactly what another goes down.", true
	case banking.InterestReceived:
		return fmt.Sprintf("You received %s of bank interest (%s).", formatAmount(o.Amount), o.Ref),
			"Interest is income the business earns, so the bank goes up and you record income — just not from selling anything.", true
	case banking.Charge:
		return fmt.Sprintf("The bank charged you %s (%s).", formatAmount(o.Amount), o.Ref),
			"A bank fee is a running cost of the business — it's an expense, and it comes straight out of the bank.", true
	case payyourself.Salary:
		tax := o.TaxNIC
		if tax.Currency().Code == "" {
			tax = money.Zero(o.Gross.Currency())
		}
		net, _ := o.Gross.Sub(tax)
		h := fmt.Sprintf("You paid yourself a %s salary (%s).", formatAmount(o.Gross), o.Ref)
		if tax.IsPositive() {
			h += fmt.Sprintf(" %s is PAYE/NIC owed to HMRC; %s reaches your bank.", formatAmount(tax), formatAmount(net))
		}
		return h, "Your salary is a company cost, so it lowers the company's profit. The tax and NIC withheld aren't yours to keep — the company owes them to HMRC — so only the net reaches your bank.", true
	case payyourself.PayPAYE:
		return fmt.Sprintf("You paid %s of PAYE/NIC to HMRC (%s).", formatAmount(o.Amount), o.Ref),
			"This isn't a new cost — the company already owed it when the salary was run. Paying it clears the debt to HMRC.", true
	case payyourself.DeclareDividend:
		return fmt.Sprintf("You declared a %s dividend (%s).", formatAmount(o.Amount), o.Ref),
			"A dividend is not a company expense — it's a share of profit paid to you as owner. It reduces the company's reserves, not its profit, and the company owes it to you until it's paid.", true
	case payyourself.PayDividend:
		return fmt.Sprintf("You paid the %s dividend (%s).", formatAmount(o.Amount), o.Ref),
			"Paying the dividend clears what the company owed you and reduces the bank. The profit was already reduced when you declared it.", true
	case payyourself.IntroduceFunds:
		return fmt.Sprintf("You put %s of your own money into the company (%s).", formatAmount(o.Amount), o.Ref),
			"This is a loan to the company, not income — the bank goes up, and the company now owes you the money back.", true
	case payyourself.DrawFunds:
		return fmt.Sprintf("You took %s out of the company (%s).", formatAmount(o.Amount), o.Ref),
			"Drawing money reduces the bank and changes the director's loan account — it isn't a company cost.", true
	case companytax.Provision:
		return fmt.Sprintf("You set aside %s for corporation tax (%s).", formatAmount(o.Amount), o.Ref),
			"Tax on this year's profit is a cost of this year, even though you pay it later — so you record the charge now and owe HMRC until you pay.", true
	case companytax.Payment:
		return fmt.Sprintf("You paid %s of corporation tax to HMRC (%s).", formatAmount(o.Amount), o.Ref),
			"Paying the tax isn't a new cost — you already provided for it. This clears the debt to HMRC and reduces the bank.", true
	}
	return "", "", false
}
