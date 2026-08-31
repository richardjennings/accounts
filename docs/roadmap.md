# What's missing — a reasoned gap analysis

The product's promise is specific: an **educational game** that runs a fully virtual UK
limited company, keeps **fully correct, rules-abiding books**, and can **generate every
artefact perfectly (accounts, iXBRL) while transmitting nothing for real** — data enters
by CSV import and leaves as generated documents; no HMRC/Companies House/bank-feed
integrations. This document measures the current system against that promise and
prioritises what to build next.

It is deliberately honest about gaps, including simplifications inside features that
already "work".

---

## Progress update (delivered since this analysis was written)

- **Statutory FRS 105 micro-entity accounts + iXBRL** (was P1 #2, the flagship). A new
  standalone **`ixbrl` module** (`github.com/richardjennings/ixbrl`) builds well-formed
  inline-XBRL by construction (validated as XML, `xmllint`-clean); the `frs105` package
  maps the ledger to the statutory balance sheet and P&L. Viewable and downloadable as a
  real `.html` iXBRL file. **Generated perfectly, transmitted never.**
- **Persistence** (was P1 #1). The company is saved to a JSON file after every change and
  restored on startup by replaying its journals; `money` and `decimal` gained JSON
  marshalers. Round-trip tested.
- **Year-end close** (was P1 #3). Posts the closing journal (P&L + dividends → retained
  earnings), locks the period, and rolls the clock into the next year.
- **CSV import — Crunch templates** (was P2 #6). A `csvimport` package loads invoices and
  expenses (tolerant header matching, UK day-first dates, VAT extracted from inclusive
  totals); the UI pastes or uploads CSV. Import is the only inbound channel.
- Also since the original list: **Class 1A NIC** on benefits, a **VAT-registered flag**,
  the **directors & shareholders register** with share capital and per-share dividends, a
  designated **main bank account**, and **recharging invoice lines against real recorded
  expenses** (reconciled, not free-text).

- **Pension auto-enrolment** (was P1 #5). Statutory 5% employee + 3% employer on qualifying
  earnings (£6,240–£50,270); posts to pension expense + pension payable; payslip + toggles.
- **Edit/correct via reversals** (was P2 #8). Reverse any posted journal from the Journals
  page (guarded so it can't desync a subsidiary ledger); corrections stay proper double-entry.
- **Bank reconciliation** (was P2 #7). Import a bank-statement CSV per account and reconcile
  it against the ledger balance, ticking lines off; shows the difference until it agrees.
- **Educational surface** (was P3 #12/#13/#14). Every posting is narrated in plain language
  (hover in activity; full explanations + principle on the Journals page), and a **Learn**
  section explains the game, the six sections, a first-month walkthrough, and key terms.
- **VAT return** (was P1 #4, previously dropped). Re-scoped in: with the VAT-registered flag
  on, a nine-box VAT return is **generated** (not filed) — Boxes 1/3/4/5 exact from the VAT
  control account. A `vatreturn` package computes it; shown under Company Tax.

**The whole P1 list plus most of P2/P3 is now built.** Remaining: purchase credit notes;
share transfers / multiple classes; accruals & prepayments; associated-company CT limits;
guided interactive lessons (beyond the Learn page); a downloadable VAT-return document.

---

## What is built today

- **Exact money** on the `decimal` engine; round-half-up (UK convention) to the penny.
- **Double-entry ledger** — balanced-by-construction immutable journals; control accounts;
  trial balance; balances as-at/period movements.
- **Company & financial year** — identity (name, number, SIC, registered office,
  incorporation), accounting reference date, derived financial years; VAT-registration flag.
- **Subsidiary ledgers** — sales (invoices → receipts) and purchase (bills → payments),
  each reconciled to its control account.
- **Itemised invoices** — multi-line, per-line VAT, and **recharging a recorded cost** to a
  customer that reconciles back to the actual expense.
- **VAT in the books** — output/input VAT, VAT control account, per-line half-up (no return).
- **Payroll** — PAYE bands, employee/employer NIC, Employment Allowance, student loans,
  benefits in kind + **Class 1A**; multiple employees.
- **Corporation tax** — small-profits/main/marginal relief; capital allowances (AIA/WDA);
  adjusted taxable profit.
- **Fixed assets** — register, straight-line/reducing-balance depreciation.
- **Company-secretarial** — register of directors/officers and members (shareholders);
  share capital issuance; **dividends declared and allocated by shareholding** with vouchers.
- **Banking** — multiple accounts, a designated **main account**, transfers, interest/charges.
- **Reports** — P&L, balance sheet, trial balance (as data + plain text).
- **Web UI** — the six product sections with a left sidebar and dated forms throughout.
- **Explain / cookbook** — plain-language narration of journals (library; not yet in the UI).

Everything above has tests; the web flows are verified end-to-end.

---

## Priority 1 — closest to the core promise, or correctness-critical

### 1. Persistence (save / load)
Today the whole company lives in memory and is lost on restart. For a *game* a child comes
back to, this is the single biggest blocker. A file-per-company store (JSON snapshot of the
journals + registers, or an append-only event log the ledger replays) keeps the "no external
integration" spirit. **Effort: medium. Enables everything else to be *used*.**

### 2. Statutory accounts + iXBRL generation
This *is* the promise ("generate correct iXBRL"), and it does not exist yet — `report`
produces figures but not the **FRS 105 micro-entity accounts** document (balance sheet +
P&L + footer statements) or its **iXBRL** tagging. This is the flagship deliverable:
a "Produce year-end accounts" button that renders the micro-entity balance sheet and P&L in
the statutory format and emits iXBRL XML (saved to a file, never transmitted).
**Effort: large. Highest product value.**

### 3. Year-end close
There is no closing process: profit is never swept to retained earnings, and periods never
lock. Without it, multi-year play and correct opening reserves don't hold. Add a close that
posts P&L → retained earnings at the year-end and freezes the period. **Effort: small–medium.
Prerequisite for realistic multi-year games and for #2.**

### 4. VAT return computation (compute, don't file)
VAT is in the books; the natural completion is to **compute the 9-box VAT return** for a
period (outputs, inputs, net due) as a generated document — explicitly *not filed*. Mirrors
the "produce everything, transmit nothing" rule and is highly educational. **Effort: small.**

### 5. Pension auto-enrolment
Real UK payroll is legally required to auto-enrol eligible workers (qualifying earnings,
3% employer / 5% employee default, postponement). The payroll engine currently ignores it,
so "real payroll" is incomplete. **Effort: medium.**

---

## Priority 2 — completeness and realism

### 6. CSV import — and the Crunch validation strategy
Import is the only inbound channel in the design, and it's not built. The most valuable
target is **importing a real Crunch export** to discover what we're missing against a
production system. Research findings (see *Crunch import* below) that shape the work:
- Crunch's **bulk export is `.XLS`, one sheet per data type, plus a `documents/` PDF folder**
  — not CSV. So either read `.XLS` or accept the documented **import CSV templates**.
- Cleanest CSV targets (documented, verbatim headers):
  - **Invoices:** `Issue date, Client, Description, Invoice Total, Includes VAT?, Payment Date, Bank Account/Director` (one row per invoice, total is **gross**, VAT back-computed).
  - **Expenses:** `Date, Supplier, Description, Amount, Payment method, Director`.
  - **Bank statement:** `Date, Reference, Paid In, Paid Out, Balance` (mapped at import).
- Crunch exports **no journals and no chart of accounts** — the importer must **synthesise
  the double entries** (our engine already can) and **map Crunch categories → our nominal
  codes**. Dates are UK day-first; totals are gross.
- Encouraging signal: Crunch's own expense model carries `recharged` and `reconciled` flags —
  exactly the recharge-reconciliation we just built. We're modelling the right things.

**Effort: medium.** Start with the invoice + expense CSV templates (well-specified), then a
bank-statement importer feeding reconciliation (#7).

### 7. Bank reconciliation + statement import
Match imported statement lines to invoices/bills/payments — Crunch's central workflow and a
genuine teaching moment (why the bank balance ≠ the ledger). **Effort: medium.** Pairs with #6.

### 8. Edit / delete / correct transactions
The ledger is immutable (correct) but the UI has no correcting-entry or reversal flow, so a
mistyped amount is permanent. Add reversals/corrections (the ledger already supports
`Reverse`). **Effort: small–medium. High everyday value.**

### 9. Accruals, prepayments, and other period-end adjustments
No accruals/prepayments, deferred income, or bad-debt write-offs — real month/year ends need
them, and they're teachable. **Effort: medium.**

### 10. Payroll realism
Monthly/weekly pay periods (only annualised today), payslip/P60/P45 documents, statutory pay
(SSP/SMP), and the directors' cumulative-vs-alternative NI method. **Effort: medium–large.**

### 11. Company-secretarial depth
Share **transfers** between members, multiple **share classes**, share premium, a **PSC
register**, a **confirmation statement** document, and dividend vouchers as printable PDFs.
Articles of association are still only referenced, not modelled. **Effort: medium.**

---

## Priority 3 — the educational layer and polish

### 12. Bring `explain` into the UI
The plain-language narration exists as a library but isn't surfaced. Every posting/screen
should be able to answer "why did this go here?" — the whole point of an *educational* game.
**Effort: small–medium. Directly on-mission.**

### 13. Guided scenarios / lessons
Scripted playthroughs ("run your first month", "your first VAT quarter", "pay yourself a
salary + dividend efficiently") with checks and explanations. **Effort: medium.**

### 14. Glossary/help in the UI
`docs/glossary.md` exists; wire it in as contextual help. **Effort: small.**

### 15. Multi-company, accessibility, richer CT600/P11D(b) detail, associated-company CT
limits, R&D, special-rate pool / balancing charges. **Effort: varies.**

---

## Known simplifications inside features that already work

These are correct enough for the game but not the full rules:

- **Dividends** credit the director's loan account for every shareholder; a non-director
  shareholder should have their own payable.
- **Class 1A** is folded into the employer-NIC posting rather than tracked as a distinct
  P11D(b) liability with its own July payment date.
- **Corporation tax** ignores associated companies (limits not divided) and augmented profits.
- **Capital allowances** cover AIA + WDA main pool; no special-rate pool, FYA, or balancing
  charges/allowances on disposal.
- **Credit notes** don't yet allocate against a specific invoice in the sales ledger.
- **Recharge VAT** is always standard-rated when registered; it should follow the liability
  of the main supply.
- **Payroll** assesses an annual earnings period only — no in-year periods or RTI artefacts.
- **Share capital** is a single ordinary class at par; no premium, classes, or transfers.

---

## Recommended sequence

1. **Persistence (#1)** — make the game keep its state.
2. **Year-end close (#3)** — needed for correct reserves and multi-year play.
3. **Statutory accounts + iXBRL (#2)** — deliver the flagship artefact.
4. **VAT return computation (#4)** — quick, completes the VAT story.
5. **CSV import + Crunch (#6/#7)** — then import real accounts to surface the next round of gaps.
6. **Explain-in-UI (#12) and scenarios (#13)** — turn a correct engine into an actual lesson.

The theme throughout: the engine is sound and the books are right; the missing third is the
**outputs** (accounts/iXBRL/VAT return), the **inputs** (import), and the **teaching surface**
that together make it the product it's meant to be.
