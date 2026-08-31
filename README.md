# accounts

**What this is.** An **educational game**: it teaches double-entry bookkeeping and
UK small-company accounting by giving learners — children and students — a *totally
virtual* limited company to run and understand, with built-in help and explanations
for every step. Under the game sits a fully-correct accounting engine: real
double-entry, real UK rules, correct statutory accounts and iXBRL. A teaching tool
has to be genuinely right, so correctness is the whole point — but every company in
it is fictional.

**Classification.** An educational game. Not intended for, and not to be used for,
the accounting of a real business.

It is built in layers, each with one job, resting on an exact-decimal foundation
so money arithmetic is never approximate.

## Architecture

```
decimal  →  money  →  ledger  →  ┬─ chart      charts of accounts (data; UK starter provided)
                                 ├─ report     P&L / balance sheet (framework-neutral)  [built]
                                 ├─ filing     Companies House profiles, iXBRL         [planned]
                                 ├─ themes     Sales, Expenses, Banking, Pay Yourself, Company Tax  [built]
                                 └─ explain    plain-language narration of any operation / journal  [built]
```

Dependencies point downward only. Each layer is testable in isolation, and the
accounting standard (FRS 105 vs FRS 102 §1A) lives in the upper layers — the
`ledger` core is deliberately framework- and jurisdiction-neutral.

| Package | Status | Purpose |
|---------|--------|---------|
| [`decimal`](https://github.com/richardjennings/decimal) | external | Arbitrary-precision decimal engine (GDA-conformant). The reason `0.1 + 0.2` is exact. |
| `money` | built | Currency-aware, fixed-scale, exact monetary type. Exact Add/Sub/allocation; single-rounded Mul/Div. |
| `ledger` | built | Double-entry engine: accounts, balanced-by-construction journals, balances, trial balance. |
| `chart` | built | Starter charts of accounts (data). A conventional UK micro-Ltd chart is provided. |
| `report` | built | Profit & loss and balance sheet from the ledger — framework-neutral; statutory formats later. |
| `themes` | built | Domain verbs that generate journals — Sales, Expenses, Banking, Pay Yourself, Company Tax. |
| `explain` | built | Plain-language narration of any operation or journal — the teaching layer. |
| `tax/corporationtax` | built | Computes the CT charge (SPR / main rate / Marginal Relief), rates keyed by financial year. |
| `tax/payroll` | built | Computes PAYE + employee/employer NI on a director's salary; fully rate-table-configurable. |
| `tax/capitalallowances` | built | AIA + writing-down allowances (main/special pools, small-pools); feeds the CT computation. |
| `dividends` | built | Distributable-reserves check — whether a proposed dividend is lawfully covered by reserves. |
| `fixedassets` | built | Fixed-asset register + depreciation (straight-line / reducing-balance); posts purchase and charge. |
| `mileage` | built | AMAP business-mileage claims (verified 2026/27 rates) and the reimbursement posting. |
| `filing` | planned | Generated artifacts per recipient — filing profiles and iXBRL XML. Generates; never submits. |
| `import` | planned | CSV ingest + column mapping into journals — the only data-entry point. |

## The product themes

Five of the product's six top-level themes are **workflows that generate journals**;
the sixth is the ledger they all post into.

| Theme | Produces | Posts to (ledger accounts) |
|-------|----------|----------------------------|
| **Sales** | Invoices, credit notes, receipts | Income; Trade debtors |
| **Expenses** | Bills, receipts, mileage | Expense accounts; Trade creditors |
| **Banking** | Feeds, statement lines, reconciliation | Bank / cash — the cash side of everything |
| **Pay Yourself** | Payslips, dividends, drawings | Director's loan; Dividends; Salaries; PAYE/NIC |
| **Company Tax** | Corporation-tax computation | CT charge & liability |
| **Accounting** | Journals, adjustments, year-end | *The ledger itself* + trial balance / reports |

## Producing vs publishing accounts

A UK small/micro company prepares **one set of full accounts** for its members and
HMRC, then puts a **reduced version** on the public Companies House register. The
engine models this as one canonical set of full accounts with per-destination
**filing profiles**, so "publish partial" is a property of the Companies House
profile — not a separate set of books.

This survives the ECCTA reforms taking effect **1 April 2028** (filleted/abridged
accounts abolished; small and micro companies must file a profit & loss account but
may opt out of *publishing* it; software-only iXBRL filing) as a change of flags,
not a rewrite. Consistent with the boundary above, the engine *generates* these
artifacts (including iXBRL); it never submits them.

## Current scope

- **In:** exact GBP money, UK round-half-up, the double-entry ledger, a starter chart.
- **Boundary (settled):** files in, documents out — CSV import and generated artifacts
  (accounts, iXBRL) only; no live HMRC / Companies House / bank-feed integrations.
- **Deferred:** VAT; the FRS 105 vs FRS 102 §1A choice; multi-currency/FX; persistence;
  reports; iXBRL generation; CSV import; guided scenarios and the plain-language
  explanation layer.

## Design principles

- **Money is exact.** Integer minor units; same-currency Add/Sub and allocation never
  lose a penny; Mul/Div round exactly once under an explicit mode.
- **UK rounding by default** — round half away from zero, not banker's rounding.
- **Journals balance or don't exist.** An unbalanced journal cannot be constructed.
- **Posted journals are immutable.** Corrections are reversing entries, preserving an
  audit trail.
- **The ledger is standard-neutral.** Recognition, measurement, and presentation rules
  live above it.
- **Files in, documents out.** Data enters by CSV import and leaves as generated
  artifacts (accounts, iXBRL). No live integrations, by design.
- **Everything is explainable.** Because it teaches, every posting and figure must be
  narratable in plain language — the *why* behind the debits and credits is a
  first-class output, not a footnote.

See [`docs/glossary.md`](docs/glossary.md) for definitions of the domain terms.

## Run the UI

```sh
go run ./cmd/web                        # http://127.0.0.1:8080 by default
go run ./cmd/web -addr 127.0.0.1:9000   # choose a port (or ACCOUNTS_ADDR=:9000); :0 auto-picks a free one
```

A self-contained front end over the engine, organised as the product is: a left-hand
menu of the six sections — **Sales, Expenses, Banking, Pay Yourself, Company Tax,
Accounting** — each expanding to its own sub-sections (Invoices, Salary, Dividends,
…). Every operation and calculator is wired in (payroll, corporation tax, the
dividend reserves check, depreciation, mileage), and the statements update live.
In-memory, no external integrations.

## Build & test

```sh
go test ./...
go vet ./...
```
