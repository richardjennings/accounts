package money

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/richardjennings/decimal"
)

// Rounding is a rounding mode, re-exported so callers need not import decimal.
type Rounding = decimal.RoundingMode

const (
	HalfUp   = decimal.RoundHalfUp   // ties away from zero (recommended UK default)
	HalfEven = decimal.RoundHalfEven // ties to even ("banker's rounding")
	HalfDown = decimal.RoundHalfDown
	Down     = decimal.RoundDown // toward zero (e.g. HMRC VAT-total concession)
	Up       = decimal.RoundUp
	Ceiling  = decimal.RoundCeiling
	Floor    = decimal.RoundFloor
)

// DefaultRounding is the mode used by helpers that do not take one explicitly.
// Round-half-up (ties away from zero) is the standard UK accounting convention:
// HMRC's own rounding rule is "below half a penny rounds down, half a penny or
// more rounds up", applied symmetrically to negatives (a -x.xx5 credit rounds to
// -x.xx-away-from-zero). It is deliberately NOT the decimal engine's half-even
// "banker's" default, which is not the UK norm. Override per context only where a
// specific rule applies.
var DefaultRounding = HalfUp

// Errors returned by the package. Wrap-friendly: test with errors.Is.
var (
	ErrMixedCurrency = errors.New("mismatched currencies")
	ErrDivByZero     = errors.New("division by zero")
	ErrNonFinite     = errors.New("non-finite operand")
	ErrInexact       = errors.New("value does not fit the currency scale")
	ErrBadNumber     = errors.New("invalid number")
	ErrUnknownCode   = errors.New("unknown currency code")
	ErrWeights       = errors.New("weights must be non-negative and not all zero")
)

// Money is an exact amount in a fixed currency, held as an integer count of the
// currency's minor units. The zero value is not a usable amount; construct with
// FromMinorUnits, Parse, or Zero.
type Money struct {
	amount decimal.Decimal // invariant: finite, with exponent == -cur.Scale
	cur    Currency
}

var quantizeCtx = decimal.Context{Rounding: HalfUp} // unlimited precision; mode unused when exact

func scalePattern(scale int32) decimal.Decimal { return decimal.New(1, -scale) }

// Zero returns a zero amount in cur.
func Zero(cur Currency) Money { return fromMinorBig(cur, big.NewInt(0)) }

// FromMinorUnits returns the amount in cur representing n minor units (e.g. pence).
func FromMinorUnits(cur Currency, n int64) Money {
	return Money{decimal.New(n, -cur.Scale), cur}
}

// Parse reads a decimal string as an amount in cur. It is exact: a value with more
// fractional places than the currency allows (e.g. "1.234" for GBP) returns
// ErrInexact. Use FromDecimal to round instead.
func Parse(cur Currency, s string) (Money, error) {
	d, err := decimal.NewFromString(s)
	if err != nil {
		return Money{}, fmt.Errorf("money: %w: %q", ErrBadNumber, s)
	}
	return FromDecimalExact(cur, d)
}

// MustParse is Parse that panics on error; for tests and constants.
func MustParse(cur Currency, s string) Money {
	m, err := Parse(cur, s)
	if err != nil {
		panic(err)
	}
	return m
}

// FromDecimalExact converts d to cur without rounding, failing with ErrInexact if
// d cannot be represented exactly at the currency's scale.
func FromDecimalExact(cur Currency, d decimal.Decimal) (Money, error) {
	if !d.IsFinite() {
		return Money{}, fmt.Errorf("money: %w", ErrNonFinite)
	}
	q, cond := quantizeCtx.Quantize(d, scalePattern(cur.Scale))
	if cond.Has(decimal.Inexact) {
		return Money{}, fmt.Errorf("money: %w: %s in %s", ErrInexact, d.String(), cur.Code)
	}
	return Money{q, cur}, nil
}

// FromDecimal converts d to cur, rounding to the currency scale under mode.
func FromDecimal(cur Currency, d decimal.Decimal, mode Rounding) (Money, error) {
	if !d.IsFinite() {
		return Money{}, fmt.Errorf("money: %w", ErrNonFinite)
	}
	return fromMinorBig(cur, roundRatToScale(d.Rat(), cur.Scale, mode)), nil
}

// FromRat returns the exact rational r as an amount in cur, rounded to the
// currency's scale under mode. Calculations (e.g. tax) that work in exact
// rationals and round once at the end use this for the final conversion.
func FromRat(cur Currency, r *big.Rat, mode Rounding) Money {
	return fromMinorBig(cur, roundRatToScale(r, cur.Scale, mode))
}

// --- accessors ---

// Currency returns the amount's currency.
func (m Money) Currency() Currency { return m.cur }

// Amount returns the exact value as a decimal, at the currency's scale.
func (m Money) Amount() decimal.Decimal { return m.amount }

// minor returns the signed integer number of minor units. It is exact because the
// stored decimal always has exponent -Scale, so its coefficient is that integer.
func (m Money) minor() *big.Int { return m.amount.Coeff() }

// MinorUnits returns the signed minor-unit count and whether it fit an int64.
func (m Money) MinorUnits() (int64, bool) {
	v := m.minor()
	if !v.IsInt64() {
		return 0, false
	}
	return v.Int64(), true
}

// Sign returns -1, 0, or +1.
func (m Money) Sign() int        { return m.minor().Sign() }
func (m Money) IsZero() bool     { return m.Sign() == 0 }
func (m Money) IsNegative() bool { return m.Sign() < 0 }
func (m Money) IsPositive() bool { return m.Sign() > 0 }

