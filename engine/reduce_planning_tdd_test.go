package engine

import (
	"strings"
	"testing"

	"github.com/razeghi71/dq/table"
)

func TestReducePlanningRejectsInvalidExpressionsBeforeRows(t *testing.T) {
	cases := []struct {
		name  string
		input *table.Table
		query string
		wants []string
	}{
		{
			name:  "non_aggregate_function",
			input: usersTable(),
			query: `filter { false } | group city | reduce bad = upper(name)`,
			wants: []string{"upper", "reduce"},
		},
		{
			name:  "non_aggregate_function_nested_in_binary",
			input: usersTable(),
			query: `filter { false } | group city | reduce bad = sum(age) + upper(name)`,
			wants: []string{"upper", "reduce"},
		},
		{
			name:  "bare_column_expression",
			input: usersTable(),
			query: `filter { false } | group city | reduce bad = name`,
			wants: []string{"column", "reduce"},
		},
		{
			name:  "sibling_assignment_not_visible",
			input: usersTable(),
			query: `filter { false } | group city | reduce total = sum(age), doubled = total * 2`,
			wants: []string{"total", "reduce"},
		},
		{
			name:  "duplicate_assignment_target",
			input: usersTable(),
			query: `filter { false } | group city | reduce x = count(), x = sum(age)`,
			wants: []string{"reduce target", "x", "more than once"},
		},
		{
			name:  "sum_string_column",
			input: usersTable(),
			query: `filter { false } | group city | reduce bad = sum(name)`,
			wants: []string{"sum", "numeric"},
		},
		{
			name:  "avg_string_column",
			input: usersTable(),
			query: `filter { false } | group city | reduce bad = avg(city)`,
			wants: []string{"avg", "numeric"},
		},
		{
			name:  "sum_list_column",
			input: reducePlanningNestedValueTable(),
			query: `filter { false } | group name | reduce bad = sum(xs)`,
			wants: []string{"sum", "numeric"},
		},
		{
			name:  "max_list_column",
			input: reducePlanningNestedValueTable(),
			query: `filter { false } | group name | reduce bad = max(xs)`,
			wants: []string{"max", "orderable"},
		},
		{
			name:  "max_record_column",
			input: reducePlanningNestedValueTable(),
			query: `filter { false } | group name | reduce bad = max(profile)`,
			wants: []string{"max", "orderable"},
		},
		{
			name:  "missing_aggregate_column",
			input: usersTable(),
			query: `filter { false } | group city | reduce bad = first(missing)`,
			wants: []string{"missing", "not found"},
		},
		{
			name:  "missing_nested_aggregate_field",
			input: reducePlanningNestedValueTable(),
			query: `filter { false } | group name | reduce bad = first(profile.missing)`,
			wants: []string{"missing", "not found"},
		},
		{
			name:  "explicit_nested_reduce_rejects_scalar_source_even_count_only",
			input: usersTable(),
			query: `filter { false } | reduce age n = count()`,
			wants: []string{"reduce", "age", "list", "record"},
		},
		{
			name:  "explicit_nested_reduce_rejects_scalar_list_source_even_count_only",
			input: reducePlanningScalarListTable(t),
			query: `filter { false } | reduce xs n = count()`,
			wants: []string{"reduce", "xs", "record"},
		},
		{
			name:  "explicit_nested_reduce_requires_record_elements",
			input: reducePlanningScalarListTable(t),
			query: `filter { false } | reduce xs bad = first(value)`,
			wants: []string{"reduce", "xs", "record"},
		},
		{
			name:  "aggregate_arg_must_be_column_function",
			input: usersTable(),
			query: `filter { false } | group city | reduce bad = first(upper(name))`,
			wants: []string{"first", "column reference"},
		},
		{
			name:  "aggregate_arg_must_be_column_expression",
			input: usersTable(),
			query: `filter { false } | group city | reduce bad = sum(age + 1)`,
			wants: []string{"sum", "column reference"},
		},
		{
			name:  "aggregate_count_takes_no_args",
			input: usersTable(),
			query: `filter { false } | group city | reduce bad = count(age)`,
			wants: []string{"count", "no arguments"},
		},
		{
			name:  "not_requires_bool",
			input: usersTable(),
			query: `filter { false } | group city | reduce bad = not sum(age)`,
			wants: []string{"not", "boolean"},
		},
		{
			name:  "and_requires_bool_left",
			input: usersTable(),
			query: `filter { false } | group city | reduce bad = sum(age) and true`,
			wants: []string{"and", "boolean"},
		},
		{
			name:  "arithmetic_requires_numeric",
			input: usersTable(),
			query: `filter { false } | group city | reduce bad = sum(age) + first(name)`,
			wants: []string{"+", "numeric"},
		},
		{
			name:  "comparison_requires_compatible_types",
			input: usersTable(),
			query: `filter { false } | group city | reduce bad = sum(age) == first(name)`,
			wants: []string{"compare", "int", "string"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expectReduceErrContains(t, tc.input, tc.query, tc.wants...)
		})
	}
}

