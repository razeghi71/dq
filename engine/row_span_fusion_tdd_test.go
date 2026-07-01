package engine

import (
	"reflect"
	"strings"
	"testing"

	"github.com/razeghi71/dq/ast"
	"github.com/razeghi71/dq/table"
)

func TestRowSpanFusionTDDOptimizedLogicalMaximalRunsAndBarriers(t *testing.T) {
	cases := []struct {
		name     string
		pipeline string
		want     []string
	}{
		{
			name:     "maximal_row_local_run",
			pipeline: `filter { age > 30 } | select name, age | transform decade = age / 10`,
			want:     []string{"row_span(filter,select,transform)"},
		},
		{
			name:     "sort_splits_spans",
			pipeline: `filter { age > 30 } | sort name | select name`,
			want:     []string{"filter", "sort", "select"},
		},
		{
			name:     "head_splits_spans_and_keeps_suffix_row_local",
			pipeline: `filter { age > 20 } | head 1 | transform age2 = age + 1 | select name, age2`,
			want:     []string{"filter", "head", "row_span(transform,select)"},
		},
		{
			name:     "count_splits_spans",
			pipeline: `filter { age > 20 } | count | transform c2 = count + 1`,
			want:     []string{"filter", "count", "transform"},
		},
		{
			name:     "describe_splits_spans",
			pipeline: `filter { age > 20 } | describe | select column, schema`,
			want:     []string{"filter", "describe", "select"},
		},
		{
			name:     "distinct_splits_spans",
			pipeline: `filter { age > 20 } | distinct city | transform city2 = city`,
			want:     []string{"filter", "distinct", "transform"},
		},
		{
			name:     "tail_splits_spans",
			pipeline: `filter { age > 20 } | tail 2 | transform age2 = age + 1`,
			want:     []string{"filter", "tail", "transform"},
		},
		{
			name:     "fused_group_reduce_splits_spans",
			pipeline: `filter { age > 25 } | group city | reduce total = sum(age), n = count() | transform avg_age = total / n`,
			want:     []string{"filter", "group_reduce", "transform"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			optimized := optimizeRowSpanFusionTDDPipeline(t, usersTable().Schema(), tc.pipeline, nil)
			requireRowSpanFusionTDDLogicalShape(t, optimized.Ops, tc.want...)
		})
	}
}

func TestRowSpanFusionTDDOptimizedLogicalJoinBarrier(t *testing.T) {
	right := table.NewTableWithSchemas(
		[]string{"user_name", "amount"},
		[]*table.TypeDescriptor{{Kind: table.TypeString}, {Kind: table.TypeInt}},
	)
	load := func(filename string, opts ast.LoadOptions) (*table.Table, error) {
		return right, nil
	}

	optimized := optimizeRowSpanFusionTDDPipeline(
		t,
		usersTable().Schema(),
		`filter { age > 20 } | join orders.csv on name == user_name | transform amount2 = amount + 1 | select name, amount2`,
		load,
	)
	requireRowSpanFusionTDDLogicalShape(t, optimized.Ops, "filter", "join", "row_span(transform,select)")
}

func TestRowSpanFusionTDDDemandPruningRunsBeforeFusion(t *testing.T) {
	optimized := optimizeRowSpanFusionTDDSourceQuery(t,
		`wide.csv | transform keep = amount * quantity, drop = year(raw) | select keep | json`,
		demandPruningTDDWideSchema(),
	)
	if optimized.Source == nil {
		t.Fatal("optimized source missing")
	}
	if got, want := optimized.Source.outputColumns.Names(), []string{"amount", "quantity"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("source output columns: got %v, want %v", got, want)
	}
	requireRowSpanFusionTDDLogicalShape(t, optimized.Ops, "row_span(transform,select)")
	if names := rowSpanFusionTDDLogicalAssignmentNames(optimized.Ops); containsString(names, "drop") {
		t.Fatalf("dead transform assignment survived into row span: assignments=%v", names)
	}
}

func TestRowSpanFusionTDDPhysicalPlannerLowersSpanAndBindsSequentialEnvironments(t *testing.T) {
	cases := []struct {
		name     string
		pipeline string
		want     []string
	}{
		{
			name:     "rename_filter_select_transform",
			pipeline: `rename age=years | filter { years > 30 } | select years | transform doubled = years * 2`,
			want:     []string{"row_span(rename,filter,select,transform)"},
		},
		{
			name:     "remove_transform_select",
			pipeline: `remove city | transform age2 = age + 1 | select name, age2`,
			want:     []string{"row_span(remove,transform,select)"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			optimized := optimizeRowSpanFusionTDDPipeline(t, usersTable().Schema(), tc.pipeline, nil)
			physical, err := planPhysicalPipeline(optimized)
			if err != nil {
				t.Fatalf("plan physical pipeline: %v", err)
			}
			requireRowSpanFusionTDDPhysicalShape(t, physical.Ops, tc.want...)
		})
	}
}

