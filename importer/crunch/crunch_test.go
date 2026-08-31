package crunch

import (
	"strings"
	"testing"

	"github.com/richardjennings/accounts/importer"
	"github.com/richardjennings/accounts/money"
)

func csvTable(name, text string) *importer.Table {
	var recs [][]string
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		recs = append(recs, strings.Split(line, ";"))
	}
	return importer.FromCSV(name, recs)
}

// export is a small Crunch export in every workbook the profile reads, with
// the same columns and vocabulary as a real one. Fields are ";"-separated.
func export() importer.Tables {
	return importer.Tables{
		"Clients":   csvTable("Clients", "Company name;Primary contact name;Billing address;VAT registration number\nWidgets Inc;;1 Main St;\nBrit Ltd;;2 High St;GB999"),
		"Suppliers": csvTable("Suppliers", "Company name;Default expense type\nCloudCo;Web Hosting / Cloud Services\nAccountants LLP;Accountancy\nOddSupplier;None"),
		"Sales invoices": csvTable("Sales invoices", `Issued date;Due date;Client;Invoice # - Ref;Net amount;VAT amount charged;VAT amount due;Gross amount;Currency;Currency net amount;Currency gross amount;Payments;Credit notes;Outstanding amount;Status
2026-01-10;2026-01-24;Widgets Inc;INV-1;750.00;0;0;750.00;$;1000;1000;None;None;1000;Overdue
2026-02-10;2026-02-24;Widgets Inc;INV-2;1500.00;0;0;1500.00;$;2000;2000;None;None;2000;Overdue
2026-02-28;2026-03-14;Brit Ltd;INV-6;50.00;0;0;50.00;GBP;50;50;None;None;50;Overdue
2026-03-01;2026-03-15;Brit Ltd;INV-3;100.00;20.00;20.00;120.00;GBP;100;120;None;None;120;Overdue
2026-03-05;2026-03-19;Brit Ltd;INV-4;300.00;0;0;300.00;$;400;400;None;None;400;Overdue`),
		"Sales invoice credit notes": csvTable("Sales invoice credit notes", "Date;Credit note number;Sales invoice number;Gross amount;Refund(s) total;Credit note status\n2026-03-06;CN-4;INV-4;400;0;Settled\n2026-03-07;CN-9;INV-9;50;0;Settled"),
		"Client payments": csvTable("Client payments", `Date;Client;Ref;Payment method;Payment account;Currency;Amount;Combined;Unallocated
2026-01-20;Widgets Inc;;Transfer into business bank account;USD Wise;$;1000;false;0
2026-02-20;Widgets Inc;;Transfer into business bank account;USD Wise;$;500;false;0
2026-02-21;Widgets Inc;;Into petty cash;None;$;1600;false;0
2026-03-10;Brit Ltd;;Transfer into business bank account;Tide;$;120;false;0
2026-03-11;Nobody Ltd;;Transfer into business bank account;Tide;$;5;false;0`),
		"Expenses": csvTable("Expenses", `Date;Supplier - Ref;Recharge to;Net amount;VAT amount;Gross amount;Payment(s) amount;Combined;Payment method(s);Credit note(s) amount;Payment status;Attachments;Line item(s) description
2026-01-05;CloudCo;Widgets Inc;150.00;30.00;180.00;180.00;false;Transfer out from business bank account;0;Paid;0;Hosting
2026-01-06;Accountants LLP;None;100.00;20.01;120.01;120.01;false;Transfer out from business bank account;0;Paid;0;Fees and postage
2026-01-07;OddSupplier;None;40.00;0;40.00;40.00;false;Paid by director personally;0;Paid;0;Taxi
2026-01-08;CloudCo;None;10.00;2.00;12.00;0;false;Transfer out from business bank account;0;You owe;0;Domain
2026-01-09;OddSupplier;None;7.00;0;7.00;7.00;false;Out from petty cash;0;Paid;0;Stamps`),
		"Expense Line Items": csvTable("Expense Line Items", `Invoice Date;Supplier Reference;Recharged Client;Expense Type;VAT;Description;Gross
2026-01-05;;Widgets Inc;Web Hosting / Cloud Services;Standard - 20%;Hosting;180
2026-01-06;;;Accountancy;Standard - 20%;Fees and postage;100
2026-01-06;;;Postage;Exempt;Fees and postage;20.01
2026-01-07;;;Travel;Outside the Scope;Taxi;40
2026-01-08;;;Web Hosting / Cloud Services;Standard - 20%;Domain;12`),
		"Money transfers":      csvTable("Money transfers", "Date;Source;Destination;Reference;Amount\n2026-01-25;USD Wise;GBP Wise;;740.00\n2026-01-26;GBP Wise;Company Petty Cash;;40.00"),
		"Bank deposits":        csvTable("Bank deposits", "Date;Payment account;Memo;Cheques;Petty cash amount;Company cheque amount;Director deposit amount;Total amount;Combined\n2026-01-27;Tide;;None;25;0;0.16;25.16;false"),
		"Interest received":    csvTable("Interest received", "Date;Payment method;Payment account;Amount;Combined\n2026-01-31;Transfer into business bank account;Tide Savings;1.23;false"),
		"Director salaries":    csvTable("Director salaries", "Date;Director;Amount\n2026-01-31;Richard;1047.5"),
		"Employee salaries":    csvTable("Employee salaries", "Date;Employee;Gross Amount"),
		"Payroll runs":         csvTable("Payroll runs", "Date;Payment status;No. employees;Total\n2026-01-31;Paid;1;0\n2026-02-28;Paid;1;12.5"),
		"Dividends":            csvTable("Dividends", "Date;Net amount;Tax credit;Shareholders\n2026-02-01;5000;0;1"),
		"Director withdrawals": csvTable("Director withdrawals", "Date;Director;Reference;Payment method;Payment account;Amount;Combined\n2026-02-02;Richard;;Transfer out from business bank account;Tide;3000;false"),
		"Tax payments":         csvTable("Tax payments", "Date;Payment type;Payment method;Amount;Combined\n2026-01-15;Corporation Tax;Transfer out from business bank account;999.99;false\n2026-01-16;VAT;Transfer out from business bank account;10;false"),
		"Tax rebates":          csvTable("Tax rebates", "Date;Payment type;Payment method;Amount;Combined\n2026-01-17;PAYE/NIC;Received by director personally;50;false"),
		"Client refunds":       csvTable("Client refunds", "Date;Client;Amount\n2026-01-18;Widgets Inc;1"),
	}
}