func TestReducePlanningRejectsInvalidNestedAggregatePathsBeforeRows(t *testing.T) {
	cases := []struct {
		name  string
		query string
		wants []string
	}{
		{
			name:  "missing_nested_field_in_loaded_schema",
			query: `filter { false } | group address.city | reduce bad = first(profile.missing)`,
			wants: []string{"missing", "not found"},
		},
		{
			name:  "cannot_step_through_list",
			query: `filter { false } | group address.city | reduce bad = first(orders.amount)`,
			wants: []string{"orders", "list"},
		},
		{
			name:  "numeric_aggregate_rejects_string_nested_field",
			query: `filter { false } | group address.city | reduce bad = avg(address.city)`,
			wants: []string{"avg", "numeric"},
		},
		{
			name:  "numeric_aggregate_rejects_record",
			query: `filter { false } | group address.city | reduce bad = sum(address)`,
			wants: []string{"sum", "numeric"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := loadAndQueryExpectErr(t, testdataDir+"/nested.json", tc.query)
			if err == nil {
				t.Fatalf("expected error containing %v", tc.wants)
			}
			assertErrContainsAll(t, err, tc.wants...)
		})
	}
}

func TestReducePlanningKeepsValidZeroRowSchemas(t *testing.T) {
	result := runQuery(t, usersTable(), `filter { false } | group city | reduce total = sum(age), avg_age = avg(age), min_age = min(age), max_age = max(age), min_name = min(name), max_name = max(name), first_name = first(name), last_city = last(city), n = count(), plus_one = sum(age) + 1, ratio = sum(age) / count(), eq_float = sum(age) == 30.0, is_empty = count() == 0, maybe = count() == 0 and null, not_empty = not (count() == 0), literal = 7, nothing = null, first_is_null = first(name) is null | describe`)
	assertDescribeSchemaRows(t, result, map[string]describeSchemaMeta{
		"city":          {typ: "string", rows: 0, schema: "string"},
		"grouped":       {typ: "list", rows: 0, schema: "list<record<age:int, city:string, name:string>>"},
		"total":         {typ: "int", rows: 0, schema: "int?"},
		"avg_age":       {typ: "float", rows: 0, schema: "float?"},
		"min_age":       {typ: "int", rows: 0, schema: "int?"},
		"max_age":       {typ: "int", rows: 0, schema: "int?"},
		"min_name":      {typ: "string", rows: 0, schema: "string?"},
		"max_name":      {typ: "string", rows: 0, schema: "string?"},
		"first_name":    {typ: "string", rows: 0, schema: "string?"},
		"last_city":     {typ: "string", rows: 0, schema: "string?"},
		"n":             {typ: "int", rows: 0, schema: "int"},
		"plus_one":      {typ: "int", rows: 0, schema: "int?"},
		"ratio":         {typ: "float", rows: 0, schema: "float?"},
		"eq_float":      {typ: "bool", rows: 0, schema: "bool?"},
		"is_empty":      {typ: "bool", rows: 0, schema: "bool"},
		"maybe":         {typ: "bool", rows: 0, schema: "bool?"},
		"not_empty":     {typ: "bool", rows: 0, schema: "bool"},
		"literal":       {typ: "int", rows: 0, schema: "int"},
		"nothing":       {typ: "string", rows: 0, schema: "string?"},
		"first_is_null": {typ: "bool", rows: 0, schema: "bool"},
	})
}

