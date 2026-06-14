package loader

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
