package engine

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/razeghi71/dq/ast"
	"github.com/razeghi71/dq/parser"
	"github.com/razeghi71/dq/table"
)

func usersTable() *table.Table {
	t := table.NewTable([]string{"name", "age", "city"})
	t.AddRow([]table.Value{table.StrVal("Alice"), table.IntVal(30), table.StrVal("NY")})
	t.AddRow([]table.Value{table.StrVal("Bob"), table.IntVal(25), table.StrVal("LA")})
	t.AddRow([]table.Value{table.StrVal("Charlie"), table.IntVal(35), table.StrVal("NY")})
	t.AddRow([]table.Value{table.StrVal("Diana"), table.IntVal(28), table.StrVal("SF")})
	t.AddRow([]table.Value{table.StrVal("Eve"), table.IntVal(22), table.StrVal("LA")})
	t.AddRow([]table.Value{table.StrVal("Frank"), table.IntVal(40), table.StrVal("NY")})
	return t
}

func runQuery(t *testing.T, input *table.Table, query string) *table.Table {
	t.Helper()
	q, err := parser.Parse("test.csv | " + query)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	result, err := Execute(q, input, nil)
	if err != nil {
		t.Fatalf("exec error: %v", err)
	}
	return result
}

func recordValuesForTest(t *testing.T, v table.Value) map[string]table.Value {
	t.Helper()
	if v.Type != table.TypeRecord {
		t.Fatalf("expected TypeRecord, got %v (%s)", v.Type, v.AsString())
	}
	out := make(map[string]table.Value, len(v.Fields))
	for _, f := range v.Fields {
		out[f.Name] = f.Value
	}
	return out
}

func TestHead(t *testing.T) {
	result := runQuery(t, usersTable(), "head 3")
	if result.NumRows != 3 {
		t.Errorf("expected 3 rows, got %d", result.NumRows)
	}
	if result.GetAt(0, 0).Str != "Alice" {
		t.Errorf("expected first row to be Alice")
	}
}

func TestTail(t *testing.T) {
	result := runQuery(t, usersTable(), "tail 2")
	if result.NumRows != 2 {
		t.Errorf("expected 2 rows, got %d", result.NumRows)
	}
	if result.GetAt(0, 0).Str != "Eve" {
		t.Errorf("expected first row to be Eve, got %s", result.GetAt(0, 0).Str)
	}
}

func TestSortAsc(t *testing.T) {
	result := runQuery(t, usersTable(), "sort age")
	if result.GetAt(0, 1).Int != 22 {
		t.Errorf("expected first age to be 22, got %d", result.GetAt(0, 1).Int)
	}
	if result.GetAt(5, 1).Int != 40 {
		t.Errorf("expected last age to be 40, got %d", result.GetAt(5, 1).Int)
	}
}

func TestSortDesc(t *testing.T) {
	result := runQuery(t, usersTable(), "sort -age")
	if result.GetAt(0, 1).Int != 40 {
		t.Errorf("expected first age to be 40, got %d", result.GetAt(0, 1).Int)
	}
}

func TestSortMixedDirections(t *testing.T) {
	// city ascending, age descending within each city.
	result := runQuery(t, usersTable(), "sort city, -age")
	got := make([][2]any, result.NumRows)
	for i := 0; i < result.NumRows; i++ {
		got[i] = [2]any{result.GetAt(i, 2).Str, result.GetAt(i, 1).Int}
	}
	want := [][2]any{
		{"LA", int64(25)}, // Bob
		{"LA", int64(22)}, // Eve
		{"NY", int64(40)}, // Frank
		{"NY", int64(35)}, // Charlie
		{"NY", int64(30)}, // Alice
		{"SF", int64(28)}, // Diana
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d: expected %v, got %v", i, want[i], got[i])
		}
	}
}

func TestSortNullsLastInAscendingAndDescending(t *testing.T) {
	tbl := table.NewTable([]string{"id", "a"})
	tbl.AddRow([]table.Value{table.StrVal("two"), table.IntVal(2)})
	tbl.AddRow([]table.Value{table.StrVal("null"), table.Null()})
	tbl.AddRow([]table.Value{table.StrVal("one"), table.IntVal(1)})

	cases := []struct {
		name  string
		query string
		want  []string
	}{
		{name: "ascending", query: "sort a", want: []string{"one", "two", "null"}},
		{name: "descending", query: "sort -a", want: []string{"two", "one", "null"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := runQuery(t, tbl, tc.query)
			if result.NumRows != len(tc.want) {
				t.Fatalf("row count: got %d, want %d", result.NumRows, len(tc.want))
			}
			for i, want := range tc.want {
				if got := result.GetAt(i, result.ColIndex("id")).Str; got != want {
					t.Fatalf("row %d id: got %q, want %q; table=%s", i, got, want, result.String())
				}
			}
		})
	}
}

func TestSelect(t *testing.T) {
	result := runQuery(t, usersTable(), "select name, city")
	if len(result.Columns) != 2 {
		t.Fatalf("expected 2 columns, got %d", len(result.Columns))
	}
	if result.Columns[0] != "name" || result.Columns[1] != "city" {
		t.Errorf("unexpected columns: %v", result.Columns)
	}
}

func TestFilter(t *testing.T) {
	result := runQuery(t, usersTable(), `filter { age > 30 }`)
	if result.NumRows != 2 {
		t.Errorf("expected 2 rows (Charlie, Frank), got %d", result.NumRows)
	}
}

func TestFilterAnd(t *testing.T) {
	result := runQuery(t, usersTable(), `filter { age > 25 and city == "NY" }`)
	if result.NumRows != 3 {
		t.Errorf("expected 3 rows, got %d", result.NumRows)
	}
}

func TestCount(t *testing.T) {
	result := runQuery(t, usersTable(), "count")
	if result.NumRows != 1 || len(result.Columns) != 1 {
		t.Fatal("count should return 1x1 table")
	}
	if result.GetAt(0, 0).Int != 6 {
		t.Errorf("expected 6, got %d", result.GetAt(0, 0).Int)
	}
}

func TestDescribeTypedValues(t *testing.T) {
	result := runQuery(t, typedValuesTable(), "describe")
	assertDescribeRows(t, result, map[string]describeMeta{
		"s":      {typ: "string", rows: 1},
		"xs":     {typ: "list", rows: 1},
		"n":      {typ: "int", rows: 1},
		"price":  {typ: "float", rows: 1},
		"rec":    {typ: "record", rows: 1},
		"flag":   {typ: "bool", rows: 1},
		"nilcol": {typ: "null", rows: 1},
	})

	wantOrder := []string{"s", "xs", "n", "price", "rec", "flag", "nilcol"}
	for i, want := range wantOrder {
		if got := result.GetAt(i, 0).Str; got != want {
			t.Errorf("describe row %d column order: got %q, want %q", i, got, want)
		}
	}
}

func TestDescribeSchemaForConstructedRecord(t *testing.T) {
	result := runQuery(t, usersTable(), `transform profile = struct(name = name, age = age, meta = struct(city = city)) | select profile | describe`)
	assertDescribeSchemaRows(t, result, map[string]describeSchemaMeta{
		"profile": {
			typ:    "record",
			rows:   6,
			schema: "record<age:int, meta:record<city:string>, name:string>",
		},
	})
}

func TestSelectTopLevelDuplicateColumns(t *testing.T) {
	result := runQuery(t, usersTable(), `select name, name, age`)
	wantCols := []string{"name", "name_2", "age"}
	for i, want := range wantCols {
		if result.Columns[i] != want {
			t.Fatalf("column %d: got %q, want %q", i, result.Columns[i], want)
		}
	}
	if got := result.GetAt(0, 0); got.Type != table.TypeString || got.Str != "Alice" {
		t.Fatalf("first name column: got %v", got)
	}
	if got := result.GetAt(0, 1); got.Type != table.TypeString || got.Str != "Alice" {
		t.Fatalf("duplicate name column: got %v", got)
	}
	if got := result.GetAt(0, 2); got.Type != table.TypeInt || got.Int != 30 {
		t.Fatalf("age column: got %v", got)
	}
}

func TestDescribeAfterFilterFalsePreservesInputTypes(t *testing.T) {
	result := runQuery(t, usersTable(), "filter { false } | describe")
	assertDescribeRows(t, result, map[string]describeMeta{
		"name": {typ: "string", rows: 0},
		"age":  {typ: "int", rows: 0},
		"city": {typ: "string", rows: 0},
	})
}

func TestDescribeEmptyAndZeroColumnTables(t *testing.T) {
	t.Run("empty_with_columns", func(t *testing.T) {
		result := runQuery(t, table.NewTable([]string{"name", "age"}), "describe")
		assertDescribeRows(t, result, map[string]describeMeta{
			"name": {typ: "null", rows: 0},
			"age":  {typ: "null", rows: 0},
		})
	})

	t.Run("zero_columns", func(t *testing.T) {
		result := runQuery(t, table.NewTable(nil), "describe")
		assertDescribeRows(t, result, map[string]describeMeta{})
	})
}

func TestDescribeAfterShapeChangingOps(t *testing.T) {
	t.Run("head", func(t *testing.T) {
		result := runQuery(t, usersTable(), "head 3 | describe")
		assertDescribeRows(t, result, map[string]describeMeta{
			"name": {typ: "string", rows: 3},
			"age":  {typ: "int", rows: 3},
			"city": {typ: "string", rows: 3},
		})
	})

	t.Run("tail", func(t *testing.T) {
		result := runQuery(t, usersTable(), "tail 2 | describe")
		assertDescribeRows(t, result, map[string]describeMeta{
			"name": {typ: "string", rows: 2},
			"age":  {typ: "int", rows: 2},
			"city": {typ: "string", rows: 2},
		})
	})

	t.Run("sort", func(t *testing.T) {
		result := runQuery(t, usersTable(), "sort -age | describe")
		assertDescribeRows(t, result, map[string]describeMeta{
			"name": {typ: "string", rows: 6},
			"age":  {typ: "int", rows: 6},
			"city": {typ: "string", rows: 6},
		})
	})

	t.Run("select", func(t *testing.T) {
		result := runQuery(t, usersTable(), "select name, age | describe")
		assertDescribeRows(t, result, map[string]describeMeta{
			"name": {typ: "string", rows: 6},
			"age":  {typ: "int", rows: 6},
		})
	})

	t.Run("remove", func(t *testing.T) {
		result := runQuery(t, usersTable(), "remove city | describe")
		assertDescribeRows(t, result, map[string]describeMeta{
			"name": {typ: "string", rows: 6},
			"age":  {typ: "int", rows: 6},
		})
	})

	t.Run("rename", func(t *testing.T) {
		result := runQuery(t, usersTable(), "rename name=first_name | describe")
		assertDescribeRows(t, result, map[string]describeMeta{
			"first_name": {typ: "string", rows: 6},
			"age":        {typ: "int", rows: 6},
			"city":       {typ: "string", rows: 6},
		})
	})

	t.Run("transform_new_and_overwritten", func(t *testing.T) {
		result := runQuery(t, usersTable(), `transform age = age / 2, missing = null, profile = struct(name = name), tags = list(city) | describe`)
		assertDescribeRows(t, result, map[string]describeMeta{
			"name":    {typ: "string", rows: 6},
			"age":     {typ: "float", rows: 6},
			"city":    {typ: "string", rows: 6},
			"missing": {typ: "string", rows: 6},
			"profile": {typ: "record", rows: 6},
			"tags":    {typ: "list", rows: 6},
		})
	})

	t.Run("group_custom_nested_name", func(t *testing.T) {
		result := runQuery(t, usersTable(), "group city as entries | describe")
		assertDescribeRows(t, result, map[string]describeMeta{
			"city":    {typ: "string", rows: 3},
			"entries": {typ: "list", rows: 3},
		})
	})

	t.Run("reduce", func(t *testing.T) {
		result := runQuery(t, usersTable(), "group city | reduce n = count(), total = sum(age) | remove grouped | describe")
		assertDescribeRows(t, result, map[string]describeMeta{
			"city":  {typ: "string", rows: 3},
			"n":     {typ: "int", rows: 3},
			"total": {typ: "int", rows: 3},
		})
	})

	t.Run("count", func(t *testing.T) {
		result := runQuery(t, usersTable(), "count | describe")
		assertDescribeRows(t, result, map[string]describeMeta{
			"count": {typ: "int", rows: 1},
		})
	})

	t.Run("distinct", func(t *testing.T) {
		result := runQuery(t, usersTable(), "distinct city | describe")
		assertDescribeRows(t, result, map[string]describeMeta{
			"city": {typ: "string", rows: 3},
		})
	})
}

func TestDescribeCanBeFilteredAndSelected(t *testing.T) {
	result := runQuery(t, typedValuesTable(), `describe | filter { type == "string" or type == "list" } | select column, type | sort column`)
	assertDescribeRows(t, runQuery(t, result, "describe"), map[string]describeMeta{
		"column": {typ: "string", rows: 2},
		"type":   {typ: "string", rows: 2},
	})
	assertNameSet(t, result, "column", "s", "xs")
}

func TestDescribeAfterJoin(t *testing.T) {
	result := runJoinQuery(t, usersTable(), `orders.csv on name == user_name | describe`)
	assertDescribeRows(t, result, map[string]describeMeta{
		"name":     {typ: "string", rows: 4},
		"age":      {typ: "int", rows: 4},
		"city":     {typ: "string", rows: 4},
		"order_id": {typ: "int", rows: 4},
		"product":  {typ: "string", rows: 4},
		"amount":   {typ: "int", rows: 4},
	})
}

func TestDistinct(t *testing.T) {
	result := runQuery(t, usersTable(), "distinct city")
	if len(result.Columns) != 1 || result.Columns[0] != "city" {
		t.Fatalf("expected only city column, got %v", result.Columns)
	}
	if result.NumRows != 3 {
		t.Errorf("expected 3 distinct cities, got %d", result.NumRows)
	}
}

func TestDistinctWithoutColumnsDeduplicatesFullRows(t *testing.T) {
	tbl := table.NewTable([]string{"name", "age"})
	tbl.AddRow([]table.Value{table.StrVal("Alice"), table.IntVal(30)})
	tbl.AddRow([]table.Value{table.StrVal("Alice"), table.IntVal(30)})
	tbl.AddRow([]table.Value{table.StrVal("Alice"), table.IntVal(31)})

	result := runQuery(t, tbl, "distinct | sort age")
	if result.NumRows != 2 {
		t.Fatalf("expected 2 full-row distinct records, got %d: %s", result.NumRows, result.String())
	}
	if got := result.GetAt(0, result.ColIndex("age")); got.Type != table.TypeInt || got.Int != 30 {
		t.Fatalf("first age: got %v, want 30", got)
	}
	if got := result.GetAt(1, result.ColIndex("age")); got.Type != table.TypeInt || got.Int != 31 {
		t.Fatalf("second age: got %v, want 31", got)
	}
}

func TestDistinctCommaColumnList(t *testing.T) {
	result := runQuery(t, usersTable(), "distinct city, age")
	if len(result.Columns) != 2 || result.Columns[0] != "city" || result.Columns[1] != "age" {
		t.Fatalf("expected city, age columns, got %v", result.Columns)
	}
	if result.NumRows != 6 {
		t.Errorf("expected 6 distinct city+age pairs, got %d", result.NumRows)
	}
}

func TestTransform(t *testing.T) {
	result := runQuery(t, usersTable(), "transform doubled = age * 2")
	if len(result.Columns) != 4 {
		t.Fatalf("expected 4 columns, got %d", len(result.Columns))
	}
	if result.Columns[3] != "doubled" {
		t.Errorf("expected column 'doubled', got %q", result.Columns[3])
	}
	// Alice: age=30, doubled=60
	if result.GetAt(0, 3).Int != 60 {
		t.Errorf("expected 60, got %d", result.GetAt(0, 3).Int)
	}
}

