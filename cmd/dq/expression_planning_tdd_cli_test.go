package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIExpressionPlanningBindingRejectsMissingTopLevelColumnsAfterZeroRowsAllFlatFormats(t *testing.T) {
	bin := buildCLI(t)

	for _, input := range cliFlatUserInputFiles() {
		t.Run(input.name, func(t *testing.T) {
			cases := []struct {
				name  string
				query string
			}{
				{
					name:  "filter",
					query: input.path + ` | filter { false } | filter { agge > 20 } | count`,
				},
				{
					name:  "transform",
					query: input.path + ` | filter { false } | transform out = upper(agge) | describe | json`,
				},
				{
					name:  "select",
					query: input.path + ` | filter { false } | select agge | describe | json`,
				},
				{
					name:  "sort",
					query: input.path + ` | filter { false } | sort agge | count`,
				},
			}

			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					out := runCLIQueryExpectError(t, bin, tc.query)
					assertCLIExpressionErrorContains(t, out, "agge", "not found")
				})
			}
		})
	}
}

func TestCLIExpressionPlanningBindingUsesSchemaAfterEachPipelineStage(t *testing.T) {
	bin := buildCLI(t)

	t.Run("later_filter_can_bind_transform_output", func(t *testing.T) {
		out := runCLIQuery(t, bin, `../../testdata/users.csv | transform age2 = age + 1 | filter { age2 > 20 } | describe | json`)
		rows := readCLIDescribeRows(t, out)
		requireCLIDescribeSchema(t, rows, "age2", "int", "int", 6)
	})

	t.Run("filter_after_select_cannot_bind_removed_column_even_with_zero_rows", func(t *testing.T) {
		out := runCLIQueryExpectError(t, bin, `../../testdata/users.csv | select name | filter { false } | filter { age > 20 } | count`)
		assertCLIExpressionErrorContains(t, out, "age", "not found")
	})

	t.Run("same_transform_rhs_cannot_bind_new_assignment_target", func(t *testing.T) {
		out := runCLIQueryExpectError(t, bin, `../../testdata/users.csv | filter { false } | transform age2 = age + 1, age3 = age2 + 1 | describe | json`)
		assertCLIExpressionErrorContains(t, out, "age2", "not found")
	})

	t.Run("filter_after_select_cannot_bind_removed_transform_output", func(t *testing.T) {
		out := runCLIQueryExpectError(t, bin, `../../testdata/users.csv | transform age2 = age + 1 | select name | filter { false } | filter { age2 > 20 } | count`)
		assertCLIExpressionErrorContains(t, out, "age2", "not found")
	})
}

func TestCLIExpressionPlanningRejectsDuplicateTransformTargetsAllFlatFormats(t *testing.T) {
	bin := buildCLI(t)

	for _, input := range cliFlatUserInputFiles() {
		t.Run(input.name, func(t *testing.T) {
			cases := []struct {
				name  string
				query string
				wants []string
			}{
				{
					name:  "with_rows",
					query: input.path + ` | transform x = 1, x = 2 | select x | json`,
					wants: []string{"transform target", "x", "more than once"},
				},
				{
					name:  "after_zero_row_filter",
					query: input.path + ` | filter { false } | transform x = 1, x = 2 | describe | json`,
					wants: []string{"transform target", "x", "more than once"},
				},
				{
					name:  "overwrite_existing_column",
					query: input.path + ` | transform age = age + 1, age = age + 2 | select age | json`,
					wants: []string{"transform target", "age", "more than once"},
				},
			}

			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					out := runCLIQueryExpectError(t, bin, tc.query)
					assertCLIExpressionErrorContains(t, out, tc.wants...)
				})
			}
		})
	}
}

