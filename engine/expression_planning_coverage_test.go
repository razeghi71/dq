package engine

import (
	"math"
	"strings"
	"testing"

	"github.com/razeghi71/dq/ast"
	"github.com/razeghi71/dq/table"
)

func TestEvalComparisonRuntimeEdgeCases(t *testing.T) {
	// Query syntax rejects ==/!= null in favor of is null / is not null.
	// Keep this test scoped to evaluator edge cases that are part of planned
	// comparison execution, not user-facing null-check syntax.
	cases := []struct {
		name     string
		op       string
		left     table.Value
		right    table.Value
		wantBool bool
		wantNull bool
		wantErr  string
	}{
		{
			name:     "null_null_ordering_returns_null",
			op:       "<",
			left:     table.Null(),
			right:    table.Null(),
			wantNull: true,
		},
		{
			name:     "null_null_equality_returns_null",
			op:       "==",
			left:     table.Null(),
			right:    table.Null(),
			wantNull: true,
		},
		{
			name:     "null_null_not_equal_returns_null",
			op:       "!=",
			left:     table.Null(),
			right:    table.Null(),
			wantNull: true,
		},
		{
			name:     "null_equality_returns_null",
			op:       "==",
			left:     table.Null(),
			right:    table.IntVal(1),
			wantNull: true,
		},
		{
			name:     "null_not_equal_returns_null",
			op:       "!=",
			left:     table.IntVal(1),
			right:    table.Null(),
			wantNull: true,
		},
		{
			name:     "null_ordering_returns_null",
			op:       "<",
			left:     table.Null(),
			right:    table.IntVal(1),
			wantNull: true,
		},
		{
			name:     "nan_ordering_is_unordered_and_false",
			op:       "<",
			left:     table.FloatVal(math.NaN()),
			right:    table.IntVal(1),
			wantBool: false,
		},
		{
			name:     "nan_not_equal_is_true",
			op:       "!=",
			left:     table.FloatVal(math.NaN()),
			right:    table.FloatVal(math.NaN()),
			wantBool: true,
		},
		{
			name:     "large_int_and_rounded_float_equality_is_false",
			op:       "==",
			left:     table.IntVal(9007199254740993),
			right:    table.FloatVal(9007199254740992),
			wantBool: false,
		},
		{
			name:     "large_int_and_rounded_float_not_equal_is_true",
			op:       "!=",
			left:     table.IntVal(9007199254740993),
			right:    table.FloatVal(9007199254740992),
			wantBool: true,
		},
		{
			name:     "large_int_orders_above_rounded_float",
			op:       ">",
			left:     table.IntVal(9007199254740993),
			right:    table.FloatVal(9007199254740992),
			wantBool: true,
		},
		{
			name:     "rounded_float_orders_below_large_int",
			op:       "<",
			left:     table.FloatVal(9007199254740992),
			right:    table.IntVal(9007199254740993),
			wantBool: true,
		},
		{
			name:     "int_orders_below_positive_infinity",
			op:       "<",
			left:     table.IntVal(1),
			right:    table.FloatVal(math.Inf(1)),
			wantBool: true,
		},
		{
			name:     "int_orders_above_negative_infinity",
			op:       ">",
			left:     table.IntVal(1),
			right:    table.FloatVal(math.Inf(-1)),
			wantBool: true,
		},
		{
			name:     "positive_infinity_orders_above_int",
			op:       ">",
			left:     table.FloatVal(math.Inf(1)),
			right:    table.IntVal(1),
			wantBool: true,
		},
		{
			name:     "negative_infinity_orders_below_int",
			op:       "<",
			left:     table.FloatVal(math.Inf(-1)),
			right:    table.IntVal(1),
			wantBool: true,
		},
		{
			name:     "string_ordering",
			op:       "<",
			left:     table.StrVal("alpha"),
			right:    table.StrVal("beta"),
			wantBool: true,
		},
		{
			name:     "string_greater_equal_equal_value",
			op:       ">=",
			left:     table.StrVal("beta"),
			right:    table.StrVal("beta"),
			wantBool: true,
		},
		{
			name:     "string_greater_than",
			op:       ">",
			left:     table.StrVal("beta"),
			right:    table.StrVal("alpha"),
			wantBool: true,
		},
		{
			name:     "bool_equality",
			op:       "==",
			left:     table.BoolVal(true),
			right:    table.BoolVal(false),
			wantBool: false,
		},
		{
			name:    "string_int_equality_errors",
			op:      "==",
			left:    table.StrVal("1"),
			right:   table.IntVal(1),
			wantErr: "cannot compare string with int",
		},
		{
			name:    "bool_ordering_errors",
			op:      "<",
			left:    table.BoolVal(true),
			right:   table.BoolVal(false),
			wantErr: "cannot compare bool with bool",
		},
		{
			name:    "string_int_ordering_errors",
			op:      "<",
			left:    table.StrVal("1"),
			right:   table.IntVal(1),
			wantErr: "cannot compare string with int",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := evalComparison(tc.op, tc.left, tc.right)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("expected error containing %q, got value=%v err=%v", tc.wantErr, got, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantNull {
				if !got.IsNull() {
					t.Fatalf("expected null, got %v", got)
				}
				return
			}
			if got.Type != table.TypeBool || got.Bool != tc.wantBool {
				t.Fatalf("expected bool %v, got %v", tc.wantBool, got)
			}
		})
	}
}