func TestAppendOnlyTypedTransformDoesNotAllocatePerRow(t *testing.T) {
	const rows = 2000
	tbl := table.NewTable([]string{"age"})
	for i := 0; i < rows; i++ {
		tbl.AddRow([]table.Value{table.IntVal(int64(i))})
	}
	q, err := parser.Parse("test.csv | transform age2 = age + 1 | select age2 | count")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	var execErr error
	allocs := testing.AllocsPerRun(20, func() {
		_, execErr = Execute(q, tbl, nil)
	})
	if execErr != nil {
		t.Fatalf("exec error: %v", execErr)
	}
	if allocs > 500 {
		t.Fatalf("append-only typed transform allocations scale with rows: got %.0f allocations for %d rows", allocs, rows)
	}
}

func TestTransformStructConstructsRecord(t *testing.T) {
	result := runQuery(t, usersTable(), `transform rec = struct(a = 1, name = name, missing = null, nested = struct(city = city))`)
	rec := result.Get(0, "rec")
	if rec.Type != table.TypeRecord {
		t.Fatalf("expected TypeRecord, got %v (%s)", rec.Type, rec.AsString())
	}
	fields := map[string]table.Value{}
	for _, f := range rec.Fields {
		fields[f.Name] = f.Value
	}
	if got := fields["a"]; got.Type != table.TypeInt || got.Int != 1 {
		t.Fatalf("rec.a: want int 1, got %v", got)
	}
	if got := fields["name"]; got.Type != table.TypeString || got.Str != "Alice" {
		t.Fatalf("rec.name: want Alice, got %v", got)
	}
	if got := fields["missing"]; got.Type != table.TypeNull {
		t.Fatalf("rec.missing: want null, got %v", got)
	}
	nested := fields["nested"]
	if nested.Type != table.TypeRecord {
		t.Fatalf("rec.nested: want record, got %v", nested)
	}
	nestedFields := map[string]table.Value{}
	for _, f := range nested.Fields {
		nestedFields[f.Name] = f.Value
	}
	if got := nestedFields["city"]; got.Type != table.TypeString || got.Str != "NY" {
		t.Fatalf("rec.nested.city: want NY, got %v", got)
	}
}

func TestTransformStructDotPathAccess(t *testing.T) {
	result := runQuery(t, usersTable(), `transform rec = struct(name = name, age = age) | select rec.name, rec.age`)
	assertColumns(t, result, []string{"rec_name", "rec_age"})
	if got := result.GetAt(0, 0); got.Type != table.TypeString || got.Str != "Alice" {
		t.Fatalf("rec.name: want Alice, got %v", got)
	}
	if got := result.GetAt(0, 1); got.Type != table.TypeInt || got.Int != 30 {
		t.Fatalf("rec.age: want 30, got %v", got)
	}
}

func TestTransformStructPreservesFieldOrder(t *testing.T) {
	result := runQuery(t, usersTable(), "transform rec = struct(z = 1, a = 2, m = 3)")
	rec := result.Get(0, "rec")
	if rec.Type != table.TypeRecord {
		t.Fatalf("expected TypeRecord, got %v", rec.Type)
	}
	if len(rec.Fields) != 3 {
		t.Fatalf("expected 3 fields, got %#v", rec.Fields)
	}
	want := []string{"z", "a", "m"}
	for i, name := range want {
		if rec.Fields[i].Name != name {
			t.Fatalf("field %d: want %q, got %q (full record %#v)", i, name, rec.Fields[i].Name, rec.Fields)
		}
	}
}

func TestTransformStructEmpty(t *testing.T) {
	result := runQuery(t, usersTable(), "transform rec = struct()")
	rec := result.Get(0, "rec")
	if rec.Type != table.TypeRecord {
		t.Fatalf("expected TypeRecord, got %v", rec.Type)
	}
	if len(rec.Fields) != 0 {
		t.Fatalf("expected empty record, got %#v", rec.Fields)
	}
}

func TestTransformListConstructsList(t *testing.T) {
	result := runQuery(t, usersTable(), `transform xs = list(1, null, name, upper(city))`)
	xs := result.Get(0, "xs")
	if xs.Type != table.TypeList {
		t.Fatalf("expected TypeList, got %v (%s)", xs.Type, xs.AsString())
	}
	if len(xs.List) != 4 {
		t.Fatalf("expected 4 elements, got %#v", xs.List)
	}
	if xs.List[0].Type != table.TypeInt || xs.List[0].Int != 1 {
		t.Fatalf("xs[0]: want int 1, got %v", xs.List[0])
	}
	if xs.List[1].Type != table.TypeNull {
		t.Fatalf("xs[1]: want null, got %v", xs.List[1])
	}
	if xs.List[2].Type != table.TypeString || xs.List[2].Str != "Alice" {
		t.Fatalf("xs[2]: want Alice, got %v", xs.List[2])
	}
	if xs.List[3].Type != table.TypeString || xs.List[3].Str != "NY" {
		t.Fatalf("xs[3]: want NY, got %v", xs.List[3])
	}
}

func TestTransformListEmpty(t *testing.T) {
	result := runQuery(t, usersTable(), `transform xs = list()`)
	xs := result.Get(0, "xs")
	if xs.Type != table.TypeList {
		t.Fatalf("expected TypeList, got %v (%s)", xs.Type, xs.AsString())
	}
	if len(xs.List) != 0 {
		t.Fatalf("expected empty list, got %#v", xs.List)
	}
}

func TestTransformListOfStructs(t *testing.T) {
	result := runQuery(t, usersTable(), `transform bundle = list(struct(name = name, age = age), struct(name = upper(name), age = age + 1))`)
	bundle := result.Get(0, "bundle")
	if bundle.Type != table.TypeList {
		t.Fatalf("expected TypeList, got %v (%s)", bundle.Type, bundle.AsString())
	}
	if len(bundle.List) != 2 {
		t.Fatalf("expected 2 records, got %#v", bundle.List)
	}
	for i, elem := range bundle.List {
		if elem.Type != table.TypeRecord {
			t.Fatalf("element %d: expected TypeRecord, got %v", i, elem)
		}
	}
	first := recordValuesForTest(t, bundle.List[0])
	if v := first["name"]; v.Type != table.TypeString || v.Str != "Alice" {
		t.Fatalf("first.name: want Alice, got %v", v)
	}
	second := recordValuesForTest(t, bundle.List[1])
	if v := second["name"]; v.Type != table.TypeString || v.Str != "ALICE" {
		t.Fatalf("second.name: want ALICE, got %v", v)
	}
	if v := second["age"]; v.Type != table.TypeInt || v.Int != 31 {
		t.Fatalf("second.age: want 31, got %v", v)
	}
}

func TestListConstructorWithListContainsUsesStrictElementTypes(t *testing.T) {
	result := runQuery(t, usersTable(), `filter { list_contains(list(1, "1"), 1) } | count`)
	if got := result.Get(0, "count"); got.Type != table.TypeInt || got.Int != int64(usersTable().NumRows) {
		t.Fatalf("expected all rows to match int element, got %v", got)
	}

	result = runQuery(t, usersTable(), `filter { list_contains(list(1, "1"), "1") } | count`)
	if got := result.Get(0, "count"); got.Type != table.TypeInt || got.Int != int64(usersTable().NumRows) {
		t.Fatalf("expected all rows to match string element, got %v", got)
	}

	expectQueryErrContains(t, usersTable(), `filter { list_contains(list(1), "1") } | count`, "list_contains() element type mismatch")
}

func TestExpressionNumericPromotionRuntimeMatchesPlanner(t *testing.T) {
	result := runQuery(t, usersTable(), `filter { age == 30.0 } | count`)
	if got := result.Get(0, "count"); got.Type != table.TypeInt || got.Int != 1 {
		t.Fatalf("age == 30.0: got %v, want count 1", got)
	}

	result = runQuery(t, usersTable(), `filter { age < 30.5 } | count`)
	if got := result.Get(0, "count"); got.Type != table.TypeInt || got.Int != 4 {
		t.Fatalf("age < 30.5: got %v, want count 4", got)
	}

	result = runQuery(t, usersTable(), `filter { list_contains(list(1), 1.0) } | count`)
	if got := result.Get(0, "count"); got.Type != table.TypeInt || got.Int != int64(usersTable().NumRows) {
		t.Fatalf("list_contains(list(1), 1.0): got %v, want all rows", got)
	}

	result = runQuery(t, usersTable(), `filter { list_contains(list(struct(x = 1)), struct(x = 1.0)) } | count`)
	if got := result.Get(0, "count"); got.Type != table.TypeInt || got.Int != int64(usersTable().NumRows) {
		t.Fatalf("record element numeric promotion: got %v, want all rows", got)
	}
}

func TestRecordEqualityRuntimeMatchesPlannedRecordSchema(t *testing.T) {
	wantAllRows := int64(usersTable().NumRows)

	result := runQuery(t, usersTable(), `filter { if(age == 30, struct(x = 1), struct(x = 1, y = null)) == struct(x = 1, y = null) } | count`)
	if got := result.Get(0, "count"); got.Type != table.TypeInt || got.Int != wantAllRows {
		t.Fatalf("inline record comparison: got %v, want count %d", got, wantAllRows)
	}

	result = runQuery(t, usersTable(), `transform r = if(age == 30, struct(x = 1), struct(x = 1, y = null)) | filter { r == struct(x = 1, y = null) } | count`)
	if got := result.Get(0, "count"); got.Type != table.TypeInt || got.Int != wantAllRows {
		t.Fatalf("staged record comparison: got %v, want count %d", got, wantAllRows)
	}

	result = runQuery(t, usersTable(), `filter { list_contains(list(struct(x = 1, y = null)), struct(x = 1)) } | count`)
	if got := result.Get(0, "count"); got.Type != table.TypeInt || got.Int != wantAllRows {
		t.Fatalf("list_contains record comparison: got %v, want count %d", got, wantAllRows)
	}

	result = runQuery(t, usersTable(), `filter { struct(x = 1) == struct(x = 1, y = 2) } | count`)
	if got := result.Get(0, "count"); got.Type != table.TypeInt || got.Int != 0 {
		t.Fatalf("missing non-null record field: got %v, want count 0", got)
	}
}

func TestRecordEqualityMissingFieldAdjacentExpressionForms(t *testing.T) {
	wantAllRows := int64(usersTable().NumRows)
	trueCases := []struct {
		name  string
		query string
	}{
		{
			name:  "symmetric_missing_null_field",
			query: `filter { struct(x = 1, y = null) == struct(x = 1) } | count`,
		},
		{
			name:  "empty_record_missing_only_null_fields",
			query: `filter { struct() == struct(y = null) } | count`,
		},
		{
			name:  "nested_record_missing_null_field",
			query: `filter { struct(n = struct(x = 1)) == struct(n = struct(x = 1, y = null)) } | count`,
		},
		{
			name:  "list_equality_recurses_into_records",
			query: `filter { list(struct(x = 1)) == list(struct(x = 1, y = null)) } | count`,
		},
		{
			name:  "list_contains_value_has_missing_null_field",
			query: `filter { list_contains(list(struct(x = 1)), struct(x = 1, y = null)) } | count`,
		},
		{
			name:  "coalesce_result_compares_with_unified_record_schema",
			query: `filter { coalesce(null, struct(x = 1), struct(x = 1, y = null)) == struct(x = 1, y = null) } | count`,
		},
		{
			name:  "staged_sibling_record_columns_compare_after_transform",
			query: `transform a = struct(x = 1), b = struct(x = 1, y = null) | filter { a == b } | count`,
		},
		{
			name:  "staged_if_record_compares_to_smaller_shape",
			query: `transform r = if(age == 30, struct(x = 1), struct(x = 1, y = null)) | filter { r == struct(x = 1) } | count`,
		},
	}
	for _, tc := range trueCases {
		t.Run(tc.name, func(t *testing.T) {
			result := runQuery(t, usersTable(), tc.query)
			if got := result.Get(0, "count"); got.Type != table.TypeInt || got.Int != wantAllRows {
				t.Fatalf("%s: got %v, want count %d", tc.query, got, wantAllRows)
			}
		})
	}

	falseCases := []struct {
		name  string
		query string
	}{
		{
			name:  "empty_record_missing_non_null_field",
			query: `filter { struct() == struct(y = 2) } | count`,
		},
		{
			name:  "nested_record_missing_non_null_field",
			query: `filter { struct(n = struct(x = 1)) == struct(n = struct(x = 1, y = "present")) } | count`,
		},
		{
			name:  "list_contains_missing_non_null_field",
			query: `filter { list_contains(list(struct(x = 1)), struct(x = 1, y = 2)) } | count`,
		},
		{
			name:  "list_length_mismatch_remains_false",
			query: `filter { list(struct(x = 1)) == list(struct(x = 1), struct(x = 1, y = null)) } | count`,
		},
	}
	for _, tc := range falseCases {
		t.Run(tc.name, func(t *testing.T) {
			result := runQuery(t, usersTable(), tc.query)
			if got := result.Get(0, "count"); got.Type != table.TypeInt || got.Int != 0 {
				t.Fatalf("%s: got %v, want count 0", tc.query, got)
			}
		})
	}

	errorCases := []struct {
		name  string
		query string
		want  string
	}{
		{
			name:  "common_field_incompatible_types_rejected",
			query: `filter { false } | filter { struct(x = 1) == struct(x = "1") } | count`,
			want:  "cannot compare record with record",
		},
		{
			name:  "list_contains_record_element_incompatible_field_rejected",
			query: `filter { false } | filter { list_contains(list(struct(x = 1)), struct(x = "1")) } | count`,
			want:  "list_contains() element type mismatch",
		},
	}
	for _, tc := range errorCases {
		t.Run(tc.name, func(t *testing.T) {
			expectQueryErrContains(t, usersTable(), tc.query, tc.want)
		})
	}
}

func TestExpressionIntegerComparisonRemainsExactForLargeValues(t *testing.T) {
	tbl := table.NewTable([]string{"id"})
	tbl.AddRow([]table.Value{table.IntVal(9007199254740992)})
	tbl.AddRow([]table.Value{table.IntVal(9007199254740993)})

	result := runQuery(t, tbl, `filter { id == 9007199254740993 } | count`)
	if got := result.Get(0, "count"); got.Type != table.TypeInt || got.Int != 1 {
		t.Fatalf("large int exact equality: got %v, want count 1", got)
	}

	result = runQuery(t, tbl, `filter { id < 9007199254740993 } | count`)
	if got := result.Get(0, "count"); got.Type != table.TypeInt || got.Int != 1 {
		t.Fatalf("large int exact ordering: got %v, want count 1", got)
	}
}

func TestExpressionMixedNumericComparisonRemainsExactForLargeValues(t *testing.T) {
	tbl := table.NewTable([]string{"id"})
	tbl.AddRow([]table.Value{table.IntVal(9007199254740993)})

	assertCount := func(query string, want int64) {
		t.Helper()
		result := runQuery(t, tbl, query)
		if got := result.Get(0, "count"); got.Type != table.TypeInt || got.Int != want {
			t.Fatalf("%s: got %v, want count %d", query, got, want)
		}
	}

	assertCount(`filter { id == 9007199254740992.0 } | count`, 0)
	assertCount(`filter { id != 9007199254740992.0 } | count`, 1)
	assertCount(`filter { id > 9007199254740992.0 } | count`, 1)
	assertCount(`filter { 9007199254740992.0 < id } | count`, 1)
	assertCount(`filter { id <= 9007199254740992.0 } | count`, 0)
	assertCount(`filter { list_contains(list(9007199254740993), 9007199254740992.0) } | count`, 0)
	assertCount(`filter { list_contains(list(9007199254740992.0), id) } | count`, 0)
	assertCount(`filter { struct(x = id) == struct(x = 9007199254740992.0) } | count`, 0)
	assertCount(`filter { struct(x = id) != struct(x = 9007199254740992.0) } | count`, 1)
	assertCount(`filter { list(struct(x = id)) == list(struct(x = 9007199254740992.0)) } | count`, 0)
}