func TestCLIExpressionPlanningBindingRejectsNestedSchemaIssuesAfterZeroRows(t *testing.T) {
	bin := buildCLI(t)

	for _, input := range cliNestedUserInputFiles() {
		t.Run(input.name, func(t *testing.T) {
			cases := []struct {
				name  string
				query string
				wants []string
			}{
				{
					name:  "filter_missing_nested_field",
					query: input.path + ` | filter { false } | filter { address.missing == "NY" } | count`,
					wants: []string{"missing", "not found"},
				},
				{
					name:  "transform_missing_nested_field",
					query: input.path + ` | filter { false } | transform city = address.missing | describe | json`,
					wants: []string{"missing", "not found"},
				},
				{
					name:  "group_missing_nested_field",
					query: input.path + ` | filter { false } | group address.missing | count`,
					wants: []string{"group", "missing", "not found"},
				},
				{
					name:  "distinct_missing_nested_field",
					query: input.path + ` | filter { false } | distinct address.missing | count`,
					wants: []string{"distinct", "missing", "not found"},
				},
				{
					name:  "join_missing_nested_field_shorthand",
					query: input.path + ` | join ` + input.path + ` on address.missing | count | json`,
					wants: []string{"join", "missing", "not found"},
				},
				{
					name:  "join_missing_nested_field_left_key",
					query: input.path + ` | join ` + input.path + ` on address.missing == address.city | count | json`,
					wants: []string{"join", "missing", "not found"},
				},
				{
					name:  "join_missing_nested_field_right_key",
					query: input.path + ` | join ` + input.path + ` on address.city == address.missing | count | json`,
					wants: []string{"join", "missing", "not found"},
				},
				{
					name:  "full_join_missing_nested_field_does_not_synthesize_key",
					query: input.path + ` | join full ` + input.path + ` on address.missing | select address_missing | describe | json`,
					wants: []string{"join", "missing", "not found"},
				},
				{
					name:  "select_list_dot_traversal",
					query: input.path + ` | filter { false } | select orders.amount | describe | json`,
					wants: []string{"orders", "list"},
				},
				{
					name:  "sort_list_dot_traversal",
					query: input.path + ` | filter { false } | sort orders.amount | count`,
					wants: []string{"orders", "list"},
				},
			}

			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					out := runCLIQueryExpectError(t, bin, tc.query)
					assertCLIExpressionErrorContains(t, out, tc.wants...)
				})
			}
		})
	}
}

func TestCLIExpressionPlanningNestedHappyPathsStillWorkAcrossFormats(t *testing.T) {
	bin := buildCLI(t)

	for _, input := range cliNestedUserInputFiles() {
		t.Run(input.name, func(t *testing.T) {
			out := runCLIQuery(t, bin, input.path+` | transform city = address.city, order_count = list_len(orders) | filter { city == "New York" and order_count > 0 } | select name, city, order_count | json`)
			var rows []map[string]any
			if err := json.Unmarshal(out, &rows); err != nil {
				t.Fatalf("invalid JSON output:\n%s", out)
			}
			if len(rows) != 1 {
				t.Fatalf("expected one New York row for %s, got:\n%s", input.name, out)
			}
			if rows[0]["name"] != "Alice" || rows[0]["city"] != "New York" || rows[0]["order_count"].(float64) != 2 {
				t.Fatalf("unexpected nested happy-path row: %#v", rows[0])
			}
		})
	}
}

func TestCLIExpressionPlanningEmptyListLiteralAdoptsTypedListContextAcrossNestedFormats(t *testing.T) {
	bin := buildCLI(t)

	for _, input := range cliNestedUserInputFiles() {
		t.Run(input.name, func(t *testing.T) {
			rows := readCLIDescribeRows(t, runCLIQuery(t, bin, input.path+` | transform maybe_orders = if(name == "Alice", orders, list()), fallback_orders = coalesce(orders, list()), first_orders = coalesce(list(), orders) | describe | json`))
			requireCLIDescribeSchema(t, rows, "maybe_orders", "list", "list<record<amount:float, order_id:int, status:string>>", 3)
			requireCLIDescribeSchema(t, rows, "fallback_orders", "list", "list<record<amount:float, order_id:int, status:string>>", 3)
			requireCLIDescribeSchema(t, rows, "first_orders", "list", "list<record<amount:float, order_id:int, status:string>>", 3)
		})
	}
}

func TestCLIExpressionPlanningEmptyListLiteralAdoptsScalarListContext(t *testing.T) {
	bin := buildCLI(t)
	path := filepath.Join(t.TempDir(), "a.csv")
	if err := os.WriteFile(path, []byte("x\n1\n2\n"), 0o644); err != nil {
		t.Fatalf("write scalar list fixture: %v", err)
	}

	rows := readCLIDescribeRows(t, runCLIQuery(t, bin, path+` | transform xs = if(x == 1, list(), list(1)), ys = if(x == 1, list(x), list()) | describe | json`))
	requireCLIDescribeSchema(t, rows, "x", "int", "int", 2)
	requireCLIDescribeSchema(t, rows, "xs", "list", "list<int>", 2)
	requireCLIDescribeSchema(t, rows, "ys", "list", "list<int>", 2)
}

