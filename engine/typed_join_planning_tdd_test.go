package engine

import (
	"strings"
	"testing"

	"github.com/razeghi71/dq/ast"
	"github.com/razeghi71/dq/parser"
	"github.com/razeghi71/dq/table"
)

func TestTypedJoinPlanningTDDJoinParticipatesInSchemaPlannedPipeline(t *testing.T) {
	if !isSchemaPlannedOp(&ast.JoinOp{}) {
		t.Fatal("join should participate in schema-planned pipeline")
	}
}

func TestTypedJoinPlanningTDDRejectsIncompatibleKeySchemas(t *testing.T) {
	cases := []struct {
		name  string
		left  *table.Table
		right *table.Table
		query string
		want  []string
	}{
		{
			name:  "int_vs_string",
			left:  typedJoinTable(t, []string{"id", "name"}, []*table.TypeDescriptor{tdJoin(table.TypeInt), tdJoin(table.TypeString)}, [][]table.Value{{table.IntVal(1), table.StrVal("left")}}),
			right: typedJoinTable(t, []string{"id", "amount"}, []*table.TypeDescriptor{tdJoin(table.TypeString), tdJoin(table.TypeInt)}, [][]table.Value{{table.StrVal("1"), table.IntVal(10)}}),
			query: "join right.csv on id | count",
			want:  []string{"join", "key", "type", "id", "int", "string"},
		},
		{
			name:  "int_vs_float",
			left:  typedJoinTable(t, []string{"id"}, []*table.TypeDescriptor{tdJoin(table.TypeInt)}, [][]table.Value{{table.IntVal(1)}}),
			right: typedJoinTable(t, []string{"id"}, []*table.TypeDescriptor{tdJoin(table.TypeFloat)}, [][]table.Value{{table.FloatVal(1)}}),
			query: "join right.csv on id | count",
			want:  []string{"join", "key", "type", "id", "int", "float"},
		},
		{
			name:  "bool_vs_int",
			left:  typedJoinTable(t, []string{"id"}, []*table.TypeDescriptor{tdJoin(table.TypeBool)}, [][]table.Value{{table.BoolVal(true)}}),
			right: typedJoinTable(t, []string{"id"}, []*table.TypeDescriptor{tdJoin(table.TypeInt)}, [][]table.Value{{table.IntVal(1)}}),
			query: "join right.csv on id | count",
			want:  []string{"join", "key", "type", "id", "bool", "int"},
		},
		{
			name: "record_vs_scalar",
			left: typedJoinTable(t, []string{"profile"}, []*table.TypeDescriptor{{
				Kind: table.TypeRecord,
				Fields: []table.FieldDescriptor{
					{Name: "id", Type: tdJoin(table.TypeInt)},
				},
			}}, [][]table.Value{{table.RecordVal([]table.RecordField{{Name: "id", Value: table.IntVal(1)}})}}),
			right: typedJoinTable(t, []string{"id"}, []*table.TypeDescriptor{tdJoin(table.TypeInt)}, [][]table.Value{{table.IntVal(1)}}),
			query: "join right.csv on profile == id | count",
			want:  []string{"join", "key", "type", "profile", "record", "id", "int"},
		},
		{
			name: "nested_record_mismatch",
			left: typedJoinTable(t, []string{"profile"}, []*table.TypeDescriptor{{
				Kind: table.TypeRecord,
				Fields: []table.FieldDescriptor{
					{Name: "id", Type: tdJoin(table.TypeInt)},
				},
			}}, [][]table.Value{{table.RecordVal([]table.RecordField{{Name: "id", Value: table.IntVal(1)}})}}),
			right: typedJoinTable(t, []string{"profile"}, []*table.TypeDescriptor{{
				Kind: table.TypeRecord,
				Fields: []table.FieldDescriptor{
					{Name: "id", Type: tdJoin(table.TypeString)},
				},
			}}, [][]table.Value{{table.RecordVal([]table.RecordField{{Name: "id", Value: table.StrVal("1")}})}}),
			query: "join right.csv on profile | count",
			want:  []string{"join", "key", "type", "profile", "record", "id", "int", "string"},
		},
		{
			name: "list_element_mismatch",
			left: typedJoinTable(t, []string{"ids"}, []*table.TypeDescriptor{{
				Kind: table.TypeList,
				Elem: tdJoin(table.TypeInt),
			}}, [][]table.Value{{table.ListVal([]table.Value{table.IntVal(1)})}}),
			right: typedJoinTable(t, []string{"ids"}, []*table.TypeDescriptor{{
				Kind: table.TypeList,
				Elem: tdJoin(table.TypeString),
			}}, [][]table.Value{{table.ListVal([]table.Value{table.StrVal("1")})}}),
			query: "join right.csv on ids | count",
			want:  []string{"join", "key", "type", "ids", "list", "int", "string"},
		},
		{
			name:  "one_of_multiple_keys_mismatch",
			left:  typedJoinTable(t, []string{"id", "region"}, []*table.TypeDescriptor{tdJoin(table.TypeInt), tdJoin(table.TypeString)}, [][]table.Value{{table.IntVal(1), table.StrVal("US")}}),
			right: typedJoinTable(t, []string{"id", "region"}, []*table.TypeDescriptor{tdJoin(table.TypeString), tdJoin(table.TypeString)}, [][]table.Value{{table.StrVal("1"), table.StrVal("US")}}),
			query: "join right.csv on id and region | count",
			want:  []string{"join", "key", "type", "id", "int", "string"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := runTypedJoinPlanningQueryExpectErr(t, tc.left, tc.right, tc.query)
			requireErrContainsAll(t, err, tc.want...)
		})
	}
}

