package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	dq "github.com/razeghi71/dq"
)

func rawID(t *testing.T, id int) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(id)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func rawJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func responseResult(t *testing.T, resp jsonrpcMessage) map[string]any {
	t.Helper()
	if resp.Error != nil {
		t.Fatalf("unexpected protocol error: %#v", resp.Error)
	}
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("result: got %T, want map[string]any", resp.Result)
	}
	return result
}

func resultContentText(t *testing.T, result any) string {
	t.Helper()
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("result: got %T, want map[string]any", result)
	}
	return mcpContentTextFromResult(m)
}

func resultIsToolError(t *testing.T, result any, want string) {
	t.Helper()
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("result: got %T, want map[string]any", result)
	}
	if isError, _ := m["isError"].(bool); !isError {
		t.Fatalf("expected tool error, got %#v", result)
	}
	text := strings.ToLower(mcpContentTextFromResult(m))
	if !strings.Contains(text, strings.ToLower(want)) {
		t.Fatalf("tool error text %q does not contain %q", text, want)
	}
}

func writeUnitCSV(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "users.csv")
	if err := os.WriteFile(path, []byte("name,age,city\nAlice,30,NY\nBob,25,LA\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestMCPReadWriteJSONRPCMessageNewlineDelimited(t *testing.T) {
	msg := jsonrpcMessage{
		JSONRPC: "2.0",
		ID:      rawID(t, 7),
		Result:  map[string]any{"ok": true},
	}
	var buf bytes.Buffer
	if err := writeJSONRPCMessage(&buf, msg); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	if strings.Contains(got, "Content-Length:") {
		t.Fatalf("unexpected Content-Length framing:\n%s", got)
	}
	if !strings.HasSuffix(got, "\n") {
		t.Fatalf("message is not newline terminated: %q", got)
	}
	if strings.Contains(strings.TrimRight(got, "\n"), "\n") {
		t.Fatalf("message contains embedded newline: %q", got)
	}

	decoded, err := readJSONRPCMessage(bufio.NewReader(strings.NewReader(got)))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.JSONRPC != "2.0" || string(decoded.ID) != "7" {
		t.Fatalf("decoded message mismatch: %#v", decoded)
	}
}

func TestMCPReadJSONRPCMessageErrors(t *testing.T) {
	if _, err := readJSONRPCMessage(bufio.NewReader(strings.NewReader("{bad json}\n"))); err == nil {
		t.Fatal("expected malformed JSON error")
	} else if !strings.Contains(err.Error(), "decode MCP JSON") {
		t.Fatalf("unexpected malformed JSON error: %v", err)
	}
	if _, err := readJSONRPCMessage(bufio.NewReader(strings.NewReader(""))); !errors.Is(err, io.EOF) {
		t.Fatalf("expected EOF, got %v", err)
	}
	if _, err := readJSONRPCMessage(bufio.NewReader(strings.NewReader(`{"jsonrpc":"2.0"}`))); !errors.Is(err, io.EOF) {
		t.Fatalf("expected EOF for unterminated line, got %v", err)
	}
}

func TestMCPWriteJSONRPCMessageMarshalError(t *testing.T) {
	msg := jsonrpcMessage{
		JSONRPC: "2.0",
		ID:      rawID(t, 1),
		Result:  map[string]any{"bad": make(chan int)},
	}
	if err := writeJSONRPCMessage(io.Discard, msg); err == nil {
		t.Fatal("expected marshal error")
	}
}

func TestRunMCPServerInProcess(t *testing.T) {
	initialize := map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize"}
	notification := map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"}
	tools := map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/list", "params": map[string]any{}}
	var input strings.Builder
	for _, msg := range []map[string]any{initialize, notification, tools} {
		payload, err := json.Marshal(msg)
		if err != nil {
			t.Fatal(err)
		}
		input.Write(payload)
		input.WriteByte('\n')
	}

	var output bytes.Buffer
	if err := runMCPServer(strings.NewReader(input.String()), &output); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected responses only for two requests, got %d:\n%s", len(lines), output.String())
	}
	for _, line := range lines {
		if strings.Contains(line, "Content-Length:") {
			t.Fatalf("unexpected Content-Length framing:\n%s", output.String())
		}
		var resp map[string]any
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			t.Fatalf("invalid response line %q: %v", line, err)
		}
	}
}

