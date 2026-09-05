package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/richardjennings/accounts/chart"
	"github.com/richardjennings/accounts/company"
	"github.com/richardjennings/accounts/dividends"
	"github.com/richardjennings/accounts/explain"
	"github.com/richardjennings/accounts/ledger"
	"github.com/richardjennings/accounts/money"
	"github.com/richardjennings/accounts/report"
)

// mcpTool is one read-only tool: its catalogue entry and the function that
// answers it. run is called with the app lock held.
type mcpTool struct {
	name        string
	description string
	schema      map[string]any // JSON Schema of the arguments
	run         func(s *mcpServer, args json.RawMessage) (any, error)
}

// mcpCatalogue lists the tools in the order tools/list presents them.
var mcpCatalogue = []mcpTool{
	{
		name: "company",
		description: "The company: identity, the game date (today), the current financial year, closed periods, " +
			"VAT status, officers, shareholders, people with significant control, share capital, " +
			"and every filing and payment date ahead.",
		schema: noArgs,
		run:    toolCompany,
	},
	{
		name: "position",
		description: "The financial position now, as the Overview shows: every bank balance, cash, debtors, " +
			"creditors, VAT, PAYE/NIC, corporation tax payable, the director's loan, this year's income, " +
			"expenses and profit, and distributable reserves.",
		schema: noArgs,
		run:    toolPosition,
	},
	{
		name: "dividend_capacity",
		description: "How much dividend the company can pay: distributable reserves (the lawful ceiling), " +
			"the corporation tax estimated but not yet charged, the prudent maximum after that tax, " +
			"cash available, and each shareholder's share. Give proposed to test a specific amount against reserves.",
		schema: argSchema(map[string]any{
			"proposed": arg("string", "A dividend amount to test, e.g. \"2500.00\"."),
		}),
		run: toolDividendCapacity,
	},
	{
		name: "dividends",
		description: "Every dividend declared and every dividend paid, latest first, with the last of each " +
			"and this financial year's totals.",
		schema: noArgs,
		run:    toolDividends,
	},
	{
		name:        "profit_and_loss",
		description: "Income and expenses over a period, by account. Defaults to the current financial year.",
		schema: argSchema(map[string]any{
			"from": arg("string", "Start of the period, YYYY-MM-DD. Defaults to the start of the financial year."),
			"to":   arg("string", "End of the period, YYYY-MM-DD. Defaults to the end of the financial year."),
		}),
		run: toolProfitAndLoss,
	},
	{
		name: "balance_sheet",
		description: "Assets, liabilities and equity as at a date. Defaults to the financial year end, " +
			"as the Accounting pages show.",
		schema: argSchema(map[string]any{
			"as_at": arg("string", "The date, YYYY-MM-DD."),
		}),
		run: toolBalanceSheet,
	},
	{
		name:        "trial_balance",
		description: "Every account with a balance, in debit and credit columns, with the totals.",
		schema:      noArgs,
		run:         toolTrialBalance,
	},
	{
		name: "journals",
		description: "Posted journals, latest first, each with its postings and a plain-language explanation. " +
			"Filter by section, reference prefix, narrative text or date range.",
		schema: argSchema(map[string]any{
			"limit":   arg("integer", "How many journals to return. Default 20, maximum 500."),
			"section": arg("string", "Only one section, e.g. sales, expenses, banking, pay-yourself, company-tax or company."),
			"ref":     arg("string", "Only references that start with this, e.g. DIV for dividends declared, DVP for dividends paid."),
			"search":  arg("string", "Only narratives that contain this text, case-insensitive."),
			"from":    arg("string", "Earliest date, YYYY-MM-DD."),
			"to":      arg("string", "Latest date, YYYY-MM-DD."),
		}),
		run: toolJournals,
	},
	{
		name:        "invoices",
		description: "Sales invoices with what has been received and what is outstanding, latest first.",
		schema: argSchema(map[string]any{
			"open_only": arg("boolean", "Only invoices not yet settled."),
		}),
		run: toolInvoices,
	},
	{
		name:        "bills",
		description: "Supplier bills with what has been paid and what is outstanding, latest first.",
		schema: argSchema(map[string]any{
			"open_only": arg("boolean", "Only bills not yet settled."),
		}),
		run: toolBills,
	},
	{
		name: "payroll",
		description: "Employees and every payroll run, latest first: gross pay, PAYE, National Insurance, " +
			"pension, student loan, net pay and the cost to the company.",
		schema: noArgs,
		run:    toolPayroll,
	},
	{
		name: "corporation_tax",
		description: "The corporation tax estimate on this year's profit to date: adjustments, taxable profit, " +
			"rate band, charge, what is already charged in the books, the payable balance and when payment is due.",
		schema: noArgs,
		run:    toolCorporationTax,
	},
}

func findTool(name string) (mcpTool, bool) {
	for _, t := range mcpCatalogue {
		if t.name == name {
			return t, true
		}
	}
	return mcpTool{}, false
}

// mcpToolList renders the catalogue as tools/list returns it.
func mcpToolList() []map[string]any {
	out := make([]map[string]any, 0, len(mcpCatalogue))
	for _, t := range mcpCatalogue {
		out = append(out, map[string]any{
			"name":        t.name,
			"description": t.description,
			"inputSchema": t.schema,
			"annotations": map[string]any{"readOnlyHint": true},
		})
	}
	return out
}

// noArgs is the schema of a tool that takes no arguments.
var noArgs = map[string]any{"type": "object", "properties": map[string]any{}}

func argSchema(props map[string]any) map[string]any {
	return map[string]any{"type": "object", "properties": props}
}

