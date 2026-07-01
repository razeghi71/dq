package engine

import (
	"fmt"
	"sort"
	"strings"

	"github.com/razeghi71/dq/ast"
	"github.com/razeghi71/dq/table"
)

// plannedOp is a schema-planned operation.
type plannedOp interface {
	OutputSchema() table.Schema
	executionTraits() plannedExecutionTraits
}

type plannedExecutionClass int

const (
	plannedExecutionRowLocal plannedExecutionClass = iota
	plannedExecutionRowSpan
	plannedExecutionEarlyStop
	plannedExecutionStreamingFold
	plannedExecutionMaterializedBoundary
)

type plannedExecutionTraits struct {
	class    plannedExecutionClass
	rowLocal plannedRowLocalInfo
}

type plannedBase struct {
	output table.Schema
}

func (p plannedBase) OutputSchema() table.Schema {
	return p.output
}

func (plannedHead) executionTraits() plannedExecutionTraits {
	return plannedExecutionTraits{class: plannedExecutionEarlyStop}
}
func (plannedTail) executionTraits() plannedExecutionTraits {
	return plannedExecutionTraits{class: plannedExecutionMaterializedBoundary}
}
func (p plannedFilter) executionTraits() plannedExecutionTraits {
	return plannedExecutionTraits{
		class: plannedExecutionRowLocal,
		rowLocal: plannedRowLocalInfo{
			dropsRows:         true,
			parallelCandidate: typedExprHasCall(p.expr),
		},
	}
}
func (p plannedTransform) executionTraits() plannedExecutionTraits {
	return plannedExecutionTraits{
		class:    plannedExecutionRowLocal,
		rowLocal: plannedRowLocalInfo{parallelCandidate: len(p.assignments) > 0},
	}
}
func (plannedRowSpan) executionTraits() plannedExecutionTraits {
	return plannedExecutionTraits{class: plannedExecutionRowSpan}
}
func (plannedGroup) executionTraits() plannedExecutionTraits {
	return plannedExecutionTraits{class: plannedExecutionMaterializedBoundary}
}
func (plannedReduce) executionTraits() plannedExecutionTraits {
	return plannedExecutionTraits{class: plannedExecutionMaterializedBoundary}
}
func (plannedGroupReduce) executionTraits() plannedExecutionTraits {
	return plannedExecutionTraits{class: plannedExecutionMaterializedBoundary}
}
func (plannedSort) executionTraits() plannedExecutionTraits {
	return plannedExecutionTraits{class: plannedExecutionMaterializedBoundary}
}
func (plannedSelect) executionTraits() plannedExecutionTraits {
	return plannedExecutionTraits{class: plannedExecutionRowLocal}
}
func (plannedRename) executionTraits() plannedExecutionTraits {
	return plannedExecutionTraits{class: plannedExecutionRowLocal}
}
func (plannedRemove) executionTraits() plannedExecutionTraits {
	return plannedExecutionTraits{class: plannedExecutionRowLocal}
}
func (plannedDistinct) executionTraits() plannedExecutionTraits {
	return plannedExecutionTraits{class: plannedExecutionMaterializedBoundary}
}
func (plannedCount) executionTraits() plannedExecutionTraits {
	return plannedExecutionTraits{class: plannedExecutionStreamingFold}
}
func (plannedDescribe) executionTraits() plannedExecutionTraits {
	return plannedExecutionTraits{class: plannedExecutionStreamingFold}
}
func (plannedJoin) executionTraits() plannedExecutionTraits {
	return plannedExecutionTraits{class: plannedExecutionMaterializedBoundary}
}

type plannedHead struct {
	plannedBase
	n int
}

type plannedTail struct {
	plannedBase
	n int
}

type plannedFilter struct {
	plannedBase
	expr typedExpr
}

type plannedTransform struct {
	plannedBase
	assignments []plannedTransformAssignment
}

type plannedRowSpan struct {
	plannedBase
	ops               []plannedOp
	dropsRows         bool
	parallelCandidate bool
}

