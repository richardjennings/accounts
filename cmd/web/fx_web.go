package main

import (
	"fmt"
	"math/big"
	"net/http"
	"strings"

	"github.com/richardjennings/accounts/chart"
	"github.com/richardjennings/accounts/ledger"
	"github.com/richardjennings/accounts/money"
	"github.com/richardjennings/accounts/themes"
	"github.com/richardjennings/accounts/themes/banking"
)

// bankCurrency returns the currency a bank account is held in; the company
// currency when none is set.
func (a *app) bankCurrency(code string) money.Currency {
	for _, b := range a.banks {
		if b.Code == code && b.Currency != "" {
			if cur, ok := money.Lookup(b.Currency); ok {
				return cur
			}
		}
	}
	return a.co.Currency
}

func (a *app) isForeign(code string) bool { return a.bankCurrency(code).Code != a.co.Currency.Code }

// fxBal is a foreign account's balance in its own currency. The ledger holds
// only the carrying value in the company currency; this runs alongside it.
func (a *app) fxBal(code string) money.Money {
	if m, ok := a.fxBalances[code]; ok {
		return m
	}
	return money.Zero(a.bankCurrency(code))
}

func (a *app) addFX(code string, delta money.Money) {
	if a.fxBalances == nil {
		a.fxBalances = map[string]money.Money{}
	}
	sum, err := a.fxBal(code).Add(delta)
	if err == nil {
		a.fxBalances[code] = sum
	}
}

// fxRoutes wires the currency handlers.
func (a *app) fxRoutes(mux *http.ServeMux) {
	// Set (or clear) an account's currency, with its present currency balance.
	mux.HandleFunc("/banking/accounts/currency", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Redirect(w, r, "/banking", http.StatusSeeOther)
			return
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		code := strings.TrimSpace(r.FormValue("bank"))
		ccy := strings.ToUpper(strings.TrimSpace(r.FormValue("currency")))
		if ccy == a.co.Currency.Code {
			ccy = ""
		}
		var cur money.Currency
		if ccy != "" {
			var ok bool
			if cur, ok = money.Lookup(ccy); !ok {
				a.flash = "⚠ unknown currency code " + ccy
				http.Redirect(w, r, "/banking", http.StatusSeeOther)
				return
			}
		}
		for i := range a.banks {
			if a.banks[i].Code != code {
				continue
			}
			a.banks[i].Currency = ccy
			if ccy == "" {
				delete(a.fxBalances, code)
				a.flash = "✓ " + a.banks[i].Name + " is a " + a.co.Currency.Code + " account"
			} else {
				bal := money.Zero(cur)
				if s := strings.TrimSpace(r.FormValue("opening")); s != "" {
					m, err := money.Parse(cur, s)
					if err != nil {
						a.flash = "⚠ enter the balance as a plain " + ccy + " amount"
						http.Redirect(w, r, "/banking", http.StatusSeeOther)
						return
					}
					bal = m
				}
				if a.fxBalances == nil {
					a.fxBalances = map[string]money.Money{}
				}
				a.fxBalances[code] = bal
				a.flash = fmt.Sprintf("✓ %s is a %s account holding %s, carried in the books at %s", a.banks[i].Name, ccy, fmtMoney(bal), fmtMoney(a.bal(code)))
			}
		}
		http.Redirect(w, r, "/banking", http.StatusSeeOther)
	})
	// Sell foreign currency for the company currency.
	mux.HandleFunc("/banking/conversions/record", a.run("banking", "/banking/transfers", func(r *http.Request) (themes.Operation, string, error) {
		from := strings.TrimSpace(r.FormValue("from"))
		to := strings.TrimSpace(r.FormValue("to"))
		if to == "" {
			to = a.main()
		}
		if !a.isForeign(from) {
			return nil, "", fmt.Errorf("choose a foreign-currency account to convert from")
		}
		if a.isForeign(to) {
			return nil, "", fmt.Errorf("convert into a %s account", a.co.Currency.Code)
		}
		cur := a.bankCurrency(from)
		sold, err := money.Parse(cur, strings.TrimSpace(r.FormValue("ccy_amount")))
		if err != nil || !sold.IsPositive() {
			return nil, "", fmt.Errorf("enter the %s amount sold", cur.Code)
		}
		proceeds, err := money.Parse(a.co.Currency, strings.TrimSpace(r.FormValue("proceeds")))
		if err != nil || !proceeds.IsPositive() {
			return nil, "", fmt.Errorf("enter the %s received", a.co.Currency.Code)
		}
		bal := a.fxBal(from)
		if c, _ := bal.Cmp(sold); c < 0 {
			return nil, "", fmt.Errorf("the account holds %s — set its balance under Banking → Accounts if that is wrong", fmtMoney(bal))
		}
		carried := a.bal(from)
		if !bal.Equal(sold) { // a partial sale carries out its share of the balance
			share := new(big.Rat).Quo(sold.Amount().Rat(), bal.Amount().Rat())
			carried = money.FromRat(a.co.Currency, new(big.Rat).Mul(carried.Amount().Rat(), share), money.HalfUp)
		}
		a.addFX(from, sold.Neg())
		return banking.Conversion{Date: a.date(r), Ref: a.ref("FX"), Proceeds: proceeds, Carried: carried, From: from, To: to},
			fmt.Sprintf("Converted %s to %s", fmtMoney(sold), fmtMoney(proceeds)), nil
	}))
}

// foreignReceipt builds the journal for money received in an account's own
// currency against an invoice: the bank takes the receipt's company-currency
// value, the debtor gives up what is settled, and the difference is a realised
// exchange gain or loss.
func (a *app) foreignReceipt(r *http.Request, invoiceRef string, outstanding money.Money) (themes.Operation, string, error) {
	bank := a.bankCode(r)
	cur := a.bankCurrency(bank)
	ccyAmount, err := money.Parse(cur, strings.TrimSpace(r.FormValue("ccy_amount")))
	if err != nil || !ccyAmount.IsPositive() {
		return nil, "", fmt.Errorf("enter the amount received in %s", cur.Code)
	}
	value, err := a.amount(r) // the receipt's value in the company currency
	if err != nil {
		return nil, "", err
	}
	settled := value
	if r.FormValue("settle") != "" {
		settled = outstanding
	} else if c, _ := settled.Cmp(outstanding); c > 0 {
		settled = outstanding
	}
	diff, err := value.Sub(settled) // received more than the book value: a gain
	if err != nil {
		return nil, "", err
	}
	postings := []ledger.Posting{
		{Account: bank, Side: ledger.Debit, Amount: value},
		{Account: chart.TradeDebtors, Side: ledger.Credit, Amount: settled},
	}
	switch {
	case diff.IsPositive():
		postings = append(postings, ledger.Posting{Account: chart.ExchangeDiff, Side: ledger.Credit, Amount: diff})
	case diff.IsNegative():
		postings = append(postings, ledger.Posting{Account: chart.ExchangeDiff, Side: ledger.Debit, Amount: diff.Abs()})
	}
	ref := a.ref("REC")
	j, err := ledger.NewJournal(a.date(r), fmt.Sprintf("Receipt %s — %s settling %s of %s", ref, fmtMoney(ccyAmount), fmtMoney(settled), invoiceRef), postings...)
	if err != nil {
		return nil, "", err
	}
	if err := a.sl.Allocate(invoiceRef, settled); err != nil {
		return nil, "", err
	}
	a.addFX(bank, ccyAmount)
	return journalOp{j: j.WithRef(ref)}, fmt.Sprintf("Receipt of %s (%s) against %s", fmtMoney(ccyAmount), fmtMoney(value), invoiceRef), nil
}