func TestRunMCPServerReturnsParseErrorAndContinues(t *testing.T) {
	good := func(id int) string {
		payload, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": "ping"})
		if err != nil {
			t.Fatal(err)
		}
		return string(payload)
	}
	input := good(1) + "\n{bad json}\n" + good(2) + "\n"
	var output bytes.Buffer
	if err := runMCPServer(strings.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected two ping responses plus parse error, got %d:\n%s", len(lines), output.String())
	}

	var parseResp map[string]any
	if err := json.Unmarshal([]byte(lines[1]), &parseResp); err != nil {
		t.Fatal(err)
	}
	if parseResp["id"] != nil {
		t.Fatalf("parse error id: got %#v, want null", parseResp["id"])
	}
	errObj := parseResp["error"].(map[string]any)
	if int(errObj["code"].(float64)) != jsonrpcParseError {
		t.Fatalf("parse error code: %#v", errObj)
	}
}

func TestHandleMCPMessageMethods(t *testing.T) {
	cases := []struct {
		name   string
		method string
		params json.RawMessage
		check  func(*testing.T, jsonrpcMessage)
	}{
		{
			name:   "ping",
			method: "ping",
			params: rawJSON(t, map[string]any{}),
			check: func(t *testing.T, resp jsonrpcMessage) {
				result := responseResult(t, resp)
				if len(result) != 0 {
					t.Fatalf("ping result: got %#v, want empty object", result)
				}
			},
		},
		{
			name:   "initialize",
			method: "initialize",
			params: rawJSON(t, map[string]any{"protocolVersion": "2025-06-18"}),
			check: func(t *testing.T, resp jsonrpcMessage) {
				result := responseResult(t, resp)
				if result["protocolVersion"] != "2025-06-18" {
					t.Fatalf("protocolVersion: %#v", result["protocolVersion"])
				}
				serverInfo, ok := result["serverInfo"].(map[string]any)
				if !ok {
					t.Fatalf("serverInfo: got %T", result["serverInfo"])
				}
				if serverInfo["name"] != "dq" || serverInfo["version"] != "0.0.0" {
					t.Fatalf("serverInfo: %#v", serverInfo)
				}
				capabilities, ok := result["capabilities"].(map[string]any)
				if !ok {
					t.Fatalf("capabilities: got %T", result["capabilities"])
				}
				if _, ok := capabilities["tools"].(map[string]any); !ok {
					t.Fatalf("capabilities missing tools object: %#v", capabilities)
				}
				if _, ok := capabilities["resources"].(map[string]any); !ok {
					t.Fatalf("capabilities missing resources object: %#v", capabilities)
				}
			},
		},
		{
			name:   "tools_list",
			method: "tools/list",
			params: rawJSON(t, map[string]any{}),
			check: func(t *testing.T, resp jsonrpcMessage) {
				tools := responseResult(t, resp)["tools"].([]any)
				if len(tools) != 1 || tools[0].(map[string]any)["name"] != "query" {
					t.Fatalf("unexpected tools: %#v", tools)
				}
				assertQueryToolSchema(t, tools[0].(map[string]any))
			},
		},
		{
			name:   "tools_call",
			method: "tools/call",
			params: rawJSON(t, map[string]any{
				"name":      "query",
				"arguments": map[string]any{"query": writeUnitCSV(t) + " | count | jsonl"},
			}),
			check: func(t *testing.T, resp jsonrpcMessage) {
				text := resultContentText(t, responseResult(t, resp))
				if strings.TrimSpace(text) != `{"count":2}` {
					t.Fatalf("unexpected query result: %q", text)
				}
			},
		},
		{
			name:   "resources_list",
			method: "resources/list",
			params: rawJSON(t, map[string]any{}),
			check: func(t *testing.T, resp jsonrpcMessage) {
				resources := responseResult(t, resp)["resources"].([]any)
				if len(resources) != 1 || resources[0].(map[string]any)["uri"] != mcpGuideURI {
					t.Fatalf("unexpected resources: %#v", resources)
				}
			},
		},
		{
			name:   "resources_read",
			method: "resources/read",
			params: rawJSON(t, map[string]any{"uri": mcpGuideURI}),
			check: func(t *testing.T, resp jsonrpcMessage) {
				contents := responseResult(t, resp)["contents"].([]any)
				if len(contents) != 1 || contents[0].(map[string]any)["text"] != dq.AgentGuide {
					t.Fatalf("unexpected contents: %#v", contents)
				}
			},
		},
		{
			name:   "unknown",
			method: "unknown/method",
			check: func(t *testing.T, resp jsonrpcMessage) {
				if resp.Error == nil || resp.Error.Code != jsonrpcMethodNotFound {
					t.Fatalf("expected -32601 method error, got %#v", resp)
				}
			},
		},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, ok := handleMCPMessage(jsonrpcMessage{
				JSONRPC: "2.0",
				ID:      rawID(t, i+1),
				Method:  tc.method,
				Params:  tc.params,
			})
			if !ok {
				t.Fatal("expected response")
			}
			if tc.check != nil {
				tc.check(t, resp)
			}
		})
	}
}