// String renders the amount as "GBP 12.34" (culture-neutral, at fixed scale).
func (m Money) String() string {
	return m.cur.Code + " " + formatFixed(m.minor(), m.cur.Scale)
}

// --- arithmetic ---

func (m Money) sameCurrency(n Money) error {
	if m.cur != n.cur {
		return fmt.Errorf("money: %w: %s and %s", ErrMixedCurrency, m.cur.Code, n.cur.Code)
	}
	return nil
}

// Add returns m + n; the currencies must match. Exact, never rounds.
func (m Money) Add(n Money) (Money, error) {
	if err := m.sameCurrency(n); err != nil {
		return Money{}, err
	}
	return fromMinorBig(m.cur, new(big.Int).Add(m.minor(), n.minor())), nil
}

// Sub returns m - n; the currencies must match. Exact, never rounds.
func (m Money) Sub(n Money) (Money, error) {
	if err := m.sameCurrency(n); err != nil {
		return Money{}, err
	}
	return fromMinorBig(m.cur, new(big.Int).Sub(m.minor(), n.minor())), nil
}

// Neg returns -m.
func (m Money) Neg() Money { return fromMinorBig(m.cur, new(big.Int).Neg(m.minor())) }

// Abs returns |m|.
func (m Money) Abs() Money { return fromMinorBig(m.cur, new(big.Int).Abs(m.minor())) }

// Mul multiplies m by a decimal factor (e.g. a tax rate or fractional quantity)
// and rounds the exact product once to the currency scale under mode.
func (m Money) Mul(factor decimal.Decimal, mode Rounding) (Money, error) {
	if !factor.IsFinite() {
		return Money{}, fmt.Errorf("money: %w", ErrNonFinite)
	}
	r := new(big.Rat).Mul(m.amount.Rat(), factor.Rat())
	return fromMinorBig(m.cur, roundRatToScale(r, m.cur.Scale, mode)), nil
}

// MulInt multiplies m by an integer (exact, no rounding).
func (m Money) MulInt(n int64) Money {
	return fromMinorBig(m.cur, new(big.Int).Mul(m.minor(), big.NewInt(n)))
}

// Div divides m by a non-zero decimal divisor and rounds the exact quotient once
// to the currency scale under mode.
func (m Money) Div(divisor decimal.Decimal, mode Rounding) (Money, error) {
	if !divisor.IsFinite() {
		return Money{}, fmt.Errorf("money: %w", ErrNonFinite)
	}
	dr := divisor.Rat()
	if dr.Sign() == 0 {
		return Money{}, fmt.Errorf("money: %w", ErrDivByZero)
	}
	r := new(big.Rat).Quo(m.amount.Rat(), dr)
	return fromMinorBig(m.cur, roundRatToScale(r, m.cur.Scale, mode)), nil
}

// --- comparison ---

// Cmp compares m and n, returning -1, 0, or +1; the currencies must match.
func (m Money) Cmp(n Money) (int, error) {
	if err := m.sameCurrency(n); err != nil {
		return 0, err
	}
	return m.minor().Cmp(n.minor()), nil
}

// Equal reports whether m and n are the same currency and amount.
func (m Money) Equal(n Money) bool {
	return m.cur == n.cur && m.minor().Cmp(n.minor()) == 0
}

// --- allocation ---

// Allocate splits m into len(weights) parts in proportion to weights, with the
// parts guaranteed to sum exactly back to m. Leftover minor units from rounding go
// to the parts with the largest fractional remainders (earliest index breaks
// ties). Weights must be non-negative and not all zero.
func (m Money) Allocate(weights ...int64) ([]Money, error) {
	if len(weights) == 0 {
		return nil, fmt.Errorf("money: %w", ErrWeights)
	}
	total := big.NewInt(0)
	for _, w := range weights {
		if w < 0 {
			return nil, fmt.Errorf("money: %w", ErrWeights)
		}
		total.Add(total, big.NewInt(w))
	}
	if total.Sign() == 0 {
		return nil, fmt.Errorf("money: %w", ErrWeights)
	}

	minor := m.minor()
	parts := make([]*big.Int, len(weights))
	rem := make([]*big.Int, len(weights))
	allocated := big.NewInt(0)
	for i, w := range weights {
		prod := new(big.Int).Mul(minor, big.NewInt(w))
		q := new(big.Int)
		r := new(big.Int)
		q.QuoRem(prod, total, r) // truncates toward zero; r keeps the sign of prod
		parts[i] = q
		rem[i] = r
		allocated.Add(allocated, q)
	}

	leftover := new(big.Int).Sub(minor, allocated) // |leftover| < len(weights)
	steps := int(new(big.Int).Abs(leftover).Int64())
	unit := int64(1)
	if leftover.Sign() < 0 {
		unit = -1
	}
	order := largestRemainderOrder(rem, leftover.Sign() < 0)
	for i := 0; i < steps; i++ {
		parts[order[i]].Add(parts[order[i]], big.NewInt(unit))
	}

	out := make([]Money, len(weights))
	for i, p := range parts {
		out[i] = fromMinorBig(m.cur, p)
	}
	return out, nil
}

// Split divides m into n equal parts that sum exactly back to m.
func (m Money) Split(n int) ([]Money, error) {
	if n <= 0 {
		return nil, fmt.Errorf("money: %w", ErrWeights)
	}
	w := make([]int64, n)
	for i := range w {
		w[i] = 1
	}
	return m.Allocate(w...)
}

func fromMinorBig(cur Currency, minor *big.Int) Money {
	return Money{decimalFromMinor(minor, cur.Scale), cur}
}
