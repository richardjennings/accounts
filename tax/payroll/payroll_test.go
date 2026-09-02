package payroll

import (
	"testing"
	"time"

	"github.com/richardjennings/accounts/chart"
	"github.com/richardjennings/accounts/ledger"
	"github.com/richardjennings/accounts/money"
	"github.com/richardjennings/accounts/themes/payyourself"
)

func mustCompute(t *testing.T, in Input) Result {
	t.Helper()
	r, err := Compute(in)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func check(t *testing.T, label, got, want string) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %s, want %s", label, got, want)
	}
}

func TestStudentLoanPlan2(t *testing.T) {
	y2025 := TaxYearOn(2025, time.June, 1)
	r := mustCompute(t, Input{GrossAnnual: gbp("30000.00"), TaxCode: "1257L", Rates: y2025.Rates, StudentLoan: y2025.Plan(Plan2Name)})
	check(t, "student loan", r.StudentLoan.String(), "GBP 137.70") // 9% × (30000 − 28470)
	check(t, "net", r.Net.String(), "GBP 24981.90")                // 30000 − 3486 − 1394.40 − 137.70

	// 2026/27 raises the Plan 2 threshold to £29,385.
	r = mustCompute(t, Input{GrossAnnual: gbp("30000.00"), TaxCode: "1257L", StudentLoan: StudentLoanByName(Plan2Name)})
	check(t, "student loan 2026/27", r.StudentLoan.String(), "GBP 55.35") // 9% × (30000 − 29385)
}

// TestTaxYearOn: the tax year turns on 6 April, and a date after the last bundled
// year still gets that year.
func TestTaxYearOn(t *testing.T) {
	cases := []struct {
		y    int
		m    time.Month
		d    int
		want int
	}{
		{2026, time.April, 5, 2025}, {2026, time.April, 6, 2026}, {2026, time.September, 2, 2026},
		{2027, time.March, 31, 2026}, {2024, time.June, 1, 2025}, {2030, time.June, 1, 2026},
	}
	for _, c := range cases {
		if got := TaxYearOn(c.y, c.m, c.d); got.Start != c.want {
			t.Errorf("TaxYearOn(%d-%d-%d) = %d, want %d", c.y, c.m, c.d, got.Start, c.want)
		}
	}
	if Latest().Rates.Name != "2026/27 (England, Wales & NI)" || TaxYearOn(2026, time.June, 1).Label() != "2026 to 2027" {
		t.Errorf("latest year = %s", Latest().Rates.Name)
	}
	// Every bundled year is complete.
	for _, ty := range TaxYears {
		if ty.Rates.Name == "" || len(ty.StudentLoans) != 5 || !ty.Pension.UpperLimit.IsPositive() || !ty.Rates.LowerEarningsLimit.IsPositive() {
			t.Errorf("tax year %d is incomplete: %+v", ty.Start, ty)
		}
	}
}

func TestBenefitInKindRaisesTax(t *testing.T) {
	r := mustCompute(t, Input{GrossAnnual: gbp("30000.00"), BenefitsInKind: gbp("5000.00")})
	check(t, "income tax", r.IncomeTax.String(), "GBP 4486.00")    // 20% × (35000 − 12570)
	check(t, "employee NI", r.EmployeeNIC.String(), "GBP 1394.40") // NI on cash pay only
}

// TestClass1AOnBenefits: a £5,000 benefit in kind draws employer Class 1A NIC at
// the secondary rate (15%), on top of the Class 1 employer NIC on cash pay, and it
// is not reduced by the Employment Allowance.
func TestClass1AOnBenefits(t *testing.T) {
	r := mustCompute(t, Input{GrossAnnual: gbp("30000.00"), BenefitsInKind: gbp("5000.00")})
	check(t, "Class 1A", r.Class1A.String(), "GBP 750.00")          // 15% × 5000
	check(t, "employer NIC", r.EmployerNIC.String(), "GBP 3750.00") // 15% × (30000 − 5000), cash only
	check(t, "total cost", r.TotalCost.String(), "GBP 34500.00")    // 30000 + 3750 + 750
}

func TestNoBenefitsNoClass1A(t *testing.T) {
	r := mustCompute(t, Input{GrossAnnual: gbp("30000.00")})
	check(t, "Class 1A", r.Class1A.String(), "GBP 0.00")
	check(t, "total cost", r.TotalCost.String(), "GBP 33750.00") // unchanged: gross + employer NIC only
}

// TestAutoEnrolmentPension: on a £30,000 salary, qualifying earnings are
// £30,000 − £6,240 = £23,760; the employee pays 5% (£1,188) and the employer 3%
// (£712.80). The employee's contribution reduces net pay; the employer's adds to cost.
func TestAutoEnrolmentPension(t *testing.T) {
	r := mustCompute(t, Input{GrossAnnual: gbp("30000.00"), AutoEnrol: true})
	check(t, "employee pension", r.EmployeePension.String(), "GBP 1188.00") // 5% × 23760
	check(t, "employer pension", r.EmployerPension.String(), "GBP 712.80")  // 3% × 23760
	check(t, "net", r.Net.String(), "GBP 23931.60")                         // 30000 − 3486 − 1394.40 − 1188
	check(t, "total cost", r.TotalCost.String(), "GBP 34462.80")            // 30000 + 3750 + 712.80
}