func TestCompileScalarColumnLiteralPredicateCoversNumericStorage(t *testing.T) {
	floatTable := table.NewTable([]string{"score"})
	floatTable.AddRow([]table.Value{table.FloatVal(1.5)})
	floatTable.AddRow([]table.Value{table.FloatVal(2)})
	floatTable.AddRow([]table.Value{table.FloatVal(3.5)})
	floatTable.AddRow([]table.Value{table.Null()})
	floatTable.AddRow([]table.Value{table.FloatVal(math.NaN())})

	cases := []struct {
		name     string
		op       string
		lit      table.Value
		reversed bool
		want     []bool
	}{
		{
			name: "float_greater_than_float_literal",
			op:   ">",
			lit:  table.FloatVal(2),
			want: []bool{false, false, true, false, false},
		},
		{
			name:     "reversed_float_literal_less_than_column",
			op:       "<",
			lit:      table.FloatVal(2),
			reversed: true,
			want:     []bool{false, false, true, false, false},
		},
		{
			name: "float_equal_int_literal",
			op:   "==",
			lit:  table.IntVal(2),
			want: []bool{false, true, false, false, false},
		},
		{
			name: "float_not_equal_nan_literal",
			op:   "!=",
			lit:  table.FloatVal(math.NaN()),
			want: []bool{true, true, true, false, true},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pred, ok := compileScalarColumnLiteralPredicate(floatTable.Col(0), tc.op, tc.lit, tc.reversed)
			if !ok {
				t.Fatal("expected compiled float predicate")
			}
			for row, want := range tc.want {
				got, err := pred(row)
				if err != nil {
					t.Fatalf("row %d: unexpected error: %v", row, err)
				}
				if got != want {
					t.Fatalf("row %d: got %v, want %v", row, got, want)
				}
			}
		})
	}
}

