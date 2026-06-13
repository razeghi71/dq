package engine

import (
	"strings"
	"testing"

	"github.com/razeghi71/dq/ast"
	"github.com/razeghi71/dq/table"
)

// truth is a three-valued boolean outcome for 3VL assertions.
type truth string

const (
	truthTrue  truth = "true"
	truthFalse truth = "false"
	truthNull  truth = "null"
)

func assertTruth(t *testing.T, v table.Value, want truth) {
	t.Helper()
	switch want {
	case truthNull:
		if !v.IsNull() {
			t.Errorf("expected null, got %v", v.AsString())
		}
	case truthTrue:
		if v.Type != table.TypeBool || !v.Bool {
			t.Errorf("expected true, got %v", v.AsString())
		}
	case truthFalse:
		if v.Type != table.TypeBool || v.Bool {
			t.Errorf("expected false, got %v", v.AsString())
		}
	default:
		t.Fatalf("unknown truth %q", want)
	}
}

func evalTransformLiteral(t *testing.T, expr string) table.Value {
	t.Helper()
	tbl := table.NewTable([]string{"dummy"})
	tbl.AddRow([]table.Value{table.IntVal(0)})
	result := runQuery(t, tbl, "transform result = "+expr+" | select result")
	return result.GetAt(0, 0)
}

func evalLiteralExpr(t *testing.T, expr ast.Expr) table.Value {
	t.Helper()
	tbl := table.NewTable([]string{"dummy"})
	tbl.AddRow([]table.Value{table.IntVal(0)})
	val, err := Eval(expr, &EvalContext{Table: tbl, RowIdx: 0})
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	return val
}

func boolLit(b bool) *ast.LiteralExpr {
	return &ast.LiteralExpr{Kind: "bool", Bool: b}
}

func nullLit() *ast.LiteralExpr {
	return &ast.LiteralExpr{Kind: "null"}
}

func binExpr(op string, left, right ast.Expr) *ast.BinaryExpr {
	return &ast.BinaryExpr{Op: op, Left: left, Right: right}
}

func unaryNot(operand ast.Expr) *ast.UnaryExpr {
	return &ast.UnaryExpr{Op: "not", Operand: operand}
}

func nullableBoolTable() *table.Table {
	tbl := table.NewTable([]string{"flag"})
	tbl.AddRow([]table.Value{table.Null()})
	tbl.AddRow([]table.Value{table.BoolVal(true)})
	tbl.AddRow([]table.Value{table.BoolVal(false)})
	return tbl
}

func nullableNamedBoolTable() *table.Table {
	tbl := table.NewTable([]string{"name", "flag"})
	tbl.AddRow([]table.Value{table.StrVal("unknown"), table.Null()})
	tbl.AddRow([]table.Value{table.StrVal("yes"), table.BoolVal(true)})
	tbl.AddRow([]table.Value{table.StrVal("no"), table.BoolVal(false)})
	return tbl
}

func groupedNullableBoolBatchTable() *table.Table {
	tbl := table.NewTable([]string{"batch", "name", "flag"})
	tbl.AddRow([]table.Value{table.StrVal("g"), table.StrVal("unknown"), table.Null()})
	tbl.AddRow([]table.Value{table.StrVal("g"), table.StrVal("yes"), table.BoolVal(true)})
	tbl.AddRow([]table.Value{table.StrVal("g"), table.StrVal("no"), table.BoolVal(false)})
	return tbl
}

func TestThreeValuedLogicNotNullLiteralViaEval(t *testing.T) {
	// Parser rejects `not null` in queries (006); runtime 3VL still defines not(null) = null.
	got := evalLiteralExpr(t, unaryNot(nullLit()))
	assertTruth(t, got, truthNull)
}

func TestThreeValuedLogicChained(t *testing.T) {
	cases := []struct {
		name string
		expr string
		want truth
	}{
		{"null_or_false_and_true", "(null or false) and true", truthNull},
		{"false_and_null_or_true", "(false and null) or true", truthTrue},
		{"not_null_or_false", "not false or null", truthTrue},
		{"not_true_and_null", "not true and null", truthFalse},
		{"null_and_true_or_false", "null and true or false", truthNull}, // (null and true) or false → null or false → null
		{"true_or_false_and_null", "true or false and null", truthTrue}, // true or (false and null) → true
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := evalTransformLiteral(t, tc.expr)
			assertTruth(t, got, tc.want)
		})
	}
}

