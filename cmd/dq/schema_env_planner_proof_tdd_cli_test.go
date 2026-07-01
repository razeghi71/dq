package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLISchemaEnvPlannerProofTDDRowsAndSchemasAcrossSupportedFormats(t *testing.T) {
	bin := buildCLI(t)
	pipeline := ` | filter { age >= 25 } | select name, city, age | transform label = upper(name), age_bucket = age / 10 | select label, city, age_bucket | sort label`

	for _, input := range cliFlatUserInputFiles() {
		t.Run(input.name+"_rows", func(t *testing.T) {
			rows := readCLIJSONMaps(t, runCLIQuery(t, bin, input.path+pipeline+` | json`))
			if len(rows) == 0 {
				t.Fatalf("rows: got %#v, want at least one row after planner-proof pipeline", rows)
			}
			requireCLIJSONColumns(t, rows, "label", "city", "age_bucket")
			for _, row := range rows {
				label, ok := row["label"].(string)
				if !ok || label == "" || label != strings.ToUpper(label) {
					t.Fatalf("label should be an uppercase string, got row %#v", row)
				}
				if _, ok := row["age_bucket"].(float64); !ok {
					t.Fatalf("age_bucket should stay numeric, got row %#v", row)
				}
			}
		})

		t.Run(input.name+"_schema", func(t *testing.T) {
			rows := readCLIDescribeRows(t, runCLIQuery(t, bin, input.path+pipeline+` | describe | json`))
			requireCLISchemaEnvPlannerProofTDDSchemaOnly(t, rows, "label", "string", "string")
			requireCLISchemaEnvPlannerProofTDDSchemaOnly(t, rows, "city", "string", "string")
			requireCLISchemaEnvPlannerProofTDDSchemaOnly(t, rows, "age_bucket", "float", "float")
			if len(rows) != 3 {
				t.Fatalf("describe rows: got %#v, want only label/city/age_bucket", rows)
			}
		})
	}
}

func TestCLISchemaEnvPlannerProofTDDSchemaBoundaryErrorsAcrossSupportedFormats(t *testing.T) {
	bin := buildCLI(t)

	for _, input := range cliFlatUserInputFiles() {
		t.Run(input.name+"_select_hides_filter_column", func(t *testing.T) {
			out := runCLIQueryExpectError(t, bin, input.path+` | select name | filter { age > 20 } | json`)
			assertCLIExpressionErrorContains(t, out, "filter", "age", "not found")
		})

		t.Run(input.name+"_rename_hides_sort_column", func(t *testing.T) {
			out := runCLIQueryExpectError(t, bin, input.path+` | rename age=years | sort age | json`)
			assertCLIExpressionErrorContains(t, out, "sort", "age", "not found")
		})

		t.Run(input.name+"_duplicate_rename_result_rejected", func(t *testing.T) {
			out := runCLIQueryExpectError(t, bin, input.path+` | rename age=city | json`)
			assertCLIExpressionErrorContains(t, out, "rename", "duplicate", "city")
		})
	}
}

func TestCLISchemaEnvPlannerProofTDDCompressedTextReadSetsStayDemandDriven(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	inputs := []struct {
		name    string
		file    string
		content []byte
	}{
		{name: "csv_gzip", file: "proof.csv.gz", content: gzipCLIBytes(t, "id,status,unused\n1,active,10\n2,active,bad\n")},
		{name: "json_deflate", file: "proof.json.deflate", content: deflateCLIBytes(t, `[{"id":1,"status":"active","unused":10},{"id":2,"status":"active","unused":"bad"}]`)},
		{name: "jsonl_zstd", file: "proof.jsonl.zst", content: zstdCLIBytes(t, "{\"id\":1,\"status\":\"active\",\"unused\":10}\n{\"id\":2,\"status\":\"active\",\"unused\":\"bad\"}\n")},
	}

	for _, input := range inputs {
		t.Run(input.name+"_unreferenced_late_bad_column_skipped", func(t *testing.T) {
			path := writeCLISchemaEnvPlannerProofTDDBytes(t, dir, input.file, input.content)
			rows := readCLIJSONMaps(t, runCLIQuery(t, bin,
				path+` with infer_rows=1 | filter { status == "active" } | select id | count | json`,
			))
			if len(rows) != 1 || rows[0]["count"] != float64(2) {
				t.Fatalf("count rows: got %#v, want count=2", rows)
			}
		})

		t.Run(input.name+"_demanded_late_bad_column_still_errors", func(t *testing.T) {
			path := writeCLISchemaEnvPlannerProofTDDBytes(t, dir, "demanded-"+input.file, input.content)
			out := runCLIQueryExpectError(t, bin,
				path+` with infer_rows=1 | filter { status == "active" } | transform used = unused + 1 | select used | json`,
			)
			assertCLIExpressionErrorContains(t, out, "unused", "expected", "got")
		})
	}
}

func requireCLISchemaEnvPlannerProofTDDSchemaOnly(t *testing.T, rows map[string]cliDescribeRow, column, typ, schema string) {
	t.Helper()
	row, ok := rows[column]
	if !ok {
		t.Fatalf("missing describe row for %q; got %#v", column, rows)
	}
	if row.Type != typ || row.Schema != schema {
		t.Fatalf("%s describe row: got type=%q schema=%q, want type=%q schema=%q", column, row.Type, row.Schema, typ, schema)
	}
}

func writeCLISchemaEnvPlannerProofTDDBytes(t *testing.T, dir, name string, content []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
