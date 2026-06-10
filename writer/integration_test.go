package writer

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/razeghi71/dq/engine"
	"github.com/razeghi71/dq/loader"
	"github.com/razeghi71/dq/parser"
	"github.com/razeghi71/dq/table"
)

const testdataDir = "../testdata"

// queryAndWrite loads a file, runs a query, then writes the result in the given format.
func queryAndWrite(t *testing.T, file, query, format string) string {
	t.Helper()
	tbl, err := loader.Load(file, "")
	if err != nil {
		t.Fatalf("load %s: %v", file, err)
	}
	q, err := parser.Parse(file + " | " + query)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	result, err := engine.Execute(q, tbl, nil)
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	var buf bytes.Buffer
	if err := Write(&buf, result, format); err != nil {
		t.Fatalf("write %s: %v", format, err)
	}
	return buf.String()
}

func TestIntegrationCSVOutput(t *testing.T) {
	out := queryAndWrite(t, testdataDir+"/users.csv", "filter { age > 30 } | select name age", "csv")
	lines := strings.Split(strings.TrimSpace(out), "\n")

	// Header + 2 rows (Charlie 35, Frank 40)
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d:\n%s", len(lines), out)
	}
	if lines[0] != "name,age" {
		t.Errorf("header: want 'name,age', got %q", lines[0])
	}
	// All data rows should have age > 30
	for _, line := range lines[1:] {
		if !strings.Contains(line, "Charlie") && !strings.Contains(line, "Frank") {
			t.Errorf("unexpected row: %s", line)
		}
	}
}

func TestIntegrationRenameJSONPreservesAllColumns(t *testing.T) {
	out := queryAndWrite(t, testdataDir+"/users.csv", "rename name username | head 1", "json")

	var rows []map[string]interface{}
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	row := rows[0]
	if len(row) != 3 {
		t.Fatalf("expected 3 JSON keys, got %d: %v", len(row), row)
	}
	if row["username"] != "Alice" {
		t.Errorf("username: want Alice, got %v", row["username"])
	}
	if row["age"].(float64) != 30 {
		t.Errorf("age: want 30, got %v", row["age"])
	}
	if row["city"] != "NY" {
		t.Errorf("city: want NY, got %v", row["city"])
	}
}

func TestIntegrationJSONOutput(t *testing.T) {
	out := queryAndWrite(t, testdataDir+"/users.csv", "sort age | head 2 | select name age", "json")

	var rows []map[string]interface{}
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	// Sorted ascending by age: Eve(22), Bob(25)
	if rows[0]["name"] != "Eve" {
		t.Errorf("row 0 name: want Eve, got %v", rows[0]["name"])
	}
	if rows[0]["age"].(float64) != 22 {
		t.Errorf("row 0 age: want 22, got %v", rows[0]["age"])
	}
}

func TestIntegrationJSONLOutput(t *testing.T) {
	out := queryAndWrite(t, testdataDir+"/users.csv", "head 3", "jsonl")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}
	for i, line := range lines {
		var obj map[string]interface{}
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			t.Errorf("line %d: invalid JSON: %v", i, err)
		}
		if _, ok := obj["name"]; !ok {
			t.Errorf("line %d: missing 'name' key", i)
		}
	}
}

func TestIntegrationTableOutput(t *testing.T) {
	out := queryAndWrite(t, testdataDir+"/users.csv", "head 1", "table")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 { // header + separator + 1 row
		t.Fatalf("expected 3 lines, got %d:\n%s", len(lines), out)
	}
	if !strings.Contains(lines[1], "-+-") {
		t.Errorf("missing separator: %s", lines[1])
	}
}

func TestIntegrationNestedJSONOutput(t *testing.T) {
	out := queryAndWrite(t, testdataDir+"/nested.json", "select name address", "json")

	var rows []map[string]interface{}
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(rows))
	}

	// address should be a nested object
	addr, ok := rows[0]["address"].(map[string]interface{})
	if !ok {
		t.Fatalf("address: expected object, got %T", rows[0]["address"])
	}
	if addr["city"] != "New York" {
		t.Errorf("address.city: want 'New York', got %v", addr["city"])
	}
}

func TestIntegrationCountCSV(t *testing.T) {
	out := queryAndWrite(t, testdataDir+"/users.csv", "count", "csv")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d:\n%s", len(lines), out)
	}
	if !strings.Contains(lines[1], "6") {
		t.Errorf("expected count of 6: %s", lines[1])
	}
}

