package engine

import (
	"reflect"
	"strings"
	"testing"

	"github.com/razeghi71/dq/ast"
	"github.com/razeghi71/dq/table"
)

func TestFusedGroupReduceTDDNoPayloadDemandPrunesSourceReadSet(t *testing.T) {
	cases := []struct {
		name  string
		query string
		read  []string
	}{
		{
			name:  "remove_grouped",
			query: `wide.csv | group status | reduce n = count(), total = sum(amount) | remove grouped | sort status | select status, total | json`,
			read:  []string{"status", "amount"},
		},
		{
			name:  "select_omits_grouped_after_filter",
			query: `wide.csv | group status | reduce n = count(), total = sum(amount) | filter { n > 0 } | select status, total | json`,
			read:  []string{"status", "amount"},
		},
		{
			name:  "custom_nested_name",
			query: `wide.csv | group status as rows | reduce rows n = count(), total = sum(amount) | remove rows | select status, total | json`,
			read:  []string{"status", "amount"},
		},
		{
			name:  "count_demands_only_group_key",
			query: `wide.csv | group status | reduce n = count() | remove grouped | select status, n | json`,
			read:  []string{"status"},
		},
		{
			name:  "aggregate_expression_demands_all_live_slots",
			query: `wide.csv | group status | reduce n = count(), total = sum(amount), ratio = sum(amount) / count() | remove grouped | select status, ratio | json`,
			read:  []string{"status", "amount"},
		},
		{
			name:  "nested_aggregate_paths_demand_top_level_roots",
			query: `nested.json | group address.city as rows | reduce rows avg_zip = avg(address.zip), first_address = first(address) | remove rows | select address_city, avg_zip, first_address | json`,
			read:  []string{"address"},
		},
		{
			name:  "reduce_target_overwrites_group_key",
			query: `wide.csv | group status | reduce status = count() | remove grouped | select status | json`,
			read:  []string{"status"},
		},
		{
			name:  "reduce_target_named_grouped_is_not_payload_demand",
			query: `wide.csv | group status | reduce grouped = count() | select status, grouped | json`,
			read:  []string{"status"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			schema := demandPruningTDDWideSchema()
			if strings.HasPrefix(tc.query, "nested.json") {
				schema = demandPruningTDDNestedSchema()
			}
			physical := planDemandPruningTDDSourceQuery(t, tc.query, schema, nil)
			requireDemandPruningTDDSourceSpec(t, physical, tc.read, tc.read, false)
			requireFusedGroupReduceTDDNoAdjacentMaterializedPair(t, physical)
		})
	}
}

func TestFusedGroupReduceTDDPayloadDemandRemainsReadAll(t *testing.T) {
	cases := []struct {
		name  string
		query string
	}{
		{name: "full_output_keeps_payload", query: `wide.csv | group status | reduce n = count() | json`},
		{name: "describe_observes_payload_schema", query: `wide.csv | group status | reduce n = count() | describe | json`},
		{name: "full_row_distinct_observes_payload_identity", query: `wide.csv | group status | reduce n = count() | distinct | select status | json`},
		{name: "filter_reads_payload", query: `wide.csv | group status | reduce n = count() | filter { list_len(grouped) > 1 } | select status | json`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			physical := planDemandPruningTDDSourceQuery(t, tc.query, demandPruningTDDWideSchema(), nil)
			requireDemandPruningTDDSourceSpec(t, physical, nil, nil, false)
		})
	}
}