func TestCompileScalarColumnLiteralPredicateKeepsIntFloatExact(t *testing.T) {
	intTable := table.NewTable([]string{"id"})
	intTable.AddRow([]table.Value{table.IntVal(2)})
	intTable.AddRow([]table.Value{table.IntVal(9007199254740993)})
	intTable.AddRow([]table.Value{table.Null()})

	pred, ok := compileScalarColumnLiteralPredicate(intTable.Col(0), "==", table.FloatVal(9007199254740992), false)
	if !ok {
		t.Fatal("expected compiled int/float predicate")
	}
	for row, want := range []bool{false, false, false} {
		got, err := pred(row)
		if err != nil {
			t.Fatalf("row %d: unexpected error: %v", row, err)
		}
		if got != want {
			t.Fatalf("row %d: got %v, want %v", row, got, want)
		}
	}

	pred, ok = compileScalarColumnLiteralPredicate(intTable.Col(0), "==", table.FloatVal(2), false)
	if !ok {
		t.Fatal("expected compiled safe-range int/float predicate")
	}
	got, err := pred(0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got {
		t.Fatal("expected int 2 to equal float 2.0")
	}
}

func TestExpressionValuesEqualNestedRuntimeEdges(t *testing.T) {
	t.Run("top_level_type_mismatch_errors", func(t *testing.T) {
		eq, err := expressionValuesEqual(table.IntVal(1), table.StrVal("1"), true)
		if err == nil || eq {
			t.Fatalf("expected mismatch error and false equality, got eq=%v err=%v", eq, err)
		}
	})

	t.Run("nested_numeric_records_match_by_name_and_promote_int_float", func(t *testing.T) {
		left := table.RecordVal([]table.RecordField{
			{Name: "name", Value: table.StrVal("a")},
			{Name: "score", Value: table.IntVal(7)},
		})
		right := table.RecordVal([]table.RecordField{
			{Name: "score", Value: table.FloatVal(7)},
			{Name: "name", Value: table.StrVal("a")},
		})
		eq, err := expressionValuesEqual(left, right, true)
		if err != nil || !eq {
			t.Fatalf("expected reordered numeric record equality, got eq=%v err=%v", eq, err)
		}
	})

	t.Run("nested_large_int_float_records_remain_distinct", func(t *testing.T) {
		left := table.RecordVal([]table.RecordField{{Name: "id", Value: table.IntVal(9007199254740993)}})
		right := table.RecordVal([]table.RecordField{{Name: "id", Value: table.FloatVal(9007199254740992)}})
		eq, err := expressionValuesEqual(left, right, true)
		if err != nil || eq {
			t.Fatalf("expected large int and rounded float record values to differ, got eq=%v err=%v", eq, err)
		}
	})

	t.Run("nested_large_int_float_lists_remain_distinct", func(t *testing.T) {
		eq, err := expressionValuesEqual(
			table.ListVal([]table.Value{table.IntVal(9007199254740993)}),
			table.ListVal([]table.Value{table.FloatVal(9007199254740992)}),
			true,
		)
		if err != nil || eq {
			t.Fatalf("expected large int and rounded float list values to differ, got eq=%v err=%v", eq, err)
		}
	})

	t.Run("nested_list_length_mismatch_is_false", func(t *testing.T) {
		eq, err := expressionValuesEqual(
			table.ListVal([]table.Value{table.IntVal(1)}),
			table.ListVal([]table.Value{table.IntVal(1), table.IntVal(2)}),
			true,
		)
		if err != nil || eq {
			t.Fatalf("expected list length mismatch to be false without error, got eq=%v err=%v", eq, err)
		}
	})

	t.Run("nested_type_mismatch_is_false_not_error", func(t *testing.T) {
		eq, err := expressionValuesEqual(
			table.ListVal([]table.Value{table.IntVal(1)}),
			table.ListVal([]table.Value{table.StrVal("1")}),
			true,
		)
		if err != nil || eq {
			t.Fatalf("expected nested mismatch to be false without error, got eq=%v err=%v", eq, err)
		}
	})

	t.Run("record_missing_null_field_is_true", func(t *testing.T) {
		eq, err := expressionValuesEqual(
			table.RecordVal([]table.RecordField{{Name: "x", Value: table.IntVal(1)}}),
			table.RecordVal([]table.RecordField{
				{Name: "x", Value: table.IntVal(1)},
				{Name: "y", Value: table.Null()},
			}),
			true,
		)
		if err != nil || !eq {
			t.Fatalf("expected missing null field to compare equal, got eq=%v err=%v", eq, err)
		}
	})

	t.Run("record_missing_null_field_is_symmetric", func(t *testing.T) {
		eq, err := expressionValuesEqual(
			table.RecordVal([]table.RecordField{
				{Name: "x", Value: table.IntVal(1)},
				{Name: "y", Value: table.Null()},
			}),
			table.RecordVal([]table.RecordField{{Name: "x", Value: table.IntVal(1)}}),
			true,
		)
		if err != nil || !eq {
			t.Fatalf("expected symmetric missing null field equality, got eq=%v err=%v", eq, err)
		}
	})

	t.Run("record_empty_equals_all_null_missing_fields", func(t *testing.T) {
		eq, err := expressionValuesEqual(
			table.RecordVal(nil),
			table.RecordVal([]table.RecordField{
				{Name: "x", Value: table.Null()},
				{Name: "y", Value: table.Null()},
			}),
			true,
		)
		if err != nil || !eq {
			t.Fatalf("expected empty record to equal all-null missing fields, got eq=%v err=%v", eq, err)
		}
	})

	t.Run("nested_record_missing_null_field_is_true", func(t *testing.T) {
		eq, err := expressionValuesEqual(
			table.RecordVal([]table.RecordField{
				{Name: "profile", Value: table.RecordVal([]table.RecordField{{Name: "x", Value: table.IntVal(1)}})},
			}),
			table.RecordVal([]table.RecordField{
				{Name: "profile", Value: table.RecordVal([]table.RecordField{
					{Name: "x", Value: table.IntVal(1)},
					{Name: "y", Value: table.Null()},
				})},
			}),
			true,
		)
		if err != nil || !eq {
			t.Fatalf("expected nested missing null field equality, got eq=%v err=%v", eq, err)
		}
	})

	t.Run("list_record_missing_null_field_is_true", func(t *testing.T) {
		eq, err := expressionValuesEqual(
			table.ListVal([]table.Value{
				table.RecordVal([]table.RecordField{{Name: "x", Value: table.IntVal(1)}}),
			}),
			table.ListVal([]table.Value{
				table.RecordVal([]table.RecordField{
					{Name: "x", Value: table.IntVal(1)},
					{Name: "y", Value: table.Null()},
				}),
			}),
			true,
		)
		if err != nil || !eq {
			t.Fatalf("expected list record missing null equality, got eq=%v err=%v", eq, err)
		}
	})

	t.Run("record_missing_non_null_field_is_false", func(t *testing.T) {
		eq, err := expressionValuesEqual(
			table.RecordVal([]table.RecordField{{Name: "x", Value: table.IntVal(1)}}),
			table.RecordVal([]table.RecordField{
				{Name: "x", Value: table.IntVal(1)},
				{Name: "y", Value: table.IntVal(1)},
			}),
			true,
		)
		if err != nil || eq {
			t.Fatalf("expected missing non-null field to be false without error, got eq=%v err=%v", eq, err)
		}
	})

	t.Run("nested_record_missing_non_null_field_is_false", func(t *testing.T) {
		eq, err := expressionValuesEqual(
			table.RecordVal([]table.RecordField{
				{Name: "profile", Value: table.RecordVal([]table.RecordField{{Name: "x", Value: table.IntVal(1)}})},
			}),
			table.RecordVal([]table.RecordField{
				{Name: "profile", Value: table.RecordVal([]table.RecordField{
					{Name: "x", Value: table.IntVal(1)},
					{Name: "y", Value: table.StrVal("present")},
				})},
			}),
			true,
		)
		if err != nil || eq {
			t.Fatalf("expected nested missing non-null field to be false without error, got eq=%v err=%v", eq, err)
		}
	})

	t.Run("record_duplicate_field_is_false", func(t *testing.T) {
		eq, err := expressionValuesEqual(
			table.RecordVal([]table.RecordField{
				{Name: "x", Value: table.IntVal(1)},
				{Name: "x", Value: table.IntVal(1)},
			}),
			table.RecordVal([]table.RecordField{
				{Name: "x", Value: table.IntVal(1)},
				{Name: "x", Value: table.IntVal(1)},
			}),
			true,
		)
		if err != nil || eq {
			t.Fatalf("expected duplicate record fields to be false without error, got eq=%v err=%v", eq, err)
		}
	})

	t.Run("record_duplicate_field_on_right_is_false", func(t *testing.T) {
		eq, err := expressionValuesEqual(
			table.RecordVal([]table.RecordField{{Name: "x", Value: table.IntVal(1)}}),
			table.RecordVal([]table.RecordField{
				{Name: "x", Value: table.IntVal(1)},
				{Name: "x", Value: table.IntVal(1)},
			}),
			true,
		)
		if err != nil || eq {
			t.Fatalf("expected duplicate right record field to be false without error, got eq=%v err=%v", eq, err)
		}
	})

	t.Run("unsupported_top_level_same_type_errors", func(t *testing.T) {
		eq, err := expressionValuesEqual(table.Value{Type: table.TypeUnion}, table.Value{Type: table.TypeUnion}, true)
		if err == nil || eq {
			t.Fatalf("expected unsupported top-level type to error, got eq=%v err=%v", eq, err)
		}
	})

	t.Run("unsupported_nested_same_type_is_false", func(t *testing.T) {
		eq, err := expressionValuesEqual(table.Value{Type: table.TypeUnion}, table.Value{Type: table.TypeUnion}, false)
		if err != nil || eq {
			t.Fatalf("expected unsupported nested type to be false without error, got eq=%v err=%v", eq, err)
		}
	})
}

func TestExpressionPlannerMixedUnificationEdges(t *testing.T) {
	expectQueryErrContains(t, usersTable(), `filter { false } | transform xs = if(age > 30, list(1), list("x"))`, "if() branches do not have one common type")
	expectQueryErrContains(t, usersTable(), `filter { false } | transform xs = coalesce(list(1), list("x"))`, "coalesce() arguments do not have one common type")

	result := runQuery(t, usersTable(), `filter { false } | transform xs = if(age > 30, list(1, "x"), list(2, "y")), ys = coalesce(list(1, "x"), list(2, "y")) | describe`)
	assertDescribeSchemaRows(t, result, map[string]describeSchemaMeta{
		"name": {typ: "string", rows: 0, schema: "string"},
		"age":  {typ: "int", rows: 0, schema: "int"},
		"city": {typ: "string", rows: 0, schema: "string"},
		"xs":   {typ: "list", rows: 0, schema: "list<mixed>"},
		"ys":   {typ: "list", rows: 0, schema: "list<mixed>"},
	})
}

func TestExpressionPlannerEmptyListLiteralAdoptsTypedListContext(t *testing.T) {
	result := loadAndQuery(t, testdataDir+"/nested.json", `transform maybe_orders = if(name == "Alice", orders, list()) | select maybe_orders | describe`)
	assertDescribeSchemaRows(t, result, map[string]describeSchemaMeta{
		"maybe_orders": {typ: "list", rows: 3, schema: "list<record<amount:float, order_id:int, status:string>>"},
	})

	result = loadAndQuery(t, testdataDir+"/nested.json", `filter { false } | transform maybe_orders = if(name == "Alice", orders, list()), fallback_orders = coalesce(orders, list()), first_orders = coalesce(list(), orders) | select maybe_orders, fallback_orders, first_orders | describe`)
	assertDescribeSchemaRows(t, result, map[string]describeSchemaMeta{
		"maybe_orders":    {typ: "list", rows: 0, schema: "list<record<amount:float, order_id:int, status:string>>"},
		"fallback_orders": {typ: "list", rows: 0, schema: "list<record<amount:float, order_id:int, status:string>>"},
		"first_orders":    {typ: "list", rows: 0, schema: "list<record<amount:float, order_id:int, status:string>>"},
	})

	result = runQuery(t, usersTable(), `filter { false } | transform ints = if(age > 30, list(age), list()), nested = if(age > 30, list(list(age)), list()), recs = coalesce(list(), list(struct(age = age))) | select ints, nested, recs | describe`)
	assertDescribeSchemaRows(t, result, map[string]describeSchemaMeta{
		"ints":   {typ: "list", rows: 0, schema: "list<int>"},
		"nested": {typ: "list", rows: 0, schema: "list<list<int>>"},
		"recs":   {typ: "list", rows: 0, schema: "list<record<age:int>>"},
	})

	result = runQuery(t, usersTable(), `transform left_empty = if(age == 30, list(), list(1)), right_empty = if(age == 30, list(age), list()), first_empty = coalesce(list(), list(1)) | select left_empty, right_empty, first_empty | describe`)
	assertDescribeSchemaRows(t, result, map[string]describeSchemaMeta{
		"left_empty":  {typ: "list", rows: 6, schema: "list<int>"},
		"right_empty": {typ: "list", rows: 6, schema: "list<int>"},
		"first_empty": {typ: "list", rows: 6, schema: "list<int>"},
	})
}

func TestExpressionPlannerEmptyListLiteralStillFinalizesStandalone(t *testing.T) {
	result := runQuery(t, usersTable(), `filter { false } | transform xs = list(), ys = list(null), nested = list(list()) | select xs, ys, nested | describe`)
	assertDescribeSchemaRows(t, result, map[string]describeSchemaMeta{
		"xs":     {typ: "list", rows: 0, schema: "list<string?>"},
		"ys":     {typ: "list", rows: 0, schema: "list<string?>"},
		"nested": {typ: "list", rows: 0, schema: "list<list<string?>>"},
	})
}

func TestExpressionPlannerEmptyListLiteralInMembershipAndEquality(t *testing.T) {
	result := runQuery(t, usersTable(), `filter { list_contains(list(), 1) } | count`)
	if got := result.Get(0, "count"); got.Type != table.TypeInt || got.Int != 0 {
		t.Fatalf("list_contains(list(), 1): got %v, want count 0", got)
	}

	result = runQuery(t, usersTable(), `filter { list() == list(age) } | count`)
	if got := result.Get(0, "count"); got.Type != table.TypeInt || got.Int != 0 {
		t.Fatalf("list() == list(age): got %v, want count 0", got)
	}

	result = runQuery(t, usersTable(), `filter { list() == list() } | count`)
	if got := result.Get(0, "count"); got.Type != table.TypeInt || got.Int != int64(usersTable().NumRows) {
		t.Fatalf("list() == list(): got %v, want all rows", got)
	}
}

func TestExpressionPlannerRejectsAdditionalTypeErrorsBeforeRows(t *testing.T) {
	cases := []struct {
		name  string
		query string
		want  string
	}{
		{
			name:  "aggregate_function_outside_reduce",
			query: `filter { false } | transform c = count()`,
			want:  `aggregate function "count" can only be used inside 'reduce'`,
		},
		{
			name:  "list_element_type_error",
			query: `filter { false } | transform xs = list(upper(age))`,
			want:  `upper() requires a string`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expectQueryErrContains(t, usersTable(), tc.query, tc.want)
		})
	}
}

