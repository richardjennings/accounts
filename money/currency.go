// Package money provides a currency-aware, fixed-scale monetary type built on the
// github.com/richardjennings/decimal arbitrary-precision decimal engine.
//
// A Money value is an exact integer number of a currency's minor units (pence for
// GBP) tagged with its Currency. Same-currency Add/Sub and allocation are exact;
// Mul and Div compute the exact rational result and round once to the currency's
// scale under a caller-chosen mode. Operations across different currencies return
// an error rather than silently mixing them.
package money

// Currency is an ISO 4217 currency together with the number of decimal places in
// its minor unit (its scale): 2 for GBP/EUR/USD, 0 for JPY, 3 for BHD.
type Currency struct {
	Code  string // ISO 4217 alphabetic code, e.g. "GBP"
	Scale int32  // minor-unit digits; GBP = 2 (pence)
}

// Common currencies. Register others with Register.
var (
	GBP = Currency{"GBP", 2}
	EUR = Currency{"EUR", 2}
	USD = Currency{"USD", 2}
	JPY = Currency{"JPY", 0}
)

var registry = map[string]Currency{
	"GBP": GBP, "EUR": EUR, "USD": USD, "JPY": JPY,
}

// Register adds or replaces a currency in the table used by Lookup and JSON
// decoding. Call it at init; it is not safe for concurrent use with lookups.
func Register(c Currency) { registry[c.Code] = c }

// Lookup returns the registered currency for an ISO code.
func Lookup(code string) (Currency, bool) { c, ok := registry[code]; return c, ok }

// Symbol returns the currency's display symbol (e.g. "£"), or "" when none is
// known — in which case callers should fall back to the ISO Code.
func (c Currency) Symbol() string {
	switch c.Code {
	case "GBP":
		return "£"
	case "EUR":
		return "€"
	case "USD":
		return "$"
	case "JPY":
		return "¥"
	}
	return ""
}
