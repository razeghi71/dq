package loader

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/razeghi71/dq/table"
)

func TestSourceProjectionTDDCSVPreparedProjectionMaterializesRequestedOrder(t *testing.T) {
	path := writeSourceProjectionTDDFile(t, "wide.csv", "id,name,status,unused\n1,Alice,active,100\n2,Bob,paused,200\n")

	tbl, err := loadPreparedSourceProjectionTDD(t, path, Options{}, "status", "id")
	if err != nil {
		t.Fatalf("load projected csv: %v", err)
	}

	requireSourceProjectionTDDColumns(t, tbl, "status", "id")
	if tbl.NumRows != 2 {
		t.Fatalf("row count: got %d, want 2", tbl.NumRows)
	}
	if got := tbl.GetAt(0, 0); got.Type != table.TypeString || got.Str != "active" {
		t.Fatalf("row 0 status: got %v, want active", got)
	}
	if got := tbl.GetAt(0, 1); got.Type != table.TypeInt || got.Int != 1 {
		t.Fatalf("row 0 id: got %v, want int 1", got)
	}
}

func TestSourceProjectionTDDCSVPreparedProjectionCanPreserveStringIdentifiers(t *testing.T) {
	path := writeSourceProjectionTDDFile(t, "ids.csv", "zip,name,unused\n007,Alice,x\n010,Bob,y\n")

	tbl, err := loadPreparedSourceProjectionTDD(t, path, Options{InferRows: 0, InferRowsSet: true}, "zip")
	if err != nil {
		t.Fatalf("load projected csv with infer_rows=0: %v", err)
	}

	requireSourceProjectionTDDColumns(t, tbl, "zip")
	if got := tbl.GetAt(0, 0); got.Type != table.TypeString || got.Str != "007" {
		t.Fatalf("row 0 zip: got %v, want string 007", got)
	}
}

func TestSourceProjectionTDDCSVPreparedProjectionHeaderlessUsesSyntheticNames(t *testing.T) {
	path := writeSourceProjectionTDDFile(t, "headerless.dat", "1,Alice,active\n2,Bob,paused\n")
	header := false

	tbl, err := loadPreparedSourceProjectionTDD(t, path, Options{Format: "csv", Header: &header}, "col3", "col1")
	if err != nil {
		t.Fatalf("load projected headerless csv: %v", err)
	}

	requireSourceProjectionTDDColumns(t, tbl, "col3", "col1")
	if got := tbl.GetAt(1, 0); got.Type != table.TypeString || got.Str != "paused" {
		t.Fatalf("row 1 col3: got %v, want paused", got)
	}
	if got := tbl.GetAt(1, 1); got.Type != table.TypeInt || got.Int != 2 {
		t.Fatalf("row 1 col1: got %v, want int 2", got)
	}
}

func TestSourceProjectionTDDCSVPreparedProjectionReportsMissingColumns(t *testing.T) {
	path := writeSourceProjectionTDDFile(t, "users.csv", "id,name\n1,Alice\n")

	_, err := loadPreparedSourceProjectionTDD(t, path, Options{}, "missing")
	if err == nil {
		t.Fatal("expected missing projected column error")
	}
	for _, want := range []string{"missing", "column"} {
		if !strings.Contains(strings.ToLower(err.Error()), want) {
			t.Fatalf("error %q missing %q", err, want)
		}
	}
}

func TestSourceProjectionTDDCSVPreparedProjectionDoesNotHideDuplicateHeaders(t *testing.T) {
	path := writeSourceProjectionTDDFile(t, "dupe.csv", "id,id,status\n1,2,active\n")

	_, err := loadPreparedSourceProjectionTDD(t, path, Options{}, "status")
	if err == nil {
		t.Fatal("expected duplicate header error")
	}
	for _, want := range []string{"duplicate", "id"} {
		if !strings.Contains(strings.ToLower(err.Error()), want) {
			t.Fatalf("error %q missing %q", err, want)
		}
	}
}

