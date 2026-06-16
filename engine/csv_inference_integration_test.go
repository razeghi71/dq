package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/razeghi71/dq/ast"
	"github.com/razeghi71/dq/loader"
	"github.com/razeghi71/dq/parser"
	"github.com/razeghi71/dq/table"
)

func writeCSVInferenceFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func csvInferenceRows(header string, rows int, badRows map[int]string) string {
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

func loadCSVInferenceSource(t *testing.T, source string) (*table.Table, error) {
	t.Helper()
	q, err := parser.Parse(source + " | count")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	return loader.Load(q.Source.Filename, loader.FromAST(q.Source.Load))
}

func expectCSVInferenceLoadError(t *testing.T, source string, parts ...string) {
	t.Helper()
	_, err := loadCSVInferenceSource(t, source)
	if err == nil {
		t.Fatalf("expected load error for %s", source)
	}
	for _, part := range parts {
		if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(part)) {
			t.Fatalf("error %q does not contain %q", err.Error(), part)
		}
	}
}

func assertCSVInferenceDescribe(t *testing.T, source string, want map[string]describeMeta) {
	t.Helper()
	result := loadAndQuery(t, source, "describe")
	assertDescribeRows(t, result, want)
}

func TestIntegrationCSVInferenceDuckDBStyleTypes(t *testing.T) {
	dir := t.TempDir()

	t.Run("ints", func(t *testing.T) {
		path := writeCSVInferenceFile(t, dir, "ints.csv", "x\n1\n2\n3\n")
		assertCSVInferenceDescribe(t, path, map[string]describeMeta{"x": {typ: "int", rows: 3}})
	})

	t.Run("ints_and_floats_infer_float", func(t *testing.T) {
		path := writeCSVInferenceFile(t, dir, "floats.csv", "x\n1\n2.5\n3\n")
		assertCSVInferenceDescribe(t, path, map[string]describeMeta{"x": {typ: "float", rows: 3}})
	})

	t.Run("booleans", func(t *testing.T) {
		path := writeCSVInferenceFile(t, dir, "bools.csv", "flag\ntrue\nfalse\ntrue\n")
		result := loadAndQuery(t, path, "filter { flag } | count")
		if got := result.GetAt(0, 0).Int; got != 2 {
			t.Fatalf("true count: got %d, want 2", got)
		}
		assertCSVInferenceDescribe(t, path, map[string]describeMeta{"flag": {typ: "bool", rows: 3}})
	})

	t.Run("numeric_and_string_sample_falls_back_to_string", func(t *testing.T) {
		path := writeCSVInferenceFile(t, dir, "numeric-string.csv", "invoice_id\n1001\n1002\nX-1003\n")
		assertCSVInferenceDescribe(t, path, map[string]describeMeta{"invoice_id": {typ: "string", rows: 3}})
	})

	t.Run("bool_and_numeric_sample_falls_back_to_string", func(t *testing.T) {
		path := writeCSVInferenceFile(t, dir, "bool-numeric.csv", "flag\ntrue\n1\nfalse\n")
		assertCSVInferenceDescribe(t, path, map[string]describeMeta{"flag": {typ: "string", rows: 3}})
	})

	t.Run("all_null_sample_infers_string_schema", func(t *testing.T) {
		path := writeCSVInferenceFile(t, dir, "all-null.csv", "x\n\"\"\nnull\nNULL\n")
		assertCSVInferenceDescribe(t, path, map[string]describeMeta{"x": {typ: "string", rows: 3}})
	})
}

