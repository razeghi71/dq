package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeCLIJSONInferenceFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCLIJSONLInferenceBadRecordsEndToEnd(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	path := writeCLIJSONInferenceFile(t, dir, "events.jsonl",
		"{\"id\":1,\"amount\":10}\n"+
			"{\"id\":2,\"amount\":\"bad\"}\n"+
			"{\"id\":3,\"amount\":30}\n")

	out := runCLIQueryExpectError(t, bin, path+" with infer_rows=1 | count")
	for _, part := range []string{"line 2", "amount", "int", "string"} {
		if !strings.Contains(strings.ToLower(string(out)), strings.ToLower(part)) {
			t.Fatalf("expected error containing %q, got:\n%s", part, out)
		}
	}

	out = runCLIQuery(t, bin, path+` with infer_rows=1, max_bad_records=1 | filter { amount > 15 } | select id, amount | json`)
	var rows []map[string]any
	if err := json.Unmarshal(out, &rows); err != nil {
		t.Fatalf("json output: %v\n%s", err, out)
	}
	if len(rows) != 1 || rows[0]["id"].(float64) != 3 || rows[0]["amount"].(float64) != 30 {
		t.Fatalf("filtered rows after skipped bad record: got %#v", rows)
	}
}

func TestCLIJSONInferenceLateFieldsEndToEnd(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	path := writeCLIJSONInferenceFile(t, dir, "events.json", `[{"id":1},{"id":2,"email":"bob@example.com"},{"id":3}]`)

	out := runCLIQueryExpectError(t, bin, path+" with infer_rows=1 | count")
	for _, part := range []string{"row 2", "email", "unknown"} {
		if !strings.Contains(strings.ToLower(string(out)), strings.ToLower(part)) {
			t.Fatalf("expected error containing %q, got:\n%s", part, out)
		}
	}

	out = runCLIQuery(t, bin, path+" with infer_rows=1, max_bad_records=1 | count | json")
	var rows []map[string]int64
	if err := json.Unmarshal(out, &rows); err != nil {
		t.Fatalf("json output: %v\n%s", err, out)
	}
	if len(rows) != 1 || rows[0]["count"] != 2 {
		t.Fatalf("count after skipped late-field row: got %#v, want count=2", rows)
	}
}

func TestCLIJSONLPostSampleNullabilityDescribeEndToEnd(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	path := writeCLIJSONInferenceFile(t, dir, "events.jsonl",
		"{\"id\":1,\"s\":{\"x\":1},\"orders\":[{\"amount\":10}]}\n"+
			"{\"id\":null,\"s\":{},\"orders\":[{}]}\n")

	out := runCLIQuery(t, bin, path+" with infer_rows=1 | describe | json")
	var rows []map[string]any
	if err := json.Unmarshal(out, &rows); err != nil {
		t.Fatalf("json output: %v\n%s", err, out)
	}
	got := map[string]string{}
	for _, row := range rows {
		got[row["column"].(string)] = row["schema"].(string)
	}
	for column, want := range map[string]string{
		"id":     "int?",
		"s":      "record<x:int?>",
		"orders": "list<record<amount:int?>>",
	} {
		if got[column] != want {
			t.Fatalf("%s schema: got %q, want %q; rows=%#v", column, got[column], want, rows)
		}
	}
}

func TestCLIJSONLNumberPrecisionEndToEnd(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	path := writeCLIJSONInferenceFile(t, dir, "events.jsonl",
		"{\"id\":9007199254740993,\"amount\":1.25,\"whole_decimal\":2.0}\n")

	out := runCLIQuery(t, bin, path+" | json")
	dec := json.NewDecoder(bytes.NewReader(out))
	dec.UseNumber()
	var rows []map[string]any
	if err := dec.Decode(&rows); err != nil {
		t.Fatalf("json output: %v\n%s", err, out)
	}
	if len(rows) != 1 {
		t.Fatalf("rows: got %d, want 1; output=%s", len(rows), out)
	}
	if got := rows[0]["id"].(json.Number).String(); got != "9007199254740993" {
		t.Fatalf("id: got %q, want exact 9007199254740993; output=%s", got, out)
	}
	if got := rows[0]["amount"].(json.Number).String(); got != "1.25" {
		t.Fatalf("amount: got %q, want 1.25; output=%s", got, out)
	}

	describe := runCLIQuery(t, bin, path+" | describe | json")
	var desc []map[string]any
	if err := json.Unmarshal(describe, &desc); err != nil {
		t.Fatalf("describe json output: %v\n%s", err, describe)
	}
	schemas := map[string]string{}
	for _, row := range desc {
		schemas[row["column"].(string)] = row["schema"].(string)
	}
	if schemas["id"] != "int" || schemas["amount"] != "float" || schemas["whole_decimal"] != "float" {
		t.Fatalf("schemas: got %#v, want id=int and decimals=float", schemas)
	}
}

func TestCLIJSONLUnrepresentableNumberBadRecordEndToEnd(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	path := writeCLIJSONInferenceFile(t, dir, "events.jsonl",
		"{\"x\":1}\n{\"x\":1e10000}\n{\"x\":2}\n")

	out := runCLIQueryExpectError(t, bin, path+" | count")
	for _, part := range []string{"line 2", "x", "unrepresentable json number", "1e10000"} {
		if !strings.Contains(strings.ToLower(string(out)), part) {
			t.Fatalf("expected error containing %q, got:\n%s", part, out)
		}
	}

	out = runCLIQuery(t, bin, path+" with max_bad_records=1 | json")
	dec := json.NewDecoder(bytes.NewReader(out))
	dec.UseNumber()
	var rows []map[string]any
	if err := dec.Decode(&rows); err != nil {
		t.Fatalf("json output: %v\n%s", err, out)
	}
	if len(rows) != 2 || rows[0]["x"].(json.Number).String() != "1" || rows[1]["x"].(json.Number).String() != "2" {
		t.Fatalf("unrepresentable-number JSONL line should be skipped, got %#v", rows)
	}
}

