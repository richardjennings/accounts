package capitalallowances

import (
	"testing"

	"github.com/richardjennings/accounts/tax/corporationtax"
)

func TestAIACoversAdditions(t *testing.T) {
	r, err := Compute(Input{MainAdditions: gbp("20000.00")})
	if err != nil {
		t.Fatal(err)
	}
	if r.AIA.String() != "GBP 20000.00" || r.TotalAllowance.String() != "GBP 20000.00" {
		t.Fatalf("AIA %s total %s, want both GBP 20000.00", r.AIA, r.TotalAllowance)
	}
	if r.CarriedForward.Main.String() != "GBP 0.00" {
		t.Errorf("main pool c/f = %s, want GBP 0.00", r.CarriedForward.Main)
	}
}

func TestMainPoolWritingDown(t *testing.T) {
	// No additions, £10,000 brought forward: 18% WDA, 82% carried forward.
	r, err := Compute(Input{BroughtForward: Pools{Main: gbp("10000.00")}})
	if err != nil {
		t.Fatal(err)
	}
	if r.MainWDA.String() != "GBP 1800.00" {
		t.Errorf("main WDA = %s, want GBP 1800.00", r.MainWDA)
	}
	if r.CarriedForward.Main.String() != "GBP 8200.00" {
		t.Errorf("main c/f = %s, want GBP 8200.00", r.CarriedForward.Main)
	}
}

func TestSpecialRatePoolWritingDown(t *testing.T) {
	r, _ := Compute(Input{BroughtForward: Pools{Special: gbp("10000.00")}})
	if r.SpecialWDA.String() != "GBP 600.00" { // 6%
		t.Errorf("special WDA = %s, want GBP 600.00", r.SpecialWDA)
	}
}

func TestAIACappedExcessToPool(t *testing.T) {
	// £1.2m of additions: £1m AIA now, £200k into the main pool at 18%.
	r, err := Compute(Input{MainAdditions: gbp("1200000.00")})
	if err != nil {
		t.Fatal(err)
	}
	if r.AIA.String() != "GBP 1000000.00" {
		t.Errorf("AIA = %s, want GBP 1000000.00", r.AIA)
	}
	if r.MainWDA.String() != "GBP 36000.00" { // 18% × 200,000
		t.Errorf("main WDA = %s, want GBP 36000.00", r.MainWDA)
	}
	if r.TotalAllowance.String() != "GBP 1036000.00" {
		t.Errorf("total = %s, want GBP 1036000.00", r.TotalAllowance)
	}
	if r.CarriedForward.Main.String() != "GBP 164000.00" {
		t.Errorf("c/f = %s, want GBP 164000.00", r.CarriedForward.Main)
	}
}

func TestSmallPoolWriteOff(t *testing.T) {
	// £800 brought forward is a small pool: write it all off, nothing carried forward.
	r, _ := Compute(Input{BroughtForward: Pools{Main: gbp("800.00")}})
	if r.SmallPoolAllowance.String() != "GBP 800.00" || !r.MainWDA.IsZero() {
		t.Fatalf("small pool %s, main WDA %s", r.SmallPoolAllowance, r.MainWDA)
	}
	if r.CarriedForward.Main.String() != "GBP 0.00" {
		t.Errorf("c/f = %s, want GBP 0.00", r.CarriedForward.Main)
	}
}

// TestClosesTheCorporationTaxLoop takes accounting profit, adds back depreciation,
// deducts computed capital allowances, and feeds the result to the CT calculator.
func TestClosesTheCorporationTaxLoop(t *testing.T) {
	allowances, err := Compute(Input{MainAdditions: gbp("20000.00")})
	if err != nil {
		t.Fatal(err)
	}
	// £50,000 accounting profit before tax, £5,000 depreciation added back,
	// £20,000 capital allowances deducted → £35,000 taxable.
	taxable, err := corporationtax.AdjustProfit(gbp("50000.00"), gbp("5000.00"), allowances.TotalAllowance)
	if err != nil {
		t.Fatal(err)
	}
	if taxable.String() != "GBP 35000.00" {
		t.Fatalf("taxable profit = %s, want GBP 35000.00", taxable)
	}
	ct, err := corporationtax.Compute(corporationtax.Input{FinancialYear: 2025, TaxableProfit: taxable})
	if err != nil {
		t.Fatal(err)
	}
	if ct.Charge.String() != "GBP 6650.00" { // 19% × 35,000
		t.Fatalf("CT charge = %s, want GBP 6650.00", ct.Charge)
	}
}

func TestConfigurableRates(t *testing.T) {
	rt := Standard
	rt.Name = "custom"
	rt.MainRate = dec("0.25") // pretend a 25% main pool
	r, _ := Compute(Input{BroughtForward: Pools{Main: gbp("10000.00")}, Rates: rt})
	if r.MainWDA.String() != "GBP 2500.00" {
		t.Errorf("main WDA at 25%% = %s, want GBP 2500.00", r.MainWDA)
	}
}
