package engine

import (
	"fmt"
	"strings"

	"github.com/razeghi71/dq/ast"
	"github.com/razeghi71/dq/table"
)

// logicalPipeline is the typed, semantic form of a pipe-shaped query. It has
// output schemas and typed expressions, but no executor-local table indexes.
// Logical plans are immutable after construction; optimizer and physical
// planning stages may read them, but must not rewrite them in place.
type logicalPipeline struct {
	Ops          []logicalOp
	InputEnv     schemaEnv
	InputSchema  table.Schema
	OutputSchema table.Schema
}

type logicalOp interface {
	OutputSchema() table.Schema
}

type logicalBase struct {
	output    table.Schema
	outputEnv schemaEnv
}

func (p logicalBase) OutputSchema() table.Schema {
	return p.output
}

func (p logicalBase) OutputEnv() schemaEnv {
	if p.outputEnv.columns != nil {
		return p.outputEnv
	}
	return schemaEnvFromSchema(p.output)
}

func logicalBaseFromEnv(env schemaEnv) logicalBase {
	return logicalBase{output: env.schema(), outputEnv: env}
}

func logicalOutputEnv(op logicalOp) schemaEnv {
	if carrier, ok := op.(interface{ OutputEnv() schemaEnv }); ok {
		return carrier.OutputEnv()
	}
	return schemaEnvFromSchema(op.OutputSchema())
}

type logicalHead struct {
	logicalBase
	n int
}

type logicalTail struct {
	logicalBase
	n int
}

type logicalFilter struct {
	logicalBase
	expr logicalTypedExpr
}

type logicalTransform struct {
	logicalBase
	assignments []logicalAssignment
}

type logicalAssignment struct {
	name string
	expr logicalTypedExpr
}

type logicalGroup struct {
	logicalBase
	keys []logicalPathBinding
}

type logicalPathBinding struct {
	name   string
	path   []string
	schema *table.TypeDescriptor
}

type logicalReduce struct {
	logicalBase
	nestedName   string
	nestedSchema *table.TypeDescriptor
	assignments  []logicalAssignment
}

type logicalSort struct {
	logicalBase
	keys []logicalSortKey
}

type logicalSortKey struct {
	path []string
	desc bool
}

type logicalSelect struct {
	logicalBase
	projections  []logicalPathBinding
	topLevelOnly bool
}

type logicalRename struct {
	logicalBase
}

type logicalRemove struct {
	logicalBase
	kept []string
}

type logicalDistinct struct {
	logicalBase
	projections  []logicalPathBinding
	topLevelOnly bool
	fullRow      bool
}

type logicalCount struct {
	logicalBase
}

type logicalDescribe struct {
	logicalBase
}

type logicalJoin struct {
	logicalBase
	kind      string
	filename  string
	right     *table.Table
	leftKeys  []logicalPathBinding
	rightKeys []logicalPathBinding
}

// optimizedLogicalPipeline is intentionally separate from logicalPipeline even
// while the optimizer is a semantic no-op. The current no-op pass shares the
// immutable logical facts to avoid planning-time allocation regressions. Future
// optimization passes that rewrite anything must allocate replacement optimized
// nodes instead of mutating shared logical plan internals.
type optimizedLogicalPipeline struct {
	Ops          []logicalOp
	InputEnv     schemaEnv
	InputSchema  table.Schema
	OutputSchema table.Schema
}

type physicalPipeline struct {
	Ops          []plannedOp
	InputSchema  table.Schema
	OutputSchema table.Schema
}

func planLogicalPipelineFromTableWithLoad(input *table.Table, ops []ast.Op, load LoadFunc) (*logicalPipeline, error) {
	return planLogicalPipelineInEnv(schemaEnvFromTable(input), ops, load)
}

func planLogicalPipelineInEnv(input schemaEnv, ops []ast.Op, load LoadFunc) (*logicalPipeline, error) {
	current := input
	planned := make([]logicalOp, 0, len(ops))
	inputSchema := input.schema()
	output := inputSchema

	for i, op := range ops {
		if !isSchemaPlannedOp(op) {
			return nil, fmt.Errorf("cannot plan operation %T in logical pipeline", op)
		}
		next, err := planLogicalOp(current, op, load)
		if err != nil {
			return nil, err
		}
		planned = append(planned, next)
		output = next.OutputSchema()
		if i+1 < len(ops) {
			current = logicalOutputEnv(next)
		}
	}

	return &logicalPipeline{Ops: planned, InputEnv: input, InputSchema: inputSchema, OutputSchema: output}, nil
}

