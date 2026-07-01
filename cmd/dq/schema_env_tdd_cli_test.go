package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLISchemaEnvTDDLookupHeavyPipelineAcrossFlatFormats(t *testing.T) {
	bin := buildCLI(t)

	for _, input := range cliFlatUserInputFiles() {
		t.Run(input.name, func(t *testing.T) {
			rows := readCLIJSONMaps(t, runCLIQuery(t, bin,
				input.path+` | filter { age >= 25 and city is not null } | transform label = upper(name), age2 = age + 2 | select city, label, age2 | rename age2=score | sort city, label | json`,
			))
			if len(rows) == 0 {
				t.Fatalf("rows: got %#v, want at least one planned row", rows)
			}
			requireCLIJSONColumns(t, rows, "city", "label", "score")
			for _, row := range rows {
				label, ok := row["label"].(string)
				if !ok || label == "" || label != strings.ToUpper(label) {
					t.Fatalf("label should be uppercase string after transform/select/rename planning, got row %#v", row)
				}
				if _, ok := row["score"].(float64); !ok {
					t.Fatalf("score should be numeric after rename, got row %#v", row)
				}
			}
		})
	}
}

func TestCLISchemaEnvTDDSchemaBoundaryErrorsAcrossFlatFormats(t *testing.T) {
	bin := buildCLI(t)
	rowCounts := map[string]int{
		"csv":     6,
		"json":    3,
		"jsonl":   3,
		"avro":    6,
		"parquet": 6,
	}

	for _, input := range cliFlatUserInputFiles() {
		t.Run(input.name+"_remove_hides_column", func(t *testing.T) {
			out := runCLIQueryExpectError(t, bin,
				input.path+` | remove age | filter { age > 30 } | json`,
			)
			assertCLIExpressionErrorContains(t, out, "filter", "age", "not found")
		})

		t.Run(input.name+"_select_hides_column_from_sort", func(t *testing.T) {
			out := runCLIQueryExpectError(t, bin,
				input.path+` | select name | sort age | json`,
			)
			assertCLIExpressionErrorContains(t, out, "sort", "age", "not found")
		})

		t.Run(input.name+"_rename_duplicate_result_rejected", func(t *testing.T) {
			out := runCLIQueryExpectError(t, bin,
				input.path+` | rename age=city | json`,
			)
			assertCLIExpressionErrorContains(t, out, "rename", "duplicate", "city")
		})

		t.Run(input.name+"_duplicate_select_outputs_stay_ordered_and_addressable", func(t *testing.T) {
			out := runCLIQuery(t, bin, input.path+` | select city, city | describe | json`)
			rowsByColumn := readCLIDescribeRows(t, out)
			rowCount := rowCounts[input.name]
			requireCLIDescribeSchema(t, rowsByColumn, "city", "string", "string", rowCount)
			requireCLIDescribeSchema(t, rowsByColumn, "city_2", "string", "string", rowCount)
			var ordered []cliDescribeRow
			if err := json.Unmarshal(out, &ordered); err != nil {
				t.Fatalf("describe json output: %v\n%s", err, out)
			}
			if len(ordered) != 2 || ordered[0].Column != "city" || ordered[1].Column != "city_2" {
				t.Fatalf("duplicate select output order: got %#v, want city then city_2", ordered)
			}
		})
	}
}

func TestCLISchemaEnvTDDWideCSVEndToEndLookupOrder(t *testing.T) {
	bin := buildCLI(t)
	path := writeCLISchemaEnvTDDWideCSV(t, t.TempDir(), "wide.csv", 160)

	rows := readCLIJSONMaps(t, runCLIQuery(t, bin,
		path+` | filter { c150 > 250 and status == "ok" } | transform score = c150 + c151, bucket = c152 / 2 | select id, city, score, bucket | sort id | json`,
	))
	if len(rows) != 2 {
		t.Fatalf("wide rows: got %#v, want two ok rows with c150 > 250", rows)
	}
	requireCLIJSONColumns(t, rows, "id", "city", "score", "bucket")
	if rows[0]["id"] != float64(2) || rows[1]["id"] != float64(3) {
		t.Fatalf("wide row order: got %#v, want ids 2,3", rows)
	}
}

