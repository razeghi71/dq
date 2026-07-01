package engine

import (
	"fmt"

	"github.com/razeghi71/dq/ast"
	"github.com/razeghi71/dq/table"
)

// pipelinePlan is a test-only compatibility wrapper around physicalPipeline for
// older planner tests that execute a prebuilt plan against varied input tables.
type pipelinePlan struct {
	Ops          []plannedOp
	InputSchema  table.Schema
	OutputSchema table.Schema
}

func planLogicalPipeline(input table.Schema, ops []ast.Op, load LoadFunc) (*logicalPipeline, error) {
	env, err := schemaEnvFromSchema(input)
	if err != nil {
		return nil, err
	}
	return planLogicalPipelineInEnv(env, ops, newLoadFuncJoinSourceProvider(load))
}

func optimizeLogicalPipeline(plan *logicalPipeline) (*optimizedLogicalPipeline, error) {
	var out optimizedLogicalPipeline
	if err := optimizeLogicalPipelineInto(plan, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func planPhysicalPipeline(plan *optimizedLogicalPipeline) (*physicalPipeline, error) {
	var out physicalPipeline
	if err := planPhysicalPipelineInto(plan, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func logicalBaseFromTestSchema(schema table.Schema) logicalBase {
	return logicalBaseFromEnv(mustSchemaEnvFromSchema(schema))
}

func plannedBaseFromTestSchema(schema table.Schema) plannedBase {
	return plannedBaseFromEnv(mustSchemaEnvFromSchema(schema))
}

func executePhysicalPipeline(plan *physicalPipeline, input *table.Table) (*table.Table, error) {
	if plan == nil {
		return nil, fmt.Errorf("execute physical pipeline: nil plan")
	}
	if err := validatePlanInputTableSchema(plan.InputSchema, input); err != nil {
		return nil, err
	}
	return executePlannedOps(plan.Ops, input)
}

func rawSchemaFromColumns(cols []string, schemas []*table.TypeDescriptor) table.Schema {
	out := table.Schema{Columns: make([]table.SchemaColumn, len(cols))}
	for i, name := range cols {
		var typ *table.TypeDescriptor
		if i < len(schemas) {
			typ = normalizePlanningSchema(schemas[i])
		}
		out.Columns[i] = table.SchemaColumn{Name: name, Type: typ}
	}
	return out
}

// executePlan executes a pipeline plan against rows.
func executePlan(plan *pipelinePlan, input *table.Table) (*table.Table, error) {
	if err := validatePlanInputTableSchema(plan.InputSchema, input); err != nil {
		return nil, err
	}
	return executePlannedPipeline(plan, input)
}

func executePlannedPipeline(plan *pipelinePlan, input *table.Table) (*table.Table, error) {
	return executePlannedOps(plan.Ops, input)
}

func validatePlanInputTableSchema(want table.Schema, got *table.Table) error {
	if got == nil {
		if len(want.Columns) == 0 {
			return nil
		}
		return fmt.Errorf("execute plan: input schema column count mismatch: planned %d columns, got 0", len(want.Columns))
	}
	env, err := schemaEnvFromTable(got)
	if err != nil {
		return err
	}
	if len(want.Columns) != len(env.columns) {
		return fmt.Errorf("execute plan: input schema column count mismatch: planned %d columns, got %d", len(want.Columns), len(env.columns))
	}
	for i := range want.Columns {
		w := want.Columns[i]
		gotCol := env.columns[i]
		if w.Name != gotCol.name {
			return fmt.Errorf("execute plan: input schema column %d mismatch: planned %q, got %q", i, w.Name, gotCol.name)
		}
		gotSchema := gotCol.raw
		if !table.Same(w.Type, gotSchema) {
			return fmt.Errorf("execute plan: input schema for column %q mismatch: planned %s, got %s", w.Name, table.Render(w.Type), table.Render(gotSchema))
		}
	}
	return nil
}

func planPhysicalPipelineForTest(input table.Schema, ops []ast.Op) (*pipelinePlan, error) {
	logical, err := planLogicalPipeline(input, ops, nil)
	if err != nil {
		return nil, err
	}
	optimized, err := optimizeLogicalPipeline(logical)
	if err != nil {
		return nil, err
	}
	physical, err := planPhysicalPipeline(optimized)
	if err != nil {
		return nil, err
	}
	return &pipelinePlan{Ops: physical.Ops, InputSchema: physical.InputSchema, OutputSchema: physical.OutputSchema}, nil
}

func planPhysicalPipelineFromTableForTest(input *table.Table, ops []ast.Op) (*pipelinePlan, error) {
	return planPhysicalPipelineFromTableWithLoadForTest(input, ops, nil)
}

func planPhysicalPipelineFromTableWithLoadForTest(input *table.Table, ops []ast.Op, load LoadFunc) (*pipelinePlan, error) {
	joinSources := newLoadFuncJoinSourceProvider(load)
	logical, err := planLogicalPipelineFromTableWithJoinSources(input, ops, joinSources)
	if err != nil {
		return nil, err
	}
	optimized, err := optimizeLogicalPipeline(logical)
	if err != nil {
		return nil, err
	}
	physical, err := planPhysicalPipeline(optimized)
	if err != nil {
		return nil, err
	}
	return &pipelinePlan{Ops: physical.Ops, InputSchema: physical.InputSchema, OutputSchema: physical.OutputSchema}, nil
}

func planPhysicalFilterExprForTest(expr ast.Expr, t *table.Table) (typedExpr, error) {
	env, err := schemaEnvFromTable(t)
	if err != nil {
		return typedExpr{}, err
	}
	typed, err := planLogicalFilterExprInEnv(expr, env)
	if err != nil {
		return typedExpr{}, err
	}
	return physicalizeTypedExpr(typed, env)
}

func planPhysicalTransformExprForTest(expr ast.Expr, t *table.Table) (typedExpr, error) {
	env, err := schemaEnvFromTable(t)
	if err != nil {
		return typedExpr{}, err
	}
	typed, err := planLogicalTransformExprInEnv(expr, env)
	if err != nil {
		return typedExpr{}, err
	}
	return physicalizeTypedExpr(typed, env)
}

func planPhysicalReduceExprForTest(expr ast.Expr, nestedSchema *table.TypeDescriptor) (typedExpr, error) {
	typed, err := planLogicalReduceExpr(expr, nestedSchema)
	if err != nil {
		return typedExpr{}, err
	}
	env, err := envForRecordSchema(nestedSchema)
	if err != nil {
		return typedExpr{}, err
	}
	return physicalizeTypedExpr(typed, env)
}
