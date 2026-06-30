package engine

import (
	"strings"
	"testing"

	"github.com/razeghi71/dq/ast"
	"github.com/razeghi71/dq/parser"
	"github.com/razeghi71/dq/table"
)

func TestStreamingPlanRowWiseOpsShortCircuitBeforeLateRuntimeError(t *testing.T) {
	result := runStreamingPlanQuery(t, streamingPlanDatesTable(), `filter { id >= 1 } | select id, raw | transform y = year(raw), next_id = id + 1 | head 1 | rename next_id=next | remove raw | sort id`)

	requireStreamingPlanColumns(t, result, "id", "y", "next")
	if result.NumRows != 1 {
		t.Fatalf("row count: got %d, want 1", result.NumRows)
	}
	if got := result.GetAt(0, result.ColIndex("id")); got.Type != table.TypeInt || got.Int != 1 {
		t.Fatalf("id: got %v, want int 1", got)
	}
	if got := result.GetAt(0, result.ColIndex("y")); got.Type != table.TypeInt || got.Int != 2024 {
		t.Fatalf("year: got %v, want int 2024", got)
	}
	if got := result.GetAt(0, result.ColIndex("next")); got.Type != table.TypeInt || got.Int != 2 {
		t.Fatalf("next: got %v, want int 2", got)
	}
}

func TestStreamingPlanCountIsBlocking(t *testing.T) {
	_, err := runStreamingPlanQueryErr(streamingPlanDatesTable(), `transform y = year(raw) | filter { y > 0 } | count`)
	if err == nil {
		t.Fatal("expected count to read the late invalid date")
	}
	for _, want := range []string{"year", "not-a-date", "cannot parse"} {
		if !strings.Contains(strings.ToLower(err.Error()), want) {
			t.Fatalf("error %q missing %q", err, want)
		}
	}
}

func TestStreamingPlanFilterDropAvoidsDownstreamRuntimeWork(t *testing.T) {
	result := runStreamingPlanQuery(t, streamingPlanDatesTable(), `filter { false } | transform y = year(raw) | count`)

	requireStreamingPlanColumns(t, result, "count")
	if result.NumRows != 1 {
		t.Fatalf("row count: got %d, want 1", result.NumRows)
	}
	if got := result.GetAt(0, 0); got.Type != table.TypeInt || got.Int != 0 {
		t.Fatalf("count: got %v, want int 0", got)
	}
}

func TestStreamingPlanDescribeIsBoundedFold(t *testing.T) {
	result := runStreamingPlanQuery(t, streamingPlanDatesTable(), `filter { id > 1 } | select raw | describe`)

	requireStreamingPlanColumns(t, result, "column", "type", "row_count", "schema")
	if result.NumRows != 1 {
		t.Fatalf("describe row count: got %d, want 1", result.NumRows)
	}
	if got := result.GetAt(0, result.ColIndex("column")); got.Type != table.TypeString || got.Str != "raw" {
		t.Fatalf("describe column: got %v, want raw", got)
	}
	if got := result.GetAt(0, result.ColIndex("row_count")); got.Type != table.TypeInt || got.Int != 1 {
		t.Fatalf("describe row_count: got %v, want int 1", got)
	}
}

func TestStreamingPlanNestedSelectUsesBoundColumnRows(t *testing.T) {
	addressSchema := &table.TypeDescriptor{
		Kind: table.TypeRecord,
		Fields: []table.FieldDescriptor{
			{Name: "city", Type: &table.TypeDescriptor{Kind: table.TypeString}},
			{Name: "zip", Type: &table.TypeDescriptor{Kind: table.TypeString}},
		},
	}
	input := table.NewTableWithSchemas(
		[]string{"id", "address"},
		[]*table.TypeDescriptor{{Kind: table.TypeInt}, addressSchema},
	)
	mustAddStreamingPlanRow(input, []table.Value{
		table.IntVal(1),
		table.RecordVal([]table.RecordField{
			{Name: "city", Value: table.StrVal("NY")},
			{Name: "zip", Value: table.StrVal("10001")},
		}),
	})

	result := runStreamingPlanQuery(t, input, `select address.city`)
	requireStreamingPlanColumns(t, result, "address_city")
	if result.NumRows != 1 || result.GetAt(0, 0).Str != "NY" {
		t.Fatalf("nested select result: rows=%d value=%v, want NY", result.NumRows, result.GetAt(0, 0))
	}
}

