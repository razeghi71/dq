package engine

import (
	"reflect"
	"strings"
	"testing"

	"github.com/razeghi71/dq/ast"
	"github.com/razeghi71/dq/parser"
	"github.com/razeghi71/dq/table"
)

func TestSourceProjectionTDDImmediateTopLevelSelectPushesColumnsIntoSource(t *testing.T) {
	q := parseSourceProjectionTDDQuery(t, "users.csv | select status, id | json")
	source := logicalSource{
		filename: q.Source.Filename,
		load:     q.Source.Load,
		schema:   sourceProjectionTDDWideSchema(),
	}

	logical, err := planLogicalQueryWithSource(q, source, nil)
	if err != nil {
		t.Fatalf("plan logical source query: %v", err)
	}
	if logical.Source == nil || logical.Source.filename != "users.csv" {
		t.Fatalf("logical source: got %#v, want users.csv", logical.Source)
	}
	if len(logical.Ops) != 1 {
		t.Fatalf("logical ops: got %d, want original select op retained before optimization", len(logical.Ops))
	}

	optimized, err := optimizeLogicalPipeline(logical)
	if err != nil {
		t.Fatalf("optimize source query: %v", err)
	}
	if optimized.Source == nil || !reflect.DeepEqual(optimized.Source.outputColumns.Names(), []string{"status", "id"}) {
		t.Fatalf("optimized source output columns: got %#v, want [status id]", optimized.Source)
	}
	if len(optimized.Ops) != 0 {
		t.Fatalf("optimized ops: got %d, want redundant direct select removed", len(optimized.Ops))
	}
	requireSourceProjectionTDDSchemaColumns(t, optimized.OutputSchema, "status", "id")

	physical, err := planPhysicalPipeline(optimized)
	if err != nil {
		t.Fatalf("physical source query: %v", err)
	}
	if physical.Source == nil || !reflect.DeepEqual(physical.Source.spec.ReadColumns.Names(), []string{"status", "id"}) || !reflect.DeepEqual(physical.Source.spec.OutputColumns.Names(), []string{"status", "id"}) {
		t.Fatalf("physical source spec: got %#v, want read/output [status id]", physical.Source)
	}
	if len(physical.Ops) != 0 {
		t.Fatalf("physical ops: got %d, want direct select satisfied by source", len(physical.Ops))
	}
}

func TestSourceProjectionTDDExecuteUsesOptimizerProjectedSourceLoad(t *testing.T) {
	q := parseSourceProjectionTDDQuery(t, "users.csv | select status, id | json")
	var loadedSpec SourceLoadSpec

	result, err := ExecuteSourceQuery(q, SourceInfo{
		Filename: "users.csv",
		Load:     q.Source.Load,
		Schema:   sourceProjectionTDDWideSchema(),
	}, func(filename string, opts ast.LoadOptions, spec SourceLoadSpec) (*table.Table, error) {
		loadedSpec = spec
		tbl := table.NewTableWithTypes([]string{"status", "id"}, []table.ValueType{table.TypeString, table.TypeInt})
		tbl.AddRow([]table.Value{table.StrVal("active"), table.IntVal(1)})
		tbl.AddRow([]table.Value{table.StrVal("paused"), table.IntVal(2)})
		return tbl, nil
	}, nil)
	if err != nil {
		t.Fatalf("execute source query: %v", err)
	}
	if !reflect.DeepEqual(loadedSpec.ReadColumns.Names(), []string{"status", "id"}) || !reflect.DeepEqual(loadedSpec.OutputColumns.Names(), []string{"status", "id"}) {
		t.Fatalf("source loader spec: got %#v, want read/output [status id]", loadedSpec)
	}
	requireSourceProjectionTDDTableColumns(t, result, "status", "id")
	if result.NumRows != 2 {
		t.Fatalf("result rows: got %d, want 2", result.NumRows)
	}
}

func TestSourceProjectionTDDExecuteAppliesSourcePredicateBeforeOutput(t *testing.T) {
	q := parseSourceProjectionTDDQuery(t, `users.csv | filter { status == "active" } | select id | json`)
	var loadedSpec SourceLoadSpec

	result, err := ExecuteSourceQuery(q, SourceInfo{
		Filename: "users.csv",
		Load:     q.Source.Load,
		Schema:   sourceProjectionTDDWideSchema(),
	}, func(filename string, opts ast.LoadOptions, spec SourceLoadSpec) (*table.Table, error) {
		loadedSpec = spec
		if spec.Predicate == nil {
			t.Fatal("expected source predicate")
		}
		tbl := table.NewTableWithTypes([]string{"id"}, []table.ValueType{table.TypeInt})
		for _, row := range [][]table.Value{
			{table.IntVal(1), table.StrVal("active")},
			{table.IntVal(2), table.StrVal("paused")},
		} {
			keep, err := spec.Predicate(row)
			if err != nil {
				t.Fatalf("source predicate: %v", err)
			}
			if keep {
				tbl.AddRow([]table.Value{row[0]})
			}
		}
		return tbl, nil
	}, nil)
	if err != nil {
		t.Fatalf("execute source query: %v", err)
	}
	if !reflect.DeepEqual(loadedSpec.ReadColumns.Names(), []string{"id", "status"}) ||
		!reflect.DeepEqual(loadedSpec.OutputColumns.Names(), []string{"id"}) ||
		loadedSpec.Predicate == nil {
		t.Fatalf("source loader spec: got %#v, want read [id status], output [id], predicate", loadedSpec)
	}
	requireSourceProjectionTDDTableColumns(t, result, "id")
	if result.NumRows != 1 {
		t.Fatalf("result rows: got %d, want 1", result.NumRows)
	}
	if got := result.GetAt(0, 0); got.Type != table.TypeInt || got.Int != 1 {
		t.Fatalf("result id: got %v, want int 1", got)
	}
}

