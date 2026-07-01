package engine

import (
	"fmt"

	"github.com/razeghi71/dq/table"
)

type columnDemand struct {
	all     bool
	columns map[string]bool
}

func demandAllColumns() columnDemand {
	return columnDemand{all: true}
}

func demandNoColumns() columnDemand {
	return columnDemand{}
}

func (d columnDemand) has(name string) bool {
	if d.all {
		return true
	}
	return d.columns[name]
}

func (d *columnDemand) add(name string) {
	if d.all {
		return
	}
	if d.columns == nil {
		d.columns = make(map[string]bool)
	}
	d.columns[name] = true
}

func (d *columnDemand) addPath(path []string) {
	if len(path) == 0 {
		return
	}
	d.add(path[0])
}

func (d *columnDemand) addExpr(expr logicalTypedExpr) {
	if d.all {
		return
	}
	if d.columns == nil {
		d.columns = make(map[string]bool)
	}
	collectLogicalTypedExprColumns(expr, d.columns)
}

func optimizeDemandDrivenPruning(plan *optimizedLogicalPipeline) error {
	if plan == nil {
		return nil
	}
	if !demandPruningMayRewrite(plan) {
		return nil
	}
	inputs, outputs := logicalOpEnvs(plan.InputEnv, plan.Ops)
	outputDemands := make([]columnDemand, len(plan.Ops))

	demand := demandAllColumns()
	for i := len(plan.Ops) - 1; i >= 0; i-- {
		outputDemands[i] = demand
		demand = requiredInputDemandForLogicalOp(plan.Ops[i], inputs[i], outputs[i], demand)
	}

	current := plan.InputEnv
	if plan.Source != nil && !plan.Source.source.disablePushdown {
		sourceOutput := sourceOutputColumnsForDemand(plan.InputEnv, plan.Source.outputColumns, demand)
		sourceEnv, ok := sourceOutputEnv(plan.Source.source, sourceOutput)
		if !ok {
			return fmt.Errorf("demand pruning: cannot derive source output schema")
		}
		plan.Source.outputColumns = sourceOutput
		plan.InputEnv = sourceEnv
		plan.InputSchema = sourceEnv.schema()
		current = sourceEnv
	}

	rewritten := make([]logicalOp, 0, len(plan.Ops))
	for i, op := range plan.Ops {
		next, nextEnv, keep, err := rewriteLogicalOpForDemand(op, inputs[i], current, outputDemands[i])
		if err != nil {
			return err
		}
		if keep {
			rewritten = append(rewritten, next)
		}
		current = nextEnv
	}
	plan.Ops = rewritten
	plan.OutputSchema = current.schema()
	return nil
}

func demandPruningMayRewrite(plan *optimizedLogicalPipeline) bool {
	if plan.Source != nil && !plan.Source.source.disablePushdown {
		return true
	}
	for _, op := range plan.Ops {
		switch op.(type) {
		case logicalTransform, logicalSelect, logicalRename, logicalRemove, logicalGroupReduce:
			return true
		}
	}
	return false
}

func logicalOpEnvs(input schemaEnv, ops []logicalOp) ([]schemaEnv, []schemaEnv) {
	inputs := make([]schemaEnv, len(ops))
	outputs := make([]schemaEnv, len(ops))
	current := input
	for i, op := range ops {
		inputs[i] = current
		outputs[i] = op.OutputEnv()
		current = outputs[i]
	}
	return inputs, outputs
}

func requiredInputDemandForLogicalOp(op logicalOp, input, output schemaEnv, out columnDemand) columnDemand {
	switch o := op.(type) {
	case logicalHead, logicalTail:
		return demandSameNames(input, out)
	case logicalFilter:
		in := demandSameNames(input, out)
		in.addExpr(o.expr)
		return in
	case logicalTransform:
		return demandForTransformInput(o, input, out)
	case logicalGroupReduce:
		return demandForGroupReduceInput(o, out)
	case logicalGroup, logicalReduce:
		return demandAllColumns()
	case logicalSort:
		in := demandSameNames(input, out)
		for _, key := range o.keys {
			in.addPath(key.path)
		}
		return in
	case logicalSelect:
		in := demandNoColumns()
		for _, projection := range o.projections {
			if out.all || out.has(projection.name) {
				in.addPath(projection.path)
			}
		}
		return in
	case logicalRename:
		return demandForRenameInput(input, output, out)
	case logicalRemove:
		return demandForRemoveInput(o, out)
	case logicalDistinct:
		if o.fullRow {
			return demandAllColumns()
		}
		in := demandNoColumns()
		for _, projection := range o.projections {
			in.addPath(projection.path)
		}
		return in
	case logicalCount:
		return demandNoColumns()
	case logicalDescribe:
		return demandAllColumns()
	case logicalJoin:
		return demandForJoinInput(o, input, out)
	default:
		return demandAllColumns()
	}
}

