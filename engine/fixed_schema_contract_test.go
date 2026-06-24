package engine

import (
	"strings"
	"testing"

	"github.com/razeghi71/dq/ast"
	"github.com/razeghi71/dq/parser"
	"github.com/razeghi71/dq/table"
)

func TestFixedSchemaContractFilterAndSelectZeroRowsAcrossFlatFormats(t *testing.T) {
	for _, input := range flatUserFormatFiles() {
		t.Run(input.name+"_filter", func(t *testing.T) {
			result := loadAndQuery(t, input.file, "filter { false } | describe")
			assertDescribeSchemaRows(t, result, map[string]describeSchemaMeta{
				"name": {typ: "string", rows: 0, schema: "string"},
				"age":  {typ: "int", rows: 0, schema: "int"},
				"city": {typ: "string", rows: 0, schema: "string"},
			})
		})

		t.Run(input.name+"_filter_select", func(t *testing.T) {
			result := loadAndQuery(t, input.file, "filter { false } | select name, age | describe")
			assertDescribeSchemaRows(t, result, map[string]describeSchemaMeta{
				"name": {typ: "string", rows: 0, schema: "string"},
				"age":  {typ: "int", rows: 0, schema: "int"},
			})
		})
	}
}

func TestFixedSchemaContractZeroRowSchemaPreservingOps(t *testing.T) {
	cases := []struct {
		name  string
		query string
		want  map[string]describeSchemaMeta
	}{
		{
			name:  "sort_after_empty_filter",
			query: "filter { false } | sort age | describe",
			want: map[string]describeSchemaMeta{
				"name": {typ: "string", rows: 0, schema: "string"},
				"age":  {typ: "int", rows: 0, schema: "int"},
				"city": {typ: "string", rows: 0, schema: "string"},
			},
		},
		{
			name:  "distinct_after_empty_filter",
			query: "filter { false } | distinct | describe",
			want: map[string]describeSchemaMeta{
				"name": {typ: "string", rows: 0, schema: "string"},
				"age":  {typ: "int", rows: 0, schema: "int"},
				"city": {typ: "string", rows: 0, schema: "string"},
			},
		},
		{
			name:  "rename_remove_after_empty_filter",
			query: "filter { false } | rename name=person | remove city | describe",
			want: map[string]describeSchemaMeta{
				"person": {typ: "string", rows: 0, schema: "string"},
				"age":    {typ: "int", rows: 0, schema: "int"},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := runQuery(t, usersTable(), tc.query)
			assertDescribeSchemaRows(t, result, tc.want)
		})
	}
}

func TestFixedSchemaContractNestedZeroRowProjectionAcrossFormats(t *testing.T) {
	inputs := []struct {
		name string
		file string
	}{
		{"json", testdataDir + "/nested.json"},
		{"jsonl", testdataDir + "/nested.jsonl"},
		{"avro", testdataDir + "/nested.avro"},
		{"parquet", testdataDir + "/nested.parquet"},
	}

	for _, input := range inputs {
		t.Run(input.name, func(t *testing.T) {
			result := loadAndQuery(t, input.file, "filter { false } | select address.city, orders | describe")
			assertDescribeSchemaRows(t, result, map[string]describeSchemaMeta{
				"address_city": {typ: "string", rows: 0, schema: "string"},
				"orders":       {typ: "list", rows: 0, schema: "list<record<amount:float, order_id:int, status:string>>"},
			})
		})
	}
}

func TestFixedSchemaContractGroupAndTransformZeroRowsHavePlannedSchemas(t *testing.T) {
	t.Run("group_empty_input", func(t *testing.T) {
		result := runQuery(t, usersTable(), "filter { false } | group city | describe")
		assertDescribeSchemaRows(t, result, map[string]describeSchemaMeta{
			"city":    {typ: "string", rows: 0, schema: "string"},
			"grouped": {typ: "list", rows: 0, schema: "list<record<age:int, city:string, name:string>>"},
		})
	})

	t.Run("transform_empty_input", func(t *testing.T) {
		result := runQuery(t, usersTable(), `filter { false } | transform age2 = age + 1, label = upper(name), profile = struct(name = name, age = age), tags = list(city, null) | select age2, label, profile, tags | describe`)
		assertDescribeSchemaRows(t, result, map[string]describeSchemaMeta{
			"age2":    {typ: "int", rows: 0, schema: "int"},
			"label":   {typ: "string", rows: 0, schema: "string"},
			"profile": {typ: "record", rows: 0, schema: "record<age:int, name:string>"},
			"tags":    {typ: "list", rows: 0, schema: "list<string?>"},
		})
	})
}