func planLogicalOp(input schemaEnv, op ast.Op, load LoadFunc) (logicalOp, error) {
	switch o := op.(type) {
	case *ast.HeadOp:
		return logicalHead{logicalBase: logicalBaseFromEnv(input), n: o.N}, nil
	case *ast.TailOp:
		return logicalTail{logicalBase: logicalBaseFromEnv(input), n: o.N}, nil
	case *ast.FilterOp:
		expr, err := planLogicalFilterExprInEnv(o.Expr, input)
		if err != nil {
			return nil, fmt.Errorf("filter: %w", err)
		}
		return logicalFilter{logicalBase: logicalBaseFromEnv(input), expr: expr}, nil
	case *ast.TransformOp:
		return planLogicalTransform(o, input)
	case *ast.GroupOp:
		return planLogicalGroup(o, input)
	case *ast.ReduceOp:
		return planLogicalReduce(o, input)
	case *ast.SortOp:
		keys, err := planLogicalSortKeys(o, input)
		if err != nil {
			return nil, err
		}
		return logicalSort{logicalBase: logicalBaseFromEnv(input), keys: keys}, nil
	case *ast.SelectOp:
		projections, topLevelOnly, err := planLogicalProjections("select", o.Columns, input)
		if err != nil {
			return nil, err
		}
		return logicalSelect{logicalBase: logicalBaseFromEnv(logicalProjectionOutputEnv(projections, topLevelOnly)), projections: projections, topLevelOnly: topLevelOnly}, nil
	case *ast.RenameOp:
		cols, err := planRenameColumns(o, input)
		if err != nil {
			return nil, err
		}
		return logicalRename{logicalBase: logicalBaseFromEnv(schemaEnvFromOwnedColumns(cols, input.rawSchemas(), false))}, nil
	case *ast.RemoveOp:
		cols, _, schemas, err := planRemoveColumns(o, input)
		if err != nil {
			return nil, err
		}
		return logicalRemove{logicalBase: logicalBaseFromEnv(schemaEnvFromOwnedColumns(cols, schemas, false)), kept: cols}, nil
	case *ast.DistinctOp:
		if len(o.Columns) == 0 {
			return logicalDistinct{logicalBase: logicalBaseFromEnv(input), fullRow: true}, nil
		}
		projections, topLevelOnly, err := planLogicalProjections("distinct", o.Columns, input)
		if err != nil {
			return nil, err
		}
		return logicalDistinct{logicalBase: logicalBaseFromEnv(logicalProjectionOutputEnv(projections, false)), projections: projections, topLevelOnly: topLevelOnly}, nil
	case *ast.CountOp:
		return logicalCount{logicalBase: logicalBaseFromEnv(countOutputEnv())}, nil
	case *ast.DescribeOp:
		return logicalDescribe{logicalBase: logicalBaseFromEnv(describeOutputEnv())}, nil
	case *ast.JoinOp:
		return planLogicalJoin(o, input, load)
	default:
		return nil, fmt.Errorf("unknown logical operation type %T", op)
	}
}

func planLogicalTransform(o *ast.TransformOp, input schemaEnv) (logicalTransform, error) {
	cols := append([]string(nil), input.columns...)
	assignments := make([]logicalAssignment, len(o.Assignments))
	seenTargets := make(map[string]bool, len(o.Assignments))

	for i, a := range o.Assignments {
		if seenTargets[a.Column] {
			return logicalTransform{}, fmt.Errorf("transform target %q assigned more than once", a.Column)
		}
		seenTargets[a.Column] = true
		if !containsColumnName(cols, a.Column) {
			cols = append(cols, a.Column)
		}
		assignments[i].name = a.Column
	}

	schemas := input.rawSchemas()
	for len(schemas) < len(cols) {
		schemas = append(schemas, nil)
	}
	for i, a := range o.Assignments {
		planned, err := planLogicalTransformExprInEnv(a.Expr, input)
		if err != nil {
			return logicalTransform{}, fmt.Errorf("transform %q: %w", a.Column, err)
		}
		assignments[i].expr = planned
		target := indexOfColumn(cols, a.Column)
		schemas[target] = finalizePlanningSchema(planned.typ)
	}

	outputEnv := schemaEnvFromOwnedColumns(cols, schemas, false)
	return logicalTransform{
		logicalBase: logicalBaseFromEnv(outputEnv),
		assignments: assignments,
	}, nil
}

func planLogicalGroup(o *ast.GroupOp, input schemaEnv) (logicalGroup, error) {
	keyNames := make([]string, 0, len(o.Columns))
	keys := make([]logicalPathBinding, len(o.Columns))
	schemas := make([]*table.TypeDescriptor, 0, len(o.Columns)+1)

	for i, path := range o.Columns {
		bound, err := bindColumnPathLogicalInEnv(input, path)
		if err != nil {
			return logicalGroup{}, fmt.Errorf("group %q: %w", strings.Join(path, "."), err)
		}
		if err := validatePathDoesNotTraverseUnionInEnv("group", input, path); err != nil {
			return logicalGroup{}, err
		}
		name := uniqueColumnName(pathToColumnName(path), keyNames)
		keyNames = append(keyNames, name)
		keys[i] = logicalPathBinding{name: name, path: clonePath(path), schema: bound.typ}
		schemas = append(schemas, bound.typ)
	}

	cols := append([]string(nil), keyNames...)
	if containsColumnName(cols, o.NestedName) {
		return logicalGroup{}, fmt.Errorf("group: nested column name %q collides with a group key output column; use as rows or another distinct nested column name", o.NestedName)
	}
	cols = append(cols, o.NestedName)
	schemas = append(schemas, &table.TypeDescriptor{Kind: table.TypeList, Elem: recordSchemaForEnv(input)})

	return logicalGroup{
		logicalBase: logicalBaseFromEnv(schemaEnvFromOwnedColumns(cols, schemas, false)),
		keys:        keys,
	}, nil
}

