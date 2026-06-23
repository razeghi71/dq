package engine

import (
	"strings"
	"testing"

	"github.com/razeghi71/dq/table"
)

func TestGroupReduceSpanPlannerTDDPlansGroupReduceInsideOneSchemaPipeline(t *testing.T) {
	input := simplePlannerInputTable()

	plan, err := planSchemaPipeline(input.Schema(), parseSimplePlannerOps(t, `filter { age > 20 } | transform bucket = if(active, "active", "inactive") | group bucket | reduce total = sum(age), avg_age = avg(age), n = count(), first_name = first(name) | remove grouped | sort bucket | select bucket, total, avg_age, n, first_name`))
	if err != nil {
		t.Fatalf("planSchemaPipeline with group/reduce span: %v", err)
	}
	if got, want := len(plan.Ops), len(parseSimplePlannerOps(t, `filter { age > 20 } | transform bucket = if(active, "active", "inactive") | group bucket | reduce total = sum(age), avg_age = avg(age), n = count(), first_name = first(name) | remove grouped | sort bucket | select bucket, total, avg_age, n, first_name`)); got != want {
		t.Fatalf("planned op count: got %d, want %d", got, want)
	}
	requireSimplePlannerSchema(t, plan.OutputSchema,
		"bucket:string",
		"total:int?",
		"avg_age:float?",
		"n:int",
		"first_name:string?",
	)
}

func TestGroupReduceSpanPlannerTDDPlansGroupOutputSchemaWithoutRows(t *testing.T) {
	input := simplePlannerInputTable()

	plan, err := planSchemaPipeline(input.Schema(), parseSimplePlannerOps(t, `filter { false } | group city | describe`))
	if err != nil {
		t.Fatalf("planSchemaPipeline with zero-row group: %v", err)
	}
	requireSimplePlannerSchema(t, plan.Ops[1].OutputSchema(),
		"city:string",
		"grouped:list<record<active:bool?, address:record<city:string, zip:string>?, address_city:string, age:int, city:string, name:string, orders:list<record<amount:float>>, profile:record<stats:record<score:float>>, u:union<record<x:int>,string>>>",
	)
	requireSimplePlannerSchema(t, plan.OutputSchema,
		"column:string",
		"type:string",
		"row_count:int",
		"schema:string",
	)
}

func TestGroupReduceSpanPlannerTDDPlansCustomNestedNameAndNestedPaths(t *testing.T) {
	input := simplePlannerInputTable()

	plan, err := planSchemaPipeline(input.Schema(), parseSimplePlannerOps(t, `group address.city, city as entries | reduce entries avg_age = avg(age), max_score = max(profile.stats.score), first_city = first(address.city) | remove entries | select address_city, city, avg_age, max_score, first_city`))
	if err != nil {
		t.Fatalf("planSchemaPipeline with custom nested group/reduce: %v", err)
	}
	requireSimplePlannerSchema(t, plan.OutputSchema,
		"address_city:string?",
		"city:string",
		"avg_age:float?",
		"max_score:float?",
		"first_city:string?",
	)
}

func TestGroupReduceSpanPlannerTDDRejectsNestedNameCollision(t *testing.T) {
	input := table.NewTableWithSchemas(
		[]string{"grouped", "city"},
		[]*table.TypeDescriptor{{Kind: table.TypeString}, {Kind: table.TypeString}},
	).Schema()

	cases := []struct {
		name     string
		pipeline string
		wants    []string
	}{
		{
			name:     "default_grouped_name_collides_with_key",
			pipeline: `group grouped | reduce n = count()`,
			wants:    []string{"group", "nested column name", "grouped", "collides", "as"},
		},
		{
			name:     "explicit_nested_name_collides_with_key",
			pipeline: `group city as city | reduce city n = count()`,
			wants:    []string{"group", "nested column name", "city", "collides", "as"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := planSchemaPipeline(input, parseSimplePlannerOps(t, tc.pipeline))
			if err == nil {
				t.Fatalf("planSchemaPipeline(%q): expected nested-name collision error", tc.pipeline)
			}
			msg := strings.ToLower(err.Error())
			for _, want := range tc.wants {
				if !strings.Contains(msg, strings.ToLower(want)) {
					t.Fatalf("planSchemaPipeline(%q): got error %q, want substring %q", tc.pipeline, err, want)
				}
			}
		})
	}
}

