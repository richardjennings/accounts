package payroll

import "testing"

func TestCumulativeEnrollmentAppliesOnlyToCurrentPayment(t *testing.T) {
	first, err := ComputeCumulative(Input{GrossAnnual: gbp("10000.00")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ComputeCumulative(Input{GrossAnnual: gbp("10000.00"), AutoEnrol: true}, []Result{first})
	if err != nil {
		t.Fatal(err)
	}
	if second.EmployeePension.String() != "GBP 500.00" || second.EmployerPension.String() != "GBP 300.00" {
		t.Fatalf("second payment pension: employee=%s employer=%s", second.EmployeePension, second.EmployerPension)
	}
	third, err := ComputeCumulative(Input{GrossAnnual: gbp("10000.00")}, []Result{first, second})
	if err != nil {
		t.Fatal(err)
	}
	if !third.EmployeePension.IsZero() || !third.EmployerPension.IsZero() {
		t.Fatal("disabling pension refunded prior contributions")
	}
	wantNet, _ := subAll(third.Gross, third.IncomeTax, third.EmployeeNIC, third.StudentLoan, third.EmployeePension)
	if !third.Net.Equal(wantNet) {
		t.Fatal("incremental net pay does not match deductions")
	}
}