func planLogicalReduce(o *ast.ReduceOp, input schemaEnv) (logicalReduce, error) {
	nestedIdx := input.colIndex(o.NestedName)
	if nestedIdx < 0 {
		return logicalReduce{}, fmt.Errorf("reduce: nested column %q not found (did you forget to group first?)", o.NestedName)
	}

	cols := append([]string(nil), input.columns...)
	assignments := make([]logicalAssignment, len(o.Assignments))
	seenTargets := make(map[string]bool, len(o.Assignments))
	for i, a := range o.Assignments {
		if seenTargets[a.Column] {
			return logicalReduce{}, fmt.Errorf("reduce target %q assigned more than once", a.Column)
		}
		seenTargets[a.Column] = true
		if !containsColumnName(cols, a.Column) {
			cols = append(cols, a.Column)
		}
		assignments[i].name = a.Column
	}

	nestedColumnSchema := input.rawSchema(nestedIdx)
	if nestedColumnSchema == nil {
		nestedColumnSchema = input.finalSchema(nestedIdx)
	}
	nestedSchema, err := nestedRecordSchemaForReduce(o.NestedName, nestedColumnSchema)
	if err != nil {
		return logicalReduce{}, err
	}

	schemas := input.rawSchemas()
	for len(schemas) < len(cols) {
		schemas = append(schemas, nil)
	}
	for i, a := range o.Assignments {
		planned, err := planLogicalReduceExpr(a.Expr, nestedSchema)
		if err != nil {
			return logicalReduce{}, fmt.Errorf("reduce %q: %w", a.Column, err)
		}
		assignments[i].expr = planned
		target := indexOfColumn(cols, a.Column)
		schemas[target] = finalizePlanningSchema(planned.typ)
	}

	outputEnv := schemaEnvFromOwnedColumns(cols, schemas, false)
	return logicalReduce{
		logicalBase:  logicalBaseFromEnv(outputEnv),
		nestedName:   o.NestedName,
		nestedSchema: nestedSchema,
		assignments:  assignments,
	}, nil
}

func planLogicalSortKeys(o *ast.SortOp, input schemaEnv) ([]logicalSortKey, error) {
	keys := make([]logicalSortKey, len(o.Keys))
	for i, k := range o.Keys {
		bound, err := bindColumnPathLogicalInEnv(input, k.Path)
		if err != nil {
			return nil, fmt.Errorf("sort %q: %w", strings.Join(k.Path, "."), err)
		}
		if err := validatePathDoesNotTraverseUnionInEnv("sort", input, k.Path); err != nil {
			return nil, err
		}
		schema := finalizePlanningSchema(bound.typ)
		if table.SchemaContainsUnion(schema) {
			return nil, fmt.Errorf("sort %q: union values are not orderable", strings.Join(k.Path, "."))
		}
		if !table.IsOrderable(schema) {
			return nil, fmt.Errorf("sort %q: %s values are not orderable", strings.Join(k.Path, "."), table.TypeName(schema.Kind))
		}
		keys[i] = logicalSortKey{path: clonePath(k.Path), desc: k.Desc}
	}
	return keys, nil
}

func planLogicalProjections(opName string, paths [][]string, env schemaEnv) ([]logicalPathBinding, bool, error) {
	projections := make([]logicalPathBinding, len(paths))
	cols := make([]string, 0, len(paths))
	topLevelOnly := true
	for i, path := range paths {
		bound, err := bindColumnPathLogicalInEnv(env, path)
		if err != nil {
			return nil, false, fmt.Errorf("%s %q: %w", opName, strings.Join(path, "."), err)
		}
		if err := validatePathDoesNotTraverseUnionInEnv(opName, env, path); err != nil {
			return nil, false, err
		}
		name := uniqueColumnName(pathToColumnName(path), cols)
		cols = append(cols, name)
		projections[i] = logicalPathBinding{name: name, path: clonePath(path), schema: bound.typ}
		if len(path) != 1 {
			topLevelOnly = false
		}
	}
	return projections, topLevelOnly, nil
}

func logicalProjectionOutputEnv(projections []logicalPathBinding, raw bool) schemaEnv {
	cols := make([]string, len(projections))
	schemas := make([]*table.TypeDescriptor, len(projections))
	for i, projection := range projections {
		cols[i] = projection.name
		schemas[i] = projection.schema
	}
	return schemaEnvFromOwnedColumns(cols, schemas, !raw)
}

func countOutputEnv() schemaEnv {
	return schemaEnvFromOwnedColumns(
		[]string{"count"},
		[]*table.TypeDescriptor{{Kind: table.TypeInt}},
		true,
	)
}

func describeOutputEnv() schemaEnv {
	return schemaEnvFromOwnedColumns(
		[]string{"column", "type", "row_count", "schema"},
		[]*table.TypeDescriptor{
			{Kind: table.TypeString},
			{Kind: table.TypeString},
			{Kind: table.TypeInt},
			{Kind: table.TypeString},
		},
		true,
	)
}