func TestSourceProjectionTDDCSVPreparedProjectionDoesNotHideRowWidthErrors(t *testing.T) {
	path := writeSourceProjectionTDDFile(t, "bad-width.csv", "id,status,unused\n1,active,ok\n2,paused,extra,bad\n")

	_, err := loadPreparedSourceProjectionTDD(t, path, Options{}, "id")
	if err == nil {
		t.Fatal("expected row-width error from unselected physical cells")
	}
	for _, want := range []string{"row", "field"} {
		if !strings.Contains(strings.ToLower(err.Error()), want) {
			t.Fatalf("error %q missing %q", err, want)
		}
	}
}

func TestSourceProjectionTDDCSVPreparedProjectionSkipsUnreferencedTypeBadRecords(t *testing.T) {
	path := writeSourceProjectionTDDFile(t, "bad-type.csv", "id,unused\n1,10\n2,bad\n")

	tbl, err := loadPreparedSourceProjectionTDD(t, path, Options{InferRows: 1, InferRowsSet: true}, "id")
	if err != nil {
		t.Fatalf("projected load should skip unreferenced type bad record: %v", err)
	}
	requireSourceProjectionTDDColumns(t, tbl, "id")
	if tbl.NumRows != 2 {
		t.Fatalf("row count: got %d, want 2", tbl.NumRows)
	}
	if got := tbl.GetAt(1, 0); got.Type != table.TypeInt || got.Int != 2 {
		t.Fatalf("row 1 id: got %v, want int 2", got)
	}
}

func TestSourceProjectionTDDCSVPreparedProjectionValidatesReferencedTypeBadRecords(t *testing.T) {
	path := writeSourceProjectionTDDFile(t, "bad-type.csv", "id,unused\n1,10\n2,bad\n")

	_, err := loadPreparedSourceProjectionTDD(t, path, Options{InferRows: 1, InferRowsSet: true}, "unused")
	if err == nil {
		t.Fatal("expected referenced-column type bad-record error")
	}
	for _, want := range []string{"unused", "int", "bad"} {
		if !strings.Contains(strings.ToLower(err.Error()), want) {
			t.Fatalf("error %q missing %q", err, want)
		}
	}
}

func TestSourceProjectionTDDPreparedLoadSpecFiltersDuringSourceLoad(t *testing.T) {
	path := writeSourceProjectionTDDFile(t, "users.csv", "id,status,unused\n1,active,100\n2,paused,200\n3,active,300\n")

	prepared, err := Prepare(path, Options{})
	if err != nil {
		t.Fatalf("prepare csv: %v", err)
	}
	tbl, err := prepared.LoadSpec(SourceLoadSpec{
		ReadColumns:   table.SelectedColumns("id", "status"),
		OutputColumns: table.SelectedColumns("id"),
		Predicate: func(row []table.Value) (bool, error) {
			if len(row) != 2 {
				t.Fatalf("predicate row: got %d values, want 2", len(row))
			}
			return row[1].Type == table.TypeString && row[1].Str == "active", nil
		},
	})
	if err != nil {
		t.Fatalf("load prepared csv with predicate: %v", err)
	}

	requireSourceProjectionTDDColumns(t, tbl, "id")
	if tbl.NumRows != 2 {
		t.Fatalf("row count: got %d, want 2", tbl.NumRows)
	}
	if got := tbl.GetAt(0, 0); got.Type != table.TypeInt || got.Int != 1 {
		t.Fatalf("row 0 id: got %v, want int 1", got)
	}
	if got := tbl.GetAt(1, 0); got.Type != table.TypeInt || got.Int != 3 {
		t.Fatalf("row 1 id: got %v, want int 3", got)
	}
}

func TestSourceProjectionTDDPreparedLoadSpecValidatesPredicateReadColumns(t *testing.T) {
	path := writeSourceProjectionTDDFile(t, "bad-predicate.csv", "id,flag\n1,10\n2,bad\n")

	prepared, err := Prepare(path, Options{InferRows: 1, InferRowsSet: true})
	if err != nil {
		t.Fatalf("prepare csv: %v", err)
	}
	_, err = prepared.LoadSpec(SourceLoadSpec{
		ReadColumns:   table.SelectedColumns("id", "flag"),
		OutputColumns: table.SelectedColumns("id"),
		Predicate: func(row []table.Value) (bool, error) {
			return row[1].Type == table.TypeInt && row[1].Int == 10, nil
		},
	})
	if err == nil {
		t.Fatal("expected predicate read-column bad-record error")
	}
	for _, want := range []string{"flag", "int", "bad"} {
		if !strings.Contains(strings.ToLower(err.Error()), want) {
			t.Fatalf("error %q missing %q", err, want)
		}
	}
}

