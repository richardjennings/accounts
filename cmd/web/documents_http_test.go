package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/richardjennings/accounts/chart"
)

func TestDividendVoucherAndMinute(t *testing.T) {
	a, err := newApp(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	h := a.persistMiddleware(a.routes())
	drive(t, h, "/accounting/journals/post", url.Values{"date": {"2026-05-01"}, "debit": {chart.Bank}, "credit": {chart.Sales}, "amount": {"10000.00"}, "narrative": {"sales"}})
	drive(t, h, "/pay-yourself/dividends/declare", url.Values{"amount": {"1000.00"}, "date": {"2026-06-01"}})
	if len(a.dividends) != 1 || a.dividends[0].Available.String() != "GBP 10000.00" {
		t.Fatalf("dividends = %+v", a.dividends)
	}

	body := page(t, h, "/pay-yourself/dividends")
	for _, s := range []string{"Dividends declared", "reserves available £10,000.00", "/pay-yourself/dividends/minute?i=0", "/pay-yourself/dividends/voucher?i=0&amp;member=Alex%20Director"} {
		if !strings.Contains(body, s) {
			t.Errorf("dividends page lacks %q", s)
		}
	}

	voucher := page(t, h, "/pay-yourself/dividends/voucher?i=0&member=Alex+Director")
	for _, s := range []string{"DIVIDEND VOUCHER", "Alex Director", "£1,000.00", "GBP 10.0000 per share", "financial year ending 2027-03-31", "Signed on behalf of the board"} {
		if !strings.Contains(voucher, s) {
			t.Errorf("voucher lacks %q", s)
		}
	}
	minute := page(t, h, "/pay-yourself/dividends/minute?i=0")
	for _, s := range []string{"BOARD MINUTE", "Present: Alex Director", "distributable reserves of <b>£10,000.00</b>", "IT WAS RESOLVED", "£1,000.00"} {
		if !strings.Contains(minute, s) {
			t.Errorf("minute lacks %q", s)
		}
	}
	if rec := page(t, h, "/pay-yourself/dividends/voucher?i=0&member=Nobody"); !strings.Contains(rec, "not found") {
		t.Error("a voucher for a non-member was produced")
	}

	b, err := newApp(a.dataPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.dividends) != 1 || b.dividends[0].Awards[0].Member.Name != "Alex Director" {
		t.Errorf("reloaded dividends = %+v", b.dividends)
	}
}

func TestVATReturnDocument(t *testing.T) {
	a, err := newApp("")
	if err != nil {
		t.Fatal(err)
	}
	h := a.routes()
	drive(t, h, "/accounting/journals/post", url.Values{"date": {"2026-05-01"}, "debit": {chart.Bank}, "credit": {chart.VAT}, "amount": {"200.00"}, "narrative": {"output VAT"}})

	doc := page(t, h, "/company-tax/vat/document")
	for _, s := range []string{"VAT RETURN", "GB123456789", "Period 2026-04-01 to 2027-03-31", "£200.00", "to pay to HMRC"} {
		if !strings.Contains(doc, s) {
			t.Errorf("VAT return document lacks %q", s)
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/company-tax/vat/document?download=1", nil))
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, `attachment; filename="vat-return-2027-03-31.html"`) {
		t.Errorf("download disposition = %q", cd)
	}
	if !strings.Contains(page(t, h, "/company-tax/vat"), "/company-tax/vat/document?download=1") {
		t.Error("VAT page has no download link")
	}

	// A company that is not VAT registered has no return to download.
	a.mu.Lock()
	a.co.VATRegistered = false
	a.mu.Unlock()
	if !strings.Contains(page(t, h, "/company-tax/vat/document"), "not found") {
		t.Error("a VAT return was produced for an unregistered company")
	}
}
