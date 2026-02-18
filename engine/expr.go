package engine

import (
	"fmt"
	"math"
	"strings"

	"github.com/razeghi71/dq/ast"
	"github.com/razeghi71/dq/table"
)

// EvalContext provides column lookup for expression evaluation.
type EvalContext struct {
	Table *table.Table
	Row   *table.Row
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
	idx := ctx.Table.ColIndex(e.Name)
	if idx < 0 {
		return table.Null(), fmt.Errorf("column %q not found", e.Name)
	}
	return ctx.Row.Values[idx], nil
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
		return evalLogicalAnd(left, right)
	case "or":
		return evalLogicalOr(left, right)
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

	// String comparison
	if left.Type == table.TypeString && right.Type == table.TypeString {
		cmp := strings.Compare(left.Str, right.Str)
		return table.BoolVal(cmpResult(op, cmp)), nil
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

	// Numeric comparison
	lf, lok := left.AsFloat()
	rf, rok := right.AsFloat()
	if lok && rok {
		diff := lf - rf
		var cmp int
		if diff < 0 {
			cmp = -1
		} else if diff > 0 {
			cmp = 1
		}
		return table.BoolVal(cmpResult(op, cmp)), nil
	}

	return table.Null(), fmt.Errorf("cannot compare %v with %v", left.AsString(), right.AsString())
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

func evalLogicalAnd(left, right table.Value) (table.Value, error) {
	lb, lok := left.AsBool()
	rb, rok := right.AsBool()
	if !lok || !rok {
		return table.Null(), fmt.Errorf("'and' requires boolean operands")
	}
	return table.BoolVal(lb && rb), nil
}

func evalLogicalOr(left, right table.Value) (table.Value, error) {
	lb, lok := left.AsBool()
	rb, rok := right.AsBool()
	if !lok || !rok {
		return table.Null(), fmt.Errorf("'or' requires boolean operands")
	}
	return table.BoolVal(lb || rb), nil
}

func evalUnary(e *ast.UnaryExpr, ctx *EvalContext) (table.Value, error) {
	operand, err := Eval(e.Operand, ctx)
	if err != nil {
		return table.Null(), err
	}

	switch e.Op {
	case "not":
		b, ok := operand.AsBool()
		if !ok {
			return table.Null(), fmt.Errorf("'not' requires boolean operand")
		}
		return table.BoolVal(!b), nil
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
