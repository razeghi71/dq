package loader

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	goavro "github.com/linkedin/goavro/v2"
	"github.com/razeghi71/dq/table"
)

const testdataDir = "../testdata"

// fieldVal returns the named field from a TypeRecord value.
func fieldVal(t *testing.T, v table.Value, name string) table.Value {
	t.Helper()
	if v.Type != table.TypeRecord {
		t.Fatalf("expected TypeRecord, got type %v (%s)", v.Type, v.AsString())
	}
	for _, f := range v.Fields {
		if f.Name == name {
			return f.Value
		}
	}
	names := make([]string, len(v.Fields))
	for i, f := range v.Fields {
		names[i] = f.Name
	}
	t.Fatalf("field %q not found in record; available: %v", name, names)
	return table.Null()
}

// listLen asserts the value is a TypeList and returns its length.
func listLen(t *testing.T, v table.Value) int {
	t.Helper()
	if v.Type != table.TypeList {
		t.Fatalf("expected TypeList, got type %v (%s)", v.Type, v.AsString())
	}
	return len(v.List)
}

// elem returns the i-th element of a TypeList value.
func elem(t *testing.T, v table.Value, i int) table.Value {
	t.Helper()
	n := listLen(t, v)
	if i >= n {
		t.Fatalf("index %d out of range (list len %d)", i, n)
	}
	return v.List[i]
}

// ============================================================
// Flat user files — CSV, JSON, JSONL, Avro, Parquet
// ============================================================

// checkUsersTable verifies the flat 6-row users table shared by
// users.csv, users.avro, and users.parquet.
func checkUsersTable(t *testing.T, tbl *table.Table) {
	t.Helper()

	if tbl.NumRows != 6 {
		t.Fatalf("expected 6 rows, got %d", tbl.NumRows)
	}

	nameIdx := tbl.ColIndex("name")
	ageIdx := tbl.ColIndex("age")
	cityIdx := tbl.ColIndex("city")
	if nameIdx < 0 || ageIdx < 0 || cityIdx < 0 {
		t.Fatalf("missing expected columns; got %v", tbl.Columns)
	}

	// row 0: Alice, 30, NY
	if tbl.GetAt(0, nameIdx).Type != table.TypeString || tbl.GetAt(0, nameIdx).Str != "Alice" {
		t.Errorf("row 0 name: want Alice, got %q", tbl.GetAt(0, nameIdx).Str)
	}
	if tbl.GetAt(0, ageIdx).Type != table.TypeInt || tbl.GetAt(0, ageIdx).Int != 30 {
		t.Errorf("row 0 age: want int 30, got %v", tbl.GetAt(0, ageIdx).AsString())
	}
	if tbl.GetAt(0, cityIdx).Str != "NY" {
		t.Errorf("row 0 city: want NY, got %q", tbl.GetAt(0, cityIdx).Str)
	}

	// row 5: Frank, 40, NY
	if tbl.GetAt(5, nameIdx).Str != "Frank" {
		t.Errorf("row 5 name: want Frank, got %q", tbl.GetAt(5, nameIdx).Str)
	}
	if tbl.GetAt(5, ageIdx).Int != 40 {
		t.Errorf("row 5 age: want 40, got %d", tbl.GetAt(5, ageIdx).Int)
	}
}

func TestLoadCSV(t *testing.T) {
	tbl, err := Load(testdataDir+"/users.csv", Options{})
	if err != nil {
		t.Fatal(err)
	}
	checkUsersTable(t, tbl)
}

func TestLoadGzipCSVByDoubleExtension(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.csv.gz")
	if err := os.WriteFile(path, gzipTestBytes(t, "name,age,city\nAlice,30,NY\nBob,25,LA\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tbl, err := Load(path, Options{})
	if err != nil {
		t.Fatalf("load gzip csv: %v", err)
	}
	if tbl.NumRows != 2 || tbl.Get(0, "name").Str != "Alice" || tbl.Get(1, "age").Int != 25 {
		t.Fatalf("unexpected table: %s", tbl.String())
	}
}

func TestLoadGzipJSONByDoubleExtension(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json.gz")
	if err := os.WriteFile(path, gzipTestBytes(t, `[{"name":"Alice","age":30},{"name":"Bob","age":25}]`), 0o644); err != nil {
		t.Fatal(err)
	}

	tbl, err := Load(path, Options{})
	if err != nil {
		t.Fatalf("load gzip json: %v", err)
	}
	if tbl.NumRows != 2 || tbl.Get(1, "name").Str != "Bob" {
		t.Fatalf("unexpected table: %s", tbl.String())
	}
}

func TestLoadGzipJSONLByDoubleExtension(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl.gz")
	data := "{\"level\":\"INFO\",\"msg\":\"start\"}\n{\"level\":\"ERROR\",\"msg\":\"timeout\"}\n"
	if err := os.WriteFile(path, gzipTestBytes(t, data), 0o644); err != nil {
		t.Fatal(err)
	}

	tbl, err := Load(path, Options{})
	if err != nil {
		t.Fatalf("load gzip jsonl: %v", err)
	}
	if tbl.NumRows != 2 || tbl.Get(1, "level").Str != "ERROR" {
		t.Fatalf("unexpected table: %s", tbl.String())
	}
}

func TestLoadGzipUnsupportedFormatsRejected(t *testing.T) {
	for _, name := range []string{"data.avro.gz", "data.parquet.gz"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), name)
			if err := os.WriteFile(path, gzipTestBytes(t, "x"), 0o644); err != nil {
				t.Fatal(err)
			}

			_, err := Load(path, Options{})
			if err == nil {
				t.Fatal("expected compression format restriction")
			}
			lower := strings.ToLower(err.Error())
			if !strings.Contains(lower, "compression=gzip") || !strings.Contains(lower, "csv") || !strings.Contains(lower, "jsonl") {
				t.Fatalf("expected compression format restriction, got %v", err)
			}
		})
	}
}

