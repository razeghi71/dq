package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/razeghi71/dq/loader"
)

type mcpSession struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	stderr *bytes.Buffer
}

func startMCPServer(t *testing.T, bin string, args ...string) *mcpSession {
	t.Helper()
	cmdArgs := append([]string{"mcp"}, args...)
	cmd := exec.Command(bin, cmdArgs...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("start dq mcp: %v", err)
	}

	s := &mcpSession{
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewReader(stdout),
		stderr: &stderr,
	}
	t.Cleanup(func() {
		_ = s.stdin.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatalf("dq mcp did not exit after cleanup; stderr:\n%s", stderr.String())
		}
	})
	return s
}

func (s *mcpSession) initialize(t *testing.T) {
	t.Helper()
	resp := s.call(t, 1, "initialize", map[string]any{
		"protocolVersion": "2025-06-18",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "dq-mcp-test",
			"version": "0.0.0",
		},
	})
	assertNoMCPProtocolError(t, resp)
	assertMCPInitializeResult(t, resp)
	s.notify(t, "notifications/initialized", map[string]any{})
}

func (s *mcpSession) call(t *testing.T, id int, method string, params any) map[string]any {
	t.Helper()
	msg := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
	}
	if params != nil {
		msg["params"] = params
	}
	s.write(t, msg)
	resp := s.read(t)
	if got, ok := resp["id"].(float64); !ok || int(got) != id {
		t.Fatalf("response id mismatch: got %#v, want %d; response=%#v", resp["id"], id, resp)
	}
	return resp
}

func (s *mcpSession) notify(t *testing.T, method string, params any) {
	t.Helper()
	msg := map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
	}
	if params != nil {
		msg["params"] = params
	}
	s.write(t, msg)
}

func (s *mcpSession) write(t *testing.T, msg map[string]any) {
	t.Helper()
	payload, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.ContainsAny(payload, "\r\n") {
		t.Fatalf("MCP test payload contains embedded newline: %q", payload)
	}
	if _, err := fmt.Fprintf(s.stdin, "%s\n", payload); err != nil {
		t.Fatalf("write MCP message: %v\nstderr:\n%s", err, s.stderr.String())
	}
}

func (s *mcpSession) read(t *testing.T) map[string]any {
	t.Helper()
	type result struct {
		msg map[string]any
		err error
	}
	ch := make(chan result, 1)
	go func() {
		msg, err := readMCPFrame(s.stdout)
		ch <- result{msg: msg, err: err}
	}()
	select {
	case res := <-ch:
		if res.err != nil {
			t.Fatalf("read MCP response: %v\nstderr:\n%s", res.err, s.stderr.String())
		}
		return res.msg
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for MCP response\nstderr:\n%s", s.stderr.String())
	}
	return nil
}

func readMCPFrame(r *bufio.Reader) (map[string]any, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return nil, err
	}
	if strings.Contains(line, "Content-Length:") {
		return nil, fmt.Errorf("MCP stdio response used Content-Length framing: %q", line)
	}
	payload := strings.TrimRight(line, "\r\n")
	var msg map[string]any
	if err := json.Unmarshal([]byte(payload), &msg); err != nil {
		return nil, fmt.Errorf("decode MCP JSON %q: %w", payload, err)
	}
	return msg, nil
}

func testdataPath(t *testing.T, name string) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("..", "..", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func mustReadREADME(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func assertNoMCPProtocolError(t *testing.T, resp map[string]any) {
	t.Helper()
	if errObj, ok := resp["error"]; ok {
		t.Fatalf("unexpected MCP protocol error: %#v", errObj)
	}
}

func assertMCPInitializeResult(t *testing.T, resp map[string]any) {
	t.Helper()
	result := mcpResult(t, resp)
	if result["protocolVersion"] != "2025-06-18" {
		t.Fatalf("initialize protocolVersion: got %#v, want 2025-06-18", result["protocolVersion"])
	}
	serverInfo, ok := result["serverInfo"].(map[string]any)
	if !ok {
		t.Fatalf("initialize serverInfo: got %T", result["serverInfo"])
	}
	if serverInfo["name"] != "dq" || serverInfo["version"] != "0.0.0" {
		t.Fatalf("initialize serverInfo: %#v", serverInfo)
	}
	capabilities, ok := result["capabilities"].(map[string]any)
	if !ok {
		t.Fatalf("initialize capabilities: got %T", result["capabilities"])
	}
	if _, ok := capabilities["tools"].(map[string]any); !ok {
		t.Fatalf("initialize capabilities missing tools object: %#v", capabilities)
	}
	if _, ok := capabilities["resources"].(map[string]any); !ok {
		t.Fatalf("initialize capabilities missing resources object: %#v", capabilities)
	}
}

func mcpResult(t *testing.T, resp map[string]any) map[string]any {
	t.Helper()
	assertNoMCPProtocolError(t, resp)
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("response has no result object: %#v", resp)
	}
	return result
}

func mcpProtocolError(t *testing.T, resp map[string]any) map[string]any {
	t.Helper()
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected MCP protocol error, got %#v", resp)
	}
	return errObj
}