func TestSourceProjectionTDDPlanningErrorDoesNotMaterializeSource(t *testing.T) {
	q := parseSourceProjectionTDDQuery(t, "users.csv | select id | select missing | json")
	sourceLoaded := false

	_, err := ExecuteSourceQuery(q, SourceInfo{
		Filename: "users.csv",
		Load:     q.Source.Load,
		Schema:   sourceProjectionTDDWideSchema(),
	}, func(filename string, opts ast.LoadOptions, spec SourceLoadSpec) (*table.Table, error) {
		sourceLoaded = true
		return table.NewTable(nil), nil
	}, nil)
	if err == nil {
		t.Fatal("expected missing-column planning error")
	}
	if sourceLoaded {
		t.Fatal("source loader was called before planning completed")
	}
	for _, want := range []string{"select", "missing", "not found"} {
		if !strings.Contains(strings.ToLower(err.Error()), want) {
			t.Fatalf("error %q missing %q", err, want)
		}
	}
}

func TestSourceProjectionTDDExecuteReportsSourceLoadError(t *testing.T) {
	q := parseSourceProjectionTDDQuery(t, "users.csv | select id | json")

	_, err := ExecuteSourceQuery(q, SourceInfo{
		Filename: "users.csv",
		Load:     q.Source.Load,
		Schema:   sourceProjectionTDDWideSchema(),
	}, func(filename string, opts ast.LoadOptions, spec SourceLoadSpec) (*table.Table, error) {
		return nil, errSourceProjectionTDDLoad
	}, nil)
	if err == nil {
		t.Fatal("expected source load error")
	}
	for _, want := range []string{"load error", errSourceProjectionTDDLoad.Error()} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q missing %q", err, want)
		}
	}
}

func TestSourceProjectionTDDExecuteReturnsRuntimeErrorAfterFullSourceLoad(t *testing.T) {
	q := parseSourceProjectionTDDQuery(t, "users.csv | transform y = year(name) | json")

	tbl := table.NewTableWithTypes([]string{"id", "name", "status", "unused"}, []table.ValueType{
		table.TypeInt,
		table.TypeString,
		table.TypeString,
		table.TypeInt,
	})
	tbl.AddRow([]table.Value{table.IntVal(1), table.StrVal("not-a-date"), table.StrVal("active"), table.IntVal(10)})

	_, err := ExecuteSourceQuery(q, SourceInfo{
		Filename: "users.csv",
		Load:     q.Source.Load,
		Schema:   sourceProjectionTDDWideSchema(),
	}, func(filename string, opts ast.LoadOptions, spec SourceLoadSpec) (*table.Table, error) {
		if !spec.ReadColumns.IsAll() || !spec.OutputColumns.IsAll() || spec.Predicate != nil {
			t.Fatalf("transform-first query should not push source requirements, got %#v", spec)
		}
		return tbl, nil
	}, nil)
	if err == nil {
		t.Fatal("expected runtime transform error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "year") {
		t.Fatalf("error: got %v, want year runtime detail", err)
	}
}

func TestSourceProjectionTDDOptimizeSourceProjectionNilPlan(t *testing.T) {
	optimizeSourcePushdown(nil)
}

func TestSourceProjectionTDDExecuteRequiresSourceLoader(t *testing.T) {
	q := parseSourceProjectionTDDQuery(t, "users.csv | count | json")

	_, err := ExecuteSourceQuery(q, SourceInfo{
		Filename: "users.csv",
		Load:     q.Source.Load,
		Schema:   sourceProjectionTDDWideSchema(),
	}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "source loader") {
		t.Fatalf("expected source loader error, got %v", err)
	}
}

func TestSourceProjectionTDDValidateSourceInputSchemaDiagnostics(t *testing.T) {
	want := table.Schema{Columns: []table.SchemaColumn{
		{Name: "id", Type: &table.TypeDescriptor{Kind: table.TypeInt}},
		{Name: "profile", Type: &table.TypeDescriptor{
			Kind: table.TypeRecord,
			Fields: []table.FieldDescriptor{
				{Name: "score", Type: &table.TypeDescriptor{Kind: table.TypeInt}},
			},
		}},
	}}

	if err := validateSourceInputSchema(want, nil); err == nil || !strings.Contains(err.Error(), "column count") {
		t.Fatalf("nil table error: got %v, want column count", err)
	}

	countMismatch := table.NewTableWithTypes([]string{"id"}, []table.ValueType{table.TypeInt})
	if err := validateSourceInputSchema(want, countMismatch); err == nil || !strings.Contains(err.Error(), "column count") {
		t.Fatalf("count mismatch error: got %v", err)
	}

	nameMismatch := table.NewTableWithTypes([]string{"other", "profile"}, []table.ValueType{table.TypeInt, table.TypeString})
	if err := validateSourceInputSchema(want, nameMismatch); err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("name mismatch error: got %v", err)
	}

	typeMismatch := table.NewTableWithTypes([]string{"id", "profile"}, []table.ValueType{table.TypeString, table.TypeString})
	if err := validateSourceInputSchema(want, typeMismatch); err == nil || !strings.Contains(err.Error(), "schema") {
		t.Fatalf("type mismatch error: got %v", err)
	}

	nullableLoaded := table.NewTableWithSchemas([]string{"id", "profile"}, []*table.TypeDescriptor{
		{Kind: table.TypeInt, Nullable: true},
		{
			Kind:     table.TypeRecord,
			Nullable: true,
			Fields: []table.FieldDescriptor{
				{Name: "score", Type: &table.TypeDescriptor{Kind: table.TypeInt, Nullable: true}},
			},
		},
	})
	if err := validateSourceInputSchema(want, nullableLoaded); err == nil || !strings.Contains(err.Error(), "schema") {
		t.Fatalf("nullable loaded schema should not satisfy precise planned schema: %v", err)
	}

	soundWant := table.Schema{Columns: []table.SchemaColumn{
		{Name: "id", Type: &table.TypeDescriptor{Kind: table.TypeInt, Nullable: true}},
		{Name: "profile", Type: &table.TypeDescriptor{
			Kind:     table.TypeRecord,
			Nullable: true,
			Fields: []table.FieldDescriptor{
				{Name: "score", Type: &table.TypeDescriptor{Kind: table.TypeInt, Nullable: true}},
			},
		}},
	}}
	preciseLoaded := table.NewTableWithSchemas([]string{"id", "profile"}, []*table.TypeDescriptor{
		{Kind: table.TypeInt},
		{
			Kind: table.TypeRecord,
			Fields: []table.FieldDescriptor{
				{Name: "score", Type: &table.TypeDescriptor{Kind: table.TypeInt}},
			},
		},
	})
	if err := validateSourceInputSchema(soundWant, preciseLoaded); err != nil {
		t.Fatalf("precise loaded schema should satisfy nullable planned schema: %v", err)
	}
}

