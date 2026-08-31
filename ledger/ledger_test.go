package ledger

import (
	"errors"
	"testing"
	"time"

	"github.com/richardjennings/accounts/money"
)

func gbp(s string) money.Money { return money.MustParse(money.GBP, s) }

func newTestBook(t *testing.T) *Book {
	t.Helper()
	b := NewBook(money.GBP)
	err := b.AddAccounts(
		Account{"1200", "Bank", Asset},
		Account{"1100", "Trade debtors", Asset},
		Account{"3000", "Share capital", Equity},
		Account{"4000", "Sales", Income},
	)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func mustPost(t *testing.T, b *Book, narrative string, ps ...Posting) {
	t.Helper()
	j, err := NewJournal(NewDate(2026, time.April, 6), narrative, ps...)
	if err != nil {
		t.Fatalf("NewJournal(%s): %v", narrative, err)
	}
	if err := b.Post(j); err != nil {
		t.Fatalf("Post(%s): %v", narrative, err)
	}
}

func TestPostAndBalances(t *testing.T) {
	b := newTestBook(t)
	mustPost(t, b, "Share issue",
		Posting{"1200", Debit, gbp("1000.00")},
		Posting{"3000", Credit, gbp("1000.00")})
	mustPost(t, b, "Credit sale",
		Posting{"1100", Debit, gbp("600.00")},
		Posting{"4000", Credit, gbp("600.00")})
	mustPost(t, b, "Customer receipt",
		Posting{"1200", Debit, gbp("600.00")},
		Posting{"1100", Credit, gbp("600.00")})

	want := map[string]string{
		"1200": "GBP 1600.00", // 1000 capital + 600 receipt
		"1100": "GBP 0.00",    // raised then settled
		"4000": "GBP 600.00",  // income, normal credit -> positive
		"3000": "GBP 1000.00", // equity, normal credit -> positive
	}
	for code, exp := range want {
		got, err := b.Balance(code)
		if err != nil {
			t.Fatal(err)
		}
		if got.String() != exp {
			t.Errorf("balance %s = %s, want %s", code, got, exp)
		}
	}
}

func TestTrialBalanceIsInBalance(t *testing.T) {
	b := newTestBook(t)
	mustPost(t, b, "Share issue",
		Posting{"1200", Debit, gbp("1000.00")},
		Posting{"3000", Credit, gbp("1000.00")})
	mustPost(t, b, "Cash sale",
		Posting{"1200", Debit, gbp("240.00")},
		Posting{"4000", Credit, gbp("240.00")})

	tb, err := b.TrialBalance()
	if err != nil {
		t.Fatal(err)
	}
	if !tb.InBalance() {
		t.Fatalf("trial balance not in balance: Dr %s vs Cr %s", tb.TotalDebit, tb.TotalCredit)
	}
	if tb.TotalDebit.String() != "GBP 1240.00" {
		t.Errorf("total debit = %s, want GBP 1240.00", tb.TotalDebit)
	}
}

func TestUnbalancedRejected(t *testing.T) {
	_, err := NewJournal(NewDate(2026, time.April, 6), "bad",
		Posting{"1200", Debit, gbp("100.00")},
		Posting{"4000", Credit, gbp("99.99")})
	if !errors.Is(err, ErrUnbalanced) {
		t.Fatalf("want ErrUnbalanced, got %v", err)
	}
}

func TestNonPositiveRejected(t *testing.T) {
	_, err := NewJournal(NewDate(2026, time.April, 6), "bad",
		Posting{"1200", Debit, gbp("0.00")},
		Posting{"4000", Credit, gbp("0.00")})
	if !errors.Is(err, ErrNonPositive) {
		t.Fatalf("want ErrNonPositive, got %v", err)
	}
}

func TestMixedCurrencyRejected(t *testing.T) {
	_, err := NewJournal(NewDate(2026, time.April, 6), "bad",
		Posting{"1200", Debit, money.MustParse(money.USD, "100.00")},
		Posting{"4000", Credit, gbp("100.00")})
	if !errors.Is(err, ErrMixedCurrency) {
		t.Fatalf("want ErrMixedCurrency, got %v", err)
	}
}

func TestPostUnknownAccountRejected(t *testing.T) {
	b := newTestBook(t)
	j, err := NewJournal(NewDate(2026, time.April, 6), "to nowhere",
		Posting{"9999", Debit, gbp("10.00")},
		Posting{"4000", Credit, gbp("10.00")})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Post(j); !errors.Is(err, ErrUnknownAccount) {
		t.Fatalf("want ErrUnknownAccount, got %v", err)
	}
}

func TestReversalUnwinds(t *testing.T) {
	b := newTestBook(t)
	j, err := NewJournal(NewDate(2026, time.April, 6), "Cash sale",
		Posting{"1200", Debit, gbp("50.00")},
		Posting{"4000", Credit, gbp("50.00")})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Post(j); err != nil {
		t.Fatal(err)
	}
	if err := b.Post(j.Reverse(NewDate(2026, time.April, 7), "Reverse cash sale")); err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{"1200", "4000"} {
		bal, err := b.Balance(code)
		if err != nil {
			t.Fatal(err)
		}
		if !bal.IsZero() {
			t.Errorf("after reversal %s = %s, want zero", code, bal)
		}
	}
}

func TestPostWrongCurrencyRejected(t *testing.T) {
	b := NewBook(money.USD)
	if err := b.AddAccounts(Account{"1200", "Bank", Asset}, Account{"4000", "Sales", Income}); err != nil {
		t.Fatal(err)
	}
	j, err := NewJournal(NewDate(2026, time.April, 6), "gbp into usd book",
		Posting{"1200", Debit, gbp("10.00")},
		Posting{"4000", Credit, gbp("10.00")})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Post(j); !errors.Is(err, ErrWrongCurrency) {
		t.Fatalf("want ErrWrongCurrency, got %v", err)
	}
}
