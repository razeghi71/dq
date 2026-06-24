package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	goavro "github.com/linkedin/goavro/v2"
)

func TestCLISimplePlannerTDDZeroRowSimpleOpCombinationsAcrossFlatFormats(t *testing.T) {
	bin := buildCLI(t)

	for _, input := range cliFlatUserInputFiles() {
		t.Run(input.name, func(t *testing.T) {
			rows := readCLIDescribeRows(t, runCLIQuery(t, bin,
				input.path+` | filter { false } | head 5 | tail 2 | sort age | select name, age | rename name=person | remove age | distinct person | describe | json`,
			))
			requireCLIDescribeSchema(t, rows, "person", "string", "string", 0)
			if len(rows) != 1 {
				t.Fatalf("describe columns: got %#v, want only person", rows)
			}
		})
	}
}

func TestCLISimplePlannerTDDStableResultSchemasAcrossFlatFormats(t *testing.T) {
	bin := buildCLI(t)

	for _, input := range cliFlatUserInputFiles() {
		t.Run(input.name+"_count_describe", func(t *testing.T) {
			rows := readCLIDescribeRows(t, runCLIQuery(t, bin,
				input.path+` | head 4 | tail 3 | filter { age > 20 } | sort city, -age | select city, age | distinct city, age | count | describe | json`,
			))
			requireCLIDescribeSchema(t, rows, "count", "int", "int", 1)
			if len(rows) != 1 {
				t.Fatalf("describe columns: got %#v, want only count", rows)
			}
		})

		t.Run(input.name+"_describe_describe", func(t *testing.T) {
			rows := readCLIDescribeRows(t, runCLIQuery(t, bin, input.path+` | describe | describe | json`))
			requireCLIDescribeSchema(t, rows, "column", "string", "string", 3)
			requireCLIDescribeSchema(t, rows, "type", "string", "string", 3)
			requireCLIDescribeSchema(t, rows, "row_count", "int", "int", 3)
			requireCLIDescribeSchema(t, rows, "schema", "string", "string", 3)
			if len(rows) != 4 {
				t.Fatalf("describe columns: got %#v, want describe metadata schema", rows)
			}
		})
	}
}

func TestCLISimplePlannerTDDNestedProjectionSimpleOpCombinationsAcrossFormats(t *testing.T) {
	bin := buildCLI(t)

	for _, input := range cliNestedUserInputFiles() {
		t.Run(input.name+"_zero_row_nested_projection", func(t *testing.T) {
			rows := readCLIDescribeRows(t, runCLIQuery(t, bin,
				input.path+` | filter { false } | select address.city, orders | rename address_city=city | sort city | describe | json`,
			))
			requireCLIDescribeSchema(t, rows, "city", "string", "string", 0)
			requireCLIDescribeSchema(t, rows, "orders", "list", "list<record<amount:float, order_id:int, status:string>>", 0)
			if len(rows) != 2 {
				t.Fatalf("describe columns: got %#v, want city and orders", rows)
			}
		})

		t.Run(input.name+"_keyed_distinct_nested_projection", func(t *testing.T) {
			rows := readCLIDescribeRows(t, runCLIQuery(t, bin,
				input.path+` | filter { false } | distinct address.city | describe | json`,
			))
			requireCLIDescribeSchema(t, rows, "address_city", "string", "string", 0)
			if len(rows) != 1 {
				t.Fatalf("describe columns: got %#v, want only address_city", rows)
			}
		})
	}
}

