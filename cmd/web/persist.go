package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
	Closing   bool
	Section   string
	Ref       string
	Narrative string
	Principle string
	Date      ledger.Date
	Postings  []postingDTO
}

type invoiceLedgerDTO struct {
	Ref, Customer         string
	Date                  ledger.Date
	Total, Paid, Credited money.Money
}

type billLedgerDTO struct {
	Ref, Supplier         string
	Date                  ledger.Date
	Total, Paid, Credited money.Money
}

const snapshotVersion = 1

type snapshot struct {
	Version        int
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
	MileageRuns    []mileageRun
	Dividends      []dividendRun
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
		Version: snapshotVersion,
		Co:      a.co, Today: a.today, ClosedThrough: a.book.ClosedThrough(), Seq: a.seq, MainBank: a.mainBank,
		Banks: a.banks, Reg: a.reg, Costs: a.costs, StmtLines: a.stmtLines, Employees: a.employees, Assets: a.assets,
		StatementSpecs: a.statementSpecs,
		FXBalances:     a.fxBalances,
		Approvals:      a.approvals,
		PayrollRuns:    a.runs,
		MileageRuns:    a.mileageRuns,
		Dividends:      a.dividends,
	}
	for _, ref := range a.invoiceOrder {
		if d, ok := a.invoiceDocs[ref]; ok {
			s.InvoiceDocs = append(s.InvoiceDocs, d)
		}
	}
	for _, e := range a.entries {
		ed := entryDTO{Closing: e.j.IsClosing(), Section: e.section, Ref: e.j.Ref(), Narrative: e.j.Narrative(), Principle: e.principle, Date: e.j.Date()}
		for _, p := range e.j.Postings() {
			ed.Postings = append(ed.Postings, postingDTO{Account: p.Account, Debit: p.Side == ledger.Debit, Amount: p.Amount})
		}
		s.Entries = append(s.Entries, ed)
	}
	for _, inv := range a.sl.Invoices() {
		s.SalesInvoices = append(s.SalesInvoices, invoiceLedgerDTO{inv.Ref, inv.Customer, inv.Date, inv.Total, inv.Paid(), inv.Credited()})
	}
	for _, b := range a.purch.Bills() {
		s.PurchaseBills = append(s.PurchaseBills, billLedgerDTO{b.Ref, b.Supplier, b.Date, b.Total, b.Paid(), b.Credited()})
	}
	return s
}

