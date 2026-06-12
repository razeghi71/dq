package engine

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/razeghi71/dq/ast"
	"github.com/razeghi71/dq/table"
)

// EvalContext provides column lookup for expression evaluation.
type EvalContext struct {
	Table  *table.Table
	RowIdx int
}

// Eval evaluates an expression against a row context.
func Eval(expr ast.Expr, ctx *EvalContext) (table.Value, error) {
	switch e := expr.(type) {
	case *ast.LiteralExpr:
		return evalLiteral(e), nil
	case *ast.ColumnExpr:
		return evalColumn(e, ctx)
	case *ast.BinaryExpr:
		return evalBinary(e, ctx)
	case *ast.UnaryExpr:
		return evalUnary(e, ctx)
	case *ast.FuncCallExpr:
		return evalFunc(e, ctx)
	case *ast.IsNullExpr:
		return evalIsNull(e, ctx)
	default:
		return table.Null(), fmt.Errorf("unknown expression type %T", expr)
	}
}

func evalLiteral(e *ast.LiteralExpr) table.Value {
	switch e.Kind {
	case "int":
		return table.IntVal(e.Int)
	case "float":
		return table.FloatVal(e.Float)
	case "string":
		return table.StrVal(e.Str)
	case "bool":
		return table.BoolVal(e.Bool)
	case "null":
		return table.Null()
	default:
		return table.Null()
	}
}

func evalColumn(e *ast.ColumnExpr, ctx *EvalContext) (table.Value, error) {
	return resolveColumnPath(e.Path, ctx.Table, ctx.RowIdx)
}

func evalBinary(e *ast.BinaryExpr, ctx *EvalContext) (table.Value, error) {
	left, err := Eval(e.Left, ctx)
	if err != nil {
		return table.Null(), err
	}
	right, err := Eval(e.Right, ctx)
	if err != nil {
		return table.Null(), err
	}

	// Null propagation for arithmetic
	switch e.Op {
	case "+", "-", "*", "/":
		if left.IsNull() || right.IsNull() {
			return table.Null(), nil
		}
		return evalArith(e.Op, left, right)
	case "==", "!=", "<", ">", "<=", ">=":
		return evalComparison(e.Op, left, right)
	case "and":
		return table.EvalTruthAnd(left, right)
	case "or":
		return table.EvalTruthOr(left, right)
	default:
		return table.Null(), fmt.Errorf("unknown operator %q", e.Op)
	}
}

func evalArith(op string, left, right table.Value) (table.Value, error) {
	// String concatenation with +
	if op == "+" && left.Type == table.TypeString && right.Type == table.TypeString {
		return table.StrVal(left.Str + right.Str), nil
	}

	lf, lok := left.AsFloat()
	rf, rok := right.AsFloat()
	if !lok || !rok {
		return table.Null(), fmt.Errorf("cannot perform %s on %v and %v", op, left.AsString(), right.AsString())
	}

	var result float64
	switch op {
	case "+":
		result = lf + rf
	case "-":
		result = lf - rf
	case "*":
		result = lf * rf
	case "/":
		if rf == 0 {
			return table.Null(), nil // division by zero returns null
		}
		result = lf / rf
	}

	// If both inputs were ints and result is whole, return int
	if left.Type == table.TypeInt && right.Type == table.TypeInt && result == math.Trunc(result) && op != "/" {
		return table.IntVal(int64(result)), nil
	}
	// Integer division that results in whole number
	if left.Type == table.TypeInt && right.Type == table.TypeInt && op == "/" {
		if left.Int%right.Int == 0 {
			return table.IntVal(left.Int / right.Int), nil
		}
	}
	return table.FloatVal(result), nil
}