func TestNoAutoEnrolNoPension(t *testing.T) {
	r := mustCompute(t, Input{GrossAnnual: gbp("30000.00")})
	check(t, "employee pension", r.EmployeePension.String(), "GBP 0.00")
	check(t, "employer pension", r.EmployerPension.String(), "GBP 0.00")
}

func TestTaxCodeBRNoAllowance(t *testing.T) {
	r := mustCompute(t, Input{GrossAnnual: gbp("30000.00"), TaxCode: "BR"})
	check(t, "income tax", r.IncomeTax.String(), "GBP 6000.00") // 20% × 30000, no allowance
}

// TestDirectorSalaryAtAllowance: a £12,570 salary — the classic owner-director
// level — bears no income tax and no employee NI, but £1,135.50 employer NI.
func TestDirectorSalaryAtAllowance(t *testing.T) {
	r := mustCompute(t, Input{GrossAnnual: gbp("12570.00")})
	check(t, "income tax", r.IncomeTax.String(), "GBP 0.00")
	check(t, "employee NIC", r.EmployeeNIC.String(), "GBP 0.00")
	check(t, "employer NIC", r.EmployerNIC.String(), "GBP 1135.50") // 15% × (12570 − 5000)
	check(t, "net", r.Net.String(), "GBP 12570.00")
	check(t, "total cost", r.TotalCost.String(), "GBP 13705.50")
}

func TestSalaryInBasicBand(t *testing.T) {
	r := mustCompute(t, Input{GrossAnnual: gbp("30000.00")})
	check(t, "income tax", r.IncomeTax.String(), "GBP 3486.00")     // 20% × (30000 − 12570)
	check(t, "employee NIC", r.EmployeeNIC.String(), "GBP 1394.40") // 8% × (30000 − 12570)
	check(t, "employer NIC", r.EmployerNIC.String(), "GBP 3750.00") // 15% × (30000 − 5000)
	check(t, "net", r.Net.String(), "GBP 25119.60")
	check(t, "total cost", r.TotalCost.String(), "GBP 33750.00")
}

func TestSalaryIntoHigherBand(t *testing.T) {
	r := mustCompute(t, Input{GrossAnnual: gbp("60000.00")})
	check(t, "income tax", r.IncomeTax.String(), "GBP 11432.00")    // 20%×37700 + 40%×9730
	check(t, "employee NIC", r.EmployeeNIC.String(), "GBP 3210.60") // 8%×37700 + 2%×9730
	check(t, "net", r.Net.String(), "GBP 45357.40")
}

func TestEmploymentAllowanceWipesEmployerNIC(t *testing.T) {
	r := mustCompute(t, Input{GrossAnnual: gbp("30000.00"), EmploymentAllowance: true})
	check(t, "employer NIC", r.EmployerNIC.String(), "GBP 0.00") // 3750 offset by the £10,500 allowance
	check(t, "total cost", r.TotalCost.String(), "GBP 30000.00")
}

// TestConfigurableRatesAndAllowance proves the numbers are data, not code: a 0T tax
// code (no allowance) taxes the whole salary, and swapping the employer rate to the
// pre-2025 13.8% changes the employer NI.
func TestConfigurableRatesAndAllowance(t *testing.T) {
	noAllowance := mustCompute(t, Input{GrossAnnual: gbp("30000.00"), PersonalAllowance: gbp("0.00")})
	check(t, "income tax (0T)", noAllowance.IncomeTax.String(), "GBP 6000.00") // 20% × 30000

	oldRates := Year2025_26
	oldRates.Name = "custom (13.8% employer)"
	oldRates.EmployerRate = dec("0.138")
	custom := mustCompute(t, Input{GrossAnnual: gbp("30000.00"), Rates: oldRates})
	check(t, "employer NIC (13.8%)", custom.EmployerNIC.String(), "GBP 3450.00") // 13.8% × 25000
	check(t, "rate table name", custom.RateTable, "custom (13.8% employer)")
}

// TestFeedsSalaryTheme runs the calculator into the Pay Yourself theme: the
// withheld PAYE + employee NI becomes the Salary's TaxNIC.
func TestFeedsSalaryTheme(t *testing.T) {
	r := mustCompute(t, Input{GrossAnnual: gbp("30000.00")})
	taxNIC, err := r.IncomeTax.Add(r.EmployeeNIC)
	if err != nil {
		t.Fatal(err)
	}
	book, err := chart.NewUKMicroLtdBook(money.GBP)
	if err != nil {
		t.Fatal(err)
	}
	j, err := payyourself.Salary{Date: ledger.NewDate(2026, time.April, 30), Ref: "PAY-YR", Gross: r.Gross, TaxNIC: taxNIC, EmployerNIC: r.EmployerNIC}.Journal()
	if err != nil {
		t.Fatal(err)
	}
	if err := book.Post(j); err != nil {
		t.Fatal(err)
	}
	bal, _ := book.Balance(chart.Salaries)
	check(t, "salaries expense", bal.String(), "GBP 30000.00")
	er, _ := book.Balance(chart.EmployerNIC)
	check(t, "employer NIC expense", er.String(), "GBP 3750.00")
	paye, _ := book.Balance(chart.PAYENIC)
	check(t, "PAYE/NIC owed", paye.String(), "GBP 8630.40") // 4880.40 employee + 3750 employer
	bank, _ := book.Balance(chart.Bank)
	check(t, "net to bank", bank.String(), "GBP -25119.60")
}
