package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLILogicalOptimizerBoundaryTDDPipelineSemanticsAcrossFlatFormats(t *testing.T) {
	bin := buildCLI(t)
	wantNames := map[string][]string{
		"csv":     {"Alice", "Charlie", "Diana", "Frank"},
		"json":    {"Alice", "Charlie"},
		"jsonl":   {"Alice", "Charlie"},
		"avro":    {"Alice", "Charlie", "Diana", "Frank"},
		"parquet": {"Alice", "Charlie", "Diana", "Frank"},
	}
	wantAges := map[string]float64{
		"Alice":   130,
		"Charlie": 135,
		"Diana":   128,
		"Frank":   140,
	}
	wantBuckets := map[string]string{
		"Alice":   "standard",
		"Charlie": "senior",
		"Diana":   "standard",
		"Frank":   "senior",
	}

	for _, input := range cliFlatUserInputFiles() {
		t.Run(input.name, func(t *testing.T) {
			rows := readCLIJSONMaps(t, runCLIQuery(t, bin,
				input.path+` | select city, name, age | filter { age > 25 } | transform age = age + 100, bucket = if(age > 30, "senior", "standard") | filter { age > 125 } | select name, age, bucket | sort name | json`,
			))
			want := wantNames[input.name]
			if len(rows) != len(want) {
				t.Fatalf("rows: got %#v, want names %v", rows, want)
			}
			for i, name := range want {
				row := rows[i]
				if row["name"] != name {
					t.Fatalf("row %d name: got %#v, want %q; rows=%#v", i, row["name"], name, rows)
				}
				if row["age"] != wantAges[name] {
					t.Fatalf("row %d age: got %#v, want %#v; rows=%#v", i, row["age"], wantAges[name], rows)
				}
				if row["bucket"] != wantBuckets[name] {
					t.Fatalf("row %d bucket: got %#v, want %q; rows=%#v", i, row["bucket"], wantBuckets[name], rows)
				}
				requireCLIJSONColumns(t, []map[string]any{row}, "name", "age", "bucket")
			}
		})
	}
}

func TestCLILogicalOptimizerBoundaryTDDPreservesRuntimeErrorOrderAcrossFlatFormats(t *testing.T) {
	bin := buildCLI(t)

	for _, input := range cliFlatUserInputFiles() {
		t.Run(input.name+"_filter_before_erroring_transform", func(t *testing.T) {
			rows := readCLIJSONMaps(t, runCLIQuery(t, bin,
				input.path+` | filter { false } | transform y = year("bad-date") | count | json`,
			))
			if len(rows) != 1 || rows[0]["count"] != float64(0) {
				t.Fatalf("count rows: got %#v, want count=0", rows)
			}
		})

		t.Run(input.name+"_transform_before_filter_still_errors", func(t *testing.T) {
			out := runCLIQueryExpectError(t, bin,
				input.path+` | transform y = year("bad-date") | filter { false } | count | json`,
			)
			assertCLIExpressionErrorContains(t, out, "transform", "y", "year")
		})

		t.Run(input.name+"_unused_transform_still_errors", func(t *testing.T) {
			out := runCLIQueryExpectError(t, bin,
				input.path+` | transform unused = year("bad-date") | select name | json`,
			)
			assertCLIExpressionErrorContains(t, out, "transform", "unused", "year")
		})
	}
}

func TestCLILogicalOptimizerBoundaryTDDPreservesColumnVisibilityAcrossFlatFormats(t *testing.T) {
	bin := buildCLI(t)
	wantCounts := map[string]float64{
		"csv":     2,
		"json":    1,
		"jsonl":   1,
		"avro":    2,
		"parquet": 2,
	}

	for _, input := range cliFlatUserInputFiles() {
		t.Run(input.name+"_select_removes_column_for_later_filter", func(t *testing.T) {
			out := runCLIQueryExpectError(t, bin, input.path+` | select name | filter { age > 30 } | json`)
			assertCLIExpressionErrorContains(t, out, "filter", "age", "not found")
		})

		t.Run(input.name+"_rename_hides_original_name", func(t *testing.T) {
			out := runCLIQueryExpectError(t, bin, input.path+` | rename age=years | filter { age > 30 } | json`)
			assertCLIExpressionErrorContains(t, out, "filter", "age", "not found")
		})

		t.Run(input.name+"_rename_new_name_remains_available", func(t *testing.T) {
			rows := readCLIJSONMaps(t, runCLIQuery(t, bin,
				input.path+` | rename age=years | filter { years > 30 } | count | json`,
			))
			if len(rows) != 1 || rows[0]["count"] != wantCounts[input.name] {
				t.Fatalf("count rows: got %#v, want %.0f", rows, wantCounts[input.name])
			}
		})
	}
}