func evalComparison(op string, left, right table.Value) (table.Value, error) {
	// Null comparisons: null == null is true, null == anything is false
	if left.IsNull() && right.IsNull() {
		switch op {
		case "==":
			return table.BoolVal(true), nil
		case "!=":
			return table.BoolVal(false), nil
		default:
			return table.Null(), nil
		}
	}
	if left.IsNull() || right.IsNull() {
		switch op {
		case "==":
			return table.BoolVal(false), nil
		case "!=":
			return table.BoolVal(true), nil
		default:
			return table.Null(), nil
		}
	}

	// Lists and records have no meaningful scalar comparison: error clearly
	// (e.g. string vs record), as before.
	if !isComparableScalar(left) || !isComparableScalar(right) {
		return table.Null(), fmt.Errorf("cannot compare %v with %v", left.AsString(), right.AsString())
	}

	// Bool comparison
	if left.Type == table.TypeBool && right.Type == table.TypeBool {
		switch op {
		case "==":
			return table.BoolVal(left.Bool == right.Bool), nil
		case "!=":
			return table.BoolVal(left.Bool != right.Bool), nil
		default:
			return table.Null(), fmt.Errorf("cannot use %s on booleans", op)
		}
	}

	// Numeric comparison: applies when both sides are numeric, including
	// strings that parse as numbers. This lets widened CSV string columns
	// (e.g. "1", "2.5") compare against numeric literals, consistent with how
	// join/group/distinct match int 1 with string "1". Unlike a pure
	// string-key normalization, parsing to a number keeps ordering (<, >)
	// correct (e.g. "10" > "9").
	if lf, lok := cmpFloat(left); lok {
		if rf, rok := cmpFloat(right); rok {
			diff := lf - rf
			var cmp int
			if diff < 0 {
				cmp = -1
			} else if diff > 0 {
				cmp = 1
			}
			return table.BoolVal(cmpResult(op, cmp)), nil
		}
	}

	// Fallback for the remaining scalar combinations (e.g. val == "something",
	// or numeric vs non-numeric string): compare by value representation, the
	// same normalization used by join/group/distinct.
	cmp := strings.Compare(left.AsString(), right.AsString())
	return table.BoolVal(cmpResult(op, cmp)), nil
}

// isComparableScalar reports whether a value can take part in a scalar
// comparison. Lists and records cannot and produce a clear error instead.
func isComparableScalar(v table.Value) bool {
	switch v.Type {
	case table.TypeInt, table.TypeFloat, table.TypeString, table.TypeBool:
		return true
	default:
		return false
	}
}

// cmpFloat coerces a value to float64 for comparison. Unlike Value.AsFloat
// (used for arithmetic), it also parses numeric strings so that widened CSV
// columns can be compared against numeric literals. It is intentionally local
// to comparison to avoid changing arithmetic/string-concat semantics.
func cmpFloat(v table.Value) (float64, bool) {
	switch v.Type {
	case table.TypeInt:
		return float64(v.Int), true
	case table.TypeFloat:
		return v.Float, true
	case table.TypeString:
		f, err := strconv.ParseFloat(strings.TrimSpace(v.Str), 64)
		if err != nil {
			return 0, false
		}
		return f, true
	default:
		return 0, false
	}
}

func cmpResult(op string, cmp int) bool {
	switch op {
	case "==":
		return cmp == 0
	case "!=":
		return cmp != 0
	case "<":
		return cmp < 0
	case ">":
		return cmp > 0
	case "<=":
		return cmp <= 0
	case ">=":
		return cmp >= 0
	}
	return false
}

func evalUnary(e *ast.UnaryExpr, ctx *EvalContext) (table.Value, error) {
	operand, err := Eval(e.Operand, ctx)
	if err != nil {
		return table.Null(), err
	}

	switch e.Op {
	case "not":
		return table.EvalTruthNot(operand)
	case "-":
		if operand.IsNull() {
			return table.Null(), nil
		}
		switch operand.Type {
		case table.TypeInt:
			return table.IntVal(-operand.Int), nil
		case table.TypeFloat:
			return table.FloatVal(-operand.Float), nil
		default:
			return table.Null(), fmt.Errorf("cannot negate %v", operand.AsString())
		}
	default:
		return table.Null(), fmt.Errorf("unknown unary operator %q", e.Op)
	}
}

func evalIsNull(e *ast.IsNullExpr, ctx *EvalContext) (table.Value, error) {
	operand, err := Eval(e.Operand, ctx)
	if err != nil {
		return table.Null(), err
	}
	isNull := operand.IsNull()
	if e.Negated {
		isNull = !isNull
	}
	return table.BoolVal(isNull), nil
}
