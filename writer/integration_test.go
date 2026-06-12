package writer

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	goavro "github.com/linkedin/goavro/v2"
	"github.com/razeghi71/dq/ast"
	"github.com/razeghi71/dq/engine"
	"github.com/razeghi71/dq/loader"
	"github.com/razeghi71/dq/parser"
	"github.com/razeghi71/dq/table"
)

const testdataDir = "../testdata"

// queryAndWriteBytes loads a file, runs a query, then writes the result in the given format.
func queryAndWriteBytes(t *testing.T, file, query, format string) []byte {
	t.Helper()
	tbl, err := loader.Load(file, loader.Options{})
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
	return buf.Bytes()
}

// queryAndWrite loads a file, runs a query, then writes the result in the given format.
func queryAndWrite(t *testing.T, file, query, format string) string {
	t.Helper()
	return string(queryAndWriteBytes(t, file, query, format))
}

func writeTempOutput(t *testing.T, data []byte, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write temp output: %v", err)
	}
	return path
}

func writeTableBytes(t *testing.T, tbl *table.Table, format string) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := Write(&buf, tbl, format); err != nil {
		t.Fatalf("write %s: %v", format, err)
	}
	return buf.Bytes()
}

func writeAndReloadTable(t *testing.T, tbl *table.Table, format string) *table.Table {
	t.Helper()
	out := writeTableBytes(t, tbl, format)
	path := writeTempOutput(t, out, "out."+format)
	reloaded, err := loader.Load(path, loader.Options{Format: format})
	if err != nil {
		t.Fatalf("reload %s: %v", format, err)
	}
	return reloaded
}

func avroRecordNames(t *testing.T, data []byte) []string {
	t.Helper()
	reader, err := goavro.NewOCFReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("read Avro OCF: %v", err)
	}
	var schema any
	if err := json.Unmarshal([]byte(reader.Codec().Schema()), &schema); err != nil {
		t.Fatalf("parse Avro schema: %v", err)
	}
	var names []string
	collectAvroRecordNames(schema, &names)
	return names
}

func collectAvroRecordNames(schema any, names *[]string) {
	switch s := schema.(type) {
	case []any:
		for _, branch := range s {
			collectAvroRecordNames(branch, names)
		}
	case map[string]any:
		switch typ := s["type"].(type) {
		case string:
			if typ == "record" {
				if name, ok := s["name"].(string); ok {
					*names = append(*names, name)
				}
				if fields, ok := s["fields"].([]any); ok {
					for _, fieldRaw := range fields {
						field, ok := fieldRaw.(map[string]any)
						if !ok {
							continue
						}
						collectAvroRecordNames(field["type"], names)
					}
				}
			}
			if typ == "array" {
				collectAvroRecordNames(s["items"], names)
			}
		case []any, map[string]any:
			collectAvroRecordNames(typ, names)
		}
	}
}

func TestIntegrationCSVOutput(t *testing.T) {
	out := queryAndWrite(t, testdataDir+"/users.csv", "filter { age > 30 } | select name, age", "csv")
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
	out := queryAndWrite(t, testdataDir+"/users.csv", "rename name=username | head 1", "json")

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
	out := queryAndWrite(t, testdataDir+"/users.csv", "sort age | head 2 | select name, age", "json")

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
	out := queryAndWrite(t, testdataDir+"/nested.json", "select name, address", "json")

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
	out := queryAndWrite(t, testdataDir+"/users.csv", "select name, age", "csv")

	reader := strings.NewReader(out)
	tbl, err := loader.LoadReader(reader, loader.Options{Format: "csv"})
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
	out := queryAndWrite(t, testdataDir+"/users.csv", "select name, age", "json")

	reader := strings.NewReader(out)
	tbl, err := loader.LoadReader(reader, loader.Options{Format: "json"})
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
	out := queryAndWrite(t, testdataDir+"/users.csv", "select name, age", "jsonl")

	reader := strings.NewReader(out)
	tbl, err := loader.LoadReader(reader, loader.Options{Format: "jsonl"})
	if err != nil {
		t.Fatalf("reload JSONL: %v", err)
	}
	if tbl.NumRows != 6 {
		t.Fatalf("expected 6 rows, got %d", tbl.NumRows)
	}
}

