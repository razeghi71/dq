package engine

import (
	"fmt"

	"github.com/razeghi71/dq/ast"
	"github.com/razeghi71/dq/rowstream"
	"github.com/razeghi71/dq/table"
)

type SourceInfo struct {
	Filename        string
	Load            ast.LoadOptions
	Schema          table.Schema
	DisablePushdown bool
}

type SourcePredicate func(row []table.Value) (bool, error)

type SourceLoadSpec struct {
	ReadColumns   table.ColumnSelection
	OutputColumns table.ColumnSelection
	Predicate     SourcePredicate
}

type SourceLoadFunc func(filename string, opts ast.LoadOptions, spec SourceLoadSpec) (*table.Table, error)

type SourceStreamFunc func(filename string, opts ast.LoadOptions, spec SourceLoadSpec) (rowstream.Stream, error)

type logicalSource struct {
	filename        string
	load            ast.LoadOptions
	schema          table.Schema
	disablePushdown bool
}

type optimizedSource struct {
	source        logicalSource
	outputColumns table.ColumnSelection
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
	physical, err := planPhysicalSourceQuery(query, source, loadJoin)
	if err != nil {
		return nil, err
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

func ExecuteSourceStreamQuery(query *ast.Query, source SourceInfo, streamSource SourceStreamFunc, loadJoin LoadFunc) (*table.Table, error) {
	if streamSource == nil {
		return nil, fmt.Errorf("source stream loader not configured")
	}
	physical, err := planPhysicalSourceQuery(query, source, loadJoin)
	if err != nil {
		return nil, err
	}
	return executePhysicalSourceStreamQuery(physical, streamSource)
}

func ExecuteSourceAdaptiveQuery(query *ast.Query, source SourceInfo, streamSource SourceStreamFunc, loadSource SourceLoadFunc, loadJoin LoadFunc) (*table.Table, error) {
	physical, err := planPhysicalSourceQuery(query, source, loadJoin)
	if err != nil {
		return nil, err
	}
	if shouldLoadSourceMaterializedForStreaming(physical.Ops) {
		if loadSource == nil {
			return nil, fmt.Errorf("source loader not configured")
		}
		input, err := loadSource(physical.Source.filename, physical.Source.load, physical.Source.spec)
		if err != nil {
			return nil, fmt.Errorf("load error: %w", err)
		}
		if err := validateSourceInputSchema(physical.InputSchema, input); err != nil {
			return nil, err
		}
		return executePlannedOpsStreaming(physical.Ops, rowstream.FromTable(input))
	}
	if streamSource == nil {
		return nil, fmt.Errorf("source stream loader not configured")
	}
	return executePhysicalSourceStreamQuery(physical, streamSource)
}

func planPhysicalSourceQuery(query *ast.Query, source SourceInfo, loadJoin LoadFunc) (*physicalPipeline, error) {
	logical, err := planLogicalQueryWithSource(query, logicalSource{
		filename:        source.Filename,
		load:            source.Load,
		schema:          source.Schema,
		disablePushdown: source.DisablePushdown,
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
	return &physical, nil
}

func executePhysicalSourceStreamQuery(physical *physicalPipeline, streamSource SourceStreamFunc) (*table.Table, error) {
	if physical == nil || physical.Source == nil {
		return nil, fmt.Errorf("source physical plan missing source")
	}
	input, err := streamSource(physical.Source.filename, physical.Source.load, physical.Source.spec)
	if err != nil {
		return nil, fmt.Errorf("load error: %w", err)
	}
	if err := validateSourceStreamSchema(physical.InputSchema, input); err != nil {
		_ = input.Close()
		return nil, err
	}
	return executePlannedOpsStreaming(physical.Ops, sourceErrorStream{Stream: input})
}

func shouldLoadSourceMaterializedForStreaming(ops []plannedOp) bool {
	for i, op := range ops {
		switch op.executionTraits().class {
		case plannedExecutionRowSpan:
			span := op.(plannedRowSpan)
			load, decided := sourceMaterializedDecisionAfterRowLocal(plannedRowLocalInfo{
				dropsRows:         span.dropsRows,
				parallelCandidate: span.parallelCandidate,
			}, nextPlannedOp(ops, i+1))
			if decided {
				return load
			}
		case plannedExecutionRowLocal:
			info, _ := plannedRowLocalInfoForOp(op)
			load, decided := sourceMaterializedDecisionAfterRowLocal(info, nextPlannedOp(ops, i+1))
			if decided {
				return load
			}
		case plannedExecutionEarlyStop, plannedExecutionStreamingFold:
			return false
		case plannedExecutionMaterializedBoundary:
			return true
		}
	}
	return false
}

func sourceMaterializedDecisionAfterRowLocal(info plannedRowLocalInfo, next plannedOp) (bool, bool) {
	if info.dropsRows || next == nil {
		return false, true
	}
	switch next.executionTraits().class {
	case plannedExecutionEarlyStop, plannedExecutionStreamingFold:
		return false, true
	default:
		if isMaterializedStreamingBoundary(next) {
			return true, true
		}
		return false, false
	}
}

type sourceErrorStream struct {
	rowstream.Stream
}

func (s sourceErrorStream) Next() (rowstream.Row, bool, error) {
	row, ok, err := s.Stream.Next()
	if err != nil {
		return nil, false, fmt.Errorf("load error: %w", err)
	}
	return row, ok, nil
}

func planLogicalQueryWithSource(query *ast.Query, source logicalSource, load LoadFunc) (*logicalPipeline, error) {
	input, err := schemaEnvFromSchema(source.schema)
	if err != nil {
		return nil, fmt.Errorf("source schema: %w", err)
	}
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
	if plan.Source.source.disablePushdown {
		return
	}
	source := plan.Source.source
	if len(plan.Ops) == 0 {
		return
	}
	outputColumns := table.AllColumns()
	var predicates []logicalTypedExpr
	consumed := 0

	for consumed < len(plan.Ops) {
		switch op := plan.Ops[consumed].(type) {
		case logicalSelect:
			cols, ok := sourceProjectionColumnsFromLogicalSelect(op)
			if !ok {
				goto done
			}
			outputColumns = table.SelectedColumns(cols...)
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
	plan.Source.outputColumns = outputColumns
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
			ReadColumns:   readColumns,
			OutputColumns: source.outputColumns,
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
	env, err := schemaEnvFromTable(got)
	if err != nil {
		return err
	}
	if len(want.Columns) != len(env.columns) {
		return fmt.Errorf("execute source plan: input schema column count mismatch: planned %d columns, got %d", len(want.Columns), len(env.columns))
	}
	for i := range want.Columns {
		w := want.Columns[i]
		gotCol := env.columns[i]
		if w.Name != gotCol.name {
			return fmt.Errorf("execute source plan: input schema column %d mismatch: planned %q, got %q", i, w.Name, gotCol.name)
		}
		if !table.SchemaAssignable(w.Type, gotCol.raw, table.AssignExactMode) {
			return fmt.Errorf("execute source plan: input schema for column %q mismatch: planned %s, got %s", w.Name, table.Render(w.Type), table.Render(gotCol.raw))
		}
	}
	return nil
}

func validateSourceStreamSchema(want table.Schema, got rowstream.Stream) error {
	if got == nil {
		if len(want.Columns) == 0 {
			return nil
		}
		return fmt.Errorf("execute source plan: input schema column count mismatch: planned %d columns, got 0", len(want.Columns))
	}
	schema := got.Schema()
	if len(want.Columns) != len(schema.Columns) {
		return fmt.Errorf("execute source plan: input schema column count mismatch: planned %d columns, got %d", len(want.Columns), len(schema.Columns))
	}
	for i := range want.Columns {
		w := want.Columns[i]
		g := schema.Columns[i]
		if w.Name != g.Name {
			return fmt.Errorf("execute source plan: input schema column %d mismatch: planned %q, got %q", i, w.Name, g.Name)
		}
		if !table.SchemaAssignable(w.Type, g.Type, table.AssignExactMode) {
			return fmt.Errorf("execute source plan: input schema for column %q mismatch: planned %s, got %s", w.Name, table.Render(w.Type), table.Render(g.Type))
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

func sourceOutputEnv(source logicalSource, selection table.ColumnSelection) (schemaEnv, bool) {
	env, err := schemaEnvFromSchema(source.schema)
	if err != nil {
		return schemaEnv{}, false
	}
	if selection.IsAll() {
		return env, true
	}
	selected := selection.Names()
	columns := make([]schemaEnvColumn, len(selected))
	for i, name := range selected {
		col, ok := env.lookupColumn(name)
		if !ok {
			return schemaEnv{}, false
		}
		columns[i] = schemaEnvColumn{name: name, raw: col.column.planningSchema()}
	}
	out, err := newSchemaEnv(columns)
	if err != nil {
		return schemaEnv{}, false
	}
	return out, true
}

func derivePhysicalSourceReadColumns(source *optimizedSource) table.ColumnSelection {
	if source == nil || source.outputColumns.IsAll() {
		return table.AllColumns()
	}
	read := source.outputColumns.Names()
	needed := make(map[string]bool)
	for _, predicate := range source.predicates {
		collectLogicalTypedExprColumns(predicate, needed)
	}
	sourceEnv := mustSchemaEnvFromSchema(source.source.schema)
	for _, col := range sourceEnv.columns {
		if needed[col.name] && !containsColumnName(read, col.name) {
			read = append(read, col.name)
		}
	}
	return table.SelectedColumns(read...)
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
