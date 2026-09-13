# Accounting workflow integrity

The September 2026 review found failures that balanced-journal tests did not catch.
The fixes keep related records consistent through rejection, closing and reload.

## Posting and closing

Web operations validate their subsidiary changes first and apply them only after
the journal posts, under the same application lock. This covers invoices, receipts,
bills, payments, costs, assets, payroll, dividends and currency conversions.
Import allocations are validated before their corresponding journals are posted.
A rejected supplier credit is reported independently: a valid payment for the
same bill is still imported and reduces its outstanding balance.

Dividend declarations now check distributable reserves as at the declaration
date, rather than the financial-year end. Income dated after the declaration can
no longer support it. This check applies to declarations made in the app;
imported dividends continue to be posted as supplied, with a review note.

The ledger enforces closed dates. Closing journals remain in balances and the audit
trail, while `ActivityBetween` excludes them from P&L, tax and comparative figures.
`MovementBetween` still returns all postings. A break-even year closes without a
zero-amount retained-earnings posting. Other closing errors leave the year open.

Raw reversals are blocked for closing entries and entries with asset, share,
currency, payroll, mileage or dividend records that a reversal cannot update.
Domain-specific correction flows for those records are still a roadmap item.

## Credit notes and VAT

Invoice and bill credits are separate from cash paid/received. Outstanding is
original total less payments and credits. Credits against outstanding debts must
name the invoice/bill, cannot exceed its remaining balance and survive reload.
The forms take net amounts and VAT rates; use the original document's rate and
expense category. Supplier cash refunds use the bank directly.

The domestic VAT return classifies the VAT leg using the journal's income or
purchase accounts. Supplier credits reduce Box 4; sales credits reduce Box 1;
payments/refunds between the bank and VAT control account are excluded. Closing
entries are excluded from sales and purchases. A journal mixing sales and purchases
with VAT must be split before producing the return. Its error identifies the
reference, date and narrative of the journal. The return stays unavailable until
the ambiguity is corrected; partial totals are not presented as a completed
return. The UI uses the VAT quarter containing the game date, defaulting to
calendar quarters when no stagger is set.

This remains a simplified domestic accrual-accounting return. VAT-only manual
journals do not identify a supply and therefore do not populate Boxes 1/4.
Cash accounting, reverse charge, imports and partial exemption require more detail.

## Payroll, depreciation and mileage

BR, D0 and D1 apply their single rate to all taxable pay, as described in
[HMRC's tax-code guidance](https://www.gov.uk/employee-tax-codes/letters).
The app accumulates a person's payments in each tax year and charges the difference
between the annual calculation and deductions already posted. Payslips show the
incremental payment; P60s sum the payments. Benefits held as an annual amount are
not added again on every run. Same-year backdating behind an existing run is
rejected because it would require recalculating later payslips.

The model continues to use annual earnings periods. It is not a monthly PAYE
implementation; pension eligibility/relief, tax-code changes, and ordinary employee
pay periods require further work. Employee identity currently uses the name.

Depreciation records its last financial year and number of charged years. Repeating
the action in that year does nothing, including after a restart. Future purchases
are skipped; closed periods reject the action. The final useful-life year clears
rounding remainders. The existing policy charges a full year without acquisition-day
proration.

Mileage records the claimant, journey date and miles. Earlier claims in the same
tax year count towards the first 10,000 miles; another claimant/year starts its own
count. Journey dates before 6 April 2026 use 45p for the first band, later dates 55p;
the second band remains 25p. See
[HMRC's mileage rules](https://www.gov.uk/expenses-and-benefits-business-travel-mileage/rules-for-tax).
Backdating behind an existing same-year claim is rejected.

## Saves and older data

Snapshot version 1 adds closing metadata, credited amounts, depreciation periods
and mileage history. Version 0 saves are accepted.

**Employee salary meaning has changed.** Older saves labelled `employee.Salary`
as an annual figure. The stored amount is kept unchanged and is now the gross
payment for every payroll run, added to that person's tax-year total. An employee
saved with a £30,000 annual salary therefore receives £30,000 on each run: after
two runs in one tax year, cumulative gross pay is £60,000 and deductions are
calculated on that total. Review saved employee amounts before running payroll
again. There is no automatic conversion into monthly pay, and existing payroll
history is retained. The Employees page explains this change beside the forms.

Older closing entries are recognised by their original `Year-end close` narrative.
For legacy assets with accumulated depreciation, the latest saved depreciation
date conservatively marks the last charged period. Older files did not retain
claimant/mileage history, so that information cannot be reconstructed from the
monetary postings.

Saving holds the application lock through snapshot encoding and writing, uses a
unique temporary file, flushes it, and renames it into place. The preceding save is
copied atomically to `.bak` first. Startup reports unreadable/unsupported saves
without replacing them, and failed restoration leaves the current state intact.
Save errors during use are visible in the next UI page. To recover from a backup,
stop the UI, preserve the damaged file, copy the `.bak` file to the configured data
path and restart. Use one writable UI process per file.

Saved files and their backups now use owner-only `0600` permissions, replacing
`0644`; newly created save directories use `0700`, replacing `0755`. Existing
directories keep their current permissions.

These changes prevent new instances of the reviewed bugs. They do not rewrite
incorrect payments, credits or duplicated depreciation already saved by older
versions; those require reviewing the original transaction history.

## Validation

Regression tests reproduce the original failures and exercise credits followed by
settlement, multi-year close/reload, locked postings, repeated depreciation,
claimant/year mileage boundaries, malformed saves, backups and concurrent writes.
Run `go test ./...`, `go vet ./...`, and `go test -race ./...`.