func TestIntegrationAvroRoundTrip(t *testing.T) {
	out := queryAndWriteBytes(t, testdataDir+"/users.csv", "select name, age, city", "avro")
	path := writeTempOutput(t, out, "users.avro")

	tbl, err := loader.Load(path, loader.Options{Format: "avro"})
	if err != nil {
		t.Fatalf("reload Avro: %v", err)
	}
	if tbl.NumRows != 6 {
		t.Fatalf("expected 6 rows, got %d", tbl.NumRows)
	}
	if len(tbl.Columns) != 3 {
		t.Fatalf("expected 3 columns, got %v", tbl.Columns)
	}
	if got := tbl.Get(0, "name").Str; got != "Alice" {
		t.Errorf("first name: want Alice, got %q", got)
	}
	if got := tbl.Get(0, "age").Int; got != 30 {
		t.Errorf("first age: want 30, got %d", got)
	}
}

func TestIntegrationParquetRoundTrip(t *testing.T) {
	out := queryAndWriteBytes(t, testdataDir+"/users.csv", "select name, age, city", "parquet")
	path := writeTempOutput(t, out, "users.parquet")

	tbl, err := loader.Load(path, loader.Options{Format: "parquet"})
	if err != nil {
		t.Fatalf("reload Parquet: %v", err)
	}
	if tbl.NumRows != 6 {
		t.Fatalf("expected 6 rows, got %d", tbl.NumRows)
	}
	if len(tbl.Columns) != 3 {
		t.Fatalf("expected 3 columns, got %v", tbl.Columns)
	}
	if got := tbl.Get(0, "name").Str; got != "Alice" {
		t.Errorf("first name: want Alice, got %q", got)
	}
	if got := tbl.Get(0, "age").Int; got != 30 {
		t.Errorf("first age: want 30, got %d", got)
	}
}

func TestIntegrationNestedBinaryRoundTrip(t *testing.T) {
	for _, format := range []string{"avro", "parquet"} {
		t.Run(format, func(t *testing.T) {
			out := queryAndWriteBytes(t, testdataDir+"/nested.json", "select name, address, tags, orders", format)
			path := writeTempOutput(t, out, "nested."+format)

			tbl, err := loader.Load(path, loader.Options{Format: format})
			if err != nil {
				t.Fatalf("reload %s: %v", format, err)
			}
			if tbl.NumRows != 3 {
				t.Fatalf("expected 3 rows, got %d", tbl.NumRows)
			}
			if got := tbl.Get(0, "address"); got.Type != table.TypeRecord {
				t.Fatalf("address: expected record, got %v", got.Type)
			}
			if got := tbl.Get(0, "tags"); got.Type != table.TypeList || len(got.List) != 2 {
				t.Fatalf("tags: expected 2-element list, got %v", got)
			}
			orders := tbl.Get(0, "orders")
			if orders.Type != table.TypeList || len(orders.List) != 2 {
				t.Fatalf("orders: expected 2-element list, got %v", orders)
			}
			firstOrder := orders.List[0]
			if firstOrder.Type != table.TypeRecord {
				t.Fatalf("orders[0]: expected record, got %v", firstOrder.Type)
			}
			orderValues := recordValues(firstOrder)
			if got := orderValues["order_id"].Int; got != 101 {
				t.Errorf("orders[0].order_id: want 101, got %d", got)
			}
			if got := orderValues["status"].Str; got != "shipped" {
				t.Errorf("orders[0].status: want shipped, got %q", got)
			}
			if got := tbl.Get(2, "orders"); got.Type != table.TypeList || len(got.List) != 0 {
				t.Fatalf("orders row 2: expected empty list, got %v", got)
			}
		})
	}
}

