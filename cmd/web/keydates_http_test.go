package main

import (
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/richardjennings/accounts/ledger"
)

// realCompany is the form for a company incorporated 26 April 2023 with a 30 April
// year end, whose last confirmation statement was made up to 25 April 2026.
func realCompany(lastStatement string) url.Values {
	return url.Values{
		"name": {"Jennings Technology Limited"}, "number": {"14829707"}, "sic": {"62020"},
		"office": {"3rd Floor, 86 - 90 Paul Street, London, EC2A 4NE"}, "email": {"mail@example.com"},
		"incorporated": {"2023-04-26"}, "yearend": {"2026-04-30"}, "laststatement": {lastStatement}, "vatquarter": {"0"},
	}
}

func TestKeyDatesOnCompanyAndOverview(t *testing.T) {
	a, err := newApp(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	h := a.persistMiddleware(a.routes())
	drive(t, h, "/company/details", realCompany("2026-04-25"))
	drive(t, h, "/company/date", url.Values{"date": {"2026-09-02"}})

	body := page(t, h, "/company")
	for _, s := range []string{
		"mail@example.com", `value="2026-04-25"`,
		"2027-01-31", "Annual accounts", "for the year to 2026-04-30",
		"2027-02-01", "Corporation tax payment",
		"2027-04-30", "Company tax return (CT600)",
		"2027-05-09", "Confirmation statement", "made up to 2027-04-25",
	} {
		if !strings.Contains(body, s) {
			t.Errorf("company page lacks %q", s)
		}
	}
	for _, s := range []string{"overdue", "VAT return and payment", "PAYE and NIC payment"} {
		if strings.Contains(body, s) {
			t.Errorf("company page shows %q", s)
		}
	}

	overview := page(t, h, "/")
	for _, s := range []string{"Next due", "2027-01-31", "2027-02-01", "2027-04-30"} {
		if !strings.Contains(overview, s) {
			t.Errorf("overview lacks %q", s)
		}
	}
	if strings.Contains(overview, "2027-05-09") {
		t.Error("overview shows more than the next three key dates")
	}

	// The new fields survive a save and reload.
	b, err := newApp(a.dataPath)
	if err != nil {
		t.Fatal(err)
	}
	if b.co.LastStatementDate != ledger.NewDate(2026, time.April, 25) || b.co.RegisteredEmail != "mail@example.com" {
		t.Errorf("reloaded company = %+v", b.co)
	}
}

func TestOverdueStatementIsFlagged(t *testing.T) {
	a, err := newApp("")
	if err != nil {
		t.Fatal(err)
	}
	h := a.routes()
	drive(t, h, "/company/details", realCompany("2025-04-25"))
	drive(t, h, "/company/date", url.Values{"date": {"2026-09-02"}})
	body := page(t, h, "/company")
	if !strings.Contains(body, "overdue") || !strings.Contains(body, "2026-05-09") {
		t.Error("a statement due 2026-05-09 is not shown as overdue on 2026-09-02")
	}
}

func TestVATAndPayrollKeyDates(t *testing.T) {
	a, err := newApp("")
	if err != nil {
		t.Fatal(err)
	}
	h := a.routes()
	form := realCompany("2026-04-25")
	form.Set("vatreg", "1")
	form.Set("vatquarter", "3")
	drive(t, h, "/company/details", form)
	drive(t, h, "/company/date", url.Values{"date": {"2026-09-02"}})
	drive(t, h, "/pay-yourself/salary/run", url.Values{"amount": {"12570.00"}, "date": {"2026-09-01"}})
	body := page(t, h, "/company")
	for _, s := range []string{"VAT return and payment", "for the quarter to 2026-09-30", "2026-11-07", "PAYE and NIC payment", "2026-09-22", "P60 to each employee"} {
		if !strings.Contains(body, s) {
			t.Errorf("company page lacks %q", s)
		}
	}
}