func TestReducePlanningKeepsValidNestedZeroRowSchemas(t *testing.T) {
	result := loadAndQuery(t, testdataDir+"/nested.json", `filter { false } | group address.city | reduce avg_score = avg(profile.stats.score), max_logins = max(profile.stats.logins), first_tags = first(tags), first_profile = first(profile) | describe`)
	assertDescribeSchemaRows(t, result, map[string]describeSchemaMeta{
		"address_city":  {typ: "string", rows: 0, schema: "string"},
		"grouped":       {typ: "list", rows: 0, schema: "list<record<address:record<city:string, street:string, zip:string>, id:int, name:string, orders:list<record<amount:float, order_id:int, status:string>>, profile:record<history:list<record<date:string, events:list<string>>>, stats:record<logins:int, score:float>>, tags:list<string>>>"},
		"avg_score":     {typ: "float", rows: 0, schema: "float?"},
		"max_logins":    {typ: "int", rows: 0, schema: "int?"},
		"first_tags":    {typ: "list", rows: 0, schema: "list<string>?"},
		"first_profile": {typ: "record", rows: 0, schema: "record<history:list<record<date:string, events:list<string>>>, stats:record<logins:int, score:float>>?"},
	})
}

func TestReducePlannedExpressionsEvaluateTypedRuntimeBranches(t *testing.T) {
	result := runQuery(t, usersTable(), `group city | reduce total = sum(age), plus_one = sum(age) + 1, minus_one = sum(age) - 1, doubled = sum(age) * 2, ratio = sum(age) / count(), neg_total = -sum(age), gt_100 = sum(age) > 100, eq_float = sum(age) == 105.0, nonempty = count() > 0 and true, known_true = count() > 0 or null, unknown_when_empty = count() == 0 or null, not_empty = not (count() == 0), first_is_null = first(name) is null, first_is_not_null = first(name) is not null | remove grouped | sort city`)

	type wantRow struct {
		total    int64
		ratio    float64
		gt100    bool
		eqFloat  bool
		negTotal int64
	}
	want := map[string]wantRow{
		"LA": {total: 47, ratio: 23.5, gt100: false, eqFloat: false, negTotal: -47},
		"NY": {total: 105, ratio: 35, gt100: true, eqFloat: true, negTotal: -105},
		"SF": {total: 28, ratio: 28, gt100: false, eqFloat: false, negTotal: -28},
	}

	cols := map[string]int{}
	for _, name := range []string{
		"city", "total", "plus_one", "minus_one", "doubled", "ratio", "neg_total",
		"gt_100", "eq_float", "nonempty", "known_true", "unknown_when_empty",
		"not_empty", "first_is_null", "first_is_not_null",
	} {
		idx := result.ColIndex(name)
		if idx < 0 {
			t.Fatalf("expected column %q in %v", name, result.Columns)
		}
		cols[name] = idx
	}
	if result.NumRows != len(want) {
		t.Fatalf("row count: got %d, want %d; table=%s", result.NumRows, len(want), result.String())
	}

	for i := 0; i < result.NumRows; i++ {
		city := result.GetAt(i, cols["city"]).Str
		wantRow, ok := want[city]
		if !ok {
			t.Fatalf("unexpected city %q in row %d", city, i)
		}
		assertIntValue(t, result.GetAt(i, cols["total"]), wantRow.total, city+".total")
		assertIntValue(t, result.GetAt(i, cols["plus_one"]), wantRow.total+1, city+".plus_one")
		assertIntValue(t, result.GetAt(i, cols["minus_one"]), wantRow.total-1, city+".minus_one")
		assertIntValue(t, result.GetAt(i, cols["doubled"]), wantRow.total*2, city+".doubled")
		assertFloatValue(t, result.GetAt(i, cols["ratio"]), wantRow.ratio, city+".ratio")
		assertIntValue(t, result.GetAt(i, cols["neg_total"]), wantRow.negTotal, city+".neg_total")
		assertBoolValue(t, result.GetAt(i, cols["gt_100"]), wantRow.gt100, city+".gt_100")
		assertBoolValue(t, result.GetAt(i, cols["eq_float"]), wantRow.eqFloat, city+".eq_float")
		assertBoolValue(t, result.GetAt(i, cols["nonempty"]), true, city+".nonempty")
		assertBoolValue(t, result.GetAt(i, cols["known_true"]), true, city+".known_true")
		if got := result.GetAt(i, cols["unknown_when_empty"]); !got.IsNull() {
			t.Fatalf("%s.unknown_when_empty: want null, got %v", city, got)
		}
		assertBoolValue(t, result.GetAt(i, cols["not_empty"]), true, city+".not_empty")
		assertBoolValue(t, result.GetAt(i, cols["first_is_null"]), false, city+".first_is_null")
		assertBoolValue(t, result.GetAt(i, cols["first_is_not_null"]), true, city+".first_is_not_null")
	}
}

