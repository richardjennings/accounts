// Package vatreturn computes a UK VAT return — the nine boxes — for a period,
// directly from the ledger. Output VAT (Box 1) and input VAT (Box 4) are read
// exactly from the credits and debits on the VAT control account, so the net VAT
// position is precise. Box 6 (sales) and Box 7 (purchases) are the net values of the
// income and purchase accounts.
//
// In keeping with the product it computes the return and presents it as a document;
// it does NOT submit anything to HMRC. It is only meaningful for a VAT-registered
// company.
package vatreturn

import (
	"github.com/richardjennings/accounts/ledger"
	"github.com/richardjennings/accounts/money"
)

// Return is a completed VAT return for a period.
type Return struct {
	From, To    ledger.Date
	Box1        money.Money // VAT due on sales and other outputs
	Box2        money.Money // VAT due on acquisitions (nil trade here)
	Box3        money.Money // total VAT due (Box1 + Box2)
	Box4        money.Money // VAT reclaimed on purchases and other inputs
	Box5        money.Money // net VAT to pay to HMRC (or reclaim) — always non-negative
	Box5Reclaim bool        // true when Box5 is a reclaim (inputs exceeded outputs)
	Box6        money.Money // total value of sales and other outputs, excluding VAT
	Box7        money.Money // total value of purchases and other inputs, excluding VAT
	Box8, Box9  money.Money // supplies of goods to/from the EU (nil here)
}

// Options names the accounts the computation reads.
type Options struct {
	VATControl      string          // the VAT control account (output VAT credits, input VAT debits)
	PurchaseExclude map[string]bool // expense codes that are NOT Box 7 purchases (wages, depreciation, tax)
	CapitalCodes    []string        // asset codes whose additions count as Box 7 inputs
}

func inPeriod(d, from, to ledger.Date) bool { return !d.Before(from) && !to.Before(d) }

// Compute builds the return for [from, to] inclusive.
func Compute(book *ledger.Book, from, to ledger.Date, opt Options) (Return, error) {
	base := book.Base()
	r := Return{
		From: from, To: to,
		Box1: money.Zero(base), Box2: money.Zero(base), Box3: money.Zero(base),
		Box4: money.Zero(base), Box5: money.Zero(base),
		Box6: money.Zero(base), Box7: money.Zero(base),
		Box8: money.Zero(base), Box9: money.Zero(base),
	}

	// Box 1 / Box 4: output VAT is credits to the control account, input VAT is debits.
	for _, j := range book.Journals() {
		if !inPeriod(j.Date(), from, to) {
			continue
		}
		for _, p := range j.Postings() {
			if p.Account != opt.VATControl {
				continue
			}
			var err error
			if p.Side == ledger.Credit {
				if r.Box1, err = r.Box1.Add(p.Amount); err != nil {
					return Return{}, err
				}
			} else {
				if r.Box4, err = r.Box4.Add(p.Amount); err != nil {
					return Return{}, err
				}
			}
		}
	}

	// Box 6 / Box 7: net values of income and purchase accounts over the period.
	for _, ac := range book.Accounts() {
		mv, err := book.MovementBetween(ac.Code, from, to)
		if err != nil {
			return Return{}, err
		}
		switch {
		case ac.Type == ledger.Income:
			if r.Box6, err = r.Box6.Add(mv); err != nil {
				return Return{}, err
			}
		case ac.Type == ledger.Expense && !opt.PurchaseExclude[ac.Code]:
			if r.Box7, err = r.Box7.Add(mv); err != nil {
				return Return{}, err
			}
		}
	}
	for _, code := range opt.CapitalCodes {
		mv, err := book.MovementBetween(code, from, to)
		if err != nil {
			return Return{}, err
		}
		if r.Box7, err = r.Box7.Add(mv); err != nil {
			return Return{}, err
		}
	}

	// Box 3 = Box 1 + Box 2; Box 5 = |Box 3 − Box 4|.
	box3, err := r.Box1.Add(r.Box2)
	if err != nil {
		return Return{}, err
	}
	r.Box3 = box3
	net, err := r.Box3.Sub(r.Box4)
	if err != nil {
		return Return{}, err
	}
	if net.IsNegative() {
		r.Box5, r.Box5Reclaim = net.Neg(), true
	} else {
		r.Box5 = net
	}
	return r, nil
}