func arg(typ, desc string) map[string]any { return map[string]any{"type": typ, "description": desc} }

// decodeArgs fills v from the call's arguments. An unknown argument is an
// error, so a misspelt name is not silently ignored.
func decodeArgs(args json.RawMessage, v any) error {
	raw := bytes.TrimSpace(args)
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("arguments: %v", err)
	}
	return nil
}

// isoDate parses a YYYY-MM-DD argument; "" gives def.
func isoDate(s string, def ledger.Date) (ledger.Date, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return def, nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return ledger.Date{}, fmt.Errorf("date %q: want YYYY-MM-DD", s)
	}
	return ledger.NewDate(t.Year(), t.Month(), t.Day()), nil
}

// amt renders money as a plain decimal string, e.g. "1234.56". A zero-value
// Money, which a missing figure leaves behind, renders as zero.
func amt(m money.Money) string {
	if m.Currency().Code == "" {
		return "0.00"
	}
	return m.Amount().StringPlain()
}

// dateStr renders a date, or "" for the zero date.
func dateStr(d ledger.Date) string {
	if d.IsZero() {
		return ""
	}
	return d.String()
}

// pct renders part of total as a percentage with one decimal place.
func pct(part, total int) string {
	if total == 0 {
		return ""
	}
	return strconv.FormatFloat(float64(part)*100/float64(total), 'f', 1, 64)
}

func inYear(fy company.FinancialYear, d ledger.Date) bool {
	return !d.Before(fy.Start) && !fy.End.Before(d)
}

func (a *app) accountName(code string) string {
	if acc, ok := a.book.Account(code); ok {
		return acc.Name
	}
	return code
}

type mcpFY struct {
	Number int    `json:"number"`
	Start  string `json:"start"`
	End    string `json:"end"`
}

func fyView(fy company.FinancialYear) mcpFY {
	return mcpFY{Number: fy.Number, Start: fy.Start.String(), End: fy.End.String()}
}

type mcpLine struct {
	Code   string `json:"code"`
	Name   string `json:"name"`
	Amount string `json:"amount"`
}

func lines(ls []report.Line) []mcpLine {
	out := make([]mcpLine, 0, len(ls))
	for _, l := range ls {
		out = append(out, mcpLine{Code: l.Code, Name: l.Name, Amount: amt(l.Amount)})
	}
	return out
}

type mcpBank struct {
	Code      string `json:"code"`
	Name      string `json:"name"`
	Balance   string `json:"balance"`
	Main      bool   `json:"main"`
	Currency  string `json:"currency,omitempty"`   // set for a foreign-currency account
	FXBalance string `json:"fx_balance,omitempty"` // the balance in that currency
}

// bankViews lists the bank accounts with their balances and the total in the
// company currency.
func (a *app) bankViews() ([]mcpBank, money.Money) {
	total := money.Zero(a.co.Currency)
	out := make([]mcpBank, 0, len(a.banks))
	for _, b := range a.banks {
		bal := a.bal(b.Code)
		if sum, err := total.Add(bal); err == nil {
			total = sum
		}
		row := mcpBank{Code: b.Code, Name: b.Name, Balance: amt(bal), Main: b.Code == a.main()}
		if b.Currency != "" {
			row.Currency = b.Currency
			row.FXBalance = amt(a.fxBal(b.Code))
		}
		out = append(out, row)
	}
	return out, total
}

// --- company ---

type mcpOfficer struct {
	Name               string `json:"name"`
	Role               string `json:"role"`
	Appointed          string `json:"appointed,omitempty"`
	Resigned           string `json:"resigned,omitempty"`
	InOffice           bool   `json:"in_office"`
	IdentityVerifiedOn string `json:"identity_verified_on,omitempty"`
}

type mcpMember struct {
	Name       string `json:"name"`
	Class      string `json:"class"`
	Shares     int    `json:"shares"`
	HoldingPct string `json:"holding_pct"`
	Since      string `json:"since,omitempty"`
}

type mcpShareCapital struct {
	TotalShares     int    `json:"total_shares"`
	NominalPerShare string `json:"nominal_per_share"`
	IssuedCapital   string `json:"issued_capital"`
}

type mcpPSC struct {
	Name                 string `json:"name"`
	Notified             string `json:"notified,omitempty"`
	Ceased               string `json:"ceased,omitempty"`
	Shares               string `json:"shares"`
	Voting               string `json:"voting"`
	AppointsDirectors    bool   `json:"appoints_directors"`
	SignificantInfluence bool   `json:"significant_influence"`
}

type mcpKeyDate struct {
	Due       string `json:"due"`
	What      string `json:"what"`
	Detail    string `json:"detail,omitempty"`
	Recipient string `json:"recipient"`
	Overdue   bool   `json:"overdue"`
}

type mcpStatement struct {
	StatementDate string `json:"statement_date"`
	Due           string `json:"due"`
}

// mcpSource says where the figures come from.
type mcpSource struct {
	SaveFile string `json:"save_file,omitempty"`
	Saved    string `json:"saved,omitempty"` // when the save file was last written
	Note     string `json:"note,omitempty"`
}