func TestLoadGzipCSVOptionsStillApply(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rows.csv.gz")
	if err := os.WriteFile(path, gzipTestBytes(t, "1;Alice\n2;Bob;extra\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tbl, err := Load(path, Options{
		Header:              BoolPtr(false),
		Delim:               ";",
		IgnoreUnknownValues: BoolPtr(true),
	})
	if err != nil {
		t.Fatalf("load gzip csv with options: %v", err)
	}
	if tbl.NumRows != 2 || tbl.Get(0, "col1").Int != 1 || tbl.Get(1, "col2").Str != "Bob" {
		t.Fatalf("unexpected table: %s", tbl.String())
	}
	if tbl.ColIndex("col3") >= 0 {
		t.Fatalf("extra field should be dropped, got columns %v", tbl.Columns)
	}
}

func TestLoadGzipCSVEmptyAndBOMOnly(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "empty.csv.gz")
		if err := os.WriteFile(path, gzipTestBytes(t, ""), 0o644); err != nil {
			t.Fatal(err)
		}
		tbl, err := Load(path, Options{})
		if err != nil {
			t.Fatalf("load empty gzip csv: %v", err)
		}
		if tbl.NumRows != 0 || len(tbl.Columns) != 0 {
			t.Fatalf("expected empty table, got %s", tbl.String())
		}
	})

	t.Run("bom_only", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "bom.csv.gz")
		if err := os.WriteFile(path, gzipTestBytes(t, "\ufeff"), 0o644); err != nil {
			t.Fatal(err)
		}
		tbl, err := Load(path, Options{})
		if err != nil {
			t.Fatalf("load BOM-only gzip csv: %v", err)
		}
		if tbl.NumRows != 0 || len(tbl.Columns) != 0 {
			t.Fatalf("expected empty table, got %s", tbl.String())
		}
	})
}

func TestLoadGzipCSVBadStreamsReturnGzipError(t *testing.T) {
	t.Run("plain_text_named_gz", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "bad.csv.gz")
		if err := os.WriteFile(path, []byte("name\nAlice\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := Load(path, Options{})
		if err == nil {
			t.Fatal("expected gzip error")
		}
		lower := strings.ToLower(err.Error())
		if !strings.Contains(lower, "cannot read gzip stream") {
			t.Fatalf("error should use gzip-specific open message, got %v", err)
		}
	})

	t.Run("truncated_gzip", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "truncated.csv.gz")
		data := gzipTestBytes(t, "name\nAlice\n")
		if err := os.WriteFile(path, data[:len(data)-4], 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := Load(path, Options{})
		if err == nil {
			t.Fatal("expected gzip error")
		}
		lower := strings.ToLower(err.Error())
		if !strings.Contains(lower, "gzip") && !strings.Contains(lower, "unexpected eof") && !strings.Contains(lower, "checksum") {
			t.Fatalf("error should mention gzip/truncation, got %v", err)
		}
	})
}

func TestLoadGzipJSONBadStreamsReturnGzipError(t *testing.T) {
	for _, name := range []string{"bad.json.gz", "bad.jsonl.gz"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), name)
			if err := os.WriteFile(path, []byte(`{"name":"Alice"}`), 0o644); err != nil {
				t.Fatal(err)
			}

			_, err := Load(path, Options{})
			if err == nil {
				t.Fatal("expected gzip error")
			}
			if !strings.Contains(strings.ToLower(err.Error()), "gzip") {
				t.Fatalf("error should mention gzip, got %v", err)
			}
		})
	}
}

func TestLoadZstdTextByDoubleExtension(t *testing.T) {
	cases := []struct {
		name     string
		filename string
		content  string
		check    func(t *testing.T, tbl *table.Table)
	}{
		{
			name:     "csv_zst",
			filename: "users.csv.zst",
			content:  "name,age,city\nAlice,30,NY\nBob,25,LA\n",
			check: func(t *testing.T, tbl *table.Table) {
				t.Helper()
				if tbl.NumRows != 2 || tbl.Get(0, "name").Str != "Alice" || tbl.Get(1, "age").Int != 25 {
					t.Fatalf("unexpected table: %s", tbl.String())
				}
			},
		},
		{
			name:     "csv_zstd",
			filename: "users.csv.zstd",
			content:  "name,age\nAlice,30\nBob,25\n",
			check: func(t *testing.T, tbl *table.Table) {
				t.Helper()
				if tbl.NumRows != 2 || tbl.Get(1, "name").Str != "Bob" {
					t.Fatalf("unexpected table: %s", tbl.String())
				}
			},
		},
		{
			name:     "json_zst",
			filename: "users.json.zst",
			content:  `[{"name":"Alice","age":30},{"name":"Bob","age":25}]`,
			check: func(t *testing.T, tbl *table.Table) {
				t.Helper()
				if tbl.NumRows != 2 || tbl.Get(1, "name").Str != "Bob" {
					t.Fatalf("unexpected table: %s", tbl.String())
				}
			},
		},
		{
			name:     "jsonl_zstd",
			filename: "events.jsonl.zstd",
			content:  "{\"level\":\"INFO\",\"msg\":\"start\"}\n{\"level\":\"ERROR\",\"msg\":\"timeout\"}\n",
			check: func(t *testing.T, tbl *table.Table) {
				t.Helper()
				if tbl.NumRows != 2 || tbl.Get(1, "level").Str != "ERROR" {
					t.Fatalf("unexpected table: %s", tbl.String())
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), tc.filename)
			if err := os.WriteFile(path, zstdTestBytes(t, tc.content), 0o644); err != nil {
				t.Fatal(err)
			}
			tbl, err := Load(path, Options{})
			if err != nil {
				t.Fatalf("load zstd input: %v", err)
			}
			tc.check(t, tbl)
		})
	}
}

