package engine

import (
	"reflect"
	"strings"
	"testing"

	"github.com/razeghi71/dq/ast"
	"github.com/razeghi71/dq/table"
)

func TestBuiltinCatalogContainsEveryCurrentBuiltinWithExpectedCategory(t *testing.T) {
	want := map[string]builtinCategory{
		"upper":         builtinScalar,
		"lower":         builtinScalar,
		"trim":          builtinScalar,
		"str_len":       builtinScalar,
		"year":          builtinScalar,
		"month":         builtinScalar,
		"day":           builtinScalar,
		"substr":        builtinScalar,
		"str_contains":  builtinScalar,
		"starts_with":   builtinScalar,
		"ends_with":     builtinScalar,
		"matches":       builtinScalar,
		"list_len":      builtinScalar,
		"list_contains": builtinScalar,
		"coalesce":      builtinSpecialForm,
		"if":            builtinSpecialForm,
		"count":         builtinAggregate,
		"sum":           builtinAggregate,
		"avg":           builtinAggregate,
		"min":           builtinAggregate,
		"max":           builtinAggregate,
		"first":         builtinAggregate,
		"last":          builtinAggregate,
	}

	if len(builtinCatalog) != len(want) {
		t.Fatalf("catalog size: got %d, want %d; catalog=%v", len(builtinCatalog), len(want), builtinCatalog)
	}
	for name, category := range want {
		spec, ok := builtinCatalog[name]
		if !ok {
			t.Fatalf("catalog missing builtin %q", name)
		}
		if spec.Name != name {
			t.Fatalf("%s: spec.Name = %q, want %q", name, spec.Name, name)
		}
		if spec.Category != category {
			t.Fatalf("%s: category = %v, want %v", name, spec.Category, category)
		}
	}
}

func TestBuiltinCatalogCategoryHookInvariants(t *testing.T) {
	for name, spec := range builtinCatalog {
		t.Run(name, func(t *testing.T) {
			if spec.Name == "" {
				t.Fatal("Name must be set")
			}
			if spec.Check == nil {
				t.Fatal("Check must be set for every builtin")
			}

			switch spec.Category {
			case builtinScalar:
				if spec.TypedEval == nil {
					t.Fatal("scalar builtin must have TypedEval")
				}
				if spec.Aggregate != nil {
					t.Fatal("scalar builtin must not have Aggregate metadata")
				}
			case builtinSpecialForm:
				if spec.TypedEval == nil {
					t.Fatal("special form must have TypedEval")
				}
				if spec.Aggregate != nil {
					t.Fatal("special form must not have Aggregate metadata")
				}
			case builtinAggregate:
				if spec.Aggregate == nil {
					t.Fatal("aggregate builtin must have Aggregate metadata")
				}
				if spec.Aggregate.NewAccumulator == nil {
					t.Fatal("aggregate builtin must have accumulator hook")
				}
				if spec.TypedEval != nil {
					t.Fatal("aggregate builtin must not have TypedEval")
				}
			default:
				t.Fatalf("unknown category %v", spec.Category)
			}
		})
	}
}

