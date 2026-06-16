package loader

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/razeghi71/dq/ast"
)

func TestLoadOptionsDefaultInferRows(t *testing.T) {
	const wantDefaultInferRows = 20480

	opts := normalizeOptions(Options{})
	if opts.InferRows != wantDefaultInferRows {
		t.Fatalf("default infer rows: got %d, want %d", opts.InferRows, wantDefaultInferRows)
	}
	if opts.InferRowsSet {
		t.Fatal("default infer rows should not mark InferRowsSet")
	}

	fromAST := FromAST(ast.LoadOptions{})
	if fromAST.InferRows != wantDefaultInferRows {
		t.Fatalf("FromAST default infer rows: got %d, want %d", fromAST.InferRows, wantDefaultInferRows)
	}
	if fromAST.InferRowsSet {
		t.Fatal("FromAST default infer rows should not mark InferRowsSet")
	}
}

func TestLoadOptionsExplicitInferRowsZeroSurvivesNormalization(t *testing.T) {
	opts := normalizeOptions(Options{InferRows: 0, InferRowsSet: true})
	if opts.InferRows != 0 {
		t.Fatalf("explicit infer rows zero: got %d, want 0", opts.InferRows)
	}
	if !opts.InferRowsSet {
		t.Fatal("explicit infer rows zero should keep InferRowsSet")
	}

	zero := 0
	fromAST := FromAST(ast.LoadOptions{InferRows: &zero})
	if fromAST.InferRows != 0 {
		t.Fatalf("FromAST explicit infer rows zero: got %d, want 0", fromAST.InferRows)
	}
	if !fromAST.InferRowsSet {
		t.Fatal("FromAST explicit infer rows zero should keep InferRowsSet")
	}
}

func TestLoadOptionsExplicitMaxBadRecordsZeroSurvivesFromAST(t *testing.T) {
	zero := 0
	opts := FromAST(ast.LoadOptions{MaxBadRecords: &zero})
	if opts.MaxBadRecords != 0 {
		t.Fatalf("FromAST explicit max bad records zero: got %d, want 0", opts.MaxBadRecords)
	}
	if !opts.MaxBadRecordsSet {
		t.Fatal("FromAST explicit max bad records zero should set MaxBadRecordsSet")
	}
}

func TestLoadOptionsAllowJSONInferenceOptions(t *testing.T) {
	cases := []struct {
		name   string
		format string
		input  string
	}{
		{name: "json", format: "json", input: `[{"x":1}]`},
		{name: "jsonl", format: "jsonl", input: "{\"x\":1}\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tbl, err := LoadReader(strings.NewReader(tc.input), Options{
				Format:           tc.format,
				InferRows:        1,
				InferRowsSet:     true,
				MaxBadRecords:    0,
				MaxBadRecordsSet: true,
			})
			if err != nil {
				t.Fatalf("load %s with inference options: %v", tc.format, err)
			}
			if tbl.NumRows != 1 || tbl.Get(0, "x").Int != 1 {
				t.Fatalf("unexpected table: %s", tbl.String())
			}
		})
	}
}

