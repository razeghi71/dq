package engine

import (
	"errors"
	"strings"
	"testing"

	"github.com/razeghi71/dq/ast"
	"github.com/razeghi71/dq/table"
)

func TestFusedGroupReduceCoveragePayloadRetainedExercisesAggregateStates(t *testing.T) {
	input := table.NewTableWithSchemas(
		[]string{"city", "age", "amount", "name"},
		[]*table.TypeDescriptor{
			{Kind: table.TypeString},
			{Kind: table.TypeInt},
			{Kind: table.TypeFloat, Nullable: true},
			{Kind: table.TypeString, Nullable: true},
		},
	)
	mustAddFusedGroupReduceTDDRow(t, input, table.StrVal("A"), table.IntVal(10), table.FloatVal(1.5), table.Null())
	mustAddFusedGroupReduceTDDRow(t, input, table.StrVal("A"), table.IntVal(20), table.Null(), table.StrVal("bob"))
	mustAddFusedGroupReduceTDDRow(t, input, table.StrVal("B"), table.IntVal(7), table.FloatVal(3.5), table.StrVal("ann"))

	out := runQuery(t, input, `
		group city
		| reduce total = sum(age), avg_amount = avg(amount), min_name = min(name), max_age = max(age),
		         first_name = first(name), last_name = last(name), ratio = sum(age) / count(),
		         neg_total = -sum(age), has_rows = count() > 0 and true, first_is_null = first(name) is null
		| filter { list_len(grouped) > 0 }
		| sort city
		| select city, grouped, total, avg_amount, min_name, max_age, first_name, last_name, ratio, neg_total, has_rows, first_is_null
	`)

	requireSourceProjectionTDDTableColumns(t, out,
		"city", "grouped", "total", "avg_amount", "min_name", "max_age",
		"first_name", "last_name", "ratio", "neg_total", "has_rows", "first_is_null",
	)
	if got := out.GetAt(0, out.ColIndex("total")).Int; got != 30 {
		t.Fatalf("A total: got %d, want 30", got)
	}
	if got := out.GetAt(0, out.ColIndex("avg_amount")).Float; got != 1.5 {
		t.Fatalf("A avg_amount: got %v, want 1.5", got)
	}
	if got := out.GetAt(0, out.ColIndex("max_age")).Int; got != 20 {
		t.Fatalf("A max_age: got %d, want 20", got)
	}
	if got := out.GetAt(0, out.ColIndex("first_name")); !got.IsNull() {
		t.Fatalf("A first_name: got %v, want null", got)
	}
	if got := out.GetAt(0, out.ColIndex("last_name")).Str; got != "bob" {
		t.Fatalf("A last_name: got %q, want bob", got)
	}
	if got := out.GetAt(0, out.ColIndex("has_rows")); !got.IsExplicitTrue() {
		t.Fatalf("A has_rows: got %v, want true", got)
	}
	if got := out.GetAt(0, out.ColIndex("first_is_null")); !got.IsExplicitTrue() {
		t.Fatalf("A first_is_null: got %v, want true", got)
	}
	if got := out.GetAt(0, out.ColIndex("grouped")); got.Type != table.TypeList || len(got.List) != 2 {
		t.Fatalf("A grouped payload: got %v, want two records", got)
	}
}

