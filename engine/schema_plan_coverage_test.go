package engine

import (
	"strings"
	"testing"

	"github.com/razeghi71/dq/ast"
	"github.com/razeghi71/dq/table"
)

func schemaPlanIntLit(v int64) *ast.LiteralExpr {
	return &ast.LiteralExpr{Kind: "int", Int: v}
}

func schemaPlanFloatLit(v float64) *ast.LiteralExpr {
	return &ast.LiteralExpr{Kind: "float", Float: v}
}

func schemaPlanStringLit(v string) *ast.LiteralExpr {
	return &ast.LiteralExpr{Kind: "string", Str: v}
}

func schemaPlanBoolLit(v bool) *ast.LiteralExpr {
	return &ast.LiteralExpr{Kind: "bool", Bool: v}
}

func schemaPlanCol(path ...string) *ast.ColumnExpr {
	return &ast.ColumnExpr{Path: path}
}

func schemaPlanCall(name string, args ...ast.Expr) *ast.FuncCallExpr {
	return &ast.FuncCallExpr{Name: name, Args: args}
}

func schemaPlanBinary(op string, left, right ast.Expr) *ast.BinaryExpr {
	return &ast.BinaryExpr{Op: op, Left: left, Right: right}
}

func schemaPlanUnary(op string, operand ast.Expr) *ast.UnaryExpr {
	return &ast.UnaryExpr{Op: op, Operand: operand}
}

func schemaPlanNestedSchema() *table.TypeDescriptor {
	return &table.TypeDescriptor{Kind: table.TypeRecord, Fields: []table.FieldDescriptor{
		{Name: "age", Type: &table.TypeDescriptor{Kind: table.TypeInt}},
		{Name: "name", Type: &table.TypeDescriptor{Kind: table.TypeString}},
		{Name: "tags", Type: &table.TypeDescriptor{Kind: table.TypeList, Elem: &table.TypeDescriptor{Kind: table.TypeString}}},
		{Name: "u", Type: &table.TypeDescriptor{Kind: table.TypeUnion, Branches: []*table.TypeDescriptor{
			{Kind: table.TypeInt},
			{Kind: table.TypeString},
		}}},
	}}
}

func requireSchemaPlanSchema(t *testing.T, got *table.TypeDescriptor, want string) {
	t.Helper()
	if got == nil {
		t.Fatalf("schema: got nil, want %s", want)
	}
	if schema := table.Render(table.FinalizeSchema(got)); schema != want {
		t.Fatalf("schema: got %s, want %s", schema, want)
	}
}

func TestTypedPlannerAddsExplicitCoercionOnlyWhereNeeded(t *testing.T) {
	tbl := table.NewTable([]string{"id"})
	tbl.AddRow([]table.Value{table.IntVal(9007199254740993)})

	cases := []struct {
		name        string
		expr        ast.Expr
		wantCoerces int
	}{
		{
			name:        "simple_comparison_has_no_coercion_nodes",
			expr:        schemaPlanBinary("==", schemaPlanCol("id"), schemaPlanIntLit(999999)),
			wantCoerces: 0,
		},
		{
			name:        "same_type_list_literal_has_no_coercion_nodes",
			expr:        schemaPlanCall("list_contains", &ast.ListExpr{Elements: []ast.Expr{schemaPlanCol("id"), schemaPlanIntLit(0)}}, schemaPlanIntLit(0)),
			wantCoerces: 0,
		},
		{
			name:        "coalesce_common_schema_has_one_result_coercion",
			expr:        schemaPlanBinary("==", schemaPlanCall("coalesce", schemaPlanCol("id"), schemaPlanFloatLit(0)), schemaPlanFloatLit(9007199254740992)),
			wantCoerces: 1,
		},
		{
			name:        "if_common_schema_has_one_result_coercion",
			expr:        schemaPlanBinary("==", schemaPlanCall("if", schemaPlanBoolLit(true), schemaPlanCol("id"), schemaPlanFloatLit(0)), schemaPlanFloatLit(9007199254740992)),
			wantCoerces: 1,
		},
		{
			name:        "list_literal_common_schema_has_one_list_coercion",
			expr:        schemaPlanCall("list_contains", &ast.ListExpr{Elements: []ast.Expr{schemaPlanCol("id"), schemaPlanFloatLit(0)}}, schemaPlanFloatLit(9007199254740992)),
			wantCoerces: 1,
		},
		{
			name:        "division_intrinsically_matches_planned_float",
			expr:        schemaPlanBinary("==", schemaPlanBinary("/", schemaPlanCol("id"), schemaPlanIntLit(1)), schemaPlanFloatLit(9007199254740992)),
			wantCoerces: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bound, err := bindLogicalExpressionInEnv(tc.expr, schemaEnvFromTable(tbl))
			if err != nil {
				t.Fatalf("bind: %v", err)
			}
			typed, err := typeCheckLogicalExpression(bound)
			if err != nil {
				t.Fatalf("type check: %v", err)
			}
			if got := countLogicalTypedCoercions(typed); got != tc.wantCoerces {
				t.Fatalf("coercion nodes: got %d, want %d", got, tc.wantCoerces)
			}
		})
	}
}