type mcpCompany struct {
	Name                      string          `json:"name"`
	Number                    string          `json:"number"`
	SICCode                   string          `json:"sic_code"`
	RegisteredOffice          string          `json:"registered_office"`
	RegisteredEmail           string          `json:"registered_email"`
	Incorporated              string          `json:"incorporated"`
	YearEnd                   string          `json:"year_end"`
	Currency                  string          `json:"currency"`
	VATRegistered             bool            `json:"vat_registered"`
	VATNumber                 string          `json:"vat_number,omitempty"`
	VATQuarterEndMonth        string          `json:"vat_quarter_end_month,omitempty"`
	Today                     string          `json:"today"`
	FinancialYear             mcpFY           `json:"financial_year"`
	ClosedThrough             string          `json:"closed_through,omitempty"`
	Source                    mcpSource       `json:"source"`
	Officers                  []mcpOfficer    `json:"officers"`
	Shareholders              []mcpMember     `json:"shareholders"`
	ShareCapital              mcpShareCapital `json:"share_capital"`
	PSCs                      []mcpPSC        `json:"pscs"`
	KeyDates                  []mcpKeyDate    `json:"key_dates"`
	LastConfirmationStatement string          `json:"last_confirmation_statement,omitempty"`
	NextConfirmationStatement mcpStatement    `json:"next_confirmation_statement"`
}

func (s *mcpServer) source() mcpSource {
	switch {
	case s.path == "":
		return mcpSource{Note: "in-memory default company; no save file"}
	case s.stamp.modTime.IsZero():
		return mcpSource{SaveFile: s.path, Note: "no save file yet; this is the default company"}
	}
	return mcpSource{SaveFile: s.path, Saved: s.stamp.modTime.Format(time.RFC3339)}
}

func toolCompany(s *mcpServer, _ json.RawMessage) (any, error) {
	a := s.app
	co := a.co
	out := mcpCompany{
		Name: co.Name, Number: co.Number, SICCode: co.SICCode,
		RegisteredOffice: co.RegisteredOffice, RegisteredEmail: co.RegisteredEmail,
		Incorporated: dateStr(co.Incorporated), YearEnd: co.YearEndLabel(),
		Currency: co.Currency.Code, VATRegistered: co.VATRegistered, VATNumber: co.VATNumber,
		Today: a.today.String(), FinancialYear: fyView(a.fy()), ClosedThrough: dateStr(a.closedThrough),
		Source:                    s.source(),
		Officers:                  []mcpOfficer{},
		Shareholders:              []mcpMember{},
		PSCs:                      []mcpPSC{},
		KeyDates:                  []mcpKeyDate{},
		LastConfirmationStatement: dateStr(co.LastStatementDate),
	}
	if co.VATQuarterEndMonth != 0 {
		out.VATQuarterEndMonth = co.VATQuarterEndMonth.String()
	}
	total := a.reg.TotalShares()
	for _, o := range a.reg.Officers {
		out.Officers = append(out.Officers, mcpOfficer{
			Name: o.Name, Role: string(o.Role), Appointed: dateStr(o.Appointed), Resigned: dateStr(o.Resigned),
			InOffice: o.InOffice(), IdentityVerifiedOn: dateStr(o.IdentityVerifiedOn),
		})
	}
	for _, m := range a.reg.Members {
		out.Shareholders = append(out.Shareholders, mcpMember{
			Name: m.Name, Class: m.Class, Shares: m.Shares, HoldingPct: pct(m.Shares, total), Since: dateStr(m.Since),
		})
	}
	out.ShareCapital = mcpShareCapital{TotalShares: total, NominalPerShare: amt(a.reg.Nominal), IssuedCapital: amt(a.reg.IssuedCapital())}
	for _, p := range a.reg.PSCs {
		out.PSCs = append(out.PSCs, mcpPSC{
			Name: p.Name, Notified: dateStr(p.Notified), Ceased: dateStr(p.Ceased),
			Shares: p.Shares.String(), Voting: p.Voting.String(),
			AppointsDirectors: p.AppointsDirectors, SignificantInfluence: p.SignificantInfluence,
		})
	}
	for _, k := range co.KeyDates(a.today, a.keyDateOptions()) {
		out.KeyDates = append(out.KeyDates, mcpKeyDate{Due: k.Due.String(), What: k.What, Detail: k.Detail, Recipient: k.Recipient, Overdue: k.Overdue})
	}
	made, due := co.NextStatement()
	out.NextConfirmationStatement = mcpStatement{StatementDate: dateStr(made), Due: dateStr(due)}
	return out, nil
}

// --- position ---

type mcpFYFigures struct {
	mcpFY
	Income   string `json:"income"`
	Expenses string `json:"expenses"`
	Profit   string `json:"profit"`
}

type mcpPosition struct {
	AsAt                  string       `json:"as_at"`
	Currency              string       `json:"currency"`
	BankAccounts          []mcpBank    `json:"bank_accounts"`
	TotalBank             string       `json:"total_bank"`
	Cash                  string       `json:"cash"`
	TradeDebtors          string       `json:"trade_debtors"`
	TradeCreditors        string       `json:"trade_creditors"`
	VAT                   string       `json:"vat"`
	PAYENIC               string       `json:"paye_nic"`
	CorporationTaxPayable string       `json:"corporation_tax_payable"`
	DirectorsLoan         string       `json:"directors_loan"`
	Accruals              string       `json:"accruals"`
	Prepayments           string       `json:"prepayments"`
	FinancialYear         mcpFYFigures `json:"financial_year"`
	DistributableReserves string       `json:"distributable_reserves"`
	Notes                 []string     `json:"notes"`
}