func TestSourceProjectionTDDPlanPhysicalSourceFallsBackToInputSchemaEnv(t *testing.T) {
	plan := &optimizedLogicalPipeline{
		Source: &optimizedSource{source: logicalSource{filename: "users.csv"}},
		InputSchema: table.Schema{Columns: []table.SchemaColumn{
			{Name: "id", Type: &table.TypeDescriptor{Kind: table.TypeInt}},
		}},
		OutputSchema: table.Schema{Columns: []table.SchemaColumn{
			{Name: "id", Type: &table.TypeDescriptor{Kind: table.TypeInt}},
		}},
	}

	physical, err := planPhysicalPipeline(plan)
	if err != nil {
		t.Fatalf("physical source plan: %v", err)
	}
	if len(physical.InputSchema.Columns) != 1 || physical.InputSchema.Columns[0].Name != "id" {
		t.Fatalf("physical input schema: got %#v", physical.InputSchema)
	}
}

func TestSourceProjectionTDDLogicalSelectPushdownRejectsRenamedProjection(t *testing.T) {
	cols, ok := sourceProjectionColumnsFromLogicalSelect(logicalSelect{
		topLevelOnly: true,
		projections: []logicalPathBinding{{
			name:   "renamed",
			path:   []string{"id"},
			schema: &table.TypeDescriptor{Kind: table.TypeInt},
		}},
	})
	if ok || cols != nil {
		t.Fatalf("renamed projection should not push down: cols=%v ok=%v", cols, ok)
	}
}

func TestSourceProjectionTDDLogicalSelectPushdownRejectsNestedProjectionMetadata(t *testing.T) {
	cols, ok := sourceProjectionColumnsFromLogicalSelect(logicalSelect{
		topLevelOnly: false,
		projections: []logicalPathBinding{{
			name:   "address_city",
			path:   []string{"address", "city"},
			schema: &table.TypeDescriptor{Kind: table.TypeString},
		}},
	})
	if ok || cols != nil {
		t.Fatalf("nested projection metadata should not push down: cols=%v ok=%v", cols, ok)
	}
}

func TestSourceProjectionTDDDuplicateSelectPrunesSourceButKeepsSelect(t *testing.T) {
	q := parseSourceProjectionTDDQuery(t, "users.csv | select id, id | json")
	optimized := optimizeSourcePushdownTDDQuery(t, q, sourceProjectionTDDWideSchema())

	if optimized.Source == nil {
		t.Fatal("optimized source missing")
	}
	if !reflect.DeepEqual(optimized.Source.outputColumns.Names(), []string{"id"}) || len(optimized.Source.predicates) != 0 {
		t.Fatalf("duplicate select source requirements: got %#v, want output [id]", optimized.Source)
	}
	if len(optimized.Ops) != 1 {
		t.Fatalf("duplicate select must keep select op for id/id_2 output naming, got %d ops", len(optimized.Ops))
	}
	requireSourceProjectionTDDSchemaColumns(t, optimized.OutputSchema, "id", "id_2")
}

