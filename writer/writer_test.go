package writer

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/razeghi71/dq/ast"
	"github.com/razeghi71/dq/table"
)

// mixedTable returns a table with columns covering all value types:
// name(string), age(int), score(float), active(bool), empty(null), address(record), tags(list)
func mixedTable() *table.Table {
	t := table.NewTable([]string{"name", "age", "score", "active", "empty", "address", "tags"})
	t.AddRow([]table.Value{
		table.StrVal("Alice"),
		table.IntVal(30),
		table.FloatVal(9.5),
		table.BoolVal(true),
		table.Null(),
		table.RecordVal([]table.RecordField{
			{Name: "city", Value: table.StrVal("NY")},
			{Name: "zip", Value: table.IntVal(10001)},
		}),
		table.ListVal([]table.Value{table.StrVal("go"), table.StrVal("rust")}),
	})
	t.AddRow([]table.Value{
		table.StrVal("Bob"),
		table.IntVal(25),
		table.FloatVal(6.2),
		table.BoolVal(false),
		table.Null(),
		table.RecordVal([]table.RecordField{
			{Name: "city", Value: table.StrVal("LA")},
			{Name: "zip", Value: table.IntVal(90001)},
		}),
		table.ListVal([]table.Value{table.StrVal("python")}),
	})
	return t
}

func TestWriteTable(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, mixedTable(), "table"); err != nil {
		t.Fatal(err)
	}
	out := buf.String()

	// Header should contain all column names
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 4 { // header + separator + 2 data rows
		t.Fatalf("expected 4 lines, got %d:\n%s", len(lines), out)
	}
	if !strings.Contains(lines[0], "name") || !strings.Contains(lines[0], "age") {
		t.Errorf("header missing columns: %s", lines[0])
	}
	// Separator line
	if !strings.Contains(lines[1], "-+-") {
		t.Errorf("expected separator with -+-, got: %s", lines[1])
	}
	// Data rows
	if !strings.Contains(lines[2], "Alice") {
		t.Errorf("expected Alice in first data row: %s", lines[2])
	}
}

func TestWriteTablePadsCellsToColumnWidths(t *testing.T) {
	tbl := table.NewTable([]string{"short", "long"})
	tbl.AddRow([]table.Value{table.StrVal("xx"), table.StrVal("y")})
	tbl.AddRow([]table.Value{table.StrVal("longer"), table.StrVal("zz")})

	var buf bytes.Buffer
	if err := Write(&buf, tbl, "table"); err != nil {
		t.Fatal(err)
	}
	want := strings.Join([]string{
		"short  | long",
		"-------+-----",
		"xx     | y   ",
		"longer | zz  ",
		"",
	}, "\n")
	if buf.String() != want {
		t.Fatalf("table output mismatch:\nwant:\n%q\ngot:\n%q", want, buf.String())
	}
}

func TestWriteTableEmpty(t *testing.T) {
	var buf bytes.Buffer
	empty := table.NewTable(nil)
	if err := Write(&buf, empty, "table"); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 0 {
		t.Errorf("expected empty output for empty table, got: %q", buf.String())
	}
}

func TestWriteTableDefault(t *testing.T) {
	var buf bytes.Buffer
	tbl := table.NewTable([]string{"x"})
	tbl.AddRow([]table.Value{table.IntVal(1)})

	// empty string format should default to table
	if err := Write(&buf, tbl, ""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "x") {
		t.Error("default format should produce table output")
	}
}

func TestWriteCSV(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, mixedTable(), "csv"); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 3 { // header + 2 data rows
		t.Fatalf("expected 3 lines, got %d:\n%s", len(lines), buf.String())
	}

	// Header
	if lines[0] != "name,age,score,active,empty,address,tags" {
		t.Errorf("unexpected CSV header: %s", lines[0])
	}

	// Null renders as empty string in CSV
	// The "empty" column is index 4
	fields := strings.Split(lines[1], ",")
	if len(fields) < 5 {
		t.Fatalf("expected at least 5 fields, got %d", len(fields))
	}
	if fields[4] != "" {
		t.Errorf("null should render as empty in CSV, got %q", fields[4])
	}

	// Bool renders as string
	if fields[3] != "true" {
		t.Errorf("expected 'true', got %q", fields[3])
	}
}

func TestWriteJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, mixedTable(), "json"); err != nil {
		t.Fatal(err)
	}

	var rows []map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &rows); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, buf.String())
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}

	row0 := rows[0]

	// String
	if row0["name"] != "Alice" {
		t.Errorf("name: want Alice, got %v", row0["name"])
	}

	// Int (JSON numbers decode as float64)
	if row0["age"].(float64) != 30 {
		t.Errorf("age: want 30, got %v", row0["age"])
	}

	// Float
	if row0["score"].(float64) != 9.5 {
		t.Errorf("score: want 9.5, got %v", row0["score"])
	}

	// Bool
	if row0["active"] != true {
		t.Errorf("active: want true, got %v", row0["active"])
	}

	// Null
	if row0["empty"] != nil {
		t.Errorf("empty: want nil, got %v", row0["empty"])
	}

	// Record
	addr, ok := row0["address"].(map[string]interface{})
	if !ok {
		t.Fatalf("address: expected object, got %T", row0["address"])
	}
	if addr["city"] != "NY" {
		t.Errorf("address.city: want NY, got %v", addr["city"])
	}
	if addr["zip"].(float64) != 10001 {
		t.Errorf("address.zip: want 10001, got %v", addr["zip"])
	}

	// List
	tags, ok := row0["tags"].([]interface{})
	if !ok {
		t.Fatalf("tags: expected array, got %T", row0["tags"])
	}
	if len(tags) != 2 || tags[0] != "go" || tags[1] != "rust" {
		t.Errorf("tags: want [go rust], got %v", tags)
	}
}

func TestWriteJSONL(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, mixedTable(), "jsonl"); err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}

	// Each line should be valid JSON
	for i, line := range lines {
		var obj map[string]interface{}
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			t.Errorf("line %d: invalid JSON: %v", i, err)
		}
	}

	// Verify first line content
	var row0 map[string]interface{}
	json.Unmarshal([]byte(lines[0]), &row0)
	if row0["name"] != "Alice" {
		t.Errorf("name: want Alice, got %v", row0["name"])
	}
	if row0["empty"] != nil {
		t.Errorf("empty: want nil, got %v", row0["empty"])
	}
	if row0["active"] != true {
		t.Errorf("active: want true, got %v", row0["active"])
	}
}

func TestWriteJSONEmpty(t *testing.T) {
	var buf bytes.Buffer
	empty := table.NewTable([]string{"a", "b"})
	if err := Write(&buf, empty, "json"); err != nil {
		t.Fatal(err)
	}
	var rows []map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &rows); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("expected empty array, got %d items", len(rows))
	}
}

func TestWriteUnsupportedFormat(t *testing.T) {
	tbl := table.NewTable([]string{"x"})
	for _, format := range []string{"xml", "XML", "tsv"} {
		t.Run(format, func(t *testing.T) {
			var buf bytes.Buffer
			err := Write(&buf, tbl, format)
			if err == nil {
				t.Fatal("expected error for unsupported format")
			}
			if !strings.Contains(err.Error(), "unsupported output format") {
				t.Errorf("expected unsupported output format in error, got: %v", err)
			}
			if !strings.Contains(err.Error(), ast.OutputFormatsList()) {
				t.Errorf("expected supported formats list in error, got: %v", err)
			}
		})
	}
}

func TestValueToJSON(t *testing.T) {
	tests := []struct {
		name string
		val  table.Value
		want interface{}
	}{
		{"null", table.Null(), nil},
		{"int", table.IntVal(42), int64(42)},
		{"float", table.FloatVal(3.14), 3.14},
		{"string", table.StrVal("hello"), "hello"},
		{"bool_true", table.BoolVal(true), true},
		{"bool_false", table.BoolVal(false), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := valueToJSON(tt.val)
			if got != tt.want {
				t.Errorf("valueToJSON(%s): want %v (%T), got %v (%T)", tt.name, tt.want, tt.want, got, got)
			}
		})
	}
}

func TestValueToJSONList(t *testing.T) {
	v := table.ListVal([]table.Value{table.IntVal(1), table.StrVal("two"), table.Null()})
	got := valueToJSON(v)
	arr, ok := got.([]interface{})
	if !ok {
		t.Fatalf("expected []interface{}, got %T", got)
	}
	if len(arr) != 3 {
		t.Fatalf("expected 3 elements, got %d", len(arr))
	}
	if arr[0] != int64(1) {
		t.Errorf("elem 0: want 1, got %v", arr[0])
	}
	if arr[1] != "two" {
		t.Errorf("elem 1: want 'two', got %v", arr[1])
	}
	if arr[2] != nil {
		t.Errorf("elem 2: want nil, got %v", arr[2])
	}
}

func TestValueToJSONRecord(t *testing.T) {
	v := table.RecordVal([]table.RecordField{
		{Name: "x", Value: table.IntVal(10)},
		{Name: "y", Value: table.StrVal("hi")},
	})
	got := valueToJSON(v)
	obj, ok := got.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map[string]interface{}, got %T", got)
	}
	if obj["x"] != int64(10) {
		t.Errorf("x: want 10, got %v", obj["x"])
	}
	if obj["y"] != "hi" {
		t.Errorf("y: want 'hi', got %v", obj["y"])
	}
}

// TestWriteCSVMissingValues verifies behavior when a row has fewer values than columns.
func TestWriteCSVMissingValues(t *testing.T) {
	tbl := table.NewTable([]string{"a", "b", "c"})
	tbl.AddRow([]table.Value{table.StrVal("x")}) // only 1 value for 3 columns

	var buf bytes.Buffer
	if err := Write(&buf, tbl, "csv"); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	// Missing columns should be empty strings
	if lines[1] != "x,," {
		t.Errorf("expected 'x,,', got %q", lines[1])
	}
}
