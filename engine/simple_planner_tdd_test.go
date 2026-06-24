package engine

import (
	"strings"
	"testing"

	"github.com/razeghi71/dq/ast"
	"github.com/razeghi71/dq/parser"
	"github.com/razeghi71/dq/table"
)

func parseSimplePlannerOps(t *testing.T, pipeline string) []ast.Op {
	t.Helper()
	q, err := parser.Parse("test.csv | " + pipeline)
	if err != nil {
		t.Fatalf("parse %q: %v", pipeline, err)
	}
	return q.Ops
}

func simplePlannerInputTable() *table.Table {
	return table.NewTableWithSchemas(
		[]string{"name", "age", "city", "active", "address", "address_city", "orders", "u", "profile"},
		[]*table.TypeDescriptor{
			{Kind: table.TypeString},
			{Kind: table.TypeInt},
			{Kind: table.TypeString},
			{Kind: table.TypeBool, Nullable: true},
			{
				Kind:     table.TypeRecord,
				Nullable: true,
				Fields: []table.FieldDescriptor{
					{Name: "city", Type: &table.TypeDescriptor{Kind: table.TypeString}},
					{Name: "zip", Type: &table.TypeDescriptor{Kind: table.TypeString}},
				},
			},
			{Kind: table.TypeString},
			{
				Kind: table.TypeList,
				Elem: &table.TypeDescriptor{
					Kind: table.TypeRecord,
					Fields: []table.FieldDescriptor{
						{Name: "amount", Type: &table.TypeDescriptor{Kind: table.TypeFloat}},
					},
				},
			},
			{
				Kind: table.TypeUnion,
				Branches: []*table.TypeDescriptor{
					{
						Kind: table.TypeRecord,
						Fields: []table.FieldDescriptor{
							{Name: "x", Type: &table.TypeDescriptor{Kind: table.TypeInt}},
						},
					},
					{Kind: table.TypeString},
				},
			},
			{
				Kind: table.TypeRecord,
				Fields: []table.FieldDescriptor{
					{Name: "stats", Type: &table.TypeDescriptor{Kind: table.TypeRecord, Fields: []table.FieldDescriptor{
						{Name: "score", Type: &table.TypeDescriptor{Kind: table.TypeFloat}},
					}}},
				},
			},
		},
	)
}

func requireSimplePlannerSchema(t *testing.T, got table.Schema, want ...string) {
	t.Helper()
	if len(got.Columns) != len(want) {
		t.Fatalf("schema columns: got %#v, want %v", got.Columns, want)
	}
	for i, spec := range want {
		parts := strings.SplitN(spec, ":", 2)
		if len(parts) != 2 {
			t.Fatalf("bad schema assertion %q", spec)
		}
		if got.Columns[i].Name != parts[0] {
			t.Fatalf("schema column %d name: got %q, want %q; schema=%#v", i, got.Columns[i].Name, parts[0], got.Columns)
		}
		if got.Columns[i].Type.String() != parts[1] {
			t.Fatalf("schema column %q type: got %s, want %s", parts[0], got.Columns[i].Type.String(), parts[1])
		}
	}
}

func requireSimplePlannerErrorContains(t *testing.T, input table.Schema, pipeline string, wants ...string) {
	t.Helper()
	_, err := planSchemaPipeline(input, parseSimplePlannerOps(t, pipeline))
	if err == nil {
		t.Fatalf("planSchemaPipeline(%q): expected error", pipeline)
	}
	msg := strings.ToLower(err.Error())
	for _, want := range wants {
		if !strings.Contains(msg, strings.ToLower(want)) {
			t.Fatalf("planSchemaPipeline(%q): got error %q, want substring %q", pipeline, err, want)
		}
	}
}

func TestSimplePlannerTDDPlansSchemaPreservingOpsWithoutRows(t *testing.T) {
	input := simplePlannerInputTable()

	for _, pipeline := range []string{
		"head 10",
		"tail 10",
		"filter { active }",
		"filter { city == \"NY\" }",
		"sort city, -age",
	} {
		t.Run(pipeline, func(t *testing.T) {
			plan, err := planSchemaPipeline(input.Schema(), parseSimplePlannerOps(t, pipeline))
			if err != nil {
				t.Fatalf("planSchemaPipeline(%q): %v", pipeline, err)
			}
			requireSimplePlannerSchema(t, plan.OutputSchema,
				"name:string",
				"age:int",
				"city:string",
				"active:bool?",
				"address:record<city:string, zip:string>?",
				"address_city:string",
				"orders:list<record<amount:float>>",
				"u:union<record<x:int>,string>",
				"profile:record<stats:record<score:float>>",
			)
			if len(plan.Ops) != 1 {
				t.Fatalf("planned op count: got %d, want 1", len(plan.Ops))
			}
			requireSimplePlannerSchema(t, plan.Ops[0].OutputSchema(), planSchemaSpecs(plan.OutputSchema)...)
		})
	}
}

