package engine

import (
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
		"eq":        {typ: "bool", rows: 0, schema: "bool"},
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

	t.Run("right_join_incompatible_key_fallback_infers_right_only_key_schema", func(t *testing.T) {
		left := table.NewTableWithSchemas([]string{"id", "name"}, []*table.TypeDescriptor{
			{Kind: table.TypeString},
			{Kind: table.TypeString},
		})
		right := table.NewTable([]string{"id", "amount"})
		right.AddRow([]table.Value{table.IntVal(1), table.IntVal(99)})

		result := runJoinContractQuery(t, left, right, "join right right.csv on id")
		if result.NumRows != 1 {
			t.Fatalf("rows: got %d, want 1\n%s", result.NumRows, result.String())
		}
		requireIntValue(t, result.Get(0, "id"), 1)
		if got := result.Get(0, "name"); !got.IsNull() {
			t.Fatalf("name: got %v, want null", got)
		}
		requireIntValue(t, result.Get(0, "amount"), 99)
		if got := result.Col(result.ColIndex("id")).Schema().String(); got != "int" {
			t.Fatalf("id schema: got %q, want int", got)
		}
	})

	t.Run("right_join_incompatible_key_fallback_ignores_unmatched_left_values", func(t *testing.T) {
		left := table.NewTable([]string{"id", "name"})
		left.AddRow([]table.Value{table.StrVal("left-only"), table.StrVal("Alice")})
		right := table.NewTable([]string{"id", "amount"})
		right.AddRow([]table.Value{table.IntVal(1), table.IntVal(99)})

		result := runJoinContractQuery(t, left, right, "join right right.csv on id")
		if result.NumRows != 1 {
			t.Fatalf("rows: got %d, want 1\n%s", result.NumRows, result.String())
		}
		requireIntValue(t, result.Get(0, "id"), 1)
		if got := result.Get(0, "name"); !got.IsNull() {
			t.Fatalf("name: got %v, want null", got)
		}
		if got := result.Col(result.ColIndex("id")).Schema().String(); got != "int" {
			t.Fatalf("id schema: got %q, want int", got)
		}
	})

	t.Run("full_join_incompatible_key_fallback_infers_right_key_after_left_null_key", func(t *testing.T) {
		left := table.NewTableWithSchemas([]string{"id", "name"}, []*table.TypeDescriptor{
			{Kind: table.TypeString},
			{Kind: table.TypeString},
		})
		left.AddRow([]table.Value{table.Null(), table.StrVal("no-key")})
		right := table.NewTable([]string{"id", "amount"})
		right.AddRow([]table.Value{table.IntVal(1), table.IntVal(99)})

		result := runJoinContractQuery(t, left, right, "join full right.csv on id")
		if result.NumRows != 2 {
			t.Fatalf("rows: got %d, want 2\n%s", result.NumRows, result.String())
		}
		if got := result.Get(0, "id"); !got.IsNull() {
			t.Fatalf("left null-key row id: got %v, want null", got)
		}
		requireIntValue(t, result.Get(1, "id"), 1)
		requireIntValue(t, result.Get(1, "amount"), 99)
		if got := result.Col(result.ColIndex("id")).Schema().String(); got != "int?" {
			t.Fatalf("id schema: got %q, want int?", got)
		}
	})

	t.Run("full_join_incompatible_key_fallback_infers_right_key_when_left_empty", func(t *testing.T) {
		left := table.NewTableWithSchemas([]string{"id", "name"}, []*table.TypeDescriptor{
			{Kind: table.TypeString},
			{Kind: table.TypeString},
		})
		right := table.NewTable([]string{"id", "amount"})
		right.AddRow([]table.Value{table.IntVal(1), table.IntVal(99)})

		result := runJoinContractQuery(t, left, right, "join full right.csv on id")
		if result.NumRows != 1 {
			t.Fatalf("rows: got %d, want 1\n%s", result.NumRows, result.String())
		}
		requireIntValue(t, result.Get(0, "id"), 1)
		if got := result.Col(result.ColIndex("id")).Schema().String(); got != "int" {
			t.Fatalf("id schema: got %q, want int", got)
		}
	})

	t.Run("right_join_incompatible_dot_path_key_fallback_infers_right_only_key_schema", func(t *testing.T) {
		left := table.NewTableWithSchemas([]string{"profile", "name"}, []*table.TypeDescriptor{
			{
				Kind: table.TypeRecord,
				Fields: []table.FieldDescriptor{
					{Name: "id", Type: &table.TypeDescriptor{Kind: table.TypeString}},
				},
			},
			{Kind: table.TypeString},
		})
		right := table.NewTable([]string{"id", "amount"})
		right.AddRow([]table.Value{table.IntVal(7), table.IntVal(99)})

		result := runJoinContractQuery(t, left, right, "join right right.csv on profile.id == id")
		if result.NumRows != 1 {
			t.Fatalf("rows: got %d, want 1\n%s", result.NumRows, result.String())
		}
		if result.ColIndex("id") >= 0 {
			t.Fatalf("right join-key column should be dropped, got columns %v", result.Columns)
		}
		requireIntValue(t, result.Get(0, "profile_id"), 7)
		if got := result.Col(result.ColIndex("profile_id")).Schema().String(); got != "int" {
			t.Fatalf("profile_id schema: got %q, want int", got)
		}
	})

	t.Run("zero_row_right_join_incompatible_key_fallback_uses_right_key_schema", func(t *testing.T) {
		left := table.NewTableWithSchemas([]string{"id", "name"}, []*table.TypeDescriptor{
			{Kind: table.TypeString},
			{Kind: table.TypeString},
		})
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

	t.Run("zero_row_full_join_incompatible_key_fallback_uses_right_key_schema", func(t *testing.T) {
		left := table.NewTableWithSchemas([]string{"id", "name"}, []*table.TypeDescriptor{
			{Kind: table.TypeString},
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

	t.Run("zero_row_right_join_incompatible_dot_path_key_fallback_uses_right_key_schema", func(t *testing.T) {
		left := table.NewTableWithSchemas([]string{"profile", "name"}, []*table.TypeDescriptor{
			{
				Kind: table.TypeRecord,
				Fields: []table.FieldDescriptor{
					{Name: "id", Type: &table.TypeDescriptor{Kind: table.TypeString}},
				},
			},
			{Kind: table.TypeString},
		})
		right := table.NewTableWithSchemas([]string{"id", "amount"}, []*table.TypeDescriptor{
			{Kind: table.TypeInt},
			{Kind: table.TypeFloat},
		})

		result := runJoinContractQuery(t, left, right, "join right right.csv on profile.id == id | describe")
		assertDescribeSchemaRows(t, result, map[string]describeSchemaMeta{
			"profile":    {typ: "record", rows: 0, schema: "record<id:string>?"},
			"name":       {typ: "string", rows: 0, schema: "string?"},
			"profile_id": {typ: "int", rows: 0, schema: "int"},
			"amount":     {typ: "float", rows: 0, schema: "float"},
		})
	})

	t.Run("zero_row_right_join_multi_key_fallback_uses_right_schemas_per_incompatible_key", func(t *testing.T) {
		left := table.NewTableWithSchemas([]string{"id", "region", "name"}, []*table.TypeDescriptor{
			{Kind: table.TypeString},
			{Kind: table.TypeString},
			{Kind: table.TypeString},
		})
		right := table.NewTableWithSchemas([]string{"id", "region", "amount"}, []*table.TypeDescriptor{
			{Kind: table.TypeInt},
			{Kind: table.TypeString},
			{Kind: table.TypeFloat},
		})

		result := runJoinContractQuery(t, left, right, "join right right.csv on id and region | describe")
		assertDescribeSchemaRows(t, result, map[string]describeSchemaMeta{
			"id":     {typ: "int", rows: 0, schema: "int"},
			"region": {typ: "string", rows: 0, schema: "string"},
			"name":   {typ: "string", rows: 0, schema: "string?"},
			"amount": {typ: "float", rows: 0, schema: "float"},
		})
	})
}

func TestFixedSchemaContractPermissiveTransformFallbackFeedsLaterStrictOps(t *testing.T) {
	recordTransform := `transform r = if(age == 30, struct(x = 1), struct(x = "a"))`
	listTransform := `transform xs = if(age == 30, list(struct(x = 1)), list(struct(x = "a")))`
	lateRecordTransform := `transform r = if(city == "LA", struct(x = "a"), struct(x = age))`
	lateListTransform := `transform xs = if(city == "LA", list(struct(x = "a")), list(struct(x = age)))`
	nestedIfRecordTransform := `transform r = if(age == 30, struct(x = 1, y = "z"), if(city == "LA", struct(x = "a", y = "b"), struct(x = age)))`
	nestedIfListTransform := `transform xs = if(age == 30, list(struct(x = 1, y = "z")), if(city == "LA", list(struct(x = "a", y = "b")), list(struct(x = age))))`
	numericRecordTransform := `transform r = if(age == 30, struct(x = 1, y = 1), if(city == "LA", struct(x = 2.5, y = "b"), struct(x = age, y = 2)))`
	numericListTransform := `transform xs = if(age == 30, list(struct(x = 1, y = 1)), if(city == "LA", list(struct(x = 2.5, y = "b")), list(struct(x = age, y = 2))))`

	cases := []struct {
		name  string
		query string
		count int64
	}{
		{
			name:  "record_then_filter",
			query: recordTransform + ` | filter { true } | count`,
			count: 6,
		},
		{
			name:  "record_then_select_dot_path",
			query: recordTransform + ` | select r.x | count`,
			count: 6,
		},
		{
			name:  "record_then_group_dot_path",
			query: recordTransform + ` | group r.x | count`,
			count: 2,
		},
		{
			name:  "record_then_distinct",
			query: recordTransform + ` | distinct r | count`,
			count: 2,
		},
		{
			name:  "list_then_filter",
			query: listTransform + ` | filter { true } | count`,
			count: 6,
		},
		{
			name:  "list_then_select",
			query: listTransform + ` | select xs | count`,
			count: 6,
		},
		{
			name:  "list_then_group",
			query: listTransform + ` | group xs | count`,
			count: 2,
		},
		{
			name:  "list_then_distinct",
			query: listTransform + ` | distinct xs | count`,
			count: 2,
		},
		{
			name:  "late_record_then_filter_select_dot_path",
			query: lateRecordTransform + ` | filter { true } | select r.x | count`,
			count: 6,
		},
		{
			name:  "late_record_then_group_dot_path",
			query: lateRecordTransform + ` | group r.x | count`,
			count: 5,
		},
		{
			name:  "late_record_then_distinct",
			query: lateRecordTransform + ` | distinct r | count`,
			count: 5,
		},
		{
			name:  "late_list_then_select",
			query: lateListTransform + ` | select xs | count`,
			count: 6,
		},
		{
			name:  "late_list_then_group",
			query: lateListTransform + ` | group xs | count`,
			count: 5,
		},
		{
			name:  "late_list_then_distinct",
			query: lateListTransform + ` | distinct xs | count`,
			count: 5,
		},
		{
			name:  "nested_if_record_then_filter_select_dot_path",
			query: nestedIfRecordTransform + ` | filter { true } | select r.y | count`,
			count: 6,
		},
		{
			name:  "nested_if_record_then_group_dot_path",
			query: nestedIfRecordTransform + ` | group r.y | count`,
			count: 3,
		},
		{
			name:  "nested_if_record_then_distinct",
			query: nestedIfRecordTransform + ` | distinct r | count`,
			count: 5,
		},
		{
			name:  "nested_if_list_then_select",
			query: nestedIfListTransform + ` | select xs | count`,
			count: 6,
		},
		{
			name:  "nested_if_list_then_group",
			query: nestedIfListTransform + ` | group xs | count`,
			count: 5,
		},
		{
			name:  "nested_if_list_then_distinct",
			query: nestedIfListTransform + ` | distinct xs | count`,
			count: 5,
		},
		{
			name:  "numeric_record_then_float_comparison_filter",
			query: numericRecordTransform + ` | filter { r.x == 1.0 } | count`,
			count: 1,
		},
		{
			name:  "numeric_record_then_group_dot_path",
			query: numericRecordTransform + ` | group r.x | count`,
			count: 5,
		},
		{
			name:  "numeric_record_then_distinct",
			query: numericRecordTransform + ` | distinct r | count`,
			count: 5,
		},
		{
			name:  "numeric_list_then_select",
			query: numericListTransform + ` | select xs | count`,
			count: 6,
		},
		{
			name:  "numeric_list_then_group",
			query: numericListTransform + ` | group xs | count`,
			count: 5,
		},
		{
			name:  "numeric_list_then_distinct",
			query: numericListTransform + ` | distinct xs | count`,
			count: 5,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			requireFixedSchemaContractCount(t, runQuery(t, usersTable(), tc.query), tc.count)
		})
	}
}

func TestFixedSchemaContractPermissiveTransformFallbackNormalizesNestedNumericValues(t *testing.T) {
	result := runQuery(t, usersTable(), `transform r = if(age == 30, struct(x = 1, y = 1), if(city == "LA", struct(x = 2.5, y = "b"), struct(x = age, y = 2))) | select r.x, r.y`)
	if result.NumRows != 6 {
		t.Fatalf("row count: got %d, want 6", result.NumRows)
	}
	xIdx := result.ColIndex("r_x")
	yIdx := result.ColIndex("r_y")
	if xIdx < 0 || yIdx < 0 {
		t.Fatalf("expected r_x and r_y columns, got %v", result.Columns)
	}
	for row, want := range []float64{1, 2.5, 35, 28, 2.5, 40} {
		got := result.GetAt(row, xIdx)
		if got.Type != table.TypeFloat || got.Float != want {
			t.Fatalf("row %d r_x: got %v, want float %g", row, got, want)
		}
	}
	for row, want := range []string{"1", "b", "2", "2", "b", "2"} {
		got := result.GetAt(row, yIdx)
		if got.Type != table.TypeString || got.Str != want {
			t.Fatalf("row %d r_y: got %v, want string %q", row, got, want)
		}
	}

	listResult := runQuery(t, usersTable(), `transform xs = if(age == 30, list(struct(x = 1, y = 1)), if(city == "LA", list(struct(x = 2.5, y = "b")), list(struct(x = age, y = 2)))) | select xs`)
	xsIdx := listResult.ColIndex("xs")
	if xsIdx < 0 {
		t.Fatalf("xs column not found in %v", listResult.Columns)
	}
	for row, want := range []float64{1, 2.5, 35, 28, 2.5, 40} {
		xs := listResult.GetAt(row, xsIdx)
		if xs.Type != table.TypeList || len(xs.List) != 1 || xs.List[0].Type != table.TypeRecord {
			t.Fatalf("row %d xs: got %v, want one record", row, xs)
		}
		fields := recordValuesForTest(t, xs.List[0])
		got := fields["x"]
		if got.Type != table.TypeFloat || got.Float != want {
			t.Fatalf("row %d xs[0].x: got %v, want float %g", row, got, want)
		}
	}
}

func TestFixedSchemaContractPermissiveTransformFallbackNormalizesLateDotPathValues(t *testing.T) {
	result := runQuery(t, usersTable(), `transform r = if(city == "LA", struct(x = "a"), struct(x = age)) | select r.x`)
	if result.NumRows != 6 {
		t.Fatalf("row count: got %d, want 6", result.NumRows)
	}
	xIdx := result.ColIndex("r_x")
	if xIdx < 0 {
		t.Fatalf("r_x column not found in %v", result.Columns)
	}
	for row, want := range []string{"30", "a", "35", "28", "a", "40"} {
		got := result.GetAt(row, xIdx)
		if got.Type != table.TypeString || got.Str != want {
			t.Fatalf("row %d r_x: got %v, want string %q", row, got, want)
		}
	}
}

func TestFixedSchemaContractPermissiveTransformFallbackHandlesUnplannedNestedIf(t *testing.T) {
	result := runQuery(t, usersTable(), `transform r = if(age == 30, struct(x = 1, y = "z"), if(city == "LA", struct(x = "a", y = "b"), struct(x = age))) | select r.y`)
	if result.NumRows != 6 {
		t.Fatalf("row count: got %d, want 6", result.NumRows)
	}
	yIdx := result.ColIndex("r_y")
	if yIdx < 0 {
		t.Fatalf("r_y column not found in %v", result.Columns)
	}
	for row, want := range []table.Value{table.StrVal("z"), table.StrVal("b"), table.Null(), table.Null(), table.StrVal("b"), table.Null()} {
		got := result.GetAt(row, yIdx)
		if !table.Equal(got, want) {
			t.Fatalf("row %d r_y: got %v, want %v", row, got, want)
		}
	}
}

func TestFixedSchemaContractPermissiveTransformFallbackNormalizesSelectedDotPathValues(t *testing.T) {
	result := runQuery(t, usersTable(), `transform r = if(age == 30, struct(x = 1), struct(x = "a")) | select name, r.x`)
	if result.NumRows != 6 {
		t.Fatalf("row count: got %d, want 6", result.NumRows)
	}
	xIdx := result.ColIndex("r_x")
	if xIdx < 0 {
		t.Fatalf("r_x column not found in %v", result.Columns)
	}
	for row, want := range []string{"1", "a"} {
		got := result.GetAt(row, xIdx)
		if got.Type != table.TypeString || got.Str != want {
			t.Fatalf("row %d r_x: got %v, want string %q", row, got, want)
		}
	}
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
