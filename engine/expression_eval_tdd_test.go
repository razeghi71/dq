package engine

import (
	"strings"
	"testing"

	"github.com/razeghi71/dq/ast"
	"github.com/razeghi71/dq/table"
)

type unknownBoundExprForTest struct{}

func (*unknownBoundExprForTest) boundExprNode() {}

func TestEvalTypedAggregateExpressionInternalBranches(t *testing.T) {
	nested := table.NewTable([]string{"x", "flag"})
	nested.AddRow([]table.Value{table.IntVal(4), table.BoolVal(true)})
	nested.AddRow([]table.Value{table.IntVal(2), table.BoolVal(false)})

	col := func(name string) ast.Expr {
		return &ast.ColumnExpr{Path: []string{name}}
	}
	call := func(name string, args ...ast.Expr) *ast.FuncCallExpr {
		return &ast.FuncCallExpr{Name: name, Args: args}
	}
	lit := func(v int64) typedExpr {
		raw := &ast.LiteralExpr{Kind: "int", Int: v}
		return typedExpr{bound: &boundLiteral{raw: raw}, raw: raw, typ: &table.TypeDescriptor{Kind: table.TypeInt}}
	}

	cases := []struct {
		name string
		expr typedExpr
		want table.Value
	}{
		{
			name: "literal",
			expr: lit(7),
			want: table.IntVal(7),
		},
		{
			name: "aggregate_call",
			expr: typedExpr{
				bound: &boundCall{raw: call("sum", col("x"))},
				typ:   &table.TypeDescriptor{Kind: table.TypeInt},
			},
			want: table.IntVal(6),
		},
		{
			name: "aggregate_count",
			expr: typedExpr{
				bound: &boundCall{raw: call("count")},
				typ:   &table.TypeDescriptor{Kind: table.TypeInt},
			},
			want: table.IntVal(2),
		},
		{
			name: "aggregate_avg",
			expr: typedExpr{
				bound: &boundCall{raw: call("avg", col("x"))},
				typ:   &table.TypeDescriptor{Kind: table.TypeFloat, Nullable: true},
			},
			want: table.FloatVal(3),
		},
		{
			name: "aggregate_min",
			expr: typedExpr{
				bound: &boundCall{raw: call("min", col("x"))},
				typ:   &table.TypeDescriptor{Kind: table.TypeInt, Nullable: true},
			},
			want: table.IntVal(2),
		},
		{
			name: "aggregate_max",
			expr: typedExpr{
				bound: &boundCall{raw: call("max", col("x"))},
				typ:   &table.TypeDescriptor{Kind: table.TypeInt, Nullable: true},
			},
			want: table.IntVal(4),
		},
		{
			name: "aggregate_first_including_nullability",
			expr: typedExpr{
				bound: &boundCall{raw: call("first", col("flag"))},
				typ:   &table.TypeDescriptor{Kind: table.TypeBool, Nullable: true},
			},
			want: table.BoolVal(true),
		},
		{
			name: "aggregate_last",
			expr: typedExpr{
				bound: &boundCall{raw: call("last", col("x"))},
				typ:   &table.TypeDescriptor{Kind: table.TypeInt, Nullable: true},
			},
			want: table.IntVal(2),
		},
		{
			name: "explicit_coerce",
			expr: typedExpr{
				bound:    &boundCoerce{},
				operand:  typedExprPtr(lit(7)),
				typ:      &table.TypeDescriptor{Kind: table.TypeFloat},
				coerceTo: &table.TypeDescriptor{Kind: table.TypeFloat},
			},
			want: table.FloatVal(7),
		},
		{
			name: "unary_null_negation",
			expr: typedExpr{
				bound:   &boundUnary{raw: &ast.UnaryExpr{Op: "-"}},
				operand: typedExprPtr(typedExpr{bound: &boundLiteral{raw: &ast.LiteralExpr{Kind: "null"}}}),
				typ:     &table.TypeDescriptor{Kind: table.TypeInt, Nullable: true},
			},
			want: table.Null(),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := evalTypedAggregateExpression(tc.expr, nested)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			assertPrimitiveValue(t, got, tc.want)
		})
	}
}