func TestCLILogicalOptimizerBoundaryTDDNestedPathsAcrossNestedFormats(t *testing.T) {
	bin := buildCLI(t)

	for _, input := range cliNestedUserInputFiles() {
		t.Run(input.name+"_post_projection_filter_rebinds_flattened_paths", func(t *testing.T) {
			rows := readCLIDescribeRows(t, runCLIQuery(t, bin,
				input.path+` | select address.city, name, profile.stats.score | filter { profile_stats_score > 6 } | select name, address_city, profile_stats_score | describe | json`,
			))
			requireCLIDescribeSchema(t, rows, "name", "string", "string", 2)
			requireCLIDescribeSchema(t, rows, "address_city", "string", "string", 2)
			requireCLIDescribeSchema(t, rows, "profile_stats_score", "float", "float", 2)
			if len(rows) != 3 {
				t.Fatalf("describe rows: got %#v, want name/address_city/profile_stats_score", rows)
			}
		})

		t.Run(input.name+"_list_path_guardrail_survives_zero_rows", func(t *testing.T) {
			out := runCLIQueryExpectError(t, bin, input.path+` | filter { false } | select orders.amount | json`)
			assertCLIExpressionErrorContains(t, out, "orders", "list")
		})

		t.Run(input.name+"_missing_nested_field_survives_zero_rows", func(t *testing.T) {
			out := runCLIQueryExpectError(t, bin, input.path+` | filter { false } | filter { profile.missing == "x" } | json`)
			assertCLIExpressionErrorContains(t, out, "missing", "not found")
		})
	}
}

func TestCLILogicalOptimizerBoundaryTDDJoinGroupReduceDistinctAcrossFlatFormats(t *testing.T) {
	bin := buildCLI(t)

	for _, input := range cliFlatUserInputFiles() {
		t.Run(input.name, func(t *testing.T) {
			rows := readCLIJSONMaps(t, runCLIQuery(t, bin,
				input.path+` | select name, city | join ../../testdata/orders.csv on name == user_name | group city | reduce total = sum(amount), n = count() | remove grouped | distinct city, total, n | sort city | json`,
			))
			want := []map[string]any{
				{"city": "LA", "total": float64(15), "n": float64(1)},
				{"city": "NY", "total": float64(55), "n": float64(3)},
			}
			if len(rows) != len(want) {
				t.Fatalf("rows: got %#v, want %#v", rows, want)
			}
			for i := range want {
				for key, value := range want[i] {
					if rows[i][key] != value {
						t.Fatalf("row %d %s: got %#v, want %#v; rows=%#v", i, key, rows[i][key], value, rows)
					}
				}
				requireCLIJSONColumns(t, []map[string]any{rows[i]}, "city", "total", "n")
			}
		})
	}
}

func TestCLILogicalOptimizerBoundaryTDDSourceProjectionReadSetControlsUnusedBadRecords(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()

	inputs := []struct {
		name string
		path string
		data []byte
	}{
		{"csv", filepath.Join(dir, "bad.csv"), []byte("id,unused\n1,10\n2,bad\n")},
		{"csv_gzip", filepath.Join(dir, "bad.csv.gz"), gzipCLIBytes(t, "id,unused\n1,10\n2,bad\n")},
		{"csv_zstd", filepath.Join(dir, "bad.csv.zst"), zstdCLIBytes(t, "id,unused\n1,10\n2,bad\n")},
		{"csv_deflate", filepath.Join(dir, "bad.csv.deflate"), deflateCLIBytes(t, "id,unused\n1,10\n2,bad\n")},
		{"jsonl", filepath.Join(dir, "bad.jsonl"), []byte("{\"id\":1,\"unused\":10}\n{\"id\":2,\"unused\":\"bad\"}\n")},
		{"jsonl_gzip", filepath.Join(dir, "bad.jsonl.gz"), gzipCLIBytes(t, "{\"id\":1,\"unused\":10}\n{\"id\":2,\"unused\":\"bad\"}\n")},
		{"jsonl_zstd", filepath.Join(dir, "bad.jsonl.zst"), zstdCLIBytes(t, "{\"id\":1,\"unused\":10}\n{\"id\":2,\"unused\":\"bad\"}\n")},
		{"jsonl_deflate", filepath.Join(dir, "bad.jsonl.deflate"), deflateCLIBytes(t, "{\"id\":1,\"unused\":10}\n{\"id\":2,\"unused\":\"bad\"}\n")},
	}

	for _, input := range inputs {
		t.Run(input.name, func(t *testing.T) {
			if err := os.WriteFile(input.path, input.data, 0o644); err != nil {
				t.Fatalf("write %s: %v", input.path, err)
			}
			query := input.path + ` with infer_rows=1 | select id | count | json`
			rows := readCLIJSONMaps(t, runCLIQuery(t, bin, query))
			if len(rows) != 1 || rows[0]["count"] != float64(2) {
				t.Fatalf("projected source count rows: got %#v, want count=2", rows)
			}

			out := runCLIQueryExpectError(t, bin, input.path+` with infer_rows=1 | filter { id + 1 > 0 } | select id | count | json`)
			msg := strings.ToLower(string(out))
			for _, want := range []string{"unused", "int"} {
				if !strings.Contains(msg, want) {
					t.Fatalf("unsupported leading filter should read all columns and mention %q, got:\n%s", want, out)
				}
			}
		})
	}
}