func TestFusedGroupReduceCoverageAccumulatorAlgebra(t *testing.T) {
	t.Run("empty_finalizers", func(t *testing.T) {
		for _, state := range []aggregateAccumulator{
			&sumAccumulator{arg: fusedGroupReduceCoverageValues(nil), hasInt: true},
			&avgAccumulator{arg: fusedGroupReduceCoverageValues(nil)},
			&extremumAccumulator{name: "min", arg: fusedGroupReduceCoverageValues(nil), better: func(cmp int) bool { return cmp < 0 }},
			&firstAccumulator{arg: fusedGroupReduceCoverageValues(nil)},
			&lastAccumulator{arg: fusedGroupReduceCoverageValues(nil)},
		} {
			got, err := state.Finalize()
			if err != nil {
				t.Fatalf("empty finalizer error: %v", err)
			}
			if !got.IsNull() {
				t.Fatalf("empty finalizer: got %v, want null", got)
			}
		}
	})

	t.Run("sum_float_and_non_numeric", func(t *testing.T) {
		floatSum := &sumAccumulator{arg: fusedGroupReduceCoverageValues([]table.Value{table.IntVal(2), table.FloatVal(1.5)}), hasInt: true}
		for row := 0; row < 2; row++ {
			if err := floatSum.Update(row); err != nil {
				t.Fatalf("float sum update: %v", err)
			}
		}
		got, err := floatSum.Finalize()
		if err != nil {
			t.Fatalf("float sum finalize: %v", err)
		}
		if got.Type != table.TypeFloat || got.Float != 3.5 {
			t.Fatalf("float sum: got %v, want 3.5", got)
		}

		bad := &sumAccumulator{arg: fusedGroupReduceCoverageValues([]table.Value{table.StrVal("x")}), hasInt: true}
		if err := bad.Update(0); err == nil || !strings.Contains(err.Error(), "non-numeric") {
			t.Fatalf("bad sum update: got %v, want non-numeric error", err)
		}
	})

	t.Run("unknown_aggregate_fails_closed", func(t *testing.T) {
		if _, err := newAggregateAccumulator(aggregateRuntimeSlot{name: "median"}); err == nil || !strings.Contains(err.Error(), "unknown aggregate") {
			t.Fatalf("unknown aggregate: got %v", err)
		}
		if _, err := newAggregateAccumulators([]aggregateRuntimeSlot{{name: "median"}}); err == nil || !strings.Contains(err.Error(), "unknown aggregate") {
			t.Fatalf("unknown aggregate slots: got %v", err)
		}
	})
}

func TestFusedGroupReduceCoverageRuntimeSlotFallbackAndEmptyInput(t *testing.T) {
	input := table.NewTableWithSchemas(
		[]string{"city", "amount"},
		[]*table.TypeDescriptor{{Kind: table.TypeString}, {Kind: table.TypeInt}},
	)

	slots := runtimeAggregateSlots([]plannedAggregateSlot{
		{name: "sum", aggregate: builtinCatalog["sum"].Aggregate, args: []boundColumn{{rawPath: []string{"amount"}, topIndex: -1}}},
	}, input)
	if len(slots) != 1 || len(slots[0].args) != 1 || slots[0].args[0] == nil {
		t.Fatalf("runtime slot fallback did not bind aggregate arg")
	}

	out := runQuery(t, input, `group city | reduce total = sum(amount) | remove grouped`)
	requireSourceProjectionTDDTableColumns(t, out, "city", "total")
	if out.NumRows != 0 {
		t.Fatalf("empty fused group/reduce rows: got %d, want 0", out.NumRows)
	}
}