func TestSimplePlannerTDDPlansShapeChangingSimpleOpsWithoutRows(t *testing.T) {
	input := simplePlannerInputTable()

	cases := []struct {
		name     string
		pipeline string
		want     []string
	}{
		{
			name:     "select_top_level_and_nested_paths",
			pipeline: "select name, address.city, profile.stats.score",
			want:     []string{"name:string", "address_city:string?", "profile_stats_score:float"},
		},
		{
			name:     "select_name_collision_suffixing",
			pipeline: "select address_city, address.city",
			want:     []string{"address_city:string", "address_city_2:string?"},
		},
		{
			name:     "rename_then_remove",
			pipeline: "rename name=person, city=location | remove age, orders",
			want: []string{
				"person:string",
				"location:string",
				"active:bool?",
				"address:record<city:string, zip:string>?",
				"address_city:string",
				"u:union<record<x:int>,string>",
				"profile:record<stats:record<score:float>>",
			},
		},
		{
			name:     "keyed_distinct_projects_keys",
			pipeline: "distinct city, age",
			want:     []string{"city:string", "age:int"},
		},
		{
			name:     "keyed_distinct_nested_path_nullable_parent",
			pipeline: "distinct address.city",
			want:     []string{"address_city:string?"},
		},
		{
			name:     "unkeyed_distinct_preserves_full_schema",
			pipeline: "distinct",
			want: []string{
				"name:string",
				"age:int",
				"city:string",
				"active:bool?",
				"address:record<city:string, zip:string>?",
				"address_city:string",
				"orders:list<record<amount:float>>",
				"u:union<record<x:int>,string>",
				"profile:record<stats:record<score:float>>",
			},
		},
		{
			name:     "count_schema",
			pipeline: "count",
			want:     []string{"count:int"},
		},
		{
			name:     "describe_schema",
			pipeline: "describe",
			want:     []string{"column:string", "type:string", "row_count:int", "schema:string"},
		},
		{
			name:     "downstream_sees_previous_simple_op_schema",
			pipeline: "select city, age | distinct city | sort city | count",
			want:     []string{"count:int"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := planSchemaPipeline(input.Schema(), parseSimplePlannerOps(t, tc.pipeline))
			if err != nil {
				t.Fatalf("planSchemaPipeline(%q): %v", tc.pipeline, err)
			}
			requireSimplePlannerSchema(t, plan.OutputSchema, tc.want...)
			if len(plan.Ops) != len(parseSimplePlannerOps(t, tc.pipeline)) {
				t.Fatalf("planned op count: got %d, want raw op count", len(plan.Ops))
			}
		})
	}
}

func TestSimplePlannerTDDHandlesNullOnlyColumnsAcrossSimpleOps(t *testing.T) {
	tbl := table.NewTable([]string{"nilcol", "name"})
	tbl.AddRow([]table.Value{table.Null(), table.StrVal("Alice")})
	tbl.AddRow([]table.Value{table.Null(), table.StrVal("Bob")})

	result := runQuery(t, tbl, `filter { list_contains(nilcol, "x") } | select nilcol | describe`)
	assertDescribeSchemaRows(t, result, map[string]describeSchemaMeta{
		"nilcol": {typ: "null", rows: 0, schema: "string?"},
	})

	staged := runQuery(t, tbl, `head 1 | filter { list_contains(nilcol, "x") } | select nilcol | describe`)
	assertDescribeSchemaRows(t, staged, map[string]describeSchemaMeta{
		"nilcol": {typ: "null", rows: 0, schema: "string?"},
	})

	distinct := runQuery(t, tbl, `distinct nilcol | describe`)
	assertDescribeSchemaRows(t, distinct, map[string]describeSchemaMeta{
		"nilcol": {typ: "string", rows: 1, schema: "string?"},
	})
}