func TestFixedSchemaContractZeroRowTransformNullOnlyExpressionsFinalizeSchemas(t *testing.T) {
	t.Run("new_columns", func(t *testing.T) {
		result := runQuery(t, usersTable(), `filter { false } | transform x = null, c = coalesce(null, null), branch = if(age > 0, null, null), neg = -null, rec = struct(x = null, y = coalesce(null, null)), xs = list(null), nested = list(struct(x = null)) | describe`)
		assertDescribeSchemaRows(t, result, map[string]describeSchemaMeta{
			"name":   {typ: "string", rows: 0, schema: "string"},
			"age":    {typ: "int", rows: 0, schema: "int"},
			"city":   {typ: "string", rows: 0, schema: "string"},
			"x":      {typ: "string", rows: 0, schema: "string?"},
			"c":      {typ: "string", rows: 0, schema: "string?"},
			"branch": {typ: "string", rows: 0, schema: "string?"},
			"neg":    {typ: "string", rows: 0, schema: "string?"},
			"rec":    {typ: "record", rows: 0, schema: "record<x:string?, y:string?>"},
			"xs":     {typ: "list", rows: 0, schema: "list<string?>"},
			"nested": {typ: "list", rows: 0, schema: "list<record<x:string?>>"},
		})
	})

	t.Run("overwrite_existing_columns", func(t *testing.T) {
		result := runQuery(t, usersTable(), `filter { false } | transform name = null, age = coalesce(null, null) | select name, age | describe`)
		assertDescribeSchemaRows(t, result, map[string]describeSchemaMeta{
			"name": {typ: "string", rows: 0, schema: "string?"},
			"age":  {typ: "string", rows: 0, schema: "string?"},
		})
	})

	t.Run("downstream_rebuild_keeps_finalized_null_only_schema", func(t *testing.T) {
		result := runQuery(t, usersTable(), `filter { false } | transform x = null | distinct | describe`)
		assertDescribeSchemaRows(t, result, map[string]describeSchemaMeta{
			"name": {typ: "string", rows: 0, schema: "string"},
			"age":  {typ: "int", rows: 0, schema: "int"},
			"city": {typ: "string", rows: 0, schema: "string"},
			"x":    {typ: "string", rows: 0, schema: "string?"},
		})
	})
}

func TestFixedSchemaContractZeroRowReduceAggregatesAreNullableExceptCount(t *testing.T) {
	path := writeJSONInferenceIntegrationFile(t, t.TempDir(), "orders.jsonl", `{"id":1,"orders":[]}
{"id":2,"orders":[{"amount":3,"note":"first"}]}
`)

	result := loadAndQuery(t, path, `filter { false } | reduce orders n = count(), total = sum(amount), avg_amt = avg(amount), min_amt = min(amount), max_amt = max(amount), first_amt = first(amount), last_amt = last(amount), first_note = first(note), last_note = last(note), plus_one = sum(amount) + 1 | describe`)
	assertDescribeSchemaRows(t, result, map[string]describeSchemaMeta{
		"id":         {typ: "int", rows: 0, schema: "int"},
		"orders":     {typ: "list", rows: 0, schema: "list<record<amount:int, note:string>>"},
		"n":          {typ: "int", rows: 0, schema: "int"},
		"total":      {typ: "int", rows: 0, schema: "int?"},
		"avg_amt":    {typ: "float", rows: 0, schema: "float?"},
		"min_amt":    {typ: "int", rows: 0, schema: "int?"},
		"max_amt":    {typ: "int", rows: 0, schema: "int?"},
		"first_amt":  {typ: "int", rows: 0, schema: "int?"},
		"last_amt":   {typ: "int", rows: 0, schema: "int?"},
		"first_note": {typ: "string", rows: 0, schema: "string?"},
		"last_note":  {typ: "string", rows: 0, schema: "string?"},
		"plus_one":   {typ: "int", rows: 0, schema: "int?"},
	})
}

