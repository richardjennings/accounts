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
	Name               string
	Role               Role
	Appointed          ledger.Date
	Resigned           ledger.Date // zero value: still in office
	ServiceAddress     string
	DateOfBirth        ledger.Date
	Nationality        string
	Occupation         string
	IdentityVerifiedOn ledger.Date // date Companies House verified the officer's identity; zero until then
}

// InOffice reports whether the officer currently holds office.
func (o Officer) InOffice() bool { return o.Resigned.IsZero() }

// IdentityVerified reports whether Companies House has verified the officer's identity.
func (o Officer) IdentityVerified() bool { return !o.IdentityVerifiedOn.IsZero() }

// ControlBand is a band of shares or voting rights a person with significant
// control holds, in the terms Companies House uses.
type ControlBand int

const (
	NoControl ControlBand = iota
	Over25                // more than 25% but not more than 50%
	Over50                // more than 50% but less than 75%
	AtLeast75             // 75% or more
)

// Bands lists the control bands a person can hold, lowest first.
var Bands = []ControlBand{Over25, Over50, AtLeast75}

func (b ControlBand) String() string {
	switch b {
	case Over25:
		return "more than 25% but not more than 50%"
	case Over50:
		return "more than 50% but less than 75%"
	case AtLeast75:
		return "75% or more"
	}
	return "none"
}

// BandForShares is the control band a holding of held shares out of total falls in.
func BandForShares(held, total int) ControlBand {
	switch {
	case total <= 0 || held*4 <= total:
		return NoControl
	case held*2 <= total:
		return Over25
	case held*4 < total*3:
		return Over50
	}
	return AtLeast75
}

// PSC is a person with significant control, as recorded in the PSC register: someone
// who holds more than 25% of the shares or voting rights, can appoint or remove a
// majority of the board, or otherwise has significant influence or control.
type PSC struct {
	Name                 string
	Notified             ledger.Date // the date the person became registrable
	Ceased               ledger.Date // zero value: still a person with significant control
	Shares               ControlBand
	Voting               ControlBand
	AppointsDirectors    bool // holds the right to appoint or remove a majority of the directors
	SignificantInfluence bool // otherwise has significant influence or control
	IdentityVerifiedOn   ledger.Date
}

// Current reports whether the person is still a person with significant control.
func (p PSC) Current() bool { return p.Ceased.IsZero() }

// IdentityVerified reports whether Companies House has verified the person's identity.
func (p PSC) IdentityVerified() bool { return !p.IdentityVerifiedOn.IsZero() }

// NatureOfControl lists the person's control in the statements Companies House uses.
func (p PSC) NatureOfControl() []string {
	var out []string
	if p.Shares != NoControl {
		out = append(out, "Ownership of shares – "+p.Shares.String())
	}
	if p.Voting != NoControl {
		out = append(out, "Ownership of voting rights – "+p.Voting.String())
	}
	if p.AppointsDirectors {
		out = append(out, "Right to appoint or remove directors")
	}
	if p.SignificantInfluence {
		out = append(out, "Has significant influence or control")
	}
	return out
}

// Member is a shareholder (a member of the company) holding a number of shares of a
// share class.
type Member struct {
	Name   string
	Class  string // e.g. "Ordinary"
	Shares int
	Since  ledger.Date
}

// Register is the set of officers, members and people with significant control,
// plus the nominal value of one share.
type Register struct {
	Officers []Officer
	Members  []Member
	PSCs     []PSC
	Nominal  money.Money // nominal (par) value of a single share
}

// CurrentPSCs returns the people who are still persons with significant control.
func (r Register) CurrentPSCs() []PSC {
	var out []PSC
	for _, p := range r.PSCs {
		if p.Current() {
			out = append(out, p)
		}
	}
	return out
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
