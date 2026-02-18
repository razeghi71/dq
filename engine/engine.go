package engine

import (
	"fmt"
	"sort"
	"strings"

	"github.com/razeghi71/dq/ast"
	"github.com/razeghi71/dq/table"
)

// Execute runs a full query pipeline on the given input table.
func Execute(query *ast.Query, input *table.Table) (*table.Table, error) {
	current := input
	for _, op := range query.Ops {
		var err error
		current, err = execOp(op, current)
		if err != nil {
			return nil, err
		}
	}
	return current, nil
}

func execOp(op ast.Op, t *table.Table) (*table.Table, error) {
	switch o := op.(type) {
	case *ast.HeadOp:
		return execHead(o, t), nil
	case *ast.TailOp:
		return execTail(o, t), nil
	case *ast.SortAscOp:
		return execSort(o.Columns, true, t)
	case *ast.SortDescOp:
		return execSort(o.Columns, false, t)
	case *ast.SelectOp:
		return execSelect(o, t)
	case *ast.FilterOp:
		return execFilter(o, t)
	case *ast.GroupOp:
		return execGroup(o, t)
	case *ast.TransformOp:
		return execTransform(o, t)
	case *ast.ReduceOp:
		return execReduce(o, t)
	case *ast.CountOp:
		return execCount(t), nil
	case *ast.DistinctOp:
		return execDistinct(o, t), nil
	case *ast.RenameOp:
		return execRename(o, t)
	case *ast.RemoveOp:
		return execRemove(o, t)
	default:
		return nil, fmt.Errorf("unknown operation type %T", op)
	}
}

func execHead(o *ast.HeadOp, t *table.Table) *table.Table {
	n := o.N
	if n > len(t.Rows) {
		n = len(t.Rows)
	}
	result := table.NewTable(t.Columns)
	result.Rows = t.Rows[:n]
	return result
}

func execTail(o *ast.TailOp, t *table.Table) *table.Table {
	n := o.N
	if n > len(t.Rows) {
		n = len(t.Rows)
	}
	result := table.NewTable(t.Columns)
	result.Rows = t.Rows[len(t.Rows)-n:]
	return result
}

func execSort(cols []string, asc bool, t *table.Table) (*table.Table, error) {
	indices := make([]int, len(cols))
	for i, c := range cols {
		idx := t.ColIndex(c)
		if idx < 0 {
			return nil, fmt.Errorf("sort: column %q not found", c)
		}
		indices[i] = idx
	}

	result := t.Clone()
	sort.SliceStable(result.Rows, func(i, j int) bool {
		for _, idx := range indices {
			a := result.Rows[i].Values[idx]
			b := result.Rows[j].Values[idx]
			cmp := compareValues(a, b)
			if cmp != 0 {
				if asc {
					return cmp < 0
				}
				return cmp > 0
			}
		}
		return false
	})
	return result, nil
}

func compareValues(a, b table.Value) int {
	// Nulls sort last
	if a.IsNull() && b.IsNull() {
		return 0
	}
	if a.IsNull() {
		return 1
	}
	if b.IsNull() {
		return -1
	}

	// Numeric comparison
	af, aok := a.AsFloat()
	bf, bok := b.AsFloat()
	if aok && bok {
		if af < bf {
			return -1
		}
		if af > bf {
			return 1
		}
		return 0
	}

	// String comparison
	return strings.Compare(a.AsString(), b.AsString())
}

func execSelect(o *ast.SelectOp, t *table.Table) (*table.Table, error) {
	indices := make([]int, len(o.Columns))
	for i, c := range o.Columns {
		idx := t.ColIndex(c)
		if idx < 0 {
			return nil, fmt.Errorf("select: column %q not found", c)
		}
		indices[i] = idx
	}

	result := table.NewTable(o.Columns)
	for _, row := range t.Rows {
		vals := make([]table.Value, len(indices))
		for i, idx := range indices {
			vals[i] = row.Values[idx]
		}
		result.AddRow(vals)
	}
	return result, nil
}

