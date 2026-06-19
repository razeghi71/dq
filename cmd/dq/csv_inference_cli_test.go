package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeCLICSVInferenceFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func cliCSVInferenceRows(header string, rows int, badRows map[int]string) string {
	var b strings.Builder
	b.WriteString(header)
	for i := 1; i <= rows; i++ {
		if bad, ok := badRows[i]; ok {
			b.WriteString(bad)
			if !strings.HasSuffix(bad, "\n") {
				b.WriteByte('\n')
			}
			continue
		}
		fmt.Fprintf(&b, "%d,%d\n", i, i*10)
	}
	return b.String()
}

func TestCLICSVInferenceBadRecordsEndToEnd(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	path := writeCLICSVInferenceFile(t, dir, "late-bad.csv", cliCSVInferenceRows("id,amount\n", 52, map[int]string{51: "51,abc"}))

	out := runCLIQueryExpectError(t, bin, path+" with infer_rows=50 | count")
	for _, part := range []string{"load error", "row 52", "amount", "int", "abc"} {
		if !strings.Contains(strings.ToLower(string(out)), strings.ToLower(part)) {
			t.Fatalf("expected error containing %q, got:\n%s", part, out)
		}
	}

	out = runCLIQuery(t, bin, path+" with infer_rows=50, max_bad_records=1 | count | json")
	var rows []map[string]int64
	if err := json.Unmarshal(out, &rows); err != nil {
		t.Fatalf("json output: %v\n%s", err, out)
	}
	if len(rows) != 1 || rows[0]["count"] != 51 {
		t.Fatalf("count after skipped bad row: got %#v, want count=51", rows)
	}
}

func TestCLICSVInferenceModesEndToEnd(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()

	t.Run("default_samples_more_than_50_rows", func(t *testing.T) {
		path := writeCLICSVInferenceFile(t, dir, "default-sample.csv", cliCSVInferenceRows("id,amount\n", 51, map[int]string{51: "51,abc"}))
		out := runCLIQuery(t, bin, path+" | describe | filter { column == \"amount\" } | json")
		var rows []map[string]any
		if err := json.Unmarshal(out, &rows); err != nil {
			t.Fatalf("json output: %v\n%s", err, out)
		}
		if len(rows) != 1 || rows[0]["type"] != "string" || rows[0]["row_count"].(float64) != 51 {
			t.Fatalf("amount describe row: got %#v, want string type over 51 rows", rows)
		}
	})

	t.Run("infer_all_rows_falls_back_to_string", func(t *testing.T) {
		path := writeCLICSVInferenceFile(t, dir, "infer-all.csv", cliCSVInferenceRows("id,amount\n", 51, map[int]string{51: "51,abc"}))
		out := runCLIQuery(t, bin, path+" with infer_rows=-1 | describe | filter { column == \"amount\" } | json")
		var rows []map[string]any
		if err := json.Unmarshal(out, &rows); err != nil {
			t.Fatalf("json output: %v\n%s", err, out)
		}
		if len(rows) != 1 || rows[0]["type"] != "string" {
			t.Fatalf("amount describe row: got %#v, want string type", rows)
		}
	})

	t.Run("infer_zero_reads_everything_as_text", func(t *testing.T) {
		path := writeCLICSVInferenceFile(t, dir, "all-string.csv", "id,flag,amount\n1,true,2.5\n")
		out := runCLIQuery(t, bin, path+" with infer_rows=0 | json")
		var rows []map[string]any
		if err := json.Unmarshal(out, &rows); err != nil {
			t.Fatalf("json output: %v\n%s", err, out)
		}
		if len(rows) != 1 {
			t.Fatalf("rows: got %#v", rows)
		}
		for _, col := range []string{"id", "flag", "amount"} {
			if _, ok := rows[0][col].(string); !ok {
				t.Fatalf("column %s: got %#v (%T), want string", col, rows[0][col], rows[0][col])
			}
		}
	})
}

func TestCLICSVInferenceCompressedInputsEndToEnd(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	content := cliCSVInferenceRows("id,amount\n", 51, map[int]string{51: "51,abc"})

	cases := []struct {
		name string
		file string
		data []byte
	}{
		{"gzip", "rows.csv.gz", gzipCLIBytes(t, content)},
		{"zstd", "rows.csv.zst", zstdCLIBytes(t, content)},
		{"deflate", "rows.csv.deflate", deflateCLIBytes(t, content)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, tc.file)
			if err := os.WriteFile(path, tc.data, 0o644); err != nil {
				t.Fatal(err)
			}
			out := runCLIQuery(t, bin, path+" with infer_rows=50, max_bad_records=1 | count | json")
			var rows []map[string]int64
			if err := json.Unmarshal(out, &rows); err != nil {
				t.Fatalf("json output: %v\n%s", err, out)
			}
			if len(rows) != 1 || rows[0]["count"] != 50 {
				t.Fatalf("count after skipped compressed bad row: got %#v, want count=50", rows)
			}
		})
	}
}

func TestCLICSVGlobInferenceUsesSingleSampleAndBadRecordBudgetEndToEnd(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	writeCLICSVInferenceFile(t, dir, "a.csv", "id,amount\n1,10\n2,20\n")
	writeCLICSVInferenceFile(t, dir, "b.csv", "id,amount\n3,abc\n4,40\n")

	glob := filepath.Join(dir, "*.csv") + " with format=csv, infer_rows=2"
	out := runCLIQueryExpectError(t, bin, glob+" | count")
	for _, part := range []string{"row 2", "amount", "int", "abc"} {
		if !strings.Contains(strings.ToLower(string(out)), strings.ToLower(part)) {
			t.Fatalf("expected error containing %q, got:\n%s", part, out)
		}
	}

	out = runCLIQuery(t, bin, glob+", max_bad_records=1 | select id, amount | sort id | json")
	var rows []map[string]int64
	if err := json.Unmarshal(out, &rows); err != nil {
		t.Fatalf("json output: %v\n%s", err, out)
	}
	want := []map[string]int64{
		{"id": 1, "amount": 10},
		{"id": 2, "amount": 20},
		{"id": 4, "amount": 40},
	}
	if fmt.Sprint(rows) != fmt.Sprint(want) {
		t.Fatalf("glob rows after skipped cross-shard bad row: got %#v, want %#v", rows, want)
	}
}

func TestCLIInferenceOptionsRejectedForSchemaFormats(t *testing.T) {
	bin := buildCLI(t)
	cases := []struct {
		name  string
		query string
		want  string
	}{
		{"avro_infer_rows", "../../testdata/users.avro with infer_rows=1 | count", "infer_rows applies only"},
		{"avro_max_bad_records", "../../testdata/users.avro with max_bad_records=1 | count", "max_bad_records applies only"},
		{"parquet_infer_rows", "../../testdata/users.parquet with infer_rows=1 | count", "infer_rows applies only"},
		{"parquet_max_bad_records", "../../testdata/users.parquet with max_bad_records=1 | count", "max_bad_records applies only"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := runCLIQueryExpectError(t, bin, tc.query)
			if !strings.Contains(strings.ToLower(string(out)), strings.ToLower(tc.want)) {
				t.Fatalf("expected error containing %q, got:\n%s", tc.want, out)
			}
		})
	}
}