func demandSameNames(input schemaEnv, out columnDemand) columnDemand {
	if out.all {
		return demandAllColumns()
	}
	in := demandNoColumns()
	for _, col := range input.columns {
		if out.has(col.name) {
			in.add(col.name)
		}
	}
	return in
}

func demandForTransformInput(op logicalTransform, input schemaEnv, out columnDemand) columnDemand {
	if out.all {
		in := demandAllColumns()
		return in
	}
	assignments := logicalAssignmentsByName(op.assignments)
	in := demandNoColumns()
	for _, col := range op.OutputEnv().columns {
		if !out.has(col.name) {
			continue
		}
		if assignment, ok := assignments[col.name]; ok {
			in.addExpr(assignment.expr)
			continue
		}
		if _, ok := input.lookupColumn(col.name); ok {
			in.add(col.name)
		}
	}
	return in
}

func demandForGroupReduceInput(op logicalGroupReduce, out columnDemand) columnDemand {
	if groupReducePayloadDemanded(op, out) {
		return demandAllColumns()
	}
	in := demandNoColumns()
	for _, key := range op.keys {
		in.addPath(key.path)
	}
	for _, assignment := range op.assignments {
		if out.all || out.has(assignment.name) {
			in.addExpr(assignment.expr)
		}
	}
	return in
}

func groupReducePayloadDemanded(op logicalGroupReduce, out columnDemand) bool {
	if logicalAssignmentNameExists(op.assignments, op.nestedName) {
		return false
	}
	return out.all || out.has(op.nestedName)
}

func demandForRenameInput(input, output schemaEnv, out columnDemand) columnDemand {
	if out.all {
		return demandAllColumns()
	}
	in := demandNoColumns()
	for i, col := range output.columns {
		if inputCol, ok := input.columnAt(i); ok && out.has(col.name) {
			in.add(inputCol.column.name)
		}
	}
	return in
}

func demandForRemoveInput(op logicalRemove, out columnDemand) columnDemand {
	if out.all {
		return demandFromColumnList(op.kept)
	}
	in := demandNoColumns()
	for _, col := range op.kept {
		if out.has(col) {
			in.add(col)
		}
	}
	return in
}

func demandForJoinInput(op logicalJoin, input schemaEnv, out columnDemand) columnDemand {
	if len(op.outputSources) == 0 || out.all {
		return demandAllColumns()
	}
	in := demandNoColumns()
	for _, key := range op.leftKeys {
		in.addPath(key.path)
	}
	for _, source := range op.outputSources {
		if !out.has(source.name) {
			continue
		}
		switch source.kind {
		case logicalJoinOutputLeft:
			in.add(source.leftName)
		case logicalJoinOutputKey:
			if source.keyIndex >= 0 && source.keyIndex < len(op.leftKeys) {
				in.addPath(op.leftKeys[source.keyIndex].path)
			}
		}
	}
	return in
}

func demandFromColumnList(cols []string) columnDemand {
	demand := demandNoColumns()
	for _, col := range cols {
		demand.add(col)
	}
	return demand
}

func sourceOutputColumnsForDemand(input schemaEnv, existing table.ColumnSelection, demand columnDemand) table.ColumnSelection {
	if demand.all {
		if existing.IsAll() {
			return table.AllColumns()
		}
		return table.SelectedColumns(input.columnNames()...)
	}
	cols := make([]string, 0, len(input.columns))
	for _, col := range input.columns {
		if demand.has(col.name) {
			cols = append(cols, col.name)
		}
	}
	return table.SelectedColumns(cols...)
}