func TestSimplePlannerTDDExecutionUsesBoundSortColumnForMatchingSchema(t *testing.T) {
	plannedInput := table.NewTableWithSchemas(
		[]string{"name", "age"},
		[]*table.TypeDescriptor{{Kind: table.TypeString}, {Kind: table.TypeString}},
	)
	plan, err := planSchemaPipeline(plannedInput.Schema(), parseSimplePlannerOps(t, "sort age"))
	if err != nil {
		t.Fatalf("planSchemaPipeline: %v", err)
	}

	input := table.NewTableWithSchemas(
		[]string{"name", "age"},
		[]*table.TypeDescriptor{{Kind: table.TypeString}, {Kind: table.TypeString}},
	)
	input.AddRow([]table.Value{table.StrVal("second"), table.StrVal("b")})
	input.AddRow([]table.Value{table.StrVal("first"), table.StrVal("a")})

	result, err := executePlan(plan, input)
	if err != nil {
		t.Fatalf("executePlan: %v", err)
	}
	if got := result.GetAt(0, 0); got.Type != table.TypeString || got.Str != "first" {
		t.Fatalf("first row name: got %v, want first", got)
	}
}

func TestSimplePlannerTDDExecutionRejectsSwappedSchemaBeforeUsingBoundIndexes(t *testing.T) {
	plannedInput := table.NewTableWithSchemas(
		[]string{"name", "age"},
		[]*table.TypeDescriptor{{Kind: table.TypeString}, {Kind: table.TypeString}},
	)
	plan, err := planSchemaPipeline(plannedInput.Schema(), parseSimplePlannerOps(t, "sort age"))
	if err != nil {
		t.Fatalf("planSchemaPipeline: %v", err)
	}

	swapped := table.NewTableWithSchemas(
		[]string{"age", "name"},
		[]*table.TypeDescriptor{{Kind: table.TypeString}, {Kind: table.TypeString}},
	)
	swapped.AddRow([]table.Value{table.StrVal("b"), table.StrVal("1")})
	swapped.AddRow([]table.Value{table.StrVal("a"), table.StrVal("2")})

	_, err = executePlan(plan, swapped)
	if err == nil {
		t.Fatal("expected executePlan schema mismatch error")
	}
	msg := strings.ToLower(err.Error())
	for _, want := range []string{"execute plan", "column 0", "name", "age"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error: got %q, want substring %q", err, want)
		}
	}
}

func TestSimplePlannerTDDPlannedExecutionRejectsMismatchedInputSchema(t *testing.T) {
	input := table.NewTableWithSchemas(
		[]string{"name", "age"},
		[]*table.TypeDescriptor{{Kind: table.TypeString}, {Kind: table.TypeInt}},
	)
	plan, err := planSchemaPipeline(input.Schema(), parseSimplePlannerOps(t, "select age"))
	if err != nil {
		t.Fatalf("planSchemaPipeline: %v", err)
	}

	cases := []struct {
		name  string
		input *table.Table
		wants []string
	}{
		{
			name: "same_count_wrong_name",
			input: table.NewTableWithSchemas(
				[]string{"name", "city"},
				[]*table.TypeDescriptor{{Kind: table.TypeString}, {Kind: table.TypeString}},
			),
			wants: []string{"execute plan", "column 1", "age", "city"},
		},
		{
			name: "same_name_wrong_type",
			input: table.NewTableWithSchemas(
				[]string{"name", "age"},
				[]*table.TypeDescriptor{{Kind: table.TypeString}, {Kind: table.TypeString}},
			),
			wants: []string{"execute plan", "age", "int", "string"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := executePlan(plan, tc.input)
			if err == nil {
				t.Fatal("expected executePlan schema mismatch error")
			}
			msg := strings.ToLower(err.Error())
			for _, want := range tc.wants {
				if !strings.Contains(msg, strings.ToLower(want)) {
					t.Fatalf("error: got %q, want substring %q", err, want)
				}
			}
		})
	}
}

