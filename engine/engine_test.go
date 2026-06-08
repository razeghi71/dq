package engine

import (
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
	result, err := Execute(q, input)
	if err != nil {
		t.Fatalf("exec error: %v", err)
	}
	return result
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
	result := runQuery(t, usersTable(), "sorta age")
	if result.GetAt(0, 1).Int != 22 {
		t.Errorf("expected first age to be 22, got %d", result.GetAt(0, 1).Int)
	}
	if result.GetAt(5, 1).Int != 40 {
		t.Errorf("expected last age to be 40, got %d", result.GetAt(5, 1).Int)
	}
}

func TestSortDesc(t *testing.T) {
	result := runQuery(t, usersTable(), "sortd age")
	if result.GetAt(0, 1).Int != 40 {
		t.Errorf("expected first age to be 40, got %d", result.GetAt(0, 1).Int)
	}
}

func TestSelect(t *testing.T) {
	result := runQuery(t, usersTable(), "select name city")
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

func TestDistinct(t *testing.T) {
	result := runQuery(t, usersTable(), "distinct city")
	if result.NumRows != 3 {
		t.Errorf("expected 3 distinct cities, got %d", result.NumRows)
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
	result := runQuery(t, usersTable(), "rename name first_name")
	if result.Columns[0] != "first_name" {
		t.Errorf("expected 'first_name', got %q", result.Columns[0])
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

func TestEvalExpr(t *testing.T) {
	tbl := table.NewTable([]string{"x"})
	tbl.AddRow([]table.Value{table.IntVal(5)})
	ctx := &EvalContext{Table: tbl, RowIdx: 0}

	// Test: x + 3 * 2 should be 5 + 6 = 11 (not 16)
	expr := &ast.BinaryExpr{
		Op:   "+",
		Left: &ast.ColumnExpr{Path: []string{"x"}},
		Right: &ast.BinaryExpr{
			Op:    "*",
			Left:  &ast.LiteralExpr{Kind: "int", Int: 3},
			Right: &ast.LiteralExpr{Kind: "int", Int: 2},
		},
	}
	val, err := Eval(expr, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if val.Int != 11 {
		t.Errorf("expected 11, got %d", val.Int)
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

func TestIfFunction(t *testing.T) {
	result := runQuery(t, usersTable(), `transform label = if(age > 30, "old", "young") | select name label`)
	// Alice(30) -> young, Charlie(35) -> old
	if result.GetAt(0, 1).Str != "young" {
		t.Errorf("expected 'young' for Alice, got %q", result.GetAt(0, 1).Str)
	}
	if result.GetAt(2, 1).Str != "old" {
		t.Errorf("expected 'old' for Charlie, got %q", result.GetAt(2, 1).Str)
	}
}

func TestUpperLower(t *testing.T) {
	result := runQuery(t, usersTable(), `transform up = upper(city), lo = lower(name) | select up lo | head 1`)
	if result.GetAt(0, 0).Str != "NY" {
		t.Errorf("expected 'NY', got %q", result.GetAt(0, 0).Str)
	}
	if result.GetAt(0, 1).Str != "alice" {
		t.Errorf("expected 'alice', got %q", result.GetAt(0, 1).Str)
	}
}

func runQueryExpectErr(t *testing.T, input *table.Table, query string) error {
	t.Helper()
	q, err := parser.Parse("test.csv | " + query)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	_, err = Execute(q, input)
	return err
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
	err := runQueryExpectErr(t, salesTable(), "transform y = year(quantity)")
	if err == nil {
		t.Fatal("expected error for year(quantity) on int column")
	}
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
	err := runQueryExpectErr(t, salesTable(), "transform y = year(date) | transform yy = year(y)")
	if err == nil {
		t.Fatal("expected error for year() on int result of year()")
	}
}

func TestStringFuncsCoerceInt(t *testing.T) {
	result := runQuery(t, usersTable(), "transform x = upper(age) | head 1")
	if result.GetAt(0, 3).Str != "30" {
		t.Errorf("expected '30', got %q", result.GetAt(0, 3).Str)
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
	err := runQueryExpectErr(t, usersTable(), "filter { age > name }")
	if err == nil {
		t.Fatal("expected error for comparing int with string")
	}
}

func TestGroupWithCustomName(t *testing.T) {
	result := runQuery(t, usersTable(), "group city as entries | reduce entries total = sum(age) | remove entries | select city total")
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

func TestSelectDotPath(t *testing.T) {
	result := runQuery(t, nestedTable(), "select name address.city")
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
	result := runQuery(t, tbl, "select address_city address.city")
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
