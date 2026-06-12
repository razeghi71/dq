package engine

import (
	"testing"

	"github.com/razeghi71/dq/table"
)

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