func rewriteLogicalOpForDemand(op logicalOp, originalInput, currentInput schemaEnv, out columnDemand) (logicalOp, schemaEnv, bool, error) {
	switch o := op.(type) {
	case logicalHead:
		base := logicalBaseFromEnv(currentInput)
		return logicalHead{logicalBase: base, n: o.n}, currentInput, true, nil
	case logicalTail:
		base := logicalBaseFromEnv(currentInput)
		return logicalTail{logicalBase: base, n: o.n}, currentInput, true, nil
	case logicalFilter:
		base := logicalBaseFromEnv(currentInput)
		return logicalFilter{logicalBase: base, expr: o.expr, sourcePushable: o.sourcePushable}, currentInput, true, nil
	case logicalTransform:
		return rewriteLogicalTransformForDemand(o, currentInput, out)
	case logicalGroupReduce:
		return rewriteLogicalGroupReduceForDemand(o, out)
	case logicalGroup:
		return o, o.OutputEnv(), true, nil
	case logicalReduce:
		return o, o.OutputEnv(), true, nil
	case logicalSort:
		base := logicalBaseFromEnv(currentInput)
		return logicalSort{logicalBase: base, keys: o.keys}, currentInput, true, nil
	case logicalSelect:
		return rewriteLogicalSelectForDemand(o, currentInput, out)
	case logicalRename:
		return rewriteLogicalRenameForDemand(o, originalInput, currentInput)
	case logicalRemove:
		return rewriteLogicalRemoveForDemand(o, currentInput)
	case logicalDistinct:
		return rewriteLogicalDistinctForDemand(o, currentInput)
	case logicalCount:
		env := countOutputEnv()
		return logicalCount{logicalBase: logicalBaseFromEnv(env)}, env, true, nil
	case logicalDescribe:
		env := describeOutputEnv()
		return logicalDescribe{logicalBase: logicalBaseFromEnv(env)}, env, true, nil
	case logicalJoin:
		return rewriteLogicalJoinForDemand(o, out)
	default:
		return op, op.OutputEnv(), true, nil
	}
}

func rewriteLogicalTransformForDemand(op logicalTransform, input schemaEnv, out columnDemand) (logicalOp, schemaEnv, bool, error) {
	assignments := make([]logicalAssignment, 0, len(op.assignments))
	for _, assignment := range op.assignments {
		if out.all || out.has(assignment.name) {
			assignments = append(assignments, assignment)
		}
	}
	if len(assignments) == 0 {
		return nil, input, false, nil
	}

	columns := input.cloneColumns()
	for i := range assignments {
		assignment := assignments[i]
		var target schemaEnvColumnRef
		columns, target = upsertEnvColumn(columns, assignment.name)
		schema := finalizePlanningSchema(assignment.expr.typ)
		columns[target.index].raw = schema
	}
	env := schemaEnvFromKnownUniqueColumns(columns)
	return logicalTransform{
		logicalBase: logicalBaseFromEnv(env),
		assignments: assignments,
	}, env, true, nil
}

func rewriteLogicalGroupReduceForDemand(op logicalGroupReduce, out columnDemand) (logicalOp, schemaEnv, bool, error) {
	materializeNested := groupReducePayloadDemanded(op, out)
	assignments := make([]logicalAssignment, 0, len(op.assignments))
	for _, assignment := range op.assignments {
		if out.all || out.has(assignment.name) {
			assignments = append(assignments, assignment)
		}
	}

	original := op.OutputEnv()
	columns := make([]schemaEnvColumn, 0, len(original.columns))
	for _, col := range original.columns {
		if out.all || out.has(col.name) {
			columns = append(columns, col)
		}
	}
	env := schemaEnvFromKnownUniqueColumns(columns)
	return logicalGroupReduce{
		logicalBase:       logicalBaseFromEnv(env),
		keys:              op.keys,
		nestedName:        op.nestedName,
		assignments:       assignments,
		materializeNested: materializeNested,
	}, env, true, nil
}

func rewriteLogicalSelectForDemand(op logicalSelect, input schemaEnv, out columnDemand) (logicalOp, schemaEnv, bool, error) {
	projections := make([]logicalPathBinding, 0, len(op.projections))
	topLevelOnly := true
	for _, projection := range op.projections {
		if out.all || out.has(projection.name) {
			projections = append(projections, projection)
			if len(projection.path) != 1 {
				topLevelOnly = false
			}
		}
	}
	if len(projections) == 0 {
		return nil, input, false, nil
	}
	env := logicalProjectionOutputEnv(projections, topLevelOnly)
	if selectIsIdentityForEnv(projections, input) {
		return nil, input, false, nil
	}
	return logicalSelect{
		logicalBase:  logicalBaseFromEnv(env),
		projections:  projections,
		topLevelOnly: topLevelOnly,
	}, env, true, nil
}