type plannedTransformAssignment struct {
	name   string
	target int
	expr   typedExpr
}

type plannedGroup struct {
	plannedBase
	keys []plannedGroupKey
}

type plannedGroupKey struct {
	column boundColumn
}

type plannedReduce struct {
	plannedBase
	nestedName   string
	nestedIndex  int
	nestedSchema *table.TypeDescriptor
	assignments  []plannedReduceAssignment
}

type plannedReduceAssignment struct {
	name   string
	target int
	expr   typedExpr
}

type plannedGroupReduce struct {
	plannedBase
	keys              []plannedGroupReduceKey
	nestedName        string
	materializeNested bool
	assignments       []plannedGroupReduceAssignment
	slots             []plannedAggregateSlot
}

type plannedGroupReduceKey struct {
	name   string
	column boundColumn
}

type plannedGroupReduceAssignment struct {
	name   string
	target int
	expr   aggregateFinalExpr
}

type plannedAggregateSlot struct {
	name      string
	aggregate *aggregateSpec
	args      []boundColumn
}

type plannedSort struct {
	plannedBase
	keys []plannedSortKey
}

type plannedSortKey struct {
	column boundColumn
	desc   bool
}

type plannedSelect struct {
	plannedBase
	projections *projectionPlan
}

type plannedRename struct {
	plannedBase
}

type plannedRemove struct {
	plannedBase
	indices []int
}

type plannedDistinct struct {
	plannedBase
	projections *projectionPlan
}

type plannedCount struct {
	plannedBase
}

type plannedDescribe struct {
	plannedBase
}

type plannedJoin struct {
	plannedBase
	kind      string
	right     *table.Table
	leftKeys  []resolvedJoinKey
	rightKeys []resolvedJoinKey
	outputs   []plannedJoinOutput
}

type plannedJoinOutputKind int

const (
	plannedJoinOutputLeft plannedJoinOutputKind = iota
	plannedJoinOutputKey
	plannedJoinOutputRight
)

type plannedJoinOutput struct {
	kind       plannedJoinOutputKind
	leftIndex  int
	keyIndex   int
	rightIndex int
}

func isSchemaPlannedOp(op ast.Op) bool {
	switch op.(type) {
	case *ast.HeadOp, *ast.TailOp, *ast.FilterOp, *ast.SelectOp, *ast.SortOp,
		*ast.TransformOp, *ast.GroupOp, *ast.ReduceOp, *ast.RenameOp,
		*ast.RemoveOp, *ast.DistinctOp, *ast.CountOp, *ast.DescribeOp,
		*ast.JoinOp:
		return true
	default:
		return false
	}
}

func containsColumnName(cols []string, name string) bool {
	for _, col := range cols {
		if col == name {
			return true
		}
	}
	return false
}

func planRenameColumns(o *ast.RenameOp, input schemaEnv) ([]schemaEnvColumn, error) {
	columns := input.cloneColumns()

	renamed := make(map[int]bool, len(o.Pairs))
	for _, pair := range o.Pairs {
		col, ok := input.lookupColumn(pair.Old)
		if !ok {
			return nil, fmt.Errorf("rename: column %q not found", pair.Old)
		}
		idx := col.index
		if renamed[idx] {
			return nil, fmt.Errorf("rename: column %q renamed more than once", pair.Old)
		}
		renamed[idx] = true
		columns[idx].name = pair.New
	}

	seen := make(map[string]bool, len(columns))
	for _, col := range columns {
		if seen[col.name] {
			return nil, fmt.Errorf("rename: duplicate column name %q in result; pick a unique name", col.name)
		}
		seen[col.name] = true
	}
	return columns, nil
}