func TestTypedJoinPlanningTDDAllowsExactCompatibleKeySchemas(t *testing.T) {
	t.Run("nullability_does_not_change_key_domain", func(t *testing.T) {
		left := typedJoinTable(t, []string{"id", "name"}, []*table.TypeDescriptor{
			{Kind: table.TypeString, Nullable: true},
			tdJoin(table.TypeString),
		}, [][]table.Value{
			{table.StrVal("A"), table.StrVal("matched")},
			{table.Null(), table.StrVal("null-key")},
		})
		right := typedJoinTable(t, []string{"id", "amount"}, []*table.TypeDescriptor{
			tdJoin(table.TypeString),
			tdJoin(table.TypeInt),
		}, [][]table.Value{
			{table.StrVal("A"), table.IntVal(10)},
			{table.Null(), table.IntVal(99)},
		})

		result := runTypedJoinPlanningQuery(t, left, right, "join right.csv on id | count")
		if got := result.Get(0, "count"); got.Type != table.TypeInt || got.Int != 1 {
			t.Fatalf("count: got %v, want 1", got)
		}
	})

	t.Run("record_keys_match_exact_schema", func(t *testing.T) {
		schema := &table.TypeDescriptor{
			Kind: table.TypeRecord,
			Fields: []table.FieldDescriptor{
				{Name: "id", Type: tdJoin(table.TypeInt)},
				{Name: "region", Type: tdJoin(table.TypeString)},
			},
		}
		key := table.RecordVal([]table.RecordField{
			{Name: "id", Value: table.IntVal(7)},
			{Name: "region", Value: table.StrVal("EU")},
		})
		left := typedJoinTable(t, []string{"profile"}, []*table.TypeDescriptor{schema}, [][]table.Value{{key}})
		right := typedJoinTable(t, []string{"profile", "amount"}, []*table.TypeDescriptor{schema, tdJoin(table.TypeInt)}, [][]table.Value{{key, table.IntVal(3)}})

		result := runTypedJoinPlanningQuery(t, left, right, "join right.csv on profile | count")
		if got := result.Get(0, "count"); got.Type != table.TypeInt || got.Int != 1 {
			t.Fatalf("count: got %v, want 1", got)
		}
	})

	t.Run("list_keys_match_exact_schema", func(t *testing.T) {
		schema := &table.TypeDescriptor{Kind: table.TypeList, Elem: tdJoin(table.TypeInt)}
		key := table.ListVal([]table.Value{table.IntVal(1), table.IntVal(2)})
		left := typedJoinTable(t, []string{"ids"}, []*table.TypeDescriptor{schema}, [][]table.Value{{key}})
		right := typedJoinTable(t, []string{"ids", "amount"}, []*table.TypeDescriptor{schema, tdJoin(table.TypeInt)}, [][]table.Value{{key, table.IntVal(5)}})

		result := runTypedJoinPlanningQuery(t, left, right, "join right.csv on ids | count")
		if got := result.Get(0, "count"); got.Type != table.TypeInt || got.Int != 1 {
			t.Fatalf("count: got %v, want 1", got)
		}
	})
}