func TestCLIJSONTopLevelNullErrorsEndToEnd(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	path := writeCLIJSONInferenceFile(t, dir, "events.json", "null")

	out := runCLIQueryExpectError(t, bin, path+" | count")
	if !strings.Contains(strings.ToLower(string(out)), "expected array of objects") {
		t.Fatalf("expected top-level shape error, got:\n%s", out)
	}
}

func TestCLIJSONTrailingValueErrorsEndToEnd(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	path := writeCLIJSONInferenceFile(t, dir, "events.json", `[{"id":1}] {"id":2}`)

	out := runCLIQueryExpectError(t, bin, path+" | count")
	for _, part := range []string{"cannot parse json", "trailing"} {
		if !strings.Contains(strings.ToLower(string(out)), part) {
			t.Fatalf("expected error containing %q, got:\n%s", part, out)
		}
	}
}

func TestCLIJSONLTrailingValueBadRecordEndToEnd(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	path := writeCLIJSONInferenceFile(t, dir, "events.jsonl",
		"{\"id\":1}\n{\"id\":2} {\"id\":3}\n{\"id\":4}\n")

	out := runCLIQueryExpectError(t, bin, path+" | count")
	for _, part := range []string{"line 2", "invalid json", "trailing"} {
		if !strings.Contains(strings.ToLower(string(out)), part) {
			t.Fatalf("expected error containing %q, got:\n%s", part, out)
		}
	}

	out = runCLIQuery(t, bin, path+" with max_bad_records=1 | json")
	dec := json.NewDecoder(bytes.NewReader(out))
	dec.UseNumber()
	var rows []map[string]any
	if err := dec.Decode(&rows); err != nil {
		t.Fatalf("json output: %v\n%s", err, out)
	}
	if len(rows) != 2 || rows[0]["id"].(json.Number).String() != "1" || rows[1]["id"].(json.Number).String() != "4" {
		t.Fatalf("trailing-token JSONL line should be skipped, got %#v", rows)
	}
}

func TestCLIJSONLMalformedLineEndToEnd(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	path := writeCLIJSONInferenceFile(t, dir, "events.jsonl", "{\"id\":1}\nnot-json\n{\"id\":2}\n")

	out := runCLIQuery(t, bin, path+" with infer_rows=1, max_bad_records=1 | count | json")
	var rows []map[string]int64
	if err := json.Unmarshal(out, &rows); err != nil {
		t.Fatalf("json output: %v\n%s", err, out)
	}
	if len(rows) != 1 || rows[0]["count"] != 2 {
		t.Fatalf("count after skipped malformed line: got %#v, want count=2", rows)
	}
}

func TestCLIJSONLInferenceCompressedAndGlobEndToEnd(t *testing.T) {
	bin := buildCLI(t)

	t.Run("gzip_jsonl", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "events.jsonl.gz")
		data := "{\"id\":1,\"amount\":10}\n{\"id\":2,\"amount\":\"bad\"}\n{\"id\":3,\"amount\":30}\n"
		if err := os.WriteFile(path, gzipCLIBytes(t, data), 0o644); err != nil {
			t.Fatal(err)
		}
		out := runCLIQuery(t, bin, path+" with infer_rows=1, max_bad_records=1 | count | json")
		var rows []map[string]int64
		if err := json.Unmarshal(out, &rows); err != nil {
			t.Fatalf("json output: %v\n%s", err, out)
		}
		if len(rows) != 1 || rows[0]["count"] != 2 {
			t.Fatalf("gzip count after skipped bad row: got %#v, want count=2", rows)
		}
	})

	t.Run("glob_jsonl", func(t *testing.T) {
		dir := t.TempDir()
		writeCLIJSONInferenceFile(t, dir, "a.jsonl", "{\"id\":1,\"amount\":10}\n")
		writeCLIJSONInferenceFile(t, dir, "b.jsonl", "{\"id\":2,\"amount\":\"bad\"}\n{\"id\":3,\"amount\":30}\n")
		out := runCLIQuery(t, bin, filepath.Join(dir, "*.jsonl")+" with format=jsonl, infer_rows=1, max_bad_records=1 | count | json")
		var rows []map[string]int64
		if err := json.Unmarshal(out, &rows); err != nil {
			t.Fatalf("json output: %v\n%s", err, out)
		}
		if len(rows) != 1 || rows[0]["count"] != 2 {
			t.Fatalf("glob count after skipped bad row: got %#v, want count=2", rows)
		}
	})
}

func TestCLIJSONInferenceOptionsRejectedEndToEnd(t *testing.T) {
	bin := buildCLI(t)
	out := runCLIQueryExpectError(t, bin, "../../testdata/users.jsonl with infer_rows=0 | count")
	if !strings.Contains(strings.ToLower(string(out)), "infer_rows=0") {
		t.Fatalf("expected infer_rows=0 rejection, got:\n%s", out)
	}
}