func planLogicalJoin(o *ast.JoinOp, left schemaEnv, load LoadFunc) (logicalJoin, error) {
	if load == nil {
		return logicalJoin{}, fmt.Errorf("join: loader not configured")
	}
	if o.Filename == "-" {
		return logicalJoin{}, fmt.Errorf("join: stdin is not supported as join source")
	}

	right, err := load(o.Filename, o.Load)
	if err != nil {
		return logicalJoin{}, fmt.Errorf("join: load %q: %w", o.Filename, err)
	}

	rightEnv := schemaEnvFromTable(right)
	leftKeys, rightKeys, err := resolveLogicalJoinKeys(o.Keys, left, rightEnv)
	if err != nil {
		return logicalJoin{}, err
	}

	outCols, leftKeyOutIdx, rightColMap := buildLogicalJoinSchema(left.columns, rightEnv.columns, leftKeys, rightKeys, o.Filename)
	outSchemas, err := buildLogicalJoinOutputSchemas(left, rightEnv, leftKeys, rightKeys, leftKeyOutIdx, rightColMap, len(outCols), o.Kind)
	if err != nil {
		return logicalJoin{}, err
	}

	return logicalJoin{
		logicalBase: logicalBaseFromEnv(schemaEnvFromOwnedColumns(outCols, outSchemas, false)),
		kind:        o.Kind,
		filename:    o.Filename,
		right:       right,
		leftKeys:    leftKeys,
		rightKeys:   rightKeys,
	}, nil
}

func resolveLogicalJoinKeys(keys []ast.JoinKey, left, right schemaEnv) ([]logicalPathBinding, []logicalPathBinding, error) {
	leftKeys := make([]logicalPathBinding, len(keys))
	rightKeys := make([]logicalPathBinding, len(keys))
	for i, k := range keys {
		lk, err := resolveLogicalJoinKeySide(k.Left, left, "left")
		if err != nil {
			return nil, nil, fmt.Errorf("join: %w", err)
		}
		rk, err := resolveLogicalJoinKeySide(k.Right, right, "right")
		if err != nil {
			return nil, nil, fmt.Errorf("join: %w", err)
		}
		leftKeys[i] = lk
		rightKeys[i] = rk
	}
	return leftKeys, rightKeys, nil
}

func resolveLogicalJoinKeySide(path []string, env schemaEnv, side string) (logicalPathBinding, error) {
	if env.colIndex(path[0]) < 0 {
		return logicalPathBinding{}, fmt.Errorf("%s join key column %q not found", side, path[0])
	}
	bound, err := bindColumnPathLogicalInEnv(env, path)
	if err != nil {
		return logicalPathBinding{}, fmt.Errorf("%s join key %q: %w", side, strings.Join(path, "."), err)
	}
	return logicalPathBinding{
		name:   pathToColumnName(path),
		path:   clonePath(path),
		schema: bound.typ,
	}, nil
}

func buildLogicalJoinSchema(leftCols, rightCols []string, leftKeys, rightKeys []logicalPathBinding, filename string) ([]string, []int, map[int]int) {
	rightKeyDrop := make(map[string]bool)
	for _, rk := range rightKeys {
		if len(rk.path) == 1 {
			rightKeyDrop[rk.path[0]] = true
		}
	}

	prefix := joinBasename(filename)
	outCols := append([]string(nil), leftCols...)

	leftKeyOutIdx := make([]int, len(leftKeys))
	for i, lk := range leftKeys {
		if len(lk.path) == 1 {
			leftKeyOutIdx[i] = indexOfColumn(leftCols, lk.path[0])
			continue
		}
		name := uniqueColumnName(lk.name, outCols)
		leftKeyOutIdx[i] = len(outCols)
		outCols = append(outCols, name)
	}

	taken := make(map[string]bool, len(outCols)+len(rightCols))
	for _, c := range outCols {
		taken[c] = true
	}
	rightColMap := make(map[int]int)
	for i, col := range rightCols {
		if rightKeyDrop[col] {
			continue
		}
		name := col
		if taken[name] {
			name = uniqueColumnName(prefix+"_"+col, outCols)
		}
		taken[name] = true
		rightColMap[len(outCols)] = i
		outCols = append(outCols, name)
	}
	return outCols, leftKeyOutIdx, rightColMap
}

