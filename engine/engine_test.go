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
	if len(result.Rows) != 3 {
		t.Errorf("expected 3 rows, got %d", len(result.Rows))
	}
	if result.Rows[0].Values[0].Str != "Alice" {
		t.Errorf("expected first row to be Alice")
	}
}

func TestTail(t *testing.T) {
	result := runQuery(t, usersTable(), "tail 2")
	if len(result.Rows) != 2 {
		t.Errorf("expected 2 rows, got %d", len(result.Rows))
	}
	if result.Rows[0].Values[0].Str != "Eve" {
		t.Errorf("expected first row to be Eve, got %s", result.Rows[0].Values[0].Str)
	}
}

func TestSortAsc(t *testing.T) {
	result := runQuery(t, usersTable(), "sorta age")
	if result.Rows[0].Values[1].Int != 22 {
		t.Errorf("expected first age to be 22, got %d", result.Rows[0].Values[1].Int)
	}
	if result.Rows[5].Values[1].Int != 40 {
		t.Errorf("expected last age to be 40, got %d", result.Rows[5].Values[1].Int)
	}
}

func TestSortDesc(t *testing.T) {
	result := runQuery(t, usersTable(), "sortd age")
	if result.Rows[0].Values[1].Int != 40 {
		t.Errorf("expected first age to be 40, got %d", result.Rows[0].Values[1].Int)
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
	if len(result.Rows) != 2 {
		t.Errorf("expected 2 rows (Charlie, Frank), got %d", len(result.Rows))
	}
}

func TestFilterAnd(t *testing.T) {
	result := runQuery(t, usersTable(), `filter { age > 25 and city == "NY" }`)
	if len(result.Rows) != 3 {
		t.Errorf("expected 3 rows, got %d", len(result.Rows))
	}
}

func TestCount(t *testing.T) {
	result := runQuery(t, usersTable(), "count")
	if len(result.Rows) != 1 || len(result.Columns) != 1 {
		t.Fatal("count should return 1x1 table")
	}
	if result.Rows[0].Values[0].Int != 6 {
		t.Errorf("expected 6, got %d", result.Rows[0].Values[0].Int)
	}
}

func TestDistinct(t *testing.T) {
	result := runQuery(t, usersTable(), "distinct city")
	if len(result.Rows) != 3 {
		t.Errorf("expected 3 distinct cities, got %d", len(result.Rows))
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
	if result.Rows[0].Values[3].Int != 60 {
		t.Errorf("expected 60, got %d", result.Rows[0].Values[3].Int)
	}
}

func TestGroupReduce(t *testing.T) {
	result := runQuery(t, usersTable(), "group city | reduce total = sum(age), n = count() | remove grouped")
	if len(result.Columns) != 3 {
		t.Fatalf("expected 3 columns (city, total, n), got %d: %v", len(result.Columns), result.Columns)
	}
	// NY: 30+35+40=105, count=3
	nyIdx := -1
	for i, r := range result.Rows {
		if r.Values[0].Str == "NY" {
			nyIdx = i
		}
	}
	if nyIdx < 0 {
		t.Fatal("NY group not found")
	}
	totalIdx := result.ColIndex("total")
	nIdx := result.ColIndex("n")
	if result.Rows[nyIdx].Values[totalIdx].Int != 105 {
		t.Errorf("expected NY total=105, got %d", result.Rows[nyIdx].Values[totalIdx].Int)
	}
	if result.Rows[nyIdx].Values[nIdx].Int != 3 {
		t.Errorf("expected NY count=3, got %d", result.Rows[nyIdx].Values[nIdx].Int)
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
	if !result.Rows[0].Values[2].IsNull() {
		t.Errorf("expected null from 10 * null, got %v", result.Rows[0].Values[2].AsString())
	}
}

func TestCoalesce(t *testing.T) {
	tbl := table.NewTable([]string{"a", "b"})
	tbl.AddRow([]table.Value{table.Null(), table.IntVal(42)})

	result := runQuery(t, tbl, "transform c = coalesce(a, b)")
	if result.Rows[0].Values[2].Int != 42 {
		t.Errorf("expected 42, got %v", result.Rows[0].Values[2].AsString())
	}
}

func TestEvalExpr(t *testing.T) {
	tbl := table.NewTable([]string{"x"})
	tbl.AddRow([]table.Value{table.IntVal(5)})
	row := &tbl.Rows[0]
	ctx := &EvalContext{Table: tbl, Row: row}

	// Test: x + 3 * 2 should be 5 + 6 = 11 (not 16)
	expr := &ast.BinaryExpr{
		Op:   "+",
		Left: &ast.ColumnExpr{Name: "x"},
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
	if len(result.Rows) != 1 {
		t.Errorf("expected 1 row, got %d", len(result.Rows))
	}

	result2 := runQuery(t, tbl, "filter { a is not null }")
	if len(result2.Rows) != 1 {
		t.Errorf("expected 1 row, got %d", len(result2.Rows))
	}
}

func TestIfFunction(t *testing.T) {
	result := runQuery(t, usersTable(), `transform label = if(age > 30, "old", "young") | select name label`)
	// Alice(30) -> young, Charlie(35) -> old
	if result.Rows[0].Values[1].Str != "young" {
		t.Errorf("expected 'young' for Alice, got %q", result.Rows[0].Values[1].Str)
	}
	if result.Rows[2].Values[1].Str != "old" {
		t.Errorf("expected 'old' for Charlie, got %q", result.Rows[2].Values[1].Str)
	}
}

func TestUpperLower(t *testing.T) {
	result := runQuery(t, usersTable(), `transform up = upper(city), lo = lower(name) | select up lo | head 1`)
	if result.Rows[0].Values[0].Str != "NY" {
		t.Errorf("expected 'NY', got %q", result.Rows[0].Values[0].Str)
	}
	if result.Rows[0].Values[1].Str != "alice" {
		t.Errorf("expected 'alice', got %q", result.Rows[0].Values[1].Str)
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
	row := result.Rows[0]
	yIdx := result.ColIndex("y")
	mIdx := result.ColIndex("m")
	dIdx := result.ColIndex("d")
	if row.Values[yIdx].Int != 2024 {
		t.Errorf("expected year 2024, got %d", row.Values[yIdx].Int)
	}
	if row.Values[mIdx].Int != 1 {
		t.Errorf("expected month 1, got %d", row.Values[mIdx].Int)
	}
	if row.Values[dIdx].Int != 15 {
		t.Errorf("expected day 15, got %d", row.Values[dIdx].Int)
	}
}

func TestDatePartNullPropagation(t *testing.T) {
	tbl := table.NewTable([]string{"d"})
	tbl.AddRow([]table.Value{table.Null()})

	result := runQuery(t, tbl, "transform y = year(d)")
	if !result.Rows[0].Values[1].IsNull() {
		t.Errorf("expected null for year(null), got %v", result.Rows[0].Values[1].AsString())
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
	if result.Rows[0].Values[3].Str != "30" {
		t.Errorf("expected '30', got %q", result.Rows[0].Values[3].Str)
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
	if len(result.Rows) != 3 {
		t.Errorf("expected 3 rows, got %d", len(result.Rows))
	}
}
