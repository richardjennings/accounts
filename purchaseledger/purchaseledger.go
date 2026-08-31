// Package purchaseledger is the purchase (payables) subsidiary ledger — the
// supplier-side mirror of salesledger. It tracks each supplier bill and the
// payments allocated against it, so an outstanding balance is known per bill. The
// general ledger holds only the trade-creditors control total; this records the
// detail behind it, and a payment is made against a specific bill.
package purchaseledger

import (
	"fmt"

	"github.com/richardjennings/accounts/ledger"
	"github.com/richardjennings/accounts/money"
)

// Bill is a supplier bill and how much of it has been paid.
type Bill struct {
	Ref      string
	Supplier string
	Date     ledger.Date
	Total    money.Money
	paid     money.Money
}

func (b *Bill) Paid() money.Money { return b.paid }

func (b *Bill) Outstanding() money.Money {
	o, _ := b.Total.Sub(b.paid)
	return o
}

func (b *Bill) Settled() bool { return !b.Outstanding().IsPositive() }

func (b *Bill) Status() string {
	switch {
	case b.paid.IsZero():
		return "Open"
	case b.Settled():
		return "Paid"
	default:
		return "Part-paid"
	}
}

// Ledger is the collection of bills.
type Ledger struct {
	order []*Bill
	byRef map[string]*Bill
}

func New() *Ledger { return &Ledger{byRef: map[string]*Bill{}} }

// Record records a new supplier bill.
func (l *Ledger) Record(ref, supplier string, date ledger.Date, total money.Money) (*Bill, error) {
	if _, ok := l.byRef[ref]; ok {
		return nil, fmt.Errorf("purchaseledger: bill %s already exists", ref)
	}
	if !total.IsPositive() {
		return nil, fmt.Errorf("purchaseledger: bill total must be positive")
	}
	b := &Bill{Ref: ref, Supplier: supplier, Date: date, Total: total, paid: money.Zero(total.Currency())}
	l.order = append(l.order, b)
	l.byRef[ref] = b
	return b, nil
}

// Get returns the bill with the given reference.
func (l *Ledger) Get(ref string) (*Bill, bool) { b, ok := l.byRef[ref]; return b, ok }

// Bills returns every bill, oldest first.
func (l *Ledger) Bills() []*Bill { return append([]*Bill(nil), l.order...) }

// Outstanding returns the bills not yet fully paid.
func (l *Ledger) Outstanding() []*Bill {
	var out []*Bill
	for _, b := range l.order {
		if !b.Settled() {
			out = append(out, b)
		}
	}
	return out
}

// Allocate records a payment of amount against a bill, capped at what is
// outstanding (you cannot pay more than is owed).
func (l *Ledger) Allocate(ref string, amount money.Money) error {
	b, ok := l.byRef[ref]
	if !ok {
		return fmt.Errorf("purchaseledger: no bill %s", ref)
	}
	if !amount.IsPositive() {
		return fmt.Errorf("purchaseledger: payment must be positive")
	}
	if cmp, _ := amount.Cmp(b.Outstanding()); cmp > 0 {
		return fmt.Errorf("payment of %s exceeds the %s outstanding on %s", amount, b.Outstanding(), ref)
	}
	np, err := b.paid.Add(amount)
	if err != nil {
		return err
	}
	b.paid = np
	return nil
}
