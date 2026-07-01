package engine

import (
	"fmt"
	"strings"
	"testing"

	"github.com/razeghi71/dq/ast"
	"github.com/razeghi71/dq/parser"
	"github.com/razeghi71/dq/table"
)

func TestSchemaEnvTDDConstructorsBuildValidEnvironments(t *testing.T) {
	schema := schemaEnvTDDWideSchema(schemaEnvIndexColumnThreshold + 4)
	t.Run("from_schema", func(t *testing.T) {
		env, err := schemaEnvFromSchema(schema)
		if err != nil {
			t.Fatalf("schemaEnvFromSchema: %v", err)
		}
		requireSchemaEnvTDDContract(t, env)
	})

	t.Run("from_table", func(t *testing.T) {
		tbl := table.NewTableWithSchemas(
			[]string{"id", "city", "amount"},
			[]*table.TypeDescriptor{schemaEnvTDDType(table.TypeInt), schemaEnvTDDType(table.TypeString), schemaEnvTDDType(table.TypeFloat)},
		)
		env, err := schemaEnvFromTable(tbl)
		if err != nil {
			t.Fatalf("schemaEnvFromTable: %v", err)
		}
		requireSchemaEnvTDDContract(t, env)
	})

	t.Run("from_paired_columns", func(t *testing.T) {
		env, err := newSchemaEnv([]schemaEnvColumn{
			{name: "id", raw: schemaEnvTDDType(table.TypeInt)},
			{name: "status", raw: schemaEnvTDDType(table.TypeString)},
			{name: "score", raw: schemaEnvTDDType(table.TypeFloat)},
		})
		if err != nil {
			t.Fatalf("newSchemaEnv: %v", err)
		}
		requireSchemaEnvTDDContract(t, env)
	})
}

func TestSchemaEnvTDDConstructorsRejectInvalidEnvironments(t *testing.T) {
	dup := table.Schema{Columns: []table.SchemaColumn{
		{Name: "id", Type: schemaEnvTDDType(table.TypeInt)},
		{Name: "id", Type: schemaEnvTDDType(table.TypeString)},
	}}
	if _, err := schemaEnvFromSchema(dup); err == nil || !strings.Contains(err.Error(), `duplicate column name "id"`) {
		t.Fatalf("duplicate schema columns should fail at the constructor, got: %v", err)
	}
	dupTable := table.NewTableWithSchemas(
		[]string{"id", "id"},
		[]*table.TypeDescriptor{schemaEnvTDDType(table.TypeInt), schemaEnvTDDType(table.TypeString)},
	)
	if _, err := schemaEnvFromTable(dupTable); err == nil || !strings.Contains(err.Error(), `duplicate column name "id"`) {
		t.Fatalf("duplicate table columns should fail at the constructor, got: %v", err)
	}
	if _, err := newSchemaEnv([]schemaEnvColumn{
		{name: "id", raw: schemaEnvTDDType(table.TypeInt)},
		{name: "id", raw: schemaEnvTDDType(table.TypeString)},
	}); err == nil || !strings.Contains(err.Error(), `duplicate column name "id"`) {
		t.Fatalf("duplicate paired columns should fail at the constructor, got: %v", err)
	}
}

func TestSchemaEnvTDDExecuteRejectsDuplicateTableInputNames(t *testing.T) {
	input := table.NewTableWithSchemas(
		[]string{"id", "id"},
		[]*table.TypeDescriptor{schemaEnvTDDType(table.TypeInt), schemaEnvTDDType(table.TypeString)},
	)
	q, err := parser.Parse("dup.csv | select id | json")
	if err != nil {
		t.Fatalf("parse query: %v", err)
	}

	_, err = Execute(q, input, nil)
	if err == nil {
		t.Fatal("Execute should reject duplicate table input column names during planning")
	}
	if got, want := err.Error(), `duplicate column name "id"`; got != want {
		t.Fatalf("duplicate table input error: got %q, want %q", got, want)
	}
	if strings.Contains(err.Error(), "schema environment") {
		t.Fatalf("duplicate table input error leaked internal wording: %v", err)
	}
}