func TestFixedSchemaContractZeroRowReduceComparisonAndLogicalSchemas(t *testing.T) {
	result := runQuery(t, usersTable(), `filter { false } | group city | reduce eq = count() == 0, neq = count() != 0, lt = count() < 1, ge = count() >= 0, both = count() == 0 and null, either = count() == 0 or null, not_eq = not (count() == 0) | select eq, neq, lt, ge, both, either, not_eq | describe`)
	assertDescribeSchemaRows(t, result, map[string]describeSchemaMeta{
		"eq":     {typ: "bool", rows: 0, schema: "bool"},
		"neq":    {typ: "bool", rows: 0, schema: "bool"},
		"lt":     {typ: "bool", rows: 0, schema: "bool"},
		"ge":     {typ: "bool", rows: 0, schema: "bool"},
		"both":   {typ: "bool", rows: 0, schema: "bool?"},
		"either": {typ: "bool", rows: 0, schema: "bool?"},
		"not_eq": {typ: "bool", rows: 0, schema: "bool"},
	})
}

func TestFixedSchemaContractZeroRowReduceNullLiteralFinalizesSchema(t *testing.T) {
	result := runQuery(t, usersTable(), `filter { false } | group city | reduce x = null | describe`)
	assertDescribeSchemaRows(t, result, map[string]describeSchemaMeta{
		"city":    {typ: "string", rows: 0, schema: "string"},
		"grouped": {typ: "list", rows: 0, schema: "list<record<age:int, city:string, name:string>>"},
		"x":       {typ: "string", rows: 0, schema: "string?"},
	})
}

func TestFixedSchemaContractZeroRowDivisionPlanningAccountsForDivideByZero(t *testing.T) {
	t.Run("transform", func(t *testing.T) {
		result := runQuery(t, usersTable(), `filter { false } | transform literal_zero = age / 0, float_zero = age / 0.0, maybe_zero_col = age / age, maybe_zero_expr = age / (age - age), nonzero_literal = age / 2, rec = struct(div = age / 0), xs = list(age / 0) | select literal_zero, float_zero, maybe_zero_col, maybe_zero_expr, nonzero_literal, rec, xs | describe`)
		assertDescribeSchemaRows(t, result, map[string]describeSchemaMeta{
			"literal_zero":    {typ: "float", rows: 0, schema: "float?"},
			"float_zero":      {typ: "float", rows: 0, schema: "float?"},
			"maybe_zero_col":  {typ: "float", rows: 0, schema: "float?"},
			"maybe_zero_expr": {typ: "float", rows: 0, schema: "float?"},
			"nonzero_literal": {typ: "float", rows: 0, schema: "float"},
			"rec":             {typ: "record", rows: 0, schema: "record<div:float?>"},
			"xs":              {typ: "list", rows: 0, schema: "list<float?>"},
		})
	})

	t.Run("nullable_operand", func(t *testing.T) {
		result := runQuery(t, nullablePlanningTable(), `filter { false } | transform div = n / 2 | select div | describe`)
		assertDescribeSchemaRows(t, result, map[string]describeSchemaMeta{
			"div": {typ: "float", rows: 0, schema: "float?"},
		})
	})

	t.Run("reduce", func(t *testing.T) {
		result := runQuery(t, usersTable(), `filter { false } | group city | reduce bad = count() / 0, ok = count() / 2, total_bad = sum(age) / 0, total_ok = sum(age) / 2 | select bad, ok, total_bad, total_ok | describe`)
		assertDescribeSchemaRows(t, result, map[string]describeSchemaMeta{
			"bad":       {typ: "float", rows: 0, schema: "float?"},
			"ok":        {typ: "float", rows: 0, schema: "float"},
			"total_bad": {typ: "float", rows: 0, schema: "float?"},
			"total_ok":  {typ: "float", rows: 0, schema: "float?"},
		})
	})
}

