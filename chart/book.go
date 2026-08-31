package chart

import (
	"github.com/richardjennings/accounts/ledger"
	"github.com/richardjennings/accounts/money"
)

// NewUKMicroLtdBook returns a ledger book in the given base currency, seeded with
// the UKMicroLtd chart of accounts — a ready-to-post set of books.
func NewUKMicroLtdBook(base money.Currency) (*ledger.Book, error) {
	b := ledger.NewBook(base)
	if err := b.AddAccounts(UKMicroLtd()...); err != nil {
		return nil, err
	}
	return b, nil
}