func TestLoadZstdExplicitCompressionExtensionless(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.data")
	data := "{\"level\":\"INFO\",\"msg\":\"start\"}\n{\"level\":\"ERROR\",\"msg\":\"timeout\"}\n"
	if err := os.WriteFile(path, zstdTestBytes(t, data), 0o644); err != nil {
		t.Fatal(err)
	}

	tbl, err := Load(path, Options{Format: "jsonl", Compression: "zstd"})
	if err != nil {
		t.Fatalf("load explicit zstd jsonl: %v", err)
	}
	if tbl.NumRows != 2 || tbl.Get(1, "msg").Str != "timeout" {
		t.Fatalf("unexpected table: %s", tbl.String())
	}
}

func TestLoadZstdExplicitFormatOverridesInnerSuffix(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.csv.zst")
	data := "{\"level\":\"ERROR\",\"msg\":\"jsonl despite suffix\"}\n"
	if err := os.WriteFile(path, zstdTestBytes(t, data), 0o644); err != nil {
		t.Fatal(err)
	}

	tbl, err := Load(path, Options{Format: "jsonl"})
	if err != nil {
		t.Fatalf("load zstd with explicit format override: %v", err)
	}
	if tbl.NumRows != 1 || tbl.Get(0, "msg").Str != "jsonl despite suffix" {
		t.Fatalf("unexpected table: %s", tbl.String())
	}
}

func TestLoadZstdCSVOptionsStillApply(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rows.csv.zst")
	if err := os.WriteFile(path, zstdTestBytes(t, "1;Alice\n2;Bob;extra\n3\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tbl, err := Load(path, Options{
		Header:              BoolPtr(false),
		Delim:               ";",
		AllowJaggedRows:     BoolPtr(true),
		IgnoreUnknownValues: BoolPtr(true),
	})
	if err != nil {
		t.Fatalf("load zstd csv with options: %v", err)
	}
	if tbl.NumRows != 3 || tbl.Get(0, "col1").Int != 1 || tbl.Get(1, "col2").Str != "Bob" {
		t.Fatalf("unexpected table: %s", tbl.String())
	}
	if tbl.Get(2, "col2").Type != table.TypeNull {
		t.Fatalf("missing trailing field should be null-filled, got %s", tbl.Get(2, "col2").AsString())
	}
	if tbl.ColIndex("col3") >= 0 {
		t.Fatalf("extra field should be dropped, got columns %v", tbl.Columns)
	}
}

func TestLoadZstdCSVEmptyAndBOMOnly(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "empty.csv.zst")
		if err := os.WriteFile(path, zstdTestBytes(t, ""), 0o644); err != nil {
			t.Fatal(err)
		}
		tbl, err := Load(path, Options{})
		if err != nil {
			t.Fatalf("load empty zstd csv: %v", err)
		}
		if tbl.NumRows != 0 || len(tbl.Columns) != 0 {
			t.Fatalf("expected empty table, got %s", tbl.String())
		}
	})

	t.Run("bom_only", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "bom.csv.zstd")
		if err := os.WriteFile(path, zstdTestBytes(t, "\ufeff"), 0o644); err != nil {
			t.Fatal(err)
		}
		tbl, err := Load(path, Options{})
		if err != nil {
			t.Fatalf("load BOM-only zstd csv: %v", err)
		}
		if tbl.NumRows != 0 || len(tbl.Columns) != 0 {
			t.Fatalf("expected empty table, got %s", tbl.String())
		}
	})
}

func TestLoadZstdUnsupportedFormatsRejected(t *testing.T) {
	for _, name := range []string{"data.avro.zst", "data.parquet.zst"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), name)
			if err := os.WriteFile(path, zstdTestBytes(t, "x"), 0o644); err != nil {
				t.Fatal(err)
			}

			_, err := Load(path, Options{})
			if err == nil {
				t.Fatal("expected compression format restriction")
			}
			lower := strings.ToLower(err.Error())
			if !strings.Contains(lower, "compression=zstd") || !strings.Contains(lower, "csv") || !strings.Contains(lower, "jsonl") {
				t.Fatalf("expected compression format restriction, got %v", err)
			}
		})
	}
}