func buildLogicalJoinOutputSchemas(left, right schemaEnv, leftKeys, rightKeys []logicalPathBinding, leftKeyOutIdx []int, rightColMap map[int]int, outLen int, joinKind string) ([]*table.TypeDescriptor, error) {
	schemas := make([]*table.TypeDescriptor, outLen)

	keyOutIdx := make(map[int]bool, len(leftKeyOutIdx))
	for _, outIdx := range leftKeyOutIdx {
		keyOutIdx[outIdx] = true
	}

	for i := range left.columns {
		schema := left.rawSchema(i)
		if (joinKind == "right" || joinKind == "full") && !keyOutIdx[i] {
			schema = table.WithNullable(schema)
		}
		schemas[i] = schema
	}

	for i := range leftKeys {
		outIdx := leftKeyOutIdx[i]
		leftSchema := leftKeys[i].schema
		rightSchema := rightKeys[i].schema
		if err := validateLogicalJoinKeySchemas(leftKeys[i], rightKeys[i]); err != nil {
			return nil, err
		}
		merged, err := table.UnifyStrict(leftSchema, rightSchema)
		if err != nil {
			return nil, logicalJoinKeyTypeError(leftKeys[i], rightKeys[i])
		}
		if outIdx >= 0 && outIdx < len(schemas) {
			schemas[outIdx] = merged
		}
	}

	for outIdx, rightIdx := range rightColMap {
		if outIdx >= 0 && outIdx < len(schemas) {
			schema := right.rawSchema(rightIdx)
			if joinKind == "left" || joinKind == "full" {
				schema = table.WithNullable(schema)
			}
			schemas[outIdx] = schema
		}
	}

	for _, schema := range schemas {
		if schema == nil {
			return nil, fmt.Errorf("join: planned output schema is incomplete")
		}
	}
	return schemas, nil
}

func validateLogicalJoinKeySchemas(left, right logicalPathBinding) error {
	if schemaContainsMixed(left.schema) || schemaContainsMixed(right.schema) {
		return fmt.Errorf("join: key type mismatch for %s and %s: mixed key schemas are not supported",
			strings.Join(left.path, "."), strings.Join(right.path, "."))
	}
	if !table.EquivalentSchema(joinKeyComparableSchema(left.schema), joinKeyComparableSchema(right.schema)) {
		return logicalJoinKeyTypeError(left, right)
	}
	return nil
}

func logicalJoinKeyTypeError(left, right logicalPathBinding) error {
	return fmt.Errorf("join: key type mismatch for %s and %s: %s vs %s",
		strings.Join(left.path, "."),
		strings.Join(right.path, "."),
		table.Render(left.schema),
		table.Render(right.schema),
	)
}

func optimizeLogicalPipelineInto(plan *logicalPipeline, out *optimizedLogicalPipeline) error {
	if plan == nil {
		return fmt.Errorf("optimize logical pipeline: nil plan")
	}
	if out == nil {
		return fmt.Errorf("optimize logical pipeline: nil output")
	}
	// The no-op optimizer creates the optimized ADT wrapper without copying the
	// immutable logical facts. Keeping this allocation-free matters for cheap
	// per-call CLI pipelines; real rewrites must allocate replacement nodes.
	*out = optimizedLogicalPipeline{
		Ops:          plan.Ops,
		InputEnv:     plan.InputEnv,
		InputSchema:  plan.InputSchema,
		OutputSchema: plan.OutputSchema,
	}
	return nil
}

func planPhysicalPipelineInto(plan *optimizedLogicalPipeline, out *physicalPipeline) error {
	if plan == nil {
		return fmt.Errorf("physical plan: nil optimized logical plan")
	}
	if out == nil {
		return fmt.Errorf("physical plan: nil output")
	}
	current := plan.InputEnv
	if current.columns == nil {
		current = schemaEnvFromSchema(plan.InputSchema)
	}
	ops := make([]plannedOp, 0, len(plan.Ops))
	output := plan.InputSchema

	for i, op := range plan.Ops {
		next, err := planPhysicalOp(current, op)
		if err != nil {
			return err
		}
		ops = append(ops, next)
		output = next.OutputSchema()
		if i+1 < len(plan.Ops) {
			current = logicalOutputEnv(op)
		}
	}

	*out = physicalPipeline{
		Ops:          ops,
		InputSchema:  plan.InputSchema,
		OutputSchema: output,
	}
	return nil
}

func planPhysicalOp(input schemaEnv, op logicalOp) (plannedOp, error) {
	switch o := op.(type) {
	case logicalHead:
		return plannedHead{plannedBase: plannedBase{output: o.OutputSchema()}, n: o.n}, nil
	case logicalTail:
		return plannedTail{plannedBase: plannedBase{output: o.OutputSchema()}, n: o.n}, nil
	case logicalFilter:
		expr, err := physicalizeTypedExpr(o.expr, input)
		if err != nil {
			return nil, fmt.Errorf("filter: %w", err)
		}
		return plannedFilter{plannedBase: plannedBase{output: o.OutputSchema()}, expr: expr}, nil
	case logicalTransform:
		return planPhysicalTransform(input, o)
	case logicalGroup:
		return planPhysicalGroup(input, o)
	case logicalReduce:
		return planPhysicalReduce(input, o)
	case logicalSort:
		return planPhysicalSort(input, o)
	case logicalSelect:
		projections, err := physicalProjectionPlan(input, o.projections, o.topLevelOnly)
		if err != nil {
			return nil, err
		}
		return plannedSelect{plannedBase: plannedBase{output: o.OutputSchema()}, projections: projections}, nil
	case logicalRename:
		return plannedRename{plannedBase: plannedBase{output: o.OutputSchema()}}, nil
	case logicalRemove:
		indices, err := physicalKeptIndices(input, o.kept)
		if err != nil {
			return nil, err
		}
		return plannedRemove{plannedBase: plannedBase{output: o.OutputSchema()}, indices: indices}, nil
	case logicalDistinct:
		if o.fullRow {
			return plannedDistinct{plannedBase: plannedBase{output: o.OutputSchema()}}, nil
		}
		projections, err := physicalProjectionPlan(input, o.projections, o.topLevelOnly)
		if err != nil {
			return nil, err
		}
		return plannedDistinct{plannedBase: plannedBase{output: o.OutputSchema()}, projections: projections}, nil
	case logicalCount:
		return plannedCount{plannedBase: plannedBase{output: o.OutputSchema()}}, nil
	case logicalDescribe:
		return plannedDescribe{plannedBase: plannedBase{output: o.OutputSchema()}}, nil
	case logicalJoin:
		return planPhysicalJoin(input, o)
	default:
		return nil, fmt.Errorf("unknown optimized logical operation type %T", op)
	}
}

