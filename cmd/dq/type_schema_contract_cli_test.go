package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type cliDescribeRow struct {
	Column   string `json:"column"`
	Type     string `json:"type"`
	RowCount int    `json:"row_count"`
	Schema   string `json:"schema"`
}

func readCLIDescribeRows(t *testing.T, out []byte) map[string]cliDescribeRow {
	t.Helper()
	var rows []cliDescribeRow
	if err := json.Unmarshal(out, &rows); err != nil {
		t.Fatalf("describe json output: %v\n%s", err, out)
	}
	got := make(map[string]cliDescribeRow, len(rows))
	for _, row := range rows {
		if _, exists := got[row.Column]; exists {
			t.Fatalf("duplicate describe row for column %q: %#v", row.Column, rows)
		}
		got[row.Column] = row
	}
	return got
}

func requireCLIDescribeSchema(t *testing.T, rows map[string]cliDescribeRow, column, typ, schema string, rowCount int) {
	t.Helper()
	row, ok := rows[column]
	if !ok {
		t.Fatalf("missing describe row for %q; got %#v", column, rows)
	}
	if row.Type != typ || row.Schema != schema || row.RowCount != rowCount {
		t.Fatalf("%s describe row: got type=%q schema=%q row_count=%d, want type=%q schema=%q row_count=%d",
			column, row.Type, row.Schema, row.RowCount, typ, schema, rowCount)
	}
}

func TestCLICentralTypeDescribeStableAcrossFlatFormats(t *testing.T) {
	bin := buildCLI(t)
	rowCounts := map[string]int{
		"csv":     6,
		"json":    3,
		"jsonl":   3,
		"avro":    6,
		"parquet": 6,
	}

	for _, input := range cliFlatUserInputFiles() {
		t.Run(input.name, func(t *testing.T) {
			rows := readCLIDescribeRows(t, runCLIQuery(t, bin, input.path+" | describe | json"))
			rowCount := rowCounts[input.name]
			requireCLIDescribeSchema(t, rows, "name", "string", "string", rowCount)
			requireCLIDescribeSchema(t, rows, "age", "int", "int", rowCount)
			requireCLIDescribeSchema(t, rows, "city", "string", "string", rowCount)
		})
	}
}

func TestCLICentralTypeRuntimeTypeErrorsAcrossFlatFormats(t *testing.T) {
	bin := buildCLI(t)

	for _, input := range cliFlatUserInputFiles() {
		t.Run(input.name, func(t *testing.T) {
			out := runCLIQueryExpectError(t, bin, input.path+" | transform bad = upper(age) | json")
			for _, part := range []string{"upper() requires a string", "got int"} {
				if !strings.Contains(strings.ToLower(string(out)), strings.ToLower(part)) {
					t.Fatalf("expected error containing %q, got:\n%s", part, out)
				}
			}
		})
	}
}

func TestCLICentralTypeDescribeStableAcrossNestedFormats(t *testing.T) {
	bin := buildCLI(t)
	inputs := []struct {
		name string
		path string
	}{
		{"json", "../../testdata/nested.json"},
		{"jsonl", "../../testdata/nested.jsonl"},
		{"avro", "../../testdata/nested.avro"},
		{"parquet", "../../testdata/nested.parquet"},
	}

	for _, input := range inputs {
		t.Run(input.name, func(t *testing.T) {
			rows := readCLIDescribeRows(t, runCLIQuery(t, bin, input.path+" | describe | json"))
			requireCLIDescribeSchema(t, rows, "id", "int", "int", 3)
			requireCLIDescribeSchema(t, rows, "name", "string", "string", 3)
			requireCLIDescribeSchema(t, rows, "address", "record", "record<city:string, street:string, zip:string>", 3)
			requireCLIDescribeSchema(t, rows, "tags", "list", "list<string>", 3)
			requireCLIDescribeSchema(t, rows, "orders", "list", "list<record<amount:float, order_id:int, status:string>>", 3)
			requireCLIDescribeSchema(t, rows, "profile", "record", "record<history:list<record<date:string, events:list<string>>>, stats:record<logins:int, score:float>>", 3)
		})
	}
}

func TestCLICentralTypeJSONNullabilityAndMixedRendering(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	path := writeCLIJSONInferenceFile(t, dir, "events.jsonl",
		"{\"s\":{\"x\":1},\"xs\":[1,\"two\"],\"items\":[{\"a\":1},{\"b\":\"x\"}]}\n"+
			"{\"s\":{\"y\":\"yes\"},\"xs\":[],\"items\":[{\"a\":2,\"b\":\"z\"}]}\n")

	rows := readCLIDescribeRows(t, runCLIQuery(t, bin, path+" with infer_rows=-1 | describe | json"))
	requireCLIDescribeSchema(t, rows, "s", "record", "record<x:int?, y:string?>", 2)
	requireCLIDescribeSchema(t, rows, "xs", "list", "list<mixed>", 2)
	requireCLIDescribeSchema(t, rows, "items", "list", "list<record<a:int?, b:string?>>", 2)
}

