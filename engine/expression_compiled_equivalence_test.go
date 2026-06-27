package engine

import (
	"math"
	"strings"
	"testing"

	"github.com/razeghi71/dq/ast"
	"github.com/razeghi71/dq/table"
)

func compiledEquivalenceTable(t *testing.T) *table.Table {
	t.Helper()
	payloadSchema := &table.TypeDescriptor{Kind: table.TypeRecord, Fields: []table.FieldDescriptor{
		{Name: "n", Type: &table.TypeDescriptor{Kind: table.TypeInt, Nullable: true}},
		{Name: "s", Type: &table.TypeDescriptor{Kind: table.TypeString, Nullable: true}},
	}}
	tbl := table.NewTableWithSchemas(
		[]string{"id", "score", "name", "flag", "payload"},
		[]*table.TypeDescriptor{
			{Kind: table.TypeInt, Nullable: true},
			{Kind: table.TypeFloat, Nullable: true},
			{Kind: table.TypeString, Nullable: true},
			{Kind: table.TypeBool, Nullable: true},
			table.WithNullable(payloadSchema),
		},
	)
	rows := [][]table.Value{
		{
			table.IntVal(1),
			table.FloatVal(1.5),
			table.StrVal("alpha"),
			table.BoolVal(true),
			table.RecordVal([]table.RecordField{{Name: "n", Value: table.IntVal(1)}, {Name: "s", Value: table.StrVal("alpha")}}),
		},
		{
			table.Null(),
			table.FloatVal(math.NaN()),
			table.Null(),
			table.BoolVal(false),
			table.Null(),
		},
		{
			table.IntVal(9007199254740993),
			table.FloatVal(9007199254740992),
			table.StrVal("beta"),
			table.BoolVal(true),
			table.RecordVal([]table.RecordField{{Name: "n", Value: table.IntVal(3)}, {Name: "s", Value: table.StrVal("beta")}}),
		},
		{
			table.IntVal(-2),
			table.FloatVal(math.Inf(1)),
			table.StrVal("gamma"),
			table.Null(),
			table.RecordVal([]table.RecordField{{Name: "n", Value: table.Null()}, {Name: "s", Value: table.StrVal("")}}),
		},
		{
			table.IntVal(math.MaxInt64),
			table.FloatVal(math.Inf(-1)),
			table.StrVal("zeta"),
			table.BoolVal(false),
			table.RecordVal([]table.RecordField{{Name: "n", Value: table.IntVal(math.MaxInt64)}, {Name: "s", Value: table.StrVal("zeta")}}),
		},
	}
	for _, row := range rows {
		if err := tbl.AddRowTyped(row); err != nil {
			t.Fatalf("add row: %v", err)
		}
	}
	return tbl
}

func TestCompiledPredicateMatchesGenericTypedEvaluation(t *testing.T) {
	tbl := compiledEquivalenceTable(t)
	cases := []struct {
		name string
		expr ast.Expr
	}{
		{name: "int_eq_literal", expr: schemaPlanBinary("==", schemaPlanCol("id"), schemaPlanIntLit(1))},
		{name: "int_float_exactness", expr: schemaPlanBinary("==", schemaPlanCol("id"), schemaPlanFloatLit(9007199254740992))},
		{name: "reversed_int_float_ordering", expr: schemaPlanBinary("<", schemaPlanFloatLit(9007199254740992), schemaPlanCol("id"))},
		{name: "float_nan_not_equal", expr: schemaPlanBinary("!=", schemaPlanCol("score"), schemaPlanFloatLit(math.NaN()))},
		{name: "float_infinity_ordering", expr: schemaPlanBinary(">", schemaPlanCol("score"), schemaPlanFloatLit(2))},
		{name: "string_eq_literal", expr: schemaPlanBinary("==", schemaPlanCol("name"), schemaPlanStringLit("alpha"))},
		{name: "bool_not_equal_literal", expr: schemaPlanBinary("!=", schemaPlanCol("flag"), schemaPlanBoolLit(true))},
		{name: "nested_int_path", expr: schemaPlanBinary(">=", schemaPlanCol("payload", "n"), schemaPlanIntLit(3))},
		{name: "contains_string", expr: schemaPlanCall("str_contains", schemaPlanCol("name"), schemaPlanStringLit("a"))},
		{name: "starts_with_string", expr: schemaPlanCall("starts_with", schemaPlanCol("name"), schemaPlanStringLit("a"))},
		{name: "ends_with_string", expr: schemaPlanCall("ends_with", schemaPlanCol("name"), schemaPlanStringLit("a"))},
		{name: "nested_contains_string", expr: schemaPlanCall("str_contains", schemaPlanCol("payload", "s"), schemaPlanStringLit("ta"))},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			typed, err := planPhysicalFilterExprForTest(tc.expr, tbl)
			if err != nil {
				t.Fatalf("plan filter: %v", err)
			}
			fast, ok := compileFastPredicate(typed, tbl)
			if !ok {
				t.Fatal("expected compiled predicate")
			}
			for row := 0; row < tbl.NumRows; row++ {
				got, gotErr := fast(row)
				want, wantErr := genericPredicateResult(typed, tbl, row)
				assertSameErrorState(t, row, gotErr, wantErr)
				if gotErr == nil && got != want {
					t.Fatalf("row %d: compiled predicate got %v, want %v", row, got, want)
				}
			}
		})
	}
}

