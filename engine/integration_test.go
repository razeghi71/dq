package engine

import (
	"os"
	"strings"
	"testing"

	"github.com/razeghi71/dq/loader"
	"github.com/razeghi71/dq/parser"
	"github.com/razeghi71/dq/table"
)

const testdataDir = "../testdata"

// loadAndQuery loads a file from disk, parses the query, and executes it.
func loadAndQuery(t *testing.T, file, query string) *table.Table {
	t.Helper()
	tbl, err := loader.Load(file, "")
	if err != nil {
		t.Fatalf("load %s: %v", file, err)
	}
	q, err := parser.Parse(file + " | " + query)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	result, err := Execute(q, tbl)
	if err != nil {
		t.Fatalf("exec error: %v", err)
	}
	return result
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
		result := loadAndQuery(t, file, "sorta age | select name age | head 3")
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
		result := loadAndQuery(t, file, "group city | reduce n = count(), total = sum(age) | remove grouped | sortd n")
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
		result := loadAndQuery(t, file, "sorta age | head 1")
		nameIdx := result.ColIndex("name")
		if result.GetAt(0, nameIdx).Str != "Bob" {
			t.Errorf("youngest should be Bob, got %q", result.GetAt(0, nameIdx).Str)
		}
	})
}

func TestIntegrationFlatJSON(t *testing.T) {
	assertFlatQueriesSmall(t, testdataDir+"/users.json")
}

func TestIntegrationFlatJSONL(t *testing.T) {
	assertFlatQueriesSmall(t, testdataDir+"/users.jsonl")
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
		result := loadAndQuery(t, file, "transform city = address.city | select name city")
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
		result := loadAndQuery(t, file, "transform score = profile.stats.score | select name score")
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
		result := loadAndQuery(t, file, "transform x = address.nonexistent | select name x")
		xIdx := result.ColIndex("x")
		for i := 0; i < result.NumRows; i++ {
			if !result.GetAt(i, xIdx).IsNull() {
				t.Errorf("row %d: expected null for missing field, got %v", i, result.GetAt(i, xIdx))
			}
		}
	})

	t.Run("sort_by_nested", func(t *testing.T) {
		result := loadAndQuery(t, file, "transform city = address.city | sorta city | select name city")
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

// assertNestedDotPathOps tests select and group with dot-path columns on nested files.
func assertNestedDotPathOps(t *testing.T, file string) {
	t.Helper()

	t.Run("select_dot_path", func(t *testing.T) {
		result := loadAndQuery(t, file, "select name address.city")
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
		result := loadAndQuery(t, file, "select name profile.stats.score")
		if result.Columns[1] != "profile_stats_score" {
			t.Errorf("expected column name 'profile_stats_score', got %q", result.Columns[1])
		}
	})

	t.Run("select_dot_path_dedup", func(t *testing.T) {
		result := loadAndQuery(t, file, `transform address_city = "test" | select address_city address.city`)
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
	result := loadAndQuery(t, testdataDir+"/mixed_types.csv", "select id val")

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
		// filter treats the widened string column as strings; only "something" != "1"/"2.5"
		result := loadAndQuery(t, testdataDir+"/mixed_types.csv", `filter { val == "something" }`)
		if result.NumRows != 1 {
			t.Fatalf("expected 1 row, got %d", result.NumRows)
		}
		if result.GetAt(0, result.ColIndex("val")).Str != "something" {
			t.Errorf("unexpected val: %q", result.GetAt(0, result.ColIndex("val")).Str)
		}
	})
}

// ============================================================
// Stdin source (-)
// ============================================================

func TestIntegrationStdinDashOnly(t *testing.T) {
	data, err := os.ReadFile(testdataDir + "/sales.csv")
	if err != nil {
		t.Fatal(err)
	}

	tbl, err := loader.LoadInput("-", "csv", strings.NewReader(string(data)))
	if err != nil {
		t.Fatalf("load stdin: %v", err)
	}

	q, err := parser.Parse("-")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	result, err := Execute(q, tbl)
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

	tbl, err := loader.LoadInput("-", "csv", strings.NewReader(string(data)))
	if err != nil {
		t.Fatalf("load stdin: %v", err)
	}

	q, err := parser.Parse(`- | filter { age > 25 }`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	result, err := Execute(q, tbl)
	if err != nil {
		t.Fatalf("exec: %v", err)
	}

	if result.NumRows != 4 {
		t.Fatalf("expected 4 rows with age > 25, got %d", result.NumRows)
	}
}