func TestSourceProjectionTDDNestedSelectPrunesToTopLevelRootAndKeepsSelect(t *testing.T) {
	schema := table.Schema{Columns: []table.SchemaColumn{
		{Name: "id", Type: &table.TypeDescriptor{Kind: table.TypeInt}},
		{Name: "address", Type: &table.TypeDescriptor{
			Kind: table.TypeRecord,
			Fields: []table.FieldDescriptor{
				{Name: "city", Type: &table.TypeDescriptor{Kind: table.TypeString}},
			},
		}},
	}}
	q := parseSourceProjectionTDDQuery(t, "users.json | select address.city | json")
	optimized := optimizeSourcePushdownTDDQuery(t, q, schema)

	if optimized.Source == nil {
		t.Fatal("optimized source missing")
	}
	if !reflect.DeepEqual(optimized.Source.outputColumns.Names(), []string{"address"}) || len(optimized.Source.predicates) != 0 {
		t.Fatalf("nested projection source requirements: got %#v, want output [address]", optimized.Source)
	}
	if len(optimized.Ops) != 1 {
		t.Fatalf("nested select must keep select op, got %d ops", len(optimized.Ops))
	}
	requireSourceProjectionTDDSchemaColumns(t, optimized.OutputSchema, "address_city")
}

func TestSourceProjectionTDDFilterBeforeSelectPushesPredicateAndProjection(t *testing.T) {
	q := parseSourceProjectionTDDQuery(t, `users.csv | filter { status == "active" } | select id | json`)
	optimized := optimizeSourcePushdownTDDQuery(t, q, sourceProjectionTDDWideSchema())

	if optimized.Source == nil {
		t.Fatal("optimized source missing")
	}
	if !reflect.DeepEqual(optimized.Source.outputColumns.Names(), []string{"id"}) {
		t.Fatalf("optimized source output columns: got %v, want [id]", optimized.Source.outputColumns.Names())
	}
	if len(optimized.Source.predicates) != 1 {
		t.Fatalf("optimized source predicates: got %d, want 1", len(optimized.Source.predicates))
	}
	if len(optimized.Ops) != 0 {
		t.Fatalf("filter/select pipeline should be satisfied by source, got %d ops", len(optimized.Ops))
	}
	physical, err := planPhysicalPipeline(optimized)
	if err != nil {
		t.Fatalf("physical source query: %v", err)
	}
	if !reflect.DeepEqual(physical.Source.spec.OutputColumns.Names(), []string{"id"}) {
		t.Fatalf("physical output columns: got %v, want [id]", physical.Source.spec.OutputColumns.Names())
	}
	if !reflect.DeepEqual(physical.Source.spec.ReadColumns.Names(), []string{"id", "status"}) {
		t.Fatalf("physical read columns: got %v, want [id status]", physical.Source.spec.ReadColumns.Names())
	}
}

func TestSourceProjectionTDDFilterOnlyPushesPredicateToSourceLoad(t *testing.T) {
	q := parseSourceProjectionTDDQuery(t, `users.csv | filter { status == "active" } | json`)
	optimized := optimizeSourcePushdownTDDQuery(t, q, sourceProjectionTDDWideSchema())

	if optimized.Source == nil {
		t.Fatal("optimized source missing")
	}
	if !optimized.Source.outputColumns.IsAll() {
		t.Fatalf("filter-only source output columns: got %v, want read/output all", optimized.Source.outputColumns)
	}
	if len(optimized.Source.predicates) != 1 {
		t.Fatalf("optimized source predicates: got %d, want 1", len(optimized.Source.predicates))
	}
	if len(optimized.Ops) != 0 {
		t.Fatalf("filter-only pipeline should be satisfied by source, got %d ops", len(optimized.Ops))
	}
	physical, err := planPhysicalPipeline(optimized)
	if err != nil {
		t.Fatalf("physical source query: %v", err)
	}
	if !physical.Source.spec.ReadColumns.IsAll() || !physical.Source.spec.OutputColumns.IsAll() || physical.Source.spec.Predicate == nil {
		t.Fatalf("physical source spec: got %#v, want read/output all with predicate", physical.Source.spec)
	}
}

func TestSourceProjectionTDDSelectBeforeFilterPushesPredicateAndProjection(t *testing.T) {
	q := parseSourceProjectionTDDQuery(t, `users.csv | select id, status | filter { status == "active" } | json`)
	optimized := optimizeSourcePushdownTDDQuery(t, q, sourceProjectionTDDWideSchema())

	if optimized.Source == nil {
		t.Fatal("optimized source missing")
	}
	if !reflect.DeepEqual(optimized.Source.outputColumns.Names(), []string{"id", "status"}) {
		t.Fatalf("optimized source output columns: got %v, want [id status]", optimized.Source.outputColumns.Names())
	}
	if len(optimized.Source.predicates) != 1 {
		t.Fatalf("optimized source predicates: got %d, want 1", len(optimized.Source.predicates))
	}
	if len(optimized.Ops) != 0 {
		t.Fatalf("select/filter pipeline should be satisfied by source, got %d ops", len(optimized.Ops))
	}
	physical, err := planPhysicalPipeline(optimized)
	if err != nil {
		t.Fatalf("physical source query: %v", err)
	}
	if !reflect.DeepEqual(physical.Source.spec.ReadColumns.Names(), []string{"id", "status"}) {
		t.Fatalf("physical read columns: got %v, want [id status]", physical.Source.spec.ReadColumns.Names())
	}
}