func TestFusedGroupReduceCoverageAggregateFinalExpressionAlgebra(t *testing.T) {
	states := []aggregateAccumulator{&countAccumulator{n: 2}}

	t.Run("operators", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			expr aggregateFinalExpr
		}{
			{name: "add", expr: aggregateFinalBinaryExpr("+", table.IntVal(5), table.IntVal(3))},
			{name: "subtract", expr: aggregateFinalBinaryExpr("-", table.IntVal(5), table.IntVal(3))},
			{name: "multiply", expr: aggregateFinalBinaryExpr("*", table.IntVal(5), table.IntVal(3))},
			{name: "divide", expr: aggregateFinalBinaryExpr("/", table.IntVal(6), table.IntVal(3))},
			{name: "equal", expr: aggregateFinalBinaryExpr("==", table.IntVal(5), table.IntVal(5))},
			{name: "not_equal", expr: aggregateFinalBinaryExpr("!=", table.IntVal(5), table.IntVal(3))},
			{name: "less", expr: aggregateFinalBinaryExpr("<", table.IntVal(3), table.IntVal(5))},
			{name: "less_equal", expr: aggregateFinalBinaryExpr("<=", table.IntVal(5), table.IntVal(5))},
			{name: "greater", expr: aggregateFinalBinaryExpr(">", table.IntVal(5), table.IntVal(3))},
			{name: "greater_equal", expr: aggregateFinalBinaryExpr(">=", table.IntVal(5), table.IntVal(5))},
			{name: "and", expr: aggregateFinalBinaryExpr("and", table.BoolVal(true), table.BoolVal(true))},
			{name: "or", expr: aggregateFinalBinaryExpr("or", table.BoolVal(false), table.BoolVal(true))},
			{name: "not", expr: aggregateFinalExpr{kind: aggregateFinalUnary, op: "not", operand: aggregateFinalLiteralPtr(table.BoolVal(false))}},
			{name: "negate_float", expr: aggregateFinalExpr{kind: aggregateFinalUnary, op: "-", operand: aggregateFinalLiteralPtr(table.FloatVal(1.5))}},
			{name: "null_arithmetic", expr: aggregateFinalBinaryExpr("+", table.Null(), table.IntVal(3))},
			{name: "null_negation", expr: aggregateFinalExpr{kind: aggregateFinalUnary, op: "-", operand: aggregateFinalLiteralPtr(table.Null())}},
			{name: "is_null", expr: aggregateFinalExpr{kind: aggregateFinalIsNull, op: "is null", operand: aggregateFinalLiteralPtr(table.Null())}},
			{name: "is_not_null", expr: aggregateFinalExpr{kind: aggregateFinalIsNull, op: "is not null", operand: aggregateFinalLiteralPtr(table.IntVal(1))}},
			{name: "coerce", expr: aggregateFinalExpr{kind: aggregateFinalCoerce, operand: aggregateFinalLiteralPtr(table.IntVal(1)), coerceTo: &table.TypeDescriptor{Kind: table.TypeFloat}}},
			{name: "slot", expr: aggregateFinalExpr{kind: aggregateFinalSlot, slot: 0}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				if _, err := evalAggregateFinalExpr(tc.expr, states); err != nil {
					t.Fatalf("eval %s: %v", tc.name, err)
				}
			})
		}
	})

	t.Run("defensive_errors", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			expr aggregateFinalExpr
			want string
		}{
			{name: "bad_binary_left", expr: aggregateFinalExpr{kind: aggregateFinalBinary, op: "+", left: nil, right: aggregateFinalLiteralPtr(table.IntVal(2))}, want: "missing"},
			{name: "bad_binary_right", expr: aggregateFinalExpr{kind: aggregateFinalBinary, op: "+", left: aggregateFinalLiteralPtr(table.IntVal(1)), right: nil}, want: "missing"},
			{name: "bad_binary", expr: aggregateFinalBinaryExpr("???", table.IntVal(1), table.IntVal(2)), want: "unknown operator"},
			{name: "bad_unary_child", expr: aggregateFinalExpr{kind: aggregateFinalUnary, op: "-", operand: nil}, want: "missing"},
			{name: "bad_unary", expr: aggregateFinalExpr{kind: aggregateFinalUnary, op: "???", operand: aggregateFinalLiteralPtr(table.IntVal(1))}, want: "unknown unary"},
			{name: "bad_negation_type", expr: aggregateFinalExpr{kind: aggregateFinalUnary, op: "-", operand: aggregateFinalLiteralPtr(table.StrVal("x"))}, want: "cannot negate"},
			{name: "bad_is_null_child", expr: aggregateFinalExpr{kind: aggregateFinalIsNull, op: "is null", operand: nil}, want: "missing"},
			{name: "bad_coerce_child", expr: aggregateFinalExpr{kind: aggregateFinalCoerce, operand: nil, coerceTo: &table.TypeDescriptor{Kind: table.TypeFloat}}, want: "missing"},
			{name: "bad_slot", expr: aggregateFinalExpr{kind: aggregateFinalSlot, slot: 10}, want: "out of range"},
			{name: "bad_kind", expr: aggregateFinalExpr{kind: aggregateFinalExprKind(99)}, want: "unknown aggregate final"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				if _, err := evalAggregateFinalExpr(tc.expr, states); err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("eval error: got %v, want %q", err, tc.want)
				}
			})
		}
		if _, err := evalAggregateFinalExprPtr(nil, states); err == nil || !strings.Contains(err.Error(), "missing") {
			t.Fatalf("nil child error: got %v", err)
		}
	})
}

