package engine

import (
	"fmt"

	"github.com/razeghi71/dq/ast"
	"github.com/razeghi71/dq/table"
)

type SourceInfo struct {
	Filename string
	Load     ast.LoadOptions
	Schema   table.Schema
}

type SourcePredicate func(row []table.Value) (bool, error)

type SourceLoadSpec struct {
	ReadColumns   []string
	OutputColumns []string
	Predicate     SourcePredicate
}

type SourceLoadFunc func(filename string, opts ast.LoadOptions, spec SourceLoadSpec) (*table.Table, error)

type logicalSource struct {
	filename string
	load     ast.LoadOptions
	schema   table.Schema
}

type optimizedSource struct {
	source        logicalSource
	outputColumns []string
	predicates    []logicalTypedExpr
}

type physicalSource struct {
	filename string
	load     ast.LoadOptions
	spec     SourceLoadSpec
}

func ExecuteSourceQuery(query *ast.Query, source SourceInfo, loadSource SourceLoadFunc, loadJoin LoadFunc) (*table.Table, error) {
	if loadSource == nil {
		return nil, fmt.Errorf("source loader not configured")
	}
	logical, err := planLogicalQueryWithSource(query, logicalSource{
		filename: source.Filename,
		load:     source.Load,
		schema:   source.Schema,
	}, loadJoin)
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
	if physical.Source == nil {
		return nil, fmt.Errorf("source physical plan missing source")
	}
	input, err := loadSource(physical.Source.filename, physical.Source.load, physical.Source.spec)
	if err != nil {
		return nil, fmt.Errorf("load error: %w", err)
	}
	if err := validateSourceInputSchema(physical.InputSchema, input); err != nil {
		return nil, err
	}
	return executePlannedOps(physical.Ops, input)
}

func planLogicalQueryWithSource(query *ast.Query, source logicalSource, load LoadFunc) (*logicalPipeline, error) {
	input := schemaEnvFromSchema(source.schema)
	pipeline, err := planLogicalPipelineInEnv(input, query.Ops, load)
	if err != nil {
		return nil, err
	}
	sourceCopy := source
	pipeline.Source = &sourceCopy
	return pipeline, nil
}

func optimizedSourceFromLogical(source *logicalSource) *optimizedSource {
	if source == nil {
		return nil
	}
	sourceCopy := *source
	return &optimizedSource{source: sourceCopy}
}

func optimizeSourcePushdown(plan *optimizedLogicalPipeline) {
	if plan == nil || plan.Source == nil {
		return
	}
	source := plan.Source.source
	if len(plan.Ops) == 0 {
		return
	}
	var outputColumns []string
	var predicates []logicalTypedExpr
	consumed := 0

	for consumed < len(plan.Ops) {
		switch op := plan.Ops[consumed].(type) {
		case logicalSelect:
			cols, ok := sourceProjectionColumnsFromLogicalSelect(op)
			if !ok {
				goto done
			}
			outputColumns = cols
			consumed++
		case logicalFilter:
			if !op.sourcePushable {
				goto done
			}
			predicates = append(predicates, op.expr)
			consumed++
		default:
			goto done
		}
	}

done:
	if consumed == 0 {
		return
	}
	sourceEnv, ok := sourceOutputEnv(source, outputColumns)
	if !ok {
		return
	}
	plan.Source.outputColumns = append([]string(nil), outputColumns...)
	plan.Source.predicates = append([]logicalTypedExpr(nil), predicates...)
	plan.Ops = append([]logicalOp(nil), plan.Ops[consumed:]...)
	plan.InputEnv = sourceEnv
	plan.InputSchema = sourceEnv.schema()
}

func physicalSourceFromOptimized(source *optimizedSource) (*physicalSource, error) {
	if source == nil {
		return nil, nil
	}
	readColumns := derivePhysicalSourceReadColumns(source)
	readEnv, ok := sourceOutputEnv(source.source, readColumns)
	if !ok {
		return nil, fmt.Errorf("physical source: cannot derive read schema")
	}
	predicates := make([]typedExpr, len(source.predicates))
	for i, predicate := range source.predicates {
		physical, err := physicalizeTypedExpr(predicate, readEnv)
		if err != nil {
			return nil, fmt.Errorf("physical source predicate: %w", err)
		}
		predicates[i] = physical
	}
	return &physicalSource{
		filename: source.source.filename,
		load:     source.source.load,
		spec: SourceLoadSpec{
			ReadColumns:   append([]string(nil), readColumns...),
			OutputColumns: append([]string(nil), source.outputColumns...),
			Predicate:     compileSourcePredicates(predicates),
		},
	}, nil
}

