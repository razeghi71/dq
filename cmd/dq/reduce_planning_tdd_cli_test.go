package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCLIReduceDivisionRuntimeMatchesPlanner(t *testing.T) {
	bin := buildCLI(t)
	path := filepath.Join(t.TempDir(), "gids.csv")
	if err := os.WriteFile(path, []byte("g,id\na,9007199254740993\n"), 0o644); err != nil {
		t.Fatalf("write large group fixture: %v", err)
	}

	requireCLIExpressionBool(t, runCLIQuery(t, bin, path+` | group g | reduce ok = (sum(id) / count()) == 9007199254740992.0 | remove grouped | select ok | json`), "ok", true)
	requireCLIExpressionBool(t, runCLIQuery(t, bin, path+` | group g | reduce avg_id = sum(id) / count() | remove grouped | transform ok = avg_id == 9007199254740992.0 | select ok | json`), "ok", true)
	requireCLIExpressionCount(t, runCLIQuery(t, bin, path+` | group g | reduce avg_id = sum(id) / count() | remove grouped | filter { avg_id == 9007199254740992.0 } | count | json`), 1)
}

func TestCLIReduceIntegerArithmeticPreservesExactRuntimeValues(t *testing.T) {
	bin := buildCLI(t)
	path := filepath.Join(t.TempDir(), "gids.csv")
	if err := os.WriteFile(path, []byte("g,id\na,9007199254740993\n"), 0o644); err != nil {
		t.Fatalf("write large group fixture: %v", err)
	}

	out := runCLIQuery(t, bin, path+` | group g | reduce add = sum(id) + 0, sub = sum(id) - 0, mul = sum(id) * 1 | remove grouped | select add, sub, mul | json`)
	for _, col := range []string{"add", "sub", "mul"} {
		requireCLIExactJSONInt(t, out, col, "9007199254740993")
	}
	requireCLIExpressionCount(t, runCLIQuery(t, bin, path+` | group g | reduce total = sum(id) + 0 | remove grouped | filter { total == 9007199254740993 } | count | json`), 1)
}

func TestCLIReduceIntegerArithmeticOverflowErrors(t *testing.T) {
	bin := buildCLI(t)

	t.Run("sum_overflow", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "gids.csv")
		if err := os.WriteFile(path, []byte("g,id\na,9223372036854775807\na,1\n"), 0o644); err != nil {
			t.Fatalf("write sum overflow fixture: %v", err)
		}
		out := runCLIQueryExpectError(t, bin, path+` | group g | reduce total = sum(id) | remove grouped | json`)
		assertCLIExpressionErrorContains(t, out, "integer overflow")
	})

	t.Run("reduce_expression_overflow", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "gids.csv")
		if err := os.WriteFile(path, []byte("g,id\na,9223372036854775807\n"), 0o644); err != nil {
			t.Fatalf("write reduce overflow fixture: %v", err)
		}
		out := runCLIQueryExpectError(t, bin, path+` | group g | reduce total = sum(id) + 1 | remove grouped | json`)
		assertCLIExpressionErrorContains(t, out, "integer overflow")
	})
}

func TestCLIReducePlanningRejectsInvalidNestedSourceBeforeRows(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	scalarPath := filepath.Join(dir, "one.csv")
	if err := os.WriteFile(scalarPath, []byte("x\n1\n"), 0o644); err != nil {
		t.Fatalf("write scalar fixture: %v", err)
	}
	listPath := filepath.Join(dir, "scalar_list.json")
	if err := os.WriteFile(listPath, []byte(`[{"xs":[1,2,3]}]`), 0o644); err != nil {
		t.Fatalf("write scalar list fixture: %v", err)
	}

	cases := []struct {
		name  string
		query string
		want  []string
	}{
		{
			name:  "scalar_source_count_only",
			query: scalarPath + ` | filter { false } | reduce x n = count() | describe | json`,
			want:  []string{"reduce", "x", "list", "record"},
		},
		{
			name:  "scalar_list_source_count_only",
			query: listPath + ` | filter { false } | reduce xs n = count() | describe | json`,
			want:  []string{"reduce", "xs", "record"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := runCLIQueryExpectError(t, bin, tc.query)
			assertCLIExpressionErrorContains(t, out, tc.want...)
		})
	}
}

