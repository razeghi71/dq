package writer

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
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

type expectedStructRow struct {
	name string
	age  int64
	city string
}

var expectedStructRows = []expectedStructRow{
	{name: "Alice", age: 30, city: "NY"},
	{name: "Bob", age: 25, city: "LA"},
}

func expectedProfileString(row expectedStructRow) string {
	return fmt.Sprintf("{name:%s, age:%d, meta:{city:%s, source:csv}}", row.name, row.age, row.city)
}

func expectedNullableString(row expectedStructRow) string {
	return fmt.Sprintf("{label:null, city:%s}", row.city)
}

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

func assertNoOutputTempFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read output dir: %v", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.Contains(name, ".tmp") || strings.Contains(name, ".partial") {
			t.Fatalf("unexpected temporary output file left behind: %s", filepath.Join(dir, name))
		}
	}
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

func TestIntegrationStructConstructionJSONOutput(t *testing.T) {
	out := queryAndWrite(t, testdataDir+"/users.csv", `head 1 | transform profile = struct(name = name, age = age, meta = struct(source = "csv", missing = null)) | select profile`, "json")

	var rows []map[string]interface{}
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	profile, ok := rows[0]["profile"].(map[string]interface{})
	if !ok {
		t.Fatalf("profile: expected object, got %T", rows[0]["profile"])
	}
	if profile["name"] != "Alice" {
		t.Fatalf("profile.name: want Alice, got %v", profile["name"])
	}
	if profile["age"].(float64) != 30 {
		t.Fatalf("profile.age: want 30, got %v", profile["age"])
	}
	meta, ok := profile["meta"].(map[string]interface{})
	if !ok {
		t.Fatalf("profile.meta: expected object, got %T", profile["meta"])
	}
	if meta["source"] != "csv" {
		t.Fatalf("profile.meta.source: want csv, got %v", meta["source"])
	}
	if _, ok := meta["missing"]; !ok || meta["missing"] != nil {
		t.Fatalf("profile.meta.missing: want explicit null, got %v present=%v", meta["missing"], ok)
	}
}

func TestIntegrationStructConstructionAllOutputFormats(t *testing.T) {
	query := `head 2 | transform profile = struct(name = name, age = age, meta = struct(city = city, source = "csv")), nullable = struct(label = null, city = city), empty = struct() | select name, profile, nullable, empty`

	t.Run("table", func(t *testing.T) {
		out := queryAndWrite(t, testdataDir+"/users.csv", query, "table")
		wants := []string{"name", "profile", "nullable", "empty", "{}"}
		for _, row := range expectedStructRows {
			wants = append(wants, expectedProfileString(row), expectedNullableString(row))
		}
		for _, want := range wants {
			if !strings.Contains(out, want) {
				t.Fatalf("table output missing %q:\n%s", want, out)
			}
		}
	})

	t.Run("csv", func(t *testing.T) {
		out := queryAndWrite(t, testdataDir+"/users.csv", query, "csv")
		rows, err := csv.NewReader(strings.NewReader(out)).ReadAll()
		if err != nil {
			t.Fatalf("read csv: %v\n%s", err, out)
		}
		if len(rows) != 3 {
			t.Fatalf("expected header + 2 rows, got %d: %#v", len(rows), rows)
		}
		wantHeader := []string{"name", "profile", "nullable", "empty"}
		for i := range wantHeader {
			if rows[0][i] != wantHeader[i] {
				t.Fatalf("header[%d]: want %q, got %q", i, wantHeader[i], rows[0][i])
			}
		}
		for i, want := range expectedStructRows {
			row := rows[i+1]
			if row[0] != want.name || row[1] != expectedProfileString(want) || row[2] != expectedNullableString(want) || row[3] != "{}" {
				t.Fatalf("unexpected csv row %d: %#v", i+1, row)
			}
		}
	})

	t.Run("json", func(t *testing.T) {
		out := queryAndWrite(t, testdataDir+"/users.csv", query, "json")
		var rows []map[string]any
		if err := json.Unmarshal([]byte(out), &rows); err != nil {
			t.Fatalf("invalid JSON: %v\n%s", err, out)
		}
		assertStructRowsJSON(t, rows)
	})

	t.Run("jsonl", func(t *testing.T) {
		out := queryAndWrite(t, testdataDir+"/users.csv", query, "jsonl")
		lines := strings.Split(strings.TrimSpace(out), "\n")
		if len(lines) != 2 {
			t.Fatalf("expected 2 JSONL lines, got %d:\n%s", len(lines), out)
		}
		rows := make([]map[string]any, len(lines))
		for i, line := range lines {
			if err := json.Unmarshal([]byte(line), &rows[i]); err != nil {
				t.Fatalf("line %d invalid JSON: %v\n%s", i, err, line)
			}
		}
		assertStructRowsJSON(t, rows)
	})

	for _, format := range []string{"avro", "parquet"} {
		t.Run(format, func(t *testing.T) {
			out := queryAndWriteBytes(t, testdataDir+"/users.csv", query, format)
			path := writeTempOutput(t, out, "structs."+format)
			got, err := loader.Load(path, loader.Options{Format: format})
			if err != nil {
				t.Fatalf("reload %s: %v", format, err)
			}
			assertStructRowsTable(t, got)
		})
	}
}

