package engine

import (
	"fmt"
	"math"

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
	case *ast.StructExpr:
		return evalStruct(e, ctx)
	case *ast.ListExpr:
		return evalList(e, ctx)
	case *ast.IsNullExpr:
		return evalIsNull(e, ctx)
	default:
		return table.Null(), fmt.Errorf("unknown expression type %T", expr)
	}
}

func evalStruct(e *ast.StructExpr, ctx *EvalContext) (table.Value, error) {
	fields := make([]table.RecordField, len(e.Fields))
	for i, f := range e.Fields {
		v, err := Eval(f.Expr, ctx)
		if err != nil {
			return table.Null(), err
		}
		fields[i] = table.RecordField{Name: f.Name, Value: v}
	}
	return table.RecordVal(fields), nil
}

func evalList(e *ast.ListExpr, ctx *EvalContext) (table.Value, error) {
	elements := make([]table.Value, len(e.Elements))
	for i, elem := range e.Elements {
		v, err := Eval(elem, ctx)
		if err != nil {
			return table.Null(), err
		}
		elements[i] = v
	}
	return table.ListVal(elements), nil
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

	switch op {
	case "==":
		eq, err := table.EqualStrict(left, right)
		if err != nil {
			return table.Null(), fmt.Errorf("cannot compare %s with %s", table.TypeName(left.Type), table.TypeName(right.Type))
		}
		return table.BoolVal(eq), nil
	case "!=":
		eq, err := table.EqualStrict(left, right)
		if err != nil {
			return table.Null(), fmt.Errorf("cannot compare %s with %s", table.TypeName(left.Type), table.TypeName(right.Type))
		}
		return table.BoolVal(!eq), nil
	}

	cmp, err := table.CompareStrict(left, right)
	if err != nil {
		return table.Null(), fmt.Errorf("cannot compare %s with %s", table.TypeName(left.Type), table.TypeName(right.Type))
	}
	return table.BoolVal(cmpResult(op, cmp)), nil
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