func TestReadExport(t *testing.T) {
	b, issues, err := Profile{}.Read(export(), money.GBP)
	if err != nil {
		t.Fatal(err)
	}
	all := strings.Join(issueStrings(issues), "\n")

	if len(b.Customers) != 2 || len(b.Suppliers) != 3 || b.Customers[1].VATNumber != "GB999" {
		t.Errorf("parties: %+v %+v", b.Customers, b.Suppliers)
	}
	if want := []string{"GBP Wise", "Tide", "Tide Savings", "USD Wise"}; strings.Join(b.Banks, ",") != strings.Join(want, ",") {
		t.Errorf("banks = %v, want %v", b.Banks, want)
	}
	if len(b.BankCurrency) != 1 || b.BankCurrency["USD Wise"] != "USD" {
		t.Errorf("bank currencies = %v, want USD Wise only", b.BankCurrency)
	}
	if !b.VATCharged {
		t.Error("VATCharged should be set: INV-3 charges VAT")
	}

	// Invoices: GBP values, with the foreign gross in the description.
	if len(b.Invoices) != 5 {
		t.Fatalf("%d invoices", len(b.Invoices))
	}
	inv := b.Invoices[0]
	if inv.Ref != "INV-1" || inv.Lines[0].Net.String() != "GBP 750.00" || inv.Lines[0].Description != "Invoiced in $: $ 1000.00 gross" || inv.Memo == "" {
		t.Errorf("INV-1: %+v", inv)
	}
	if l := b.Invoices[3].Lines[0]; l.VAT.String() != "GBP 20.00" || l.VATRate.String() != "0.20" {
		t.Errorf("INV-3 VAT: %+v", l)
	}

	// Credit note in the invoice currency settles the invoice at its GBP gross.
	if len(b.CreditNotes) != 2 || b.CreditNotes[0].Gross.String() != "GBP 300.00" || b.CreditNotes[0].Invoice != "INV-4" {
		t.Errorf("credit notes: %+v", b.CreditNotes)
	}
	if !strings.Contains(all, "unknown invoice \"INV-9\"") {
		t.Errorf("missing issue for CN-9:\n%s", all)
	}

	// Receipts: $1000 settles INV-1 in full at £750; $500 is a quarter of INV-2
	// ($2000 = £1500), so £375; $1600 covers the remaining $1500 (£1125) with
	// $100 left over, unallocated at face value; £120 settles INV-3 exactly even
	// though INV-6 is older and still open; Nobody Ltd has no invoice, so its £5
	// is unallocated.
	if r := b.Receipts[0]; r.CcyAmount.String() != "USD 1000.00" {
		t.Errorf("receipt 0 currency amount = %s", r.CcyAmount)
	}
	if r := b.Receipts[4]; !r.CcyAmount.IsZero() { // INV-3 is a GBP invoice
		t.Errorf("GBP receipt carries a currency amount: %s", r.CcyAmount)
	}
	got := receiptSummary(b.Receipts)
	want := "INV-1 GBP 750.00 USD Wise|INV-2 GBP 375.00 USD Wise|INV-2 GBP 1125.00 petty|- GBP 100.00 petty|INV-3 GBP 120.00 Tide|- GBP 5.00 Tide"
	if got != want {
		t.Errorf("receipts:\n got %s\nwant %s", got, want)
	}
	if !strings.Contains(all, "no open invoice for Nobody Ltd") || !strings.Contains(all, "exceeds the open invoices of Widgets Inc") {
		t.Errorf("missing receipt issues:\n%s", all)
	}

	// Expenses: joined to line items, VAT reconciled to the expense's amount.
	if len(b.Bills) != 6 {
		t.Fatalf("%d bills: %+v", len(b.Bills), b.Bills)
	}
	bill := b.Bills[0]
	if bill.Supplier != "CloudCo" || bill.Category != "Web Hosting / Cloud Services" || bill.Net.String() != "GBP 150.00" || bill.VAT.String() != "GBP 30.00" || bill.Recharge != "Widgets Inc" || bill.PaidBy != importer.Bank || bill.Paid.String() != "GBP 180.00" {
		t.Errorf("bill 0: %+v", bill)
	}
	// Two lines: 100 standard (net 83.33 + 16.67) and 20.01 exempt; the expense
	// says VAT 20.01, so the standard line absorbs the 3.34 difference.
	if b1, b2 := b.Bills[1], b.Bills[2]; b1.Category != "Accountancy" || b1.VAT.String() != "GBP 20.01" || b1.Net.String() != "GBP 79.99" || b2.Category != "Postage" || !b2.VAT.IsZero() || b2.Net.String() != "GBP 20.01" || b1.Paid.String() != "GBP 100.00" || b2.Paid.String() != "GBP 20.01" {
		t.Errorf("split bills: %+v / %+v", b1, b2)
	}
	if b3 := b.Bills[3]; b3.PaidBy != importer.Director || b3.Category != "Travel" || b3.Net.String() != "GBP 40.00" {
		t.Errorf("director-paid bill: %+v", b3)
	}
	if b4 := b.Bills[4]; b4.PaidBy != importer.Unpaid || !b4.Paid.IsZero() || b4.Net.String() != "GBP 10.00" {
		t.Errorf("unpaid bill: %+v", b4)
	}
	if b5 := b.Bills[5]; b5.PaidBy != importer.PettyCash || b5.Category != "" || b5.Net.String() != "GBP 7.00" || !strings.Contains(all, "no line item matches \"Stamps\"") {
		t.Errorf("petty cash bill without lines: %+v\n%s", b5, all)
	}

	// Transfers include the petty-cash float from Bank deposits.
	if len(b.Transfers) != 3 || b.Transfers[1].To != "" || b.Transfers[2].From != "" || b.Transfers[2].To != "Tide" || b.Transfers[2].Amount.String() != "GBP 25.00" {
		t.Errorf("transfers: %+v", b.Transfers)
	}
	if len(b.Introduced) != 1 || b.Introduced[0].Amount.String() != "GBP 0.16" {
		t.Errorf("introduced: %+v", b.Introduced)
	}
	if len(b.Interest) != 1 || b.Interest[0].Bank != "Tide Savings" {
		t.Errorf("interest: %+v", b.Interest)
	}
	if len(b.Salaries) != 1 || b.Salaries[0].Gross.String() != "GBP 1047.50" || !b.Salaries[0].TaxNIC.IsZero() || !b.Salaries[0].Owed {
		t.Errorf("salaries: %+v", b.Salaries)
	}
	if !strings.Contains(all, "payroll run total GBP 12.50 not posted") {
		t.Errorf("missing payroll issue:\n%s", all)
	}
	if len(b.Dividends) != 1 || len(b.Drawings) != 1 || b.Drawings[0].Bank != "Tide" {
		t.Errorf("dividends %+v drawings %+v", b.Dividends, b.Drawings)
	}
	if len(b.TaxPayments) != 2 || b.TaxPayments[0].Kind != importer.CorporationTax || b.TaxPayments[1].Kind != importer.VATTax {
		t.Errorf("tax payments: %+v", b.TaxPayments)
	}
	if len(b.TaxRebates) != 1 || !b.TaxRebates[0].ToDirector || b.TaxRebates[0].Kind != importer.PAYE {
		t.Errorf("tax rebates: %+v", b.TaxRebates)
	}
	if !strings.Contains(all, "Client refunds: 1 row(s) not imported") {
		t.Errorf("missing unsupported issue:\n%s", all)
	}
}

func TestVATRate(t *testing.T) {
	for in, want := range map[string]string{"Standard - 20%": "0.2", "Reduced - 5%": "0.05", "Zero": "0", "Exempt": "0", "Outside the Scope": "0", "12.5%": "0.125"} {
		if got := vatRate(in).String(); got != want {
			t.Errorf("vatRate(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestNotACrunchExport(t *testing.T) {
	if _, _, err := (Profile{}).Read(importer.Tables{"Other": csvTable("Other", "a;b\n1;2")}, money.GBP); err == nil {
		t.Error("accepted a source with no Crunch tables")
	}
}

func issueStrings(issues []importer.Issue) []string {
	var out []string
	for _, i := range issues {
		out = append(out, i.String())
	}
	return out
}

func receiptSummary(rs []importer.Receipt) string {
	var parts []string
	for _, r := range rs {
		inv, bank := r.Invoice, r.Bank
		if inv == "" {
			inv = "-"
		}
		if bank == "" {
			bank = "petty"
		}
		parts = append(parts, inv+" "+r.Amount.String()+" "+bank)
	}
	return strings.Join(parts, "|")
}
