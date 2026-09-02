package register

import (
	"testing"
	"time"

	"github.com/richardjennings/accounts/chart"
	"github.com/richardjennings/accounts/ledger"
	"github.com/richardjennings/accounts/money"
	"github.com/richardjennings/accounts/themes/capital"
)

func gbp(s string) money.Money { return money.MustParse(money.GBP, s) }
func date(d int) ledger.Date   { return ledger.NewDate(2026, time.April, d) }

func sample() Register {
	return Register{
		Officers: []Officer{
			{Name: "Alex Director", Role: Director, Appointed: date(1)},
			{Name: "Sam Secretary", Role: Secretary, Appointed: date(1)},
			{Name: "Pat Past", Role: Director, Appointed: date(1), Resigned: date(2)},
		},
		Members: []Member{
			{Name: "Alex Director", Class: "Ordinary", Shares: 60, Since: date(1)},
			{Name: "Jo Investor", Class: "Ordinary", Shares: 40, Since: date(1)},
		},
		Nominal: gbp("1.00"),
	}
}

func TestIssuedCapitalAndDirectors(t *testing.T) {
	r := sample()
	if r.TotalShares() != 100 {
		t.Errorf("total shares = %d, want 100", r.TotalShares())
	}
	if got := r.IssuedCapital().String(); got != "GBP 100.00" {
		t.Errorf("issued capital = %s, want GBP 100.00", got)
	}
	if d := r.Directors(); len(d) != 1 || d[0].Name != "Alex Director" {
		t.Errorf("directors = %+v, want just the in-office Alex Director", d)
	}
}

func TestDividendAllocatedByHolding(t *testing.T) {
	r := sample()
	awards, err := r.AllocateDividend(gbp("1000.00"))
	if err != nil {
		t.Fatal(err)
	}
	if len(awards) != 2 {
		t.Fatalf("awards = %d, want 2", len(awards))
	}
	if awards[0].Amount.String() != "GBP 600.00" { // 60 shares
		t.Errorf("Alex award = %s, want GBP 600.00", awards[0].Amount)
	}
	if awards[1].Amount.String() != "GBP 400.00" { // 40 shares
		t.Errorf("Jo award = %s, want GBP 400.00", awards[1].Amount)
	}
}

// TestDividendAllocationIsExact: an amount that does not divide evenly is still
// distributed to the penny, summing back to the total.
func TestDividendAllocationIsExact(t *testing.T) {
	r := Register{
		Members: []Member{
			{Name: "A", Shares: 1}, {Name: "B", Shares: 1}, {Name: "C", Shares: 1},
		},
		Nominal: gbp("1.00"),
	}
	awards, err := r.AllocateDividend(gbp("10.00"))
	if err != nil {
		t.Fatal(err)
	}
	sum := money.Zero(money.GBP)
	for _, a := range awards {
		sum, _ = sum.Add(a.Amount)
	}
	if sum.String() != "GBP 10.00" {
		t.Errorf("allocated sum = %s, want GBP 10.00", sum)
	}
	// 1000 pence / 3 = 333 each, one penny left over → 3.34, 3.33, 3.33
	if awards[0].Amount.String() != "GBP 3.34" {
		t.Errorf("first award = %s, want GBP 3.34 (largest remainder)", awards[0].Amount)
	}
}

func TestNoSharesNoDividend(t *testing.T) {
	r := Register{Nominal: gbp("1.00")}
	if _, err := r.AllocateDividend(gbp("100.00")); err == nil {
		t.Error("expected an error allocating a dividend with no shares in issue")
	}
}

// TestIssueSharesPostsToLedger runs the capital theme: issuing 100 £1 shares brings
// £100 into the bank and raises share capital by £100.
func TestIssueSharesPostsToLedger(t *testing.T) {
	book, err := chart.NewUKMicroLtdBook(money.GBP)
	if err != nil {
		t.Fatal(err)
	}
	r := sample()
	j, err := capital.IssueShares{Date: date(1), Ref: "SC-1", Amount: r.IssuedCapital()}.Journal()
	if err != nil {
		t.Fatal(err)
	}
	if err := book.Post(j); err != nil {
		t.Fatal(err)
	}
	bank, _ := book.Balance(chart.Bank)
	if bank.String() != "GBP 100.00" {
		t.Errorf("bank = %s, want GBP 100.00", bank)
	}
	sc, _ := book.Balance(chart.ShareCapital)
	if sc.String() != "GBP 100.00" {
		t.Errorf("share capital = %s, want GBP 100.00", sc)
	}
}

func TestBandForShares(t *testing.T) {
	cases := []struct {
		held, total int
		want        ControlBand
	}{
		{25, 100, NoControl}, {26, 100, Over25}, {50, 100, Over25}, {51, 100, Over50},
		{74, 100, Over50}, {75, 100, AtLeast75}, {100, 100, AtLeast75}, {1, 0, NoControl},
	}
	for _, c := range cases {
		if got := BandForShares(c.held, c.total); got != c.want {
			t.Errorf("BandForShares(%d, %d) = %s, want %s", c.held, c.total, got, c.want)
		}
	}
}

func TestPSCNatureOfControl(t *testing.T) {
	p := PSC{Name: "Alex Director", Notified: date(1), Shares: AtLeast75, Voting: AtLeast75, AppointsDirectors: true}
	got := p.NatureOfControl()
	want := []string{
		"Ownership of shares – 75% or more",
		"Ownership of voting rights – 75% or more",
		"Right to appoint or remove directors",
	}
	if len(got) != len(want) {
		t.Fatalf("nature of control = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("statement %d = %q, want %q", i, got[i], want[i])
		}
	}
	if p.IdentityVerified() {
		t.Error("a PSC with no verification date is shown as verified")
	}
	p.IdentityVerifiedOn = date(3)
	if !p.IdentityVerified() {
		t.Error("a PSC with a verification date is not shown as verified")
	}
}

func TestCurrentPSCs(t *testing.T) {
	r := Register{PSCs: []PSC{
		{Name: "Alex Director", Notified: date(1)},
		{Name: "Pat Past", Notified: date(1), Ceased: date(2)},
	}}
	if got := r.CurrentPSCs(); len(got) != 1 || got[0].Name != "Alex Director" {
		t.Errorf("current PSCs = %+v, want just Alex Director", got)
	}
}

func TestOfficerIdentityVerified(t *testing.T) {
	o := Officer{Name: "Alex Director", Role: Director, Appointed: date(1)}
	if o.IdentityVerified() {
		t.Error("an officer with no verification date is shown as verified")
	}
	o.IdentityVerifiedOn = date(5)
	if !o.IdentityVerified() {
		t.Error("an officer with a verification date is not shown as verified")
	}
}
