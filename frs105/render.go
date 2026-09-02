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

	ctxInstant      = "period-end"
	ctxPeriod       = "period"
	ctxPriorInstant = "prior-period-end"
	ctxPriorPeriod  = "prior-period"
)

const accountsCSS = "body{font-family:Georgia,'Times New Roman',serif;color:#111;max-width:720px;margin:24px auto;padding:0 24px;line-height:1.5}" +
	"h1{font-size:20px}h2{font-size:15px;border-bottom:1px solid #000;padding-bottom:3px;margin-top:28px}" +
	"table{border-collapse:collapse;width:100%;font-size:14px;margin:8px 0}" +
	"td{padding:3px 6px}td.num{text-align:right;font-variant-numeric:tabular-nums;white-space:nowrap}" +
	"th{font-weight:normal;font-size:12px;color:#555;padding:3px 6px}th.num{text-align:right}" +
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

// figure is a tagged figure shown £-prefixed. A deduction (creditors, costs) and a
// negative value are shown in brackets.
func figure(concept, ctx string, m money.Money, deduct bool) ixbrl.Inline {
	fact := ixbrl.Numeric(concept, ctx, gbpUnit, amount(m), 2)
	if deduct || m.IsNegative() {
		return ixbrl.Seq(ixbrl.Text("(£"), fact, ixbrl.Text(")"))
	}
	return ixbrl.Seq(ixbrl.Text("£"), fact)
}

// getter picks one figure out of a year's Figures.
type getter func(Figures) money.Money

// line builds one row of a statement: the year's figure tagged in the current
// context and, when there is a year before, its figure in the prior context.
func (a Accounts) line(label, emphasis, concept string, instant bool, get getter, deduct bool) ixbrl.Row {
	ctx, priorCtx := ctxPeriod, ctxPriorPeriod
	if instant {
		ctx, priorCtx = ctxInstant, ctxPriorInstant
	}
	r := ixbrl.Row{Label: label, Emphasis: emphasis, Amount: figure(concept, ctx, get(a.Figures), deduct)}
	if a.Prior != nil {
		r.Prior = figure(concept, priorCtx, get(*a.Prior), deduct)
	}
	return r
}

// heads are the column headings: the year end, then the year before's when there is one.
func (a Accounts) heads() []string {
	h := []string{iso(a.FY.End)}
	if a.Prior != nil {
		h = append(h, iso(a.Prior.FY.End))
	}
	return h
}

// document builds the statutory accounts as an inline-XBRL document.
func (a Accounts) document() ixbrl.Document {
	contexts := []ixbrl.Context{
		{ID: ctxInstant, EntityScheme: chScheme, EntityID: a.Co.Number, Instant: iso(a.FY.End)},
		{ID: ctxPeriod, EntityScheme: chScheme, EntityID: a.Co.Number, Start: iso(a.FY.Start), End: iso(a.FY.End)},
	}
	period := "Micro-entity accounts for the year ended " + iso(a.FY.End)
	if a.Prior != nil {
		contexts = append(contexts,
			ixbrl.Context{ID: ctxPriorInstant, EntityScheme: chScheme, EntityID: a.Co.Number, Instant: iso(a.Prior.FY.End)},
			ixbrl.Context{ID: ctxPriorPeriod, EntityScheme: chScheme, EntityID: a.Co.Number, Start: iso(a.Prior.FY.Start), End: iso(a.Prior.FY.End)},
		)
		period += ", with comparative figures for the year ended " + iso(a.Prior.FY.End)
	}

	const instant, duration = true, false
	body := []ixbrl.Block{
		ixbrl.Heading(1, ixbrl.NonNumeric("uk-bus:EntityCurrentLegalOrRegisteredName", ctxInstant, a.Co.Name)),
		ixbrl.Paragraph("muted", ixbrl.Text("Company registration number "), ixbrl.NonNumeric("uk-bus:UKCompaniesHouseRegisteredNumber", ctxInstant, a.Co.Number)),
		ixbrl.Paragraph("", ixbrl.Text(period)),

		ixbrl.Heading(2, ixbrl.Text("Balance sheet as at "+iso(a.FY.End))),
		ixbrl.Table(a.heads(),
			a.line("Fixed assets", "", "uk-core:FixedAssets", instant, func(f Figures) money.Money { return f.FixedAssets }, false),
			a.line("Current assets", "", "uk-core:CurrentAssets", instant, func(f Figures) money.Money { return f.CurrentAssets }, false),
			a.line("Creditors: amounts falling due within one year", "", "uk-core:Creditors", instant, func(f Figures) money.Money { return f.CreditorsWithin1Yr }, true),
			a.line("Net current assets", "subtotal", "uk-core:NetCurrentAssetsLiabilities", instant, func(f Figures) money.Money { return f.NetCurrentAssets }, false),
			a.line("Total assets less current liabilities", "subtotal", "uk-core:TotalAssetsLessCurrentLiabilities", instant, func(f Figures) money.Money { return f.TotalAssetsLessCL }, false),
			a.line("Net assets", "total", "uk-core:NetAssetsLiabilities", instant, func(f Figures) money.Money { return f.NetAssets }, false),
		),

		ixbrl.Heading(2, ixbrl.Text("Capital and reserves")),
		ixbrl.Table(a.heads(),
			a.line("Called-up share capital", "", "uk-core:CalledUpShareCapital", instant, func(f Figures) money.Money { return f.CalledUpCapital }, false),
			a.line("Profit and loss account", "", "uk-core:RetainedEarningsAccumulatedLosses", instant, func(f Figures) money.Money { return f.ProfitLossReserve }, false),
			a.line("Shareholders' funds", "total", "uk-core:Equity", instant, func(f Figures) money.Money { return f.CapitalAndReserves }, false),
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
		ixbrl.Table(a.heads(),
			a.line("Turnover", "", "uk-core:TurnoverRevenue", duration, func(f Figures) money.Money { return f.Turnover }, false),
			a.line("Cost of sales", "", "uk-core:CostSales", duration, func(f Figures) money.Money { return f.CostOfSales }, true),
			a.line("Gross profit", "subtotal", "uk-core:GrossProfitLoss", duration, func(f Figures) money.Money { return f.GrossProfit }, false),
			a.line("Administrative expenses", "", "uk-core:AdministrativeExpenses", duration, func(f Figures) money.Money { return f.AdminExpenses }, true),
			a.line("Profit before taxation", "subtotal", "uk-core:ProfitLossOnOrdinaryActivitiesBeforeTax", duration, func(f Figures) money.Money { return f.ProfitBeforeTax }, false),
			a.line("Tax on profit", "", "uk-core:TaxTaxCreditOnProfitOrLossOnOrdinaryActivities", duration, func(f Figures) money.Money { return f.Tax }, true),
			a.line("Profit for the financial year", "total", "uk-core:ProfitLoss", duration, func(f Figures) money.Money { return f.ProfitForYear }, false),
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
		Contexts:   contexts,
		Units:      []ixbrl.Unit{{ID: gbpUnit, Measure: "iso4217:GBP"}, {ID: pureUnit, Measure: "xbrli:pure"}},
		Body:       body,
	}
}

// IXBRL renders the statutory accounts as an inline-XBRL XHTML document.
func (a Accounts) IXBRL() (string, error) { return a.document().Render() }