func TestCLIExpressionPlanningNullableParentPathStillReturnsNull(t *testing.T) {
	bin := buildCLI(t)
	out := runCLIQuery(t, bin, `../../testdata/nested_missing.json | transform city = addr.city | sort name | json`)

	var rows []map[string]any
	if err := json.Unmarshal(out, &rows); err != nil {
		t.Fatalf("invalid JSON output:\n%s", out)
	}
	if len(rows) != 2 {
		t.Fatalf("expected two rows, got:\n%s", out)
	}
	if rows[0]["name"] != "a" || rows[0]["city"] != nil {
		t.Fatalf("null parent should project null child, got first row %#v", rows[0])
	}
	if rows[1]["name"] != "b" || rows[1]["city"] != "NY" {
		t.Fatalf("existing nested child should project value, got second row %#v", rows[1])
	}

	rowsByColumn := readCLIDescribeRows(t, runCLIQuery(t, bin, `../../testdata/nested_missing.json | filter { false } | transform city = addr.city | describe | json`))
	requireCLIDescribeSchema(t, rowsByColumn, "city", "string", "string?", 0)
}

func TestCLIExpressionTypeCheckerRejectsPredictableTypeErrorsAfterZeroRowsAllFlatFormats(t *testing.T) {
	bin := buildCLI(t)

	for _, input := range cliFlatUserInputFiles() {
		t.Run(input.name, func(t *testing.T) {
			cases := []struct {
				name  string
				query string
				wants []string
			}{
				{
					name:  "if_incompatible_branches",
					query: input.path + ` | filter { false } | transform out = if(age > 30, age, city) | json`,
					wants: []string{"if", "int", "string"},
				},
				{
					name:  "coalesce_incompatible_args",
					query: input.path + ` | filter { false } | transform out = coalesce(age, "missing") | json`,
					wants: []string{"coalesce", "int", "string"},
				},
				{
					name:  "upper_requires_string",
					query: input.path + ` | filter { false } | transform out = upper(age) | json`,
					wants: []string{"upper", "string", "int"},
				},
				{
					name:  "arithmetic_requires_numeric",
					query: input.path + ` | filter { false } | transform out = age + city | json`,
					wants: []string{"+", "int", "string"},
				},
				{
					name:  "filter_requires_bool",
					query: input.path + ` | filter { false } | filter { age } | count`,
					wants: []string{"filter", "bool", "age"},
				},
				{
					name:  "null_equality_uses_is_null",
					query: input.path + ` | filter { false } | filter { name == null } | count`,
					wants: []string{"null", "is null"},
				},
				{
					name:  "literal_null_equality_uses_is_null",
					query: input.path + ` | filter { null == null } | count`,
					wants: []string{"null", "is null"},
				},
			}

			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					out := runCLIQueryExpectError(t, bin, tc.query)
					assertCLIExpressionErrorContains(t, out, tc.wants...)
				})
			}
		})
	}
}

func TestCLIExpressionTypeCheckerRejectsNestedTypeErrorsAfterZeroRows(t *testing.T) {
	bin := buildCLI(t)

	for _, input := range cliNestedUserInputFiles() {
		t.Run(input.name, func(t *testing.T) {
			cases := []struct {
				name  string
				query string
				wants []string
			}{
				{
					name:  "str_len_rejects_list",
					query: input.path + ` | filter { false } | transform bad = str_len(orders) | json`,
					wants: []string{"str_len", "string", "list"},
				},
				{
					name:  "list_len_rejects_string",
					query: input.path + ` | filter { false } | transform bad = list_len(address.city) | json`,
					wants: []string{"list_len", "list", "string"},
				},
				{
					name:  "list_contains_rejects_wrong_element_type",
					query: input.path + ` | filter { false } | transform bad = list_contains(tags, 1) | json`,
					wants: []string{"list_contains", "string", "int"},
				},
				{
					name:  "sort_rejects_record",
					query: input.path + ` | filter { false } | sort address | count`,
					wants: []string{"sort", "record"},
				},
				{
					name:  "sort_rejects_list",
					query: input.path + ` | filter { false } | sort orders | count`,
					wants: []string{"sort", "list"},
				},
			}

			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					out := runCLIQueryExpectError(t, bin, tc.query)
					assertCLIExpressionErrorContains(t, out, tc.wants...)
				})
			}
		})
	}
}