func TestFilterTypeErrorsUseSpecificExpressionLabels(t *testing.T) {
	cases := []struct {
		name  string
		query string
		want  string
	}{
		{name: "column", query: `filter { age }`, want: "from age"},
		{name: "function", query: `filter { upper(name) }`, want: "from upper()"},
		{name: "unary", query: `filter { -age }`, want: "from -"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expectQueryErrContains(t, usersTable(), tc.query, tc.want)
		})
	}
}

func TestSchemaContainsMixedTraversesNestedSchemas(t *testing.T) {
	cases := []struct {
		name   string
		schema *table.TypeDescriptor
		want   bool
	}{
		{name: "nil", schema: nil, want: false},
		{name: "scalar", schema: &table.TypeDescriptor{Kind: table.TypeInt}, want: false},
		{name: "mixed", schema: &table.TypeDescriptor{Kind: table.TypeMixed}, want: true},
		{
			name: "record_field",
			schema: &table.TypeDescriptor{Kind: table.TypeRecord, Fields: []table.FieldDescriptor{
				{Name: "payload", Type: &table.TypeDescriptor{Kind: table.TypeMixed}},
			}},
			want: true,
		},
		{
			name:   "list_elem",
			schema: &table.TypeDescriptor{Kind: table.TypeList, Elem: &table.TypeDescriptor{Kind: table.TypeMixed}},
			want:   true,
		},
		{
			name: "union_branch",
			schema: &table.TypeDescriptor{Kind: table.TypeUnion, Branches: []*table.TypeDescriptor{
				{Kind: table.TypeString},
				{Kind: table.TypeMixed},
			}},
			want: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := schemaContainsMixed(tc.schema); got != tc.want {
				t.Fatalf("schemaContainsMixed(%s): got %v, want %v", table.Render(tc.schema), got, tc.want)
			}
		})
	}
}