func TestCLICentralTypeJSONCrossRowListConflictStillFails(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	path := writeCLIJSONInferenceFile(t, dir, "events.jsonl",
		"{\"xs\":[1]}\n"+
			"{\"xs\":[\"two\"]}\n")

	out := runCLIQueryExpectError(t, bin, path+" with infer_rows=-1 | describe | json")
	for _, part := range []string{"line 2", "xs[]", "expected int", "got string"} {
		if !strings.Contains(strings.ToLower(string(out)), strings.ToLower(part)) {
			t.Fatalf("expected error containing %q, got:\n%s", part, out)
		}
	}
}

func TestCLICentralTypeCSVNullOnlyAndAllStringModesStayStable(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()

	t.Run("header_only_csv", func(t *testing.T) {
		path := writeCLICSVInferenceFile(t, dir, "header-only.csv", "id,zip,note\n")
		rows := readCLIDescribeRows(t, runCLIQuery(t, bin, path+" | describe | json"))
		requireCLIDescribeSchema(t, rows, "id", "string", "string", 0)
		requireCLIDescribeSchema(t, rows, "zip", "string", "string", 0)
		requireCLIDescribeSchema(t, rows, "note", "string", "string", 0)
	})

	t.Run("infer_rows_zero_preserves_text_identifiers", func(t *testing.T) {
		path := writeCLICSVInferenceFile(t, dir, "ids.csv", "id,zip,active\n007,02110,true\n")
		rows := readCLIDescribeRows(t, runCLIQuery(t, bin, path+" with infer_rows=0 | describe | json"))
		requireCLIDescribeSchema(t, rows, "id", "string", "string", 1)
		requireCLIDescribeSchema(t, rows, "zip", "string", "string", 1)
		requireCLIDescribeSchema(t, rows, "active", "string", "string", 1)
	})
}

func TestCLICentralTypeDescribeThroughPipelineOperations(t *testing.T) {
	bin := buildCLI(t)

	t.Run("select_nested_paths", func(t *testing.T) {
		rows := readCLIDescribeRows(t, runCLIQuery(t, bin, "../../testdata/nested.jsonl | select address.city, orders | describe | json"))
		requireCLIDescribeSchema(t, rows, "address_city", "string", "string", 3)
		requireCLIDescribeSchema(t, rows, "orders", "list", "list<record<amount:float, order_id:int, status:string>>", 3)
	})

	t.Run("group_reduce_remove", func(t *testing.T) {
		rows := readCLIDescribeRows(t, runCLIQuery(t, bin, "../../testdata/users.csv | group city | reduce total = count() | remove grouped | describe | json"))
		requireCLIDescribeSchema(t, rows, "city", "string", "string", 3)
		requireCLIDescribeSchema(t, rows, "total", "int", "int", 3)
	})

	t.Run("join", func(t *testing.T) {
		rows := readCLIDescribeRows(t, runCLIQuery(t, bin, "../../testdata/users.csv | join ../../testdata/orders.csv on name == user_name | describe | json"))
		requireCLIDescribeSchema(t, rows, "name", "string", "string", 4)
		requireCLIDescribeSchema(t, rows, "age", "int", "int", 4)
		requireCLIDescribeSchema(t, rows, "city", "string", "string", 4)
		requireCLIDescribeSchema(t, rows, "order_id", "int", "int", 4)
		requireCLIDescribeSchema(t, rows, "product", "string", "string", 4)
		requireCLIDescribeSchema(t, rows, "amount", "int", "int", 4)
	})
}

func TestCLICentralTypeDescribeOutputFormats(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	outPath := filepath.Join(dir, "describe.json")

	out := runCLIQuery(t, bin, "../../testdata/users.csv | describe | json to "+outPath)
	assertNoCLIStdout(t, out)

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	rows := readCLIDescribeRows(t, data)
	requireCLIDescribeSchema(t, rows, "age", "int", "int", 6)

	jsonlOut := runCLIQuery(t, bin, "../../testdata/users.csv | describe | jsonl")
	lines := strings.Split(strings.TrimSpace(string(jsonlOut)), "\n")
	if len(lines) != 3 {
		t.Fatalf("describe jsonl lines: got %d, want 3\n%s", len(lines), jsonlOut)
	}
	for _, line := range lines {
		var row cliDescribeRow
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatalf("invalid describe jsonl line %q: %v", line, err)
		}
		if row.Schema == "" || row.Type == "" {
			t.Fatalf("describe jsonl row missing schema/type: %#v", row)
		}
	}
}
