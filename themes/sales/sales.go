// Package sales provides the Sales theme's operations — invoices, receipts, cash
// sales, and credit notes — each of which builds one balanced journal against the
// starter chart. Income is always a credit; the matching debit is wherever the
// value landed (a debtor if the customer pays later, the bank if now).
package sales

import (
	"math/big"
	"strings"

	"github.com/richardjennings/accounts/chart"
	"github.com/richardjennings/accounts/ledger"
	"github.com/richardjennings/accounts/money"
	"github.com/richardjennings/decimal"
)

func acct(override, def string) string {
	if override == "" {
		return def
	}
	return override
}

func orZero(m money.Money, cur money.Currency) money.Money {
	if m.Currency().Code == "" {
		return money.Zero(cur)
	}
	return m
}

// InvoiceLine is one line of an itemised invoice: a description of what is being
// charged, a quantity at a net (VAT-exclusive) unit price, and the VAT rate that
// applies to that line. A line may be flagged as a Recharge — recovering a cost the
// company met on the customer's behalf (travel, materials, a supplier bill) — which
// posts to recharged-expenses income rather than sales. Recharging a cost is a
// standard-rated supply in its own right, so VAT is charged on it regardless of
// whether the original cost bore VAT.
type InvoiceLine struct {
	Description string
	Quantity    decimal.Decimal // units; the zero value means 1
	UnitPrice   money.Money     // net (VAT-exclusive) price per unit
	VATRate     decimal.Decimal // e.g. 0.20; the zero value means no VAT
	Recharge    bool            // true: this line recovers a cost from the customer
}

// qty is the line quantity, treating an unset (non-finite) or zero value as 1.
func (l InvoiceLine) qty() decimal.Decimal {
	if !l.Quantity.IsFinite() || l.Quantity.Rat().Sign() == 0 {
		return decimal.MustParse("1")
	}
	return l.Quantity
}

// Qty is the effective line quantity for display (an unset value shows as 1).
func (l InvoiceLine) Qty() decimal.Decimal { return l.qty() }

// VATPercent renders the line's VAT rate as a percentage, e.g. "20%" or "No VAT".
func (l InvoiceLine) VATPercent() string {
	pct := new(big.Rat).Mul(l.VATRate.Rat(), big.NewRat(100, 1))
	if pct.Sign() == 0 {
		return "No VAT"
	}
	s := pct.FloatString(2)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	return s + "%"
}

// Net is the line's VAT-exclusive total: quantity × unit price, to the penny.
func (l InvoiceLine) Net() money.Money {
	n, _ := l.UnitPrice.Mul(l.qty(), money.HalfUp)
	return n
}

// VAT is the VAT charged on the line, rounded to the penny half-up (HMRC permits
// rounding VAT per line on an itemised invoice).
func (l InvoiceLine) VAT() money.Money {
	v, _ := l.Net().Mul(l.VATRate, money.HalfUp)
	return v
}

// Gross is Net + VAT.
func (l InvoiceLine) Gross() money.Money {
	g, _ := l.Net().Add(l.VAT())
	return g
}

// Invoice records a credit sale — the customer will pay later. It debits trade
// debtors (the gross owed), and credits income plus, if any, the VAT control
// account (output VAT). It is either itemised (one or more Lines, VAT charged per
// line) or a single net Amount with its own VAT — the Lines take precedence when
// present.
type Invoice struct {
	Date           ledger.Date
	Ref            string        // e.g. "INV-001"
	Customer       string        // label for the narrative
	Lines          []InvoiceLine // itemised lines; when set, Amount/VAT are ignored
	Amount         money.Money   // net amount (VAT-exclusive) — used when Lines is empty
	VAT            money.Money   // output VAT; zero/unset for no VAT — used when Lines is empty
	Income         string        // defaults to chart.Sales
	RechargeIncome string        // recharged-expense income; defaults to chart.RechargedExpenses
	Debtors        string        // defaults to chart.TradeDebtors
	VATAccount     string        // defaults to chart.VAT
}

// currency reports the invoice's currency, taken from Amount, or from the first
// line when the invoice is itemised.
func (inv Invoice) currency() money.Currency {
	if inv.Amount.Currency().Code != "" {
		return inv.Amount.Currency()
	}
	if len(inv.Lines) > 0 {
		return inv.Lines[0].UnitPrice.Currency()
	}
	return inv.Amount.Currency()
}

// split returns the invoice's net split between ordinary sales and recharged costs,
// plus the total VAT. For a non-itemised invoice everything is a sale.
func (inv Invoice) split() (salesNet, rechargeNet, vat money.Money, err error) {
	cur := inv.currency()
	salesNet, rechargeNet, vat = money.Zero(cur), money.Zero(cur), money.Zero(cur)
	if len(inv.Lines) == 0 {
		salesNet = inv.Amount
		vat = orZero(inv.VAT, cur)
		return salesNet, rechargeNet, vat, nil
	}
	for _, l := range inv.Lines {
		if l.Recharge {
			rechargeNet, err = rechargeNet.Add(l.Net())
		} else {
			salesNet, err = salesNet.Add(l.Net())
		}
		if err != nil {
			return
		}
		if vat, err = vat.Add(l.VAT()); err != nil {
			return
		}
	}
	return salesNet, rechargeNet, vat, nil
}