func TestSourceProjectionTDDMultipleAdjacentSelectsAndFiltersPushDeterministicReadSet(t *testing.T) {
	q := parseSourceProjectionTDDQuery(t, `users.csv | filter { status == "active" } | select id, status, name | filter { id > 0 } | select name | json`)
	optimized := optimizeSourcePushdownTDDQuery(t, q, sourceProjectionTDDWideSchema())

	if optimized.Source == nil {
		t.Fatal("optimized source missing")
	}
	if !reflect.DeepEqual(optimized.Source.outputColumns.Names(), []string{"name"}) {
		t.Fatalf("optimized source output columns: got %v, want [name]", optimized.Source.outputColumns.Names())
	}
	if len(optimized.Source.predicates) != 2 {
		t.Fatalf("optimized source predicates: got %d, want 2", len(optimized.Source.predicates))
	}
	if len(optimized.Ops) != 0 {
		t.Fatalf("adjacent select/filter prefix should be satisfied by source, got %d ops", len(optimized.Ops))
	}
	physical, err := planPhysicalPipeline(optimized)
	if err != nil {
		t.Fatalf("physical source query: %v", err)
	}
	if !reflect.DeepEqual(physical.Source.spec.OutputColumns.Names(), []string{"name"}) {
		t.Fatalf("physical output columns: got %v, want [name]", physical.Source.spec.OutputColumns.Names())
	}
	if !reflect.DeepEqual(physical.Source.spec.ReadColumns.Names(), []string{"name", "id", "status"}) {
		t.Fatalf("physical read columns: got %v, want [name id status]", physical.Source.spec.ReadColumns.Names())
	}
}

func TestSourceProjectionTDDUnsupportedFilterAfterSelectKeepsFilterButPushesSelect(t *testing.T) {
	q := parseSourceProjectionTDDQuery(t, `users.csv | select id, status | filter { starts_with(status, "a") } | json`)
	optimized := optimizeSourcePushdownTDDQuery(t, q, sourceProjectionTDDWideSchema())

	if optimized.Source == nil {
		t.Fatal("optimized source missing")
	}
	if !reflect.DeepEqual(optimized.Source.outputColumns.Names(), []string{"id", "status"}) {
		t.Fatalf("optimized source output columns: got %v, want [id status]", optimized.Source.outputColumns.Names())
	}
	if len(optimized.Source.predicates) != 0 {
		t.Fatalf("unsupported filter should not be pushed, got %d predicates", len(optimized.Source.predicates))
	}
	ops := flattenSourceProjectionTDDLogicalOps(optimized.Ops)
	if len(ops) != 1 {
		t.Fatalf("unsupported filter must stay as a normal op, got %d ops", len(ops))
	}
	if _, ok := ops[0].(logicalFilter); !ok {
		t.Fatalf("remaining op: got %T, want logicalFilter", ops[0])
	}
	physical, err := planPhysicalPipeline(optimized)
	if err != nil {
		t.Fatalf("physical source query: %v", err)
	}
	if !reflect.DeepEqual(physical.Source.spec.ReadColumns.Names(), []string{"id", "status"}) {
		t.Fatalf("physical read columns: got %v, want [id status]", physical.Source.spec.ReadColumns.Names())
	}
	if physical.Source.spec.Predicate != nil {
		t.Fatalf("unsupported filter should not compile as source predicate")
	}
}

func TestSourceProjectionTDDSelectAfterUnsupportedFilterPrunesToDemandedColumns(t *testing.T) {
	q := parseSourceProjectionTDDQuery(t, `users.csv | filter { id + 1 > 10 } | select id | json`)
	optimized := optimizeSourcePushdownTDDQuery(t, q, sourceProjectionTDDWideSchema())

	if optimized.Source == nil {
		t.Fatal("optimized source missing")
	}
	if !reflect.DeepEqual(optimized.Source.outputColumns.Names(), []string{"id"}) {
		t.Fatalf("optimized source output columns: got %v, want [id]", optimized.Source.outputColumns.Names())
	}
	if len(optimized.Source.predicates) != 0 {
		t.Fatalf("unsupported filter should not be pushed, got %d predicates", len(optimized.Source.predicates))
	}
	ops := flattenSourceProjectionTDDLogicalOps(optimized.Ops)
	if len(ops) != 1 {
		t.Fatalf("unsupported filter should remain and identity select should be removed, got %d ops", len(ops))
	}
	if _, ok := ops[0].(logicalFilter); !ok {
		t.Fatalf("first remaining op: got %T, want logicalFilter", ops[0])
	}
	physical, err := planPhysicalPipeline(optimized)
	if err != nil {
		t.Fatalf("physical source query: %v", err)
	}
	if !reflect.DeepEqual(physical.Source.spec.ReadColumns.Names(), []string{"id"}) ||
		!reflect.DeepEqual(physical.Source.spec.OutputColumns.Names(), []string{"id"}) ||
		physical.Source.spec.Predicate != nil {
		t.Fatalf("physical source spec: got %#v, want read/output [id] with no predicate", physical.Source.spec)
	}
}

