package purchaseledger

import (
	"testing"
	"time"

	"github.com/richardjennings/accounts/ledger"
	"github.com/richardjennings/accounts/money"
)

func gbp(s string) money.Money { return money.MustParse(money.GBP, s) }
func date() ledger.Date        { return ledger.NewDate(2026, time.June, 1) }

func TestRecordAndPay(t *testing.T) {
	l := New()
	if _, err := l.Record("BILL-1", "Supplies Ltd", date(), gbp("300.00")); err != nil {
		t.Fatal(err)
	}
	if err := l.Allocate("BILL-1", gbp("100.00")); err != nil {
		t.Fatal(err)
	}
	b, _ := l.Get("BILL-1")
	if b.Outstanding().String() != "GBP 200.00" || b.Status() != "Part-paid" {
		t.Fatalf("after part payment: outstanding %s status %s", b.Outstanding(), b.Status())
	}
	if err := l.Allocate("BILL-1", gbp("200.00")); err != nil {
		t.Fatal(err)
	}
	if !b.Settled() || len(l.Outstanding()) != 0 {
		t.Fatalf("bill should be settled")
	}
}

func TestOverpaymentRejected(t *testing.T) {
	l := New()
	l.Record("BILL-1", "Supplies Ltd", date(), gbp("100.00"))
	if err := l.Allocate("BILL-1", gbp("150.00")); err == nil {
		t.Fatal("expected an error paying more than outstanding")
	}
}
