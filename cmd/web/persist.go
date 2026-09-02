package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"

	"github.com/richardjennings/accounts/chart"
	"github.com/richardjennings/accounts/company"
	"github.com/richardjennings/accounts/importer"
	"github.com/richardjennings/accounts/ledger"
	"github.com/richardjennings/accounts/money"
	"github.com/richardjennings/accounts/purchaseledger"
	"github.com/richardjennings/accounts/register"
	"github.com/richardjennings/accounts/salesledger"
)

// The snapshot is the persisted form of a company. The general ledger is stored as
// its posted journals and rebuilt by replay on load — the journals are the source of
// truth, so balances are never persisted directly. The subsidiary ledgers keep an
// unexported paid amount, so they travel as small DTOs.

type postingDTO struct {
	Account string
	Debit   bool
	Amount  money.Money
}

type entryDTO struct {
	Section   string
	Ref       string
	Narrative string
	Principle string
	Date      ledger.Date
	Postings  []postingDTO
}

type invoiceLedgerDTO struct {
	Ref, Customer string
	Date          ledger.Date
	Total, Paid   money.Money
}

type billLedgerDTO struct {
	Ref, Supplier string
	Date          ledger.Date
	Total, Paid   money.Money
}

type snapshot struct {
	Co             company.Company
	Today          ledger.Date
	ClosedThrough  ledger.Date
	Seq            int
	MainBank       string
	Banks          []bankAcct
	Reg            register.Register
	Costs          []*costRecord
	StmtLines      []*stmtLine
	Employees      []*employee
	PayrollRuns    []payrollRun
	Assets         []*assetHolding
	InvoiceDocs    []*invoiceDoc
	Approvals      []accountsApproval
	Entries        []entryDTO
	SalesInvoices  []invoiceLedgerDTO
	PurchaseBills  []billLedgerDTO
	StatementSpecs map[string]importer.StatementSpec
	FXBalances     map[string]money.Money
}

// snapshot builds the persisted form of the current state. The caller holds a.mu.
func (a *app) buildSnapshot() snapshot {
	s := snapshot{
		Co: a.co, Today: a.today, ClosedThrough: a.closedThrough, Seq: a.seq, MainBank: a.mainBank,
		Banks: a.banks, Reg: a.reg, Costs: a.costs, StmtLines: a.stmtLines, Employees: a.employees, Assets: a.assets,
		StatementSpecs: a.statementSpecs,
		FXBalances:     a.fxBalances,
		Approvals:      a.approvals,
		PayrollRuns:    a.runs,
	}
	for _, ref := range a.invoiceOrder {
		if d, ok := a.invoiceDocs[ref]; ok {
			s.InvoiceDocs = append(s.InvoiceDocs, d)
		}
	}
	for _, e := range a.entries {
		ed := entryDTO{Section: e.section, Ref: e.j.Ref(), Narrative: e.j.Narrative(), Principle: e.principle, Date: e.j.Date()}
		for _, p := range e.j.Postings() {
			ed.Postings = append(ed.Postings, postingDTO{Account: p.Account, Debit: p.Side == ledger.Debit, Amount: p.Amount})
		}
		s.Entries = append(s.Entries, ed)
	}
	for _, inv := range a.sl.Invoices() {
		s.SalesInvoices = append(s.SalesInvoices, invoiceLedgerDTO{inv.Ref, inv.Customer, inv.Date, inv.Total, inv.Paid()})
	}
	for _, b := range a.purch.Bills() {
		s.PurchaseBills = append(s.PurchaseBills, billLedgerDTO{b.Ref, b.Supplier, b.Date, b.Total, b.Paid()})
	}
	return s
}

// save writes the current state to the data file (atomically). It is a no-op when no
// data path is configured. It takes the lock itself, so call it after a handler has
// released it.
func (a *app) save() {
	if a.dataPath == "" {
		return
	}
	a.mu.Lock()
	s := a.buildSnapshot()
	a.mu.Unlock()

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		log.Printf("save: marshal: %v", err)
		return
	}
	if err := os.MkdirAll(filepath.Dir(a.dataPath), 0o755); err != nil {
		log.Printf("save: mkdir: %v", err)
		return
	}
	tmp := a.dataPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		log.Printf("save: write: %v", err)
		return
	}
	if err := os.Rename(tmp, a.dataPath); err != nil {
		log.Printf("save: rename: %v", err)
	}
}

func loadSnapshot(path string) (*snapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s snapshot
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// restore rebuilds the app state from a snapshot: a fresh book with the saved bank
// accounts, every journal replayed, and the subsidiary ledgers and registers
// reinstated. The general-ledger balances fall out of the replay.
func (a *app) restore(s *snapshot) error {
	book, err := chart.NewUKMicroLtdBook(s.Co.Currency)
	if err != nil {
		return err
	}
	for _, bk := range s.Banks {
		if _, ok := book.Account(bk.Code); !ok {
			if err := book.AddAccount(ledger.Account{Code: bk.Code, Name: bk.Name, Type: ledger.Asset}); err != nil {
				return err
			}
		}
	}
	a.entries = nil
	for _, ed := range s.Entries {
		postings := make([]ledger.Posting, 0, len(ed.Postings))
		for _, p := range ed.Postings {
			side := ledger.Credit
			if p.Debit {
				side = ledger.Debit
			}
			postings = append(postings, ledger.Posting{Account: p.Account, Side: side, Amount: p.Amount})
		}
		j, err := ledger.NewJournal(ed.Date, ed.Narrative, postings...)
		if err != nil {
			return err
		}
		j = j.WithRef(ed.Ref)
		if err := book.Post(j); err != nil {
			return err
		}
		a.entries = append(a.entries, entry{ed.Section, j, ed.Principle})
	}

	a.co, a.today, a.closedThrough, a.seq, a.mainBank = s.Co, s.Today, s.ClosedThrough, s.Seq, s.MainBank
	a.banks, a.reg, a.costs, a.employees, a.assets = s.Banks, s.Reg, s.Costs, s.Employees, s.Assets
	a.statementSpecs = s.StatementSpecs
	a.fxBalances = s.FXBalances
	a.stmtLines = s.StmtLines
	a.approvals = s.Approvals
	a.runs = s.PayrollRuns
	a.book = book

	a.invoiceDocs = map[string]*invoiceDoc{}
	a.invoiceOrder = nil
	for _, d := range s.InvoiceDocs {
		a.invoiceDocs[d.Ref] = d
		a.invoiceOrder = append(a.invoiceOrder, d.Ref)
	}

	a.sl = salesledger.New()
	for _, inv := range s.SalesInvoices {
		if _, err := a.sl.Raise(inv.Ref, inv.Customer, inv.Date, inv.Total); err != nil {
			return err
		}
		if inv.Paid.IsPositive() {
			if err := a.sl.Allocate(inv.Ref, inv.Paid); err != nil {
				return err
			}
		}
	}

	a.purch = purchaseledger.New()
	for _, b := range s.PurchaseBills {
		if _, err := a.purch.Record(b.Ref, b.Supplier, b.Date, b.Total); err != nil {
			return err
		}
		if b.Paid.IsPositive() {
			if err := a.purch.Allocate(b.Ref, b.Paid); err != nil {
				return err
			}
		}
	}
	return nil
}

// defaultDataPath is the per-user save location, or "" if it cannot be determined.
func defaultDataPath() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		return ""
	}
	return filepath.Join(dir, "virtual-accounts", "state.json")
}
