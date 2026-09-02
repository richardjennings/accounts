package frs105

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/richardjennings/accounts/ledger"
	"github.com/richardjennings/accounts/money"
	"github.com/richardjennings/ixbrl"
)

// FRC taxonomy for these accounts. The concept names are representative FRC concepts
// for a micro-entity; see the package doc on conformance scope.
const (
	schemaRef = "https://xbrl.frc.org.uk/FRS-105/2023-01-01/FRS-105-2023-01-01.xsd"
	nsCore    = "http://xbrl.frc.org.uk/fr/2023-01-01/core"
	nsBus     = "http://xbrl.frc.org.uk/cd/2023-01-01/business"

	chScheme = "http://www.companieshouse.gov.uk/"
	gbpUnit  = "GBP"
	pureUnit = "pure" // a count, such as the number of employees

	ctxInstant = "period-end"
	ctxPeriod  = "period"
)

const accountsCSS = "body{font-family:Georgia,'Times New Roman',serif;color:#111;max-width:720px;margin:24px auto;padding:0 24px;line-height:1.5}" +
	"h1{font-size:20px}h2{font-size:15px;border-bottom:1px solid #000;padding-bottom:3px;margin-top:28px}" +
	"table{border-collapse:collapse;width:100%;font-size:14px;margin:8px 0}" +
	"td{padding:3px 6px}td.num{text-align:right;font-variant-numeric:tabular-nums;white-space:nowrap}" +
	"tr.subtotal td{border-top:1px solid #000;font-weight:bold}tr.total td{border-top:1px solid #000;border-bottom:3px double #000;font-weight:bold}" +
	".st{font-size:12px;margin:6px 0}.muted{color:#555;font-size:12px}"

func iso(d ledger.Date) string { return fmt.Sprintf("%04d-%02d-%02d", d.Year, int(d.Month), d.Day) }

// amount converts a money value to an ixbrl.Amount (magnitude + separate sign).
func amount(m money.Money) ixbrl.Amount {
	s := m.Amount().String()
	if strings.HasPrefix(s, "-") {
		return ixbrl.Amount{Magnitude: s[1:], Negative: true}
	}
	return ixbrl.Amount{Magnitude: s}
}

// fig is a tagged figure shown £-prefixed (negatives in brackets).
func fig(concept, ctx string, m money.Money) ixbrl.Inline { return figure(concept, ctx, m, false) }

// deduct is a tagged figure always shown as a bracketed deduction (creditors, costs).
func deduct(concept, ctx string, m money.Money) ixbrl.Inline { return figure(concept, ctx, m, true) }

func figure(concept, ctx string, m money.Money, bracket bool) ixbrl.Inline {
	fact := ixbrl.Numeric(concept, ctx, gbpUnit, amount(m), 2)
	if bracket || m.IsNegative() {
		return ixbrl.Seq(ixbrl.Text("(£"), fact, ixbrl.Text(")"))
	}
	return ixbrl.Seq(ixbrl.Text("£"), fact)
}

func row(label string, amount ixbrl.Inline) ixbrl.Row {
	return ixbrl.Row{Label: label, Amount: amount}
}
func subtotal(label string, amount ixbrl.Inline) ixbrl.Row {
	return ixbrl.Row{Label: label, Amount: amount, Emphasis: "subtotal"}
}
func grand(label string, amount ixbrl.Inline) ixbrl.Row {
	return ixbrl.Row{Label: label, Amount: amount, Emphasis: "total"}
}

