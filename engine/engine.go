package engine

import (
	"fmt"
	"sort"
	"strings"

	"github.com/razeghi71/dq/ast"
	"github.com/razeghi71/dq/table"
)

// Execute runs a full query pipeline on the given input table.
// load is required when the pipeline contains join; pass nil otherwise.
func Execute(query *ast.Query, input *table.Table, load LoadFunc) (*table.Table, error) {
	current := input
	for _, op := range query.Ops {
		var err error
		current, err = execOp(op, current, load)
		if err != nil {
			return nil, err
		}
	}
	return current, nil
}

func execOp(op ast.Op, t *table.Table, load LoadFunc) (*table.Table, error) {
	switch o := op.(type) {
	case *ast.HeadOp:
		return execHead(o, t), nil
	case *ast.TailOp:
		return execTail(o, t), nil
	case *ast.SortOp:
		return execSort(o, t)
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
		return execDistinct(o, t)
	case *ast.RenameOp:
		return execRename(o, t)
	case *ast.RemoveOp:
		return execRemove(o, t)
	case *ast.JoinOp:
		return execJoin(o, t, load)
	default:
		return nil, fmt.Errorf("unknown operation type %T", op)
	}
}

// resolveColumnPath walks a dot-path (e.g. ["address", "city"]) to extract a value from a row.
func resolveColumnPath(path []string, t *table.Table, rowIdx int) (table.Value, error) {
	idx := t.ColIndex(path[0])
	if idx < 0 {
		return table.Null(), fmt.Errorf("column %q not found", path[0])
	}
	val := t.Col(idx).Get(rowIdx)
	for _, seg := range path[1:] {
		if val.Type != table.TypeRecord {
			if val.IsNull() {
				return table.Null(), nil
			}
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

// rowVals materializes all column values for row i as a []Value slice.
func rowVals(t *table.Table, i int) []table.Value {
	vals := make([]table.Value, len(t.Columns))
	for j := range t.Columns {
		vals[j] = t.Col(j).Get(i)
	}
	return vals
}

func execHead(o *ast.HeadOp, t *table.Table) *table.Table {
	n := o.N
	if n > t.NumRows {
		n = t.NumRows
	}
	return t.SliceRows(0, n)
}

func execTail(o *ast.TailOp, t *table.Table) *table.Table {
	n := o.N
	if n > t.NumRows {
		n = t.NumRows
	}
	return t.SliceRows(t.NumRows-n, t.NumRows)
}

func execSort(o *ast.SortOp, t *table.Table) (*table.Table, error) {
	type key struct {
		path []string
		desc bool
	}
	keys := make([]key, len(o.Keys))
	for i, k := range o.Keys {
		keys[i] = key{k.Path, k.Desc}
	}

	sortVals := make([][]table.Value, t.NumRows)
	for row := 0; row < t.NumRows; row++ {
		sortVals[row] = make([]table.Value, len(keys))
		for j, k := range keys {
			v, err := resolveColumnPath(k.path, t, row)
			if err != nil {
				return nil, fmt.Errorf("sort %q: %w", strings.Join(k.path, "."), err)
			}
			sortVals[row][j] = v
		}
	}

	perm := make([]int, t.NumRows)
	for i := range perm {
		perm[i] = i
	}
	sort.SliceStable(perm, func(a, b int) bool {
		for j, k := range keys {
			cmp := compareValues(sortVals[perm[a]][j], sortVals[perm[b]][j])
			if cmp != 0 {
				if k.desc {
					return cmp > 0
				}
				return cmp < 0
			}
		}
		return false
	})
	return t.ApplyPermutation(perm), nil
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
	var resultCols []string
	for _, path := range o.Columns {
		base := pathToColumnName(path)
		name := uniqueColumnName(base, resultCols)
		resultCols = append(resultCols, name)
	}

	result := table.NewTable(resultCols)
	for i := 0; i < t.NumRows; i++ {
		vals := make([]table.Value, len(o.Columns))
		for j, path := range o.Columns {
			v, err := resolveColumnPath(path, t, i)
			if err != nil {
				return nil, fmt.Errorf("select: %w", err)
			}
			vals[j] = v
		}
		result.AddRow(vals)
	}
	return result, nil
}

func execFilter(o *ast.FilterOp, t *table.Table) (*table.Table, error) {
	result := table.NewTable(t.Columns)
	for i := 0; i < t.NumRows; i++ {
		ctx := &EvalContext{Table: t, RowIdx: i}
		val, err := Eval(o.Expr, ctx)
		if err != nil {
			return nil, fmt.Errorf("filter: %w", err)
		}
		switch {
		case val.IsExplicitTrue():
			result.AddRow(rowVals(t, i))
		case val.IsBoolOrNull():
			// false or unknown — drop row
		default:
			return nil, fmt.Errorf("filter: expression did not return boolean, got %v", val.AsString())
		}
	}
	return result, nil
}

func execGroup(o *ast.GroupOp, t *table.Table) (*table.Table, error) {
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

	for i := 0; i < t.NumRows; i++ {
		keyParts := make([]string, len(o.Columns))
		keyVals := make([]table.Value, len(o.Columns))
		for j, path := range o.Columns {
			v, err := resolveColumnPath(path, t, i)
			if err != nil {
				return nil, fmt.Errorf("group: %w", err)
			}
			keyVals[j] = v
			keyParts[j] = v.AsString()
		}
		keyStr := strings.Join(keyParts, "\x00")

		gi, exists := keyMap[keyStr]
		if !exists {
			gi = len(groups)
			groups = append(groups, groupEntry{key: keyVals})
			keyMap[keyStr] = gi
		}

		// Build a TypeRecord for this row (all columns including key)
		fields := make([]table.RecordField, len(t.Columns))
		for j, colName := range t.Columns {
			fields[j] = table.RecordField{Name: colName, Value: t.Col(j).Get(i)}
		}
		groups[gi].records = append(groups[gi].records, table.RecordVal(fields))
	}

	// Build result table: key columns + list column
	resultCols := append(append([]string{}, keyColNames...), o.NestedName)
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
	for i := 0; i < t.NumRows; i++ {
		vals := make([]table.Value, len(newCols))
		// Copy existing column values
		for j := 0; j < len(t.Columns); j++ {
			vals[j] = t.Col(j).Get(i)
		}
		// Fill new columns with null
		for j := len(t.Columns); j < len(newCols); j++ {
			vals[j] = table.Null()
		}

		ctx := &EvalContext{Table: t, RowIdx: i}
		for j, a := range o.Assignments {
			v, err := Eval(a.Expr, ctx)
			if err != nil {
				return nil, fmt.Errorf("transform %q: %w", a.Column, err)
			}
			vals[assignTargets[j]] = v
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
	for i := 0; i < t.NumRows; i++ {
		nested := t.Col(nestedIdx).Get(i)
		if nested.Type != table.TypeList {
			return nil, fmt.Errorf("reduce: column %q is not a list (did you forget to group first?)", o.NestedName)
		}

		nestedTable, err := table.ListToTable(nested)
		if err != nil {
			return nil, fmt.Errorf("reduce: %w", err)
		}

		vals := make([]table.Value, len(newCols))
		for j := 0; j < len(t.Columns); j++ {
			vals[j] = t.Col(j).Get(i)
		}
		for j := len(t.Columns); j < len(newCols); j++ {
			vals[j] = table.Null()
		}

		for j, a := range o.Assignments {
			v, err := EvalAggregate(a.Expr, nestedTable)
			if err != nil {
				return nil, fmt.Errorf("reduce %q: %w", a.Column, err)
			}
			vals[assignTargets[j]] = v
		}
		result.AddRow(vals)
	}
	return result, nil
}

func execCount(t *table.Table) *table.Table {
	result := table.NewTable([]string{"count"})
	result.AddRow([]table.Value{table.IntVal(int64(t.NumRows))})
	return result
}

func execDistinct(o *ast.DistinctOp, t *table.Table) (*table.Table, error) {
	seen := make(map[string]bool)
	result := table.NewTable(t.Columns)
	for i := 0; i < t.NumRows; i++ {
		var key string
		if len(o.Columns) > 0 {
			parts := make([]string, len(o.Columns))
			for j, path := range o.Columns {
				v, err := resolveColumnPath(path, t, i)
				if err != nil {
					return nil, fmt.Errorf("distinct %q: %w", strings.Join(path, "."), err)
				}
				parts[j] = v.AsString()
			}
			key = strings.Join(parts, "\x00")
		} else {
			parts := make([]string, len(t.Columns))
			for j := range t.Columns {
				parts[j] = t.Col(j).Get(i).AsString()
			}
			key = strings.Join(parts, "\x00")
		}

		if !seen[key] {
			seen[key] = true
			result.AddRow(rowVals(t, i))
		}
	}
	return result, nil
}

func execRename(o *ast.RenameOp, t *table.Table) (*table.Table, error) {
	newCols := make([]string, len(t.Columns))
	copy(newCols, t.Columns)

	renamed := make(map[int]bool)
	for _, pair := range o.Pairs {
		idx := t.ColIndex(pair.Old)
		if idx < 0 {
			return nil, fmt.Errorf("rename: column %q not found", pair.Old)
		}
		if renamed[idx] {
			return nil, fmt.Errorf("rename: column %q renamed more than once", pair.Old)
		}
		renamed[idx] = true
		newCols[idx] = pair.New
	}

	seen := make(map[string]bool)
	for _, c := range newCols {
		if seen[c] {
			return nil, fmt.Errorf("rename: duplicate column name %q in result; pick a unique name", c)
		}
		seen[c] = true
	}

	return t.ShallowClone(newCols), nil
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

	return t.SelectCols(keepIndices, keepCols), nil
}
