package engine

import (
	"fmt"

	"github.com/razeghi71/dq/rowstream"
	"github.com/razeghi71/dq/table"
)

func executePlannedOpsStreaming(ops []plannedOp, input rowstream.Stream) (*table.Table, error) {
	current := input
	for i := 0; i < len(ops); {
		if end := rowLocalSpanEnd(ops, i); end > i {
			span := ops[i:end]
			schema := span[len(span)-1].OutputSchema()
			next := nextPlannedOp(ops, end)
			if shouldExecuteRowSpanMaterialized(span, next) {
				materialized, err := materializeStreamingInput(current)
				if err != nil {
					return nil, err
				}
				for _, spanOp := range span {
					materialized, err = execPlannedOp(spanOp, materialized)
					if err != nil {
						return nil, err
					}
				}
				current = rowstream.FromTable(materialized)
			} else if program := compileRowProgram(span); shouldExecuteRowSpanParallel(span, next) {
				current = rowstream.ParallelMapOrdered(current, schema, program, rowstream.DefaultParallelOptions())
			} else {
				current = rowstream.Map(current, schema, program)
			}
			i = end
			continue
		}

		op := ops[i]
		if head, ok := op.(plannedHead); ok {
			current = &headStream{input: current, schema: head.OutputSchema(), n: head.n}
			i++
			continue
		}

		if result, ok, err := executeStreamingFold(op, current); ok || err != nil {
			if err != nil {
				return nil, err
			}
			current = rowstream.FromTable(result)
			i++
			continue
		}

		materialized, err := materializeStreamingInput(current)
		if err != nil {
			return nil, err
		}
		result, err := execPlannedOp(op, materialized)
		if err != nil {
			return nil, err
		}
		current = rowstream.FromTable(result)
		i++
	}
	return materializeStreamingInput(current)
}

func materializeStreamingInput(input rowstream.Stream) (*table.Table, error) {
	if materialized, ok := rowstream.AsTable(input); ok {
		return materialized, nil
	}
	return rowstream.Materialize(input)
}

func rowLocalSpanEnd(ops []plannedOp, start int) int {
	end := start
	for end < len(ops) && isRowLocalStreamingOp(ops[end]) {
		end++
	}
	return end
}

func isRowLocalStreamingOp(op plannedOp) bool {
	switch op.(type) {
	case plannedFilter, plannedSelect, plannedRename, plannedRemove, plannedTransform:
		return true
	default:
		return false
	}
}

func nextPlannedOp(ops []plannedOp, idx int) plannedOp {
	if idx < 0 || idx >= len(ops) {
		return nil
	}
	return ops[idx]
}

func shouldExecuteRowSpanParallel(span []plannedOp, next plannedOp) bool {
	if len(span) == 0 {
		return false
	}
	if _, earlyStop := next.(plannedHead); earlyStop {
		return false
	}
	for _, op := range span {
		switch p := op.(type) {
		case plannedTransform:
			if len(p.assignments) > 0 {
				return true
			}
		case plannedFilter:
			if typedExprHasCall(p.expr) {
				return true
			}
		}
	}
	return false
}

func shouldExecuteRowSpanMaterialized(span []plannedOp, next plannedOp) bool {
	if len(span) == 0 || !isMaterializedStreamingBoundary(next) {
		return false
	}
	if rowSpanCanDropRows(span) {
		return false
	}
	return true
}

func rowSpanCanDropRows(span []plannedOp) bool {
	for _, op := range span {
		if _, dropsRows := op.(plannedFilter); dropsRows {
			return true
		}
	}
	return false
}

func isMaterializedStreamingBoundary(op plannedOp) bool {
	switch op.(type) {
	case plannedTail, plannedGroup, plannedReduce, plannedSort, plannedDistinct, plannedJoin:
		return true
	default:
		return false
	}
}

