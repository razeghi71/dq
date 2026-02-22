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

// resolveColumnPath walks a dot-path (e.g. ["address", "city"]) to extract a value from a row.
func resolveColumnPath(path []string, t *table.Table, row *table.Row) (table.Value, error) {
	idx := t.ColIndex(path[0])
	if idx < 0 {
		return table.Null(), fmt.Errorf("column %q not found", path[0])
	}
	val := row.Values[idx]
	for _, seg := range path[1:] {
		if val.Type != table.TypeRecord {
			return table.Null(), fmt.Errorf("cannot access field %q: value is not a record", seg)
		}
		found := false
		for _, f := range val.Fields {
			if f.Name == seg {
				val = f.Value
				found = true
				break
			}
		}
		if !found {
			return table.Null(), nil
		}
	}
	return val, nil
}

// pathToColumnName flattens a dot-path to an output column name using underscores.
func pathToColumnName(path []string) string {
	if len(path) == 1 {
		return path[0]
	}
	return strings.Join(path, "_")
}

// uniqueColumnName returns base if it's not in existing, otherwise base_2, base_3, etc.
func uniqueColumnName(base string, existing []string) string {
	taken := make(map[string]bool, len(existing))
	for _, e := range existing {
		taken[e] = true
	}
	if !taken[base] {
		return base
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s_%d", base, i)
		if !taken[candidate] {
			return candidate
		}
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

func execSort(cols [][]string, asc bool, t *table.Table) (*table.Table, error) {
	indices := make([]int, len(cols))
	for i, path := range cols {
		idx := t.ColIndex(path[0])
		if idx < 0 {
			return nil, fmt.Errorf("sort: column %q not found", path[0])
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
	// Build output column names with dedup
	var resultCols []string
	for _, path := range o.Columns {
		base := pathToColumnName(path)
		name := uniqueColumnName(base, resultCols)
		resultCols = append(resultCols, name)
	}

	result := table.NewTable(resultCols)
	for _, row := range t.Rows {
		vals := make([]table.Value, len(o.Columns))
		for i, path := range o.Columns {
			v, err := resolveColumnPath(path, t, &row)
			if err != nil {
				return nil, fmt.Errorf("select: %w", err)
			}
			vals[i] = v
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
	// All columns go into the nested records (including group keys)
	nestedCols := make([]string, len(t.Columns))
	nestedIndices := make([]int, len(t.Columns))
	for i, col := range t.Columns {
		nestedCols[i] = col
		nestedIndices[i] = i
	}

	// Build output key column names with dedup
	var keyColNames []string
	for _, path := range o.Columns {
		base := pathToColumnName(path)
		name := uniqueColumnName(base, keyColNames)
		keyColNames = append(keyColNames, name)
	}

	// Build groups preserving order
	type groupEntry struct {
		key     []table.Value
		records []table.Value // each a TypeRecord
	}
	var groups []groupEntry
	keyMap := make(map[string]int) // key string -> index in groups

	for _, row := range t.Rows {
		// Build key by resolving each path
		keyParts := make([]string, len(o.Columns))
		keyVals := make([]table.Value, len(o.Columns))
		for i, path := range o.Columns {
			v, err := resolveColumnPath(path, t, &row)
			if err != nil {
				return nil, fmt.Errorf("group: %w", err)
			}
			keyVals[i] = v
			keyParts[i] = v.AsString()
		}
		keyStr := strings.Join(keyParts, "\x00")

		gi, exists := keyMap[keyStr]
		if !exists {
			gi = len(groups)
			groups = append(groups, groupEntry{key: keyVals})
			keyMap[keyStr] = gi
		}

		// Build a TypeRecord for this row's nested columns
		fields := make([]table.RecordField, len(nestedIndices))
		for i, idx := range nestedIndices {
			fields[i] = table.RecordField{Name: nestedCols[i], Value: row.Values[idx]}
		}
		groups[gi].records = append(groups[gi].records, table.RecordVal(fields))
	}

	// Build result table: key columns + list column
	resultCols := make([]string, len(keyColNames))
	copy(resultCols, keyColNames)
	resultCols = append(resultCols, o.NestedName)

	result := table.NewTable(resultCols)
	for _, g := range groups {
		vals := make([]table.Value, len(g.key)+1)
		copy(vals, g.key)
		vals[len(g.key)] = table.ListVal(g.records)
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
		if nested.Type != table.TypeList {
			return nil, fmt.Errorf("reduce: column %q is not a list (did you forget to group first?)", o.NestedName)
		}

		nestedTable, err := table.ListToTable(nested)
		if err != nil {
			return nil, fmt.Errorf("reduce: %w", err)
		}

		vals := make([]table.Value, len(newCols))
		copy(vals, row.Values)
		for i := len(row.Values); i < len(newCols); i++ {
			vals[i] = table.Null()
		}

		for i, a := range o.Assignments {
			v, err := EvalAggregate(a.Expr, nestedTable)
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
		for i, path := range o.Columns {
			idx := t.ColIndex(path[0])
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
	for _, path := range o.Columns {
		c := path[0]
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