func TestIntegrationAvroEmptyResultRoundTrip(t *testing.T) {
	out := queryAndWriteBytes(t, testdataDir+"/users.csv", "filter { age > 100 } | select name, age", "avro")
	path := writeTempOutput(t, out, "empty.avro")

	tbl, err := loader.Load(path, loader.Options{Format: "avro"})
	if err != nil {
		t.Fatalf("reload empty Avro: %v", err)
	}
	if tbl.NumRows != 0 {
		t.Fatalf("expected 0 rows, got %d", tbl.NumRows)
	}
	if got := strings.Join(tbl.Columns, ","); got != "name,age" {
		t.Fatalf("columns: want name,age; got %v", tbl.Columns)
	}
}

func TestIntegrationParquetGroupRoundTrip(t *testing.T) {
	out := queryAndWriteBytes(t, testdataDir+"/users.csv", "group city | sort city", "parquet")
	path := writeTempOutput(t, out, "grouped.parquet")

	tbl, err := loader.Load(path, loader.Options{Format: "parquet"})
	if err != nil {
		t.Fatalf("reload grouped Parquet: %v", err)
	}
	grouped := tbl.Get(1, "grouped") // NY after sorting LA, NY, SF.
	if grouped.Type != table.TypeList || len(grouped.List) != 3 {
		t.Fatalf("grouped: expected 3 records, got %v", grouped)
	}
	first := grouped.List[0]
	if first.Type != table.TypeRecord {
		t.Fatalf("grouped[0]: expected record, got %v", first.Type)
	}
	values := recordValues(first)
	if got := values["name"].Str; got != "Alice" {
		t.Errorf("grouped[0].name: want Alice, got %q", got)
	}
	if got := values["age"].Int; got != 30 {
		t.Errorf("grouped[0].age: want 30, got %d", got)
	}
	if got := values["city"].Str; got != "NY" {
		t.Errorf("grouped[0].city: want NY, got %q", got)
	}
}

func TestIntegrationBinaryNullsAndSchemaMergeRoundTrip(t *testing.T) {
	tbl := table.NewTable([]string{"mixed", "empty", "obj"})
	tbl.AddRow([]table.Value{
		table.IntVal(1),
		table.Null(),
		table.RecordVal([]table.RecordField{
			{Name: "x", Value: table.IntVal(1)},
		}),
	})
	tbl.AddRow([]table.Value{
		table.FloatVal(2.5),
		table.Null(),
		table.RecordVal([]table.RecordField{
			{Name: "x", Value: table.IntVal(2)},
			{Name: "y", Value: table.StrVal("hi")},
		}),
	})
	tbl.AddRow([]table.Value{
		table.Null(),
		table.Null(),
		table.Null(),
	})

	for _, format := range []string{"avro", "parquet"} {
		t.Run(format, func(t *testing.T) {
			got := writeAndReloadTable(t, tbl, format)
			if got.NumRows != 3 {
				t.Fatalf("expected 3 rows, got %d", got.NumRows)
			}
			if v := got.Get(0, "mixed"); v.Type != table.TypeFloat || v.Float != 1 {
				t.Fatalf("mixed row 0: want float 1, got %v", v)
			}
			if v := got.Get(1, "mixed"); v.Type != table.TypeFloat || v.Float != 2.5 {
				t.Fatalf("mixed row 1: want float 2.5, got %v", v)
			}
			if v := got.Get(0, "empty"); v.Type != table.TypeNull {
				t.Fatalf("empty row 0: want null, got %v", v)
			}
			obj := got.Get(1, "obj")
			if obj.Type != table.TypeRecord {
				t.Fatalf("obj row 1: want record, got %v", obj)
			}
			fields := recordValues(obj)
			if v := fields["y"]; v.Type != table.TypeString || v.Str != "hi" {
				t.Fatalf("obj.y: want hi, got %v", v)
			}
		})
	}
}

