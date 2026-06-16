package engine

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
	"github.com/razeghi71/dq/ast"
	"github.com/razeghi71/dq/loader"
	"github.com/razeghi71/dq/parser"
	"github.com/razeghi71/dq/table"
)

const testdataDir = "../testdata"

func gzipIntegrationBytes(t *testing.T, content string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func zstdIntegrationBytes(t *testing.T, content string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw, err := zstd.NewWriter(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := zw.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func deflateIntegrationBytes(t *testing.T, content string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	if _, err := zw.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// loadAndQuery parses source | query, loads the source with any with-clause options, and executes.
func loadAndQuery(t *testing.T, source, query string) *table.Table {
	t.Helper()
	q, err := parser.Parse(source + " | " + query)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	tbl, err := loader.Load(q.Source.Filename, loader.FromAST(q.Source.Load))
	if err != nil {
		t.Fatalf("load %s: %v", q.Source.Filename, err)
	}
	result, err := Execute(q, tbl, nil)
	if err != nil {
		t.Fatalf("exec error: %v", err)
	}
	return result
}

func loadAndQueryExpectErr(t *testing.T, source, query string) error {
	t.Helper()
	q, err := parser.Parse(source + " | " + query)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	tbl, err := loader.Load(q.Source.Filename, loader.FromAST(q.Source.Load))
	if err != nil {
		t.Fatalf("load %s: %v", q.Source.Filename, err)
	}
	_, err = Execute(q, tbl, nil)
	return err
}

func flatUserFormatFiles() []struct {
	name string
	file string
} {
	return []struct {
		name string
		file string
	}{
		{"csv", testdataDir + "/users.csv"},
		{"json", testdataDir + "/users.json"},
		{"jsonl", testdataDir + "/users.jsonl"},
		{"avro", testdataDir + "/users.avro"},
		{"parquet", testdataDir + "/users.parquet"},
	}
}

// ============================================================
// Flat files (users.{csv,json,jsonl,avro,parquet})
// ============================================================

// assertFlatQueries runs the same query assertions against any flat users file.
// Expected schema: name(string), age(int), city(string), 6 rows.
func assertFlatQueries(t *testing.T, file string) {
	t.Helper()

	t.Run("filter", func(t *testing.T) {
		result := loadAndQuery(t, file, `filter { age > 30 }`)
		if result.NumRows != 2 {
			t.Fatalf("expected 2 rows, got %d", result.NumRows)
		}
		ageIdx := result.ColIndex("age")
		for i := 0; i < result.NumRows; i++ {
			age := result.GetAt(i, ageIdx)
			if age.Int <= 30 {
				t.Errorf("expected age > 30, got %d", age.Int)
			}
		}
	})

	t.Run("sort_select_head", func(t *testing.T) {
		result := loadAndQuery(t, file, "sort age | select name, age | head 3")
		if result.NumRows != 3 {
			t.Fatalf("expected 3 rows, got %d", result.NumRows)
		}
		if len(result.Columns) != 2 {
			t.Fatalf("expected 2 columns, got %v", result.Columns)
		}
		// youngest first
		ages := []int64{22, 25, 28}
		ageIdx := result.ColIndex("age")
		for i, want := range ages {
			got := result.GetAt(i, ageIdx).Int
			if got != want {
				t.Errorf("row %d age: want %d, got %d", i, want, got)
			}
		}
	})

	t.Run("group_reduce", func(t *testing.T) {
		result := loadAndQuery(t, file, "group city | reduce n = count(), total = sum(age) | remove grouped | sort -n")
		// 3 cities: NY(3), LA(2), SF(1)
		if result.NumRows != 3 {
			t.Fatalf("expected 3 rows, got %d", result.NumRows)
		}
		nIdx := result.ColIndex("n")
		if result.GetAt(0, nIdx).Int != 3 {
			t.Errorf("top group count: want 3, got %d", result.GetAt(0, nIdx).Int)
		}
	})

	t.Run("transform", func(t *testing.T) {
		result := loadAndQuery(t, file, "transform doubled = age * 2 | head 1")
		dIdx := result.ColIndex("doubled")
		if dIdx < 0 {
			t.Fatal("column 'doubled' not found")
		}
		ageIdx := result.ColIndex("age")
		got := result.GetAt(0, dIdx).Int
		want := result.GetAt(0, ageIdx).Int * 2
		if got != want {
			t.Errorf("doubled: want %d, got %d", want, got)
		}
	})

	t.Run("distinct", func(t *testing.T) {
		result := loadAndQuery(t, file, "distinct city")
		if result.NumRows != 3 {
			t.Errorf("expected 3 distinct cities, got %d", result.NumRows)
		}
	})

	t.Run("count", func(t *testing.T) {
		result := loadAndQuery(t, file, "count")
		if result.GetAt(0, 0).Int != 6 {
			t.Errorf("expected count 6, got %d", result.GetAt(0, 0).Int)
		}
	})

	assertFlatLengthQueries(t, file)
	assertFlatDescribeQueries(t, file, 6)
}

func assertFlatDescribeQueries(t *testing.T, file string, rows int64) {
	t.Helper()

	t.Run("describe", func(t *testing.T) {
		result := loadAndQuery(t, file, "describe")
		assertDescribeRows(t, result, map[string]describeMeta{
			"name": {typ: "string", rows: rows},
			"age":  {typ: "int", rows: rows},
			"city": {typ: "string", rows: rows},
		})
	})

	t.Run("describe_after_filter_and_output_shape", func(t *testing.T) {
		result := loadAndQuery(t, file, `filter { city == "NY" } | describe | filter { type == "string" } | select column, row_count | sort column`)
		if result.NumRows != 2 {
			t.Fatalf("expected two string metadata rows, got %d: %s", result.NumRows, result.String())
		}
		assertNameSet(t, result, "column", "city", "name")
		for i := 0; i < result.NumRows; i++ {
			got := result.GetAt(i, result.ColIndex("row_count")).Int
			if got < 1 {
				t.Fatalf("expected positive post-filter row_count, got %d", got)
			}
		}
	})
}

// assertFlatLengthQueries exercises str_len and substr on flat user files.
func assertFlatLengthQueries(t *testing.T, file string) {
	t.Helper()

	t.Run("str_len_filter", func(t *testing.T) {
		result := loadAndQuery(t, file, "filter { str_len(name) > 4 } | select name | sort name")
		assertNameSet(t, result, "name", "Alice", "Charlie", "Diana", "Frank")
	})

	t.Run("substr_prefix", func(t *testing.T) {
		result := loadAndQuery(t, file, `transform prefix = substr(name, 0, 2) | filter { prefix == "Al" } | select name`)
		if result.NumRows != 1 {
			t.Fatalf("expected 1 row, got %d", result.NumRows)
		}
		if got := result.GetAt(0, 0).Str; got != "Alice" {
			t.Errorf("expected Alice, got %q", got)
		}
	})

	t.Run("substr_unicode_literal", func(t *testing.T) {
		result := loadAndQuery(t, file, `transform part = substr("café", 3, 1) | head 1 | select part`)
		if got := result.GetAt(0, 0).Str; got != "é" {
			t.Errorf("expected %q, got %q", "é", got)
		}
	})
}

func TestIntegrationFlatCSV(t *testing.T) {
	assertFlatQueries(t, testdataDir+"/users.csv")
}

func TestIntegrationFlatAvro(t *testing.T) {
	assertFlatQueries(t, testdataDir+"/users.avro")
}

func TestIntegrationFlatParquet(t *testing.T) {
	assertFlatQueries(t, testdataDir+"/users.parquet")
}

// users.json and users.jsonl only have 3 rows (Alice, Bob, Charlie),
// so they need their own assertions.
func assertFlatQueriesSmall(t *testing.T, file string) {
	t.Helper()

	t.Run("filter", func(t *testing.T) {
		result := loadAndQuery(t, file, `filter { age > 25 }`)
		ageIdx := result.ColIndex("age")
		for i := 0; i < result.NumRows; i++ {
			if result.GetAt(i, ageIdx).Int <= 25 {
				t.Error("filter did not exclude rows with age <= 25")
			}
		}
	})

	t.Run("count", func(t *testing.T) {
		result := loadAndQuery(t, file, "count")
		if result.GetAt(0, 0).Int != 3 {
			t.Errorf("expected count 3, got %d", result.GetAt(0, 0).Int)
		}
	})

	t.Run("sort_head", func(t *testing.T) {
		result := loadAndQuery(t, file, "sort age | head 1")
		nameIdx := result.ColIndex("name")
		if result.GetAt(0, nameIdx).Str != "Bob" {
			t.Errorf("youngest should be Bob, got %q", result.GetAt(0, nameIdx).Str)
		}
	})

	assertFlatDescribeQueries(t, file, 3)
}

func TestIntegrationFlatJSON(t *testing.T) {
	assertFlatQueriesSmall(t, testdataDir+"/users.json")
}

func TestIntegrationFlatJSONL(t *testing.T) {
	assertFlatQueriesSmall(t, testdataDir+"/users.jsonl")
}

func TestIntegrationFunctionCallsWithoutTrailingCommaAllFlatFormats(t *testing.T) {
	for _, tc := range flatUserFormatFiles() {
		t.Run(tc.name, func(t *testing.T) {
			result := loadAndQuery(t, tc.file, `transform norm = upper(trim(name)), bucket = if(age > 25, coalesce(city, "unknown"), "young") | filter { starts_with(norm, "A") or str_contains(bucket, "N") } | select norm, bucket | sort norm`)
			if result.NumRows == 0 {
				t.Fatalf("expected at least one result row for %s", tc.file)
			}
			if result.ColIndex("norm") < 0 || result.ColIndex("bucket") < 0 {
				t.Fatalf("expected norm and bucket columns, got %v", result.Columns)
			}
			foundAlice := false
			normIdx := result.ColIndex("norm")
			bucketIdx := result.ColIndex("bucket")
			for i := 0; i < result.NumRows; i++ {
				if result.GetAt(i, normIdx).Str == "ALICE" && result.GetAt(i, bucketIdx).Str == "NY" {
					foundAlice = true
				}
			}
			if !foundAlice {
				t.Fatalf("expected transformed Alice row, got:\n%s", result.String())
			}
		})
	}
}

func TestIntegrationFunctionCallTrailingCommaParseErrorsAllFlatFormats(t *testing.T) {
	cases := []struct {
		name  string
		query string
	}{
		{"transform", "transform bad = upper(name,)"},
		{"filter", `filter { str_contains(name, "A",) }`},
		{"nested_call", "transform bad = upper(trim(name,))"},
		{"reduce_aggregate", "group city | reduce total = sum(age,)"},
	}

	for _, file := range flatUserFormatFiles() {
		t.Run(file.name, func(t *testing.T) {
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					_, err := parser.Parse(file.file + " | " + tc.query)
					if err == nil {
						t.Fatalf("expected parse error for %s | %s", file.file, tc.query)
					}
					if !strings.Contains(err.Error(), "expected expression after ','") {
						t.Fatalf("expected clear trailing-comma error, got: %v", err)
					}
				})
			}
		})
	}
}

// ============================================================
// Nested files (nested.{json,jsonl,avro,parquet})
// ============================================================

// assertNestedQueries runs dot-path query assertions against any nested file.
// Expected schema: id, name, address(record), tags(list), orders(list), profile(record).
func assertNestedQueries(t *testing.T, file string) {
	t.Helper()

	t.Run("filter_address_city", func(t *testing.T) {
		result := loadAndQuery(t, file, `filter { address.city == "Chicago" }`)
		if result.NumRows != 1 {
			t.Fatalf("expected 1 row, got %d", result.NumRows)
		}
		nameIdx := result.ColIndex("name")
		if result.GetAt(0, nameIdx).Str != "Charlie" {
			t.Errorf("expected Charlie, got %q", result.GetAt(0, nameIdx).Str)
		}
	})

	t.Run("filter_deep_path", func(t *testing.T) {
		result := loadAndQuery(t, file, "filter { profile.stats.logins > 10 }")
		if result.NumRows != 1 {
			t.Fatalf("expected 1 row, got %d", result.NumRows)
		}
		nameIdx := result.ColIndex("name")
		if result.GetAt(0, nameIdx).Str != "Alice" {
			t.Errorf("expected Alice, got %q", result.GetAt(0, nameIdx).Str)
		}
	})

	t.Run("transform_extract_city", func(t *testing.T) {
		result := loadAndQuery(t, file, "transform city = address.city | select name, city")
		if len(result.Columns) != 2 {
			t.Fatalf("expected 2 columns, got %v", result.Columns)
		}
		wantCities := []string{"New York", "Los Angeles", "Chicago"}
		cityIdx := result.ColIndex("city")
		for i, want := range wantCities {
			got := result.GetAt(i, cityIdx).Str
			if got != want {
				t.Errorf("row %d city: want %q, got %q", i, want, got)
			}
		}
	})

	t.Run("transform_deep_score", func(t *testing.T) {
		result := loadAndQuery(t, file, "transform score = profile.stats.score | select name, score")
		wantScores := []float64{9.5, 6.2, 0}
		scoreIdx := result.ColIndex("score")
		for i, want := range wantScores {
			got, _ := result.GetAt(i, scoreIdx).AsFloat()
			if got != want {
				t.Errorf("row %d score: want %v, got %v", i, want, got)
			}
		}
	})

	t.Run("missing_subfield_null", func(t *testing.T) {
		result := loadAndQuery(t, file, "transform x = address.nonexistent | select name, x")
		xIdx := result.ColIndex("x")
		for i := 0; i < result.NumRows; i++ {
			if !result.GetAt(i, xIdx).IsNull() {
				t.Errorf("row %d: expected null for missing field, got %v", i, result.GetAt(i, xIdx))
			}
		}
	})

	t.Run("sort_by_nested", func(t *testing.T) {
		result := loadAndQuery(t, file, "transform city = address.city | sort city | select name, city")
		// Chicago < Los Angeles < New York
		wantOrder := []string{"Charlie", "Bob", "Alice"}
		nameIdx := result.ColIndex("name")
		for i, want := range wantOrder {
			got := result.GetAt(i, nameIdx).Str
			if got != want {
				t.Errorf("row %d: want %q, got %q", i, want, got)
			}
		}
	})

	t.Run("sort_by_nested_dot_path", func(t *testing.T) {
		result := loadAndQuery(t, file, "sort profile.stats.logins | select name, profile.stats.logins")
		wantOrder := []string{"Charlie", "Bob", "Alice"}
		nameIdx := result.ColIndex("name")
		for i, want := range wantOrder {
			got := result.GetAt(i, nameIdx).Str
			if got != want {
				t.Errorf("row %d: want %q, got %q", i, want, got)
			}
		}
	})

	assertNestedLengthQueries(t, file)
	assertNestedDescribeQueries(t, file)
}

func assertNestedDescribeQueries(t *testing.T, file string) {
	t.Helper()

	t.Run("describe_nested_top_level_types", func(t *testing.T) {
		result := loadAndQuery(t, file, "describe")
		assertDescribeRows(t, result, map[string]describeMeta{
			"id":      {typ: "int", rows: 3},
			"name":    {typ: "string", rows: 3},
			"address": {typ: "record", rows: 3},
			"tags":    {typ: "list", rows: 3},
			"orders":  {typ: "list", rows: 3},
			"profile": {typ: "record", rows: 3},
		})
	})

	t.Run("describe_recursive_schema", func(t *testing.T) {
		result := loadAndQuery(t, file, "describe")
		assertDescribeSchemaRows(t, result, map[string]describeSchemaMeta{
			"id": {
				typ:    "int",
				rows:   3,
				schema: "int",
			},
			"name": {
				typ:    "string",
				rows:   3,
				schema: "string",
			},
			"address": {
				typ:    "record",
				rows:   3,
				schema: "record<city:string, street:string, zip:string>",
			},
			"tags": {
				typ:    "list",
				rows:   3,
				schema: "list<string>",
			},
			"orders": {
				typ:    "list",
				rows:   3,
				schema: "list<record<amount:float, order_id:int, status:string>>",
			},
			"profile": {
				typ:    "record",
				rows:   3,
				schema: "record<history:list<record<date:string, events:list<string>>>, stats:record<logins:int, score:float>>",
			},
		})
	})

	t.Run("describe_schema_can_be_filtered_and_selected", func(t *testing.T) {
		result := loadAndQuery(t, file, `describe | filter { schema == "record<city:string, street:string, zip:string>" } | select column, schema`)
		if result.NumRows != 1 {
			t.Fatalf("expected one schema match, got %d: %s", result.NumRows, result.String())
		}
		if got := result.Get(0, "column").Str; got != "address" {
			t.Fatalf("schema filter matched %q, want address", got)
		}
		if got := result.Get(0, "schema").Str; got != "record<city:string, street:string, zip:string>" {
			t.Fatalf("schema: got %q", got)
		}
	})

	t.Run("describe_schema_after_dot_projection_and_empty_head", func(t *testing.T) {
		result := loadAndQuery(t, file, "select address.city, profile.stats.logins | head 0 | describe")
		assertDescribeSchemaRows(t, result, map[string]describeSchemaMeta{
			"address_city": {
				typ:    "string",
				rows:   0,
				schema: "string",
			},
			"profile_stats_logins": {
				typ:    "int",
				rows:   0,
				schema: "int",
			},
		})
	})

	t.Run("describe_schema_after_rename_and_head", func(t *testing.T) {
		result := loadAndQuery(t, file, "rename address=addr | head 1 | describe")
		assertDescribeSchemaRows(t, result, map[string]describeSchemaMeta{
			"id": {
				typ:    "int",
				rows:   1,
				schema: "int",
			},
			"name": {
				typ:    "string",
				rows:   1,
				schema: "string",
			},
			"addr": {
				typ:    "record",
				rows:   1,
				schema: "record<city:string, street:string, zip:string>",
			},
			"tags": {
				typ:    "list",
				rows:   1,
				schema: "list<string>",
			},
			"orders": {
				typ:    "list",
				rows:   1,
				schema: "list<record<amount:float, order_id:int, status:string>>",
			},
			"profile": {
				typ:    "record",
				rows:   1,
				schema: "record<history:list<record<date:string, events:list<string>>>, stats:record<logins:int, score:float>>",
			},
		})
	})

	t.Run("describe_after_nested_transform", func(t *testing.T) {
		result := loadAndQuery(t, file, "transform city = address.city, order_count = list_len(orders) | select city, order_count | describe")
		assertDescribeRows(t, result, map[string]describeMeta{
			"city":        {typ: "string", rows: 3},
			"order_count": {typ: "int", rows: 3},
		})
	})
}

func TestIntegrationDescribeSchemaForFlatFormats(t *testing.T) {
	for _, file := range flatUserFormatFiles() {
		t.Run(file.name, func(t *testing.T) {
			result := loadAndQuery(t, file.file, "describe")
			rows := int64(6)
			if file.name == "json" || file.name == "jsonl" {
				rows = 3
			}
			assertDescribeSchemaRows(t, result, map[string]describeSchemaMeta{
				"name": {typ: "string", rows: rows, schema: "string"},
				"age":  {typ: "int", rows: rows, schema: "int"},
				"city": {typ: "string", rows: rows, schema: "string"},
			})
		})
	}
}

func TestIntegrationDescribeSchemaForSparseJSONRecords(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name string
		path string
		data string
	}{
		{
			name: "json",
			path: filepath.Join(dir, "sparse.json"),
			data: `[{"id":1,"s":{"x":1}},{"id":2,"s":{"y":"yes"}},{"id":3,"s":null}]`,
		},
		{
			name: "jsonl",
			path: filepath.Join(dir, "sparse.jsonl"),
			data: "{\"id\":1,\"s\":{\"x\":1}}\n{\"id\":2,\"s\":{\"y\":\"yes\"}}\n{\"id\":3,\"s\":null}\n",
		},
	}
	for _, tc := range cases {
		if err := os.WriteFile(tc.path, []byte(tc.data), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Run(tc.name, func(t *testing.T) {
			result := loadAndQuery(t, tc.path, "describe")
			assertDescribeSchemaRows(t, result, map[string]describeSchemaMeta{
				"id": {typ: "int", rows: 3, schema: "int"},
				"s":  {typ: "record", rows: 3, schema: "record<x:int?, y:string?>?"},
			})
		})
	}
}

func TestIntegrationDescribeSchemaNullOnlyNestedFieldFallback(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nulls.jsonl")
	if err := os.WriteFile(path, []byte("{\"s\":{\"x\":null}}\n{\"s\":{}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := loadAndQuery(t, path, "describe")
	assertDescribeSchemaRows(t, result, map[string]describeSchemaMeta{
		"s": {typ: "record", rows: 2, schema: "record<x:string?>"},
	})
}

// assertNestedLengthQueries exercises str_len, list_len, and substr on nested files.
func assertNestedLengthQueries(t *testing.T, file string) {
	t.Helper()

	t.Run("str_len_name", func(t *testing.T) {
		result := loadAndQuery(t, file, "transform name_len = str_len(name) | select name, name_len")
		assertIntColByName(t, result, "name", "name_len", map[string]int64{
			"Alice": 5, "Bob": 3, "Charlie": 7,
		})
	})

	t.Run("str_len_filter", func(t *testing.T) {
		result := loadAndQuery(t, file, "filter { str_len(name) > 5 } | select name")
		assertNameSet(t, result, "name", "Charlie")
	})

	t.Run("list_len_orders", func(t *testing.T) {
		result := loadAndQuery(t, file, "transform n = list_len(orders) | select name, n")
		assertIntColByName(t, result, "name", "n", map[string]int64{
			"Alice": 2, "Bob": 1, "Charlie": 0,
		})
	})

	t.Run("list_len_filter_orders", func(t *testing.T) {
		result := loadAndQuery(t, file, "filter { list_len(orders) > 1 } | select name")
		assertNameSet(t, result, "name", "Alice")
	})

	t.Run("list_len_filter_tags", func(t *testing.T) {
		result := loadAndQuery(t, file, "filter { list_len(tags) >= 2 } | select name")
		assertNameSet(t, result, "name", "Alice", "Charlie")
	})

	t.Run("list_len_dot_path_history", func(t *testing.T) {
		result := loadAndQuery(t, file, "transform n = list_len(profile.history) | select name, n")
		assertIntColByName(t, result, "name", "n", map[string]int64{
			"Alice": 2, "Bob": 1, "Charlie": 0,
		})
	})

	t.Run("substr_name_prefix", func(t *testing.T) {
		result := loadAndQuery(t, file, `transform prefix = substr(name, 0, 2) | select name, prefix`)
		want := map[string]string{"Alice": "Al", "Bob": "Bo", "Charlie": "Ch"}
		nameIdx := result.ColIndex("name")
		prefixIdx := result.ColIndex("prefix")
		for i := 0; i < result.NumRows; i++ {
			name := result.GetAt(i, nameIdx).Str
			if w, ok := want[name]; ok {
				if got := result.GetAt(i, prefixIdx).Str; got != w {
					t.Errorf("%s prefix: want %q, got %q", name, w, got)
				}
			}
		}
	})

	t.Run("substr_unicode_literal", func(t *testing.T) {
		result := loadAndQuery(t, file, `transform part = substr("日本語", 1, 2) | head 1 | select part`)
		if got := result.GetAt(0, 0).Str; got != "本語" {
			t.Errorf("expected %q, got %q", "本語", got)
		}
	})

	t.Run("substr_negative_start", func(t *testing.T) {
		result := loadAndQuery(t, file, `transform suffix = substr(name, -1, 1) | select name, suffix`)
		want := map[string]string{"Alice": "e", "Bob": "b", "Charlie": "e"}
		nameIdx := result.ColIndex("name")
		suffixIdx := result.ColIndex("suffix")
		for i := 0; i < result.NumRows; i++ {
			name := result.GetAt(i, nameIdx).Str
			if w, ok := want[name]; ok {
				if got := result.GetAt(i, suffixIdx).Str; got != w {
					t.Errorf("%s suffix: want %q, got %q", name, w, got)
				}
			}
		}
	})
}

func TestIntegrationNestedMissingJSON(t *testing.T) {
	t.Helper()
	file := testdataDir + "/nested_missing.json"

	t.Run("select_null_parent", func(t *testing.T) {
		result := loadAndQuery(t, file, "select name, addr.city")
		if result.NumRows != 2 {
			t.Fatalf("expected 2 rows, got %d", result.NumRows)
		}
		cityIdx := result.ColIndex("addr_city")
		if !result.GetAt(0, cityIdx).IsNull() {
			t.Errorf("row 0 addr.city: expected null, got %v", result.GetAt(0, cityIdx))
		}
		if got := result.GetAt(1, cityIdx).Str; got != "NY" {
			t.Errorf("row 1 addr.city: expected NY, got %q", got)
		}
	})

	t.Run("filter_equality", func(t *testing.T) {
		result := loadAndQuery(t, file, `filter { addr.city == "NY" }`)
		if result.NumRows != 1 {
			t.Fatalf("expected 1 row, got %d", result.NumRows)
		}
		if got := result.GetAt(0, result.ColIndex("name")).Str; got != "b" {
			t.Errorf("expected name b, got %q", got)
		}
	})

	t.Run("filter_is_null", func(t *testing.T) {
		result := loadAndQuery(t, file, "filter { addr.city is null }")
		if result.NumRows != 1 {
			t.Fatalf("expected 1 row, got %d", result.NumRows)
		}
		if got := result.GetAt(0, result.ColIndex("name")).Str; got != "a" {
			t.Errorf("expected name a, got %q", got)
		}
	})
}

func TestIntegrationNestedJSON(t *testing.T) {
	assertNestedQueries(t, testdataDir+"/nested.json")
}

func TestIntegrationNestedJSONL(t *testing.T) {
	assertNestedQueries(t, testdataDir+"/nested.jsonl")
}

func TestIntegrationNestedAvro(t *testing.T) {
	assertNestedQueries(t, testdataDir+"/nested.avro")
}

func TestIntegrationNestedParquet(t *testing.T) {
	assertNestedQueries(t, testdataDir+"/nested.parquet")
}

func TestReduceOrdersSumAmount(t *testing.T) {
	result := loadAndQuery(t, testdataDir+"/nested.json", "reduce orders total = sum(amount) | select name, total | sort name")
	nameIdx := result.ColIndex("name")
	totalIdx := result.ColIndex("total")
	want := map[string]struct {
		null  bool
		total float64
	}{
		"Alice":   {total: 188.99},
		"Bob":     {total: 39.99},
		"Charlie": {null: true},
	}
	if result.NumRows != len(want) {
		t.Fatalf("expected %d rows, got %d", len(want), result.NumRows)
	}
	for i := 0; i < result.NumRows; i++ {
		name := result.GetAt(i, nameIdx).Str
		w, ok := want[name]
		if !ok {
			t.Fatalf("unexpected name %q", name)
		}
		v := result.GetAt(i, totalIdx)
		if w.null {
			if !v.IsNull() {
				t.Errorf("%s: want null total, got %v", name, v.AsString())
			}
			continue
		}
		f, ok := v.AsFloat()
		if !ok || f != w.total {
			t.Errorf("%s: want %v, got %v", name, w.total, v.AsString())
		}
	}
}

// assertNestedDotPathOps tests select and group with dot-path columns on nested files.
func assertNestedDotPathOps(t *testing.T, file string) {
	t.Helper()

	t.Run("select_dot_path", func(t *testing.T) {
		result := loadAndQuery(t, file, "select name, address.city")
		if len(result.Columns) != 2 {
			t.Fatalf("expected 2 columns, got %v", result.Columns)
		}
		if result.Columns[1] != "address_city" {
			t.Errorf("expected column name 'address_city', got %q", result.Columns[1])
		}
		wantCities := []string{"New York", "Los Angeles", "Chicago"}
		for i, want := range wantCities {
			got := result.GetAt(i, 1).Str
			if got != want {
				t.Errorf("row %d: want %q, got %q", i, want, got)
			}
		}
	})

	t.Run("select_deep_dot_path", func(t *testing.T) {
		result := loadAndQuery(t, file, "select name, profile.stats.score")
		if result.Columns[1] != "profile_stats_score" {
			t.Errorf("expected column name 'profile_stats_score', got %q", result.Columns[1])
		}
	})

	t.Run("select_dot_path_dedup", func(t *testing.T) {
		result := loadAndQuery(t, file, `transform address_city = "test" | select address_city, address.city`)
		if len(result.Columns) != 2 {
			t.Fatalf("expected 2 columns, got %v", result.Columns)
		}
		if result.Columns[0] != "address_city" {
			t.Errorf("col 0: expected 'address_city', got %q", result.Columns[0])
		}
		if result.Columns[1] != "address_city_2" {
			t.Errorf("col 1: expected 'address_city_2', got %q", result.Columns[1])
		}
		// First column should all be "test", second should be actual cities
		for i := 0; i < result.NumRows; i++ {
			if result.GetAt(i, 0).Str != "test" {
				t.Errorf("col 0: expected 'test', got %q", result.GetAt(i, 0).Str)
			}
			if result.GetAt(i, 1).Str == "test" || result.GetAt(i, 1).Str == "" {
				t.Errorf("col 1: expected a real city, got %q", result.GetAt(i, 1).Str)
			}
		}
	})

	t.Run("group_dot_path", func(t *testing.T) {
		result := loadAndQuery(t, file, "group address.city | reduce n = count() | remove grouped")
		if result.Columns[0] != "address_city" {
			t.Errorf("expected column name 'address_city', got %q", result.Columns[0])
		}
		if result.NumRows != 3 {
			t.Fatalf("expected 3 groups, got %d", result.NumRows)
		}
	})

	t.Run("distinct_dot_path", func(t *testing.T) {
		result := loadAndQuery(t, file, "distinct address.city")
		if result.NumRows != 3 {
			t.Fatalf("expected 3 distinct cities, got %d", result.NumRows)
		}
	})

	t.Run("reduce_aggregate_dot_path", func(t *testing.T) {
		result := loadAndQuery(t, file, "group address.city | reduce avg_score = avg(profile.stats.score), max_logins = max(profile.stats.logins) | remove grouped")
		if result.NumRows != 3 {
			t.Fatalf("expected 3 groups, got %d", result.NumRows)
		}
		cityIdx := result.ColIndex("address_city")
		avgIdx := result.ColIndex("avg_score")
		maxIdx := result.ColIndex("max_logins")
		for i := 0; i < result.NumRows; i++ {
			switch result.GetAt(i, cityIdx).Str {
			case "New York":
				if got := result.GetAt(i, avgIdx).Float; got != 9.5 {
					t.Errorf("New York avg_score: want 9.5, got %v", got)
				}
				if got := result.GetAt(i, maxIdx).Int; got != 42 {
					t.Errorf("New York max_logins: want 42, got %d", got)
				}
			case "Los Angeles":
				if got := result.GetAt(i, avgIdx).Float; got != 6.2 {
					t.Errorf("Los Angeles avg_score: want 6.2, got %v", got)
				}
				if got := result.GetAt(i, maxIdx).Int; got != 7 {
					t.Errorf("Los Angeles max_logins: want 7, got %d", got)
				}
			case "Chicago":
				if got := result.GetAt(i, avgIdx).Float; got != 0 {
					t.Errorf("Chicago avg_score: want 0, got %v", got)
				}
				if got := result.GetAt(i, maxIdx).Int; got != 0 {
					t.Errorf("Chicago max_logins: want 0, got %d", got)
				}
			default:
				t.Errorf("unexpected city %q", result.GetAt(i, cityIdx).Str)
			}
		}
	})
}

func TestIntegrationNestedDotPathJSON(t *testing.T) {
	assertNestedDotPathOps(t, testdataDir+"/nested.json")
}

func TestIntegrationNestedDotPathJSONL(t *testing.T) {
	assertNestedDotPathOps(t, testdataDir+"/nested.jsonl")
}

func TestIntegrationNestedDotPathAvro(t *testing.T) {
	assertNestedDotPathOps(t, testdataDir+"/nested.avro")
}

func TestIntegrationNestedDotPathParquet(t *testing.T) {
	assertNestedDotPathOps(t, testdataDir+"/nested.parquet")
}

// ============================================================
// Column type widening (mixed_types.csv)
// ============================================================

func TestIntegrationColumnTypeWidening(t *testing.T) {
	// val column: 1 (int) → 2.5 (float) → "something" (string)
	// After loading, the whole column must be TypeString and all three
	// values must survive the round-trip through filter + select.
	result := loadAndQuery(t, testdataDir+"/mixed_types.csv", "select id, val")

	if result.NumRows != 3 {
		t.Fatalf("expected 3 rows, got %d", result.NumRows)
	}

	valIdx := result.ColIndex("val")
	if valIdx < 0 {
		t.Fatal("column 'val' not found")
	}
	if result.Col(valIdx).ColType() != table.TypeString {
		t.Fatalf("expected column type String after widening, got %v", result.Col(valIdx).ColType())
	}

	want := []string{"1", "2.5", "something"}
	for i, w := range want {
		got := result.GetAt(i, valIdx)
		if got.Type != table.TypeString {
			t.Errorf("row %d: expected TypeString, got %v", i, got.Type)
		}
		if got.Str != w {
			t.Errorf("row %d: want %q, got %q", i, w, got.Str)
		}
	}

	t.Run("filter_on_widened_column", func(t *testing.T) {
		// filter compares the widened string column against exact string values.
		result := loadAndQuery(t, testdataDir+"/mixed_types.csv", `filter { val == "something" }`)
		if result.NumRows != 1 {
			t.Fatalf("expected 1 row, got %d", result.NumRows)
		}
		if result.GetAt(0, result.ColIndex("val")).Str != "something" {
			t.Errorf("unexpected val: %q", result.GetAt(0, result.ColIndex("val")).Str)
		}
	})

	t.Run("describe_preserves_widened_type_with_surviving_rows", func(t *testing.T) {
		result := loadAndQuery(t, testdataDir+"/mixed_types.csv", `filter { val == "something" } | describe`)
		assertDescribeRows(t, result, map[string]describeMeta{
			"id":  {typ: "int", rows: 1},
			"val": {typ: "string", rows: 1},
		})
	})

	t.Run("describe_zero_surviving_rows_reports_null_types", func(t *testing.T) {
		result := loadAndQuery(t, testdataDir+"/mixed_types.csv", `filter { false } | describe`)
		assertDescribeRows(t, result, map[string]describeMeta{
			"id":  {typ: "null", rows: 0},
			"val": {typ: "null", rows: 0},
		})
	})
}

// TestIntegrationFilterTypeSafety covers the stricter comparison contract.
func TestIntegrationFilterTypeSafety(t *testing.T) {
	cases := []struct {
		name    string
		query   string
		want    int
		wantErr string
	}{
		{name: "eq_int_literal", query: `filter { val == 1 }`, wantErr: "cannot compare string with int"},
		{name: "eq_float_literal", query: `filter { val == 2.5 }`, wantErr: "cannot compare string with float"},
		{name: "eq_string_literal", query: `filter { val == "1" }`, want: 1},
		{name: "eq_string_literal2", query: `filter { val == "something" }`, want: 1},
		{name: "neq_int_literal", query: `filter { val != 1 }`, wantErr: "cannot compare string with int"},
		{name: "gt_int_literal", query: `filter { val > 1 }`, wantErr: "cannot compare string with int"},
		{name: "gt_int_literal2", query: `filter { val > 9 }`, wantErr: "cannot compare string with int"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.wantErr != "" {
				err := loadAndQueryExpectErr(t, testdataDir+"/mixed_types.csv", c.query)
				if err == nil {
					t.Fatalf("expected error containing %q", c.wantErr)
				}
				if !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("expected error containing %q, got %v", c.wantErr, err)
				}
				return
			}
			result := loadAndQuery(t, testdataDir+"/mixed_types.csv", c.query)
			if result.NumRows != c.want {
				t.Fatalf("%s: expected %d rows, got %d", c.query, c.want, result.NumRows)
			}
		})
	}
}

// ============================================================
// Stdin source (-)
// ============================================================

func TestIntegrationStdinDashOnly(t *testing.T) {
	data, err := os.ReadFile(testdataDir + "/sales.csv")
	if err != nil {
		t.Fatal(err)
	}

	tbl, err := loader.LoadInput("-", loader.Options{Format: "csv"}, strings.NewReader(string(data)))
	if err != nil {
		t.Fatalf("load stdin: %v", err)
	}

	q, err := parser.Parse("-")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	result, err := Execute(q, tbl, nil)
	if err != nil {
		t.Fatalf("exec: %v", err)
	}

	if result.NumRows != 9 {
		t.Fatalf("expected 9 sales rows, got %d", result.NumRows)
	}
}

func TestIntegrationStdinPipeline(t *testing.T) {
	data, err := os.ReadFile(testdataDir + "/users.csv")
	if err != nil {
		t.Fatal(err)
	}

	tbl, err := loader.LoadInput("-", loader.Options{Format: "csv"}, strings.NewReader(string(data)))
	if err != nil {
		t.Fatalf("load stdin: %v", err)
	}

	q, err := parser.Parse(`- | filter { age > 25 }`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	result, err := Execute(q, tbl, nil)
	if err != nil {
		t.Fatalf("exec: %v", err)
	}

	if result.NumRows != 4 {
		t.Fatalf("expected 4 rows with age > 25, got %d", result.NumRows)
	}
}

func TestIntegrationStringPredicates(t *testing.T) {
	file := testdataDir + "/users.csv"

	t.Run("str_contains_filter", func(t *testing.T) {
		result := loadAndQuery(t, file, `filter { str_contains(city, "NY") } | select name | sort name`)
		want := []string{"Alice", "Charlie", "Frank"}
		if result.NumRows != len(want) {
			t.Fatalf("expected %d rows, got %d", len(want), result.NumRows)
		}
		nameIdx := result.ColIndex("name")
		for i, w := range want {
			if got := result.GetAt(i, nameIdx).Str; got != w {
				t.Errorf("row %d: expected %q, got %q", i, w, got)
			}
		}
	})

	t.Run("matches_filter", func(t *testing.T) {
		result := loadAndQuery(t, file, `filter { matches(name, "^[AB]") } | select name | sort name`)
		want := []string{"Alice", "Bob"}
		if result.NumRows != len(want) {
			t.Fatalf("expected %d rows, got %d", len(want), result.NumRows)
		}
		nameIdx := result.ColIndex("name")
		for i, w := range want {
			if got := result.GetAt(i, nameIdx).Str; got != w {
				t.Errorf("row %d: expected %q, got %q", i, w, got)
			}
		}
	})
}

func TestJoinIntegration(t *testing.T) {
	usersFile := testdataDir + "/users.csv"
	ordersFile := testdataDir + "/orders.csv"
	tbl, err := loader.Load(usersFile, loader.Options{})
	if err != nil {
		t.Fatalf("load users: %v", err)
	}
	q, err := parser.Parse(usersFile + ` | join ` + ordersFile + ` on name == user_name | sort name, product`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	load := func(filename string, opts ast.LoadOptions) (*table.Table, error) {
		return loader.Load(filename, loader.FromAST(opts))
	}
	result, err := Execute(q, tbl, load)
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if result.NumRows != 4 {
		t.Fatalf("expected 4 rows, got %d", result.NumRows)
	}
	if result.ColIndex("product") < 0 {
		t.Fatal("expected product column from orders")
	}

	q, err = parser.Parse(usersFile + ` | join ` + ordersFile + ` on name == user_name | describe`)
	if err != nil {
		t.Fatalf("parse describe: %v", err)
	}
	result, err = Execute(q, tbl, load)
	if err != nil {
		t.Fatalf("exec describe: %v", err)
	}
	assertDescribeRows(t, result, map[string]describeMeta{
		"name":     {typ: "string", rows: 4},
		"age":      {typ: "int", rows: 4},
		"city":     {typ: "string", rows: 4},
		"order_id": {typ: "int", rows: 4},
		"product":  {typ: "string", rows: 4},
		"amount":   {typ: "int", rows: 4},
	})
}

func TestIntegrationGlobPrimarySource(t *testing.T) {
	result := loadAndQuery(t, testdataDir+"/glob/users-*.csv", "count")
	if result.NumRows != 1 || result.Get(0, "count").Int != 2 {
		t.Fatalf("expected count 2, got %s", result.String())
	}

	result = loadAndQuery(t, testdataDir+"/glob/users-*.csv", "describe")
	assertDescribeRows(t, result, map[string]describeMeta{
		"name": {typ: "string", rows: 2},
		"age":  {typ: "int", rows: 2},
		"city": {typ: "string", rows: 2},
	})
}

func TestIntegrationGlobJoin(t *testing.T) {
	usersFile := testdataDir + "/users.csv"
	ordersGlob := testdataDir + "/glob/orders-*.csv"

	tbl, err := loader.Load(usersFile, loader.Options{})
	if err != nil {
		t.Fatalf("load users: %v", err)
	}
	q, err := parser.Parse(usersFile + ` | join ` + ordersGlob + ` on name == user_name | sort name`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	load := func(filename string, opts ast.LoadOptions) (*table.Table, error) {
		return loader.Load(filename, loader.FromAST(opts))
	}
	result, err := Execute(q, tbl, load)
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if result.NumRows != 2 {
		t.Fatalf("expected 2 rows, got %d", result.NumRows)
	}
	if result.Get(0, "name").Str != "Alice" || result.Get(0, "order_id").Int != 101 {
		t.Errorf("row 0: got %s", result.String())
	}
	if result.Get(1, "name").Str != "Bob" || result.Get(1, "status").Str != "pending" {
		t.Errorf("row 1: got %s", result.String())
	}
}

func TestIntegrationJoinExactTypeMismatchOnWidenedCSV(t *testing.T) {
	dir := t.TempDir()
	leftFile := filepath.Join(dir, "left.csv")
	rightFile := filepath.Join(dir, "right.csv")

	leftData := "id,name\n1,Alice\n"
	rightData := "id,note\n1,string-key\nx,other\n"

	if err := os.WriteFile(leftFile, []byte(leftData), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rightFile, []byte(rightData), 0o644); err != nil {
		t.Fatal(err)
	}

	rightTbl, err := loader.Load(rightFile, loader.Options{})
	if err != nil {
		t.Fatalf("load right: %v", err)
	}
	if rightTbl.Col(0).ColType() != table.TypeString {
		t.Fatalf("expected widened string column on right, got %v", rightTbl.Col(0).ColType())
	}
	if got := rightTbl.Get(0, "id"); got.Type != table.TypeString || got.Str != "1" {
		t.Fatalf("expected right id to be string \"1\", got %v", got)
	}

	leftTbl, err := loader.Load(leftFile, loader.Options{})
	if err != nil {
		t.Fatalf("load left: %v", err)
	}

	q, err := parser.Parse(leftFile + ` | join ` + rightFile + ` on id | sort name`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	load := func(filename string, opts ast.LoadOptions) (*table.Table, error) {
		return loader.Load(filename, loader.FromAST(opts))
	}
	result, err := Execute(q, leftTbl, load)
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if result.NumRows != 0 {
		t.Fatalf("expected no join matches for int vs widened string keys, got %d rows: %s", result.NumRows, result.String())
	}
}

func TestIntegrationGlobJoinCollisionPrefix(t *testing.T) {
	left := table.NewTable([]string{"name", "note"})
	left.AddRow([]table.Value{table.StrVal("Alice"), table.StrVal("left-note")})
	left.AddRow([]table.Value{table.StrVal("Bob"), table.StrVal("left-note-2")})

	q, err := parser.Parse("left.csv | join left " + testdataDir + "/glob/collision-*.csv on name")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	load := func(filename string, opts ast.LoadOptions) (*table.Table, error) {
		return loader.Load(filename, loader.FromAST(opts))
	}
	result, err := Execute(q, left, load)
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if result.ColIndex("collision_note") < 0 {
		t.Fatalf("expected collision_note column, got %v", result.Columns)
	}
	if result.Get(0, "note").Str != "left-note" || result.Get(0, "collision_note").Str != "from-shard-a" {
		t.Errorf("Alice row: got %s", result.String())
	}
	if result.Get(1, "note").Str != "left-note-2" || result.Get(1, "collision_note").Str != "from-shard-b" {
		t.Errorf("Bob row: got %s", result.String())
	}
}

func TestIntegrationGlobRecursivePrimary(t *testing.T) {
	result := loadAndQuery(t, testdataDir+"/glob/recursive/**/*.csv", "count")
	if result.NumRows != 1 || result.Get(0, "count").Int != 2 {
		t.Fatalf("expected count 2, got %s", result.String())
	}
}

func TestIntegrationPrimarySourceWithLoadOptions(t *testing.T) {
	t.Run("format_override_extensionless", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "data.dat")
		if err := os.WriteFile(path, []byte("name\nAlice\nBob\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		result := loadAndQuery(t, path+` with format=csv`, "count")
		if result.NumRows != 1 || result.Get(0, "count").Int != 2 {
			t.Fatalf("got %s", result.String())
		}
	})

	t.Run("glob_format_override", func(t *testing.T) {
		dir := t.TempDir()
		glob := filepath.Join(dir, "part-*.dat")
		if err := os.WriteFile(filepath.Join(dir, "part-001.dat"), []byte("id\n1\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "part-002.dat"), []byte("id\n2\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		result := loadAndQuery(t, glob+` with format=csv`, "count")
		if result.NumRows != 1 || result.Get(0, "count").Int != 2 {
			t.Fatalf("got %s", result.String())
		}
	})

	t.Run("header_false", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "rows.dat")
		if err := os.WriteFile(path, []byte("1,2\n3,4\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		result := loadAndQuery(t, path+` with format=csv, header=false`, "count")
		if result.NumRows != 1 || result.Get(0, "count").Int != 2 {
			t.Fatalf("got %s", result.String())
		}
	})

	t.Run("delim_semicolon", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "semi.dat")
		if err := os.WriteFile(path, []byte("a;b\n1;2\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		result := loadAndQuery(t, path+` with format=csv, delim=";"`, "select a")
		if result.NumRows != 1 || result.Get(0, "a").Int != 1 {
			t.Fatalf("got %s", result.String())
		}
	})

	t.Run("delim_without_format_on_csv", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "semi.csv")
		if err := os.WriteFile(path, []byte("a;b\n1;2\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		result := loadAndQuery(t, path+` with delim=";"`, "select a")
		if result.NumRows != 1 || result.Get(0, "a").Int != 1 {
			t.Fatalf("got %s", result.String())
		}
	})

	t.Run("gzip_double_extension", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "events.jsonl.gz")
		data := "{\"level\":\"INFO\",\"message\":\"start\"}\n{\"level\":\"ERROR\",\"message\":\"timeout\"}\n"
		if err := os.WriteFile(path, gzipIntegrationBytes(t, data), 0o644); err != nil {
			t.Fatal(err)
		}
		result := loadAndQuery(t, path, `filter { level == "ERROR" } | select message`)
		if result.NumRows != 1 || result.Get(0, "message").Str != "timeout" {
			t.Fatalf("got %s", result.String())
		}
	})

	t.Run("gzip_csv_double_extension", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "users.csv.gz")
		data := "name,level\nAlice,INFO\nBob,ERROR\n"
		if err := os.WriteFile(path, gzipIntegrationBytes(t, data), 0o644); err != nil {
			t.Fatal(err)
		}
		result := loadAndQuery(t, path, `filter { level == "ERROR" } | select name`)
		if result.NumRows != 1 || result.Get(0, "name").Str != "Bob" {
			t.Fatalf("got %s", result.String())
		}
	})

	t.Run("gzip_explicit_compression", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "events.logdata")
		data := "{\"level\":\"INFO\",\"message\":\"start\"}\n{\"level\":\"ERROR\",\"message\":\"timeout\"}\n"
		if err := os.WriteFile(path, gzipIntegrationBytes(t, data), 0o644); err != nil {
			t.Fatal(err)
		}
		result := loadAndQuery(t, path+` with format=jsonl, compression=gzip`, `filter { level == "ERROR" } | count`)
		if result.NumRows != 1 || result.Get(0, "count").Int != 1 {
			t.Fatalf("got %s", result.String())
		}
	})

	t.Run("gzip_explicit_format_overrides_inner_suffix", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "events.csv.gz")
		data := "{\"level\":\"ERROR\",\"message\":\"jsonl despite suffix\"}\n"
		if err := os.WriteFile(path, gzipIntegrationBytes(t, data), 0o644); err != nil {
			t.Fatal(err)
		}
		result := loadAndQuery(t, path+` with format=jsonl`, `select message`)
		if result.NumRows != 1 || result.Get(0, "message").Str != "jsonl despite suffix" {
			t.Fatalf("got %s", result.String())
		}
	})

	t.Run("zstd_double_extension", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "events.jsonl.zst")
		data := "{\"level\":\"INFO\",\"message\":\"start\"}\n{\"level\":\"ERROR\",\"message\":\"timeout\"}\n"
		if err := os.WriteFile(path, zstdIntegrationBytes(t, data), 0o644); err != nil {
			t.Fatal(err)
		}
		result := loadAndQuery(t, path, `filter { level == "ERROR" } | select message`)
		if result.NumRows != 1 || result.Get(0, "message").Str != "timeout" {
			t.Fatalf("got %s", result.String())
		}
	})

	t.Run("zstd_explicit_compression", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "events.logdata")
		data := "{\"level\":\"INFO\",\"message\":\"start\"}\n{\"level\":\"ERROR\",\"message\":\"timeout\"}\n"
		if err := os.WriteFile(path, zstdIntegrationBytes(t, data), 0o644); err != nil {
			t.Fatal(err)
		}
		result := loadAndQuery(t, path+` with format=jsonl, compression=zstd`, `filter { level == "ERROR" } | count`)
		if result.NumRows != 1 || result.Get(0, "count").Int != 1 {
			t.Fatalf("got %s", result.String())
		}
	})

	t.Run("zstd_explicit_format_overrides_inner_suffix", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "events.csv.zst")
		data := "{\"level\":\"ERROR\",\"message\":\"jsonl despite suffix\"}\n"
		if err := os.WriteFile(path, zstdIntegrationBytes(t, data), 0o644); err != nil {
			t.Fatal(err)
		}
		result := loadAndQuery(t, path+` with format=jsonl`, `select message`)
		if result.NumRows != 1 || result.Get(0, "message").Str != "jsonl despite suffix" {
			t.Fatalf("got %s", result.String())
		}
	})

	t.Run("deflate_double_extension", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "events.jsonl.deflate")
		data := "{\"level\":\"INFO\",\"message\":\"start\"}\n{\"level\":\"ERROR\",\"message\":\"timeout\"}\n"
		if err := os.WriteFile(path, deflateIntegrationBytes(t, data), 0o644); err != nil {
			t.Fatal(err)
		}
		result := loadAndQuery(t, path, `filter { level == "ERROR" } | select message`)
		if result.NumRows != 1 || result.Get(0, "message").Str != "timeout" {
			t.Fatalf("got %s", result.String())
		}
	})

	t.Run("deflate_zlib_double_extension", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "events.jsonl.zlib")
		data := "{\"level\":\"INFO\",\"message\":\"start\"}\n{\"level\":\"ERROR\",\"message\":\"timeout\"}\n"
		if err := os.WriteFile(path, deflateIntegrationBytes(t, data), 0o644); err != nil {
			t.Fatal(err)
		}
		result := loadAndQuery(t, path, `filter { level == "ERROR" } | select message`)
		if result.NumRows != 1 || result.Get(0, "message").Str != "timeout" {
			t.Fatalf("got %s", result.String())
		}
	})

	t.Run("deflate_explicit_compression", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "events.logdata")
		data := "{\"level\":\"INFO\",\"message\":\"start\"}\n{\"level\":\"ERROR\",\"message\":\"timeout\"}\n"
		if err := os.WriteFile(path, deflateIntegrationBytes(t, data), 0o644); err != nil {
			t.Fatal(err)
		}
		result := loadAndQuery(t, path+` with format=jsonl, compression=deflate`, `filter { level == "ERROR" } | count`)
		if result.NumRows != 1 || result.Get(0, "count").Int != 1 {
			t.Fatalf("got %s", result.String())
		}
	})

	t.Run("deflate_explicit_format_overrides_inner_suffix", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "events.csv.deflate")
		data := "{\"level\":\"ERROR\",\"message\":\"jsonl despite suffix\"}\n"
		if err := os.WriteFile(path, deflateIntegrationBytes(t, data), 0o644); err != nil {
			t.Fatal(err)
		}
		result := loadAndQuery(t, path+` with format=jsonl`, `select message`)
		if result.NumRows != 1 || result.Get(0, "message").Str != "jsonl despite suffix" {
			t.Fatalf("got %s", result.String())
		}
	})
}

