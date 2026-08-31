package corporationtax

import (
	"testing"
	"time"

	"github.com/richardjennings/accounts/chart"
	"github.com/richardjennings/accounts/ledger"
	"github.com/richardjennings/accounts/money"
	"github.com/richardjennings/accounts/themes/companytax"
)

func TestSmallProfitsRate(t *testing.T) {
	res, err := Compute(Input{FinancialYear: 2025, TaxableProfit: gbp("30000.00")})
	if err != nil {
		t.Fatal(err)
	}
	if res.Band != "small profits" || res.Charge.String() != "GBP 5700.00" {
		t.Fatalf("got band %q charge %s, want small profits / GBP 5700.00", res.Band, res.Charge)
	}
}

func TestMainRate(t *testing.T) {
	res, err := Compute(Input{FinancialYear: 2025, TaxableProfit: gbp("300000.00")})
	if err != nil {
		t.Fatal(err)
	}
	if res.Band != "main rate" || res.Charge.String() != "GBP 75000.00" {
		t.Fatalf("got band %q charge %s, want main rate / GBP 75000.00", res.Band, res.Charge)
	}
	if res.EffectiveRate.String() != "0.25" {
		t.Errorf("effective rate = %s, want 0.25", res.EffectiveRate)
	}
}

// TestMarginalMatchesHMRC reproduces HMRC's own worked example (CTM03925): £90,000
// taxable profit plus £8,000 exempt distributions, giving £20,406 tax to the pound.
func TestMarginalMatchesHMRC(t *testing.T) {
	res, err := Compute(Input{
		FinancialYear:       2025,
		TaxableProfit:       gbp("90000.00"),
		ExemptDistributions: gbp("8000.00"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Band != "marginal" {
		t.Fatalf("band = %q, want marginal", res.Band)
	}
	if res.MarginalRelief.String() != "GBP 2093.88" {
		t.Errorf("marginal relief = %s, want GBP 2093.88", res.MarginalRelief)
	}
	if res.Charge.String() != "GBP 20406.12" { // HMRC rounds to £20,406 for illustration
		t.Errorf("charge = %s, want GBP 20406.12", res.Charge)
	}
}

func TestAssociatedCompaniesShrinkLimits(t *testing.T) {
	// One associated company halves the limits to £25k/£125k, so £60k is marginal.
	res, err := Compute(Input{FinancialYear: 2025, TaxableProfit: gbp("60000.00"), AssociatedCompanies: 1})
	if err != nil {
		t.Fatal(err)
	}
	if res.Band != "marginal" || res.Charge.String() != "GBP 14025.00" {
		t.Fatalf("got band %q charge %s, want marginal / GBP 14025.00", res.Band, res.Charge)
	}
}

func TestLossBearsNoTax(t *testing.T) {
	res, err := Compute(Input{FinancialYear: 2025, TaxableProfit: gbp("-5000.00")})
	if err != nil {
		t.Fatal(err)
	}
	if res.Band != "none" || !res.Charge.IsZero() {
		t.Fatalf("got band %q charge %s, want none / zero", res.Band, res.Charge)
	}
}

func TestUnknownYearErrors(t *testing.T) {
	if _, err := Compute(Input{FinancialYear: 1999, TaxableProfit: gbp("10000.00")}); err == nil {
		t.Fatal("expected an error for an unknown financial year")
	}
}

// TestComputeThenPost shows the calculator feeding the theme: compute the charge,
// then post it with companytax.Provision.
func TestComputeThenPost(t *testing.T) {
	res, err := Compute(Input{FinancialYear: 2025, TaxableProfit: gbp("90000.00"), ExemptDistributions: gbp("8000.00")})
	if err != nil {
		t.Fatal(err)
	}
	book, err := chart.NewUKMicroLtdBook(money.GBP)
	if err != nil {
		t.Fatal(err)
	}
	j, err := companytax.Provision{Date: ledger.NewDate(2026, time.March, 31), Ref: "CT-2025", Amount: res.Charge}.Journal()
	if err != nil {
		t.Fatal(err)
	}
	if err := book.Post(j); err != nil {
		t.Fatal(err)
	}
	bal, _ := book.Balance(chart.CorpTaxPayable)
	if bal.String() != "GBP 20406.12" {
		t.Fatalf("corporation tax payable = %s, want GBP 20406.12", bal)
	}
}