func toolPosition(s *mcpServer, _ json.RawMessage) (any, error) {
	a := s.app
	fy := a.fy()
	pl, err := report.NewProfitAndLoss(a.book, fy.Start, fy.End)
	if err != nil {
		return nil, err
	}
	reserves, err := dividends.Available(a.book, fy.End)
	if err != nil {
		return nil, err
	}
	banks, total := a.bankViews()
	return mcpPosition{
		AsAt: a.today.String(), Currency: a.co.Currency.Code,
		BankAccounts: banks, TotalBank: amt(total),
		Cash:           amt(a.bal(chart.Cash)),
		TradeDebtors:   amt(a.bal(chart.TradeDebtors)),
		TradeCreditors: amt(a.bal(chart.TradeCreditors)),
		VAT:            amt(a.bal(chart.VAT)),
		PAYENIC:        amt(a.bal(chart.PAYENIC)),

		CorporationTaxPayable: amt(a.bal(chart.CorpTaxPayable)),
		DirectorsLoan:         amt(a.bal(chart.DirectorsLoan)),
		Accruals:              amt(a.bal(chart.Accruals)),
		Prepayments:           amt(a.bal(chart.Prepayments)),
		FinancialYear:         mcpFYFigures{mcpFY: fyView(fy), Income: amt(pl.TotalIncome), Expenses: amt(pl.TotalExpenses), Profit: amt(pl.Profit)},
		DistributableReserves: amt(reserves),
		Notes: []string{
			"Each balance is in the account's natural sense: an asset is positive when in funds, a liability is positive when owed. A positive directors_loan is money the company owes the director.",
			"Bank and cash balances count every posting. The financial year figures cover the year to its end.",
		},
	}, nil
}

// --- dividend_capacity ---

type mcpCTProvision struct {
	EstimatedCharge string `json:"estimated_charge"`
	ProvidedSoFar   string `json:"provided_so_far"`
	NotYetProvided  string `json:"not_yet_provided"`
}

type mcpCashView struct {
	MainAccount mcpBank `json:"main_account"`
	AllAccounts string  `json:"all_bank_accounts"`
}

type mcpShareOfDividend struct {
	Name       string `json:"name"`
	Shares     int    `json:"shares"`
	HoldingPct string `json:"holding_pct"`
	Share      string `json:"share_of_prudent_maximum"`
}

type mcpProposed struct {
	Amount    string `json:"amount"`
	Lawful    bool   `json:"lawful"`
	Shortfall string `json:"shortfall"`
}

type mcpDividendCapacity struct {
	Currency              string               `json:"currency"`
	Today                 string               `json:"today"`
	ReservesAsAt          string               `json:"reserves_as_at"`
	DistributableReserves string               `json:"distributable_reserves"`
	CorporationTax        mcpCTProvision       `json:"corporation_tax"`
	PrudentMaximum        string               `json:"prudent_maximum"`
	Cash                  mcpCashView          `json:"cash"`
	DirectorsLoan         string               `json:"directors_loan"`
	Shareholders          []mcpShareOfDividend `json:"shareholders"`
	Proposed              *mcpProposed         `json:"proposed,omitempty"`
	Notes                 []string             `json:"notes"`
}

func toolDividendCapacity(s *mcpServer, args json.RawMessage) (any, error) {
	var in struct {
		Proposed string `json:"proposed"`
	}
	if err := decodeArgs(args, &in); err != nil {
		return nil, err
	}
	a := s.app
	cur := a.co.Currency
	fy := a.fy()
	reserves, err := dividends.Available(a.book, fy.End)
	if err != nil {
		return nil, err
	}
	ct := a.estimateCT()
	provided := a.fyMovement(chart.CorpTaxCharge)
	unprovided, err := ct.Charge.Sub(provided)
	if err != nil || unprovided.IsNegative() {
		unprovided = money.Zero(cur)
	}
	prudent, err := reserves.Sub(unprovided)
	if err != nil {
		return nil, err
	}
	banks, totalBank := a.bankViews()
	out := mcpDividendCapacity{
		Currency: cur.Code, Today: a.today.String(), ReservesAsAt: fy.End.String(),
		DistributableReserves: amt(reserves),
		CorporationTax:        mcpCTProvision{EstimatedCharge: amt(ct.Charge), ProvidedSoFar: amt(provided), NotYetProvided: amt(unprovided)},
		PrudentMaximum:        amt(prudent),
		Cash:                  mcpCashView{AllAccounts: amt(totalBank)},
		DirectorsLoan:         amt(a.bal(chart.DirectorsLoan)),
		Shareholders:          []mcpShareOfDividend{},
		Notes: []string{
			"distributable_reserves is total equity less share capital as at reserves_as_at, the year end, as the Dividends page measures it. Companies Act 2006 section 830 allows a dividend only out of these reserves.",
			"prudent_maximum deducts the corporation tax estimated on this year's profit that is not yet charged in the books, so that money stays in the company for the tax bill.",
			"Cash is a separate limit: the company needs the money in the bank to pay what it declares. directors_loan is what the company already owes the director, including dividends declared but not yet paid.",
		},
	}
	for _, b := range banks {
		if b.Main {
			out.Cash.MainAccount = b
		}
	}
	shares := map[string]money.Money{}
	if prudent.IsPositive() {
		if awards, err := a.reg.AllocateDividend(prudent); err == nil {
			for _, aw := range awards {
				shares[aw.Member.Name] = aw.Amount
			}
		}
	}
	total := a.reg.TotalShares()
	for _, m := range a.reg.Members {
		share := money.Zero(cur)
		if v, ok := shares[m.Name]; ok {
			share = v
		}
		out.Shareholders = append(out.Shareholders, mcpShareOfDividend{Name: m.Name, Shares: m.Shares, HoldingPct: pct(m.Shares, total), Share: amt(share)})
	}
	if p := strings.TrimSpace(in.Proposed); p != "" {
		m, err := money.Parse(cur, p)
		if err != nil {
			return nil, fmt.Errorf("proposed: %v", err)
		}
		dec, err := dividends.Check(a.book, fy.End, m)
		if err != nil {
			return nil, err
		}
		out.Proposed = &mcpProposed{Amount: amt(dec.Requested), Lawful: dec.Lawful, Shortfall: amt(dec.Shortfall)}
	}
	return out, nil
}