func TestSourceProjectionTDDUnsupportedFilterBeforeSelectRebindsAgainstPrunedSource(t *testing.T) {
	q := parseSourceProjectionTDDQuery(t, `wrong.csv | filter { d + 1 > 0 } | select id | json`)
	var loadedSpec SourceLoadSpec

	result, err := ExecuteSourceQuery(q, SourceInfo{
		Filename: "wrong.csv",
		Load:     q.Source.Load,
		Schema: table.Schema{Columns: []table.SchemaColumn{
			{Name: "d", Type: &table.TypeDescriptor{Kind: table.TypeInt}},
			{Name: "id", Type: &table.TypeDescriptor{Kind: table.TypeInt}},
			{Name: "unused", Type: &table.TypeDescriptor{Kind: table.TypeString}},
		}},
	}, func(filename string, opts ast.LoadOptions, spec SourceLoadSpec) (*table.Table, error) {
		loadedSpec = spec
		tbl := table.NewTableWithTypes([]string{"d", "id"}, []table.ValueType{table.TypeInt, table.TypeInt})
		tbl.AddRow([]table.Value{table.IntVal(10), table.IntVal(1)})
		tbl.AddRow([]table.Value{table.IntVal(20), table.IntVal(2)})
		return tbl, nil
	}, nil)
	if err != nil {
		t.Fatalf("execute source query: %v", err)
	}
	if !reflect.DeepEqual(loadedSpec.ReadColumns.Names(), []string{"d", "id"}) ||
		!reflect.DeepEqual(loadedSpec.OutputColumns.Names(), []string{"d", "id"}) ||
		loadedSpec.Predicate != nil {
		t.Fatalf("source loader spec: got %#v, want read/output [d id]", loadedSpec)
	}
	requireSourceProjectionTDDTableColumns(t, result, "id")
	if result.NumRows != 2 {
		t.Fatalf("result rows: got %d, want 2", result.NumRows)
	}
	if got := result.GetAt(0, 0); got.Type != table.TypeInt || got.Int != 1 {
		t.Fatalf("first id: got %v, want int 1", got)
	}
	if got := result.GetAt(1, 0); got.Type != table.TypeInt || got.Int != 2 {
		t.Fatalf("second id: got %v, want int 2", got)
	}
}

func TestSourceProjectionTDDUnsupportedFilterBeforeSelectDemandsPredicateColumns(t *testing.T) {
	q := parseSourceProjectionTDDQuery(t, `users.csv | filter { status == "active" or unused + 1 > 10 } | select id | json`)
	optimized := optimizeSourcePushdownTDDQuery(t, q, sourceProjectionTDDWideSchema())

	if optimized.Source == nil {
		t.Fatal("optimized source missing")
	}
	if !reflect.DeepEqual(optimized.Source.outputColumns.Names(), []string{"id", "status", "unused"}) {
		t.Fatalf("optimized source output columns: got %v, want [id status unused]", optimized.Source.outputColumns.Names())
	}
	if len(optimized.Source.predicates) != 0 {
		t.Fatalf("unsupported filter should not be pushed, got %d predicates", len(optimized.Source.predicates))
	}
	ops := flattenSourceProjectionTDDLogicalOps(optimized.Ops)
	if len(ops) != 2 {
		t.Fatalf("unsupported filter plus final select should remain, got %d ops", len(ops))
	}
	if _, ok := ops[0].(logicalFilter); !ok {
		t.Fatalf("first remaining op: got %T, want logicalFilter", ops[0])
	}
	if _, ok := ops[1].(logicalSelect); !ok {
		t.Fatalf("second remaining op: got %T, want logicalSelect", ops[1])
	}
	physical, err := planPhysicalPipeline(optimized)
	if err != nil {
		t.Fatalf("physical source query: %v", err)
	}
	if !reflect.DeepEqual(physical.Source.spec.ReadColumns.Names(), []string{"id", "status", "unused"}) ||
		!reflect.DeepEqual(physical.Source.spec.OutputColumns.Names(), []string{"id", "status", "unused"}) ||
		physical.Source.spec.Predicate != nil {
		t.Fatalf("physical source spec: got %#v, want read/output [id status unused] with no predicate", physical.Source.spec)
	}
}

