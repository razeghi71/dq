package engine

import (
	"strings"
	"testing"

	"github.com/razeghi71/dq/table"
)

func assertColumnsExact(t *testing.T, result *table.Table, want ...string) {
	t.Helper()
	if len(result.Columns) != len(want) {
		t.Fatalf("columns: got %v, want %v", result.Columns, want)
	}
	for i, name := range want {
		if result.Columns[i] != name {
			t.Fatalf("columns: got %v, want %v", result.Columns, want)
		}
	}
}

func TestDistinctProjectionSemanticsTDDFlatTables(t *testing.T) {
	t.Run("single_key_projects_only_key_column", func(t *testing.T) {
		result := runQuery(t, usersTable(), "distinct city")
		assertColumnsExact(t, result, "city")
		if result.NumRows != 3 {
			t.Fatalf("rows: got %d, want 3", result.NumRows)
		}
		want := []string{"NY", "LA", "SF"}
		for i, city := range want {
			if got := result.GetAt(i, 0); got.Type != table.TypeString || got.Str != city {
				t.Fatalf("row %d city: got %v, want %q", i, got, city)
			}
		}
	})

	t.Run("multi_key_projects_only_requested_columns", func(t *testing.T) {
		result := runQuery(t, usersTable(), "distinct city, age")
		assertColumnsExact(t, result, "city", "age")
		if result.NumRows != 6 {
			t.Fatalf("rows: got %d, want 6", result.NumRows)
		}
	})

	t.Run("unkeyed_distinct_still_deduplicates_full_rows", func(t *testing.T) {
		tbl := table.NewTable([]string{"name", "age"})
		tbl.AddRow([]table.Value{table.StrVal("Alice"), table.IntVal(30)})
		tbl.AddRow([]table.Value{table.StrVal("Alice"), table.IntVal(30)})
		tbl.AddRow([]table.Value{table.StrVal("Alice"), table.IntVal(31)})

		result := runQuery(t, tbl, "distinct | sort age")
		assertColumnsExact(t, result, "name", "age")
		if result.NumRows != 2 {
			t.Fatalf("rows: got %d, want 2", result.NumRows)
		}
	})

	t.Run("projected_away_columns_are_not_available_downstream", func(t *testing.T) {
		err := runQueryExpectErr(t, usersTable(), "distinct city | select name")
		if err == nil {
			t.Fatal("expected select name to fail after keyed distinct projects name away")
		}
		if !strings.Contains(err.Error(), "name") || !strings.Contains(err.Error(), "not found") {
			t.Fatalf("error: got %v, want missing name", err)
		}
	})
}

func TestDistinctProjectionSemanticsTDDLoadedFlatFormats(t *testing.T) {
	for _, input := range flatUserFormatFiles() {
		t.Run(input.name, func(t *testing.T) {
			rows := loadAndQuery(t, input.file, "distinct city | describe")
			assertDescribeSchemaRows(t, rows, map[string]describeSchemaMeta{
				"city": {typ: "string", rows: int64(loadAndQuery(t, input.file, "distinct city").NumRows), schema: "string"},
			})

			result := loadAndQuery(t, input.file, "distinct city | sort city")
			assertColumnsExact(t, result, "city")
			for i := 1; i < result.NumRows; i++ {
				prev := result.GetAt(i-1, 0).Str
				curr := result.GetAt(i, 0).Str
				if prev >= curr {
					t.Fatalf("cities not unique/sorted at row %d: %q then %q", i, prev, curr)
				}
			}
		})
	}
}

func TestDistinctProjectionSemanticsTDDZeroRows(t *testing.T) {
	t.Run("keyed_distinct_zero_rows_projects_schema", func(t *testing.T) {
		result := runQuery(t, usersTable(), "filter { false } | distinct city | describe")
		assertDescribeSchemaRows(t, result, map[string]describeSchemaMeta{
			"city": {typ: "string", rows: 0, schema: "string"},
		})
	})

	t.Run("multi_key_zero_rows_projects_schema", func(t *testing.T) {
		result := runQuery(t, usersTable(), "filter { false } | distinct city, age | describe")
		assertDescribeSchemaRows(t, result, map[string]describeSchemaMeta{
			"city": {typ: "string", rows: 0, schema: "string"},
			"age":  {typ: "int", rows: 0, schema: "int"},
		})
	})

	t.Run("unkeyed_zero_rows_preserves_full_schema", func(t *testing.T) {
		result := runQuery(t, usersTable(), "filter { false } | distinct | describe")
		assertDescribeSchemaRows(t, result, map[string]describeSchemaMeta{
			"name": {typ: "string", rows: 0, schema: "string"},
			"age":  {typ: "int", rows: 0, schema: "int"},
			"city": {typ: "string", rows: 0, schema: "string"},
		})
	})
}

