package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	goavro "github.com/linkedin/goavro/v2"
)

func readCLIJSONMaps(t *testing.T, out []byte) []map[string]any {
	t.Helper()
	var rows []map[string]any
	if err := json.Unmarshal(out, &rows); err != nil {
		t.Fatalf("json output: %v\n%s", err, out)
	}
	return rows
}

func requireCLIJSONColumns(t *testing.T, rows []map[string]any, want ...string) {
	t.Helper()
	wantSet := make(map[string]bool, len(want))
	for _, col := range want {
		wantSet[col] = true
	}
	for i, row := range rows {
		if len(row) != len(want) {
			t.Fatalf("row %d columns: got %#v, want %v", i, row, want)
		}
		for _, col := range want {
			if _, ok := row[col]; !ok {
				t.Fatalf("row %d missing column %q: %#v", i, col, row)
			}
		}
		for col := range row {
			if !wantSet[col] {
				t.Fatalf("row %d unexpected column %q: %#v", i, col, row)
			}
		}
	}
}

func TestCLIDistinctProjectionSemanticsTDDFlatFormats(t *testing.T) {
	bin := buildCLI(t)
	wantRows := map[string]int{
		"csv":     3,
		"json":    2,
		"jsonl":   2,
		"avro":    3,
		"parquet": 3,
	}

	for _, input := range cliFlatUserInputFiles() {
		t.Run(input.name, func(t *testing.T) {
			describe := readCLIDescribeRows(t, runCLIQuery(t, bin, input.path+" | distinct city | describe | json"))
			requireCLIDescribeSchema(t, describe, "city", "string", "string", wantRows[input.name])
			if len(describe) != 1 {
				t.Fatalf("describe columns: got %#v, want only city", describe)
			}

			rows := readCLIJSONMaps(t, runCLIQuery(t, bin, input.path+" | distinct city | sort city | json"))
			if len(rows) != wantRows[input.name] {
				t.Fatalf("rows: got %#v, want %d unique city rows", rows, wantRows[input.name])
			}
			requireCLIJSONColumns(t, rows, "city")
		})
	}
}

func TestCLIDistinctProjectionSemanticsTDDMultiKeyAndUnkeyed(t *testing.T) {
	bin := buildCLI(t)

	t.Run("multi_key_projects_only_requested_columns", func(t *testing.T) {
		rows := readCLIJSONMaps(t, runCLIQuery(t, bin, "../../testdata/users.csv | distinct city, age | sort city, age | json"))
		if len(rows) != 6 {
			t.Fatalf("rows: got %#v, want 6 unique city+age rows", rows)
		}
		requireCLIJSONColumns(t, rows, "city", "age")
	})

	t.Run("unkeyed_distinct_preserves_full_rows", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "dupes.csv")
		if err := os.WriteFile(path, []byte("name,age\nAlice,30\nAlice,30\nAlice,31\n"), 0o644); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
		rows := readCLIJSONMaps(t, runCLIQuery(t, bin, path+" | distinct | sort age | json"))
		if len(rows) != 2 {
			t.Fatalf("rows: got %#v, want 2 full-row distinct rows", rows)
		}
		requireCLIJSONColumns(t, rows, "name", "age")
	})
}

func TestCLIDistinctProjectionSemanticsTDDZeroRows(t *testing.T) {
	bin := buildCLI(t)

	for _, input := range cliFlatUserInputFiles() {
		t.Run(input.name+"_keyed", func(t *testing.T) {
			rows := readCLIDescribeRows(t, runCLIQuery(t, bin, input.path+" | filter { false } | distinct city | describe | json"))
			requireCLIDescribeSchema(t, rows, "city", "string", "string", 0)
			if len(rows) != 1 {
				t.Fatalf("describe columns: got %#v, want only city", rows)
			}
		})
	}

	t.Run("unkeyed_preserves_full_schema", func(t *testing.T) {
		rows := readCLIDescribeRows(t, runCLIQuery(t, bin, "../../testdata/users.csv | filter { false } | distinct | describe | json"))
		requireCLIDescribeSchema(t, rows, "name", "string", "string", 0)
		requireCLIDescribeSchema(t, rows, "age", "int", "int", 0)
		requireCLIDescribeSchema(t, rows, "city", "string", "string", 0)
		if len(rows) != 3 {
			t.Fatalf("describe columns: got %#v, want full input schema", rows)
		}
	})
}

func TestCLIDistinctProjectionSemanticsTDDNestedFormats(t *testing.T) {
	bin := buildCLI(t)

	for _, input := range cliNestedUserInputFiles() {
		t.Run(input.name+"_dot_path_projection", func(t *testing.T) {
			rows := readCLIDescribeRows(t, runCLIQuery(t, bin, input.path+" | distinct address.city | describe | json"))
			requireCLIDescribeSchema(t, rows, "address_city", "string", "string", 3)
			if len(rows) != 1 {
				t.Fatalf("describe columns: got %#v, want only address_city", rows)
			}
		})

		t.Run(input.name+"_combines_with_sort_select_count", func(t *testing.T) {
			rows := readCLIJSONMaps(t, runCLIQuery(t, bin, input.path+" | distinct address.city | sort address_city | select address_city | json"))
			if len(rows) != 3 {
				t.Fatalf("rows: got %#v, want 3 unique city rows", rows)
			}
			requireCLIJSONColumns(t, rows, "address_city")
		})
	}
}