func TestPlannerSchemaHelpersHandleNilSchemas(t *testing.T) {
	if schemaKindOrNull(nil, table.TypeString) {
		t.Fatal("nil schema should not match a scalar kind")
	}
	if schemaOrderableOrNull(nil) {
		t.Fatal("nil schema should not be orderable")
	}
	if got := schemaTypeName(nil); got != "null" {
		t.Fatalf("nil schema type name: got %s, want null", got)
	}
}

func TestCheckBinarySignaturePlannerEdges(t *testing.T) {
	intType := &table.TypeDescriptor{Kind: table.TypeInt}
	nullableIntType := &table.TypeDescriptor{Kind: table.TypeInt, Nullable: true}
	stringType := &table.TypeDescriptor{Kind: table.TypeString}
	boolType := &table.TypeDescriptor{Kind: table.TypeBool}
	nullType := &table.TypeDescriptor{Kind: table.TypeNull, Nullable: true}
	recordType := &table.TypeDescriptor{Kind: table.TypeRecord, Fields: []table.FieldDescriptor{
		{Name: "x", Type: intType},
	}}
	unionType := &table.TypeDescriptor{Kind: table.TypeUnion, Branches: []*table.TypeDescriptor{
		intType,
		stringType,
	}}

	cases := []struct {
		name       string
		op         string
		left       typedExpr
		right      typedExpr
		wantSchema string
		wantErr    string
	}{
		{
			name:       "string_plus_string",
			op:         "+",
			left:       typedExpr{typ: stringType},
			right:      typedExpr{typ: stringType},
			wantSchema: "string",
		},
		{
			name:    "plus_string_and_int_rejects",
			op:      "+",
			left:    typedExpr{typ: stringType},
			right:   typedExpr{typ: intType},
			wantErr: "operator + requires numeric operands",
		},
		{
			name:    "equality_null_rejects",
			op:      "==",
			left:    typedExpr{typ: intType},
			right:   typedExpr{typ: nullType},
			wantErr: "is not comparison syntax",
		},
		{
			name:       "equality_nullable_operand_returns_nullable_bool",
			op:         "==",
			left:       typedExpr{typ: nullableIntType},
			right:      typedExpr{typ: intType},
			wantSchema: "bool?",
		},
		{
			name:    "equality_union_rejects",
			op:      "==",
			left:    typedExpr{typ: unionType},
			right:   typedExpr{typ: unionType},
			wantErr: "cannot compare union values",
		},
		{
			name:    "equality_mixed_rejects_as_not_comparable",
			op:      "==",
			left:    typedExpr{typ: &table.TypeDescriptor{Kind: table.TypeMixed}},
			right:   typedExpr{typ: &table.TypeDescriptor{Kind: table.TypeMixed}},
			wantErr: "cannot compare mixed with mixed",
		},
		{
			name:    "ordering_union_rejects",
			op:      "<",
			left:    typedExpr{typ: unionType},
			right:   typedExpr{typ: unionType},
			wantErr: "cannot compare union values",
		},
		{
			name:    "ordering_record_rejects",
			op:      "<",
			left:    typedExpr{typ: recordType},
			right:   typedExpr{typ: recordType},
			wantErr: "cannot compare record with record",
		},
		{
			name:    "and_rejects_non_bool",
			op:      "and",
			left:    typedExpr{typ: boolType},
			right:   typedExpr{typ: intType},
			wantErr: "requires boolean operands",
		},
		{
			name:    "unknown_operator_rejects",
			op:      "%",
			left:    typedExpr{typ: intType},
			right:   typedExpr{typ: intType},
			wantErr: `unknown operator "%"`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := checkBinarySignature(tc.op, tc.left, tc.right)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("expected error containing %q, got schema=%s err=%v", tc.wantErr, table.Render(got), err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if schema := table.Render(got); schema != tc.wantSchema {
				t.Fatalf("schema: got %s, want %s", schema, tc.wantSchema)
			}
		})
	}
}