func TestPlannedNumericPromotionCoercesDirectExpressionValues(t *testing.T) {
	tbl := table.NewTable([]string{"id"})
	tbl.AddRow([]table.Value{table.IntVal(9007199254740993)})

	assertCount := func(query string, want int64) {
		t.Helper()
		result := runQuery(t, tbl, query)
		if got := result.Get(0, "count"); got.Type != table.TypeInt || got.Int != want {
			t.Fatalf("%s: got %v, want count %d", query, got, want)
		}
	}

	cases := []struct {
		name   string
		direct string
		staged string
	}{
		{
			name:   "coalesce_promotes_to_planned_float",
			direct: `filter { coalesce(id, 0.0) == 9007199254740992.0 } | count`,
			staged: `transform y = coalesce(id, 0.0) | filter { y == 9007199254740992.0 } | count`,
		},
		{
			name:   "if_promotes_to_planned_float",
			direct: `filter { if(true, id, 0.0) == 9007199254740992.0 } | count`,
			staged: `transform y = if(true, id, 0.0) | filter { y == 9007199254740992.0 } | count`,
		},
		{
			name:   "list_literal_elements_promote_to_planned_float",
			direct: `filter { list_contains(list(id, 0.0), 9007199254740992.0) } | count`,
			staged: `transform xs = list(id, 0.0) | filter { list_contains(xs, 9007199254740992.0) } | count`,
		},
		{
			name:   "record_field_promotes_inside_planned_expression",
			direct: `filter { struct(x = coalesce(id, 0.0)) == struct(x = 9007199254740992.0) } | count`,
			staged: `transform r = struct(x = coalesce(id, 0.0)) | filter { r == struct(x = 9007199254740992.0) } | count`,
		},
		{
			name:   "list_record_field_promotes_inside_planned_expression",
			direct: `filter { list_contains(list(struct(x = id), struct(x = 0.0)), struct(x = 9007199254740992.0)) } | count`,
			staged: `transform xs = list(struct(x = id), struct(x = 0.0)) | filter { list_contains(xs, struct(x = 9007199254740992.0)) } | count`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertCount(tc.direct, 1)
			assertCount(tc.staged, 1)
		})
	}
}

func TestPlannedDivisionRuntimeMatchesPlannerAndStagedValues(t *testing.T) {
	tbl := table.NewTable([]string{"id"})
	tbl.AddRow([]table.Value{table.IntVal(9007199254740993)})

	assertCount := func(query string, want int64) {
		t.Helper()
		result := runQuery(t, tbl, query)
		if got := result.Get(0, "count"); got.Type != table.TypeInt || got.Int != want {
			t.Fatalf("%s: got %v, want count %d", query, got, want)
		}
	}
	assertBool := func(query string, want bool) {
		t.Helper()
		result := runQuery(t, tbl, query)
		got := result.Get(0, "ok")
		if got.Type != table.TypeBool || got.Bool != want {
			t.Fatalf("%s: got %v, want ok=%v", query, got, want)
		}
	}
	assertFloat := func(query string, want float64) {
		t.Helper()
		result := runQuery(t, tbl, query)
		got := result.Get(0, "y")
		if got.Type != table.TypeFloat || got.Float != want {
			t.Fatalf("%s: got %v, want float %v", query, got, want)
		}
	}

	assertCount(`filter { (id / 1) == 9007199254740992.0 } | count`, 1)
	assertCount(`transform y = id / 1 | filter { y == 9007199254740992.0 } | count`, 1)
	assertBool(`transform ok = (id / 1) == 9007199254740992.0 | select ok`, true)
	assertBool(`transform y = id / 1 | transform ok = y == 9007199254740992.0 | select ok`, true)
	assertCount(`filter { list_contains(list(id / 1), 9007199254740992.0) } | count`, 1)
	assertCount(`filter { struct(y = id / 1) == struct(y = 9007199254740992.0) } | count`, 1)
	assertFloat(`transform y = id / 1 | select y`, 9007199254740992)
}

func TestPlannedIntegerArithmeticRuntimePreservesExactIntValues(t *testing.T) {
	tbl := table.NewTable([]string{"id"})
	tbl.AddRow([]table.Value{table.IntVal(9007199254740993)})

	result := runQuery(t, tbl, `transform add = id + 0, sub = id - 0, mul = id * 1 | select add, sub, mul`)
	for _, col := range []string{"add", "sub", "mul"} {
		got := result.Get(0, col)
		if got.Type != table.TypeInt || got.Int != 9007199254740993 {
			t.Fatalf("%s: got %v, want exact int 9007199254740993", col, got)
		}
	}

	for _, query := range []string{
		`filter { id + 0 == 9007199254740993 } | count`,
		`filter { id - 0 == 9007199254740993 } | count`,
		`filter { id * 1 == 9007199254740993 } | count`,
	} {
		t.Run(query, func(t *testing.T) {
			result := runQuery(t, tbl, query)
			if got := result.Get(0, "count"); got.Type != table.TypeInt || got.Int != 1 {
				t.Fatalf("%s: got %v, want count 1", query, got)
			}
		})
	}
}

func TestPlannedIntegerArithmeticOverflowErrors(t *testing.T) {
	cases := []struct {
		name  string
		value int64
		query string
	}{
		{name: "add", value: math.MaxInt64, query: `transform y = id + 1 | select y`},
		{name: "subtract", value: math.MinInt64, query: `transform y = id - 1 | select y`},
		{name: "multiply", value: math.MaxInt64/2 + 1, query: `transform y = id * 2 | select y`},
		{name: "unary_negate", value: math.MinInt64, query: `transform y = -id | select y`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tbl := table.NewTable([]string{"id"})
			tbl.AddRow([]table.Value{table.IntVal(tc.value)})
			expectQueryErrContains(t, tbl, tc.query, "integer overflow")
		})
	}
}

func TestPlannedReduceDivisionRuntimeMatchesPlannerAndStagedValues(t *testing.T) {
	tbl := table.NewTable([]string{"g", "id"})
	tbl.AddRow([]table.Value{table.StrVal("a"), table.IntVal(9007199254740993)})

	assertCount := func(query string, want int64) {
		t.Helper()
		result := runQuery(t, tbl, query)
		if got := result.Get(0, "count"); got.Type != table.TypeInt || got.Int != want {
			t.Fatalf("%s: got %v, want count %d", query, got, want)
		}
	}
	assertBool := func(query string, want bool) {
		t.Helper()
		result := runQuery(t, tbl, query)
		got := result.Get(0, "ok")
		if got.Type != table.TypeBool || got.Bool != want {
			t.Fatalf("%s: got %v, want ok=%v", query, got, want)
		}
	}
	assertFloat := func(query string, want float64) {
		t.Helper()
		result := runQuery(t, tbl, query)
		got := result.Get(0, "avg_id")
		if got.Type != table.TypeFloat || got.Float != want {
			t.Fatalf("%s: got %v, want float %v", query, got, want)
		}
	}

	assertBool(`group g | reduce ok = (sum(id) / count()) == 9007199254740992.0 | remove grouped | select ok`, true)
	assertBool(`group g | reduce avg_id = sum(id) / count() | remove grouped | transform ok = avg_id == 9007199254740992.0 | select ok`, true)
	assertCount(`group g | reduce avg_id = sum(id) / count() | remove grouped | filter { avg_id == 9007199254740992.0 } | count`, 1)
	assertFloat(`group g | reduce avg_id = sum(id) / count() | remove grouped | select avg_id`, 9007199254740992)
}

func TestPlannedReduceIntegerArithmeticRuntimePreservesExactIntValues(t *testing.T) {
	tbl := table.NewTable([]string{"g", "id"})
	tbl.AddRow([]table.Value{table.StrVal("a"), table.IntVal(9007199254740993)})

	result := runQuery(t, tbl, `group g | reduce add = sum(id) + 0, sub = sum(id) - 0, mul = sum(id) * 1 | remove grouped | select add, sub, mul`)
	for _, col := range []string{"add", "sub", "mul"} {
		got := result.Get(0, col)
		if got.Type != table.TypeInt || got.Int != 9007199254740993 {
			t.Fatalf("%s: got %v, want exact int 9007199254740993", col, got)
		}
	}

	result = runQuery(t, tbl, `group g | reduce total = sum(id) + 0 | remove grouped | filter { total == 9007199254740993 } | count`)
	if got := result.Get(0, "count"); got.Type != table.TypeInt || got.Int != 1 {
		t.Fatalf("staged reduce exact filter: got %v, want count 1", got)
	}
}

func TestPlannedReduceIntegerArithmeticOverflowErrors(t *testing.T) {
	t.Run("sum_overflow", func(t *testing.T) {
		tbl := table.NewTable([]string{"g", "id"})
		tbl.AddRow([]table.Value{table.StrVal("a"), table.IntVal(math.MaxInt64)})
		tbl.AddRow([]table.Value{table.StrVal("a"), table.IntVal(1)})
		expectQueryErrContains(t, tbl, `group g | reduce total = sum(id) | remove grouped | select total`, "integer overflow")
	})

	t.Run("reduce_expression_overflow", func(t *testing.T) {
		tbl := table.NewTable([]string{"g", "id"})
		tbl.AddRow([]table.Value{table.StrVal("a"), table.IntVal(math.MaxInt64)})
		expectQueryErrContains(t, tbl, `group g | reduce total = sum(id) + 1 | remove grouped | select total`, "integer overflow")
	})
}

func TestPlannedExpressionEvaluatorRunsSharedScalarFunctions(t *testing.T) {
	tbl := table.NewTable([]string{"name", "code", "xs", "date", "flag", "n"})
	tbl.AddRow([]table.Value{
		table.StrVal(" Alice "),
		table.StrVal("abcdef"),
		table.ListVal([]table.Value{table.IntVal(1), table.IntVal(2), table.IntVal(3)}),
		table.StrVal("2024-05-06"),
		table.BoolVal(true),
		table.IntVal(7),
	})

	result := runQuery(t, tbl, `transform neg = -n, not_flag = not flag, upper_name = upper(name), lower_name = lower(name), trimmed = trim(name), name_len = str_len(name), xs_len = list_len(xs), sub = substr(code, 1, 3), has_cd = str_contains(code, "cd"), starts_ab = starts_with(code, "ab"), ends_ef = ends_with(code, "ef"), matched = matches(code, "^[a-z]+$"), year_part = year(date), month_part = month(date), day_part = day(date), chosen = if(flag, n, 0), fallback = coalesce(null, n) | select neg, not_flag, upper_name, lower_name, trimmed, name_len, xs_len, sub, has_cd, starts_ab, ends_ef, matched, year_part, month_part, day_part, chosen, fallback`)

	want := map[string]table.Value{
		"neg":        table.IntVal(-7),
		"not_flag":   table.BoolVal(false),
		"upper_name": table.StrVal(" ALICE "),
		"lower_name": table.StrVal(" alice "),
		"trimmed":    table.StrVal("Alice"),
		"name_len":   table.IntVal(7),
		"xs_len":     table.IntVal(3),
		"sub":        table.StrVal("bcd"),
		"has_cd":     table.BoolVal(true),
		"starts_ab":  table.BoolVal(true),
		"ends_ef":    table.BoolVal(true),
		"matched":    table.BoolVal(true),
		"year_part":  table.IntVal(2024),
		"month_part": table.IntVal(5),
		"day_part":   table.IntVal(6),
		"chosen":     table.IntVal(7),
		"fallback":   table.IntVal(7),
	}
	for col, wantVal := range want {
		if got := result.Get(0, col); !table.Equal(got, wantVal) {
			t.Fatalf("%s: got %v, want %v", col, got, wantVal)
		}
	}
}

func TestFilterBareListConstructorErrorsAsNonBoolean(t *testing.T) {
	err := runQueryExpectErr(t, usersTable(), `filter { list(1) }`)
	if err == nil {
		t.Fatal("expected non-boolean filter error")
	}
	if !strings.Contains(err.Error(), "filter expression must return bool") || !strings.Contains(err.Error(), "list<int>") {
		t.Fatalf("expected non-boolean list filter error, got: %v", err)
	}
}

func TestReduceListConstructorUnsupported(t *testing.T) {
	err := runQueryExpectErr(t, usersTable(), `group city | reduce xs = list(first(age))`)
	if err == nil {
		t.Fatal("expected reduce error for list constructor")
	}
	if !strings.Contains(err.Error(), "list constructor is not supported in reduce") {
		t.Fatalf("expected reduce/list error, got: %v", err)
	}
}

func TestReduceStructConstructorUnsupported(t *testing.T) {
	err := runQueryExpectErr(t, usersTable(), `group city | reduce rec = struct(age = first(age))`)
	if err == nil {
		t.Fatal("expected reduce error for struct constructor")
	}
	if !strings.Contains(err.Error(), "struct constructor is not supported in reduce") {
		t.Fatalf("expected reduce/struct error, got: %v", err)
	}
}

func TestTransformListScalarMixedColumnRejectsIncompatibleBranches(t *testing.T) {
	tbl := table.NewTable([]string{"name", "active"})
	tbl.AddRow([]table.Value{table.StrVal("a"), table.BoolVal(true)})
	tbl.AddRow([]table.Value{table.StrVal("b"), table.BoolVal(false)})

	expectQueryErrContains(t, tbl, `transform xs = if(active, list(1, 2), "off") | select xs`, "if() branches do not have one common type")
}

func TestTransformRejectsDuplicateAssignmentTargets(t *testing.T) {
	cases := []struct {
		name  string
		query string
		want  string
	}{
		{
			name:  "with_rows",
			query: `transform x = 1, x = 2 | select x`,
			want:  `transform target "x" assigned more than once`,
		},
		{
			name:  "after_zero_row_filter",
			query: `filter { false } | transform x = 1, x = 2 | describe`,
			want:  `transform target "x" assigned more than once`,
		},
		{
			name:  "overwrite_existing_column",
			query: `transform age = age + 1, age = age + 2 | select age`,
			want:  `transform target "age" assigned more than once`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expectQueryErrContains(t, usersTable(), tc.query, tc.want)
		})
	}
}

func TestGroupReduce(t *testing.T) {
	result := runQuery(t, usersTable(), "group city | reduce total = sum(age), n = count() | remove grouped")
	if len(result.Columns) != 3 {
		t.Fatalf("expected 3 columns (city, total, n), got %d: %v", len(result.Columns), result.Columns)
	}
	// NY: 30+35+40=105, count=3
	nyIdx := -1
	for i := 0; i < result.NumRows; i++ {
		if result.GetAt(i, 0).Str == "NY" {
			nyIdx = i
		}
	}
	if nyIdx < 0 {
		t.Fatal("NY group not found")
	}
	totalIdx := result.ColIndex("total")
	nIdx := result.ColIndex("n")
	if result.GetAt(nyIdx, totalIdx).Int != 105 {
		t.Errorf("expected NY total=105, got %d", result.GetAt(nyIdx, totalIdx).Int)
	}
	if result.GetAt(nyIdx, nIdx).Int != 3 {
		t.Errorf("expected NY count=3, got %d", result.GetAt(nyIdx, nIdx).Int)
	}
}

func TestGroupCommaColumnList(t *testing.T) {
	result := runQuery(t, usersTable(), "group city, name")
	if result.NumRows != 6 {
		t.Errorf("expected 6 groups, got %d", result.NumRows)
	}
	assertColumns(t, result, []string{"city", "name", "grouped"})
}

