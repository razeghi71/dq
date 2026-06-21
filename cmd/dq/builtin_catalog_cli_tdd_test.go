package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCLIBuiltinCatalogScalarSpecialAndAggregateHappyPathsAllFlatFormats(t *testing.T) {
	bin := buildCLI(t)

	for _, input := range cliFlatUserInputFiles() {
		t.Run(input.name, func(t *testing.T) {
			out := runCLIQuery(t, bin, input.path+` | transform norm = upper(trim(name)), lower_city = lower(city), name_len = str_len(name), prefix = substr(name, 0, 2), has_i = str_contains(name, "i"), starts_a = starts_with(name, "A"), ends_e = ends_with(name, "e"), match_a = matches(name, "^[A-Z]"), city_or_unknown = coalesce(city, "unknown"), class = if(age > 30, "senior", "standard") | group city_or_unknown | reduce n = count(), total_age = sum(age), avg_age = avg(age), min_name = min(name), max_name = max(name), first_norm = first(norm), last_class = last(class) | remove grouped | sort city_or_unknown | json`)
			var rows []map[string]any
			if err := json.Unmarshal(out, &rows); err != nil {
				t.Fatalf("invalid JSON output:\n%s", out)
			}
			if len(rows) == 0 {
				t.Fatalf("expected grouped rows, got none:\n%s", out)
			}
			for _, row := range rows {
				for _, col := range []string{"city_or_unknown", "n", "total_age", "avg_age", "min_name", "max_name", "first_norm", "last_class"} {
					if _, ok := row[col]; !ok {
						t.Fatalf("missing column %q in row %#v", col, row)
					}
				}
			}
		})
	}
}

func TestCLIBuiltinCatalogNestedListFunctionsAllNestedFormats(t *testing.T) {
	bin := buildCLI(t)

	for _, input := range cliNestedUserInputFiles() {
		t.Run(input.name, func(t *testing.T) {
			rows := readCLIDescribeRows(t, runCLIQuery(t, bin, input.path+` | transform city = upper(address.city), tag_count = list_len(tags), has_user = list_contains(tags, "user"), fallback_city = coalesce(address.city, "unknown"), route = if(list_len(orders) > 0, "has_orders", "empty") | filter { starts_with(city, "NEW") or has_user } | select name, city, tag_count, fallback_city, route | describe | json`))
			requireCLIDescribeSchema(t, rows, "name", "string", "string", 3)
			requireCLIDescribeSchema(t, rows, "city", "string", "string", 3)
			requireCLIDescribeSchema(t, rows, "tag_count", "int", "int", 3)
			requireCLIDescribeSchema(t, rows, "fallback_city", "string", "string", 3)
			requireCLIDescribeSchema(t, rows, "route", "string", "string", 3)
		})
	}
}

func TestCLIBuiltinCatalogDateFunctionsStayStable(t *testing.T) {
	bin := buildCLI(t)

	rows := readCLIDescribeRows(t, runCLIQuery(t, bin, `../../testdata/sales.csv | transform y = year(date), m = month(date), d = day(date), label = upper(trim(product)) | filter { y >= 2024 and starts_with(label, "WIDGET") } | select y, m, d, label | describe | json`))
	requireCLIDescribeSchema(t, rows, "y", "int", "int", 5)
	requireCLIDescribeSchema(t, rows, "m", "int", "int", 5)
	requireCLIDescribeSchema(t, rows, "d", "int", "int", 5)
	requireCLIDescribeSchema(t, rows, "label", "string", "string", 5)
}

