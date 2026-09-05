package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"time"
)

// MCP mode serves the books to a Model Context Protocol client over stdio,
// read only: the tools answer questions and never post. Messages are JSON-RPC
// 2.0, one per line. The server reads the save file the web UI writes and
// loads it again whenever the file changes, so an answer always reflects the
// latest save. It never writes the save file.

const (
	mcpServerName    = "accounts"
	mcpServerVersion = "0.1.0"
)

// mcpVersions are the protocol revisions the server speaks, newest first.
var mcpVersions = []string{"2025-06-18", "2025-03-26", "2024-11-05"}

const mcpInstructions = "Read-only view of the books of one virtual UK limited company. " +
	"Every tool reads the save file the web UI writes, so figures match the UI. " +
	"Money is a decimal string in the company currency; dates are YYYY-MM-DD. " +
	"Start with company for the game date and financial year, position for balances, " +
	"and dividend_capacity for how much dividend the company can pay."

// JSON-RPC 2.0 error codes.
const (
	rpcParseError     = -32700
	rpcInvalidRequest = -32600
	rpcMethodNotFound = -32601
	rpcInvalidParams  = -32602
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// mcpServer answers MCP requests from the books in app. stamp records the save
// file as last loaded, so reload notices a change.
type mcpServer struct {
	path  string // save file; "" serves the default in-memory company
	app   *app
	stamp fileStamp
}

// fileStamp identifies one version of the save file.
type fileStamp struct {
	size    int64
	modTime time.Time
}

func (f fileStamp) same(g fileStamp) bool { return f.size == g.size && f.modTime.Equal(g.modTime) }

// newMCPServer prepares a server over the save file at path. A missing file
// serves the default company; a file that cannot be loaded is an error.
func newMCPServer(path string) (*mcpServer, error) {
	a, err := newApp("")
	if err != nil {
		return nil, err
	}
	s := &mcpServer{path: path, app: a}
	if err := s.reload(); err != nil {
		return nil, err
	}
	return s, nil
}

// reload loads the save file when it has changed since the last load. A load
// that fails leaves the previous state in place and returns the error.
func (s *mcpServer) reload() error {
	if s.path == "" {
		return nil
	}
	st, err := os.Stat(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	cur := fileStamp{size: st.Size(), modTime: st.ModTime()}
	if cur.same(s.stamp) {
		return nil
	}
	snap, err := loadSnapshot(s.path)
	if err != nil {
		return err
	}
	fresh, err := newApp("")
	if err != nil {
		return err
	}
	if err := fresh.restore(snap); err != nil {
		return err
	}
	s.app, s.stamp = fresh, cur
	return nil
}

// serve answers the requests on r until it ends, one response line per
// request. Notifications get no response.
func (s *mcpServer) serve(r io.Reader, w io.Writer) error {
	dec := json.NewDecoder(r)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	for {
		var raw json.RawMessage
		err := dec.Decode(&raw)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			_ = enc.Encode(rpcResponse{JSONRPC: "2.0", Error: &rpcError{rpcParseError, "parse error: " + err.Error()}})
			return err
		}
		if resp, ok := s.handle(raw); ok {
			if err := enc.Encode(resp); err != nil {
				return err
			}
		}
	}
}

// handle answers one message. The second result is false when the message
// needs no response: a notification, or a reply to a request the server
// never sent.
func (s *mcpServer) handle(raw json.RawMessage) (rpcResponse, bool) {
	var req rpcRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return rpcResponse{JSONRPC: "2.0", Error: &rpcError{rpcInvalidRequest, "invalid request"}}, true
	}
	if req.Method == "" || len(req.ID) == 0 || string(req.ID) == "null" {
		return rpcResponse{}, false
	}
	result, rerr := s.dispatch(req.Method, req.Params)
	if rerr != nil {
		return rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: rerr}, true
	}
	return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}, true
}

// dispatch runs one request and returns its result, or the protocol error.
func (s *mcpServer) dispatch(method string, params json.RawMessage) (any, *rpcError) {
	switch method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(params, &p)
		return map[string]any{
			"protocolVersion": negotiate(p.ProtocolVersion),
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]any{"name": mcpServerName, "version": mcpServerVersion},
			"instructions":    mcpInstructions,
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": mcpToolList()}, nil
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(params, &p); err != nil || p.Name == "" {
			return nil, &rpcError{rpcInvalidParams, "tools/call needs the name of a tool"}
		}
		t, ok := findTool(p.Name)
		if !ok {
			return nil, &rpcError{rpcInvalidParams, "unknown tool: " + p.Name}
		}
		return s.call(t, p.Arguments), nil
	}
	return nil, &rpcError{rpcMethodNotFound, "method not found: " + method}
}

// negotiate picks the protocol revision: the client's when the server speaks
// it, otherwise the newest the server speaks.
func negotiate(requested string) string {
	for _, v := range mcpVersions {
		if v == requested {
			return v
		}
	}
	return mcpVersions[0]
}

// call runs one tool over the latest save. A failure is a tool result with
// isError set, not a protocol error, so the client sees the reason.
func (s *mcpServer) call(t mcpTool, args json.RawMessage) map[string]any {
	if err := s.reload(); err != nil {
		return toolText("could not load the save file: "+err.Error(), true)
	}
	s.app.mu.Lock()
	defer s.app.mu.Unlock()
	v, err := t.run(s, args)
	if err != nil {
		return toolText(err.Error(), true)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return toolText(err.Error(), true)
	}
	return toolText(string(bytes.TrimRight(buf.Bytes(), "\n")), false)
}

// toolText is a tools/call result holding one block of text.
func toolText(text string, isError bool) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
		"isError": isError,
	}
}