func TestEvalTypedAggregateExpressionDefensiveErrors(t *testing.T) {
	nested := table.NewTable([]string{"x"})
	nested.AddRow([]table.Value{table.IntVal(1)})
	overflow := table.NewTable([]string{"x"})
	overflow.AddRow([]table.Value{table.IntVal(9223372036854775807)})
	overflow.AddRow([]table.Value{table.IntVal(1)})
	stringsTable := table.NewTable([]string{"x"})
	stringsTable.AddRow([]table.Value{table.StrVal("bad")})
	nullsTable := table.NewTableWithSchemas([]string{"x"}, []*table.TypeDescriptor{{Kind: table.TypeInt, Nullable: true}})
	if err := nullsTable.AddRowTyped([]table.Value{table.Null()}); err != nil {
		t.Fatalf("add null row: %v", err)
	}
	if err := nullsTable.AddRowTyped([]table.Value{table.IntVal(5)}); err != nil {
		t.Fatalf("add int row: %v", err)
	}
	lit := func(v int64) typedExpr {
		raw := &ast.LiteralExpr{Kind: "int", Int: v}
		return typedExpr{bound: &boundLiteral{raw: raw}, raw: raw, typ: &table.TypeDescriptor{Kind: table.TypeInt}}
	}
	call := func(name string, args ...ast.Expr) typedExpr {
		return typedExpr{bound: &boundCall{raw: &ast.FuncCallExpr{Name: name, Args: args}}}
	}
	badColumn := typedExpr{bound: &boundColumn{rawPath: []string{"x"}, topIndex: -1}}

	cases := []struct {
		name string
		expr typedExpr
		want string
	}{
		{
			name: "binary_left_error",
			expr: typedExpr{bound: &boundBinary{raw: &ast.BinaryExpr{Op: "+"}}, left: typedExprPtr(badColumn), right: typedExprPtr(lit(1))},
			want: "cannot be used directly",
		},
		{
			name: "binary_right_error",
			expr: typedExpr{bound: &boundBinary{raw: &ast.BinaryExpr{Op: "+"}}, left: typedExprPtr(lit(1)), right: typedExprPtr(badColumn)},
			want: "cannot be used directly",
		},
		{
			name: "binary_unknown_operator",
			expr: typedExpr{bound: &boundBinary{raw: &ast.BinaryExpr{Op: "%"}}, left: typedExprPtr(lit(1)), right: typedExprPtr(lit(1))},
			want: "unknown operator",
		},
		{
			name: "unary_child_error",
			expr: typedExpr{bound: &boundUnary{raw: &ast.UnaryExpr{Op: "-"}}, operand: typedExprPtr(badColumn)},
			want: "cannot be used directly",
		},
		{
			name: "unary_unknown_operator",
			expr: typedExpr{bound: &boundUnary{raw: &ast.UnaryExpr{Op: "~"}}, operand: typedExprPtr(lit(1))},
			want: "unknown unary operator",
		},
		{
			name: "coerce_child_error",
			expr: typedExpr{bound: &boundCoerce{}, operand: typedExprPtr(badColumn), coerceTo: &table.TypeDescriptor{Kind: table.TypeFloat}},
			want: "cannot be used directly",
		},
		{
			name: "is_null_child_error",
			expr: typedExpr{bound: &boundIsNull{raw: &ast.IsNullExpr{}}, operand: typedExprPtr(badColumn)},
			want: "cannot be used directly",
		},
		{
			name: "direct_column",
			expr: badColumn,
			want: "use an aggregate",
		},
		{
			name: "struct_constructor",
			expr: typedExpr{bound: &boundStruct{}},
			want: "struct constructor",
		},
		{
			name: "list_constructor",
			expr: typedExpr{bound: &boundList{}},
			want: "list constructor",
		},
		{
			name: "unknown_bound_expression",
			expr: typedExpr{bound: &unknownBoundExprForTest{}},
			want: "unknown bound expression",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := evalTypedAggregateExpression(tc.expr, nested); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error containing %q, got %v", tc.want, err)
			}
		})
	}

	if _, err := evalTypedAggregateChild(nil, nested); err == nil || !strings.Contains(err.Error(), "missing typed aggregate expression child") {
		t.Fatalf("expected missing child error, got %v", err)
	}

	aggregateErrors := []struct {
		name   string
		input  *table.Table
		expr   typedExpr
		wanted string
	}{
		{name: "count_rejects_arg", input: nested, expr: call("count", &ast.ColumnExpr{Path: []string{"x"}}), wanted: "takes no arguments"},
		{name: "sum_requires_arg", input: nested, expr: call("sum"), wanted: "takes 1 argument"},
		{name: "sum_arg_must_be_column", input: nested, expr: call("sum", &ast.LiteralExpr{Kind: "int", Int: 1}), wanted: "column reference"},
		{name: "sum_missing_column", input: nested, expr: call("sum", &ast.ColumnExpr{Path: []string{"missing"}}), wanted: "not found"},
		{name: "avg_missing_column", input: nested, expr: call("avg", &ast.ColumnExpr{Path: []string{"missing"}}), wanted: "not found"},
		{name: "min_missing_column", input: nested, expr: call("min", &ast.ColumnExpr{Path: []string{"missing"}}), wanted: "not found"},
		{name: "first_missing_column", input: nested, expr: call("first", &ast.ColumnExpr{Path: []string{"missing"}}), wanted: "not found"},
		{name: "last_missing_column", input: nested, expr: call("last", &ast.ColumnExpr{Path: []string{"missing"}}), wanted: "not found"},
		{name: "sum_non_numeric_runtime", input: stringsTable, expr: call("sum", &ast.ColumnExpr{Path: []string{"x"}}), wanted: "non-numeric"},
		{name: "avg_non_numeric_runtime", input: stringsTable, expr: call("avg", &ast.ColumnExpr{Path: []string{"x"}}), wanted: "non-numeric"},
		{name: "sum_overflow", input: overflow, expr: call("sum", &ast.ColumnExpr{Path: []string{"x"}}), wanted: "overflow"},
	}
	for _, tc := range aggregateErrors {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := evalTypedAggregateExpression(tc.expr, tc.input); err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tc.wanted)) {
				t.Fatalf("aggregate error: got %v, want %q", err, tc.wanted)
			}
		})
	}

	for _, tc := range []struct {
		name string
		expr typedExpr
		want table.Value
	}{
		{name: "avg_skips_null", expr: call("avg", &ast.ColumnExpr{Path: []string{"x"}}), want: table.FloatVal(5)},
		{name: "min_skips_null", expr: call("min", &ast.ColumnExpr{Path: []string{"x"}}), want: table.IntVal(5)},
		{name: "max_skips_null", expr: call("max", &ast.ColumnExpr{Path: []string{"x"}}), want: table.IntVal(5)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := evalTypedAggregateExpression(tc.expr, nullsTable)
			if err != nil {
				t.Fatalf("aggregate null-skip error: %v", err)
			}
			assertPrimitiveValue(t, got, tc.want)
		})
	}
}

