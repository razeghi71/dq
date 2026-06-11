package parser

import (
	"strings"
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

func TestParseIsNullCombinesWithAnd(t *testing.T) {
	q, err := Parse(`users.csv | filter { age is not null and city == "NY" }`)
	if err != nil {
		t.Fatal(err)
	}
	f := q.Ops[0].(*ast.FilterOp)
	bin, ok := f.Expr.(*ast.BinaryExpr)
	if !ok || bin.Op != "and" {
		t.Fatalf("expected BinaryExpr(and), got %T", f.Expr)
	}
	isn, ok := bin.Left.(*ast.IsNullExpr)
	if !ok {
		t.Fatalf("expected left IsNullExpr, got %T", bin.Left)
	}
	if !isn.Negated {
		t.Error("expected is not null (negated)")
	}
	col, ok := isn.Operand.(*ast.ColumnExpr)
	if !ok || len(col.Path) != 1 || col.Path[0] != "age" {
		t.Fatalf("expected age column, got %T %v", isn.Operand, isn.Operand)
	}
	comp, ok := bin.Right.(*ast.BinaryExpr)
	if !ok || comp.Op != "==" {
		t.Fatalf("expected right == comparison, got %T", bin.Right)
	}
}

func TestParseIsNullCombinesWithOr(t *testing.T) {
	q, err := Parse("users.csv | filter { city is null or age > 30 }")
	if err != nil {
		t.Fatal(err)
	}
	f := q.Ops[0].(*ast.FilterOp)
	bin, ok := f.Expr.(*ast.BinaryExpr)
	if !ok || bin.Op != "or" {
		t.Fatalf("expected BinaryExpr(or), got %T", f.Expr)
	}
	isn, ok := bin.Left.(*ast.IsNullExpr)
	if !ok || isn.Negated {
		t.Fatalf("expected left is null, got %T negated=%v", bin.Left, isn != nil && isn.Negated)
	}
	comp, ok := bin.Right.(*ast.BinaryExpr)
	if !ok || comp.Op != ">" {
		t.Fatalf("expected right > comparison, got %T", bin.Right)
	}
}

func TestParseIsNullBothSidesWithAnd(t *testing.T) {
	q, err := Parse("users.csv | filter { age is null and city is null }")
	if err != nil {
		t.Fatal(err)
	}
	f := q.Ops[0].(*ast.FilterOp)
	bin, ok := f.Expr.(*ast.BinaryExpr)
	if !ok || bin.Op != "and" {
		t.Fatalf("expected BinaryExpr(and), got %T", f.Expr)
	}
	if _, ok := bin.Left.(*ast.IsNullExpr); !ok {
		t.Fatalf("expected left IsNullExpr, got %T", bin.Left)
	}
	if _, ok := bin.Right.(*ast.IsNullExpr); !ok {
		t.Fatalf("expected right IsNullExpr, got %T", bin.Right)
	}
}

func TestParseNotAgeIsNull(t *testing.T) {
	q, err := Parse("users.csv | filter { not age is null }")
	if err != nil {
		t.Fatal(err)
	}
	f := q.Ops[0].(*ast.FilterOp)
	unary, ok := f.Expr.(*ast.UnaryExpr)
	if !ok || unary.Op != "not" {
		t.Fatalf("expected UnaryExpr(not), got %T", f.Expr)
	}
	isn, ok := unary.Operand.(*ast.IsNullExpr)
	if !ok || isn.Negated {
		t.Fatalf("expected not (age is null), got %T negated=%v", unary.Operand, isn != nil && isn.Negated)
	}
	col, ok := isn.Operand.(*ast.ColumnExpr)
	if !ok || len(col.Path) != 1 || col.Path[0] != "age" {
		t.Fatalf("expected age column inside is null, got %T", isn.Operand)
	}
}

func TestParseIsNullOnCompoundExpr(t *testing.T) {
	cases := []struct {
		name  string
		query string
	}{
		{"parens", "users.csv | filter { (age + 1) is null }"},
		{"no_parens", "users.csv | filter { age + 1 is null }"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q, err := Parse(tc.query)
			if err != nil {
				t.Fatal(err)
			}
			f := q.Ops[0].(*ast.FilterOp)
			isn, ok := f.Expr.(*ast.IsNullExpr)
			if !ok {
				t.Fatalf("expected IsNullExpr, got %T", f.Expr)
			}
			if _, ok := isn.Operand.(*ast.BinaryExpr); !ok {
				t.Fatalf("expected is null over arithmetic expr, got operand %T", isn.Operand)
			}
		})
	}
}