// --- dividends ---

type mcpAward struct {
	Member string `json:"member"`
	Amount string `json:"amount"`
}

type mcpDividendDeclared struct {
	when      ledger.Date
	Date      string     `json:"date"`
	Ref       string     `json:"ref"`
	Amount    string     `json:"amount"`
	PerShare  string     `json:"per_share,omitempty"`
	Available string     `json:"reserves_at_declaration,omitempty"`
	Awards    []mcpAward `json:"awards,omitempty"`
	Voucher   bool       `json:"voucher"`
}

type mcpDividendPaid struct {
	when   ledger.Date
	Date   string `json:"date"`
	Ref    string `json:"ref"`
	Amount string `json:"amount"`
	Bank   string `json:"bank"`
}

type mcpDividendYear struct {
	mcpFY
	Declared string `json:"declared"`
	Paid     string `json:"paid"`
}

type mcpDividendsOut struct {
	Currency      string                `json:"currency"`
	LastDeclared  *mcpDividendDeclared  `json:"last_declared"`
	LastPaid      *mcpDividendPaid      `json:"last_paid"`
	FinancialYear mcpDividendYear       `json:"financial_year"`
	Declared      []mcpDividendDeclared `json:"declared"`
	Paid          []mcpDividendPaid     `json:"paid"`
	Notes         []string              `json:"notes"`
}

// postingAmount is the amount of the first posting on account and side, or of
// the first posting when none matches.
func postingAmount(j ledger.Journal, account string, side ledger.Side) money.Money {
	ps := j.Postings()
	for _, p := range ps {
		if p.Account == account && p.Side == side {
			return p.Amount
		}
	}
	if len(ps) > 0 {
		return ps[0].Amount
	}
	return money.Money{}
}

func toolDividends(s *mcpServer, _ json.RawMessage) (any, error) {
	a := s.app
	cur := a.co.Currency
	fy := a.fy()
	vouchers := map[string]dividendRun{}
	for _, d := range a.dividends {
		vouchers[d.Ref] = d
	}
	declared, paid := []mcpDividendDeclared{}, []mcpDividendPaid{}
	declaredFY, paidFY := money.Zero(cur), money.Zero(cur)
	for i := len(a.entries) - 1; i >= 0; i-- {
		j := a.entries[i].j
		switch {
		case strings.HasPrefix(j.Ref(), "DIV-"):
			amount := postingAmount(j, chart.Dividends, ledger.Debit)
			d := mcpDividendDeclared{when: j.Date(), Date: j.Date().String(), Ref: j.Ref(), Amount: amt(amount)}
			if v, ok := vouchers[j.Ref()]; ok {
				d.PerShare, d.Available, d.Voucher = v.PerShare, amt(v.Available), true
				for _, aw := range v.Awards {
					d.Awards = append(d.Awards, mcpAward{Member: aw.Member.Name, Amount: amt(aw.Amount)})
				}
			}
			declared = append(declared, d)
			if inYear(fy, j.Date()) {
				declaredFY, _ = declaredFY.Add(amount)
			}
		case strings.HasPrefix(j.Ref(), "DVP-"):
			amount, bank := money.Zero(cur), ""
			for _, p := range j.Postings() {
				if p.Side == ledger.Credit {
					amount, bank = p.Amount, a.accountName(p.Account)
				}
			}
			paid = append(paid, mcpDividendPaid{when: j.Date(), Date: j.Date().String(), Ref: j.Ref(), Amount: amt(amount), Bank: bank})
			if inYear(fy, j.Date()) {
				paidFY, _ = paidFY.Add(amount)
			}
		}
	}
	sort.SliceStable(declared, func(i, k int) bool { return declared[k].when.Before(declared[i].when) })
	sort.SliceStable(paid, func(i, k int) bool { return paid[k].when.Before(paid[i].when) })
	out := mcpDividendsOut{
		Currency:      cur.Code,
		FinancialYear: mcpDividendYear{mcpFY: fyView(fy), Declared: amt(declaredFY), Paid: amt(paidFY)},
		Declared:      declared,
		Paid:          paid,
		Notes: []string{
			"A declaration credits the director's loan account. The money leaves the bank only when a payment is recorded under Pay Yourself, Dividends.",
			"An imported history holds declarations only, so paid can be empty although the dividends were drawn through the director's loan account.",
		},
	}
	if len(declared) > 0 {
		out.LastDeclared = &declared[0]
	}
	if len(paid) > 0 {
		out.LastPaid = &paid[0]
	}
	return out, nil
}

// --- profit_and_loss, balance_sheet, trial_balance ---

type mcpProfitAndLoss struct {
	Currency      string    `json:"currency"`
	From          string    `json:"from"`
	To            string    `json:"to"`
	Income        []mcpLine `json:"income"`
	Expenses      []mcpLine `json:"expenses"`
	TotalIncome   string    `json:"total_income"`
	TotalExpenses string    `json:"total_expenses"`
	Profit        string    `json:"profit"`
}