func TestCLIReducePlanningRejectsInvalidExpressionsBeforeRowsAllFlatFormats(t *testing.T) {
	bin := buildCLI(t)

	for _, input := range cliFlatUserInputFiles() {
		t.Run(input.name, func(t *testing.T) {
			cases := []struct {
				name string
				expr string
				want []string
			}{
				{"non_aggregate_function", `upper(name)`, []string{"upper", "reduce"}},
				{"non_aggregate_function_nested_in_binary", `sum(age) + upper(name)`, []string{"upper", "reduce"}},
				{"bare_column_expression", `name`, []string{"column", "reduce"}},
				{"sibling_assignment_not_visible", `sum(age), doubled = total * 2`, []string{"total", "reduce"}},
				{"sum_string_column", `sum(name)`, []string{"sum", "numeric"}},
				{"avg_string_column", `avg(city)`, []string{"avg", "numeric"}},
				{"missing_aggregate_column", `first(missing)`, []string{"missing", "not found"}},
				{"aggregate_arg_must_be_column_function", `first(upper(name))`, []string{"first", "column reference"}},
				{"aggregate_arg_must_be_column_expression", `sum(age + 1)`, []string{"sum", "column reference"}},
				{"aggregate_count_takes_no_args", `count(age)`, []string{"count", "no arguments"}},
				{"not_requires_bool", `not sum(age)`, []string{"not", "boolean"}},
				{"and_requires_bool_left", `sum(age) and true`, []string{"and", "boolean"}},
				{"arithmetic_requires_numeric", `sum(age) + first(name)`, []string{"+", "numeric"}},
				{"comparison_requires_compatible_types", `sum(age) == first(name)`, []string{"compare", "int", "string"}},
			}

			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					query := input.path + ` | filter { false } | group city | reduce bad = ` + tc.expr + ` | describe | json`
					if tc.name == "sibling_assignment_not_visible" {
						query = input.path + ` | filter { false } | group city | reduce total = ` + tc.expr + ` | describe | json`
					}
					out := runCLIQueryExpectError(t, bin, query)
					assertCLIExpressionErrorContains(t, out, tc.want...)
				})
			}
		})
	}
}

func TestCLIReducePlanningRejectsInvalidNestedAggregatePathsBeforeRowsAllNestedFormats(t *testing.T) {
	bin := buildCLI(t)

	for _, input := range cliNestedUserInputFiles() {
		t.Run(input.name, func(t *testing.T) {
			cases := []struct {
				name string
				expr string
				want []string
			}{
				{"missing_nested_field_in_loaded_schema", `first(profile.missing)`, []string{"missing", "not found"}},
				{"cannot_step_through_list", `first(orders.amount)`, []string{"orders", "list"}},
				{"numeric_aggregate_rejects_string_nested_field", `avg(address.city)`, []string{"avg", "numeric"}},
				{"numeric_aggregate_rejects_record", `sum(address)`, []string{"sum", "numeric"}},
			}

			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					out := runCLIQueryExpectError(t, bin, input.path+` | filter { false } | group address.city | reduce bad = `+tc.expr+` | describe | json`)
					assertCLIExpressionErrorContains(t, out, tc.want...)
				})
			}
		})
	}
}