func TestTypedJoinPlanningTDDAllowsNestedNullabilityAndUnionKeySchemas(t *testing.T) {
	t.Run("nested_record_nullability_is_ignored_for_key_compatibility", func(t *testing.T) {
		leftSchema := &table.TypeDescriptor{
			Kind: table.TypeRecord,
			Fields: []table.FieldDescriptor{
				{Name: "id", Type: &table.TypeDescriptor{Kind: table.TypeInt, Nullable: true}},
			},
		}
		rightSchema := &table.TypeDescriptor{
			Kind: table.TypeRecord,
			Fields: []table.FieldDescriptor{
				{Name: "id", Type: tdJoin(table.TypeInt)},
			},
		}
		key := table.RecordVal([]table.RecordField{{Name: "id", Value: table.IntVal(7)}})
		left := typedJoinTable(t, []string{"profile"}, []*table.TypeDescriptor{leftSchema}, [][]table.Value{{key}})
		right := typedJoinTable(t, []string{"profile"}, []*table.TypeDescriptor{rightSchema}, [][]table.Value{{key}})

		result := runTypedJoinPlanningQuery(t, left, right, "join right.csv on profile | count")
		if got := result.Get(0, "count"); got.Type != table.TypeInt || got.Int != 1 {
			t.Fatalf("count: got %v, want 1", got)
		}
	})

	t.Run("ordered_union_key_schemas_match_active_branch_values", func(t *testing.T) {
		schema := &table.TypeDescriptor{
			Kind: table.TypeUnion,
			Branches: []*table.TypeDescriptor{
				tdJoin(table.TypeInt),
				tdJoin(table.TypeString),
			},
		}
		left := typedJoinTable(t, []string{"u"}, []*table.TypeDescriptor{schema}, [][]table.Value{
			{table.IntVal(7)},
			{table.StrVal("7")},
		})
		right := typedJoinTable(t, []string{"u", "label"}, []*table.TypeDescriptor{schema, tdJoin(table.TypeString)}, [][]table.Value{
			{table.IntVal(7), table.StrVal("numeric")},
			{table.StrVal("7"), table.StrVal("text")},
		})

		result := runTypedJoinPlanningQuery(t, left, right, "join right.csv on u | count")
		if got := result.Get(0, "count"); got.Type != table.TypeInt || got.Int != 2 {
			t.Fatalf("count: got %v, want 2", got)
		}
	})
}

func TestTypedJoinPlanningTDDRejectsUnionOrderAndMixedKeySchemas(t *testing.T) {
	t.Run("union_branch_order_must_match", func(t *testing.T) {
		left := typedJoinTable(t, []string{"u"}, []*table.TypeDescriptor{{
			Kind: table.TypeUnion,
			Branches: []*table.TypeDescriptor{
				tdJoin(table.TypeInt),
				tdJoin(table.TypeString),
			},
		}}, nil)
		right := typedJoinTable(t, []string{"u"}, []*table.TypeDescriptor{{
			Kind: table.TypeUnion,
			Branches: []*table.TypeDescriptor{
				tdJoin(table.TypeString),
				tdJoin(table.TypeInt),
			},
		}}, nil)

		err := runTypedJoinPlanningQueryExpectErr(t, left, right, "join right.csv on u | count")
		requireErrContainsAll(t, err, "join", "key", "type", "u", "union<int,string>", "union<string,int>")
	})

	t.Run("mixed_schema_keys_are_rejected", func(t *testing.T) {
		left := typedJoinTable(t, []string{"xs"}, []*table.TypeDescriptor{{
			Kind: table.TypeList,
			Elem: &table.TypeDescriptor{Kind: table.TypeMixed},
		}}, nil)
		right := typedJoinTable(t, []string{"xs"}, []*table.TypeDescriptor{{
			Kind: table.TypeList,
			Elem: &table.TypeDescriptor{Kind: table.TypeMixed},
		}}, nil)

		err := runTypedJoinPlanningQueryExpectErr(t, left, right, "join right.csv on xs | count")
		requireErrContainsAll(t, err, "join", "key", "mixed")
	})
}

func TestTypedJoinPlanningTDDPlannerWrapperPreservesSchemas(t *testing.T) {
	left := typedJoinTable(t, []string{"id"}, []*table.TypeDescriptor{tdJoin(table.TypeInt)}, [][]table.Value{{table.IntVal(1)}})

	q, err := parser.Parse("left.csv | head 1 | count")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	plan, err := planPhysicalPipelineFromTableForTest(left, q.Ops)
	if err != nil {
		t.Fatalf("planPhysicalPipelineFromTableForTest: %v", err)
	}
	if got := plan.OutputSchema.Columns[0].Name; got != "count" {
		t.Fatalf("planned output column: got %q, want count", got)
	}
}