func rewriteLogicalRenameForDemand(op logicalRename, originalInput, currentInput schemaEnv) (logicalOp, schemaEnv, bool, error) {
	originalOutput := op.OutputEnv()
	renamedByInput := make(map[string]string, len(originalInput.columns))
	for i, col := range originalInput.columns {
		if outCol, ok := originalOutput.columnAt(i); ok {
			renamedByInput[col.name] = outCol.column.name
		}
	}
	columns := currentInput.cloneColumns()
	for i, col := range currentInput.columns {
		next, ok := renamedByInput[col.name]
		if !ok {
			next = col.name
		}
		columns[i].name = next
	}
	env := schemaEnvFromKnownUniqueColumns(columns)
	if schemaEnvColumnNamesEqual(env, currentInput) {
		return nil, currentInput, false, nil
	}
	return logicalRename{logicalBase: logicalBaseFromEnv(env)}, env, true, nil
}

func rewriteLogicalRemoveForDemand(op logicalRemove, input schemaEnv) (logicalOp, schemaEnv, bool, error) {
	keptSet := make(map[string]bool, len(op.kept))
	for _, col := range op.kept {
		keptSet[col] = true
	}
	columns := make([]schemaEnvColumn, 0, len(input.columns))
	for _, col := range input.columns {
		if keptSet[col.name] {
			columns = append(columns, col)
		}
	}
	env := schemaEnvFromKnownUniqueColumns(columns)
	if schemaEnvColumnNamesEqual(env, input) {
		return nil, input, false, nil
	}
	return logicalRemove{logicalBase: logicalBaseFromEnv(env), kept: env.columnNames()}, env, true, nil
}

func rewriteLogicalDistinctForDemand(op logicalDistinct, input schemaEnv) (logicalOp, schemaEnv, bool, error) {
	if op.fullRow {
		base := logicalBaseFromEnv(input)
		return logicalDistinct{logicalBase: base, fullRow: true}, input, true, nil
	}
	env := logicalProjectionOutputEnv(op.projections, false)
	return logicalDistinct{
		logicalBase:  logicalBaseFromEnv(env),
		projections:  op.projections,
		topLevelOnly: op.topLevelOnly,
	}, env, true, nil
}

func rewriteLogicalJoinForDemand(op logicalJoin, out columnDemand) (logicalOp, schemaEnv, bool, error) {
	original := op.OutputEnv()
	if len(op.outputSources) != len(original.columns) {
		return nil, schemaEnv{}, false, fmt.Errorf("demand pruning: join output source count mismatch")
	}
	columns := make([]schemaEnvColumn, 0, len(original.columns))
	sources := make([]logicalJoinOutputSource, 0, len(original.columns))
	for i, col := range original.columns {
		if out.all || out.has(col.name) {
			columns = append(columns, col)
			sources = append(sources, op.outputSources[i])
		}
	}
	env := schemaEnvFromKnownUniqueColumns(columns)
	return logicalJoin{
		logicalBase:   logicalBaseFromEnv(env),
		kind:          op.kind,
		filename:      op.filename,
		right:         op.right,
		leftKeys:      op.leftKeys,
		rightKeys:     op.rightKeys,
		outputSources: sources,
	}, env, true, nil
}

func selectIsIdentityForEnv(projections []logicalPathBinding, env schemaEnv) bool {
	if len(projections) != len(env.columns) {
		return false
	}
	for i, projection := range projections {
		name := env.columns[i].name
		if len(projection.path) != 1 || projection.path[0] != name || projection.name != name {
			return false
		}
	}
	return true
}

func logicalAssignmentsByName(assignments []logicalAssignment) map[string]logicalAssignment {
	out := make(map[string]logicalAssignment, len(assignments))
	for _, assignment := range assignments {
		out[assignment.name] = assignment
	}
	return out
}

func logicalAssignmentNameExists(assignments []logicalAssignment, name string) bool {
	for _, assignment := range assignments {
		if assignment.name == name {
			return true
		}
	}
	return false
}