func TestBuiltinCatalogAggregateAccumulatorsMatchFusedAndMaterializedReduce(t *testing.T) {
	input := table.NewTableWithSchemas(
		[]string{"city", "age", "amount", "name"},
		[]*table.TypeDescriptor{
			{Kind: table.TypeString},
			{Kind: table.TypeInt},
			{Kind: table.TypeFloat},
			{Kind: table.TypeString, Nullable: true},
		},
	)
	mustAddFusedGroupReduceTDDRow(t, input, table.StrVal("A"), table.IntVal(10), table.FloatVal(1.5), table.StrVal("ann"))
	mustAddFusedGroupReduceTDDRow(t, input, table.StrVal("A"), table.IntVal(20), table.FloatVal(2.5), table.Null())
	mustAddFusedGroupReduceTDDRow(t, input, table.StrVal("B"), table.IntVal(8), table.FloatVal(4), table.StrVal("bee"))

	assignments := map[string]string{
		"count": "v = count()",
		"sum":   "v = sum(age)",
		"avg":   "v = avg(amount)",
		"min":   "v = min(age)",
		"max":   "v = max(age)",
		"first": "v = first(name)",
		"last":  "v = last(name)",
	}
	for name, spec := range builtinCatalog {
		if spec.Category != builtinAggregate {
			continue
		}
		assignment, ok := assignments[name]
		if !ok {
			t.Fatalf("missing fused/materialized equivalence case for aggregate %q", name)
		}
		t.Run(name, func(t *testing.T) {
			fused := runQuery(t, input, `group city | reduce `+assignment+` | remove grouped | sort city`)
			materialized := runQuery(t, input, `group city | filter { city is not null } | reduce `+assignment+` | remove grouped | sort city`)
			requireBuiltinCatalogTDDTablesEqual(t, fused, materialized)
		})
	}
}

func requireBuiltinCatalogTDDTablesEqual(t *testing.T, left, right *table.Table) {
	t.Helper()
	if !reflect.DeepEqual(left.Columns, right.Columns) {
		t.Fatalf("columns: got %v, want %v", left.Columns, right.Columns)
	}
	if left.NumRows != right.NumRows {
		t.Fatalf("row count: got %d, want %d", left.NumRows, right.NumRows)
	}
	for row := 0; row < left.NumRows; row++ {
		for col := range left.Columns {
			lv := left.GetAt(row, col)
			rv := right.GetAt(row, col)
			if !reflect.DeepEqual(lv, rv) {
				t.Fatalf("row %d col %s: got %v, want %v", row, left.Columns[col], lv, rv)
			}
		}
	}
}

func TestBuiltinCatalogRejectsDriftBetweenPlanningAndScalarRuntime(t *testing.T) {
	stringArg := typedExpr{typ: &table.TypeDescriptor{Kind: table.TypeString}}
	intArg := typedExpr{typ: &table.TypeDescriptor{Kind: table.TypeInt}}
	listArg := typedExpr{typ: &table.TypeDescriptor{Kind: table.TypeList, Elem: &table.TypeDescriptor{Kind: table.TypeInt}}}

	cases := []struct {
		name      string
		args      []typedExpr
		values    []table.Value
		badArgs   []typedExpr
		badErrSub string
	}{
		{"upper", []typedExpr{stringArg}, []table.Value{table.StrVal("abc")}, []typedExpr{intArg}, "requires a string"},
		{"lower", []typedExpr{stringArg}, []table.Value{table.StrVal("ABC")}, []typedExpr{intArg}, "requires a string"},
		{"trim", []typedExpr{stringArg}, []table.Value{table.StrVal(" abc ")}, []typedExpr{intArg}, "requires a string"},
		{"str_len", []typedExpr{stringArg}, []table.Value{table.StrVal("abc")}, []typedExpr{intArg}, "requires a string"},
		{"year", []typedExpr{stringArg}, []table.Value{table.StrVal("2024-01-02")}, []typedExpr{intArg}, "requires a string"},
		{"month", []typedExpr{stringArg}, []table.Value{table.StrVal("2024-01-02")}, []typedExpr{intArg}, "requires a string"},
		{"day", []typedExpr{stringArg}, []table.Value{table.StrVal("2024-01-02")}, []typedExpr{intArg}, "requires a string"},
		{"substr", []typedExpr{stringArg, intArg, intArg}, []table.Value{table.StrVal("abc"), table.IntVal(0), table.IntVal(1)}, []typedExpr{intArg, intArg, intArg}, "requires a string"},
		{"str_contains", []typedExpr{stringArg, stringArg}, []table.Value{table.StrVal("abc"), table.StrVal("a")}, []typedExpr{intArg, stringArg}, "requires a string"},
		{"starts_with", []typedExpr{stringArg, stringArg}, []table.Value{table.StrVal("abc"), table.StrVal("a")}, []typedExpr{intArg, stringArg}, "requires a string"},
		{"ends_with", []typedExpr{stringArg, stringArg}, []table.Value{table.StrVal("abc"), table.StrVal("c")}, []typedExpr{intArg, stringArg}, "requires a string"},
		{"matches", []typedExpr{stringArg, stringArg}, []table.Value{table.StrVal("abc"), table.StrVal("^a")}, []typedExpr{intArg, stringArg}, "requires a string"},
		{"list_len", []typedExpr{listArg}, []table.Value{table.ListVal([]table.Value{table.IntVal(1)})}, []typedExpr{stringArg}, "requires a list"},
		{"list_contains", []typedExpr{listArg, intArg}, []table.Value{table.ListVal([]table.Value{table.IntVal(1)}), table.IntVal(1)}, []typedExpr{stringArg, intArg}, "requires a list"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec, ok := builtinCatalog[tc.name]
			if !ok {
				t.Fatalf("%s missing from catalog", tc.name)
			}
			if spec.Category != builtinScalar {
				t.Fatalf("%s category = %v, want scalar", tc.name, spec.Category)
			}
			if _, err := spec.Check(tc.args); err != nil {
				t.Fatalf("%s checker rejected valid args: %v", tc.name, err)
			}
			if _, err := spec.TypedEval(typedExprsForValues(tc.values), &EvalContext{}); err != nil {
				t.Fatalf("%s typed evaluator rejected valid args: %v", tc.name, err)
			}
			if _, err := spec.Check(tc.badArgs); err == nil || !strings.Contains(err.Error(), tc.badErrSub) {
				t.Fatalf("%s checker error = %v, want substring %q", tc.name, err, tc.badErrSub)
			}
		})
	}
}

