package parser

import (
	"testing"

	"github.com/razeghi71/dq/ast"
)

func TestParseSimple(t *testing.T) {
	q, err := Parse("users.csv | head 10")
	if err != nil {
		t.Fatal(err)
	}
	if q.Source.Filename != "users.csv" {
		t.Errorf("expected 'users.csv', got %q", q.Source.Filename)
	}
	if len(q.Ops) != 1 {
		t.Fatalf("expected 1 op, got %d", len(q.Ops))
	}
	head, ok := q.Ops[0].(*ast.HeadOp)
	if !ok {
		t.Fatalf("expected HeadOp, got %T", q.Ops[0])
	}
	if head.N != 10 {
		t.Errorf("expected 10, got %d", head.N)
	}
}

func TestParsePipeline(t *testing.T) {
	q, err := Parse("users.csv | filter { age > 20 } | select name age | head 5")
	if err != nil {
		t.Fatal(err)
	}
	if len(q.Ops) != 3 {
		t.Fatalf("expected 3 ops, got %d", len(q.Ops))
	}
	if _, ok := q.Ops[0].(*ast.FilterOp); !ok {
		t.Errorf("op[0]: expected FilterOp, got %T", q.Ops[0])
	}
	if _, ok := q.Ops[1].(*ast.SelectOp); !ok {
		t.Errorf("op[1]: expected SelectOp, got %T", q.Ops[1])
	}
	if _, ok := q.Ops[2].(*ast.HeadOp); !ok {
		t.Errorf("op[2]: expected HeadOp, got %T", q.Ops[2])
	}
}

func TestParseFilter(t *testing.T) {
	q, err := Parse(`users.csv | filter { age > 20 and city == "NY" }`)
	if err != nil {
		t.Fatal(err)
	}
	f := q.Ops[0].(*ast.FilterOp)
	bin, ok := f.Expr.(*ast.BinaryExpr)
	if !ok {
		t.Fatalf("expected BinaryExpr, got %T", f.Expr)
	}
	if bin.Op != "and" {
		t.Errorf("expected 'and', got %q", bin.Op)
	}
}

func TestParseGroup(t *testing.T) {
	q, err := Parse("users.csv | group city department as entries")
	if err != nil {
		t.Fatal(err)
	}
	g := q.Ops[0].(*ast.GroupOp)
	if len(g.Columns) != 2 {
		t.Fatalf("expected 2 group columns, got %d", len(g.Columns))
	}
	if len(g.Columns[0]) != 1 || g.Columns[0][0] != "city" || len(g.Columns[1]) != 1 || g.Columns[1][0] != "department" {
		t.Errorf("expected [[city], [department]], got %v", g.Columns)
	}
	if g.NestedName != "entries" {
		t.Errorf("expected nested name 'entries', got %q", g.NestedName)
	}
}

func TestParseGroupDefaultNested(t *testing.T) {
	q, err := Parse("users.csv | group city")
	if err != nil {
		t.Fatal(err)
	}
	g := q.Ops[0].(*ast.GroupOp)
	if g.NestedName != "grouped" {
		t.Errorf("expected default nested name 'grouped', got %q", g.NestedName)
	}
}

func TestParseTransform(t *testing.T) {
	q, err := Parse("users.csv | transform age2 = age * 2, city = upper(city)")
	if err != nil {
		t.Fatal(err)
	}
	tr := q.Ops[0].(*ast.TransformOp)
	if len(tr.Assignments) != 2 {
		t.Fatalf("expected 2 assignments, got %d", len(tr.Assignments))
	}
	if tr.Assignments[0].Column != "age2" {
		t.Errorf("expected 'age2', got %q", tr.Assignments[0].Column)
	}
	if tr.Assignments[1].Column != "city" {
		t.Errorf("expected 'city', got %q", tr.Assignments[1].Column)
	}
}

func TestParseReduce(t *testing.T) {
	q, err := Parse("users.csv | group name | reduce max_age = max(age), count = count()")
	if err != nil {
		t.Fatal(err)
	}
	r := q.Ops[1].(*ast.ReduceOp)
	if r.NestedName != "grouped" {
		t.Errorf("expected nested name 'grouped', got %q", r.NestedName)
	}
	if len(r.Assignments) != 2 {
		t.Fatalf("expected 2 assignments, got %d", len(r.Assignments))
	}
}

