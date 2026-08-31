// Package frs105 produces a UK micro-entity's statutory year-end accounts under
// FRS 105 and renders them as an inline-XBRL (iXBRL) document — the format in which
// accounts are filed at Companies House and attached to a CT600. The document is
// valid XHTML that reads as the accounts on screen while carrying machine-readable
// XBRL facts inline, exactly as a real filing does.
//
// It builds the statutory balance-sheet layout (Companies Act 2006 micro-entity
// format), a summarised profit & loss account, and the fixed statutory statements,
// from the ledger. In keeping with the product, it GENERATES the document perfectly
// but transmits nothing — there is no Companies House or HMRC submission.
//
// Honesty note: the figures and structure are correct, and the inline XBRL is
// well-formed and tagged with representative FRC-taxonomy concepts. It is not
// claimed to pass full FRC taxonomy validation — that would need the complete
// taxonomy and every mandatory tag, which is beyond an educational model.
package frs105

import (
	"fmt"
	"strings"

	"github.com/richardjennings/accounts/company"
	"github.com/richardjennings/accounts/ledger"
	"github.com/richardjennings/accounts/money"
	"github.com/richardjennings/accounts/report"
)

// Accounts is a micro-entity's statutory accounts for one financial year.
type Accounts struct {
	Co       company.Company
	FY       company.FinancialYear
	SignedBy string // director approving the accounts

	// Balance sheet (as at the year-end), in the statutory micro-entity order.
	FixedAssets        money.Money
	CurrentAssets      money.Money
	CreditorsWithin1Yr money.Money // amounts falling due within one year (positive)
	NetCurrentAssets   money.Money // current assets − creditors
	TotalAssetsLessCL  money.Money // fixed assets + net current assets
	NetAssets          money.Money
	CalledUpCapital    money.Money // called-up share capital
	ProfitLossReserve  money.Money // reserves including the profit & loss account
	CapitalAndReserves money.Money

	// Profit & loss account (for the year).
	Turnover        money.Money
	CostOfSales     money.Money
	GrossProfit     money.Money
	AdminExpenses   money.Money
	ProfitBeforeTax money.Money
	Tax             money.Money
	ProfitForYear   money.Money
}

// chart codes this mapping relies on.
const (
	shareCapital  = "3000"
	costOfSales   = "5000"
	corpTaxCharge = "8200"
)

// Build assembles the accounts from the book for the given financial year.
func Build(book *ledger.Book, co company.Company, fy company.FinancialYear, signedBy string) (Accounts, error) {
	base := book.Base()
	bs, err := report.NewBalanceSheet(book, fy.End)
	if err != nil {
		return Accounts{}, err
	}
	pl, err := report.NewProfitAndLoss(book, fy.Start, fy.End)
	if err != nil {
		return Accounts{}, err
	}
	a := Accounts{Co: co, FY: fy, SignedBy: signedBy}

	// Balance sheet: split assets into fixed (0xxx) and current (everything else),
	// treat all liabilities as due within one year (this chart has no long-term debt).
	fixed, current := money.Zero(base), money.Zero(base)
	for _, l := range bs.Assets {
		if strings.HasPrefix(l.Code, "0") {
			fixed, _ = fixed.Add(l.Amount)
		} else {
			current, _ = current.Add(l.Amount)
		}
	}
	a.FixedAssets, a.CurrentAssets = fixed, current
	a.CreditorsWithin1Yr = bs.TotalLiabilities
	a.NetCurrentAssets, _ = current.Sub(bs.TotalLiabilities)
	a.TotalAssetsLessCL, _ = fixed.Add(a.NetCurrentAssets)
	a.NetAssets = a.TotalAssetsLessCL // no creditors >1yr, provisions or accruals here

	// Capital and reserves: share capital, and everything else in equity (including
	// the profit for the period the balance sheet folds in) as the P&L reserve.
	a.CalledUpCapital, _ = book.BalanceAsAt(shareCapital, fy.End)
	a.CapitalAndReserves = bs.TotalEquity
	a.ProfitLossReserve, _ = bs.TotalEquity.Sub(a.CalledUpCapital)

	// Profit & loss account.
	a.Turnover = pl.TotalIncome
	a.CostOfSales, _ = book.MovementBetween(costOfSales, fy.Start, fy.End)
	a.GrossProfit, _ = a.Turnover.Sub(a.CostOfSales)
	a.Tax, _ = book.MovementBetween(corpTaxCharge, fy.Start, fy.End)
	// Admin expenses are every expense except cost of sales and the tax charge.
	admin := money.Zero(base)
	adminSubs, _ := a.CostOfSales.Add(a.Tax)
	if admin, err = pl.TotalExpenses.Sub(adminSubs); err != nil {
		return Accounts{}, err
	}
	a.AdminExpenses = admin
	a.ProfitBeforeTax, _ = a.GrossProfit.Sub(a.AdminExpenses)
	a.ProfitForYear, _ = a.ProfitBeforeTax.Sub(a.Tax)
	return a, nil
}

// Balances reports whether net assets equal capital and reserves.
func (a Accounts) Balances() bool { return a.NetAssets.Equal(a.CapitalAndReserves) }

// statements are the fixed FRS 105 / Companies Act declarations on the balance sheet.
func (a Accounts) statements() []string {
	return []string{
		fmt.Sprintf("For the year ending %s the company was entitled to exemption from audit under section 477 of the Companies Act 2006 relating to small companies.", a.FY.End),
		"The members have not required the company to obtain an audit of its accounts for the year in question in accordance with section 476 of the Companies Act 2006.",
		"The directors acknowledge their responsibilities for complying with the requirements of the Act with respect to accounting records and the preparation of accounts.",
		"These accounts have been prepared and delivered in accordance with the provisions applicable to companies subject to the micro-entity regime.",
	}
}
