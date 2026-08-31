package vat

import (
	"testing"

	"github.com/richardjennings/accounts/money"
)

func gbp(s string) money.Money { return money.MustParse(money.GBP, s) }

func TestRates(t *testing.T) {
	cases := []struct {
		rate Rate
		net  string
		want string
	}{
		{Standard, "100.00", "GBP 20.00"},
		{Reduced, "100.00", "GBP 5.00"},
		{Zero, "100.00", "GBP 0.00"},
		{None, "100.00", "GBP 0.00"},
		{Standard, "12.34", "GBP 2.47"}, // 2.468 rounds half-up to 2.47
	}
	for _, c := range cases {
		if got := c.rate.On(gbp(c.net)); got.String() != c.want {
			t.Errorf("%s on %s = %s, want %s", c.rate.Code, c.net, got, c.want)
		}
	}
}

func TestByCode(t *testing.T) {
	if ByCode("standard").Code != "standard" {
		t.Error("standard lookup failed")
	}
	if ByCode("nonsense").Code != "none" {
		t.Error("unknown code should default to none")
	}
}