func TestThreeValuedLogicNullableBoolColumn(t *testing.T) {
	tbl := nullableBoolTable()

	t.Run("not_flag", func(t *testing.T) {
		result := runQuery(t, tbl, "transform x = not flag | select x")
		assertTruth(t, result.GetAt(0, 0), truthNull)
		assertTruth(t, result.GetAt(1, 0), truthFalse)
		assertTruth(t, result.GetAt(2, 0), truthTrue)
	})

	t.Run("flag_and_true", func(t *testing.T) {
		result := runQuery(t, tbl, "transform x = flag and true | select x")
		assertTruth(t, result.GetAt(0, 0), truthNull)
		assertTruth(t, result.GetAt(1, 0), truthTrue)
		assertTruth(t, result.GetAt(2, 0), truthFalse)
	})

	t.Run("flag_or_false", func(t *testing.T) {
		result := runQuery(t, tbl, "transform x = flag or false | select x")
		assertTruth(t, result.GetAt(0, 0), truthNull)
		assertTruth(t, result.GetAt(1, 0), truthTrue)
		assertTruth(t, result.GetAt(2, 0), truthFalse)
	})

	t.Run("false_and_flag", func(t *testing.T) {
		result := runQuery(t, tbl, "transform x = false and flag | select x")
		assertTruth(t, result.GetAt(0, 0), truthFalse)
		assertTruth(t, result.GetAt(1, 0), truthFalse)
		assertTruth(t, result.GetAt(2, 0), truthFalse)
	})

	t.Run("true_or_flag", func(t *testing.T) {
		result := runQuery(t, tbl, "transform x = true or flag | select x")
		assertTruth(t, result.GetAt(0, 0), truthTrue)
		assertTruth(t, result.GetAt(1, 0), truthTrue)
		assertTruth(t, result.GetAt(2, 0), truthTrue)
	})

	t.Run("bare_flag_column", func(t *testing.T) {
		result := runQuery(t, tbl, "transform x = flag | select x")
		assertTruth(t, result.GetAt(0, 0), truthNull)
		assertTruth(t, result.GetAt(1, 0), truthTrue)
		assertTruth(t, result.GetAt(2, 0), truthFalse)
	})
}

func TestThreeValuedLogicIfUnchanged(t *testing.T) {
	cases := []struct {
		cond string
		want int64
	}{
		{"null", 2},
		{"false", 2},
		{"true", 1},
		{"null and true", 2},
		{"null or false", 2},
		{"null or true", 1},
		{"false and null", 2},
	}

	for _, tc := range cases {
		t.Run(tc.cond, func(t *testing.T) {
			got := evalTransformLiteral(t, "if("+tc.cond+", 1, 2)")
			if got.Type != table.TypeInt || got.Int != tc.want {
				t.Errorf("if(%s, 1, 2): expected %d, got %v", tc.cond, tc.want, got.AsString())
			}
		})
	}
}

func TestThreeValuedLogicIfNullableBoolColumn(t *testing.T) {
	// SQL CASE semantics: only explicit true takes then; null/false → else.
	tbl := nullableNamedBoolTable()
	result := runQuery(t, tbl, "transform x = if(flag, 1, 2) | select name, x | sort name")

	cases := []struct {
		name string
		want int64
	}{
		{"no", 2},
		{"unknown", 2},
		{"yes", 1},
	}
	for i, tc := range cases {
		if got := result.GetAt(i, 1).Int; got != tc.want {
			t.Errorf("%s: expected x=%d, got %d", tc.name, tc.want, got)
		}
	}
}

func TestThreeValuedLogicGroupReduceBooleanNull(t *testing.T) {
	// Transform (Eval) produces per-row ok = flag and true; reduce first(ok) reads nested values.
	tbl := groupedNullableBoolBatchTable()
	result := runQuery(t, tbl, `
		transform branch = if(flag, 1, 0), ok = flag and true
		| group batch
		| reduce grouped total = sum(branch), first_ok = first(ok)
		| remove grouped
		| select total, first_ok
	`)
	if result.NumRows != 1 {
		t.Fatalf("expected 1 group row, got %d", result.NumRows)
	}
	totalIdx := result.ColIndex("total")
	firstOKIdx := result.ColIndex("first_ok")

	if got := result.GetAt(0, totalIdx).Int; got != 1 {
		t.Errorf("total: expected sum(if(flag,1,0))=1, got %d", got)
	}
	assertTruth(t, result.GetAt(0, firstOKIdx), truthNull)
}