func TestRuntimeCoercionNeededIgnoresNullabilityOnly(t *testing.T) {
	listOf := func(elem *table.TypeDescriptor) *table.TypeDescriptor {
		return &table.TypeDescriptor{Kind: table.TypeList, Elem: elem}
	}
	recordOf := func(fields ...table.FieldDescriptor) *table.TypeDescriptor {
		return &table.TypeDescriptor{Kind: table.TypeRecord, Fields: fields}
	}
	unionOf := func(branches ...*table.TypeDescriptor) *table.TypeDescriptor {
		return &table.TypeDescriptor{Kind: table.TypeUnion, Branches: branches}
	}
	intType := &table.TypeDescriptor{Kind: table.TypeInt}
	nullableInt := &table.TypeDescriptor{Kind: table.TypeInt, Nullable: true}
	floatType := &table.TypeDescriptor{Kind: table.TypeFloat}
	stringType := &table.TypeDescriptor{Kind: table.TypeString}

	cases := []struct {
		name string
		from *table.TypeDescriptor
		to   *table.TypeDescriptor
		want bool
	}{
		{name: "same_scalar", from: intType, to: intType, want: false},
		{name: "top_level_nullability_only", from: nullableInt, to: intType, want: false},
		{name: "null_only_never_requires_runtime_change", from: &table.TypeDescriptor{Kind: table.TypeNull}, to: floatType, want: false},
		{name: "int_to_float_scalar", from: intType, to: floatType, want: true},
		{name: "list_nullability_only", from: listOf(nullableInt), to: listOf(intType), want: false},
		{name: "list_element_promotion", from: listOf(intType), to: listOf(floatType), want: true},
		{
			name: "record_order_and_nullability_only",
			from: recordOf(
				table.FieldDescriptor{Name: "y", Type: &table.TypeDescriptor{Kind: table.TypeString, Nullable: true}},
				table.FieldDescriptor{Name: "x", Type: intType},
			),
			to: recordOf(
				table.FieldDescriptor{Name: "x", Type: nullableInt},
				table.FieldDescriptor{Name: "y", Type: stringType},
			),
			want: false,
		},
		{
			name: "record_missing_nullable_field_requires_materialized_null",
			from: recordOf(table.FieldDescriptor{Name: "x", Type: intType}),
			to: recordOf(
				table.FieldDescriptor{Name: "x", Type: intType},
				table.FieldDescriptor{Name: "y", Type: &table.TypeDescriptor{Kind: table.TypeString, Nullable: true}},
			),
			want: true,
		},
		{
			name: "union_same_branches",
			from: unionOf(intType, stringType),
			to:   unionOf(nullableInt, stringType),
			want: false,
		},
		{
			name: "union_branch_promotion",
			from: unionOf(intType, stringType),
			to:   unionOf(floatType, stringType),
			want: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := runtimeCoercionNeeded(tc.from, tc.to); got != tc.want {
				t.Fatalf("runtimeCoercionNeeded: got %v, want %v", got, tc.want)
			}
		})
	}
}

