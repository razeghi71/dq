package engine

import (
	"fmt"
	"strings"

	"github.com/razeghi71/dq/table"
)

type typedCallEvaluator func(args []typedExpr, ctx *EvalContext) (table.Value, error)

func evalTypedExpression(expr typedExpr, ctx *EvalContext) (table.Value, error) {
	var (
		v   table.Value
		err error
	)
	switch e := expr.bound.(type) {
	case *boundLiteral:
		v = evalLiteral(e.raw)
	case *boundColumn:
		v, err = evalBoundColumn(e, ctx)
	case *boundBinary:
		v, err = evalTypedBinary(e.raw.Op, expr.left, expr.right, ctx)
	case *boundUnary:
		v, err = evalTypedUnary(e.raw.Op, expr.operand, ctx)
	case *boundCall:
		v, err = evalTypedCall(expr, ctx)
	case *boundStruct:
		v, err = evalTypedStruct(expr.fields, ctx)
	case *boundList:
		v, err = evalTypedList(expr.elements, ctx)
	case *boundIsNull:
		v, err = evalTypedIsNull(e.raw.Negated, expr.operand, ctx)
	case *boundCoerce:
		v, err = evalTypedChild(expr.operand, ctx)
		if err != nil {
			return table.Null(), err
		}
		return coercePlannedExpressionValue(v, expr.coerceTo)
	default:
		err = fmt.Errorf("unknown bound expression type %T", expr.bound)
	}
	if err != nil {
		return table.Null(), err
	}
	return v, nil
}

func evalBoundColumn(e *boundColumn, ctx *EvalContext) (table.Value, error) {
	if ctx != nil && ctx.RowValues != nil {
		if e == nil || e.topIndex < 0 || e.topIndex >= len(ctx.RowValues) {
			name := "<unknown>"
			if e != nil && len(e.rawPath) > 0 {
				name = e.rawPath[0]
			}
			return table.Null(), fmt.Errorf("column %q not found", name)
		}
		return resolveNestedValuePath(ctx.RowValues[e.topIndex], e.nestedPath)
	}
	if e == nil || ctx == nil || ctx.Table == nil || e.topIndex < 0 || e.topIndex >= len(ctx.Table.Columns) {
		name := "<unknown>"
		if e != nil && len(e.rawPath) > 0 {
			name = e.rawPath[0]
		}
		return table.Null(), fmt.Errorf("column %q not found", name)
	}
	col := ctx.Table.Col(e.topIndex)
	if col == nil {
		name := "<unknown>"
		if len(e.rawPath) > 0 {
			name = e.rawPath[0]
		}
		return table.Null(), fmt.Errorf("column %q not found", name)
	}
	return resolveNestedValuePath(col.Get(ctx.RowIdx), e.nestedPath)
}

func evalTypedBinary(op string, leftExpr, rightExpr *typedExpr, ctx *EvalContext) (table.Value, error) {
	left, err := evalTypedChild(leftExpr, ctx)
	if err != nil {
		return table.Null(), err
	}
	right, err := evalTypedChild(rightExpr, ctx)
	if err != nil {
		return table.Null(), err
	}
	switch op {
	case "+", "-", "*", "/":
		if left.IsNull() || right.IsNull() {
			return table.Null(), nil
		}
		return evalArith(op, left, right)
	case "==", "!=", "<", ">", "<=", ">=":
		return evalComparison(op, left, right)
	case "and":
		return table.EvalTruthAnd(left, right)
	case "or":
		return table.EvalTruthOr(left, right)
	default:
		return table.Null(), fmt.Errorf("unknown operator %q", op)
	}
}

func evalTypedUnary(op string, operandExpr *typedExpr, ctx *EvalContext) (table.Value, error) {
	operand, err := evalTypedChild(operandExpr, ctx)
	if err != nil {
		return table.Null(), err
	}
	switch op {
	case "not":
		return table.EvalTruthNot(operand)
	case "-":
		if operand.IsNull() {
			return table.Null(), nil
		}
		switch operand.Type {
		case table.TypeInt:
			v, err := negInt64(operand.Int)
			if err != nil {
				return table.Null(), err
			}
			return table.IntVal(v), nil
		case table.TypeFloat:
			return table.FloatVal(-operand.Float), nil
		default:
			return table.Null(), fmt.Errorf("cannot negate %v", operand.AsString())
		}
	default:
		return table.Null(), fmt.Errorf("unknown unary operator %q", op)
	}
}

func evalTypedCall(expr typedExpr, ctx *EvalContext) (table.Value, error) {
	if expr.callEval == nil {
		if call, ok := expr.bound.(*boundCall); ok {
			return table.Null(), fmt.Errorf("missing runtime evaluator for function %q", call.raw.Name)
		}
		return table.Null(), fmt.Errorf("missing runtime evaluator for function call")
	}
	return expr.callEval(expr.args, ctx)
}

func evalTypedStruct(fields []typedStructField, ctx *EvalContext) (table.Value, error) {
	out := make([]table.RecordField, len(fields))
	for i, field := range fields {
		v, err := evalTypedExpression(field.expr, ctx)
		if err != nil {
			return table.Null(), err
		}
		out[i] = table.RecordField{Name: field.name, Value: v}
	}
	return table.RecordVal(out), nil
}