func TestSchemaEnvTDDEmptyColumnNameIsARealColumnName(t *testing.T) {
	env, err := newSchemaEnv([]schemaEnvColumn{
		{name: "", raw: schemaEnvTDDType(table.TypeString)},
		{name: "id", raw: schemaEnvTDDType(table.TypeInt)},
	})
	if err != nil {
		t.Fatalf("newSchemaEnv with empty column name: %v", err)
	}
	col, ok := env.lookupColumn("")
	if !ok {
		t.Fatal("lookupColumn(empty): got absence, want the empty-named column")
	}
	if col.index != 0 || col.column.name != "" || col.column.raw.Kind != table.TypeString {
		t.Fatalf("lookupColumn(empty): got %#v, want first string column", col)
	}
	if _, ok := env.lookupColumn("__schema_env_tdd_missing__"); ok {
		t.Fatal("missing lookup should stay distinct from the empty-named column")
	}

	demand := demandNoColumns()
	demand.add("")
	if !demand.has("") {
		t.Fatal("columnDemand should preserve demand for an empty-named column")
	}
}

func TestSchemaEnvTDDConstructorAndIndexBranches(t *testing.T) {
	t.Run("nil_table", func(t *testing.T) {
		env, err := schemaEnvFromTable(nil)
		if err != nil {
			t.Fatalf("schemaEnvFromTable(nil): %v", err)
		}
		if _, ok := env.lookupColumn("missing"); len(env.columns) != 0 || ok {
			t.Fatalf("nil table env: got columns=%v lookup ok=%v", env.columns, ok)
		}
	})

	t.Run("tiny_envs_validate_without_retained_index", func(t *testing.T) {
		columns := schemaEnvTDDColumns(schemaEnvIndexColumnThreshold - 1)
		env, err := newSchemaEnv(columns)
		if err != nil {
			t.Fatalf("newSchemaEnv(%d): %v", len(columns), err)
		}
		requireSchemaEnvTDDContract(t, env)
		if env.index != nil {
			t.Fatalf("env with %d columns should validate without retaining an index", len(columns))
		}
		last := fmt.Sprintf("c%03d", len(columns)-1)
		if got, ok := env.lookupColumn(last); !ok || got.index != len(columns)-1 {
			t.Fatalf("env lookup(%q): got (%#v, %v), want index %d", last, got, ok, len(columns)-1)
		}
	})

	t.Run("non_tiny_envs_retain_plain_map_index", func(t *testing.T) {
		for _, n := range []int{20, 100, 500} {
			columns := schemaEnvTDDColumns(n)
			env, err := newSchemaEnv(columns)
			if err != nil {
				t.Fatalf("newSchemaEnv(%d): %v", n, err)
			}
			requireSchemaEnvTDDContract(t, env)
			if env.index == nil {
				t.Fatalf("env with %d columns should retain a map index", n)
			}
			last := fmt.Sprintf("c%03d", n-1)
			if got, ok := env.lookupColumn(last); !ok || got.index != n-1 {
				t.Fatalf("env lookup(%q): got (%#v, %v), want index %d", last, got, ok, n-1)
			}
		}
	})

	t.Run("threshold_width_envs_retain_plain_map_index", func(t *testing.T) {
		columns := schemaEnvTDDColumns(schemaEnvIndexColumnThreshold)
		env, err := newSchemaEnv(columns)
		if err != nil {
			t.Fatalf("newSchemaEnv(%d): %v", len(columns), err)
		}
		requireSchemaEnvTDDContract(t, env)
		if env.index == nil {
			t.Fatalf("env with %d columns should retain a map index", len(columns))
		}
	})

	t.Run("map_validation_rejects_duplicate", func(t *testing.T) {
		columns := schemaEnvTDDColumns(100)
		columns[99].name = columns[17].name
		_, err := newSchemaEnv(columns)
		if err == nil || !strings.Contains(err.Error(), `duplicate column name "c017"`) {
			t.Fatalf("duplicate error: got %v", err)
		}
	})

	t.Run("indexed_schema_lookup_and_missing_paths", func(t *testing.T) {
		env, err := schemaEnvFromSchema(schemaEnvTDDWideSchema(schemaEnvIndexColumnThreshold))
		if err != nil {
			t.Fatalf("indexed schema env: %v", err)
		}
		requireSchemaEnvTDDContract(t, env)
		if env.index == nil {
			t.Fatal("threshold-width schema should retain an index")
		}
		if _, ok := env.index["not_present"]; ok {
			t.Fatal("indexed missing lookup unexpectedly found not_present")
		}
	})

	t.Run("wide_index_rejects_duplicate", func(t *testing.T) {
		schema := schemaEnvTDDWideSchema(schemaEnvIndexColumnThreshold)
		schema.Columns[len(schema.Columns)-1].Name = schema.Columns[3].Name
		_, err := schemaEnvFromSchema(schema)
		if err == nil || !strings.Contains(err.Error(), `duplicate column name "c003"`) {
			t.Fatalf("wide duplicate error: got %v", err)
		}
	})

	t.Run("schema_helpers_return_explicit_absence_and_preserve_nil_raw_schema", func(t *testing.T) {
		env, err := newSchemaEnv([]schemaEnvColumn{{name: "x"}})
		if err != nil {
			t.Fatalf("newSchemaEnv nil raw: %v", err)
		}
		if got, ok := env.columnAt(-1); ok {
			t.Fatalf("columnAt(-1): got (%#v, true), want absence", got)
		}
		if got, ok := env.columnAt(2); ok {
			t.Fatalf("columnAt(2): got (%#v, true), want absence", got)
		}
		col, ok := env.columnAt(0)
		if !ok {
			t.Fatal("columnAt(0): got absence, want existing column")
		}
		if col.column.raw != nil || col.column.finalSchema() != nil {
			t.Fatalf("nil raw schema should remain nil: got raw=%v final=%v", col.column.raw, col.column.finalSchema())
		}
		if got := env.schema(); len(got.Columns) != 1 || got.Columns[0].Type != nil {
			t.Fatalf("schema with nil raw: got %#v", got)
		}
	})

	t.Run("must_wrapper_panics_on_invalid_env", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatal("mustSchemaEnv should panic on constructor error")
			}
		}()
		_ = mustSchemaEnv(schemaEnv{}, fmt.Errorf("boom"))
	})
}

