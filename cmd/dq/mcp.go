package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	dq "github.com/razeghi71/dq"
)

const mcpGuideURI = "dq://guide"

var supportedMCPProtocolVersions = []string{"2025-06-18"}

const (
	jsonrpcParseError       = -32700
	jsonrpcInvalidParams    = -32602
	jsonrpcMethodNotFound   = -32601
	jsonrpcResourceNotFound = -32002
)

type jsonrpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *jsonrpcError   `json:"error,omitempty"`
}

type jsonrpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type jsonrpcMessageParseError struct {
	payload string
	err     error
}

func (e *jsonrpcMessageParseError) Error() string {
	return fmt.Sprintf("decode MCP JSON %q: %v", e.payload, e.err)
}

func (e *jsonrpcMessageParseError) Unwrap() error {
	return e.err
}

func runMCPServer(r io.Reader, w io.Writer) error {
	br := bufio.NewReader(r)
	for {
		msg, err := readJSONRPCMessage(br)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			var parseErr *jsonrpcMessageParseError
			if errors.As(err, &parseErr) {
				resp := jsonrpcMessage{
					JSONRPC: "2.0",
					ID:      json.RawMessage("null"),
					Error: &jsonrpcError{
						Code:    jsonrpcParseError,
						Message: "parse error: " + parseErr.err.Error(),
					},
				}
				if err := writeJSONRPCMessage(w, resp); err != nil {
					return err
				}
				continue
			}
			return err
		}

		resp, shouldRespond := handleMCPMessage(msg)
		if !shouldRespond {
			continue
		}
		if err := writeJSONRPCMessage(w, resp); err != nil {
			return err
		}
	}
}

func readJSONRPCMessage(r *bufio.Reader) (jsonrpcMessage, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return jsonrpcMessage{}, err
	}
	payload := strings.TrimRight(line, "\r\n")

	var msg jsonrpcMessage
	if err := json.Unmarshal([]byte(payload), &msg); err != nil {
		return jsonrpcMessage{}, &jsonrpcMessageParseError{payload: payload, err: err}
	}
	return msg, nil
}

func writeJSONRPCMessage(w io.Writer, msg jsonrpcMessage) error {
	payload, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	if bytes.ContainsAny(payload, "\r\n") {
		return fmt.Errorf("MCP stdio message contains embedded newline")
	}
	_, err = fmt.Fprintf(w, "%s\n", payload)
	return err
}

func handleMCPMessage(msg jsonrpcMessage) (jsonrpcMessage, bool) {
	if len(msg.ID) == 0 {
		return jsonrpcMessage{}, false
	}

	resp := jsonrpcMessage{
		JSONRPC: "2.0",
		ID:      msg.ID,
	}

	switch msg.Method {
	case "ping":
		resp.Result = map[string]any{}
	case "initialize":
		resp.Result = map[string]any{
			"protocolVersion": negotiateMCPProtocolVersion(msg.Params),
			"capabilities": map[string]any{
				"tools":     map[string]any{},
				"resources": map[string]any{},
			},
			"serverInfo": map[string]any{
				"name":    "dq",
				"version": "0.0.0",
			},
		}
	case "tools/list":
		resp.Result = map[string]any{
			"tools": []any{
				map[string]any{
					"name":        "query",
					"description": "Run one complete dq query string through the same execution path as the CLI.",
					"inputSchema": map[string]any{
						"type":     "object",
						"required": []string{"query"},
						"properties": map[string]any{
							"query": map[string]any{
								"type":        "string",
								"description": "A complete dq query, including source, pipeline operations, and optional terminal output command.",
							},
						},
						"additionalProperties": false,
					},
				},
			},
		}
	case "tools/call":
		result, err := handleMCPToolCall(msg.Params)
		if err != nil {
			resp.Error = err
		} else {
			resp.Result = result
		}
	case "resources/list":
		resp.Result = map[string]any{
			"resources": []any{
				map[string]any{
					"uri":         mcpGuideURI,
					"name":        "dq agent guide",
					"description": "Detailed dq query language and design contract for agents.",
					"mimeType":    "text/markdown",
				},
			},
		}
	case "resources/read":
		result, err := handleMCPResourceRead(msg.Params)
		if err != nil {
			resp.Error = err
		} else {
			resp.Result = result
		}
	default:
		resp.Error = &jsonrpcError{Code: jsonrpcMethodNotFound, Message: "method not found: " + msg.Method}
	}

	return resp, true
}

func negotiateMCPProtocolVersion(raw json.RawMessage) string {
	latest := supportedMCPProtocolVersions[0]

	var params struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return latest
	}
	for _, version := range supportedMCPProtocolVersions {
		if params.ProtocolVersion == version {
			return params.ProtocolVersion
		}
	}
	return latest
}

func handleMCPToolCall(raw json.RawMessage) (map[string]any, *jsonrpcError) {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, &jsonrpcError{Code: jsonrpcInvalidParams, Message: "invalid tools/call params: " + err.Error()}
	}
	if params.Name != "query" {
		return nil, &jsonrpcError{Code: jsonrpcInvalidParams, Message: fmt.Sprintf("unknown tool: %s", params.Name)}
	}

	var args struct {
		Query string `json:"query"`
	}
	if len(params.Arguments) == 0 || string(params.Arguments) == "null" {
		return nil, &jsonrpcError{Code: jsonrpcInvalidParams, Message: `query tool requires a string "query" argument`}
	}
	if err := json.Unmarshal(params.Arguments, &args); err != nil {
		return nil, &jsonrpcError{Code: jsonrpcInvalidParams, Message: `query tool requires a string "query" argument: ` + err.Error()}
	}
	if strings.TrimSpace(args.Query) == "" {
		return nil, &jsonrpcError{Code: jsonrpcInvalidParams, Message: `query tool requires a non-empty string "query" argument`}
	}

	var stdout bytes.Buffer
	if err := runMCPQuery(args.Query, &stdout); err != nil {
		return mcpToolError(err.Error()), nil
	}
	return mcpToolResult(stdout.String()), nil
}

func handleMCPResourceRead(raw json.RawMessage) (map[string]any, *jsonrpcError) {
	var params struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, &jsonrpcError{Code: jsonrpcInvalidParams, Message: "invalid resources/read params: " + err.Error()}
	}
	if params.URI != mcpGuideURI {
		return nil, &jsonrpcError{Code: jsonrpcResourceNotFound, Message: "resource not found: " + params.URI}
	}
	return map[string]any{
		"contents": []any{
			map[string]any{
				"uri":      mcpGuideURI,
				"mimeType": "text/markdown",
				"text":     dq.AgentGuide,
			},
		},
	}, nil
}

func mcpToolResult(text string) map[string]any {
	return map[string]any{
		"content": []any{
			map[string]any{
				"type": "text",
				"text": text,
			},
		},
	}
}

func mcpToolError(text string) map[string]any {
	return map[string]any{
		"isError": true,
		"content": []any{
			map[string]any{
				"type": "text",
				"text": text,
			},
		},
	}
}