func TestIntegrationJoinGzipCompressedSource(t *testing.T) {
	dir := t.TempDir()
	usersPath := filepath.Join(dir, "users.csv")
	ordersPath := filepath.Join(dir, "orders.csv.gz")
	if err := os.WriteFile(usersPath, []byte("user_id,name,note\n1,Alice,left-note\n2,Bob,left-note-2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ordersPath, gzipIntegrationBytes(t, "user_id,total,note\n1,10,first\n2,20,second\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	left, err := loader.Load(usersPath, loader.Options{})
	if err != nil {
		t.Fatal(err)
	}
	q, err := parser.Parse(usersPath + ` | join ` + ordersPath + ` on user_id | sort user_id`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	result, err := Execute(q, left, func(filename string, opts ast.LoadOptions) (*table.Table, error) {
		return loader.Load(filename, loader.FromAST(opts))
	})
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if result.NumRows != 2 || result.Get(0, "total").Int != 10 || result.Get(1, "total").Int != 20 {
		t.Fatalf("unexpected table: %s", result.String())
	}
	if result.ColIndex("orders_csv_note") < 0 {
		t.Fatalf("expected colliding right note column to be prefixed, got %v", result.Columns)
	}
}

func TestIntegrationJoinZstdCompressedSource(t *testing.T) {
	dir := t.TempDir()
	usersPath := filepath.Join(dir, "users.csv")
	ordersPath := filepath.Join(dir, "orders.csv.zst")
	if err := os.WriteFile(usersPath, []byte("user_id,name,note\n1,Alice,left-note\n2,Bob,left-note-2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ordersPath, zstdIntegrationBytes(t, "user_id,total,note\n1,10,first\n2,20,second\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	left, err := loader.Load(usersPath, loader.Options{})
	if err != nil {
		t.Fatal(err)
	}
	q, err := parser.Parse(usersPath + ` | join ` + ordersPath + ` on user_id | sort user_id`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	result, err := Execute(q, left, func(filename string, opts ast.LoadOptions) (*table.Table, error) {
		return loader.Load(filename, loader.FromAST(opts))
	})
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if result.NumRows != 2 || result.Get(0, "total").Int != 10 || result.Get(1, "total").Int != 20 {
		t.Fatalf("unexpected table: %s", result.String())
	}
	if result.ColIndex("orders_csv_note") < 0 {
		t.Fatalf("expected colliding right note column to be prefixed, got %v", result.Columns)
	}
}

func TestIntegrationJoinDeflateCompressedSource(t *testing.T) {
	dir := t.TempDir()
	usersPath := filepath.Join(dir, "users.csv")
	ordersPath := filepath.Join(dir, "orders.csv.deflate")
	if err := os.WriteFile(usersPath, []byte("user_id,name,note\n1,Alice,left-note\n2,Bob,left-note-2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ordersPath, deflateIntegrationBytes(t, "user_id,total,note\n1,10,first\n2,20,second\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	left, err := loader.Load(usersPath, loader.Options{})
	if err != nil {
		t.Fatal(err)
	}
	q, err := parser.Parse(usersPath + ` | join ` + ordersPath + ` on user_id | sort user_id`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	result, err := Execute(q, left, func(filename string, opts ast.LoadOptions) (*table.Table, error) {
		return loader.Load(filename, loader.FromAST(opts))
	})
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if result.NumRows != 2 || result.Get(0, "total").Int != 10 || result.Get(1, "total").Int != 20 {
		t.Fatalf("unexpected table: %s", result.String())
	}
	if result.ColIndex("orders_csv_note") < 0 {
		t.Fatalf("expected colliding right note column to be prefixed, got %v", result.Columns)
	}
}

func TestIntegrationJoinExplicitDeflateCompressionSource(t *testing.T) {
	dir := t.TempDir()
	usersPath := filepath.Join(dir, "users.csv")
	ordersPath := filepath.Join(dir, "orders.data")
	if err := os.WriteFile(usersPath, []byte("user_id,name\n1,Alice\n2,Bob\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ordersPath, deflateIntegrationBytes(t, "user_id,total\n1,10\n2,20\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	left, err := loader.Load(usersPath, loader.Options{})
	if err != nil {
		t.Fatal(err)
	}
	q, err := parser.Parse(usersPath + ` | join ` + ordersPath + ` with format=csv, compression=deflate on user_id | sort user_id`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	result, err := Execute(q, left, func(filename string, opts ast.LoadOptions) (*table.Table, error) {
		return loader.Load(filename, loader.FromAST(opts))
	})
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if result.NumRows != 2 || result.Get(0, "total").Int != 10 || result.Get(1, "total").Int != 20 {
		t.Fatalf("unexpected table: %s", result.String())
	}
}

func TestIntegrationJoinExplicitZstdCompressionSource(t *testing.T) {
	dir := t.TempDir()
	usersPath := filepath.Join(dir, "users.csv")
	ordersPath := filepath.Join(dir, "orders.data")
	if err := os.WriteFile(usersPath, []byte("user_id,name\n1,Alice\n2,Bob\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ordersPath, zstdIntegrationBytes(t, "user_id,total\n1,10\n2,20\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	left, err := loader.Load(usersPath, loader.Options{})
	if err != nil {
		t.Fatal(err)
	}
	q, err := parser.Parse(usersPath + ` | join ` + ordersPath + ` with format=csv, compression=zstd on user_id | sort user_id`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	result, err := Execute(q, left, func(filename string, opts ast.LoadOptions) (*table.Table, error) {
		return loader.Load(filename, loader.FromAST(opts))
	})
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if result.NumRows != 2 || result.Get(0, "total").Int != 10 || result.Get(1, "total").Int != 20 {
		t.Fatalf("unexpected table: %s", result.String())
	}
}

func TestIntegrationJoinExplicitGzipCompressionSource(t *testing.T) {
	dir := t.TempDir()
	usersPath := filepath.Join(dir, "users.csv")
	ordersPath := filepath.Join(dir, "orders.data")
	if err := os.WriteFile(usersPath, []byte("user_id,name\n1,Alice\n2,Bob\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ordersPath, gzipIntegrationBytes(t, "user_id,total\n1,10\n2,20\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	left, err := loader.Load(usersPath, loader.Options{})
	if err != nil {
		t.Fatal(err)
	}
	q, err := parser.Parse(usersPath + ` | join ` + ordersPath + ` with format=csv, compression=gzip on user_id | sort user_id`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	result, err := Execute(q, left, func(filename string, opts ast.LoadOptions) (*table.Table, error) {
		return loader.Load(filename, loader.FromAST(opts))
	})
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if result.NumRows != 2 || result.Get(0, "total").Int != 10 || result.Get(1, "total").Int != 20 {
		t.Fatalf("unexpected table: %s", result.String())
	}
}

func TestIntegrationStdinWithLoadOptions(t *testing.T) {
	data := "name,age\nAlice,30\nBob,25\n"
	q, err := parser.Parse(`- with format=csv | filter { age > 25 } | select name`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if q.Source.Load.Format != "csv" {
		t.Fatalf("source format: got %q", q.Source.Load.Format)
	}
	tbl, err := loader.LoadInput("-", loader.FromAST(q.Source.Load), strings.NewReader(data))
	if err != nil {
		t.Fatalf("load stdin: %v", err)
	}
	result, err := Execute(q, tbl, nil)
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if result.NumRows != 1 || result.Get(0, "name").Str != "Alice" {
		t.Fatalf("got %s", result.String())
	}
}

func TestIntegrationDeflateStdinWithLoadOptions(t *testing.T) {
	data := deflateIntegrationBytes(t, "name,age\nAlice,30\nBob,25\n")
	q, err := parser.Parse(`- with format=csv, compression=deflate | filter { age > 25 } | select name`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if q.Source.Load.Format != "csv" || q.Source.Load.Compression != "deflate" {
		t.Fatalf("source load options: got format=%q compression=%q", q.Source.Load.Format, q.Source.Load.Compression)
	}
	tbl, err := loader.LoadInput("-", loader.FromAST(q.Source.Load), bytes.NewReader(data))
	if err != nil {
		t.Fatalf("load deflate stdin: %v", err)
	}
	result, err := Execute(q, tbl, nil)
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if result.NumRows != 1 || result.Get(0, "name").Str != "Alice" {
		t.Fatalf("got %s", result.String())
	}
}

func TestIntegrationStdinUnsupportedFormat(t *testing.T) {
	q, err := parser.Parse(`- with format=parquet | count`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	_, err = loader.LoadInput("-", loader.FromAST(q.Source.Load), strings.NewReader("x"))
	if err == nil {
		t.Fatal("expected error for parquet on stdin")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "unsupported") {
		t.Errorf("error: %q", err.Error())
	}
}

func TestIntegrationEmptyCSV(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.csv")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("count", func(t *testing.T) {
		result := loadAndQuery(t, path, "count")
		if result.NumRows != 1 || result.Get(0, "count").Int != 0 {
			t.Fatalf("expected count 0, got %s", result.String())
		}
	})

	t.Run("head", func(t *testing.T) {
		result := loadAndQuery(t, path, "head 5")
		if result.NumRows != 0 {
			t.Fatalf("expected 0 rows, got %d", result.NumRows)
		}
	})

	t.Run("select", func(t *testing.T) {
		result := loadAndQuery(t, path, "select name")
		if result.NumRows != 0 || len(result.Columns) != 1 || result.Columns[0] != "name" {
			t.Fatalf("expected empty projected table, got %s", result.String())
		}
	})

	t.Run("describe_zero_column_source", func(t *testing.T) {
		result := loadAndQuery(t, path, "describe")
		assertDescribeRows(t, result, map[string]describeMeta{})
	})

	t.Run("describe_after_select_on_empty_source", func(t *testing.T) {
		result := loadAndQuery(t, path, "select name | describe")
		assertDescribeRows(t, result, map[string]describeMeta{
			"name": {typ: "null", rows: 0},
		})
	})
}