func countLogicalTypedCoercions(expr logicalTypedExpr) int {
	count := 0
	if _, ok := expr.bound.(*logicalBoundCoerce); ok {
		count++
	}
	if expr.left != nil {
		count += countLogicalTypedCoercions(*expr.left)
	}
	if expr.right != nil {
		count += countLogicalTypedCoercions(*expr.right)
	}
	if expr.operand != nil {
		count += countLogicalTypedCoercions(*expr.operand)
	}
	for _, arg := range expr.args {
		count += countLogicalTypedCoercions(arg)
	}
	for _, field := range expr.fields {
		count += countLogicalTypedCoercions(field.expr)
	}
	for _, elem := range expr.elements {
		count += countLogicalTypedCoercions(elem)
	}
	return count
}

func TestPlanReduceExprSchemaEdges(t *testing.T) {
	nested := schemaPlanNestedSchema()
	cases := []struct {
		name string
		expr ast.Expr
		want string
	}{
		{name: "count", expr: schemaPlanCall("count"), want: "int"},
		{name: "sum", expr: schemaPlanCall("sum", schemaPlanCol("age")), want: "int?"},
		{name: "avg", expr: schemaPlanCall("avg", schemaPlanCol("age")), want: "float?"},
		{name: "min_string", expr: schemaPlanCall("min", schemaPlanCol("name")), want: "string?"},
		{name: "max_string", expr: schemaPlanCall("max", schemaPlanCol("name")), want: "string?"},
		{name: "first", expr: schemaPlanCall("first", schemaPlanCol("name")), want: "string?"},
		{name: "last", expr: schemaPlanCall("last", schemaPlanCol("name")), want: "string?"},
		{name: "aggregate_plus", expr: schemaPlanBinary("+", schemaPlanCall("sum", schemaPlanCol("age")), schemaPlanCall("count")), want: "int?"},
		{name: "aggregate_divide_by_count", expr: schemaPlanBinary("/", schemaPlanCall("sum", schemaPlanCol("age")), schemaPlanCall("count")), want: "float?"},
		{name: "count_divide_by_nonzero_literal", expr: schemaPlanBinary("/", schemaPlanCall("count"), schemaPlanIntLit(2)), want: "float"},
		{name: "count_divide_by_zero_literal", expr: schemaPlanBinary("/", schemaPlanCall("count"), schemaPlanIntLit(0)), want: "float?"},
		{name: "aggregate_ordering_nullable", expr: schemaPlanBinary("<", schemaPlanCall("sum", schemaPlanCol("age")), schemaPlanCall("count")), want: "bool?"},
		{name: "aggregate_logical", expr: schemaPlanBinary("and", schemaPlanBinary("==", schemaPlanCall("count"), schemaPlanIntLit(0)), schemaPlanBoolLit(true)), want: "bool"},
		{name: "aggregate_not", expr: schemaPlanUnary("not", schemaPlanBinary("==", schemaPlanCall("count"), schemaPlanIntLit(0))), want: "bool"},
		{name: "aggregate_negation", expr: schemaPlanUnary("-", schemaPlanCall("sum", schemaPlanCol("age"))), want: "int?"},
		{name: "aggregate_is_null", expr: &ast.IsNullExpr{Operand: schemaPlanCall("first", schemaPlanCol("name"))}, want: "bool"},
		{name: "literal", expr: schemaPlanIntLit(7), want: "int"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			typed, err := planPhysicalReduceExprForTest(tc.expr, nested)
			if err != nil {
				t.Fatalf("plan reduce: %v", err)
			}
			requireSchemaPlanSchema(t, typed.typ, tc.want)
		})
	}

	invalid := []struct {
		name    string
		expr    ast.Expr
		wantErr string
	}{
		{name: "sum_string", expr: schemaPlanCall("sum", schemaPlanCol("name")), wantErr: "requires a numeric column"},
		{name: "min_list", expr: schemaPlanCall("min", schemaPlanCol("tags")), wantErr: "requires an orderable column"},
		{name: "unknown_aggregate", expr: schemaPlanCall("median", schemaPlanCol("age")), wantErr: "unknown function"},
		{name: "bad_arity", expr: schemaPlanCall("count", schemaPlanCol("age")), wantErr: "count() takes no arguments"},
		{name: "bad_binary_operand", expr: schemaPlanBinary("+", schemaPlanCall("sum", schemaPlanCol("name")), schemaPlanCall("count")), wantErr: "requires a numeric column"},
		{name: "bad_order_operand", expr: schemaPlanBinary("<", schemaPlanCall("sum", schemaPlanCol("name")), schemaPlanCall("count")), wantErr: "requires a numeric column"},
		{name: "bad_logical_operand", expr: schemaPlanBinary("and", schemaPlanCall("sum", schemaPlanCol("name")), schemaPlanBoolLit(true)), wantErr: "requires a numeric column"},
		{name: "bad_unary_operand", expr: schemaPlanUnary("-", schemaPlanCall("first", schemaPlanCol("name"))), wantErr: "requires numeric operand"},
		{name: "unknown_unary", expr: schemaPlanUnary("~", schemaPlanCall("count")), wantErr: "unknown unary operator"},
		{name: "unknown_binary", expr: schemaPlanBinary("%", schemaPlanCall("count"), schemaPlanIntLit(2)), wantErr: "unknown operator"},
	}

	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			_, err := planPhysicalReduceExprForTest(tc.expr, nested)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestPlanTransformExprSchemaEdges(t *testing.T) {
	tbl := nullablePlanningTable()
	cases := []struct {
		name string
		expr ast.Expr
		want string
	}{
		{name: "string_plus", expr: schemaPlanBinary("+", schemaPlanStringLit("a"), schemaPlanStringLit("b")), want: "string"},
		{name: "int_divide_by_nonzero", expr: schemaPlanBinary("/", schemaPlanIntLit(6), schemaPlanIntLit(3)), want: "float"},
		{name: "int_divide_by_zero", expr: schemaPlanBinary("/", schemaPlanIntLit(6), schemaPlanIntLit(0)), want: "float?"},
		{name: "nullable_ordering", expr: schemaPlanBinary("<", schemaPlanCol("n"), schemaPlanIntLit(10)), want: "bool?"},
		{name: "nullable_logical", expr: schemaPlanBinary("and", schemaPlanCol("flag"), schemaPlanBoolLit(true)), want: "bool?"},
		{name: "nullable_not", expr: schemaPlanUnary("not", schemaPlanCol("flag")), want: "bool?"},
		{name: "nullable_negation", expr: schemaPlanUnary("-", schemaPlanCol("n")), want: "int?"},
		{name: "upper", expr: schemaPlanCall("upper", schemaPlanCol("s")), want: "string?"},
		{name: "substr", expr: schemaPlanCall("substr", schemaPlanCol("s"), schemaPlanIntLit(0), schemaPlanCol("n")), want: "string?"},
		{name: "str_len", expr: schemaPlanCall("str_len", schemaPlanCol("s")), want: "int?"},
		{name: "str_contains", expr: schemaPlanCall("str_contains", schemaPlanCol("s"), schemaPlanStringLit("a")), want: "bool?"},
		{name: "coalesce", expr: schemaPlanCall("coalesce", schemaPlanCol("n"), schemaPlanIntLit(0)), want: "int"},
		{name: "if", expr: schemaPlanCall("if", schemaPlanCol("flag"), schemaPlanCol("n"), schemaPlanIntLit(0)), want: "int?"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			typed, err := planPhysicalTransformExprForTest(tc.expr, tbl)
			if err != nil {
				t.Fatalf("plan transform: %v", err)
			}
			requireSchemaPlanSchema(t, typed.typ, tc.want)
		})
	}

	invalid := []struct {
		name    string
		expr    ast.Expr
		wantErr string
	}{
		{name: "unknown_binary", expr: schemaPlanBinary("%", schemaPlanIntLit(1), schemaPlanIntLit(2)), wantErr: "unknown operator"},
		{name: "bad_numeric_binary", expr: schemaPlanBinary("-", schemaPlanStringLit("a"), schemaPlanIntLit(2)), wantErr: "requires numeric operands"},
		{name: "unknown_unary", expr: schemaPlanUnary("~", schemaPlanIntLit(1)), wantErr: "unknown unary operator"},
		{name: "bad_unary", expr: schemaPlanUnary("-", schemaPlanStringLit("a")), wantErr: "requires numeric operand"},
		{name: "unknown_func", expr: schemaPlanCall("unknown", schemaPlanIntLit(1)), wantErr: "unknown function"},
		{name: "substr_arity", expr: schemaPlanCall("substr", schemaPlanStringLit("abc")), wantErr: "substr() takes 3 arguments"},
		{name: "coalesce_arity", expr: schemaPlanCall("coalesce"), wantErr: "coalesce() requires at least 1 argument"},
		{name: "coalesce_mismatch", expr: schemaPlanCall("coalesce", schemaPlanIntLit(1), schemaPlanStringLit("x")), wantErr: "do not have one common type"},
		{name: "if_arity", expr: schemaPlanCall("if", schemaPlanBoolLit(true), schemaPlanIntLit(1)), wantErr: "if() takes 3 arguments"},
		{name: "if_mismatch", expr: schemaPlanCall("if", schemaPlanBoolLit(true), schemaPlanIntLit(1), schemaPlanStringLit("x")), wantErr: "branches do not have one common type"},
	}

	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			_, err := planPhysicalTransformExprForTest(tc.expr, tbl)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestPlanFilterExprRequiresBooleanResult(t *testing.T) {
	typed, err := planPhysicalFilterExprForTest(schemaPlanBinary(">", schemaPlanCol("n"), schemaPlanIntLit(1)), nullablePlanningTable())
	if err != nil {
		t.Fatalf("plan filter: %v", err)
	}
	requireSchemaPlanSchema(t, typed.typ, "bool?")

	_, err = planPhysicalFilterExprForTest(schemaPlanCol("n"), nullablePlanningTable())
	if err == nil || !strings.Contains(err.Error(), "filter expression must return bool") {
		t.Fatalf("expected non-bool filter error, got %v", err)
	}
}

