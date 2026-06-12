package loader

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadCSVExtraFieldsStrict(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "extra.csv")
	if err := os.WriteFile(path, []byte("a,b\n1,2,3,4\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path, Options{})
	if err == nil {
		t.Fatal("expected error for extra fields")
	}
	if !strings.Contains(err.Error(), "ignore_unknown_values=true") {
		t.Fatalf("error should suggest ignore_unknown_values: %v", err)
	}
}

func TestLoadCSVExtraFieldsLenient(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "extra.csv")
	if err := os.WriteFile(path, []byte("a,b\n1,2,3,4\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tbl, err := Load(path, Options{IgnoreUnknownValues: BoolPtr(true)})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if tbl.NumRows != 1 || tbl.Get(0, "a").Int != 1 || tbl.Get(0, "b").Int != 2 {
		t.Fatalf("got %s", tbl.String())
	}
}

func TestLoadCSVJaggedRowsStrict(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "jagged.csv")
	if err := os.WriteFile(path, []byte("a,b\n1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path, Options{})
	if err == nil {
		t.Fatal("expected error for missing trailing column")
	}
	if !strings.Contains(err.Error(), "allow_jagged_rows=true") {
		t.Fatalf("error should suggest allow_jagged_rows: %v", err)
	}
}

func TestLoadCSVJaggedRowsLenient(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "jagged.csv")
	if err := os.WriteFile(path, []byte("a,b\n1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tbl, err := Load(path, Options{AllowJaggedRows: BoolPtr(true)})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if tbl.NumRows != 1 || tbl.Get(0, "a").Int != 1 || !tbl.Get(0, "b").IsNull() {
		t.Fatalf("got %s", tbl.String())
	}
}

func TestLoadCSVSingleFileAndGlobSameStrictRules(t *testing.T) {
	dir := t.TempDir()
	single := filepath.Join(dir, "one.csv")
	if err := os.WriteFile(single, []byte("a,b\n1,2,3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	globDir := filepath.Join(dir, "parts")
	if err := os.Mkdir(globDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(globDir, "a.csv"), []byte("a,b\n1,2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(globDir, "b.csv"), []byte("3,4,5\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, errSingle := Load(single, Options{})
	_, errGlob := Load(filepath.Join(globDir, "*.csv"), Options{})
	if errSingle == nil || errGlob == nil {
		t.Fatalf("expected both to fail: single=%v glob=%v", errSingle, errGlob)
	}
	if !strings.Contains(errSingle.Error(), "ignore_unknown_values=true") {
		t.Fatalf("single file error: %v", errSingle)
	}
	if !strings.Contains(errGlob.Error(), "ignore_unknown_values=true") {
		t.Fatalf("glob error: %v", errGlob)
	}
}