func TestSourceProjectionTDDPreparedLoadSpecValidatesOutputColumnsBeforePredicateDrop(t *testing.T) {
	path := writeSourceProjectionTDDFile(t, "bad-output.csv", "id,amount\n1,10\n2,bad\n")

	prepared, err := Prepare(path, Options{InferRows: 1, InferRowsSet: true})
	if err != nil {
		t.Fatalf("prepare csv: %v", err)
	}
	_, err = prepared.LoadSpec(SourceLoadSpec{
		ReadColumns:   table.SelectedColumns("amount", "id"),
		OutputColumns: table.SelectedColumns("amount"),
		Predicate: func(row []table.Value) (bool, error) {
			return row[1].Type == table.TypeInt && row[1].Int == 1, nil
		},
	})
	if err == nil {
		t.Fatal("expected output-column bad-record error before predicate drop")
	}
	for _, want := range []string{"amount", "int", "bad"} {
		if !strings.Contains(strings.ToLower(err.Error()), want) {
			t.Fatalf("error %q missing %q", err, want)
		}
	}
}

func TestSourceProjectionTDDCSVPreparedProjectionWorksWithTextCompressionWrappers(t *testing.T) {
	cases := []struct {
		name string
		data []byte
	}{
		{"wide.csv.gz", gzipTestBytes(t, "id,name,status\n1,Alice,active\n2,Bob,paused\n")},
		{"wide.csv.zst", zstdTestBytes(t, "id,name,status\n1,Alice,active\n2,Bob,paused\n")},
		{"wide.csv.deflate", deflateTestBytes(t, "id,name,status\n1,Alice,active\n2,Bob,paused\n")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSourceProjectionTDDBinaryFile(t, tc.name, tc.data)
			tbl, err := loadPreparedSourceProjectionTDD(t, path, Options{}, "status", "id")
			if err != nil {
				t.Fatalf("load projected compressed csv: %v", err)
			}
			requireSourceProjectionTDDColumns(t, tbl, "status", "id")
		})
	}
}

func loadPreparedSourceProjectionTDD(t *testing.T, path string, opts Options, columns ...string) (*table.Table, error) {
	t.Helper()
	prepared, err := Prepare(path, opts)
	if err != nil {
		return nil, err
	}
	defer prepared.Close()
	return prepared.Load(columns)
}

func writeSourceProjectionTDDFile(t *testing.T, name, content string) string {
	t.Helper()
	return writeSourceProjectionTDDBinaryFile(t, name, []byte(content))
}

func writeSourceProjectionTDDBinaryFile(t *testing.T, name string, content []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func requireSourceProjectionTDDColumns(t *testing.T, tbl *table.Table, want ...string) {
	t.Helper()
	if tbl == nil {
		t.Fatal("nil table")
	}
	if len(tbl.Columns) != len(want) {
		t.Fatalf("columns: got %v, want %v", tbl.Columns, want)
	}
	for i := range want {
		if tbl.Columns[i] != want[i] {
			t.Fatalf("columns: got %v, want %v", tbl.Columns, want)
		}
	}
}

func requireSourceProjectionTDDSchema(t *testing.T, schema table.Schema, want ...string) {
	t.Helper()
	if len(schema.Columns) != len(want) {
		t.Fatalf("schema: got %#v, want %v", schema.Columns, want)
	}
	for i, col := range schema.Columns {
		got := col.Name + ":" + table.Render(col.Type)
		if got != want[i] {
			t.Fatalf("schema column %d: got %s, want %s; schema=%#v", i, got, want[i], schema.Columns)
		}
	}
}