func TestBuiltinCatalogSpecialFormsStayLazyAtRuntime(t *testing.T) {
	tbl := table.NewTable([]string{"age"})
	tbl.AddRow([]table.Value{table.IntVal(30)})

	cases := []struct {
		name  string
		query string
	}{
		{
			name:  "if_skips_runtime_invalid_then_branch",
			query: `transform x = if(false, year("not-a-date"), 7) | select x`,
		},
		{
			name:  "if_skips_runtime_invalid_else_branch",
			query: `transform x = if(true, 7, year("not-a-date")) | select x`,
		},
		{
			name:  "coalesce_stops_before_runtime_invalid_later_branch",
			query: `transform x = coalesce(7, year("not-a-date")) | select x`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := runQuery(t, tbl, tc.query)
			if got := result.GetAt(0, 0); got.Type != table.TypeInt || got.Int != 7 {
				t.Fatalf("got %s (%v), want int 7", got.AsString(), got.Type)
			}
		})
	}
}

func TestBuiltinCatalogSpecialFormsStillTypeCheckUnselectedBranches(t *testing.T) {
	cases := []struct {
		name  string
		query string
		want  string
	}{
		{
			name:  "if_incompatible_branches_fail_after_zero_rows",
			query: `filter { false } | transform bad = if(true, 1, "x") | describe`,
			want:  "branches do not have one common type",
		},
		{
			name:  "coalesce_incompatible_args_fail_after_zero_rows",
			query: `filter { false } | transform bad = coalesce(age, "missing") | describe`,
			want:  "do not have one common type",
		},
		{
			name:  "scalar_type_error_fails_after_zero_rows",
			query: `filter { false } | transform bad = upper(age) | describe`,
			want:  "upper() requires a string",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expectQueryErrContains(t, usersTable(), tc.query, tc.want)
		})
	}
}