func TestCLIDistinctProjectionSemanticsTDDNameCollisionsAndNullableParents(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()

	t.Run("dot_path_name_collision_matches_select_suffixing", func(t *testing.T) {
		path := filepath.Join(dir, "collision.jsonl")
		content := strings.Join([]string{
			`{"address_city":"existing","address":{"city":"New York"}}`,
			`{"address_city":"existing","address":{"city":"New York"}}`,
			`{"address_city":"other","address":{"city":"Boston"}}`,
		}, "\n") + "\n"
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write fixture: %v", err)
		}

		rows := readCLIDescribeRows(t, runCLIQuery(t, bin, path+" | distinct address_city, address.city | describe | json"))
		requireCLIDescribeSchema(t, rows, "address_city", "string", "string", 2)
		requireCLIDescribeSchema(t, rows, "address_city_2", "string", "string", 2)
		if len(rows) != 2 {
			t.Fatalf("describe columns: got %#v, want collision-suffixed projections", rows)
		}
	})

	t.Run("nullable_parent_child_projection_is_nullable", func(t *testing.T) {
		path := filepath.Join(dir, "optional.jsonl")
		content := strings.Join([]string{
			`{"name":"a","addr":null}`,
			`{"name":"b","addr":{"city":"NY"}}`,
			`{"name":"c","addr":{"city":"NY"}}`,
		}, "\n") + "\n"
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write fixture: %v", err)
		}

		describe := readCLIDescribeRows(t, runCLIQuery(t, bin, path+" | filter { false } | distinct addr.city | describe | json"))
		requireCLIDescribeSchema(t, describe, "addr_city", "string", "string?", 0)
		if len(describe) != 1 {
			t.Fatalf("describe columns: got %#v, want only addr_city", describe)
		}

		rows := readCLIJSONMaps(t, runCLIQuery(t, bin, path+" | distinct addr.city | json"))
		if len(rows) != 2 {
			t.Fatalf("rows: got %#v, want null and NY", rows)
		}
		requireCLIJSONColumns(t, rows, "addr_city")
	})
}

func TestCLIDistinctProjectionSemanticsTDDCombinations(t *testing.T) {
	bin := buildCLI(t)

	t.Run("group_reduce_after_keyed_distinct_sees_projected_schema", func(t *testing.T) {
		rows := readCLIJSONMaps(t, runCLIQuery(t, bin, "../../testdata/users.csv | distinct city | group city | reduce n = count() | remove grouped | sort city | json"))
		if len(rows) != 3 {
			t.Fatalf("rows: got %#v, want 3 groups", rows)
		}
		requireCLIJSONColumns(t, rows, "city", "n")
		for _, row := range rows {
			if row["n"] != float64(1) {
				t.Fatalf("row count after grouping distinct city: got %#v, want n=1", row)
			}
		}
	})

	t.Run("projected_away_column_is_unavailable_downstream", func(t *testing.T) {
		out := runCLIQueryExpectError(t, bin, "../../testdata/users.csv | distinct city | select name | json")
		assertCLIExpressionErrorContains(t, out, "name", "not found")
	})
}

func TestCLIDistinctProjectionSemanticsTDDErrors(t *testing.T) {
	bin := buildCLI(t)

	for _, input := range cliNestedUserInputFiles() {
		t.Run(input.name+"_missing_nested_field", func(t *testing.T) {
			out := runCLIQueryExpectError(t, bin, input.path+" | filter { false } | distinct address.missing | count | json")
			assertCLIExpressionErrorContains(t, out, "distinct", "missing", "not found")
		})

		t.Run(input.name+"_list_traversal", func(t *testing.T) {
			out := runCLIQueryExpectError(t, bin, input.path+" | filter { false } | distinct orders.amount | count | json")
			assertCLIExpressionErrorContains(t, out, "orders", "list")
		})
	}

	for _, input := range cliFlatUserInputFiles() {
		t.Run(input.name+"_missing_top_level", func(t *testing.T) {
			out := runCLIQueryExpectError(t, bin, input.path+" | filter { false } | distinct missing | count | json")
			assertCLIExpressionErrorContains(t, out, "distinct", "missing", "not found")
		})
	}

	t.Run("union_branch_traversal", func(t *testing.T) {
		schema := cliAvroUnionTDDRowSchema(`{"name":"u","type":[{"type":"record","name":"Inner","fields":[{"name":"x","type":"long"}]},"string"]}`)
		path := writeCLIAvroUnionTDDFile(t, schema, []map[string]any{
			{"u": goavro.Union("Inner", map[string]any{"x": int64(9)})},
			{"u": goavro.Union("string", "nine")},
		})
		out := runCLIQueryExpectError(t, bin, path+" | filter { false } | distinct u.x | count | json")
		assertCLIExpressionErrorContains(t, out, "u.x", "union")
	})
}

func TestCLIDistinctProjectionSemanticsTDDExactStructuralUnionKeys(t *testing.T) {
	bin := buildCLI(t)
	path := writeCLIAvroUnionTDDScalarFile(t, []map[string]any{
		{"u": goavro.Union("int", int32(7))},
		{"u": goavro.Union("string", "7")},
		{"u": goavro.Union("int", int32(7))},
	})

	rows := readCLIDescribeRows(t, runCLIQuery(t, bin, path+" | distinct u | describe | json"))
	requireCLIDescribeSchema(t, rows, "u", "union", "union<int,string>", 2)
	if len(rows) != 1 {
		t.Fatalf("describe columns: got %#v, want only union key column", rows)
	}
}
