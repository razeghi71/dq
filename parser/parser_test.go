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
	q, err := Parse("users.csv | filter { age > 20 } | select name, age | head 5")
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
	q, err := Parse("users.csv | group city, department as entries")
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

func TestParseStructExpr(t *testing.T) {
	q, err := Parse("users.csv | transform rec = struct(a = 1, b = name, nested = struct(`weird name` = null, `and` = true))")
	if err != nil {
		t.Fatal(err)
	}
	tr := q.Ops[0].(*ast.TransformOp)
	st, ok := tr.Assignments[0].Expr.(*ast.StructExpr)
	if !ok {
		t.Fatalf("expected StructExpr, got %T", tr.Assignments[0].Expr)
	}
	if len(st.Fields) != 3 {
		t.Fatalf("expected 3 fields, got %d", len(st.Fields))
	}
	if st.Fields[0].Name != "a" || st.Fields[1].Name != "b" || st.Fields[2].Name != "nested" {
		t.Fatalf("unexpected field names: %#v", st.Fields)
	}
	nested, ok := st.Fields[2].Expr.(*ast.StructExpr)
	if !ok {
		t.Fatalf("expected nested StructExpr, got %T", st.Fields[2].Expr)
	}
	if len(nested.Fields) != 2 || nested.Fields[0].Name != "weird name" || nested.Fields[1].Name != "and" {
		t.Fatalf("unexpected nested fields: %#v", nested.Fields)
	}
}

func TestParseStructExprEmpty(t *testing.T) {
	q, err := Parse("users.csv | transform rec = struct()")
	if err != nil {
		t.Fatal(err)
	}
	tr := q.Ops[0].(*ast.TransformOp)
	st, ok := tr.Assignments[0].Expr.(*ast.StructExpr)
	if !ok {
		t.Fatalf("expected StructExpr, got %T", tr.Assignments[0].Expr)
	}
	if len(st.Fields) != 0 {
		t.Fatalf("expected empty struct, got %#v", st.Fields)
	}
}