func TestCLIReducePlanningRejectsDuplicateTargetsAllFlatFormats(t *testing.T) {
	bin := buildCLI(t)

	for _, input := range cliFlatUserInputFiles() {
		t.Run(input.name, func(t *testing.T) {
			cases := []struct {
				name  string
				query string
			}{
				{
					name:  "with_rows",
					query: input.path + ` | group city | reduce x = count(), x = sum(age) | remove grouped | sort city | json`,
				},
				{
					name:  "zero_rows",
					query: input.path + ` | filter { false } | group city | reduce x = count(), x = sum(age) | describe | json`,
				},
			}

			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					out := runCLIQueryExpectError(t, bin, tc.query)
					assertCLIExpressionErrorContains(t, out, "reduce target", "x", "more than once")
				})
			}
		})
	}
}

func TestCLIReducePlanningKeepsValidZeroRowSchemasAcrossFlatFormats(t *testing.T) {
	bin := buildCLI(t)

	for _, input := range cliFlatUserInputFiles() {
		t.Run(input.name, func(t *testing.T) {
			rows := readCLIDescribeRows(t, runCLIQuery(t, bin, input.path+` | filter { false } | group city | reduce total = sum(age), avg_age = avg(age), min_age = min(age), max_age = max(age), min_name = min(name), max_name = max(name), first_name = first(name), last_city = last(city), n = count(), plus_one = sum(age) + 1, ratio = sum(age) / count(), eq_float = sum(age) == 30.0, maybe = count() == 0 and null, not_empty = not (count() == 0), first_is_null = first(name) is null | describe | json`))
			requireCLIDescribeSchema(t, rows, "city", "string", "string", 0)
			requireCLIDescribeSchema(t, rows, "grouped", "list", "list<record<age:int, city:string, name:string>>", 0)
			requireCLIDescribeSchema(t, rows, "total", "int", "int?", 0)
			requireCLIDescribeSchema(t, rows, "avg_age", "float", "float?", 0)
			requireCLIDescribeSchema(t, rows, "min_age", "int", "int?", 0)
			requireCLIDescribeSchema(t, rows, "max_age", "int", "int?", 0)
			requireCLIDescribeSchema(t, rows, "min_name", "string", "string?", 0)
			requireCLIDescribeSchema(t, rows, "max_name", "string", "string?", 0)
			requireCLIDescribeSchema(t, rows, "first_name", "string", "string?", 0)
			requireCLIDescribeSchema(t, rows, "last_city", "string", "string?", 0)
			requireCLIDescribeSchema(t, rows, "n", "int", "int", 0)
			requireCLIDescribeSchema(t, rows, "plus_one", "int", "int?", 0)
			requireCLIDescribeSchema(t, rows, "ratio", "float", "float?", 0)
			requireCLIDescribeSchema(t, rows, "eq_float", "bool", "bool?", 0)
			requireCLIDescribeSchema(t, rows, "maybe", "bool", "bool?", 0)
			requireCLIDescribeSchema(t, rows, "not_empty", "bool", "bool", 0)
			requireCLIDescribeSchema(t, rows, "first_is_null", "bool", "bool", 0)
		})
	}
}

func TestCLIReducePlanningKeepsValidNestedZeroRowSchemasAcrossNestedFormats(t *testing.T) {
	bin := buildCLI(t)

	for _, input := range cliNestedUserInputFiles() {
		t.Run(input.name, func(t *testing.T) {
			rows := readCLIDescribeRows(t, runCLIQuery(t, bin, input.path+` | filter { false } | group address.city | reduce avg_score = avg(profile.stats.score), max_logins = max(profile.stats.logins), first_tags = first(tags), first_profile = first(profile) | describe | json`))
			requireCLIDescribeSchema(t, rows, "address_city", "string", "string", 0)
			requireCLIDescribeSchema(t, rows, "avg_score", "float", "float?", 0)
			requireCLIDescribeSchema(t, rows, "max_logins", "int", "int?", 0)
			requireCLIDescribeSchema(t, rows, "first_tags", "list", "list<string>?", 0)
			requireCLIDescribeSchema(t, rows, "first_profile", "record", "record<history:list<record<date:string, events:list<string>>>, stats:record<logins:int, score:float>>?", 0)
		})
	}
}