// save writes the current state to the data file (atomically). It is a no-op when no
// data path is configured. It takes the lock itself, so call it after a handler has
// released it.
func (a *app) save() error {
	if a.dataPath == "" {
		return nil
	}
	// Keep both the snapshot and the write under the same lock: no alias can
	// change during encoding, and an older save cannot overtake a newer one.
	a.mu.Lock()
	defer a.mu.Unlock()
	data, err := json.MarshalIndent(a.buildSnapshot(), "", "  ")
	if err != nil {
		return fmt.Errorf("encode save: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(a.dataPath), 0700); err != nil {
		return err
	}
	if previous, err := os.ReadFile(a.dataPath); err == nil {
		if !json.Valid(previous) {
			return fmt.Errorf("existing save is invalid; preserved without overwriting")
		}
		if err := writeAtomic(a.dataPath+".bak", previous); err != nil {
			return fmt.Errorf("backup save: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return writeAtomic(a.dataPath, data)
}

// writeAtomic makes a complete, flushed file visible with one rename. Unique
// temporary names also prevent collisions with another writer's temporary file.
func writeAtomic(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".accounts-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
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
	if s.Version < 0 || s.Version > snapshotVersion {
		return fmt.Errorf("unsupported save version %d", s.Version)
	}
	cur, ok := money.Lookup(s.Co.Currency.Code)
	if !ok || cur != s.Co.Currency || s.Co.Incorporated.IsZero() || s.Co.YearEndMonth < 1 || s.Co.YearEndMonth > 12 || s.Co.YearEndDay < 1 || s.Co.YearEndDay > 31 {
		return fmt.Errorf("save has invalid company details")
	}
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
	var entries []entry
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
		if ed.Closing || (s.Version == 0 && strings.HasPrefix(ed.Narrative, "Year-end close ")) {
			j = j.AsClosing()
		}
		if err := book.Post(j); err != nil {
			return err
		}
		entries = append(entries, entry{ed.Section, j, ed.Principle})
	}

	invoiceDocs := map[string]*invoiceDoc{}
	var invoiceOrder []string
	for _, d := range s.InvoiceDocs {
		if d == nil {
			return fmt.Errorf("save contains a null invoice document")
		}
		invoiceDocs[d.Ref] = d
		invoiceOrder = append(invoiceOrder, d.Ref)
	}

	sl := salesledger.New()
	for _, inv := range s.SalesInvoices {
		if inv.Paid.IsNegative() || inv.Credited.IsNegative() {
			return fmt.Errorf("invoice %s has negative payments or credits", inv.Ref)
		}
		if _, err := sl.Raise(inv.Ref, inv.Customer, inv.Date, inv.Total); err != nil {
			return err
		}
		if inv.Credited.IsPositive() {
			if err := sl.Credit(inv.Ref, inv.Credited); err != nil {
				return err
			}
		}
		if inv.Paid.IsPositive() {
			if err := sl.Allocate(inv.Ref, inv.Paid); err != nil {
				return err
			}
		}
	}

	purch := purchaseledger.New()
	for _, b := range s.PurchaseBills {
		if b.Paid.IsNegative() || b.Credited.IsNegative() {
			return fmt.Errorf("bill %s has negative payments or credits", b.Ref)
		}
		if _, err := purch.Record(b.Ref, b.Supplier, b.Date, b.Total); err != nil {
			return err
		}
		if b.Credited.IsPositive() {
			if err := purch.Credit(b.Ref, b.Credited); err != nil {
				return err
			}
		}
		if b.Paid.IsPositive() {
			if err := purch.Allocate(b.Ref, b.Paid); err != nil {
				return err
			}
		}
	}
	// Older saves did not record the last depreciation period. Preserve their
	// accumulated charges and conservatively use the latest depreciation date;
	// this prevents charging that period again after upgrading.
	var latestDep ledger.Date
	if s.Version == 0 {
		for _, e := range entries {
			if strings.HasPrefix(e.j.Narrative(), "Depreciation ") && latestDep.Before(e.j.Date()) {
				latestDep = e.j.Date()
			}
		}
	}
	assets := make([]*assetHolding, len(s.Assets))
	for i, h := range s.Assets {
		if h == nil {
			return fmt.Errorf("save contains a null asset")
		}
		copied := *h
		if s.Version == 0 && h.Accumulated.IsPositive() && h.DepreciatedThrough.IsZero() && !latestDep.IsZero() {
			copied.DepreciatedThrough = s.Co.YearContaining(latestDep).End
			periods := map[ledger.Date]bool{}
			for _, e := range entries {
				if strings.HasPrefix(e.j.Narrative(), "Depreciation ") && !e.j.Date().Before(h.Asset.Acquired) {
					periods[s.Co.YearContaining(e.j.Date()).End] = true
				}
			}
			copied.DepreciationYears = len(periods)
		}
		assets[i] = &copied
	}
	book.CloseThrough(s.ClosedThrough)
	a.co, a.today, a.seq, a.mainBank = s.Co, s.Today, s.Seq, s.MainBank
	a.banks, a.reg, a.costs, a.employees, a.assets = s.Banks, s.Reg, s.Costs, s.Employees, assets
	a.statementSpecs = s.StatementSpecs
	a.fxBalances = s.FXBalances
	a.stmtLines = s.StmtLines
	a.approvals = s.Approvals
	a.runs = s.PayrollRuns
	a.mileageRuns = s.MileageRuns
	a.dividends = s.Dividends
	a.book = book

	a.entries = entries
	a.sl, a.purch = sl, purch
	a.invoiceDocs, a.invoiceOrder = invoiceDocs, invoiceOrder

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