// Totals returns the invoice's net, VAT and gross.
func (inv Invoice) Totals() (net, vat, gross money.Money, err error) {
	salesNet, rechargeNet, vat, err := inv.split()
	if err != nil {
		return
	}
	if net, err = salesNet.Add(rechargeNet); err != nil {
		return
	}
	gross, err = net.Add(vat)
	return net, vat, gross, err
}

func (inv Invoice) Journal() (ledger.Journal, error) {
	salesNet, rechargeNet, vat, err := inv.split()
	if err != nil {
		return ledger.Journal{}, err
	}
	net, err := salesNet.Add(rechargeNet)
	if err != nil {
		return ledger.Journal{}, err
	}
	gross, err := net.Add(vat)
	if err != nil {
		return ledger.Journal{}, err
	}
	narr := "Sales invoice " + inv.Ref
	if inv.Customer != "" {
		narr += " — " + inv.Customer
	}
	postings := []ledger.Posting{
		{Account: acct(inv.Debtors, chart.TradeDebtors), Side: ledger.Debit, Amount: gross},
	}
	if salesNet.IsPositive() {
		postings = append(postings, ledger.Posting{Account: acct(inv.Income, chart.Sales), Side: ledger.Credit, Amount: salesNet})
	}
	if rechargeNet.IsPositive() {
		postings = append(postings, ledger.Posting{Account: acct(inv.RechargeIncome, chart.RechargedExpenses), Side: ledger.Credit, Amount: rechargeNet})
	}
	if vat.IsPositive() {
		postings = append(postings, ledger.Posting{Account: acct(inv.VATAccount, chart.VAT), Side: ledger.Credit, Amount: vat})
	}
	j, err := ledger.NewJournal(inv.Date, narr, postings...)
	if err != nil {
		return ledger.Journal{}, err
	}
	return j.WithRef(inv.Ref), nil
}

// Receipt records a customer paying an invoice: debit the bank, credit trade
// debtors (the debt is cleared).
type Receipt struct {
	Date    ledger.Date
	Ref     string
	Amount  money.Money
	Bank    string // defaults to chart.Bank
	Debtors string // defaults to chart.TradeDebtors
}

func (r Receipt) Journal() (ledger.Journal, error) {
	j, err := ledger.NewJournal(r.Date, "Receipt "+r.Ref,
		ledger.Posting{Account: acct(r.Bank, chart.Bank), Side: ledger.Debit, Amount: r.Amount},
		ledger.Posting{Account: acct(r.Debtors, chart.TradeDebtors), Side: ledger.Credit, Amount: r.Amount},
	)
	if err != nil {
		return ledger.Journal{}, err
	}
	return j.WithRef(r.Ref), nil
}

// CashSale records an immediate sale paid on the spot: debit the bank (gross),
// credit sales (net) and the VAT control account (output VAT, if any).
type CashSale struct {
	Date       ledger.Date
	Ref        string
	Amount     money.Money // net
	VAT        money.Money // output VAT; zero/unset for no VAT
	Bank       string      // defaults to chart.Bank
	Income     string      // defaults to chart.Sales
	VATAccount string      // defaults to chart.VAT
}

func (c CashSale) Journal() (ledger.Journal, error) {
	cur := c.Amount.Currency()
	vat := orZero(c.VAT, cur)
	gross, err := c.Amount.Add(vat)
	if err != nil {
		return ledger.Journal{}, err
	}
	postings := []ledger.Posting{
		{Account: acct(c.Bank, chart.Bank), Side: ledger.Debit, Amount: gross},
		{Account: acct(c.Income, chart.Sales), Side: ledger.Credit, Amount: c.Amount},
	}
	if vat.IsPositive() {
		postings = append(postings, ledger.Posting{Account: acct(c.VATAccount, chart.VAT), Side: ledger.Credit, Amount: vat})
	}
	j, err := ledger.NewJournal(c.Date, "Cash sale "+c.Ref, postings...)
	if err != nil {
		return ledger.Journal{}, err
	}
	return j.WithRef(c.Ref), nil
}

// CreditNote reverses or reduces a sale (e.g. a refund): debit sales income, credit
// what it is set against — trade debtors, or the bank if refunded in cash.
type CreditNote struct {
	Date    ledger.Date
	Ref     string
	Amount  money.Money
	Income  string // defaults to chart.Sales
	Against string // debtors or bank; defaults to chart.TradeDebtors
}

func (c CreditNote) Journal() (ledger.Journal, error) {
	j, err := ledger.NewJournal(c.Date, "Credit note "+c.Ref,
		ledger.Posting{Account: acct(c.Income, chart.Sales), Side: ledger.Debit, Amount: c.Amount},
		ledger.Posting{Account: acct(c.Against, chart.TradeDebtors), Side: ledger.Credit, Amount: c.Amount},
	)
	if err != nil {
		return ledger.Journal{}, err
	}
	return j.WithRef(c.Ref), nil
}
