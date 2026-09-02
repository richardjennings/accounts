package main

import (
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/richardjennings/accounts/ledger"
	"github.com/richardjennings/accounts/register"
)

func TestOfficerDetailsAndIdentity(t *testing.T) {
	a, err := newApp(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	h := a.persistMiddleware(a.routes())

	body := page(t, h, "/company/people")
	if !strings.Contains(body, "not verified") {
		t.Error("a fresh director is not flagged as unverified")
	}
	drive(t, h, "/company/officers/update", url.Values{
		"i": {"0"}, "address": {"1 Example Street, London"}, "dob": {"1980-06-15"},
		"nationality": {"British"}, "occupation": {"Software engineer"}, "verified": {"2025-11-20"},
	})
	o := a.reg.Officers[0]
	if o.ServiceAddress != "1 Example Street, London" || o.DateOfBirth != ledger.NewDate(1980, time.June, 15) || o.Occupation != "Software engineer" {
		t.Errorf("officer = %+v", o)
	}
	if !o.IdentityVerified() || o.IdentityVerifiedOn != ledger.NewDate(2025, time.November, 20) || !o.InOffice() {
		t.Errorf("officer identity/office = %+v", o)
	}
	body = page(t, h, "/company/people")
	for _, s := range []string{"Verified 2025-11-20", "Software engineer", `value="1980-06-15"`} {
		if !strings.Contains(body, s) {
			t.Errorf("people page lacks %q", s)
		}
	}
	if n := strings.Count(body, "not verified"); n != 1 { // only the PSC row is left unverified
		t.Errorf("people page flags %d unverified identities, want 1", n)
	}

	// Resigning through the same form leaves the record in place.
	drive(t, h, "/company/officers/update", url.Values{
		"i": {"0"}, "address": {"1 Example Street, London"}, "dob": {"1980-06-15"},
		"nationality": {"British"}, "occupation": {"Software engineer"}, "verified": {"2025-11-20"}, "resigned": {"2026-09-01"},
	})
	if a.reg.Officers[0].InOffice() {
		t.Error("officer still in office after resignation")
	}
	if len(a.reg.Directors()) != 0 {
		t.Error("a resigned director is still counted as a director")
	}

	b, err := newApp(a.dataPath)
	if err != nil {
		t.Fatal(err)
	}
	if b.reg.Officers[0].IdentityVerifiedOn != ledger.NewDate(2025, time.November, 20) || b.reg.Officers[0].ServiceAddress == "" {
		t.Errorf("reloaded officer = %+v", b.reg.Officers[0])
	}
}

func TestPSCRegister(t *testing.T) {
	a, err := newApp(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	h := a.persistMiddleware(a.routes())

	// The starter company's sole shareholder is its one PSC.
	if got := a.reg.CurrentPSCs(); len(got) != 1 || got[0].Shares != register.AtLeast75 {
		t.Fatalf("default PSCs = %+v", got)
	}
	body := page(t, h, "/company/people")
	for _, s := range []string{"people with significant control", "Ownership of shares – 75% or more", "Right to appoint or remove directors"} {
		if !strings.Contains(body, s) {
			t.Errorf("people page lacks %q", s)
		}
	}

	// A PSC needs a nature of control.
	drive(t, h, "/company/pscs/add", url.Values{"name": {"Jo Investor"}, "date": {"2026-07-01"}})
	if len(a.reg.PSCs) != 1 {
		t.Fatal("a PSC with no nature of control was registered")
	}
	drive(t, h, "/company/pscs/add", url.Values{"name": {"Jo Investor"}, "date": {"2026-07-01"}, "shares": {"1"}, "voting": {"1"}})
	if len(a.reg.PSCs) != 2 || a.reg.PSCs[1].Shares != register.Over25 || a.reg.PSCs[1].Notified != ledger.NewDate(2026, time.July, 1) {
		t.Fatalf("PSCs after add = %+v", a.reg.PSCs)
	}

	drive(t, h, "/company/pscs/update", url.Values{"i": {"0"}, "shares": {"3"}, "voting": {"3"}, "appoints": {"1"}, "verified": {"2025-12-01"}})
	drive(t, h, "/company/pscs/update", url.Values{"i": {"1"}, "shares": {"1"}, "voting": {"1"}, "ceased": {"2026-08-01"}})
	if !a.reg.PSCs[0].IdentityVerified() || a.reg.PSCs[1].Current() {
		t.Errorf("PSCs after update = %+v", a.reg.PSCs)
	}
	if got := a.reg.CurrentPSCs(); len(got) != 1 {
		t.Errorf("current PSCs = %+v, want one", got)
	}
	body = page(t, h, "/company/people")
	for _, s := range []string{"Verified 2025-12-01", "Ceased 2026-08-01", "more than 25% but not more than 50%"} {
		if !strings.Contains(body, s) {
			t.Errorf("people page lacks %q", s)
		}
	}

	b, err := newApp(a.dataPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.reg.PSCs) != 2 || b.reg.PSCs[0].IdentityVerifiedOn != ledger.NewDate(2025, time.December, 1) {
		t.Errorf("reloaded PSCs = %+v", b.reg.PSCs)
	}
}
