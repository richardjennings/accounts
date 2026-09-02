# Glossary

Domain and technical terms used across the `accounts` project. Bookkeeping,
financial-statement, UK company-filing, and money/number terms in one A–Z.

---

**Abridged accounts** — A simplified set of accounts a small company could prepare
(with shareholder consent) by combining certain statutory line items in the balance
sheet and/or profit & loss. Distinct from *filleted*. Being abolished from 1 April 2028.

**Account** — A single bucket in the *chart of accounts* that money is recorded into.
Has a code, a name, and a *type*. In code: `ledger.Account`.

**Accounting equation** — `Assets = Liabilities + Equity`. The balance sheet always
obeys it; double-entry is what keeps it true.

**Accounting reference date (ARD)** — The day and month a company's financial year
ends. The first accounting reference period runs from incorporation for more than six
months and at most eighteen months; each later one is twelve months. In code:
`company.Company.YearEndDay` / `YearEndMonth`.

**Accrual basis / cash basis** — Accrual recognises income and costs when *earned or
incurred*; cash basis when money actually *moves*. UK companies generally use accrual.
(Not yet modelled — deferred.)

**Allocation (largest-remainder)** — Splitting a money amount into parts that sum
*exactly* back to the whole, giving leftover minor units to the parts rounded down the
most. In code: `money.Money.Allocate` / `Split`.

**Asset** — Something the business owns or is owed (bank, cash, trade debtors). Balance
sheet; normal balance is a *debit*.

**Balance** — The net of an account's postings. Reported in its natural sense: positive
on the account's *normal side*. In code: `ledger.Book.Balance`.

**Balance sheet** — Statement of financial position at a point in time: assets,
liabilities, and equity.

**Banker's rounding** — Round-half-to-even. The decimal engine's default, and
deliberately *not* used for UK money — see *round half up*.

**Chart of accounts** — The master list of all accounts a business uses; the vocabulary
every posting is expressed in. Package `chart`.

**Companies House** — The UK registrar of companies. Holds the *public* company record;
receives a (currently reduced) set of accounts.

**Confirmation statement** — The annual filing that confirms the company's registered
details at Companies House: officers, people with significant control, shareholders and
share capital, registered office and email, SIC code. Due within fourteen days after the
end of each *review period*. In code: `company.Company.NextStatement`.

**Corporation tax** — Tax on a company's taxable profit, computed from accounting profit
with adjustments. Filed to HMRC on the **CT600** return.

**Credit** — The right-hand side of a posting. Increases liabilities, equity, and income;
decreases assets and expenses.

**Currency / minor units / scale** — A currency (e.g. GBP) has a *scale*: the number of
decimal places in its minor unit (2 for GBP → pence). `money` holds amounts as an exact
integer count of minor units. In code: `money.Currency`.

**Debit** — The left-hand side of a posting. Increases assets and expenses; decreases
liabilities, equity, and income.

**Director's loan account (DLA)** — Running record of money moving between a company and
its director personally. Swings between an asset (director owes company) and a liability
(company owes director); an overdrawn DLA has tax consequences.

**Dividend** — A distribution of post-tax profit to shareholders. Reduces retained
earnings; must come from distributable reserves.

**Double-entry bookkeeping** — Every transaction is recorded as equal debits and credits,
so the books always balance. The `ledger` package enforces this.

**ECCTA / April 2028 changes** — The Economic Crime and Corporate Transparency Act 2023.
From 1 April 2028 it abolishes filleted/abridged accounts, requires small and micro
companies to file a profit & loss account (with an option to keep it off the public
register), and mandates software-only iXBRL filing.

**Equity** — The owners' residual interest: share capital plus retained earnings. Balance
sheet; normal balance is a *credit*.

**Expense** — A cost incurred in trading (salaries, cost of sales, office costs). Profit &
loss; normal balance is a *debit*.

