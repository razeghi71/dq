package table

import (
	"strings"
	"testing"
)

func requireColumnOrder(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("columns: got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("columns: got %v, want %v", got, want)
		}
	}
}

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

func TestListToTableHappyPathAndErrors(t *testing.T) {
	tbl, err := ListToTable(ListVal([]Value{
		RecordVal([]RecordField{{Name: "id", Value: IntVal(1)}, {Name: "name", Value: StrVal("a")}}),
		RecordVal([]RecordField{{Name: "id", Value: IntVal(2)}}),
	}))
	if err != nil {
		t.Fatalf("ListToTable returned error: %v", err)
	}
	requireColumnOrder(t, tbl.Columns, []string{"id", "name"})
	if tbl.NumRows != 2 {
		t.Fatalf("rows: got %d, want 2", tbl.NumRows)
	}
	if got := tbl.Get(1, "name"); !got.IsNull() {
		t.Fatalf("missing record field: got %v, want null", got)
	}

	empty, err := ListToTable(ListVal(nil))
	if err != nil {
		t.Fatalf("empty ListToTable returned error: %v", err)
	}
	if empty.NumRows != 0 || len(empty.Columns) != 0 {
		t.Fatalf("empty table: got %d rows and columns %v", empty.NumRows, empty.Columns)
	}

	for _, tc := range []struct {
		name string
		v    Value
	}{
		{name: "not list", v: IntVal(1)},
		{name: "first element not record", v: ListVal([]Value{IntVal(1)})},
		{name: "later element not record", v: ListVal([]Value{
			RecordVal([]RecordField{{Name: "id", Value: IntVal(1)}}),
			StrVal("bad"),
		})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ListToTable(tc.v); err == nil {
				t.Fatal("expected ListToTable error")
			}
		})
	}
}

func TestListToTableWithSchemaPreservesUnionBranchValues(t *testing.T) {
	unionSchema := &TypeDescriptor{Kind: TypeUnion, Branches: []*TypeDescriptor{
		{Kind: TypeInt},
		{Kind: TypeString},
	}}
	recordSchema := &TypeDescriptor{Kind: TypeRecord, Fields: []FieldDescriptor{
		{Name: "k", Type: &TypeDescriptor{Kind: TypeString}},
		{Name: "u", Type: unionSchema},
	}}
	tbl, err := ListToTableWithSchema(ListVal([]Value{
		RecordVal([]RecordField{{Name: "k", Value: StrVal("a")}, {Name: "u", Value: IntVal(7)}}),
		RecordVal([]RecordField{{Name: "k", Value: StrVal("a")}, {Name: "u", Value: StrVal("seven")}}),
	}), recordSchema)
	if err != nil {
		t.Fatalf("ListToTableWithSchema returned error: %v", err)
	}
	requireColumnOrder(t, tbl.Columns, []string{"k", "u"})
	requireSchemaString(t, tbl.Col(tbl.ColIndex("u")).Schema(), "union<int,string>")
	if got := tbl.Get(0, "u"); got.Type != TypeInt || got.Int != 7 {
		t.Fatalf("first union value: got %s, want int 7", got.AsString())
	}
	if got := tbl.Get(1, "u"); got.Type != TypeString || got.Str != "seven" {
		t.Fatalf("second union value: got %s, want string seven", got.AsString())
	}

	empty, err := ListToTableWithSchema(ListVal(nil), recordSchema)
	if err != nil {
		t.Fatalf("empty ListToTableWithSchema returned error: %v", err)
	}
	requireColumnOrder(t, empty.Columns, []string{"k", "u"})
	requireSchemaString(t, empty.Col(empty.ColIndex("u")).Schema(), "union<int,string>")
}