func TestTypedJoinPlanningTDDDotPathOuterRowsUsePlannedKeyBindings(t *testing.T) {
	profileSchema := &table.TypeDescriptor{
		Kind: table.TypeRecord,
		Fields: []table.FieldDescriptor{
			{Name: "id", Type: tdJoin(table.TypeInt)},
		},
	}

	t.Run("left_only_dot_path_key_fills_synthetic_column", func(t *testing.T) {
		left := typedJoinTable(t, []string{"profile", "name"}, []*table.TypeDescriptor{profileSchema, tdJoin(table.TypeString)}, [][]table.Value{
			{table.RecordVal([]table.RecordField{{Name: "id", Value: table.IntVal(7)}}), table.StrVal("left")},
		})
		right := typedJoinTable(t, []string{"id", "amount"}, []*table.TypeDescriptor{tdJoin(table.TypeInt), tdJoin(table.TypeInt)}, nil)

		result := runTypedJoinPlanningQuery(t, left, right, "join left right.csv on profile.id == id")
		if result.NumRows != 1 {
			t.Fatalf("rows: got %d, want 1", result.NumRows)
		}
		if got := result.Get(0, "profile_id"); got.Type != table.TypeInt || got.Int != 7 {
			t.Fatalf("profile_id: got %v, want 7", got)
		}
	})

	t.Run("right_only_dot_path_key_fills_synthetic_column", func(t *testing.T) {
		left := typedJoinTable(t, []string{"profile", "name"}, []*table.TypeDescriptor{profileSchema, tdJoin(table.TypeString)}, nil)
		right := typedJoinTable(t, []string{"id", "amount"}, []*table.TypeDescriptor{tdJoin(table.TypeInt), tdJoin(table.TypeInt)}, [][]table.Value{
			{table.IntVal(9), table.IntVal(90)},
		})

		result := runTypedJoinPlanningQuery(t, left, right, "join right right.csv on profile.id == id")
		if result.NumRows != 1 {
			t.Fatalf("rows: got %d, want 1", result.NumRows)
		}
		if got := result.Get(0, "profile_id"); got.Type != table.TypeInt || got.Int != 9 {
			t.Fatalf("profile_id: got %v, want 9", got)
		}
	})
}

func TestTypedJoinPlanningTDDDefensiveBoundJoinKeyBranches(t *testing.T) {
	intSchema := tdJoin(table.TypeInt)
	badKeys := []resolvedJoinKey{{column: boundColumn{topIndex: 99, rawPath: []string{"missing"}, typ: intSchema}}}
	left := typedJoinTable(t, []string{"id"}, []*table.TypeDescriptor{intSchema}, [][]table.Value{{table.IntVal(1)}})
	if _, _, err := joinKeyAt(left, badKeys, 0); err == nil {
		t.Fatal("expected joinKeyAt bound-column error")
	}
	if _, err := buildJoinIndex(left, badKeys); err == nil {
		t.Fatal("expected buildJoinIndex bound-column error")
	}

	joinSources := newLoadFuncJoinSourceProvider(func(string, ast.LoadOptions) (*table.Table, error) {
		return left, nil
	})
	rightSource, err := joinSources.PrepareJoinSource("right.csv", ast.LoadOptions{})
	if err != nil {
		t.Fatalf("prepare right source: %v", err)
	}
	plan := plannedJoin{
		plannedBase: plannedBaseFromTestSchema(rawSchemaFromColumns([]string{"id"}, []*table.TypeDescriptor{intSchema})),
		kind:        "inner",
		right: plannedJoinRightSource{
			source: rightSource,
			spec:   JoinSourceLoadSpec{Columns: table.AllColumns()},
			env:    mustSchemaEnvFromTable(left),
		},
		leftKeys:  []resolvedJoinKey{{column: boundColumn{topIndex: 0, rawPath: []string{"id"}, typ: intSchema}}},
		rightKeys: badKeys,
	}
	if _, err := execPlannedJoin(plan, left); err == nil {
		t.Fatal("expected execPlannedJoin index-build error")
	}
}

func TestTypedJoinPlanningTDDPipelinePlanningDiagnosticsAcrossJoin(t *testing.T) {
	left := typedJoinTable(t, []string{"id", "name"}, []*table.TypeDescriptor{
		tdJoin(table.TypeInt),
		tdJoin(table.TypeString),
	}, [][]table.Value{{table.IntVal(1), table.StrVal("left")}})
	right := typedJoinTable(t, []string{"id", "amount"}, []*table.TypeDescriptor{
		tdJoin(table.TypeInt),
		tdJoin(table.TypeInt),
	}, [][]table.Value{{table.IntVal(1), table.IntVal(10)}})

	err := runTypedJoinPlanningQueryExpectErr(t, left, right, `transform y = year("bad-date") | join right.csv on id | select missing | json`)
	requireErrContainsAll(t, err, "select", "missing", "not found")
	if strings.Contains(strings.ToLower(err.Error()), "year") {
		t.Fatalf("expected downstream planning error to win before runtime year() error, got: %v", err)
	}
}