func TestGroupKeepsKeyInNestedRows(t *testing.T) {
	result := runQuery(t, usersTable(), "group city")
	groupedIdx := result.ColIndex("grouped")
	if groupedIdx < 0 {
		t.Fatal("grouped column not found")
	}
	// Pick the first group row and check nested records contain the key column
	nested := result.GetAt(0, groupedIdx)
	if nested.Type != table.TypeList {
		t.Fatalf("expected TypeList, got %v", nested.Type)
	}
	rec := nested.List[0]
	if rec.Type != table.TypeRecord {
		t.Fatalf("expected TypeRecord, got %v", rec.Type)
	}
	// The nested record should contain all 3 original columns: name, age, city
	fieldNames := make(map[string]bool)
	for _, f := range rec.Fields {
		fieldNames[f.Name] = true
	}
	for _, col := range []string{"name", "age", "city"} {
		if !fieldNames[col] {
			t.Errorf("nested record missing field %q, got fields %v", col, fieldNames)
		}
	}
}

func TestGroupKeepsKeyInNestedRowsReduceStillWorks(t *testing.T) {
	// Since the key column is now in the nested rows, reduce should still
	// be able to aggregate on it (e.g. first(city) should return the group key).
	result := runQuery(t, usersTable(), "group city | reduce city_check = first(city), n = count() | remove grouped")
	cityIdx := result.ColIndex("city")
	checkIdx := result.ColIndex("city_check")
	for i := 0; i < result.NumRows; i++ {
		if result.GetAt(i, cityIdx).Str != result.GetAt(i, checkIdx).Str {
			t.Errorf("expected city_check to match city, got %q vs %q",
				result.GetAt(i, checkIdx).Str, result.GetAt(i, cityIdx).Str)
		}
	}
}

func TestRename(t *testing.T) {
	result := runQuery(t, usersTable(), "rename name=first_name")
	assertColumns(t, result, []string{"first_name", "age", "city"})
}

func TestRenameMultiplePairsAreSimultaneous(t *testing.T) {
	result := runQuery(t, usersTable(), "rename name=age, age=name")
	assertColumns(t, result, []string{"age", "name", "city"})

	if got := result.GetAt(0, 0).Str; got != "Alice" {
		t.Errorf("expected first column value to remain Alice, got %q", got)
	}
	if got := result.GetAt(0, 1).Int; got != 30 {
		t.Errorf("expected second column value to remain 30, got %d", got)
	}
}

func TestRenameNoOpToSameName(t *testing.T) {
	result := runQuery(t, usersTable(), "rename name=name")
	assertColumns(t, result, []string{"name", "age", "city"})
}

func TestRenameChained(t *testing.T) {
	result := runQuery(t, usersTable(), "rename name=username | rename city=location")
	assertColumns(t, result, []string{"username", "age", "location"})
}

func TestRenameMultipleValidPairsInOneOp(t *testing.T) {
	result := runQuery(t, usersTable(), "rename name=first_name, city=location")
	assertColumns(t, result, []string{"first_name", "age", "location"})
}

func TestRenameColumnNotFound(t *testing.T) {
	err := runQueryExpectErr(t, usersTable(), "rename missing=foo")
	if err == nil {
		t.Fatal("expected column not found error")
	}
	if !strings.Contains(err.Error(), `column "missing" not found`) {
		t.Errorf("expected column not found error, got %v", err)
	}
}

func TestRenameRejectsDuplicateResultColumns(t *testing.T) {
	cases := []struct {
		name      string
		query     string
		collision string
	}{
		{"target_exists", "rename name=age", `duplicate column name "age"`},
		{"target_exists_other_column", "rename city=name", `duplicate column name "name"`},
		{"pairs_share_target", "rename name=x, city=x", `duplicate column name "x"`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := runQueryExpectErr(t, usersTable(), tc.query)
			if err == nil {
				t.Fatal("expected duplicate column error")
			}
			if !strings.Contains(err.Error(), tc.collision) {
				t.Errorf("expected error containing %q, got %v", tc.collision, err)
			}
		})
	}
}

func TestRenameRejectsRepeatedSourceColumn(t *testing.T) {
	err := runQueryExpectErr(t, usersTable(), "rename name=first_name, name=full_name")
	if err == nil {
		t.Fatal("expected repeated source column error")
	}
	if !strings.Contains(err.Error(), `column "name" renamed more than once`) {
		t.Errorf("expected repeated source column error, got %v", err)
	}
}

func assertColumns(t *testing.T, tbl *table.Table, want []string) {
	t.Helper()
	if len(tbl.Columns) != len(want) {
		t.Fatalf("expected columns %v, got %v", want, tbl.Columns)
	}
	for i, col := range want {
		if tbl.Columns[i] != col {
			t.Fatalf("expected columns %v, got %v", want, tbl.Columns)
		}
	}
}

func TestRemove(t *testing.T) {
	result := runQuery(t, usersTable(), "remove city")
	if len(result.Columns) != 2 {
		t.Fatalf("expected 2 columns, got %d", len(result.Columns))
	}
	for _, c := range result.Columns {
		if c == "city" {
			t.Error("city should have been removed")
		}
	}
}

func TestRemoveCommaColumnList(t *testing.T) {
	result := runQuery(t, usersTable(), "remove age, city")
	assertColumns(t, result, []string{"name"})
	if result.NumRows != 6 {
		t.Errorf("expected 6 rows, got %d", result.NumRows)
	}
}

func TestNullArithmetic(t *testing.T) {
	tbl := table.NewTable([]string{"a", "b"})
	tbl.AddRow([]table.Value{table.IntVal(10), table.Null()})

	result := runQuery(t, tbl, "transform c = a * b")
	if !result.GetAt(0, 2).IsNull() {
		t.Errorf("expected null from 10 * null, got %v", result.GetAt(0, 2).AsString())
	}
}

func TestCoalesce(t *testing.T) {
	tbl := table.NewTable([]string{"a", "b"})
	tbl.AddRow([]table.Value{table.Null(), table.IntVal(42)})

	result := runQuery(t, tbl, "transform c = coalesce(a, b)")
	if result.GetAt(0, 2).Int != 42 {
		t.Errorf("expected 42, got %v", result.GetAt(0, 2).AsString())
	}
}

func TestPlannedExpressionPrecedence(t *testing.T) {
	tbl := table.NewTable([]string{"x"})
	tbl.AddRow([]table.Value{table.IntVal(5)})
	result := runQuery(t, tbl, "transform y = x + 3 * 2 | select y")
	if val := result.GetAt(0, 0); val.Type != table.TypeInt || val.Int != 11 {
		t.Errorf("expected 11, got %v", val)
	}
}

func TestIsNull(t *testing.T) {
	tbl := table.NewTable([]string{"a"})
	tbl.AddRow([]table.Value{table.Null()})
	tbl.AddRow([]table.Value{table.IntVal(1)})

	result := runQuery(t, tbl, "filter { a is null }")
	if result.NumRows != 1 {
		t.Errorf("expected 1 row, got %d", result.NumRows)
	}

	result2 := runQuery(t, tbl, "filter { a is not null }")
	if result2.NumRows != 1 {
		t.Errorf("expected 1 row, got %d", result2.NumRows)
	}
}

func nullableUsersTable() *table.Table {
	t := table.NewTable([]string{"name", "age", "city"})
	t.AddRow([]table.Value{table.StrVal("Alice"), table.IntVal(30), table.StrVal("NY")})
	t.AddRow([]table.Value{table.StrVal("Bob"), table.Null(), table.StrVal("LA")})
	t.AddRow([]table.Value{table.StrVal("Charlie"), table.IntVal(35), table.StrVal("NY")})
	t.AddRow([]table.Value{table.StrVal("Diana"), table.Null(), table.Null()})
	t.AddRow([]table.Value{table.StrVal("Eve"), table.IntVal(22), table.Null()})
	return t
}

func TestIsNullCombinesWithAnd(t *testing.T) {
	result := runQuery(t, nullableUsersTable(), `filter { age is not null and city == "NY" }`)
	if result.NumRows != 2 {
		t.Fatalf("expected 2 rows (Alice, Charlie), got %d", result.NumRows)
	}
	names := sortedNames(t, result)
	if names[0] != "Alice" || names[1] != "Charlie" {
		t.Errorf("expected [Alice Charlie], got %v", names)
	}
}

func TestIsNullCombinesWithOr(t *testing.T) {
	result := runQuery(t, nullableUsersTable(), "filter { city is null or age > 30 }")
	if result.NumRows != 3 {
		t.Fatalf("expected 3 rows (Charlie, Diana, Eve), got %d", result.NumRows)
	}
	names := sortedNames(t, result)
	if names[0] != "Charlie" || names[1] != "Diana" || names[2] != "Eve" {
		t.Errorf("expected [Charlie Diana Eve], got %v", names)
	}
}

func TestNotAgeIsNull(t *testing.T) {
	tbl := nullableUsersTable()
	want := runQuery(t, tbl, "filter { age is not null }")
	got := runQuery(t, tbl, "filter { not age is null }")
	if got.NumRows != want.NumRows {
		t.Fatalf("expected %d rows, got %d", want.NumRows, got.NumRows)
	}
	for i := 0; i < want.NumRows; i++ {
		if got.GetAt(i, 0).Str != want.GetAt(i, 0).Str {
			t.Errorf("row %d: expected name %q, got %q", i, want.GetAt(i, 0).Str, got.GetAt(i, 0).Str)
		}
	}
}

func TestIfFunction(t *testing.T) {
	result := runQuery(t, usersTable(), `transform label = if(age > 30, "old", "young") | select name, label`)
	// Alice(30) -> young, Charlie(35) -> old
	if result.GetAt(0, 1).Str != "young" {
		t.Errorf("expected 'young' for Alice, got %q", result.GetAt(0, 1).Str)
	}
	if result.GetAt(2, 1).Str != "old" {
		t.Errorf("expected 'old' for Charlie, got %q", result.GetAt(2, 1).Str)
	}
}

func TestUpperLower(t *testing.T) {
	result := runQuery(t, usersTable(), `transform up = upper(city), lo = lower(name) | select up, lo | head 1`)
	if result.GetAt(0, 0).Str != "NY" {
		t.Errorf("expected 'NY', got %q", result.GetAt(0, 0).Str)
	}
	if result.GetAt(0, 1).Str != "alice" {
		t.Errorf("expected 'alice', got %q", result.GetAt(0, 1).Str)
	}
}

func TestStringTransformsNullPropagation(t *testing.T) {
	tbl := table.NewTable([]string{"s"})
	tbl.AddRow([]table.Value{table.Null()})
	cases := []struct {
		name  string
		query string
	}{
		{"upper", "transform x = upper(s) | select x"},
		{"lower", "transform x = lower(s) | select x"},
		{"trim", "transform x = trim(s) | select x"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := runQuery(t, tbl, tc.query)
			if !result.GetAt(0, 0).IsNull() {
				t.Errorf("want null, got %v", result.GetAt(0, 0).AsString())
			}
		})
	}
}

func sortedNames(t *testing.T, result *table.Table) []string {
	t.Helper()
	idx := result.ColIndex("name")
	if idx < 0 {
		t.Fatal("name column not found")
	}
	names := make([]string, result.NumRows)
	for i := range names {
		names[i] = result.GetAt(i, idx).Str
	}
	return names
}

func assertSortedNames(t *testing.T, result *table.Table, want []string) {
	t.Helper()
	got := sortedNames(t, result)
	if len(got) != len(want) {
		t.Fatalf("expected %d rows, got %d: %v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d: expected %q, got %q", i, want[i], got[i])
		}
	}
}

func TestStringPredicatesFilter(t *testing.T) {
	cases := []struct {
		name  string
		query string
		want  []string
	}{
		{"str_contains", `filter { str_contains(name, "a") }`, []string{"Charlie", "Diana", "Frank"}},
		{"starts_with", `filter { starts_with(name, "C") }`, []string{"Charlie"}},
		{"ends_with", `filter { ends_with(name, "e") }`, []string{"Alice", "Charlie", "Eve"}},
		{"matches", `filter { matches(name, "^[AB]") }`, []string{"Alice", "Bob"}},
		{"matches_unanchored", `filter { matches(name, "li") }`, []string{"Alice", "Charlie"}},
		{"matches_anchored_full", `filter { matches(name, "^Alice$") }`, []string{"Alice"}},
		{"negative", `filter { str_contains(name, "zzz") }`, nil},
		{"not_str_contains", `filter { not str_contains(name, "a") }`, []string{"Alice", "Bob", "Eve"}},
		{"combined", `filter { str_contains(name, "a") and city == "NY" }`, []string{"Charlie", "Frank"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := runQuery(t, usersTable(), tc.query+" | select name | sort name")
			assertSortedNames(t, result, tc.want)
		})
	}
}

func TestStringPredicatesTransform(t *testing.T) {
	cases := []struct {
		name  string
		query string
		want  bool
	}{
		{"str_contains", `transform hit = str_contains(city, "Y")`, true},
		{"starts_with", `transform hit = starts_with(name, "Al")`, true},
		{"ends_with", `transform hit = ends_with(name, "ce")`, true},
		{"matches", `transform hit = matches(name, "^Al")`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := runQuery(t, usersTable(), tc.query+" | select hit | head 1")
			idx := result.ColIndex("hit")
			b, ok := result.GetAt(0, idx).AsBool()
			if !ok || b != tc.want {
				t.Errorf("expected %v, got ok=%v val=%v", tc.want, ok, b)
			}
		})
	}
}

func TestStringPredicatesWrongTypeErrors(t *testing.T) {
	cases := []struct {
		name    string
		query   string
		wantErr string
	}{
		{"str_contains_int_haystack", `transform hit = str_contains(age, "3")`, "str_contains() requires a string, got int"},
		{"str_contains_int_needle", `transform hit = str_contains(name, age)`, "str_contains() requires a string substring, got int"},
		{"starts_with_int_needle", `transform hit = starts_with(name, age)`, "starts_with() requires a string prefix, got int"},
		{"ends_with_int_needle", `transform hit = ends_with(name, age)`, "ends_with() requires a string suffix, got int"},
		{"matches_int_haystack", `transform hit = matches(age, "3")`, "matches() requires a string, got int"},
		{"matches_int_pattern", `filter { matches(name, age) }`, "matches() requires a string regex, got int"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expectQueryErrContains(t, usersTable(), tc.query, tc.wantErr)
		})
	}
}

func TestStringPredicatesNullPropagation(t *testing.T) {
	tbl := table.NewTable([]string{"s", "needle"})
	tbl.AddRow([]table.Value{table.Null(), table.StrVal("a")})
	tbl.AddRow([]table.Value{table.StrVal("abc"), table.Null()})
	for _, fn := range []string{"str_contains", "starts_with", "ends_with", "matches"} {
		t.Run(fn+"_null_haystack", func(t *testing.T) {
			result := runQuery(t, tbl, "transform x = "+fn+`(s, "a") | head 1`)
			idx := result.ColIndex("x")
			if !result.GetAt(0, idx).IsNull() {
				t.Errorf("%s(null, ...) should be null, got %v", fn, result.GetAt(0, idx).AsString())
			}
		})
		t.Run(fn+"_null_needle", func(t *testing.T) {
			result := runQuery(t, tbl, "transform x = "+fn+`(s, needle) | tail 1`)
			idx := result.ColIndex("x")
			if !result.GetAt(0, idx).IsNull() {
				t.Errorf("%s(..., null) should be null, got %v", fn, result.GetAt(0, idx).AsString())
			}
		})
	}
}