func TestFixedSchemaContractZeroRowTransformPropagatesNullability(t *testing.T) {
	result := runQuery(t, nullablePlanningTable(), `filter { false } | transform sl = str_len(s), ll = list_len(xs), y = year(d), mo = month(d), da = day(d), sub = substr(s, 0, n), sub_start = substr("abcdef", n, 2), sub_len = substr("abcdef", 0, n), contains = str_contains(s, "a"), starts = starts_with(s, "a"), ends = ends_with(s, "a"), match = matches(s, "^a"), has = list_contains(xs, 1), not_flag = not flag, both = flag and true, either = flag or false, lt = n < 10, eq = n == 3, is_null = n is null | select sl, ll, y, mo, da, sub, sub_start, sub_len, contains, starts, ends, match, has, not_flag, both, either, lt, eq, is_null | describe`)

	assertDescribeSchemaRows(t, result, map[string]describeSchemaMeta{
		"sl":        {typ: "int", rows: 0, schema: "int?"},
		"ll":        {typ: "int", rows: 0, schema: "int?"},
		"y":         {typ: "int", rows: 0, schema: "int?"},
		"mo":        {typ: "int", rows: 0, schema: "int?"},
		"da":        {typ: "int", rows: 0, schema: "int?"},
		"sub":       {typ: "string", rows: 0, schema: "string?"},
		"sub_start": {typ: "string", rows: 0, schema: "string?"},
		"sub_len":   {typ: "string", rows: 0, schema: "string?"},
		"contains":  {typ: "bool", rows: 0, schema: "bool?"},
		"starts":    {typ: "bool", rows: 0, schema: "bool?"},
		"ends":      {typ: "bool", rows: 0, schema: "bool?"},
		"match":     {typ: "bool", rows: 0, schema: "bool?"},
		"has":       {typ: "bool", rows: 0, schema: "bool?"},
		"not_flag":  {typ: "bool", rows: 0, schema: "bool?"},
		"both":      {typ: "bool", rows: 0, schema: "bool?"},
		"either":    {typ: "bool", rows: 0, schema: "bool?"},
		"lt":        {typ: "bool", rows: 0, schema: "bool?"},
		"eq":        {typ: "bool", rows: 0, schema: "bool?"},
		"is_null":   {typ: "bool", rows: 0, schema: "bool"},
	})
}

func TestFixedSchemaContractZeroRowTransformKeepsNonNullExpressionSchemas(t *testing.T) {
	result := runQuery(t, usersTable(), `filter { false } | transform sl = str_len(name), y = year("2024-01-01"), contains = str_contains(name, "a"), not_flag = not true, lt = age < 50, eq = age == 30, is_null = age is null | select sl, y, contains, not_flag, lt, eq, is_null | describe`)

	assertDescribeSchemaRows(t, result, map[string]describeSchemaMeta{
		"sl":       {typ: "int", rows: 0, schema: "int"},
		"y":        {typ: "int", rows: 0, schema: "int"},
		"contains": {typ: "bool", rows: 0, schema: "bool"},
		"not_flag": {typ: "bool", rows: 0, schema: "bool"},
		"lt":       {typ: "bool", rows: 0, schema: "bool"},
		"eq":       {typ: "bool", rows: 0, schema: "bool"},
		"is_null":  {typ: "bool", rows: 0, schema: "bool"},
	})
}

func TestFixedSchemaContractNullabilityPlanningMatchesRuntimeNulls(t *testing.T) {
	result := runQuery(t, nullablePlanningTable(), `transform y = year(d), ok = str_contains(d, "2024"), neg = not flag, lt = n < 10 | select y, ok, neg, lt`)
	if result.NumRows != 2 {
		t.Fatalf("row count: got %d, want 2", result.NumRows)
	}
	for _, col := range []string{"y", "ok", "neg", "lt"} {
		if got := result.Get(1, col); got.Type != table.TypeNull {
			t.Fatalf("row 1 %s: want runtime null, got %v", col, got)
		}
	}
}

