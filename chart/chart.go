// Package chart provides starter charts of accounts. These are ordinary data —
// the ledger is chart-agnostic — kept out of the ledger package so it stays
// jurisdiction- and framework-neutral. Adjust freely per company.
package chart

import "github.com/richardjennings/accounts/ledger"

// Account codes for the UK micro-Ltd starter chart. Themes and callers reference
// these constants rather than raw strings, so the chart is the single source of
// truth for codes.
const (
	PlantEquipment          = "0100"
	AccumulatedDepreciation = "0110"
	TradeDebtors            = "1100"
	Prepayments             = "1150"
	Bank                    = "1200"
	Cash                    = "1210"
	TradeCreditors          = "2100"
	VAT                     = "2200"
	PAYENIC                 = "2210"
	PensionPayable          = "2220"
	DirectorsLoan           = "2300"
	CorpTaxPayable          = "2320"
	Accruals                = "2400"
	ShareCapital            = "3000"
	Dividends               = "3100"
	RetainedEarnings        = "3200"
	Sales                   = "4000"
	RechargedExpenses       = "4100"
	OtherIncome             = "4900"
	CostOfSales             = "5000"
	Salaries                = "7000"
	EmployerNIC             = "7010"
	PensionCosts            = "7020"
	Travel                  = "7400"
	Accountancy             = "7500"
	OfficeAdmin             = "7600"
	Depreciation            = "7900"
	CorpTaxCharge           = "8200"
)

// UKMicroLtd returns a conventional starter chart for a small UK owner-managed
// limited company. VAT accounts are intentionally omitted for now. The director's
// loan account is typed as a liability but legitimately swings to an asset when the
// director owes the company — the ledger handles either sign. Accumulated
// depreciation is a contra-asset: an asset account carrying a credit balance, so
// net book value = cost + accumulated depreciation falls out of the balance sheet.
func UKMicroLtd() []ledger.Account {
	return []ledger.Account{
		// Fixed assets (0xxx)
		{Code: PlantEquipment, Name: "Plant & equipment", Type: ledger.Asset},
		{Code: AccumulatedDepreciation, Name: "Accumulated depreciation", Type: ledger.Asset},

		// Current assets (1xxx)
		{Code: TradeDebtors, Name: "Trade debtors", Type: ledger.Asset},
		{Code: Prepayments, Name: "Prepayments", Type: ledger.Asset},
		{Code: Bank, Name: "Bank current account", Type: ledger.Asset},
		{Code: Cash, Name: "Cash", Type: ledger.Asset},

		// Liabilities (2xxx)
		{Code: TradeCreditors, Name: "Trade creditors", Type: ledger.Liability},
		{Code: VAT, Name: "VAT control", Type: ledger.Liability},
		{Code: PAYENIC, Name: "PAYE / NIC payable", Type: ledger.Liability},
		{Code: PensionPayable, Name: "Pension payable", Type: ledger.Liability},
		{Code: DirectorsLoan, Name: "Director's loan account", Type: ledger.Liability},
		{Code: CorpTaxPayable, Name: "Corporation tax payable", Type: ledger.Liability},
		{Code: Accruals, Name: "Accruals", Type: ledger.Liability},

		// Equity (3xxx)
		{Code: ShareCapital, Name: "Share capital", Type: ledger.Equity},
		{Code: Dividends, Name: "Dividends", Type: ledger.Equity}, // distribution: carries a debit balance
		{Code: RetainedEarnings, Name: "Retained earnings", Type: ledger.Equity},

		// Income (4xxx)
		{Code: Sales, Name: "Sales", Type: ledger.Income},
		{Code: RechargedExpenses, Name: "Recharged expenses", Type: ledger.Income},
		{Code: OtherIncome, Name: "Other income", Type: ledger.Income},

		// Expenses (5xxx-8xxx)
		{Code: CostOfSales, Name: "Cost of sales", Type: ledger.Expense},
		{Code: Salaries, Name: "Directors' salaries", Type: ledger.Expense},
		{Code: EmployerNIC, Name: "Employer's NIC", Type: ledger.Expense},
		{Code: PensionCosts, Name: "Employer pension", Type: ledger.Expense},
		{Code: Travel, Name: "Travel & subsistence", Type: ledger.Expense},
		{Code: Accountancy, Name: "Accountancy fees", Type: ledger.Expense},
		{Code: OfficeAdmin, Name: "Office and admin", Type: ledger.Expense},
		{Code: Depreciation, Name: "Depreciation", Type: ledger.Expense},
		{Code: CorpTaxCharge, Name: "Corporation tax charge", Type: ledger.Expense},
	}
}
