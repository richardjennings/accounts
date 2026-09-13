package payroll

import "github.com/richardjennings/accounts/money"

// ComputeCumulative assesses a payment using the director's annual earnings
// period. GrossAnnual and BenefitsInKind are this payment's additions. Previous
// results must belong to the same employee and tax year. Only the difference
// from deductions already posted is charged in the returned result.
func ComputeCumulative(in Input, previous []Result) (Result, error) {
	cur := in.GrossAnnual.Currency()
	priorGross := money.Zero(cur)
	if in.BenefitsInKind.Currency().Code == "" {
		in.BenefitsInKind = money.Zero(cur)
	}
	for _, p := range previous {
		var err error
		in.GrossAnnual, err = in.GrossAnnual.Add(p.Gross)
		if err != nil {
			return Result{}, err
		}
		priorGross, _ = priorGross.Add(p.Gross)
		if p.BenefitsInKind.Currency().Code != "" {
			in.BenefitsInKind, err = in.BenefitsInKind.Add(p.BenefitsInKind)
			if err != nil {
				return Result{}, err
			}
		}
	}
	total, err := Compute(in)
	if err != nil {
		return Result{}, err
	}
	// Enrolment/loan selections apply to this payment, not retrospectively to
	// earlier payments. Preserve what was already withheld and add the part of
	// the annual qualifying band occupied by the new payment.
	ee, er := money.Zero(cur), money.Zero(cur)
	if in.AutoEnrol {
		scheme := in.Pension
		if scheme.LowerLimit.Currency().Code == "" {
			scheme = Latest().Pension
		}
		ee, er = scheme.contributions(in.GrossAnnual, cur)
		beforeEE, beforeER := scheme.contributions(priorGross, cur)
		ee, _ = ee.Sub(beforeEE)
		er, _ = er.Sub(beforeER)
	}
	loan, _ := in.StudentLoan.deduction(in.GrossAnnual, cur).Sub(in.StudentLoan.deduction(priorGross, cur))
	fields := func(r *Result) []*money.Money {
		return []*money.Money{
			&r.Gross, &r.IncomeTax, &r.EmployeeNIC, &r.EmployerNIC, &r.Class1A,
			&r.StudentLoan, &r.BenefitsInKind, &r.EmployeePension, &r.EmployerPension,
			&r.Net, &r.TotalCost,
		}
	}
	dest := fields(&total)
	for _, p := range previous {
		for i, value := range fields(&p) {
			if value.Currency().Code == "" {
				continue
			}
			*dest[i], err = dest[i].Sub(*value)
			if err != nil {
				return Result{}, err
			}
		}
	}
	total.EmployeePension, total.EmployerPension, total.StudentLoan = ee, er, loan
	total.Net, err = subAll(total.Gross, total.IncomeTax, total.EmployeeNIC, total.StudentLoan, total.EmployeePension)
	if err != nil {
		return Result{}, err
	}
	total.TotalCost, err = addAll(total.Gross, total.EmployerNIC, total.Class1A, total.EmployerPension)
	if err != nil {
		return Result{}, err
	}
	return total, nil
}
