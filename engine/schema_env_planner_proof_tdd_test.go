package engine

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/razeghi71/dq/ast"
	"github.com/razeghi71/dq/table"
)

type schemaEnvPlannerProofTDDOutputEnvCarrier interface {
	OutputEnv() schemaEnv
}

func TestSchemaEnvPlannerProofTDDBasesStoreOnlySchemaEnvAuthority(t *testing.T) {
	requireSchemaEnvPlannerProofTDDBaseAuthority(t, reflect.TypeOf(logicalBase{}), "logicalBase")
	requireSchemaEnvPlannerProofTDDBaseAuthority(t, reflect.TypeOf(plannedBase{}), "plannedBase")
}

func TestSchemaEnvPlannerProofTDDLogicalOpsRenderOutputSchemaFromOutputEnv(t *testing.T) {
	input := simplePlannerInputTable()

	for _, tc := range schemaEnvPlannerProofTDDPlanCases(t) {
		t.Run(tc.name, func(t *testing.T) {
			logical, err := planLogicalPipeline(input.Schema(), parseSimplePlannerOps(t, tc.pipeline), tc.load)
			if err != nil {
				t.Fatalf("plan logical pipeline %q: %v", tc.pipeline, err)
			}
			optimized, err := optimizeLogicalPipeline(logical)
			if err != nil {
				t.Fatalf("optimize logical pipeline %q: %v", tc.pipeline, err)
			}

			for _, op := range schemaEnvPlannerProofTDDLogicalOpsIncludingSpanChildren(optimized.Ops) {
				env := op.OutputEnv()
				requireSchemaEnvPlannerProofTDDSchemaEqual(t, op.OutputSchema(), env.schema(), "%T", op)
			}
		})
	}
}

func TestSchemaEnvPlannerProofTDDPhysicalOpsExposeOutputEnvProof(t *testing.T) {
	input := simplePlannerInputTable()

	for _, tc := range schemaEnvPlannerProofTDDPlanCases(t) {
		t.Run(tc.name, func(t *testing.T) {
			plan := schemaEnvPlannerProofTDDPhysicalPlan(t, input.Schema(), tc.pipeline, tc.load)
			for _, op := range schemaEnvPlannerProofTDDPhysicalOpsIncludingSpanChildren(plan.Ops) {
				carrier, ok := op.(schemaEnvPlannerProofTDDOutputEnvCarrier)
				if !ok {
					t.Fatalf("%T must expose OutputEnv() schemaEnv so table.Schema is a rendered view, not the physical proof authority", op)
				}
				requireSchemaEnvPlannerProofTDDSchemaEqual(t, op.OutputSchema(), carrier.OutputEnv().schema(), "%T", op)
			}
		})
	}
}

func TestSchemaEnvPlannerProofTDDPlannedRowSpanOutputEnvIsFinalChildProof(t *testing.T) {
	input := simplePlannerInputTable()
	plan := schemaEnvPlannerProofTDDPhysicalPlan(
		t,
		input.Schema(),
		`filter { age > 20 } | select name, age | transform label = upper(name), age2 = age + 1 | select label, age2`,
		nil,
	)
	if len(plan.Ops) != 1 {
		t.Fatalf("planned ops: got %d, want one row span; ops=%#v", len(plan.Ops), plan.Ops)
	}
	span, ok := plan.Ops[0].(plannedRowSpan)
	if !ok {
		t.Fatalf("planned op: got %T, want plannedRowSpan", plan.Ops[0])
	}
	if len(span.ops) == 0 {
		t.Fatal("planned row span has no children")
	}

	spanCarrier, ok := any(span).(schemaEnvPlannerProofTDDOutputEnvCarrier)
	if !ok {
		t.Fatalf("%T must expose OutputEnv() schemaEnv", span)
	}
	last := span.ops[len(span.ops)-1]
	lastCarrier, ok := last.(schemaEnvPlannerProofTDDOutputEnvCarrier)
	if !ok {
		t.Fatalf("final row-span child %T must expose OutputEnv() schemaEnv", last)
	}

	spanEnv := spanCarrier.OutputEnv()
	lastEnv := lastCarrier.OutputEnv()
	requireSchemaEnvPlannerProofTDDSameProof(t, spanEnv, lastEnv, "plannedRowSpan output must be the final child proof")
}