**Filleted accounts** — The current way a small company "publishes partial": it files at
Companies House with the profit & loss and directors' report *omitted*, leaving only the
balance sheet and notes on public record. Being abolished from 1 April 2028.

**FRS 102 Section 1A** — The UK accounting standard for *small* companies. More
disclosures and judgements than FRS 105 (e.g. deferred tax).

**FRS 105** — The UK accounting standard for *micro-entities*. Heavily simplified: no
deferred tax, no revaluation, minimal notes, no profit & loss filed publicly today.

**Full accounts** — The complete statutory accounts (balance sheet, profit & loss, notes,
and directors' report where required) prepared for *members* and *HMRC*.

**HMRC** — His Majesty's Revenue and Customs. Receives *full* accounts plus the tax
computation (CT600), in iXBRL.

**Income** — Revenue the business earns (sales, other income). Profit & loss; normal
balance is a *credit*.

**iXBRL** — Inline eXtensible Business Reporting Language: accounts as human-readable
documents with machine-readable tags. Required by HMRC now, and for *all* Companies House
accounts filing from April 2028. (Planned — a `filing` concern.)

**Journal (journal entry)** — A balanced set of postings recorded on a date; the unit of
recording. Immutable once posted. In code: `ledger.Journal`.

**Key date** — One filing or payment and the date it is due, derived from the year end,
the date of incorporation and the last *statement date*. In code: `company.KeyDate`.

**Ledger (general ledger)** — The complete books: the chart of accounts plus every posted
journal, from which balances and the trial balance are derived. In code: `ledger.Book`.

**Liability** — Something the business owes (trade creditors, tax payable, director's
loan). Balance sheet; normal balance is a *credit*.

**Micro-entity** — The smallest company size band (post-6 April 2025: turnover ≤ £1m,
balance sheet ≤ £500k, ≤ 10 employees — meet 2 of 3). Eligible for FRS 105.

**Money** — The project's exact monetary type: an integer count of a currency's minor
units. Package `money`.

**Normal balance / normal side** — The side an account type increases on: debit for
assets and expenses, credit for liabilities, equity, and income.

**PAYE / NIC** — Pay As You Earn income tax and National Insurance Contributions,
deducted from salary and owed to HMRC. Relevant to the *Pay Yourself* theme.

**Posting** — One line of a journal: an amount applied to one account on one side (debit
or credit). Always a positive amount. In code: `ledger.Posting`.

**Profit and loss account (P&L)** — Statement of performance over a period: income minus
expenses. The profit rolls into equity as retained earnings.

**Retained earnings** — Accumulated post-tax profit not yet distributed; part of equity.
The link between the P&L and the balance sheet.

**Reversing entry** — A journal that swaps the debits and credits of an earlier one to
undo it, used to correct posted (immutable) journals without altering them. In code:
`ledger.Journal.Reverse`.

**Review period** — The twelve months a confirmation statement covers. The first begins
on incorporation; each later one begins the day after the last *statement date*. In
code: `company.Company.ReviewPeriod`.

**Round half up (round half away from zero)** — The UK money-rounding convention and the
project default: exactly-half cases round away from zero (£1.005 → £1.01, −£1.005 →
−£1.01). Matches HMRC's rule; contrast *banker's rounding*.

**Small company** — The company size band above micro (post-6 April 2025: turnover ≤ £15m,
balance sheet ≤ £7.5m, ≤ 50 employees — meet 2 of 3). Uses FRS 102 Section 1A.

**Statement date** — The date a confirmation statement is made up to: the last day of
its *review period*.

**Statutory accounts** — The annual accounts a company is legally required to prepare under
the Companies Act, in the format its size band and standard prescribe.

**Trial balance** — A listing of every account's balance in debit and credit columns; the
totals must match, proving the ledger is in balance. In code: `ledger.Book.TrialBalance`.

**VAT** — Value Added Tax. **Out of scope for now** — noted here only so its absence is
deliberate, not an oversight.
