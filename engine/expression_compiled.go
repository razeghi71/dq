package engine

import (
	"fmt"
	"strings"

	"github.com/razeghi71/dq/table"
)

type rowValueEvaluator func(row int) (table.Value, error)
type rowPredicateEvaluator func(row int) (bool, error)

func compileTypedRowValue(expr typedExpr, t *table.Table) rowValueEvaluator {
	if eval, ok := compileFastRowValue(expr, t); ok {
		return eval
	}
	ctx := EvalContext{Table: t}
	return func(row int) (table.Value, error) {
		ctx.RowIdx = row
		return evalTypedExpression(expr, &ctx)
	}
}

func compileFilterPredicate(expr typedExpr, t *table.Table) rowPredicateEvaluator {
	if pred, ok := compileFastPredicate(expr, t); ok {
		return pred
	}
	eval := compileTypedRowValue(expr, t)
	return func(row int) (bool, error) {
		v, err := eval(row)
		if err != nil {
			return false, err
		}
		switch {
		case v.IsExplicitTrue():
			return true, nil
		case v.IsBoolOrNull():
			return false, nil
		default:
			return false, fmt.Errorf("filter expression did not return boolean")
		}
	}
}

func compileFastRowValue(expr typedExpr, t *table.Table) (rowValueEvaluator, bool) {
	switch e := expr.bound.(type) {
	case *boundLiteral:
		v := evalLiteral(e.raw)
		return func(int) (table.Value, error) { return v, nil }, true
	case *boundColumn:
		return compileBoundColumnValue(e, t)
	case *boundBinary:
		return compileFastBinaryValue(e.raw.Op, expr.left, expr.right, t)
	default:
		return nil, false
	}
}

func compileBoundColumnValue(col *boundColumn, t *table.Table) (rowValueEvaluator, bool) {
	c := boundColumnStorage(col, t)
	if c == nil {
		return nil, false
	}
	if len(col.nestedPath) == 0 {
		return func(row int) (table.Value, error) {
			return c.Get(row), nil
		}, true
	}
	path := append([]string(nil), col.nestedPath...)
	return func(row int) (table.Value, error) {
		return resolveNestedValuePath(c.Get(row), path)
	}, true
}

func compileFastBinaryValue(op string, left, right *typedExpr, t *table.Table) (rowValueEvaluator, bool) {
	if op != "+" && op != "-" && op != "*" {
		return nil, false
	}
	col, lit, reversed, ok := columnLiteralPair(left, right)
	if !ok || len(col.nestedPath) != 0 {
		return nil, false
	}
	c := boundColumnStorage(col, t)
	if c == nil || c.ColType() != table.TypeInt {
		return nil, false
	}
	litVal := evalLiteral(lit.raw)
	if litVal.Type != table.TypeInt {
		return nil, false
	}
	return func(row int) (table.Value, error) {
		v, ok := c.IntAt(row)
		if !ok {
			return table.Null(), nil
		}
		leftInt, rightInt := v, litVal.Int
		if reversed {
			leftInt, rightInt = rightInt, leftInt
		}
		out, err := evalIntArith(op, leftInt, rightInt)
		if err != nil {
			return table.Null(), err
		}
		return table.IntVal(out), nil
	}, true
}

func compileFastPredicate(expr typedExpr, t *table.Table) (rowPredicateEvaluator, bool) {
	switch e := expr.bound.(type) {
	case *boundBinary:
		if pred, ok := compileFastComparisonPredicate(e.raw.Op, expr.left, expr.right, t); ok {
			return pred, true
		}
	case *boundCall:
		if pred, ok := compileFastStringPredicate(e.raw.Name, expr.args, t); ok {
			return pred, true
		}
	}
	return nil, false
}

func compileFastComparisonPredicate(op string, left, right *typedExpr, t *table.Table) (rowPredicateEvaluator, bool) {
	switch op {
	case "==", "!=", "<", ">", "<=", ">=":
	default:
		return nil, false
	}
	col, lit, reversed, ok := columnLiteralPair(left, right)
	if !ok {
		return nil, false
	}
	litVal := evalLiteral(lit.raw)
	if litVal.IsNull() {
		return func(int) (bool, error) { return false, nil }, true
	}
	c := boundColumnStorage(col, t)
	if c == nil {
		return nil, false
	}
	if len(col.nestedPath) == 0 {
		if pred, ok := compileScalarColumnLiteralPredicate(c, op, litVal, reversed); ok {
			return pred, true
		}
	}
	path := append([]string(nil), col.nestedPath...)
	return func(row int) (bool, error) {
		v, err := resolveNestedValuePath(c.Get(row), path)
		if err != nil {
			return false, err
		}
		var out table.Value
		if reversed {
			out, err = evalComparison(op, litVal, v)
		} else {
			out, err = evalComparison(op, v, litVal)
		}
		if err != nil {
			return false, err
		}
		return out.IsExplicitTrue(), nil
	}, true
}

