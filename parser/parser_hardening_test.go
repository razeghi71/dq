package parser

import (
	"reflect"
	"strings"
	"testing"

	"github.com/razeghi71/dq/ast"
)

func requireParseErrorContains(t *testing.T, query string, wants ...string) {
	t.Helper()
	_, err := Parse(query)
	if err == nil {
		t.Fatalf("expected parse error for %q", query)
	}
	msg := err.Error()
	for _, want := range wants {
		if !strings.Contains(msg, want) {
			t.Fatalf("error %q does not contain %q for query %q", msg, want, query)
		}
	}
}

func requireParseOK(t *testing.T, query string) *ast.Query {
	t.Helper()
	q, err := Parse(query)
	if err != nil {
		t.Fatalf("expected valid query %q, got error: %v", query, err)
	}
	return q
}

func TestParseFailClosedOnLexerErrorsAfterExpressionPrefix(t *testing.T) {
	cases := []struct {
		name  string
		query string
		wants []string
	}{
		{
			name:  "transform_modulo_before_output",
			query: `users.csv | transform out = age % 2 | json`,
			wants: []string{"lex error", "%"},
		},
		{
			name:  "transform_at_before_output",
			query: `users.csv | transform out = age @ 2 | json`,
			wants: []string{"lex error", "@"},
		},
		{
			name:  "transform_modulo_before_eof",
			query: `users.csv | transform out = age % 2`,
			wants: []string{"lex error", "%"},
		},
		{
			name:  "reduce_modulo_before_output",
			query: `users.csv | group city | reduce total = age % 2 | json`,
			wants: []string{"lex error", "%"},
		},
		{
			name:  "function_arg_modulo_before_comma",
			query: `users.csv | transform out = if(age % 2, "odd", "even") | json`,
			wants: []string{"lex error", "%"},
		},
		{
			name:  "list_element_at_before_comma",
			query: `users.csv | transform out = list(age @ 2, city) | json`,
			wants: []string{"lex error", "@"},
		},
		{
			name:  "struct_field_modulo_before_comma",
			query: `users.csv | transform out = struct(bucket = age % 2, city = city) | json`,
			wants: []string{"lex error", "%"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			requireParseErrorContains(t, tc.query, tc.wants...)
		})
	}
}

func TestParseRejectsTrailingJunkAtExpressionBoundaries(t *testing.T) {
	cases := []struct {
		name  string
		query string
	}{
		{"transform_column_then_identifier", `users.csv | transform out = age unexpected | json`},
		{"transform_call_then_identifier", `users.csv | transform out = upper(name) trailing | json`},
		{"transform_parens_then_identifier", `users.csv | transform out = (age + 1) trailing | json`},
		{"transform_list_then_identifier", `users.csv | transform out = list(age, upper(name)) trailing | json`},
		{"transform_struct_then_identifier", `users.csv | transform out = struct(age = age) trailing | json`},
		{"filter_after_closing_brace", `users.csv | filter { age > 1 } trailing | count`},
		{"filter_inside_braces", `users.csv | filter { age > 1 trailing } | count`},
		{"reduce_call_then_identifier", `users.csv | group city | reduce total = sum(age) trailing | json`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			requireParseErrorContains(t, tc.query)
		})
	}
}

func TestParseAcceptsValidExpressionTerminators(t *testing.T) {
	cases := []struct {
		name  string
		query string
	}{
		{"transform_rhs_eof", `users.csv | transform out = age + 1`},
		{"transform_rhs_pipe_output", `users.csv | transform out = age + 1 | json`},
		{"transform_rhs_comma", `users.csv | transform a = age + 1, b = upper(name) | json`},
		{"filter_rbrace", `users.csv | filter { (age + 1) > 2 and name is not null } | count`},
		{"function_arg_comma_and_rparen", `users.csv | transform out = if(age > 30, upper(name), "young") | json`},
		{"list_element_comma_and_rparen", `users.csv | transform xs = list(age, age + 1, upper(name)) | json`},
		{"struct_field_comma_and_rparen", `users.csv | transform rec = struct(id = age, label = upper(name)) | json`},
		{"reduce_assignment_comma_and_pipe", `users.csv | group city | reduce total = sum(age), n = count() | json`},
		{"nested_expression_boundaries", `users.csv | transform out = list(if(age > 30, upper(name), lower(name)), struct(city = city)) | json`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			requireParseOK(t, tc.query)
		})
	}
}

func TestParseRejectsTrailingJunkAfterOperationArguments(t *testing.T) {
	cases := []string{
		`users.csv | head 1 2`,
		`users.csv | tail 1 extra`,
		`users.csv | count now`,
		`users.csv | describe stats`,
		`users.csv | select name age`,
		`users.csv | sort age -name`,
		`users.csv | group city as entries trailing`,
		`users.csv | distinct city age`,
		`users.csv | remove password ssn`,
		`users.csv | join orders.csv on id trailing`,
		`users.csv | json extra`,
	}

	for _, query := range cases {
		t.Run(query, func(t *testing.T) {
			requireParseErrorContains(t, query)
		})
	}
}

