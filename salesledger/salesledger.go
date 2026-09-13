// Package salesledger is the sales (receivables) subsidiary ledger. The general
// ledger holds only the trade-debtors control total; this records the detail
// behind it — each customer invoice and the receipts allocated against it — so an
// outstanding balance is known per invoice. A receipt is recorded against a
// specific invoice, not the control account.
package salesledger

import (
	"fmt"

	"github.com/richardjennings/accounts/ledger"
	"github.com/richardjennings/accounts/money"
)

// Invoice is a customer invoice and how much of it has been received.
type Invoice struct {
	Ref      string
	Customer string
	Date     ledger.Date
	Total    money.Money
	paid     money.Money
	credited money.Money
}

// Paid returns how much has been received against the invoice.
func (i *Invoice) Paid() money.Money { return i.paid }

// Outstanding returns how much is still owed.
func (i *Invoice) Credited() money.Money { return i.credited }

func (i *Invoice) Outstanding() money.Money {
	o, _ := i.Total.Sub(i.paid)
	o, _ = o.Sub(i.credited)
	return o
}

// Settled reports whether nothing is outstanding.
func (i *Invoice) Settled() bool { return !i.Outstanding().IsPositive() }

// Status is a display label: Open, Part-paid, or Paid.
func (i *Invoice) Status() string {
	switch {
	case i.credited.IsPositive() && i.Settled():
		return "Settled"
	case i.credited.IsPositive() && i.paid.IsZero():
		return "Part-credited"
	case i.paid.IsZero():
		return "Open"
	case i.Settled():
		return "Paid"
	default:
		return "Part-paid"
	}
}

// Ledger is the collection of invoices.
type Ledger struct {
	order []*Invoice
	byRef map[string]*Invoice
}

func New() *Ledger { return &Ledger{byRef: map[string]*Invoice{}} }

// Raise records a new invoice.
func (l *Ledger) Raise(ref, customer string, date ledger.Date, total money.Money) (*Invoice, error) {
	if _, ok := l.byRef[ref]; ok {
		return nil, fmt.Errorf("salesledger: invoice %s already exists", ref)
	}
	if !total.IsPositive() {
		return nil, fmt.Errorf("salesledger: invoice total must be positive")
	}
	inv := &Invoice{Ref: ref, Customer: customer, Date: date, Total: total, paid: money.Zero(total.Currency()), credited: money.Zero(total.Currency())}
	l.order = append(l.order, inv)
	l.byRef[ref] = inv
	return inv, nil
}

// Get returns the invoice with the given reference.
func (l *Ledger) Get(ref string) (*Invoice, bool) { i, ok := l.byRef[ref]; return i, ok }

// Invoices returns every invoice, oldest first.
func (l *Ledger) Invoices() []*Invoice { return append([]*Invoice(nil), l.order...) }

// Outstanding returns the invoices that are not yet fully paid.
func (l *Ledger) Outstanding() []*Invoice {
	var out []*Invoice
	for _, i := range l.order {
		if !i.Settled() {
			out = append(out, i)
		}
	}
	return out
}

// Allocate records a receipt of amount against an invoice, capped at what is
// outstanding (you cannot receive more than is owed).
func (l *Ledger) ValidateAllocation(ref string, amount money.Money) error {
	inv, ok := l.byRef[ref]
	if !ok {
		return fmt.Errorf("salesledger: no invoice %s", ref)
	}
	if !amount.IsPositive() {
		return fmt.Errorf("salesledger: receipt must be positive")
	}
	cmp, err := amount.Cmp(inv.Outstanding())
	if err != nil {
		return err
	}
	if cmp > 0 {
		return fmt.Errorf("receipt of %s exceeds the %s outstanding on %s", amount, inv.Outstanding(), ref)
	}
	return nil
}

// Allocate records a payment after checking currency and remaining balance.
func (l *Ledger) Allocate(ref string, amount money.Money) error {
	if err := l.ValidateAllocation(ref, amount); err != nil {
		return err
	}
	v := l.byRef[ref]
	v.paid, _ = v.paid.Add(amount)
	return nil
}

// Credit reduces the outstanding debt without recording a cash payment.
func (l *Ledger) Credit(ref string, amount money.Money) error {
	if err := l.ValidateAllocation(ref, amount); err != nil {
		return err
	}
	v := l.byRef[ref]
	v.credited, _ = v.credited.Add(amount)
	return nil
}