func TestIntegrationCSVInferRowsAndBadRecords(t *testing.T) {
	dir := t.TempDir()

	t.Run("default_sample_includes_string_so_column_is_string", func(t *testing.T) {
		path := writeCSVInferenceFile(t, dir, "early-string.csv", "id,amount\n1,10\n2,9\n3,100\n4,abc\n")
		assertCSVInferenceDescribe(t, path, map[string]describeMeta{
			"id":     {typ: "int", rows: 4},
			"amount": {typ: "string", rows: 4},
		})
	})

	t.Run("default_errors_on_late_bad_record_after_50_data_rows", func(t *testing.T) {
		path := writeCSVInferenceFile(t, dir, "late-bad.csv", csvInferenceRows("id,amount\n", 51, map[int]string{51: "51,abc"}))
		expectCSVInferenceLoadError(t, path, "row 52", "amount", "int", "abc")
	})

	t.Run("infer_all_rows_sees_late_string_and_infers_string", func(t *testing.T) {
		path := writeCSVInferenceFile(t, dir, "infer-all.csv", csvInferenceRows("id,amount\n", 51, map[int]string{51: "51,abc"}))
		assertCSVInferenceDescribe(t, path+" with infer_rows=-1", map[string]describeMeta{
			"id":     {typ: "int", rows: 51},
			"amount": {typ: "string", rows: 51},
		})
	})

	t.Run("infer_zero_loads_all_non_null_values_as_strings", func(t *testing.T) {
		path := writeCSVInferenceFile(t, dir, "all-strings.csv", "id,flag,amount\n1,true,2.5\n2,false,10\n")
		assertCSVInferenceDescribe(t, path+" with infer_rows=0", map[string]describeMeta{
			"id":     {typ: "string", rows: 2},
			"flag":   {typ: "string", rows: 2},
			"amount": {typ: "string", rows: 2},
		})
	})

	t.Run("max_bad_records_skips_whole_bad_rows", func(t *testing.T) {
		path := writeCSVInferenceFile(t, dir, "skip-one.csv", csvInferenceRows("id,amount\n", 52, map[int]string{51: "51,abc"}))
		result := loadAndQuery(t, path+" with max_bad_records=1", "count")
		if got := result.GetAt(0, 0).Int; got != 51 {
			t.Fatalf("row count after skipping one bad row: got %d, want 51", got)
		}
		result = loadAndQuery(t, path+" with max_bad_records=1", "transform one = 1 | group one | reduce total = sum(amount) | remove grouped | select total")
		if got := result.GetAt(0, result.ColIndex("total")).Int; got != 13270 {
			t.Fatalf("sum after skipping whole bad row: got %d, want 13270", got)
		}
	})

	t.Run("max_bad_records_errors_on_next_bad_row", func(t *testing.T) {
		path := writeCSVInferenceFile(t, dir, "too-many-bad.csv", csvInferenceRows("id,amount\n", 52, map[int]string{
			51: "51,abc",
			52: "52,def",
		}))
		expectCSVInferenceLoadError(t, path+" with max_bad_records=1", "row 53", "amount", "int", "def")
	})
}

func TestIntegrationCSVInferenceInteractions(t *testing.T) {
	dir := t.TempDir()

	t.Run("header_false_and_delim", func(t *testing.T) {
		path := writeCSVInferenceFile(t, dir, "semi.csv", "1;10\n2;20\n3;abc\n4;40\n")
		result := loadAndQuery(t, path+` with header=false, delim=";", infer_rows=2, max_bad_records=1`, "count")
		if got := result.GetAt(0, 0).Int; got != 3 {
			t.Fatalf("count after one skipped bad row: got %d, want 3", got)
		}
	})

	t.Run("allow_jagged_rows_still_controls_missing_fields", func(t *testing.T) {
		path := writeCSVInferenceFile(t, dir, "jagged.csv", "id,amount\n1,10\n2\n3,30\n")
		result := loadAndQuery(t, path+" with allow_jagged_rows=true, infer_rows=-1", "count")
		if got := result.GetAt(0, 0).Int; got != 3 {
			t.Fatalf("jagged row count: got %d, want 3", got)
		}
	})

	t.Run("max_bad_records_does_not_swallow_extra_columns", func(t *testing.T) {
		path := writeCSVInferenceFile(t, dir, "extra.csv", "id,amount\n1,10\n2,20,extra\n")
		expectCSVInferenceLoadError(t, path+" with infer_rows=1, max_bad_records=10", "ignore_unknown_values=true")
	})

	t.Run("ignore_unknown_values_still_controls_extra_columns", func(t *testing.T) {
		path := writeCSVInferenceFile(t, dir, "extra-ignored.csv", "id,amount\n1,10\n2,20,extra\n")
		result := loadAndQuery(t, path+" with ignore_unknown_values=true, infer_rows=-1", "count")
		if got := result.GetAt(0, 0).Int; got != 2 {
			t.Fatalf("row count with ignored extra column: got %d, want 2", got)
		}
	})
}