func TestSchemaEnvTDDFindAndUpsertColumnContract(t *testing.T) {
	columns := []schemaEnvColumn{
		{name: "id", raw: schemaEnvTDDType(table.TypeInt)},
		{name: "city", raw: schemaEnvTDDType(table.TypeString)},
	}
	if got, ok := findEnvColumn(columns, "city"); !ok || got.index != 1 || got.column.name != "city" {
		t.Fatalf("findEnvColumn(existing): got (%#v, %v), want city at index 1", got, ok)
	}
	if got, ok := findEnvColumn(columns, "missing"); ok || got != (schemaEnvColumnRef{}) {
		t.Fatalf("findEnvColumn(missing): got (%#v, %v), want explicit absence", got, ok)
	}

	same, existing := upsertEnvColumn(columns, "id")
	if len(same) != len(columns) || existing.index != 0 || existing.column.name != "id" {
		t.Fatalf("upsertEnvColumn(existing): got len=%d ref=%#v, want same length and id at index 0", len(same), existing)
	}

	extended, appended := upsertEnvColumn(columns, "amount")
	if len(extended) != len(columns)+1 || appended.index != len(columns) || appended.column.name != "amount" {
		t.Fatalf("upsertEnvColumn(new): got len=%d ref=%#v, want appended amount at index %d", len(extended), appended, len(columns))
	}
}

func TestSchemaEnvTDDRejectsInvalidSchemaNamesBeforePlanning(t *testing.T) {
	dup := table.Schema{Columns: []table.SchemaColumn{
		{Name: "id", Type: schemaEnvTDDType(table.TypeInt)},
		{Name: "id", Type: schemaEnvTDDType(table.TypeString)},
	}}
	q, err := parser.Parse("dup.csv | select id | json")
	if err != nil {
		t.Fatalf("parse query: %v", err)
	}
	_, err = planPhysicalSourceQuery(q, SourceInfo{Filename: "dup.csv", Schema: dup}, nil)
	if err == nil {
		t.Fatal("duplicate source schema names should be rejected before a physical plan is observable")
	}
	if got, want := err.Error(), `source schema: duplicate column name "id"`; got != want {
		t.Fatalf("duplicate schema error: got %q, want %q", got, want)
	}
	if strings.Contains(err.Error(), "schema environment") {
		t.Fatalf("duplicate schema error leaked internal wording: %v", err)
	}
}