func TestSourceProjectionTDDRetainedFilterBlocksLaterPredicatePushdownButStillPrunesColumns(t *testing.T) {
	q := parseSourceProjectionTDDQuery(t, `users.csv | filter { starts_with(status, "a") } | filter { id > 1 } | select id | json`)
	optimized := optimizeSourcePushdownTDDQuery(t, q, sourceProjectionTDDWideSchema())

	if optimized.Source == nil {
		t.Fatal("optimized source missing")
	}
	if !reflect.DeepEqual(optimized.Source.outputColumns.Names(), []string{"id", "status"}) {
		t.Fatalf("optimized source output columns: got %v, want [id status]", optimized.Source.outputColumns.Names())
	}
	if len(optimized.Source.predicates) != 0 {
		t.Fatalf("later filter must not be pushed across retained filter, got %d predicates", len(optimized.Source.predicates))
	}
	ops := flattenSourceProjectionTDDLogicalOps(optimized.Ops)
	if len(ops) != 3 {
		t.Fatalf("retained filters plus final select should remain, got %d ops", len(ops))
	}
	if _, ok := ops[0].(logicalFilter); !ok {
		t.Fatalf("first remaining op: got %T, want logicalFilter", ops[0])
	}
	if _, ok := ops[1].(logicalFilter); !ok {
		t.Fatalf("second remaining op: got %T, want logicalFilter", ops[1])
	}
	if _, ok := ops[2].(logicalSelect); !ok {
		t.Fatalf("third remaining op: got %T, want logicalSelect", ops[2])
	}
	physical, err := planPhysicalPipeline(optimized)
	if err != nil {
		t.Fatalf("physical source query: %v", err)
	}
	if !reflect.DeepEqual(physical.Source.spec.ReadColumns.Names(), []string{"id", "status"}) ||
		!reflect.DeepEqual(physical.Source.spec.OutputColumns.Names(), []string{"id", "status"}) ||
		physical.Source.spec.Predicate != nil {
		t.Fatalf("physical source spec: got %#v, want read/output [id status] with no predicate", physical.Source.spec)
	}
}

func TestSourceProjectionTDDSourcePushdownIsFormatAgnostic(t *testing.T) {
	q := parseSourceProjectionTDDQuery(t, "users.jsonl | select id, status | json")
	source := logicalSource{
		filename: q.Source.Filename,
		load:     ast.LoadOptions{Format: "jsonl"},
		schema:   sourceProjectionTDDWideSchema(),
	}

	logical, err := planLogicalQueryWithSource(q, source, nil)
	if err != nil {
		t.Fatalf("plan logical source query: %v", err)
	}
	optimized, err := optimizeLogicalPipeline(logical)
	if err != nil {
		t.Fatalf("optimize source query: %v", err)
	}
	physical, err := planPhysicalPipeline(optimized)
	if err != nil {
		t.Fatalf("physical source query: %v", err)
	}

	if physical.Source == nil {
		t.Fatal("physical source missing")
	}
	if !reflect.DeepEqual(physical.Source.spec.ReadColumns.Names(), []string{"id", "status"}) ||
		!reflect.DeepEqual(physical.Source.spec.OutputColumns.Names(), []string{"id", "status"}) ||
		physical.Source.spec.Predicate != nil {
		t.Fatalf("jsonl source spec: got %#v, want read/output [id status]", physical.Source.spec)
	}
	if len(physical.Ops) != 0 {
		t.Fatalf("jsonl direct select should be satisfied by source, got %d ops", len(physical.Ops))
	}
}