func planRemoveColumns(o *ast.RemoveOp, input schemaEnv) ([]schemaEnvColumn, []int, []string, error) {
	removeSet := make(map[string]bool, len(o.Columns))
	for _, path := range o.Columns {
		if len(path) != 1 {
			return nil, nil, nil, fmt.Errorf("remove: dot paths not supported, got %q", strings.Join(path, "."))
		}
		col := path[0]
		if _, ok := input.lookupColumn(col); !ok {
			return nil, nil, nil, fmt.Errorf("remove: column %q not found", col)
		}
		removeSet[col] = true
	}

	columns := make([]schemaEnvColumn, 0, len(input.columns))
	indices := make([]int, 0, len(input.columns))
	kept := make([]string, 0, len(input.columns))
	for i, col := range input.columns {
		if removeSet[col.name] {
			continue
		}
		columns = append(columns, col)
		indices = append(indices, i)
		kept = append(kept, col.name)
	}
	return columns, indices, kept, nil
}

func tableFromOutputSchema(schema table.Schema) *table.Table {
	cols := make([]string, len(schema.Columns))
	types := make([]*table.TypeDescriptor, len(schema.Columns))
	for i, col := range schema.Columns {
		cols[i] = col.Name
		types[i] = col.Type
	}
	return table.NewTableWithSchemas(cols, types)
}

func executePlannedOps(ops []plannedOp, input *table.Table) (*table.Table, error) {
	current := input
	for _, op := range ops {
		var err error
		current, err = execPlannedOp(op, current)
		if err != nil {
			return nil, err
		}
	}
	return current, nil
}

func execPlannedOp(op plannedOp, input *table.Table) (*table.Table, error) {
	switch p := op.(type) {
	case plannedHead:
		return execPlannedHead(p, input), nil
	case plannedTail:
		return execPlannedTail(p, input), nil
	case plannedFilter:
		return execPlannedFilter(p, input)
	case plannedRowSpan:
		return execPlannedRowSpan(p, input)
	case plannedTransform:
		return execPlannedTransform(p, input)
	case plannedGroup:
		return execPlannedGroup(p, input)
	case plannedReduce:
		return execPlannedReduce(p, input)
	case plannedGroupReduce:
		return execPlannedGroupReduce(p, input)
	case plannedSort:
		return execPlannedSort(p, input)
	case plannedSelect:
		return execPlannedSelect(p, input)
	case plannedRename:
		return execPlannedRename(p, input)
	case plannedRemove:
		return input.SelectColsWithSchema(p.indices, p.OutputSchema())
	case plannedDistinct:
		return execPlannedDistinct(p, input)
	case plannedCount:
		return execPlannedCount(p, input)
	case plannedDescribe:
		return execPlannedDescribe(p, input)
	case plannedJoin:
		return execPlannedJoin(p, input)
	default:
		return nil, fmt.Errorf("unknown planned operation type %T", op)
	}
}

func newPlannedRowSpan(ops []plannedOp) (plannedRowSpan, error) {
	if len(ops) == 0 {
		return plannedRowSpan{}, fmt.Errorf("row span: empty physical span")
	}
	return plannedRowSpan{
		plannedBase:       plannedBase{output: ops[len(ops)-1].OutputSchema()},
		ops:               ops,
		dropsRows:         plannedRowSpanDropsRows(ops),
		parallelCandidate: plannedRowSpanParallelCandidate(ops),
	}, nil
}

type plannedRowLocalInfo struct {
	dropsRows         bool
	parallelCandidate bool
}

func plannedRowLocalInfoForOp(op plannedOp) (plannedRowLocalInfo, bool) {
	traits := op.executionTraits()
	if traits.class != plannedExecutionRowLocal {
		return plannedRowLocalInfo{}, false
	}
	return traits.rowLocal, true
}

func plannedRowSpanDropsRows(ops []plannedOp) bool {
	for _, op := range ops {
		info, ok := plannedRowLocalInfoForOp(op)
		if ok && info.dropsRows {
			return true
		}
	}
	return false
}

func plannedRowSpanParallelCandidate(ops []plannedOp) bool {
	for _, op := range ops {
		info, ok := plannedRowLocalInfoForOp(op)
		if ok && info.parallelCandidate {
			return true
		}
	}
	return false
}

