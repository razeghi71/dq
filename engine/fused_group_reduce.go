package engine

import (
	"fmt"
	"strings"

	"github.com/razeghi71/dq/table"
)

type aggregateFinalExprKind int

const (
	aggregateFinalLiteral aggregateFinalExprKind = iota
	aggregateFinalBinary
	aggregateFinalUnary
	aggregateFinalSlot
	aggregateFinalIsNull
	aggregateFinalCoerce
)

type aggregateFinalExpr struct {
	kind     aggregateFinalExprKind
	literal  table.Value
	op       string
	left     *aggregateFinalExpr
	right    *aggregateFinalExpr
	operand  *aggregateFinalExpr
	slot     int
	coerceTo *table.TypeDescriptor
}

func compileAggregateFinalExpr(expr typedExpr, slots *[]plannedAggregateSlot) (aggregateFinalExpr, error) {
	switch e := expr.bound.(type) {
	case *boundLiteral:
		return aggregateFinalExpr{kind: aggregateFinalLiteral, literal: evalLiteral(e.raw)}, nil
	case *boundBinary:
		left, err := compileAggregateFinalExprPtr(expr.left, slots)
		if err != nil {
			return aggregateFinalExpr{}, err
		}
		right, err := compileAggregateFinalExprPtr(expr.right, slots)
		if err != nil {
			return aggregateFinalExpr{}, err
		}
		return aggregateFinalExpr{kind: aggregateFinalBinary, op: e.raw.Op, left: left, right: right}, nil
	case *boundUnary:
		operand, err := compileAggregateFinalExprPtr(expr.operand, slots)
		if err != nil {
			return aggregateFinalExpr{}, err
		}
		return aggregateFinalExpr{kind: aggregateFinalUnary, op: e.raw.Op, operand: operand}, nil
	case *boundCall:
		spec, ok := builtinCatalog[e.raw.Name]
		if !ok {
			return aggregateFinalExpr{}, fmt.Errorf("unknown aggregate function %q", e.raw.Name)
		}
		if spec.Category != builtinAggregate || spec.Aggregate == nil {
			return aggregateFinalExpr{}, nonAggregateReduceFunctionError(e.raw.Name)
		}
		if len(expr.args) != spec.Aggregate.Arity {
			return aggregateFinalExpr{}, aggregateArityError(e.raw.Name, spec.Aggregate.Arity, len(expr.args))
		}
		args := make([]boundColumn, len(expr.args))
		for i, exprArg := range expr.args {
			col, ok := exprArg.bound.(*boundColumn)
			if !ok {
				return aggregateFinalExpr{}, fmt.Errorf("%s() argument must be a column reference", e.raw.Name)
			}
			args[i] = *col
		}
		slot := len(*slots)
		*slots = append(*slots, plannedAggregateSlot{name: e.raw.Name, aggregate: spec.Aggregate, args: args})
		return aggregateFinalExpr{kind: aggregateFinalSlot, slot: slot}, nil
	case *boundIsNull:
		operand, err := compileAggregateFinalExprPtr(expr.operand, slots)
		if err != nil {
			return aggregateFinalExpr{}, err
		}
		op := "is null"
		if e.raw.Negated {
			op = "is not null"
		}
		return aggregateFinalExpr{kind: aggregateFinalIsNull, op: op, operand: operand}, nil
	case *boundCoerce:
		operand, err := compileAggregateFinalExprPtr(expr.operand, slots)
		if err != nil {
			return aggregateFinalExpr{}, err
		}
		return aggregateFinalExpr{kind: aggregateFinalCoerce, operand: operand, coerceTo: expr.coerceTo}, nil
	case *boundColumn:
		return aggregateFinalExpr{}, fmt.Errorf("column cannot be used directly in reduce")
	case *boundStruct:
		return aggregateFinalExpr{}, fmt.Errorf("struct constructor is not supported in reduce")
	case *boundList:
		return aggregateFinalExpr{}, fmt.Errorf("list constructor is not supported in reduce")
	default:
		return aggregateFinalExpr{}, fmt.Errorf("unknown aggregate expression binding %T", expr.bound)
	}
}

