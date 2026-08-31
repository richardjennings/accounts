// Package payroll computes PAYE income tax and National Insurance on a director's
// salary. Everything rate-related is configuration: a RateTable holds every
// threshold and percentage as data, so a new tax year — or Scotland's bands, or a
// hand-tweaked scenario — is a different table, not different code. The bundled
// Year2025_26 table is verified against HMRC's published rates and thresholds.
//
// It uses the directors' annual earnings period (an owner-director is assessed on
// total earnings for the year). PAYE tax bands apply to taxable income after the
// personal allowance; employee NI runs between the primary threshold and the upper
// earnings limit; employer NI runs above the secondary threshold.
package payroll

import (
	"math/big"
	"strconv"
	"strings"

	"github.com/richardjennings/accounts/money"
	"github.com/richardjennings/decimal"
)

func gbp(s string) money.Money     { return money.MustParse(money.GBP, s) }
func dec(s string) decimal.Decimal { return decimal.MustParse(s) }
func toRat(m money.Money) *big.Rat { return m.Amount().Rat() }

// Band is one income-tax band: its rate applies to taxable income up to UpTo
// (a cumulative limit measured from zero). A zero UpTo marks the open-ended top band.
type Band struct {
	UpTo money.Money
	Rate decimal.Decimal
}

// RateTable is a full set of payroll rates and thresholds — the unit of
// configuration. Swap or edit it to model a different tax year or region.
type RateTable struct {
	Name                string
	PersonalAllowance   money.Money
	Bands               []Band          // income-tax bands on taxable income
	PrimaryThreshold    money.Money     // employee NI lower threshold
	UpperEarningsLimit  money.Money     // employee NI upper limit
	EmployeeRate        decimal.Decimal // employee NI between PT and UEL
	EmployeeUpperRate   decimal.Decimal // employee NI above UEL
	SecondaryThreshold  money.Money     // employer NI threshold
	EmployerRate        decimal.Decimal // employer NI above the secondary threshold
	EmploymentAllowance money.Money     // annual employer-NI offset, if eligible
}

// Year2025_26 is the England, Wales & Northern Ireland table for 2025/26, verified
// against HMRC's "Rates and thresholds for employers 2025 to 2026".
var Year2025_26 = RateTable{
	Name:              "2025/26 (England, Wales & NI)",
	PersonalAllowance: gbp("12570.00"),
	Bands: []Band{
		{UpTo: gbp("37700.00"), Rate: dec("0.20")},       // basic
		{UpTo: gbp("125140.00"), Rate: dec("0.40")},      // higher
		{UpTo: money.Zero(money.GBP), Rate: dec("0.45")}, // additional (open-ended)
	},
	PrimaryThreshold:    gbp("12570.00"),
	UpperEarningsLimit:  gbp("50270.00"),
	EmployeeRate:        dec("0.08"),
	EmployeeUpperRate:   dec("0.02"),
	SecondaryThreshold:  gbp("5000.00"),
	EmployerRate:        dec("0.15"),
	EmploymentAllowance: gbp("10500.00"),
}

// StudentLoanPlan is a student/postgraduate loan repayment plan: a percentage of
// earnings above an annual threshold. Verified against HMRC 2025/26 figures.
type StudentLoanPlan struct {
	Name      string
	Threshold money.Money
	Rate      decimal.Decimal
}

var (
	Plan1    = StudentLoanPlan{"Plan 1", gbp("26065.00"), dec("0.09")}
	Plan2    = StudentLoanPlan{"Plan 2", gbp("28470.00"), dec("0.09")}
	Plan4    = StudentLoanPlan{"Plan 4", gbp("32745.00"), dec("0.09")}
	Plan5    = StudentLoanPlan{"Plan 5", gbp("25000.00"), dec("0.09")}
	Postgrad = StudentLoanPlan{"Postgraduate", gbp("21000.00"), dec("0.06")}
)

// StudentLoanPlans lists the selectable plans.
var StudentLoanPlans = []StudentLoanPlan{Plan1, Plan2, Plan4, Plan5, Postgrad}

// AutoEnrolment is a workplace pension's contribution rates and the band of
// qualifying earnings they apply to. The bundled default is the UK statutory
// auto-enrolment minimum for 2025/26 (verified against The Pensions Regulator):
// contributions on qualifying earnings between £6,240 and £50,270, at a total of 8%
// — employee 5% and employer 3%.
type AutoEnrolment struct {
	LowerLimit   money.Money
	UpperLimit   money.Money
	EmployeeRate decimal.Decimal
	EmployerRate decimal.Decimal
}

var AutoEnrol2025_26 = AutoEnrolment{
	LowerLimit:   gbp("6240.00"),
	UpperLimit:   gbp("50270.00"),
	EmployeeRate: dec("0.05"),
	EmployerRate: dec("0.03"),
}