func TestFixedSchemaContractJoinSchemas(t *testing.T) {
	t.Run("no_match_inner_join_preserves_planned_output_schema", func(t *testing.T) {
		left := table.NewTable([]string{"id", "name"})
		left.AddRow([]table.Value{table.IntVal(1), table.StrVal("Alice")})
		right := table.NewTable([]string{"id", "note"})
		right.AddRow([]table.Value{table.IntVal(2), table.StrVal("right-only")})

		result := runJoinContractQuery(t, left, right, "join right.csv on id | describe")
		assertDescribeSchemaRows(t, result, map[string]describeSchemaMeta{
			"id":   {typ: "int", rows: 0, schema: "int"},
			"name": {typ: "string", rows: 0, schema: "string"},
			"note": {typ: "string", rows: 0, schema: "string"},
		})
	})

	t.Run("zero_row_left_join_marks_right_side_nullable", func(t *testing.T) {
		left := table.NewTableWithSchemas([]string{"id", "name"}, []*table.TypeDescriptor{
			{Kind: table.TypeInt},
			{Kind: table.TypeString},
		})
		right := table.NewTable([]string{"id", "amount"})
		right.AddRow([]table.Value{table.IntVal(1), table.IntVal(10)})

		result := runJoinContractQuery(t, left, right, "join left right.csv on id | describe")
		assertDescribeSchemaRows(t, result, map[string]describeSchemaMeta{
			"id":     {typ: "int", rows: 0, schema: "int"},
			"name":   {typ: "string", rows: 0, schema: "string"},
			"amount": {typ: "int", rows: 0, schema: "int?"},
		})
	})

	t.Run("zero_row_right_join_marks_left_side_nullable", func(t *testing.T) {
		left := table.NewTable([]string{"id", "name"})
		left.AddRow([]table.Value{table.IntVal(1), table.StrVal("Alice")})
		right := table.NewTableWithSchemas([]string{"id", "amount"}, []*table.TypeDescriptor{
			{Kind: table.TypeInt},
			{Kind: table.TypeFloat},
		})

		result := runJoinContractQuery(t, left, right, "join right right.csv on id | describe")
		assertDescribeSchemaRows(t, result, map[string]describeSchemaMeta{
			"id":     {typ: "int", rows: 0, schema: "int"},
			"name":   {typ: "string", rows: 0, schema: "string?"},
			"amount": {typ: "float", rows: 0, schema: "float"},
		})
	})

	t.Run("zero_row_full_join_marks_both_padded_sides_nullable", func(t *testing.T) {
		left := table.NewTableWithSchemas([]string{"id", "name"}, []*table.TypeDescriptor{
			{Kind: table.TypeInt},
			{Kind: table.TypeString},
		})
		right := table.NewTableWithSchemas([]string{"id", "amount"}, []*table.TypeDescriptor{
			{Kind: table.TypeInt},
			{Kind: table.TypeFloat},
		})

		result := runJoinContractQuery(t, left, right, "join full right.csv on id | describe")
		assertDescribeSchemaRows(t, result, map[string]describeSchemaMeta{
			"id":     {typ: "int", rows: 0, schema: "int"},
			"name":   {typ: "string", rows: 0, schema: "string?"},
			"amount": {typ: "float", rows: 0, schema: "float?"},
		})
	})

	t.Run("right_join_null_padding_marks_left_side_nullable", func(t *testing.T) {
		result := runJoinQuery(t, usersTable(), "right orders.csv on name == user_name | describe")
		assertDescribeSchemaRows(t, result, map[string]describeSchemaMeta{
			"name":     {typ: "string", rows: 5, schema: "string"},
			"age":      {typ: "int", rows: 5, schema: "int?"},
			"city":     {typ: "string", rows: 5, schema: "string?"},
			"order_id": {typ: "int", rows: 5, schema: "int"},
			"product":  {typ: "string", rows: 5, schema: "string"},
			"amount":   {typ: "int", rows: 5, schema: "int"},
		})
	})

	t.Run("full_join_null_padding_preserves_kinds", func(t *testing.T) {
		left := table.NewTable([]string{"id", "name"})
		left.AddRow([]table.Value{table.IntVal(1), table.StrVal("left-only")})
		right := table.NewTable([]string{"id", "amount"})
		right.AddRow([]table.Value{table.IntVal(2), table.IntVal(99)})

		result := runJoinContractQuery(t, left, right, "join full right.csv on id | describe")
		assertDescribeSchemaRows(t, result, map[string]describeSchemaMeta{
			"id":     {typ: "int", rows: 2, schema: "int"},
			"name":   {typ: "string", rows: 2, schema: "string?"},
			"amount": {typ: "int", rows: 2, schema: "int?"},
		})
	})

	t.Run("right_column_collision_schema_is_preserved", func(t *testing.T) {
		left := table.NewTable([]string{"id", "name"})
		left.AddRow([]table.Value{table.IntVal(1), table.StrVal("Alice")})
		right := table.NewTable([]string{"id", "customer_id", "note"})
		right.AddRow([]table.Value{table.IntVal(99), table.IntVal(1), table.StrVal("hello")})

		result := runJoinContractQuery(t, left, right, "join right.csv on id == customer_id | describe")
		assertDescribeSchemaRows(t, result, map[string]describeSchemaMeta{
			"id":       {typ: "int", rows: 1, schema: "int"},
			"name":     {typ: "string", rows: 1, schema: "string"},
			"right_id": {typ: "int", rows: 1, schema: "int"},
			"note":     {typ: "string", rows: 1, schema: "string"},
		})
	})

	t.Run("incompatible_join_key_schemas_fail_during_planning", func(t *testing.T) {
		stringIDLeft := func() *table.Table {
			return table.NewTableWithSchemas([]string{"id", "name"}, []*table.TypeDescriptor{
				{Kind: table.TypeString},
				{Kind: table.TypeString},
			})
		}
		intIDRight := func() *table.Table {
			return table.NewTableWithSchemas([]string{"id", "amount"}, []*table.TypeDescriptor{
				{Kind: table.TypeInt},
				{Kind: table.TypeFloat},
			})
		}

		cases := []struct {
			name  string
			left  *table.Table
			right *table.Table
			query string
			want  []string
		}{
			{
				name:  "right_join_flat_key",
				left:  stringIDLeft(),
				right: intIDRight(),
				query: "join right right.csv on id",
				want:  []string{"join", "key", "type", "id", "string", "int"},
			},
			{
				name:  "full_join_flat_key",
				left:  stringIDLeft(),
				right: intIDRight(),
				query: "join full right.csv on id",
				want:  []string{"join", "key", "type", "id", "string", "int"},
			},
			{
				name: "right_join_dot_path_key",
				left: table.NewTableWithSchemas([]string{"profile", "name"}, []*table.TypeDescriptor{
					{
						Kind: table.TypeRecord,
						Fields: []table.FieldDescriptor{
							{Name: "id", Type: &table.TypeDescriptor{Kind: table.TypeString}},
						},
					},
					{Kind: table.TypeString},
				}),
				right: intIDRight(),
				query: "join right right.csv on profile.id == id",
				want:  []string{"join", "key", "type", "profile.id", "id", "string", "int"},
			},
			{
				name: "multi_key_reports_first_mismatch",
				left: table.NewTableWithSchemas([]string{"id", "region", "name"}, []*table.TypeDescriptor{
					{Kind: table.TypeString},
					{Kind: table.TypeString},
					{Kind: table.TypeString},
				}),
				right: table.NewTableWithSchemas([]string{"id", "region", "amount"}, []*table.TypeDescriptor{
					{Kind: table.TypeInt},
					{Kind: table.TypeString},
					{Kind: table.TypeFloat},
				}),
				query: "join right right.csv on id and region",
				want:  []string{"join", "key", "type", "id", "string", "int"},
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				err := runJoinContractQueryExpectErr(t, tc.left, tc.right, tc.query)
				for _, part := range tc.want {
					if !strings.Contains(err.Error(), part) {
						t.Fatalf("join key error missing %q: %v", part, err)
					}
				}
			})
		}
	})
}