func TestSchemaEnvTDDLogicalPlanningPreservesEnvironmentInvariants(t *testing.T) {
	input := table.NewSchema(
		[]string{"id", "name", "age", "city", "amount", "status"},
		[]*table.TypeDescriptor{
			schemaEnvTDDType(table.TypeInt),
			schemaEnvTDDType(table.TypeString),
			schemaEnvTDDType(table.TypeInt),
			schemaEnvTDDType(table.TypeString),
			schemaEnvTDDType(table.TypeFloat),
			schemaEnvTDDType(table.TypeString),
		},
	)
	right := table.NewTableWithSchemas(
		[]string{"user_id", "status", "total"},
		[]*table.TypeDescriptor{schemaEnvTDDType(table.TypeInt), schemaEnvTDDType(table.TypeString), schemaEnvTDDType(table.TypeFloat)},
	)
	load := func(filename string, _ ast.LoadOptions) (*table.Table, error) {
		if filename != "orders.csv" {
			t.Fatalf("unexpected join source %q", filename)
		}
		return right, nil
	}
	q, err := parser.Parse(`users.csv | filter { age > 20 and status == "active" } | transform bucket = age / 10, label = upper(name) | select id, city, bucket, label | rename bucket=age_bucket | join orders.csv on id == user_id | remove status | group city | reduce n = count(), total_amount = sum(total) | remove grouped | sort city | distinct city, n, total_amount`)
	if err != nil {
		t.Fatalf("parse query: %v", err)
	}

	plan, err := planLogicalPipeline(input, q.Ops, load)
	if err != nil {
		t.Fatalf("plan logical pipeline: %v", err)
	}
	inputs, outputs := logicalOpEnvs(plan.InputEnv, plan.Ops)
	requireSchemaEnvTDDContract(t, plan.InputEnv)
	for i, env := range inputs {
		requireSchemaEnvTDDContract(t, env)
		if len(env.columns) > 0 {
			got, ok := env.lookupColumn(env.columns[0].name)
			if !ok || got.index != 0 {
				t.Fatalf("op %d input first column lookup drifted: got (%#v, %v) columns=%v", i, got, ok, env.columnNames())
			}
		}
	}
	for i, env := range outputs {
		requireSchemaEnvTDDContract(t, env)
		if got := env.schema(); len(got.Columns) != len(env.columns) {
			t.Fatalf("op %d output schema length: got %d, want %d", i, len(got.Columns), len(env.columns))
		}
	}
}

func requireSchemaEnvTDDContract(t *testing.T, env schemaEnv) {
	t.Helper()
	seen := make(map[string]int, len(env.columns))
	for i, col := range env.columns {
		if prev, ok := seen[col.name]; ok {
			t.Fatalf("schemaEnv duplicate column %q at positions %d and %d", col.name, prev, i)
		}
		seen[col.name] = i
	}

	index, indexed := schemaEnvTDDIndexSnapshot(t, env)
	if indexed && len(index) != len(env.columns) {
		t.Fatalf("schemaEnv index length %d does not match columns length %d: index=%v columns=%v", len(index), len(env.columns), index, env.columns)
	}
	for i, col := range env.columns {
		got, ok := env.lookupColumn(col.name)
		if !ok || got.index != i || got.column != col {
			t.Fatalf("lookupColumn(%q): got (%#v, %v), want index %d and column %#v; columns=%v", col.name, got, ok, i, col, env.columnNames())
		}
		if indexed {
			if got, ok := index[col.name]; !ok || got != i {
				t.Fatalf("derived index[%q]: got (%d, %v), want (%d, true); index=%v columns=%v", col.name, got, ok, i, index, env.columnNames())
			}
		}
	}
	if got, ok := env.lookupColumn("__schema_env_tdd_missing__"); ok {
		t.Fatalf("missing column lookup: got (%#v, true), want absence", got)
	}
}

func schemaEnvTDDIndexSnapshot(t *testing.T, env schemaEnv) (map[string]int, bool) {
	t.Helper()
	if env.index == nil {
		return nil, false
	}
	out := make(map[string]int, len(env.columns))
	for i, col := range env.columns {
		got, ok := env.index[col.name]
		if !ok || got != i {
			t.Fatalf("stored schemaEnv index lookup(%q): got %d, want %d", col.name, got, i)
		}
		out[col.name] = got
	}
	return out, true
}

func schemaEnvTDDWideSchema(cols int) table.Schema {
	names, types := schemaEnvTDDColumnsAndTypes(cols)
	return table.NewSchema(names, types)
}

func schemaEnvTDDColumns(cols int) []schemaEnvColumn {
	names, types := schemaEnvTDDColumnsAndTypes(cols)
	columns := make([]schemaEnvColumn, cols)
	for i := range columns {
		columns[i] = schemaEnvColumn{name: names[i], raw: types[i]}
	}
	return columns
}

func schemaEnvTDDColumnsAndTypes(cols int) ([]string, []*table.TypeDescriptor) {
	names := make([]string, cols)
	types := make([]*table.TypeDescriptor, cols)
	for i := range names {
		names[i] = fmt.Sprintf("c%03d", i)
		if i%3 == 0 {
			types[i] = schemaEnvTDDType(table.TypeString)
		} else {
			types[i] = schemaEnvTDDType(table.TypeInt)
		}
	}
	return names, types
}

func schemaEnvTDDType(kind table.ValueType) *table.TypeDescriptor {
	return &table.TypeDescriptor{Kind: kind}
}