func execPlannedRowSpan(p plannedRowSpan, input *table.Table) (*table.Table, error) {
	current := input
	for _, op := range p.ops {
		var err error
		current, err = execPlannedOp(op, current)
		if err != nil {
			return nil, err
		}
	}
	return current, nil
}

func execPlannedHead(p plannedHead, input *table.Table) *table.Table {
	n := p.n
	if n > input.NumRows {
		n = input.NumRows
	}
	return input.SliceRows(0, n)
}

func execPlannedTail(p plannedTail, input *table.Table) *table.Table {
	n := p.n
	if n > input.NumRows {
		n = input.NumRows
	}
	return input.SliceRows(input.NumRows-n, input.NumRows)
}

func execPlannedFilter(p plannedFilter, input *table.Table) (*table.Table, error) {
	predicate := compileFilterPredicate(p.expr, input)
	kept := make([]int, 0, input.NumRows)
	for i := 0; i < input.NumRows; i++ {
		keep, err := predicate(i)
		if err != nil {
			return nil, fmt.Errorf("filter: %w", err)
		}
		if keep {
			kept = append(kept, i)
		}
	}
	return input.ApplyPermutation(kept), nil
}

func execPlannedTransform(p plannedTransform, input *table.Table) (*table.Table, error) {
	compiled := make([]rowValueEvaluator, len(p.assignments))
	for i, assignment := range p.assignments {
		compiled[i] = compileTypedRowValue(assignment.expr, input)
	}

	cols, schemas := outputSchemaColumns(p.OutputSchema())
	targets := transformAssignmentTargets(p.assignments)
	if allSchemasKnown(schemas, targets) {
		if result, ok, err := execAppendOnlyTypedTransform(input, cols, schemas, p.assignments, compiled); ok || err != nil {
			return result, err
		}
	}

	result := table.NewTableWithSchemas(cols, schemas)
	for row := 0; row < input.NumRows; row++ {
		vals := make([]table.Value, len(cols))
		for col := 0; col < len(input.Columns); col++ {
			vals[col] = input.Col(col).Get(row)
		}
		for col := len(input.Columns); col < len(cols); col++ {
			vals[col] = table.Null()
		}

		for i, assignment := range p.assignments {
			v, err := compiled[i](row)
			if err != nil {
				return nil, fmt.Errorf("transform %q: %w", assignment.name, err)
			}
			vals[assignment.target] = v
		}
		if err := result.AddRowTypedColumns(vals, targets); err != nil {
			return nil, fmt.Errorf("transform: %w", err)
		}
	}
	return result, nil
}

func transformAssignmentTargets(assignments []plannedTransformAssignment) []int {
	targets := make([]int, len(assignments))
	for i, assignment := range assignments {
		targets[i] = assignment.target
	}
	return targets
}

func reduceAssignmentTargets(assignments []plannedReduceAssignment) []int {
	targets := make([]int, len(assignments))
	for i, assignment := range assignments {
		targets[i] = assignment.target
	}
	return targets
}

func outputSchemaColumns(schema table.Schema) ([]string, []*table.TypeDescriptor) {
	cols := make([]string, len(schema.Columns))
	schemas := make([]*table.TypeDescriptor, len(schema.Columns))
	for i, col := range schema.Columns {
		cols[i] = col.Name
		schemas[i] = col.Type
	}
	return cols, schemas
}

type plannedGroupEntry struct {
	key     []table.Value
	records []table.Value
}

func execPlannedGroup(p plannedGroup, input *table.Table) (*table.Table, error) {
	groups := make([]plannedGroupEntry, 0)
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
			groupIdx = len(groups)
			keyMap[key] = groupIdx
			groups = append(groups, plannedGroupEntry{key: keyVals})
		}

		fields := make([]table.RecordField, len(input.Columns))
		for col, name := range input.Columns {
			fields[col] = table.RecordField{Name: name, Value: input.Col(col).Get(row)}
		}
		groups[groupIdx].records = append(groups[groupIdx].records, table.RecordVal(fields))
	}

	result := tableFromOutputSchema(p.OutputSchema())
	for _, group := range groups {
		vals := make([]table.Value, len(group.key)+1)
		copy(vals, group.key)
		vals[len(group.key)] = table.ListVal(group.records)
		if err := result.AddRowTyped(vals); err != nil {
			return nil, fmt.Errorf("group: %w", err)
		}
	}
	return result, nil
}