func toolProfitAndLoss(s *mcpServer, args json.RawMessage) (any, error) {
	var in struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if err := decodeArgs(args, &in); err != nil {
		return nil, err
	}
	a := s.app
	fy := a.fy()
	from, err := isoDate(in.From, fy.Start)
	if err != nil {
		return nil, err
	}
	to, err := isoDate(in.To, fy.End)
	if err != nil {
		return nil, err
	}
	if to.Before(from) {
		return nil, fmt.Errorf("to %s is before from %s", to, from)
	}
	pl, err := report.NewProfitAndLoss(a.book, from, to)
	if err != nil {
		return nil, err
	}
	return mcpProfitAndLoss{
		Currency: a.co.Currency.Code, From: pl.From.String(), To: pl.To.String(),
		Income: lines(pl.Income), Expenses: lines(pl.Expenses),
		TotalIncome: amt(pl.TotalIncome), TotalExpenses: amt(pl.TotalExpenses), Profit: amt(pl.Profit),
	}, nil
}

type mcpBalanceSheet struct {
	Currency         string    `json:"currency"`
	AsAt             string    `json:"as_at"`
	Assets           []mcpLine `json:"assets"`
	Liabilities      []mcpLine `json:"liabilities"`
	Equity           []mcpLine `json:"equity"`
	ProfitForPeriod  string    `json:"profit_for_period"`
	TotalAssets      string    `json:"total_assets"`
	TotalLiabilities string    `json:"total_liabilities"`
	TotalEquity      string    `json:"total_equity"`
}

func toolBalanceSheet(s *mcpServer, args json.RawMessage) (any, error) {
	var in struct {
		AsAt string `json:"as_at"`
	}
	if err := decodeArgs(args, &in); err != nil {
		return nil, err
	}
	a := s.app
	asAt, err := isoDate(in.AsAt, a.fy().End)
	if err != nil {
		return nil, err
	}
	bs, err := report.NewBalanceSheet(a.book, asAt)
	if err != nil {
		return nil, err
	}
	return mcpBalanceSheet{
		Currency: a.co.Currency.Code, AsAt: bs.AsAt.String(),
		Assets: lines(bs.Assets), Liabilities: lines(bs.Liabilities), Equity: lines(bs.Equity),
		ProfitForPeriod: amt(bs.ProfitForPeriod),
		TotalAssets:     amt(bs.TotalAssets), TotalLiabilities: amt(bs.TotalLiabilities), TotalEquity: amt(bs.TotalEquity),
	}, nil
}

type mcpTBLine struct {
	Code   string `json:"code"`
	Name   string `json:"name"`
	Type   string `json:"type"`
	Debit  string `json:"debit"`
	Credit string `json:"credit"`
}

type mcpTrialBalance struct {
	Currency    string      `json:"currency"`
	Lines       []mcpTBLine `json:"lines"`
	TotalDebit  string      `json:"total_debit"`
	TotalCredit string      `json:"total_credit"`
	InBalance   bool        `json:"in_balance"`
}

func toolTrialBalance(s *mcpServer, _ json.RawMessage) (any, error) {
	a := s.app
	tb, err := a.book.TrialBalance()
	if err != nil {
		return nil, err
	}
	out := mcpTrialBalance{Currency: a.co.Currency.Code, Lines: []mcpTBLine{}, TotalDebit: amt(tb.TotalDebit), TotalCredit: amt(tb.TotalCredit), InBalance: tb.InBalance()}
	for _, l := range tb.Lines {
		out.Lines = append(out.Lines, mcpTBLine{Code: l.Account.Code, Name: l.Account.Name, Type: l.Account.Type.String(), Debit: amt(l.Debit), Credit: amt(l.Credit)})
	}
	return out, nil
}

// --- journals ---

type mcpPosting struct {
	AccountCode string `json:"account_code"`
	Account     string `json:"account"`
	Side        string `json:"side"`
	Amount      string `json:"amount"`
	Effect      string `json:"effect,omitempty"`
}

type mcpJournal struct {
	Date      string       `json:"date"`
	Ref       string       `json:"ref"`
	Section   string       `json:"section"`
	Narrative string       `json:"narrative"`
	Principle string       `json:"principle,omitempty"`
	Postings  []mcpPosting `json:"postings"`
}

type mcpJournalsOut struct {
	Currency string       `json:"currency"`
	Matched  int          `json:"matched"`
	Returned int          `json:"returned"`
	Journals []mcpJournal `json:"journals"`
}

// journalView narrates one posted journal for a reader.
func (a *app) journalView(e entry) mcpJournal {
	j := e.j
	ex := explain.ExplainJournal(a.book, j)
	jv := mcpJournal{Date: j.Date().String(), Ref: j.Ref(), Section: e.section, Narrative: j.Narrative(), Principle: ex.Principle, Postings: []mcpPosting{}}
	if e.principle != "" {
		jv.Principle = e.principle
	}
	for i, p := range j.Postings() {
		pv := mcpPosting{AccountCode: p.Account, Account: a.accountName(p.Account), Side: "debit", Amount: amt(p.Amount)}
		if p.Side == ledger.Credit {
			pv.Side = "credit"
		}
		if i < len(ex.Postings) {
			pv.Effect = ex.Postings[i].Effect
		}
		jv.Postings = append(jv.Postings, pv)
	}
	return jv
}