func execFilter(o *ast.FilterOp, t *table.Table) (*table.Table, error) {
	result := table.NewTable(t.Columns)
	for _, row := range t.Rows {
		ctx := &EvalContext{Table: t, Row: &row}
		val, err := Eval(o.Expr, ctx)
		if err != nil {
			return nil, fmt.Errorf("filter: %w", err)
		}
		b, ok := val.AsBool()
		if !ok {
			return nil, fmt.Errorf("filter: expression did not return boolean, got %v", val.AsString())
		}
		if b {
			result.AddRow(row.Values)
		}
	}
	return result, nil
}

func execGroup(o *ast.GroupOp, t *table.Table) (*table.Table, error) {
	groupIndices := make([]int, len(o.Columns))
	for i, c := range o.Columns {
		idx := t.ColIndex(c)
		if idx < 0 {
			return nil, fmt.Errorf("group: column %q not found", c)
		}
		groupIndices[i] = idx
	}

	// Determine which columns go into the nested table
	nestedCols := make([]string, 0)
	nestedIndices := make([]int, 0)
	groupColSet := make(map[int]bool)
	for _, idx := range groupIndices {
		groupColSet[idx] = true
	}
	for i, col := range t.Columns {
		if !groupColSet[i] {
			nestedCols = append(nestedCols, col)
			nestedIndices = append(nestedIndices, i)
		}
	}

	// Build groups preserving order
	type groupEntry struct {
		key    []table.Value
		nested *table.Table
	}
	var groups []groupEntry
	keyMap := make(map[string]int) // key string -> index in groups

	for _, row := range t.Rows {
		// Build key
		keyParts := make([]string, len(groupIndices))
		keyVals := make([]table.Value, len(groupIndices))
		for i, idx := range groupIndices {
			keyVals[i] = row.Values[idx]
			keyParts[i] = row.Values[idx].AsString()
		}
		keyStr := strings.Join(keyParts, "\x00")

		gi, exists := keyMap[keyStr]
		if !exists {
			nested := table.NewTable(nestedCols)
			gi = len(groups)
			groups = append(groups, groupEntry{key: keyVals, nested: nested})
			keyMap[keyStr] = gi
		}

		// Add nested row
		nestedVals := make([]table.Value, len(nestedIndices))
		for i, idx := range nestedIndices {
			nestedVals[i] = row.Values[idx]
		}
		groups[gi].nested.AddRow(nestedVals)
	}

	// Build result table: group columns + nested column
	resultCols := make([]string, len(o.Columns))
	copy(resultCols, o.Columns)
	resultCols = append(resultCols, o.NestedName)

	result := table.NewTable(resultCols)
	for _, g := range groups {
		vals := make([]table.Value, len(g.key)+1)
		copy(vals, g.key)
		vals[len(g.key)] = table.NestedVal(g.nested)
		result.AddRow(vals)
	}
	return result, nil
}

func execTransform(o *ast.TransformOp, t *table.Table) (*table.Table, error) {
	// Figure out which columns are new vs existing
	newCols := make([]string, len(t.Columns))
	copy(newCols, t.Columns)
	assignTargets := make([]int, len(o.Assignments)) // index in newCols

	for i, a := range o.Assignments {
		idx := -1
		for j, c := range newCols {
			if c == a.Column {
				idx = j
				break
			}
		}
		if idx < 0 {
			idx = len(newCols)
			newCols = append(newCols, a.Column)
		}
		assignTargets[i] = idx
	}

	result := table.NewTable(newCols)
	for _, row := range t.Rows {
		vals := make([]table.Value, len(newCols))
		copy(vals, row.Values)
		// Fill new columns with null
		for i := len(row.Values); i < len(newCols); i++ {
			vals[i] = table.Null()
		}

		ctx := &EvalContext{Table: t, Row: &row}
		for i, a := range o.Assignments {
			v, err := Eval(a.Expr, ctx)
			if err != nil {
				return nil, fmt.Errorf("transform %q: %w", a.Column, err)
			}
			vals[assignTargets[i]] = v
		}
		result.AddRow(vals)
	}
	return result, nil
}