func execPlannedReduce(p plannedReduce, input *table.Table) (*table.Table, error) {
	if p.nestedIndex < 0 || p.nestedIndex >= len(input.Columns) {
		return nil, fmt.Errorf("reduce: nested column %q not found (did you forget to group first?)", p.nestedName)
	}

	cols, schemas := outputSchemaColumns(p.OutputSchema())
	targets := reduceAssignmentTargets(p.assignments)
	result := table.NewTableWithSchemas(cols, schemas)

	for row := 0; row < input.NumRows; row++ {
		nested := input.Col(p.nestedIndex).Get(row)
		if nested.Type != table.TypeList {
			return nil, fmt.Errorf("reduce: column %q is not a list (did you forget to group first?)", p.nestedName)
		}

		nestedTable, err := table.ListToTableWithSchema(nested, p.nestedSchema)
		if err != nil {
			return nil, fmt.Errorf("reduce: %w", err)
		}

		vals := make([]table.Value, len(cols))
		for col := 0; col < len(input.Columns); col++ {
			vals[col] = input.Col(col).Get(row)
		}
		for col := len(input.Columns); col < len(cols); col++ {
			vals[col] = table.Null()
		}

		for _, assignment := range p.assignments {
			v, err := evalTypedAggregateExpression(assignment.expr, nestedTable)
			if err != nil {
				return nil, fmt.Errorf("reduce %q: %w", assignment.name, err)
			}
			vals[assignment.target] = v
		}
		if err := result.AddRowTypedColumns(vals, targets); err != nil {
			return nil, fmt.Errorf("reduce: %w", err)
		}
	}
	return result, nil
}

func execPlannedSort(p plannedSort, input *table.Table) (*table.Table, error) {
	sortVals := make([][]table.Value, input.NumRows)
	for row := 0; row < input.NumRows; row++ {
		sortVals[row] = make([]table.Value, len(p.keys))
		for j, key := range p.keys {
			v, err := resolveBoundColumn(key.column, input, row)
			if err != nil {
				return nil, fmt.Errorf("sort %q: %w", strings.Join(key.column.rawPath, "."), err)
			}
			sortVals[row][j] = v
		}
	}

	perm := make([]int, input.NumRows)
	for i := range perm {
		perm[i] = i
	}
	sort.SliceStable(perm, func(a, b int) bool {
		for j, key := range p.keys {
			left := sortVals[perm[a]][j]
			right := sortVals[perm[b]][j]
			cmp := compareValues(left, right)
			if cmp != 0 {
				if left.IsNull() || right.IsNull() {
					return cmp < 0
				}
				if key.desc {
					return cmp > 0
				}
				return cmp < 0
			}
		}
		return false
	})
	return input.ApplyPermutation(perm), nil
}

func execPlannedSelect(p plannedSelect, input *table.Table) (*table.Table, error) {
	if p.projections.topLevelIdx != nil {
		return input.SelectColsWithSchema(p.projections.topLevelIdx, p.OutputSchema())
	}
	result := tableFromOutputSchema(p.OutputSchema())
	for i := 0; i < input.NumRows; i++ {
		vals := make([]table.Value, len(p.projections.projections))
		for j, projection := range p.projections.projections {
			v, err := resolveBoundColumn(projection.column, input, i)
			if err != nil {
				return nil, fmt.Errorf("select: %w", err)
			}
			vals[j] = v
		}
		if err := result.AddRowTyped(vals); err != nil {
			return nil, fmt.Errorf("select: %w", err)
		}
	}
	return result, nil
}

func execPlannedRename(p plannedRename, input *table.Table) (*table.Table, error) {
	indices := make([]int, len(input.Columns))
	for i := range indices {
		indices[i] = i
	}
	return input.SelectColsWithSchema(indices, p.OutputSchema())
}