func toolJournals(s *mcpServer, args json.RawMessage) (any, error) {
	var in struct {
		Limit   int    `json:"limit"`
		Section string `json:"section"`
		Ref     string `json:"ref"`
		Search  string `json:"search"`
		From    string `json:"from"`
		To      string `json:"to"`
	}
	if err := decodeArgs(args, &in); err != nil {
		return nil, err
	}
	a := s.app
	limit := in.Limit
	switch {
	case limit <= 0:
		limit = 20
	case limit > 500:
		limit = 500
	}
	from, err := isoDate(in.From, ledger.Date{})
	if err != nil {
		return nil, err
	}
	to, err := isoDate(in.To, ledger.Date{})
	if err != nil {
		return nil, err
	}
	section, ref, search := strings.ToLower(strings.TrimSpace(in.Section)), strings.ToUpper(strings.TrimSpace(in.Ref)), strings.ToLower(strings.TrimSpace(in.Search))
	var matched []entry
	for i := len(a.entries) - 1; i >= 0; i-- {
		e := a.entries[i]
		j := e.j
		switch {
		case section != "" && strings.ToLower(e.section) != section:
			continue
		case ref != "" && !strings.HasPrefix(strings.ToUpper(j.Ref()), ref):
			continue
		case search != "" && !strings.Contains(strings.ToLower(j.Narrative()), search):
			continue
		case !from.IsZero() && j.Date().Before(from):
			continue
		case !to.IsZero() && to.Before(j.Date()):
			continue
		}
		matched = append(matched, e)
	}
	// Latest date first; journals on one date keep posting order, latest first.
	sort.SliceStable(matched, func(i, k int) bool { return matched[k].j.Date().Before(matched[i].j.Date()) })
	out := mcpJournalsOut{Currency: a.co.Currency.Code, Matched: len(matched), Journals: []mcpJournal{}}
	for _, e := range matched {
		if len(out.Journals) == limit {
			break
		}
		out.Journals = append(out.Journals, a.journalView(e))
	}
	out.Returned = len(out.Journals)
	return out, nil
}

// --- invoices, bills ---

type mcpInvoice struct {
	when        ledger.Date
	Ref         string `json:"ref"`
	Customer    string `json:"customer,omitempty"`
	Supplier    string `json:"supplier,omitempty"`
	Date        string `json:"date"`
	Total       string `json:"total"`
	Paid        string `json:"paid"`
	Outstanding string `json:"outstanding"`
	Status      string `json:"status"`
}

type mcpInvoicesOut struct {
	Currency    string       `json:"currency"`
	Count       int          `json:"count"`
	Total       string       `json:"total"`
	Outstanding string       `json:"outstanding"`
	Invoices    []mcpInvoice `json:"invoices"`
}

type mcpBillsOut struct {
	Currency    string       `json:"currency"`
	Count       int          `json:"count"`
	Total       string       `json:"total"`
	Outstanding string       `json:"outstanding"`
	Bills       []mcpInvoice `json:"bills"`
}

func toolInvoices(s *mcpServer, args json.RawMessage) (any, error) {
	var in struct {
		OpenOnly bool `json:"open_only"`
	}
	if err := decodeArgs(args, &in); err != nil {
		return nil, err
	}
	a := s.app
	list := a.sl.Invoices()
	if in.OpenOnly {
		list = a.sl.Outstanding()
	}
	rows := make([]mcpInvoice, 0, len(list))
	total, outstanding := money.Zero(a.co.Currency), money.Zero(a.co.Currency)
	for _, inv := range list {
		rows = append(rows, mcpInvoice{when: inv.Date, Ref: inv.Ref, Customer: inv.Customer, Date: inv.Date.String(), Total: amt(inv.Total), Paid: amt(inv.Paid()), Outstanding: amt(inv.Outstanding()), Status: inv.Status()})
		total, _ = total.Add(inv.Total)
		outstanding, _ = outstanding.Add(inv.Outstanding())
	}
	sortLatestFirst(rows)
	return mcpInvoicesOut{Currency: a.co.Currency.Code, Count: len(rows), Total: amt(total), Outstanding: amt(outstanding), Invoices: rows}, nil
}

func toolBills(s *mcpServer, args json.RawMessage) (any, error) {
	var in struct {
		OpenOnly bool `json:"open_only"`
	}
	if err := decodeArgs(args, &in); err != nil {
		return nil, err
	}
	a := s.app
	list := a.purch.Bills()
	if in.OpenOnly {
		list = a.purch.Outstanding()
	}
	rows := make([]mcpInvoice, 0, len(list))
	total, outstanding := money.Zero(a.co.Currency), money.Zero(a.co.Currency)
	for _, b := range list {
		rows = append(rows, mcpInvoice{when: b.Date, Ref: b.Ref, Supplier: b.Supplier, Date: b.Date.String(), Total: amt(b.Total), Paid: amt(b.Paid()), Outstanding: amt(b.Outstanding()), Status: b.Status()})
		total, _ = total.Add(b.Total)
		outstanding, _ = outstanding.Add(b.Outstanding())
	}
	sortLatestFirst(rows)
	return mcpBillsOut{Currency: a.co.Currency.Code, Count: len(rows), Total: amt(total), Outstanding: amt(outstanding), Bills: rows}, nil
}

// sortLatestFirst orders rows by date, latest first; rows on one date keep
// the order they were recorded in, latest first.
func sortLatestFirst(rows []mcpInvoice) {
	for i, k := 0, len(rows)-1; i < k; i, k = i+1, k-1 {
		rows[i], rows[k] = rows[k], rows[i]
	}
	sort.SliceStable(rows, func(i, k int) bool { return rows[k].when.Before(rows[i].when) })
}

// --- payroll ---

type mcpEmployee struct {
	Name           string `json:"name"`
	TaxCode        string `json:"tax_code"`
	StudentLoan    string `json:"student_loan,omitempty"`
	Salary         string `json:"salary"`
	BenefitsInKind string `json:"benefits_in_kind"`
	AutoEnrolled   bool   `json:"auto_enrolled"`
}