func compileScalarColumnLiteralPredicate(c *table.Column, op string, lit table.Value, reversed bool) (rowPredicateEvaluator, bool) {
	switch c.ColType() {
	case table.TypeInt:
		if lit.Type == table.TypeInt {
			return func(row int) (bool, error) {
				v, ok := c.IntAt(row)
				if !ok {
					return false, nil
				}
				cmp := compareInt64(v, lit.Int)
				if reversed {
					cmp = -cmp
				}
				return cmpResult(op, cmp), nil
			}, true
		}
		if lit.Type == table.TypeFloat {
			return func(row int) (bool, error) {
				v, ok := c.IntAt(row)
				if !ok {
					return false, nil
				}
				cmp, unordered, err := compareIntFloatExact(v, lit.Float)
				if err != nil {
					return false, err
				}
				if reversed {
					cmp = -cmp
				}
				return comparisonPredicateResult(op, cmp, unordered), nil
			}, true
		}
	case table.TypeFloat:
		switch lit.Type {
		case table.TypeFloat:
			return func(row int) (bool, error) {
				v, ok := c.FloatAt(row)
				if !ok {
					return false, nil
				}
				cmp, unordered, err := compareFloat64(v, lit.Float)
				if err != nil {
					return false, err
				}
				if reversed {
					cmp = -cmp
				}
				return comparisonPredicateResult(op, cmp, unordered), nil
			}, true
		case table.TypeInt:
			return func(row int) (bool, error) {
				v, ok := c.FloatAt(row)
				if !ok {
					return false, nil
				}
				cmp, unordered, err := compareIntFloatExact(lit.Int, v)
				if err != nil {
					return false, err
				}
				cmp = -cmp
				if reversed {
					cmp = -cmp
				}
				return comparisonPredicateResult(op, cmp, unordered), nil
			}, true
		}
	case table.TypeString:
		if lit.Type == table.TypeString {
			return func(row int) (bool, error) {
				v, ok := c.StringAt(row)
				if !ok {
					return false, nil
				}
				cmp := strings.Compare(v, lit.Str)
				if reversed {
					cmp = -cmp
				}
				return cmpResult(op, cmp), nil
			}, true
		}
	case table.TypeBool:
		if lit.Type == table.TypeBool && (op == "==" || op == "!=") {
			return func(row int) (bool, error) {
				v, ok := c.BoolAt(row)
				if !ok {
					return false, nil
				}
				eq := v == lit.Bool
				if op == "!=" {
					eq = !eq
				}
				return eq, nil
			}, true
		}
	}
	return nil, false
}

func comparisonPredicateResult(op string, cmp int, unordered bool) bool {
	if unordered {
		return op == "!="
	}
	return cmpResult(op, cmp)
}

func compileFastStringPredicate(name string, args []typedExpr, t *table.Table) (rowPredicateEvaluator, bool) {
	spec, ok := builtinCatalog[name]
	if !ok || spec.Category != builtinScalar || spec.TypedEval == nil {
		return nil, false
	}
	if len(args) != 2 {
		return nil, false
	}
	col, ok := args[0].bound.(*boundColumn)
	if !ok {
		return nil, false
	}
	lit, ok := args[1].bound.(*boundLiteral)
	if !ok || lit.raw.Kind != "string" {
		return nil, false
	}
	needle := lit.raw.Str
	var pred func(string, string) bool
	switch name {
	case "str_contains":
		pred = strings.Contains
	case "starts_with":
		pred = strings.HasPrefix
	case "ends_with":
		pred = strings.HasSuffix
	default:
		return nil, false
	}
	c := boundColumnStorage(col, t)
	if c == nil {
		return nil, false
	}
	if len(col.nestedPath) == 0 && c.ColType() == table.TypeString {
		return func(row int) (bool, error) {
			v, ok := c.StringAt(row)
			if !ok {
				return false, nil
			}
			return pred(v, needle), nil
		}, true
	}
	path := append([]string(nil), col.nestedPath...)
	return func(row int) (bool, error) {
		v, err := resolveNestedValuePath(c.Get(row), path)
		if err != nil {
			return false, err
		}
		if v.IsNull() {
			return false, nil
		}
		if v.Type != table.TypeString {
			return false, fmt.Errorf("%s() requires a string, got %s", name, valueTypeName(v))
		}
		return pred(v.Str, needle), nil
	}, true
}

func columnLiteralPair(left, right *typedExpr) (*boundColumn, *boundLiteral, bool, bool) {
	if left == nil || right == nil {
		return nil, nil, false, false
	}
	if col, ok := left.bound.(*boundColumn); ok {
		if lit, ok := right.bound.(*boundLiteral); ok {
			return col, lit, false, true
		}
	}
	if col, ok := right.bound.(*boundColumn); ok {
		if lit, ok := left.bound.(*boundLiteral); ok {
			return col, lit, true, true
		}
	}
	return nil, nil, false, false
}

func boundColumnStorage(col *boundColumn, t *table.Table) *table.Column {
	if col == nil || t == nil || col.topIndex < 0 || col.topIndex >= len(t.Columns) {
		return nil
	}
	return t.Col(col.topIndex)
}