func TestEvalTypedAggregateExpressionEmptyAggregateResults(t *testing.T) {
	nested := table.NewTableWithSchemas(
		[]string{"x", "flag"},
		[]*table.TypeDescriptor{{Kind: table.TypeInt}, {Kind: table.TypeBool}},
	)
	col := func(name string) ast.Expr {
		return &ast.ColumnExpr{Path: []string{name}}
	}
	call := func(name string, args ...ast.Expr) typedExpr {
		return typedExpr{bound: &boundCall{raw: &ast.FuncCallExpr{Name: name, Args: args}}}
	}

	cases := []struct {
		name string
		expr typedExpr
		want table.Value
	}{
		{name: "count", expr: call("count"), want: table.IntVal(0)},
		{name: "sum", expr: call("sum", col("x")), want: table.Null()},
		{name: "avg", expr: call("avg", col("x")), want: table.Null()},
		{name: "min", expr: call("min", col("x")), want: table.Null()},
		{name: "max", expr: call("max", col("x")), want: table.Null()},
		{name: "first", expr: call("first", col("flag")), want: table.Null()},
		{name: "last", expr: call("last", col("flag")), want: table.Null()},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := evalTypedAggregateExpression(tc.expr, nested)
			if err != nil {
				t.Fatalf("empty aggregate error: %v", err)
			}
			assertPrimitiveValue(t, got, tc.want)
		})
	}
}

