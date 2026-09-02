package main

import (
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

// TestPayrollUsesTheTaxYearOfTheRunDate runs the same Plan 2 employee in 2025/26
// and 2026/27 and checks each run uses that year's thresholds, then reads the
// payslip and P60 documents built from the runs.
func TestPayrollUsesTheTaxYearOfTheRunDate(t *testing.T) {
	a, err := newApp(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	h := a.persistMiddleware(a.routes())
	drive(t, h, "/pay-yourself/employees/add", url.Values{"name": {"Jo Coder"}, "amount": {"30000.00"}, "taxcode": {"1257L"}, "plan": {"Plan 2"}})

	drive(t, h, "/company/date", url.Values{"date": {"2026-09-01"}})
	drive(t, h, "/pay-yourself/employees/pay", url.Values{"emp": {"0"}})
	drive(t, h, "/company/date", url.Values{"date": {"2027-03-01"}})
	drive(t, h, "/pay-yourself/employees/pay", url.Values{"emp": {"0"}})
	drive(t, h, "/company/date", url.Values{"date": {"2026-03-01"}}) // back in 2025/26
	drive(t, h, "/pay-yourself/employees/pay", url.Values{"emp": {"0"}})
	if len(a.runs) != 3 {
		t.Fatalf("runs = %d, want 3", len(a.runs))
	}
	if got := a.runs[0].Result; got.RateTable != "2026/27 (England, Wales & NI)" || got.StudentLoan.String() != "GBP 55.35" {
		t.Errorf("2026/27 run = %s, student loan %s; want 2026/27 table and GBP 55.35", got.RateTable, got.StudentLoan)
	}
	if got := a.runs[2].Result; got.RateTable != "2025/26 (England, Wales & NI)" || got.StudentLoan.String() != "GBP 137.70" {
		t.Errorf("2025/26 run = %s, student loan %s; want 2025/26 table and GBP 137.70", got.RateTable, got.StudentLoan)
	}

	body := page(t, h, "/pay-yourself/employees")
	for _, s := range []string{"Payroll runs", "2026 to 2027", "2025 to 2026", "/pay-yourself/payslip?i=0", "/pay-yourself/p60?name=Jo%20Coder&amp;year=2026", "Payroll runs at the rates of the tax year today falls in: 2025/26"} {
		if !strings.Contains(body, s) {
			t.Errorf("employees page lacks %q", s)
		}
	}

	slip := page(t, h, "/pay-yourself/payslip?i=0")
	for _, s := range []string{"PAYSLIP", "Jo Coder", "£30,000.00", "Tax year 2026 to 2027", "£55.35", "£3,486.00", "£1,394.40", "£3,750.00"} {
		if !strings.Contains(slip, s) {
			t.Errorf("payslip lacks %q", s)
		}
	}

	// The 2026/27 P60 sums the two runs in that year: pay £60,000, and the NI bands
	// come from that year's limits (LEL £6,708, PT £12,570, UEL £50,270).
	p60 := page(t, h, "/pay-yourself/p60?name=Jo+Coder&year=2026")
	for _, s := range []string{"P60 End of Year Certificate", "Tax year to 5 April 2027", "2 payroll run(s)", "£60,000.00", "£6,972.00", "£6,708.00", "£5,862.00", "£37,700.00", "£2,788.80", "£110.70"} {
		if !strings.Contains(p60, s) {
			t.Errorf("P60 lacks %q", s)
		}
	}
	if rec := page(t, h, "/pay-yourself/p60?name=Nobody&year=2026"); !strings.Contains(rec, "404") && !strings.Contains(rec, "not found") {
		t.Error("a P60 for an unknown employee was produced")
	}

	// The runs survive a reload.
	b, err := newApp(a.dataPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.runs) != 3 || b.runs[0].Result.StudentLoan.String() != "GBP 55.35" || b.lastPayroll() == nil {
		t.Errorf("reloaded runs = %+v", b.runs)
	}
}

func TestQuickSalaryRunIsRecorded(t *testing.T) {
	a, err := newApp("")
	if err != nil {
		t.Fatal(err)
	}
	h := a.routes()
	drive(t, h, "/pay-yourself/salary/run", url.Values{"amount": {"12570.00"}, "date": {"2026-06-01"}, "pension": {"1"}})
	if len(a.runs) != 1 || a.runs[0].Employee != "Alex Director" || a.runs[0].Result.RateTable != "2026/27 (England, Wales & NI)" {
		t.Fatalf("runs = %+v", a.runs)
	}
	body := page(t, h, "/pay-yourself")
	for _, s := range []string{"today that is 2026/27", "£6,240.00–£50,270.00", "Payslip", "P60"} {
		if !strings.Contains(body, s) {
			t.Errorf("salary page lacks %q", s)
		}
	}
}
