package main

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/richardjennings/accounts/ledger"
)

func TestConfirmationStatementDocument(t *testing.T) {
	a, err := newApp("")
	if err != nil {
		t.Fatal(err)
	}
	h := a.routes()
	drive(t, h, "/company/details", realCompany("2026-04-25"))
	drive(t, h, "/company/date", url.Values{"date": {"2026-09-02"}})

	body := page(t, h, "/company")
	for _, s := range []string{"Last made up to", "2026-04-25", "Next statement date", "2027-04-25", "Deliver by", "2027-05-09"} {
		if !strings.Contains(body, s) {
			t.Errorf("company page lacks %q", s)
		}
	}

	doc := page(t, h, "/company/confirmation-statement")
	for _, s := range []string{
		"CONFIRMATION STATEMENT", "Made up to <b>2027-04-25</b>", "Jennings Technology Limited", "14829707", "mail@example.com", "62020",
		"Alex Director", "Ownership of shares – 75% or more", "Right to appoint or remove directors",
		"Ordinary", "£100.00", "intended future activities of the company are lawful",
		"Identity verification is not recorded for: Alex Director, Alex Director",
	} {
		if !strings.Contains(doc, s) {
			t.Errorf("statement lacks %q", s)
		}
	}

	// Once the director and the PSC are verified the statement says so.
	drive(t, h, "/company/officers/update", url.Values{"i": {"0"}, "verified": {"2025-11-20"}})
	drive(t, h, "/company/pscs/update", url.Values{"i": {"0"}, "shares": {"3"}, "voting": {"3"}, "appoints": {"1"}, "verified": {"2025-11-20"}})
	doc = page(t, h, "/company/confirmation-statement?date=2026-04-25")
	if !strings.Contains(doc, "Made up to <b>2026-04-25</b>") || !strings.Contains(doc, "has verified their identity") || strings.Contains(doc, "Not verified") {
		t.Error("statement for a chosen date with everyone verified is wrong")
	}

	// Recording the statement as made starts the next review period.
	drive(t, h, "/company/statement/made", url.Values{"date": {""}})
	if a.co.LastStatementDate != ledger.NewDate(2026, time.April, 25) {
		t.Error("an empty date changed the last statement date")
	}
	drive(t, h, "/company/statement/made", url.Values{"date": {"2027-04-25"}})
	if a.co.LastStatementDate != ledger.NewDate(2027, time.April, 25) {
		t.Errorf("last statement date = %s, want 2027-04-25", a.co.LastStatementDate)
	}
	body = page(t, h, "/company")
	if !strings.Contains(body, "2028-05-09") || !strings.Contains(body, "made up to 2028-04-25") {
		t.Error("the key dates did not move to the next review period")
	}
}