func TestIntegrationCSVInferenceCompressedInputs(t *testing.T) {
	dir := t.TempDir()
	content := csvInferenceRows("id,amount\n", 51, map[int]string{51: "51,abc"})

	cases := []struct {
		name string
		file string
		data []byte
	}{
		{"gzip", "rows.csv.gz", gzipIntegrationBytes(t, content)},
		{"zstd", "rows.csv.zst", zstdIntegrationBytes(t, content)},
		{"deflate", "rows.csv.deflate", deflateIntegrationBytes(t, content)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, tc.file)
			if err := os.WriteFile(path, tc.data, 0o644); err != nil {
				t.Fatal(err)
			}
			expectCSVInferenceLoadError(t, path, "row 52", "amount", "abc")
			result := loadAndQuery(t, path+" with max_bad_records=1", "count")
			if got := result.GetAt(0, 0).Int; got != 50 {
				t.Fatalf("count after skipping compressed bad row: got %d, want 50", got)
			}
		})
	}
}

func TestIntegrationCSVInferenceGlobAndStdin(t *testing.T) {
	t.Run("glob_infers_across_deterministic_shard_order", func(t *testing.T) {
		dir := t.TempDir()
		writeCSVInferenceFile(t, dir, "a.csv", csvInferenceRows("id,amount\n", 50, nil))
		writeCSVInferenceFile(t, dir, "b.csv", "id,amount\n51,abc\n52,520\n")
		glob := filepath.Join(dir, "*.csv") + " with format=csv"

		expectCSVInferenceLoadError(t, glob, "row 2", "amount", "abc")
		assertCSVInferenceDescribe(t, glob+", infer_rows=-1", map[string]describeMeta{
			"id":     {typ: "int", rows: 52},
			"amount": {typ: "string", rows: 52},
		})
	})

	t.Run("stdin_supports_inference_options", func(t *testing.T) {
		q, err := parser.Parse("- with format=csv, infer_rows=1, max_bad_records=1 | count")
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}
		tbl, err := loader.LoadInput("-", loader.FromAST(q.Source.Load), strings.NewReader("id\n1\nbad\n2\n"))
		if err != nil {
			t.Fatalf("load stdin: %v", err)
		}
		result, err := Execute(q, tbl, nil)
		if err != nil {
			t.Fatalf("exec stdin query: %v", err)
		}
		if got := result.GetAt(0, 0).Int; got != 2 {
			t.Fatalf("stdin count after skipped row: got %d, want 2", got)
		}
	})
}

func TestIntegrationCSVInferenceJoinSource(t *testing.T) {
	dir := t.TempDir()
	leftPath := writeCSVInferenceFile(t, dir, "left.csv", "id\n1\n2\n")
	rightPath := writeCSVInferenceFile(t, dir, "right.csv", "id,note\n1,one\nbad,bad-row\n2,two\n")

	q, err := parser.Parse(leftPath + ` | join left ` + rightPath + ` with infer_rows=1, max_bad_records=1 on id | sort id`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	left, err := loader.Load(q.Source.Filename, loader.FromAST(q.Source.Load))
	if err != nil {
		t.Fatalf("load left: %v", err)
	}
	result, err := Execute(q, left, func(filename string, opts ast.LoadOptions) (*table.Table, error) {
		return loader.Load(filename, loader.FromAST(opts))
	})
	if err != nil {
		t.Fatalf("join exec: %v", err)
	}
	if result.NumRows != 2 {
		t.Fatalf("joined rows: got %d, want 2", result.NumRows)
	}
}