func TestSimplePlannerTDDRejectsInvalidSimpleOpsBeforeRows(t *testing.T) {
	input := simplePlannerInputTable().Schema()

	cases := []struct {
		name     string
		pipeline string
		wants    []string
	}{
		{name: "filter_non_bool", pipeline: "filter { age }", wants: []string{"filter", "bool"}},
		{name: "select_missing_top_level", pipeline: "select missing", wants: []string{"select", "missing", "not found"}},
		{name: "select_missing_nested", pipeline: "select address.missing", wants: []string{"select", "missing", "not found"}},
		{name: "select_list_traversal", pipeline: "select orders.amount", wants: []string{"orders", "list"}},
		{name: "select_union_branch_traversal", pipeline: "select u.x", wants: []string{"u.x", "union"}},
		{name: "sort_record", pipeline: "sort address", wants: []string{"sort", "record", "not orderable"}},
		{name: "sort_list", pipeline: "sort orders", wants: []string{"sort", "list", "not orderable"}},
		{name: "sort_union", pipeline: "sort u", wants: []string{"sort", "union", "not orderable"}},
		{name: "sort_missing_after_select", pipeline: "select city | sort age", wants: []string{"sort", "age", "not found"}},
		{name: "rename_missing", pipeline: "rename missing=out", wants: []string{"rename", "missing", "not found"}},
		{name: "rename_same_column_twice", pipeline: "rename name=person, name=label", wants: []string{"rename", "name", "more than once"}},
		{name: "rename_duplicate_result", pipeline: "rename name=city", wants: []string{"rename", "duplicate", "city"}},
		{name: "remove_missing", pipeline: "remove missing", wants: []string{"remove", "missing", "not found"}},
		{name: "distinct_missing_after_projection", pipeline: "select city | distinct age", wants: []string{"distinct", "age", "not found"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			requireSimplePlannerErrorContains(t, input, tc.pipeline, tc.wants...)
		})
	}
}

func TestSimplePlannerTDDRejectsMalformedRemoveDotPathAST(t *testing.T) {
	input := simplePlannerInputTable().Schema()
	_, err := planSchemaPipeline(input, []ast.Op{&ast.RemoveOp{Columns: [][]string{{"address", "city"}}}})
	if err == nil {
		t.Fatal("expected remove dot-path planning error")
	}
	for _, want := range []string{"remove", "dot paths", "address.city"} {
		if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(want)) {
			t.Fatalf("error: got %q, want substring %q", err, want)
		}
	}
}

func TestSimplePlannerTDDPlannedExecutionUsesPlannedMetadata(t *testing.T) {
	input := simplePlannerInputTable()
	plan, err := planSchemaPipeline(input.Schema(), parseSimplePlannerOps(t, "filter { false } | select address.city, city | distinct address_city, city | describe"))
	if err != nil {
		t.Fatalf("planSchemaPipeline: %v", err)
	}
	requireSimplePlannerSchema(t, plan.OutputSchema,
		"column:string",
		"type:string",
		"row_count:int",
		"schema:string",
	)

	result, err := executePlan(plan, input)
	if err != nil {
		t.Fatalf("executePlan: %v", err)
	}
	assertDescribeSchemaRows(t, result, map[string]describeSchemaMeta{
		"address_city": {typ: "string", rows: 0, schema: "string?"},
		"city":         {typ: "string", rows: 0, schema: "string"},
	})
}

func TestSimplePlannerTDDExecutionUsesPlannedOutputSchemaAsAuthority(t *testing.T) {
	input := table.NewTableWithSchemas(
		[]string{"name", "age"},
		[]*table.TypeDescriptor{{Kind: table.TypeString}, {Kind: table.TypeInt}},
	)
	if err := input.AddRowTyped([]table.Value{table.StrVal("Alice"), table.IntVal(30)}); err != nil {
		t.Fatalf("AddRowTyped: %v", err)
	}

	projections, err := planProjectionsInEnv("select", [][]string{{"age"}}, schemaEnvFromTable(input))
	if err != nil {
		t.Fatalf("planProjectionsInEnv: %v", err)
	}
	selected, err := execPlannedSelect(plannedSelect{
		plannedBase: plannedBase{output: table.Schema{Columns: []table.SchemaColumn{
			{Name: "years", Type: &table.TypeDescriptor{Kind: table.TypeInt}},
		}}},
		projections: projections,
	}, input)
	if err != nil {
		t.Fatalf("execPlannedSelect: %v", err)
	}
	assertColumnsExact(t, selected, "years")
	requireSimplePlannerSchema(t, selected.Schema(), "years:int")
}