func TestRowSpanFusionTDDSchemaBoundaryErrorsStillHappenBeforeFusion(t *testing.T) {
	cases := []struct {
		name     string
		pipeline string
		want     []string
	}{
		{
			name:     "remove_hides_column_from_later_filter",
			pipeline: `remove age | filter { age > 30 }`,
			want:     []string{"filter", "age", "not found"},
		},
		{
			name:     "rename_hides_original_name",
			pipeline: `rename age=years | filter { age > 30 }`,
			want:     []string{"filter", "age", "not found"},
		},
		{
			name:     "select_hides_column_from_later_transform",
			pipeline: `select name | transform age2 = age + 1`,
			want:     []string{"transform", "age", "not found"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := planPhysicalPipelineForTest(usersTable().Schema(), parseSimplePlannerOps(t, tc.pipeline))
			if err == nil {
				t.Fatalf("expected planning error for %q", tc.pipeline)
			}
			msg := strings.ToLower(err.Error())
			for _, want := range tc.want {
				if !strings.Contains(msg, strings.ToLower(want)) {
					t.Fatalf("planning error should contain %q, got %v", want, err)
				}
			}
		})
	}
}

func TestRowSpanFusionTDDRuntimeErrorOrderingRemainsSemantic(t *testing.T) {
	t.Run("filter_before_erroring_transform_suppresses_runtime_error", func(t *testing.T) {
		result := runQuery(t, usersTable(), `filter { false } | transform y = year("bad-date") | json`)
		if result.NumRows != 0 {
			t.Fatalf("rows: got %d, want 0", result.NumRows)
		}
	})

	t.Run("erroring_transform_before_filter_still_errors_when_output_is_demanded", func(t *testing.T) {
		err := runQueryExpectErr(t, usersTable(), `transform y = year("bad-date") | filter { false }`)
		if err == nil {
			t.Fatal("expected transform runtime error")
		}
		msg := strings.ToLower(err.Error())
		for _, want := range []string{"transform", "y", "year"} {
			if !strings.Contains(msg, want) {
				t.Fatalf("runtime error should contain %q, got %v", want, err)
			}
		}
	})
}

func optimizeRowSpanFusionTDDPipeline(t *testing.T, input table.Schema, pipeline string, load LoadFunc) *optimizedLogicalPipeline {
	t.Helper()
	logical, err := planLogicalPipeline(input, parseSimplePlannerOps(t, pipeline), load)
	if err != nil {
		t.Fatalf("plan logical pipeline %q: %v", pipeline, err)
	}
	optimized, err := optimizeLogicalPipeline(logical)
	if err != nil {
		t.Fatalf("optimize logical pipeline %q: %v", pipeline, err)
	}
	return optimized
}

func optimizeRowSpanFusionTDDSourceQuery(t *testing.T, query string, schema table.Schema) *optimizedLogicalPipeline {
	t.Helper()
	q := parseSourceProjectionTDDQuery(t, query)
	logical, err := planLogicalQueryWithSource(q, logicalSource{
		filename: q.Source.Filename,
		load:     q.Source.Load,
		schema:   schema,
	}, nil)
	if err != nil {
		t.Fatalf("plan logical source query %q: %v", query, err)
	}
	optimized, err := optimizeLogicalPipeline(logical)
	if err != nil {
		t.Fatalf("optimize logical source query %q: %v", query, err)
	}
	return optimized
}

func requireRowSpanFusionTDDLogicalShape(t *testing.T, ops []logicalOp, want ...string) {
	t.Helper()
	got := make([]string, len(ops))
	for i, op := range ops {
		got[i] = rowSpanFusionTDDPlanShape(reflect.ValueOf(op))
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("optimized logical ops: got %v, want %v", got, want)
	}
}

func requireRowSpanFusionTDDPhysicalShape(t *testing.T, ops []plannedOp, want ...string) {
	t.Helper()
	got := make([]string, len(ops))
	for i, op := range ops {
		got[i] = rowSpanFusionTDDPlanShape(reflect.ValueOf(op))
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("physical ops: got %v, want %v", got, want)
	}
}