func TestTypedJoinPlanningTDDOuterJoinSchemasStayConcrete(t *testing.T) {
	left := typedJoinTable(t, []string{"id", "name"}, []*table.TypeDescriptor{
		tdJoin(table.TypeInt),
		tdJoin(table.TypeString),
	}, nil)
	right := typedJoinTable(t, []string{"id", "amount"}, []*table.TypeDescriptor{
		tdJoin(table.TypeInt),
		tdJoin(table.TypeFloat),
	}, nil)

	result := runTypedJoinPlanningQuery(t, left, right, "join full right.csv on id | describe")
	assertDescribeSchemaRows(t, result, map[string]describeSchemaMeta{
		"id":     {typ: "int", rows: 0, schema: "int"},
		"name":   {typ: "string", rows: 0, schema: "string?"},
		"amount": {typ: "float", rows: 0, schema: "float?"},
	})
}

func TestTypedJoinPlanningTDDMergedKeySchemaIncludesKeyNullability(t *testing.T) {
	left := typedJoinTable(t, []string{"id", "name"}, []*table.TypeDescriptor{
		tdJoin(table.TypeInt),
		tdJoin(table.TypeString),
	}, [][]table.Value{{table.IntVal(1), table.StrVal("left")}})
	right := typedJoinTable(t, []string{"id", "amount"}, []*table.TypeDescriptor{
		{Kind: table.TypeInt, Nullable: true},
		tdJoin(table.TypeFloat),
	}, [][]table.Value{{table.IntVal(1), table.FloatVal(10)}})

	result := runTypedJoinPlanningQuery(t, left, right, "join full right.csv on id | describe")
	assertDescribeSchemaRows(t, result, map[string]describeSchemaMeta{
		"id":     {typ: "int", rows: 1, schema: "int?"},
		"name":   {typ: "string", rows: 1, schema: "string?"},
		"amount": {typ: "float", rows: 1, schema: "float?"},
	})
}

func tdJoin(kind table.ValueType) *table.TypeDescriptor {
	return &table.TypeDescriptor{Kind: kind}
}

func typedJoinTable(t *testing.T, cols []string, schemas []*table.TypeDescriptor, rows [][]table.Value) *table.Table {
	t.Helper()
	tbl := table.NewTableWithSchemas(cols, schemas)
	for _, row := range rows {
		if err := tbl.AddRowTyped(row); err != nil {
			t.Fatalf("seed typed join table: %v", err)
		}
	}
	return tbl
}

func runTypedJoinPlanningQuery(t *testing.T, left, right *table.Table, pipeline string) *table.Table {
	t.Helper()
	q, err := parser.Parse("left.csv | " + pipeline)
	if err != nil {
		t.Fatalf("parse %q: %v", pipeline, err)
	}
	result, err := Execute(q, left, typedJoinPlanningLoader(t, right))
	if err != nil {
		t.Fatalf("execute %q: %v", pipeline, err)
	}
	return result
}

func runTypedJoinPlanningQueryExpectErr(t *testing.T, left, right *table.Table, pipeline string) error {
	t.Helper()
	q, err := parser.Parse("left.csv | " + pipeline)
	if err != nil {
		t.Fatalf("parse %q: %v", pipeline, err)
	}
	_, err = Execute(q, left, typedJoinPlanningLoader(t, right))
	if err == nil {
		t.Fatalf("expected error for %q", pipeline)
	}
	return err
}

func typedJoinPlanningLoader(t *testing.T, right *table.Table) LoadFunc {
	t.Helper()
	return func(filename string, _ ast.LoadOptions) (*table.Table, error) {
		if filename != "right.csv" {
			t.Fatalf("unexpected join filename %q", filename)
		}
		return right, nil
	}
}

func requireErrContainsAll(t *testing.T, err error, wants ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error containing %v", wants)
	}
	msg := strings.ToLower(err.Error())
	for _, want := range wants {
		if !strings.Contains(msg, strings.ToLower(want)) {
			t.Fatalf("expected error containing %q, got: %v", want, err)
		}
	}
}