func assertMCPProtocolErrorContains(t *testing.T, resp map[string]any, code int, want string) {
	t.Helper()
	errObj := mcpProtocolError(t, resp)
	if got := int(errObj["code"].(float64)); got != code {
		t.Fatalf("protocol error code: got %d, want %d; error=%#v", got, code, errObj)
	}
	if !strings.Contains(strings.ToLower(fmt.Sprint(errObj["message"])), strings.ToLower(want)) {
		t.Fatalf("protocol error message did not contain %q: %#v", want, errObj)
	}
}

func assertQueryToolSchema(t *testing.T, tool map[string]any) {
	t.Helper()
	schema, ok := tool["inputSchema"].(map[string]any)
	if !ok {
		t.Fatalf("query inputSchema: got %T", tool["inputSchema"])
	}
	if schema["type"] != "object" {
		t.Fatalf("query inputSchema type: %#v", schema["type"])
	}
	requiredOK := false
	switch required := schema["required"].(type) {
	case []any:
		requiredOK = len(required) == 1 && required[0] == "query"
	case []string:
		requiredOK = len(required) == 1 && required[0] == "query"
	}
	if !requiredOK {
		t.Fatalf("query inputSchema required: %#v", schema["required"])
	}
	if schema["additionalProperties"] != false {
		t.Fatalf("query inputSchema additionalProperties: %#v", schema["additionalProperties"])
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("query inputSchema properties: got %T", schema["properties"])
	}
	query, ok := properties["query"].(map[string]any)
	if !ok {
		t.Fatalf("query inputSchema missing query property: %#v", properties)
	}
	if query["type"] != "string" {
		t.Fatalf("query inputSchema query type: %#v", query["type"])
	}
}

func mcpTextContent(t *testing.T, resp map[string]any) string {
	t.Helper()
	result := mcpResult(t, resp)
	if isError, _ := result["isError"].(bool); isError {
		t.Fatalf("unexpected MCP tool error: %s", mcpContentTextFromResult(result))
	}
	return mcpContentTextFromResult(result)
}

func mcpContentTextFromResult(result map[string]any) string {
	content, _ := result["content"].([]any)
	var b strings.Builder
	for _, item := range content {
		m, _ := item.(map[string]any)
		if text, _ := m["text"].(string); text != "" {
			b.WriteString(text)
		}
	}
	return b.String()
}

func assertMCPToolErrorContains(t *testing.T, resp map[string]any, want string) {
	t.Helper()
	if errObj, ok := resp["error"].(map[string]any); ok {
		if strings.Contains(strings.ToLower(fmt.Sprint(errObj)), strings.ToLower(want)) {
			return
		}
		t.Fatalf("MCP protocol error did not contain %q: %#v", want, errObj)
	}
	result := mcpResult(t, resp)
	if isError, _ := result["isError"].(bool); !isError {
		t.Fatalf("expected MCP tool error containing %q, got success: %#v", want, resp)
	}
	text := mcpContentTextFromResult(result)
	if !strings.Contains(strings.ToLower(text), strings.ToLower(want)) {
		t.Fatalf("MCP tool error did not contain %q:\n%s", want, text)
	}
}

func callMCPQuery(t *testing.T, s *mcpSession, id int, query string) map[string]any {
	t.Helper()
	return s.call(t, id, "tools/call", map[string]any{
		"name": "query",
		"arguments": map[string]any{
			"query": query,
		},
	})
}

func TestMCPCLIRejectsExtraArgs(t *testing.T) {
	bin := buildCLI(t)
	cmd := exec.Command(bin, "mcp", "extra")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected dq mcp extra to fail, got:\n%s", out)
	}
	s := strings.ToLower(string(out))
	if !strings.Contains(s, "mcp") || !strings.Contains(s, "extra") {
		t.Fatalf("expected mcp extra-args error, got:\n%s", out)
	}
}