func TestCompiledRowValueMatchesGenericTypedEvaluation(t *testing.T) {
	tbl := compiledEquivalenceTable(t)
	cases := []struct {
		name string
		expr ast.Expr
	}{
		{name: "literal", expr: schemaPlanIntLit(7)},
		{name: "top_level_column", expr: schemaPlanCol("id")},
		{name: "nested_column", expr: schemaPlanCol("payload", "n")},
		{name: "int_add_literal", expr: schemaPlanBinary("+", schemaPlanCol("id"), schemaPlanIntLit(2))},
		{name: "reversed_int_subtract_literal", expr: schemaPlanBinary("-", schemaPlanIntLit(2), schemaPlanCol("id"))},
		{name: "int_multiply_literal", expr: schemaPlanBinary("*", schemaPlanCol("id"), schemaPlanIntLit(-3))},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			typed, err := planPhysicalTransformExprForTest(tc.expr, tbl)
			if err != nil {
				t.Fatalf("plan transform: %v", err)
			}
			fast, ok := compileFastRowValue(typed, tbl)
			if !ok {
				t.Fatal("expected compiled row value")
			}
			for row := 0; row < tbl.NumRows; row++ {
				got, gotErr := fast(row)
				want, wantErr := genericRowValueResult(typed, tbl, row)
				assertSameErrorState(t, row, gotErr, wantErr)
				if gotErr == nil && !sameCompiledTestValue(got, want) {
					t.Fatalf("row %d: compiled value got %s (%v), want %s (%v)", row, got.AsString(), got.Type, want.AsString(), want.Type)
				}
			}
		})
	}
}

func genericPredicateResult(expr typedExpr, tbl *table.Table, row int) (bool, error) {
	v, err := genericRowValueResult(expr, tbl, row)
	if err != nil {
		return false, err
	}
	switch {
	case v.IsExplicitTrue():
		return true, nil
	case v.IsBoolOrNull():
		return false, nil
	default:
		return false, nil
	}
}

func genericRowValueResult(expr typedExpr, tbl *table.Table, row int) (table.Value, error) {
	return evalTypedExpression(expr, &EvalContext{Table: tbl, RowIdx: row})
}

func assertSameErrorState(t *testing.T, row int, gotErr, wantErr error) {
	t.Helper()
	switch {
	case gotErr == nil && wantErr == nil:
		return
	case gotErr == nil || wantErr == nil:
		t.Fatalf("row %d: compiled error=%v, generic error=%v", row, gotErr, wantErr)
	case !strings.Contains(gotErr.Error(), wantErr.Error()) && !strings.Contains(wantErr.Error(), gotErr.Error()):
		t.Fatalf("row %d: compiled error=%v, generic error=%v", row, gotErr, wantErr)
	}
}

func sameCompiledTestValue(a, b table.Value) bool {
	if a.Type != b.Type {
		return false
	}
	switch a.Type {
	case table.TypeNull:
		return true
	case table.TypeInt:
		return a.Int == b.Int
	case table.TypeFloat:
		return a.Float == b.Float || (math.IsNaN(a.Float) && math.IsNaN(b.Float))
	case table.TypeString:
		return a.Str == b.Str
	case table.TypeBool:
		return a.Bool == b.Bool
	default:
		return a.AsString() == b.AsString()
	}
}