func TestEvalTypedExpressionInternalCoercionAndUnaryBranches(t *testing.T) {
	tbl := table.NewTable([]string{"x", "flag"})
	tbl.AddRow([]table.Value{table.IntVal(4), table.BoolVal(true)})
	ctx := &EvalContext{Table: tbl, RowIdx: 0}

	lit := func(v int64) typedExpr {
		raw := &ast.LiteralExpr{Kind: "int", Int: v}
		return typedExpr{bound: &boundLiteral{raw: raw}, raw: raw, typ: &table.TypeDescriptor{Kind: table.TypeInt}}
	}
	floatLit := func(v float64) typedExpr {
		raw := &ast.LiteralExpr{Kind: "float", Float: v}
		return typedExpr{bound: &boundLiteral{raw: raw}, raw: raw, typ: &table.TypeDescriptor{Kind: table.TypeFloat}}
	}
	nullLit := typedExpr{bound: &boundLiteral{raw: &ast.LiteralExpr{Kind: "null"}}, typ: &table.TypeDescriptor{Kind: table.TypeNull}}

	cases := []struct {
		name string
		expr typedExpr
		want table.Value
	}{
		{
			name: "explicit_coerce",
			expr: typedExpr{
				bound:    &boundCoerce{},
				operand:  typedExprPtr(lit(4)),
				typ:      &table.TypeDescriptor{Kind: table.TypeFloat},
				coerceTo: &table.TypeDescriptor{Kind: table.TypeFloat},
			},
			want: table.FloatVal(4),
		},
		{
			name: "float_negation",
			expr: typedExpr{bound: &boundUnary{raw: &ast.UnaryExpr{Op: "-"}}, operand: typedExprPtr(floatLit(1.5))},
			want: table.FloatVal(-1.5),
		},
		{
			name: "null_negation",
			expr: typedExpr{bound: &boundUnary{raw: &ast.UnaryExpr{Op: "-"}}, operand: typedExprPtr(nullLit)},
			want: table.Null(),
		},
		{
			name: "is_not_null",
			expr: typedExpr{bound: &boundIsNull{raw: &ast.IsNullExpr{Negated: true}}, operand: typedExprPtr(lit(4))},
			want: table.BoolVal(true),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := evalTypedExpression(tc.expr, ctx)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			assertPrimitiveValue(t, got, tc.want)
		})
	}

	got, err := coercePlannedExpressionValue(table.IntVal(4), nil)
	if err != nil {
		t.Fatalf("nil target schema should not error: %v", err)
	}
	assertPrimitiveValue(t, got, table.IntVal(4))
}