func TestCLIExpressionTypeCheckerHappyPathsAcrossFlatFormats(t *testing.T) {
	bin := buildCLI(t)
	rowCounts := map[string]int{
		"csv":     6,
		"json":    3,
		"jsonl":   3,
		"avro":    6,
		"parquet": 6,
	}

	for _, input := range cliFlatUserInputFiles() {
		t.Run(input.name, func(t *testing.T) {
			rows := readCLIDescribeRows(t, runCLIQuery(t, bin, input.path+` | transform age2 = age + 1, label = upper(name), class = if(age > 30, "senior", "standard"), age_or_zero = coalesce(age, 0) | filter { age2 > 20 and label is not null } | describe | json`))
			rowCount := rowCounts[input.name]
			requireCLIDescribeSchema(t, rows, "age2", "int", "int", rowCount)
			requireCLIDescribeSchema(t, rows, "label", "string", "string", rowCount)
			requireCLIDescribeSchema(t, rows, "class", "string", "string", rowCount)
			requireCLIDescribeSchema(t, rows, "age_or_zero", "int", "int", rowCount)
		})
	}
}

func TestCLIExpressionNumericPromotionRuntimeMatchesPlanner(t *testing.T) {
	bin := buildCLI(t)

	t.Run("int_column_equals_float_literal", func(t *testing.T) {
		out := runCLIQuery(t, bin, `../../testdata/users.csv | filter { age == 30.0 } | count | json`)
		requireCLIExpressionCount(t, out, 1)
	})

	t.Run("list_contains_int_list_float_needle", func(t *testing.T) {
		out := runCLIQuery(t, bin, `../../testdata/users.csv | filter { list_contains(list(1), 1.0) } | count | json`)
		requireCLIExpressionCount(t, out, 6)
	})

	t.Run("large_int_float_comparisons_are_exact", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "ids.csv")
		if err := os.WriteFile(path, []byte("id\n9007199254740993\n"), 0o644); err != nil {
			t.Fatalf("write large int fixture: %v", err)
		}

		cases := []struct {
			name  string
			query string
			want  int64
		}{
			{
				name:  "equality_distinguishes_large_int_from_rounded_float",
				query: path + ` | filter { id == 9007199254740992.0 } | count | json`,
				want:  0,
			},
			{
				name:  "not_equal_distinguishes_large_int_from_rounded_float",
				query: path + ` | filter { id != 9007199254740992.0 } | count | json`,
				want:  1,
			},
			{
				name:  "ordering_keeps_large_int_above_rounded_float",
				query: path + ` | filter { id > 9007199254740992.0 } | count | json`,
				want:  1,
			},
			{
				name:  "reversed_ordering_keeps_rounded_float_below_large_int",
				query: path + ` | filter { 9007199254740992.0 < id } | count | json`,
				want:  1,
			},
			{
				name:  "list_contains_distinguishes_large_int_from_rounded_float",
				query: path + ` | filter { list_contains(list(9007199254740993), 9007199254740992.0) } | count | json`,
				want:  0,
			},
			{
				name:  "record_equality_distinguishes_large_int_from_rounded_float",
				query: path + ` | filter { struct(x = id) == struct(x = 9007199254740992.0) } | count | json`,
				want:  0,
			},
			{
				name:  "nested_list_record_equality_distinguishes_large_int_from_rounded_float",
				query: path + ` | filter { list(struct(x = id)) == list(struct(x = 9007199254740992.0)) } | count | json`,
				want:  0,
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				requireCLIExpressionCount(t, runCLIQuery(t, bin, tc.query), tc.want)
			})
		}
	})

	t.Run("planned_promoted_expression_values_match_staged_transform", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "ids.csv")
		if err := os.WriteFile(path, []byte("id\n9007199254740993\n"), 0o644); err != nil {
			t.Fatalf("write large int fixture: %v", err)
		}

		cases := []struct {
			name  string
			query string
			want  int64
		}{
			{
				name:  "direct_coalesce_promotes_to_planned_float",
				query: path + ` | filter { coalesce(id, 0.0) == 9007199254740992.0 } | count | json`,
				want:  1,
			},
			{
				name:  "staged_coalesce_promotes_to_planned_float",
				query: path + ` | transform y = coalesce(id, 0.0) | filter { y == 9007199254740992.0 } | count | json`,
				want:  1,
			},
			{
				name:  "direct_if_promotes_to_planned_float",
				query: path + ` | filter { if(true, id, 0.0) == 9007199254740992.0 } | count | json`,
				want:  1,
			},
			{
				name:  "staged_if_promotes_to_planned_float",
				query: path + ` | transform y = if(true, id, 0.0) | filter { y == 9007199254740992.0 } | count | json`,
				want:  1,
			},
			{
				name:  "direct_list_literal_elements_promote_to_planned_float",
				query: path + ` | filter { list_contains(list(id, 0.0), 9007199254740992.0) } | count | json`,
				want:  1,
			},
			{
				name:  "staged_list_literal_elements_promote_to_planned_float",
				query: path + ` | transform xs = list(id, 0.0) | filter { list_contains(xs, 9007199254740992.0) } | count | json`,
				want:  1,
			},
			{
				name:  "direct_nested_list_record_promotes_to_planned_float",
				query: path + ` | filter { list_contains(list(struct(x = id), struct(x = 0.0)), struct(x = 9007199254740992.0)) } | count | json`,
				want:  1,
			},
			{
				name:  "staged_nested_list_record_promotes_to_planned_float",
				query: path + ` | transform xs = list(struct(x = id), struct(x = 0.0)) | filter { list_contains(xs, struct(x = 9007199254740992.0)) } | count | json`,
				want:  1,
			},
			{
				name:  "direct_division_produces_planned_float",
				query: path + ` | filter { (id / 1) == 9007199254740992.0 } | count | json`,
				want:  1,
			},
			{
				name:  "staged_division_produces_planned_float",
				query: path + ` | transform y = id / 1 | filter { y == 9007199254740992.0 } | count | json`,
				want:  1,
			},
			{
				name:  "direct_division_list_literal_produces_planned_float",
				query: path + ` | filter { list_contains(list(id / 1), 9007199254740992.0) } | count | json`,
				want:  1,
			},
			{
				name:  "direct_division_record_field_produces_planned_float",
				query: path + ` | filter { struct(y = id / 1) == struct(y = 9007199254740992.0) } | count | json`,
				want:  1,
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				requireCLIExpressionCount(t, runCLIQuery(t, bin, tc.query), tc.want)
			})
		}

		boolCases := []struct {
			name  string
			query string
			want  bool
		}{
			{
				name:  "direct_division_inside_transform_parent_expression",
				query: path + ` | transform ok = (id / 1) == 9007199254740992.0 | select ok | json`,
				want:  true,
			},
			{
				name:  "staged_division_inside_transform_parent_expression",
				query: path + ` | transform y = id / 1 | transform ok = y == 9007199254740992.0 | select ok | json`,
				want:  true,
			},
		}
		for _, tc := range boolCases {
			t.Run(tc.name, func(t *testing.T) {
				requireCLIExpressionBool(t, runCLIQuery(t, bin, tc.query), "ok", tc.want)
			})
		}
	})
}