func execPlannedCount(p plannedCount, input *table.Table) (*table.Table, error) {
	result := tableFromOutputSchema(p.OutputSchema())
	if err := result.AddRowTyped([]table.Value{table.IntVal(int64(input.NumRows))}); err != nil {
		return nil, fmt.Errorf("count: %w", err)
	}
	return result, nil
}

func execPlannedDescribe(p plannedDescribe, input *table.Table) (*table.Table, error) {
	result := tableFromOutputSchema(p.OutputSchema())
	for i, name := range input.Columns {
		if err := result.AddRowTyped([]table.Value{
			table.StrVal(name),
			table.StrVal(table.TypeName(input.Col(i).ColType())),
			table.IntVal(int64(input.NumRows)),
			table.StrVal(input.Col(i).Schema().String()),
		}); err != nil {
			return nil, fmt.Errorf("describe: %w", err)
		}
	}
	return result, nil
}

func execPlannedDistinct(p plannedDistinct, input *table.Table) (*table.Table, error) {
	if p.projections == nil {
		return execPlannedFullRowDistinct(p, input)
	}
	return execPlannedProjectedDistinct(p, input)
}

func execPlannedProjectedDistinct(p plannedDistinct, input *table.Table) (*table.Table, error) {
	projections := p.projections
	seen := make(map[string]bool)
	result := tableFromOutputSchema(p.OutputSchema())
	if projections.topLevelIdx != nil {
		for i := 0; i < input.NumRows; i++ {
			var key string
			if len(projections.topLevelIdx) == 1 {
				key = table.CanonicalKey(input.Col(projections.topLevelIdx[0]).Get(i))
			} else {
				keyParts := make([]string, len(projections.topLevelIdx))
				for j, idx := range projections.topLevelIdx {
					keyParts[j] = table.CanonicalKey(input.Col(idx).Get(i))
				}
				key = canonicalTupleKey(keyParts)
			}
			if seen[key] {
				continue
			}
			seen[key] = true
			vals := make([]table.Value, len(projections.topLevelIdx))
			for j, idx := range projections.topLevelIdx {
				vals[j] = input.Col(idx).Get(i)
			}
			if err := result.AddRowTyped(vals); err != nil {
				return nil, fmt.Errorf("distinct: %w", err)
			}
		}
		return result, nil
	}

	for i := 0; i < input.NumRows; i++ {
		vals := make([]table.Value, len(projections.projections))
		var key string
		var keyParts []string
		if len(projections.projections) > 1 {
			keyParts = make([]string, len(projections.projections))
		}
		for j, projection := range projections.projections {
			v, err := resolveBoundColumn(projection.column, input, i)
			if err != nil {
				return nil, fmt.Errorf("distinct %q: %w", strings.Join(projection.column.rawPath, "."), err)
			}
			vals[j] = v
			if len(projections.projections) == 1 {
				key = table.CanonicalKey(v)
			} else {
				keyParts[j] = table.CanonicalKey(v)
			}
		}
		if len(projections.projections) > 1 {
			key = canonicalTupleKey(keyParts)
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		if err := result.AddRowTyped(vals); err != nil {
			return nil, fmt.Errorf("distinct: %w", err)
		}
	}
	return result, nil
}

func execPlannedFullRowDistinct(p plannedDistinct, input *table.Table) (*table.Table, error) {
	seen := make(map[string]bool)
	result := tableFromOutputSchema(p.OutputSchema())
	for i := 0; i < input.NumRows; i++ {
		parts := make([]string, len(input.Columns))
		for j := range input.Columns {
			parts[j] = table.CanonicalKey(input.Col(j).Get(i))
		}
		key := canonicalTupleKey(parts)
		if seen[key] {
			continue
		}
		seen[key] = true
		if err := result.AddRowTyped(rowVals(input, i)); err != nil {
			return nil, fmt.Errorf("distinct: %w", err)
		}
	}
	return result, nil
}