func TestFixedSchemaContractIncompatibleTransformBranchesFailDuringPlanning(t *testing.T) {
	cases := []struct {
		name  string
		query string
	}{
		{"record_field_type_conflict", `transform r = if(age == 30, struct(x = 1), struct(x = "a"))`},
		{"list_record_field_type_conflict", `transform xs = if(age == 30, list(struct(x = 1)), list(struct(x = "a")))`},
		{"late_record_field_type_conflict", `transform r = if(city == "LA", struct(x = "a"), struct(x = age))`},
		{"late_list_field_type_conflict", `transform xs = if(city == "LA", list(struct(x = "a")), list(struct(x = age)))`},
		{"nested_record_field_type_conflict", `transform r = if(age == 30, struct(x = 1, y = "z"), if(city == "LA", struct(x = "a", y = "b"), struct(x = age)))`},
		{"list_scalar_conflict", `transform xs = if(age == 30, list(1, 2), "off")`},
		{"numeric_string_field_conflict", `transform r = if(age == 30, struct(x = 1, y = 1), if(city == "LA", struct(x = 2.5, y = "b"), struct(x = age, y = 2)))`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expectQueryErrContains(t, usersTable(), tc.query, "if() branches do not have one common type")
		})
	}
}

func TestFixedSchemaContractCompatibleTransformBranchesProducePlannedSchemas(t *testing.T) {
	result := runQuery(t, usersTable(), `transform r = if(age == 30, struct(x = 1, y = 1), if(city == "LA", struct(x = 2.5, y = 2), struct(x = age, y = 2))) | select r.x, r.y | describe`)
	assertDescribeSchemaRows(t, result, map[string]describeSchemaMeta{
		"r_x": {typ: "float", rows: 6, schema: "float"},
		"r_y": {typ: "int", rows: 6, schema: "int"},
	})

	listResult := runQuery(t, usersTable(), `transform xs = if(age == 30, list(struct(x = 1, y = 1)), if(city == "LA", list(struct(x = 2.5, y = 2)), list(struct(x = age, y = 2)))) | select xs | describe`)
	assertDescribeSchemaRows(t, listResult, map[string]describeSchemaMeta{
		"xs": {typ: "list", rows: 6, schema: "list<record<x:float, y:int>>"},
	})
}

