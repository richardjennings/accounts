package money

import (
	"encoding/json"
	"fmt"
)

type moneyJSON struct {
	Currency string `json:"currency"`
	Amount   string `json:"amount"`
}

// MarshalJSON encodes as {"currency":"GBP","amount":"12.34"}. The amount is a
// fixed-scale string so it survives JavaScript's float64 JSON numbers intact.
func (m Money) MarshalJSON() ([]byte, error) {
	return json.Marshal(moneyJSON{Currency: m.cur.Code, Amount: formatFixed(m.minor(), m.cur.Scale)})
}

// UnmarshalJSON decodes {"currency","amount"} using a registered currency, and
// rejects an amount that does not fit that currency's scale.
func (m *Money) UnmarshalJSON(b []byte) error {
	var j moneyJSON
	if err := json.Unmarshal(b, &j); err != nil {
		return err
	}
	cur, ok := Lookup(j.Currency)
	if !ok {
		return fmt.Errorf("money: %w: %q", ErrUnknownCode, j.Currency)
	}
	parsed, err := Parse(cur, j.Amount)
	if err != nil {
		return err
	}
	*m = parsed
	return nil
}