func TestIntegrationAvroRecordNameCollisionUsesUniqueNames(t *testing.T) {
	tbl := table.NewTable([]string{"a", "a_b"})
	tbl.AddRow([]table.Value{
		table.RecordVal([]table.RecordField{
			{Name: "b", Value: table.RecordVal([]table.RecordField{{Name: "x", Value: table.IntVal(1)}})},
		}),
		table.RecordVal([]table.RecordField{{Name: "y", Value: table.StrVal("s")}}),
	})

	out := writeTableBytes(t, tbl, "avro")
	names := avroRecordNames(t, out)
	seen := map[string]bool{}
	for _, name := range names {
		if seen[name] {
			t.Fatalf("duplicate Avro record name %q in %v", name, names)
		}
		seen[name] = true
	}

	path := writeTempOutput(t, out, "collision.avro")
	got, err := loader.Load(path, loader.Options{Format: "avro"})
	if err != nil {
		t.Fatalf("reload collision Avro: %v", err)
	}
	nested := recordValues(recordValues(got.Get(0, "a"))["b"])["x"]
	if nested.Type != table.TypeInt || nested.Int != 1 {
		t.Fatalf("a.b.x: want 1, got %v", nested)
	}
}

func TestIntegrationAvroRejectsInvalidFieldNames(t *testing.T) {
	tbl := table.NewTable([]string{"bad name"})
	tbl.AddRow([]table.Value{table.IntVal(1)})
	var buf bytes.Buffer
	err := Write(&buf, tbl, "avro")
	if err == nil {
		t.Fatal("expected invalid Avro field name error")
	}
	if !strings.Contains(err.Error(), "requires column names") {
		t.Fatalf("expected column name error, got %v", err)
	}
}

func TestIntegrationParquetRejectsZeroColumns(t *testing.T) {
	var buf bytes.Buffer
	err := Write(&buf, table.NewTable(nil), "parquet")
	if err == nil {
		t.Fatal("expected zero-column Parquet error")
	}
	if !strings.Contains(err.Error(), "at least one column") {
		t.Fatalf("expected zero-column error, got %v", err)
	}
}

func TestIntegrationAvroRejectsZeroColumns(t *testing.T) {
	var buf bytes.Buffer
	err := Write(&buf, table.NewTable(nil), "avro")
	if err == nil {
		t.Fatal("expected zero-column Avro error")
	}
	if !strings.Contains(err.Error(), "at least one column") {
		t.Fatalf("expected zero-column error, got %v", err)
	}
}

func TestIntegrationParquetColumnOrderRoundTrip(t *testing.T) {
	out := queryAndWriteBytes(t, testdataDir+"/users.csv", "select name, age, city", "parquet")
	path := writeTempOutput(t, out, "ordered.parquet")

	tbl, err := loader.Load(path, loader.Options{Format: "parquet"})
	if err != nil {
		t.Fatalf("reload Parquet: %v", err)
	}
	if got := strings.Join(tbl.Columns, ","); got != "name,age,city" {
		t.Fatalf("columns: want name,age,city; got %v", tbl.Columns)
	}
}

func TestIntegrationParquetEmptyResultRoundTrip(t *testing.T) {
	out := queryAndWriteBytes(t, testdataDir+"/users.csv", "filter { age > 100 } | select name, age", "parquet")
	path := writeTempOutput(t, out, "empty.parquet")

	tbl, err := loader.Load(path, loader.Options{Format: "parquet"})
	if err != nil {
		t.Fatalf("reload empty Parquet: %v", err)
	}
	if tbl.NumRows != 0 {
		t.Fatalf("expected 0 rows, got %d", tbl.NumRows)
	}
	if got := strings.Join(tbl.Columns, ","); got != "name,age" {
		t.Fatalf("columns: want name,age; got %v", tbl.Columns)
	}
}