func TestCLIExpressionNullableComparisonsUseThreeValuedLogic(t *testing.T) {
	bin := buildCLI(t)
	path := filepath.Join(t.TempDir(), "nullcmp.json")
	if err := os.WriteFile(path, []byte(`[{"a":null},{"a":1},{"a":2}]`), 0o644); err != nil {
		t.Fatalf("write nullable comparison fixture: %v", err)
	}

	t.Run("transform_outputs_unknown_for_all_null_comparisons", func(t *testing.T) {
		out := runCLIQuery(t, bin, path+` | transform eq = a == 1, ne = a != 1, lt = a < 2, gt = a > 1 | json`)
		var rows []map[string]any
		if err := json.Unmarshal(out, &rows); err != nil {
			t.Fatalf("invalid JSON output:\n%s", out)
		}
		if len(rows) != 3 {
			t.Fatalf("got %d rows, want 3:\n%s", len(rows), out)
		}
		for _, key := range []string{"eq", "ne", "lt", "gt"} {
			if rows[0][key] != nil {
				t.Fatalf("row 0 %s: got %#v, want null in %#v", key, rows[0][key], rows[0])
			}
		}
		want := []map[string]bool{
			{"eq": true, "ne": false, "lt": true, "gt": false},
			{"eq": false, "ne": true, "lt": false, "gt": true},
		}
		for i, expected := range want {
			row := rows[i+1]
			for key, value := range expected {
				got, ok := row[key].(bool)
				if !ok || got != value {
					t.Fatalf("row %d %s: got %#v, want %v in %#v", i+1, key, row[key], value, row)
				}
			}
		}
	})

	t.Run("filter_drops_unknown_comparison_results", func(t *testing.T) {
		out := runCLIQuery(t, bin, path+` | filter { a != 1 } | json`)
		var rows []map[string]any
		if err := json.Unmarshal(out, &rows); err != nil {
			t.Fatalf("invalid JSON output:\n%s", out)
		}
		if len(rows) != 1 || rows[0]["a"] != float64(2) {
			t.Fatalf("a != 1 should keep only a=2, got %#v", rows)
		}
	})

	t.Run("explicit_null_check_keeps_null_rows", func(t *testing.T) {
		out := runCLIQuery(t, bin, path+` | filter { a != 1 or a is null } | json`)
		var rows []map[string]any
		if err := json.Unmarshal(out, &rows); err != nil {
			t.Fatalf("invalid JSON output:\n%s", out)
		}
		if len(rows) != 2 || rows[0]["a"] != nil || rows[1]["a"] != float64(2) {
			t.Fatalf("explicit is null should keep null and a=2 rows, got %#v", rows)
		}
	})
}

