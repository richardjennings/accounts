package main

import (
	"bytes"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/richardjennings/accounts/chart"
	"github.com/richardjennings/accounts/dividends"
)

// rpc sends one request to the server and returns its response.
func rpc(t *testing.T, s *mcpServer, method string, params any) rpcResponse {
	t.Helper()
	req := map[string]any{"jsonrpc": "2.0", "id": 1, "method": method}
	if params != nil {
		req["params"] = params
	}
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	resp, ok := s.handle(raw)
	if !ok {
		t.Fatalf("%s: no response", method)
	}
	return resp
}

// callTool runs a tool and decodes the JSON text it returns.
func callTool(t *testing.T, s *mcpServer, name string, args map[string]any) map[string]any {
	t.Helper()
	resp := rpc(t, s, "tools/call", map[string]any{"name": name, "arguments": args})
	if resp.Error != nil {
		t.Fatalf("%s: %s", name, resp.Error.Message)
	}
	res := resp.Result.(map[string]any)
	text := res["content"].([]map[string]any)[0]["text"].(string)
	if res["isError"] == true {
		t.Fatalf("%s: tool error: %s", name, text)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("%s: %v in %q", name, err, text)
	}
	return out
}

// toolError runs a tool that is expected to fail and returns its message.
func toolError(t *testing.T, s *mcpServer, name string, args map[string]any) string {
	t.Helper()
	resp := rpc(t, s, "tools/call", map[string]any{"name": name, "arguments": args})
	if resp.Error != nil {
		t.Fatalf("%s: protocol error %s", name, resp.Error.Message)
	}
	res := resp.Result.(map[string]any)
	if res["isError"] != true {
		t.Fatalf("%s: expected a tool error", name)
	}
	return res["content"].([]map[string]any)[0]["text"].(string)
}

func TestMCPLifecycle(t *testing.T) {
	s, err := newMCPServer("")
	if err != nil {
		t.Fatal(err)
	}

	init := rpc(t, s, "initialize", map[string]any{"protocolVersion": "2025-03-26", "capabilities": map[string]any{}})
	res := init.Result.(map[string]any)
	if res["protocolVersion"] != "2025-03-26" {
		t.Errorf("protocolVersion = %v, want the client's", res["protocolVersion"])
	}
	if info := res["serverInfo"].(map[string]any); info["name"] != mcpServerName {
		t.Errorf("serverInfo = %v", info)
	}
	if _, ok := res["capabilities"].(map[string]any)["tools"]; !ok {
		t.Error("initialize does not advertise tools")
	}
	if res["instructions"] == "" {
		t.Error("initialize carries no instructions")
	}
	res = rpc(t, s, "initialize", map[string]any{"protocolVersion": "2099-01-01"}).Result.(map[string]any)
	if res["protocolVersion"] != mcpVersions[0] {
		t.Errorf("unknown client version negotiated to %v, want %s", res["protocolVersion"], mcpVersions[0])
	}

	if _, ok := s.handle(json.RawMessage(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)); ok {
		t.Error("a notification got a response")
	}
	if _, ok := s.handle(json.RawMessage(`{"jsonrpc":"2.0","id":9,"result":{}}`)); ok {
		t.Error("a stray response got a response")
	}
	if r, ok := s.handle(json.RawMessage(`[1,2]`)); !ok || r.Error == nil || r.Error.Code != rpcInvalidRequest {
		t.Errorf("batch = %+v, want invalid request", r)
	}

	if pong := rpc(t, s, "ping", nil); pong.Error != nil || len(pong.Result.(map[string]any)) != 0 {
		t.Errorf("ping = %+v", pong)
	}
	if r := rpc(t, s, "resources/list", nil); r.Error == nil || r.Error.Code != rpcMethodNotFound {
		t.Errorf("unknown method = %+v, want method not found", r)
	}
	if r := rpc(t, s, "tools/call", map[string]any{"name": "nope"}); r.Error == nil || r.Error.Code != rpcInvalidParams {
		t.Errorf("unknown tool = %+v, want invalid params", r)
	}

	list := rpc(t, s, "tools/list", nil).Result.(map[string]any)["tools"].([]map[string]any)
	want := []string{"company", "position", "dividend_capacity", "dividends", "profit_and_loss", "balance_sheet",
		"trial_balance", "journals", "invoices", "bills", "payroll", "corporation_tax"}
	if len(list) != len(want) {
		t.Fatalf("tools/list returned %d tools, want %d", len(list), len(want))
	}
	for i, tool := range list {
		if tool["name"] != want[i] {
			t.Errorf("tool %d = %v, want %s", i, tool["name"], want[i])
		}
		if tool["annotations"].(map[string]any)["readOnlyHint"] != true {
			t.Errorf("tool %v is not marked read only", tool["name"])
		}
		if _, ok := tool["inputSchema"].(map[string]any)["properties"]; !ok {
			t.Errorf("tool %v has no input schema", tool["name"])
		}
	}

	// Every tool answers over the default company.
	for _, name := range want {
		callTool(t, s, name, nil)
	}
	co := callTool(t, s, "company", nil)
	if co["name"] != "Your Company Ltd" || co["today"] != "2026-06-01" {
		t.Errorf("company = %v / %v", co["name"], co["today"])
	}
	if note := co["source"].(map[string]any)["note"]; !strings.Contains(note.(string), "no save file") {
		t.Errorf("source = %v", co["source"])
	}

	if msg := toolError(t, s, "journals", map[string]any{"limit": "ten"}); !strings.Contains(msg, "arguments") {
		t.Errorf("bad argument type: %q", msg)
	}
	if msg := toolError(t, s, "journals", map[string]any{"limt": 5}); !strings.Contains(msg, "unknown field") {
		t.Errorf("unknown argument: %q", msg)
	}
	if msg := toolError(t, s, "balance_sheet", map[string]any{"as_at": "1/2/2026"}); !strings.Contains(msg, "YYYY-MM-DD") {
		t.Errorf("bad date: %q", msg)
	}
	if msg := toolError(t, s, "dividend_capacity", map[string]any{"proposed": "lots"}); !strings.Contains(msg, "proposed") {
		t.Errorf("bad amount: %q", msg)
	}
}

