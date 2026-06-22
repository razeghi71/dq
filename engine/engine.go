package engine

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/razeghi71/dq/ast"
	"github.com/razeghi71/dq/table"
)

// Execute runs a full query pipeline on the given input table.
// load is required when the pipeline contains join; pass nil otherwise.
func Execute(query *ast.Query, input *table.Table, load LoadFunc) (*table.Table, error) {
	current := input
	for i := 0; i < len(query.Ops); {
		if isSimpleOp(query.Ops[i]) {
			end := i + 1
			for end < len(query.Ops) && isSimpleOp(query.Ops[end]) {
				end++
			}
			plan, err := planSimplePipelineFromTable(current, query.Ops[i:end])
			if err != nil {
				return nil, err
			}
			current, err = executePlannedPipeline(plan, current)
			if err != nil {
				return nil, err
			}
			i = end
			continue
		}

		var err error
		current, err = execOp(query.Ops[i], current, load)
		if err != nil {
			return nil, err
		}
		i++
	}
	return current, nil
}

func execOp(op ast.Op, t *table.Table, load LoadFunc) (*table.Table, error) {
	switch o := op.(type) {
	case *ast.GroupOp:
		return execGroup(o, t)
	case *ast.TransformOp:
		return execTransform(o, t)
	case *ast.ReduceOp:
		return execReduce(o, t)
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
	return resolveNestedValuePath(val, path[1:])
}

func resolveBoundColumn(col boundColumn, t *table.Table, rowIdx int) (table.Value, error) {
	if col.topIndex < 0 || col.topIndex >= len(t.Columns) {
		return table.Null(), fmt.Errorf("bound column index %d out of range", col.topIndex)
	}
	val := t.Col(col.topIndex).Get(rowIdx)
	return resolveNestedValuePath(val, col.nestedPath)
}

func resolveNestedValuePath(val table.Value, path []string) (table.Value, error) {
	for _, seg := range path {
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

func columnSchemas(t *table.Table) []*table.TypeDescriptor {
	schemas := make([]*table.TypeDescriptor, len(t.Columns))
	for i := range t.Columns {
		schemas[i] = t.Col(i).Schema()
	}
	return schemas
}

func canonicalTupleKey(parts []string) string {
	if len(parts) == 1 {
		return parts[0]
	}
	size := 0
	for _, part := range parts {
		size += len(part) + 21
	}
	var b strings.Builder
	b.Grow(size)
	var buf [20]byte
	for _, part := range parts {
		n := strconv.AppendInt(buf[:0], int64(len(part)), 10)
		b.Write(n)
		b.WriteByte(':')
		b.WriteString(part)
	}
	return b.String()
}

type plannedProjection struct {
	column boundColumn
}

type projectionPlan struct {
	cols        []string
	schemas     []*table.TypeDescriptor
	projections []plannedProjection
	topLevelIdx []int
}

func planProjectionsInEnv(opName string, paths [][]string, env schemaEnv) (*projectionPlan, error) {
	plan := &projectionPlan{
		cols:        make([]string, 0, len(paths)),
		schemas:     make([]*table.TypeDescriptor, len(paths)),
		projections: make([]plannedProjection, len(paths)),
		topLevelIdx: make([]int, len(paths)),
	}
	topLevelOnly := true
	for i, path := range paths {
		bound, err := bindColumnPathInEnv(env, path, &ast.ColumnExpr{Path: path})
		if err != nil {
			return nil, fmt.Errorf("%s %q: %w", opName, strings.Join(path, "."), err)
		}
		if err := validatePathDoesNotTraverseUnionInEnv(opName, env, path); err != nil {
			return nil, err
		}
		base := pathToColumnName(path)
		name := uniqueColumnName(base, plan.cols)
		plan.cols = append(plan.cols, name)
		plan.schemas[i] = bound.typ
		plan.projections[i] = plannedProjection{column: *bound}
		if len(path) == 1 {
			plan.topLevelIdx[i] = bound.topIndex
		} else {
			topLevelOnly = false
		}
	}
	if !topLevelOnly {
		plan.topLevelIdx = nil
	}
	return plan, nil
}

func unionBeforePathEnd(t *table.Table, path []string) *table.TypeDescriptor {
	return unionBeforePathEndInEnv(schemaEnvFromTable(t), path)
}

func unionBeforePathEndInEnv(env schemaEnv, path []string) *table.TypeDescriptor {
	if len(path) < 2 {
		return nil
	}
	idx := env.colIndex(path[0])
	if idx < 0 {
		return nil
	}
	return unionBeforePathEndInSchema(env.finalSchema(idx), path[1:])
}

func unionBeforePathEndInSchema(schema *table.TypeDescriptor, path []string) *table.TypeDescriptor {
	if schema == nil || len(path) == 0 {
		return nil
	}
	cur := schema
	for _, seg := range path {
		cur = table.FinalizeSchema(cur)
		if cur == nil {
			return nil
		}
		if cur.Kind == table.TypeUnion {
			return cur
		}
		if cur.Kind != table.TypeRecord {
			return nil
		}
		var next *table.TypeDescriptor
		for _, field := range cur.Fields {
			if field.Name == seg {
				next = field.Type
				break
			}
		}
		if next == nil {
			return nil
		}
		cur = next
	}
	return nil
}

func unionPathTraversalError(path []string, schema *table.TypeDescriptor) error {
	return fmt.Errorf("%q: cannot access fields through union schema %s", strings.Join(path, "."), schema.String())
}

func validatePathDoesNotTraverseUnion(op string, t *table.Table, path []string) error {
	return validatePathDoesNotTraverseUnionInEnv(op, schemaEnvFromTable(t), path)
}

func validatePathDoesNotTraverseUnionInEnv(op string, env schemaEnv, path []string) error {
	if schema := unionBeforePathEndInEnv(env, path); schema != nil {
		return fmt.Errorf("%s %q: cannot access fields through union schema %s", op, strings.Join(path, "."), schema.String())
	}
	return nil
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

	if cmp, err := table.CompareStrict(a, b); err == nil {
		return cmp
	}

	if a.Type != b.Type {
		return strings.Compare(table.TypeName(a.Type), table.TypeName(b.Type))
	}
	return strings.Compare(table.CanonicalKey(a), table.CanonicalKey(b))
}

func execGroup(o *ast.GroupOp, t *table.Table) (*table.Table, error) {
	// Build output key column names with dedup
	var keyColNames []string
	boundKeys := make([]*boundColumn, len(o.Columns))
	for _, path := range o.Columns {
		bound, err := bindColumnPath(t, path, &ast.ColumnExpr{Path: path})
		if err != nil {
			return nil, fmt.Errorf("group %q: %w", strings.Join(path, "."), err)
		}
		if err := validatePathDoesNotTraverseUnion("group", t, path); err != nil {
			return nil, err
		}
		boundKeys[len(keyColNames)] = bound
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
			keyParts[j] = table.CanonicalKey(v)
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
	schemas := make([]*table.TypeDescriptor, len(resultCols))
	for i, bound := range boundKeys {
		schemas[i] = bound.typ
	}
	schemas[len(schemas)-1] = &table.TypeDescriptor{Kind: table.TypeList, Elem: recordSchemaForTable(t)}
	result := table.NewTableWithSchemas(resultCols, schemas)
	for _, g := range groups {
		vals := make([]table.Value, len(g.key)+1)
		copy(vals, g.key)
		vals[len(g.key)] = table.ListVal(g.records)
		if err := result.AddRowTyped(vals); err != nil {
			return nil, fmt.Errorf("group: %w", err)
		}
	}
	return result, nil
}

func execTransform(o *ast.TransformOp, t *table.Table) (*table.Table, error) {
	newCols := make([]string, len(t.Columns))
	copy(newCols, t.Columns)
	assignTargets := make([]int, len(o.Assignments))
	seenTargets := make(map[string]bool, len(o.Assignments))

	for i, a := range o.Assignments {
		if seenTargets[a.Column] {
			return nil, fmt.Errorf("transform target %q assigned more than once", a.Column)
		}
		seenTargets[a.Column] = true
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

	plannedAssignments := make([]typedExpr, len(o.Assignments))
	for i, a := range o.Assignments {
		planned, err := planTransformExpr(a.Expr, t)
		if err != nil {
			return nil, fmt.Errorf("transform %q: %w", a.Column, err)
		}
		plannedAssignments[i] = planned
	}

	schemas := columnSchemas(t)
	for len(schemas) < len(newCols) {
		schemas = append(schemas, nil)
	}
	for _, target := range assignTargets {
		schemas[target] = nil
	}
	for i, planned := range plannedAssignments {
		schemas[assignTargets[i]] = table.FinalizeSchema(planned.typ)
	}
	compiledAssignments := make([]rowValueEvaluator, len(plannedAssignments))
	for i, planned := range plannedAssignments {
		compiledAssignments[i] = compileTypedRowValue(planned, t)
	}
	useTypedAppend := allSchemasKnown(schemas, assignTargets)
	if useTypedAppend {
		if result, ok, err := execAppendOnlyTypedTransform(o, t, newCols, schemas, assignTargets, compiledAssignments); ok || err != nil {
			return result, err
		}
	}
	result := table.NewTableWithSchemas(newCols, schemas)
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

		for j, a := range o.Assignments {
			v, err := compiledAssignments[j](i)
			if err != nil {
				return nil, fmt.Errorf("transform %q: %w", a.Column, err)
			}
			vals[assignTargets[j]] = v
		}
		if useTypedAppend {
			if err := result.AddRowTypedColumns(vals, assignTargets); err != nil {
				return nil, fmt.Errorf("transform: %w", err)
			}
		} else {
			result.AddRow(vals)
		}
	}
	return result, nil
}

func execAppendOnlyTypedTransform(o *ast.TransformOp, t *table.Table, newCols []string, schemas []*table.TypeDescriptor, assignTargets []int, assignments []rowValueEvaluator) (*table.Table, bool, error) {
	if !appendOnlyTransformTargets(assignTargets, len(t.Columns)) {
		return nil, false, nil
	}

	addedNames := newCols[len(t.Columns):]
	addedSchemas := schemas[len(t.Columns):]
	result, err := t.AppendTypedComputedColumnsFunc(addedNames, addedSchemas, func(row int, values []table.Value) error {
		for i, a := range o.Assignments {
			v, err := assignments[i](row)
			if err != nil {
				return transformAssignmentError{err: fmt.Errorf("transform %q: %w", a.Column, err)}
			}
			values[assignTargets[i]-len(t.Columns)] = v
		}
		return nil
	})
	if err != nil {
		var assignmentErr transformAssignmentError
		if errors.As(err, &assignmentErr) {
			return nil, true, assignmentErr.err
		}
		return nil, true, fmt.Errorf("transform: %w", err)
	}
	return result, true, nil
}

type transformAssignmentError struct {
	err error
}

func (e transformAssignmentError) Error() string {
	return e.err.Error()
}

func (e transformAssignmentError) Unwrap() error {
	return e.err
}

func appendOnlyTransformTargets(assignTargets []int, baseCols int) bool {
	seen := make(map[int]bool, len(assignTargets))
	for _, target := range assignTargets {
		if target < baseCols || seen[target] {
			return false
		}
		seen[target] = true
	}
	return true
}

func execReduce(o *ast.ReduceOp, t *table.Table) (*table.Table, error) {
	nestedIdx := t.ColIndex(o.NestedName)
	if nestedIdx < 0 {
		return nil, fmt.Errorf("reduce: nested column %q not found (did you forget to group first?)", o.NestedName)
	}

	newCols := make([]string, len(t.Columns))
	copy(newCols, t.Columns)
	assignTargets := make([]int, len(o.Assignments))
	seenTargets := make(map[string]bool, len(o.Assignments))

	for i, a := range o.Assignments {
		if seenTargets[a.Column] {
			return nil, fmt.Errorf("reduce target %q assigned more than once", a.Column)
		}
		seenTargets[a.Column] = true
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

	schemas := columnSchemas(t)
	for len(schemas) < len(newCols) {
		schemas = append(schemas, nil)
	}
	nestedSchema, err := nestedRecordSchemaForReduce(o.NestedName, t.Col(nestedIdx).Schema())
	if err != nil {
		return nil, err
	}
	plannedAssignments := make([]typedExpr, len(o.Assignments))
	for i, a := range o.Assignments {
		planned, err := planReduceExpr(a.Expr, nestedSchema)
		if err != nil {
			return nil, fmt.Errorf("reduce %q: %w", a.Column, err)
		}
		plannedAssignments[i] = planned
	}
	for _, target := range assignTargets {
		schemas[target] = nil
	}
	for i, planned := range plannedAssignments {
		schemas[assignTargets[i]] = table.FinalizeSchema(planned.typ)
	}
	result := table.NewTableWithSchemas(newCols, schemas)
	useTypedAppend := allSchemasKnown(schemas, assignTargets)
	for i := 0; i < t.NumRows; i++ {
		nested := t.Col(nestedIdx).Get(i)
		if nested.Type != table.TypeList {
			return nil, fmt.Errorf("reduce: column %q is not a list (did you forget to group first?)", o.NestedName)
		}

		nestedTable, err := table.ListToTableWithSchema(nested, nestedSchema)
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
			v, err := evalTypedAggregateExpression(plannedAssignments[j], nestedTable)
			if err != nil {
				return nil, fmt.Errorf("reduce %q: %w", a.Column, err)
			}
			vals[assignTargets[j]] = v
		}
		if useTypedAppend {
			if err := result.AddRowTypedColumns(vals, assignTargets); err != nil {
				return nil, fmt.Errorf("reduce: %w", err)
			}
		} else {
			result.AddRow(vals)
		}
	}
	return result, nil
}