func TestParseStructExprErrors(t *testing.T) {
	cases := []struct {
		name    string
		query   string
		wantMsg string
	}{
		{"duplicate", "users.csv | transform rec = struct(a = 1, a = 2)", `duplicate field "a"`},
		{"positional", "users.csv | transform rec = struct(a, b)", "expected '=' after field"},
		{"trailing_comma", "users.csv | transform rec = struct(a = 1,)", "expected field name after ','"},
		{"missing_comma", "users.csv | transform rec = struct(a = 1 b = 2)", "expected )"},
		{"unclosed", "users.csv | transform rec = struct(a = 1", "expected )"},
		{"missing_field_name", "users.csv | transform rec = struct(= 1)", "expected field name"},
		{"keyword_field_name", "users.csv | transform rec = struct(and = 1)", "expected field name"},
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

func TestParseStructColumnReferenceStillAllowed(t *testing.T) {
	q, err := Parse("users.csv | transform x = struct")
	if err != nil {
		t.Fatal(err)
	}
	tr := q.Ops[0].(*ast.TransformOp)
	col, ok := tr.Assignments[0].Expr.(*ast.ColumnExpr)
	if !ok {
		t.Fatalf("expected ColumnExpr, got %T", tr.Assignments[0].Expr)
	}
	if len(col.Path) != 1 || col.Path[0] != "struct" {
		t.Fatalf("unexpected column path: %v", col.Path)
	}
}

func TestParseListExpr(t *testing.T) {
	q, err := Parse(`users.csv | transform xs = list(1, null, name, upper(city), struct(a = age))`)
	if err != nil {
		t.Fatal(err)
	}
	tr := q.Ops[0].(*ast.TransformOp)
	ls, ok := tr.Assignments[0].Expr.(*ast.ListExpr)
	if !ok {
		t.Fatalf("expected ListExpr, got %T", tr.Assignments[0].Expr)
	}
	if len(ls.Elements) != 5 {
		t.Fatalf("expected 5 list elements, got %d", len(ls.Elements))
	}
	if lit, ok := ls.Elements[0].(*ast.LiteralExpr); !ok || lit.Kind != "int" || lit.Int != 1 {
		t.Fatalf("element 0: expected int literal 1, got %#v", ls.Elements[0])
	}
	if lit, ok := ls.Elements[1].(*ast.LiteralExpr); !ok || lit.Kind != "null" {
		t.Fatalf("element 1: expected null literal, got %#v", ls.Elements[1])
	}
	if col, ok := ls.Elements[2].(*ast.ColumnExpr); !ok || len(col.Path) != 1 || col.Path[0] != "name" {
		t.Fatalf("element 2: expected name column, got %#v", ls.Elements[2])
	}
	if fn, ok := ls.Elements[3].(*ast.FuncCallExpr); !ok || fn.Name != "upper" || len(fn.Args) != 1 {
		t.Fatalf("element 3: expected upper(city), got %#v", ls.Elements[3])
	}
	st, ok := ls.Elements[4].(*ast.StructExpr)
	if !ok {
		t.Fatalf("element 4: expected StructExpr, got %T", ls.Elements[4])
	}
	if len(st.Fields) != 1 || st.Fields[0].Name != "a" {
		t.Fatalf("element 4: unexpected struct fields %#v", st.Fields)
	}
}

func TestParseListExprEmpty(t *testing.T) {
	q, err := Parse("users.csv | transform xs = list()")
	if err != nil {
		t.Fatal(err)
	}
	tr := q.Ops[0].(*ast.TransformOp)
	ls, ok := tr.Assignments[0].Expr.(*ast.ListExpr)
	if !ok {
		t.Fatalf("expected ListExpr, got %T", tr.Assignments[0].Expr)
	}
	if len(ls.Elements) != 0 {
		t.Fatalf("expected empty list, got %#v", ls.Elements)
	}
}

func TestParseListExprCaseInsensitiveConstructor(t *testing.T) {
	q, err := Parse("users.csv | transform xs = LIST(1, 2)")
	if err != nil {
		t.Fatal(err)
	}
	tr := q.Ops[0].(*ast.TransformOp)
	ls, ok := tr.Assignments[0].Expr.(*ast.ListExpr)
	if !ok {
		t.Fatalf("expected ListExpr, got %T", tr.Assignments[0].Expr)
	}
	if len(ls.Elements) != 2 {
		t.Fatalf("expected 2 elements, got %d", len(ls.Elements))
	}
}

func TestParseListColumnReferenceStillAllowed(t *testing.T) {
	q, err := Parse("users.csv | transform x = list")
	if err != nil {
		t.Fatal(err)
	}
	tr := q.Ops[0].(*ast.TransformOp)
	col, ok := tr.Assignments[0].Expr.(*ast.ColumnExpr)
	if !ok {
		t.Fatalf("expected ColumnExpr, got %T", tr.Assignments[0].Expr)
	}
	if len(col.Path) != 1 || col.Path[0] != "list" {
		t.Fatalf("unexpected column path: %v", col.Path)
	}
}

func TestParseListExprErrors(t *testing.T) {
	cases := []struct {
		name    string
		query   string
		wantMsg string
	}{
		{"trailing_comma", "users.csv | transform xs = list(1,)", "expected expression after ','"},
		{"unclosed", "users.csv | transform xs = list(1, 2", "expected )"},
		{"missing_comma", "users.csv | transform xs = list(1 2)", "expected )"},
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
	q, err := Parse("users.csv | rename `first name`=first_name, `last name`=last_name")
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

func TestParseNotNullLiteralRejected(t *testing.T) {
	cases := []struct {
		name    string
		query   string
		wantMsg string
	}{
		{"filter", "users.csv | filter { not null }", `use "is not null" for null checks, not "not null"`},
		{"transform", "users.csv | transform x = not null", `use "is not null" for null checks, not "not null"`},
		{"parens", "users.csv | filter { not (null) }", `use "is not null" for null checks, not "not null"`},
		{"inside_or", "users.csv | filter { age > 20 or not null }", `use "is not null" for null checks, not "not null"`},
		{"inside_and", "users.csv | filter { not null and age > 20 }", `use "is not null" for null checks, not "not null"`},
		{"in_if", `users.csv | transform x = if(not null, 0, age)`, `use "is not null" for null checks, not "not null"`},
		{"in_reduce", "users.csv | group name | reduce grouped total = if(not null, 1, 0)", `use "is not null" for null checks, not "not null"`},
		{"double_not", "users.csv | filter { not not null }", `use "is not null" for null checks, not "not null"`},
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
		"users.csv | filter { not age is null }",
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
	q, err := Parse("users.csv | distinct city, age")
	if err != nil {
		t.Fatal(err)
	}
	d := q.Ops[0].(*ast.DistinctOp)
	if len(d.Columns) != 2 {
		t.Fatalf("expected 2 columns, got %d", len(d.Columns))
	}
}

func TestParseSortMixedDirections(t *testing.T) {
	q, err := Parse("users.csv | sort a, -b, c")
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
	q, err := Parse(`sales.csv | filter { year(date) == 2024 } | transform revenue = coalesce(quantity, 0) * coalesce(price, 0) | group category, city | reduce total_revenue = sum(revenue), order_count = count() | remove grouped | filter { total_revenue > 1000 } | sort -total_revenue | head 3 | select category, city, total_revenue, order_count`)
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
	q, err := Parse("users.csv | select name, address.city, profile.stats.logins")
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

// --- Comma column lists and = rename bindings ---

func TestParseCommaColumnLists(t *testing.T) {
	t.Run("select_comma_list", func(t *testing.T) {
		q, err := Parse("users.csv | select name, age")
		if err != nil {
			t.Fatal(err)
		}
		s := q.Ops[0].(*ast.SelectOp)
		if len(s.Columns) != 2 || s.Columns[0][0] != "name" || s.Columns[1][0] != "age" {
			t.Errorf("expected [[name], [age]], got %v", s.Columns)
		}
	})

	t.Run("select_dot_paths", func(t *testing.T) {
		q, err := Parse("users.csv | select name, address.city, profile.stats.logins")
		if err != nil {
			t.Fatal(err)
		}
		s := q.Ops[0].(*ast.SelectOp)
		if len(s.Columns) != 3 {
			t.Fatalf("expected 3 columns, got %d", len(s.Columns))
		}
		if len(s.Columns[1]) != 2 || s.Columns[1][0] != "address" || s.Columns[1][1] != "city" {
			t.Errorf("col[1]: expected [address city], got %v", s.Columns[1])
		}
		if len(s.Columns[2]) != 3 {
			t.Errorf("col[2]: expected 3 segments, got %v", s.Columns[2])
		}
	})

	t.Run("select_single_column", func(t *testing.T) {
		q, err := Parse("users.csv | select name")
		if err != nil {
			t.Fatal(err)
		}
		s := q.Ops[0].(*ast.SelectOp)
		if len(s.Columns) != 1 || s.Columns[0][0] != "name" {
			t.Errorf("expected [[name]], got %v", s.Columns)
		}
	})

	t.Run("sort_comma_list", func(t *testing.T) {
		q, err := Parse("users.csv | sort age, name")
		if err != nil {
			t.Fatal(err)
		}
		s := q.Ops[0].(*ast.SortOp)
		if len(s.Keys) != 2 || s.Keys[0].Path[0] != "age" || s.Keys[1].Path[0] != "name" {
			t.Errorf("expected keys age, name, got %v", s.Keys)
		}
	})

	t.Run("sort_mixed_directions", func(t *testing.T) {
		q, err := Parse("users.csv | sort a, -b, c")
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
	})

	t.Run("sort_both_descending", func(t *testing.T) {
		q, err := Parse("users.csv | sort -age, -name")
		if err != nil {
			t.Fatal(err)
		}
		s := q.Ops[0].(*ast.SortOp)
		if len(s.Keys) != 2 || !s.Keys[0].Desc || !s.Keys[1].Desc {
			t.Errorf("expected both descending, got %v", s.Keys)
		}
	})

	t.Run("sort_dot_path_comma_list", func(t *testing.T) {
		q, err := Parse("users.csv | sort address.city, -name")
		if err != nil {
			t.Fatal(err)
		}
		s := q.Ops[0].(*ast.SortOp)
		if len(s.Keys) != 2 {
			t.Fatalf("expected 2 keys, got %d", len(s.Keys))
		}
		if len(s.Keys[0].Path) != 2 || s.Keys[0].Path[0] != "address" || s.Keys[0].Path[1] != "city" {
			t.Errorf("key[0]: expected [address city], got %v", s.Keys[0].Path)
		}
		if s.Keys[0].Desc {
			t.Errorf("key[0]: expected ascending, got descending")
		}
		if s.Keys[1].Path[0] != "name" || !s.Keys[1].Desc {
			t.Errorf("key[1]: expected [name] desc, got %v desc=%v", s.Keys[1].Path, s.Keys[1].Desc)
		}
	})

	t.Run("group_comma_list", func(t *testing.T) {
		q, err := Parse("users.csv | group city, department as entries")
		if err != nil {
			t.Fatal(err)
		}
		g := q.Ops[0].(*ast.GroupOp)
		if len(g.Columns) != 2 || g.Columns[0][0] != "city" || g.Columns[1][0] != "department" {
			t.Errorf("expected [[city], [department]], got %v", g.Columns)
		}
		if g.NestedName != "entries" {
			t.Errorf("expected nested name 'entries', got %q", g.NestedName)
		}
	})

	t.Run("group_single_with_as", func(t *testing.T) {
		q, err := Parse("users.csv | group city as entries")
		if err != nil {
			t.Fatal(err)
		}
		g := q.Ops[0].(*ast.GroupOp)
		if len(g.Columns) != 1 || g.Columns[0][0] != "city" {
			t.Errorf("expected [[city]], got %v", g.Columns)
		}
	})

	t.Run("distinct_comma_list", func(t *testing.T) {
		q, err := Parse("users.csv | distinct city, age")
		if err != nil {
			t.Fatal(err)
		}
		d := q.Ops[0].(*ast.DistinctOp)
		if len(d.Columns) != 2 || d.Columns[0][0] != "city" || d.Columns[1][0] != "age" {
			t.Errorf("expected [[city], [age]], got %v", d.Columns)
		}
	})

	t.Run("distinct_no_columns", func(t *testing.T) {
		q, err := Parse("users.csv | distinct")
		if err != nil {
			t.Fatal(err)
		}
		d := q.Ops[0].(*ast.DistinctOp)
		if len(d.Columns) != 0 {
			t.Errorf("expected no columns, got %v", d.Columns)
		}
	})

	t.Run("remove_comma_list", func(t *testing.T) {
		q, err := Parse("users.csv | remove password, ssn")
		if err != nil {
			t.Fatal(err)
		}
		r := q.Ops[0].(*ast.RemoveOp)
		if len(r.Columns) != 2 || r.Columns[0][0] != "password" || r.Columns[1][0] != "ssn" {
			t.Errorf("expected [[password], [ssn]], got %v", r.Columns)
		}
	})

	t.Run("rename_equals_bindings", func(t *testing.T) {
		q, err := Parse("users.csv | rename name=first_name, city=location")
		if err != nil {
			t.Fatal(err)
		}
		r := q.Ops[0].(*ast.RenameOp)
		if len(r.Pairs) != 2 {
			t.Fatalf("expected 2 pairs, got %d", len(r.Pairs))
		}
		if r.Pairs[0].Old != "name" || r.Pairs[0].New != "first_name" {
			t.Errorf("pair[0]: got %v -> %v", r.Pairs[0].Old, r.Pairs[0].New)
		}
		if r.Pairs[1].Old != "city" || r.Pairs[1].New != "location" {
			t.Errorf("pair[1]: got %v -> %v", r.Pairs[1].Old, r.Pairs[1].New)
		}
	})

	t.Run("rename_backtick_bindings", func(t *testing.T) {
		q, err := Parse("users.csv | rename `first name`=first_name, `last name`=last_name")
		if err != nil {
			t.Fatal(err)
		}
		r := q.Ops[0].(*ast.RenameOp)
		if len(r.Pairs) != 2 {
			t.Fatalf("expected 2 pairs, got %d", len(r.Pairs))
		}
		if r.Pairs[0].Old != "first name" || r.Pairs[0].New != "first_name" {
			t.Errorf("pair[0]: got %v -> %v", r.Pairs[0].Old, r.Pairs[0].New)
		}
	})

	t.Run("rename_single_pair", func(t *testing.T) {
		q, err := Parse("users.csv | rename name=first_name")
		if err != nil {
			t.Fatal(err)
		}
		r := q.Ops[0].(*ast.RenameOp)
		if len(r.Pairs) != 1 || r.Pairs[0].Old != "name" || r.Pairs[0].New != "first_name" {
			t.Errorf("expected name -> first_name, got %v", r.Pairs)
		}
	})

	t.Run("rename_spaces_around_equals", func(t *testing.T) {
		q, err := Parse("users.csv | rename name = first_name, city = location")
		if err != nil {
			t.Fatal(err)
		}
		r := q.Ops[0].(*ast.RenameOp)
		if len(r.Pairs) != 2 {
			t.Fatalf("expected 2 pairs, got %d", len(r.Pairs))
		}
		if r.Pairs[0].Old != "name" || r.Pairs[0].New != "first_name" {
			t.Errorf("pair[0]: got %v -> %v", r.Pairs[0].Old, r.Pairs[0].New)
		}
		if r.Pairs[1].Old != "city" || r.Pairs[1].New != "location" {
			t.Errorf("pair[1]: got %v -> %v", r.Pairs[1].Old, r.Pairs[1].New)
		}
	})

	t.Run("rename_swap_idiom", func(t *testing.T) {
		q, err := Parse("users.csv | rename name=age, age=name")
		if err != nil {
			t.Fatal(err)
		}
		r := q.Ops[0].(*ast.RenameOp)
		if len(r.Pairs) != 2 {
			t.Fatalf("expected 2 pairs, got %d", len(r.Pairs))
		}
	})

	t.Run("full_query_new_syntax", func(t *testing.T) {
		q, err := Parse(`sales.csv | filter { year(date) == 2024 } | transform revenue = coalesce(quantity, 0) * coalesce(price, 0) | group category, city | reduce total_revenue = sum(revenue), order_count = count() | remove grouped | filter { total_revenue > 1000 } | sort -total_revenue | head 3 | select category, city, total_revenue, order_count`)
		if err != nil {
			t.Fatal(err)
		}
		if len(q.Ops) != 9 {
			t.Errorf("expected 9 ops, got %d", len(q.Ops))
		}
	})

	t.Run("join_keys_unchanged", func(t *testing.T) {
		q, err := Parse("a.csv | join b.csv on id == customer_id and region == region | sort name, product")
		if err != nil {
			t.Fatal(err)
		}
		j := q.Ops[0].(*ast.JoinOp)
		if len(j.Keys) != 2 {
			t.Fatalf("expected 2 join keys, got %d", len(j.Keys))
		}
		s := q.Ops[1].(*ast.SortOp)
		if len(s.Keys) != 2 || s.Keys[0].Path[0] != "name" || s.Keys[1].Path[0] != "product" {
			t.Errorf("expected sort keys name, product, got %v", s.Keys)
		}
	})

	t.Run("reduce_nested_name_unchanged", func(t *testing.T) {
		q, err := Parse("users.csv | group name as entries | reduce entries max_age = max(age), count = count()")
		if err != nil {
			t.Fatal(err)
		}
		r := q.Ops[1].(*ast.ReduceOp)
		if r.NestedName != "entries" {
			t.Errorf("expected nested name 'entries', got %q", r.NestedName)
		}
		if len(r.Assignments) != 2 {
			t.Fatalf("expected 2 assignments, got %d", len(r.Assignments))
		}
	})
}

func TestParseSpaceSeparatedColumnListsRejected(t *testing.T) {
	cases := []struct {
		name    string
		query   string
		wantMsg string
	}{
		{"select_space_list", "users.csv | select name age", "expected ',' between columns"},
		{"sort_space_list", "users.csv | sort age name", "expected ',' between"},
		{"group_space_list", "users.csv | group city department", "expected ',' between columns"},
		{"distinct_space_list", "users.csv | distinct city age", "expected ',' between columns"},
		{"remove_space_list", "users.csv | remove password ssn", "expected ',' between columns"},
		{"rename_space_pairs", "users.csv | rename name first_name", "expected '='"},
		{"rename_multi_space_pairs", "users.csv | rename name first_name city location", "expected '='"},
		{"sort_missing_comma_before_desc", "users.csv | sort age -name", "expected ',' between sort keys"},
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

func TestParseInvalidCommaSyntaxRejected(t *testing.T) {
	cases := []struct {
		name    string
		query   string
		wantMsg string
	}{
		{"rename_double_equals", "users.csv | rename name==first_name", "expected '='"},
		{"select_double_comma", "users.csv | select name,, age", "expected column name after ','"},
		{"group_trailing_comma_before_as", "users.csv | group city, as entries", "expected column name after ','"},
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

func TestParseMutationBoundarySyntaxCases(t *testing.T) {
	t.Run("sort_backtick_column", func(t *testing.T) {
		q, err := Parse("users.csv | sort `first name`")
		if err != nil {
			t.Fatal(err)
		}
		s := q.Ops[0].(*ast.SortOp)
		if len(s.Keys) != 1 || len(s.Keys[0].Path) != 1 || s.Keys[0].Path[0] != "first name" {
			t.Fatalf("unexpected sort keys: %#v", s.Keys)
		}
	})

	t.Run("group_requires_valid_nested_name", func(t *testing.T) {
		_, err := Parse("users.csv | group city as 1")
		if err == nil {
			t.Fatal("expected invalid nested name error")
		}
		if !strings.Contains(err.Error(), "expected nested name") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("reduce_with_backtick_assignment", func(t *testing.T) {
		q, err := Parse("users.csv | group city | reduce `total count` = count()")
		if err != nil {
			t.Fatal(err)
		}
		r := q.Ops[1].(*ast.ReduceOp)
		if r.NestedName != "grouped" {
			t.Fatalf("nested name: want grouped, got %q", r.NestedName)
		}
		if len(r.Assignments) != 1 || r.Assignments[0].Column != "total count" {
			t.Fatalf("unexpected assignments: %#v", r.Assignments)
		}
	})

	t.Run("reduce_nested_name_with_backtick_assignment", func(t *testing.T) {
		q, err := Parse("users.csv | group city as entries | reduce entries `total count` = count()")
		if err != nil {
			t.Fatal(err)
		}
		r := q.Ops[1].(*ast.ReduceOp)
		if r.NestedName != "entries" {
			t.Fatalf("nested name: want entries, got %q", r.NestedName)
		}
		if len(r.Assignments) != 1 || r.Assignments[0].Column != "total count" {
			t.Fatalf("unexpected assignments: %#v", r.Assignments)
		}
	})

	t.Run("rename_accepts_backtick_new_name", func(t *testing.T) {
		q, err := Parse("users.csv | rename first=`first name`")
		if err != nil {
			t.Fatal(err)
		}
		r := q.Ops[0].(*ast.RenameOp)
		if len(r.Pairs) != 1 || r.Pairs[0].Old != "first" || r.Pairs[0].New != "first name" {
			t.Fatalf("unexpected rename pairs: %#v", r.Pairs)
		}
	})

	t.Run("column_path_rejects_numeric_segment", func(t *testing.T) {
		_, err := Parse("users.csv | select address.1")
		if err == nil {
			t.Fatal("expected invalid dot-path segment error")
		}
		if !strings.Contains(err.Error(), "expected field name after '.'") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("expression_path_rejects_numeric_segment", func(t *testing.T) {
		_, err := Parse("users.csv | filter { address.1 == \"NY\" }")
		if err == nil {
			t.Fatal("expected invalid expression dot-path segment error")
		}
		if !strings.Contains(err.Error(), "expected field name after '.'") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("assignment_requires_identifier", func(t *testing.T) {
		_, err := Parse("users.csv | transform 1 = age")
		if err == nil {
			t.Fatal("expected invalid assignment target error")
		}
		if !strings.Contains(err.Error(), "expected column name in assignment") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestParseTrailingCommaInColumnListsRejected(t *testing.T) {
	cases := []struct {
		name  string
		query string
	}{
		{"select", "users.csv | select name,"},
		{"sort", "users.csv | sort age,"},
		{"group", "users.csv | group city,"},
		{"distinct", "users.csv | distinct city,"},
		{"remove", "users.csv | remove password,"},
		{"rename", "users.csv | rename name=first_name,"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(tc.query)
			if err == nil {
				t.Fatalf("expected parse error for trailing comma in %q", tc.query)
			}
		})
	}
}

func TestParseSourceWithLoadOptions(t *testing.T) {
	t.Run("format_only", func(t *testing.T) {
		q, err := Parse("data.dat with format=CSV | head 5")
		if err != nil {
			t.Fatal(err)
		}
		if q.Source.Filename != "data.dat" {
			t.Errorf("filename: got %q", q.Source.Filename)
		}
		if q.Source.Load.Format != "csv" {
			t.Errorf("format: got %q", q.Source.Load.Format)
		}
		if q.Source.Load.Header != nil {
			t.Errorf("header: expected nil default, got %v", *q.Source.Load.Header)
		}
	})

	t.Run("csv_options", func(t *testing.T) {
		q, err := Parse(`data.csv with format=csv, header=false, delim=";" | count`)
		if err != nil {
			t.Fatal(err)
		}
		if q.Source.Load.Format != "csv" {
			t.Errorf("format: got %q", q.Source.Load.Format)
		}
		if q.Source.Load.Header == nil || *q.Source.Load.Header != false {
			t.Errorf("header: want false, got %v", q.Source.Load.Header)
		}
		if q.Source.Load.Delim != ";" {
			t.Errorf("delim: got %q", q.Source.Load.Delim)
		}
	})

	t.Run("glob_with_format", func(t *testing.T) {
		q, err := Parse("logs/part-* with format=csv | count")
		if err != nil {
			t.Fatal(err)
		}
		if q.Source.Filename != "logs/part-*" {
			t.Errorf("filename: got %q", q.Source.Filename)
		}
		if q.Source.Load.Format != "csv" {
			t.Errorf("format: got %q", q.Source.Load.Format)
		}
	})

	t.Run("stdin_with_format", func(t *testing.T) {
		q, err := Parse("- with format=csv | filter { age > 25 }")
		if err != nil {
			t.Fatal(err)
		}
		if q.Source.Filename != "-" {
			t.Errorf("filename: got %q", q.Source.Filename)
		}
		if q.Source.Load.Format != "csv" {
			t.Errorf("format: got %q", q.Source.Load.Format)
		}
	})

	t.Run("stdin_with_gzip_compression", func(t *testing.T) {
		q, err := Parse("- with format=csv, compression=gzip | count")
		if err != nil {
			t.Fatal(err)
		}
		if q.Source.Filename != "-" {
			t.Errorf("filename: got %q", q.Source.Filename)
		}
		if q.Source.Load.Format != "csv" {
			t.Errorf("format: got %q", q.Source.Load.Format)
		}
		if q.Source.Load.Compression != "gzip" {
			t.Errorf("compression: got %q", q.Source.Load.Compression)
		}
	})

	t.Run("explicit_header_true", func(t *testing.T) {
		q, err := Parse("data.csv with header=true | head")
		if err != nil {
			t.Fatal(err)
		}
		if q.Source.Load.Header == nil || *q.Source.Load.Header != true {
			t.Errorf("header: want true, got %v", q.Source.Load.Header)
		}
	})

	t.Run("delim_without_format", func(t *testing.T) {
		q, err := Parse(`data.csv with delim=";" | count`)
		if err != nil {
			t.Fatal(err)
		}
		if q.Source.Load.Delim != ";" {
			t.Errorf("delim: got %q", q.Source.Load.Delim)
		}
		if q.Source.Load.Format != "" {
			t.Errorf("format: expected empty, got %q", q.Source.Load.Format)
		}
	})

	t.Run("csv_row_shape_options", func(t *testing.T) {
		q, err := Parse(`data.csv with allow_jagged_rows=true, ignore_unknown_values=true | count`)
		if err != nil {
			t.Fatal(err)
		}
		if q.Source.Load.AllowJaggedRows == nil || *q.Source.Load.AllowJaggedRows != true {
			t.Errorf("allow_jagged_rows: got %v", q.Source.Load.AllowJaggedRows)
		}
		if q.Source.Load.IgnoreUnknownValues == nil || *q.Source.Load.IgnoreUnknownValues != true {
			t.Errorf("ignore_unknown_values: got %v", q.Source.Load.IgnoreUnknownValues)
		}
	})

	t.Run("gzip_compression_option", func(t *testing.T) {
		q, err := Parse(`events.gz with format=jsonl, compression=gzip | count`)
		if err != nil {
			t.Fatal(err)
		}
		if q.Source.Filename != "events.gz" {
			t.Errorf("filename: got %q", q.Source.Filename)
		}
		if q.Source.Load.Format != "jsonl" {
			t.Errorf("format: got %q", q.Source.Load.Format)
		}
		if q.Source.Load.Compression != "gzip" {
			t.Errorf("compression: got %q", q.Source.Load.Compression)
		}
	})

	t.Run("gzip_double_extension_csv_options", func(t *testing.T) {
		q, err := Parse(`data.csv.gz with header=false, delim=";" | count`)
		if err != nil {
			t.Fatal(err)
		}
		if q.Source.Load.Header == nil || *q.Source.Load.Header != false {
			t.Errorf("header: want false, got %v", q.Source.Load.Header)
		}
		if q.Source.Load.Delim != ";" {
			t.Errorf("delim: got %q", q.Source.Load.Delim)
		}
	})
}

func TestParseJoinWithLoadOptions(t *testing.T) {
	t.Run("inner", func(t *testing.T) {
		q, err := Parse(`users.csv | join orders.dat with format=csv on name == user_name`)
		if err != nil {
			t.Fatal(err)
		}
		j := q.Ops[0].(*ast.JoinOp)
		if j.Filename != "orders.dat" {
			t.Errorf("filename: got %q", j.Filename)
		}
		if j.Load.Format != "csv" {
			t.Errorf("format: got %q", j.Load.Format)
		}
	})

	t.Run("left_with_delim", func(t *testing.T) {
		q, err := Parse(`users.csv | join left orders/part-*.dat with format=csv, delim=";" on user_id == customer_id`)
		if err != nil {
			t.Fatal(err)
		}
		j := q.Ops[0].(*ast.JoinOp)
		if j.Kind != "left" {
			t.Errorf("kind: got %q", j.Kind)
		}
		if j.Filename != "orders/part-*.dat" {
			t.Errorf("filename: got %q", j.Filename)
		}
		if j.Load.Format != "csv" || j.Load.Delim != ";" {
			t.Errorf("load: got format=%q delim=%q", j.Load.Format, j.Load.Delim)
		}
	})

	t.Run("format_case_insensitive", func(t *testing.T) {
		q, err := Parse(`users.csv | join orders.dat with format=CSV on name == user_name`)
		if err != nil {
			t.Fatal(err)
		}
		j := q.Ops[0].(*ast.JoinOp)
		if j.Load.Format != "csv" {
			t.Errorf("format: got %q", j.Load.Format)
		}
	})

	t.Run("after_other_op", func(t *testing.T) {
		q, err := Parse(`users.csv | filter { age > 20 } | join orders.dat with format=csv on id`)
		if err != nil {
			t.Fatal(err)
		}
		j := q.Ops[1].(*ast.JoinOp)
		if j.Load.Format != "csv" {
			t.Errorf("format: got %q", j.Load.Format)
		}
	})

	t.Run("gzip_compression_option", func(t *testing.T) {
		q, err := Parse(`users.csv | join orders.gz with format=csv, compression=gzip on user_id`)
		if err != nil {
			t.Fatal(err)
		}
		j := q.Ops[0].(*ast.JoinOp)
		if j.Filename != "orders.gz" {
			t.Errorf("filename: got %q", j.Filename)
		}
		if j.Load.Format != "csv" {
			t.Errorf("format: got %q", j.Load.Format)
		}
		if j.Load.Compression != "gzip" {
			t.Errorf("compression: got %q", j.Load.Compression)
		}
	})

	t.Run("gzip_double_extension_csv_options", func(t *testing.T) {
		q, err := Parse(`users.csv | join orders.csv.gz with header=false on user_id == col1`)
		if err != nil {
			t.Fatal(err)
		}
		j := q.Ops[0].(*ast.JoinOp)
		if j.Load.Header == nil || *j.Load.Header != false {
			t.Errorf("header: want false, got %v", j.Load.Header)
		}
	})
}

func TestParseWithLoadOptionsRejected(t *testing.T) {
	cases := []struct {
		name  string
		query string
		msg   string
	}{
		{"unknown_key", "data.csv with foo=bar | head", "unknown"},
		{"duplicate_format", "data.csv with format=csv, format=json | head", "duplicate"},
		{"duplicate_compression", "data.csv.gz with compression=gzip, compression=gzip | head", "duplicate"},
		{"unsupported_format", "data.csv with format=csvv | head", "unsupported"},
		{"unsupported_compression", "data.csv with compression=brotli | head", "unsupported"},
		{"with_after_on", "users.csv | join orders.csv on id with format=csv", "with"},
		{"csv_header_on_json", "data.json with format=json, header=false | head", "header"},
		{"csv_delim_on_json", "data.json with format=json, delim=\";\" | head", "delim"},
		{"csv_allow_jagged_on_json", "data.json with format=json, allow_jagged_rows=true | head", "allow_jagged_rows"},
		{"csv_ignore_unknown_on_json", "data.json with format=json, ignore_unknown_values=true | head", "ignore_unknown_values"},
		{"inferred_json_header", "data.json with header=false | head", "header"},
		{"inferred_json_delim", "data.json with delim=\";\" | head", "delim"},
		{"join_inferred_json_header", "users.csv | join data.json with header=false on id", "header"},
		{"unknown_ext_header", "data.dat with header=false | head", "with format"},
		{"unknown_ext_delim", "data.dat with delim=\";\" | head", "with format"},
		{"glob_csv_opts_without_format", "part-*.dat with header=false | head", "with format"},
		{"gzip_on_avro", "data.avro with compression=gzip | head", "compression"},
		{"gzip_on_parquet", "data.parquet with compression=gzip | head", "compression"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(tc.query)
			if err == nil {
				t.Fatalf("expected parse error for %q", tc.query)
			}
			if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tc.msg)) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.msg)
			}
		})
	}
}