func TestEvalTypedExpressionDefensiveErrors(t *testing.T) {
	tbl := table.NewTable([]string{"x"})
	tbl.AddRow([]table.Value{table.IntVal(1)})
	ctx := &EvalContext{Table: tbl, RowIdx: 0}
	lit := func(v int64) typedExpr {
		raw := &ast.LiteralExpr{Kind: "int", Int: v}
		return typedExpr{bound: &boundLiteral{raw: raw}, raw: raw, typ: &table.TypeDescriptor{Kind: table.TypeInt}}
	}
	badColumn := typedExpr{bound: &boundColumn{rawPath: []string{"missing"}, topIndex: -1}}

	cases := []struct {
		name string
		expr typedExpr
		want string
	}{
		{
			name: "binary_left_error",
			expr: typedExpr{bound: &boundBinary{raw: &ast.BinaryExpr{Op: "+"}}, left: typedExprPtr(badColumn), right: typedExprPtr(lit(1))},
			want: "not found",
		},
		{
			name: "binary_right_error",
			expr: typedExpr{bound: &boundBinary{raw: &ast.BinaryExpr{Op: "+"}}, left: typedExprPtr(lit(1)), right: typedExprPtr(badColumn)},
			want: "not found",
		},
		{
			name: "binary_unknown_operator",
			expr: typedExpr{bound: &boundBinary{raw: &ast.BinaryExpr{Op: "%"}}, left: typedExprPtr(lit(1)), right: typedExprPtr(lit(1))},
			want: "unknown operator",
		},
		{
			name: "unary_child_error",
			expr: typedExpr{bound: &boundUnary{raw: &ast.UnaryExpr{Op: "-"}}, operand: typedExprPtr(badColumn)},
			want: "not found",
		},
		{
			name: "unary_unknown_operator",
			expr: typedExpr{bound: &boundUnary{raw: &ast.UnaryExpr{Op: "~"}}, operand: typedExprPtr(lit(1))},
			want: "unknown unary operator",
		},
		{
			name: "struct_child_error",
			expr: typedExpr{bound: &boundStruct{}, fields: []typedStructField{{name: "x", expr: badColumn}}},
			want: "not found",
		},
		{
			name: "list_child_error",
			expr: typedExpr{bound: &boundList{}, elements: []typedExpr{badColumn}},
			want: "not found",
		},
		{
			name: "coerce_child_error",
			expr: typedExpr{bound: &boundCoerce{}, operand: typedExprPtr(badColumn), coerceTo: &table.TypeDescriptor{Kind: table.TypeFloat}},
			want: "not found",
		},
		{
			name: "is_null_child_error",
			expr: typedExpr{bound: &boundIsNull{raw: &ast.IsNullExpr{}}, operand: typedExprPtr(badColumn)},
			want: "not found",
		},
		{
			name: "unknown_bound_expression",
			expr: typedExpr{bound: &unknownBoundExprForTest{}},
			want: "unknown bound expression",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := evalTypedExpression(tc.expr, ctx); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error containing %q, got %v", tc.want, err)
			}
		})
	}

	if _, err := evalTypedChild(nil, ctx); err == nil || !strings.Contains(err.Error(), "missing typed expression child") {
		t.Fatalf("expected missing child error, got %v", err)
	}
}

func TestEvalTypedExpressionUsesBoundColumnIndex(t *testing.T) {
	tbl := table.NewTable([]string{"renamed", "profile"})
	tbl.AddRow([]table.Value{
		table.IntVal(42),
		table.RecordVal([]table.RecordField{{Name: "score", Value: table.IntVal(99)}}),
	})
	ctx := &EvalContext{Table: tbl, RowIdx: 0}

	direct := typedExpr{bound: &boundColumn{
		rawPath:  []string{"original"},
		topIndex: 0,
		typ:      &table.TypeDescriptor{Kind: table.TypeInt},
	}}
	got, err := evalTypedExpression(direct, ctx)
	if err != nil {
		t.Fatalf("direct bound column eval returned error: %v", err)
	}
	assertPrimitiveValue(t, got, table.IntVal(42))

	nested := typedExpr{bound: &boundColumn{
		rawPath:    []string{"stale_profile_name", "score"},
		topIndex:   1,
		nestedPath: []string{"score"},
		typ:        &table.TypeDescriptor{Kind: table.TypeInt},
	}}
	got, err = evalTypedExpression(nested, ctx)
	if err != nil {
		t.Fatalf("nested bound column eval returned error: %v", err)
	}
	assertPrimitiveValue(t, got, table.IntVal(99))
}