func TestSimplePlannerTDDFixedOutputOpsUseCanonicalSchemas(t *testing.T) {
	input := simplePlannerInputTable()

	countPlan, err := planSchemaPipeline(input.Schema(), parseSimplePlannerOps(t, "count"))
	if err != nil {
		t.Fatalf("plan count: %v", err)
	}
	requireSimplePlannerSchema(t, countPlan.OutputSchema, "count:int")

	counted, err := executePlan(countPlan, input)
	if err != nil {
		t.Fatalf("execute count: %v", err)
	}
	assertColumnsExact(t, counted, "count")
	requireSimplePlannerSchema(t, counted.Schema(), "count:int")

	describePlan, err := planSchemaPipeline(input.Schema(), parseSimplePlannerOps(t, "describe"))
	if err != nil {
		t.Fatalf("plan describe: %v", err)
	}
	requireSimplePlannerSchema(t, describePlan.OutputSchema, "column:string", "type:string", "row_count:int", "schema:string")

	described, err := executePlan(describePlan, input)
	if err != nil {
		t.Fatalf("execute describe: %v", err)
	}
	assertColumnsExact(t, described, "column", "type", "row_count", "schema")
	requireSimplePlannerSchema(t, described.Schema(), "column:string", "type:string", "row_count:int", "schema:string")
}

func TestSimplePlannerTDDExecutePlansFreshForChangedInputSchema(t *testing.T) {
	q, err := parser.Parse("test.csv | filter { score > 1 } | count")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	intInput := table.NewTableWithSchemas(
		[]string{"score"},
		[]*table.TypeDescriptor{{Kind: table.TypeInt}},
	)
	if err := intInput.AddRowTyped([]table.Value{table.IntVal(2)}); err != nil {
		t.Fatalf("AddRowTyped int input: %v", err)
	}
	if _, err := Execute(q, intInput, nil); err != nil {
		t.Fatalf("Execute int input: %v", err)
	}

	floatInput := table.NewTableWithSchemas(
		[]string{"score"},
		[]*table.TypeDescriptor{{Kind: table.TypeFloat}},
	)
	if err := floatInput.AddRowTyped([]table.Value{table.FloatVal(2.5)}); err != nil {
		t.Fatalf("AddRowTyped float input: %v", err)
	}
	result, err := Execute(q, floatInput, nil)
	if err != nil {
		t.Fatalf("Execute float input after cached int plan: %v", err)
	}
	if got := result.GetAt(0, 0); got.Type != table.TypeInt || got.Int != 1 {
		t.Fatalf("count after replan: got %v, want int 1", got)
	}
}

func TestSimplePlannerTDDExecutePlansFreshForRawNullVsNullableString(t *testing.T) {
	q, err := parser.Parse("test.csv | filter { x } | count")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	nullOnly := table.NewTable([]string{"x"})
	nullOnly.AddRow([]table.Value{table.Null()})
	if _, err := Execute(q, nullOnly, nil); err != nil {
		t.Fatalf("Execute null-only input: %v", err)
	}

	stringInput := table.NewTableWithSchemas(
		[]string{"x"},
		[]*table.TypeDescriptor{{Kind: table.TypeString, Nullable: true}},
	)
	_, err = Execute(q, stringInput, nil)
	if err == nil {
		t.Fatal("Execute nullable string input after cached null-only plan: expected planning error")
	}
	if !strings.Contains(err.Error(), "filter expression must return bool") {
		t.Fatalf("Execute nullable string input: got error %q, want filter bool planning error", err)
	}
}

func TestSimplePlannerTDDExecuteDoesNotReuseCachedPlanAfterQueryMutation(t *testing.T) {
	q, err := parser.Parse("test.csv | filter { active } | count")
	if err != nil {
		t.Fatalf("parse original: %v", err)
	}
	input := table.NewTableWithSchemas(
		[]string{"active"},
		[]*table.TypeDescriptor{{Kind: table.TypeBool}},
	)
	if err := input.AddRowTyped([]table.Value{table.BoolVal(true)}); err != nil {
		t.Fatalf("AddRowTyped input: %v", err)
	}
	if _, err := Execute(q, input, nil); err != nil {
		t.Fatalf("Execute original query: %v", err)
	}

	mutated, err := parser.Parse("test.csv | filter { missing } | count")
	if err != nil {
		t.Fatalf("parse mutated: %v", err)
	}
	q.Ops[0] = mutated.Ops[0]

	_, err = Execute(q, input, nil)
	if err == nil {
		t.Fatal("Execute mutated query after cached original plan: expected planning error")
	}
	if !strings.Contains(err.Error(), `column "missing" not found`) {
		t.Fatalf("Execute mutated query: got error %q, want missing column planning error", err)
	}
}

func planSchemaSpecs(schema table.Schema) []string {
	specs := make([]string, len(schema.Columns))
	for i, col := range schema.Columns {
		specs[i] = col.Name + ":" + col.Type.String()
	}
	return specs
}