func TestStringPredicatesFilterNullDropsRow(t *testing.T) {
	tbl := table.NewTable([]string{"name", "message"})
	tbl.AddRow([]table.Value{table.StrVal("Alice"), table.StrVal("ERROR: timeout")})
	tbl.AddRow([]table.Value{table.StrVal("Bob"), table.Null()})
	result := runQuery(t, tbl, `filter { str_contains(message, "ERROR") } | select name`)
	if result.NumRows != 1 {
		t.Fatalf("expected 1 row, got %d", result.NumRows)
	}
	if got := result.GetAt(0, 0).Str; got != "Alice" {
		t.Errorf("expected Alice, got %q", got)
	}
}

func TestMatchesInvalidRegex(t *testing.T) {
	err := runQueryExpectErr(t, usersTable(), `filter { matches(name, "[") }`)
	if err == nil {
		t.Fatal("expected error for invalid regex")
	}
	if !strings.Contains(err.Error(), "invalid regex") {
		t.Errorf("expected invalid regex error, got: %v", err)
	}
}

func TestMatchesInvalidRegexFromColumn(t *testing.T) {
	tbl := table.NewTable([]string{"s", "pattern"})
	tbl.AddRow([]table.Value{table.StrVal("hello"), table.StrVal("ell")})
	tbl.AddRow([]table.Value{table.StrVal("world"), table.StrVal("[invalid")})
	err := runQueryExpectErr(t, tbl, `filter { matches(s, pattern) }`)
	if err == nil {
		t.Fatal("expected error for invalid regex from column")
	}
	if !strings.Contains(err.Error(), "invalid regex") {
		t.Errorf("expected invalid regex error, got: %v", err)
	}
}

func TestStringPredicatesArity(t *testing.T) {
	cases := []struct {
		name  string
		query string
	}{
		{"str_contains_1_arg", `filter { str_contains(name) }`},
		{"str_contains_3_args", `filter { str_contains(name, "a", "b") }`},
		{"starts_with_1_arg", `filter { starts_with(name) }`},
		{"starts_with_3_args", `filter { starts_with(name, "A", "B") }`},
		{"ends_with_1_arg", `filter { ends_with(name) }`},
		{"ends_with_3_args", `filter { ends_with(name, "e", "x") }`},
		{"matches_1_arg", `filter { matches(name) }`},
		{"matches_3_args", `filter { matches(name, "A", "B") }`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := runQueryExpectErr(t, usersTable(), tc.query)
			if err == nil {
				t.Fatal("expected arity error")
			}
		})
	}
}

func TestContainsRemoved(t *testing.T) {
	err := runQueryExpectErr(t, usersTable(), `filter { contains(name, "a") }`)
	if err == nil {
		t.Fatal("expected contains() to be removed")
	}
	if !strings.Contains(err.Error(), `unknown function "contains"`) {
		t.Fatalf("expected unknown function error, got %v", err)
	}
}

func TestListContains(t *testing.T) {
	tbl := table.NewTable([]string{"name", "tags", "nums", "empty", "nilcol"})
	tbl.AddRow([]table.Value{
		table.StrVal("Alice"),
		table.ListVal([]table.Value{table.StrVal("admin"), table.StrVal("user")}),
		table.ListVal([]table.Value{table.IntVal(1), table.FloatVal(2.5)}),
		table.ListVal(nil),
		table.Null(),
	})
	tbl.AddRow([]table.Value{
		table.StrVal("Bob"),
		table.ListVal([]table.Value{table.StrVal("user")}),
		table.ListVal([]table.Value{table.StrVal("1"), table.IntVal(3)}),
		table.ListVal(nil),
		table.Null(),
	})

	cases := []struct {
		name  string
		query string
		want  []string
	}{
		{"string_hit", `filter { list_contains(tags, "admin") }`, []string{"Alice"}},
		{"string_exact_type", `filter { list_contains(nums, "1") }`, []string{"Bob"}},
		{"float_exact_type_after_numeric_widening", `filter { list_contains(nums, 1.0) }`, []string{"Alice"}},
		{"int_hit_after_numeric_widening", `filter { list_contains(nums, 1) }`, []string{"Alice"}},
		{"string_miss", `filter { list_contains(tags, "missing") }`, nil},
		{"empty_list", `filter { list_contains(empty, "x") }`, nil},
		{"null_list_drops", `filter { list_contains(nilcol, "x") }`, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := runQuery(t, tbl, tc.query+" | select name | sort name")
			assertSortedNames(t, result, tc.want)
		})
	}
}

func TestListContainsTransformNullPropagation(t *testing.T) {
	tbl := table.NewTable([]string{"xs", "needle"})
	tbl.AddRow([]table.Value{table.Null(), table.StrVal("a")})
	tbl.AddRow([]table.Value{table.ListVal([]table.Value{table.StrVal("a")}), table.Null()})

	result := runQuery(t, tbl, `transform hit = list_contains(xs, needle) | select hit`)
	for i := 0; i < result.NumRows; i++ {
		if !result.GetAt(i, 0).IsNull() {
			t.Fatalf("row %d: expected null, got %v", i, result.GetAt(i, 0).AsString())
		}
	}
}

func TestListContainsWrongTypeErrors(t *testing.T) {
	cases := []struct {
		name    string
		query   string
		wantErr string
	}{
		{"string_first_arg", `transform hit = list_contains(name, "a")`, "list_contains() requires a list, got string"},
		{"int_first_arg", `transform hit = list_contains(age, 1)`, "list_contains() requires a list, got int"},
		{"too_few_args", `transform hit = list_contains(name)`, "list_contains() takes 2 arguments"},
		{"too_many_args", `transform hit = list_contains(name, "a", "b")`, "list_contains() takes 2 arguments"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expectQueryErrContains(t, usersTable(), tc.query, tc.wantErr)
		})
	}
}

func TestListAndRecordEqualityAndTypeSafety(t *testing.T) {
	tbl := table.NewTable([]string{"name", "tags", "profile"})
	tbl.AddRow([]table.Value{
		table.StrVal("Alice"),
		table.ListVal([]table.Value{table.StrVal("admin"), table.StrVal("user")}),
		table.RecordVal([]table.RecordField{{Name: "role", Value: table.StrVal("admin")}}),
	})

	cases := []struct {
		name    string
		query   string
		want    []string
		wantErr string
	}{
		{"list_self_eq", `filter { tags == tags } | select name`, []string{"Alice"}, ""},
		{"list_different_type_eq", `filter { tags == "admin" } | select name`, nil, "cannot compare list with string"},
		{"list_different_type_neq", `filter { tags != "admin" } | select name`, nil, "cannot compare list with string"},
		{"record_self_eq", `filter { profile == profile } | select name`, []string{"Alice"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.wantErr != "" {
				expectQueryErrContains(t, tbl, tc.query, tc.wantErr)
				return
			}
			result := runQuery(t, tbl, tc.query)
			assertSortedNames(t, result, tc.want)
		})
	}
}

func TestListAndRecordOrderingErrors(t *testing.T) {
	tbl := table.NewTable([]string{"name", "tags", "profile"})
	tbl.AddRow([]table.Value{
		table.StrVal("Alice"),
		table.ListVal([]table.Value{table.StrVal("admin"), table.StrVal("user")}),
		table.RecordVal([]table.RecordField{{Name: "role", Value: table.StrVal("admin")}}),
	})

	cases := []struct {
		name    string
		query   string
		wantErr string
	}{
		{"list_order_string", `filter { tags > "admin" }`, "cannot compare list with string"},
		{"record_order_record", `filter { profile > profile }`, "cannot compare record with record"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expectQueryErrContains(t, tbl, tc.query, tc.wantErr)
		})
	}
}

func TestDistinctUsesExactStructuralKeys(t *testing.T) {
	tbl := table.NewTable([]string{"v"})
	tbl.AddRow([]table.Value{
		table.ListVal([]table.Value{table.StrVal("1"), table.IntVal(2)}),
	})
	tbl.AddRow([]table.Value{
		table.ListVal([]table.Value{table.IntVal(1), table.StrVal("2")}),
	})

	distinct := runQuery(t, tbl, "distinct v")
	if distinct.NumRows != 2 {
		t.Fatalf("expected distinct to keep both rows, got %d", distinct.NumRows)
	}
}

func TestGroupUsesExactStructuralKeys(t *testing.T) {
	tbl := table.NewTable([]string{"v"})
	tbl.AddRow([]table.Value{
		table.ListVal([]table.Value{table.StrVal("1"), table.IntVal(2)}),
	})
	tbl.AddRow([]table.Value{
		table.ListVal([]table.Value{table.IntVal(1), table.StrVal("2")}),
	})

	grouped := runQuery(t, tbl, "group v | reduce n = count() | remove grouped")
	if grouped.NumRows != 2 {
		t.Fatalf("expected group to keep both rows, got %d", grouped.NumRows)
	}
}

func TestReduceComparisonAndLogicalOperators(t *testing.T) {
	result := runQuery(t, usersTable(), `group city | reduce eq2 = count() == 2, ne2 = count() != 2, gt1 = count() > 1, ge2 = count() >= 2, lt2 = count() < 2, le1 = count() <= 1, both = count() > 1 and count() < 3, either = count() == 1 or count() == 2, and_null = count() == 2 and null, or_null = count() == 2 or null, not2 = not (count() == 2) | remove grouped | sort city`)

	want := map[string]map[string]table.Value{
		"LA": {
			"eq2": table.BoolVal(true), "ne2": table.BoolVal(false),
			"gt1": table.BoolVal(true), "ge2": table.BoolVal(true),
			"lt2": table.BoolVal(false), "le1": table.BoolVal(false),
			"both": table.BoolVal(true), "either": table.BoolVal(true),
			"and_null": table.Null(), "or_null": table.BoolVal(true),
			"not2": table.BoolVal(false),
		},
		"NY": {
			"eq2": table.BoolVal(false), "ne2": table.BoolVal(true),
			"gt1": table.BoolVal(true), "ge2": table.BoolVal(true),
			"lt2": table.BoolVal(false), "le1": table.BoolVal(false),
			"both": table.BoolVal(false), "either": table.BoolVal(false),
			"and_null": table.BoolVal(false), "or_null": table.Null(),
			"not2": table.BoolVal(true),
		},
		"SF": {
			"eq2": table.BoolVal(false), "ne2": table.BoolVal(true),
			"gt1": table.BoolVal(false), "ge2": table.BoolVal(false),
			"lt2": table.BoolVal(true), "le1": table.BoolVal(true),
			"both": table.BoolVal(false), "either": table.BoolVal(true),
			"and_null": table.BoolVal(false), "or_null": table.Null(),
			"not2": table.BoolVal(true),
		},
	}

	cityIdx := result.ColIndex("city")
	if cityIdx < 0 {
		t.Fatalf("city column missing from %v", result.Columns)
	}
	if result.NumRows != len(want) {
		t.Fatalf("row count: got %d, want %d", result.NumRows, len(want))
	}
	for row := 0; row < result.NumRows; row++ {
		city := result.GetAt(row, cityIdx).Str
		fields, ok := want[city]
		if !ok {
			t.Fatalf("unexpected city %q", city)
		}
		for col, expected := range fields {
			idx := result.ColIndex(col)
			if idx < 0 {
				t.Fatalf("missing column %q in %v", col, result.Columns)
			}
			got := result.GetAt(row, idx)
			if !table.Equal(got, expected) {
				t.Fatalf("%s.%s: got %s, want %s", city, col, got.AsString(), expected.AsString())
			}
		}
	}
}

func TestReduceUnaryAndIsNullOperatorsRuntime(t *testing.T) {
	tbl := table.NewTable([]string{"g", "amount"})
	tbl.AddRow([]table.Value{table.StrVal("a"), table.IntVal(2)})
	tbl.AddRow([]table.Value{table.StrVal("a"), table.IntVal(4)})
	tbl.AddRow([]table.Value{table.StrVal("b"), table.Null()})

	result := runQuery(t, tbl, `group g | reduce neg_count = -count(), neg_avg = -avg(amount), first_missing = first(amount) is null, first_present = first(amount) is not null | remove grouped | sort g`)
	if result.NumRows != 2 {
		t.Fatalf("row count: got %d, want 2", result.NumRows)
	}
	if got := result.Get(0, "neg_count").Int; got != -2 {
		t.Fatalf("neg_count group a: got %d, want -2", got)
	}
	if got := result.Get(0, "neg_avg").Float; got != -3 {
		t.Fatalf("neg_avg group a: got %g, want -3", got)
	}
	if result.Get(0, "first_missing").Bool || !result.Get(0, "first_present").Bool {
		t.Fatalf("group a null checks: missing=%v present=%v", result.Get(0, "first_missing").Bool, result.Get(0, "first_present").Bool)
	}
	if got := result.Get(1, "neg_count").Int; got != -1 {
		t.Fatalf("neg_count group b: got %d, want -1", got)
	}
	if !result.Get(1, "neg_avg").IsNull() {
		t.Fatalf("neg_avg group b: got %s, want null", result.Get(1, "neg_avg").AsString())
	}
	if !result.Get(1, "first_missing").Bool || result.Get(1, "first_present").Bool {
		t.Fatalf("group b null checks: missing=%v present=%v", result.Get(1, "first_missing").Bool, result.Get(1, "first_present").Bool)
	}
}

func unionRecordBranchTable(t *testing.T, includeStringBranch bool) *table.Table {
	t.Helper()
	unionSchema := &table.TypeDescriptor{Kind: table.TypeUnion, Branches: []*table.TypeDescriptor{
		{
			Kind: table.TypeRecord,
			Fields: []table.FieldDescriptor{
				{Name: "x", Type: &table.TypeDescriptor{Kind: table.TypeInt}},
			},
		},
		{Kind: table.TypeString},
	}}
	tbl := table.NewTableWithSchemas(
		[]string{"k", "u", "payload"},
		[]*table.TypeDescriptor{
			{Kind: table.TypeString},
			unionSchema,
			{
				Kind: table.TypeRecord,
				Fields: []table.FieldDescriptor{
					{Name: "u", Type: unionSchema},
				},
			},
		},
	)
	rows := [][]table.Value{
		{
			table.StrVal("a"),
			table.RecordVal([]table.RecordField{{Name: "x", Value: table.IntVal(1)}}),
			table.RecordVal([]table.RecordField{{Name: "u", Value: table.RecordVal([]table.RecordField{{Name: "x", Value: table.IntVal(1)}})}}),
		},
		{
			table.StrVal("a"),
			table.RecordVal([]table.RecordField{{Name: "x", Value: table.IntVal(2)}}),
			table.RecordVal([]table.RecordField{{Name: "u", Value: table.RecordVal([]table.RecordField{{Name: "x", Value: table.IntVal(2)}})}}),
		},
	}
	if includeStringBranch {
		rows = append(rows, []table.Value{
			table.StrVal("b"),
			table.StrVal("text"),
			table.RecordVal([]table.RecordField{{Name: "u", Value: table.StrVal("text")}}),
		})
	}
	for _, row := range rows {
		if err := tbl.AddRowTyped(row); err != nil {
			t.Fatalf("add union row: %v", err)
		}
	}
	return tbl
}

func scalarUnionTable(t *testing.T, includeStringBranch bool) *table.Table {
	t.Helper()
	unionSchema := &table.TypeDescriptor{Kind: table.TypeUnion, Branches: []*table.TypeDescriptor{
		{Kind: table.TypeInt},
		{Kind: table.TypeString},
	}}
	tbl := table.NewTableWithSchemas(
		[]string{"k", "u"},
		[]*table.TypeDescriptor{{Kind: table.TypeString}, unionSchema},
	)
	rows := [][]table.Value{
		{table.StrVal("a"), table.IntVal(7)},
		{table.StrVal("a"), table.IntVal(8)},
	}
	if includeStringBranch {
		rows = append(rows, []table.Value{table.StrVal("a"), table.StrVal("seven")})
	}
	for _, row := range rows {
		if err := tbl.AddRowTyped(row); err != nil {
			t.Fatalf("add scalar union row: %v", err)
		}
	}
	return tbl
}