func validateSourceInputSchema(want table.Schema, got *table.Table) error {
	if got == nil {
		if len(want.Columns) == 0 {
			return nil
		}
		return fmt.Errorf("execute source plan: input schema column count mismatch: planned %d columns, got 0", len(want.Columns))
	}
	env := schemaEnvFromTable(got)
	if len(want.Columns) != len(env.columns) {
		return fmt.Errorf("execute source plan: input schema column count mismatch: planned %d columns, got %d", len(want.Columns), len(env.columns))
	}
	for i := range want.Columns {
		w := want.Columns[i]
		if w.Name != env.columns[i] {
			return fmt.Errorf("execute source plan: input schema column %d mismatch: planned %q, got %q", i, w.Name, env.columns[i])
		}
		if !table.SchemaAssignable(w.Type, env.rawSchema(i), table.AssignExactMode) {
			return fmt.Errorf("execute source plan: input schema for column %q mismatch: planned %s, got %s", w.Name, table.Render(w.Type), table.Render(env.rawSchema(i)))
		}
	}
	return nil
}

func sourceProjectionColumnsFromLogicalSelect(op logicalSelect) ([]string, bool) {
	if !op.topLevelOnly {
		return nil, false
	}
	cols := make([]string, len(op.projections))
	for i, projection := range op.projections {
		if len(projection.path) != 1 || projection.name != projection.path[0] {
			return nil, false
		}
		cols[i] = projection.path[0]
	}
	return cols, true
}

func sourceFilterASTCanPush(expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.LiteralExpr:
		return true
	case *ast.ColumnExpr:
		return len(e.Path) == 1
	case *ast.BinaryExpr:
		switch e.Op {
		case "==", "!=", "<", ">", "<=", ">=", "and", "or":
			return sourceFilterASTCanPush(e.Left) && sourceFilterASTCanPush(e.Right)
		default:
			return false
		}
	case *ast.UnaryExpr:
		return e.Op == "not" && sourceFilterASTCanPush(e.Operand)
	case *ast.IsNullExpr:
		return sourceFilterASTCanPush(e.Operand)
	default:
		return false
	}
}

func sourceOutputEnv(source logicalSource, columns []string) (schemaEnv, bool) {
	env := schemaEnvFromSchema(source.schema)
	if columns == nil {
		return env, true
	}
	schemas := make([]*table.TypeDescriptor, len(columns))
	for i, col := range columns {
		idx := env.colIndex(col)
		if idx < 0 {
			return schemaEnv{}, false
		}
		schemas[i] = env.rawSchema(idx)
		if schemas[i] == nil {
			schemas[i] = env.finalSchema(idx)
		}
	}
	return schemaEnvFromOwnedColumns(append([]string(nil), columns...), schemas, false), true
}

func derivePhysicalSourceReadColumns(source *optimizedSource) []string {
	if source == nil || source.outputColumns == nil {
		return nil
	}
	read := append([]string(nil), source.outputColumns...)
	needed := make(map[string]bool)
	for _, predicate := range source.predicates {
		collectLogicalTypedExprColumns(predicate, needed)
	}
	sourceEnv := schemaEnvFromSchema(source.source.schema)
	for _, col := range sourceEnv.columns {
		if needed[col] && !containsColumnName(read, col) {
			read = append(read, col)
		}
	}
	return read
}

func collectLogicalTypedExprColumns(expr logicalTypedExpr, out map[string]bool) {
	switch b := expr.bound.(type) {
	case *logicalBoundColumn:
		if len(b.rawPath) > 0 {
			out[b.rawPath[0]] = true
		}
	}
	if expr.left != nil {
		collectLogicalTypedExprColumns(*expr.left, out)
	}
	if expr.right != nil {
		collectLogicalTypedExprColumns(*expr.right, out)
	}
	if expr.operand != nil {
		collectLogicalTypedExprColumns(*expr.operand, out)
	}
	for i := range expr.args {
		collectLogicalTypedExprColumns(expr.args[i], out)
	}
	for i := range expr.fields {
		collectLogicalTypedExprColumns(expr.fields[i].expr, out)
	}
	for i := range expr.elements {
		collectLogicalTypedExprColumns(expr.elements[i], out)
	}
}

func compileSourcePredicates(predicates []typedExpr) SourcePredicate {
	if len(predicates) == 0 {
		return nil
	}
	return func(row []table.Value) (bool, error) {
		ctx := EvalContext{RowValues: row}
		for _, predicate := range predicates {
			v, err := evalTypedExpression(predicate, &ctx)
			if err != nil {
				return false, fmt.Errorf("filter: %w", err)
			}
			if !v.IsExplicitTrue() {
				return false, nil
			}
		}
		return true, nil
	}
}
