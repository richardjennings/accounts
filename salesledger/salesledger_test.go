package salesledger

import (
	"testing"
	"time"

	"github.com/richardjennings/accounts/ledger"
	"github.com/richardjennings/accounts/money"
)

func gbp(s string) money.Money { return money.MustParse(money.GBP, s) }
func date() ledger.Date        { return ledger.NewDate(2026, time.June, 1) }

func TestRaiseAndOutstanding(t *testing.T) {
	l := New()
	inv, err := l.Raise("INV-1", "Acme", date(), gbp("1200.00"))
	if err != nil {
		t.Fatal(err)
	}
	if inv.Outstanding().String() != "GBP 1200.00" || inv.Status() != "Open" {
		t.Fatalf("new invoice: outstanding %s status %s", inv.Outstanding(), inv.Status())
	}
	if len(l.Outstanding()) != 1 {
		t.Fatalf("want 1 outstanding invoice")
	}
}

func TestPartialThenFullReceipt(t *testing.T) {
	l := New()
	l.Raise("INV-1", "Acme", date(), gbp("1200.00"))

	if err := l.Allocate("INV-1", gbp("500.00")); err != nil {
		t.Fatal(err)
	}
	inv, _ := l.Get("INV-1")
	if inv.Outstanding().String() != "GBP 700.00" || inv.Status() != "Part-paid" {
		t.Fatalf("after part receipt: outstanding %s status %s", inv.Outstanding(), inv.Status())
	}

	if err := l.Allocate("INV-1", gbp("700.00")); err != nil {
		t.Fatal(err)
	}
	if !inv.Settled() || inv.Status() != "Paid" {
		t.Fatalf("after full receipt: settled %v status %s", inv.Settled(), inv.Status())
	}
	if len(l.Outstanding()) != 0 {
		t.Fatalf("want no outstanding invoices once paid")
	}
}

func TestOverpaymentRejected(t *testing.T) {
	l := New()
	l.Raise("INV-1", "Acme", date(), gbp("100.00"))
	if err := l.Allocate("INV-1", gbp("150.00")); err == nil {
		t.Fatal("expected an error receiving more than is outstanding")
	}
}

func TestUnknownInvoice(t *testing.T) {
	if err := New().Allocate("NOPE", gbp("10.00")); err == nil {
		t.Fatal("expected an error for an unknown invoice")
	}
}
