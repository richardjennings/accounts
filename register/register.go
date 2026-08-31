// Package register holds the company's statutory registers — the people behind the
// entity and their stake in it. It records the officers (directors and the company
// secretary) the Companies Act requires a company to keep, and the members
// (shareholders) with their shareholdings. Shares are issued at a nominal (par)
// value; the members' holdings sum to the issued share capital. Dividends are
// declared as a total and allocated to members by holding, to the penny.
//
// This is statutory record-keeping — who owns and runs the company — kept separate
// from the ledger, which records the money. The two meet only when shares are
// issued (cash in, share capital up) or a dividend is paid.
package register

import (
	"fmt"
	"math/big"

	"github.com/richardjennings/accounts/ledger"
	"github.com/richardjennings/accounts/money"
)

// Role is an officer's role in the company.
type Role string

const (
	Director  Role = "Director"
	Secretary Role = "Secretary"
)

// Officer is a company officer — a director or the secretary — as recorded in the
// register of directors.
type Officer struct {
	Name      string
	Role      Role
	Appointed ledger.Date
	Resigned  ledger.Date // zero value: still in office
}

// InOffice reports whether the officer currently holds office.
func (o Officer) InOffice() bool { return o.Resigned.IsZero() }

// Member is a shareholder (a member of the company) holding a number of shares of a
// share class.
type Member struct {
	Name   string
	Class  string // e.g. "Ordinary"
	Shares int
	Since  ledger.Date
}

// Register is the set of officers and members plus the nominal value of one share.
type Register struct {
	Officers []Officer
	Members  []Member
	Nominal  money.Money // nominal (par) value of a single share
}

// Directors returns the officers who are in-office directors.
func (r Register) Directors() []Officer {
	var out []Officer
	for _, o := range r.Officers {
		if o.Role == Director && o.InOffice() {
			out = append(out, o)
		}
	}
	return out
}

// TotalShares is the number of shares in issue across all members.
func (r Register) TotalShares() int {
	n := 0
	for _, m := range r.Members {
		if m.Shares > 0 {
			n += m.Shares
		}
	}
	return n
}

// IssuedCapital is the nominal value of the shares in issue: shares × nominal.
func (r Register) IssuedCapital() money.Money {
	return r.Nominal.MulInt(int64(r.TotalShares()))
}

// Award is a member's slice of a declared dividend.
type Award struct {
	Member Member
	Amount money.Money
}

// AllocateDividend splits a total dividend across the members in proportion to
// their shareholdings. Allocation is exact — the per-member amounts sum back to the
// total to the penny (largest-remainder rounding). It errors if there are no shares
// in issue.
func (r Register) AllocateDividend(total money.Money) ([]Award, error) {
	var members []Member
	var weights []int64
	for _, m := range r.Members {
		if m.Shares > 0 {
			members = append(members, m)
			weights = append(weights, int64(m.Shares))
		}
	}
	if len(members) == 0 {
		return nil, fmt.Errorf("register: no shares in issue to pay a dividend on")
	}
	parts, err := total.Allocate(weights...)
	if err != nil {
		return nil, err
	}
	awards := make([]Award, len(members))
	for i := range members {
		awards[i] = Award{Member: members[i], Amount: parts[i]}
	}
	return awards, nil
}

// PerShareLabel renders the implied dividend per share for a total, to four decimal
// places — indicative, since a per-share rate need not be a whole number of pence.
func (r Register) PerShareLabel(total money.Money) string {
	n := r.TotalShares()
	if n == 0 {
		return ""
	}
	per := new(big.Rat).Quo(total.Amount().Rat(), new(big.Rat).SetInt64(int64(n)))
	return total.Currency().Code + " " + per.FloatString(4) + " per share"
}