func scalarUnionStringRowsTable(t *testing.T) *table.Table {
	t.Helper()
	unionSchema := &table.TypeDescriptor{Kind: table.TypeUnion, Branches: []*table.TypeDescriptor{
		{Kind: table.TypeInt},
		{Kind: table.TypeString},
	}}
	tbl := table.NewTableWithSchemas(
		[]string{"k", "u"},
		[]*table.TypeDescriptor{{Kind: table.TypeString}, unionSchema},
	)
	for _, row := range [][]table.Value{
		{table.StrVal("a"), table.StrVal("alice")},
		{table.StrVal("a"), table.StrVal("bob")},
	} {
		if err := tbl.AddRowTyped(row); err != nil {
			t.Fatalf("add string union row: %v", err)
		}
	}
	return tbl
}

func boolUnionTable(t *testing.T) *table.Table {
	t.Helper()
	unionSchema := &table.TypeDescriptor{Kind: table.TypeUnion, Branches: []*table.TypeDescriptor{
		{Kind: table.TypeBool},
		{Kind: table.TypeString},
	}}
	tbl := table.NewTableWithSchemas(
		[]string{"k", "u"},
		[]*table.TypeDescriptor{{Kind: table.TypeString}, unionSchema},
	)
	for _, row := range [][]table.Value{
		{table.StrVal("a"), table.BoolVal(true)},
		{table.StrVal("a"), table.BoolVal(false)},
	} {
		if err := tbl.AddRowTyped(row); err != nil {
			t.Fatalf("add bool union row: %v", err)
		}
	}
	return tbl
}

func TestUnionDotPathTraversalRejectedInExpressions(t *testing.T) {
	tbl := unionRecordBranchTable(t, false)
	mixed := unionRecordBranchTable(t, true)
	cases := []struct {
		name  string
		input *table.Table
		query string
	}{
		{"transform_direct", tbl, "transform x = u.x"},
		{"transform_struct_field", tbl, "transform wrapped = struct(x = u.x)"},
		{"transform_list_element", tbl, "transform xs = list(u.x)"},
		{"transform_function_arg", tbl, `transform y = str_contains(u.x, "1")`},
		{"filter_comparison", tbl, "filter { u.x == 1 }"},
		{"filter_ordering", tbl, "filter { u.x > 1 }"},
		{"filter_is_null", tbl, "filter { u.x is not null }"},
		{"filter_function_arg", tbl, "filter { list_contains(list(u.x), 1) }"},
		{"filter_after_head_keeps_schema", mixed, "head 1 | filter { u.x == 1 }"},
		{"transform_after_zero_rows_keeps_schema", mixed, "filter { false } | transform x = u.x"},
		{"nested_union_transform", tbl, "transform x = payload.u.x"},
		{"nested_union_filter", tbl, "filter { payload.u.x == 1 }"},
		{"reduce_aggregate_arg", tbl, "group k | reduce total = sum(u.x)"},
		{"reduce_binary_aggregate_arg", tbl, "group k | reduce total = sum(u.x) + count()"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expectQueryErrContains(t, tc.input, tc.query, "cannot access fields through union schema")
			expectQueryErrContains(t, tc.input, tc.query, "u.x")
		})
	}
}

func TestUnionTypeSpecificOperationsFailClosedFromSchema(t *testing.T) {
	cases := []struct {
		name    string
		input   *table.Table
		query   string
		wantErr string
	}{
		{"transform_arithmetic_active_int", scalarUnionTable(t, false), "transform x = u + 0", "requires numeric operands"},
		{"transform_string_function_active_string", scalarUnionStringRowsTable(t), "transform y = upper(u)", "requires a string"},
		{"filter_boolean_active_bool", boolUnionTable(t), "filter { u }", "filter expression must return bool"},
		{"reduce_sum_active_int", scalarUnionTable(t, false), "group k | reduce total = sum(u)", "requires a numeric column"},
		{"reduce_min_active_int", scalarUnionTable(t, false), "group k | reduce total = min(u)", "requires an orderable column"},
		{"reduce_max_active_int", scalarUnionTable(t, false), "group k | reduce total = max(u)", "requires an orderable column"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expectQueryErrContains(t, tc.input, tc.query, tc.wantErr)
		})
	}
}

func TestUnionComparisonsFailClosedThroughTypedPlanning(t *testing.T) {
	intOnly := scalarUnionTable(t, false)
	mixed := scalarUnionTable(t, true)
	cases := []struct {
		name    string
		input   *table.Table
		query   string
		wantErr string
	}{
		{"filter_arithmetic_int_only", intOnly, "filter { u + 0 == 7 } | count", "requires numeric operands"},
		{"filter_arithmetic_mixed", mixed, "filter { u + 0 == 7 } | count", "requires numeric operands"},
		{"filter_coalesce_int_only", intOnly, "filter { coalesce(u, 1.5) == 7 } | count", "cannot compare union values"},
		{"filter_coalesce_mixed", mixed, "filter { coalesce(u, 1.5) == 7 } | count", "cannot compare union values"},
		{"filter_unary_operand", intOnly, "filter { -u == -7 } | count", "requires numeric operand"},
		{"filter_struct_operand", intOnly, "filter { struct(x = u + 0) == struct(x = 7) } | count", "requires numeric operands"},
		{"filter_list_operand", intOnly, "filter { list(u + 0) == list(7) } | count", "requires numeric operands"},
		{"transform_arithmetic", intOnly, "transform eq = u + 0 == 7", "requires numeric operands"},
		{"transform_coalesce", intOnly, "transform eq = coalesce(u, 1.5) == 7", "cannot compare union values"},
		{"transform_if_condition", intOnly, `transform label = if(u + 0 == 7, "yes", "no")`, "requires numeric operands"},
		{"transform_struct_field", intOnly, "transform wrapped = struct(eq = u + 0 == 7)", "requires numeric operands"},
		{"transform_list_element", intOnly, "transform xs = list(u + 0 == 7)", "requires numeric operands"},
		{"reduce_first_arithmetic", intOnly, "group k | reduce eq = first(u) + 0 == 7", "requires numeric operands"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expectQueryErrContains(t, tc.input, tc.query, tc.wantErr)
		})
	}
}

func TestUnionNullCheckResultCanStillBeCompared(t *testing.T) {
	result := runQuery(t, scalarUnionTable(t, true), `filter { (u is not null) == true } | count`)
	if got := result.Get(0, "count").Int; got != 3 {
		t.Fatalf("count: got %d, want 3", got)
	}
}

func TestReduceFirstLastPreservesUnionBranchValues(t *testing.T) {
	result := runQuery(t, scalarUnionTable(t, true), `group k | reduce first_u = first(u), last_u = last(u) | select first_u, last_u`)
	if result.NumRows != 1 {
		t.Fatalf("row count: got %d, want 1", result.NumRows)
	}
	first := result.Get(0, "first_u")
	if first.Type != table.TypeInt || first.Int != 7 {
		t.Fatalf("first_u: got %s (%v), want int 7", first.AsString(), first.Type)
	}
	last := result.Get(0, "last_u")
	if last.Type != table.TypeString || last.Str != "seven" {
		t.Fatalf("last_u: got %s (%v), want string seven", last.AsString(), last.Type)
	}
	if got := result.Col(result.ColIndex("first_u")).Schema().String(); got != "union<int,string>?" {
		t.Fatalf("first_u schema: got %s, want union<int,string>?", got)
	}
	if got := result.Col(result.ColIndex("last_u")).Schema().String(); got != "union<int,string>?" {
		t.Fatalf("last_u schema: got %s, want union<int,string>?", got)
	}
}

func TestSortRejectsContainersContainingUnion(t *testing.T) {
	unionSchema := &table.TypeDescriptor{Kind: table.TypeUnion, Branches: []*table.TypeDescriptor{
		{Kind: table.TypeInt},
		{Kind: table.TypeString},
	}}
	tbl := table.NewTableWithSchemas(
		[]string{"xs", "p"},
		[]*table.TypeDescriptor{
			{Kind: table.TypeList, Elem: unionSchema},
			{Kind: table.TypeRecord, Fields: []table.FieldDescriptor{{Name: "u", Type: unionSchema}}},
		},
	)
	rows := [][]table.Value{
		{
			table.ListVal([]table.Value{table.IntVal(7), table.StrVal("seven")}),
			table.RecordVal([]table.RecordField{{Name: "u", Value: table.IntVal(7)}}),
		},
		{
			table.ListVal([]table.Value{table.StrVal("eight"), table.IntVal(8)}),
			table.RecordVal([]table.RecordField{{Name: "u", Value: table.StrVal("eight")}}),
		},
	}
	for _, row := range rows {
		if err := tbl.AddRowTyped(row); err != nil {
			t.Fatalf("add container union row: %v", err)
		}
	}

	for _, query := range []string{"sort xs", "sort p"} {
		t.Run(query, func(t *testing.T) {
			expectQueryErrContains(t, tbl, query, "union values are not orderable")
		})
	}
}