func TestCLIExpressionIntegerArithmeticPreservesExactRuntimeValues(t *testing.T) {
	bin := buildCLI(t)
	path := filepath.Join(t.TempDir(), "ids.csv")
	if err := os.WriteFile(path, []byte("id\n9007199254740993\n"), 0o644); err != nil {
		t.Fatalf("write large int fixture: %v", err)
	}

	out := runCLIQuery(t, bin, path+` | transform add = id + 0, sub = id - 0, mul = id * 1 | select id, add, sub, mul | json`)
	for _, col := range []string{"id", "add", "sub", "mul"} {
		requireCLIExactJSONInt(t, out, col, "9007199254740993")
	}

	for _, query := range []string{
		path + ` | filter { id + 0 == 9007199254740993 } | count | json`,
		path + ` | filter { id - 0 == 9007199254740993 } | count | json`,
		path + ` | filter { id * 1 == 9007199254740993 } | count | json`,
	} {
		t.Run(query, func(t *testing.T) {
			requireCLIExpressionCount(t, runCLIQuery(t, bin, query), 1)
		})
	}
}

func TestCLIExpressionIntegerArithmeticOverflowErrors(t *testing.T) {
	bin := buildCLI(t)
	path := filepath.Join(t.TempDir(), "ids.csv")
	if err := os.WriteFile(path, []byte("id\n9223372036854775807\n-9223372036854775808\n4611686018427387904\n"), 0o644); err != nil {
		t.Fatalf("write overflow fixture: %v", err)
	}

	cases := []struct {
		name  string
		query string
	}{
		{name: "add", query: path + ` | filter { id == 9223372036854775807 } | transform y = id + 1 | json`},
		{name: "subtract", query: path + ` | filter { id == -9223372036854775808 } | transform y = id - 1 | json`},
		{name: "multiply", query: path + ` | filter { id == 4611686018427387904 } | transform y = id * 2 | json`},
		{name: "unary_negate", query: path + ` | filter { id == -9223372036854775808 } | transform y = -id | json`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := runCLIQueryExpectError(t, bin, tc.query)
			assertCLIExpressionErrorContains(t, out, "integer overflow")
		})
	}
}