func TestIntegrationListConstructionJSONOutput(t *testing.T) {
	out := queryAndWrite(t, testdataDir+"/users.csv", `head 1 | transform tags = list("user", city, null), bundle = list(struct(name = name, age = age), struct(name = upper(name), age = age + 1)), empty = list() | select tags, bundle, empty`, "json")

	var rows []map[string]any
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	tags, ok := rows[0]["tags"].([]any)
	if !ok {
		t.Fatalf("tags: expected array, got %T", rows[0]["tags"])
	}
	if len(tags) != 3 || tags[0] != "user" || tags[1] != "NY" || tags[2] != nil {
		t.Fatalf("unexpected tags: %#v", tags)
	}
	bundle, ok := rows[0]["bundle"].([]any)
	if !ok {
		t.Fatalf("bundle: expected array, got %T", rows[0]["bundle"])
	}
	if len(bundle) != 2 {
		t.Fatalf("expected 2 bundle records, got %#v", bundle)
	}
	first, ok := bundle[0].(map[string]any)
	if !ok {
		t.Fatalf("bundle[0]: expected object, got %T", bundle[0])
	}
	if first["name"] != "Alice" || first["age"] != float64(30) {
		t.Fatalf("unexpected bundle[0]: %#v", first)
	}
	second, ok := bundle[1].(map[string]any)
	if !ok {
		t.Fatalf("bundle[1]: expected object, got %T", bundle[1])
	}
	if second["name"] != "ALICE" || second["age"] != float64(31) {
		t.Fatalf("unexpected bundle[1]: %#v", second)
	}
	empty, ok := rows[0]["empty"].([]any)
	if !ok {
		t.Fatalf("empty: expected array, got %T", rows[0]["empty"])
	}
	if len(empty) != 0 {
		t.Fatalf("expected empty array, got %#v", empty)
	}
}