func TestCheckUnaryAndIfSignaturePlannerEdges(t *testing.T) {
	intArg := typedExpr{typ: &table.TypeDescriptor{Kind: table.TypeInt}}
	boolArg := typedExpr{typ: &table.TypeDescriptor{Kind: table.TypeBool}}
	stringArg := typedExpr{typ: &table.TypeDescriptor{Kind: table.TypeString}}

	if _, err := checkUnarySignature("-", stringArg); err == nil || !strings.Contains(err.Error(), "requires numeric operand") {
		t.Fatalf("unary - should reject strings, got %v", err)
	}
	if _, err := checkUnarySignature("~", intArg); err == nil || !strings.Contains(err.Error(), `unknown unary operator "~"`) {
		t.Fatalf("unknown unary op should fail closed, got %v", err)
	}
	if _, err := checkIfSignature([]typedExpr{boolArg, intArg}); err == nil || !strings.Contains(err.Error(), "if() takes 3 arguments") {
		t.Fatalf("if arity should fail closed, got %v", err)
	}
	if _, err := checkIfSignature([]typedExpr{intArg, stringArg, stringArg}); err == nil || !strings.Contains(err.Error(), "condition must be boolean") {
		t.Fatalf("if condition should require bool, got %v", err)
	}
}

func TestTypeCheckLogicalReduceExpressionFailsClosedForUnsupportedBoundNodes(t *testing.T) {
	cases := []struct {
		name string
		expr logicalBoundExpr
		want string
	}{
		{
			name: "list_constructor",
			expr: &logicalBoundList{raw: &ast.ListExpr{}, elements: []logicalBoundExpr{
				&logicalBoundLiteral{raw: &ast.LiteralExpr{Kind: "int", Int: 1}},
			}},
			want: "list constructor is not supported in reduce",
		},
		{
			name: "struct_constructor",
			expr: &logicalBoundStruct{raw: &ast.StructExpr{}, fields: []logicalBoundStructField{
				{name: "x", expr: &logicalBoundLiteral{raw: &ast.LiteralExpr{Kind: "int", Int: 1}}},
			}},
			want: "struct constructor is not supported in reduce",
		},
		{
			name: "non_aggregate_call",
			expr: &logicalBoundCall{raw: &ast.FuncCallExpr{Name: "upper"}},
			want: `non-aggregate function "upper" in reduce context`,
		},
		{
			name: "unknown_call",
			expr: &logicalBoundCall{raw: &ast.FuncCallExpr{Name: "median"}},
			want: `unknown function "median"`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := typeCheckLogicalReduceExpression(tc.expr); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestTypeCheckLogicalExpressionFailsClosedForUnsupportedBoundNodes(t *testing.T) {
	duplicateStruct := &logicalBoundStruct{raw: &ast.StructExpr{}, fields: []logicalBoundStructField{
		{name: "x", expr: &logicalBoundLiteral{raw: &ast.LiteralExpr{Kind: "int", Int: 1}}},
		{name: "x", expr: &logicalBoundLiteral{raw: &ast.LiteralExpr{Kind: "int", Int: 2}}},
	}}
	if _, err := typeCheckLogicalExpression(duplicateStruct); err == nil || !strings.Contains(err.Error(), `struct() duplicate field "x"`) {
		t.Fatalf("duplicate logical struct field should fail closed, got %v", err)
	}

	badList := &logicalBoundList{raw: &ast.ListExpr{}, elements: []logicalBoundExpr{
		&logicalBoundCall{raw: &ast.FuncCallExpr{Name: "upper"}, args: []logicalBoundExpr{
			&logicalBoundLiteral{raw: &ast.LiteralExpr{Kind: "int", Int: 1}},
		}},
	}}
	if _, err := typeCheckLogicalExpression(badList); err == nil || !strings.Contains(err.Error(), "upper() requires a string") {
		t.Fatalf("list element type error should fail closed, got %v", err)
	}

	if _, err := typeCheckLogicalExpression(&unknownLogicalBoundExprForBoundaryTDD{}); err == nil || !strings.Contains(err.Error(), "unknown logical bound expression type") {
		t.Fatalf("unknown logical bound expression should fail closed, got %v", err)
	}
	if _, err := typeCheckLogicalReduceExpression(&unknownLogicalBoundExprForBoundaryTDD{}); err == nil || !strings.Contains(err.Error(), "unknown logical bound expression type") {
		t.Fatalf("unknown logical reduce expression should fail closed, got %v", err)
	}
}

func TestCheckAggregateSignaturePlannerEdges(t *testing.T) {
	intArg := typedExpr{typ: &table.TypeDescriptor{Kind: table.TypeInt}}
	stringArg := typedExpr{typ: &table.TypeDescriptor{Kind: table.TypeString}}
	listArg := typedExpr{typ: &table.TypeDescriptor{Kind: table.TypeList, Elem: &table.TypeDescriptor{Kind: table.TypeInt}}}

	cases := []struct {
		name       string
		aggregate  string
		args       []typedExpr
		wantSchema string
		wantErr    string
	}{
		{name: "count_no_args", aggregate: "count", wantSchema: "int"},
		{name: "sum_int", aggregate: "sum", args: []typedExpr{intArg}, wantSchema: "int?"},
		{name: "avg_int", aggregate: "avg", args: []typedExpr{intArg}, wantSchema: "float?"},
		{name: "min_string", aggregate: "min", args: []typedExpr{stringArg}, wantSchema: "string?"},
		{name: "max_string", aggregate: "max", args: []typedExpr{stringArg}, wantSchema: "string?"},
		{name: "first_list", aggregate: "first", args: []typedExpr{listArg}, wantSchema: "list<int>?"},
		{name: "last_string", aggregate: "last", args: []typedExpr{stringArg}, wantSchema: "string?"},
		{name: "count_rejects_args", aggregate: "count", args: []typedExpr{intArg}, wantErr: "count() takes no arguments"},
		{name: "sum_rejects_string", aggregate: "sum", args: []typedExpr{stringArg}, wantErr: "sum() requires a numeric column"},
		{name: "min_rejects_list", aggregate: "min", args: []typedExpr{listArg}, wantErr: "min() requires an orderable column"},
		{name: "avg_rejects_no_args", aggregate: "avg", wantErr: "avg() takes 1 argument"},
		{name: "first_rejects_no_args", aggregate: "first", wantErr: "first() takes 1 argument"},
		{name: "unknown_aggregate", aggregate: "median", args: []typedExpr{intArg}, wantErr: `unknown aggregate function "median"`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := checkAggregateSignature(tc.aggregate, tc.args)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("expected error containing %q, got schema=%s err=%v", tc.wantErr, table.Render(got), err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if schema := table.Render(table.FinalizeSchema(got)); schema != tc.wantSchema {
				t.Fatalf("schema: got %s, want %s", schema, tc.wantSchema)
			}
		})
	}
}
