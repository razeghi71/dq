package engine

import (
	"fmt"
	"strings"

	"github.com/razeghi71/dq/ast"
	"github.com/razeghi71/dq/table"
)

type aggregateAccumulator interface {
	Update(row int) error
	Finalize() (table.Value, error)
}

type aggregateRuntimeSlot struct {
	name      string
	aggregate *aggregateSpec
	args      []rowValueEvaluator
}

func evalAggregateWithSpec(e *ast.FuncCallExpr, nested *table.Table, spec *aggregateSpec) (table.Value, error) {
	if spec == nil || spec.NewAccumulator == nil {
		return table.Null(), nonAggregateReduceFunctionError(e.Name)
	}
	if len(e.Args) != spec.Arity {
		return table.Null(), aggregateArityError(e.Name, spec.Arity, len(e.Args))
	}
	if nested == nil {
		return table.Null(), fmt.Errorf("%s(): nil nested table", e.Name)
	}
	args, err := aggregateCallArgEvaluators(e, nested)
	if err != nil {
		return table.Null(), err
	}
	acc, err := newAggregateAccumulator(aggregateRuntimeSlot{name: e.Name, aggregate: spec, args: args})
	if err != nil {
		return table.Null(), err
	}
	for row := 0; row < nested.NumRows; row++ {
		if err := acc.Update(row); err != nil {
			return table.Null(), err
		}
	}
	return acc.Finalize()
}

func aggregateCallArgEvaluators(e *ast.FuncCallExpr, nested *table.Table) ([]rowValueEvaluator, error) {
	args := make([]rowValueEvaluator, len(e.Args))
	for i, arg := range e.Args {
		colExpr, ok := arg.(*ast.ColumnExpr)
		if !ok {
			return nil, fmt.Errorf("%s() argument must be a column reference", e.Name)
		}
		path := append([]string(nil), colExpr.Path...)
		args[i] = func(row int) (table.Value, error) {
			v, err := resolveColumnPath(path, nested, row)
			if err != nil {
				return table.Null(), fmt.Errorf("%s(%s): %w", e.Name, strings.Join(path, "."), err)
			}
			return v, nil
		}
	}
	return args, nil
}

func newAggregateAccumulators(slots []aggregateRuntimeSlot) ([]aggregateAccumulator, error) {
	states := make([]aggregateAccumulator, len(slots))
	for i, slot := range slots {
		state, err := newAggregateAccumulator(slot)
		if err != nil {
			return nil, err
		}
		states[i] = state
	}
	return states, nil
}

func newAggregateAccumulator(slot aggregateRuntimeSlot) (aggregateAccumulator, error) {
	if slot.aggregate == nil || slot.aggregate.NewAccumulator == nil {
		return nil, fmt.Errorf("unknown aggregate function %q", slot.name)
	}
	if len(slot.args) != slot.aggregate.Arity {
		return nil, aggregateArityError(slot.name, slot.aggregate.Arity, len(slot.args))
	}
	return slot.aggregate.NewAccumulator(slot.args)
}

func aggregateArityError(name string, want, got int) error {
	if want == 0 {
		return fmt.Errorf("%s() takes no arguments, got %d", name, got)
	}
	if want == 1 {
		return fmt.Errorf("%s() takes 1 argument, got %d", name, got)
	}
	return fmt.Errorf("%s() takes %d arguments, got %d", name, want, got)
}

func requireAggregateArgCount(name string, args []rowValueEvaluator, want int) error {
	if len(args) != want {
		return aggregateArityError(name, want, len(args))
	}
	return nil
}

func newCountAccumulator(args []rowValueEvaluator) (aggregateAccumulator, error) {
	if err := requireAggregateArgCount("count", args, 0); err != nil {
		return nil, err
	}
	return &countAccumulator{}, nil
}

func newSumAccumulator(args []rowValueEvaluator) (aggregateAccumulator, error) {
	if err := requireAggregateArgCount("sum", args, 1); err != nil {
		return nil, err
	}
	return &sumAccumulator{arg: args[0], hasInt: true}, nil
}

func newAvgAccumulator(args []rowValueEvaluator) (aggregateAccumulator, error) {
	if err := requireAggregateArgCount("avg", args, 1); err != nil {
		return nil, err
	}
	return &avgAccumulator{arg: args[0]}, nil
}

func newMinAccumulator(args []rowValueEvaluator) (aggregateAccumulator, error) {
	if err := requireAggregateArgCount("min", args, 1); err != nil {
		return nil, err
	}
	return &extremumAccumulator{name: "min", arg: args[0], better: func(cmp int) bool { return cmp < 0 }}, nil
}

