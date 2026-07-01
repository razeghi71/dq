package engine

import (
	"reflect"
	"testing"

	"github.com/razeghi71/dq/ast"
	"github.com/razeghi71/dq/table"
)

func TestDemandPruningTDDUnsupportedFilterPrunesSourceReadSetWithoutPredicatePushdown(t *testing.T) {
	physical := planDemandPruningTDDSourceQuery(t,
		`wide.csv | filter { upper(status) == "ACTIVE" } | select id | json`,
		demandPruningTDDWideSchema(),
		nil,
	)

	requireDemandPruningTDDSourceSpec(t, physical, []string{"id", "status"}, []string{"id", "status"}, false)
	requireDemandPruningTDDPlannedOpTypes(t, physical, "filter", "select")
}

func TestDemandPruningTDDSortAndDeadTransformDemandOnlySemanticInputs(t *testing.T) {
	physical := planDemandPruningTDDSourceQuery(t,
		`wide.csv | filter { upper(status) == "ACTIVE" } | transform gross = amount * quantity, label = upper(name) | sort -created_at | select id, gross | json`,
		demandPruningTDDWideSchema(),
		nil,
	)

	requireDemandPruningTDDSourceSpec(t, physical, []string{"id", "status", "amount", "quantity", "created_at"}, []string{"id", "status", "amount", "quantity", "created_at"}, false)
	transform := requireDemandPruningTDDTransform(t, physical, 1)
	requireDemandPruningTDDTransformAssignments(t, transform, "gross")
	requireDemandPruningTDDPlannedOpTypes(t, physical, "filter", "transform", "sort", "select")
}

func TestDemandPruningTDDCountReadsZeroDataColumnsButPreservesRows(t *testing.T) {
	q := parseSourceProjectionTDDQuery(t, `wide.csv | count | json`)
	var loadedSpec SourceLoadSpec

	result, err := ExecuteSourceQuery(q, SourceInfo{
		Filename: "wide.csv",
		Load:     q.Source.Load,
		Schema:   demandPruningTDDWideSchema(),
	}, func(filename string, opts ast.LoadOptions, spec SourceLoadSpec) (*table.Table, error) {
		loadedSpec = spec
		tbl := table.NewTableWithSchemas(nil, nil)
		if err := tbl.AddRowTyped(nil); err != nil {
			t.Fatalf("add zero-column row 1: %v", err)
		}
		if err := tbl.AddRowTyped(nil); err != nil {
			t.Fatalf("add zero-column row 2: %v", err)
		}
		return tbl, nil
	}, nil)
	if err != nil {
		t.Fatalf("execute count query: %v", err)
	}
	if !reflect.DeepEqual(loadedSpec.ReadColumns.Names(), []string{}) || !reflect.DeepEqual(loadedSpec.OutputColumns.Names(), []string{}) {
		t.Fatalf("source loader spec: got %#v, want explicit empty read/output columns", loadedSpec)
	}
	if result.NumRows != 1 || result.ColIndex("count") < 0 {
		t.Fatalf("count result shape: got rows=%d columns=%v", result.NumRows, result.Columns)
	}
	if got := result.GetAt(0, result.ColIndex("count")); got.Type != table.TypeInt || got.Int != 2 {
		t.Fatalf("count result: got %v, want int 2", got)
	}
}

func TestDemandPruningTDDDeadTransformAssignmentIsDroppedAndDoesNotDemandItsInputs(t *testing.T) {
	physical := planDemandPruningTDDSourceQuery(t,
		`wide.csv | transform unused = year(raw), kept = id + 1 | select kept | json`,
		demandPruningTDDWideSchema(),
		nil,
	)

	requireDemandPruningTDDSourceSpec(t, physical, []string{"id"}, []string{"id"}, false)
	transform := requireDemandPruningTDDTransform(t, physical, 0)
	requireDemandPruningTDDTransformAssignments(t, transform, "kept")
}