func TestListToTableWithSchemaRejectsDuplicateRecordFieldsBeforeExtraction(t *testing.T) {
	cases := []struct {
		name   string
		schema *TypeDescriptor
		value  Value
		want   string
	}{
		{
			name:   "known_field",
			schema: recordOf(field("x", td(TypeInt))),
			value: ListVal([]Value{
				RecordVal([]RecordField{{Name: "x", Value: IntVal(1)}, {Name: "x", Value: IntVal(2)}}),
			}),
			want: "x duplicate record field",
		},
		{
			name:   "unknown_field_that_would_be_dropped",
			schema: recordOf(field("x", td(TypeInt))),
			value: ListVal([]Value{
				RecordVal([]RecordField{{Name: "y", Value: IntVal(1)}, {Name: "y", Value: IntVal(2)}}),
			}),
			want: "y duplicate record field",
		},
		{
			name:   "nested_record_field",
			schema: recordOf(field("payload", recordOf(field("x", td(TypeInt))))),
			value: ListVal([]Value{
				RecordVal([]RecordField{{Name: "payload", Value: RecordVal([]RecordField{
					{Name: "x", Value: IntVal(1)},
					{Name: "x", Value: IntVal(2)},
				})}}),
			}),
			want: "payload.x duplicate record field",
		},
		{
			name:   "later_list_element",
			schema: recordOf(field("x", td(TypeInt))),
			value: ListVal([]Value{
				RecordVal([]RecordField{{Name: "x", Value: IntVal(1)}}),
				RecordVal([]RecordField{{Name: "x", Value: IntVal(2)}, {Name: "x", Value: IntVal(3)}}),
			}),
			want: "x duplicate record field",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ListToTableWithSchema(tc.value, tc.schema)
			if err == nil {
				t.Fatal("expected duplicate record field error")
			}
			if got := err.Error(); got != tc.want {
				t.Fatalf("error: got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestNewTableWithTypesPreservesDeclaredTypes(t *testing.T) {
	tbl := NewTableWithTypes([]string{"id", "amount", "note", "active"}, []ValueType{
		TypeInt,
		TypeFloat,
		TypeString,
		TypeBool,
	})
	tbl.AddRow([]Value{Null(), Null(), Null(), Null()})

	for i, want := range []ValueType{TypeInt, TypeFloat, TypeString, TypeBool} {
		if got := tbl.Col(i).ColType(); got != want {
			t.Fatalf("%s column type: got %v, want %v", tbl.Columns[i], got, want)
		}
		if !tbl.GetAt(0, i).IsNull() {
			t.Fatalf("%s row 0: want null, got %v", tbl.Columns[i], tbl.GetAt(0, i))
		}
	}
}

func TestNewTableWithTypesDefaultsMissingTypesToNull(t *testing.T) {
	tbl := NewTableWithTypes([]string{"known", "unknown"}, []ValueType{TypeString})

	if got := tbl.Col(0).ColType(); got != TypeString {
		t.Fatalf("known column type: got %v, want string", got)
	}
	if got := tbl.Col(1).ColType(); got != TypeNull {
		t.Fatalf("unknown column type: got %v, want null", got)
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

func TestSliceRowsCopiesAllColumnTypesAndSchemas(t *testing.T) {
	tbl := mixedColumnTable()

	sliced := tbl.SliceRows(0, 2)
	if sliced.NumRows != 2 {
		t.Fatalf("rows: got %d, want 2", sliced.NumRows)
	}
	assertMixedColumnRow(t, sliced, 0, 1, "a")
	for col := range sliced.Columns {
		if !sliced.GetAt(1, col).IsNull() {
			t.Fatalf("%s row 1: got %v, want null", sliced.Columns[col], sliced.GetAt(1, col))
		}
	}
	for i, name := range tbl.Columns {
		if sliced.Col(i) == tbl.Col(i) {
			t.Fatalf("%s column storage was shared", name)
		}
		if got, want := sliced.Col(i).Schema().String(), tbl.Col(i).Schema().String(); got != want {
			t.Fatalf("%s schema: got %s, want %s", name, got, want)
		}
	}

	clamped := tbl.SliceRows(-10, 100)
	if clamped.NumRows != tbl.NumRows {
		t.Fatalf("clamped rows: got %d, want %d", clamped.NumRows, tbl.NumRows)
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

func TestApplyPermutationCopiesAllColumnTypesAndSchemas(t *testing.T) {
	tbl := mixedColumnTable()

	permuted := tbl.ApplyPermutation([]int{2, 0, 1})
	if permuted.NumRows != 3 {
		t.Fatalf("rows: got %d, want 3", permuted.NumRows)
	}
	assertMixedColumnRow(t, permuted, 0, 2, "c")
	assertMixedColumnRow(t, permuted, 1, 1, "a")
	for col := range permuted.Columns {
		if !permuted.GetAt(2, col).IsNull() {
			t.Fatalf("%s row 2: got %v, want null", permuted.Columns[col], permuted.GetAt(2, col))
		}
	}
	for i, name := range tbl.Columns {
		if permuted.Col(i) == tbl.Col(i) {
			t.Fatalf("%s column storage was shared", name)
		}
		if got, want := permuted.Col(i).Schema().String(), tbl.Col(i).Schema().String(); got != want {
			t.Fatalf("%s schema: got %s, want %s", name, got, want)
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

func TestAppendTypedComputedColumnsSharesExistingAndValidatesNewColumns(t *testing.T) {
	tbl := NewTable([]string{"id", "amount"})
	tbl.AddRow([]Value{IntVal(1), IntVal(10)})
	tbl.AddRow([]Value{IntVal(2), IntVal(3)})

	got, err := tbl.AppendTypedComputedColumns(
		[]string{"total"},
		[]*TypeDescriptor{ScalarSchema(TypeFloat)},
		[][]Value{{IntVal(20), FloatVal(6.5)}},
	)
	if err != nil {
		t.Fatalf("AppendTypedComputedColumns returned error: %v", err)
	}
	requireColumnOrder(t, got.Columns, []string{"id", "amount", "total"})
	if got.NumRows != tbl.NumRows {
		t.Fatalf("row count: got %d, want %d", got.NumRows, tbl.NumRows)
	}
	if got.Col(0) != tbl.Col(0) || got.Col(1) != tbl.Col(1) {
		t.Fatal("existing columns should be shared")
	}
	if got.Col(2) == nil || got.Col(2) == tbl.Col(0) || got.Col(2) == tbl.Col(1) {
		t.Fatal("computed column should have separate storage")
	}
	if got.Col(2).ColType() != TypeFloat {
		t.Fatalf("computed column type: got %v, want float", got.Col(2).ColType())
	}
	if v := got.Get(0, "total"); v.Type != TypeFloat || v.Float != 20 {
		t.Fatalf("row 0 total: got %v", v)
	}
	if v := got.Get(1, "total"); v.Type != TypeFloat || v.Float != 6.5 {
		t.Fatalf("row 1 total: got %v", v)
	}
}

func TestAppendTypedComputedColumnsRejectsInvalidComputedValue(t *testing.T) {
	tbl := NewTable([]string{"id"})
	tbl.AddRow([]Value{IntVal(1)})

	_, err := tbl.AppendTypedComputedColumns(
		[]string{"bad"},
		[]*TypeDescriptor{ScalarSchema(TypeInt)},
		[][]Value{{StrVal("oops")}},
	)
	if err == nil {
		t.Fatal("expected invalid computed value error")
	}
	if got, want := err.Error(), "bad expected int, got string"; got != want {
		t.Fatalf("error: got %q, want %q", got, want)
	}
}

func TestTableCloneCopiesColumnData(t *testing.T) {
	tbl := NewTable([]string{"name", "age"})
	tbl.AddRow([]Value{StrVal("Alice"), IntVal(30)})
	tbl.AddRow([]Value{StrVal("Bob"), Null()})

	clone := tbl.Clone()
	if clone == tbl {
		t.Fatal("Clone returned the original table")
	}
	if clone.Col(0) == tbl.Col(0) || clone.Col(1) == tbl.Col(1) {
		t.Fatal("Clone should copy column storage")
	}
	if got := clone.Col(0).ColName(); got != "name" {
		t.Fatalf("cloned column name: want name, got %q", got)
	}
	if got := clone.GetAt(0, 0).Str; got != "Alice" {
		t.Fatalf("clone row 0 name: want Alice, got %q", got)
	}
	if !clone.GetAt(1, 1).IsNull() {
		t.Fatalf("clone row 1 age: want null, got %v", clone.GetAt(1, 1))
	}

	tbl.Col(0).Append(StrVal("Carol"))
	if clone.Col(0).Len() != 2 {
		t.Fatalf("clone should not grow after source mutation, got %d rows", clone.Col(0).Len())
	}
}

func TestTableCloneCopiesAllColumnTypes(t *testing.T) {
	tbl := NewTable([]string{"i", "f", "s", "b", "list", "record"})
	tbl.AddRow([]Value{
		IntVal(1),
		FloatVal(1.5),
		StrVal("x"),
		BoolVal(true),
		ListVal([]Value{IntVal(2)}),
		RecordVal([]RecordField{{Name: "n", Value: IntVal(3)}}),
	})
	tbl.AddRow([]Value{Null(), Null(), Null(), Null(), Null(), Null()})

	clone := tbl.Clone()
	for i, col := range tbl.Columns {
		if clone.Col(i) == tbl.Col(i) {
			t.Fatalf("%s column storage was shared", col)
		}
		if got, want := clone.Col(i).ColType(), tbl.Col(i).ColType(); got != want {
			t.Fatalf("%s column type: want %v, got %v", col, want, got)
		}
		if !Equal(clone.GetAt(0, i), tbl.GetAt(0, i)) {
			t.Fatalf("%s row 0: clone value mismatch", col)
		}
		if !clone.GetAt(1, i).IsNull() {
			t.Fatalf("%s row 1: want null, got %v", col, clone.GetAt(1, i))
		}
	}
}

func TestTableStringIncludesRowsAndEmptyShape(t *testing.T) {
	empty := NewTable([]string{"name", "age"})
	if got, want := empty.String(), "[name, age] (0 rows)"; got != want {
		t.Fatalf("empty String(): want %q, got %q", want, got)
	}

	tbl := NewTable([]string{"name", "age"})
	tbl.AddRow([]Value{StrVal("Alice"), IntVal(30)})
	tbl.AddRow([]Value{StrVal("Bob"), Null()})

	got := tbl.String()
	for _, want := range []string{"name:Alice", "age:30", "name:Bob", "age:null"} {
		if !strings.Contains(got, want) {
			t.Fatalf("String() = %q, missing %q", got, want)
		}
	}
}

func TestTableInvalidAccessReturnsNullOrNil(t *testing.T) {
	tbl := NewTable([]string{"x"})
	tbl.AddRow([]Value{IntVal(1)})

	if tbl.Col(-1) != nil || tbl.Col(1) != nil {
		t.Fatal("out-of-range Col should return nil")
	}
	for _, got := range []Value{
		tbl.GetAt(-1, 0),
		tbl.GetAt(0, -1),
		tbl.GetAt(1, 0),
		tbl.GetAt(0, 1),
	} {
		if !got.IsNull() {
			t.Fatalf("out-of-range GetAt should return null, got %v", got)
		}
	}
}

func TestValueAsBoolVariants(t *testing.T) {
	cases := []struct {
		name string
		v    Value
		want bool
		ok   bool
	}{
		{name: "true", v: BoolVal(true), want: true, ok: true},
		{name: "false", v: BoolVal(false), want: false, ok: true},
		{name: "null", v: Null(), want: false, ok: true},
		{name: "string", v: StrVal("true"), want: false, ok: false},
	}

	for _, tc := range cases {
		got, ok := tc.v.AsBool()
		if got != tc.want || ok != tc.ok {
			t.Fatalf("%s: want (%v, %v), got (%v, %v)", tc.name, tc.want, tc.ok, got, ok)
		}
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

func TestUnionColumnsKeepsAnchorAndSortsLaterExtras(t *testing.T) {
	got := UnionColumns(
		[]string{"id", "name"},
		[]string{"id", "name", "zebra", "apple"},
	)

	requireColumnOrder(t, got, []string{"id", "name", "apple", "zebra"})
}

func TestUnionColumnsSortsAndDeduplicatesExtrasAcrossLaterSets(t *testing.T) {
	got := UnionColumns(
		[]string{"id", "name"},
		[]string{"zebra", "apple", "id"},
		[]string{"banana", "apple", "aardvark", "name"},
	)

	requireColumnOrder(t, got, []string{"id", "name", "aardvark", "apple", "banana", "zebra"})
}

func TestUnionColumnsDoesNotMutateInputs(t *testing.T) {
	anchor := []string{"id", "name"}
	later := []string{"zebra", "apple"}

	got := UnionColumns(anchor, later)

	requireColumnOrder(t, got, []string{"id", "name", "apple", "zebra"})
	requireColumnOrder(t, anchor, []string{"id", "name"})
	requireColumnOrder(t, later, []string{"zebra", "apple"})
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

func TestConcatPreservesZeroRowSchemas(t *testing.T) {
	a := NewTableWithSchemas([]string{"id"}, []*TypeDescriptor{{Kind: TypeInt}})
	b := NewTableWithSchemas([]string{"id"}, []*TypeDescriptor{{Kind: TypeInt}})

	got, err := Concat([]*Table{a, b})
	if err != nil {
		t.Fatal(err)
	}
	if got.NumRows != 0 {
		t.Fatalf("expected 0 rows, got %d", got.NumRows)
	}
	if schema := got.Col(0).Schema().String(); schema != "int" {
		t.Fatalf("schema: got %s, want int", schema)
	}
	if typ := TypeName(got.Col(0).ColType()); typ != "int" {
		t.Fatalf("type: got %s, want int", typ)
	}
}

func TestConcatPreservesUnionSchemaAndBranchIdentity(t *testing.T) {
	union := &TypeDescriptor{Kind: TypeUnion, Branches: []*TypeDescriptor{
		{Kind: TypeInt},
		{Kind: TypeString},
	}}
	empty := NewTableWithSchemas([]string{"u"}, []*TypeDescriptor{union})
	rows := NewTableWithSchemas([]string{"u"}, []*TypeDescriptor{union})
	rows.AddRow([]Value{IntVal(7)})
	rows.AddRow([]Value{StrVal("7")})

	got, err := Concat([]*Table{empty, rows})
	if err != nil {
		t.Fatal(err)
	}
	if schema := got.Col(0).Schema().String(); schema != "union<int,string>" {
		t.Fatalf("schema: got %s, want union<int,string>", schema)
	}
	if typ := TypeName(got.Col(0).ColType()); typ != "union" {
		t.Fatalf("type: got %s, want union", typ)
	}
	if got.Get(0, "u").Type != TypeInt || got.Get(1, "u").Type != TypeString {
		t.Fatalf("branch values were not preserved: %v / %v", got.Get(0, "u"), got.Get(1, "u"))
	}
	if CanonicalKey(got.Get(0, "u")) == CanonicalKey(got.Get(1, "u")) {
		t.Fatalf("int and string union branch values should have distinct keys")
	}
}

func TestAddRowKnownUnionSchemaMarksNullRowsNullable(t *testing.T) {
	union := &TypeDescriptor{Kind: TypeUnion, Branches: []*TypeDescriptor{
		{Kind: TypeInt},
		{Kind: TypeString},
	}}
	tbl := NewTableWithSchemas([]string{"u"}, []*TypeDescriptor{union})

	tbl.AddRow([]Value{Null()})
	requireSchemaString(t, tbl.Col(0).Schema(), "union<int,string>?")
	if got := tbl.Get(0, "u"); !got.IsNull() {
		t.Fatalf("row 0: got %v, want null", got)
	}

	tbl.AddRow([]Value{IntVal(7)})
	requireSchemaString(t, tbl.Col(0).Schema(), "union<int,string>?")
	if got := tbl.Get(1, "u"); got.Type != TypeInt || got.Int != 7 {
		t.Fatalf("row 1: got %v, want int 7", got)
	}
}

func TestAddRowKnownNestedUnionSchemaMarksNullFieldNullable(t *testing.T) {
	union := &TypeDescriptor{Kind: TypeUnion, Branches: []*TypeDescriptor{
		{Kind: TypeInt},
		{Kind: TypeString},
	}}
	schema := recordOf(
		field("kind", td(TypeString)),
		field("u", union),
	)
	tbl := NewTableWithSchemas([]string{"payload"}, []*TypeDescriptor{schema})

	tbl.AddRow([]Value{RecordVal([]RecordField{
		{Name: "kind", Value: StrVal("empty")},
		{Name: "u", Value: Null()},
	})})

	requireSchemaString(t, tbl.Col(0).Schema(), "record<kind:string, u:union<int,string>?>")
	payload := recordValuesForTableTest(tbl.Get(0, "payload"))
	if got := payload["u"]; !got.IsNull() {
		t.Fatalf("payload.u: got %v, want null", got)
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

type triOutcome int

const (
	outFalse triOutcome = iota
	outTrue
	outNull
)

func boolOrNull(b bool, isNull bool) Value {
	if isNull {
		return Null()
	}
	return BoolVal(b)
}

func assertTriOutcome(t *testing.T, v Value, want triOutcome) {
	t.Helper()
	switch want {
	case outNull:
		if !v.IsNull() {
			t.Errorf("expected null, got %v", v.AsString())
		}
	case outTrue:
		if v.Type != TypeBool || !v.Bool {
			t.Errorf("expected true, got %v", v.AsString())
		}
	case outFalse:
		if v.Type != TypeBool || v.Bool {
			t.Errorf("expected false, got %v", v.AsString())
		}
	}
}

func TestEvalTruthAnd(t *testing.T) {
	cases := []struct {
		left, right triOutcome
		want        triOutcome
	}{
		{outTrue, outTrue, outTrue},
		{outTrue, outFalse, outFalse},
		{outTrue, outNull, outNull},
		{outFalse, outTrue, outFalse},
		{outFalse, outFalse, outFalse},
		{outFalse, outNull, outFalse},
		{outNull, outTrue, outNull},
		{outNull, outFalse, outFalse},
		{outNull, outNull, outNull},
	}
	for _, tc := range cases {
		left := boolOrNull(tc.left == outTrue, tc.left == outNull)
		right := boolOrNull(tc.right == outTrue, tc.right == outNull)
		got, err := EvalTruthAnd(left, right)
		if err != nil {
			t.Fatalf("EvalTruthAnd: %v", err)
		}
		assertTriOutcome(t, got, tc.want)
	}
}

func TestEvalTruthOr(t *testing.T) {
	cases := []struct {
		left, right triOutcome
		want        triOutcome
	}{
		{outTrue, outTrue, outTrue},
		{outTrue, outFalse, outTrue},
		{outTrue, outNull, outTrue},
		{outFalse, outTrue, outTrue},
		{outFalse, outFalse, outFalse},
		{outFalse, outNull, outNull},
		{outNull, outTrue, outTrue},
		{outNull, outFalse, outNull},
		{outNull, outNull, outNull},
	}
	for _, tc := range cases {
		left := boolOrNull(tc.left == outTrue, tc.left == outNull)
		right := boolOrNull(tc.right == outTrue, tc.right == outNull)
		got, err := EvalTruthOr(left, right)
		if err != nil {
			t.Fatalf("EvalTruthOr: %v", err)
		}
		assertTriOutcome(t, got, tc.want)
	}
}

func TestEvalTruthNot(t *testing.T) {
	for _, tc := range []struct {
		in   triOutcome
		want triOutcome
	}{
		{outTrue, outFalse},
		{outFalse, outTrue},
		{outNull, outNull},
	} {
		in := boolOrNull(tc.in == outTrue, tc.in == outNull)
		got, err := EvalTruthNot(in)
		if err != nil {
			t.Fatalf("EvalTruthNot: %v", err)
		}
		assertTriOutcome(t, got, tc.want)
	}
}

func TestIsExplicitTrueAndIsBoolOrNull(t *testing.T) {
	if !BoolVal(true).IsExplicitTrue() {
		t.Error("true should be explicit true")
	}
	if BoolVal(false).IsExplicitTrue() || Null().IsExplicitTrue() {
		t.Error("false and null should not be explicit true")
	}
	if !BoolVal(true).IsBoolOrNull() || !BoolVal(false).IsBoolOrNull() || !Null().IsBoolOrNull() {
		t.Error("bool and null should be valid logical operands")
	}
	if IntVal(1).IsBoolOrNull() || StrVal("x").IsBoolOrNull() {
		t.Error("non-bool/non-null should not be valid logical operands")
	}
}

func mixedColumnTable() *Table {
	tbl := NewTable([]string{"i", "f", "s", "b", "list", "record"})
	tbl.AddRow([]Value{
		IntVal(1),
		FloatVal(1.5),
		StrVal("a"),
		BoolVal(true),
		ListVal([]Value{IntVal(1), StrVal("a")}),
		RecordVal([]RecordField{{Name: "id", Value: IntVal(1)}, {Name: "label", Value: StrVal("a")}}),
	})
	tbl.AddRow([]Value{Null(), Null(), Null(), Null(), Null(), Null()})
	tbl.AddRow([]Value{
		IntVal(2),
		FloatVal(2.5),
		StrVal("c"),
		BoolVal(false),
		ListVal([]Value{IntVal(2), StrVal("c")}),
		RecordVal([]RecordField{{Name: "id", Value: IntVal(2)}, {Name: "label", Value: StrVal("c")}}),
	})
	return tbl
}

func assertMixedColumnRow(t *testing.T, tbl *Table, row int, id int64, label string) {
	t.Helper()
	if got := tbl.Get(row, "i"); got.Type != TypeInt || got.Int != id {
		t.Fatalf("row %d i: got %v, want int %d", row, got, id)
	}
	if got := tbl.Get(row, "f"); got.Type != TypeFloat || got.Float != float64(id)+0.5 {
		t.Fatalf("row %d f: got %v, want float %.1f", row, got, float64(id)+0.5)
	}
	if got := tbl.Get(row, "s"); got.Type != TypeString || got.Str != label {
		t.Fatalf("row %d s: got %v, want string %q", row, got, label)
	}
	if got := tbl.Get(row, "b"); got.Type != TypeBool || got.Bool != (id == 1) {
		t.Fatalf("row %d b: got %v", row, got)
	}
	list := tbl.Get(row, "list")
	if list.Type != TypeList || len(list.List) != 2 || list.List[0].Int != id || list.List[1].Str != label {
		t.Fatalf("row %d list: got %v", row, list)
	}
	record := recordValuesForTableTest(tbl.Get(row, "record"))
	if record["id"].Type != TypeInt || record["id"].Int != id || record["label"].Type != TypeString || record["label"].Str != label {
		t.Fatalf("row %d record: got %v", row, tbl.Get(row, "record"))
	}
}

func recordValuesForTableTest(v Value) map[string]Value {
	out := make(map[string]Value, len(v.Fields))
	for _, field := range v.Fields {
		out[field.Name] = field.Value
	}
	return out
}

func TestEvalTruthNonBooleanErrors(t *testing.T) {
	intVal := IntVal(1)
	if _, err := EvalTruthAnd(intVal, BoolVal(true)); err == nil {
		t.Fatal("expected error for non-boolean and operand")
	}
	if _, err := EvalTruthOr(BoolVal(false), intVal); err == nil {
		t.Fatal("expected error for non-boolean or operand")
	}
	if _, err := EvalTruthNot(intVal); err == nil {
		t.Fatal("expected error for non-boolean not operand")
	}
}