func TestMCPServeFraming(t *testing.T) {
	s, err := newMCPServer("")
	if err != nil {
		t.Fatal(err)
	}
	script := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}` + "\n" +
		`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n" +
		`{"jsonrpc":"2.0","id":"two","method":"ping"}` + "\n"
	var out bytes.Buffer
	if err := s.serve(strings.NewReader(script), &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d response lines, want 2: %q", len(lines), out.String())
	}
	for i, want := range []string{"1", `"two"`} {
		var resp rpcResponse
		if err := json.Unmarshal([]byte(lines[i]), &resp); err != nil {
			t.Fatalf("line %d: %v", i, err)
		}
		if string(resp.ID) != want || resp.Error != nil {
			t.Errorf("line %d = %s", i, lines[i])
		}
	}

	out.Reset()
	if err := s.serve(strings.NewReader(`{"jsonrpc":`), &out); err == nil {
		t.Error("a truncated message did not stop the server")
	}
	if !strings.Contains(out.String(), `"id":null`) || !strings.Contains(out.String(), "parse error") {
		t.Errorf("parse error response = %q", out.String())
	}
}

// seedCompany records a cash sale, then declares and pays a dividend, through
// the web handlers so the save file is written the way the UI writes it.
func seedCompany(t *testing.T, path string) *app {
	t.Helper()
	a, err := newApp(path)
	if err != nil {
		t.Fatal(err)
	}
	h := a.persistMiddleware(a.routes())
	drive(t, h, "/sales/cash/record", url.Values{"amount": {"10000"}, "date": {"2026-05-10"}})
	drive(t, h, "/pay-yourself/dividends/declare", url.Values{"amount": {"4000"}, "date": {"2026-06-01"}})
	drive(t, h, "/pay-yourself/dividends/pay", url.Values{"amount": {"4000"}, "date": {"2026-06-02"}})
	if len(a.dividends) != 1 {
		t.Fatalf("seed: %d dividends declared, flash %q", len(a.dividends), a.flash)
	}
	return a
}

func TestMCPDividendCapacityAndHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	a := seedCompany(t, path)
	s, err := newMCPServer(path)
	if err != nil {
		t.Fatal(err)
	}

	hist := callTool(t, s, "dividends", nil)
	last := hist["last_declared"].(map[string]any)
	if last["date"] != "2026-06-01" || last["amount"] != "4000.00" || last["voucher"] != true || !strings.HasPrefix(last["ref"].(string), "DIV-") {
		t.Errorf("last_declared = %v", last)
	}
	if awards := last["awards"].([]any); len(awards) != 1 || awards[0].(map[string]any)["amount"] != "4000.00" {
		t.Errorf("awards = %v", last["awards"])
	}
	paid := hist["last_paid"].(map[string]any)
	if paid["date"] != "2026-06-02" || paid["amount"] != "4000.00" || !strings.HasPrefix(paid["ref"].(string), "DVP-") {
		t.Errorf("last_paid = %v", paid)
	}
	year := hist["financial_year"].(map[string]any)
	if year["declared"] != "4000.00" || year["paid"] != "4000.00" {
		t.Errorf("financial_year = %v", year)
	}

	cap := callTool(t, s, "dividend_capacity", map[string]any{"proposed": "1000000"})
	reserves, err := dividends.Available(a.book, a.fy().End)
	if err != nil {
		t.Fatal(err)
	}
	if cap["distributable_reserves"] != amt(reserves) || amt(reserves) != "6000.00" {
		t.Errorf("distributable_reserves = %v, want %s", cap["distributable_reserves"], amt(reserves))
	}
	ct := cap["corporation_tax"].(map[string]any)
	if ct["estimated_charge"] != amt(a.estimateCT().Charge) || ct["provided_so_far"] != "0.00" {
		t.Errorf("corporation_tax = %v", ct)
	}
	unprovided := a.estimateCT().Charge
	prudent, _ := reserves.Sub(unprovided)
	if cap["prudent_maximum"] != amt(prudent) {
		t.Errorf("prudent_maximum = %v, want %s", cap["prudent_maximum"], amt(prudent))
	}
	holders := cap["shareholders"].([]any)
	if len(holders) != 1 || holders[0].(map[string]any)["share_of_prudent_maximum"] != amt(prudent) || holders[0].(map[string]any)["holding_pct"] != "100.0" {
		t.Errorf("shareholders = %v", holders)
	}
	if cap["directors_loan"] != "0.00" {
		t.Errorf("directors_loan = %v, want 0.00 once the dividend is paid", cap["directors_loan"])
	}
	cash := cap["cash"].(map[string]any)
	if cash["main_account"].(map[string]any)["balance"] != amt(a.bal(a.main())) {
		t.Errorf("cash = %v", cash)
	}
	proposed := cap["proposed"].(map[string]any)
	if proposed["lawful"] != false || proposed["shortfall"] == "0.00" {
		t.Errorf("proposed = %v, want unlawful with a shortfall", proposed)
	}
	if p := callTool(t, s, "dividend_capacity", map[string]any{"proposed": "1"})["proposed"].(map[string]any); p["lawful"] != true || p["shortfall"] != "0.00" {
		t.Errorf("proposed 1 = %v", p)
	}

	// The journals tool finds the same two postings by reference prefix.
	js := callTool(t, s, "journals", map[string]any{"ref": "dv", "limit": 1})
	if js["matched"] != float64(1) || js["returned"] != float64(1) {
		t.Errorf("journals ref=dv: %v", js)
	}
	j := js["journals"].([]any)[0].(map[string]any)
	if j["section"] != "pay-yourself" || len(j["postings"].([]any)) != 2 {
		t.Errorf("journal = %v", j)
	}
	if by := callTool(t, s, "journals", map[string]any{"search": "dividend"}); by["matched"] != float64(2) {
		t.Errorf("journals search=dividend matched %v, want 2", by["matched"])
	}
	if pos := callTool(t, s, "position", nil); pos["total_bank"] != amt(a.bal(chart.Bank)) {
		t.Errorf("position total_bank = %v, want %s", pos["total_bank"], amt(a.bal(chart.Bank)))
	}
}

func TestMCPReloadsOnSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	a := seedCompany(t, path)
	s, err := newMCPServer(path)
	if err != nil {
		t.Fatal(err)
	}
	before := callTool(t, s, "position", nil)["total_bank"]
	if before != amt(a.bal(chart.Bank)) {
		t.Fatalf("total_bank = %v, want %s", before, amt(a.bal(chart.Bank)))
	}
	if saved := callTool(t, s, "company", nil)["source"].(map[string]any)["saved"]; saved == nil || saved == "" {
		t.Error("source.saved is empty for a loaded save file")
	}

	h := a.persistMiddleware(a.routes())
	drive(t, h, "/sales/cash/record", url.Values{"amount": {"500"}, "date": {"2026-05-01"}})
	after := callTool(t, s, "position", nil)["total_bank"]
	if after == before || after != amt(a.bal(chart.Bank)) {
		t.Errorf("total_bank after the UI saved = %v, want %s (was %v)", after, amt(a.bal(chart.Bank)), before)
	}

	// The sale was posted last but dated earliest, so it comes last, not first.
	js := callTool(t, s, "journals", map[string]any{"limit": 10})
	dates := []string{}
	for _, j := range js["journals"].([]any) {
		dates = append(dates, j.(map[string]any)["date"].(string))
	}
	if js["matched"] != float64(5) || len(dates) != 5 || dates[0] != "2026-06-02" || dates[4] != "2026-04-01" || dates[3] != "2026-05-01" {
		t.Errorf("journals dates = %v, want latest first", dates)
	}
}

func TestMCPNeverWritesSaveFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s, err := newMCPServer(path)
	if err != nil {
		t.Fatal(err)
	}
	co := callTool(t, s, "company", nil)
	if src := co["source"].(map[string]any); src["save_file"] != path || !strings.Contains(src["note"].(string), "no save file yet") {
		t.Errorf("source = %v", src)
	}
	callTool(t, s, "position", nil)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("the MCP server created %s", path)
	}
}