func TestParseReduceWithNestedName(t *testing.T) {
	q, err := Parse("users.csv | group name as entries | reduce entries max_age = max(age)")
	if err != nil {
		t.Fatal(err)
	}
	r := q.Ops[1].(*ast.ReduceOp)
	if r.NestedName != "entries" {
		t.Errorf("expected nested name 'entries', got %q", r.NestedName)
	}
}

func TestParseRename(t *testing.T) {
	q, err := Parse("users.csv | rename `first name` first_name `last name` last_name")
	if err != nil {
		t.Fatal(err)
	}
	r := q.Ops[0].(*ast.RenameOp)
	if len(r.Pairs) != 2 {
		t.Fatalf("expected 2 rename pairs, got %d", len(r.Pairs))
	}
	if r.Pairs[0].Old != "first name" || r.Pairs[0].New != "first_name" {
		t.Errorf("pair[0]: expected first name -> first_name, got %v -> %v", r.Pairs[0].Old, r.Pairs[0].New)
	}
}

func TestParseIsNull(t *testing.T) {
	q, err := Parse("users.csv | filter { age is not null }")
	if err != nil {
		t.Fatal(err)
	}
	f := q.Ops[0].(*ast.FilterOp)
	isn, ok := f.Expr.(*ast.IsNullExpr)
	if !ok {
		t.Fatalf("expected IsNullExpr, got %T", f.Expr)
	}
	if !isn.Negated {
		t.Error("expected negated (is not null)")
	}
}

func TestParseIsNullNotNegated(t *testing.T) {
	q, err := Parse("users.csv | filter { city is null }")
	if err != nil {
		t.Fatal(err)
	}
	f := q.Ops[0].(*ast.FilterOp)
	isn, ok := f.Expr.(*ast.IsNullExpr)
	if !ok {
		t.Fatalf("expected IsNullExpr, got %T", f.Expr)
	}
	if isn.Negated {
		t.Error("expected not negated (is null)")
	}
}

func TestParsePathFilename(t *testing.T) {
	q, err := Parse("path/to/data.csv | head 5")
	if err != nil {
		t.Fatal(err)
	}
	if q.Source.Filename != "path/to/data.csv" {
		t.Errorf("expected 'path/to/data.csv', got %q", q.Source.Filename)
	}
}

func TestParseDistinct(t *testing.T) {
	q, err := Parse("users.csv | distinct city age")
	if err != nil {
		t.Fatal(err)
	}
	d := q.Ops[0].(*ast.DistinctOp)
	if len(d.Columns) != 2 {
		t.Fatalf("expected 2 columns, got %d", len(d.Columns))
	}
}

func TestParseSortMixedDirections(t *testing.T) {
	q, err := Parse("users.csv | sort a -b c")
	if err != nil {
		t.Fatal(err)
	}
	s := q.Ops[0].(*ast.SortOp)
	if len(s.Keys) != 3 {
		t.Fatalf("expected 3 keys, got %d", len(s.Keys))
	}
	want := []struct {
		col  string
		desc bool
	}{{"a", false}, {"b", true}, {"c", false}}
	for i, w := range want {
		if s.Keys[i].Path[0] != w.col || s.Keys[i].Desc != w.desc {
			t.Errorf("key %d: expected (%q, desc=%v), got (%q, desc=%v)", i, w.col, w.desc, s.Keys[i].Path[0], s.Keys[i].Desc)
		}
	}
}

func TestParseFullQuery(t *testing.T) {
	q, err := Parse(`sales.csv | filter { year(date) == 2024 } | transform revenue = coalesce(quantity, 0) * coalesce(price, 0) | group category city | reduce total_revenue = sum(revenue), order_count = count() | remove grouped | filter { total_revenue > 1000 } | sort -total_revenue | head 3 | select category city total_revenue order_count`)
	if err != nil {
		t.Fatal(err)
	}
	if len(q.Ops) != 9 {
		t.Errorf("expected 9 ops, got %d", len(q.Ops))
	}
}

func TestParseFilenameLeadingNumber(t *testing.T) {
	q, err := Parse("123data.csv | head 10")
	if err != nil {
		t.Fatal(err)
	}
	if q.Source.Filename != "123data.csv" {
		t.Errorf("expected '123data.csv', got %q", q.Source.Filename)
	}
	head := q.Ops[0].(*ast.HeadOp)
	if head.N != 10 {
		t.Errorf("expected 10, got %d", head.N)
	}
}