func TestThreeValuedLogicFilterSemantics(t *testing.T) {
	t.Run("filter_true_keeps_all", func(t *testing.T) {
		result := runQuery(t, usersTable(), "filter { true } | count")
		if result.GetAt(0, 0).Int != int64(usersTable().NumRows) {
			t.Errorf("expected %d rows, got %d", usersTable().NumRows, result.GetAt(0, 0).Int)
		}
	})

	t.Run("filter_false_drops_all", func(t *testing.T) {
		result := runQuery(t, usersTable(), "filter { false } | count")
		if result.GetAt(0, 0).Int != 0 {
			t.Errorf("expected 0 rows, got %d", result.GetAt(0, 0).Int)
		}
	})

	t.Run("comparison_null_drops", func(t *testing.T) {
		result := runQuery(t, usersTable(), "filter { age > null } | count")
		if result.GetAt(0, 0).Int != 0 {
			t.Errorf("expected 0 rows, got %d", result.GetAt(0, 0).Int)
		}
	})

	t.Run("null_and_true_drops_all", func(t *testing.T) {
		result := runQuery(t, usersTable(), "filter { null and true } | count")
		if result.GetAt(0, 0).Int != 0 {
			t.Errorf("expected 0 rows, got %d", result.GetAt(0, 0).Int)
		}
	})

	t.Run("null_or_false_drops_all", func(t *testing.T) {
		result := runQuery(t, usersTable(), "filter { null or false } | count")
		if result.GetAt(0, 0).Int != 0 {
			t.Errorf("expected 0 rows, got %d", result.GetAt(0, 0).Int)
		}
	})

	t.Run("null_or_true_keeps_all", func(t *testing.T) {
		result := runQuery(t, usersTable(), "filter { null or true } | count")
		if result.GetAt(0, 0).Int != int64(usersTable().NumRows) {
			t.Errorf("expected %d rows, got %d", usersTable().NumRows, result.GetAt(0, 0).Int)
		}
	})

	t.Run("null_literal_drops_all", func(t *testing.T) {
		result := runQuery(t, usersTable(), "filter { null } | count")
		if result.GetAt(0, 0).Int != 0 {
			t.Errorf("expected 0 rows, got %d", result.GetAt(0, 0).Int)
		}
	})

	t.Run("not_predicate_null_drops", func(t *testing.T) {
		tbl := table.NewTable([]string{"name", "message"})
		tbl.AddRow([]table.Value{table.StrVal("Alice"), table.StrVal("ERROR: timeout")})
		tbl.AddRow([]table.Value{table.StrVal("Bob"), table.Null()})
		result := runQuery(t, tbl, `filter { not str_contains(message, "ERROR") } | count`)
		if result.GetAt(0, 0).Int != 0 {
			t.Errorf("expected 0 rows, got %d", result.GetAt(0, 0).Int)
		}
	})
}

func TestThreeValuedLogicFilterNullableBoolColumn(t *testing.T) {
	tbl := nullableNamedBoolTable()

	t.Run("filter_flag", func(t *testing.T) {
		result := runQuery(t, tbl, "filter { flag } | select name | sort name")
		assertSortedNames(t, result, []string{"yes"})
	})

	t.Run("filter_not_flag", func(t *testing.T) {
		result := runQuery(t, tbl, "filter { not flag } | select name | sort name")
		assertSortedNames(t, result, []string{"no"})
	})

	t.Run("filter_flag_and_true", func(t *testing.T) {
		result := runQuery(t, tbl, "filter { flag and true } | select name | sort name")
		assertSortedNames(t, result, []string{"yes"})
	})

	t.Run("filter_flag_or_false", func(t *testing.T) {
		result := runQuery(t, tbl, "filter { flag or false } | select name | sort name")
		assertSortedNames(t, result, []string{"yes"})
	})

	t.Run("filter_not_not_flag", func(t *testing.T) {
		result := runQuery(t, tbl, "filter { not not flag } | select name | sort name")
		assertSortedNames(t, result, []string{"yes"})
	})
}

func TestThreeValuedLogicNonBooleanErrors(t *testing.T) {
	cases := []struct {
		name  string
		query string
		want  string
	}{
		{"and_int_left", "transform x = 1 and true", "'and' requires boolean operands"},
		{"and_int_right", "transform x = true and 1", "'and' requires boolean operands"},
		{"or_int", "transform x = 1 or false", "'or' requires boolean operands"},
		{"not_int", "transform x = not 1", "'not' requires boolean operand"},
		{"not_string", "transform x = not name", "'not' requires boolean operand"},
		{"filter_non_bool", "filter { age + 1 }", "did not return boolean"},
	}

	tbl := usersTable()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := runQueryExpectErr(t, tbl, tc.query)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("expected error containing %q, got: %v", tc.want, err)
			}
		})
	}
}

func TestThreeValuedLogicEvalDirectAndOrNot(t *testing.T) {
	// Direct Eval coverage mirroring truth tables (no parser involved).
	andCases := []struct {
		name        string
		left, right ast.Expr
		want        truth
	}{
		{"true_and_true", boolLit(true), boolLit(true), truthTrue},
		{"true_and_null", boolLit(true), nullLit(), truthNull},
		{"null_and_false", nullLit(), boolLit(false), truthFalse},
		{"null_and_null", nullLit(), nullLit(), truthNull},
	}
	for _, tc := range andCases {
		t.Run("and_"+tc.name, func(t *testing.T) {
			got := evalLiteralExpr(t, binExpr("and", tc.left, tc.right))
			assertTruth(t, got, tc.want)
		})
	}

	orCases := []struct {
		name        string
		left, right ast.Expr
		want        truth
	}{
		{"false_or_null", boolLit(false), nullLit(), truthNull},
		{"null_or_false", nullLit(), boolLit(false), truthNull},
		{"null_or_true", nullLit(), boolLit(true), truthTrue},
	}
	for _, tc := range orCases {
		t.Run("or_"+tc.name, func(t *testing.T) {
			got := evalLiteralExpr(t, binExpr("or", tc.left, tc.right))
			assertTruth(t, got, tc.want)
		})
	}

	t.Run("not_null", func(t *testing.T) {
		got := evalLiteralExpr(t, unaryNot(nullLit()))
		assertTruth(t, got, truthNull)
	})
}