func TestUnionComparisonsRejectedOutsideFilter(t *testing.T) {
	tbl := unionRecordBranchTable(t, false)
	cases := []struct {
		name  string
		query string
	}{
		{"transform_self_eq", "transform eq = u == u"},
		{"transform_literal_eq", "transform eq = u == 1"},
		{"transform_if_condition", `transform s = if(u == u, "yes", "no")`},
		{"transform_struct_field", "transform wrapped = struct(eq = u == u)"},
		{"transform_list_element", "transform xs = list(u == u)"},
		{"reduce_first_self_eq", "group k | reduce eq = first(u) == first(u)"},
		{"reduce_first_literal_neq", `group k | reduce neq = first(u) != "x"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expectQueryErrContains(t, tbl, tc.query, "cannot compare union values")
		})
	}
}

func TestReduceCountRejectsArguments(t *testing.T) {
	cases := []string{
		"group city | reduce c = count(age)",
		"group city | reduce c = count(age) + 1",
	}
	for _, query := range cases {
		t.Run(query, func(t *testing.T) {
			expectQueryErrContains(t, usersTable(), query, "count() takes no arguments")
		})
	}
}

func runQueryExpectErr(t *testing.T, input *table.Table, query string) error {
	t.Helper()
	q, err := parser.Parse("test.csv | " + query)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	_, err = Execute(q, input, nil)
	return err
}

func expectQueryErrContains(t *testing.T, input *table.Table, query, substr string) {
	t.Helper()
	err := runQueryExpectErr(t, input, query)
	if err == nil {
		t.Fatalf("expected error containing %q", substr)
	}
	if !strings.Contains(err.Error(), substr) {
		t.Fatalf("expected error containing %q, got: %v", substr, err)
	}
}

func salesTable() *table.Table {
	t := table.NewTable([]string{"date", "quantity"})
	t.AddRow([]table.Value{table.StrVal("2024-01-15"), table.IntVal(10)})
	t.AddRow([]table.Value{table.StrVal("2024-02-20"), table.IntVal(5)})
	return t
}

func TestDatePartValidDate(t *testing.T) {
	result := runQuery(t, salesTable(), "transform y = year(date), m = month(date), d = day(date) | head 1")
	yIdx := result.ColIndex("y")
	mIdx := result.ColIndex("m")
	dIdx := result.ColIndex("d")
	if result.GetAt(0, yIdx).Int != 2024 {
		t.Errorf("expected year 2024, got %d", result.GetAt(0, yIdx).Int)
	}
	if result.GetAt(0, mIdx).Int != 1 {
		t.Errorf("expected month 1, got %d", result.GetAt(0, mIdx).Int)
	}
	if result.GetAt(0, dIdx).Int != 15 {
		t.Errorf("expected day 15, got %d", result.GetAt(0, dIdx).Int)
	}
}

func TestDatePartNullPropagation(t *testing.T) {
	tbl := table.NewTable([]string{"d"})
	tbl.AddRow([]table.Value{table.Null()})

	result := runQuery(t, tbl, "transform y = year(d)")
	if !result.GetAt(0, 1).IsNull() {
		t.Errorf("expected null for year(null), got %v", result.GetAt(0, 1).AsString())
	}
}

func TestDatePartErrorOnInt(t *testing.T) {
	expectQueryErrContains(t, salesTable(), "transform y = year(quantity)", "year() requires a string, got int")
}

func TestDatePartErrorOnString(t *testing.T) {
	tbl := table.NewTable([]string{"x"})
	tbl.AddRow([]table.Value{table.StrVal("notadate")})

	err := runQueryExpectErr(t, tbl, "transform y = year(x)")
	if err == nil {
		t.Fatal("expected error for year() on unparseable string")
	}
}

func TestDatePartErrorOnIntResult(t *testing.T) {
	// year(date) returns int; year(year(date)) should error
	expectQueryErrContains(t, salesTable(), "transform y = year(date) | transform yy = year(y)", "year() requires a string, got int")
}

func TestStringFuncsWrongTypeErrors(t *testing.T) {
	cases := []struct {
		name    string
		query   string
		wantErr string
	}{
		{"upper_int", "transform x = upper(age)", "upper() requires a string, got int"},
		{"lower_int", "transform x = lower(age)", "lower() requires a string, got int"},
		{"trim_int", "transform x = trim(age)", "trim() requires a string, got int"},
		{"substr_int", "transform x = substr(age, 0, 1)", "substr() requires a string, got int"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expectQueryErrContains(t, usersTable(), tc.query, tc.wantErr)
		})
	}
}

func TestArithmeticErrorOnStringTimesInt(t *testing.T) {
	err := runQueryExpectErr(t, usersTable(), "transform x = name * 2")
	if err == nil {
		t.Fatal("expected error for string * int")
	}
}

func TestArithmeticErrorOnIntPlusString(t *testing.T) {
	err := runQueryExpectErr(t, usersTable(), "transform x = age + name")
	if err == nil {
		t.Fatal("expected error for int + string")
	}
}

func TestLogicalErrorOnNonBool(t *testing.T) {
	err := runQueryExpectErr(t, usersTable(), "filter { age and city }")
	if err == nil {
		t.Fatal("expected error for 'and' on non-bool operands")
	}
}

func TestComparisonErrorOnTypeMismatch(t *testing.T) {
	// Records/lists are not comparable to scalars and must error clearly.
	err := runQueryExpectErr(t, nestedTable(), "filter { name > address }")
	if err == nil {
		t.Fatal("expected error for comparing string with record")
	}
}

func TestGroupWithCustomName(t *testing.T) {
	result := runQuery(t, usersTable(), "group city as entries | reduce entries total = sum(age) | remove entries | select city, total")
	if result.NumRows != 3 {
		t.Errorf("expected 3 rows, got %d", result.NumRows)
	}
}

// nestedTable creates a table with record-typed columns for testing dot-path operations.
func nestedTable() *table.Table {
	t := table.NewTable([]string{"name", "address"})
	t.AddRow([]table.Value{
		table.StrVal("Alice"),
		table.RecordVal([]table.RecordField{
			{Name: "city", Value: table.StrVal("New York")},
			{Name: "zip", Value: table.StrVal("10001")},
		}),
	})
	t.AddRow([]table.Value{
		table.StrVal("Bob"),
		table.RecordVal([]table.RecordField{
			{Name: "city", Value: table.StrVal("Los Angeles")},
			{Name: "zip", Value: table.StrVal("90001")},
		}),
	})
	t.AddRow([]table.Value{
		table.StrVal("Charlie"),
		table.RecordVal([]table.RecordField{
			{Name: "city", Value: table.StrVal("New York")},
			{Name: "zip", Value: table.StrVal("10002")},
		}),
	})
	return t
}

// optionalNestedTable has one row with a null parent record and one with a nested value.
// Mirrors testdata/nested_missing.json for unit-level nested missing-field coverage.
func optionalNestedTable() *table.Table {
	t := table.NewTable([]string{"name", "addr"})
	t.AddRow([]table.Value{
		table.StrVal("a"),
		table.Null(),
	})
	t.AddRow([]table.Value{
		table.StrVal("b"),
		table.RecordVal([]table.RecordField{
			{Name: "city", Value: table.StrVal("NY")},
		}),
	})
	return t
}

func TestSelectNullParentDotPath(t *testing.T) {
	result := runQuery(t, optionalNestedTable(), "select name, addr.city")
	if result.NumRows != 2 {
		t.Fatalf("expected 2 rows, got %d", result.NumRows)
	}
	cityIdx := result.ColIndex("addr_city")
	if !result.GetAt(0, cityIdx).IsNull() {
		t.Errorf("row 0 addr.city: expected null, got %v", result.GetAt(0, cityIdx))
	}
	if got := result.GetAt(1, cityIdx).Str; got != "NY" {
		t.Errorf("row 1 addr.city: expected NY, got %q", got)
	}
}

func TestFilterNullParentDotPathEquality(t *testing.T) {
	result := runQuery(t, optionalNestedTable(), `filter { addr.city == "NY" }`)
	if result.NumRows != 1 {
		t.Fatalf("expected 1 row, got %d", result.NumRows)
	}
	if got := result.GetAt(0, 0).Str; got != "b" {
		t.Errorf("expected name b, got %q", got)
	}
}

func TestFilterNullParentDotPathIsNull(t *testing.T) {
	result := runQuery(t, optionalNestedTable(), "filter { addr.city is null }")
	if result.NumRows != 1 {
		t.Fatalf("expected 1 row, got %d", result.NumRows)
	}
	if got := result.GetAt(0, 0).Str; got != "a" {
		t.Errorf("expected name a, got %q", got)
	}
}

func TestTransformNullParentDotPath(t *testing.T) {
	result := runQuery(t, optionalNestedTable(), "transform city = addr.city | select name, city")
	cityIdx := result.ColIndex("city")
	if !result.GetAt(0, cityIdx).IsNull() {
		t.Errorf("row 0 city: expected null, got %v", result.GetAt(0, cityIdx))
	}
	if got := result.GetAt(1, cityIdx).Str; got != "NY" {
		t.Errorf("row 1 city: expected NY, got %q", got)
	}
}

func TestGroupNullParentDotPath(t *testing.T) {
	result := runQuery(t, optionalNestedTable(), "group addr.city | reduce n = count() | remove grouped")
	if result.NumRows != 2 {
		t.Fatalf("expected 2 groups, got %d", result.NumRows)
	}
	nIdx := result.ColIndex("n")
	for i := 0; i < result.NumRows; i++ {
		key := result.GetAt(i, 0)
		n := result.GetAt(i, nIdx).Int
		if key.IsNull() && n != 1 {
			t.Errorf("null group: expected count 1, got %d", n)
		}
		if !key.IsNull() && key.Str == "NY" && n != 1 {
			t.Errorf("NY group: expected count 1, got %d", n)
		}
	}
}

func TestSortNullParentDotPath(t *testing.T) {
	result := runQuery(t, optionalNestedTable(), "sort addr.city | select name, addr.city")
	if result.NumRows != 2 {
		t.Fatalf("expected 2 rows, got %d", result.NumRows)
	}
	// null sorts last
	nameIdx := result.ColIndex("name")
	if got := result.GetAt(0, nameIdx).Str; got != "b" {
		t.Errorf("row 0: expected b (NY first), got %q", got)
	}
	if got := result.GetAt(1, nameIdx).Str; got != "a" {
		t.Errorf("row 1: expected a (null last), got %q", got)
	}
}

func TestDistinctNullParentDotPath(t *testing.T) {
	result := runQuery(t, optionalNestedTable(), "distinct addr.city")
	if result.NumRows != 2 {
		t.Fatalf("expected 2 distinct values (null + NY), got %d", result.NumRows)
	}
}

func TestNullParentDotPathPreservesTypeMismatchError(t *testing.T) {
	// String parent is not null — must still error when traversing into a scalar.
	err := runQueryExpectErr(t, optionalNestedTable(), "sort name.first")
	if err == nil {
		t.Fatal("expected error for dot path through string column")
	}
	if !strings.Contains(err.Error(), "sort \"name.first\"") {
		t.Errorf("expected full path in sort error, got: %v", err)
	}
}

func TestSelectDotPath(t *testing.T) {
	result := runQuery(t, nestedTable(), "select name, address.city")
	if len(result.Columns) != 2 {
		t.Fatalf("expected 2 columns, got %d", len(result.Columns))
	}
	if result.Columns[0] != "name" {
		t.Errorf("col 0: expected 'name', got %q", result.Columns[0])
	}
	if result.Columns[1] != "address_city" {
		t.Errorf("col 1: expected 'address_city', got %q", result.Columns[1])
	}
	if result.GetAt(0, 1).Str != "New York" {
		t.Errorf("row 0 city: expected 'New York', got %q", result.GetAt(0, 1).Str)
	}
}

func TestSelectDotPathDedup(t *testing.T) {
	// Create a table that already has an address_city column
	tbl := table.NewTable([]string{"address_city", "address"})
	tbl.AddRow([]table.Value{
		table.StrVal("existing"),
		table.RecordVal([]table.RecordField{
			{Name: "city", Value: table.StrVal("New York")},
		}),
	})
	result := runQuery(t, tbl, "select address_city, address.city")
	if result.Columns[0] != "address_city" {
		t.Errorf("col 0: expected 'address_city', got %q", result.Columns[0])
	}
	if result.Columns[1] != "address_city_2" {
		t.Errorf("col 1: expected 'address_city_2', got %q", result.Columns[1])
	}
}

func TestGroupDotPath(t *testing.T) {
	result := runQuery(t, nestedTable(), "group address.city")
	if len(result.Columns) != 2 {
		t.Fatalf("expected 2 columns, got %v", result.Columns)
	}
	if result.Columns[0] != "address_city" {
		t.Errorf("col 0: expected 'address_city', got %q", result.Columns[0])
	}
	if result.Columns[1] != "grouped" {
		t.Errorf("col 1: expected 'grouped', got %q", result.Columns[1])
	}
	// 2 groups: New York and Los Angeles
	if result.NumRows != 2 {
		t.Fatalf("expected 2 groups, got %d", result.NumRows)
	}
}

func TestGroupDotPathReduce(t *testing.T) {
	result := runQuery(t, nestedTable(), "group address.city | reduce n = count() | remove grouped")
	if len(result.Columns) != 2 {
		t.Fatalf("expected 2 columns (address_city, n), got %v", result.Columns)
	}
	// Find the New York group
	nIdx := result.ColIndex("n")
	for i := 0; i < result.NumRows; i++ {
		if result.GetAt(i, 0).Str == "New York" {
			if result.GetAt(i, nIdx).Int != 2 {
				t.Errorf("expected count 2 for New York, got %d", result.GetAt(i, nIdx).Int)
			}
		}
	}
}

func TestSortDotPath(t *testing.T) {
	result := runQuery(t, nestedTable(), "sort address.city, -name | select name, address.city")
	want := []string{"Bob", "Charlie", "Alice"}
	nameIdx := result.ColIndex("name")
	for i, w := range want {
		if got := result.GetAt(i, nameIdx).Str; got != w {
			t.Errorf("row %d: expected %q, got %q", i, w, got)
		}
	}
}

func TestSortDotPathSurfacesResolutionError(t *testing.T) {
	err := runQueryExpectErr(t, nestedTable(), "sort name.first")
	if err == nil {
		t.Fatal("expected error for invalid sort dot path")
	}
	if !strings.Contains(err.Error(), "sort \"name.first\"") {
		t.Errorf("expected full path in sort error, got: %v", err)
	}
}

func TestDistinctDotPath(t *testing.T) {
	result := runQuery(t, nestedTable(), "distinct address.city")
	if result.NumRows != 2 {
		t.Fatalf("expected 2 distinct cities, got %d: %s", result.NumRows, result.String())
	}
}

func TestDistinctDotPathSurfacesResolutionError(t *testing.T) {
	err := runQueryExpectErr(t, nestedTable(), "distinct name.first")
	if err == nil {
		t.Fatal("expected error for invalid distinct dot path")
	}
	if !strings.Contains(err.Error(), "distinct \"name.first\"") {
		t.Errorf("expected full path in distinct error, got: %v", err)
	}
}

func nestedStatsTable() *table.Table {
	t := table.NewTable([]string{"name", "address", "profile"})
	rows := []struct {
		name   string
		city   string
		score  float64
		logins int64
	}{
		{"Alice", "New York", 9.5, 42},
		{"Bob", "Los Angeles", 6.2, 7},
		{"Charlie", "New York", 0, 0},
	}
	for _, row := range rows {
		t.AddRow([]table.Value{
			table.StrVal(row.name),
			table.RecordVal([]table.RecordField{
				{Name: "city", Value: table.StrVal(row.city)},
			}),
			table.RecordVal([]table.RecordField{
				{Name: "stats", Value: table.RecordVal([]table.RecordField{
					{Name: "score", Value: table.FloatVal(row.score)},
					{Name: "logins", Value: table.IntVal(row.logins)},
				})},
			}),
		})
	}
	return t
}

func TestReduceAggregateDotPath(t *testing.T) {
	result := runQuery(t, nestedStatsTable(), "group address.city | reduce avg_score = avg(profile.stats.score), max_logins = max(profile.stats.logins), first_score = first(profile.stats.score) | remove grouped")

	cityIdx := result.ColIndex("address_city")
	avgIdx := result.ColIndex("avg_score")
	maxIdx := result.ColIndex("max_logins")
	firstIdx := result.ColIndex("first_score")

	for i := 0; i < result.NumRows; i++ {
		switch result.GetAt(i, cityIdx).Str {
		case "New York":
			if got := result.GetAt(i, avgIdx).Float; got != 4.75 {
				t.Errorf("New York avg_score: expected 4.75, got %v", got)
			}
			if got := result.GetAt(i, maxIdx).Int; got != 42 {
				t.Errorf("New York max_logins: expected 42, got %d", got)
			}
			if got := result.GetAt(i, firstIdx).Float; got != 9.5 {
				t.Errorf("New York first_score: expected 9.5, got %v", got)
			}
		case "Los Angeles":
			if got := result.GetAt(i, avgIdx).Float; got != 6.2 {
				t.Errorf("Los Angeles avg_score: expected 6.2, got %v", got)
			}
			if got := result.GetAt(i, maxIdx).Int; got != 7 {
				t.Errorf("Los Angeles max_logins: expected 7, got %d", got)
			}
		default:
			t.Errorf("unexpected city %q", result.GetAt(i, cityIdx).Str)
		}
	}
}

func TestReduceMinLastAndAggregateExpression(t *testing.T) {
	result := runQuery(t, usersTable(), "group city | reduce min_age = min(age), last_name = last(name), age_span = max(age) - min(age) | remove grouped | sort city")

	cityIdx := result.ColIndex("city")
	minIdx := result.ColIndex("min_age")
	lastIdx := result.ColIndex("last_name")
	spanIdx := result.ColIndex("age_span")

	want := map[string]struct {
		minAge int64
		last   string
		span   int64
	}{
		"LA": {minAge: 22, last: "Eve", span: 3},
		"NY": {minAge: 30, last: "Frank", span: 10},
		"SF": {minAge: 28, last: "Diana", span: 0},
	}

	if result.NumRows != len(want) {
		t.Fatalf("expected %d rows, got %d", len(want), result.NumRows)
	}
	for i := 0; i < result.NumRows; i++ {
		city := result.GetAt(i, cityIdx).Str
		w, ok := want[city]
		if !ok {
			t.Fatalf("unexpected city %q", city)
		}
		if got := result.GetAt(i, minIdx); got.Type != table.TypeInt || got.Int != w.minAge {
			t.Fatalf("%s min_age: want %d, got %v", city, w.minAge, got)
		}
		if got := result.GetAt(i, lastIdx); got.Type != table.TypeString || got.Str != w.last {
			t.Fatalf("%s last_name: want %q, got %v", city, w.last, got)
		}
		if got := result.GetAt(i, spanIdx); got.Type != table.TypeInt || got.Int != w.span {
			t.Fatalf("%s age_span: want %d, got %v", city, w.span, got)
		}
	}
}

func TestPlannedUnaryBranches(t *testing.T) {
	tbl := table.NewTable([]string{"flag", "n", "f", "missing"})
	tbl.AddRow([]table.Value{table.BoolVal(false), table.IntVal(4), table.FloatVal(1.5), table.Null()})

	cases := []struct {
		name string
		expr string
		want table.Value
	}{
		{
			name: "not",
			expr: "not flag",
			want: table.BoolVal(true),
		},
		{
			name: "negative_int",
			expr: "-n",
			want: table.IntVal(-4),
		},
		{
			name: "negative_float",
			expr: "-f",
			want: table.FloatVal(-1.5),
		},
		{
			name: "negative_null",
			expr: "-missing",
			want: table.Null(),
		},
	}

	for _, tc := range cases {
		result := runQuery(t, tbl, "transform out = "+tc.expr+" | select out")
		got := result.GetAt(0, 0)
		if !table.Equal(got, tc.want) {
			t.Fatalf("%s: want %v, got %v", tc.name, tc.want, got)
		}
	}

	if err := runQueryExpectErr(t, tbl, "transform out = -flag"); err == nil {
		t.Fatal("expected bool negation to fail")
	} else if !strings.Contains(err.Error(), "numeric") {
		t.Fatalf("expected numeric unary error, got %v", err)
	}
}

func TestCmpResultCoversAllOperators(t *testing.T) {
	cases := []struct {
		op   string
		cmp  int
		want bool
	}{
		{op: "==", cmp: 0, want: true},
		{op: "!=", cmp: 1, want: true},
		{op: "<", cmp: -1, want: true},
		{op: ">", cmp: 1, want: true},
		{op: "<=", cmp: 0, want: true},
		{op: ">=", cmp: 0, want: true},
		{op: "bad", cmp: 0, want: false},
	}
	for _, tc := range cases {
		if got := cmpResult(tc.op, tc.cmp); got != tc.want {
			t.Fatalf("cmpResult(%q, %d): want %v, got %v", tc.op, tc.cmp, tc.want, got)
		}
	}
}

func TestPlannedArithmeticBranches(t *testing.T) {
	tbl := table.NewTable([]string{"dummy"})
	tbl.AddRow([]table.Value{table.IntVal(0)})

	result := runQuery(t, tbl, `transform out = "a" + "b" | select out`)
	if got := result.GetAt(0, 0); got.Type != table.TypeString || got.Str != "ab" {
		t.Fatalf("string concat: want ab, got %v", got)
	}

	result = runQuery(t, tbl, `transform out = 4 / 2 | select out`)
	if got := result.GetAt(0, 0); got.Type != table.TypeFloat || got.Float != 2 {
		t.Fatalf("whole int division: want float 2, got %v", got)
	}

	result = runQuery(t, tbl, `transform out = 5 / 2 | select out`)
	if got := result.GetAt(0, 0); got.Type != table.TypeFloat || got.Float != 2.5 {
		t.Fatalf("fractional int division: want 2.5, got %v", got)
	}

	result = runQuery(t, tbl, `transform out = 5 / 0 | select out`)
	if got := result.GetAt(0, 0); !got.IsNull() {
		t.Fatalf("division by zero: want null, got %v", got)
	}

	if err := runQueryExpectErr(t, tbl, `transform out = "a" + 1`); err == nil {
		t.Fatal("expected mixed string/int arithmetic to fail")
	}
}

func TestCompareValuesFallbackOrdering(t *testing.T) {
	if got := compareValues(table.Null(), table.Null()); got != 0 {
		t.Fatalf("null/null comparison: want 0, got %d", got)
	}
	if got := compareValues(table.Null(), table.IntVal(1)); got <= 0 {
		t.Fatalf("null should sort after non-null, got %d", got)
	}
	if got := compareValues(table.IntVal(1), table.Null()); got >= 0 {
		t.Fatalf("non-null should sort before null, got %d", got)
	}
	if got := compareValues(table.IntVal(1), table.StrVal("1")); got >= 0 {
		t.Fatalf("different types should fall back to type-name ordering, got %d", got)
	}
	if got := compareValues(table.ListVal([]table.Value{table.IntVal(1)}), table.ListVal([]table.Value{table.IntVal(2)})); got >= 0 {
		t.Fatalf("same non-orderable type should fall back to canonical key ordering, got %d", got)
	}
}

func ordersTable() *table.Table {
	t := table.NewTable([]string{"order_id", "user_name", "product", "amount"})
	t.AddRow([]table.Value{table.IntVal(1), table.StrVal("Alice"), table.StrVal("Widget"), table.IntVal(10)})
	t.AddRow([]table.Value{table.IntVal(2), table.StrVal("Alice"), table.StrVal("Gadget"), table.IntVal(25)})
	t.AddRow([]table.Value{table.IntVal(3), table.StrVal("Bob"), table.StrVal("Widget"), table.IntVal(15)})
	t.AddRow([]table.Value{table.IntVal(4), table.StrVal("Charlie"), table.StrVal("Widget"), table.IntVal(20)})
	t.AddRow([]table.Value{table.IntVal(5), table.StrVal("Zara"), table.StrVal("Thing"), table.IntVal(99)})
	return t
}

func runJoinQuery(t *testing.T, left *table.Table, joinClause string) *table.Table {
	t.Helper()
	q, err := parser.Parse("users.csv | join " + joinClause)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	load := func(filename string, _ ast.LoadOptions) (*table.Table, error) {
		if filename == "orders.csv" {
			return ordersTable(), nil
		}
		return nil, fmt.Errorf("unknown file %q", filename)
	}
	result, err := Execute(q, left, load)
	if err != nil {
		t.Fatalf("exec error: %v", err)
	}
	return result
}

func TestJoinInner(t *testing.T) {
	result := runJoinQuery(t, usersTable(), `orders.csv on name == user_name`)
	if result.NumRows != 4 {
		t.Fatalf("expected 4 rows, got %d", result.NumRows)
	}
	if result.ColIndex("product") < 0 {
		t.Fatal("expected product column")
	}
}

func TestJoinLeft(t *testing.T) {
	result := runJoinQuery(t, usersTable(), `left orders.csv on name == user_name`)
	if result.NumRows != 7 {
		t.Fatalf("expected 7 rows, got %d", result.NumRows)
	}
	productIdx := result.ColIndex("product")
	nullProducts := 0
	for i := 0; i < result.NumRows; i++ {
		if result.GetAt(i, productIdx).IsNull() {
			nullProducts++
		}
	}
	if nullProducts != 3 {
		t.Errorf("expected 3 rows with null product, got %d", nullProducts)
	}
}

func TestJoinRight(t *testing.T) {
	result := runJoinQuery(t, usersTable(), `right orders.csv on name == user_name`)
	if result.NumRows != 5 {
		t.Fatalf("expected 5 rows, got %d", result.NumRows)
	}
	ageIdx := result.ColIndex("age")
	zaraFound := false
	for i := 0; i < result.NumRows; i++ {
		if result.GetAt(i, 0).Str == "Zara" {
			zaraFound = true
			if !result.GetAt(i, ageIdx).IsNull() {
				t.Error("expected null age for unmatched right row Zara")
			}
		}
	}
	if !zaraFound {
		t.Error("expected row for Zara from right table")
	}
}

func TestJoinFull(t *testing.T) {
	result := runJoinQuery(t, usersTable(), `full orders.csv on name == user_name`)
	if result.NumRows != 8 {
		t.Fatalf("expected 8 rows, got %d", result.NumRows)
	}
}

func TestJoinBasename(t *testing.T) {
	cases := []struct{ in, want string }{
		{"data/order-items.csv", "order_items"},
		{"ORDERS.csv", "orders"},
		{"MyOrders.csv", "my_orders"},
		{"v2Data.json", "v2_data"},
		{"---.csv", "right"},
		{"data[1].csv", "data_1"},
		{"orders/part-*.csv", "part"},
		{"orders/*.csv", "orders"},
		{"logs/**/*.csv", "logs"},
		{"*.csv", "right"},
	}
	for _, c := range cases {
		if got := joinBasename(c.in); got != c.want {
			t.Errorf("joinBasename(%q): expected %q, got %q", c.in, c.want, got)
		}
	}
}

func TestJoinMultiKey(t *testing.T) {
	left := table.NewTable([]string{"city", "dept", "lead"})
	left.AddRow([]table.Value{table.StrVal("NY"), table.StrVal("sales"), table.StrVal("Alice")})
	left.AddRow([]table.Value{table.StrVal("NY"), table.StrVal("eng"), table.StrVal("Bob")})
	left.AddRow([]table.Value{table.StrVal("LA"), table.StrVal("sales"), table.StrVal("Carol")})

	right := table.NewTable([]string{"city", "dept", "budget"})
	right.AddRow([]table.Value{table.StrVal("NY"), table.StrVal("sales"), table.IntVal(100)})
	right.AddRow([]table.Value{table.StrVal("LA"), table.StrVal("eng"), table.IntVal(50)})

	q, err := parser.Parse("left.csv | join right.csv on city and dept")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	load := func(string, ast.LoadOptions) (*table.Table, error) { return right, nil }
	result, err := Execute(q, left, load)
	if err != nil {
		t.Fatalf("exec error: %v", err)
	}
	if result.NumRows != 1 {
		t.Fatalf("expected 1 row (NY/sales), got %d", result.NumRows)
	}
	if result.Get(0, "lead").Str != "Alice" || result.Get(0, "budget").Int != 100 {
		t.Errorf("wrong joined row: %s", result.String())
	}
}

func TestJoinUsesExactStructuralKeys(t *testing.T) {
	left := table.NewTable([]string{"id", "name"})
	left.AddRow([]table.Value{table.IntVal(1), table.StrVal("Alice")})

	right := table.NewTable([]string{"user_id", "note"})
	right.AddRow([]table.Value{table.StrVal("1"), table.StrVal("string-key")})

	q, err := parser.Parse("left.csv | join right.csv on id == user_id")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	load := func(string, ast.LoadOptions) (*table.Table, error) { return right, nil }
	result, err := Execute(q, left, load)
	if err != nil {
		t.Fatalf("exec error: %v", err)
	}
	if result.NumRows != 0 {
		t.Fatalf("expected no join match for int vs string key, got %d rows: %s", result.NumRows, result.String())
	}
}

func TestJoinConfigurationAndTopLevelKeyErrors(t *testing.T) {
	left := table.NewTable([]string{"id"})
	left.AddRow([]table.Value{table.IntVal(1)})
	right := table.NewTable([]string{"id"})
	right.AddRow([]table.Value{table.IntVal(1)})

	t.Run("loader_not_configured", func(t *testing.T) {
		q, err := parser.Parse("left.csv | join right.csv on id")
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}
		_, err = Execute(q, left, nil)
		if err == nil || !strings.Contains(err.Error(), "loader not configured") {
			t.Fatalf("expected loader configuration error, got %v", err)
		}
	})

	t.Run("stdin_join_source_rejected", func(t *testing.T) {
		_, err := execJoin(&ast.JoinOp{
			Filename: "-",
			Keys:     []ast.JoinKey{{Left: []string{"id"}, Right: []string{"id"}}},
		}, left, func(string, ast.LoadOptions) (*table.Table, error) { return right, nil })
		if err == nil || !strings.Contains(err.Error(), "stdin is not supported") {
			t.Fatalf("expected stdin join source error, got %v", err)
		}
	})

	t.Run("load_error_wrapped", func(t *testing.T) {
		q, err := parser.Parse("left.csv | join right.csv on id")
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}
		_, err = Execute(q, left, func(filename string, _ ast.LoadOptions) (*table.Table, error) {
			return nil, fmt.Errorf("boom %s", filename)
		})
		if err == nil || !strings.Contains(err.Error(), `load "right.csv"`) || !strings.Contains(err.Error(), "boom") {
			t.Fatalf("expected wrapped join load error, got %v", err)
		}
	})

	for _, tc := range []struct {
		name string
		on   string
		want string
	}{
		{name: "missing_left_top_level_key", on: "missing == id", want: `left join key column "missing" not found`},
		{name: "missing_right_top_level_key", on: "id == missing", want: `right join key column "missing" not found`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q, err := parser.Parse("left.csv | join right.csv on " + tc.on)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}
			_, err = Execute(q, left, func(string, ast.LoadOptions) (*table.Table, error) { return right, nil })
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q, got %v", tc.want, err)
			}
		})
	}
}

// TestJoinKeepsRightColumnCollidingWithLeftKeyName guards against dropping a
// right column merely because its name matches a left join-key name. Only the
// actual right join-key column should be dropped; collisions must be renamed.
func TestJoinKeepsRightColumnCollidingWithLeftKeyName(t *testing.T) {
	left := table.NewTable([]string{"id", "name"})
	left.AddRow([]table.Value{table.IntVal(1), table.StrVal("Alice")})
	left.AddRow([]table.Value{table.IntVal(2), table.StrVal("Bob")})

	right := table.NewTable([]string{"id", "customer_id", "note"})
	right.AddRow([]table.Value{table.IntVal(99), table.IntVal(1), table.StrVal("hello")})
	right.AddRow([]table.Value{table.IntVal(98), table.IntVal(2), table.StrVal("world")})

	q, err := parser.Parse("left.csv | join right.csv on id == customer_id")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	load := func(string, ast.LoadOptions) (*table.Table, error) { return right, nil }
	result, err := Execute(q, left, load)
	if err != nil {
		t.Fatalf("exec error: %v", err)
	}

	ridIdx := result.ColIndex("right_id")
	if ridIdx < 0 {
		t.Fatalf("expected right_id column (renamed collision), got %v", result.Columns)
	}
	if result.GetAt(0, ridIdx).Int != 99 || result.GetAt(1, ridIdx).Int != 98 {
		t.Errorf("right id values lost: got %v, %v", result.GetAt(0, ridIdx), result.GetAt(1, ridIdx))
	}
	if result.ColIndex("customer_id") >= 0 {
		t.Errorf("right join-key column customer_id should be dropped, got %v", result.Columns)
	}
}

// TestJoinSurfacesKeyPathError ensures a structurally invalid dot-path key
// returns an error instead of silently dropping rows -- for every join kind.
func TestJoinSurfacesKeyPathError(t *testing.T) {
	for _, kind := range []string{"", "left ", "right ", "full "} {
		left := table.NewTable([]string{"name"})
		left.AddRow([]table.Value{table.StrVal("Alice")})

		right := table.NewTable([]string{"name", "x"})
		right.AddRow([]table.Value{table.StrVal("Alice"), table.IntVal(1)})

		// name.sub treats a string column as a record -> per-row resolution error.
		q, err := parser.Parse("left.csv | join " + kind + "right.csv on name.sub == name")
		if err != nil {
			t.Fatalf("kind %q: parse error: %v", kind, err)
		}
		load := func(string, ast.LoadOptions) (*table.Table, error) { return right, nil }
		if _, err := Execute(q, left, load); err == nil {
			t.Errorf("kind %q: expected error for invalid dot-path join key, got nil", kind)
		}
	}
}

// TestJoinSurfacesRightKeyPathError covers the same for a bad right-side key.
func TestJoinSurfacesRightKeyPathError(t *testing.T) {
	left := table.NewTable([]string{"name"})
	left.AddRow([]table.Value{table.StrVal("Alice")})

	right := table.NewTable([]string{"name"})
	right.AddRow([]table.Value{table.StrVal("Alice")})

	q, err := parser.Parse("left.csv | join right.csv on name == name.sub")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	load := func(string, ast.LoadOptions) (*table.Table, error) { return right, nil }
	if _, err := Execute(q, left, load); err == nil {
		t.Fatal("expected error for invalid right dot-path join key, got nil")
	}
}

func TestJoinRejectsMissingNestedKeyFieldFromSchema(t *testing.T) {
	recordSchema := &table.TypeDescriptor{
		Kind: table.TypeRecord,
		Fields: []table.FieldDescriptor{
			{Name: "city", Type: &table.TypeDescriptor{Kind: table.TypeString}},
		},
	}
	left := table.NewTableWithSchemas([]string{"address", "name"}, []*table.TypeDescriptor{
		recordSchema,
		{Kind: table.TypeString},
	})
	right := table.NewTableWithSchemas([]string{"address", "note"}, []*table.TypeDescriptor{
		recordSchema,
		{Kind: table.TypeString},
	})
	load := func(string, ast.LoadOptions) (*table.Table, error) { return right, nil }

	cases := []struct {
		name string
		kind string
		on   string
	}{
		{name: "inner_left_missing_field", on: "address.missing == address.city"},
		{name: "left_left_missing_field", kind: "left ", on: "address.missing == address.city"},
		{name: "right_left_missing_field", kind: "right ", on: "address.missing == address.city"},
		{name: "full_left_missing_field", kind: "full ", on: "address.missing == address.city"},
		{name: "right_missing_field", on: "address.city == address.missing"},
		{name: "shorthand_missing_field", on: "address.missing"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q, err := parser.Parse("left.csv | join " + tc.kind + "right.csv on " + tc.on)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}
			_, err = Execute(q, left, load)
			if err == nil {
				t.Fatalf("expected missing nested join key error for %q", tc.on)
			}
			msg := err.Error()
			for _, want := range []string{"join", "missing", "not found"} {
				if !strings.Contains(msg, want) {
					t.Fatalf("expected error to contain %q, got %v", want, err)
				}
			}
		})
	}
}

// TestJoinDotPathKeyDoesNotAliasExistingColumn guards against a dot-path key
// whose flattened name collides with an unrelated left column: the key must
// get its own suffixed column, and the original column must stay untouched.
func TestJoinDotPathKeyDoesNotAliasExistingColumn(t *testing.T) {
	left := table.NewTable([]string{"address", "address_city"})
	left.AddRow([]table.Value{
		table.RecordVal([]table.RecordField{{Name: "city", Value: table.StrVal("NY")}}),
		table.StrVal("UNRELATED"),
	})

	right := table.NewTable([]string{"city", "pop"})
	right.AddRow([]table.Value{table.StrVal("NY"), table.IntVal(8)})
	right.AddRow([]table.Value{table.StrVal("LA"), table.IntVal(4)})

	q, err := parser.Parse("left.json | join full right.csv on address.city == city")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	load := func(string, ast.LoadOptions) (*table.Table, error) { return right, nil }
	result, err := Execute(q, left, load)
	if err != nil {
		t.Fatalf("exec error: %v", err)
	}
	if result.NumRows != 2 {
		t.Fatalf("expected 2 rows (NY matched, LA unmatched), got %d", result.NumRows)
	}
	keyIdx := result.ColIndex("address_city_2")
	if keyIdx < 0 {
		t.Fatalf("expected suffixed key column address_city_2, got %v", result.Columns)
	}
	if got := result.Get(0, "address_city").Str; got != "UNRELATED" {
		t.Errorf("unrelated column overwritten: got %q", got)
	}
	if got := result.GetAt(0, keyIdx).Str; got != "NY" {
		t.Errorf("expected key value NY, got %q", got)
	}
	if !result.Get(1, "address_city").IsNull() {
		t.Errorf("unmatched right row must not write into unrelated left column")
	}
	if got := result.GetAt(1, keyIdx).Str; got != "LA" {
		t.Errorf("expected right key LA in key column, got %q", got)
	}
}
