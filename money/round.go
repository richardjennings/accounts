package money

import (
	"math/big"
	"sort"
	"strconv"

	"github.com/richardjennings/decimal"
)

// decimalFromMinor builds the decimal minor × 10^-scale with its exponent exactly
// -scale, preserving the Money invariant. minor.Text is an integer literal, so the
// scientific form parses without rounding.
func decimalFromMinor(minor *big.Int, scale int32) decimal.Decimal {
	return decimal.MustParse(minor.Text(10) + "E-" + strconv.FormatInt(int64(scale), 10))
}

// roundRatToScale returns round(r × 10^scale) as an integer count of minor units,
// rounding the exact rational exactly once under mode.
func roundRatToScale(r *big.Rat, scale int32, mode decimal.RoundingMode) *big.Int {
	pow := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(scale)), nil)
	scaled := new(big.Rat).Mul(r, new(big.Rat).SetInt(pow))
	return roundRat(scaled, mode)
}

// roundRat rounds an exact rational to the nearest integer under mode, using the
// same tie semantics as the decimal engine's rounding.
func roundRat(x *big.Rat, mode decimal.RoundingMode) *big.Int {
	neg := x.Sign() < 0
	num := new(big.Int).Abs(x.Num())
	den := x.Denom() // always > 0
	q := new(big.Int)
	rem := new(big.Int)
	q.QuoRem(num, den, rem) // q = trunc(|x|); 0 <= rem < den
	if rem.Sign() != 0 && roundAway(q, rem, den, neg, mode) {
		q.Add(q, big.NewInt(1))
	}
	if neg {
		q.Neg(q)
	}
	return q
}

// roundAway reports whether the truncated magnitude q should step away from zero,
// given the discarded remainder rem (< den), the sign, and the mode.
func roundAway(q, rem, den *big.Int, neg bool, mode decimal.RoundingMode) bool {
	cmp := new(big.Int).Lsh(rem, 1).Cmp(den) // 2·rem vs den: <0 below, 0 tie, >0 above
	switch mode {
	case decimal.RoundDown:
		return false
	case decimal.RoundUp:
		return true
	case decimal.RoundCeiling:
		return !neg
	case decimal.RoundFloor:
		return neg
	case decimal.RoundHalfUp:
		return cmp >= 0
	case decimal.RoundHalfDown:
		return cmp > 0
	case decimal.RoundHalfEven:
		if cmp != 0 {
			return cmp > 0
		}
		return q.Bit(0) == 1
	case decimal.Round05Up:
		d := new(big.Int).Mod(q, big.NewInt(10)).Int64()
		return d == 0 || d == 5
	}
	return false
}

// largestRemainderOrder returns part indices ordered by descending remainder
// (ascending when the leftover is negative), ties broken by ascending index — so
// spare minor units go to the parts that were truncated the most.
func largestRemainderOrder(rem []*big.Int, negLeftover bool) []int {
	order := make([]int, len(rem))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		c := rem[order[a]].Cmp(rem[order[b]])
		if negLeftover {
			return c < 0
		}
		return c > 0
	})
	return order
}
