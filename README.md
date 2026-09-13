# accounts

**What this is.** An **educational game**: it teaches double-entry bookkeeping and
UK small-company accounting by giving learners — children and students — a *totally
virtual* limited company to run and understand, with built-in help and explanations
for every step. Under the game sits an exact-money,
double-entry engine with UK accounting workflows and educational FRS 105/iXBRL
outputs. Correctness is the aim; the supported rules and remaining simplifications
are listed in [the roadmap](docs/roadmap.md). Every company in it is fictional.

**Classification.** An educational game. Not intended for, and not to be used for,
the accounting of a real business.

It is built in layers, each with one job, resting on an exact-decimal foundation
so money arithmetic is never approximate.

## Architecture

```
decimal  →  money  →  ledger  →  ┬─ chart      charts of accounts (data; UK starter provided)
                                 ├─ report     P&L / balance sheet (framework-neutral)  [built]
                                 ├─ frs105     micro-entity layouts, educational iXBRL  [built]
                                 ├─ themes     Sales, Expenses, Banking, Pay Yourself, Company Tax  [built]
                                 └─ explain    plain-language narration of any operation / journal  [built]
```

Dependencies point downward only. Each layer is testable in isolation, and the
accounting standard (FRS 105 vs FRS 102 §1A) lives in the upper layers — the
`ledger` core is deliberately framework- and jurisdiction-neutral.

| Package | Status | Purpose |
|---------|--------|---------|
| [`decimal`](https://github.com/richardjennings/decimal) | external | Arbitrary-precision decimal engine (GDA-conformant). The reason `0.1 + 0.2` is exact. |
| [`xls`](https://github.com/richardjennings/xls) | external | Reads the binary `.xls` workbooks accounting packages export, with dates and exact amounts. |
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
| `frs105` | built | Micro-entity balance sheet, P&L, comparatives and educational iXBRL; full taxonomy validation remains open. |
| `filing` | planned | Recipient-specific filing profiles and validated artifacts. Generates; never submits. |
| `csvimport` | built | CSV rows of invoices, expenses and bank statements, matched by header name. |
| `importer` | built | A whole history from another package's export: typed tables (`.xls` via [`xls`](https://github.com/richardjennings/xls), or CSV) → a profile per package → records the engine posts. `importer/crunch` is the Crunch profile. |

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
planned filing layer will use one canonical set of full accounts with
per-destination **filing profiles**. Those profiles are not implemented yet.

The current `frs105` package generates an educational accounts document with
representative FRC taxonomy tags. It is tested as XML, but it does not validate
against the complete taxonomy or recipient filing rules. No document is submitted.

## Current scope

- **In:** exact GBP money, UK round-half-up, the double-entry ledger, a starter chart.
- **Boundary (settled):** files in, documents out — CSV import and generated artifacts
  (accounts, iXBRL) only; no live HMRC / Companies House / bank-feed integrations.
- **Import:** paste or upload CSV, or upload a whole Crunch export archive (Company →
  Import). A foreign-currency invoice is posted at its value in the company currency;
  the currency figure is kept on the invoice line.
- **Deferred:** FRS 102 §1A, complete taxonomy/filing validation, broader FX workflows,
  and guided interactive scenarios.

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
State is saved locally after changes. Saves are versioned and written atomically;
the preceding save is kept as `state.json.bak`. An unreadable save causes startup
to return an error and leaves the file intact. A save failure during use is shown
in the UI; the changes remain in memory. Run one writable UI process per save file.

## Ask questions over MCP

```sh
go run ./cmd/web -mcp                   # serves the books over stdio, read only
```

The same binary can serve the books to a Model Context Protocol (MCP) client such as
Claude Code. It reads the save file the web UI writes and loads it again whenever the
file changes, so an answer always matches the latest save. It never posts and never
writes the save file. `-data` and `ACCOUNTS_DATA` choose the save file, as for the UI.

The tools cover the company and its key dates, the financial position, dividend
capacity and history, the profit and loss, balance sheet and trial balance, journals
with their explanations, invoices, bills, payroll and the corporation-tax estimate.

To register it with Claude Code for this project on this machine:

```sh
claude mcp add accounts -- go -C "$PWD" run ./cmd/web -mcp
```

## Build & test

This development checkout currently uses local Go module replacements. Clone
`richardjennings/decimal`, `richardjennings/ixbrl`, and `richardjennings/xls` into
sibling directories (`../decimal`, `../ixbrl`, `../xls`) before building. Published,
pinned dependency versions remain a packaging task.

```sh
go test ./...
go vet ./...
go test -race ./...
```

See [the workflow integrity changes](docs/integrity-fixes.md) for the corrected
posting, closing, credit-note, VAT, payroll, depreciation and mileage behavior.