func TestBuiltinCatalogFunctionCategoriesRemainStageScoped(t *testing.T) {
	cases := []struct {
		name  string
		query string
		want  string
	}{
		{
			name:  "aggregate_rejected_in_transform",
			query: `transform bad = count()`,
			want:  `aggregate function "count" can only be used inside 'reduce'`,
		},
		{
			name:  "scalar_rejected_in_reduce",
			query: `group city | reduce bad = upper(name)`,
			want:  `non-aggregate function "upper" in reduce context`,
		},
		{
			name:  "special_form_rejected_in_reduce",
			query: `group city | reduce bad = coalesce(first(name), "unknown")`,
			want:  `non-aggregate function "coalesce" in reduce context`,
		},
		{
			name:  "unknown_function_unchanged",
			query: `transform bad = slugify(name)`,
			want:  `unknown function "slugify"`,
		},
		{
			name:  "function_names_remain_case_sensitive",
			query: `transform bad = Upper(name)`,
			want:  `unknown function "Upper"`,
		},
		{
			name:  "list_constructor_name_remains_case_sensitive",
			query: `transform bad = List(1)`,
			want:  `unknown function "List"`,
		},
		{
			name:  "struct_constructor_name_remains_case_sensitive",
			query: `transform bad = STRUCT(1)`,
			want:  `unknown function "STRUCT"`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expectQueryErrContains(t, usersTable(), tc.query, tc.want)
		})
	}
}

func TestBuiltinCatalogCompiledFastPredicatesAreCatalogBacked(t *testing.T) {
	for _, name := range []string{"str_contains", "starts_with", "ends_with"} {
		t.Run(name, func(t *testing.T) {
			spec, ok := builtinCatalog[name]
			if !ok {
				t.Fatalf("%s missing from catalog", name)
			}
			if spec.Category != builtinScalar {
				t.Fatalf("%s category = %v, want scalar", name, spec.Category)
			}
			if spec.TypedEval == nil {
				t.Fatalf("%s must have typed runtime eval before a compiled fast path can optimize it", name)
			}

			tbl := compiledEquivalenceTable(t)
			typed, err := planPhysicalFilterExprForTest(schemaPlanCall(name, schemaPlanCol("name"), schemaPlanStringLit("a")), tbl)
			if err != nil {
				t.Fatalf("plan filter: %v", err)
			}
			fast, ok := compileFastPredicate(typed, tbl)
			if !ok {
				t.Fatal("expected compiled fast predicate")
			}
			for row := 0; row < tbl.NumRows; row++ {
				got, gotErr := fast(row)
				want, wantErr := genericPredicateResult(typed, tbl, row)
				assertSameErrorState(t, row, gotErr, wantErr)
				if gotErr == nil && got != want {
					t.Fatalf("row %d: compiled got %v, generic got %v", row, got, want)
				}
			}
		})
	}
}