func compileAggregateFinalExprPtr(expr *typedExpr, slots *[]plannedAggregateSlot) (*aggregateFinalExpr, error) {
	if expr == nil {
		return nil, fmt.Errorf("missing aggregate expression child")
	}
	out, err := compileAggregateFinalExpr(*expr, slots)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func runtimeAggregateSlots(slots []plannedAggregateSlot, input *table.Table) []aggregateRuntimeSlot {
	out := make([]aggregateRuntimeSlot, len(slots))
	for i, slot := range slots {
		out[i].name = slot.name
		out[i].aggregate = slot.aggregate
		out[i].args = make([]rowValueEvaluator, len(slot.args))
		for argIdx := range slot.args {
			arg := slot.args[argIdx]
			if eval, ok := compileBoundColumnValue(&arg, input); ok {
				out[i].args[argIdx] = eval
				continue
			}
			out[i].args[argIdx] = func(row int) (table.Value, error) {
				return resolveBoundColumn(arg, input, row)
			}
		}
	}
	return out
}

func evalAggregateFinalExpr(expr aggregateFinalExpr, states []aggregateAccumulator) (table.Value, error) {
	switch expr.kind {
	case aggregateFinalLiteral:
		return expr.literal, nil
	case aggregateFinalBinary:
		left, err := evalAggregateFinalExprPtr(expr.left, states)
		if err != nil {
			return table.Null(), err
		}
		right, err := evalAggregateFinalExprPtr(expr.right, states)
		if err != nil {
			return table.Null(), err
		}
		switch expr.op {
		case "+", "-", "*", "/":
			if left.IsNull() || right.IsNull() {
				return table.Null(), nil
			}
			return evalArith(expr.op, left, right)
		case "==", "!=", "<", ">", "<=", ">=":
			return evalComparison(expr.op, left, right)
		case "and":
			return table.EvalTruthAnd(left, right)
		case "or":
			return table.EvalTruthOr(left, right)
		default:
			return table.Null(), fmt.Errorf("unknown operator %q", expr.op)
		}
	case aggregateFinalUnary:
		operand, err := evalAggregateFinalExprPtr(expr.operand, states)
		if err != nil {
			return table.Null(), err
		}
		switch expr.op {
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
			return table.Null(), fmt.Errorf("unknown unary operator %q", expr.op)
		}
	case aggregateFinalSlot:
		if expr.slot < 0 || expr.slot >= len(states) {
			return table.Null(), fmt.Errorf("aggregate slot %d out of range", expr.slot)
		}
		return states[expr.slot].Finalize()
	case aggregateFinalIsNull:
		operand, err := evalAggregateFinalExprPtr(expr.operand, states)
		if err != nil {
			return table.Null(), err
		}
		isNull := operand.IsNull()
		if expr.op == "is not null" {
			isNull = !isNull
		}
		return table.BoolVal(isNull), nil
	case aggregateFinalCoerce:
		v, err := evalAggregateFinalExprPtr(expr.operand, states)
		if err != nil {
			return table.Null(), err
		}
		return coercePlannedExpressionValue(v, expr.coerceTo)
	default:
		return table.Null(), fmt.Errorf("unknown aggregate final expression kind %d", expr.kind)
	}
}

func evalAggregateFinalExprPtr(expr *aggregateFinalExpr, states []aggregateAccumulator) (table.Value, error) {
	if expr == nil {
		return table.Null(), fmt.Errorf("missing aggregate expression child")
	}
	return evalAggregateFinalExpr(*expr, states)
}

type plannedGroupReduceEntry struct {
	key     []table.Value
	records []table.Value
	states  []aggregateAccumulator
}

func execPlannedGroupReduce(p plannedGroupReduce, input *table.Table) (*table.Table, error) {
	runtimeSlots := runtimeAggregateSlots(p.slots, input)
	groups := make([]plannedGroupReduceEntry, 0)
	keyMap := make(map[string]int)

	for row := 0; row < input.NumRows; row++ {
		keyVals := make([]table.Value, len(p.keys))
		keyParts := make([]string, len(p.keys))
		for i, key := range p.keys {
			v, err := resolveBoundColumn(key.column, input, row)
			if err != nil {
				return nil, fmt.Errorf("group %q: %w", strings.Join(key.column.rawPath, "."), err)
			}
			keyVals[i] = v
			keyParts[i] = table.CanonicalKey(v)
		}
		key := canonicalTupleKey(keyParts)
		groupIdx, exists := keyMap[key]
		if !exists {
			states, err := newAggregateAccumulators(runtimeSlots)
			if err != nil {
				return nil, fmt.Errorf("reduce: %w", err)
			}
			groupIdx = len(groups)
			keyMap[key] = groupIdx
			groups = append(groups, plannedGroupReduceEntry{key: keyVals, states: states})
		}

		if p.materializeNested {
			groups[groupIdx].records = append(groups[groupIdx].records, recordValueForInputRow(input, row))
		}
		for _, state := range groups[groupIdx].states {
			if err := state.Update(row); err != nil {
				return nil, fmt.Errorf("reduce: %w", err)
			}
		}
	}

	result := tableFromOutputEnv(p.OutputEnv())
	cols, _ := outputEnvColumns(p.OutputEnv())
	keyByName := make(map[string]int, len(p.keys))
	for i, key := range p.keys {
		keyByName[key.name] = i
	}
	for _, group := range groups {
		vals := make([]table.Value, len(cols))
		for i := range vals {
			vals[i] = table.Null()
		}
		for col, name := range cols {
			if keyIdx, ok := keyByName[name]; ok {
				vals[col] = group.key[keyIdx]
				continue
			}
			if p.materializeNested && name == p.nestedName {
				vals[col] = table.ListVal(group.records)
			}
		}
		for _, assignment := range p.assignments {
			v, err := evalAggregateFinalExpr(assignment.expr, group.states)
			if err != nil {
				return nil, fmt.Errorf("reduce %q: %w", assignment.name, err)
			}
			vals[assignment.target] = v
		}
		if err := result.AddRowTyped(vals); err != nil {
			return nil, fmt.Errorf("reduce: %w", err)
		}
	}
	return result, nil
}

func recordValueForInputRow(input *table.Table, row int) table.Value {
	fields := make([]table.RecordField, len(input.Columns))
	for col, name := range input.Columns {
		fields[col] = table.RecordField{Name: name, Value: input.Col(col).Get(row)}
	}
	return table.RecordVal(fields)
}
