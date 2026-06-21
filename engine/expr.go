package engine

import (
	"fmt"
	"math"
	"math/big"
	"math/bits"

	"github.com/razeghi71/dq/ast"
	"github.com/razeghi71/dq/table"
)

// EvalContext provides column lookup for typed expression evaluation.
type EvalContext struct {
	Table  *table.Table
	RowIdx int
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

func evalArith(op string, left, right table.Value) (table.Value, error) {
	// String concatenation with +
	if op == "+" && left.Type == table.TypeString && right.Type == table.TypeString {
		return table.StrVal(left.Str + right.Str), nil
	}

	if op != "/" && left.Type == table.TypeInt && right.Type == table.TypeInt {
		result, err := evalIntArith(op, left.Int, right.Int)
		if err != nil {
			return table.Null(), err
		}
		return table.IntVal(result), nil
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
	default:
		return table.Null(), fmt.Errorf("unknown arithmetic operator %q", op)
	}

	return table.FloatVal(result), nil
}

func evalIntArith(op string, left, right int64) (int64, error) {
	switch op {
	case "+":
		result, overflow := checkedAddInt64(left, right)
		if overflow {
			return 0, integerOverflowError(op, left, right)
		}
		return result, nil
	case "-":
		result, overflow := checkedSubInt64(left, right)
		if overflow {
			return 0, integerOverflowError(op, left, right)
		}
		return result, nil
	case "*":
		result, overflow := checkedMulInt64(left, right)
		if overflow {
			return 0, integerOverflowError(op, left, right)
		}
		return result, nil
	default:
		return 0, fmt.Errorf("unknown arithmetic operator %q", op)
	}
}

func checkedAddInt64(left, right int64) (int64, bool) {
	resultBits, _ := bits.Add64(uint64(left), uint64(right), 0)
	result := int64(resultBits)
	return result, ((left ^ result) & (right ^ result)) < 0
}

func checkedSubInt64(left, right int64) (int64, bool) {
	resultBits, _ := bits.Sub64(uint64(left), uint64(right), 0)
	result := int64(resultBits)
	return result, ((left ^ right) & (left ^ result)) < 0
}

func checkedMulInt64(left, right int64) (int64, bool) {
	leftAbs := absInt64AsUint(left)
	rightAbs := absInt64AsUint(right)
	hi, lo := bits.Mul64(leftAbs, rightAbs)
	if hi != 0 {
		return 0, true
	}

	negative := (left < 0) != (right < 0)
	if negative {
		const minInt64Magnitude = uint64(1) << 63
		if lo > minInt64Magnitude {
			return 0, true
		}
		if lo == minInt64Magnitude {
			return math.MinInt64, false
		}
		return -int64(lo), false
	}
	if lo > uint64(math.MaxInt64) {
		return 0, true
	}
	return int64(lo), false
}

func absInt64AsUint(v int64) uint64 {
	if v >= 0 {
		return uint64(v)
	}
	return uint64(^v) + 1
}

func negInt64(v int64) (int64, error) {
	if v == math.MinInt64 {
		return 0, fmt.Errorf("integer overflow in unary -: %d", v)
	}
	return -v, nil
}

func integerOverflowError(op string, left, right int64) error {
	return fmt.Errorf("integer overflow in %s: %d and %d", op, left, right)
}

type comparisonTruth int

const (
	comparisonUnknown comparisonTruth = iota
	comparisonFalse
	comparisonTrue
)

func knownComparisonTruth(v bool) comparisonTruth {
	if v {
		return comparisonTrue
	}
	return comparisonFalse
}

func (t comparisonTruth) Value() table.Value {
	switch t {
	case comparisonTrue:
		return table.BoolVal(true)
	case comparisonFalse:
		return table.BoolVal(false)
	default:
		return table.Null()
	}
}

func evalComparison(op string, left, right table.Value) (table.Value, error) {
	if left.IsNull() || right.IsNull() {
		return comparisonUnknown.Value(), nil
	}

	switch op {
	case "==":
		eq, err := expressionValuesEqual(left, right, true)
		if err != nil {
			return table.Null(), fmt.Errorf("cannot compare %s with %s", table.TypeName(left.Type), table.TypeName(right.Type))
		}
		return knownComparisonTruth(eq).Value(), nil
	case "!=":
		eq, err := expressionValuesEqual(left, right, true)
		if err != nil {
			return table.Null(), fmt.Errorf("cannot compare %s with %s", table.TypeName(left.Type), table.TypeName(right.Type))
		}
		return knownComparisonTruth(!eq).Value(), nil
	}

	cmp, unordered, err := expressionValuesCompare(left, right)
	if err != nil {
		return table.Null(), fmt.Errorf("cannot compare %s with %s", table.TypeName(left.Type), table.TypeName(right.Type))
	}
	if unordered {
		return comparisonFalse.Value(), nil
	}
	return knownComparisonTruth(cmpResult(op, cmp)).Value(), nil
}

func expressionValuesCompare(a, b table.Value) (int, bool, error) {
	if isNumericValue(a) && isNumericValue(b) {
		return compareNumericValuesExact(a, b)
	}
	if a.Type != b.Type {
		return 0, false, fmt.Errorf("type mismatch: %s vs %s", table.TypeName(a.Type), table.TypeName(b.Type))
	}
	switch a.Type {
	case table.TypeString:
		switch {
		case a.Str < b.Str:
			return -1, false, nil
		case a.Str > b.Str:
			return 1, false, nil
		default:
			return 0, false, nil
		}
	default:
		return 0, false, fmt.Errorf("%s values are not orderable", table.TypeName(a.Type))
	}
}

func expressionValuesEqual(a, b table.Value, topLevelMismatchIsError bool) (bool, error) {
	if a.IsNull() || b.IsNull() {
		return a.IsNull() && b.IsNull(), nil
	}
	if isNumericValue(a) && isNumericValue(b) {
		cmp, unordered, err := compareNumericValuesExact(a, b)
		if err != nil || unordered {
			return false, err
		}
		return cmp == 0, nil
	}
	if a.Type != b.Type {
		if topLevelMismatchIsError {
			return false, fmt.Errorf("type mismatch: %s vs %s", table.TypeName(a.Type), table.TypeName(b.Type))
		}
		return false, nil
	}
	switch a.Type {
	case table.TypeString:
		return a.Str == b.Str, nil
	case table.TypeBool:
		return a.Bool == b.Bool, nil
	case table.TypeList:
		if len(a.List) != len(b.List) {
			return false, nil
		}
		for i := range a.List {
			eq, err := expressionValuesEqual(a.List[i], b.List[i], false)
			if err != nil || !eq {
				return eq, err
			}
		}
		return true, nil
	case table.TypeRecord:
		return expressionRecordsEqual(a.Fields, b.Fields)
	default:
		if topLevelMismatchIsError {
			return false, fmt.Errorf("%s values are not comparable", table.TypeName(a.Type))
		}
		return false, nil
	}
}

func expressionRecordsEqual(a, b []table.RecordField) (bool, error) {
	left := make(map[string]table.Value, len(a))
	names := make(map[string]bool, len(a)+len(b))
	for _, field := range a {
		if _, ok := left[field.Name]; ok {
			return false, nil
		}
		left[field.Name] = field.Value
		names[field.Name] = true
	}
	right := make(map[string]table.Value, len(b))
	for _, field := range b {
		if _, ok := right[field.Name]; ok {
			return false, nil
		}
		right[field.Name] = field.Value
		names[field.Name] = true
	}

	for name := range names {
		leftValue, ok := left[name]
		if !ok {
			leftValue = table.Null()
		}
		rightValue, ok := right[name]
		if !ok {
			rightValue = table.Null()
		}
		eq, err := expressionValuesEqual(leftValue, rightValue, false)
		if err != nil || !eq {
			return eq, err
		}
	}
	return true, nil
}

func isNumericValue(v table.Value) bool {
	return v.Type == table.TypeInt || v.Type == table.TypeFloat
}

func compareNumericValuesExact(a, b table.Value) (int, bool, error) {
	switch {
	case a.Type == table.TypeInt && b.Type == table.TypeInt:
		return compareInt64(a.Int, b.Int), false, nil
	case a.Type == table.TypeFloat && b.Type == table.TypeFloat:
		return compareFloat64(a.Float, b.Float)
	case a.Type == table.TypeInt && b.Type == table.TypeFloat:
		return compareIntFloatExact(a.Int, b.Float)
	case a.Type == table.TypeFloat && b.Type == table.TypeInt:
		cmp, unordered, err := compareIntFloatExact(b.Int, a.Float)
		return -cmp, unordered, err
	default:
		return 0, false, fmt.Errorf("type mismatch: %s vs %s", table.TypeName(a.Type), table.TypeName(b.Type))
	}
}

func compareInt64(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

func compareFloat64(a, b float64) (int, bool, error) {
	if math.IsNaN(a) || math.IsNaN(b) {
		return 0, true, nil
	}
	switch {
	case a < b:
		return -1, false, nil
	case a > b:
		return 1, false, nil
	default:
		return 0, false, nil
	}
}

func compareIntFloatExact(i int64, f float64) (int, bool, error) {
	if math.IsNaN(f) {
		return 0, true, nil
	}
	if math.IsInf(f, 1) {
		return -1, false, nil
	}
	if math.IsInf(f, -1) {
		return 1, false, nil
	}
	const maxExactIntInFloat = int64(1 << 53)
	if i >= -maxExactIntInFloat && i <= maxExactIntInFloat {
		return compareFloat64(float64(i), f)
	}
	intRat := big.NewRat(i, 1)
	floatRat := new(big.Rat).SetFloat64(f)
	if floatRat == nil {
		return 0, false, fmt.Errorf("invalid float value")
	}
	return intRat.Cmp(floatRat), false, nil
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