func typedExprHasCall(expr typedExpr) bool {
	if _, ok := expr.bound.(*boundCall); ok {
		return true
	}
	if expr.left != nil && typedExprHasCall(*expr.left) {
		return true
	}
	if expr.right != nil && typedExprHasCall(*expr.right) {
		return true
	}
	if expr.operand != nil && typedExprHasCall(*expr.operand) {
		return true
	}
	for _, arg := range expr.args {
		if typedExprHasCall(arg) {
			return true
		}
	}
	for _, field := range expr.fields {
		if typedExprHasCall(field.expr) {
			return true
		}
	}
	for _, elem := range expr.elements {
		if typedExprHasCall(elem) {
			return true
		}
	}
	return false
}

func compileRowProgram(ops []plannedOp) rowstream.MapFunc {
	program := rowstream.MapFunc(func(row rowstream.Row) (rowstream.Row, bool, error) {
		return row, true, nil
	})
	for _, op := range ops {
		step := compileRowProgramStep(op)
		prev := program
		program = func(row rowstream.Row) (rowstream.Row, bool, error) {
			out, keep, err := prev(row)
			if err != nil || !keep {
				return out, keep, err
			}
			return step(out)
		}
	}
	return program
}

func compileRowProgramStep(op plannedOp) rowstream.MapFunc {
	switch p := op.(type) {
	case plannedFilter:
		return func(row rowstream.Row) (rowstream.Row, bool, error) {
			keep, err := evalStreamingPredicate(p.expr, row)
			if err != nil {
				return nil, false, fmt.Errorf("filter: %w", err)
			}
			if !keep {
				return nil, false, nil
			}
			return row, true, nil
		}
	case plannedSelect:
		return func(row rowstream.Row) (rowstream.Row, bool, error) {
			out, err := projectStreamingRow(row, p.projections)
			if err != nil {
				return nil, false, err
			}
			return out, true, nil
		}
	case plannedRename:
		return func(row rowstream.Row) (rowstream.Row, bool, error) {
			return row, true, nil
		}
	case plannedRemove:
		return func(row rowstream.Row) (rowstream.Row, bool, error) {
			out, err := removeStreamingRow(row, p.indices)
			if err != nil {
				return nil, false, err
			}
			return out, true, nil
		}
	case plannedTransform:
		cols, schemas := outputSchemaColumns(p.OutputSchema())
		return func(row rowstream.Row) (rowstream.Row, bool, error) {
			out, err := transformStreamingRow(row, cols, schemas, p.assignments)
			if err != nil {
				return nil, false, err
			}
			return out, true, nil
		}
	default:
		return func(row rowstream.Row) (rowstream.Row, bool, error) {
			return nil, false, fmt.Errorf("row program: unsupported operation %T", op)
		}
	}
}

func executeStreamingFold(op plannedOp, input rowstream.Stream) (*table.Table, bool, error) {
	switch p := op.(type) {
	case plannedCount:
		result, err := execStreamingCount(p, input)
		return result, true, err
	case plannedDescribe:
		result, err := execStreamingDescribe(p, input)
		return result, true, err
	default:
		return nil, false, nil
	}
}

type headStream struct {
	input  rowstream.Stream
	schema table.Schema
	n      int
	seen   int
	closed bool
}

func (s *headStream) Schema() table.Schema { return s.schema }

func (s *headStream) Next() (rowstream.Row, bool, error) {
	if s.n <= 0 || s.seen >= s.n {
		if !s.closed {
			s.closed = true
			if err := s.input.Close(); err != nil {
				return nil, false, err
			}
		}
		return nil, false, nil
	}
	row, ok, err := s.input.Next()
	if err != nil || !ok {
		return row, ok, err
	}
	s.seen++
	return row, true, nil
}

func (s *headStream) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	return s.input.Close()
}

func projectStreamingRow(row rowstream.Row, projections *projectionPlan) (rowstream.Row, error) {
	if projections.topLevelIdx != nil {
		out := make([]table.Value, len(projections.topLevelIdx))
		for i, idx := range projections.topLevelIdx {
			if idx < 0 || idx >= len(row) {
				return nil, fmt.Errorf("select: bound column index %d out of range", idx)
			}
			out[i] = row[idx]
		}
		return out, nil
	}
	out := make([]table.Value, len(projections.projections))
	for i, projection := range projections.projections {
		v, err := resolveBoundColumnRow(projection.column, row)
		if err != nil {
			return nil, fmt.Errorf("select: %w", err)
		}
		out[i] = v
	}
	return out, nil
}