func TestCLIRecordEqualityMissingFieldsRuntimeMatchesPlannerAcrossFlatFormats(t *testing.T) {
	bin := buildCLI(t)
	rowCounts := map[string]int64{
		"csv":     6,
		"json":    3,
		"jsonl":   3,
		"avro":    6,
		"parquet": 6,
	}

	trueCases := []struct {
		name  string
		stage string
	}{
		{
			name:  "inline_if_record_matches_larger_shape",
			stage: `filter { if(age == 30, struct(x = 1), struct(x = 1, y = null)) == struct(x = 1, y = null) }`,
		},
		{
			name:  "staged_if_record_matches_larger_shape",
			stage: `transform r = if(age == 30, struct(x = 1), struct(x = 1, y = null)) | filter { r == struct(x = 1, y = null) }`,
		},
		{
			name:  "staged_if_record_matches_smaller_shape",
			stage: `transform r = if(age == 30, struct(x = 1), struct(x = 1, y = null)) | filter { r == struct(x = 1) }`,
		},
		{
			name:  "list_contains_record_missing_null_field",
			stage: `filter { list_contains(list(struct(x = 1, y = null)), struct(x = 1)) }`,
		},
		{
			name:  "coalesce_record_missing_null_field",
			stage: `filter { coalesce(null, struct(x = 1), struct(x = 1, y = null)) == struct(x = 1, y = null) }`,
		},
		{
			name:  "nested_record_missing_null_field",
			stage: `filter { struct(n = struct(x = 1)) == struct(n = struct(x = 1, y = null)) }`,
		},
		{
			name:  "list_equality_record_missing_null_field",
			stage: `filter { list(struct(x = 1)) == list(struct(x = 1, y = null)) }`,
		},
	}

	falseCases := []struct {
		name  string
		stage string
	}{
		{
			name:  "missing_non_null_record_field",
			stage: `filter { struct(x = 1) == struct(x = 1, y = 2) }`,
		},
		{
			name:  "list_contains_missing_non_null_record_field",
			stage: `filter { list_contains(list(struct(x = 1)), struct(x = 1, y = 2)) }`,
		},
	}

	for _, input := range cliFlatUserInputFiles() {
		t.Run(input.name, func(t *testing.T) {
			for _, tc := range trueCases {
				t.Run(tc.name, func(t *testing.T) {
					out := runCLIQuery(t, bin, input.path+` | `+tc.stage+` | count | json`)
					requireCLIExpressionCount(t, out, rowCounts[input.name])
				})
			}
			for _, tc := range falseCases {
				t.Run(tc.name, func(t *testing.T) {
					out := runCLIQuery(t, bin, input.path+` | `+tc.stage+` | count | json`)
					requireCLIExpressionCount(t, out, 0)
				})
			}
		})
	}
}

func TestCLIRecordEqualityRejectsIncompatibleFieldTypesBeforeRows(t *testing.T) {
	bin := buildCLI(t)

	cases := []struct {
		name  string
		query string
		wants []string
	}{
		{
			name:  "record_common_field_type_mismatch",
			query: `../../testdata/users.csv | filter { false } | filter { struct(x = 1) == struct(x = "1") } | count`,
			wants: []string{"compare", "record"},
		},
		{
			name:  "list_contains_record_common_field_type_mismatch",
			query: `../../testdata/users.csv | filter { false } | filter { list_contains(list(struct(x = 1)), struct(x = "1")) } | count`,
			wants: []string{"list_contains", "mismatch"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := runCLIQueryExpectError(t, bin, tc.query)
			assertCLIExpressionErrorContains(t, out, tc.wants...)
		})
	}
}

func TestCLIExpressionTypeCheckerConstructorsKeepStableSchemasAcrossFlatFormats(t *testing.T) {
	bin := buildCLI(t)

	for _, input := range cliFlatUserInputFiles() {
		t.Run(input.name, func(t *testing.T) {
			rows := readCLIDescribeRows(t, runCLIQuery(t, bin, input.path+` | filter { false } | transform profile = struct(name = name, age = age), tags2 = list("user", city, null), mixed = list(1, "two") | describe | json`))
			requireCLIDescribeSchema(t, rows, "profile", "record", "record<age:int, name:string>", 0)
			requireCLIDescribeSchema(t, rows, "tags2", "list", "list<string?>", 0)
			requireCLIDescribeSchema(t, rows, "mixed", "list", "list<mixed>", 0)
		})
	}
}

func TestCLIExpressionPlanningErrorsDoNotWriteOutputFiles(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	outputs := []struct {
		format string
		ext    string
	}{
		{"table", ".txt"},
		{"csv", ".csv"},
		{"json", ".json"},
		{"jsonl", ".jsonl"},
		{"avro", ".avro"},
		{"parquet", ".parquet"},
	}

	for _, output := range outputs {
		t.Run(output.format, func(t *testing.T) {
			outPath := filepath.Join(dir, "bad-"+output.format+output.ext)
			out := runCLIQueryExpectError(t, bin, `../../testdata/users.csv | filter { false } | transform bad = upper(age) | `+output.format+` to `+outPath)
			assertCLIExpressionErrorContains(t, out, "upper", "string", "int")
			if _, err := os.Stat(outPath); !os.IsNotExist(err) {
				t.Fatalf("expression planning failure should not create %s, stat err=%v", outPath, err)
			}
		})
	}
}

