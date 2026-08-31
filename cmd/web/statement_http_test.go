package main

import (
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
)

// drive posts a form and returns the redirect target.
func drive(t *testing.T, h http.Handler, path string, form url.Values) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("%s: %d", path, rec.Code)
	}
	return rec.Header().Get("Location")
}

func page(t *testing.T, h http.Handler, path string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	b, _ := io.ReadAll(rec.Body)
	return string(b)
}

func uploadStatement(t *testing.T, h http.Handler, bank, filename string, content []byte) {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	mw.WriteField("bank", bank)
	fw, _ := mw.CreateFormFile("file", filename)
	fw.Write(content)
	mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/banking/statements/upload", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("upload: %d", rec.Code)
	}
}

func TestStatementImportFlow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	a, err := newApp(path)
	if err != nil {
		t.Fatal(err)
	}
	h := a.persistMiddleware(a.routes())

	csv := []byte("Booking Date,Narrative,Value,Balance After\n01/05/2026,Client payment,1200.00,3200.00\n03/05/2026,Hosting,-36.00,3164.00\nbad,x,1,1\n")
	uploadStatement(t, h, "1200", "tide-may.csv", csv)

	// The mapping page detected the odd headers and previews two lines.
	p := page(t, h, "/banking/statements")
	for _, want := range []string{"tide-may.csv", `<option value="Booking Date" selected>`, `<option value="Value" selected>`, `<option value="Balance After" selected>`, "2 line(s), 1 unreadable", "2026-05-01", "£1,164.00"} {
		if !strings.Contains(p, want) {
			t.Errorf("mapping page lacks %q", want)
		}
	}

	// Remap: flip the sign, then confirm.
	drive(t, h, "/banking/statements/map", url.Values{"col_date": {"Booking Date"}, "col_desc": {"Narrative"}, "col_amount": {"Value"}, "col_balance": {"Balance After"}, "date_order": {"dmy"}, "negate": {"1"}})
	p = page(t, h, "/banking/statements")
	if !strings.Contains(p, "−£1,164.00") {
		t.Error("negate did not flip the preview total")
	}
	drive(t, h, "/banking/statements/map", url.Values{"col_date": {"Booking Date"}, "col_desc": {"Narrative"}, "col_amount": {"Value"}, "col_balance": {"Balance After"}, "date_order": {"dmy"}})
	if loc := drive(t, h, "/banking/statements/confirm", nil); loc != "/banking/reconcile" {
		t.Fatalf("confirm redirected to %s", loc)
	}
	if len(a.stmtLines) != 2 || a.stmtLines[0].Amount.String() != "GBP 1200.00" || a.stmtLines[1].Amount.String() != "GBP -36.00" || !a.stmtLines[0].HasBalance {
		t.Fatalf("lines: %+v", a.stmtLines)
	}
	if a.statementSpecs["1200"].Amount != "Value" {
		t.Errorf("preset not saved: %+v", a.statementSpecs)
	}

	// Re-uploading the same file imports nothing new, via the saved preset.
	uploadStatement(t, h, "1200", "tide-may.csv", csv)
	drive(t, h, "/banking/statements/confirm", nil)
	if len(a.stmtLines) != 2 {
		t.Errorf("duplicates were not skipped: %d lines", len(a.stmtLines))
	}

	// The saved mapping and the lines survive a restart.
	b, err := newApp(path)
	if err != nil {
		t.Fatal(err)
	}
	if b.statementSpecs["1200"].Amount != "Value" || len(b.stmtLines) != 2 {
		t.Errorf("restore: %+v, %d lines", b.statementSpecs, len(b.stmtLines))
	}
}

func TestStatementImportXLS(t *testing.T) {
	a, err := newApp("")
	if err != nil {
		t.Fatal(err)
	}
	h := a.persistMiddleware(a.routes())
	data, err := os.ReadFile("testdata/stmt.xls")
	if err != nil {
		t.Fatal(err)
	}
	uploadStatement(t, h, "1200", "stmt.xls", data)
	p := page(t, h, "/banking/statements")
	for _, want := range []string{`<option value="Paid In" selected>`, "2 line(s), 0 unreadable", "2026-05-01"} {
		if !strings.Contains(p, want) {
			t.Errorf("xls mapping page lacks %q", want)
		}
	}
	drive(t, h, "/banking/statements/confirm", nil)
	if len(a.stmtLines) != 2 || a.stmtLines[1].Amount.String() != "GBP -36.00" {
		t.Fatalf("xls lines: %+v", a.stmtLines)
	}
}