func TestDistinctProjectionSemanticsTDDNestedProjectionRules(t *testing.T) {
	t.Run("dot_path_uses_select_output_name_and_schema", func(t *testing.T) {
		result := runQuery(t, nestedTable(), "distinct address.city | sort address_city")
		assertColumnsExact(t, result, "address_city")
		if result.NumRows != 2 {
			t.Fatalf("rows: got %d, want 2", result.NumRows)
		}
	})

	t.Run("dot_path_name_collisions_match_select_suffixing", func(t *testing.T) {
		tbl := table.NewTable([]string{"address_city", "address"})
		tbl.AddRow([]table.Value{
			table.StrVal("existing"),
			table.RecordVal([]table.RecordField{{Name: "city", Value: table.StrVal("New York")}}),
		})
		tbl.AddRow([]table.Value{
			table.StrVal("existing"),
			table.RecordVal([]table.RecordField{{Name: "city", Value: table.StrVal("New York")}}),
		})

		result := runQuery(t, tbl, "distinct address_city, address.city")
		assertColumnsExact(t, result, "address_city", "address_city_2")
		if result.NumRows != 1 {
			t.Fatalf("rows: got %d, want 1", result.NumRows)
		}
	})

	t.Run("nullable_parent_projected_child_is_nullable", func(t *testing.T) {
		result := runQuery(t, optionalNestedTable(), "distinct addr.city | sort addr_city")
		assertColumnsExact(t, result, "addr_city")
		if result.NumRows != 2 {
			t.Fatalf("rows: got %d, want 2", result.NumRows)
		}

		describe := runQuery(t, optionalNestedTable(), "filter { false } | distinct addr.city | describe")
		assertDescribeSchemaRows(t, describe, map[string]describeSchemaMeta{
			"addr_city": {typ: "string", rows: 0, schema: "string?"},
		})
	})
}

func TestDistinctProjectionSemanticsTDDCombinations(t *testing.T) {
	t.Run("sort_select_count_after_keyed_distinct", func(t *testing.T) {
		result := runQuery(t, usersTable(), "filter { age > 20 } | distinct city | sort city | select city | count")
		assertColumnsExact(t, result, "count")
		if got := result.GetAt(0, 0); got.Type != table.TypeInt || got.Int != 3 {
			t.Fatalf("count: got %v, want 3", got)
		}
	})

	t.Run("group_reduce_after_keyed_distinct_sees_projected_schema", func(t *testing.T) {
		result := runQuery(t, usersTable(), "distinct city | group city | reduce n = count() | remove grouped | sort city")
		assertColumnsExact(t, result, "city", "n")
		if result.NumRows != 3 {
			t.Fatalf("rows: got %d, want 3", result.NumRows)
		}
		for i := 0; i < result.NumRows; i++ {
			if got := result.GetAt(i, 1); got.Type != table.TypeInt || got.Int != 1 {
				t.Fatalf("row %d count: got %v, want 1", i, got)
			}
		}
	})
}

func TestDistinctProjectionSemanticsTDDErrors(t *testing.T) {
	cases := []struct {
		name  string
		input *table.Table
		query string
		wants []string
	}{
		{
			name:  "missing_top_level",
			input: usersTable(),
			query: "filter { false } | distinct missing",
			wants: []string{"distinct", "missing", "not found"},
		},
		{
			name:  "missing_nested_field",
			input: nestedTable(),
			query: "filter { false } | distinct address.missing",
			wants: []string{"distinct", "missing", "not found"},
		},
		{
			name:  "list_traversal",
			input: distinctProjectionListTraversalTable(),
			query: "filter { false } | distinct orders.amount",
			wants: []string{"orders", "list"},
		},
		{
			name:  "union_branch_traversal",
			input: unionRecordBranchTable(t, true),
			query: "filter { false } | distinct u.x",
			wants: []string{"union"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := runQueryExpectErr(t, tc.input, tc.query)
			if err == nil {
				t.Fatalf("expected error for %q", tc.query)
			}
			msg := strings.ToLower(err.Error())
			for _, want := range tc.wants {
				if !strings.Contains(msg, strings.ToLower(want)) {
					t.Fatalf("error: got %v, want substring %q", err, want)
				}
			}
		})
	}
}

func distinctProjectionListTraversalTable() *table.Table {
	return table.NewTableWithSchemas(
		[]string{"orders"},
		[]*table.TypeDescriptor{{
			Kind: table.TypeList,
			Elem: &table.TypeDescriptor{
				Kind: table.TypeRecord,
				Fields: []table.FieldDescriptor{
					{Name: "amount", Type: &table.TypeDescriptor{Kind: table.TypeInt}},
				},
			},
		}},
	)
}

func TestDistinctProjectionSemanticsTDDExactStructuralKeys(t *testing.T) {
	tbl := table.NewTableWithSchemas(
		[]string{"v", "payload"},
		[]*table.TypeDescriptor{
			{Kind: table.TypeUnion, Branches: []*table.TypeDescriptor{{Kind: table.TypeInt}, {Kind: table.TypeString}}},
			{Kind: table.TypeString},
		},
	)
	rows := [][]table.Value{
		{table.IntVal(7), table.StrVal("int-seven")},
		{table.StrVal("7"), table.StrVal("string-seven")},
		{table.IntVal(7), table.StrVal("duplicate-int-seven")},
	}
	for _, row := range rows {
		if err := tbl.AddRowTyped(row); err != nil {
			t.Fatalf("add row: %v", err)
		}
	}

	result := runQuery(t, tbl, "distinct v")
	assertColumnsExact(t, result, "v")
	if result.NumRows != 2 {
		t.Fatalf("rows: got %d, want 2", result.NumRows)
	}
}

func TestCanonicalTupleKeyDoesNotCollideOnEmbeddedSeparators(t *testing.T) {
	left := canonicalTupleKey([]string{"x\x00y", "z"})
	right := canonicalTupleKey([]string{"x", "y\x00z"})
	if left == right {
		t.Fatalf("tuple keys collided: %q", left)
	}

	single := "x\x00y"
	if got := canonicalTupleKey([]string{single}); got != single {
		t.Fatalf("single tuple key: got %q, want %q", got, single)
	}
}