func TestCompiledFilterPredicateFastPathsPreserveSemantics(t *testing.T) {
	tbl := table.NewTable([]string{"age", "name", "profile"})
	tbl.AddRow([]table.Value{
		table.IntVal(30),
		table.StrVal("alice"),
		table.RecordVal([]table.RecordField{{Name: "score", Value: table.IntVal(30)}}),
	})
	tbl.AddRow([]table.Value{
		table.IntVal(50),
		table.StrVal("bob-999"),
		table.RecordVal([]table.RecordField{{Name: "score", Value: table.IntVal(50)}}),
	})
	tbl.AddRow([]table.Value{
		table.Null(),
		table.Null(),
		table.RecordVal([]table.RecordField{{Name: "score", Value: table.Null()}}),
	})
	tbl = appendCompiledPredicateFlagColumn(t, tbl)

	intLit := func(v int64) *boundLiteral {
		return &boundLiteral{raw: &ast.LiteralExpr{Kind: "int", Int: v}}
	}
	stringLit := func(v string) *boundLiteral {
		return &boundLiteral{raw: &ast.LiteralExpr{Kind: "string", Str: v}}
	}
	boolLit := func(v bool) *boundLiteral {
		return &boundLiteral{raw: &ast.LiteralExpr{Kind: "bool", Bool: v}}
	}
	nullLit := &boundLiteral{raw: &ast.LiteralExpr{Kind: "null"}}
	ageCol := &boundColumn{rawPath: []string{"age"}, topIndex: 0, typ: &table.TypeDescriptor{Kind: table.TypeInt, Nullable: true}}
	nameCol := &boundColumn{rawPath: []string{"name"}, topIndex: 1, typ: &table.TypeDescriptor{Kind: table.TypeString, Nullable: true}}
	scoreCol := &boundColumn{rawPath: []string{"profile", "score"}, topIndex: 2, nestedPath: []string{"score"}, typ: &table.TypeDescriptor{Kind: table.TypeInt, Nullable: true}}
	flagCol := &boundColumn{rawPath: []string{"flag"}, topIndex: 3, typ: &table.TypeDescriptor{Kind: table.TypeBool, Nullable: true}}

	cases := []struct {
		name string
		expr typedExpr
		want []bool
	}{
		{
			name: "int_column_literal_comparison",
			expr: typedExpr{
				bound: &boundBinary{raw: &ast.BinaryExpr{Op: ">"}, left: ageCol, right: intLit(40)},
				left:  typedExprPtr(typedExpr{bound: ageCol}),
				right: typedExprPtr(typedExpr{bound: intLit(40)}),
			},
			want: []bool{false, true, false},
		},
		{
			name: "reversed_int_literal_column_comparison",
			expr: typedExpr{
				bound: &boundBinary{raw: &ast.BinaryExpr{Op: "<"}, left: intLit(40), right: ageCol},
				left:  typedExprPtr(typedExpr{bound: intLit(40)}),
				right: typedExprPtr(typedExpr{bound: ageCol}),
			},
			want: []bool{false, true, false},
		},
		{
			name: "string_column_literal_comparison",
			expr: typedExpr{
				bound: &boundBinary{raw: &ast.BinaryExpr{Op: "=="}, left: nameCol, right: stringLit("alice")},
				left:  typedExprPtr(typedExpr{bound: nameCol}),
				right: typedExprPtr(typedExpr{bound: stringLit("alice")}),
			},
			want: []bool{true, false, false},
		},
		{
			name: "bool_column_literal_comparison",
			expr: typedExpr{
				bound: &boundBinary{raw: &ast.BinaryExpr{Op: "=="}, left: flagCol, right: boolLit(true)},
				left:  typedExprPtr(typedExpr{bound: flagCol}),
				right: typedExprPtr(typedExpr{bound: boolLit(true)}),
			},
			want: []bool{true, false, false},
		},
		{
			name: "null_literal_comparison_drops_all",
			expr: typedExpr{
				bound: &boundBinary{raw: &ast.BinaryExpr{Op: ">"}, left: ageCol, right: nullLit},
				left:  typedExprPtr(typedExpr{bound: ageCol}),
				right: typedExprPtr(typedExpr{bound: nullLit}),
			},
			want: []bool{false, false, false},
		},
		{
			name: "string_predicate",
			expr: typedExpr{
				bound: &boundCall{raw: &ast.FuncCallExpr{Name: "str_contains"}},
				args:  []typedExpr{{bound: nameCol}, {bound: stringLit("999")}},
			},
			want: []bool{false, true, false},
		},
		{
			name: "string_prefix_predicate",
			expr: typedExpr{
				bound: &boundCall{raw: &ast.FuncCallExpr{Name: "starts_with"}},
				args:  []typedExpr{{bound: nameCol}, {bound: stringLit("ali")}},
			},
			want: []bool{true, false, false},
		},
		{
			name: "string_suffix_predicate",
			expr: typedExpr{
				bound: &boundCall{raw: &ast.FuncCallExpr{Name: "ends_with"}},
				args:  []typedExpr{{bound: nameCol}, {bound: stringLit("999")}},
			},
			want: []bool{false, true, false},
		},
		{
			name: "nested_dot_path_comparison",
			expr: typedExpr{
				bound: &boundBinary{raw: &ast.BinaryExpr{Op: ">"}, left: scoreCol, right: intLit(40)},
				left:  typedExprPtr(typedExpr{bound: scoreCol}),
				right: typedExprPtr(typedExpr{bound: intLit(40)}),
			},
			want: []bool{false, true, false},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pred := compileFilterPredicate(tc.expr, tbl)
			for row, want := range tc.want {
				got, err := pred(row)
				if err != nil {
					t.Fatalf("row %d returned error: %v", row, err)
				}
				if got != want {
					t.Fatalf("row %d: got %v, want %v", row, got, want)
				}
			}
		})
	}

	nonBool := typedExpr{bound: intLit(1)}
	pred := compileFilterPredicate(nonBool, tbl)
	if _, err := pred(0); err == nil || !strings.Contains(err.Error(), "did not return boolean") {
		t.Fatalf("expected non-boolean fallback error, got %v", err)
	}
}