func TestSchemaEnvPlannerProofTDDPhysicalPipelineOutputSchemaUsesFinalOpProofAfterPruningAndFusion(t *testing.T) {
	q := parseSourceProjectionTDDQuery(
		t,
		`wide.csv | filter { upper(status) == "ACTIVE" } | transform unused_calc = year(raw), gross = amount * quantity, label = upper(name) | select id, gross | json`,
	)
	logical, err := planLogicalQueryWithSource(q, logicalSource{
		filename: q.Source.Filename,
		load:     q.Source.Load,
		schema:   demandPruningTDDWideSchema(),
	}, nil)
	if err != nil {
		t.Fatalf("plan logical source query: %v", err)
	}
	optimized, err := optimizeLogicalPipeline(logical)
	if err != nil {
		t.Fatalf("optimize logical source query: %v", err)
	}
	if got, want := optimized.Source.outputColumns.Names(), []string{"id", "status", "amount", "quantity"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("optimized source output columns: got %v, want %v", got, want)
	}

	physical, err := planPhysicalPipeline(optimized)
	if err != nil {
		t.Fatalf("plan physical pipeline: %v", err)
	}
	if len(physical.Ops) != 1 {
		t.Fatalf("physical ops: got %d, want one fused row span; ops=%#v", len(physical.Ops), physical.Ops)
	}
	span, ok := physical.Ops[0].(plannedRowSpan)
	if !ok {
		t.Fatalf("physical op: got %T, want plannedRowSpan", physical.Ops[0])
	}
	if len(span.ops) != 3 {
		t.Fatalf("planned row span children: got %d, want filter/transform/select; children=%#v", len(span.ops), span.ops)
	}
	transform, ok := span.ops[1].(plannedTransform)
	if !ok {
		t.Fatalf("planned row span child 1: got %T, want plannedTransform", span.ops[1])
	}
	if len(transform.assignments) != 1 || transform.assignments[0].name != "gross" {
		t.Fatalf("pruned transform assignments: got %#v, want only gross", transform.assignments)
	}

	requireSchemaEnvPlannerProofTDDSchemaEqual(t, physical.OutputSchema, physical.Ops[len(physical.Ops)-1].OutputEnv().schema(), "physical pipeline output schema must render the final planned op proof")
	requireSchemaEnvPlannerProofTDDSchemaEqual(t, physical.OutputSchema, span.ops[len(span.ops)-1].OutputEnv().schema(), "physical pipeline output schema must render the final row-span child proof")
}

func TestSchemaEnvPlannerProofTDDPhysicalSchemaPreservingOpsReuseInputProof(t *testing.T) {
	input := simplePlannerInputTable()
	inputEnv := mustSchemaEnvFromSchema(input.Schema())

	for _, pipeline := range []string{
		`head 1`,
		`tail 1`,
		`filter { age > 20 }`,
		`sort city, -age`,
		`distinct`,
	} {
		t.Run(pipeline, func(t *testing.T) {
			plan := schemaEnvPlannerProofTDDPhysicalPlanInEnv(t, inputEnv, pipeline, nil)
			if len(plan.Ops) != 1 {
				t.Fatalf("planned ops for %q: got %d, want 1", pipeline, len(plan.Ops))
			}
			carrier, ok := plan.Ops[0].(schemaEnvPlannerProofTDDOutputEnvCarrier)
			if !ok {
				t.Fatalf("%T must expose OutputEnv() schemaEnv", plan.Ops[0])
			}
			requireSchemaEnvPlannerProofTDDSameProof(t, inputEnv, carrier.OutputEnv(), "%q should reuse the input proof", pipeline)
		})
	}
}

