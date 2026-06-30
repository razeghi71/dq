package engine

import (
	"reflect"
	"strings"
	"testing"

	"github.com/razeghi71/dq/ast"
	"github.com/razeghi71/dq/parser"
	"github.com/razeghi71/dq/table"
)

func parseOptimizerBoundaryOpsTDD(t *testing.T, pipeline string) []ast.Op {
	t.Helper()
	q, err := parser.Parse("test.csv | " + pipeline)
	if err != nil {
		t.Fatalf("parse %q: %v", pipeline, err)
	}
	return q.Ops
}

func executeThroughOptimizerBoundaryTDD(t *testing.T, input *table.Table, pipeline string, load LoadFunc) (*table.Table, error) {
	t.Helper()
	logical, err := planLogicalPipeline(input.Schema(), parseOptimizerBoundaryOpsTDD(t, pipeline), load)
	if err != nil {
		return nil, err
	}

	optimized, err := optimizeLogicalPipeline(logical)
	if err != nil {
		return nil, err
	}
	if reflect.TypeOf(logical) == reflect.TypeOf(optimized) {
		t.Fatalf("optimizer must return a distinct optimized logical IR type, got %T", optimized)
	}

	physical, err := planPhysicalPipeline(optimized)
	if err != nil {
		return nil, err
	}

	return executePhysicalPipeline(physical, input)
}

func TestLogicalOptimizerBoundaryTDDNoOpArtifactsAreDistinctAndPhysicalOnlyAfterOptimize(t *testing.T) {
	result, err := executeThroughOptimizerBoundaryTDD(
		t,
		usersTable(),
		`select city, name, age | filter { age > 30 } | select name, age`,
		nil,
	)
	if err != nil {
		t.Fatalf("execute through optimizer boundary: %v", err)
	}

	requireSimplePlannerSchema(t, result.Schema(), "name:string", "age:int")
	if result.NumRows != 2 {
		t.Fatalf("row count: got %d, want 2; result=\n%s", result.NumRows, result.String())
	}
	want := []struct {
		name string
		age  int64
	}{
		{"Charlie", 35},
		{"Frank", 40},
	}
	for i, w := range want {
		if got := result.GetAt(i, 0); got.Type != table.TypeString || got.Str != w.name {
			t.Fatalf("row %d name: got %v, want %q", i, got, w.name)
		}
		if got := result.GetAt(i, 1); got.Type != table.TypeInt || got.Int != w.age {
			t.Fatalf("row %d age: got %v, want %d", i, got, w.age)
		}
	}
}

