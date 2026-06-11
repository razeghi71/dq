package engine

import (
	"fmt"
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
	result := runQuery(t, usersTable(), "sort city -age")
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
	assertColumns(t, result, []string{"first_name", "age", "city"})
}

func TestRenameMultiplePairsAreSimultaneous(t *testing.T) {
	result := runQuery(t, usersTable(), "rename name age age name")
	assertColumns(t, result, []string{"age", "name", "city"})

	if got := result.GetAt(0, 0).Str; got != "Alice" {
		t.Errorf("expected first column value to remain Alice, got %q", got)
	}
	if got := result.GetAt(0, 1).Int; got != 30 {
		t.Errorf("expected second column value to remain 30, got %d", got)
	}
}

func TestRenameNoOpToSameName(t *testing.T) {
	result := runQuery(t, usersTable(), "rename name name")
	assertColumns(t, result, []string{"name", "age", "city"})
}

func TestRenameChained(t *testing.T) {
	result := runQuery(t, usersTable(), "rename name username | rename city location")
	assertColumns(t, result, []string{"username", "age", "location"})
}

func TestRenameMultipleValidPairsInOneOp(t *testing.T) {
	result := runQuery(t, usersTable(), "rename name first_name city location")
	assertColumns(t, result, []string{"first_name", "age", "location"})
}

func TestRenameColumnNotFound(t *testing.T) {
	err := runQueryExpectErr(t, usersTable(), "rename missing foo")
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
		{"target_exists", "rename name age", `duplicate column name "age"`},
		{"target_exists_other_column", "rename city name", `duplicate column name "name"`},
		{"pairs_share_target", "rename name x city x", `duplicate column name "x"`},
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
	err := runQueryExpectErr(t, usersTable(), "rename name first_name name full_name")
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
		{"contains", `filter { contains(name, "a") }`, []string{"Charlie", "Diana", "Frank"}},
		{"starts_with", `filter { starts_with(name, "C") }`, []string{"Charlie"}},
		{"ends_with", `filter { ends_with(name, "e") }`, []string{"Alice", "Charlie", "Eve"}},
		{"matches", `filter { matches(name, "^[AB]") }`, []string{"Alice", "Bob"}},
		{"matches_unanchored", `filter { matches(name, "li") }`, []string{"Alice", "Charlie"}},
		{"matches_anchored_full", `filter { matches(name, "^Alice$") }`, []string{"Alice"}},
		{"negative", `filter { contains(name, "zzz") }`, nil},
		{"not_contains", `filter { not contains(name, "a") }`, []string{"Alice", "Bob", "Eve"}},
		{"combined", `filter { contains(name, "a") and city == "NY" }`, []string{"Charlie", "Frank"}},
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
		{"contains", `transform hit = contains(city, "Y")`, true},
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

func TestStringPredicatesCoercion(t *testing.T) {
	result := runQuery(t, usersTable(), `transform hit = contains(age, "3") | select name hit`)
	hitIdx := result.ColIndex("hit")
	nameIdx := result.ColIndex("name")
	for i := 0; i < result.NumRows; i++ {
		name := result.GetAt(i, nameIdx).Str
		hit, ok := result.GetAt(i, hitIdx).AsBool()
		want := name == "Alice" || name == "Charlie" // ages 30, 35 stringify to "30", "35"
		if !ok || hit != want {
			t.Errorf("%s: expected hit=%v, got ok=%v val=%v", name, want, ok, hit)
		}
	}
}

func TestStringPredicatesNullPropagation(t *testing.T) {
	tbl := table.NewTable([]string{"s", "needle"})
	tbl.AddRow([]table.Value{table.Null(), table.StrVal("a")})
	tbl.AddRow([]table.Value{table.StrVal("abc"), table.Null()})
	for _, fn := range []string{"contains", "starts_with", "ends_with", "matches"} {
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
	result := runQuery(t, tbl, `filter { contains(message, "ERROR") } | select name`)
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
		{"contains_1_arg", `filter { contains(name) }`},
		{"contains_3_args", `filter { contains(name, "a", "b") }`},
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

func runQueryExpectErr(t *testing.T, input *table.Table, query string) error {
	t.Helper()
	q, err := parser.Parse("test.csv | " + query)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	_, err = Execute(q, input, nil)
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
	// Records/lists are not comparable to scalars and must error clearly.
	err := runQueryExpectErr(t, nestedTable(), "filter { name > address }")
	if err == nil {
		t.Fatal("expected error for comparing string with record")
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

// optionalNestedTable has one row with a null parent record and one with a nested value.
// Mirrors testdata/nested_missing.json for unit-level TDD on ticket 001.
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
	result := runQuery(t, optionalNestedTable(), "select name addr.city")
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
	result := runQuery(t, optionalNestedTable(), "transform city = addr.city | select name city")
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
	result := runQuery(t, optionalNestedTable(), "sort addr.city | select name addr.city")
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
	// String parent is not null — must still error after ticket 001 fix.
	err := runQueryExpectErr(t, optionalNestedTable(), "sort name.first")
	if err == nil {
		t.Fatal("expected error for dot path through string column")
	}
	if !strings.Contains(err.Error(), "sort \"name.first\"") {
		t.Errorf("expected full path in sort error, got: %v", err)
	}
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

func TestSortDotPath(t *testing.T) {
	result := runQuery(t, nestedTable(), "sort address.city -name | select name address.city")
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
	load := func(filename string) (*table.Table, error) {
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
	load := func(string) (*table.Table, error) { return right, nil }
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
	load := func(string) (*table.Table, error) { return right, nil }
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
		load := func(string) (*table.Table, error) { return right, nil }
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
	load := func(string) (*table.Table, error) { return right, nil }
	if _, err := Execute(q, left, load); err == nil {
		t.Fatal("expected error for invalid right dot-path join key, got nil")
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
	load := func(string) (*table.Table, error) { return right, nil }
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