func TestCLIExpressionPlanningCompressedTextInputs(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	jsonl := "{\"name\":\"Alice\",\"age\":30,\"city\":\"NY\"}\n{\"name\":\"Bob\",\"age\":25,\"city\":\"LA\"}\n"

	inputs := []struct {
		name string
		path string
		data []byte
	}{
		{
			name: "csv_gzip",
			path: filepath.Join(dir, "users.csv.gz"),
			data: gzipCLIBytes(t, "name,age,city\nAlice,30,NY\nBob,25,LA\n"),
		},
		{
			name: "jsonl_zstd",
			path: filepath.Join(dir, "users.jsonl.zst"),
			data: zstdCLIBytes(t, jsonl),
		},
		{
			name: "jsonl_deflate",
			path: filepath.Join(dir, "users.jsonl.deflate"),
			data: deflateCLIBytes(t, jsonl),
		},
	}

	for _, input := range inputs {
		if err := os.WriteFile(input.path, input.data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	for _, input := range inputs {
		t.Run(input.name+"_missing_column", func(t *testing.T) {
			out := runCLIQueryExpectError(t, bin, input.path+` | filter { false } | filter { agge > 20 } | count`)
			assertCLIExpressionErrorContains(t, out, "agge", "not found")
		})

		t.Run(input.name+"_type_error", func(t *testing.T) {
			out := runCLIQueryExpectError(t, bin, input.path+` | filter { false } | transform out = upper(age) | json`)
			assertCLIExpressionErrorContains(t, out, "upper", "string", "int")
		})

		t.Run(input.name+"_happy_path", func(t *testing.T) {
			out := runCLIQuery(t, bin, input.path+` | transform label = upper(name), age2 = age + 1 | filter { age2 > 26 } | select label, age2 | json`)
			var rows []map[string]any
			if err := json.Unmarshal(out, &rows); err != nil {
				t.Fatalf("invalid JSON output:\n%s", out)
			}
			if len(rows) != 1 || rows[0]["label"] != "ALICE" || rows[0]["age2"].(float64) != 31 {
				t.Fatalf("unexpected compressed happy-path rows:\n%s", out)
			}
		})
	}
}

func cliNestedUserInputFiles() []struct {
	name string
	path string
} {
	return []struct {
		name string
		path string
	}{
		{"json", "../../testdata/nested.json"},
		{"jsonl", "../../testdata/nested.jsonl"},
		{"avro", "../../testdata/nested.avro"},
		{"parquet", "../../testdata/nested.parquet"},
	}
}

func assertCLIExpressionErrorContains(t *testing.T, out []byte, wants ...string) {
	t.Helper()
	msg := strings.ToLower(string(out))
	for _, want := range wants {
		if !strings.Contains(msg, strings.ToLower(want)) {
			t.Fatalf("expected expression planning error to contain %q, got:\n%s", want, out)
		}
	}
}

func requireCLIExpressionCount(t *testing.T, out []byte, want int64) {
	t.Helper()
	var rows []struct {
		Count int64 `json:"count"`
	}
	if err := json.Unmarshal(out, &rows); err != nil {
		t.Fatalf("invalid JSON output:\n%s", out)
	}
	if len(rows) != 1 || rows[0].Count != want {
		t.Fatalf("got count rows %+v, want count %d", rows, want)
	}
}

func requireCLIExpressionBool(t *testing.T, out []byte, column string, want bool) {
	t.Helper()
	var rows []map[string]bool
	if err := json.Unmarshal(out, &rows); err != nil {
		t.Fatalf("invalid JSON output:\n%s", out)
	}
	if len(rows) != 1 || rows[0][column] != want {
		t.Fatalf("got bool rows %+v, want %s=%v", rows, column, want)
	}
}

func requireCLIExactJSONInt(t *testing.T, out []byte, column, want string) {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(out))
	dec.UseNumber()
	var rows []map[string]any
	if err := dec.Decode(&rows); err != nil {
		t.Fatalf("invalid JSON output:\n%s", out)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1:\n%s", len(rows), out)
	}
	gotValue := rows[0][column]
	got, ok := gotValue.(json.Number)
	if !ok {
		t.Fatalf("column %s: got %T %v, want JSON number %s", column, gotValue, gotValue, want)
	}
	if got.String() != want {
		t.Fatalf("column %s: got %s, want %s", column, got.String(), want)
	}
}