func TestSchemaEnvPlannerProofTDDSchemaBoundaryErrorsStayAtPlanningTime(t *testing.T) {
	input := simplePlannerInputTable().Schema()

	cases := []struct {
		name     string
		pipeline string
		want     []string
	}{
		{
			name:     "select_hides_filter_column",
			pipeline: `select name | filter { age > 20 }`,
			want:     []string{"filter", "age", "not found"},
		},
		{
			name:     "rename_hides_original_column",
			pipeline: `rename age=years | sort age`,
			want:     []string{"sort", "age", "not found"},
		},
		{
			name:     "remove_hides_transform_column",
			pipeline: `remove age | transform age2 = age + 1`,
			want:     []string{"transform", "age", "not found"},
		},
		{
			name:     "rename_duplicate_output_rejected",
			pipeline: `rename age=city`,
			want:     []string{"rename", "duplicate", "city"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			requireSimplePlannerErrorContains(t, input, tc.pipeline, tc.want...)
		})
	}
}

func schemaEnvPlannerProofTDDPlanCases(t *testing.T) []struct {
	name     string
	pipeline string
	load     LoadFunc
} {
	t.Helper()
	right := table.NewTableWithSchemas(
		[]string{"user_name", "amount"},
		[]*table.TypeDescriptor{{Kind: table.TypeString}, {Kind: table.TypeInt}},
	)
	loadRight := func(filename string, _ ast.LoadOptions) (*table.Table, error) {
		if filename != "orders.csv" {
			t.Fatalf("unexpected join source %q", filename)
		}
		return right, nil
	}

	return []struct {
		name     string
		pipeline string
		load     LoadFunc
	}{
		{name: "head", pipeline: `head 1`},
		{name: "tail", pipeline: `tail 1`},
		{name: "filter", pipeline: `filter { age > 20 }`},
		{name: "transform", pipeline: `transform age2 = age + 1, label = upper(name)`},
		{name: "row_span", pipeline: `filter { age > 20 } | select name, age | transform label = upper(name), age2 = age + 1 | rename age2=score | remove age`},
		{name: "group", pipeline: `group city`},
		{name: "reduce", pipeline: `reduce orders total = sum(amount), n = count()`},
		{name: "group_reduce", pipeline: `group city | reduce total = sum(age), n = count()`},
		{name: "sort", pipeline: `sort city, -age`},
		{name: "select", pipeline: `select name, address.city, profile.stats.score`},
		{name: "rename", pipeline: `rename age=years, city=location`},
		{name: "remove", pipeline: `remove city, active`},
		{name: "distinct_full", pipeline: `distinct`},
		{name: "distinct_projected", pipeline: `distinct city, age`},
		{name: "count", pipeline: `count`},
		{name: "describe", pipeline: `describe`},
		{name: "join", pipeline: `join orders.csv on name == user_name | select name, amount`, load: loadRight},
	}
}

func schemaEnvPlannerProofTDDPhysicalPlan(t *testing.T, input table.Schema, pipeline string, load LoadFunc) *physicalPipeline {
	t.Helper()
	logical, err := planLogicalPipeline(input, parseSimplePlannerOps(t, pipeline), load)
	if err != nil {
		t.Fatalf("plan logical pipeline %q: %v", pipeline, err)
	}
	optimized, err := optimizeLogicalPipeline(logical)
	if err != nil {
		t.Fatalf("optimize logical pipeline %q: %v", pipeline, err)
	}
	physical, err := planPhysicalPipeline(optimized)
	if err != nil {
		t.Fatalf("plan physical pipeline %q: %v", pipeline, err)
	}
	return physical
}

func schemaEnvPlannerProofTDDPhysicalPlanInEnv(t *testing.T, input schemaEnv, pipeline string, load LoadFunc) *physicalPipeline {
	t.Helper()
	logical, err := planLogicalPipelineInEnv(input, parseSimplePlannerOps(t, pipeline), newLoadFuncJoinSourceProvider(load))
	if err != nil {
		t.Fatalf("plan logical pipeline %q: %v", pipeline, err)
	}
	optimized, err := optimizeLogicalPipeline(logical)
	if err != nil {
		t.Fatalf("optimize logical pipeline %q: %v", pipeline, err)
	}
	physical, err := planPhysicalPipeline(optimized)
	if err != nil {
		t.Fatalf("plan physical pipeline %q: %v", pipeline, err)
	}
	return physical
}

