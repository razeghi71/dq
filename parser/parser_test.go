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
	if g.Columns[0] != "city" || g.Columns[1] != "department" {
		t.Errorf("expected [city, department], got %v", g.Columns)
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

func TestParseFullQuery(t *testing.T) {
	q, err := Parse(`sales.csv | filter { year(date) == 2024 } | transform revenue = coalesce(quantity, 0) * coalesce(price, 0) | group category city | reduce total_revenue = sum(revenue), order_count = count() | remove grouped | filter { total_revenue > 1000 } | sortd total_revenue | head 3 | select category city total_revenue order_count`)
	if err != nil {
		t.Fatal(err)
	}
	if len(q.Ops) != 9 {
		t.Errorf("expected 9 ops, got %d", len(q.Ops))
	}
}