func newMaxAccumulator(args []rowValueEvaluator) (aggregateAccumulator, error) {
	if err := requireAggregateArgCount("max", args, 1); err != nil {
		return nil, err
	}
	return &extremumAccumulator{name: "max", arg: args[0], better: func(cmp int) bool { return cmp > 0 }}, nil
}

func newFirstAccumulator(args []rowValueEvaluator) (aggregateAccumulator, error) {
	if err := requireAggregateArgCount("first", args, 1); err != nil {
		return nil, err
	}
	return &firstAccumulator{arg: args[0]}, nil
}

func newLastAccumulator(args []rowValueEvaluator) (aggregateAccumulator, error) {
	if err := requireAggregateArgCount("last", args, 1); err != nil {
		return nil, err
	}
	return &lastAccumulator{arg: args[0]}, nil
}

type countAccumulator struct {
	n int64
}

func (a *countAccumulator) Update(int) error {
	a.n++
	return nil
}

func (a *countAccumulator) Finalize() (table.Value, error) {
	return table.IntVal(a.n), nil
}

type sumAccumulator struct {
	arg         rowValueEvaluator
	sum         float64
	hasInt      bool
	intSum      int64
	intOverflow error
	any         bool
}

func (a *sumAccumulator) Update(row int) error {
	v, err := a.arg(row)
	if err != nil {
		return err
	}
	if v.IsNull() {
		return nil
	}
	f, ok := v.AsFloat()
	if !ok {
		return fmt.Errorf("sum: non-numeric value %v", v.AsString())
	}
	a.sum += f
	a.any = true
	if v.Type == table.TypeInt {
		if a.hasInt && a.intOverflow == nil {
			next, err := evalIntArith("+", a.intSum, v.Int)
			if err != nil {
				a.intOverflow = fmt.Errorf("sum: %w", err)
			} else {
				a.intSum = next
			}
		}
	} else {
		a.hasInt = false
	}
	return nil
}

func (a *sumAccumulator) Finalize() (table.Value, error) {
	if !a.any {
		return table.Null(), nil
	}
	if a.hasInt {
		if a.intOverflow != nil {
			return table.Null(), a.intOverflow
		}
		return table.IntVal(a.intSum), nil
	}
	return table.FloatVal(a.sum), nil
}

type avgAccumulator struct {
	arg   rowValueEvaluator
	sum   float64
	count int
}

func (a *avgAccumulator) Update(row int) error {
	v, err := a.arg(row)
	if err != nil {
		return err
	}
	if v.IsNull() {
		return nil
	}
	f, ok := v.AsFloat()
	if !ok {
		return fmt.Errorf("avg: non-numeric value %v", v.AsString())
	}
	a.sum += f
	a.count++
	return nil
}

func (a *avgAccumulator) Finalize() (table.Value, error) {
	if a.count == 0 {
		return table.Null(), nil
	}
	return table.FloatVal(a.sum / float64(a.count)), nil
}

type extremumAccumulator struct {
	name   string
	arg    rowValueEvaluator
	better func(int) bool
	best   table.Value
	any    bool
}

func (a *extremumAccumulator) Update(row int) error {
	v, err := a.arg(row)
	if err != nil {
		return err
	}
	if v.IsNull() {
		return nil
	}
	if !a.any {
		a.best = v
		a.any = true
		return nil
	}
	cmp, unordered, err := expressionValuesCompare(v, a.best)
	if err != nil {
		return fmt.Errorf("%s: %s", a.name, err)
	}
	if !unordered && a.better(cmp) {
		a.best = v
	}
	return nil
}

func (a *extremumAccumulator) Finalize() (table.Value, error) {
	if !a.any {
		return table.Null(), nil
	}
	return a.best, nil
}

type firstAccumulator struct {
	arg   rowValueEvaluator
	value table.Value
	seen  bool
}

func (a *firstAccumulator) Update(row int) error {
	if a.seen {
		return nil
	}
	v, err := a.arg(row)
	if err != nil {
		return err
	}
	a.value = v
	a.seen = true
	return nil
}

func (a *firstAccumulator) Finalize() (table.Value, error) {
	if !a.seen {
		return table.Null(), nil
	}
	return a.value, nil
}

type lastAccumulator struct {
	arg   rowValueEvaluator
	value table.Value
	seen  bool
}

func (a *lastAccumulator) Update(row int) error {
	v, err := a.arg(row)
	if err != nil {
		return err
	}
	a.value = v
	a.seen = true
	return nil
}

func (a *lastAccumulator) Finalize() (table.Value, error) {
	if !a.seen {
		return table.Null(), nil
	}
	return a.value, nil
}