func TestGroupReduceSpanPlannerTDDAllowsDistinctNestedNameForGroupedKey(t *testing.T) {
	input := table.NewTableWithSchemas(
		[]string{"grouped", "value"},
		[]*table.TypeDescriptor{{Kind: table.TypeString}, {Kind: table.TypeInt}},
	)
	if err := input.AddRowTyped([]table.Value{table.StrVal("a"), table.IntVal(1)}); err != nil {
		t.Fatalf("add row 1: %v", err)
	}
	if err := input.AddRowTyped([]table.Value{table.StrVal("a"), table.IntVal(2)}); err != nil {
		t.Fatalf("add row 2: %v", err)
	}
	if err := input.AddRowTyped([]table.Value{table.StrVal("b"), table.IntVal(3)}); err != nil {
		t.Fatalf("add row 3: %v", err)
	}

	out := runQuery(t, input, `group grouped as rows | reduce rows n = count() | remove rows | sort grouped | select grouped, n`)
	requireSimplePlannerSchema(t, out.Schema(), "grouped:string", "n:int")
	if got, want := out.NumRows, 2; got != want {
		t.Fatalf("row count: got %d, want %d", got, want)
	}
	if got, want := out.GetAt(0, out.ColIndex("n")).Int, int64(2); got != want {
		t.Fatalf("first grouped count: got %d, want %d", got, want)
	}
	if got, want := out.GetAt(1, out.ColIndex("n")).Int, int64(1); got != want {
		t.Fatalf("second grouped count: got %d, want %d", got, want)
	}
}

func TestGroupReduceSpanPlannerTDDPlansExplicitReduceOverExistingListColumn(t *testing.T) {
	input := simplePlannerInputTable()

	plan, err := planSchemaPipeline(input.Schema(), parseSimplePlannerOps(t, `reduce orders total = sum(amount), n = count(), first_amount = first(amount) | remove orders | select name, total, n, first_amount`))
	if err != nil {
		t.Fatalf("planSchemaPipeline with explicit list reduce: %v", err)
	}
	requireSimplePlannerSchema(t, plan.OutputSchema,
		"name:string",
		"total:float?",
		"n:int",
		"first_amount:float?",
	)
}

func TestGroupReduceSpanPlannerTDDDownstreamOpsBindReduceOutputInSamePlan(t *testing.T) {
	input := simplePlannerInputTable().Schema()

	cases := []struct {
		name     string
		pipeline string
		want     []string
	}{
		{
			name:     "filter_sort_select_reduce_outputs",
			pipeline: `group city | reduce total = sum(age), n = count() | remove grouped | filter { total > 40 and n > 0 } | sort -total | select city, total, n`,
			want:     []string{"city:string", "total:int?", "n:int"},
		},
		{
			name:     "transform_after_reduce_sees_aggregate_schema",
			pipeline: `group city | reduce total = sum(age), n = count() | remove grouped | transform avg = total / n | select city, avg`,
			want:     []string{"city:string", "avg:float?"},
		},
		{
			name:     "distinct_projects_reduce_output",
			pipeline: `group city | reduce first_name = first(name) | remove grouped | distinct first_name`,
			want:     []string{"first_name:string?"},
		},
		{
			name:     "describe_after_reduce_uses_planned_schema",
			pipeline: `filter { false } | group city | reduce total = sum(age), n = count() | remove grouped | describe`,
			want:     []string{"column:string", "type:string", "row_count:int", "schema:string"},
		},
		{
			name:     "reduce_assignment_overwrites_existing_column_schema",
			pipeline: `group city | reduce city = count() | remove grouped | select city`,
			want:     []string{"city:int"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := planSchemaPipeline(input, parseSimplePlannerOps(t, tc.pipeline))
			if err != nil {
				t.Fatalf("planSchemaPipeline(%q): %v", tc.pipeline, err)
			}
			requireSimplePlannerSchema(t, plan.OutputSchema, tc.want...)
		})
	}
}

func TestGroupReduceSpanPlannerTDDReduceOverwritesExistingColumn(t *testing.T) {
	input := table.NewTableWithSchemas(
		[]string{"city", "age"},
		[]*table.TypeDescriptor{{Kind: table.TypeString}, {Kind: table.TypeInt}},
	)
	if err := input.AddRowTyped([]table.Value{table.StrVal("NY"), table.IntVal(30)}); err != nil {
		t.Fatalf("add row 1: %v", err)
	}
	if err := input.AddRowTyped([]table.Value{table.StrVal("NY"), table.IntVal(25)}); err != nil {
		t.Fatalf("add row 2: %v", err)
	}
	if err := input.AddRowTyped([]table.Value{table.StrVal("LA"), table.IntVal(28)}); err != nil {
		t.Fatalf("add row 3: %v", err)
	}

	out := runQuery(t, input, `group city | reduce city = count() | remove grouped | sort city | select city`)
	requireSimplePlannerSchema(t, out.Schema(), "city:int")
	if got, want := out.NumRows, 2; got != want {
		t.Fatalf("row count: got %d, want %d", got, want)
	}
	if got, want := out.GetAt(0, out.ColIndex("city")).Int, int64(1); got != want {
		t.Fatalf("first overwritten city count: got %d, want %d", got, want)
	}
	if got, want := out.GetAt(1, out.ColIndex("city")).Int, int64(2); got != want {
		t.Fatalf("second overwritten city count: got %d, want %d", got, want)
	}
}