func TestLoadZstdBadStreamsReturnZstdError(t *testing.T) {
	cases := []struct {
		name    string
		content []byte
	}{
		{name: "plain_text_named_zst", content: []byte("name\nAlice\n")},
		{name: "truncated_zstd", content: zstdTestBytes(t, "name\nAlice\n")[:8]},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "bad.csv.zst")
			if err := os.WriteFile(path, tc.content, 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := Load(path, Options{})
			if err == nil {
				t.Fatal("expected zstd error")
			}
			lower := strings.ToLower(err.Error())
			if !strings.Contains(lower, "zstd") && !strings.Contains(lower, "zstandard") {
				t.Fatalf("error should mention zstd, got %v", err)
			}
		})
	}
}

func TestLoadJSONLInvalidLineNumberIncludesBlankLines(t *testing.T) {
	_, err := LoadReader(strings.NewReader("\n{\"ok\":true}\nnot-json\n"), Options{Format: "jsonl"})
	if err == nil {
		t.Fatal("expected invalid JSONL error")
	}
	if !strings.Contains(err.Error(), "invalid JSON on line 3") {
		t.Fatalf("expected physical line 3 in error, got %v", err)
	}
}

func TestLoadEmptyCSVReader(t *testing.T) {
	tbl, err := LoadReader(strings.NewReader(""), Options{Format: "csv"})
	if err != nil {
		t.Fatalf("empty CSV should load: %v", err)
	}
	if tbl.NumRows != 0 {
		t.Fatalf("expected 0 rows, got %d", tbl.NumRows)
	}
	if len(tbl.Columns) != 0 {
		t.Fatalf("expected 0 columns, got %v", tbl.Columns)
	}
}

func TestLoadReaderGzipCompression(t *testing.T) {
	tbl, err := LoadReader(bytes.NewReader(gzipTestBytes(t, "name\nAlice\n")), Options{
		Format:      "csv",
		Compression: "gzip",
	})
	if err != nil {
		t.Fatalf("load compressed reader: %v", err)
	}
	if tbl.NumRows != 1 || tbl.Get(0, "name").Str != "Alice" {
		t.Fatalf("got %s", tbl.String())
	}
}

func TestLoadReaderZstdCompression(t *testing.T) {
	tbl, err := LoadReader(bytes.NewReader(zstdTestBytes(t, "name\nAlice\n")), Options{
		Format:      "csv",
		Compression: "zstd",
	})
	if err != nil {
		t.Fatalf("load zstd reader: %v", err)
	}
	if tbl.NumRows != 1 || tbl.Get(0, "name").Str != "Alice" {
		t.Fatalf("got %s", tbl.String())
	}
}

func TestLoadEmptyCSVFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.csv")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	tbl, err := Load(path, Options{})
	if err != nil {
		t.Fatalf("empty CSV file should load: %v", err)
	}
	if tbl.NumRows != 0 || len(tbl.Columns) != 0 {
		t.Fatalf("expected empty table, got %s", tbl.String())
	}
}

func TestLoadCSVBOMOnlyReader(t *testing.T) {
	tbl, err := LoadReader(strings.NewReader("\ufeff"), Options{Format: "csv"})
	if err != nil {
		t.Fatalf("BOM-only CSV should load: %v", err)
	}
	if tbl.NumRows != 0 || len(tbl.Columns) != 0 {
		t.Fatalf("expected empty table, got %s", tbl.String())
	}
}

func TestLoadCSVBOMOnlyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bom.csv")
	if err := os.WriteFile(path, []byte("\ufeff"), 0o644); err != nil {
		t.Fatal(err)
	}
	tbl, err := Load(path, Options{})
	if err != nil {
		t.Fatalf("BOM-only CSV file should load: %v", err)
	}
	if tbl.NumRows != 0 || len(tbl.Columns) != 0 {
		t.Fatalf("expected empty table, got %s", tbl.String())
	}
}

func TestLoadEmptyCSVStdin(t *testing.T) {
	tbl, err := LoadInput("-", Options{Format: "csv"}, strings.NewReader(""))
	if err != nil {
		t.Fatalf("empty stdin CSV should load: %v", err)
	}
	if tbl.NumRows != 0 || len(tbl.Columns) != 0 {
		t.Fatalf("expected empty table, got %s", tbl.String())
	}
}