func TestParseLexerErrorsInDelimitedInputs(t *testing.T) {
	cases := []struct {
		name  string
		query string
		wants []string
	}{
		{
			name:  "unterminated_quoted_source_path",
			query: `"unterminated.csv | count`,
			wants: []string{"unterminated string"},
		},
		{
			name:  "unterminated_backtick_source_path",
			query: "`unterminated.csv | count",
			wants: []string{"unterminated backtick"},
		},
		{
			name:  "unterminated_join_path",
			query: `users.csv | join "orders.csv on id`,
			wants: []string{"unterminated string"},
		},
		{
			name:  "unterminated_output_path",
			query: `users.csv | csv to "out.csv`,
			wants: []string{"unterminated string"},
		},
		{
			name:  "unterminated_string_literal",
			query: `users.csv | filter { name == "Alice }`,
			wants: []string{"lex error", "unterminated string"},
		},
		{
			name:  "unterminated_backtick_column",
			query: "users.csv | select `first name",
			wants: []string{"lex error", "unterminated backtick"},
		},
		{
			name:  "unterminated_backtick_field",
			query: "users.csv | filter { address.`city == \"NY\" }",
			wants: []string{"lex error", "unterminated backtick"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			requireParseErrorContains(t, tc.query, tc.wants...)
		})
	}
}

func TestParseASTNodesExposeSourceSpans(t *testing.T) {
	t.Run("literal", func(t *testing.T) {
		query := `users.csv | filter { 123 }`
		q := requireParseOK(t, query)
		f := q.Ops[0].(*ast.FilterOp)
		requireNodeSpan(t, f.Expr, strings.Index(query, "123"), strings.Index(query, "123")+len("123"))
	})

	t.Run("column_dot_path", func(t *testing.T) {
		query := `users.csv | filter { address.city }`
		q := requireParseOK(t, query)
		f := q.Ops[0].(*ast.FilterOp)
		start := strings.Index(query, "address.city")
		requireNodeSpan(t, f.Expr, start, start+len("address.city"))
	})

	t.Run("binary_expression", func(t *testing.T) {
		query := `users.csv | filter { age + 1 }`
		q := requireParseOK(t, query)
		f := q.Ops[0].(*ast.FilterOp)
		start := strings.Index(query, "age + 1")
		requireNodeSpan(t, f.Expr, start, start+len("age + 1"))
	})

	t.Run("function_call", func(t *testing.T) {
		query := `users.csv | filter { upper(name) }`
		q := requireParseOK(t, query)
		f := q.Ops[0].(*ast.FilterOp)
		start := strings.Index(query, "upper(name)")
		requireNodeSpan(t, f.Expr, start, start+len("upper(name)"))
	})

	t.Run("operation_and_assignment", func(t *testing.T) {
		query := `users.csv | transform out = age + 1`
		q := requireParseOK(t, query)
		tr := q.Ops[0].(*ast.TransformOp)
		requireNodeSpan(t, tr, strings.Index(query, "transform"), len(query))
		requireNodeSpan(t, &tr.Assignments[0], strings.Index(query, "out"), len(query))
	})

	t.Run("unicode_byte_spans", func(t *testing.T) {
		query := `ü.csv | filter { naïve == "Zürich" }`
		q := requireParseOK(t, query)
		f := q.Ops[0].(*ast.FilterOp)
		bin := f.Expr.(*ast.BinaryExpr)
		start := strings.Index(query, "naïve")
		requireNodeSpan(t, bin, start, start+len(`naïve == "Zürich"`))
		requireNodeSpan(t, bin.Left, start, start+len("naïve"))
	})
}

func requireNodeSpan(t *testing.T, node any, wantStart, wantEnd int) {
	t.Helper()
	gotStart, gotEnd, ok := reflectedSpan(node)
	if !ok {
		t.Fatalf("%T does not expose a Span() method or Span field with Start/End ints", node)
	}
	if gotStart != wantStart || gotEnd != wantEnd {
		t.Fatalf("%T span: got [%d,%d), want [%d,%d)", node, gotStart, gotEnd, wantStart, wantEnd)
	}
}

func reflectedSpan(node any) (int, int, bool) {
	if node == nil {
		return 0, 0, false
	}

	v := reflect.ValueOf(node)
	if !v.IsValid() || (v.Kind() == reflect.Ptr && v.IsNil()) {
		return 0, 0, false
	}

	if m := v.MethodByName("Span"); m.IsValid() && m.Type().NumIn() == 0 && m.Type().NumOut() == 1 {
		if start, end, ok := spanValue(m.Call(nil)[0]); ok {
			return start, end, true
		}
	}

	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if v.IsValid() && v.Kind() == reflect.Struct {
		if f := v.FieldByName("Span"); f.IsValid() {
			return spanValue(f)
		}
	}

	return 0, 0, false
}

func spanValue(v reflect.Value) (int, int, bool) {
	if !v.IsValid() {
		return 0, 0, false
	}
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return 0, 0, false
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return 0, 0, false
	}

	start, ok := intField(v.FieldByName("Start"))
	if !ok {
		return 0, 0, false
	}
	end, ok := intField(v.FieldByName("End"))
	if !ok {
		return 0, 0, false
	}
	return start, end, true
}

func intField(v reflect.Value) (int, bool) {
	if !v.IsValid() {
		return 0, false
	}
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return int(v.Int()), true
	default:
		return 0, false
	}
}