func planPhysicalTransform(input schemaEnv, o logicalTransform) (plannedTransform, error) {
	assignments := make([]plannedTransformAssignment, len(o.assignments))
	for i, assignment := range o.assignments {
		target := schemaColumnIndex(o.OutputSchema(), assignment.name)
		if target < 0 {
			return plannedTransform{}, fmt.Errorf("transform: target %q missing from output schema", assignment.name)
		}
		expr, err := physicalizeTypedExpr(assignment.expr, input)
		if err != nil {
			return plannedTransform{}, fmt.Errorf("transform %q: %w", assignment.name, err)
		}
		assignments[i] = plannedTransformAssignment{name: assignment.name, target: target, expr: expr}
	}
	return plannedTransform{plannedBase: plannedBase{output: o.OutputSchema()}, assignments: assignments}, nil
}

func planPhysicalGroup(input schemaEnv, o logicalGroup) (plannedGroup, error) {
	keys := make([]plannedGroupKey, len(o.keys))
	for i, key := range o.keys {
		bound, err := bindColumnPathInEnv(input, key.path)
		if err != nil {
			return plannedGroup{}, fmt.Errorf("group %q: %w", strings.Join(key.path, "."), err)
		}
		keys[i] = plannedGroupKey{column: *bound}
	}
	return plannedGroup{plannedBase: plannedBase{output: o.OutputSchema()}, keys: keys}, nil
}

func planPhysicalReduce(input schemaEnv, o logicalReduce) (plannedReduce, error) {
	nestedIdx := input.colIndex(o.nestedName)
	if nestedIdx < 0 {
		return plannedReduce{}, fmt.Errorf("reduce: nested column %q not found (did you forget to group first?)", o.nestedName)
	}
	nestedEnv, err := envForRecordSchema(o.nestedSchema)
	if err != nil {
		return plannedReduce{}, err
	}
	assignments := make([]plannedReduceAssignment, len(o.assignments))
	for i, assignment := range o.assignments {
		target := schemaColumnIndex(o.OutputSchema(), assignment.name)
		if target < 0 {
			return plannedReduce{}, fmt.Errorf("reduce: target %q missing from output schema", assignment.name)
		}
		expr, err := physicalizeTypedExpr(assignment.expr, nestedEnv)
		if err != nil {
			return plannedReduce{}, fmt.Errorf("reduce %q: %w", assignment.name, err)
		}
		assignments[i] = plannedReduceAssignment{name: assignment.name, target: target, expr: expr}
	}
	return plannedReduce{
		plannedBase:  plannedBase{output: o.OutputSchema()},
		nestedName:   o.nestedName,
		nestedIndex:  nestedIdx,
		nestedSchema: o.nestedSchema,
		assignments:  assignments,
	}, nil
}

func planPhysicalSort(input schemaEnv, o logicalSort) (plannedSort, error) {
	keys := make([]plannedSortKey, len(o.keys))
	for i, key := range o.keys {
		bound, err := bindColumnPathInEnv(input, key.path)
		if err != nil {
			return plannedSort{}, fmt.Errorf("sort %q: %w", strings.Join(key.path, "."), err)
		}
		keys[i] = plannedSortKey{column: *bound, desc: key.desc}
	}
	return plannedSort{plannedBase: plannedBase{output: o.OutputSchema()}, keys: keys}, nil
}

func physicalProjectionPlan(input schemaEnv, projections []logicalPathBinding, topLevelOnly bool) (*projectionPlan, error) {
	plan := &projectionPlan{
		cols:        make([]string, len(projections)),
		schemas:     make([]*table.TypeDescriptor, len(projections)),
		projections: make([]plannedProjection, len(projections)),
	}
	if topLevelOnly {
		plan.topLevelIdx = make([]int, len(projections))
	}
	for i, projection := range projections {
		bound, err := bindColumnPathInEnv(input, projection.path)
		if err != nil {
			return nil, err
		}
		plan.cols[i] = projection.name
		plan.schemas[i] = projection.schema
		plan.projections[i] = plannedProjection{column: *bound}
		if topLevelOnly {
			plan.topLevelIdx[i] = bound.topIndex
		}
	}
	return plan, nil
}