func TestHandleMCPMessageNotificationNoResponse(t *testing.T) {
	resp, ok := handleMCPMessage(jsonrpcMessage{
		JSONRPC: "2.0",
		Method:  "notifications/initialized",
	})
	if ok {
		t.Fatalf("expected no response for notification, got %#v", resp)
	}
}

func TestNegotiateMCPProtocolVersion(t *testing.T) {
	cases := []struct {
		name string
		raw  json.RawMessage
		want string
	}{
		{"supported", rawJSON(t, map[string]any{"protocolVersion": "2025-06-18"}), "2025-06-18"},
		{"unsupported", rawJSON(t, map[string]any{"protocolVersion": "2024-11-05"}), "2025-06-18"},
		{"absent_params", nil, "2025-06-18"},
		{"missing", rawJSON(t, map[string]any{}), "2025-06-18"},
		{"malformed", json.RawMessage(`{bad json}`), "2025-06-18"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := negotiateMCPProtocolVersion(tc.raw); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestHandleMCPToolCallProtocolErrors(t *testing.T) {
	cases := []struct {
		name string
		raw  json.RawMessage
		want string
	}{
		{"malformed_params", json.RawMessage(`{bad json}`), "invalid"},
		{"wrong_tool", rawJSON(t, map[string]any{"name": "describe", "arguments": map[string]any{"source": "x"}}), "unknown tool"},
		{"missing_args", rawJSON(t, map[string]any{"name": "query"}), "requires"},
		{"null_args", rawJSON(t, map[string]any{"name": "query", "arguments": nil}), "requires"},
		{"non_string_query", rawJSON(t, map[string]any{"name": "query", "arguments": map[string]any{"query": 42}}), "requires"},
		{"empty_query", rawJSON(t, map[string]any{"name": "query", "arguments": map[string]any{"query": "  \t"}}), "non-empty"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, errObj := handleMCPToolCall(tc.raw)
			if result != nil {
				t.Fatalf("expected no tool result for protocol error, got %#v", result)
			}
			if errObj == nil || errObj.Code != jsonrpcInvalidParams || !strings.Contains(strings.ToLower(errObj.Message), strings.ToLower(tc.want)) {
				t.Fatalf("expected invalid-params error containing %q, got %#v", tc.want, errObj)
			}
		})
	}
}

func TestHandleMCPToolCallExecutionError(t *testing.T) {
	result, errObj := handleMCPToolCall(rawJSON(t, map[string]any{
		"name":      "query",
		"arguments": map[string]any{"query": "missing.csv | count"},
	}))
	if errObj != nil {
		t.Fatalf("query execution failure should be tool result error, got protocol error %#v", errObj)
	}
	resultIsToolError(t, result, "load")
}

func TestHandleMCPToolCallSuccess(t *testing.T) {
	path := writeUnitCSV(t)
	result, errObj := handleMCPToolCall(rawJSON(t, map[string]any{
		"name":      "query",
		"arguments": map[string]any{"query": path + " | select name | head 1 | csv"},
	}))
	if errObj != nil {
		t.Fatalf("unexpected protocol error: %#v", errObj)
	}
	text := resultContentText(t, result)
	if strings.TrimSpace(text) != "name\nAlice" {
		t.Fatalf("unexpected query text: %q", text)
	}
}

func TestHandleMCPResourceRead(t *testing.T) {
	good, errObj := handleMCPResourceRead(rawJSON(t, map[string]any{"uri": mcpGuideURI}))
	if errObj != nil {
		t.Fatalf("unexpected resource error: %#v", errObj)
	}
	contents := good["contents"].([]any)
	if len(contents) != 1 || contents[0].(map[string]any)["text"] != dq.AgentGuide {
		t.Fatalf("unexpected guide resource: %#v", good)
	}

	_, errObj = handleMCPResourceRead(rawJSON(t, map[string]any{"uri": "dq://missing"}))
	if errObj == nil || errObj.Code != jsonrpcResourceNotFound || !strings.Contains(errObj.Message, "dq://missing") {
		t.Fatalf("expected resource-not-found error for bad URI, got %#v", errObj)
	}

	_, errObj = handleMCPResourceRead(json.RawMessage(`{bad json}`))
	if errObj == nil || errObj.Code != jsonrpcInvalidParams {
		t.Fatalf("expected invalid-params error for malformed params, got %#v", errObj)
	}
}