func TestLoadEmptyCSVStdinNilReader(t *testing.T) {
	if testing.Short() {
		t.Skip("requires os.Stdin")
	}
	oldStdin := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdin = r
	defer func() { os.Stdin = oldStdin }()
	if _, err := w.Write([]byte("")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	tbl, err := LoadInput("-", Options{Format: "csv"}, nil)
	if err != nil {
		t.Fatalf("empty stdin CSV should load: %v", err)
	}
	if tbl.NumRows != 0 || len(tbl.Columns) != 0 {
		t.Fatalf("expected empty table, got %s", tbl.String())
	}
	_, err = io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
}

func TestLoadCSVHeaderOnly(t *testing.T) {
	tbl, err := LoadReader(strings.NewReader("name,age\n"), Options{Format: "csv"})
	if err != nil {
		t.Fatal(err)
	}
	if tbl.NumRows != 0 {
		t.Fatalf("expected 0 rows, got %d", tbl.NumRows)
	}
	if tbl.ColIndex("name") < 0 || tbl.ColIndex("age") < 0 {
		t.Fatalf("expected name and age columns, got %v", tbl.Columns)
	}
}

func csvWithUTF8BOM(content string) string {
	return "\ufeff" + content
}

func TestLoadCSVUTF8BOMStripsFirstHeaderField(t *testing.T) {
	tbl, err := LoadReader(strings.NewReader(csvWithUTF8BOM("name,age\nAlice,30\n")), Options{Format: "csv"})
	if err != nil {
		t.Fatal(err)
	}
	if tbl.ColIndex("name") < 0 {
		t.Fatalf("expected column name, got %v", tbl.Columns)
	}
	if tbl.ColIndex("\ufeffname") >= 0 {
		t.Fatalf("BOM must not appear in column names; got %v", tbl.Columns)
	}
	if tbl.NumRows != 1 {
		t.Fatalf("expected 1 row, got %d", tbl.NumRows)
	}
	if tbl.Get(0, "name").Str != "Alice" {
		t.Errorf("name: want Alice, got %q", tbl.Get(0, "name").Str)
	}
	if tbl.Get(0, "age").Int != 30 {
		t.Errorf("age: want 30, got %d", tbl.Get(0, "age").Int)
	}
}

func TestCSVColumnTypeWideningIntFloat(t *testing.T) {
	csv := "val\n1\n2.5\n3\n"
	tbl, err := LoadReader(strings.NewReader(csv), Options{Format: "csv"})
	if err != nil {
		t.Fatal(err)
	}
	idx := tbl.ColIndex("val")
	if tbl.Col(idx).ColType() != table.TypeFloat {
		t.Fatalf("expected column type Float, got %v", tbl.Col(idx).ColType())
	}
	want := []float64{1, 2.5, 3}
	for i, w := range want {
		got := tbl.GetAt(i, idx)
		if got.Type != table.TypeFloat {
			t.Errorf("row %d: expected TypeFloat, got %v", i, got.Type)
		}
		if got.Float != w {
			t.Errorf("row %d: want %g, got %g", i, w, got.Float)
		}
	}
}

func TestCSVColumnTypeWidening(t *testing.T) {
	// Column "val" goes int → float → string; all three rows must end up as strings.
	csv := "val\n1\n2.5\nsomething\n"
	tbl, err := LoadReader(strings.NewReader(csv), Options{Format: "csv"})
	if err != nil {
		t.Fatal(err)
	}
	if tbl.NumRows != 3 {
		t.Fatalf("expected 3 rows, got %d", tbl.NumRows)
	}
	idx := tbl.ColIndex("val")
	if tbl.Col(idx).ColType() != table.TypeString {
		t.Fatalf("expected column type String after widening, got %v", tbl.Col(idx).ColType())
	}
	want := []string{"1", "2.5", "something"}
	for i, w := range want {
		got := tbl.GetAt(i, idx)
		if got.Type != table.TypeString {
			t.Errorf("row %d: expected TypeString, got %v", i, got.Type)
		}
		if got.Str != w {
			t.Errorf("row %d: want %q, got %q", i, w, got.Str)
		}
	}
}

func TestLoadUsersJSON(t *testing.T) {
	tbl, err := Load(testdataDir+"/users.json", Options{})
	if err != nil {
		t.Fatal(err)
	}
	// users.json has 3 rows only
	if tbl.NumRows != 3 {
		t.Fatalf("expected 3 rows, got %d", tbl.NumRows)
	}
	nameIdx := tbl.ColIndex("name")
	if tbl.GetAt(0, nameIdx).Str != "Alice" {
		t.Errorf("row 0: want Alice, got %q", tbl.GetAt(0, nameIdx).Str)
	}
	if tbl.GetAt(0, tbl.ColIndex("age")).Int != 30 {
		t.Errorf("Alice age: want 30, got %d", tbl.GetAt(0, tbl.ColIndex("age")).Int)
	}
}

func TestLoadUsersJSONL(t *testing.T) {
	tbl, err := Load(testdataDir+"/users.jsonl", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if tbl.NumRows != 3 {
		t.Fatalf("expected 3 rows, got %d", tbl.NumRows)
	}
	if tbl.GetAt(2, tbl.ColIndex("name")).Str != "Charlie" {
		t.Errorf("row 2: want Charlie, got %q", tbl.GetAt(2, tbl.ColIndex("name")).Str)
	}
}

func TestLoadUsersAvro(t *testing.T) {
	tbl, err := Load(testdataDir+"/users.avro", Options{})
	if err != nil {
		t.Fatal(err)
	}
	checkUsersTable(t, tbl)
}

func TestLoadUsersParquet(t *testing.T) {
	tbl, err := Load(testdataDir+"/users.parquet", Options{})
	if err != nil {
		t.Fatal(err)
	}
	checkUsersTable(t, tbl)
}

// ============================================================
// Nested files — JSON, JSONL, Avro, Parquet
// ============================================================

// checkNestedTable validates the deeply nested test data shared across
// nested.json, nested.jsonl, nested.avro, and nested.parquet.
//
// Schema: id, name, address(record), tags(list), orders(list of records),
//
//	profile(record{stats(record), history(list of records{date, events(list)})})
func checkNestedTable(t *testing.T, tbl *table.Table) {
	t.Helper()

	if tbl.NumRows != 3 {
		t.Fatalf("expected 3 rows, got %d", tbl.NumRows)
	}
	for _, col := range []string{"id", "name", "address", "tags", "orders", "profile"} {
		if tbl.ColIndex(col) < 0 {
			t.Errorf("missing column %q; got columns %v", col, tbl.Columns)
		}
	}

	idIdx := tbl.ColIndex("id")
	nameIdx := tbl.ColIndex("name")
	addressIdx := tbl.ColIndex("address")
	tagsIdx := tbl.ColIndex("tags")
	ordersIdx := tbl.ColIndex("orders")
	profileIdx := tbl.ColIndex("profile")

	// ---- Row 0: Alice ----
	if tbl.GetAt(0, idIdx).Int != 1 {
		t.Errorf("Alice id: want 1, got %d", tbl.GetAt(0, idIdx).Int)
	}
	if tbl.GetAt(0, nameIdx).Str != "Alice" {
		t.Errorf("Alice name: want Alice, got %q", tbl.GetAt(0, nameIdx).Str)
	}

	// address is a record with city, street, zip
	addr := tbl.GetAt(0, addressIdx)
	if addr.Type != table.TypeRecord {
		t.Fatalf("address: want TypeRecord, got %v", addr.Type)
	}
	if v := fieldVal(t, addr, "city"); v.Str != "New York" {
		t.Errorf("address.city: want New York, got %q", v.Str)
	}
	if v := fieldVal(t, addr, "street"); v.Str != "123 Main St" {
		t.Errorf("address.street: want '123 Main St', got %q", v.Str)
	}
	if v := fieldVal(t, addr, "zip"); v.Str != "10001" {
		t.Errorf("address.zip: want 10001, got %q", v.Str)
	}

	// tags is a list of strings: ["admin", "user"]
	tags := tbl.GetAt(0, tagsIdx)
	if listLen(t, tags) != 2 {
		t.Errorf("Alice tags len: want 2, got %d", listLen(t, tags))
	}
	if elem(t, tags, 0).Str != "admin" {
		t.Errorf("tags[0]: want admin, got %q", elem(t, tags, 0).Str)
	}
	if elem(t, tags, 1).Str != "user" {
		t.Errorf("tags[1]: want user, got %q", elem(t, tags, 1).Str)
	}

	// orders is a list of 2 records
	orders := tbl.GetAt(0, ordersIdx)
	if listLen(t, orders) != 2 {
		t.Errorf("Alice orders len: want 2, got %d", listLen(t, orders))
	}
	o0 := elem(t, orders, 0)
	if o0.Type != table.TypeRecord {
		t.Fatalf("orders[0]: want TypeRecord, got %v", o0.Type)
	}
	if v := fieldVal(t, o0, "order_id"); v.Int != 101 {
		t.Errorf("orders[0].order_id: want 101, got %d", v.Int)
	}
	if v := fieldVal(t, o0, "status"); v.Str != "shipped" {
		t.Errorf("orders[0].status: want shipped, got %q", v.Str)
	}
	if v := fieldVal(t, o0, "amount"); v.Float != 59.99 {
		t.Errorf("orders[0].amount: want 59.99, got %v", v.Float)
	}
	o1 := elem(t, orders, 1)
	if v := fieldVal(t, o1, "order_id"); v.Int != 102 {
		t.Errorf("orders[1].order_id: want 102, got %d", v.Int)
	}
	if v := fieldVal(t, o1, "status"); v.Str != "pending" {
		t.Errorf("orders[1].status: want pending, got %q", v.Str)
	}

	// profile is a record with stats (record) and history (list)
	profile := tbl.GetAt(0, profileIdx)
	if profile.Type != table.TypeRecord {
		t.Fatalf("profile: want TypeRecord, got %v", profile.Type)
	}

	// profile.stats: {logins:42, score:9.5}
	stats := fieldVal(t, profile, "stats")
	if stats.Type != table.TypeRecord {
		t.Fatalf("profile.stats: want TypeRecord, got %v", stats.Type)
	}
	if v := fieldVal(t, stats, "logins"); v.Int != 42 {
		t.Errorf("stats.logins: want 42, got %d", v.Int)
	}
	if v := fieldVal(t, stats, "score"); v.Float != 9.5 {
		t.Errorf("stats.score: want 9.5, got %v", v.Float)
	}

	// profile.history: list of 2 records
	history := fieldVal(t, profile, "history")
	if listLen(t, history) != 2 {
		t.Errorf("Alice history len: want 2, got %d", listLen(t, history))
	}

	// history[0]: {date:"2024-01-10", events:["login","purchase","logout"]}
	h0 := elem(t, history, 0)
	if h0.Type != table.TypeRecord {
		t.Fatalf("history[0]: want TypeRecord, got %v", h0.Type)
	}
	if v := fieldVal(t, h0, "date"); v.Str != "2024-01-10" {
		t.Errorf("history[0].date: want 2024-01-10, got %q", v.Str)
	}
	events0 := fieldVal(t, h0, "events")
	if listLen(t, events0) != 3 {
		t.Errorf("history[0].events len: want 3, got %d", listLen(t, events0))
	}
	if elem(t, events0, 0).Str != "login" {
		t.Errorf("events[0]: want login, got %q", elem(t, events0, 0).Str)
	}
	if elem(t, events0, 1).Str != "purchase" {
		t.Errorf("events[1]: want purchase, got %q", elem(t, events0, 1).Str)
	}
	if elem(t, events0, 2).Str != "logout" {
		t.Errorf("events[2]: want logout, got %q", elem(t, events0, 2).Str)
	}

	// history[1]: {date:"2024-01-11", events:[]}
	h1 := elem(t, history, 1)
	if v := fieldVal(t, h1, "date"); v.Str != "2024-01-11" {
		t.Errorf("history[1].date: want 2024-01-11, got %q", v.Str)
	}
	if listLen(t, fieldVal(t, h1, "events")) != 0 {
		t.Errorf("history[1].events: want empty list")
	}

	// ---- Row 1: Bob ----
	if tbl.GetAt(1, nameIdx).Str != "Bob" {
		t.Errorf("row 1 name: want Bob, got %q", tbl.GetAt(1, nameIdx).Str)
	}

	bobOrders := tbl.GetAt(1, ordersIdx)
	if listLen(t, bobOrders) != 1 {
		t.Errorf("Bob orders len: want 1, got %d", listLen(t, bobOrders))
	}
	if v := fieldVal(t, elem(t, bobOrders, 0), "order_id"); v.Int != 201 {
		t.Errorf("Bob orders[0].order_id: want 201, got %d", v.Int)
	}

	bobProfile := tbl.GetAt(1, profileIdx)
	bobStats := fieldVal(t, bobProfile, "stats")
	if v := fieldVal(t, bobStats, "logins"); v.Int != 7 {
		t.Errorf("Bob stats.logins: want 7, got %d", v.Int)
	}
	bobHistory := fieldVal(t, bobProfile, "history")
	if listLen(t, bobHistory) != 1 {
		t.Errorf("Bob history len: want 1, got %d", listLen(t, bobHistory))
	}
	bobH0Events := fieldVal(t, elem(t, bobHistory, 0), "events")
	if listLen(t, bobH0Events) != 1 {
		t.Errorf("Bob history[0].events len: want 1, got %d", listLen(t, bobH0Events))
	}
	if elem(t, bobH0Events, 0).Str != "login" {
		t.Errorf("Bob history[0].events[0]: want login, got %q", elem(t, bobH0Events, 0).Str)
	}

	// ---- Row 2: Charlie ----
	if tbl.GetAt(2, nameIdx).Str != "Charlie" {
		t.Errorf("row 2 name: want Charlie, got %q", tbl.GetAt(2, nameIdx).Str)
	}

	// orders: empty
	if listLen(t, tbl.GetAt(2, ordersIdx)) != 0 {
		t.Errorf("Charlie orders: want empty list, got len %d", listLen(t, tbl.GetAt(2, ordersIdx)))
	}

	// tags: ["moderator", "user", "beta"]
	charlieTags := tbl.GetAt(2, tagsIdx)
	if listLen(t, charlieTags) != 3 {
		t.Errorf("Charlie tags len: want 3, got %d", listLen(t, charlieTags))
	}
	if elem(t, charlieTags, 0).Str != "moderator" {
		t.Errorf("Charlie tags[0]: want moderator, got %q", elem(t, charlieTags, 0).Str)
	}

	// profile.history: empty
	charlieProfile := tbl.GetAt(2, profileIdx)
	charlieHistory := fieldVal(t, charlieProfile, "history")
	if listLen(t, charlieHistory) != 0 {
		t.Errorf("Charlie history: want empty list, got len %d", listLen(t, charlieHistory))
	}

	// profile.stats.logins: 0
	charlieStats := fieldVal(t, charlieProfile, "stats")
	if v := fieldVal(t, charlieStats, "logins"); v.Int != 0 {
		t.Errorf("Charlie stats.logins: want 0, got %d", v.Int)
	}
}

func TestLoadNestedJSON(t *testing.T) {
	tbl, err := Load(testdataDir+"/nested.json", Options{})
	if err != nil {
		t.Fatal(err)
	}
	checkNestedTable(t, tbl)
}

func TestLoadNestedJSONL(t *testing.T) {
	tbl, err := Load(testdataDir+"/nested.jsonl", Options{})
	if err != nil {
		t.Fatal(err)
	}
	checkNestedTable(t, tbl)
}

func TestLoadNestedAvro(t *testing.T) {
	tbl, err := Load(testdataDir+"/nested.avro", Options{})
	if err != nil {
		t.Fatal(err)
	}
	checkNestedTable(t, tbl)
}

func TestLoadNestedParquet(t *testing.T) {
	tbl, err := Load(testdataDir+"/nested.parquet", Options{})
	if err != nil {
		t.Fatal(err)
	}
	checkNestedTable(t, tbl)
}

func TestAvroSchemaNameNamespacedRecord(t *testing.T) {
	schema := `{
	  "type":"record","name":"Row","namespace":"com.example",
	  "fields":[
	    {"name":"v","type":["null","string",{"type":"record","name":"Inner","fields":[{"name":"x","type":"long"}]}]}
	  ]}`
	var schemaDef struct {
		Namespace string `json:"namespace"`
		Fields    []struct {
			Type any `json:"type"`
		} `json:"fields"`
	}
	if err := json.Unmarshal([]byte(schema), &schemaDef); err != nil {
		t.Fatal(err)
	}
	branches, ok := asSlice(schemaDef.Fields[0].Type)
	if !ok {
		t.Fatalf("union type is %T, want slice", schemaDef.Fields[0].Type)
	}
	if got := avroSchemaName(branches[2], schemaDef.Namespace); got != "com.example.Inner" {
		t.Fatalf("record branch name: want com.example.Inner, got %q", got)
	}
	v := map[string]any{"com.example.Inner": map[string]any{"x": int64(7)}}
	got := avroValue(v, schemaDef.Fields[0].Type, schemaDef.Namespace)
	if got.Type != table.TypeRecord {
		t.Fatalf("avroValue: want record, got %v (%s)", got.Type, got.AsString())
	}
}

func TestLoadAvroNamespacedUnionRecord(t *testing.T) {
	schema := `{
	  "type":"record","name":"Row","namespace":"com.example",
	  "fields":[
	    {"name":"v","type":["null","string",{"type":"record","name":"Inner","fields":[{"name":"x","type":"long"}]}]}
	  ]}`
	writeAvro := func(t *testing.T, rows []map[string]any) string {
		t.Helper()
		var buf bytes.Buffer
		w, err := goavro.NewOCFWriter(goavro.OCFConfig{W: &buf, Schema: schema})
		if err != nil {
			t.Fatal(err)
		}
		if err := w.Append(rows); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "namespaced.avro")
		if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}

	path := writeAvro(t, []map[string]any{
		{"v": goavro.Union("com.example.Inner", map[string]any{"x": int64(7)})},
	})
	tbl, err := Load(path, Options{Format: "avro"})
	if err != nil {
		t.Fatal(err)
	}
	inner := tbl.Get(0, "v")
	if inner.Type != table.TypeRecord {
		t.Fatalf("record branch: want record, got %v", inner.Type)
	}
	if got := fieldVal(t, inner, "x").Int; got != 7 {
		t.Fatalf("record branch v.x: want 7, got %d", got)
	}

	path = writeAvro(t, []map[string]any{
		{"v": goavro.Union("string", "hello")},
	})
	tbl, err = Load(path, Options{Format: "avro"})
	if err != nil {
		t.Fatal(err)
	}
	if got := tbl.Get(0, "v").Str; got != "hello" {
		t.Fatalf("string branch: want hello, got %q", got)
	}
}

func TestAvroValueSchemaBranchShapes(t *testing.T) {
	if got := avroValue(map[string]any{"string": "wrapped"}, "string", ""); got.Type != table.TypeString || got.Str != "wrapped" {
		t.Fatalf("wrapped primitive branch: want wrapped string, got %v", got)
	}

	if got := avroValue("fallback", []any{"null", "string"}, ""); got.Type != table.TypeString || got.Str != "fallback" {
		t.Fatalf("union fallback branch: want fallback string, got %v", got)
	}
	if got := avroValue("ignored", []any{"null"}, ""); !got.IsNull() {
		t.Fatalf("null-only union: want null, got %v", got)
	}

	arraySchema := map[string]any{"type": "array", "items": "long"}
	got := avroValue([]any{int64(1), int64(2)}, arraySchema, "")
	if got.Type != table.TypeList || len(got.List) != 2 || got.List[0].Int != 1 || got.List[1].Int != 2 {
		t.Fatalf("array schema: want [1,2], got %v", got)
	}

	nestedSliceType := map[string]any{"type": []any{"null", "long"}}
	if got := avroValue(int64(7), nestedSliceType, ""); got.Type != table.TypeInt || got.Int != 7 {
		t.Fatalf("nested slice type: want 7, got %v", got)
	}

	nestedMapType := map[string]any{"type": map[string]any{"type": "array", "items": "string"}}
	got = avroValue([]any{"a"}, nestedMapType, "")
	if got.Type != table.TypeList || len(got.List) != 1 || got.List[0].Str != "a" {
		t.Fatalf("nested map type: want [a], got %v", got)
	}

	recordSchema := map[string]any{
		"type": "record",
		"name": "Row",
		"fields": []any{
			"not-a-field",
			map[string]any{"type": "long"},
			map[string]any{"name": "b", "type": "string"},
			map[string]any{"name": "a", "type": "long"},
		},
	}
	got = avroValue(map[string]any{"a": int64(1), "b": "x"}, recordSchema, "")
	if got.Type != table.TypeRecord || len(got.Fields) != 2 {
		t.Fatalf("record schema: want two fields, got %v", got)
	}
	if got.Fields[0].Name != "a" || got.Fields[0].Value.Int != 1 || got.Fields[1].Name != "b" || got.Fields[1].Value.Str != "x" {
		t.Fatalf("record fields should be sorted and decoded, got %v", got)
	}
}

func TestAnyToValueAdditionalTypes(t *testing.T) {
	if got := anyToValue(float64(1.25)); got.Type != table.TypeFloat || got.Float != 1.25 {
		t.Fatalf("float64: want 1.25, got %v", got)
	}
	if got := anyToValue(float32(2.5)); got.Type != table.TypeFloat || got.Float != 2.5 {
		t.Fatalf("float32: want 2.5, got %v", got)
	}
	if got := anyToValue([]byte("bytes")); got.Type != table.TypeString || got.Str != "bytes" {
		t.Fatalf("[]byte: want bytes, got %v", got)
	}
	if got := anyToValue([]interface{}{float64(1), "x"}); got.Type != table.TypeList || len(got.List) != 2 || got.List[0].Int != 1 || got.List[1].Str != "x" {
		t.Fatalf("slice: want [1,x], got %v", got)
	}
	if got := anyToValue(map[string]interface{}{"element": int64(9)}); got.Type != table.TypeInt || got.Int != 9 {
		t.Fatalf("parquet element wrapper: want 9, got %v", got)
	}
	if got := anyToValue(struct{ X int }{X: 1}); got.Type != table.TypeString || got.Str != `{"X":1}` {
		t.Fatalf("fallback JSON value: want object string, got %v", got)
	}
}