func TestParseFilenameHyphen(t *testing.T) {
	q, err := Parse("my-file.csv | head 5")
	if err != nil {
		t.Fatal(err)
	}
	if q.Source.Filename != "my-file.csv" {
		t.Errorf("expected 'my-file.csv', got %q", q.Source.Filename)
	}
}

func TestParseFilenameAbsolutePath(t *testing.T) {
	q, err := Parse("/absolute/path.csv | head 5")
	if err != nil {
		t.Fatal(err)
	}
	if q.Source.Filename != "/absolute/path.csv" {
		t.Errorf("expected '/absolute/path.csv', got %q", q.Source.Filename)
	}
}

func TestParseFilenameMultipleDots(t *testing.T) {
	q, err := Parse("file.tar.gz | head 5")
	if err != nil {
		t.Fatal(err)
	}
	if q.Source.Filename != "file.tar.gz" {
		t.Errorf("expected 'file.tar.gz', got %q", q.Source.Filename)
	}
}

func TestParseFilenameSpecialChars(t *testing.T) {
	q, err := Parse("data@2024#v2.csv | head 5")
	if err != nil {
		t.Fatal(err)
	}
	if q.Source.Filename != "data@2024#v2.csv" {
		t.Errorf("expected 'data@2024#v2.csv', got %q", q.Source.Filename)
	}
}

func TestParseFilenameOnly(t *testing.T) {
	q, err := Parse("data.csv")
	if err != nil {
		t.Fatal(err)
	}
	if q.Source.Filename != "data.csv" {
		t.Errorf("expected 'data.csv', got %q", q.Source.Filename)
	}
	if len(q.Ops) != 0 {
		t.Errorf("expected 0 ops, got %d", len(q.Ops))
	}
}

func TestParseFilenameQuotedWithPipe(t *testing.T) {
	q, err := Parse(`"file|weird.csv" | head 5`)
	if err != nil {
		t.Fatal(err)
	}
	if q.Source.Filename != "file|weird.csv" {
		t.Errorf("expected 'file|weird.csv', got %q", q.Source.Filename)
	}
}

func TestParseHeadDefault(t *testing.T) {
	q, err := Parse("users.csv | head")
	if err != nil {
		t.Fatal(err)
	}
	head := q.Ops[0].(*ast.HeadOp)
	if head.N != 10 {
		t.Errorf("expected default 10, got %d", head.N)
	}
}

func TestParseTailDefault(t *testing.T) {
	q, err := Parse("users.csv | tail")
	if err != nil {
		t.Fatal(err)
	}
	tail := q.Ops[0].(*ast.TailOp)
	if tail.N != 10 {
		t.Errorf("expected default 10, got %d", tail.N)
	}
}

func TestParseHeadDefaultTailExplicit(t *testing.T) {
	q, err := Parse("users.csv | head | tail 3")
	if err != nil {
		t.Fatal(err)
	}
	if len(q.Ops) != 2 {
		t.Fatalf("expected 2 ops, got %d", len(q.Ops))
	}
	head := q.Ops[0].(*ast.HeadOp)
	if head.N != 10 {
		t.Errorf("expected head default 10, got %d", head.N)
	}
	tail := q.Ops[1].(*ast.TailOp)
	if tail.N != 3 {
		t.Errorf("expected tail 3, got %d", tail.N)
	}
}

func TestParseDashOnly(t *testing.T) {
	q, err := Parse("-")
	if err != nil {
		t.Fatal(err)
	}
	if q.Source.Filename != "-" {
		t.Errorf("expected '-', got %q", q.Source.Filename)
	}
	if len(q.Ops) != 0 {
		t.Errorf("expected no ops, got %d", len(q.Ops))
	}
}

func TestParseStdinSource(t *testing.T) {
	q, err := Parse("- | head 10")
	if err != nil {
		t.Fatal(err)
	}
	if q.Source.Filename != "-" {
		t.Errorf("expected '-', got %q", q.Source.Filename)
	}
	head, ok := q.Ops[0].(*ast.HeadOp)
	if !ok || head.N != 10 {
		t.Errorf("expected head 10, got %v", q.Ops[0])
	}
}