func TestSourceProjectionTDDSourceFilterPushdownEligibility(t *testing.T) {
	col := &ast.ColumnExpr{Path: []string{"id"}}
	nested := &ast.ColumnExpr{Path: []string{"profile", "score"}}
	lit := &ast.LiteralExpr{Kind: "int", Int: 1}

	cases := []struct {
		name string
		expr ast.Expr
		want bool
	}{
		{name: "top_level_column", expr: col, want: true},
		{name: "nested_column", expr: nested, want: false},
		{name: "comparison", expr: &ast.BinaryExpr{
			Op:    ">",
			Left:  col,
			Right: lit,
		}, want: true},
		{name: "arithmetic", expr: &ast.BinaryExpr{
			Op:    "+",
			Left:  col,
			Right: lit,
		}, want: false},
		{name: "not", expr: &ast.UnaryExpr{
			Op:      "not",
			Operand: col,
		}, want: true},
		{name: "unary_minus", expr: &ast.UnaryExpr{
			Op:      "-",
			Operand: col,
		}, want: false},
		{name: "is_null", expr: &ast.IsNullExpr{
			Operand: col,
		}, want: true},
		{name: "function", expr: &ast.FuncCallExpr{
			Name: "starts_with",
			Args: []ast.Expr{col},
		}, want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sourceFilterASTCanPush(tc.expr); got != tc.want {
				t.Fatalf("sourceFilterASTCanPush: got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSourceProjectionTDDSourceOutputEnvRejectsMissingColumn(t *testing.T) {
	_, ok := sourceOutputEnv(logicalSource{schema: sourceProjectionTDDWideSchema()}, table.SelectedColumns("missing"))
	if ok {
		t.Fatal("missing source output column should be rejected")
	}
}

func TestSourceProjectionTDDSourceOutputEnvFallsBackToFinalSchema(t *testing.T) {
	env, ok := sourceOutputEnv(logicalSource{schema: table.Schema{Columns: []table.SchemaColumn{{Name: "id"}}}}, table.SelectedColumns("id"))
	if !ok {
		t.Fatal("expected source output env for nil raw schema")
	}
	if len(env.columns) != 1 || env.columns[0] != "id" {
		t.Fatalf("columns: got %v, want [id]", env.columns)
	}
}

func TestSourceProjectionTDDPhysicalSourceRejectsInvalidReadSet(t *testing.T) {
	_, err := physicalSourceFromOptimized(&optimizedSource{
		source:        logicalSource{filename: "users.csv", schema: sourceProjectionTDDWideSchema()},
		outputColumns: table.SelectedColumns("missing"),
	})
	if err == nil || !strings.Contains(err.Error(), "read schema") {
		t.Fatalf("expected physical source read schema error, got %v", err)
	}
}

func TestSourceProjectionTDDCollectLogicalTypedExprColumnsTraversesAllChildren(t *testing.T) {
	col := func(name string) logicalTypedExpr {
		return logicalTypedExpr{bound: &logicalBoundColumn{rawPath: []string{name}}}
	}
	expr := logicalTypedExpr{
		bound:   &logicalBoundLiteral{raw: &ast.LiteralExpr{Kind: "bool", Bool: true}},
		left:    ptrLogicalTypedExpr(col("left")),
		right:   ptrLogicalTypedExpr(col("right")),
		operand: ptrLogicalTypedExpr(col("operand")),
		args:    []logicalTypedExpr{col("arg")},
		fields: []logicalTypedStructField{{
			name: "field",
			expr: col("field"),
		}},
		elements: []logicalTypedExpr{col("element")},
	}
	got := map[string]bool{}
	collectLogicalTypedExprColumns(expr, got)
	for _, want := range []string{"left", "right", "operand", "arg", "field", "element"} {
		if !got[want] {
			t.Fatalf("collected columns: got %v, missing %q", got, want)
		}
	}
}

func TestSourceProjectionTDDCompiledSourcePredicateReportsRowBindingErrors(t *testing.T) {
	predicate := compileSourcePredicates([]typedExpr{{
		bound: &boundColumn{rawPath: []string{"status"}, topIndex: 1},
		typ:   &table.TypeDescriptor{Kind: table.TypeBool},
	}})
	if predicate == nil {
		t.Fatal("expected compiled predicate")
	}
	_, err := predicate([]table.Value{table.IntVal(1)})
	if err == nil {
		t.Fatal("expected source predicate row binding error")
	}
	for _, want := range []string{"filter", "status", "not found"} {
		if !strings.Contains(strings.ToLower(err.Error()), want) {
			t.Fatalf("error %q missing %q", err, want)
		}
	}
}

func ptrLogicalTypedExpr(expr logicalTypedExpr) *logicalTypedExpr {
	return &expr
}

func optimizeSourcePushdownTDDQuery(t *testing.T, q *ast.Query, schema table.Schema) *optimizedLogicalPipeline {
	t.Helper()
	logical, err := planLogicalQueryWithSource(q, logicalSource{
		filename: q.Source.Filename,
		load:     q.Source.Load,
		schema:   schema,
	}, nil)
	if err != nil {
		t.Fatalf("plan logical source query: %v", err)
	}
	optimized, err := optimizeLogicalPipeline(logical)
	if err != nil {
		t.Fatalf("optimize source query: %v", err)
	}
	return optimized
}

func parseSourceProjectionTDDQuery(t *testing.T, query string) *ast.Query {
	t.Helper()
	q, err := parser.Parse(query)
	if err != nil {
		t.Fatalf("parse %q: %v", query, err)
	}
	return q
}

func flattenSourceProjectionTDDLogicalOps(ops []logicalOp) []logicalOp {
	var out []logicalOp
	for _, op := range ops {
		if span, ok := op.(optimizedRowSpan); ok {
			out = append(out, flattenSourceProjectionTDDLogicalOps(span.ops)...)
			continue
		}
		out = append(out, op)
	}
	return out
}

func sourceProjectionTDDWideSchema() table.Schema {
	return table.Schema{Columns: []table.SchemaColumn{
		{Name: "id", Type: &table.TypeDescriptor{Kind: table.TypeInt}},
		{Name: "name", Type: &table.TypeDescriptor{Kind: table.TypeString}},
		{Name: "status", Type: &table.TypeDescriptor{Kind: table.TypeString}},
		{Name: "unused", Type: &table.TypeDescriptor{Kind: table.TypeInt}},
	}}
}

var errSourceProjectionTDDLoad = sourceProjectionTDDError("boom")

type sourceProjectionTDDError string

func (e sourceProjectionTDDError) Error() string { return string(e) }

func requireSourceProjectionTDDSchemaColumns(t *testing.T, schema table.Schema, want ...string) {
	t.Helper()
	if len(schema.Columns) != len(want) {
		t.Fatalf("schema columns: got %v, want %v", schema.Columns, want)
	}
	for i := range want {
		if schema.Columns[i].Name != want[i] {
			t.Fatalf("schema columns: got %v, want %v", schema.Columns, want)
		}
	}
}

func requireSourceProjectionTDDTableColumns(t *testing.T, tbl *table.Table, want ...string) {
	t.Helper()
	if tbl == nil {
		t.Fatal("nil table")
	}
	if !reflect.DeepEqual(tbl.Columns, want) {
		t.Fatalf("table columns: got %v, want %v", tbl.Columns, want)
	}
}