func schemaEnvPlannerProofTDDLogicalOpsIncludingSpanChildren(ops []logicalOp) []logicalOp {
	out := make([]logicalOp, 0, len(ops))
	for _, op := range ops {
		out = append(out, op)
		if span, ok := op.(optimizedRowSpan); ok {
			out = append(out, schemaEnvPlannerProofTDDLogicalOpsIncludingSpanChildren(span.ops)...)
		}
	}
	return out
}

func schemaEnvPlannerProofTDDPhysicalOpsIncludingSpanChildren(ops []plannedOp) []plannedOp {
	out := make([]plannedOp, 0, len(ops))
	for _, op := range ops {
		out = append(out, op)
		if span, ok := op.(plannedRowSpan); ok {
			out = append(out, schemaEnvPlannerProofTDDPhysicalOpsIncludingSpanChildren(span.ops)...)
		}
	}
	return out
}

func requireSchemaEnvPlannerProofTDDBaseAuthority(t *testing.T, typ reflect.Type, name string) {
	t.Helper()
	schemaType := reflect.TypeOf(table.Schema{})
	envType := reflect.TypeOf(schemaEnv{})
	var schemaFields []string
	var envFields []string
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		switch field.Type {
		case schemaType:
			schemaFields = append(schemaFields, field.Name)
		case envType:
			envFields = append(envFields, field.Name)
		}
	}
	if len(schemaFields) > 0 {
		t.Fatalf("%s must not store eager table.Schema authority fields; got %v", name, schemaFields)
	}
	if len(envFields) != 1 {
		t.Fatalf("%s must store exactly one schemaEnv proof field, got %v", name, envFields)
	}
}

func requireSchemaEnvPlannerProofTDDSchemaEqual(t *testing.T, want, got table.Schema, format string, args ...any) {
	t.Helper()
	if len(want.Columns) != len(got.Columns) {
		t.Fatalf(format+": schema column count got %d, want %d; got=%#v want=%#v", append(args, len(got.Columns), len(want.Columns), got.Columns, want.Columns)...)
	}
	for i := range want.Columns {
		w := want.Columns[i]
		g := got.Columns[i]
		if w.Name != g.Name || !table.Same(w.Type, g.Type) {
			t.Fatalf(format+": schema column %d got %s:%s, want %s:%s", append(args, i, g.Name, table.Render(g.Type), w.Name, table.Render(w.Type))...)
		}
	}
}

func requireSchemaEnvPlannerProofTDDSameProof(t *testing.T, want, got schemaEnv, format string, args ...any) {
	t.Helper()
	requireSchemaEnvPlannerProofTDDSchemaEqual(t, want.schema(), got.schema(), format, args...)
	if !schemaEnvPlannerProofTDDSameColumnStorage(want, got) {
		t.Fatalf(format+": got an equal schema rendered from a different schemaEnv column proof; got=%s want=%s", append(args, schemaEnvPlannerProofTDDColumns(got), schemaEnvPlannerProofTDDColumns(want))...)
	}
}

func schemaEnvPlannerProofTDDSameColumnStorage(a, b schemaEnv) bool {
	if len(a.columns) != len(b.columns) {
		return false
	}
	if len(a.columns) == 0 {
		return true
	}
	return &a.columns[0] == &b.columns[0]
}

func schemaEnvPlannerProofTDDColumns(env schemaEnv) string {
	cols := make([]string, len(env.columns))
	for i, col := range env.columns {
		cols[i] = fmt.Sprintf("%s:%s", col.name, table.Render(col.planningSchema()))
	}
	return fmt.Sprintf("%v", cols)
}