func TestCLISimplePlannerTDDRejectsInvalidSimpleOpsAfterZeroRows(t *testing.T) {
	bin := buildCLI(t)

	for _, input := range cliFlatUserInputFiles() {
		t.Run(input.name+"_filter_non_bool", func(t *testing.T) {
			out := runCLIQueryExpectError(t, bin, input.path+` | filter { false } | filter { age } | count`)
			assertCLIExpressionErrorContains(t, out, "filter", "bool")
		})

		t.Run(input.name+"_sort_missing_after_select", func(t *testing.T) {
			out := runCLIQueryExpectError(t, bin, input.path+` | filter { false } | select city | sort age | count`)
			assertCLIExpressionErrorContains(t, out, "sort", "age", "not found")
		})

		t.Run(input.name+"_rename_duplicate_result", func(t *testing.T) {
			out := runCLIQueryExpectError(t, bin, input.path+` | filter { false } | rename name=city | count`)
			assertCLIExpressionErrorContains(t, out, "rename", "duplicate", "city")
		})

		t.Run(input.name+"_remove_missing", func(t *testing.T) {
			out := runCLIQueryExpectError(t, bin, input.path+` | filter { false } | remove missing | count`)
			assertCLIExpressionErrorContains(t, out, "remove", "missing", "not found")
		})

		t.Run(input.name+"_distinct_projected_away_column", func(t *testing.T) {
			out := runCLIQueryExpectError(t, bin, input.path+` | filter { false } | distinct city | select age | count`)
			assertCLIExpressionErrorContains(t, out, "age", "not found")
		})
	}

	for _, input := range cliNestedUserInputFiles() {
		t.Run(input.name+"_select_missing_nested", func(t *testing.T) {
			out := runCLIQueryExpectError(t, bin, input.path+` | filter { false } | select address.missing | count`)
			assertCLIExpressionErrorContains(t, out, "select", "missing", "not found")
		})

		t.Run(input.name+"_select_list_traversal", func(t *testing.T) {
			out := runCLIQueryExpectError(t, bin, input.path+` | filter { false } | select orders.amount | count`)
			assertCLIExpressionErrorContains(t, out, "orders", "list")
		})

		t.Run(input.name+"_sort_list", func(t *testing.T) {
			out := runCLIQueryExpectError(t, bin, input.path+` | filter { false } | sort orders | count`)
			assertCLIExpressionErrorContains(t, out, "sort", "list", "not orderable")
		})

		t.Run(input.name+"_remove_dot_path", func(t *testing.T) {
			out := runCLIQueryExpectError(t, bin, input.path+` | remove address.city | count`)
			assertCLIExpressionErrorContains(t, out, "remove", "dot paths", "address.city")
		})
	}

	t.Run("select_union_branch_traversal", func(t *testing.T) {
		schema := cliAvroUnionTDDRowSchema(`{"name":"u","type":[{"type":"record","name":"Inner","fields":[{"name":"x","type":"long"}]},"string"]}`)
		path := writeCLIAvroUnionTDDFile(t, schema, []map[string]any{
			{"u": goavro.Union("Inner", map[string]any{"x": int64(9)})},
			{"u": goavro.Union("string", "nine")},
		})
		out := runCLIQueryExpectError(t, bin, path+` | filter { false } | select u.x | count`)
		assertCLIExpressionErrorContains(t, out, "u.x", "union")
	})

	t.Run("sort_union", func(t *testing.T) {
		path := writeCLIAvroUnionTDDScalarFile(t, []map[string]any{
			{"u": goavro.Union("int", int32(7))},
			{"u": goavro.Union("string", "7")},
		})
		out := runCLIQueryExpectError(t, bin, path+` | filter { false } | sort u | count`)
		assertCLIExpressionErrorContains(t, out, "sort", "union", "not orderable")
	})
}

func TestCLISimplePlannerTDDWholePipelinePlanningCoversFormerHandoffCases(t *testing.T) {
	bin := buildCLI(t)

	for _, input := range cliFlatUserInputFiles() {
		t.Run(input.name+"_transform_group_reduce_in_schema_pipeline", func(t *testing.T) {
			out := runCLIQuery(t, bin,
				input.path+` | select name, age, city | transform bucket = if(age > 30, "senior", "standard") | group bucket | reduce n = count() | remove grouped | sort bucket | json`,
			)
			rows := readCLIJSONMaps(t, out)
			want := cliSimplePlannerHandoffBucketRows(input.name)
			if len(rows) != len(want) {
				t.Fatalf("grouped rows: got %#v, want %#v", rows, want)
			}
			for i := range want {
				if rows[i]["bucket"] != want[i]["bucket"] || rows[i]["n"] != want[i]["n"] {
					t.Fatalf("grouped row %d: got %#v, want %#v", i, rows[i], want[i])
				}
			}
		})
	}

	t.Run("join_in_schema_pipeline", func(t *testing.T) {
		out := runCLIQuery(t, bin,
			`../../testdata/users.csv | select name, city | join ../../testdata/orders.csv on name == user_name | select name, order_id | sort order_id | count | json`,
		)
		var rows []map[string]any
		if err := json.Unmarshal(out, &rows); err != nil {
			t.Fatalf("json output: %v\n%s", err, out)
		}
		if len(rows) != 1 || rows[0]["count"] != float64(4) {
			t.Fatalf("join simple-op handoff count: got %#v, want [{count:4}]", rows)
		}
	})
}