func TestLogicalOptimizerBoundaryTDDNoOpPreservesOperationOrderAndRuntimeErrors(t *testing.T) {
	cases := []struct {
		name      string
		pipeline  string
		wantCount int64
		wantErr   []string
	}{
		{
			name:      "filter_before_erroring_transform_still_suppresses_runtime_error",
			pipeline:  `filter { false } | transform y = year("bad-date") | count`,
			wantCount: 0,
		},
		{
			name:      "dead_erroring_transform_before_filter_is_eliminated",
			pipeline:  `transform y = year("bad-date") | filter { false } | count`,
			wantCount: 0,
		},
		{
			name:      "unused_erroring_transform_is_eliminated",
			pipeline:  `transform unused = year("bad-date") | select name | count`,
			wantCount: 6,
		},
		{
			name:     "select_removes_column_for_later_filter",
			pipeline: `select name | filter { age > 30 } | count`,
			wantErr:  []string{"filter", "age", "not found"},
		},
		{
			name:     "rename_hides_original_column_name",
			pipeline: `rename age=years | filter { age > 30 } | count`,
			wantErr:  []string{"filter", "age", "not found"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := executeThroughOptimizerBoundaryTDD(t, usersTable(), tc.pipeline, nil)
			if len(tc.wantErr) > 0 {
				if err == nil {
					t.Fatalf("expected error containing %v, got result:\n%s", tc.wantErr, result.String())
				}
				msg := strings.ToLower(err.Error())
				for _, want := range tc.wantErr {
					if !strings.Contains(msg, strings.ToLower(want)) {
						t.Fatalf("error %q missing %q", err, want)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("execute through optimizer boundary: %v", err)
			}
			if result.NumRows != 1 || result.GetAt(0, 0).Int != tc.wantCount {
				t.Fatalf("count result: got\n%s\nwant count=%d", result.String(), tc.wantCount)
			}
		})
	}
}

func TestLogicalOptimizerBoundaryTDDNoOpPreservesTransformOverwriteAndZeroRowSchemas(t *testing.T) {
	result, err := executeThroughOptimizerBoundaryTDD(
		t,
		usersTable(),
		`transform age = age + 100 | filter { age > 130 } | select name, age | sort name`,
		nil,
	)
	if err != nil {
		t.Fatalf("execute transform overwrite: %v", err)
	}
	requireSimplePlannerSchema(t, result.Schema(), "name:string", "age:int")
	if result.NumRows != 2 {
		t.Fatalf("row count: got %d, want 2; result=\n%s", result.NumRows, result.String())
	}
	if got := result.GetAt(0, 0); got.Str != "Charlie" {
		t.Fatalf("first row name: got %v, want Charlie", got)
	}
	if got := result.GetAt(0, 1); got.Int != 135 {
		t.Fatalf("first row transformed age: got %v, want 135", got)
	}
	if got := result.GetAt(1, 0); got.Str != "Frank" {
		t.Fatalf("second row name: got %v, want Frank", got)
	}
	if got := result.GetAt(1, 1); got.Int != 140 {
		t.Fatalf("second row transformed age: got %v, want 140", got)
	}

	empty, err := executeThroughOptimizerBoundaryTDD(
		t,
		usersTable(),
		`filter { false } | transform age2 = age + 1 | select name, age2 | describe`,
		nil,
	)
	if err != nil {
		t.Fatalf("execute zero-row schema pipeline: %v", err)
	}
	assertDescribeSchemaRows(t, empty, map[string]describeSchemaMeta{
		"name": {typ: "string", rows: 0, schema: "string"},
		"age2": {typ: "int", rows: 0, schema: "int"},
	})
}

func TestLogicalOptimizerBoundaryTDDNoOpPreservesJoinGroupReduceAndDistinctSemantics(t *testing.T) {
	left := table.NewTableWithSchemas(
		[]string{"id", "city", "age"},
		[]*table.TypeDescriptor{{Kind: table.TypeInt}, {Kind: table.TypeString}, {Kind: table.TypeInt}},
	)
	left.AddRow([]table.Value{table.IntVal(1), table.StrVal("NY"), table.IntVal(30)})
	left.AddRow([]table.Value{table.IntVal(2), table.StrVal("LA"), table.IntVal(25)})
	left.AddRow([]table.Value{table.IntVal(3), table.StrVal("NY"), table.IntVal(35)})

	right := table.NewTableWithSchemas(
		[]string{"id", "amount"},
		[]*table.TypeDescriptor{{Kind: table.TypeInt}, {Kind: table.TypeInt}},
	)
	right.AddRow([]table.Value{table.IntVal(1), table.IntVal(10)})
	right.AddRow([]table.Value{table.IntVal(1), table.IntVal(20)})
	right.AddRow([]table.Value{table.IntVal(3), table.IntVal(30)})

	load := func(filename string, opts ast.LoadOptions) (*table.Table, error) {
		if filename != "orders.csv" {
			t.Fatalf("unexpected join load filename %q", filename)
		}
		return right, nil
	}

	result, err := executeThroughOptimizerBoundaryTDD(
		t,
		left,
		`join orders.csv on id | group city | reduce total = sum(amount), n = count() | remove grouped | distinct city, total, n | sort city`,
		load,
	)
	if err != nil {
		t.Fatalf("execute join/group/reduce pipeline: %v", err)
	}
	requireSimplePlannerSchema(t, result.Schema(), "city:string", "total:int?", "n:int")
	if result.NumRows != 1 {
		t.Fatalf("row count: got %d, want 1; result=\n%s", result.NumRows, result.String())
	}
	if got := result.GetAt(0, 0); got.Str != "NY" {
		t.Fatalf("city: got %v, want NY", got)
	}
	if got := result.GetAt(0, 1); got.Int != 60 {
		t.Fatalf("total: got %v, want 60", got)
	}
	if got := result.GetAt(0, 2); got.Int != 3 {
		t.Fatalf("n: got %v, want 3", got)
	}
}

func TestLogicalOptimizerBoundaryTDDNoOpPreservesNestedPathGuardrails(t *testing.T) {
	input := simplePlannerInputTable()
	cases := []struct {
		name     string
		pipeline string
		wants    []string
	}{
		{
			name:     "list_path_traversal_still_fails",
			pipeline: `filter { false } | select orders.amount`,
			wants:    []string{"orders", "list"},
		},
		{
			name:     "union_path_traversal_still_fails",
			pipeline: `filter { false } | select u.x`,
			wants:    []string{"u.x", "union"},
		},
		{
			name:     "missing_nested_field_still_fails",
			pipeline: `filter { false } | filter { address.missing == "NY" }`,
			wants:    []string{"missing", "not found"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := executeThroughOptimizerBoundaryTDD(t, input, tc.pipeline, nil)
			if err == nil {
				t.Fatalf("expected planning error containing %v, got result:\n%s", tc.wants, result.String())
			}
			msg := strings.ToLower(err.Error())
			for _, want := range tc.wants {
				if !strings.Contains(msg, strings.ToLower(want)) {
					t.Fatalf("error %q missing %q", err, want)
				}
			}
		})
	}
}

func TestLogicalOptimizerBoundaryTDDPhysicalizesExpressionShapes(t *testing.T) {
	result, err := executeThroughOptimizerBoundaryTDD(
		t,
		simplePlannerInputTable(),
		`transform neg = -age, rec = struct(name = name, age = age), xs = list(age, null), c = coalesce(age, 0.0), address_null = address is null | select neg, rec, xs, c, address_null | describe`,
		nil,
	)
	if err != nil {
		t.Fatalf("execute expression-shape physicalization pipeline: %v", err)
	}
	assertDescribeSchemaRows(t, result, map[string]describeSchemaMeta{
		"neg":          {typ: "int", rows: 0, schema: "int"},
		"rec":          {typ: "record", rows: 0, schema: "record<age:int, name:string>"},
		"xs":           {typ: "list", rows: 0, schema: "list<int?>"},
		"c":            {typ: "float", rows: 0, schema: "float"},
		"address_null": {typ: "bool", rows: 0, schema: "bool"},
	})
}

type unknownLogicalOpForBoundaryTDD struct {
	logicalBase
}

type schemaOnlyLogicalOpForBoundaryTDD struct {
	output table.Schema
}

func (op schemaOnlyLogicalOpForBoundaryTDD) OutputSchema() table.Schema {
	return op.output
}

type unknownLogicalBoundExprForBoundaryTDD struct{}

func (*unknownLogicalBoundExprForBoundaryTDD) logicalBoundExprNode() {}

func TestLogicalOptimizerBoundaryTDDPhaseDefensiveErrors(t *testing.T) {
	if _, err := optimizeLogicalPipeline(nil); err == nil || !strings.Contains(err.Error(), "nil plan") {
		t.Fatalf("optimize nil error: got %v, want nil plan", err)
	}
	if _, err := planPhysicalPipeline(nil); err == nil || !strings.Contains(err.Error(), "nil optimized") {
		t.Fatalf("physical nil error: got %v, want nil optimized", err)
	}
	if _, err := executePhysicalPipeline(nil, usersTable()); err == nil || !strings.Contains(err.Error(), "nil plan") {
		t.Fatalf("execute nil error: got %v, want nil plan", err)
	}
	logicalForInto, err := planLogicalPipelineFromTableWithLoad(usersTable(), parseOptimizerBoundaryOpsTDD(t, `filter { age > 30 } | count`), nil)
	if err != nil {
		t.Fatalf("logical plan for into helpers: %v", err)
	}
	if err := optimizeLogicalPipelineInto(logicalForInto, nil); err == nil || !strings.Contains(err.Error(), "nil output") {
		t.Fatalf("optimize nil output error: got %v", err)
	}
	var optimizedInto optimizedLogicalPipeline
	if err := optimizeLogicalPipelineInto(logicalForInto, &optimizedInto); err != nil {
		t.Fatalf("optimize into: %v", err)
	}
	if len(optimizedInto.Ops) != len(logicalForInto.Ops) || optimizedInto.OutputSchema.Columns[0].Name != "count" {
		t.Fatalf("optimized into did not preserve logical shape: %#v", optimizedInto)
	}
	if err := planPhysicalPipelineInto(&optimizedInto, nil); err == nil || !strings.Contains(err.Error(), "nil output") {
		t.Fatalf("physical nil output error: got %v", err)
	}
	var physicalInto physicalPipeline
	if err := planPhysicalPipelineInto(&optimizedInto, &physicalInto); err != nil {
		t.Fatalf("physical into: %v", err)
	}
	executed, err := executePhysicalPipeline(&physicalInto, usersTable())
	if err != nil {
		t.Fatalf("execute physical pipeline with validation: %v", err)
	}
	if executed.NumRows != 1 || executed.GetAt(0, 0).Int != 2 {
		t.Fatalf("validated physical execution result: got\n%s\nwant count=2", executed.String())
	}
	_, err = planPhysicalPipeline(&optimizedLogicalPipeline{
		InputSchema:  usersTable().Schema(),
		OutputSchema: usersTable().Schema(),
		Ops:          []logicalOp{unknownLogicalOpForBoundaryTDD{logicalBase: logicalBase{output: usersTable().Schema()}}},
	})
	if err == nil || !strings.Contains(err.Error(), "unknown optimized logical operation") {
		t.Fatalf("unknown physical op error: got %v", err)
	}

	inputSchema := usersTable().Schema()
	envFromBase := (logicalBase{output: inputSchema}).OutputEnv()
	if len(envFromBase.columns) != len(inputSchema.Columns) || envFromBase.columns[0] != "name" {
		t.Fatalf("logical base fallback env: got %#v", envFromBase.columns)
	}
	envFromSchemaOnly := logicalOutputEnv(schemaOnlyLogicalOpForBoundaryTDD{output: inputSchema})
	if len(envFromSchemaOnly.columns) != len(inputSchema.Columns) || envFromSchemaOnly.columns[1] != "age" {
		t.Fatalf("logical output env fallback: got %#v", envFromSchemaOnly.columns)
	}
	if schema := envFromSchemaOnly.finalSchema(1); schema == nil || schema.Kind != table.TypeInt {
		t.Fatalf("lazy final schema: got %v, want int", schema)
	}
}

func TestLogicalOptimizerBoundaryTDDPlannerDefensiveBranches(t *testing.T) {
	input := usersTable().Schema()

	if _, err := planLogicalPipeline(input, []ast.Op{&ast.SourceOp{Filename: "bad.csv"}}, nil); err == nil || !strings.Contains(err.Error(), "cannot plan operation") {
		t.Fatalf("unsupported logical op error: got %v", err)
	}
	if _, err := planLogicalOp(schemaEnvFromSchema(input), &ast.SourceOp{Filename: "bad.csv"}, nil); err == nil || !strings.Contains(err.Error(), "unknown logical operation") {
		t.Fatalf("unknown logical op error: got %v", err)
	}

	_, err := planPhysicalPipeline(&optimizedLogicalPipeline{
		InputSchema: input,
		Ops: []logicalOp{logicalTransform{
			logicalBase: logicalBase{output: input},
			assignments: []logicalAssignment{{
				name: "missing",
				expr: logicalTypedExpr{bound: &logicalBoundLiteral{raw: &ast.LiteralExpr{Kind: "int", Int: 1}}, typ: &table.TypeDescriptor{Kind: table.TypeInt}},
			}},
		}},
	})
	if err == nil || !strings.Contains(err.Error(), `target "missing"`) {
		t.Fatalf("missing transform target error: got %v", err)
	}

	groupedSchema := table.NewSchema(
		[]string{"grouped"},
		[]*table.TypeDescriptor{{
			Kind: table.TypeList,
			Elem: &table.TypeDescriptor{Kind: table.TypeRecord, Fields: []table.FieldDescriptor{
				{Name: "age", Type: &table.TypeDescriptor{Kind: table.TypeInt}},
			}},
		}},
	)
	_, err = planPhysicalPipeline(&optimizedLogicalPipeline{
		InputSchema: groupedSchema,
		Ops: []logicalOp{logicalReduce{
			logicalBase:  logicalBase{output: groupedSchema},
			nestedName:   "grouped",
			nestedSchema: groupedSchema.Columns[0].Type.Elem,
			assignments: []logicalAssignment{{
				name: "missing",
				expr: logicalTypedExpr{bound: &logicalBoundCall{raw: &ast.FuncCallExpr{Name: "count"}}, typ: &table.TypeDescriptor{Kind: table.TypeInt}},
			}},
		}},
	})
	if err == nil || !strings.Contains(err.Error(), `target "missing"`) {
		t.Fatalf("missing reduce target error: got %v", err)
	}

	_, err = planPhysicalPipeline(&optimizedLogicalPipeline{
		InputSchema: input,
		Ops: []logicalOp{logicalReduce{
			logicalBase:  logicalBase{output: input},
			nestedName:   "missing",
			nestedSchema: groupedSchema.Columns[0].Type.Elem,
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "nested column") {
		t.Fatalf("missing reduce nested column error: got %v", err)
	}

	_, err = planPhysicalPipeline(&optimizedLogicalPipeline{
		InputSchema: groupedSchema,
		Ops: []logicalOp{logicalReduce{
			logicalBase:  logicalBase{output: groupedSchema},
			nestedName:   "grouped",
			nestedSchema: nil,
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "nested row schema") {
		t.Fatalf("bad reduce nested schema error: got %v", err)
	}

	_, err = planPhysicalPipeline(&optimizedLogicalPipeline{
		InputSchema: input,
		Ops: []logicalOp{logicalRemove{
			logicalBase: logicalBase{output: input},
			kept:        []string{"missing"},
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "kept column") {
		t.Fatalf("missing kept column error: got %v", err)
	}

	_, err = planPhysicalPipeline(&optimizedLogicalPipeline{
		InputSchema: input,
		Ops: []logicalOp{logicalGroup{
			logicalBase: logicalBase{output: input},
			keys:        []logicalPathBinding{{name: "missing", path: []string{"missing"}, schema: &table.TypeDescriptor{Kind: table.TypeInt}}},
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("missing physical group key error: got %v", err)
	}

	_, err = planPhysicalPipeline(&optimizedLogicalPipeline{
		InputSchema: input,
		Ops: []logicalOp{logicalSort{
			logicalBase: logicalBase{output: input},
			keys:        []logicalSortKey{{path: []string{"missing"}}},
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("missing physical sort key error: got %v", err)
	}

	_, err = planPhysicalPipeline(&optimizedLogicalPipeline{
		InputSchema: input,
		Ops: []logicalOp{logicalSelect{
			logicalBase:  logicalBase{output: input},
			projections:  []logicalPathBinding{{name: "missing", path: []string{"missing"}, schema: &table.TypeDescriptor{Kind: table.TypeInt}}},
			topLevelOnly: true,
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("missing physical projection error: got %v", err)
	}

	_, err = planPhysicalPipeline(&optimizedLogicalPipeline{
		InputSchema: input,
		Ops: []logicalOp{logicalFilter{
			logicalBase: logicalBase{output: input},
			expr: logicalTypedExpr{
				bound: &logicalBoundBinary{raw: &ast.BinaryExpr{Op: "+"}},
				typ:   &table.TypeDescriptor{Kind: table.TypeInt},
				right: &logicalTypedExpr{bound: &logicalBoundLiteral{raw: &ast.LiteralExpr{Kind: "int", Int: 1}}, typ: &table.TypeDescriptor{Kind: table.TypeInt}},
			},
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "missing typed expression child") {
		t.Fatalf("missing physicalize child error: got %v", err)
	}

	_, err = physicalizeTypedExpr(logicalTypedExpr{bound: &unknownLogicalBoundExprForBoundaryTDD{}}, schemaEnvFromSchema(input))
	if err == nil || !strings.Contains(err.Error(), "unknown logical typed expression binding") {
		t.Fatalf("unknown physicalize binding error: got %v", err)
	}
}

func TestLogicalOptimizerBoundaryTDDPlanningSchemaComparisonHelpers(t *testing.T) {
	intSchema := &table.TypeDescriptor{Kind: table.TypeInt}
	stringSchema := &table.TypeDescriptor{Kind: table.TypeString}
	recordA := &table.TypeDescriptor{Kind: table.TypeRecord, Fields: []table.FieldDescriptor{
		{Name: "id", Type: intSchema},
		{Name: "name", Type: stringSchema},
	}}
	recordB := &table.TypeDescriptor{Kind: table.TypeRecord, Fields: []table.FieldDescriptor{
		{Name: "id", Type: &table.TypeDescriptor{Kind: table.TypeInt}},
		{Name: "name", Type: &table.TypeDescriptor{Kind: table.TypeString}},
	}}
	recordDifferent := &table.TypeDescriptor{Kind: table.TypeRecord, Fields: []table.FieldDescriptor{
		{Name: "id", Type: intSchema},
		{Name: "nickname", Type: stringSchema},
	}}
	listA := &table.TypeDescriptor{Kind: table.TypeList, Elem: recordA}
	listB := &table.TypeDescriptor{Kind: table.TypeList, Elem: recordB}
	unionA := &table.TypeDescriptor{Kind: table.TypeUnion, Branches: []*table.TypeDescriptor{intSchema, stringSchema}}
	unionB := &table.TypeDescriptor{Kind: table.TypeUnion, Branches: []*table.TypeDescriptor{{Kind: table.TypeInt}, {Kind: table.TypeString}}}
	unionDifferent := &table.TypeDescriptor{Kind: table.TypeUnion, Branches: []*table.TypeDescriptor{stringSchema, intSchema}}

	if !samePlanningSchema(recordA, recordB) {
		t.Fatal("samePlanningSchema should match equivalent normalized records")
	}
	if samePlanningSchema(recordA, recordDifferent) {
		t.Fatal("samePlanningSchema should reject different record fields")
	}
	if !sameNormalizedPlanningSchema(listA, listB) {
		t.Fatal("sameNormalizedPlanningSchema should match equivalent lists")
	}
	if !sameNormalizedPlanningSchema(unionA, unionB) {
		t.Fatal("sameNormalizedPlanningSchema should match same ordered union branches")
	}
	if sameNormalizedPlanningSchema(unionA, unionDifferent) {
		t.Fatal("sameNormalizedPlanningSchema should reject reordered union branches")
	}
	if sameNormalizedPlanningSchema(nil, intSchema) || !sameNormalizedPlanningSchema(nil, nil) {
		t.Fatal("sameNormalizedPlanningSchema nil handling is incorrect")
	}
}

func TestLogicalOptimizerBoundaryTDDLogicalAndPhysicalColumnRefsAreDifferentADTs(t *testing.T) {
	input := usersTable()
	ops := parseOptimizerBoundaryOpsTDD(t, `filter { age > 30 } | count`)
	logical, err := planLogicalPipelineFromTableWithLoad(input, ops, nil)
	if err != nil {
		t.Fatalf("logical plan: %v", err)
	}
	filter, ok := logical.Ops[0].(logicalFilter)
	if !ok {
		t.Fatalf("first logical op: got %T, want logicalFilter", logical.Ops[0])
	}
	binary, ok := filter.expr.bound.(*logicalBoundBinary)
	if !ok {
		t.Fatalf("logical filter expression: got %T, want logicalBoundBinary", filter.expr.bound)
	}
	left, ok := filter.expr.left.bound.(*logicalBoundColumn)
	if !ok {
		t.Fatalf("logical filter left expression: got %T, want logicalBoundColumn", filter.expr.left.bound)
	}
	if left.rawPath[0] != "age" || left.typ.Kind != table.TypeInt {
		t.Fatalf("logical column ref: got path=%v schema=%s", left.rawPath, table.Render(left.typ))
	}
	if _, ok := any(left).(*boundColumn); ok {
		t.Fatal("logical column ref must not be a physical boundColumn")
	}
	if binary.raw.Op != ">" {
		t.Fatalf("logical binary op: got %q, want >", binary.raw.Op)
	}

	optimized, err := optimizeLogicalPipeline(logical)
	if err != nil {
		t.Fatalf("optimize logical plan: %v", err)
	}
	physical, err := planPhysicalPipeline(optimized)
	if err != nil {
		t.Fatalf("physical plan: %v", err)
	}
	plannedFilter, ok := physical.Ops[0].(plannedFilter)
	if !ok {
		t.Fatalf("first physical op: got %T, want plannedFilter", physical.Ops[0])
	}
	physicalBinary, ok := plannedFilter.expr.bound.(*boundBinary)
	if !ok {
		t.Fatalf("physical filter expression: got %T, want boundBinary", plannedFilter.expr.bound)
	}
	physicalColumn, ok := plannedFilter.expr.left.bound.(*boundColumn)
	if !ok {
		t.Fatalf("physical filter left expression: got %T, want boundColumn", plannedFilter.expr.left.bound)
	}
	if physicalColumn.topIndex != input.ColIndex("age") {
		t.Fatalf("physical column top index: got %d, want %d", physicalColumn.topIndex, input.ColIndex("age"))
	}
	if physicalBinary.raw.Op != ">" {
		t.Fatalf("physical binary op: got %q, want >", physicalBinary.raw.Op)
	}
}

func TestLogicalOptimizerBoundaryTDDNoOpOptimizerTreatsSharedLogicalFactsAsImmutable(t *testing.T) {
	input := usersTable()
	logical, err := planLogicalPipelineFromTableWithLoad(input, parseOptimizerBoundaryOpsTDD(t, `filter { age > 30 } | select name, age`), nil)
	if err != nil {
		t.Fatalf("logical plan: %v", err)
	}

	assertLogicalPlanStillOriginal := func(t *testing.T) {
		t.Helper()
		if logical.InputEnv.columns[1] != "age" || logical.InputSchema.Columns[1].Name != "age" || logical.OutputSchema.Columns[0].Name != "name" {
			t.Fatalf("logical pipeline metadata was mutated: input env=%v input schema=%v output schema=%v", logical.InputEnv.columns, logical.InputSchema.Columns, logical.OutputSchema.Columns)
		}
		filter := logical.Ops[0].(logicalFilter)
		filterExpr := filter.expr.bound.(*logicalBoundBinary)
		if filterExpr.raw.Op != ">" {
			t.Fatalf("logical filter op was mutated: got %q, want >", filterExpr.raw.Op)
		}
		filterColumn := filter.expr.left.bound.(*logicalBoundColumn)
		if filterColumn.rawPath[0] != "age" || filterColumn.typ.Kind != table.TypeInt {
			t.Fatalf("logical filter column was mutated: path=%v schema=%s", filterColumn.rawPath, table.Render(filterColumn.typ))
		}
		selectOp := logical.Ops[1].(logicalSelect)
		if selectOp.projections[0].path[0] != "name" || selectOp.projections[0].schema.Kind != table.TypeString {
			t.Fatalf("logical select projection was mutated: path=%v schema=%s", selectOp.projections[0].path, table.Render(selectOp.projections[0].schema))
		}
	}
	assertLogicalPlanStillOriginal(t)

	optimized, err := optimizeLogicalPipeline(logical)
	if err != nil {
		t.Fatalf("optimize logical plan: %v", err)
	}
	assertLogicalPlanStillOriginal(t)
	physical, err := planPhysicalPipeline(optimized)
	if err != nil {
		t.Fatalf("physical plan from optimized copy: %v", err)
	}
	assertLogicalPlanStillOriginal(t)
	result, err := executePhysicalPipeline(physical, input)
	if err != nil {
		t.Fatalf("execute optimized copy: %v", err)
	}
	assertLogicalPlanStillOriginal(t)
	requireSimplePlannerSchema(t, result.Schema(), "name:string", "age:int")
	if result.NumRows != 2 || result.GetAt(0, 0).Str != "Charlie" || result.GetAt(1, 0).Str != "Frank" {
		t.Fatalf("optimized no-op result changed:\n%s", result.String())
	}
}

func TestLogicalOptimizerBoundaryTDDColumnBindingsCloneCallerPaths(t *testing.T) {
	env := schemaEnvFromTable(simplePlannerInputTable())

	topPath := []string{"age"}
	physicalTop, err := bindColumnPathInEnv(env, topPath)
	if err != nil {
		t.Fatalf("physical top-level bind: %v", err)
	}
	logicalTop, err := bindColumnPathLogicalInEnv(env, topPath)
	if err != nil {
		t.Fatalf("logical top-level bind: %v", err)
	}
	topPath[0] = "missing"
	if physicalTop.rawPath[0] != "age" {
		t.Fatalf("physical top-level path aliased caller path: got %v", physicalTop.rawPath)
	}
	if logicalTop.rawPath[0] != "age" {
		t.Fatalf("logical top-level path aliased caller path: got %v", logicalTop.rawPath)
	}

	nestedPath := []string{"address", "city"}
	physicalNested, err := bindColumnPathInEnv(env, nestedPath)
	if err != nil {
		t.Fatalf("physical nested bind: %v", err)
	}
	logicalNested, err := bindColumnPathLogicalInEnv(env, nestedPath)
	if err != nil {
		t.Fatalf("logical nested bind: %v", err)
	}
	nestedPath[0] = "missing"
	nestedPath[1] = "zip"
	if !reflect.DeepEqual(physicalNested.rawPath, []string{"address", "city"}) {
		t.Fatalf("physical nested raw path aliased caller path: got %v", physicalNested.rawPath)
	}
	if !reflect.DeepEqual(physicalNested.nestedPath, []string{"city"}) {
		t.Fatalf("physical nested path aliased caller path: got %v", physicalNested.nestedPath)
	}
	if !reflect.DeepEqual(logicalNested.rawPath, []string{"address", "city"}) {
		t.Fatalf("logical nested path aliased caller path: got %v", logicalNested.rawPath)
	}
	physicalNested.rawPath[1] = "state"
	if !reflect.DeepEqual(physicalNested.nestedPath, []string{"city"}) {
		t.Fatalf("physical nested path should not alias raw path tail: got %v", physicalNested.nestedPath)
	}
}

func TestLogicalOptimizerBoundaryTDDJoinLoaderAndStdinErrors(t *testing.T) {
	env := schemaEnvFromTable(usersTable())
	if _, err := planLogicalOp(env, &ast.JoinOp{Filename: "right.csv"}, nil); err == nil || !strings.Contains(err.Error(), "loader not configured") {
		t.Fatalf("logical join nil loader error: got %v", err)
	}
	if _, err := planLogicalOp(env, &ast.JoinOp{Filename: "-"}, func(string, ast.LoadOptions) (*table.Table, error) { return usersTable(), nil }); err == nil || !strings.Contains(err.Error(), "stdin") {
		t.Fatalf("logical join stdin error: got %v", err)
	}
}

func TestLogicalOptimizerBoundaryTDDPhysicalJoinRejectsStaleOptimizedKeyMetadata(t *testing.T) {
	left := table.NewTableWithSchemas(
		[]string{"id"},
		[]*table.TypeDescriptor{{Kind: table.TypeInt}},
	)
	right := table.NewTableWithSchemas(
		[]string{"id"},
		[]*table.TypeDescriptor{{Kind: table.TypeInt}},
	)

	baseLogical, err := planLogicalPipelineFromTableWithLoad(left, parseOptimizerBoundaryOpsTDD(t, `join right.csv on id`), func(string, ast.LoadOptions) (*table.Table, error) {
		return right, nil
	})
	if err != nil {
		t.Fatalf("logical join plan: %v", err)
	}
	baseJoin, ok := baseLogical.Ops[0].(logicalJoin)
	if !ok {
		t.Fatalf("logical op type: got %T, want logicalJoin", baseLogical.Ops[0])
	}

	cases := []struct {
		name string
		edit func(*logicalJoin)
		want string
	}{
		{
			name: "left_key_removed",
			edit: func(j *logicalJoin) {
				j.leftKeys[0].path = []string{"missing"}
			},
			want: "left join key",
		},
		{
			name: "right_key_removed",
			edit: func(j *logicalJoin) {
				j.rightKeys[0].path = []string{"missing"}
			},
			want: "right join key",
		},
		{
			name: "left_key_schema_changed",
			edit: func(j *logicalJoin) {
				j.leftKeys[0].schema = &table.TypeDescriptor{Kind: table.TypeString}
			},
			want: "left key",
		},
		{
			name: "right_key_schema_changed",
			edit: func(j *logicalJoin) {
				j.rightKeys[0].schema = &table.TypeDescriptor{Kind: table.TypeString}
			},
			want: "right key",
		},
		{
			name: "output_column_count_changed",
			edit: func(j *logicalJoin) {
				out := j.OutputSchema()
				out.Columns = out.Columns[:len(out.Columns)-1]
				j.logicalBase.output = out
			},
			want: "output source count",
		},
		{
			name: "output_column_name_changed",
			edit: func(j *logicalJoin) {
				out := j.OutputSchema()
				out.Columns = append([]table.SchemaColumn(nil), out.Columns...)
				out.Columns[0].Name = "renamed"
				j.logicalBase.output = out
			},
			want: "output source 0 changed",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			join := baseJoin
			join.leftKeys = append([]logicalPathBinding(nil), baseJoin.leftKeys...)
			join.rightKeys = append([]logicalPathBinding(nil), baseJoin.rightKeys...)
			tc.edit(&join)

			_, err := planPhysicalPipeline(&optimizedLogicalPipeline{
				InputSchema:  baseLogical.InputSchema,
				OutputSchema: join.OutputSchema(),
				Ops:          []logicalOp{join},
			})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("physical join error: got %v, want substring %q", err, tc.want)
			}
		})
	}
}