func TestParseNullEqualityRejected(t *testing.T) {
	cases := []struct {
		name    string
		query   string
		wantMsg string
	}{
		{"eq_null", "users.csv | filter { age == null }", `use "age is null" instead of "age == null"`},
		{"neq_null", "users.csv | filter { age != null }", `use "age is not null" instead of "age != null"`},
		{"null_eq_column", "users.csv | filter { null == age }", `use "age is null" instead of "age == null"`},
		{"dot_path", "users.csv | filter { address.city == null }", `use "address.city is null"`},
		{"inside_or", "users.csv | filter { name == null or age > 20 }", `use "name is null"`},
		{"null_eq_null", "users.csv | filter { null == null }", `use "is null" / "is not null" for null checks, not ==`},
		{"in_transform", "users.csv | transform flag = age == null", `use "age is null"`},
		{"in_if", `users.csv | transform x = if(age == null, 0, age)`, `use "age is null"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(tc.query)
			if err == nil {
				t.Fatalf("expected parse error for %q", tc.query)
			}
			if !strings.Contains(err.Error(), tc.wantMsg) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.wantMsg)
			}
		})
	}
}

func TestParseNullLiteralStillAllowed(t *testing.T) {
	valid := []string{
		"users.csv | filter { age is null }",
		"users.csv | filter { age is not null }",
		"users.csv | transform x = coalesce(age, null)",
		`users.csv | transform x = if(age is null, 0, age)`,
		"users.csv | filter { age > 20 }",
		`users.csv | filter { city == "NY" }`,
	}
	for _, q := range valid {
		if _, err := Parse(q); err != nil {
			t.Errorf("expected %q to parse, got error: %v", q, err)
		}
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

func TestParseJoinInner(t *testing.T) {
	q, err := Parse("users.csv | join orders.csv on name == user_name")
	if err != nil {
		t.Fatal(err)
	}
	j := q.Ops[0].(*ast.JoinOp)
	if j.Kind != "inner" {
		t.Errorf("expected inner, got %q", j.Kind)
	}
	if j.Filename != "orders.csv" {
		t.Errorf("expected orders.csv, got %q", j.Filename)
	}
	if len(j.Keys) != 1 || len(j.Keys[0].Left) != 1 || j.Keys[0].Left[0] != "name" {
		t.Errorf("expected left key name, got %v", j.Keys[0].Left)
	}
	if len(j.Keys[0].Right) != 1 || j.Keys[0].Right[0] != "user_name" {
		t.Errorf("expected right key user_name, got %v", j.Keys[0].Right)
	}
}

func TestParseJoinLeft(t *testing.T) {
	q, err := Parse("users.csv | join left orders.csv on name == user_name")
	if err != nil {
		t.Fatal(err)
	}
	j := q.Ops[0].(*ast.JoinOp)
	if j.Kind != "left" {
		t.Errorf("expected left, got %q", j.Kind)
	}
}

func TestParseJoinShorthandKey(t *testing.T) {
	q, err := Parse("a.csv | join b.csv on id")
	if err != nil {
		t.Fatal(err)
	}
	j := q.Ops[0].(*ast.JoinOp)
	if len(j.Keys[0].Left) != 1 || j.Keys[0].Left[0] != "id" {
		t.Errorf("expected left id, got %v", j.Keys[0].Left)
	}
	if len(j.Keys[0].Right) != 1 || j.Keys[0].Right[0] != "id" {
		t.Errorf("expected right id, got %v", j.Keys[0].Right)
	}
}

func TestParseJoinMultiKey(t *testing.T) {
	q, err := Parse("a.csv | join b.csv on id == customer_id and region == region")
	if err != nil {
		t.Fatal(err)
	}
	j := q.Ops[0].(*ast.JoinOp)
	if len(j.Keys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(j.Keys))
	}
}

func TestParseJoinFilenameIsKindWord(t *testing.T) {
	q, err := Parse("users.csv | join left on id")
	if err != nil {
		t.Fatal(err)
	}
	j := q.Ops[0].(*ast.JoinOp)
	if j.Kind != "inner" {
		t.Errorf("expected inner, got %q", j.Kind)
	}
	if j.Filename != "left" {
		t.Errorf("expected filename left, got %q", j.Filename)
	}
}

func TestParseJoinRejectsStdin(t *testing.T) {
	_, err := Parse("users.csv | join - on id")
	if err == nil {
		t.Fatal("expected error for stdin join source")
	}
}

// TestParseJoinAfterOtherOp guards the lexer-buffer handover into ScanSource:
// join must read the right filename correctly when preceded by another op.
func TestParseJoinAfterOtherOp(t *testing.T) {
	q, err := Parse("users.csv | head 5 | join orders.csv on id")
	if err != nil {
		t.Fatal(err)
	}
	if len(q.Ops) != 2 {
		t.Fatalf("expected 2 ops, got %d", len(q.Ops))
	}
	j, ok := q.Ops[1].(*ast.JoinOp)
	if !ok {
		t.Fatalf("expected JoinOp, got %T", q.Ops[1])
	}
	if j.Filename != "orders.csv" {
		t.Errorf("expected orders.csv, got %q", j.Filename)
	}
}

func TestParseGlobSourceFilename(t *testing.T) {
	q, err := Parse("logs/**/*.csv | head 5")
	if err != nil {
		t.Fatal(err)
	}
	if q.Source.Filename != "logs/**/*.csv" {
		t.Errorf("expected logs/**/*.csv, got %q", q.Source.Filename)
	}

	q, err = Parse("users.csv | join left orders/*.csv on id")
	if err != nil {
		t.Fatal(err)
	}
	j := q.Ops[0].(*ast.JoinOp)
	if j.Filename != "orders/*.csv" {
		t.Errorf("expected orders/*.csv, got %q", j.Filename)
	}
}