func cliSimplePlannerHandoffBucketRows(inputName string) []map[string]any {
	if inputName == "json" || inputName == "jsonl" {
		return []map[string]any{
			{"bucket": "senior", "n": float64(1)},
			{"bucket": "standard", "n": float64(2)},
		}
	}
	return []map[string]any{
		{"bucket": "senior", "n": float64(2)},
		{"bucket": "standard", "n": float64(4)},
	}
}

func TestCLISimplePlannerTDDDotPathNameCollisionsAndNullableParents(t *testing.T) {
	bin := buildCLI(t)

	t.Run("projection_name_collision", func(t *testing.T) {
		rows := readCLIDescribeRows(t, runCLIQuery(t, bin,
			`../../testdata/nested.jsonl | filter { false } | select address.city, address.city | describe | json`,
		))
		requireCLIDescribeSchema(t, rows, "address_city", "string", "string", 0)
		requireCLIDescribeSchema(t, rows, "address_city_2", "string", "string", 0)
		if len(rows) != 2 {
			t.Fatalf("describe columns: got %#v, want collision-suffixed projections", rows)
		}
	})

	t.Run("nullable_parent_projection", func(t *testing.T) {
		out := runCLIQuery(t, bin, `../../testdata/nested_missing.json | filter { false } | select addr.city | describe | json`)
		rows := readCLIDescribeRows(t, out)
		requireCLIDescribeSchema(t, rows, "addr_city", "string", "string?", 0)
		if len(rows) != 1 {
			t.Fatalf("describe columns: got %#v, want only addr_city", rows)
		}
	})
}

func TestCLISimplePlannerTDDCompressedTextInputsKeepSimpleOpSemantics(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	content := "name,age,city\nAlice,30,NY\nBob,25,LA\n"
	inputs := []struct {
		name string
		path string
		data []byte
	}{
		{"gzip", filepath.Join(dir, "users.csv.gz"), gzipCLIBytes(t, content)},
		{"zstd", filepath.Join(dir, "users.csv.zst"), zstdCLIBytes(t, content)},
		{"deflate", filepath.Join(dir, "users.csv.deflate"), deflateCLIBytes(t, content)},
	}

	for _, input := range inputs {
		t.Run(input.name, func(t *testing.T) {
			if err := os.WriteFile(input.path, input.data, 0o644); err != nil {
				t.Fatalf("write compressed fixture: %v", err)
			}
			rows := readCLIDescribeRows(t, runCLIQuery(t, bin,
				input.path+` | filter { false } | select city | distinct city | describe | json`,
			))
			requireCLIDescribeSchema(t, rows, "city", "string", "string", 0)
			if len(rows) != 1 {
				t.Fatalf("describe columns: got %#v, want only city", rows)
			}
		})
	}
}

func TestCLISimplePlannerTDDErrorMessagesKeepOperationContext(t *testing.T) {
	bin := buildCLI(t)

	cases := []struct {
		name  string
		query string
		wants []string
	}{
		{
			name:  "select_context",
			query: `../../testdata/users.csv | filter { false } | select missing | count`,
			wants: []string{"select", "missing", "not found"},
		},
		{
			name:  "sort_context",
			query: `../../testdata/users.csv | filter { false } | sort missing | count`,
			wants: []string{"sort", "missing", "not found"},
		},
		{
			name:  "distinct_context",
			query: `../../testdata/users.csv | filter { false } | distinct missing | count`,
			wants: []string{"distinct", "missing", "not found"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := runCLIQueryExpectError(t, bin, tc.query)
			msg := strings.ToLower(string(out))
			for _, want := range tc.wants {
				if !strings.Contains(msg, strings.ToLower(want)) {
					t.Fatalf("error for %s: got\n%s\nwant substring %q", tc.query, out, want)
				}
			}
		})
	}
}
