package main

import (
	"fmt"

	"github.com/richardjennings/accounts/chart"
	"github.com/richardjennings/accounts/explain"
	"github.com/richardjennings/accounts/ledger"
	"github.com/richardjennings/accounts/themes"
)

// A pendingOperation validates all subsidiary changes before posting and applies
// them only after the journal succeeds. Both phases run under the app lock.
type pendingOperation struct {
	themes.Operation
	apply func()
}

// A raw reversal cannot repair the associated register or annual history. Keep
// such entries out of the generic correction flow until a domain correction is
// available; credit notes handle outstanding invoices and bills.
func (a *app) reversalReason(j ledger.Journal) string {
	if j.IsClosing() {
		return "a year-end closing entry cannot be reversed independently of its period lock"
	}
	if touchesSubsidiary(j) {
		return "this entry moves trade debtors/creditors — correct it with a credit note"
	}
	for _, p := range j.Postings() {
		if p.Account == chart.PlantEquipment || p.Account == chart.AccumulatedDepreciation || p.Account == chart.ShareCapital || a.isForeign(p.Account) {
			return "this entry has an asset, share or currency register that a raw reversal cannot update"
		}
	}
	for _, run := range a.runs {
		if run.Ref == j.Ref() {
			return "this payroll entry has a payslip and annual deductions; a raw reversal cannot correct them"
		}
	}
	for _, run := range a.mileageRuns {
		if run.Ref == j.Ref() {
			return "this mileage entry counts towards annual mileage; a raw reversal cannot correct it"
		}
	}
	for _, run := range a.dividends {
		if run.Ref == j.Ref() {
			return "this dividend entry has vouchers; a raw reversal cannot correct them"
		}
	}
	return ""
}

func afterPost(op themes.Operation, apply func()) themes.Operation {
	return pendingOperation{Operation: op, apply: apply}
}

func (a *app) postOperation(section string, op themes.Operation) error {
	j, err := op.Journal()
	if err != nil {
		return err
	}
	if a.inClosedPeriod(j.Date()) {
		return fmt.Errorf("%s is in a closed period — use a later date", j.Date())
	}
	if err := a.book.Post(j); err != nil {
		return err
	}
	if pending, ok := op.(pendingOperation); ok {
		pending.apply()
		op = pending.Operation
	}
	principle := ""
	if ex, err := explain.Explain(a.book, op); err == nil {
		principle = ex.Principle
	}
	a.entries = append(a.entries, entry{section, j, principle})
	return nil
}
