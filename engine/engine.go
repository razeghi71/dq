package engine

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/razeghi71/dq/ast"
	"github.com/razeghi71/dq/rowstream"
	"github.com/razeghi71/dq/table"
)

// Execute runs a full query pipeline on the given input table.
// load is required when the pipeline contains join; pass nil otherwise.
func Execute(query *ast.Query, input *table.Table, load LoadFunc) (*table.Table, error) {
	if len(query.Ops) == 0 {
		return input, nil
	}
	logical, err := planLogicalPipelineFromTableWithLoad(input, query.Ops, load)
	if err != nil {
		return nil, err
	}
	var optimized optimizedLogicalPipeline
	if err := optimizeLogicalPipelineInto(logical, &optimized); err != nil {
		return nil, err
	}
	var physical physicalPipeline
	if err := planPhysicalPipelineInto(&optimized, &physical); err != nil {
		return nil, err
	}
	// The physical plan was derived from this input table above, so validating
	// the plan input schema here would be redundant. Test-only helpers validate
	// mismatches when executing prebuilt plans directly.
	return executePlannedOps(physical.Ops, input)
}

// ExecuteStreaming runs the query pipeline through the streaming physical
// interpreter over an already materialized input table. It is a compatibility
// bridge for source forms that do not yet provide a native source stream.
func ExecuteStreaming(query *ast.Query, input *table.Table, load LoadFunc) (*table.Table, error) {
	if len(query.Ops) == 0 {
		return input, nil
	}
	logical, err := planLogicalPipelineFromTableWithLoad(input, query.Ops, load)
	if err != nil {
		return nil, err
	}
	var optimized optimizedLogicalPipeline
	if err := optimizeLogicalPipelineInto(logical, &optimized); err != nil {
		return nil, err
	}
	var physical physicalPipeline
	if err := planPhysicalPipelineInto(&optimized, &physical); err != nil {
		return nil, err
	}
	return executePlannedOpsStreaming(physical.Ops, rowstream.FromTable(input))
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
		cur = finalizePlanningSchema(cur)
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

func execAppendOnlyTypedTransform(t *table.Table, newCols []string, schemas []*table.TypeDescriptor, assignments []plannedTransformAssignment, evaluators []rowValueEvaluator) (*table.Table, bool, error) {
	targets := transformAssignmentTargets(assignments)
	if !appendOnlyTransformTargets(targets, len(t.Columns)) {
		return nil, false, nil
	}

	addedNames := newCols[len(t.Columns):]
	addedSchemas := schemas[len(t.Columns):]
	result, err := t.AppendTypedComputedColumnsFunc(addedNames, addedSchemas, func(row int, values []table.Value) error {
		for i, assignment := range assignments {
			v, err := evaluators[i](row)
			if err != nil {
				return transformAssignmentError{err: fmt.Errorf("transform %q: %w", assignment.name, err)}
			}
			values[assignment.target-len(t.Columns)] = v
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
