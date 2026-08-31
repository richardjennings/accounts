// Package importer loads a company's history from the tables another accounting
// package exports, in three layers that keep the source format out of the engine:
//
//   - Source: the tables themselves, as typed cells, from .xls workbooks or CSV.
//   - Profile: reads the tables of one package's export layout and produces a
//     Batch of plain records. Sub-packages hold one profile per package.
//   - Batch: the records — invoices, receipts, bills, transfers, salaries and so
//     on — in the engine's own terms, ready for the caller to post.
//
// A profile is the only part that knows a package's column names and vocabulary.
package importer

import (
	"fmt"
	"sort"

	"github.com/richardjennings/accounts/ledger"
	"github.com/richardjennings/accounts/money"
	"github.com/richardjennings/decimal"
)

// Issue notes a row the profile could not use as intended, so an import can
// report what it skipped or guessed rather than fail wholesale.
type Issue struct {
	Table string
	Row   int // 1-based data row (the header is row 0)
	Msg   string
}

func (i Issue) String() string {
	if i.Row > 0 {
		return fmt.Sprintf("%s row %d: %s", i.Table, i.Row, i.Msg)
	}
	return fmt.Sprintf("%s: %s", i.Table, i.Msg)
}

// Profile reads one package's export layout.
type Profile interface {
	Name() string
	Read(src Source, cur money.Currency) (*Batch, []Issue, error)
}

// Source offers tables by name.
type Source interface {
	Table(name string) (*Table, bool)
	Names() []string
}

// Tables is an in-memory Source.
type Tables map[string]*Table

func (t Tables) Table(name string) (*Table, bool) { tb, ok := t[name]; return tb, ok }

func (t Tables) Names() []string {
	names := make([]string, 0, len(t))
	for n := range t {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Batch is everything a profile read, as records the engine understands.
// Every amount is in the company's currency. A record's Memo carries anything
// worth keeping that has no field of its own, such as a foreign-currency figure.
type Batch struct {
	Customers    []Party
	Suppliers    []Party
	Banks        []string          // bank account names to make sure exist
	BankCurrency map[string]string // currency code by bank name, when a bank is not in the company currency
	VATCharged   bool              // any sales invoice charged VAT
	Invoices     []Invoice
	CreditNotes  []CreditNote
	Receipts     []Receipt
	Bills        []Bill
	Transfers    []Transfer
	Interest     []Interest
	Salaries     []Salary
	Dividends    []Dividend
	Drawings     []Drawing
	Introduced   []Introduced
	TaxPayments  []TaxPayment
	TaxRebates   []TaxRebate
}

// Party is a customer or supplier.
type Party struct {
	Name, Address, VATNumber string
}

// Invoice is a sales invoice: one line per row of the source, net of VAT.
type Invoice struct {
	Date     ledger.Date
	Ref      string
	Customer string
	Lines    []InvoiceLine
	Memo     string
}

// InvoiceLine is one net amount at one VAT rate.
type InvoiceLine struct {
	Description string
	Net         money.Money
	VATRate     decimal.Decimal // e.g. 0.20; zero for none
	VAT         money.Money     // the VAT the source recorded (authoritative over the rate)
	Recharge    bool
}

// CreditNote reduces a sales invoice.
type CreditNote struct {
	Date    ledger.Date
	Ref     string
	Invoice string // the invoice it credits, when known
	Gross   money.Money
}

// Receipt is money in from a customer. Amount is the value in the company
// currency; CcyAmount is what was paid when the payment was in the currency of
// a foreign bank account (zero otherwise).
type Receipt struct {
	Date      ledger.Date
	Ref       string
	Customer  string
	Invoice   string // the invoice it settles, when matched
	Bank      string // bank name; "" means petty cash
	Amount    money.Money
	CcyAmount money.Money
	Memo      string
}

// Bill is a cost: a supplier bill with how and whether it was paid.
type Bill struct {
	Date        ledger.Date
	Ref         string
	Supplier    string
	Description string
	Category    string // the source's category, for the account mapping
	Net         money.Money
	VAT         money.Money
	Recharge    string // customer the cost is recharged to, or ""
	PaidBy      PaidBy
	PaidFrom    string      // bank name when PaidBy is Bank; "" means the main account
	Paid        money.Money // amount paid so far
	Credited    money.Money // supplier credit notes against it
}

// PaidBy is how a bill was settled.
type PaidBy uint8

const (
	Unpaid PaidBy = iota
	Bank
	PettyCash
	Director // paid personally: owed to the director
)

// Transfer moves money between two of the company's own accounts. Petty cash
// is the name "" on either side.
type Transfer struct {
	Date     ledger.Date
	Ref      string
	From, To string
	Amount   money.Money
}

// Interest is bank interest received into a named account.
type Interest struct {
	Date   ledger.Date
	Bank   string
	Amount money.Money
}

// Salary is one gross pay; deductions are zero unless the source gives them.
// Owed marks pay credited to the director's loan account, to be drawn later,
// rather than paid from a bank account.
type Salary struct {
	Date        ledger.Date
	Person      string
	Gross       money.Money
	TaxNIC      money.Money
	EmployerNIC money.Money
	Owed        bool
}

// Dividend is a dividend declared, credited to the director's loan account.
type Dividend struct {
	Date   ledger.Date
	Amount money.Money
}

// Drawing is money the director took out of a bank account.
type Drawing struct {
	Date   ledger.Date
	Person string
	Bank   string
	Amount money.Money
}

// Introduced is money the director paid into a bank account.
type Introduced struct {
	Date   ledger.Date
	Bank   string
	Amount money.Money
}

// TaxPayment is a payment to HMRC.
type TaxPayment struct {
	Date   ledger.Date
	Kind   TaxKind
	Bank   string
	Amount money.Money
}

// TaxRebate is a refund from HMRC. ToDirector is set when the director received
// it personally.
type TaxRebate struct {
	Date       ledger.Date
	Kind       TaxKind
	Bank       string
	ToDirector bool
	Amount     money.Money
}

// TaxKind is which tax a payment or rebate concerns.
type TaxKind uint8

const (
	CorporationTax TaxKind = iota
	PAYE
	VATTax
	OtherTax
)

func (k TaxKind) String() string {
	switch k {
	case CorporationTax:
		return "corporation tax"
	case PAYE:
		return "PAYE/NIC"
	case VATTax:
		return "VAT"
	}
	return "tax"
}
