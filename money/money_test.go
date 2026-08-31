package money

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/richardjennings/decimal"
)

func sumAll(t *testing.T, cur Currency, parts []Money) Money {
	t.Helper()
	sum := Zero(cur)
	for _, p := range parts {
		var err error
		if sum, err = sum.Add(p); err != nil {
			t.Fatal(err)
		}
	}
	return sum
}

func TestAddSubExact(t *testing.T) {
	a := MustParse(GBP, "12.34")
	b := MustParse(GBP, "0.66")
	if got, err := a.Add(b); err != nil || got.String() != "GBP 13.00" {
		t.Fatalf("add: %v %s", err, got)
	}
	if got, _ := a.Sub(b); got.String() != "GBP 11.68" {
		t.Fatalf("sub: %s", got)
	}
}

func TestMixedCurrency(t *testing.T) {
	_, err := MustParse(GBP, "1.00").Add(MustParse(USD, "1.00"))
	if !errors.Is(err, ErrMixedCurrency) {
		t.Fatalf("want ErrMixedCurrency, got %v", err)
	}
}

func TestMulDivRoundToScale(t *testing.T) {
	price := MustParse(GBP, "100.00")
	part, _ := price.Mul(decimal.MustParse("0.20"), HalfUp) // 20% of 100.00
	if part.String() != "GBP 20.00" {
		t.Fatalf("mul: %s", part)
	}
	sum, _ := price.Add(part)
	if sum.String() != "GBP 120.00" {
		t.Fatalf("add: %s", sum)
	}
	back, _ := sum.Div(decimal.MustParse("1.20"), HalfUp)
	if back.String() != "GBP 100.00" {
		t.Fatalf("div: %s", back)
	}
}

// TestRoundingHalfUp pins the UK convention: ties round away from zero for both
// signs, distinct from the decimal engine's half-even default.
func TestRoundingHalfUp(t *testing.T) {
	f := decimal.MustParse("1.005") // forces an exact half at the third place
	pos := MustParse(GBP, "1.00")
	neg := MustParse(GBP, "-1.00")

	if up, _ := pos.Mul(f, HalfUp); up.String() != "GBP 1.01" {
		t.Fatalf("half-up +: %s", up)
	}
	if ev, _ := pos.Mul(f, HalfEven); ev.String() != "GBP 1.00" {
		t.Fatalf("half-even +: %s", ev)
	}
	if nup, _ := neg.Mul(f, HalfUp); nup.String() != "GBP -1.01" {
		t.Fatalf("half-up - (should round away from zero): %s", nup)
	}
	if nev, _ := neg.Mul(f, HalfEven); nev.String() != "GBP -1.00" {
		t.Fatalf("half-even -: %s", nev)
	}
}

func TestAllocateSumsBack(t *testing.T) {
	total := MustParse(GBP, "100.00")
	parts, err := total.Split(3)
	if err != nil {
		t.Fatal(err)
	}
	if got := sumAll(t, GBP, parts); !got.Equal(total) {
		t.Fatalf("sum %s != %s", got, total)
	}
	if parts[0].String() != "GBP 33.34" || parts[1].String() != "GBP 33.33" || parts[2].String() != "GBP 33.33" {
		t.Fatalf("parts: %s %s %s", parts[0], parts[1], parts[2])
	}
}

func TestAllocateWeighted(t *testing.T) {
	total := MustParse(GBP, "0.05")
	parts, err := total.Allocate(70, 20, 10)
	if err != nil {
		t.Fatal(err)
	}
	if got := sumAll(t, GBP, parts); !got.Equal(total) {
		t.Fatalf("weighted sum %s != %s", got, total)
	}
	if parts[0].String() != "GBP 0.04" || parts[1].String() != "GBP 0.01" || parts[2].String() != "GBP 0.00" {
		t.Fatalf("weighted parts: %s %s %s", parts[0], parts[1], parts[2])
	}
}

func TestAllocateNegative(t *testing.T) {
	total := MustParse(GBP, "-100.00")
	parts, err := total.Split(3)
	if err != nil {
		t.Fatal(err)
	}
	if got := sumAll(t, GBP, parts); !got.Equal(total) {
		t.Fatalf("neg sum %s != %s", got, total)
	}
}

func TestParseInexactRejected(t *testing.T) {
	if _, err := Parse(GBP, "1.234"); !errors.Is(err, ErrInexact) {
		t.Fatalf("GBP 1.234 want ErrInexact, got %v", err)
	}
	if _, err := Parse(JPY, "1.5"); !errors.Is(err, ErrInexact) {
		t.Fatalf("JPY 1.5 want ErrInexact, got %v", err)
	}
	if m := MustParse(GBP, "12"); m.String() != "GBP 12.00" {
		t.Fatalf("whole parse: %s", m)
	}
	if m := MustParse(JPY, "1500"); m.String() != "JPY 1500" {
		t.Fatalf("jpy parse: %s", m)
	}
}

func TestDivByZero(t *testing.T) {
	if _, err := MustParse(GBP, "1.00").Div(decimal.MustParse("0"), HalfUp); !errors.Is(err, ErrDivByZero) {
		t.Fatalf("want ErrDivByZero, got %v", err)
	}
}

func TestJSONRoundTrip(t *testing.T) {
	m := MustParse(GBP, "-7.50")
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"currency":"GBP","amount":"-7.50"}` {
		t.Fatalf("json: %s", b)
	}
	var back Money
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if !back.Equal(m) {
		t.Fatalf("round trip: %s != %s", back, m)
	}
}

func TestJSONZeroValue(t *testing.T) {
	b, err := json.Marshal(Money{})
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "null" {
		t.Fatalf("zero Money json = %s, want null", b)
	}
	back := MustParse(GBP, "1.00")
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.Currency().Code != "" || !back.IsZero() {
		t.Fatalf("null did not decode as the zero Money: %+v", back)
	}
	if err := json.Unmarshal([]byte(`{"currency":"GBP"}`), &back); err == nil {
		t.Fatal("missing amount accepted")
	}
}

func TestMinorUnits(t *testing.T) {
	m := MustParse(GBP, "12.34")
	if v, ok := m.MinorUnits(); !ok || v != 1234 {
		t.Fatalf("minor units: %d %v", v, ok)
	}
	if FromMinorUnits(GBP, 1234).String() != "GBP 12.34" {
		t.Fatal("FromMinorUnits mismatch")
	}
}
