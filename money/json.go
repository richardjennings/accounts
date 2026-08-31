package money

import (
	"encoding/json"
	"fmt"

	"github.com/richardjennings/decimal"
)

type moneyJSON struct {
	Currency string           `json:"currency"`
	Amount   *decimal.Decimal `json:"amount"`
}

// MarshalJSON encodes as {"currency":"GBP","amount":"12.34"}. The amount takes the
// decimal's own JSON form, a string, so it survives JavaScript's float64 JSON
// numbers intact. The zero Money, which has no currency, encodes as null.
func (m Money) MarshalJSON() ([]byte, error) {
	if m.cur.Code == "" {
		return []byte("null"), nil
	}
	return json.Marshal(moneyJSON{Currency: m.cur.Code, Amount: &m.amount})
}

// UnmarshalJSON decodes {"currency","amount"} using a registered currency, and
// rejects a missing amount or one that does not fit that currency's scale. A JSON
// null decodes as the zero Money.
func (m *Money) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		*m = Money{}
		return nil
	}
	var j moneyJSON
	if err := json.Unmarshal(b, &j); err != nil {
		return err
	}
	cur, ok := Lookup(j.Currency)
	if !ok {
		return fmt.Errorf("money: %w: %q", ErrUnknownCode, j.Currency)
	}
	if j.Amount == nil {
		return fmt.Errorf("money: %w: missing amount", ErrBadNumber)
	}
	parsed, err := FromDecimalExact(cur, *j.Amount)
	if err != nil {
		return err
	}
	*m = parsed
	return nil
}