func TestDemandPruningTDDTransformPruningPreservesSimultaneousAssignmentInputs(t *testing.T) {
	physical := planDemandPruningTDDSourceQuery(t,
		`wide.csv | transform x = amount + 1, y = x + 1 | select y | json`,
		demandPruningTDDWideSchema(),
		nil,
	)

	requireDemandPruningTDDSourceSpec(t, physical, []string{"x"}, []string{"x"}, false)
	transform := requireDemandPruningTDDTransform(t, physical, 0)
	requireDemandPruningTDDTransformAssignments(t, transform, "y")
}

func TestDemandPruningTDDRenameAndRemoveMapDemandBackToSourceColumns(t *testing.T) {
	t.Run("rename_maps_output_name_to_input_name", func(t *testing.T) {
		physical := planDemandPruningTDDSourceQuery(t,
			`wide.csv | rename status=state | filter { upper(state) == "ACTIVE" } | select id | json`,
			demandPruningTDDWideSchema(),
			nil,
		)

		requireDemandPruningTDDSourceSpec(t, physical, []string{"id", "status"}, []string{"id", "status"}, false)
		requireDemandPruningTDDPlannedOpTypes(t, physical, "rename", "filter", "select")
	})

	t.Run("remove_can_be_satisfied_by_pruned_source_output", func(t *testing.T) {
		physical := planDemandPruningTDDSourceQuery(t,
			`wide.csv | remove unused | select id | json`,
			demandPruningTDDWideSchema(),
			nil,
		)

		requireDemandPruningTDDSourceSpec(t, physical, []string{"id"}, []string{"id"}, false)
		requireDemandPruningTDDPlannedOpTypes(t, physical)
	})
}

func TestDemandPruningTDDNestedExpressionsDemandTopLevelRoots(t *testing.T) {
	physical := planDemandPruningTDDSourceQuery(t,
		`nested.json | filter { upper(address.city) == "NY" } | select id | json`,
		demandPruningTDDNestedSchema(),
		nil,
	)

	requireDemandPruningTDDSourceSpec(t, physical, []string{"id", "address"}, []string{"id", "address"}, false)
	requireDemandPruningTDDPlannedOpTypes(t, physical, "filter", "select")
}

func TestDemandPruningTDDProjectedDistinctKeepsAllSemanticKeys(t *testing.T) {
	physical := planDemandPruningTDDSourceQuery(t,
		`wide.csv | distinct id, status | select id | json`,
		demandPruningTDDWideSchema(),
		nil,
	)

	requireDemandPruningTDDSourceSpec(t, physical, []string{"id", "status"}, []string{"id", "status"}, false)
	requireDemandPruningTDDPlannedOpTypes(t, physical, "distinct", "select")
}

func TestDemandPruningTDDDescribeAndFullRowDistinctRemainReadAllBarriers(t *testing.T) {
	for _, tc := range []struct {
		name  string
		query string
	}{
		{name: "describe", query: `wide.csv | describe | select column | json`},
		{name: "full_row_distinct", query: `wide.csv | distinct | select id | json`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			physical := planDemandPruningTDDSourceQuery(t, tc.query, demandPruningTDDWideSchema(), nil)
			requireDemandPruningTDDSourceSpec(t, physical, nil, nil, false)
		})
	}
}

func TestDemandPruningTDDJoinDemandsLeftKeysAndDemandedLeftOutputs(t *testing.T) {
	right := table.NewTableWithTypes([]string{"amount", "user_id", "right_unused"}, []table.ValueType{table.TypeInt, table.TypeInt, table.TypeString})
	right.AddRow([]table.Value{table.IntVal(10), table.IntVal(1), table.StrVal("x")})

	physical := planDemandPruningTDDSourceQuery(t,
		`users.csv | join orders.csv on id == user_id | select name, amount | json`,
		demandPruningTDDJoinLeftSchema(),
		func(filename string, opts ast.LoadOptions) (*table.Table, error) {
			if filename != "orders.csv" {
				t.Fatalf("join loader filename: got %q, want orders.csv", filename)
			}
			return right, nil
		},
	)

	requireDemandPruningTDDSourceSpec(t, physical, []string{"id", "name"}, []string{"id", "name"}, false)
	requireDemandPruningTDDJoinRightSpec(t, physical, []string{"amount", "user_id"})
	requireDemandPruningTDDPlannedOpTypes(t, physical, "join")
}