// contributions returns the employee and employer pension contributions on the
// qualifying earnings — gross capped at the upper limit, less the lower limit.
func (s AutoEnrolment) contributions(gross money.Money, cur money.Currency) (employee, employer money.Money) {
	upper := gross
	if cmp, _ := gross.Cmp(s.UpperLimit); cmp > 0 {
		upper = s.UpperLimit
	}
	qual, err := upper.Sub(s.LowerLimit)
	if err != nil || !qual.IsPositive() {
		return money.Zero(cur), money.Zero(cur)
	}
	employee, _ = qual.Mul(s.EmployeeRate, money.HalfUp)
	employer, _ = qual.Mul(s.EmployerRate, money.HalfUp)
	return employee, employer
}

// StudentLoanByName returns the plan for a name, or the zero value (no plan).
func StudentLoanByName(name string) StudentLoanPlan {
	for _, p := range StudentLoanPlans {
		if p.Name == name {
			return p
		}
	}
	return StudentLoanPlan{}
}

// deduction is the annual repayment: rate × earnings above the threshold, rounded
// down (student-loan deductions favour the borrower).
func (p StudentLoanPlan) deduction(gross money.Money, cur money.Currency) money.Money {
	if p.Threshold.Currency().Code == "" {
		return money.Zero(cur)
	}
	above, err := gross.Sub(p.Threshold)
	if err != nil || !above.IsPositive() {
		return money.Zero(cur)
	}
	d, err := above.Mul(p.Rate, money.Down)
	if err != nil {
		return money.Zero(cur)
	}
	return d
}

// taxCodeAllowance derives the personal allowance from a PAYE tax code: numeric L
// codes give (number × 10); BR/0T/D0/D1 give none. Anything else falls back to def.
func taxCodeAllowance(code string, def money.Money) money.Money {
	code = strings.ToUpper(strings.TrimSpace(code))
	switch code {
	case "":
		return def
	case "BR", "0T", "D0", "D1":
		return money.Zero(def.Currency())
	}
	num := ""
	for _, c := range code {
		if c < '0' || c > '9' {
			break
		}
		num += string(c)
	}
	n, err := strconv.Atoi(num)
	if err != nil {
		return def
	}
	m, err := money.Parse(def.Currency(), strconv.Itoa(n*10))
	if err != nil {
		return def
	}
	return m
}

// Input is one director's annual salary plus the configuration to assess it.
type Input struct {
	GrossAnnual         money.Money
	Rates               RateTable       // if the zero value, Year2025_26 is used
	PersonalAllowance   money.Money     // override the table allowance; zero = use table/tax code
	TaxCode             string          // e.g. "1257L"; sets the allowance when given
	StudentLoan         StudentLoanPlan // repayment plan; zero value = none
	BenefitsInKind      money.Money     // taxable benefits (P11D value); zero = none
	EmploymentAllowance bool            // true if the company may claim the Employment Allowance
	AutoEnrol           bool            // true if enrolled in a workplace pension
	Pension             AutoEnrolment   // scheme to use when AutoEnrol; zero = AutoEnrol2025_26
}

// Result is the assessed salary, broken into its parts.
type Result struct {
	RateTable       string
	Gross           money.Money
	IncomeTax       money.Money // PAYE (on cash pay plus benefits in kind)
	EmployeeNIC     money.Money
	EmployerNIC     money.Money // secondary Class 1 NIC on cash pay, after any Employment Allowance
	Class1A         money.Money // employer Class 1A NIC on benefits in kind
	StudentLoan     money.Money
	BenefitsInKind  money.Money
	EmployeePension money.Money // employee's workplace-pension contribution (withheld)
	EmployerPension money.Money // employer's workplace-pension contribution
	Net             money.Money // Gross − IncomeTax − EmployeeNIC − StudentLoan − EmployeePension
	TotalCost       money.Money // Gross + EmployerNIC + Class1A + EmployerPension (cost to the company)
}