func TestReduceMinMaxAcceptOrderableStringColumns(t *testing.T) {
	result := runQuery(t, usersTable(), `group city | reduce min_name = min(name), max_name = max(name) | remove grouped | sort city`)
	cityIdx := result.ColIndex("city")
	minIdx := result.ColIndex("min_name")
	maxIdx := result.ColIndex("max_name")
	if cityIdx < 0 || minIdx < 0 || maxIdx < 0 {
		t.Fatalf("expected city/min_name/max_name columns, got %v", result.Columns)
	}

	want := map[string][2]string{
		"LA": {"Bob", "Eve"},
		"NY": {"Alice", "Frank"},
		"SF": {"Diana", "Diana"},
	}
	if result.NumRows != len(want) {
		t.Fatalf("row count: got %d, want %d; table=%s", result.NumRows, len(want), result.String())
	}
	for i := 0; i < result.NumRows; i++ {
		city := result.GetAt(i, cityIdx).Str
		got := [2]string{result.GetAt(i, minIdx).Str, result.GetAt(i, maxIdx).Str}
		if got != want[city] {
			t.Fatalf("%s min/max names: got %v, want %v", city, got, want[city])
		}
	}
}

func reducePlanningNestedValueTable() *table.Table {
	tbl := table.NewTable([]string{"name", "xs", "profile"})
	tbl.AddRow([]table.Value{
		table.StrVal("a"),
		table.ListVal([]table.Value{table.IntVal(1), table.IntVal(2)}),
		table.RecordVal([]table.RecordField{
			{Name: "score", Value: table.IntVal(10)},
		}),
	})
	return tbl
}

func reducePlanningScalarListTable(t *testing.T) *table.Table {
	t.Helper()
	tbl := table.NewTableWithSchemas(
		[]string{"name", "xs"},
		[]*table.TypeDescriptor{
			{Kind: table.TypeString},
			{Kind: table.TypeList, Elem: &table.TypeDescriptor{Kind: table.TypeInt}},
		},
	)
	if err := tbl.AddRowTyped([]table.Value{
		table.StrVal("a"),
		table.ListVal([]table.Value{table.IntVal(1), table.IntVal(2)}),
	}); err != nil {
		t.Fatalf("seed scalar list table: %v", err)
	}
	return tbl
}

func expectReduceErrContains(t *testing.T, input *table.Table, query string, wants ...string) {
	t.Helper()
	err := runQueryExpectErr(t, input, query)
	if err == nil {
		t.Fatalf("expected error containing %v", wants)
	}
	assertErrContainsAll(t, err, wants...)
}

func assertErrContainsAll(t *testing.T, err error, wants ...string) {
	t.Helper()
	msg := strings.ToLower(err.Error())
	for _, want := range wants {
		if !strings.Contains(msg, strings.ToLower(want)) {
			t.Fatalf("expected error containing %q, got: %v", want, err)
		}
	}
}

func assertIntValue(t *testing.T, got table.Value, want int64, label string) {
	t.Helper()
	if got.Type != table.TypeInt || got.Int != want {
		t.Fatalf("%s: want int %d, got %v", label, want, got)
	}
}

func assertFloatValue(t *testing.T, got table.Value, want float64, label string) {
	t.Helper()
	if got.Type != table.TypeFloat || got.Float != want {
		t.Fatalf("%s: want float %v, got %v", label, want, got)
	}
}

func assertBoolValue(t *testing.T, got table.Value, want bool, label string) {
	t.Helper()
	if got.Type != table.TypeBool || got.Bool != want {
		t.Fatalf("%s: want bool %v, got %v", label, want, got)
	}
}