func TestDemandPruningTDDJoinCountDemandsOnlyJoinKeys(t *testing.T) {
	right := table.NewTableWithTypes([]string{"amount", "user_id", "right_unused"}, []table.ValueType{table.TypeInt, table.TypeInt, table.TypeString})
	right.AddRow([]table.Value{table.IntVal(10), table.IntVal(1), table.StrVal("x")})

	physical := planDemandPruningTDDSourceQuery(t,
		`users.csv | join orders.csv on id == user_id | count | json`,
		demandPruningTDDJoinLeftSchema(),
		func(filename string, opts ast.LoadOptions) (*table.Table, error) {
			if filename != "orders.csv" {
				t.Fatalf("join loader filename: got %q, want orders.csv", filename)
			}
			return right, nil
		},
	)

	requireDemandPruningTDDSourceSpec(t, physical, []string{"id"}, []string{"id"}, false)
	requireDemandPruningTDDJoinRightSpec(t, physical, []string{"user_id"})
	requireDemandPruningTDDPlannedOpTypes(t, physical, "join", "count")
}

func TestDemandPruningTDDJoinOutputNamesStayStableWhenLeftCollisionColumnOtherwiseUnused(t *testing.T) {
	left := table.NewTableWithTypes([]string{"id", "amount", "name"}, []table.ValueType{table.TypeInt, table.TypeInt, table.TypeString})
	left.AddRow([]table.Value{table.IntVal(1), table.IntVal(100), table.StrVal("Alice")})
	left.AddRow([]table.Value{table.IntVal(2), table.IntVal(200), table.StrVal("Bob")})
	right := table.NewTableWithTypes([]string{"user_id", "amount"}, []table.ValueType{table.TypeInt, table.TypeInt})
	right.AddRow([]table.Value{table.IntVal(1), table.IntVal(50)})
	right.AddRow([]table.Value{table.IntVal(2), table.IntVal(60)})

	t.Run("select_prefixed_right_column", func(t *testing.T) {
		var loadedSpec SourceLoadSpec
		result := executeDemandPruningTDDJoinCollisionQuery(t,
			`left.csv | join orders.csv on id == user_id | select orders_amount | json`,
			left,
			right,
			&loadedSpec,
		)
		if !reflect.DeepEqual(loadedSpec.ReadColumns.Names(), []string{"id"}) || !reflect.DeepEqual(loadedSpec.OutputColumns.Names(), []string{"id"}) {
			t.Fatalf("source spec: got %#v, want read/output [id]", loadedSpec)
		}
		requireSourceProjectionTDDTableColumns(t, result, "orders_amount")
		if result.NumRows != 2 || result.GetAt(0, 0).Int != 50 || result.GetAt(1, 0).Int != 60 {
			t.Fatalf("orders_amount rows: got\n%s\nwant 50, 60", result.String())
		}
	})

	t.Run("filter_prefixed_right_column", func(t *testing.T) {
		var loadedSpec SourceLoadSpec
		result := executeDemandPruningTDDJoinCollisionQuery(t,
			`left.csv | join orders.csv on id == user_id | filter { orders_amount > 50 } | select name | json`,
			left,
			right,
			&loadedSpec,
		)
		if !reflect.DeepEqual(loadedSpec.ReadColumns.Names(), []string{"id", "name"}) || !reflect.DeepEqual(loadedSpec.OutputColumns.Names(), []string{"id", "name"}) {
			t.Fatalf("source spec: got %#v, want read/output [id name]", loadedSpec)
		}
		requireSourceProjectionTDDTableColumns(t, result, "name")
		if result.NumRows != 1 || result.GetAt(0, 0).Str != "Bob" {
			t.Fatalf("filtered join rows: got\n%s\nwant Bob", result.String())
		}
	})
}

