package table

import (
	"testing"
)

func TestColumnWidenIntToFloat(t *testing.T) {
	c := &Column{name: "val", typ: TypeNull}
	c.Append(IntVal(1))
	c.Append(FloatVal(2.5))
	c.Append(IntVal(3))

	if c.ColType() != TypeFloat {
		t.Fatalf("expected TypeFloat, got %v", c.ColType())
	}
	if c.Len() != 3 {
		t.Fatalf("expected 3 rows, got %d", c.Len())
	}

	want := []float64{1, 2.5, 3}
	for i, w := range want {
		got := c.Get(i)
		if got.Type != TypeFloat {
			t.Errorf("row %d: expected TypeFloat, got %v", i, got.Type)
		}
		if got.Float != w {
			t.Errorf("row %d: want %g, got %g", i, w, got.Float)
		}
	}
}

func TestColumnWidenIntFloatToString(t *testing.T) {
	c := &Column{name: "val", typ: TypeNull}
	c.Append(IntVal(1))
	c.Append(FloatVal(2.5))
	c.Append(StrVal("something"))

	if c.ColType() != TypeString {
		t.Fatalf("expected TypeString, got %v", c.ColType())
	}

	want := []string{"1", "2.5", "something"}
	for i, w := range want {
		got := c.Get(i)
		if got.Type != TypeString {
			t.Errorf("row %d: expected TypeString, got %v", i, got.Type)
		}
		if got.Str != w {
			t.Errorf("row %d: want %q, got %q", i, w, got.Str)
		}
	}
}

func TestColumnNulls(t *testing.T) {
	c := &Column{name: "x", typ: TypeNull}
	c.Append(IntVal(10))
	c.Append(Null())
	c.Append(IntVal(20))

	if c.Len() != 3 {
		t.Fatalf("expected 3 rows, got %d", c.Len())
	}
	if !c.Get(1).IsNull() {
		t.Errorf("row 1: expected null, got %v", c.Get(1).AsString())
	}
	if c.Get(0).Int != 10 || c.Get(2).Int != 20 {
		t.Errorf("non-null rows not preserved: %v, %v", c.Get(0), c.Get(2))
	}
}

func TestTableAddRowAndGetAt(t *testing.T) {
	tbl := NewTable([]string{"name", "age"})
	tbl.AddRow([]Value{StrVal("Alice"), IntVal(30)})
	tbl.AddRow([]Value{StrVal("Bob"), IntVal(25)})

	if tbl.NumRows != 2 {
		t.Fatalf("expected 2 rows, got %d", tbl.NumRows)
	}
	if tbl.Col(0).ColType() != TypeString {
		t.Errorf("name column: want TypeString, got %v", tbl.Col(0).ColType())
	}
	if tbl.Col(1).ColType() != TypeInt {
		t.Errorf("age column: want TypeInt, got %v", tbl.Col(1).ColType())
	}
	if tbl.GetAt(0, 0).Str != "Alice" || tbl.GetAt(1, 1).Int != 25 {
		t.Errorf("unexpected values: %v %v", tbl.GetAt(0, 0), tbl.GetAt(1, 1))
	}
}

func TestSliceRows(t *testing.T) {
	tbl := NewTable([]string{"n"})
	for i := int64(1); i <= 5; i++ {
		tbl.AddRow([]Value{IntVal(i)})
	}

	sliced := tbl.SliceRows(1, 4)
	if sliced.NumRows != 3 {
		t.Fatalf("expected 3 rows, got %d", sliced.NumRows)
	}
	for i, want := range []int64{2, 3, 4} {
		if sliced.GetAt(i, 0).Int != want {
			t.Errorf("row %d: want %d, got %d", i, want, sliced.GetAt(i, 0).Int)
		}
	}

	// slice is independent of source
	tbl.Col(0).Append(IntVal(99))
	if sliced.NumRows != 3 {
		t.Errorf("slice should not grow with source append")
	}
}

