package engine

import (
	"fmt"
	"sort"
	"strings"

	"github.com/razeghi71/dq/ast"
	"github.com/razeghi71/dq/table"
)

// pipelinePlan is the planned form of a schema-planned pipeline span.
type pipelinePlan struct {
	Ops          []plannedOp
	InputSchema  table.Schema
	OutputSchema table.Schema
}

// plannedOp is a schema-planned operation.
type plannedOp interface {
	OutputSchema() table.Schema
}

type plannedBase struct {
	output table.Schema
}

func (p plannedBase) OutputSchema() table.Schema {
	return p.output
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

type plannedTransformAssignment struct {
	name   string
	target int
	expr   typedExpr
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

func isPlannedSpanOp(op ast.Op) bool {
	switch op.(type) {
	case *ast.HeadOp, *ast.TailOp, *ast.FilterOp, *ast.SelectOp, *ast.SortOp,
		*ast.TransformOp, *ast.RenameOp, *ast.RemoveOp, *ast.DistinctOp,
		*ast.CountOp, *ast.DescribeOp:
		return true
	default:
		return false
	}
}

// planSchemaPipeline plans a span containing operations with schema-only
// planners. It binds column references, validates operator-specific schema
// rules, and computes the output schema for each operation without inspecting
// rows.
func planSchemaPipeline(input table.Schema, ops []ast.Op) (*pipelinePlan, error) {
	return planSchemaPipelineInEnv(schemaEnvFromSchema(input), ops)
}

func planSchemaPipelineFromTable(input *table.Table, ops []ast.Op) (*pipelinePlan, error) {
	return planSchemaPipelineInEnv(schemaEnvFromTable(input), ops)
}

func planSchemaPipelineInEnv(input schemaEnv, ops []ast.Op) (*pipelinePlan, error) {
	current := input
	planned := make([]plannedOp, 0, len(ops))
	output := input.schema()

	for i, op := range ops {
		if !isPlannedSpanOp(op) {
			return nil, fmt.Errorf("cannot plan operation %T in schema-planned pipeline", op)
		}
		next, err := planSchemaOp(current, op)
		if err != nil {
			return nil, err
		}
		planned = append(planned, next)
		output = next.OutputSchema()
		if i+1 < len(ops) {
			current = schemaEnvFromSchema(output)
		}
	}

	return &pipelinePlan{Ops: planned, InputSchema: input.schema(), OutputSchema: output}, nil
}

func planSchemaOp(input schemaEnv, op ast.Op) (plannedOp, error) {
	switch o := op.(type) {
	case *ast.HeadOp:
		return plannedHead{plannedBase: plannedBase{output: input.schema()}, n: o.N}, nil
	case *ast.TailOp:
		return plannedTail{plannedBase: plannedBase{output: input.schema()}, n: o.N}, nil
	case *ast.FilterOp:
		expr, err := planFilterExprInEnv(o.Expr, input)
		if err != nil {
			return nil, fmt.Errorf("filter: %w", err)
		}
		return plannedFilter{plannedBase: plannedBase{output: input.schema()}, expr: expr}, nil
	case *ast.TransformOp:
		return planTransform(o, input)
	case *ast.SortOp:
		keys, err := planSortKeys(o, input)
		if err != nil {
			return nil, err
		}
		return plannedSort{plannedBase: plannedBase{output: input.schema()}, keys: keys}, nil
	case *ast.SelectOp:
		projections, err := planProjectionsInEnv("select", o.Columns, input)
		if err != nil {
			return nil, err
		}
		output := schemaFromColumns(projections.cols, projections.schemas)
		if projections.topLevelIdx != nil {
			output = rawSchemaFromColumns(projections.cols, projections.schemas)
		}
		return plannedSelect{plannedBase: plannedBase{output: output}, projections: projections}, nil
	case *ast.RenameOp:
		cols, err := planRenameColumns(o, input)
		if err != nil {
			return nil, err
		}
		return plannedRename{plannedBase: plannedBase{output: rawSchemaFromColumns(cols, input.rawSchemas())}}, nil
	case *ast.RemoveOp:
		cols, indices, schemas, err := planRemoveColumns(o, input)
		if err != nil {
			return nil, err
		}
		return plannedRemove{plannedBase: plannedBase{output: rawSchemaFromColumns(cols, schemas)}, indices: indices}, nil
	case *ast.DistinctOp:
		if len(o.Columns) == 0 {
			return plannedDistinct{plannedBase: plannedBase{output: input.schema()}}, nil
		}
		projections, err := planProjectionsInEnv("distinct", o.Columns, input)
		if err != nil {
			return nil, err
		}
		output := schemaFromColumns(projections.cols, projections.schemas)
		return plannedDistinct{plannedBase: plannedBase{output: output}, projections: projections}, nil
	case *ast.CountOp:
		return plannedCount{plannedBase: plannedBase{output: countOutputSchema()}}, nil
	case *ast.DescribeOp:
		return plannedDescribe{plannedBase: plannedBase{output: describeOutputSchema()}}, nil
	default:
		return nil, fmt.Errorf("unknown schema-planned operation type %T", op)
	}
}

func planTransform(o *ast.TransformOp, input schemaEnv) (plannedTransform, error) {
	cols := append([]string(nil), input.columns...)
	assignments := make([]plannedTransformAssignment, len(o.Assignments))
	seenTargets := make(map[string]bool, len(o.Assignments))

	for i, a := range o.Assignments {
		if seenTargets[a.Column] {
			return plannedTransform{}, fmt.Errorf("transform target %q assigned more than once", a.Column)
		}
		seenTargets[a.Column] = true

		idx := -1
		for j, col := range cols {
			if col == a.Column {
				idx = j
				break
			}
		}
		if idx < 0 {
			idx = len(cols)
			cols = append(cols, a.Column)
		}
		assignments[i].name = a.Column
		assignments[i].target = idx
	}

	for i, a := range o.Assignments {
		planned, err := planTransformExprInEnv(a.Expr, input)
		if err != nil {
			return plannedTransform{}, fmt.Errorf("transform %q: %w", a.Column, err)
		}
		assignments[i].expr = planned
	}

	schemas := input.rawSchemas()
	for len(schemas) < len(cols) {
		schemas = append(schemas, nil)
	}
	for _, assignment := range assignments {
		schemas[assignment.target] = nil
	}
	for _, assignment := range assignments {
		schemas[assignment.target] = table.FinalizeSchema(assignment.expr.typ)
	}

	return plannedTransform{
		plannedBase: plannedBase{output: rawSchemaFromColumns(cols, schemas)},
		assignments: assignments,
	}, nil
}

func planSortKeys(o *ast.SortOp, input schemaEnv) ([]plannedSortKey, error) {
	keys := make([]plannedSortKey, len(o.Keys))
	for i, k := range o.Keys {
		bound, err := bindColumnPathInEnv(input, k.Path, &ast.ColumnExpr{Path: k.Path})
		if err != nil {
			return nil, fmt.Errorf("sort %q: %w", strings.Join(k.Path, "."), err)
		}
		if err := validatePathDoesNotTraverseUnionInEnv("sort", input, k.Path); err != nil {
			return nil, err
		}
		schema := table.FinalizeSchema(bound.typ)
		if table.SchemaContainsUnion(schema) {
			return nil, fmt.Errorf("sort %q: union values are not orderable", strings.Join(k.Path, "."))
		}
		if !table.IsOrderable(schema) {
			return nil, fmt.Errorf("sort %q: %s values are not orderable", strings.Join(k.Path, "."), table.TypeName(schema.Kind))
		}
		keys[i] = plannedSortKey{column: *bound, desc: k.Desc}
	}
	return keys, nil
}

func planRenameColumns(o *ast.RenameOp, input schemaEnv) ([]string, error) {
	cols := make([]string, len(input.columns))
	copy(cols, input.columns)

	renamed := make(map[int]bool, len(o.Pairs))
	for _, pair := range o.Pairs {
		idx := input.colIndex(pair.Old)
		if idx < 0 {
			return nil, fmt.Errorf("rename: column %q not found", pair.Old)
		}
		if renamed[idx] {
			return nil, fmt.Errorf("rename: column %q renamed more than once", pair.Old)
		}
		renamed[idx] = true
		cols[idx] = pair.New
	}

	seen := make(map[string]bool, len(cols))
	for _, col := range cols {
		if seen[col] {
			return nil, fmt.Errorf("rename: duplicate column name %q in result; pick a unique name", col)
		}
		seen[col] = true
	}
	return cols, nil
}

func planRemoveColumns(o *ast.RemoveOp, input schemaEnv) ([]string, []int, []*table.TypeDescriptor, error) {
	removeSet := make(map[string]bool, len(o.Columns))
	for _, path := range o.Columns {
		if len(path) != 1 {
			return nil, nil, nil, fmt.Errorf("remove: dot paths not supported, got %q", strings.Join(path, "."))
		}
		col := path[0]
		if input.colIndex(col) < 0 {
			return nil, nil, nil, fmt.Errorf("remove: column %q not found", col)
		}
		removeSet[col] = true
	}

	cols := make([]string, 0, len(input.columns))
	indices := make([]int, 0, len(input.columns))
	schemas := make([]*table.TypeDescriptor, 0, len(input.columns))
	for i, col := range input.columns {
		if removeSet[col] {
			continue
		}
		cols = append(cols, col)
		indices = append(indices, i)
		schemas = append(schemas, input.rawSchema(i))
	}
	return cols, indices, schemas, nil
}

func schemaFromColumns(cols []string, schemas []*table.TypeDescriptor) table.Schema {
	return table.NewTableWithSchemas(cols, schemas).Schema()
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

func rawSchemaFromColumns(cols []string, schemas []*table.TypeDescriptor) table.Schema {
	return table.NewSchema(cols, schemas)
}

func countOutputSchema() table.Schema {
	return schemaFromColumns([]string{"count"}, []*table.TypeDescriptor{{Kind: table.TypeInt}})
}

func describeOutputSchema() table.Schema {
	return schemaFromColumns(
		[]string{"column", "type", "row_count", "schema"},
		[]*table.TypeDescriptor{
			{Kind: table.TypeString},
			{Kind: table.TypeString},
			{Kind: table.TypeInt},
			{Kind: table.TypeString},
		},
	)
}

// executePlan executes a planned pipeline span against rows.
func executePlan(plan *pipelinePlan, input *table.Table) (*table.Table, error) {
	if err := validatePlanInputTableSchema(plan.InputSchema, input); err != nil {
		return nil, err
	}
	return executePlannedPipeline(plan, input)
}

func executePlannedPipeline(plan *pipelinePlan, input *table.Table) (*table.Table, error) {
	current := input
	for _, op := range plan.Ops {
		var err error
		current, err = execPlannedOp(op, current)
		if err != nil {
			return nil, err
		}
	}
	return current, nil
}

func validatePlanInputTableSchema(want table.Schema, got *table.Table) error {
	if got == nil {
		if len(want.Columns) == 0 {
			return nil
		}
		return fmt.Errorf("execute plan: input schema column count mismatch: planned %d columns, got 0", len(want.Columns))
	}
	env := schemaEnvFromTable(got)
	if len(want.Columns) != len(env.columns) {
		return fmt.Errorf("execute plan: input schema column count mismatch: planned %d columns, got %d", len(want.Columns), len(env.columns))
	}
	for i := range want.Columns {
		w := want.Columns[i]
		if w.Name != env.columns[i] {
			return fmt.Errorf("execute plan: input schema column %d mismatch: planned %q, got %q", i, w.Name, env.columns[i])
		}
		gotSchema := env.rawSchema(i)
		if !table.Same(w.Type, gotSchema) {
			return fmt.Errorf("execute plan: input schema for column %q mismatch: planned %s, got %s", w.Name, table.Render(w.Type), table.Render(gotSchema))
		}
	}
	return nil
}

func execPlannedOp(op plannedOp, input *table.Table) (*table.Table, error) {
	switch p := op.(type) {
	case plannedHead:
		return execPlannedHead(p, input), nil
	case plannedTail:
		return execPlannedTail(p, input), nil
	case plannedFilter:
		return execPlannedFilter(p, input)
	case plannedTransform:
		return execPlannedTransform(p, input)
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
	default:
		return nil, fmt.Errorf("unknown planned operation type %T", op)
	}
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

func outputSchemaColumns(schema table.Schema) ([]string, []*table.TypeDescriptor) {
	cols := make([]string, len(schema.Columns))
	schemas := make([]*table.TypeDescriptor, len(schema.Columns))
	for i, col := range schema.Columns {
		cols[i] = col.Name
		schemas[i] = col.Type
	}
	return cols, schemas
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