// document builds the statutory accounts as an inline-XBRL document.
func (a Accounts) document() ixbrl.Document {
	inst := ixbrl.Context{ID: ctxInstant, EntityScheme: chScheme, EntityID: a.Co.Number, Instant: iso(a.FY.End)}
	per := ixbrl.Context{ID: ctxPeriod, EntityScheme: chScheme, EntityID: a.Co.Number, Start: iso(a.FY.Start), End: iso(a.FY.End)}

	body := []ixbrl.Block{
		ixbrl.Heading(1, ixbrl.NonNumeric("uk-bus:EntityCurrentLegalOrRegisteredName", ctxInstant, a.Co.Name)),
		ixbrl.Paragraph("muted", ixbrl.Text("Company registration number "), ixbrl.NonNumeric("uk-bus:UKCompaniesHouseRegisteredNumber", ctxInstant, a.Co.Number)),
		ixbrl.Paragraph("", ixbrl.Text("Micro-entity accounts for the year ended "+iso(a.FY.End))),

		ixbrl.Heading(2, ixbrl.Text("Balance sheet as at "+iso(a.FY.End))),
		ixbrl.Rows(
			row("Fixed assets", fig("uk-core:FixedAssets", ctxInstant, a.FixedAssets)),
			row("Current assets", fig("uk-core:CurrentAssets", ctxInstant, a.CurrentAssets)),
			row("Creditors: amounts falling due within one year", deduct("uk-core:Creditors", ctxInstant, a.CreditorsWithin1Yr)),
			subtotal("Net current assets", fig("uk-core:NetCurrentAssetsLiabilities", ctxInstant, a.NetCurrentAssets)),
			subtotal("Total assets less current liabilities", fig("uk-core:TotalAssetsLessCurrentLiabilities", ctxInstant, a.TotalAssetsLessCL)),
			grand("Net assets", fig("uk-core:NetAssetsLiabilities", ctxInstant, a.NetAssets)),
		),

		ixbrl.Heading(2, ixbrl.Text("Capital and reserves")),
		ixbrl.Rows(
			row("Called-up share capital", fig("uk-core:CalledUpShareCapital", ctxInstant, a.CalledUpCapital)),
			row("Profit and loss account", fig("uk-core:RetainedEarningsAccumulatedLosses", ctxInstant, a.ProfitLossReserve)),
			grand("Shareholders' funds", fig("uk-core:Equity", ctxInstant, a.CapitalAndReserves)),
		),
	}
	for _, s := range a.statements() {
		body = append(body, ixbrl.Paragraph("st", ixbrl.Text(s)))
	}
	body = append(body, ixbrl.Paragraph("st",
		ixbrl.Text("The average number of persons employed by the company during the year, including directors, was "),
		ixbrl.Numeric("uk-core:AverageNumberEmployeesDuringPeriod", ctxPeriod, pureUnit, ixbrl.Amount{Magnitude: strconv.Itoa(a.AverageEmployees)}, 0),
		ixbrl.Text(".")))
	if a.Approved() {
		body = append(body,
			ixbrl.Paragraph("st", ixbrl.Text("Approved by the board on "),
				ixbrl.NonNumeric("uk-core:DateAuthorisationFinancialStatementsForIssue", ctxInstant, iso(a.ApprovedOn)),
				ixbrl.Text(" and signed on its behalf by:")),
			ixbrl.Paragraph("st", ixbrl.NonNumeric("uk-bus:NameEntityOfficer", ctxInstant, a.SignedBy), ixbrl.Text(" — Director")))
	} else {
		body = append(body, ixbrl.Paragraph("st", ixbrl.Text("DRAFT — the board has not yet approved these accounts.")))
	}
	body = append(body,

		ixbrl.Heading(2, ixbrl.Text("Profit and loss account for the year ended "+iso(a.FY.End))),
		ixbrl.Rows(
			row("Turnover", fig("uk-core:TurnoverRevenue", ctxPeriod, a.Turnover)),
			row("Cost of sales", deduct("uk-core:CostSales", ctxPeriod, a.CostOfSales)),
			subtotal("Gross profit", fig("uk-core:GrossProfitLoss", ctxPeriod, a.GrossProfit)),
			row("Administrative expenses", deduct("uk-core:AdministrativeExpenses", ctxPeriod, a.AdminExpenses)),
			subtotal("Profit before taxation", fig("uk-core:ProfitLossOnOrdinaryActivitiesBeforeTax", ctxPeriod, a.ProfitBeforeTax)),
			row("Tax on profit", deduct("uk-core:TaxTaxCreditOnProfitOrLossOnOrdinaryActivities", ctxPeriod, a.Tax)),
			grand("Profit for the financial year", fig("uk-core:ProfitLoss", ctxPeriod, a.ProfitForYear)),
		),
		ixbrl.Paragraph("muted", ixbrl.Text("Generated by a virtual accounting game. Inline XBRL is tagged with representative FRC-taxonomy concepts for learning; the document is not filed or transmitted.")),
	)

	title := a.Co.Name + " — accounts " + iso(a.FY.End)
	if !a.Approved() {
		title += " (draft)"
	}
	return ixbrl.Document{
		Title:      title,
		SchemaRef:  schemaRef,
		Style:      accountsCSS,
		Namespaces: map[string]string{"uk-core": nsCore, "uk-bus": nsBus},
		Contexts:   []ixbrl.Context{inst, per},
		Units:      []ixbrl.Unit{{ID: gbpUnit, Measure: "iso4217:GBP"}, {ID: pureUnit, Measure: "xbrli:pure"}},
		Body:       body,
	}
}

// IXBRL renders the statutory accounts as an inline-XBRL XHTML document.
func (a Accounts) IXBRL() (string, error) { return a.document().Render() }
