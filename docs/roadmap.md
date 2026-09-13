# Current scope and remaining work

This is an educational game for a fictional UK limited company. Exact arithmetic
and balanced journals are enforced; broader accounting and filing correctness is
limited to the workflows and assumptions below. It reads files and generates
local documents, with no live HMRC, Companies House or bank-feed integrations.

## Built

- Exact currency amounts and immutable, balanced journals.
- Sales and purchase ledgers, invoices, receipts, bills, payments and credit notes.
  Credits reduce outstanding debt separately from cash payments.
- Company identity, financial years, period locking, key dates, directors,
  shareholders, PSCs, share issuance/transfers and dividend documents.
- Framework-neutral P&L, balance sheet and trial balance. Closing entries transfer
  profit and dividends to reserves without erasing historical trading figures.
- Educational FRS 105 accounts layouts, comparative figures, approval details,
  employee notes and inline-XBRL output.
- Domestic VAT on sales/purchases, credit-note adjustments and a return for the
  configured VAT quarter. HMRC cash settlements are excluded from supplies.
- Annual-model payroll calculations, cumulative payments within a tax year,
  payslips/P60s, benefits, student loans and pension contribution calculations.
- Corporation-tax and capital-allowance calculators; a simplified web tax estimate.
- Asset acquisition and annual depreciation, with a period record preventing
  duplicate charges after reload.
- Mileage claims by claimant and tax year, using rates from the journey date.
- CSV/Crunch imports, statement imports/mappings, basic bank reconciliation,
  foreign-currency receipts and conversions.
- Local versioned JSON saves, atomic writes, a previous-save backup, and read-only
  MCP access to the saved company.
- Journal explanations, a glossary and a Learn page.

The [workflow integrity notes](integrity-fixes.md) explain the September 2026 bug
fixes, tests and migration limits. Existing erroneous historical entries are not
silently rewritten.

## Next priorities

1. **Domain correction workflows.** Correct payroll, asset, mileage, dividend and
   FX records with their linked journals and documents. Raw reversal is guarded
   where it cannot update the associated records. Add credit-note documents and
   line-specific credits/refunds, including already-paid sales invoices.
2. **Persisted tax state.** Wire period lengths, associated companies, loss relief
   and brought-forward capital-allowance pools into the web estimate. The underlying
   CT calculator already accepts days and associated-company counts; the UI does not
   supply them. Add downward CT provision adjustments and a CT600 document.
3. **Document validation.** Validate against a complete supported FRC taxonomy and
   recipient rules, then implement filing profiles. Current iXBRL has representative
   tags and XML validation; it is not a validated filing artifact. Add FRS 102 section 1A,
   long-term liability presentation and further statutory disclosures.
4. **Payroll periods and schemes.** Add proper weekly/monthly PAYE and employee NI,
   stable employee identifiers, fuller tax-code handling, pension eligibility and
   relief arrangements, and explicit handling of unsupported tax years. RTI-format
   document generation may follow; live submissions remain out of scope.
5. **VAT detail.** Store transaction tax classifications/tax points explicitly.
   Add cash accounting, reverse charge, imports, partial exemption, and adjustment
   workflows. The current calculation infers domestic supplies from income and
   purchase legs of journals; arbitrary VAT-only journals are not enough evidence.
6. **Import and reconciliation depth.** Add transaction-level bank matching and
   reconciliation as at a statement date, stronger duplicate/import provenance,
   and fuller foreign-currency movement/revaluation support.
7. **Portable releases.** Publish and pin the decimal, xls and ixbrl dependencies;
   the current development module requires their sibling checkouts. Add automated
   test/vet/race checks and release packaging.
8. **Teaching experience.** Add guided interactive scenarios, checkpoints,
   exercises and accessibility review beyond the existing explanations/Learn page.

## Explicit simplifications

- Payroll uses a director-style annual earnings model for all configured people.
  Bundled tables cover 2025/26 and 2026/27; dates outside that range currently use
  the nearest bundled table. PAYE references and National Insurance numbers are
  not held. Tax-code and pension treatment remain incomplete.
- Depreciation uses full annual charges without acquisition-date proration; the
  register has no complete disposal/impairment workflow.
- The UI capital-allowance estimate claims AIA on current-year additions. It does
  not persist the calculator's carried-forward pools or loss-relief state.
- Share capital is a single ordinary class at par. Share classes and share premium
  remain open.
- Average employee numbers default to current headcount until explicitly approved.
- Statutory accounts group all liabilities as current and other equity as reserves;
  the chart mapping needs expansion for more complex accounts.
- Bank reconciliation compares balances and supports ticks; it does not yet prove
  a one-to-one match of statement lines to ledger transactions.
- Saves are local to one company/file and assume one writable UI process. The
  read-only MCP process can run alongside it.