func TestIntegrationGroupReduceJSON(t *testing.T) {
	out := queryAndWrite(t, testdataDir+"/users.csv", "group city | reduce n = count() | remove grouped | sort -n", "json")

	var rows []map[string]interface{}
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if len(rows) != 3 {
		t.Fatalf("expected 3 city groups, got %d", len(rows))
	}
	// Sorted desc by n: NY=3, LA=2, SF=1
	if rows[0]["n"].(float64) != 3 {
		t.Errorf("top group count: want 3, got %v", rows[0]["n"])
	}
}

// TestIntegrationCSVRoundTrip verifies CSV output can be reloaded.
func TestIntegrationCSVRoundTrip(t *testing.T) {
	out := queryAndWrite(t, testdataDir+"/users.csv", "select name age", "csv")

	reader := strings.NewReader(out)
	tbl, err := loader.LoadReader(reader, "csv")
	if err != nil {
		t.Fatalf("reload CSV: %v", err)
	}
	if len(tbl.Columns) != 2 {
		t.Fatalf("expected 2 columns, got %v", tbl.Columns)
	}
	if tbl.NumRows != 6 {
		t.Fatalf("expected 6 rows, got %d", tbl.NumRows)
	}
}

// TestIntegrationJSONRoundTrip verifies JSON output can be reloaded.
func TestIntegrationJSONRoundTrip(t *testing.T) {
	out := queryAndWrite(t, testdataDir+"/users.csv", "select name age", "json")

	reader := strings.NewReader(out)
	tbl, err := loader.LoadReader(reader, "json")
	if err != nil {
		t.Fatalf("reload JSON: %v", err)
	}
	if tbl.NumRows != 6 {
		t.Fatalf("expected 6 rows, got %d", tbl.NumRows)
	}

	nameIdx := tbl.ColIndex("name")
	if nameIdx < 0 {
		t.Fatal("missing 'name' column")
	}
	if tbl.GetAt(0, nameIdx).Str != "Alice" {
		t.Errorf("first name: want Alice, got %q", tbl.GetAt(0, nameIdx).Str)
	}
}

// TestIntegrationJSONLRoundTrip verifies JSONL output can be reloaded.
func TestIntegrationJSONLRoundTrip(t *testing.T) {
	out := queryAndWrite(t, testdataDir+"/users.csv", "select name age", "jsonl")

	reader := strings.NewReader(out)
	tbl, err := loader.LoadReader(reader, "jsonl")
	if err != nil {
		t.Fatalf("reload JSONL: %v", err)
	}
	if tbl.NumRows != 6 {
		t.Fatalf("expected 6 rows, got %d", tbl.NumRows)
	}
}

// Test writing results from all supported input formats to all output formats.
func TestIntegrationAllFormatsMatrix(t *testing.T) {
	inputFiles := []string{
		testdataDir + "/users.csv",
		testdataDir + "/users.avro",
		testdataDir + "/users.parquet",
	}
	outputFormats := []string{"table", "csv", "json", "jsonl"}

	for _, file := range inputFiles {
		for _, format := range outputFormats {
			t.Run(file+"_to_"+format, func(t *testing.T) {
				out := queryAndWrite(t, file, "head 2", format)
				if len(out) == 0 {
					t.Error("empty output")
				}
			})
		}
	}
}

// TestWritePreservesTypes ensures JSON output preserves Go types correctly.
func TestWritePreservesTypes(t *testing.T) {
	tbl := table.NewTable([]string{"i", "f", "s", "b", "n"})
	tbl.AddRow([]table.Value{
		table.IntVal(42),
		table.FloatVal(3.14),
		table.StrVal("hello"),
		table.BoolVal(true),
		table.Null(),
	})

	var buf bytes.Buffer
	if err := Write(&buf, tbl, "json"); err != nil {
		t.Fatal(err)
	}

	var rows []map[string]interface{}
	json.Unmarshal(buf.Bytes(), &rows)
	row := rows[0]

	// json.Unmarshal decodes numbers as float64
	if row["i"].(float64) != 42 {
		t.Errorf("int: want 42, got %v", row["i"])
	}
	if row["f"].(float64) != 3.14 {
		t.Errorf("float: want 3.14, got %v", row["f"])
	}
	if row["s"] != "hello" {
		t.Errorf("string: want hello, got %v", row["s"])
	}
	if row["b"] != true {
		t.Errorf("bool: want true, got %v", row["b"])
	}
	if row["n"] != nil {
		t.Errorf("null: want nil, got %v", row["n"])
	}
}