func TestFusedGroupReduceCoverageCompileAggregateFinalExprErrors(t *testing.T) {
	cases := []struct {
		name string
		expr typedExpr
		want string
	}{
		{
			name: "unknown_function",
			expr: typedExpr{bound: &boundCall{raw: &ast.FuncCallExpr{Name: "wat"}}},
			want: "unknown aggregate",
		},
		{
			name: "non_aggregate_function",
			expr: typedExpr{bound: &boundCall{raw: &ast.FuncCallExpr{Name: "upper"}}},
			want: "non-aggregate",
		},
		{
			name: "aggregate_arg_count",
			expr: typedExpr{bound: &boundCall{raw: &ast.FuncCallExpr{Name: "sum"}}, args: nil},
			want: "takes 1 argument",
		},
		{
			name: "aggregate_arg_not_column",
			expr: typedExpr{
				bound: &boundCall{raw: &ast.FuncCallExpr{Name: "sum"}},
				args:  []typedExpr{{bound: &boundLiteral{raw: &ast.LiteralExpr{Kind: "int", Int: 1}}}},
			},
			want: "column reference",
		},
		{
			name: "direct_column",
			expr: typedExpr{bound: &boundColumn{rawPath: []string{"x"}}},
			want: "directly",
		},
		{
			name: "struct",
			expr: typedExpr{bound: &boundStruct{}},
			want: "struct",
		},
		{
			name: "list",
			expr: typedExpr{bound: &boundList{}},
			want: "list",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var slots []plannedAggregateSlot
			if _, err := compileAggregateFinalExpr(tc.expr, &slots); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("compile error: got %v, want %q", err, tc.want)
			}
		})
	}

	if _, err := compileAggregateFinalExprPtr(nil, nil); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("nil compile child: got %v", err)
	}
}

func TestFusedGroupReduceCoverageAccumulatorInputErrors(t *testing.T) {
	wantErr := errors.New("boom")
	evalErr := func(int) (table.Value, error) { return table.Null(), wantErr }
	for _, state := range []aggregateAccumulator{
		&sumAccumulator{arg: evalErr, hasInt: true},
		&avgAccumulator{arg: evalErr},
		&extremumAccumulator{name: "min", arg: evalErr, better: func(cmp int) bool { return cmp < 0 }},
		&firstAccumulator{arg: evalErr},
		&lastAccumulator{arg: evalErr},
	} {
		if err := state.Update(0); !errors.Is(err, wantErr) {
			t.Fatalf("update error: got %v, want boom", err)
		}
	}
}

func fusedGroupReduceCoverageValues(vals []table.Value) rowValueEvaluator {
	return func(row int) (table.Value, error) {
		if row < 0 || row >= len(vals) {
			return table.Null(), nil
		}
		return vals[row], nil
	}
}

func aggregateFinalLiteralPtr(v table.Value) *aggregateFinalExpr {
	return &aggregateFinalExpr{kind: aggregateFinalLiteral, literal: v}
}

func aggregateFinalBinaryExpr(op string, left, right table.Value) aggregateFinalExpr {
	return aggregateFinalExpr{
		kind:  aggregateFinalBinary,
		op:    op,
		left:  aggregateFinalLiteralPtr(left),
		right: aggregateFinalLiteralPtr(right),
	}
}