func TestBuiltinCatalogArityAndUnknownErrorsStayStable(t *testing.T) {
	cases := []struct {
		name  string
		query string
		want  string
	}{
		{"upper_no_args", `transform bad = upper()`, "upper() takes 1 argument"},
		{"substr_one_arg", `transform bad = substr(name)`, "substr() takes 3 arguments"},
		{"list_contains_one_arg", `transform bad = list_contains(list(1))`, "list_contains() takes 2 arguments"},
		{"count_arg", `group city | reduce bad = count(age)`, "count() takes no arguments"},
		{"unknown", `transform bad = slugify(name)`, `unknown function "slugify"`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := runQueryExpectErr(t, usersTable(), tc.query)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestBuiltinCatalogRuntimeDispatchCategoryErrors(t *testing.T) {
	nested := table.NewTable([]string{"name"})
	if _, err := evalAggregateCall(&ast.FuncCallExpr{Name: "upper"}, nested); err == nil || !strings.Contains(err.Error(), `non-aggregate function "upper" in reduce context`) {
		t.Fatalf("non-aggregate reduce dispatch error = %v", err)
	}

	if err := validateAggregateFunctionArity(&ast.FuncCallExpr{Name: "sum"}); err == nil || !strings.Contains(err.Error(), "sum() takes 1 argument") {
		t.Fatalf("aggregate arity error = %v", err)
	}
}

func TestBuiltinCatalogTypedEvalDefensiveBranches(t *testing.T) {
	ctx := &EvalContext{}
	cases := []struct {
		name    string
		builtin string
		args    []typedExpr
		want    string
	}{
		{"missing_runtime_evaluator", "", nil, "missing runtime evaluator"},
		{"upper_arity", "upper", nil, "upper() takes 1 argument"},
		{"upper_bad_type", "upper", []typedExpr{typedIntLiteral(1)}, "upper() requires a string"},
		{"str_len_bad_type", "str_len", []typedExpr{typedIntLiteral(1)}, "str_len() requires a string"},
		{"year_bad_type", "year", []typedExpr{typedIntLiteral(1)}, "year() requires a string"},
		{"list_len_bad_type", "list_len", []typedExpr{typedStringLiteral("x")}, "list_len() requires a list"},
		{"substr_arity", "substr", []typedExpr{typedStringLiteral("abc")}, "substr() takes 3 arguments"},
		{"substr_bad_string", "substr", []typedExpr{typedIntLiteral(1), typedIntLiteral(0), typedIntLiteral(1)}, "substr() requires a string"},
		{"substr_bad_start", "substr", []typedExpr{typedStringLiteral("abc"), typedStringLiteral("0"), typedIntLiteral(1)}, "substr: start must be an int"},
		{"substr_bad_length", "substr", []typedExpr{typedStringLiteral("abc"), typedIntLiteral(0), typedStringLiteral("1")}, "substr: length must be an int"},
		{"str_contains_arity", "str_contains", []typedExpr{typedStringLiteral("abc")}, "str_contains() takes 2 arguments"},
		{"str_contains_bad_haystack", "str_contains", []typedExpr{typedIntLiteral(1), typedStringLiteral("a")}, "str_contains() requires a string"},
		{"str_contains_bad_needle", "str_contains", []typedExpr{typedStringLiteral("abc"), typedIntLiteral(1)}, "str_contains() requires a string substring"},
		{"list_contains_arity", "list_contains", []typedExpr{typedListLiteral(table.IntVal(1))}, "list_contains() takes 2 arguments"},
		{"list_contains_bad_list", "list_contains", []typedExpr{typedStringLiteral("abc"), typedStringLiteral("a")}, "list_contains() requires a list"},
		{"matches_bad_regex", "matches", []typedExpr{typedStringLiteral("abc"), typedStringLiteral("[")}, "matches(): invalid regex"},
		{"coalesce_arity", "coalesce", nil, "coalesce() requires at least 1 argument"},
		{"if_arity", "if", []typedExpr{typedBoolLiteral(true)}, "if() takes 3 arguments"},
		{"if_bad_condition", "if", []typedExpr{typedIntLiteral(1), typedIntLiteral(2), typedIntLiteral(3)}, "if: condition must be boolean"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var err error
			if tc.builtin == "" {
				_, err = evalTypedExpression(typedExpr{bound: &boundCall{raw: &ast.FuncCallExpr{Name: "missing_eval"}}}, ctx)
			} else {
				spec := builtinCatalog[tc.builtin]
				_, err = spec.TypedEval(tc.args, ctx)
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestBuiltinCatalogTypedEvalArgumentErrorsAndNulls(t *testing.T) {
	ctx := &EvalContext{}
	badColumn := typedExpr{bound: &boundColumn{rawPath: []string{"missing"}, topIndex: -1}}

	errorCases := []struct {
		name    string
		builtin string
		args    []typedExpr
		want    string
	}{
		{"upper_arg_error", "upper", []typedExpr{badColumn}, `column "missing" not found`},
		{"str_len_arg_error", "str_len", []typedExpr{badColumn}, `column "missing" not found`},
		{"year_arg_error", "year", []typedExpr{badColumn}, `column "missing" not found`},
		{"list_len_arg_error", "list_len", []typedExpr{badColumn}, `column "missing" not found`},
		{"substr_string_arg_error", "substr", []typedExpr{badColumn, typedIntLiteral(0), typedIntLiteral(1)}, `column "missing" not found`},
		{"substr_start_arg_error", "substr", []typedExpr{typedStringLiteral("abc"), badColumn, typedIntLiteral(1)}, `column "missing" not found`},
		{"substr_length_arg_error", "substr", []typedExpr{typedStringLiteral("abc"), typedIntLiteral(0), badColumn}, `column "missing" not found`},
		{"predicate_haystack_arg_error", "starts_with", []typedExpr{badColumn, typedStringLiteral("a")}, `column "missing" not found`},
		{"predicate_needle_arg_error", "starts_with", []typedExpr{typedStringLiteral("abc"), badColumn}, `column "missing" not found`},
		{"list_contains_list_arg_error", "list_contains", []typedExpr{badColumn, typedIntLiteral(1)}, `column "missing" not found`},
		{"list_contains_elem_arg_error", "list_contains", []typedExpr{typedListLiteral(table.IntVal(1)), badColumn}, `column "missing" not found`},
		{"coalesce_arg_error", "coalesce", []typedExpr{badColumn}, `column "missing" not found`},
		{"if_condition_arg_error", "if", []typedExpr{badColumn, typedIntLiteral(1), typedIntLiteral(2)}, `column "missing" not found`},
	}

	for _, tc := range errorCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := builtinCatalog[tc.builtin].TypedEval(tc.args, ctx)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want substring %q", err, tc.want)
			}
		})
	}

	nullCases := []struct {
		name    string
		builtin string
		args    []typedExpr
	}{
		{"upper_null", "upper", []typedExpr{typedNullLiteral()}},
		{"str_len_null", "str_len", []typedExpr{typedNullLiteral()}},
		{"year_null", "year", []typedExpr{typedNullLiteral()}},
		{"list_len_null", "list_len", []typedExpr{typedNullLiteral()}},
		{"substr_string_null", "substr", []typedExpr{typedNullLiteral(), typedIntLiteral(0), typedIntLiteral(1)}},
		{"substr_start_null", "substr", []typedExpr{typedStringLiteral("abc"), typedNullLiteral(), typedIntLiteral(1)}},
		{"substr_length_null", "substr", []typedExpr{typedStringLiteral("abc"), typedIntLiteral(0), typedNullLiteral()}},
		{"predicate_haystack_null", "ends_with", []typedExpr{typedNullLiteral(), typedStringLiteral("c")}},
		{"predicate_needle_null", "ends_with", []typedExpr{typedStringLiteral("abc"), typedNullLiteral()}},
		{"matches_null", "matches", []typedExpr{typedNullLiteral(), typedStringLiteral("a")}},
		{"list_contains_list_null", "list_contains", []typedExpr{typedNullLiteral(), typedIntLiteral(1)}},
		{"list_contains_elem_null", "list_contains", []typedExpr{typedListLiteral(table.IntVal(1)), typedNullLiteral()}},
		{"coalesce_all_null", "coalesce", []typedExpr{typedNullLiteral(), typedNullLiteral()}},
	}

	for _, tc := range nullCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := builtinCatalog[tc.builtin].TypedEval(tc.args, ctx)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !got.IsNull() {
				t.Fatalf("got %s, want null", got.AsString())
			}
		})
	}
}