func TestCLISchemaEnvTDDWideCSVJoinUsesLateColumns(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	left := writeCLISchemaEnvTDDWideCSV(t, dir, "left.csv", 180)
	right := filepath.Join(dir, "right.csv")
	if err := os.WriteFile(right, []byte("r179,label,total\n379,A,10\n479,B,20\n999,Z,30\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rows := readCLIJSONMaps(t, runCLIQuery(t, bin,
		left+` | filter { status == "ok" } | join `+right+` on c179 == r179 | select id, label, total | sort id | json`,
	))
	if len(rows) != 2 {
		t.Fatalf("joined rows: got %#v, want two rows matched through late wide columns", rows)
	}
	requireCLIJSONColumns(t, rows, "id", "label", "total")
	if rows[0]["label"] != "A" || rows[1]["label"] != "B" {
		t.Fatalf("join labels: got %#v, want A then B", rows)
	}
}

func TestCLISchemaEnvTDDSourceReadSetUnaffectedByCompressedTextEdges(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	inputs := []struct {
		name    string
		file    string
		content []byte
	}{
		{name: "csv_gzip", file: "bad.csv.gz", content: gzipCLIBytes(t, "id,status,unused\n1,active,10\n2,active,bad\n")},
		{name: "json_deflate", file: "bad.json.deflate", content: deflateCLIBytes(t, `[{"id":1,"status":"active","unused":10},{"id":2,"status":"active","unused":"bad"}]`)},
		{name: "jsonl_zstd", file: "bad.jsonl.zst", content: zstdCLIBytes(t, "{\"id\":1,\"status\":\"active\",\"unused\":10}\n{\"id\":2,\"status\":\"active\",\"unused\":\"bad\"}\n")},
	}

	for _, input := range inputs {
		t.Run(input.name+"_unreferenced_bad_column_skipped", func(t *testing.T) {
			path := writeCLISchemaEnvTDDBytes(t, dir, input.file, input.content)
			rows := readCLIJSONMaps(t, runCLIQuery(t, bin,
				path+` with infer_rows=1 | filter { status == "active" } | select id | count | json`,
			))
			if len(rows) != 1 || rows[0]["count"] != float64(2) {
				t.Fatalf("count rows: got %#v, want count=2", rows)
			}
		})

		t.Run(input.name+"_demanded_bad_column_still_errors", func(t *testing.T) {
			path := writeCLISchemaEnvTDDBytes(t, dir, "demanded-"+input.file, input.content)
			out := runCLIQueryExpectError(t, bin,
				path+` with infer_rows=1 | filter { status == "active" } | transform used = unused + 1 | select used | json`,
			)
			assertCLIExpressionErrorContains(t, out, "unused", "bad")
		})
	}
}

func writeCLISchemaEnvTDDWideCSV(t *testing.T, dir, name string, width int) string {
	t.Helper()
	var sb strings.Builder
	sb.WriteString("id")
	for i := 1; i <= width; i++ {
		fmt.Fprintf(&sb, ",c%03d", i)
	}
	sb.WriteString(",city,status\n")
	for row := 1; row <= 3; row++ {
		fmt.Fprintf(&sb, "%d", row)
		for i := 1; i <= width; i++ {
			fmt.Fprintf(&sb, ",%d", row*100+i)
		}
		city := "LA"
		status := "hold"
		if row >= 2 {
			city = "NY"
			status = "ok"
		}
		fmt.Fprintf(&sb, ",%s,%s\n", city, status)
	}
	return writeCLISourceProjectionTDDFile(t, dir, name, sb.String())
}

func writeCLISchemaEnvTDDBytes(t *testing.T, dir, name string, content []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