func execReduce(o *ast.ReduceOp, t *table.Table) (*table.Table, error) {
	nestedIdx := t.ColIndex(o.NestedName)
	if nestedIdx < 0 {
		return nil, fmt.Errorf("reduce: nested column %q not found (did you forget to group first?)", o.NestedName)
	}

	// Result columns: existing columns + new aggregated columns
	newCols := make([]string, len(t.Columns))
	copy(newCols, t.Columns)
	assignTargets := make([]int, len(o.Assignments))

	for i, a := range o.Assignments {
		idx := -1
		for j, c := range newCols {
			if c == a.Column {
				idx = j
				break
			}
		}
		if idx < 0 {
			idx = len(newCols)
			newCols = append(newCols, a.Column)
		}
		assignTargets[i] = idx
	}

	result := table.NewTable(newCols)
	for _, row := range t.Rows {
		nested := row.Values[nestedIdx]
		if nested.Type != table.TypeNested || nested.Nested == nil {
			return nil, fmt.Errorf("reduce: column %q is not a nested table", o.NestedName)
		}

		vals := make([]table.Value, len(newCols))
		copy(vals, row.Values)
		for i := len(row.Values); i < len(newCols); i++ {
			vals[i] = table.Null()
		}

		for i, a := range o.Assignments {
			v, err := EvalAggregate(a.Expr, nested.Nested)
			if err != nil {
				return nil, fmt.Errorf("reduce %q: %w", a.Column, err)
			}
			vals[assignTargets[i]] = v
		}
		result.AddRow(vals)
	}
	return result, nil
}

func execCount(t *table.Table) *table.Table {
	result := table.NewTable([]string{"count"})
	result.AddRow([]table.Value{table.IntVal(int64(len(t.Rows)))})
	return result
}

func execDistinct(o *ast.DistinctOp, t *table.Table) *table.Table {
	var indices []int
	if len(o.Columns) > 0 {
		indices = make([]int, len(o.Columns))
		for i, c := range o.Columns {
			idx := t.ColIndex(c)
			if idx < 0 {
				// Column not found - return empty
				return table.NewTable(t.Columns)
			}
			indices[i] = idx
		}
	}

	seen := make(map[string]bool)
	result := table.NewTable(t.Columns)
	for _, row := range t.Rows {
		var key string
		if len(indices) > 0 {
			parts := make([]string, len(indices))
			for i, idx := range indices {
				parts[i] = row.Values[idx].AsString()
			}
			key = strings.Join(parts, "\x00")
		} else {
			parts := make([]string, len(row.Values))
			for i, v := range row.Values {
				parts[i] = v.AsString()
			}
			key = strings.Join(parts, "\x00")
		}

		if !seen[key] {
			seen[key] = true
			result.AddRow(row.Values)
		}
	}
	return result
}

func execRename(o *ast.RenameOp, t *table.Table) (*table.Table, error) {
	newCols := make([]string, len(t.Columns))
	copy(newCols, t.Columns)

	for _, pair := range o.Pairs {
		found := false
		for i, c := range newCols {
			if c == pair.Old {
				newCols[i] = pair.New
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("rename: column %q not found", pair.Old)
		}
	}

	result := table.NewTable(newCols)
	result.Rows = t.Rows
	return result, nil
}

func execRemove(o *ast.RemoveOp, t *table.Table) (*table.Table, error) {
	removeSet := make(map[string]bool)
	for _, c := range o.Columns {
		if t.ColIndex(c) < 0 {
			return nil, fmt.Errorf("remove: column %q not found", c)
		}
		removeSet[c] = true
	}

	var keepCols []string
	var keepIndices []int
	for i, c := range t.Columns {
		if !removeSet[c] {
			keepCols = append(keepCols, c)
			keepIndices = append(keepIndices, i)
		}
	}

	result := table.NewTable(keepCols)
	for _, row := range t.Rows {
		vals := make([]table.Value, len(keepIndices))
		for i, idx := range keepIndices {
			vals[i] = row.Values[idx]
		}
		result.AddRow(vals)
	}
	return result, nil
}
