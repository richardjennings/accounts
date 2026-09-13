package main

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/richardjennings/accounts/ledger"
	"github.com/richardjennings/accounts/mileage"
	"github.com/richardjennings/accounts/themes"
)

type mileageRun struct {
	Ref, Person string
	Date        ledger.Date
	Miles       int
}

func (a *app) mileageOperation(r *http.Request) (themes.Operation, string, error) {
	miles, err := a.whole(r, "miles")
	if err != nil || miles <= 0 {
		return nil, "", fmt.Errorf("enter a positive number of business miles")
	}
	when := a.date(r)
	person := strings.TrimSpace(r.FormValue("person"))
	if person == "" {
		person = a.signer()
	}
	prior := 0
	for _, run := range a.mileageRuns {
		if run.Person != person || taxYearStart(run.Date) != taxYearStart(when) {
			continue
		}
		if when.Before(run.Date) {
			return nil, "", fmt.Errorf("mileage for %s already recorded through %s; use that date or later", person, run.Date)
		}
		// Only the first 10,000 miles matter for the split; cap the sum to
		// avoid integer overflow even for unusually large entered claims.
		if run.Miles >= 10000-prior {
			prior = 10000
		} else {
			prior += run.Miles
		}
	}
	claim := mileage.Claim(miles, prior, mileage.Car, mileage.RatesOn(when))
	ref := a.ref("MIL")
	return afterPost(mileage.Reimbursement{Date: when, Ref: ref, Amount: claim}, func() {
		a.mileageRuns = append(a.mileageRuns, mileageRun{ref, person, when, miles})
	}), fmt.Sprintf("Mileage claim for %d miles: %s", miles, fmtMoney(claim)), nil
}