func TestFixedSchemaContractRecordBranchMissingFieldsRemainNullable(t *testing.T) {
	result := runQuery(t, usersTable(), `transform r = if(age == 30, struct(x = 1, y = "z"), struct(x = age)) | select r.y`)
	if result.NumRows != 6 {
		t.Fatalf("row count: got %d, want 6", result.NumRows)
	}
	yIdx := result.ColIndex("r_y")
	if yIdx < 0 {
		t.Fatalf("r_y column not found in %v", result.Columns)
	}
	for row, want := range []table.Value{table.StrVal("z"), table.Null(), table.Null(), table.Null(), table.Null(), table.Null()} {
		got := result.GetAt(row, yIdx)
		if !table.Equal(got, want) {
			t.Fatalf("row %d r_y: got %v, want %v", row, got, want)
		}
	}

	describe := runQuery(t, usersTable(), `transform r = if(age == 30, struct(x = 1, y = "z"), struct(x = age)) | select r.y | describe`)
	assertDescribeSchemaRows(t, describe, map[string]describeSchemaMeta{
		"r_y": {typ: "string", rows: 6, schema: "string?"},
	})
}

func requireFixedSchemaContractCount(t *testing.T, result *table.Table, want int64) {
	t.Helper()
	if result.NumRows != 1 {
		t.Fatalf("count result rows: got %d, want 1", result.NumRows)
	}
	if got := result.Get(0, "count"); got.Type != table.TypeInt || got.Int != want {
		t.Fatalf("count: got %v, want int %d", got, want)
	}
}

func nullablePlanningTable() *table.Table {
	tbl := table.NewTable([]string{"s", "d", "xs", "n", "flag"})
	tbl.AddRow([]table.Value{
		table.StrVal("alpha"),
		table.StrVal("2024-01-01"),
		table.ListVal([]table.Value{table.IntVal(1), table.IntVal(2)}),
		table.IntVal(3),
		table.BoolVal(true),
	})
	tbl.AddRow([]table.Value{
		table.Null(),
		table.Null(),
		table.Null(),
		table.Null(),
		table.Null(),
	})
	return tbl
}

func requireIntValue(t *testing.T, got table.Value, want int64) {
	t.Helper()
	if got.Type != table.TypeInt || got.Int != want {
		t.Fatalf("value: got %v, want int %d", got, want)
	}
}

func runJoinContractQuery(t *testing.T, left, right *table.Table, query string) *table.Table {
	t.Helper()
	q, err := parser.Parse("left.csv | " + query)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	result, err := Execute(q, left, func(filename string, _ ast.LoadOptions) (*table.Table, error) {
		if filename == "right.csv" {
			return right, nil
		}
		t.Fatalf("unexpected join filename %q", filename)
		return nil, nil
	})
	if err != nil {
		t.Fatalf("exec error: %v", err)
	}
	return result
}

func runJoinContractQueryExpectErr(t *testing.T, left, right *table.Table, query string) error {
	t.Helper()
	q, err := parser.Parse("left.csv | " + query)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	_, err = Execute(q, left, func(filename string, _ ast.LoadOptions) (*table.Table, error) {
		if filename == "right.csv" {
			return right, nil
		}
		t.Fatalf("unexpected join filename %q", filename)
		return nil, nil
	})
	if err == nil {
		t.Fatal("expected query error")
	}
	return err
}