func rowSpanFusionTDDPlanShape(v reflect.Value) string {
	v = rowSpanFusionTDDDeref(v)
	if !v.IsValid() {
		return "<nil>"
	}
	kind := rowSpanFusionTDDKindFromType(v.Type().Name())
	if kind != "row_span" {
		return kind
	}
	children := rowSpanFusionTDDSpanChildShapes(v)
	if len(children) == 0 {
		return "row_span(?)"
	}
	return "row_span(" + strings.Join(children, ",") + ")"
}

func rowSpanFusionTDDSpanChildShapes(v reflect.Value) []string {
	field := rowSpanFusionTDDFieldByName(v, "ops", "Ops")
	if !field.IsValid() || field.Kind() != reflect.Slice {
		return nil
	}
	children := make([]string, field.Len())
	for i := 0; i < field.Len(); i++ {
		children[i] = rowSpanFusionTDDPlanShape(field.Index(i))
	}
	return children
}

func rowSpanFusionTDDLogicalAssignmentNames(ops []logicalOp) []string {
	var names []string
	for _, op := range ops {
		names = append(names, rowSpanFusionTDDAssignmentNamesValue(reflect.ValueOf(op))...)
	}
	return names
}

func rowSpanFusionTDDAssignmentNamesValue(v reflect.Value) []string {
	v = rowSpanFusionTDDDeref(v)
	if !v.IsValid() {
		return nil
	}
	if rowSpanFusionTDDKindFromType(v.Type().Name()) == "row_span" {
		var names []string
		field := rowSpanFusionTDDFieldByName(v, "ops", "Ops")
		if field.IsValid() && field.Kind() == reflect.Slice {
			for i := 0; i < field.Len(); i++ {
				names = append(names, rowSpanFusionTDDAssignmentNamesValue(field.Index(i))...)
			}
		}
		return names
	}
	if rowSpanFusionTDDKindFromType(v.Type().Name()) != "transform" {
		return nil
	}
	assignments := rowSpanFusionTDDFieldByName(v, "assignments", "Assignments")
	if !assignments.IsValid() || assignments.Kind() != reflect.Slice {
		return nil
	}
	names := make([]string, 0, assignments.Len())
	for i := 0; i < assignments.Len(); i++ {
		assignment := rowSpanFusionTDDDeref(assignments.Index(i))
		name := rowSpanFusionTDDFieldByName(assignment, "name", "Name")
		if name.IsValid() && name.Kind() == reflect.String {
			names = append(names, name.String())
		}
	}
	return names
}

func rowSpanFusionTDDDeref(v reflect.Value) reflect.Value {
	for v.IsValid() && (v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer) {
		if v.IsNil() {
			return reflect.Value{}
		}
		v = v.Elem()
	}
	return v
}

func rowSpanFusionTDDFieldByName(v reflect.Value, names ...string) reflect.Value {
	if !v.IsValid() {
		return reflect.Value{}
	}
	v = rowSpanFusionTDDDeref(v)
	if !v.IsValid() || v.Kind() != reflect.Struct {
		return reflect.Value{}
	}
	for _, name := range names {
		field := v.FieldByName(name)
		if field.IsValid() {
			return field
		}
	}
	return reflect.Value{}
}

func rowSpanFusionTDDKindFromType(name string) string {
	normalized := strings.ReplaceAll(strings.ToLower(name), "_", "")
	switch {
	case strings.Contains(normalized, "rowspan"):
		return "row_span"
	case strings.Contains(normalized, "groupreduce"):
		return "group_reduce"
	case strings.Contains(normalized, "filter"):
		return "filter"
	case strings.Contains(normalized, "select"):
		return "select"
	case strings.Contains(normalized, "rename"):
		return "rename"
	case strings.Contains(normalized, "remove"):
		return "remove"
	case strings.Contains(normalized, "transform"):
		return "transform"
	case strings.Contains(normalized, "sort"):
		return "sort"
	case strings.Contains(normalized, "distinct"):
		return "distinct"
	case strings.Contains(normalized, "head"):
		return "head"
	case strings.Contains(normalized, "tail"):
		return "tail"
	case strings.Contains(normalized, "count"):
		return "count"
	case strings.Contains(normalized, "describe"):
		return "describe"
	case strings.Contains(normalized, "join"):
		return "join"
	case strings.Contains(normalized, "group"):
		return "group"
	case strings.Contains(normalized, "reduce"):
		return "reduce"
	default:
		return "unknown(" + name + ")"
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