func appendCompiledPredicateFlagColumn(t *testing.T, tbl *table.Table) *table.Table {
	t.Helper()
	withFlag := table.NewTable([]string{"age", "name", "profile", "flag"})
	for row, flag := range []table.Value{table.BoolVal(true), table.BoolVal(false), table.Null()} {
		withFlag.AddRow([]table.Value{
			tbl.Get(row, "age"),
			tbl.Get(row, "name"),
			tbl.Get(row, "profile"),
			flag,
		})
	}
	return withFlag
}

func typedExprPtr(expr typedExpr) *typedExpr {
	return &expr
}

func assertPrimitiveValue(t *testing.T, got, want table.Value) {
	t.Helper()
	if got.Type != want.Type {
		t.Fatalf("type got %v, want %v; value=%v", got.Type, want.Type, got)
	}
	switch want.Type {
	case table.TypeNull:
		if !got.IsNull() {
			t.Fatalf("want null, got %v", got)
		}
	case table.TypeInt:
		if got.Int != want.Int {
			t.Fatalf("int got %d, want %d", got.Int, want.Int)
		}
	case table.TypeFloat:
		if got.Float != want.Float {
			t.Fatalf("float got %v, want %v", got.Float, want.Float)
		}
	case table.TypeBool:
		if got.Bool != want.Bool {
			t.Fatalf("bool got %v, want %v", got.Bool, want.Bool)
		}
	default:
		t.Fatalf("unsupported primitive test type %v", want.Type)
	}
}
