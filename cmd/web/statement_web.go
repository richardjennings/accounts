package main

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/richardjennings/accounts/importer"
	"github.com/richardjennings/accounts/money"
	"github.com/richardjennings/xls"
)

// maxStatementFile bounds an uploaded statement file.
const maxStatementFile = 8 << 20

// pendingStatement is an uploaded statement awaiting its column mapping. It
// lives in memory only: a restart drops it and the user uploads again.
type pendingStatement struct {
	BankCode string
	FileName string
	Table    *importer.Table
	Spec     importer.StatementSpec
}

// stmtUploadView is what the mapping page shows.
type stmtUploadView struct {
	BankName, FileName string
	Headers            []string
	Spec               importer.StatementSpec
	Preview            []stmtPreviewLine
	Issues             []string
	Lines, Skipped     int
	Total              money.Money
	SpecErr            string
}

type stmtPreviewLine struct {
	Date, Desc string
	Amount     money.Money
	HasBalance bool
	Balance    money.Money
}

// statementTable reads an uploaded statement file: a BIFF8 workbook by its
// signature, otherwise CSV.
func statementTable(name string, data []byte) (*importer.Table, error) {
	if len(data) >= 8 && bytes.HasPrefix(data, []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}) {
		wb, err := xls.Open(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return nil, err
		}
		if len(wb.Sheets) == 0 {
			return nil, fmt.Errorf("the workbook has no sheets")
		}
		return importer.FromXLS(name, wb.Sheets[0]), nil
	}
	cr := csv.NewReader(bytes.NewReader(data))
	cr.FieldsPerRecord = -1
	cr.TrimLeadingSpace = true
	recs, err := cr.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("could not read the file as CSV: %v", err)
	}
	if len(recs) < 2 {
		return nil, fmt.Errorf("the file has no data rows")
	}
	return importer.FromCSV(name, recs), nil
}

// stmtUploadView builds the mapping page's view, or nil when nothing is pending.
func (a *app) stmtUploadView() *stmtUploadView {
	p := a.pendingStmt
	if p == nil {
		return nil
	}
	v := &stmtUploadView{BankName: a.bankName(p.BankCode), FileName: p.FileName, Headers: p.Table.Header, Spec: p.Spec, Total: money.Zero(a.co.Currency)}
	if err := p.Spec.Validate(p.Table); err != nil {
		v.SpecErr = err.Error()
		return v
	}
	lines, issues := importer.ReadStatement(p.Table, p.Spec, a.co.Currency)
	v.Lines, v.Skipped = len(lines), len(issues)
	for i, l := range lines {
		v.Total, _ = v.Total.Add(l.Amount)
		if i < 8 {
			v.Preview = append(v.Preview, stmtPreviewLine{Date: l.Date.String(), Desc: l.Description, Amount: l.Amount, HasBalance: l.HasBalance, Balance: l.Balance})
		}
	}
	for i, is := range issues {
		if i >= 5 {
			v.Issues = append(v.Issues, fmt.Sprintf("… and %d more", len(issues)-5))
			break
		}
		v.Issues = append(v.Issues, is.String())
	}
	return v
}

func (a *app) bankName(code string) string {
	for _, b := range a.banks {
		if b.Code == code {
			return b.Name
		}
	}
	return code
}

// statementSpecFromForm reads the mapping form.
func statementSpecFromForm(r *http.Request) importer.StatementSpec {
	return importer.StatementSpec{
		Date:        r.FormValue("col_date"),
		Description: r.FormValue("col_desc"),
		Amount:      r.FormValue("col_amount"),
		In:          r.FormValue("col_in"),
		Out:         r.FormValue("col_out"),
		Balance:     r.FormValue("col_balance"),
		DateOrder:   r.FormValue("date_order"),
		Negate:      r.FormValue("negate") != "",
	}
}