// Compute assesses the salary. All arithmetic is done in exact rationals and
// rounded once, to the penny.
func Compute(in Input) (Result, error) {
	rt := in.Rates
	if rt.Name == "" {
		rt = Year2025_26
	}
	cur := in.GrossAnnual.Currency()

	allowance := rt.PersonalAllowance
	if in.PersonalAllowance.Currency().Code != "" {
		allowance = in.PersonalAllowance
	}
	if in.TaxCode != "" {
		allowance = taxCodeAllowance(in.TaxCode, allowance)
	}

	bik := in.BenefitsInKind
	if bik.Currency().Code == "" {
		bik = money.Zero(cur)
	}
	taxablePay, err := in.GrossAnnual.Add(bik) // income tax on cash pay plus benefits in kind
	if err != nil {
		return Result{}, err
	}
	incomeTax := bandedTax(taxablePay, allowance, rt.Bands, cur)
	empNIC := employeeNIC(in.GrossAnnual, rt, cur) // employee NI on cash pay only
	erNIC := employerNIC(in.GrossAnnual, rt, in.EmploymentAllowance, cur)
	class1A := classOneA(bik, rt, cur) // employer NIC on benefits in kind
	studentLoan := in.StudentLoan.deduction(in.GrossAnnual, cur)

	eePension, erPension := money.Zero(cur), money.Zero(cur)
	if in.AutoEnrol {
		scheme := in.Pension
		if scheme.LowerLimit.Currency().Code == "" {
			scheme = AutoEnrol2025_26
		}
		eePension, erPension = scheme.contributions(in.GrossAnnual, cur)
	}

	net, err := subAll(in.GrossAnnual, incomeTax, empNIC, studentLoan, eePension)
	if err != nil {
		return Result{}, err
	}
	cost, err := addAll(in.GrossAnnual, erNIC, class1A, erPension) // gross + employer NIC + Class 1A + employer pension
	if err != nil {
		return Result{}, err
	}
	return Result{
		RateTable:       rt.Name,
		Gross:           in.GrossAnnual,
		IncomeTax:       incomeTax,
		EmployeeNIC:     empNIC,
		EmployerNIC:     erNIC,
		Class1A:         class1A,
		StudentLoan:     studentLoan,
		BenefitsInKind:  bik,
		EmployeePension: eePension,
		EmployerPension: erPension,
		Net:             net,
		TotalCost:       cost,
	}, nil
}

// classOneA is the employer's Class 1A NIC on benefits in kind: the employer
// secondary rate applied to the P11D value. The Employment Allowance does not
// offset Class 1A, so it is charged in full.
func classOneA(bik money.Money, rt RateTable, cur money.Currency) money.Money {
	if !bik.IsPositive() {
		return money.Zero(cur)
	}
	c, err := bik.Mul(rt.EmployerRate, money.HalfUp)
	if err != nil {
		return money.Zero(cur)
	}
	return c
}

// bandedTax charges each band's rate on the slice of taxable income that falls in it.
func bandedTax(gross, allowance money.Money, bands []Band, cur money.Currency) money.Money {
	taxable := new(big.Rat).Sub(toRat(gross), toRat(allowance))
	if taxable.Sign() <= 0 {
		return money.Zero(cur)
	}
	tax := new(big.Rat)
	prev := new(big.Rat)
	for _, b := range bands {
		top := new(big.Rat).Set(taxable) // open-ended top band reaches the whole taxable amount
		if !b.UpTo.IsZero() {
			top = toRat(b.UpTo)
		}
		hi := top
		if hi.Cmp(taxable) > 0 {
			hi = taxable
		}
		if hi.Cmp(prev) > 0 {
			portion := new(big.Rat).Sub(hi, prev)
			tax.Add(tax, portion.Mul(portion, b.Rate.Rat()))
		}
		prev = top
		if prev.Cmp(taxable) >= 0 {
			break
		}
	}
	return money.FromRat(cur, tax, money.HalfUp)
}

func employeeNIC(gross money.Money, rt RateTable, cur money.Currency) money.Money {
	g, pt, uel := toRat(gross), toRat(rt.PrimaryThreshold), toRat(rt.UpperEarningsLimit)
	nic := new(big.Rat)
	if g.Cmp(pt) > 0 {
		upper := g
		if upper.Cmp(uel) > 0 {
			upper = uel
		}
		main := new(big.Rat).Sub(upper, pt)
		nic.Add(nic, main.Mul(main, rt.EmployeeRate.Rat()))
		if g.Cmp(uel) > 0 {
			above := new(big.Rat).Sub(g, uel)
			nic.Add(nic, above.Mul(above, rt.EmployeeUpperRate.Rat()))
		}
	}
	return money.FromRat(cur, nic, money.HalfUp)
}

func employerNIC(gross money.Money, rt RateTable, eaEligible bool, cur money.Currency) money.Money {
	g, st := toRat(gross), toRat(rt.SecondaryThreshold)
	nic := new(big.Rat)
	if g.Cmp(st) > 0 {
		above := new(big.Rat).Sub(g, st)
		nic = above.Mul(above, rt.EmployerRate.Rat())
	}
	erNIC := money.FromRat(cur, nic, money.HalfUp)
	if eaEligible {
		if reduced, err := erNIC.Sub(rt.EmploymentAllowance); err == nil {
			if reduced.IsNegative() {
				return money.Zero(cur)
			}
			return reduced
		}
	}
	return erNIC
}

func subAll(from money.Money, subs ...money.Money) (money.Money, error) {
	acc := from
	for _, s := range subs {
		next, err := acc.Sub(s)
		if err != nil {
			return money.Money{}, err
		}
		acc = next
	}
	return acc, nil
}

func addAll(from money.Money, adds ...money.Money) (money.Money, error) {
	acc := from
	for _, s := range adds {
		next, err := acc.Add(s)
		if err != nil {
			return money.Money{}, err
		}
		acc = next
	}
	return acc, nil
}