func TestFusedGroupReduceTDDNoPayloadExecutesAggregateValues(t *testing.T) {
	input := table.NewTableWithSchemas(
		[]string{"city", "age", "amount", "name"},
		[]*table.TypeDescriptor{
			{Kind: table.TypeString},
			{Kind: table.TypeInt},
			{Kind: table.TypeFloat},
			{Kind: table.TypeString, Nullable: true},
		},
	)
	mustAddFusedGroupReduceTDDRow(t, input, table.StrVal("A"), table.IntVal(10), table.FloatVal(1.5), table.StrVal("ann"))
	mustAddFusedGroupReduceTDDRow(t, input, table.StrVal("A"), table.IntVal(20), table.FloatVal(2.5), table.Null())
	mustAddFusedGroupReduceTDDRow(t, input, table.StrVal("B"), table.IntVal(8), table.FloatVal(4), table.StrVal("bee"))

	plan, err := planPhysicalPipelineForTest(input.Schema(), parseSimplePlannerOps(t, `
		group city
		| reduce total = sum(age), avg_amount = avg(amount), min_age = min(age), max_age = max(age),
		         first_name = first(name), last_name = last(name)
		| remove grouped
		| sort city
		| select city, total, avg_amount, min_age, max_age, first_name, last_name
	`))
	if err != nil {
		t.Fatalf("plan no-payload group/reduce: %v", err)
	}
	requireFusedGroupReduceTDDNoPayloadOp(t, plan)

	out, err := executePlan(plan, input)
	if err != nil {
		t.Fatalf("execute no-payload group/reduce: %v", err)
	}
	requireSourceProjectionTDDTableColumns(t, out, "city", "total", "avg_amount", "min_age", "max_age", "first_name", "last_name")
	if out.NumRows != 2 {
		t.Fatalf("result rows: got %d, want 2", out.NumRows)
	}

	if got := out.GetAt(0, out.ColIndex("city")).Str; got != "A" {
		t.Fatalf("row 0 city: got %q, want A", got)
	}
	if got := out.GetAt(0, out.ColIndex("total")).Int; got != 30 {
		t.Fatalf("A total: got %d, want 30", got)
	}
	if got := out.GetAt(0, out.ColIndex("avg_amount")).Float; got != 2 {
		t.Fatalf("A avg_amount: got %v, want 2", got)
	}
	if got := out.GetAt(0, out.ColIndex("min_age")).Int; got != 10 {
		t.Fatalf("A min_age: got %d, want 10", got)
	}
	if got := out.GetAt(0, out.ColIndex("max_age")).Int; got != 20 {
		t.Fatalf("A max_age: got %d, want 20", got)
	}
	if got := out.GetAt(0, out.ColIndex("first_name")).Str; got != "ann" {
		t.Fatalf("A first_name: got %q, want ann", got)
	}
	if got := out.GetAt(0, out.ColIndex("last_name")); !got.IsNull() {
		t.Fatalf("A last_name: got %v, want null", got)
	}

	if got := out.GetAt(1, out.ColIndex("city")).Str; got != "B" {
		t.Fatalf("row 1 city: got %q, want B", got)
	}
	if got := out.GetAt(1, out.ColIndex("total")).Int; got != 8 {
		t.Fatalf("B total: got %d, want 8", got)
	}
	if got := out.GetAt(1, out.ColIndex("avg_amount")).Float; got != 4 {
		t.Fatalf("B avg_amount: got %v, want 4", got)
	}
	if got := out.GetAt(1, out.ColIndex("min_age")).Int; got != 8 {
		t.Fatalf("B min_age: got %d, want 8", got)
	}
	if got := out.GetAt(1, out.ColIndex("max_age")).Int; got != 8 {
		t.Fatalf("B max_age: got %d, want 8", got)
	}
	if got := out.GetAt(1, out.ColIndex("first_name")).Str; got != "bee" {
		t.Fatalf("B first_name: got %q, want bee", got)
	}
	if got := out.GetAt(1, out.ColIndex("last_name")).Str; got != "bee" {
		t.Fatalf("B last_name: got %q, want bee", got)
	}
}

func TestFusedGroupReduceTDDDeadReduceAssignmentsSkipRuntimeErrorsButKeepPlanningErrors(t *testing.T) {
	t.Run("dead_runtime_overflow_is_not_evaluated", func(t *testing.T) {
		q := parseSourceProjectionTDDQuery(t, `wide.csv | group status | reduce n = count(), too_big = sum(amount) | remove grouped | select status, n | sort status | json`)
		var loadedSpec SourceLoadSpec

		result, err := ExecuteSourceQuery(q, SourceInfo{
			Filename: "wide.csv",
			Load:     q.Source.Load,
			Schema:   fusedGroupReduceTDDOverflowSchema(),
		}, func(filename string, opts ast.LoadOptions, spec SourceLoadSpec) (*table.Table, error) {
			loadedSpec = spec
			return fusedGroupReduceTDDOverflowRows(t, spec), nil
		}, nil)
		if err != nil {
			t.Fatalf("dead reduce assignment overflow should not be evaluated: %v", err)
		}
		if !reflect.DeepEqual(loadedSpec.ReadColumns.Names(), []string{"status"}) || !reflect.DeepEqual(loadedSpec.OutputColumns.Names(), []string{"status"}) {
			t.Fatalf("source spec: got %#v, want only status", loadedSpec)
		}
		requireSourceProjectionTDDTableColumns(t, result, "status", "n")
		if result.NumRows != 1 || result.GetAt(0, result.ColIndex("n")).Int != 2 {
			t.Fatalf("result: got\n%s\nwant one group with n=2", result.String())
		}
	})

	t.Run("demanded_runtime_overflow_still_errors", func(t *testing.T) {
		q := parseSourceProjectionTDDQuery(t, `wide.csv | group status | reduce too_big = sum(amount) | remove grouped | select status, too_big | json`)
		_, err := ExecuteSourceQuery(q, SourceInfo{
			Filename: "wide.csv",
			Load:     q.Source.Load,
			Schema:   fusedGroupReduceTDDOverflowSchema(),
		}, func(filename string, opts ast.LoadOptions, spec SourceLoadSpec) (*table.Table, error) {
			return fusedGroupReduceTDDOverflowRows(t, spec), nil
		}, nil)
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "overflow") {
			t.Fatalf("demanded overflow error: got %v, want overflow", err)
		}
	})

	t.Run("dead_static_reduce_type_error_still_fails_planning", func(t *testing.T) {
		_, err := planPhysicalPipelineForTest(demandPruningTDDWideSchema(), parseSimplePlannerOps(t,
			`group status | reduce n = count(), bad = sum(name) | remove grouped | select status, n`,
		))
		if err == nil {
			t.Fatalf("expected static reduce type error for dead assignment")
		}
		msg := strings.ToLower(err.Error())
		for _, want := range []string{"reduce", "bad", "sum", "numeric"} {
			if !strings.Contains(msg, want) {
				t.Fatalf("planning error should contain %q, got %v", want, err)
			}
		}
	})
}