// statementRoutes wires the statement-import flow.
func (a *app) statementRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/banking/statements/upload", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Redirect(w, r, "/banking/statements", http.StatusSeeOther)
			return
		}
		var data []byte
		name := "statement"
		if f, hdr, err := r.FormFile("file"); err == nil {
			defer f.Close()
			data, _ = io.ReadAll(io.LimitReader(f, maxStatementFile))
			name = hdr.Filename
		} else if text := strings.TrimSpace(r.FormValue("csv")); text != "" {
			data = []byte(text)
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		if len(data) == 0 {
			a.flash = "⚠ choose a statement file (.csv or .xls) or paste CSV"
			http.Redirect(w, r, "/banking/statements", http.StatusSeeOther)
			return
		}
		t, err := statementTable(name, data)
		if err != nil {
			a.flash = "⚠ " + err.Error()
			http.Redirect(w, r, "/banking/statements", http.StatusSeeOther)
			return
		}
		bank := a.bankCode(r)
		spec := importer.DetectStatement(t)
		// A preset saved for this account wins when its columns fit this file.
		if saved, ok := a.statementSpecs[bank]; ok && saved.Validate(t) == nil {
			spec = saved
		}
		a.pendingStmt = &pendingStatement{BankCode: bank, FileName: name, Table: t, Spec: spec}
		a.flash = "Check the column mapping, then import"
		http.Redirect(w, r, "/banking/statements", http.StatusSeeOther)
	})
	mux.HandleFunc("/banking/statements/map", func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		defer a.mu.Unlock()
		if r.Method == http.MethodPost && a.pendingStmt != nil {
			a.pendingStmt.Spec = statementSpecFromForm(r)
		}
		http.Redirect(w, r, "/banking/statements", http.StatusSeeOther)
	})
	mux.HandleFunc("/banking/statements/cancel", func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		a.pendingStmt = nil
		a.flash = "Upload discarded"
		a.mu.Unlock()
		http.Redirect(w, r, "/banking/statements", http.StatusSeeOther)
	})
	mux.HandleFunc("/banking/statements/confirm", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Redirect(w, r, "/banking/statements", http.StatusSeeOther)
			return
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		p := a.pendingStmt
		if p == nil {
			http.Redirect(w, r, "/banking/statements", http.StatusSeeOther)
			return
		}
		if err := p.Spec.Validate(p.Table); err != nil {
			a.flash = "⚠ " + err.Error()
			http.Redirect(w, r, "/banking/statements", http.StatusSeeOther)
			return
		}
		lines, issues := importer.ReadStatement(p.Table, p.Spec, a.co.Currency)
		// Overlapping exports are safe: a line equal to one already imported is a
		// duplicate. Counts, not a set — a statement may legitimately hold two
		// identical movements, so only as many copies as already exist are skipped.
		existing := map[string]int{}
		for _, l := range a.stmtLines {
			if l.BankCode == p.BankCode {
				existing[l.Date.String()+"|"+l.Desc+"|"+l.Amount.String()]++
			}
		}
		added, dup := 0, 0
		for _, l := range lines {
			key := l.Date.String() + "|" + l.Description + "|" + l.Amount.String()
			if existing[key] > 0 {
				existing[key]--
				dup++
				continue
			}
			a.stmtLines = append(a.stmtLines, &stmtLine{BankCode: p.BankCode, Date: l.Date, Desc: l.Description, Amount: l.Amount, Balance: l.Balance, HasBalance: l.HasBalance})
			added++
		}
		if a.statementSpecs == nil {
			a.statementSpecs = map[string]importer.StatementSpec{}
		}
		a.statementSpecs[p.BankCode] = p.Spec // next time this account's file maps itself
		bank := a.bankName(p.BankCode)
		a.pendingStmt = nil
		a.flash = fmt.Sprintf("✓ Imported %d statement line(s) for %s; %d duplicate(s) and %d unreadable row(s) skipped. Reconcile them under Banking → Reconcile.", added, bank, dup, len(issues))
		http.Redirect(w, r, "/banking/reconcile", http.StatusSeeOther)
	})
}