func TestStreamingPlanFinalRowWiseStreamsExposeSchemas(t *testing.T) {
	filtered := runStreamingPlanQuery(t, streamingPlanDatesTable(), `filter { id > 0 }`)
	requireStreamingPlanColumns(t, filtered, "id", "raw", "label")
	if filtered.NumRows != 2 {
		t.Fatalf("filtered rows: got %d, want 2", filtered.NumRows)
	}

	renamed := runStreamingPlanQuery(t, streamingPlanDatesTable(), `rename label=name`)
	requireStreamingPlanColumns(t, renamed, "id", "raw", "name")

	removed := runStreamingPlanQuery(t, streamingPlanDatesTable(), `remove raw`)
	requireStreamingPlanColumns(t, removed, "id", "label")

	transformed := runStreamingPlanQuery(t, streamingPlanDatesTable(), `head 1 | transform next = id + 1`)
	requireStreamingPlanColumns(t, transformed, "id", "raw", "label", "next")
	if got := transformed.GetAt(0, transformed.ColIndex("next")); got.Type != table.TypeInt || got.Int != 2 {
		t.Fatalf("next: got %v, want int 2", got)
	}
}

func TestStreamingPlanHeadZeroAndNullablePredicate(t *testing.T) {
	headZero := runStreamingPlanQuery(t, streamingPlanDatesTable(), `head 0`)
	if headZero.NumRows != 0 {
		t.Fatalf("head 0 rows: got %d, want 0", headZero.NumRows)
	}

	input := table.NewTableWithSchemas(
		[]string{"flag"},
		[]*table.TypeDescriptor{{Kind: table.TypeBool, Nullable: true}},
	)
	mustAddStreamingPlanRow(input, []table.Value{table.Null()})
	mustAddStreamingPlanRow(input, []table.Value{table.BoolVal(true)})
	filtered := runStreamingPlanQuery(t, input, `filter { flag } | count`)
	if got := filtered.GetAt(0, 0); got.Type != table.TypeInt || got.Int != 1 {
		t.Fatalf("nullable predicate count: got %v, want int 1", got)
	}
}

func TestStreamingPlanPureHelperBranches(t *testing.T) {
	if got := streamingSchemaKind(nil); got != table.TypeNull {
		t.Fatalf("nil streaming schema kind: got %v, want null", got)
	}
	_, err := resolveBoundColumnRow(boundColumn{topIndex: 10}, []table.Value{table.IntVal(1)})
	if err == nil || !strings.Contains(err.Error(), "out of range") {
		t.Fatalf("out-of-range bound row error: got %v", err)
	}
}

