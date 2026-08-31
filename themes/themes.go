// Package themes turns real-world business events into balanced ledger journals.
// Each sub-package (sales, expenses, …) offers domain operations — an Invoice, a
// Bill — that build a journal via Journal(); Post applies them to a book. The
// operations are the player's verbs: they wrap the postings proven in the cookbook
// behind validated, named APIs so no caller hand-writes debits and credits.
package themes

import "github.com/richardjennings/accounts/ledger"

// Operation is a business event that produces exactly one balanced journal.
type Operation interface {
	Journal() (ledger.Journal, error)
}

// Post builds each operation's journal and posts it to the book, in order,
// stopping at the first error.
func Post(book *ledger.Book, ops ...Operation) error {
	for _, op := range ops {
		j, err := op.Journal()
		if err != nil {
			return err
		}
		if err := book.Post(j); err != nil {
			return err
		}
	}
	return nil
}