func TestIntegrationBinaryListNullsRoundTrip(t *testing.T) {
	tbl := table.NewTable([]string{"xs"})
	tbl.AddRow([]table.Value{
		table.ListVal([]table.Value{table.IntVal(1), table.Null(), table.IntVal(3)}),
	})

	for _, format := range []string{"avro", "parquet"} {
		t.Run(format, func(t *testing.T) {
			got := writeAndReloadTable(t, tbl, format)
			xs := got.Get(0, "xs")
			if xs.Type != table.TypeList || len(xs.List) != 3 {
				t.Fatalf("xs: want 3-element list, got %v", xs)
			}
			if xs.List[0].Type != table.TypeInt || xs.List[0].Int != 1 {
				t.Fatalf("xs[0]: want 1, got %v", xs.List[0])
			}
			if xs.List[1].Type != table.TypeNull {
				t.Fatalf("xs[1]: want null, got %v", xs.List[1])
			}
			if xs.List[2].Type != table.TypeInt || xs.List[2].Int != 3 {
				t.Fatalf("xs[2]: want 3, got %v", xs.List[2])
			}
		})
	}
}

func TestIntegrationBinaryNestedEmptyListRoundTrip(t *testing.T) {
	tbl := table.NewTable([]string{"orders"})
	tbl.AddRow([]table.Value{
		table.ListVal([]table.Value{
			table.RecordVal([]table.RecordField{
				{Name: "id", Value: table.IntVal(1)},
				{Name: "tags", Value: table.ListVal(nil)},
			}),
		}),
	})

	for _, format := range []string{"avro", "parquet"} {
		t.Run(format, func(t *testing.T) {
			got := writeAndReloadTable(t, tbl, format)
			orders := got.Get(0, "orders")
			if orders.Type != table.TypeList || len(orders.List) != 1 {
				t.Fatalf("orders: want 1-element list, got %v", orders)
			}
			tags := recordValues(orders.List[0])["tags"]
			if tags.Type != table.TypeList {
				t.Fatalf("tags: want list, got %v", tags)
			}
			if len(tags.List) != 0 {
				t.Fatalf("tags: want empty list, got %v", tags)
			}
		})
	}
}

// TestIntegrationQueryOutputEndToEnd runs parse → engine → writer using Query.Output from the query string.
func TestIntegrationQueryOutputEndToEnd(t *testing.T) {
	file := testdataDir + "/users.csv"
	query := file + " | select name, age | head 2 | csv"

	tbl, err := loader.Load(file, loader.Options{})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	q, err := parser.Parse(query)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if q.Output != "csv" {
		t.Fatalf("Output: got %q, want csv", q.Output)
	}

	result, err := engine.Execute(q, tbl, nil)
	if err != nil {
		t.Fatalf("exec: %v", err)
	}

	var buf bytes.Buffer
	if err := Write(&buf, result, q.Output); err != nil {
		t.Fatalf("write: %v", err)
	}

	out := strings.TrimSpace(buf.String())
	if !strings.HasPrefix(out, "name,age") {
		t.Fatalf("expected CSV header, got:\n%s", out)
	}
	if strings.Contains(out, " | ") {
		t.Fatalf("expected CSV not table, got:\n%s", out)
	}
}

// Test writing results from all supported input formats to all output formats.
func TestIntegrationAllFormatsMatrix(t *testing.T) {
	inputFiles := []string{
		testdataDir + "/users.csv",
		testdataDir + "/users.avro",
		testdataDir + "/users.parquet",
	}
	outputFormats := ast.OutputFormatNames()

	for _, file := range inputFiles {
		for _, format := range outputFormats {
			t.Run(file+"_to_"+format, func(t *testing.T) {
				out := queryAndWriteBytes(t, file, "head 2", format)
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
