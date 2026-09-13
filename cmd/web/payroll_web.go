package main

import (
	"fmt"
	"time"

	"github.com/richardjennings/accounts/ledger"
	"github.com/richardjennings/accounts/money"
	"github.com/richardjennings/accounts/tax/payroll"
	"github.com/richardjennings/accounts/themes"
	"github.com/richardjennings/accounts/themes/payyourself"
)

func taxYearStart(d ledger.Date) int {
	y := d.Year
	if d.Month < time.April || d.Month == time.April && d.Day < 6 {
		y--
	}
	return y
}

// payrollOperation applies the annual calculation cumulatively, retaining each
// payment's incremental deductions for the payslip and P60. Backdating within an
// already assessed year would require recalculating later payslips, so reject it.
func (a *app) payrollOperation(name string, when ledger.Date, gross money.Money, code, plan string, annualBIK money.Money, pension bool) (themes.Operation, error) {
	var previous []payroll.Result
	bikPosted := money.Zero(a.co.Currency)
	for _, r := range a.runs {
		if r.Employee != name || taxYearStart(r.Date) != taxYearStart(when) {
			continue
		}
		if when.Before(r.Date) {
			return nil, fmt.Errorf("payroll for %s already runs through %s; use that date or later", name, r.Date)
		}
		previous = append(previous, r.Result)
		bikPosted, _ = bikPosted.Add(r.Result.BenefitsInKind)
	}
	if annualBIK.Currency().Code == "" {
		annualBIK = money.Zero(a.co.Currency)
	}
	additionalBIK, err := annualBIK.Sub(bikPosted)
	if err != nil {
		return nil, err
	}
	if additionalBIK.IsNegative() {
		additionalBIK = money.Zero(a.co.Currency)
	}
	ty := taxYearOn(when)
	res, err := payroll.ComputeCumulative(payroll.Input{GrossAnnual: gross, Rates: ty.Rates, TaxCode: code,
		StudentLoan: ty.Plan(plan), BenefitsInKind: additionalBIK, AutoEnrol: pension, Pension: ty.Pension}, previous)
	if err != nil {
		return nil, err
	}
	ref := a.ref("SAL")
	taxNIC, _ := res.IncomeTax.Add(res.EmployeeNIC)
	taxNIC, _ = taxNIC.Add(res.StudentLoan)
	erNIC, _ := res.EmployerNIC.Add(res.Class1A)
	op := payyourself.Salary{Date: when, Ref: ref, Gross: gross, TaxNIC: taxNIC, EmployerNIC: erNIC,
		EmployeePension: res.EmployeePension, EmployerPension: res.EmployerPension, Bank: a.main()}
	return afterPost(op, func() {
		a.runs = append(a.runs, payrollRun{Employee: name, TaxCode: code, Date: when, Ref: ref, Result: res})
	}), nil
}
