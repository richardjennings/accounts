# Posting cookbook

How each of the six product themes turns a real-world event into balanced
double-entry, against the starter UK micro-Ltd chart (package `chart`). Every row is
one journal — total debits equal total credits. No VAT (out of scope). Amounts are
illustrative.

This is the **developer reference**. The in-game explanations for children and
students are a simpler, graded register of the same material; the plain-language
*"reading it"* notes here are the seed for them. The **executable** form — the same
postings run through the real `ledger` and asserted to balance — lives in
[`cookbook/`](../cookbook).

## Sales

| Event | Debit | Credit |
|-------|-------|--------|
| Raise a sales invoice (credit sale) | 1100 Trade debtors | 4000 Sales |
| Customer pays the invoice | 1200 Bank | 1100 Trade debtors |
| Immediate (cash) sale | 1200 Bank | 4000 Sales |
| Credit note / refund | 4000 Sales | 1100 Trade debtors (or 1200 Bank) |

*Reading it:* income is always a **credit**; the matching **debit** is wherever the
value landed — a debtor if the customer pays later, the bank if they pay now. A
refund runs the sale backwards.

## Expenses

| Event | Debit | Credit |
|-------|-------|--------|
| Record a supplier bill | 7600 Office and admin | 2100 Trade creditors |
| Pay the supplier | 2100 Trade creditors | 1200 Bank |
| Expense paid straight from the bank | 7600 Office and admin | 1200 Bank |
| Purchase for resale (cost of sales) | 5000 Cost of sales | 2100 Trade creditors |
| Director pays a business cost personally | 7600 Office and admin | 2300 Director's loan account |

*Reading it:* a cost is a **debit** to an expense account; the **credit** says how it
was funded — a creditor if unpaid, the bank if paid, or the director's loan account
if the director paid out of their own pocket (the company now owes them). Use
whichever expense code fits (5000/7000/7500/7600…).

## Banking

| Event | Debit | Credit |
|-------|-------|--------|
| Move money bank → cash | 1210 Cash | 1200 Bank |
| Interest received | 1200 Bank | 4900 Other income |
| Bank charges | 7600 Office and admin | 1200 Bank |

*Reading it:* a bank account is an **asset**, so money in is a debit and money out is
a credit. **Reconciliation posts nothing** — it *matches* imported CSV statement
lines to postings that already exist; anything unmatched becomes one of the journals
above.

## Pay Yourself

| Event | Debit | Credit |
|-------|-------|--------|
| Director's salary (gross = tax/NIC withheld + net paid) | 7000 Directors' salaries | 2210 PAYE/NIC payable **and** 1200 Bank (net) |
| Pay PAYE/NIC over to HMRC | 2210 PAYE/NIC payable | 1200 Bank |
| Declare a dividend | 3100 Dividends | 2300 Director's loan account |
| Pay the dividend | 2300 Director's loan account | 1200 Bank |
| Director introduces funds (loan in) | 1200 Bank | 2300 Director's loan account |
| Director draws funds | 2300 Director's loan account | 1200 Bank |

*Reading it:* salary is a company **expense**; a dividend is **not** — it's a
distribution of profit, so it reduces equity (a debit to Dividends) rather than
touching the P&L. The **director's loan account** is the running tab between company
and director personally: a liability when the company owes them, an asset when they
owe the company. Employer's NIC, when modelled, is an extra expense: Dr 7000 / Cr 2210.

## Company Tax

| Event | Debit | Credit |
|-------|-------|--------|
| Provide for corporation tax at year end | 8200 Corporation tax charge | 2320 Corporation tax payable |
| Pay corporation tax to HMRC | 2320 Corporation tax payable | 1200 Bank |

*Reading it:* the tax **charge** is an expense in this year's P&L; the tax **payable**
is a liability on the balance sheet until you pay it. (What the charge *should be* —
profit adjusted for disallowables and allowances — is a computation in a later layer,
not a posting rule.)

## Accounting

| Event | Debit | Credit |
|-------|-------|--------|
| Issue share capital at incorporation | 1200 Bank | 3000 Share capital |
| Correct a mistake | *reverse the wrong journal (`Journal.Reverse`), then post the right one* | |
| Year-end close | income & expense balances → 3200 Retained earnings; 3100 Dividends → 3200 | |

*Reading it:* this theme is the ledger itself. At year end the P&L accounts are
"closed" into retained earnings, carrying the year's profit onto the balance sheet;
dividends are closed the same way, reducing it. Adjusting entries (accruals,
prepayments, depreciation) are recognition/measurement steps set by the accounting
standard — they live above the ledger and may need extra chart accounts.