func TestCLIBuiltinCatalogPlanningErrorsAllFlatFormats(t *testing.T) {
	bin := buildCLI(t)

	for _, input := range cliFlatUserInputFiles() {
		t.Run(input.name, func(t *testing.T) {
			cases := []struct {
				name  string
				query string
				want  []string
			}{
				{
					name:  "scalar_bad_type_after_zero_rows",
					query: input.path + ` | filter { false } | transform bad = upper(age) | json`,
					want:  []string{"upper", "string", "int"},
				},
				{
					name:  "coalesce_mismatch_after_zero_rows",
					query: input.path + ` | filter { false } | transform bad = coalesce(age, "missing") | json`,
					want:  []string{"coalesce", "common type"},
				},
				{
					name:  "if_branch_mismatch_after_zero_rows",
					query: input.path + ` | filter { false } | transform bad = if(age > 20, 1, "x") | json`,
					want:  []string{"if", "branches", "common type"},
				},
				{
					name:  "aggregate_outside_reduce",
					query: input.path + ` | transform bad = count() | json`,
					want:  []string{"count", "reduce"},
				},
				{
					name:  "scalar_inside_reduce",
					query: input.path + ` | group city | reduce bad = upper(name) | json`,
					want:  []string{"upper", "reduce"},
				},
				{
					name:  "special_form_inside_reduce",
					query: input.path + ` | group city | reduce bad = coalesce(first(name), "unknown") | json`,
					want:  []string{"coalesce", "reduce"},
				},
				{
					name:  "unknown_function",
					query: input.path + ` | transform bad = slugify(name) | json`,
					want:  []string{`unknown function "slugify"`},
				},
				{
					name:  "case_sensitive_name",
					query: input.path + ` | transform bad = Upper(name) | json`,
					want:  []string{`unknown function "Upper"`},
				},
				{
					name:  "case_sensitive_list_constructor",
					query: input.path + ` | transform bad = List(1) | json`,
					want:  []string{`unknown function "List"`},
				},
				{
					name:  "case_sensitive_struct_constructor",
					query: input.path + ` | transform bad = STRUCT(1) | json`,
					want:  []string{`unknown function "STRUCT"`},
				},
			}

			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					out := runCLIQueryExpectError(t, bin, tc.query)
					for _, want := range tc.want {
						if !strings.Contains(strings.ToLower(string(out)), strings.ToLower(want)) {
							t.Fatalf("expected error containing %q, got:\n%s", want, out)
						}
					}
				})
			}
		})
	}
}

func TestCLIBuiltinCatalogSpecialFormsRemainLazyAtRuntime(t *testing.T) {
	bin := buildCLI(t)

	cases := []struct {
		name  string
		query string
	}{
		{
			name:  "if_skips_runtime_invalid_then_branch",
			query: `../../testdata/users.csv | transform x = if(false, year("not-a-date"), 7) | filter { x == 7 } | count | json`,
		},
		{
			name:  "if_skips_runtime_invalid_else_branch",
			query: `../../testdata/users.csv | transform x = if(true, 7, year("not-a-date")) | filter { x == 7 } | count | json`,
		},
		{
			name:  "coalesce_skips_runtime_invalid_later_branch",
			query: `../../testdata/users.csv | transform x = coalesce(7, year("not-a-date")) | filter { x == 7 } | count | json`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			requireCLIExpressionCount(t, runCLIQuery(t, bin, tc.query), 6)
		})
	}
}

func TestCLIBuiltinCatalogCompiledPredicatePathMatchesTransformPath(t *testing.T) {
	bin := buildCLI(t)

	for _, input := range cliFlatUserInputFiles() {
		t.Run(input.name, func(t *testing.T) {
			filterOut := runCLIQuery(t, bin, input.path+` | filter { starts_with(name, "A") } | count | json`)
			transformOut := runCLIQuery(t, bin, input.path+` | transform hit = starts_with(name, "A") | filter { hit } | count | json`)
			var filterRows, transformRows []map[string]any
			if err := json.Unmarshal(filterOut, &filterRows); err != nil {
				t.Fatalf("invalid filter JSON:\n%s", filterOut)
			}
			if err := json.Unmarshal(transformOut, &transformRows); err != nil {
				t.Fatalf("invalid transform JSON:\n%s", transformOut)
			}
			if len(filterRows) != 1 || len(transformRows) != 1 || filterRows[0]["count"] != transformRows[0]["count"] {
				t.Fatalf("compiled filter count %s differs from transform count %s", filterOut, transformOut)
			}
		})
	}
}