func evalTypedList(elements []typedExpr, ctx *EvalContext) (table.Value, error) {
	out := make([]table.Value, len(elements))
	for i, elem := range elements {
		v, err := evalTypedExpression(elem, ctx)
		if err != nil {
			return table.Null(), err
		}
		out[i] = v
	}
	return table.ListVal(out), nil
}

func evalTypedIsNull(negated bool, operandExpr *typedExpr, ctx *EvalContext) (table.Value, error) {
	operand, err := evalTypedChild(operandExpr, ctx)
	if err != nil {
		return table.Null(), err
	}
	isNull := operand.IsNull()
	if negated {
		isNull = !isNull
	}
	return table.BoolVal(isNull), nil
}

func evalTypedChild(expr *typedExpr, ctx *EvalContext) (table.Value, error) {
	if expr == nil {
		return table.Null(), fmt.Errorf("missing typed expression child")
	}
	return evalTypedExpression(*expr, ctx)
}

func coercePlannedExpressionValue(v table.Value, schema *table.TypeDescriptor) (table.Value, error) {
	if schema == nil {
		return v, nil
	}
	return table.CoerceValueToFinalSchemaMode(v, schema, table.CoerceCoerciveMode)
}

func evalTypedAggregateExpression(expr typedExpr, nested *table.Table) (table.Value, error) {
	switch e := expr.bound.(type) {
	case *boundLiteral:
		return evalLiteral(e.raw), nil
	case *boundBinary:
		return evalTypedAggregateBinary(e.raw.Op, expr.left, expr.right, nested)
	case *boundUnary:
		return evalTypedAggregateUnary(e.raw.Op, expr.operand, nested)
	case *boundCall:
		return evalAggregateCall(e.raw, nested)
	case *boundIsNull:
		return evalTypedAggregateIsNull(e.raw.Negated, expr.operand, nested)
	case *boundCoerce:
		v, err := evalTypedAggregateChild(expr.operand, nested)
		if err != nil {
			return table.Null(), err
		}
		return coercePlannedExpressionValue(v, expr.coerceTo)
	case *boundColumn:
		return table.Null(), fmt.Errorf("column %q cannot be used directly in reduce; use an aggregate such as first(%s)", strings.Join(e.rawPath, "."), strings.Join(e.rawPath, "."))
	case *boundStruct:
		return table.Null(), fmt.Errorf("struct constructor is not supported in reduce")
	case *boundList:
		return table.Null(), fmt.Errorf("list constructor is not supported in reduce")
	default:
		return table.Null(), fmt.Errorf("unknown bound expression type %T", expr.bound)
	}
}

func evalTypedAggregateBinary(op string, leftExpr, rightExpr *typedExpr, nested *table.Table) (table.Value, error) {
	left, err := evalTypedAggregateChild(leftExpr, nested)
	if err != nil {
		return table.Null(), err
	}
	right, err := evalTypedAggregateChild(rightExpr, nested)
	if err != nil {
		return table.Null(), err
	}
	switch op {
	case "+", "-", "*", "/":
		if left.IsNull() || right.IsNull() {
			return table.Null(), nil
		}
		return evalArith(op, left, right)
	case "==", "!=", "<", ">", "<=", ">=":
		return evalComparison(op, left, right)
	case "and":
		return table.EvalTruthAnd(left, right)
	case "or":
		return table.EvalTruthOr(left, right)
	default:
		return table.Null(), fmt.Errorf("unknown operator %q", op)
	}
}

func evalTypedAggregateUnary(op string, operandExpr *typedExpr, nested *table.Table) (table.Value, error) {
	operand, err := evalTypedAggregateChild(operandExpr, nested)
	if err != nil {
		return table.Null(), err
	}
	switch op {
	case "not":
		return table.EvalTruthNot(operand)
	case "-":
		if operand.IsNull() {
			return table.Null(), nil
		}
		switch operand.Type {
		case table.TypeInt:
			v, err := negInt64(operand.Int)
			if err != nil {
				return table.Null(), err
			}
			return table.IntVal(v), nil
		case table.TypeFloat:
			return table.FloatVal(-operand.Float), nil
		default:
			return table.Null(), fmt.Errorf("cannot negate %v", operand.AsString())
		}
	default:
		return table.Null(), fmt.Errorf("unknown unary operator %q", op)
	}
}

func evalTypedAggregateIsNull(negated bool, operandExpr *typedExpr, nested *table.Table) (table.Value, error) {
	operand, err := evalTypedAggregateChild(operandExpr, nested)
	if err != nil {
		return table.Null(), err
	}
	isNull := operand.IsNull()
	if negated {
		isNull = !isNull
	}
	return table.BoolVal(isNull), nil
}

func evalTypedAggregateChild(expr *typedExpr, nested *table.Table) (table.Value, error) {
	if expr == nil {
		return table.Null(), fmt.Errorf("missing typed aggregate expression child")
	}
	return evalTypedAggregateExpression(*expr, nested)
}