func removeStreamingRow(row rowstream.Row, indices []int) (rowstream.Row, error) {
	out := make([]table.Value, len(indices))
	for i, idx := range indices {
		if idx < 0 || idx >= len(row) {
			return nil, fmt.Errorf("remove: bound column index %d out of range", idx)
		}
		out[i] = row[idx]
	}
	return out, nil
}

func transformStreamingRow(row rowstream.Row, columns []string, schemas []*table.TypeDescriptor, assignments []plannedTransformAssignment) (rowstream.Row, error) {
	out := make([]table.Value, len(columns))
	copy(out, row)
	for i := len(row); i < len(out); i++ {
		out[i] = table.Null()
	}

	computed := make([]table.Value, len(assignments))
	for i, assignment := range assignments {
		v, err := evalStreamingValue(assignment.expr, row)
		if err != nil {
			return nil, fmt.Errorf("transform %q: %w", assignment.name, err)
		}
		if assignment.target < 0 || assignment.target >= len(schemas) {
			return nil, fmt.Errorf("transform: target index %d out of range", assignment.target)
		}
		cv, err := table.CoerceValueToFinalSchemaMode(v, schemas[assignment.target], table.CoerceCoerciveMode)
		if err != nil {
			return nil, fmt.Errorf("transform: %w", err)
		}
		computed[i] = cv
	}
	for i, assignment := range assignments {
		out[assignment.target] = computed[i]
	}
	return out, nil
}

func execStreamingCount(p plannedCount, input rowstream.Stream) (*table.Table, error) {
	var count int64
	for {
		_, ok, err := input.Next()
		if err != nil {
			_ = input.Close()
			return nil, err
		}
		if !ok {
			if err := input.Close(); err != nil {
				return nil, err
			}
			result := tableFromOutputSchema(p.OutputSchema())
			if err := result.AddRowTyped([]table.Value{table.IntVal(count)}); err != nil {
				return nil, fmt.Errorf("count: %w", err)
			}
			return result, nil
		}
		count++
	}
}

func execStreamingDescribe(p plannedDescribe, input rowstream.Stream) (*table.Table, error) {
	var rowCount int64
	for {
		_, ok, err := input.Next()
		if err != nil {
			_ = input.Close()
			return nil, err
		}
		if !ok {
			if err := input.Close(); err != nil {
				return nil, err
			}
			break
		}
		rowCount++
	}

	result := tableFromOutputSchema(p.OutputSchema())
	for _, col := range input.Schema().Columns {
		if err := result.AddRowTyped([]table.Value{
			table.StrVal(col.Name),
			table.StrVal(table.TypeName(streamingSchemaKind(col.Type))),
			table.IntVal(rowCount),
			table.StrVal(table.Render(col.Type)),
		}); err != nil {
			return nil, fmt.Errorf("describe: %w", err)
		}
	}
	return result, nil
}

func evalStreamingValue(expr typedExpr, row []table.Value) (table.Value, error) {
	ctx := EvalContext{RowValues: row}
	return evalTypedExpression(expr, &ctx)
}

func evalStreamingPredicate(expr typedExpr, row []table.Value) (bool, error) {
	v, err := evalStreamingValue(expr, row)
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

func resolveBoundColumnRow(col boundColumn, row []table.Value) (table.Value, error) {
	if col.topIndex < 0 || col.topIndex >= len(row) {
		return table.Null(), fmt.Errorf("bound column index %d out of range", col.topIndex)
	}
	return resolveNestedValuePath(row[col.topIndex], col.nestedPath)
}

func streamingSchemaKind(schema *table.TypeDescriptor) table.ValueType {
	final := table.FinalizeSchema(schema)
	if final == nil {
		return table.TypeNull
	}
	return final.Kind
}