func TestStreamingPlanTypedExprHasCallBranches(t *testing.T) {
	call := typedExpr{bound: &boundCall{}}
	literal := typedExpr{bound: &boundLiteral{}}
	cases := []struct {
		name string
		expr typedExpr
		want bool
	}{
		{name: "direct", expr: call, want: true},
		{name: "left", expr: typedExpr{left: &call}, want: true},
		{name: "right", expr: typedExpr{left: &literal, right: &call}, want: true},
		{name: "operand", expr: typedExpr{operand: &call}, want: true},
		{name: "args", expr: typedExpr{args: []typedExpr{literal, call}}, want: true},
		{name: "fields", expr: typedExpr{fields: []typedStructField{{expr: literal}, {expr: call}}}, want: true},
		{name: "elements", expr: typedExpr{elements: []typedExpr{literal, call}}, want: true},
		{name: "none", expr: typedExpr{left: &literal, args: []typedExpr{literal}}, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := typedExprHasCall(tc.expr); got != tc.want {
				t.Fatalf("typedExprHasCall: got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestStreamingPlanRejectsNonBooleanFilterAtRuntime(t *testing.T) {
	_, err := runStreamingPlanQueryErr(streamingPlanDatesTable(), `filter { id + 1 } | count`)
	if err == nil || !strings.Contains(err.Error(), "filter expression must return bool") {
		t.Fatalf("non-boolean filter error: got %v", err)
	}
}

func TestStreamingPlanEmptyPipelineReturnsInput(t *testing.T) {
	input := streamingPlanDatesTable()
	q, err := parser.Parse(`test.csv`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	result, err := ExecuteStreaming(q, input, nil)
	if err != nil {
		t.Fatalf("execute empty streaming query: %v", err)
	}
	if result != input {
		t.Fatal("empty streaming query should return input table")
	}
}

func TestStreamingPlanRowLocalSpanAfterBlockingOp(t *testing.T) {
	left := table.NewTableWithSchemas(
		[]string{"id", "amount"},
		[]*table.TypeDescriptor{{Kind: table.TypeInt}, {Kind: table.TypeInt}},
	)
	mustAddStreamingPlanRow(left, []table.Value{table.IntVal(1), table.IntVal(10)})
	mustAddStreamingPlanRow(left, []table.Value{table.IntVal(2), table.IntVal(20)})

	right := table.NewTableWithSchemas(
		[]string{"id", "tier"},
		[]*table.TypeDescriptor{{Kind: table.TypeInt}, {Kind: table.TypeString}},
	)
	mustAddStreamingPlanRow(right, []table.Value{table.IntVal(1), table.StrVal("free")})
	mustAddStreamingPlanRow(right, []table.Value{table.IntVal(2), table.StrVal("paid")})

	q, err := parser.Parse(`left.csv | transform gross = amount * 2 | join right.csv on id | select id, gross, tier | filter { tier != "free" } | transform label = upper(tier) | sort id`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	result, err := ExecuteStreaming(q, left, func(filename string, _ ast.LoadOptions) (*table.Table, error) {
		if filename != "right.csv" {
			t.Fatalf("unexpected join file %q", filename)
		}
		return right, nil
	})
	if err != nil {
		t.Fatalf("execute streaming query: %v", err)
	}

	requireStreamingPlanColumns(t, result, "id", "gross", "tier", "label")
	if result.NumRows != 1 {
		t.Fatalf("row count: got %d, want 1", result.NumRows)
	}
	if got := result.GetAt(0, result.ColIndex("id")); got.Type != table.TypeInt || got.Int != 2 {
		t.Fatalf("id: got %v, want int 2", got)
	}
	if got := result.GetAt(0, result.ColIndex("gross")); got.Type != table.TypeInt || got.Int != 40 {
		t.Fatalf("gross: got %v, want int 40", got)
	}
	if got := result.GetAt(0, result.ColIndex("label")); got.Type != table.TypeString || got.Str != "PAID" {
		t.Fatalf("label: got %v, want PAID", got)
	}
}

func runStreamingPlanQuery(t *testing.T, input *table.Table, pipeline string) *table.Table {
	t.Helper()
	result, err := runStreamingPlanQueryErr(input, pipeline)
	if err != nil {
		t.Fatalf("execute streaming query %q: %v", pipeline, err)
	}
	return result
}

func runStreamingPlanQueryErr(input *table.Table, pipeline string) (*table.Table, error) {
	q, err := parser.Parse("test.csv | " + pipeline)
	if err != nil {
		return nil, err
	}
	return ExecuteStreaming(q, input, nil)
}

func streamingPlanDatesTable() *table.Table {
	t := table.NewTableWithSchemas(
		[]string{"id", "raw", "label"},
		[]*table.TypeDescriptor{
			{Kind: table.TypeInt},
			{Kind: table.TypeString},
			{Kind: table.TypeString},
		},
	)
	mustAddStreamingPlanRow(t, []table.Value{table.IntVal(1), table.StrVal("2024-01-02"), table.StrVal("ok")})
	mustAddStreamingPlanRow(t, []table.Value{table.IntVal(2), table.StrVal("not-a-date"), table.StrVal("bad")})
	return t
}

func mustAddStreamingPlanRow(t *table.Table, row []table.Value) {
	if err := t.AddRowTyped(row); err != nil {
		panic(err)
	}
}

func requireStreamingPlanColumns(t *testing.T, tbl *table.Table, want ...string) {
	t.Helper()
	if len(tbl.Columns) != len(want) {
		t.Fatalf("columns: got %v, want %v", tbl.Columns, want)
	}
	for i, col := range want {
		if tbl.Columns[i] != col {
			t.Fatalf("columns: got %v, want %v", tbl.Columns, want)
		}
	}
}