func physicalKeptIndices(input schemaEnv, kept []string) ([]int, error) {
	indices := make([]int, len(kept))
	for i, col := range kept {
		idx := input.colIndex(col)
		if idx < 0 {
			return nil, fmt.Errorf("remove: kept column %q not found", col)
		}
		indices[i] = idx
	}
	return indices, nil
}

func planPhysicalJoin(input schemaEnv, o logicalJoin) (plannedJoin, error) {
	rightEnv := schemaEnvFromTable(o.right)
	leftKeys, rightKeys, err := physicalizeJoinKeys(input, rightEnv, o.leftKeys, o.rightKeys)
	if err != nil {
		return plannedJoin{}, err
	}
	// Recompute the output map from the chosen physical input schemas instead
	// of trusting cached logical metadata. Joins are not a hot planning path, and
	// this keeps physical executor indexes derived after the optimizer boundary.
	outCols, leftKeyOutIdx, rightColMap := buildLogicalJoinSchema(input.columns, rightEnv.columns, o.leftKeys, o.rightKeys, o.filename)
	if err := validatePhysicalJoinOutputColumns(o.OutputSchema(), outCols); err != nil {
		return plannedJoin{}, err
	}
	return plannedJoin{
		plannedBase:   plannedBase{output: o.OutputSchema()},
		kind:          o.kind,
		right:         o.right,
		leftKeys:      leftKeys,
		rightKeys:     rightKeys,
		leftKeyOutIdx: leftKeyOutIdx,
		rightColMap:   rightColMap,
	}, nil
}

func validatePhysicalJoinOutputColumns(schema table.Schema, cols []string) error {
	if len(schema.Columns) != len(cols) {
		return fmt.Errorf("join: physical output column count changed from %d to %d", len(schema.Columns), len(cols))
	}
	for i, col := range cols {
		if schema.Columns[i].Name != col {
			return fmt.Errorf("join: physical output column %d changed from %q to %q", i, schema.Columns[i].Name, col)
		}
	}
	return nil
}

func physicalizeJoinKeys(left, right schemaEnv, leftKeys, rightKeys []logicalPathBinding) ([]resolvedJoinKey, []resolvedJoinKey, error) {
	physicalLeft := make([]resolvedJoinKey, len(leftKeys))
	physicalRight := make([]resolvedJoinKey, len(rightKeys))
	for i := range leftKeys {
		lb, err := bindColumnPathInEnv(left, leftKeys[i].path)
		if err != nil {
			return nil, nil, fmt.Errorf("join: left join key %q: %w", strings.Join(leftKeys[i].path, "."), err)
		}
		rb, err := bindColumnPathInEnv(right, rightKeys[i].path)
		if err != nil {
			return nil, nil, fmt.Errorf("join: right join key %q: %w", strings.Join(rightKeys[i].path, "."), err)
		}
		if !samePlanningSchema(leftKeys[i].schema, lb.typ) {
			return nil, nil, fmt.Errorf("join: left key %q schema changed from %s to %s during physical planning", strings.Join(leftKeys[i].path, "."), table.Render(leftKeys[i].schema), table.Render(lb.typ))
		}
		if !samePlanningSchema(rightKeys[i].schema, rb.typ) {
			return nil, nil, fmt.Errorf("join: right key %q schema changed from %s to %s during physical planning", strings.Join(rightKeys[i].path, "."), table.Render(rightKeys[i].schema), table.Render(rb.typ))
		}
		physicalLeft[i] = resolvedJoinKey{colName: leftKeys[i].name, column: *lb}
		physicalRight[i] = resolvedJoinKey{colName: rightKeys[i].name, column: *rb}
	}
	return physicalLeft, physicalRight, nil
}

