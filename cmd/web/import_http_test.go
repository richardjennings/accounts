package main

import (
	"archive/zip"
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/richardjennings/accounts/chart"
	"github.com/richardjennings/accounts/ledger"
	"github.com/richardjennings/accounts/money"
	"github.com/richardjennings/accounts/register"
)

// exportZip zips the synthetic Crunch workbooks in testdata/crunch/xls.
func exportZip(t *testing.T) []byte {
	t.Helper()
	files, _ := filepath.Glob("testdata/crunch/xls/*.xls")
	if len(files) == 0 {
		t.Fatal("no fixture workbooks")
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		w, _ := zw.Create(filepath.Base(f))
		w.Write(data)
	}
	zw.Create("Documents/payslip/ignored.pdf")
	zw.Close()
	return buf.Bytes()
}

// uploadCrunch posts the fixture export through the router, with or without the
// "replace the books" option. It runs under a deadline: the handler and the save
// middleware share one mutex, and a deadlock would otherwise hang the test.
func uploadCrunch(t *testing.T, h http.Handler, replace bool) {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if replace {
		mw.WriteField("replace", "1")
	}
	fw, _ := mw.CreateFormFile("file", "Crunch Export - Complete.zip")
	fw.Write(exportZip(t))
	mw.Close()

	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		req := httptest.NewRequest(http.MethodPost, "/company/import/crunch", &body)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		done <- rec
	}()
	var rec *httptest.ResponseRecorder
	select {
	case rec = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("import request hung (deadlock)")
	}
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/company/import" {
		t.Fatalf("upload: %d %s", rec.Code, rec.Header().Get("Location"))
	}
}

// TestImportReplacesBooks imports over a company that already has details, a
// register and postings: with "replace" set, the postings go and the rest stays.
func TestImportReplacesBooks(t *testing.T) {
	a, err := newApp(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	h := a.persistMiddleware(a.routes())
	drive(t, h, "/company/details", url.Values{"name": {"Jennings Technology Limited"}, "number": {"14829707"}, "sic": {"62020"}, "office": {"Paul Street"}, "incorporated": {"2023-04-26"}, "yearend": {"2026-04-30"}})
	inc := a.co.Incorporated
	a.reg = register.Register{
		Officers: []register.Officer{{Name: "Richard Jennings", Role: register.Director, Appointed: inc}},
		Members:  []register.Member{{Name: "Richard Jennings", Class: "Ordinary", Shares: 1, Since: inc}},
		Nominal:  a.reg.Nominal,
	}
	drive(t, h, "/accounting/journals/post", url.Values{"date": {"2026-05-01"}, "debit": {chart.OfficeAdmin}, "credit": {chart.Bank}, "amount": {"999.00"}, "narrative": {"stale"}})
	five, _ := money.Parse(money.USD, "5.00")
	a.fxBalances = map[string]money.Money{}
	for _, code := range []string{"1201", "1202", "1203", "1204", "1205"} {
		a.fxBalances[code] = five
	}
	a.closedThrough = ledger.NewDate(2026, time.April, 30)

	uploadCrunch(t, h, true)

	if a.co.Name != "Jennings Technology Limited" || a.co.YearEndMonth != time.April || a.co.YearEndDay != 30 || a.co.Incorporated != inc {
		t.Errorf("company details changed: %+v", a.co)
	}
	if len(a.reg.Members) != 1 || a.reg.Members[0].Name != "Richard Jennings" || a.reg.Members[0].Shares != 1 {
		t.Errorf("register changed: %+v", a.reg)
	}
	if got := a.bal(chart.ShareCapital).String(); got != "GBP 1.00" {
		t.Errorf("share capital = %s, want GBP 1.00", got)
	}
	for _, e := range a.entries {
		if e.j.Narrative() == "stale" {
			t.Error("old posting survived the replace")
		}
	}
	if !a.closedThrough.IsZero() {
		t.Error("closed period survived the replace")
	}
	var usd bankAcct
	for _, bk := range a.banks {
		if bk.Name == "USD Wise" {
			usd = bk
		}
	}
	if got := a.fxBal(usd.Code).String(); got != "USD 13.33" {
		t.Errorf("USD balance = %s, want USD 13.33", got)
	}
	if got := a.bal("1100").String(); got != "GBP 1620.00" {
		t.Errorf("trade debtors = %s, want GBP 1620.00", got)
	}
}

// TestImportOverHTTP uploads an export through the real router with a save
// file, as the browser does, and checks the import lands, renders and persists.
// It runs under a deadline: the handler and the save middleware share one
// mutex, and a deadlock would otherwise hang the test.
func TestImportOverHTTP(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	a, err := newApp(path)
	if err != nil {
		t.Fatal(err)
	}
	h := a.persistMiddleware(a.routes())
	uploadCrunch(t, h, false)

	get := func(p string) string {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		b, _ := io.ReadAll(rec.Body)
		return string(b)
	}
	page := get("/company/import")
	for _, want := range []string{"Last import — Crunch", "<td>Sales invoices</td><td class=\"num\">3</td>", "<td>Receipts</td><td class=\"num\">1</td>", "<td>Bills</td><td class=\"num\">1</td>", "<td>Currency conversions</td><td class=\"num\">1</td>", "Added bank account: USD Wise"} {
		if !strings.Contains(page, want) {
			t.Errorf("import page lacks %q", want)
		}
	}
	sales := get("/sales")
	if strings.Count(sales, "/sales/invoices/view?ref=INV-") != 3 || !strings.Contains(sales, "Widgets Inc") {
		t.Errorf("sales page does not list the three invoices")
	}
	// The source's own numbers are the references, in date order.
	i1, i2, i3 := strings.Index(sales, `ref=INV-1"`), strings.Index(sales, `ref=INV-2"`), strings.Index(sales, `ref=INV-3"`)
	if i1 < 0 || i2 < i1 || i3 < i2 {
		t.Errorf("invoice references or order wrong: %d %d %d", i1, i2, i3)
	}

	// USD Wise was created as a USD account; the $1000 receipt filled it and the
	// 740 transfer to Tide converted out at the average carried rate:
	// carrying 750 → sold $1000×740/750 = $986.67, leaving $13.33 carried at £10.
	var usd bankAcct
	for _, bk := range a.banks {
		if bk.Name == "USD Wise" {
			usd = bk
		}
	}
	if usd.Currency != "USD" {
		t.Fatalf("USD Wise: %+v", usd)
	}
	if got := a.fxBal(usd.Code).String(); got != "USD 13.33" {
		t.Errorf("USD balance = %s", got)
	}
	if got := a.bal(usd.Code).String(); got != "GBP 10.00" {
		t.Errorf("carrying = %s", got)
	}

	// Persisted: a fresh app restores it all.
	b, err := newApp(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.invoiceDocs) != 3 || len(b.book.Journals()) < 6 || !b.co.VATRegistered { // INV-3 charges VAT
		t.Errorf("restored: %d invoice docs, %d journals, VAT registered %v", len(b.invoiceDocs), len(b.book.Journals()), b.co.VATRegistered)
	}
	if got := b.bal("1100").String(); got != "GBP 1620.00" { // 750 + 1500 + 120 invoiced, 750 received
		t.Errorf("trade debtors after restore = %s, want GBP 1620.00", got)
	}
}
