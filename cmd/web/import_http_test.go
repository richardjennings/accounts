package main

import (
	"archive/zip"
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
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
