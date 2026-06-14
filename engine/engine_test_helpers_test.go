package engine

import (
	"testing"

	"github.com/razeghi71/dq/table"
)

type describeMeta struct {
	typ  string
	rows int64
}

// typedValuesTable is a single-row table with one value of each scalar and container type.
func typedValuesTable() *table.Table {
	tbl := table.NewTable([]string{"s", "xs", "n", "price", "rec", "flag", "nilcol"})
	tbl.AddRow([]table.Value{
		table.StrVal("hello"),
		table.ListVal([]table.Value{table.IntVal(1), table.IntVal(2), table.IntVal(3)}),
		table.IntVal(42),
		table.FloatVal(12.5),
		table.RecordVal([]table.RecordField{{Name: "a", Value: table.IntVal(1)}}),
		table.BoolVal(true),
		table.Null(),
	})
	return tbl
}

func assertIntColByName(t *testing.T, result *table.Table, nameCol, intCol string, want map[string]int64) {
	t.Helper()
	nameIdx := result.ColIndex(nameCol)
	intIdx := result.ColIndex(intCol)
	if nameIdx < 0 || intIdx < 0 {
		t.Fatalf("columns not found in %v", result.Columns)
	}
	got := make(map[string]int64, result.NumRows)
	for i := 0; i < result.NumRows; i++ {
		got[result.GetAt(i, nameIdx).Str] = result.GetAt(i, intIdx).Int
	}
	if len(got) != len(want) {
		t.Fatalf("expected %d rows, got %d: %v", len(want), len(got), got)
	}
	for name, w := range want {
		if got[name] != w {
			t.Errorf("%s: want %d, got %d", name, w, got[name])
		}
	}
}

func assertDescribeRows(t *testing.T, result *table.Table, want map[string]describeMeta) {
	t.Helper()
	if len(result.Columns) != 3 ||
		result.Columns[0] != "column" ||
		result.Columns[1] != "type" ||
		result.Columns[2] != "row_count" {
		t.Fatalf("describe columns: got %v, want [column type row_count]", result.Columns)
	}
	if result.NumRows != len(want) {
		t.Fatalf("describe row count: got %d, want %d; table=%s", result.NumRows, len(want), result.String())
	}

	got := make(map[string]describeMeta, result.NumRows)
	for i := 0; i < result.NumRows; i++ {
		col := result.GetAt(i, 0)
		typ := result.GetAt(i, 1)
		rows := result.GetAt(i, 2)
		if col.Type != table.TypeString || typ.Type != table.TypeString || rows.Type != table.TypeInt {
			t.Fatalf("describe row %d has wrong value types: column=%v type=%v row_count=%v", i, col.Type, typ.Type, rows.Type)
		}
		got[col.Str] = describeMeta{typ: typ.Str, rows: rows.Int}
	}
	for name, w := range want {
		g, ok := got[name]
		if !ok {
			t.Errorf("describe missing column %q; got %v", name, got)
			continue
		}
		if g != w {
			t.Errorf("describe %q: got %+v, want %+v", name, g, w)
		}
	}
	for name := range got {
		if _, ok := want[name]; !ok {
			t.Errorf("describe unexpected column %q; got %v", name, got)
		}
	}
}

func assertNameSet(t *testing.T, result *table.Table, nameCol string, want ...string) {
	t.Helper()
	nameIdx := result.ColIndex(nameCol)
	if nameIdx < 0 {
		t.Fatalf("column %q not found", nameCol)
	}
	got := make(map[string]bool, result.NumRows)
	for i := 0; i < result.NumRows; i++ {
		got[result.GetAt(i, nameIdx).Str] = true
	}
	if len(got) != len(want) {
		t.Fatalf("expected %d names %v, got %d: %v", len(want), want, len(got), got)
	}
	for _, name := range want {
		if !got[name] {
			t.Errorf("expected name %q in result", name)
		}
	}
}