func TestIntegrationListConstructionAllOutputFormats(t *testing.T) {
	query := `head 2 | transform tags = list("user", city, null), bundle = list(struct(name = name, age = age), struct(name = upper(name), age = age + 1)), empty = list(), nulls = list(null) | select name, tags, bundle, empty, nulls`

	t.Run("table", func(t *testing.T) {
		out := queryAndWrite(t, testdataDir+"/users.csv", query, "table")
		for _, want := range []string{
			"name", "tags", "bundle", "empty", "nulls",
			"[user, NY, null]", "[{name:Alice, age:30}, {name:ALICE, age:31}]", "[]", "[null]",
			"[user, LA, null]", "[{name:Bob, age:25}, {name:BOB, age:26}]",
		} {
			if !strings.Contains(out, want) {
				t.Fatalf("table output missing %q:\n%s", want, out)
			}
		}
	})

	t.Run("csv", func(t *testing.T) {
		out := queryAndWrite(t, testdataDir+"/users.csv", query, "csv")
		rows, err := csv.NewReader(strings.NewReader(out)).ReadAll()
		if err != nil {
			t.Fatalf("read csv: %v\n%s", err, out)
		}
		if len(rows) != 3 {
			t.Fatalf("expected header + 2 rows, got %d: %#v", len(rows), rows)
		}
		if got := rows[0]; strings.Join(got, ",") != "name,tags,bundle,empty,nulls" {
			t.Fatalf("unexpected header: %#v", got)
		}
		if rows[1][0] != "Alice" || rows[1][1] != "[user, NY, null]" || rows[1][2] != "[{name:Alice, age:30}, {name:ALICE, age:31}]" || rows[1][3] != "[]" || rows[1][4] != "[null]" {
			t.Fatalf("unexpected first csv row: %#v", rows[1])
		}
	})

	t.Run("json", func(t *testing.T) {
		out := queryAndWrite(t, testdataDir+"/users.csv", query, "json")
		var rows []map[string]any
		if err := json.Unmarshal([]byte(out), &rows); err != nil {
			t.Fatalf("invalid JSON: %v\n%s", err, out)
		}
		assertListRowsJSON(t, rows)
	})

	t.Run("jsonl", func(t *testing.T) {
		out := queryAndWrite(t, testdataDir+"/users.csv", query, "jsonl")
		lines := strings.Split(strings.TrimSpace(out), "\n")
		if len(lines) != 2 {
			t.Fatalf("expected 2 JSONL lines, got %d:\n%s", len(lines), out)
		}
		rows := make([]map[string]any, len(lines))
		for i, line := range lines {
			if err := json.Unmarshal([]byte(line), &rows[i]); err != nil {
				t.Fatalf("line %d invalid JSON: %v\n%s", i, err, line)
			}
		}
		assertListRowsJSON(t, rows)
	})

	for _, format := range []string{"avro", "parquet"} {
		t.Run(format, func(t *testing.T) {
			out := queryAndWriteBytes(t, testdataDir+"/users.csv", query, format)
			path := writeTempOutput(t, out, "lists."+format)
			got, err := loader.Load(path, loader.Options{Format: format})
			if err != nil {
				t.Fatalf("reload %s: %v", format, err)
			}
			assertListRowsTable(t, got)
		})
	}
}

func assertListRowsJSON(t *testing.T, rows []map[string]any) {
	t.Helper()
	if len(rows) != len(expectedStructRows) {
		t.Fatalf("expected %d rows, got %d", len(expectedStructRows), len(rows))
	}
	for i, want := range expectedStructRows {
		if rows[i]["name"] != want.name {
			t.Fatalf("row %d name: want %s, got %v", i, want.name, rows[i]["name"])
		}
		tags, ok := rows[i]["tags"].([]any)
		if !ok {
			t.Fatalf("row %d tags: expected array, got %T", i, rows[i]["tags"])
		}
		if len(tags) != 3 || tags[0] != "user" || tags[1] != want.city || tags[2] != nil {
			t.Fatalf("row %d unexpected tags: %#v", i, tags)
		}
		bundle, ok := rows[i]["bundle"].([]any)
		if !ok || len(bundle) != 2 {
			t.Fatalf("row %d bundle: expected 2-element array, got %#v", i, rows[i]["bundle"])
		}
		first, ok := bundle[0].(map[string]any)
		if !ok {
			t.Fatalf("row %d bundle[0]: expected object, got %T", i, bundle[0])
		}
		if first["name"] != want.name || first["age"] != float64(want.age) {
			t.Fatalf("row %d unexpected bundle[0]: %#v", i, first)
		}
		empty, ok := rows[i]["empty"].([]any)
		if !ok || len(empty) != 0 {
			t.Fatalf("row %d empty: expected empty array, got %#v", i, rows[i]["empty"])
		}
		nulls, ok := rows[i]["nulls"].([]any)
		if !ok || len(nulls) != 1 || nulls[0] != nil {
			t.Fatalf("row %d nulls: expected [null], got %#v", i, rows[i]["nulls"])
		}
	}
}