func executeDemandPruningTDDJoinCollisionQuery(t *testing.T, query string, left, right *table.Table, loadedSpec *SourceLoadSpec) *table.Table {
	t.Helper()
	q := parseSourceProjectionTDDQuery(t, query)
	result, err := ExecuteSourceQuery(q, SourceInfo{
		Filename: q.Source.Filename,
		Load:     q.Source.Load,
		Schema:   left.Schema(),
	}, func(filename string, opts ast.LoadOptions, spec SourceLoadSpec) (*table.Table, error) {
		if filename != "left.csv" {
			t.Fatalf("source loader filename: got %q, want left.csv", filename)
		}
		*loadedSpec = spec
		return projectDemandPruningTDDTableForSpec(t, left, spec), nil
	}, newLoadFuncJoinSourceProvider(func(filename string, opts ast.LoadOptions) (*table.Table, error) {
		if filename != "orders.csv" {
			t.Fatalf("join loader filename: got %q, want orders.csv", filename)
		}
		return right, nil
	}))
	if err != nil {
		t.Fatalf("execute join collision query %q: %v", query, err)
	}
	return result
}

func projectDemandPruningTDDTableForSpec(t *testing.T, input *table.Table, spec SourceLoadSpec) *table.Table {
	t.Helper()
	if spec.OutputColumns.IsAll() {
		return input
	}
	outputColumns := spec.OutputColumns.Names()
	cols := make([]string, len(outputColumns))
	schemas := make([]*table.TypeDescriptor, len(outputColumns))
	indexes := make([]int, len(outputColumns))
	for i, col := range outputColumns {
		idx := input.ColIndex(col)
		if idx < 0 {
			t.Fatalf("project source table: missing column %q", col)
		}
		cols[i] = col
		schemas[i] = input.Col(idx).RawSchema()
		if schemas[i] == nil {
			schemas[i] = input.Col(idx).Schema()
		}
		indexes[i] = idx
	}
	out := table.NewTableWithSchemas(cols, schemas)
	for row := 0; row < input.NumRows; row++ {
		vals := make([]table.Value, len(indexes))
		for i, idx := range indexes {
			vals[i] = input.GetAt(row, idx)
		}
		if err := out.AddRowTyped(vals); err != nil {
			t.Fatalf("project source table: add row: %v", err)
		}
	}
	return out
}

func planDemandPruningTDDSourceQuery(t *testing.T, query string, schema table.Schema, load LoadFunc) *physicalPipeline {
	t.Helper()
	q := parseSourceProjectionTDDQuery(t, query)
	physical, err := planPhysicalSourceQuery(q, SourceInfo{
		Filename: q.Source.Filename,
		Load:     q.Source.Load,
		Schema:   schema,
	}, newLoadFuncJoinSourceProvider(func(filename string, opts ast.LoadOptions) (*table.Table, error) {
		if load != nil {
			return load(filename, opts)
		}
		t.Fatalf("unexpected load for %q", filename)
		return nil, nil
	}))
	if err != nil {
		t.Fatalf("plan physical source query %q: %v", query, err)
	}
	return physical
}

func requireDemandPruningTDDSourceSpec(t *testing.T, physical *physicalPipeline, read, output []string, wantPredicate bool) {
	t.Helper()
	if physical == nil || physical.Source == nil {
		t.Fatalf("physical source missing: %#v", physical)
	}
	spec := physical.Source.spec
	if !reflect.DeepEqual(spec.ReadColumns.Names(), read) || !reflect.DeepEqual(spec.OutputColumns.Names(), output) {
		t.Fatalf("source spec: got read=%#v output=%#v, want read=%#v output=%#v", spec.ReadColumns.Names(), spec.OutputColumns.Names(), read, output)
	}
	if gotPredicate := spec.Predicate != nil; gotPredicate != wantPredicate {
		t.Fatalf("source predicate presence: got %v, want %v", gotPredicate, wantPredicate)
	}
}

func requireDemandPruningTDDJoinRightSpec(t *testing.T, physical *physicalPipeline, columns []string) {
	t.Helper()
	ops := flattenPlannedRowSpans(physical.Ops)
	for _, op := range ops {
		join, ok := op.(plannedJoin)
		if !ok {
			continue
		}
		got := join.right.spec.Columns.Names()
		if !reflect.DeepEqual(got, columns) {
			t.Fatalf("join right spec columns: got %#v, want %#v", got, columns)
		}
		return
	}
	t.Fatalf("planned join missing; ops=%v", demandPruningTDDOpTypes(physical.Ops))
}