func TestMCPStdioUsesNewlineDelimitedJSON(t *testing.T) {
	bin := buildCLI(t)
	s := startMCPServer(t, bin)

	init := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-06-18",
			"capabilities":    map[string]any{},
			"clientInfo": map[string]any{
				"name":    "dq-mcp-test",
				"version": "0.0.0",
			},
		},
	}
	payload, err := json.Marshal(init)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprintf(s.stdin, "%s\n", payload); err != nil {
		t.Fatalf("write initialize: %v", err)
	}

	line, err := s.stdout.ReadString('\n')
	if err != nil {
		t.Fatalf("read initialize response: %v\nstderr:\n%s", err, s.stderr.String())
	}
	if strings.Contains(line, "Content-Length:") {
		t.Fatalf("stdio MCP must be newline-delimited JSON, got Content-Length framing:\n%s", line)
	}
	if !strings.HasSuffix(line, "\n") {
		t.Fatalf("MCP response is not newline terminated: %q", line)
	}
	if strings.Contains(strings.TrimRight(line, "\n"), "\n") {
		t.Fatalf("MCP response contains embedded newline: %q", line)
	}

	var resp map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(line)), &resp); err != nil {
		t.Fatalf("response is not valid JSON-RPC JSON: %v\n%s", err, line)
	}
	if resp["jsonrpc"] != "2.0" || resp["id"] != float64(1) {
		t.Fatalf("unexpected initialize response: %#v", resp)
	}
	assertMCPInitializeResult(t, resp)
}

func TestMCPHandshakeToolsAndGuideResource(t *testing.T) {
	bin := buildCLI(t)
	s := startMCPServer(t, bin)
	s.initialize(t)

	ping := mcpResult(t, s.call(t, 2, "ping", map[string]any{}))
	if len(ping) != 0 {
		t.Fatalf("ping result: got %#v, want empty object", ping)
	}

	tools := mcpResult(t, s.call(t, 3, "tools/list", map[string]any{}))["tools"].([]any)
	var names []string
	for _, tool := range tools {
		m := tool.(map[string]any)
		names = append(names, m["name"].(string))
		if m["name"] == "query" {
			assertQueryToolSchema(t, m)
		}
	}
	if len(names) != 1 || names[0] != "query" {
		t.Fatalf("expected exactly the query tool, got %#v", names)
	}
	for _, forbidden := range []string{"describe", "agent_guide"} {
		for _, name := range names {
			if name == forbidden {
				t.Fatalf("unexpected separate MCP tool %q; use query/resource instead", forbidden)
			}
		}
	}

	resources := mcpResult(t, s.call(t, 4, "resources/list", map[string]any{}))["resources"].([]any)
	foundGuide := false
	for _, resource := range resources {
		m := resource.(map[string]any)
		if m["uri"] == "dq://guide" {
			foundGuide = true
			if m["mimeType"] != "text/markdown" {
				t.Fatalf("dq://guide mimeType: got %#v, want text/markdown", m["mimeType"])
			}
		}
	}
	if !foundGuide {
		t.Fatalf("resources/list did not include dq://guide: %#v", resources)
	}

	resp := s.call(t, 5, "resources/read", map[string]any{"uri": "dq://guide"})
	contents := mcpResult(t, resp)["contents"].([]any)
	if len(contents) != 1 {
		t.Fatalf("expected one guide resource content, got %#v", contents)
	}
	guide := contents[0].(map[string]any)
	if guide["uri"] != "dq://guide" || guide["mimeType"] != "text/markdown" {
		t.Fatalf("unexpected guide resource metadata: %#v", guide)
	}
	if guide["text"] != mustReadREADME(t) {
		t.Fatal("dq://guide text is not README.md")
	}

	resp = s.call(t, 6, "resources/read", map[string]any{"uri": "dq://missing"})
	errObj := mcpProtocolError(t, resp)
	if errObj["code"] != float64(jsonrpcResourceNotFound) || !strings.Contains(fmt.Sprint(errObj["message"]), "dq://missing") {
		t.Fatalf("expected resource-not-found error, got %#v", errObj)
	}
}