func assertListRowsTable(t *testing.T, got *table.Table) {
	t.Helper()
	if got.NumRows != len(expectedStructRows) {
		t.Fatalf("expected %d rows, got %d", len(expectedStructRows), got.NumRows)
	}
	for _, col := range []string{"name", "tags", "bundle", "empty", "nulls"} {
		if got.ColIndex(col) < 0 {
			t.Fatalf("missing column %q in %v", col, got.Columns)
		}
	}
	for i, want := range expectedStructRows {
		if v := got.Get(i, "name"); v.Type != table.TypeString || v.Str != want.name {
			t.Fatalf("row %d name: want %s, got %v", i, want.name, v)
		}
		tags := got.Get(i, "tags")
		if tags.Type != table.TypeList || len(tags.List) != 3 {
			t.Fatalf("row %d tags: want 3-element list, got %v", i, tags)
		}
		if tags.List[0].Type != table.TypeString || tags.List[0].Str != "user" || tags.List[1].Type != table.TypeString || tags.List[1].Str != want.city || tags.List[2].Type != table.TypeNull {
			t.Fatalf("row %d unexpected tags: %#v", i, tags.List)
		}
		bundle := got.Get(i, "bundle")
		if bundle.Type != table.TypeList || len(bundle.List) != 2 {
			t.Fatalf("row %d bundle: want 2-record list, got %v", i, bundle)
		}
		first := recordValues(bundle.List[0])
		if v := first["name"]; v.Type != table.TypeString || v.Str != want.name {
			t.Fatalf("row %d bundle[0].name: want %s, got %v", i, want.name, v)
		}
		if v := first["age"]; v.Type != table.TypeInt || v.Int != want.age {
			t.Fatalf("row %d bundle[0].age: want %d, got %v", i, want.age, v)
		}
		if empty := got.Get(i, "empty"); empty.Type != table.TypeList || len(empty.List) != 0 {
			t.Fatalf("row %d empty: want empty list, got %v", i, empty)
		}
		if nulls := got.Get(i, "nulls"); nulls.Type != table.TypeList || len(nulls.List) != 1 || nulls.List[0].Type != table.TypeNull {
			t.Fatalf("row %d nulls: want [null], got %v", i, nulls)
		}
	}
}

func assertStructRowsJSON(t *testing.T, rows []map[string]any) {
	t.Helper()
	if len(rows) != len(expectedStructRows) {
		t.Fatalf("expected %d rows, got %d", len(expectedStructRows), len(rows))
	}
	for i, want := range expectedStructRows {
		if rows[i]["name"] != want.name {
			t.Fatalf("row %d name: want %s, got %v", i, want.name, rows[i]["name"])
		}
		profile, ok := rows[i]["profile"].(map[string]any)
		if !ok {
			t.Fatalf("row %d profile: expected object, got %T", i, rows[i]["profile"])
		}
		if profile["name"] != want.name {
			t.Fatalf("row %d profile.name: want %s, got %v", i, want.name, profile["name"])
		}
		if profile["age"] != float64(want.age) {
			t.Fatalf("row %d profile.age: want %d, got %v", i, want.age, profile["age"])
		}
		meta, ok := profile["meta"].(map[string]any)
		if !ok {
			t.Fatalf("row %d profile.meta: expected object, got %T", i, profile["meta"])
		}
		if meta["city"] != want.city || meta["source"] != "csv" {
			t.Fatalf("row %d profile.meta: unexpected values %#v", i, meta)
		}
		nullable, ok := rows[i]["nullable"].(map[string]any)
		if !ok {
			t.Fatalf("row %d nullable: expected object, got %T", i, rows[i]["nullable"])
		}
		if _, ok := nullable["label"]; !ok || nullable["label"] != nil {
			t.Fatalf("row %d nullable.label: want explicit null, got %v present=%v", i, nullable["label"], ok)
		}
		if nullable["city"] != want.city {
			t.Fatalf("row %d nullable.city: want %s, got %v", i, want.city, nullable["city"])
		}
		empty, ok := rows[i]["empty"].(map[string]any)
		if !ok {
			t.Fatalf("row %d empty: expected object, got %T", i, rows[i]["empty"])
		}
		if len(empty) != 0 {
			t.Fatalf("row %d empty: want empty object, got %#v", i, empty)
		}
	}
}