func physicalizeTypedExpr(expr logicalTypedExpr, env schemaEnv) (typedExpr, error) {
	switch b := expr.bound.(type) {
	case *logicalBoundLiteral:
		return typedExpr{bound: &boundLiteral{raw: b.raw}, raw: expr.raw, typ: expr.typ}, nil
	case *logicalBoundColumn:
		bound, err := bindColumnPathInEnv(env, b.rawPath)
		if err != nil {
			return typedExpr{}, err
		}
		if !samePlanningSchema(expr.typ, bound.typ) {
			return typedExpr{}, fmt.Errorf("column %q schema changed from %s to %s during physical planning", strings.Join(b.rawPath, "."), table.Render(expr.typ), table.Render(bound.typ))
		}
		return typedExpr{bound: bound, raw: expr.raw, typ: expr.typ}, nil
	case *logicalBoundBinary:
		left, err := physicalizeTypedExprPtr(expr.left, env)
		if err != nil {
			return typedExpr{}, err
		}
		right, err := physicalizeTypedExprPtr(expr.right, env)
		if err != nil {
			return typedExpr{}, err
		}
		return typedExpr{
			bound: &boundBinary{raw: b.raw, left: left.bound, right: right.bound},
			raw:   expr.raw,
			typ:   expr.typ,
			left:  left,
			right: right,
		}, nil
	case *logicalBoundUnary:
		operand, err := physicalizeTypedExprPtr(expr.operand, env)
		if err != nil {
			return typedExpr{}, err
		}
		return typedExpr{
			bound:   &boundUnary{raw: b.raw, operand: operand.bound},
			raw:     expr.raw,
			typ:     expr.typ,
			operand: operand,
		}, nil
	case *logicalBoundCall:
		args := make([]typedExpr, len(expr.args))
		boundArgs := make([]boundExpr, len(expr.args))
		for i := range expr.args {
			arg, err := physicalizeTypedExpr(expr.args[i], env)
			if err != nil {
				return typedExpr{}, err
			}
			args[i] = arg
			boundArgs[i] = arg.bound
		}
		spec, ok := builtinCatalog[b.raw.Name]
		if !ok {
			return typedExpr{}, fmt.Errorf("unknown function %q during physical planning", b.raw.Name)
		}
		return typedExpr{
			bound:    &boundCall{raw: b.raw, args: boundArgs},
			raw:      expr.raw,
			typ:      expr.typ,
			callEval: spec.TypedEval,
			args:     args,
		}, nil
	case *logicalBoundStruct:
		fields := make([]typedStructField, len(expr.fields))
		boundFields := make([]boundStructField, len(expr.fields))
		for i := range expr.fields {
			fieldExpr, err := physicalizeTypedExpr(expr.fields[i].expr, env)
			if err != nil {
				return typedExpr{}, err
			}
			fields[i] = typedStructField{name: expr.fields[i].name, raw: expr.fields[i].raw, expr: fieldExpr}
			boundFields[i] = boundStructField{name: expr.fields[i].name, raw: expr.fields[i].raw, expr: fieldExpr.bound}
		}
		return typedExpr{
			bound:  &boundStruct{raw: b.raw, fields: boundFields},
			raw:    expr.raw,
			typ:    expr.typ,
			fields: fields,
		}, nil
	case *logicalBoundList:
		elements := make([]typedExpr, len(expr.elements))
		boundElements := make([]boundExpr, len(expr.elements))
		for i := range expr.elements {
			elem, err := physicalizeTypedExpr(expr.elements[i], env)
			if err != nil {
				return typedExpr{}, err
			}
			elements[i] = elem
			boundElements[i] = elem.bound
		}
		return typedExpr{
			bound:    &boundList{raw: b.raw, elements: boundElements},
			raw:      expr.raw,
			typ:      expr.typ,
			elements: elements,
		}, nil
	case *logicalBoundIsNull:
		operand, err := physicalizeTypedExprPtr(expr.operand, env)
		if err != nil {
			return typedExpr{}, err
		}
		return typedExpr{
			bound:   &boundIsNull{raw: b.raw, operand: operand.bound},
			raw:     expr.raw,
			typ:     expr.typ,
			operand: operand,
		}, nil
	case *logicalBoundCoerce:
		operand, err := physicalizeTypedExprPtr(expr.operand, env)
		if err != nil {
			return typedExpr{}, err
		}
		return typedExpr{
			bound:    &boundCoerce{},
			raw:      expr.raw,
			typ:      expr.typ,
			operand:  operand,
			coerceTo: expr.coerceTo,
		}, nil
	default:
		return typedExpr{}, fmt.Errorf("unknown logical typed expression binding %T", expr.bound)
	}
}

func physicalizeTypedExprPtr(expr *logicalTypedExpr, env schemaEnv) (*typedExpr, error) {
	if expr == nil {
		return nil, fmt.Errorf("missing typed expression child")
	}
	out, err := physicalizeTypedExpr(*expr, env)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func clonePath(path []string) []string {
	return append([]string(nil), path...)
}

func indexOfColumn(cols []string, name string) int {
	for i, col := range cols {
		if col == name {
			return i
		}
	}
	return -1
}

func schemaColumnIndex(schema table.Schema, name string) int {
	for i, col := range schema.Columns {
		if col.Name == name {
			return i
		}
	}
	return -1
}

func samePlanningSchema(a, b *table.TypeDescriptor) bool {
	if schemaIsNormalized(a) && schemaIsNormalized(b) {
		return sameNormalizedPlanningSchema(a, b)
	}
	return table.Same(a, b)
}

func sameNormalizedPlanningSchema(a, b *table.TypeDescriptor) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	if a.Kind != b.Kind || a.Nullable != b.Nullable {
		return false
	}
	switch a.Kind {
	case table.TypeRecord:
		if len(a.Fields) != len(b.Fields) {
			return false
		}
		for i := range a.Fields {
			if a.Fields[i].Name != b.Fields[i].Name || !sameNormalizedPlanningSchema(a.Fields[i].Type, b.Fields[i].Type) {
				return false
			}
		}
	case table.TypeList:
		return sameNormalizedPlanningSchema(a.Elem, b.Elem)
	case table.TypeUnion:
		if len(a.Branches) != len(b.Branches) {
			return false
		}
		for i := range a.Branches {
			if !sameNormalizedPlanningSchema(a.Branches[i], b.Branches[i]) {
				return false
			}
		}
	}
	return true
}