func TestParseStdinNotAlias(t *testing.T) {
	q, err := Parse("stdin | head 10")
	if err != nil {
		t.Fatal(err)
	}
	if q.Source.Filename != "stdin" {
		t.Errorf("expected 'stdin' as file path, got %q", q.Source.Filename)
	}
}

func TestParseFilenameEmpty(t *testing.T) {
	_, err := Parse("| head 10")
	if err == nil {
		t.Fatal("expected error for empty filename")
	}
}

func TestParseDotPathExpr(t *testing.T) {
	q, err := Parse(`users.csv | filter { address.city == "Chicago" }`)
	if err != nil {
		t.Fatal(err)
	}
	f := q.Ops[0].(*ast.FilterOp)
	bin, ok := f.Expr.(*ast.BinaryExpr)
	if !ok {
		t.Fatalf("expected BinaryExpr, got %T", f.Expr)
	}
	col, ok := bin.Left.(*ast.ColumnExpr)
	if !ok {
		t.Fatalf("expected ColumnExpr on left, got %T", bin.Left)
	}
	if len(col.Path) != 2 || col.Path[0] != "address" || col.Path[1] != "city" {
		t.Errorf("expected path [address city], got %v", col.Path)
	}
}

func TestParseDotPathDeep(t *testing.T) {
	q, err := Parse("users.csv | filter { profile.stats.logins > 10 }")
	if err != nil {
		t.Fatal(err)
	}
	f := q.Ops[0].(*ast.FilterOp)
	bin := f.Expr.(*ast.BinaryExpr)
	col := bin.Left.(*ast.ColumnExpr)
	if len(col.Path) != 3 {
		t.Fatalf("expected 3-segment path, got %v", col.Path)
	}
	if col.Path[0] != "profile" || col.Path[1] != "stats" || col.Path[2] != "logins" {
		t.Errorf("unexpected path: %v", col.Path)
	}
}

func TestParseDotPathInSelect(t *testing.T) {
	q, err := Parse("users.csv | select name address.city profile.stats.logins")
	if err != nil {
		t.Fatal(err)
	}
	s := q.Ops[0].(*ast.SelectOp)
	if len(s.Columns) != 3 {
		t.Fatalf("expected 3 columns, got %d", len(s.Columns))
	}
	// name -> ["name"]
	if len(s.Columns[0]) != 1 || s.Columns[0][0] != "name" {
		t.Errorf("col[0]: expected [name], got %v", s.Columns[0])
	}
	// address.city -> ["address", "city"]
	if len(s.Columns[1]) != 2 || s.Columns[1][0] != "address" || s.Columns[1][1] != "city" {
		t.Errorf("col[1]: expected [address city], got %v", s.Columns[1])
	}
	// profile.stats.logins -> ["profile", "stats", "logins"]
	if len(s.Columns[2]) != 3 {
		t.Errorf("col[2]: expected 3 segments, got %v", s.Columns[2])
	}
}

func TestParseDotPathInGroup(t *testing.T) {
	q, err := Parse("users.csv | group address.city")
	if err != nil {
		t.Fatal(err)
	}
	g := q.Ops[0].(*ast.GroupOp)
	if len(g.Columns) != 1 {
		t.Fatalf("expected 1 group column, got %d", len(g.Columns))
	}
	if len(g.Columns[0]) != 2 || g.Columns[0][0] != "address" || g.Columns[0][1] != "city" {
		t.Errorf("expected [address city], got %v", g.Columns[0])
	}
	if g.NestedName != "grouped" {
		t.Errorf("expected default nested name, got %q", g.NestedName)
	}
}

func TestParseDotPathInGroupWithAs(t *testing.T) {
	q, err := Parse("users.csv | group address.city as entries")
	if err != nil {
		t.Fatal(err)
	}
	g := q.Ops[0].(*ast.GroupOp)
	if len(g.Columns[0]) != 2 || g.Columns[0][0] != "address" || g.Columns[0][1] != "city" {
		t.Errorf("expected [address city], got %v", g.Columns[0])
	}
	if g.NestedName != "entries" {
		t.Errorf("expected 'entries', got %q", g.NestedName)
	}
}

func TestParseRemoveRejectsDotPath(t *testing.T) {
	_, err := Parse("users.csv | remove address.city")
	if err == nil {
		t.Fatal("expected error for dot path in remove")
	}
}