func assertStructRowsTable(t *testing.T, got *table.Table) {
	t.Helper()
	if got.NumRows != len(expectedStructRows) {
		t.Fatalf("expected %d rows, got %d", len(expectedStructRows), got.NumRows)
	}
	for _, col := range []string{"name", "profile", "nullable", "empty"} {
		if got.ColIndex(col) < 0 {
			t.Fatalf("missing column %q in %v", col, got.Columns)
		}
	}
	for i, want := range expectedStructRows {
		if v := got.Get(i, "name"); v.Type != table.TypeString || v.Str != want.name {
			t.Fatalf("row %d name: want %s, got %v", i, want.name, v)
		}
		profile := got.Get(i, "profile")
		if profile.Type != table.TypeRecord {
			t.Fatalf("row %d profile: want record, got %v", i, profile)
		}
		profileFields := recordValues(profile)
		if v := profileFields["name"]; v.Type != table.TypeString || v.Str != want.name {
			t.Fatalf("row %d profile.name: want %s, got %v", i, want.name, v)
		}
		if v := profileFields["age"]; v.Type != table.TypeInt || v.Int != want.age {
			t.Fatalf("row %d profile.age: want %d, got %v", i, want.age, v)
		}
		meta := profileFields["meta"]
		if meta.Type != table.TypeRecord {
			t.Fatalf("row %d profile.meta: want record, got %v", i, meta)
		}
		metaFields := recordValues(meta)
		if v := metaFields["city"]; v.Type != table.TypeString || v.Str != want.city {
			t.Fatalf("row %d profile.meta.city: want %s, got %v", i, want.city, v)
		}
		if v := metaFields["source"]; v.Type != table.TypeString || v.Str != "csv" {
			t.Fatalf("row %d profile.meta.source: want csv, got %v", i, v)
		}

		nullable := got.Get(i, "nullable")
		if nullable.Type != table.TypeRecord {
			t.Fatalf("row %d nullable: want record, got %v", i, nullable)
		}
		nullableFields := recordValues(nullable)
		if v := nullableFields["label"]; v.Type != table.TypeNull {
			t.Fatalf("row %d nullable.label: want null, got %v", i, v)
		}
		if v := nullableFields["city"]; v.Type != table.TypeString || v.Str != want.city {
			t.Fatalf("row %d nullable.city: want %s, got %v", i, want.city, v)
		}

		empty := got.Get(i, "empty")
		if empty.Type != table.TypeRecord {
			t.Fatalf("row %d empty: want record, got %v", i, empty)
		}
		if len(empty.Fields) != 0 {
			t.Fatalf("row %d empty: want no fields, got %#v", i, empty.Fields)
		}
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

func TestIntegrationParquetCollidingGoFieldNamesRoundTrip(t *testing.T) {
	tbl := table.NewTable([]string{"field", "Field", "Field_2"})
	tbl.AddRow([]table.Value{
		table.IntVal(1),
		table.IntVal(2),
		table.IntVal(3),
	})

	got := writeAndReloadTable(t, tbl, "parquet")
	if got.NumRows != 1 {
		t.Fatalf("expected 1 row, got %d", got.NumRows)
	}
	if want := "field,Field,Field_2"; strings.Join(got.Columns, ",") != want {
		t.Fatalf("columns: want %q, got %v", want, got.Columns)
	}
	if v := got.Get(0, "field"); v.Type != table.TypeInt || v.Int != 1 {
		t.Fatalf("field: want 1, got %v", v)
	}
	if v := got.Get(0, "Field"); v.Type != table.TypeInt || v.Int != 2 {
		t.Fatalf("Field: want 2, got %v", v)
	}
	if v := got.Get(0, "Field_2"); v.Type != table.TypeInt || v.Int != 3 {
		t.Fatalf("Field_2: want 3, got %v", v)
	}
}

func TestIntegrationParquetNestedCollidingGoFieldNamesRoundTrip(t *testing.T) {
	tbl := table.NewTable([]string{"obj"})
	tbl.AddRow([]table.Value{
		table.RecordVal([]table.RecordField{
			{Name: "field", Value: table.IntVal(1)},
			{Name: "Field", Value: table.IntVal(2)},
			{Name: "Field_2", Value: table.IntVal(3)},
		}),
	})

	got := writeAndReloadTable(t, tbl, "parquet")
	obj := got.Get(0, "obj")
	if obj.Type != table.TypeRecord {
		t.Fatalf("obj: want record, got %v", obj)
	}
	fields := recordValues(obj)
	if v := fields["field"]; v.Type != table.TypeInt || v.Int != 1 {
		t.Fatalf("obj.field: want 1, got %v", v)
	}
	if v := fields["Field"]; v.Type != table.TypeInt || v.Int != 2 {
		t.Fatalf("obj.Field: want 2, got %v", v)
	}
	if v := fields["Field_2"]; v.Type != table.TypeInt || v.Int != 3 {
		t.Fatalf("obj.Field_2: want 3, got %v", v)
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
	if q.Output.Format != "csv" {
		t.Fatalf("Output.Format: got %q, want csv", q.Output.Format)
	}

	result, err := engine.Execute(q, tbl, nil)
	if err != nil {
		t.Fatalf("exec: %v", err)
	}

	var buf bytes.Buffer
	if err := Write(&buf, result, q.Output.Format); err != nil {
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

func TestWriteOutputSingleFileErrorRemovesPartial(t *testing.T) {
	tbl := table.NewTable([]string{"bad name"})
	tbl.AddRow([]table.Value{table.StrVal("Alice")})
	path := filepath.Join(t.TempDir(), "bad.avro")

	err := WriteOutput(tbl, ast.OutputSpec{
		Format: "avro",
		Path:   path,
	})
	if err == nil {
		t.Fatal("expected invalid Avro field name error")
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("expected failed single-file write to remove partial %s, stat err=%v", path, statErr)
	}
	assertNoOutputTempFiles(t, filepath.Dir(path))

	good := table.NewTable([]string{"name"})
	good.AddRow([]table.Value{table.StrVal("Alice")})
	if err := WriteOutput(good, ast.OutputSpec{Format: "avro", Path: path}); err != nil {
		t.Fatalf("retry after failed write should succeed: %v", err)
	}
	if _, err := loader.Load(path, loader.Options{}); err != nil {
		t.Fatalf("reload retry output: %v", err)
	}
}

func TestWriteOutputSplitErrorRemovesPartsFromFailedRun(t *testing.T) {
	tbl := table.NewTable([]string{"name"})
	for _, name := range []string{"Alice", "Bob", "Cara", "Dan"} {
		tbl.AddRow([]table.Value{table.StrVal(name)})
	}
	dir := t.TempDir()
	preexisting := filepath.Join(dir, "output-2.csv")
	if err := os.WriteFile(preexisting, []byte("preexisting\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := WriteOutput(tbl, ast.OutputSpec{
		Format: "csv",
		Path:   dir + string(os.PathSeparator),
		Options: ast.OutputOptions{
			SplitRows: 2,
		},
	})
	if err == nil {
		t.Fatal("expected split write to fail on pre-existing part")
	}
	if _, statErr := os.Stat(filepath.Join(dir, "output-1.csv")); !os.IsNotExist(statErr) {
		t.Fatalf("expected failed split write to remove newly-created output-1.csv, stat err=%v", statErr)
	}
	assertNoOutputTempFiles(t, dir)
	got, err := os.ReadFile(preexisting)
	if err != nil {
		t.Fatalf("pre-existing part should remain: %v", err)
	}
	if string(got) != "preexisting\n" {
		t.Fatalf("pre-existing part changed: %q", got)
	}
}

func TestWriteOutputSplitErrorRemovesBinaryPartsFromFailedRun(t *testing.T) {
	tbl := table.NewTable([]string{"name"})
	for _, name := range []string{"Alice", "Bob", "Cara", "Dan"} {
		tbl.AddRow([]table.Value{table.StrVal(name)})
	}
	dir := t.TempDir()
	preexisting := filepath.Join(dir, "part-2.parquet")
	if err := os.WriteFile(preexisting, []byte("preexisting"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := WriteOutput(tbl, ast.OutputSpec{
		Format: "parquet",
		Path:   filepath.Join(dir, "part-{n}.parquet"),
		Options: ast.OutputOptions{
			SplitRows: 2,
		},
	})
	if err == nil {
		t.Fatal("expected split write to fail on pre-existing parquet part")
	}
	if _, statErr := os.Stat(filepath.Join(dir, "part-1.parquet")); !os.IsNotExist(statErr) {
		t.Fatalf("expected failed split write to remove newly-created part-1.parquet, stat err=%v", statErr)
	}
	assertNoOutputTempFiles(t, dir)
	got, err := os.ReadFile(preexisting)
	if err != nil {
		t.Fatalf("pre-existing parquet part should remain: %v", err)
	}
	if string(got) != "preexisting" {
		t.Fatalf("pre-existing parquet part changed: %q", got)
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