func TestMCPQueryToolSupportsAllInputFormats(t *testing.T) {
	bin := buildCLI(t)
	s := startMCPServer(t, bin)
	s.initialize(t)

	files := []string{
		"users.csv",
		"users.json",
		"users.jsonl",
		"users.avro",
		"users.parquet",
	}
	for i, file := range files {
		t.Run(file, func(t *testing.T) {
			resp := callMCPQuery(t, s, 10+i, testdataPath(t, file)+" | count | json")
			text := mcpTextContent(t, resp)
			var rows []map[string]any
			if err := json.Unmarshal([]byte(text), &rows); err != nil {
				t.Fatalf("invalid JSON result for %s: %v\n%s", file, err, text)
			}
			if len(rows) != 1 || rows[0]["count"].(float64) <= 0 {
				t.Fatalf("unexpected count result for %s: %#v", file, rows)
			}
		})
	}
}

func TestMCPQueryToolCoversPipelineCombinations(t *testing.T) {
	bin := buildCLI(t)
	s := startMCPServer(t, bin)
	s.initialize(t)

	users := testdataPath(t, "users.csv")
	orders := testdataPath(t, "orders.csv")
	nested := testdataPath(t, "nested.json")
	glob := filepath.Join(testdataPath(t, "glob"), "users-*.csv")

	describe := mcpTextContent(t, callMCPQuery(t, s, 20, users+` | describe | filter { type == "string" } | sort column | json`))
	var describeRows []map[string]any
	if err := json.Unmarshal([]byte(describe), &describeRows); err != nil {
		t.Fatalf("invalid describe JSON: %v\n%s", err, describe)
	}
	if len(describeRows) != 2 || describeRows[0]["column"] != "city" || describeRows[1]["column"] != "name" {
		t.Fatalf("describe query did not return expected string columns: %#v", describeRows)
	}

	globOut := mcpTextContent(t, callMCPQuery(t, s, 21, glob+` with format=csv | sort name | csv`))
	if got := strings.TrimSpace(globOut); got != "name,age,city\nAlice,30,NY\nBob,25,LA" {
		t.Fatalf("unexpected glob CSV result:\n%s", globOut)
	}

	joinQuery := users + ` | join ` + orders + ` on name == user_name | group name | reduce total = sum(amount), n = count() | remove grouped | sort -total | head 1 | json`
	joined := mcpTextContent(t, callMCPQuery(t, s, 22, joinQuery))
	var joinedRows []map[string]any
	if err := json.Unmarshal([]byte(joined), &joinedRows); err != nil {
		t.Fatalf("invalid joined JSON: %v\n%s", err, joined)
	}
	if len(joinedRows) != 1 || joinedRows[0]["name"] != "Alice" || joinedRows[0]["total"] != float64(35) || joinedRows[0]["n"] != float64(2) {
		t.Fatalf("unexpected join/group/reduce result: %#v", joinedRows)
	}

	nestedOut := mcpTextContent(t, callMCPQuery(t, s, 23, nested+` | transform order_count = list_len(orders) | filter { order_count > 1 } | select name, order_count | json`))
	var nestedRows []map[string]any
	if err := json.Unmarshal([]byte(nestedOut), &nestedRows); err != nil {
		t.Fatalf("invalid nested JSON: %v\n%s", err, nestedOut)
	}
	if len(nestedRows) == 0 || nestedRows[0]["order_count"] != float64(2) {
		t.Fatalf("unexpected nested/list query result: %#v", nestedRows)
	}

	tmp := t.TempDir()
	gzipPath := filepath.Join(tmp, "events.data")
	if err := os.WriteFile(gzipPath, gzipCLIBytes(t, "level,msg\nINFO,start\nERROR,timeout\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gzipOut := mcpTextContent(t, callMCPQuery(t, s, 24, gzipPath+` with format=csv, compression=gzip | filter { level == "ERROR" } | select msg | jsonl`))
	if strings.TrimSpace(gzipOut) != `{"msg":"timeout"}` {
		t.Fatalf("unexpected gzip query result:\n%s", gzipOut)
	}

	emptyPath := filepath.Join(tmp, "empty.csv")
	if err := os.WriteFile(emptyPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	emptyOut := mcpTextContent(t, callMCPQuery(t, s, 25, emptyPath+` | count | json`))
	var emptyRows []map[string]any
	if err := json.Unmarshal([]byte(emptyOut), &emptyRows); err != nil {
		t.Fatalf("invalid empty count JSON: %v\n%s", err, emptyOut)
	}
	if len(emptyRows) != 1 || emptyRows[0]["count"] != float64(0) {
		t.Fatalf("unexpected empty CSV count result: %#v", emptyRows)
	}

	outPath := filepath.Join(t.TempDir(), "nested", "users.json")
	resp := callMCPQuery(t, s, 26, users+" | select name, age | head 2 | json to "+outPath)
	_ = mcpTextContent(t, resp)
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("expected MCP query to write %s: %v", outPath, err)
	}
	var rows []map[string]any
	if err := json.Unmarshal(data, &rows); err != nil {
		t.Fatalf("invalid written JSON: %v\n%s", err, data)
	}
	if len(rows) != 2 {
		t.Fatalf("written JSON row count: got %d, want 2", len(rows))
	}
}

func TestMCPQueryToolOutputFormatsAndFileOutputs(t *testing.T) {
	bin := buildCLI(t)
	s := startMCPServer(t, bin)
	s.initialize(t)

	users := testdataPath(t, "users.csv")
	tableOut := mcpTextContent(t, callMCPQuery(t, s, 30, users+" | select name, age | head 1 | table"))
	if !strings.Contains(tableOut, " | ") || !strings.Contains(tableOut, "Alice") {
		t.Fatalf("expected table text output, got:\n%s", tableOut)
	}

	csvOut := mcpTextContent(t, callMCPQuery(t, s, 31, users+" | select name | head 1 | csv"))
	if strings.TrimSpace(csvOut) != "name\nAlice" {
		t.Fatalf("expected CSV text output, got:\n%s", csvOut)
	}

	jsonlOut := mcpTextContent(t, callMCPQuery(t, s, 32, users+" | select name | head 1 | jsonl"))
	if strings.TrimSpace(jsonlOut) != `{"name":"Alice"}` {
		t.Fatalf("expected JSONL text output, got:\n%s", jsonlOut)
	}

	dir := t.TempDir()
	cases := []struct {
		format string
		ext    string
	}{
		{"csv", ".csv"},
		{"json", ".json"},
		{"jsonl", ".jsonl"},
		{"avro", ".avro"},
		{"parquet", ".parquet"},
	}
	for i, tc := range cases {
		t.Run(tc.format, func(t *testing.T) {
			outPath := filepath.Join(dir, "out-"+tc.format+tc.ext)
			resp := callMCPQuery(t, s, 40+i, users+" | select name, age | head 2 | "+tc.format+" to "+outPath)
			_ = mcpTextContent(t, resp)
			tbl, err := loader.Load(outPath, loader.Options{})
			if err != nil {
				t.Fatalf("reload MCP output %s: %v", outPath, err)
			}
			if tbl.NumRows != 2 {
				t.Fatalf("MCP output %s row count: got %d, want 2", outPath, tbl.NumRows)
			}
		})
	}

	splitDir := filepath.Join(dir, "split")
	resp := callMCPQuery(t, s, 50, users+" | select name, age | csv with split_rows=2 to "+splitDir+"/")
	_ = mcpTextContent(t, resp)
	for _, name := range []string{"output-1.csv", "output-2.csv", "output-3.csv"} {
		if _, err := os.Stat(filepath.Join(splitDir, name)); err != nil {
			t.Fatalf("expected split output part %s: %v", name, err)
		}
	}
}

func TestMCPQueryToolErrors(t *testing.T) {
	bin := buildCLI(t)
	s := startMCPServer(t, bin)
	s.initialize(t)

	users := testdataPath(t, "users.csv")
	existing := filepath.Join(t.TempDir(), "existing.csv")
	if err := os.WriteFile(existing, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name  string
		query string
		want  string
	}{
		{"parse", users + " | csv | head 1", "last"},
		{"load", filepath.Join(t.TempDir(), "missing.csv") + " | count", "load"},
		{"output", users + " | csv to " + existing, "exist"},
		{"stdin_source", "- with format=csv | count", "stdin"},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := callMCPQuery(t, s, 50+i, tc.query)
			assertMCPToolErrorContains(t, resp, tc.want)
		})
	}

	resp := s.call(t, 60, "tools/call", map[string]any{
		"name":      "describe",
		"arguments": map[string]any{"source": users},
	})
	assertMCPProtocolErrorContains(t, resp, jsonrpcInvalidParams, "unknown tool")

	resp = s.call(t, 61, "tools/call", map[string]any{
		"name":      "query",
		"arguments": map[string]any{},
	})
	assertMCPProtocolErrorContains(t, resp, jsonrpcInvalidParams, "query")

	resp = s.call(t, 62, "tools/call", map[string]any{
		"name":      "query",
		"arguments": map[string]any{"query": 42},
	})
	assertMCPProtocolErrorContains(t, resp, jsonrpcInvalidParams, "query")
}