func TestGroupReduceSpanPlannerTDDRejectsInvalidSchemasBeforeRows(t *testing.T) {
	input := simplePlannerInputTable().Schema()

	cases := []struct {
		name     string
		pipeline string
		wants    []string
	}{
		{name: "missing_group_key", pipeline: `filter { false } | group missing | count`, wants: []string{"group", "missing", "not found"}},
		{name: "group_cannot_step_through_list", pipeline: `filter { false } | group orders.amount | count`, wants: []string{"orders", "list"}},
		{name: "reduce_without_grouped_column", pipeline: `filter { false } | reduce total = sum(age) | count`, wants: []string{"reduce", "grouped", "not found"}},
		{name: "reduce_scalar_nested_source", pipeline: `filter { false } | reduce age n = count() | count`, wants: []string{"reduce", "age", "list", "record"}},
		{name: "sum_rejects_string_column", pipeline: `filter { false } | group city | reduce bad = sum(name) | count`, wants: []string{"reduce", "bad", "sum", "numeric"}},
		{name: "aggregate_arg_must_be_column_path", pipeline: `filter { false } | group city | reduce bad = sum(age + 1) | count`, wants: []string{"sum", "column reference"}},
		{name: "scalar_function_rejected_in_reduce", pipeline: `filter { false } | group city | reduce bad = upper(name) | count`, wants: []string{"upper", "reduce"}},
		{name: "duplicate_reduce_target", pipeline: `filter { false } | group city | reduce x = count(), x = sum(age) | count`, wants: []string{"reduce target", "x", "more than once"}},
		{name: "sibling_reduce_assignment_not_visible", pipeline: `filter { false } | group city | reduce total = sum(age), doubled = total * 2 | count`, wants: []string{"total", "reduce"}},
		{name: "reduce_cannot_step_through_list", pipeline: `filter { false } | group city | reduce bad = first(orders.amount) | count`, wants: []string{"orders", "list"}},
		{name: "reduce_rejects_union_for_numeric_aggregate", pipeline: `filter { false } | group city | reduce bad = sum(u) | count`, wants: []string{"sum", "numeric"}},
		{name: "downstream_missing_reduce_output", pipeline: `filter { false } | group city | reduce total = sum(age) | select missing | count`, wants: []string{"missing", "not found"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := planSchemaPipeline(input, parseSimplePlannerOps(t, tc.pipeline))
			if err == nil {
				t.Fatalf("planSchemaPipeline(%q): expected error", tc.pipeline)
			}
			msg := strings.ToLower(err.Error())
			for _, want := range tc.wants {
				if !strings.Contains(msg, strings.ToLower(want)) {
					t.Fatalf("planSchemaPipeline(%q): got error %q, want substring %q", tc.pipeline, err, want)
				}
			}
		})
	}
}

func TestGroupReduceSpanPlannerTDDDownstreamPlanningErrorWinsBeforeAggregateRuntime(t *testing.T) {
	input := table.NewTableWithSchemas(
		[]string{"g", "id"},
		[]*table.TypeDescriptor{{Kind: table.TypeString}, {Kind: table.TypeInt}},
	)
	if err := input.AddRowTyped([]table.Value{table.StrVal("a"), table.IntVal(9223372036854775807)}); err != nil {
		t.Fatalf("add overflow row 1: %v", err)
	}
	if err := input.AddRowTyped([]table.Value{table.StrVal("a"), table.IntVal(1)}); err != nil {
		t.Fatalf("add overflow row 2: %v", err)
	}

	q := parseSimplePlannerOps(t, `group g | reduce total = sum(id) | select missing`)
	plan, err := planSchemaPipeline(input.Schema(), q)
	if err == nil {
		t.Fatalf("planSchemaPipeline: expected downstream missing-column error")
	}
	msg := strings.ToLower(err.Error())
	for _, want := range []string{"missing", "not found"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("planSchemaPipeline error: got %q, want %q", err, want)
		}
	}
	if plan != nil {
		t.Fatalf("planSchemaPipeline returned plan on error: %#v", plan)
	}
}
