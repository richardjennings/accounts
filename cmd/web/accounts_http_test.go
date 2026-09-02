package main

import (
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/richardjennings/accounts/chart"
	"github.com/richardjennings/accounts/ledger"
)

func TestAccountsApprovalAndComparatives(t *testing.T) {
	a, err := newApp(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	h := a.persistMiddleware(a.routes())
	journal := func(date, debit, credit, amount string) {
		drive(t, h, "/accounting/journals/post", url.Values{"date": {date}, "debit": {debit}, "credit": {credit}, "amount": {amount}, "narrative": {"test"}})
	}
	journal("2026-05-01", chart.Bank, chart.Sales, "2000.00") // first year: 1 April 2026 to 31 March 2027

	body := page(t, h, "/accounting/accounts")
	for _, s := range []string{"Not yet approved", "First year, so no comparative column", "£2,000.00", `name="employees" value="1"`} {
		if !strings.Contains(body, s) {
			t.Errorf("accounts page lacks %q", s)
		}
	}
	doc := page(t, h, "/accounting/accounts/ixbrl")
	if !strings.Contains(doc, "DRAFT") || !strings.Contains(doc, `unitRef="pure" decimals="0">1<`) {
		t.Error("unapproved accounts are not a draft with the headcount as the employee note")
	}

	drive(t, h, "/accounting/accounts/approve", url.Values{"date": {"2027-06-15"}, "signer": {"Alex Director"}, "employees": {"2"}})
	body = page(t, h, "/accounting/accounts")
	if !strings.Contains(body, "Approved on <b>2027-06-15</b>") || !strings.Contains(body, "signed by <b>Alex Director</b>") {
		t.Error("accounts page does not show the approval")
	}
	doc = page(t, h, "/accounting/accounts/ixbrl")
	for _, s := range []string{
		`name="uk-core:DateAuthorisationFinancialStatementsForIssue" contextRef="period-end">2027-06-15<`,
		`name="uk-bus:NameEntityOfficer" contextRef="period-end">Alex Director<`,
		`name="uk-core:AverageNumberEmployeesDuringPeriod" contextRef="period" unitRef="pure" decimals="0">2<`,
	} {
		if !strings.Contains(doc, s) {
			t.Errorf("iXBRL lacks %q", s)
		}
	}
	if strings.Contains(doc, "DRAFT") {
		t.Error("approved accounts are still a draft")
	}

	// The approval survives a reload and belongs to its year only.
	b, err := newApp(a.dataPath)
	if err != nil {
		t.Fatal(err)
	}
	if ap := b.approval(1); ap == nil || ap.On != ledger.NewDate(2027, time.June, 15) || ap.AverageEmployees != 2 {
		t.Errorf("reloaded approval = %+v", ap)
	}

	// The second year shows the first beside it and is a draft until approved.
	drive(t, h, "/company/date", url.Values{"date": {"2027-06-01"}})
	journal("2027-05-01", chart.Bank, chart.Sales, "5000.00")
	body = page(t, h, "/accounting/accounts")
	for _, s := range []string{"The second column is the comparative: the year to 2027-03-31", "£5,000.00", "£2,000.00", "Not yet approved"} {
		if !strings.Contains(body, s) {
			t.Errorf("second-year accounts page lacks %q", s)
		}
	}
	if !strings.Contains(page(t, h, "/accounting/accounts/ixbrl"), "DRAFT") {
		t.Error("second-year accounts inherited the first year's approval")
	}
}