func TestBuiltinCatalogTypedEvalCallAndContentBranches(t *testing.T) {
	ctx := &EvalContext{}

	call := typedExpr{
		bound:    &boundCall{raw: &ast.FuncCallExpr{Name: "upper"}},
		args:     []typedExpr{typedStringLiteral("abc")},
		callEval: builtinCatalog["upper"].TypedEval,
	}
	got, err := evalTypedExpression(call, ctx)
	if err != nil {
		t.Fatalf("typed call success: %v", err)
	}
	if got.Type != table.TypeString || got.Str != "ABC" {
		t.Fatalf("got %s, want ABC", got.AsString())
	}

	if _, err := evalTypedCall(typedExpr{}, ctx); err == nil || !strings.Contains(err.Error(), "missing runtime evaluator for function call") {
		t.Fatalf("generic missing evaluator error = %v", err)
	}

	if _, err := builtinCatalog["year"].TypedEval([]typedExpr{typedStringLiteral("not-a-date")}, ctx); err == nil || !strings.Contains(err.Error(), "cannot parse") {
		t.Fatalf("date content error = %v", err)
	}

	got, err = builtinCatalog["matches"].TypedEval([]typedExpr{typedStringLiteral("abc"), typedStringLiteral("^a")}, ctx)
	if err != nil {
		t.Fatalf("matches success: %v", err)
	}
	if got.Type != table.TypeBool || !got.Bool {
		t.Fatalf("got %s, want true", got.AsString())
	}
}