func requireDemandPruningTDDPlannedOpTypes(t *testing.T, physical *physicalPipeline, want ...string) {
	t.Helper()
	if want == nil {
		want = []string{}
	}
	got := demandPruningTDDOpTypes(physical.Ops)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("physical ops: got %v, want %v", got, want)
	}
}

func requireDemandPruningTDDTransform(t *testing.T, physical *physicalPipeline, idx int) plannedTransform {
	t.Helper()
	ops := flattenPlannedRowSpans(physical.Ops)
	if idx < 0 || idx >= len(ops) {
		t.Fatalf("transform op index %d out of range; ops=%v", idx, demandPruningTDDOpTypes(physical.Ops))
	}
	transform, ok := ops[idx].(plannedTransform)
	if !ok {
		t.Fatalf("op %d: got %T, want plannedTransform; ops=%v", idx, ops[idx], demandPruningTDDOpTypes(physical.Ops))
	}
	return transform
}

func requireDemandPruningTDDTransformAssignments(t *testing.T, transform plannedTransform, want ...string) {
	t.Helper()
	got := make([]string, len(transform.assignments))
	for i, assignment := range transform.assignments {
		got[i] = assignment.name
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("transform assignments: got %v, want %v", got, want)
	}
}

func demandPruningTDDOpTypes(ops []plannedOp) []string {
	flat := flattenPlannedRowSpans(ops)
	got := make([]string, len(flat))
	for i, op := range flat {
		switch op.(type) {
		case plannedFilter:
			got[i] = "filter"
		case plannedTransform:
			got[i] = "transform"
		case plannedSort:
			got[i] = "sort"
		case plannedSelect:
			got[i] = "select"
		case plannedRename:
			got[i] = "rename"
		case plannedRemove:
			got[i] = "remove"
		case plannedDistinct:
			got[i] = "distinct"
		case plannedCount:
			got[i] = "count"
		case plannedDescribe:
			got[i] = "describe"
		case plannedJoin:
			got[i] = "join"
		case plannedGroup:
			got[i] = "group"
		case plannedReduce:
			got[i] = "reduce"
		case plannedGroupReduce:
			got[i] = "group_reduce"
		default:
			got[i] = "unknown"
		}
	}
	return got
}

func flattenPlannedRowSpans(ops []plannedOp) []plannedOp {
	var out []plannedOp
	for _, op := range ops {
		if span, ok := op.(plannedRowSpan); ok {
			out = append(out, flattenPlannedRowSpans(span.ops)...)
			continue
		}
		out = append(out, op)
	}
	return out
}

func demandPruningTDDWideSchema() table.Schema {
	intType := &table.TypeDescriptor{Kind: table.TypeInt}
	stringType := &table.TypeDescriptor{Kind: table.TypeString}
	return table.Schema{Columns: []table.SchemaColumn{
		{Name: "id", Type: intType},
		{Name: "status", Type: stringType},
		{Name: "amount", Type: intType},
		{Name: "quantity", Type: intType},
		{Name: "created_at", Type: intType},
		{Name: "name", Type: stringType},
		{Name: "raw", Type: stringType},
		{Name: "x", Type: intType},
		{Name: "unused", Type: intType},
	}}
}

func demandPruningTDDNestedSchema() table.Schema {
	return table.Schema{Columns: []table.SchemaColumn{
		{Name: "id", Type: &table.TypeDescriptor{Kind: table.TypeInt}},
		{Name: "address", Type: &table.TypeDescriptor{Kind: table.TypeRecord, Fields: []table.FieldDescriptor{
			{Name: "city", Type: &table.TypeDescriptor{Kind: table.TypeString}},
			{Name: "zip", Type: &table.TypeDescriptor{Kind: table.TypeInt}},
		}}},
		{Name: "unused", Type: &table.TypeDescriptor{Kind: table.TypeInt}},
	}}
}

func demandPruningTDDJoinLeftSchema() table.Schema {
	return table.Schema{Columns: []table.SchemaColumn{
		{Name: "id", Type: &table.TypeDescriptor{Kind: table.TypeInt}},
		{Name: "name", Type: &table.TypeDescriptor{Kind: table.TypeString}},
		{Name: "unused", Type: &table.TypeDescriptor{Kind: table.TypeInt}},
	}}
}
