package engine

import (
	"strings"
	"testing"

	"github.com/razeghi71/dq/table"
)

func TestTransformPlannerTDDPlansTransformInsideSimplePipelineWithoutRows(t *testing.T) {
	input := simplePlannerInputTable()

	plan, err := planSchemaPipeline(input.Schema(), parseSimplePlannerOps(t, `filter { age > 20 } | transform age2 = age + 1, label = upper(name), ratio = age / 2, profile = struct(name = name, city = city), tags = list(city, null) | filter { age2 > 30 and label is not null } | select name, age2, label, ratio, profile, tags`))
	if err != nil {
		t.Fatalf("planSchemaPipeline with transform: %v", err)
	}
	if len(plan.Ops) != 4 {
		t.Fatalf("planned op count: got %d, want 4", len(plan.Ops))
	}
	requireSimplePlannerSchema(t, plan.OutputSchema,
		"name:string",
		"age2:int",
		"label:string",
		"ratio:float",
		"profile:record<city:string, name:string>",
		"tags:list<string?>",
	)
	requireSimplePlannerSchema(t, plan.Ops[1].OutputSchema(),
		"name:string",
		"age:int",
		"city:string",
		"active:bool?",
		"address:record<city:string, zip:string>?",
		"address_city:string",
		"orders:list<record<amount:float>>",
		"u:union<record<x:int>,string>",
		"profile:record<city:string, name:string>",
		"age2:int",
		"label:string",
		"ratio:float",
		"tags:list<string?>",
	)
}

func TestTransformPlannerTDDPreservesOverwriteAndAppendOrder(t *testing.T) {
	input := simplePlannerInputTable()

	plan, err := planSchemaPipeline(input.Schema(), parseSimplePlannerOps(t, `transform age = age / 2, city = upper(city), name_len = str_len(name) | select name, age, city, name_len`))
	if err != nil {
		t.Fatalf("planSchemaPipeline with overwrite transform: %v", err)
	}
	requireSimplePlannerSchema(t, plan.OutputSchema,
		"name:string",
		"age:float",
		"city:string",
		"name_len:int",
	)
}

func TestTransformPlannerTDDDownstreamOpsBindTransformOutputInSamePlan(t *testing.T) {
	input := simplePlannerInputTable().Schema()

	cases := []struct {
		name     string
		pipeline string
		want     []string
	}{
		{
			name:     "filter_select_sort_see_new_column",
			pipeline: `transform age2 = age + 1 | filter { age2 > 30 } | sort -age2 | select age2`,
			want:     []string{"age2:int"},
		},
		{
			name:     "distinct_projects_transform_output",
			pipeline: `transform bucket = if(active, "on", "off") | distinct bucket | sort bucket`,
			want:     []string{"bucket:string"},
		},
		{
			name:     "describe_uses_transform_schema",
			pipeline: `transform age2 = age + 1, maybe = coalesce(null, age) | select age2, maybe | describe`,
			want:     []string{"column:string", "type:string", "row_count:int", "schema:string"},
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

func TestTransformPlannerTDDRejectsInvalidTransformAndDownstreamSchemasBeforeRows(t *testing.T) {
	input := simplePlannerInputTable().Schema()

	cases := []struct {
		name     string
		pipeline string
		wants    []string
	}{
		{name: "bad_function_argument", pipeline: `transform bad = upper(age)`, wants: []string{"transform", "bad", "upper", "string", "int"}},
		{name: "incompatible_if_branches", pipeline: `transform out = if(active, age, city)`, wants: []string{"transform", "out", "if", "common type"}},
		{name: "incompatible_coalesce_arguments", pipeline: `transform out = coalesce(age, "missing")`, wants: []string{"transform", "out", "coalesce", "common type"}},
		{name: "duplicate_new_target", pipeline: `transform x = 1, x = 2`, wants: []string{"transform target", "x", "more than once"}},
		{name: "duplicate_overwrite_target", pipeline: `transform age = age + 1, age = age + 2`, wants: []string{"transform target", "age", "more than once"}},
		{name: "sibling_assignment_not_visible", pipeline: `transform age2 = age + 1, age3 = age2 + 1`, wants: []string{"age2", "not found"}},
		{name: "previous_projection_controls_transform_input", pipeline: `select name | transform age2 = age + 1`, wants: []string{"age", "not found"}},
		{name: "downstream_missing_transform_output", pipeline: `transform city2 = upper(city) | select name | filter { city2 == "NY" }`, wants: []string{"city2", "not found"}},
		{name: "transform_cannot_step_through_list", pipeline: `transform amount = orders.amount`, wants: []string{"orders", "list"}},
		{name: "transform_cannot_step_through_union", pipeline: `transform x = u.x`, wants: []string{"u.x", "union"}},
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

func TestTransformPlannerTDDExecutesPlannedTransformPipeline(t *testing.T) {
	plan, err := planSchemaPipeline(usersTable().Schema(), parseSimplePlannerOps(t, `transform age2 = age + 1, bucket = if(age > 30, "senior", "standard") | filter { age2 > 31 } | select name, bucket | sort name`))
	if err != nil {
		t.Fatalf("planSchemaPipeline with executable transform: %v", err)
	}

	result, err := executePlan(plan, usersTable())
	if err != nil {
		t.Fatalf("executePlan with transform: %v", err)
	}
	requireSimplePlannerSchema(t, result.Schema(), "name:string", "bucket:string")
	if result.NumRows != 2 {
		t.Fatalf("row count: got %d, want 2; result=\n%s", result.NumRows, result.String())
	}
	want := []struct {
		name   string
		bucket string
	}{
		{"Charlie", "senior"},
		{"Frank", "senior"},
	}
	for i := range want {
		if got := result.GetAt(i, 0); got.Type != table.TypeString || got.Str != want[i].name {
			t.Fatalf("row %d name: got %v, want %q", i, got, want[i].name)
		}
		if got := result.GetAt(i, 1); got.Type != table.TypeString || got.Str != want[i].bucket {
			t.Fatalf("row %d bucket: got %v, want %q", i, got, want[i].bucket)
		}
	}
}