func TestNumericExprKnownNonZeroEdges(t *testing.T) {
	cases := []struct {
		name string
		expr ast.Expr
		want bool
	}{
		{name: "int_zero", expr: schemaPlanIntLit(0), want: false},
		{name: "int_nonzero", expr: schemaPlanIntLit(2), want: true},
		{name: "float_zero", expr: schemaPlanFloatLit(0), want: false},
		{name: "float_nonzero", expr: schemaPlanFloatLit(2.5), want: true},
		{name: "negative_nonzero", expr: schemaPlanUnary("-", schemaPlanIntLit(2)), want: true},
		{name: "string", expr: schemaPlanStringLit("2"), want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := numericExprKnownNonZero(tc.expr); got != tc.want {
				t.Fatalf("numericExprKnownNonZero: got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestTypedPlannerRejectsUnionMisuseWithoutLegacySchemaInference(t *testing.T) {
	unionSchema := &table.TypeDescriptor{Kind: table.TypeUnion, Branches: []*table.TypeDescriptor{
		{Kind: table.TypeInt},
		{Kind: table.TypeString},
	}}
	tbl := table.NewTableWithSchemas([]string{"u", "xs"}, []*table.TypeDescriptor{
		unionSchema,
		{Kind: table.TypeList, Elem: unionSchema},
	})
	nested := recordSchemaForEnv(schemaEnvFromTable(tbl))

	cases := []struct {
		name    string
		expr    ast.Expr
		filter  bool
		reduce  bool
		wantErr string
	}{
		{name: "transform_arithmetic", expr: schemaPlanBinary("+", schemaPlanCol("u"), schemaPlanIntLit(1)), wantErr: "requires numeric operands"},
		{name: "transform_string_function", expr: schemaPlanCall("upper", schemaPlanCol("u")), wantErr: "requires a string"},
		{name: "transform_comparison", expr: schemaPlanBinary("==", schemaPlanCol("u"), schemaPlanCol("u")), wantErr: "cannot compare union values"},
		{name: "filter_direct_union", expr: schemaPlanCol("u"), filter: true, wantErr: "filter expression must return bool"},
		{name: "filter_comparison_union", expr: schemaPlanBinary("==", schemaPlanCol("u"), schemaPlanCol("u")), filter: true, wantErr: "cannot compare union values"},
		{name: "if_union_condition", expr: schemaPlanCall("if", schemaPlanCol("u"), schemaPlanIntLit(1), schemaPlanIntLit(2)), wantErr: "condition must be boolean"},
		{name: "coalesce_union_branch_preserves_union", expr: schemaPlanCall("coalesce", schemaPlanCol("u"), schemaPlanIntLit(1)), wantErr: ""},
		{name: "list_len_allows_list_containing_union", expr: schemaPlanCall("list_len", schemaPlanCol("xs")), wantErr: ""},
		{name: "reduce_sum_union", expr: schemaPlanCall("sum", schemaPlanCol("u")), reduce: true, wantErr: "requires a numeric column"},
		{name: "reduce_first_arithmetic", expr: schemaPlanBinary("+", schemaPlanCall("first", schemaPlanCol("u")), schemaPlanIntLit(1)), reduce: true, wantErr: "requires numeric operands"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var err error
			switch {
			case tc.reduce:
				_, err = planPhysicalReduceExprForTest(tc.expr, nested)
			case tc.filter:
				_, err = planPhysicalFilterExprForTest(tc.expr, tbl)
			default:
				_, err = planPhysicalTransformExprForTest(tc.expr, tbl)
			}
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestRuntimeFunctionEdgeDiagnostics(t *testing.T) {
	cases := []struct {
		name    string
		fn      string
		args    []table.Value
		wantErr string
		want    table.Value
	}{
		{
			name:    "coalesce_requires_argument",
			fn:      "coalesce",
			wantErr: "coalesce() requires at least 1 argument",
		},
		{
			name: "coalesce_all_null_returns_null",
			fn:   "coalesce",
			args: []table.Value{table.Null(), table.Null()},
			want: table.Null(),
		},
		{
			name:    "if_requires_three_args",
			fn:      "if",
			args:    []table.Value{table.BoolVal(true), table.IntVal(1)},
			wantErr: "if() takes 3 arguments",
		},
		{
			name:    "if_requires_bool_condition",
			fn:      "if",
			args:    []table.Value{table.IntVal(1), table.IntVal(2), table.IntVal(3)},
			wantErr: "condition must be boolean",
		},
		{
			name: "if_false_takes_else",
			fn:   "if",
			args: []table.Value{table.BoolVal(false), table.IntVal(2), table.IntVal(3)},
			want: table.IntVal(3),
		},
		{
			name:    "list_contains_requires_two_args",
			fn:      "list_contains",
			args:    []table.Value{table.ListVal(nil)},
			wantErr: "list_contains() takes 2 arguments",
		},
		{
			name:    "list_contains_requires_list",
			fn:      "list_contains",
			args:    []table.Value{table.StrVal("x"), table.StrVal("x")},
			wantErr: "list_contains() requires a list",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := builtinCatalog[tc.fn]
			got, err := spec.TypedEval(typedExprsForValues(tc.args), &EvalContext{})
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("expected error containing %q, got value=%v err=%v", tc.wantErr, got, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if eq, _ := expressionValuesEqual(got, tc.want, true); !eq {
				t.Fatalf("value: got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestValueTypeNameCoversRuntimeDiagnostics(t *testing.T) {
	cases := []struct {
		value table.Value
		want  string
	}{
		{value: table.Null(), want: "null"},
		{value: table.IntVal(1), want: "int"},
		{value: table.FloatVal(1), want: "float"},
		{value: table.StrVal("x"), want: "string"},
		{value: table.BoolVal(true), want: "bool"},
		{value: table.ListVal(nil), want: "list"},
		{value: table.RecordVal(nil), want: "record"},
		{value: table.Value{Type: table.TypeMixed}, want: "unknown"},
	}

	for _, tc := range cases {
		if got := valueTypeName(tc.value); got != tc.want {
			t.Fatalf("valueTypeName(%v): got %s, want %s", tc.value.Type, got, tc.want)
		}
	}
}