func TestApplyPermutation(t *testing.T) {
	tbl := NewTable([]string{"n"})
	for i := int64(1); i <= 3; i++ {
		tbl.AddRow([]Value{IntVal(i)})
	}

	permuted := tbl.ApplyPermutation([]int{2, 0, 1})
	want := []int64{3, 1, 2}
	for i, w := range want {
		if permuted.GetAt(i, 0).Int != w {
			t.Errorf("row %d: want %d, got %d", i, w, permuted.GetAt(i, 0).Int)
		}
	}
}

func TestSelectColsSharesData(t *testing.T) {
	tbl := NewTable([]string{"a", "b", "c"})
	tbl.AddRow([]Value{IntVal(1), IntVal(2), IntVal(3)})

	sub := tbl.SelectCols([]int{0, 2}, []string{"a", "c"})
	if sub.NumRows != 1 || len(sub.Columns) != 2 {
		t.Fatalf("unexpected shape: %d rows, %d cols", sub.NumRows, len(sub.Columns))
	}
	if sub.Col(0) != tbl.Col(0) || sub.Col(1) != tbl.Col(2) {
		t.Error("SelectCols should share column pointers")
	}
}

func TestShallowCloneRenamesOnly(t *testing.T) {
	tbl := NewTable([]string{"old"})
	tbl.AddRow([]Value{StrVal("x")})

	renamed := tbl.ShallowClone([]string{"new"})
	if renamed.Columns[0] != "new" {
		t.Errorf("expected column name 'new', got %q", renamed.Columns[0])
	}
	if renamed.Col(0) != tbl.Col(0) {
		t.Error("ShallowClone should share column data")
	}
}

func TestConcatIdenticalSchema(t *testing.T) {
	a := NewTable([]string{"id", "name"})
	a.AddRow([]Value{IntVal(1), StrVal("Alice")})
	b := NewTable([]string{"id", "name"})
	b.AddRow([]Value{IntVal(2), StrVal("Bob")})

	got, err := Concat([]*Table{a, b})
	if err != nil {
		t.Fatal(err)
	}
	if got.NumRows != 2 {
		t.Fatalf("expected 2 rows, got %d", got.NumRows)
	}
	if got.Get(0, "name").Str != "Alice" || got.Get(1, "name").Str != "Bob" {
		t.Errorf("unexpected rows: %s", got.String())
	}
}

func TestConcatExtraColumnsSorted(t *testing.T) {
	a := NewTable([]string{"id", "name"})
	a.AddRow([]Value{IntVal(1), StrVal("Alice")})
	b := NewTable([]string{"id", "email"})
	b.AddRow([]Value{IntVal(2), StrVal("bob@x.com")})

	got, err := Concat([]*Table{a, b})
	if err != nil {
		t.Fatal(err)
	}
	if got.Columns[0] != "id" || got.Columns[1] != "name" || got.Columns[2] != "email" {
		t.Fatalf("columns: %v", got.Columns)
	}
	if got.Get(0, "email").Type != TypeNull {
		t.Errorf("row 0 email should be null")
	}
	if got.Get(1, "name").Type != TypeNull {
		t.Errorf("row 1 name should be null")
	}
}

func TestConcatTypeWidening(t *testing.T) {
	a := NewTable([]string{"v"})
	a.AddRow([]Value{IntVal(1)})
	b := NewTable([]string{"v"})
	b.AddRow([]Value{FloatVal(2.5)})

	got, err := Concat([]*Table{a, b})
	if err != nil {
		t.Fatal(err)
	}
	if got.Col(0).ColType() != TypeFloat {
		t.Fatalf("expected float column, got %v", got.Col(0).ColType())
	}
}

func TestConcatEmpty(t *testing.T) {
	_, err := Concat(nil)
	if err == nil || err.Error() != "concat: no tables" {
		t.Fatalf("expected concat: no tables error, got %v", err)
	}
}

func TestAppendIntToStringColumn(t *testing.T) {
	tbl := NewTable([]string{"id"})
	tbl.AddRow([]Value{StrVal("name")})
	tbl.AddRow([]Value{IntVal(2)})
	if tbl.Get(1, "id").AsString() != "2" {
		t.Fatalf("expected stringified int, got %v", tbl.Get(1, "id"))
	}
}