func TestFusedGroupReduceTDDUnsupportedShapesKeepCompatibilityPath(t *testing.T) {
	input := simplePlannerInputTable()

	t.Run("standalone_group_is_not_fused", func(t *testing.T) {
		plan, err := planPhysicalPipelineForTest(input.Schema(), parseSimplePlannerOps(t, `group city | select city`))
		if err != nil {
			t.Fatalf("plan standalone group: %v", err)
		}
		requireFusedGroupReduceTDDOpTypes(t, plan.Ops, "group", "select")
	})

	t.Run("explicit_existing_list_reduce_is_not_fused", func(t *testing.T) {
		plan, err := planPhysicalPipelineForTest(input.Schema(), parseSimplePlannerOps(t, `reduce orders total = sum(amount) | remove orders | select name, total`))
		if err != nil {
			t.Fatalf("plan existing-list reduce: %v", err)
		}
		requireFusedGroupReduceTDDOpTypes(t, plan.Ops, "reduce", "remove", "select")
	})
}

func requireFusedGroupReduceTDDOpTypes(t *testing.T, ops []plannedOp, want ...string) {
	t.Helper()
	got := demandPruningTDDOpTypes(ops)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("physical ops: got %v, want %v", got, want)
	}
}

func requireFusedGroupReduceTDDNoAdjacentMaterializedPair(t *testing.T, physical *physicalPipeline) {
	t.Helper()
	ops := demandPruningTDDOpTypes(physical.Ops)
	for i := 0; i+1 < len(ops); i++ {
		if ops[i] == "group" && ops[i+1] == "reduce" {
			t.Fatalf("physical ops: got adjacent materialized group/reduce pair %v; want fused group/reduce op", ops)
		}
	}
}

func requireFusedGroupReduceTDDNoPayloadOp(t *testing.T, plan *pipelinePlan) {
	t.Helper()
	for _, op := range plan.Ops {
		groupReduce, ok := op.(plannedGroupReduce)
		if !ok {
			continue
		}
		if groupReduce.materializeNested {
			t.Fatalf("group/reduce plan materializes nested payload; want no-payload path")
		}
		return
	}
	t.Fatalf("physical ops: got %v, want fused group/reduce op", demandPruningTDDOpTypes(plan.Ops))
}

func fusedGroupReduceTDDOverflowSchema() table.Schema {
	return table.Schema{Columns: []table.SchemaColumn{
		{Name: "status", Type: &table.TypeDescriptor{Kind: table.TypeString}},
		{Name: "amount", Type: &table.TypeDescriptor{Kind: table.TypeInt}},
	}}
}

func fusedGroupReduceTDDOverflowRows(t *testing.T, spec SourceLoadSpec) *table.Table {
	t.Helper()
	if spec.OutputColumns.IsAll() {
		tbl := table.NewTableWithSchemas(
			[]string{"status", "amount"},
			[]*table.TypeDescriptor{{Kind: table.TypeString}, {Kind: table.TypeInt}},
		)
		mustAddFusedGroupReduceTDDRow(t, tbl, table.StrVal("active"), table.IntVal(9223372036854775807))
		mustAddFusedGroupReduceTDDRow(t, tbl, table.StrVal("active"), table.IntVal(1))
		return tbl
	}
	switch names := spec.OutputColumns.Names(); {
	case reflect.DeepEqual(names, []string{"status"}):
		tbl := table.NewTableWithSchemas(
			[]string{"status"},
			[]*table.TypeDescriptor{{Kind: table.TypeString}},
		)
		mustAddFusedGroupReduceTDDRow(t, tbl, table.StrVal("active"))
		mustAddFusedGroupReduceTDDRow(t, tbl, table.StrVal("active"))
		return tbl
	case reflect.DeepEqual(names, []string{"status", "amount"}):
		tbl := table.NewTableWithSchemas(
			[]string{"status", "amount"},
			[]*table.TypeDescriptor{{Kind: table.TypeString}, {Kind: table.TypeInt}},
		)
		mustAddFusedGroupReduceTDDRow(t, tbl, table.StrVal("active"), table.IntVal(9223372036854775807))
		mustAddFusedGroupReduceTDDRow(t, tbl, table.StrVal("active"), table.IntVal(1))
		return tbl
	default:
		t.Fatalf("unexpected source output columns: %#v", names)
		return nil
	}
}

func mustAddFusedGroupReduceTDDRow(t *testing.T, tbl *table.Table, vals ...table.Value) {
	t.Helper()
	if err := tbl.AddRowTyped(vals); err != nil {
		t.Fatalf("add row: %v", err)
	}
}