func TestLoadOptionsRejectsJSONInferRowsZero(t *testing.T) {
	for _, format := range []string{"json", "jsonl"} {
		t.Run(format, func(t *testing.T) {
			_, err := LoadReader(strings.NewReader(`[{"x":1}]`), Options{
				Format:       format,
				InferRows:    0,
				InferRowsSet: true,
			})
			if err == nil {
				t.Fatal("expected infer_rows=0 to be rejected")
			}
			if !strings.Contains(err.Error(), "infer_rows=0") || !strings.Contains(err.Error(), format) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestLoadOptionsRejectsInvalidDirectInferenceValues(t *testing.T) {
	cases := []struct {
		name string
		opts Options
		want string
	}{
		{
			name: "csv_infer_rows_too_negative",
			opts: Options{Format: "csv", InferRows: -2},
			want: "infer_rows must be -1 or greater",
		},
		{
			name: "csv_max_bad_records_negative",
			opts: Options{Format: "csv", MaxBadRecords: -1},
			want: "max_bad_records must be greater than or equal to 0",
		},
		{
			name: "json_infer_rows_too_negative",
			opts: Options{Format: "json", InferRows: -2},
			want: "infer_rows must be -1 or greater",
		},
		{
			name: "jsonl_max_bad_records_negative",
			opts: Options{Format: "jsonl", MaxBadRecords: -1},
			want: "max_bad_records must be greater than or equal to 0",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadReader(strings.NewReader("x\n1\n"), tc.opts)
			if err == nil {
				t.Fatal("expected load option validation error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestLoadOptionsAllowsExplicitMaxBadRecordsZeroOnCSV(t *testing.T) {
	tbl, err := LoadReader(strings.NewReader("x\n1\n"), Options{
		Format:           "csv",
		MaxBadRecords:    0,
		MaxBadRecordsSet: true,
	})
	if err != nil {
		t.Fatalf("load csv with explicit max_bad_records=0: %v", err)
	}
	if tbl.NumRows != 1 || tbl.Get(0, "x").Int != 1 {
		t.Fatalf("unexpected table: %s", tbl.String())
	}
}

func TestLoadOptionsFormatOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.dat")
	if err := os.WriteFile(path, []byte("name\nAlice\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tbl, err := Load(path, Options{Format: "csv"})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if tbl.NumRows != 1 || tbl.ColIndex("name") < 0 {
		t.Fatalf("expected one row with name column, got %s", tbl.String())
	}
}

func TestLoadOptionsCSVHeaderFalse(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rows.dat")
	if err := os.WriteFile(path, []byte("1,2\n3,4\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tbl, err := Load(path, Options{Format: "csv", Header: BoolPtr(false)})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if tbl.ColIndex("col1") < 0 || tbl.ColIndex("col2") < 0 {
		t.Fatalf("expected col1/col2 columns, got %v", tbl.Columns)
	}
	if tbl.NumRows != 2 {
		t.Fatalf("expected 2 data rows, got %d", tbl.NumRows)
	}
	if tbl.Get(0, "col1").Int != 1 || tbl.Get(0, "col2").Int != 2 {
		t.Errorf("row 0: got %s", tbl.String())
	}
	if tbl.Get(1, "col1").Int != 3 || tbl.Get(1, "col2").Int != 4 {
		t.Errorf("row 1: got %s", tbl.String())
	}
}

func TestLoadOptionsCSVDelim(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "semi.csv")
	if err := os.WriteFile(path, []byte("a;b\n1;2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tbl, err := Load(path, Options{Format: "csv", Delim: ";"})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if tbl.ColIndex("a") < 0 || tbl.ColIndex("b") < 0 {
		t.Fatalf("expected columns a,b got %v", tbl.Columns)
	}
	if tbl.Get(0, "a").Int != 1 || tbl.Get(0, "b").Int != 2 {
		t.Errorf("row 0: got %s", tbl.String())
	}
}

func TestLoadOptionsStdin(t *testing.T) {
	tbl, err := LoadInput("-", Options{Format: "csv"}, strings.NewReader("name\nBob\n"))
	if err != nil {
		t.Fatalf("load stdin: %v", err)
	}
	if tbl.NumRows != 1 || tbl.Get(0, "name").Str != "Bob" {
		t.Fatalf("got %s", tbl.String())
	}
}

func TestLoadOptionsGzipStdin(t *testing.T) {
	tbl, err := LoadInput("-", Options{Format: "csv", Compression: "gzip"}, bytes.NewReader(gzipTestBytes(t, "name\nBob\n")))
	if err != nil {
		t.Fatalf("load gzip stdin: %v", err)
	}
	if tbl.NumRows != 1 || tbl.Get(0, "name").Str != "Bob" {
		t.Fatalf("got %s", tbl.String())
	}
}

func TestLoadOptionsGzipJSONLStdin(t *testing.T) {
	data := "{\"level\":\"INFO\"}\n{\"level\":\"ERROR\"}\n"
	tbl, err := LoadInput("-", Options{Format: "jsonl", Compression: "gzip"}, bytes.NewReader(gzipTestBytes(t, data)))
	if err != nil {
		t.Fatalf("load gzip jsonl stdin: %v", err)
	}
	if tbl.NumRows != 2 || tbl.Get(1, "level").Str != "ERROR" {
		t.Fatalf("got %s", tbl.String())
	}
}

func TestLoadOptionsZstdStdin(t *testing.T) {
	tbl, err := LoadInput("-", Options{Format: "csv", Compression: "zstd"}, bytes.NewReader(zstdTestBytes(t, "name\nBob\n")))
	if err != nil {
		t.Fatalf("load zstd stdin: %v", err)
	}
	if tbl.NumRows != 1 || tbl.Get(0, "name").Str != "Bob" {
		t.Fatalf("got %s", tbl.String())
	}
}

func TestLoadOptionsZstdJSONLStdin(t *testing.T) {
	data := "{\"level\":\"INFO\"}\n{\"level\":\"ERROR\"}\n"
	tbl, err := LoadInput("-", Options{Format: "jsonl", Compression: "zstd"}, bytes.NewReader(zstdTestBytes(t, data)))
	if err != nil {
		t.Fatalf("load zstd jsonl stdin: %v", err)
	}
	if tbl.NumRows != 2 || tbl.Get(1, "level").Str != "ERROR" {
		t.Fatalf("got %s", tbl.String())
	}
}

func TestLoadOptionsDeflateStdin(t *testing.T) {
	tbl, err := LoadInput("-", Options{Format: "csv", Compression: "deflate"}, bytes.NewReader(deflateTestBytes(t, "name\nBob\n")))
	if err != nil {
		t.Fatalf("load deflate stdin: %v", err)
	}
	if tbl.NumRows != 1 || tbl.Get(0, "name").Str != "Bob" {
		t.Fatalf("got %s", tbl.String())
	}
}

func TestLoadOptionsDeflateJSONLStdin(t *testing.T) {
	data := "{\"level\":\"INFO\"}\n{\"level\":\"ERROR\"}\n"
	tbl, err := LoadInput("-", Options{Format: "jsonl", Compression: "deflate"}, bytes.NewReader(deflateTestBytes(t, data)))
	if err != nil {
		t.Fatalf("load deflate jsonl stdin: %v", err)
	}
	if tbl.NumRows != 2 || tbl.Get(1, "level").Str != "ERROR" {
		t.Fatalf("got %s", tbl.String())
	}
}

func TestLoadOptionsBadDeflateStdin(t *testing.T) {
	_, err := LoadInput("-", Options{Format: "csv", Compression: "deflate"}, strings.NewReader("name\nBob\n"))
	if err == nil {
		t.Fatal("expected deflate error")
	}
	lower := strings.ToLower(err.Error())
	if !strings.Contains(lower, "deflate") && !strings.Contains(lower, "zlib") {
		t.Fatalf("error should mention deflate/zlib, got %v", err)
	}
}

func TestLoadOptionsDeflateStdinRejectsUnsupportedFormat(t *testing.T) {
	_, err := LoadInput("-", Options{Format: "parquet", Compression: "deflate"}, bytes.NewReader(deflateTestBytes(t, "x")))
	if err == nil {
		t.Fatal("expected error for compressed parquet stdin")
	}
	lower := strings.ToLower(err.Error())
	if !strings.Contains(lower, "compression=deflate") || !strings.Contains(lower, "csv") || !strings.Contains(lower, "jsonl") {
		t.Fatalf("expected compression format restriction, got %v", err)
	}
}

func TestLoadOptionsBadZstdStdin(t *testing.T) {
	_, err := LoadInput("-", Options{Format: "csv", Compression: "zstd"}, strings.NewReader("name\nBob\n"))
	if err == nil {
		t.Fatal("expected zstd error")
	}
	lower := strings.ToLower(err.Error())
	if !strings.Contains(lower, "zstd") && !strings.Contains(lower, "zstandard") {
		t.Fatalf("error should mention zstd, got %v", err)
	}
}

func TestLoadOptionsZstdStdinRejectsUnsupportedFormat(t *testing.T) {
	_, err := LoadInput("-", Options{Format: "parquet", Compression: "zstd"}, bytes.NewReader(zstdTestBytes(t, "x")))
	if err == nil {
		t.Fatal("expected error for compressed parquet stdin")
	}
	lower := strings.ToLower(err.Error())
	if !strings.Contains(lower, "compression=zstd") || !strings.Contains(lower, "csv") || !strings.Contains(lower, "jsonl") {
		t.Fatalf("expected compression format restriction, got %v", err)
	}
}

func TestLoadOptionsBadGzipStdin(t *testing.T) {
	_, err := LoadInput("-", Options{Format: "csv", Compression: "gzip"}, strings.NewReader("name\nBob\n"))
	if err == nil {
		t.Fatal("expected gzip error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "gzip") {
		t.Fatalf("error should mention gzip, got %v", err)
	}
}

func TestLoadOptionsGzipStdinRejectsUnsupportedFormat(t *testing.T) {
	_, err := LoadInput("-", Options{Format: "parquet", Compression: "gzip"}, bytes.NewReader(gzipTestBytes(t, "x")))
	if err == nil {
		t.Fatal("expected error for compressed parquet stdin")
	}
	lower := strings.ToLower(err.Error())
	if !strings.Contains(lower, "compression=gzip") || !strings.Contains(lower, "csv") || !strings.Contains(lower, "jsonl") {
		t.Fatalf("expected compression format restriction, got %v", err)
	}
}

func TestLoadOptionsStdinRequiresFormat(t *testing.T) {
	_, err := LoadInput("-", Options{}, strings.NewReader("name\nBob\n"))
	if err == nil {
		t.Fatal("expected error when stdin format missing")
	}
	if !strings.Contains(err.Error(), "with format=") {
		t.Errorf("error should mention with format=: got %q", err.Error())
	}
}

func TestLoadOptionsRejectsCSVKeysOnJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.json")
	if err := os.WriteFile(path, []byte(`[{"x":1}]`), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path, Options{Format: "json", Header: BoolPtr(false)})
	if err == nil {
		t.Fatal("expected error for header= on json format")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "header") {
		t.Errorf("error: %q", err.Error())
	}
}

func TestLoadOptionsFormatCaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.dat")
	if err := os.WriteFile(path, []byte("name\nAlice\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tbl, err := Load(path, Options{Format: "CSV"})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if tbl.NumRows != 1 || tbl.ColIndex("name") < 0 {
		t.Fatalf("expected one row with name column, got %s", tbl.String())
	}
}

func TestLoadOptionsRejectsCSVHeaderOnInferredJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.json")
	if err := os.WriteFile(path, []byte(`[{"x":1}]`), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path, Options{Header: BoolPtr(false)})
	if err == nil {
		t.Fatal("expected error for header= on inferred json format")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "header") {
		t.Errorf("error: %q", err.Error())
	}
}

func TestLoadOptionsRejectsCSVDelimOnInferredJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.json")
	if err := os.WriteFile(path, []byte(`[{"x":1}]`), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path, Options{Delim: ";"})
	if err == nil {
		t.Fatal("expected error for delim= on inferred json format")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "delim") {
		t.Errorf("error: %q", err.Error())
	}
}

func TestLoadOptionsRejectsCSVKeysWithoutFormatOnExtensionlessFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.dat")
	if err := os.WriteFile(path, []byte("1,2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path, Options{Header: BoolPtr(false)})
	if err == nil {
		t.Fatal("expected error for header= without format on extensionless file")
	}
	if !strings.Contains(err.Error(), "with format=") {
		t.Errorf("error should mention with format=: got %q", err.Error())
	}
}

func TestLoadOptionsStdinRejectsUnsupportedFormat(t *testing.T) {
	_, err := LoadInput("-", Options{Format: "parquet"}, strings.NewReader("name\nBob\n"))
	if err == nil {
		t.Fatal("expected error for parquet on stdin")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "unsupported") {
		t.Errorf("error: %q", err.Error())
	}
}
