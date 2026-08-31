package importer

import (
	"fmt"
	"strings"
	"time"

	"github.com/richardjennings/accounts/ledger"
	"github.com/richardjennings/accounts/money"
	"github.com/richardjennings/xls"
)

// Table is one exported table: a header and typed rows.
type Table struct {
	Name   string
	Header []string
	Rows   [][]Value
	index  map[string]int
}

// Value is one cell. A source that knows a cell's type sets Date or Num; a
// text-only source (CSV) leaves them unset and the accessors parse Text.
type Value struct {
	Text  string
	Date  ledger.Date // set when the source knows the cell is a date
	IsNum bool        // Num holds the exact decimal text of a numeric cell
	Num   string
}

// FromXLS converts a worksheet: row 0 is the header.
func FromXLS(name string, sh *xls.Sheet) *Table {
	t := &Table{Name: name}
	if len(sh.Rows) == 0 {
		return t
	}
	for _, c := range sh.Rows[0] {
		t.Header = append(t.Header, c.String())
	}
	for _, row := range sh.Rows[1:] {
		vals := make([]Value, len(row))
		for i, c := range row {
			v := Value{Text: c.String()}
			switch {
			case c.IsDate():
				if tm, ok := c.Time(); ok {
					v.Date = ledger.NewDate(tm.Year(), tm.Month(), tm.Day())
				}
			case c.Kind == xls.Number:
				v.IsNum, v.Num = true, c.NumText()
			}
			vals[i] = v
		}
		t.Rows = append(t.Rows, vals)
	}
	return t
}

// FromCSV converts CSV records: the first is the header.
func FromCSV(name string, records [][]string) *Table {
	t := &Table{Name: name}
	if len(records) == 0 {
		return t
	}
	t.Header = records[0]
	for _, rec := range records[1:] {
		vals := make([]Value, len(rec))
		for i, s := range rec {
			vals[i] = Value{Text: strings.TrimSpace(s)}
		}
		t.Rows = append(t.Rows, vals)
	}
	return t
}

// Col returns the index of the first header matching one of the names, ignoring
// case, surrounding space and a trailing "?", or -1.
func (t *Table) Col(names ...string) int {
	if t.index == nil {
		t.index = map[string]int{}
		for i, h := range t.Header {
			if _, dup := t.index[normalise(h)]; !dup {
				t.index[normalise(h)] = i
			}
		}
	}
	for _, n := range names {
		if i, ok := t.index[normalise(n)]; ok {
			return i
		}
	}
	return -1
}

// Require returns an error naming the first missing column.
func (t *Table) Require(names ...string) error {
	for _, n := range names {
		if t.Col(n) < 0 {
			return fmt.Errorf("%s: no column %q", t.Name, n)
		}
	}
	return nil
}

func normalise(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimSuffix(s, "?")
	return strings.Join(strings.Fields(s), " ")
}

// Row gives typed access to one row.
type Row struct {
	t    *Table
	n    int // 1-based data row
	vals []Value
}

// Row returns data row n (1-based).
func (t *Table) Row(n int) Row { return Row{t: t, n: n, vals: t.Rows[n-1]} }

// Each calls fn for every data row.
func (t *Table) Each(fn func(r Row)) {
	for n := range t.Rows {
		fn(t.Row(n + 1))
	}
}

// N is the 1-based data row number.
func (r Row) N() int { return r.n }

func (r Row) value(name string) (Value, bool) {
	i := r.t.Col(name)
	if i < 0 || i >= len(r.vals) {
		return Value{}, false
	}
	return r.vals[i], true
}

// Text returns a cell as text, trimmed; "" when the column is absent.
func (r Row) Text(name string) string {
	v, _ := r.value(name)
	return strings.TrimSpace(v.Text)
}

// Date returns a date cell, or an error when the column is absent, empty or not
// a date. Text dates are read day-first (UK) or as ISO 8601.
func (r Row) Date(name string) (ledger.Date, error) {
	v, ok := r.value(name)
	if !ok {
		return ledger.Date{}, fmt.Errorf("no column %q", name)
	}
	if !v.Date.IsZero() {
		return v.Date, nil
	}
	s := strings.TrimSpace(v.Text)
	for _, layout := range []string{"02/01/2006", "2/1/2006", "02/01/06", "2006-01-02", "02-01-2006", "2006-01-02T15:04:05"} {
		if tm, err := time.Parse(layout, s); err == nil {
			return ledger.NewDate(tm.Year(), tm.Month(), tm.Day()), nil
		}
	}
	return ledger.Date{}, fmt.Errorf("%q is not a date", s)
}

// Money returns an amount cell in cur. An empty cell, or the placeholder
// "None", is zero. Text amounts may carry a currency sign and thousands
// separators.
func (r Row) Money(cur money.Currency, name string) (money.Money, error) {
	v, ok := r.value(name)
	if !ok {
		return money.Money{}, fmt.Errorf("no column %q", name)
	}
	text := v.Num
	if !v.IsNum {
		text = strings.TrimSpace(v.Text)
		if text == "" || strings.EqualFold(text, "none") {
			return money.Zero(cur), nil
		}
		text = strings.NewReplacer("£", "", "$", "", "€", "", ",", "", " ", "").Replace(text)
	}
	m, err := money.Parse(cur, text)
	if err != nil {
		return money.Money{}, fmt.Errorf("%q is not an amount", v.Text)
	}
	return m, nil
}

// Int returns a whole-number cell, or 0 when absent or empty.
func (r Row) Int(name string) int {
	v, ok := r.value(name)
	if !ok {
		return 0
	}
	text := v.Num
	if !v.IsNum {
		text = strings.TrimSpace(v.Text)
	}
	var n int
	fmt.Sscanf(text, "%d", &n)
	return n
}