type mcpPayrollRun struct {
	when            ledger.Date
	Date            string `json:"date"`
	Ref             string `json:"ref"`
	Employee        string `json:"employee"`
	TaxCode         string `json:"tax_code"`
	RateTable       string `json:"rate_table"`
	Gross           string `json:"gross"`
	IncomeTax       string `json:"income_tax"`
	EmployeeNIC     string `json:"employee_nic"`
	EmployerNIC     string `json:"employer_nic"`
	Class1A         string `json:"class_1a"`
	StudentLoan     string `json:"student_loan"`
	BenefitsInKind  string `json:"benefits_in_kind"`
	EmployeePension string `json:"employee_pension"`
	EmployerPension string `json:"employer_pension"`
	Net             string `json:"net"`
	TotalCost       string `json:"total_cost"`
}

type mcpPayrollOut struct {
	Currency  string          `json:"currency"`
	TaxYear   string          `json:"tax_year"`
	Employees []mcpEmployee   `json:"employees"`
	LastRun   *mcpPayrollRun  `json:"last_run"`
	Runs      []mcpPayrollRun `json:"runs"`
	Notes     []string        `json:"notes"`
}

func toolPayroll(s *mcpServer, _ json.RawMessage) (any, error) {
	a := s.app
	out := mcpPayrollOut{
		Currency: a.co.Currency.Code, TaxYear: taxYearOn(a.today).Label(), Employees: []mcpEmployee{}, Runs: []mcpPayrollRun{},
		Notes: []string{
			"Runs are the salaries assessed in the app, with a payslip each. An imported history posts salaries as journals only; search journals for Salary to see them.",
		},
	}
	for _, e := range a.employees {
		out.Employees = append(out.Employees, mcpEmployee{Name: e.Name, TaxCode: e.TaxCode, StudentLoan: e.StudentLoan, Salary: amt(e.Salary), BenefitsInKind: amt(e.BIK), AutoEnrolled: e.AutoEnrol})
	}
	for i := len(a.runs) - 1; i >= 0; i-- {
		r := a.runs[i]
		res := r.Result
		out.Runs = append(out.Runs, mcpPayrollRun{
			when: r.Date, Date: r.Date.String(), Ref: r.Ref, Employee: r.Employee, TaxCode: r.TaxCode, RateTable: res.RateTable,
			Gross: amt(res.Gross), IncomeTax: amt(res.IncomeTax), EmployeeNIC: amt(res.EmployeeNIC), EmployerNIC: amt(res.EmployerNIC),
			Class1A: amt(res.Class1A), StudentLoan: amt(res.StudentLoan), BenefitsInKind: amt(res.BenefitsInKind),
			EmployeePension: amt(res.EmployeePension), EmployerPension: amt(res.EmployerPension), Net: amt(res.Net), TotalCost: amt(res.TotalCost),
		})
	}
	sort.SliceStable(out.Runs, func(i, k int) bool { return out.Runs[k].when.Before(out.Runs[i].when) })
	if len(out.Runs) > 0 {
		out.LastRun = &out.Runs[0]
	}
	return out, nil
}

// --- corporation_tax ---

type mcpCorporationTax struct {
	Currency              string   `json:"currency"`
	FinancialYear         mcpFY    `json:"financial_year"`
	ProfitBeforeTax       string   `json:"profit_before_tax"`
	DepreciationAddedBack string   `json:"depreciation_added_back"`
	CapitalAllowances     string   `json:"capital_allowances"`
	TaxableProfit         string   `json:"taxable_profit"`
	Band                  string   `json:"band"`
	Charge                string   `json:"charge"`
	MarginalRelief        string   `json:"marginal_relief"`
	EffectiveRatePct      string   `json:"effective_rate_pct"`
	ProvidedSoFar         string   `json:"provided_so_far"`
	PayableBalance        string   `json:"payable_balance"`
	PaymentDue            []string `json:"payment_due"`
	Notes                 []string `json:"notes"`
}

// ratePct renders part as a percentage of whole to two decimal places, for
// display beside the exact figures; "" when whole is not positive.
func ratePct(part, whole money.Money) string {
	p, okP := part.MinorUnits()
	w, okW := whole.MinorUnits()
	if !okP || !okW || w <= 0 {
		return ""
	}
	return strconv.FormatFloat(float64(p)*100/float64(w), 'f', 2, 64)
}

func toolCorporationTax(s *mcpServer, _ json.RawMessage) (any, error) {
	a := s.app
	fy := a.fy()
	ct := a.estimateCT()
	out := mcpCorporationTax{
		Currency: a.co.Currency.Code, FinancialYear: fyView(fy),
		ProfitBeforeTax:       amt(a.profitBeforeTax()),
		DepreciationAddedBack: amt(a.fyMovement(chart.Depreciation)),
		CapitalAllowances:     amt(a.capitalAllowances()),
		TaxableProfit:         amt(ct.TaxableProfit),
		Band:                  ct.Band,
		Charge:                amt(ct.Charge),
		MarginalRelief:        amt(ct.MarginalRelief),
		EffectiveRatePct:      ratePct(ct.Charge, ct.TaxableProfit),
		ProvidedSoFar:         amt(a.fyMovement(chart.CorpTaxCharge)),
		PayableBalance:        amt(a.bal(chart.CorpTaxPayable)),
		PaymentDue:            []string{},
		Notes: []string{
			"An estimate on this financial year's profit to date, as the Company Tax page shows. The charge is posted to the books at the year end.",
			"profit_before_tax adds back any charge already posted. effective_rate_pct is the charge as a percentage of taxable profit.",
		},
	}
	for _, p := range company.TaxPeriods(fy) {
		out.PaymentDue = append(out.PaymentDue, company.TaxPaymentDue(p).String())
	}
	return out, nil
}