func typedExprsForValues(values []table.Value) []typedExpr {
	out := make([]typedExpr, len(values))
	for i, v := range values {
		out[i] = typedExprForValue(v)
	}
	return out
}

func typedExprForValue(v table.Value) typedExpr {
	switch v.Type {
	case table.TypeNull:
		return typedNullLiteral()
	case table.TypeInt:
		return typedIntLiteral(v.Int)
	case table.TypeFloat:
		return typedFloatLiteral(v.Float)
	case table.TypeString:
		return typedStringLiteral(v.Str)
	case table.TypeBool:
		return typedBoolLiteral(v.Bool)
	case table.TypeList:
		elements := typedExprsForValues(v.List)
		return typedExpr{bound: &boundList{raw: &ast.ListExpr{}, elements: boundExprsForTyped(elements)}, elements: elements, typ: &table.TypeDescriptor{Kind: table.TypeList}}
	default:
		return typedNullLiteral()
	}
}

func boundExprsForTyped(values []typedExpr) []boundExpr {
	out := make([]boundExpr, len(values))
	for i := range values {
		out[i] = values[i].bound
	}
	return out
}

func typedNullLiteral() typedExpr {
	raw := &ast.LiteralExpr{Kind: "null"}
	return typedExpr{bound: &boundLiteral{raw: raw}, raw: raw, typ: &table.TypeDescriptor{Kind: table.TypeNull, Nullable: true}}
}

func typedIntLiteral(v int64) typedExpr {
	raw := &ast.LiteralExpr{Kind: "int", Int: v}
	return typedExpr{bound: &boundLiteral{raw: raw}, raw: raw, typ: &table.TypeDescriptor{Kind: table.TypeInt}}
}

func typedFloatLiteral(v float64) typedExpr {
	raw := &ast.LiteralExpr{Kind: "float", Float: v}
	return typedExpr{bound: &boundLiteral{raw: raw}, raw: raw, typ: &table.TypeDescriptor{Kind: table.TypeFloat}}
}

func typedStringLiteral(v string) typedExpr {
	raw := &ast.LiteralExpr{Kind: "string", Str: v}
	return typedExpr{bound: &boundLiteral{raw: raw}, raw: raw, typ: &table.TypeDescriptor{Kind: table.TypeString}}
}

func typedBoolLiteral(v bool) typedExpr {
	raw := &ast.LiteralExpr{Kind: "bool", Bool: v}
	return typedExpr{bound: &boundLiteral{raw: raw}, raw: raw, typ: &table.TypeDescriptor{Kind: table.TypeBool}}
}

func typedListLiteral(values ...table.Value) typedExpr {
	return typedExprForValue(table.ListVal(values))
}
