package engine

import (
	"fmt"
	"strings"
	"testing"

	"github.com/razeghi71/dq/ast"
	"github.com/razeghi71/dq/parser"
	"github.com/razeghi71/dq/rowstream"
	"github.com/razeghi71/dq/table"
)

func TestExecuteSourceStreamQueryPlansPushdownAndStreamsRows(t *testing.T) {
	q := parseSourceStreamPlanQuery(t, `input.csv | filter { id > 1 } | select name | head 1`)
	source := sourceStreamPlanInfo()
	var gotSpec SourceLoadSpec

	result, err := ExecuteSourceStreamQuery(q, source, func(filename string, opts ast.LoadOptions, spec SourceLoadSpec) (rowstream.Stream, error) {
		gotSpec = spec
		outputSchema := table.NewSchema(
			[]string{"name"},
			[]*table.TypeDescriptor{{Kind: table.TypeString}},
		)
		rows := []rowstream.Row{
			{table.StrVal("Alice"), table.IntVal(1)},
			{table.StrVal("Bob"), table.IntVal(2)},
		}
		return &sourceStreamPlanSpecStream{schema: outputSchema, rows: rows, predicate: spec.Predicate}, nil
	}, nil)
	if err != nil {
		t.Fatalf("execute source stream query: %v", err)
	}
	if result.NumRows != 1 || result.GetAt(0, 0).Str != "Bob" {
		t.Fatalf("result: rows=%d first=%v, want Bob", result.NumRows, result.GetAt(0, 0))
	}
	if strings.Join(gotSpec.OutputColumns.Names(), ",") != "name" {
		t.Fatalf("output columns: got %v, want [name]", gotSpec.OutputColumns.Names())
	}
	if strings.Join(gotSpec.ReadColumns.Names(), ",") != "name,id" {
		t.Fatalf("read columns: got %v, want [name id]", gotSpec.ReadColumns.Names())
	}
	if gotSpec.Predicate == nil {
		t.Fatal("expected pushed source predicate")
	}
}

func TestExecuteSourceQueryMaterializedSourceContracts(t *testing.T) {
	q := parseSourceStreamPlanQuery(t, `input.csv | head 1 | select name`)
	source := sourceStreamPlanInfo()

	result, err := ExecuteSourceQuery(q, source, func(filename string, opts ast.LoadOptions, spec SourceLoadSpec) (*table.Table, error) {
		return sourceStreamPlanTableForSpec(t, spec), nil
	}, nil)
	if err != nil {
		t.Fatalf("execute source query: %v", err)
	}
	if result.NumRows != 1 || result.GetAt(0, 0).Str != "Alice" {
		t.Fatalf("source query result: rows=%d first=%v, want Alice", result.NumRows, result.GetAt(0, 0))
	}

	if _, err := ExecuteSourceQuery(q, source, nil, nil); err == nil || !strings.Contains(err.Error(), "source loader not configured") {
		t.Fatalf("nil source loader error: got %v", err)
	}
	if _, err := ExecuteSourceQuery(q, source, func(filename string, opts ast.LoadOptions, spec SourceLoadSpec) (*table.Table, error) {
		return nil, fmt.Errorf("open failed")
	}, nil); err == nil || !strings.Contains(err.Error(), "load error: open failed") {
		t.Fatalf("source load error: got %v", err)
	}
	if _, err := ExecuteSourceQuery(q, source, func(filename string, opts ast.LoadOptions, spec SourceLoadSpec) (*table.Table, error) {
		return table.NewTable([]string{"wrong", "extra"}), nil
	}, nil); err == nil || !strings.Contains(err.Error(), "input schema column count mismatch") {
		t.Fatalf("source schema mismatch error: got %v", err)
	}
	if _, err := ExecuteSourceQuery(q, source, func(filename string, opts ast.LoadOptions, spec SourceLoadSpec) (*table.Table, error) {
		return table.NewTableWithSchemas(
			[]string{"wrong"},
			[]*table.TypeDescriptor{{Kind: table.TypeString}},
		), nil
	}, nil); err == nil || !strings.Contains(err.Error(), "column 0 mismatch") {
		t.Fatalf("source schema name mismatch error: got %v", err)
	}
	if _, err := ExecuteSourceQuery(q, source, func(filename string, opts ast.LoadOptions, spec SourceLoadSpec) (*table.Table, error) {
		return table.NewTableWithSchemas(
			[]string{"name"},
			[]*table.TypeDescriptor{{Kind: table.TypeInt}},
		), nil
	}, nil); err == nil || !strings.Contains(err.Error(), "schema for column") {
		t.Fatalf("source schema type mismatch error: got %v", err)
	}
}

func TestExecuteSourceStreamQueryWrapsLazySourceErrors(t *testing.T) {
	q := parseSourceStreamPlanQuery(t, `input.csv | count`)
	_, err := ExecuteSourceStreamQuery(q, sourceStreamPlanInfo(), func(filename string, opts ast.LoadOptions, spec SourceLoadSpec) (rowstream.Stream, error) {
		return &sourceStreamPlanErrorStream{
			schema: sourceStreamPlanSchemaForColumns(t, spec.OutputColumns.Names()),
			err:    fmt.Errorf("late row failed"),
		}, nil
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "load error: late row failed") {
		t.Fatalf("lazy source error: got %v", err)
	}
}

func TestExecuteSourceStreamQueryWrapsSourceLoaderSetupErrors(t *testing.T) {
	q := parseSourceStreamPlanQuery(t, `input.csv | head 1`)
	_, err := ExecuteSourceStreamQuery(q, sourceStreamPlanInfo(), func(filename string, opts ast.LoadOptions, spec SourceLoadSpec) (rowstream.Stream, error) {
		return nil, fmt.Errorf("open failed")
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "load error: open failed") {
		t.Fatalf("source loader setup error: got %v", err)
	}
}

func TestExecuteSourceStreamQueryValidatesStreamSchema(t *testing.T) {
	q := parseSourceStreamPlanQuery(t, `input.csv | head 1`)
	_, err := ExecuteSourceStreamQuery(q, sourceStreamPlanInfo(), func(filename string, opts ast.LoadOptions, spec SourceLoadSpec) (rowstream.Stream, error) {
		return rowstream.FromTable(table.NewTable([]string{"wrong"})), nil
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "input schema column count mismatch") {
		t.Fatalf("schema validation error: got %v", err)
	}
}

func TestExecuteSourceStreamQueryValidatesStreamSchemaNamesAndTypes(t *testing.T) {
	cases := []struct {
		name   string
		schema table.Schema
		want   string
	}{
		{
			name: "name",
			schema: table.NewSchema(
				[]string{"id", "wrong"},
				[]*table.TypeDescriptor{{Kind: table.TypeInt}, {Kind: table.TypeString}},
			),
			want: "column 1 mismatch",
		},
		{
			name: "type",
			schema: table.NewSchema(
				[]string{"id", "name"},
				[]*table.TypeDescriptor{{Kind: table.TypeString}, {Kind: table.TypeString}},
			),
			want: "schema for column",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q := parseSourceStreamPlanQuery(t, `input.csv | head 1`)
			_, err := ExecuteSourceStreamQuery(q, sourceStreamPlanInfo(), func(filename string, opts ast.LoadOptions, spec SourceLoadSpec) (rowstream.Stream, error) {
				return &sourceStreamPlanErrorStream{schema: tc.schema}, nil
			}, nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("schema validation error: got %v, want %q", err, tc.want)
			}
		})
	}
}

func TestExecuteSourceStreamQueryRejectsNilSourceLoader(t *testing.T) {
	q := parseSourceStreamPlanQuery(t, `input.csv | head 1`)
	_, err := ExecuteSourceStreamQuery(q, sourceStreamPlanInfo(), nil, nil)
	if err == nil || !strings.Contains(err.Error(), "source stream loader not configured") {
		t.Fatalf("nil source loader error: got %v", err)
	}
}

func TestExecuteSourceAdaptiveQueryLoadsMaterializedBeforeNonDroppingBlockingSpan(t *testing.T) {
	q := parseSourceStreamPlanQuery(t, `input.csv | transform label = upper(name) | sort label | head 1`)
	var loaded bool
	var streamed bool

	result, err := ExecuteSourceAdaptiveQuery(q, sourceStreamPlanInfo(), func(filename string, opts ast.LoadOptions, spec SourceLoadSpec) (rowstream.Stream, error) {
		streamed = true
		return nil, fmt.Errorf("stream should not be used")
	}, func(filename string, opts ast.LoadOptions, spec SourceLoadSpec) (*table.Table, error) {
		loaded = true
		return sourceStreamPlanTableForSpec(t, spec), nil
	}, nil)
	if err != nil {
		t.Fatalf("execute adaptive source query: %v", err)
	}
	if !loaded || streamed {
		t.Fatalf("adaptive source choice: loaded=%v streamed=%v, want materialized only", loaded, streamed)
	}
	if result.NumRows != 1 || result.GetAt(0, result.ColIndex("label")).Str != "ALICE" {
		t.Fatalf("adaptive result: rows=%d label=%v, want ALICE", result.NumRows, result.GetAt(0, result.ColIndex("label")))
	}
}

func TestExecuteSourceAdaptiveQueryStreamsDroppingSpanBeforeBlockingOp(t *testing.T) {
	q := parseSourceStreamPlanQuery(t, `input.csv | filter { upper(name) == "ALICE" } | sort name | count`)
	var loaded bool
	var streamed bool

	result, err := ExecuteSourceAdaptiveQuery(q, sourceStreamPlanInfo(), func(filename string, opts ast.LoadOptions, spec SourceLoadSpec) (rowstream.Stream, error) {
		streamed = true
		return rowstream.FromTable(sourceStreamPlanTableForSpec(t, spec)), nil
	}, func(filename string, opts ast.LoadOptions, spec SourceLoadSpec) (*table.Table, error) {
		loaded = true
		return nil, fmt.Errorf("materialized load should not be used")
	}, nil)
	if err != nil {
		t.Fatalf("execute adaptive source query: %v", err)
	}
	if loaded || !streamed {
		t.Fatalf("adaptive source choice: loaded=%v streamed=%v, want stream only", loaded, streamed)
	}
	if result.NumRows != 1 || result.GetAt(0, 0).Int != 1 {
		t.Fatalf("adaptive count: rows=%d count=%v, want 1", result.NumRows, result.GetAt(0, 0))
	}
}

func TestExecuteSourceAdaptiveQueryInstrumentationHeadStopsWithoutReadAhead(t *testing.T) {
	q := parseSourceStreamPlanQuery(t, `input.csv | head 2`)
	source := sourceStreamPlanInfo()
	source.DisablePushdown = true
	stream := newSourceStreamPlanInstrumentedStream(1, 2, 3, 4)
	var loaded bool

	result, err := ExecuteSourceAdaptiveQuery(q, source, func(filename string, opts ast.LoadOptions, spec SourceLoadSpec) (rowstream.Stream, error) {
		return stream, nil
	}, func(filename string, opts ast.LoadOptions, spec SourceLoadSpec) (*table.Table, error) {
		loaded = true
		return nil, fmt.Errorf("materialized load should not be used")
	}, nil)
	if err != nil {
		t.Fatalf("execute adaptive head query: %v", err)
	}
	if loaded {
		t.Fatal("head should use the source stream, not materialized source load")
	}
	if result.NumRows != 2 {
		t.Fatalf("head result rows: got %d, want 2", result.NumRows)
	}
	if stream.nextCalls != 2 || stream.rowsRead != 2 {
		t.Fatalf("source reads: nextCalls=%d rowsRead=%d, want exactly 2 rows and no EOF read", stream.nextCalls, stream.rowsRead)
	}
	if stream.closeCalls != 1 {
		t.Fatalf("source close calls: got %d, want 1", stream.closeCalls)
	}
}

func TestExecuteSourceAdaptiveQueryInstrumentationFilterHeadReadsThroughFirstKeptRow(t *testing.T) {
	q := parseSourceStreamPlanQuery(t, `input.csv | filter { id > 2 } | head 1`)
	source := sourceStreamPlanInfo()
	source.DisablePushdown = true
	stream := newSourceStreamPlanInstrumentedStream(1, 2, 3, 4)

	result, err := ExecuteSourceAdaptiveQuery(q, source, func(filename string, opts ast.LoadOptions, spec SourceLoadSpec) (rowstream.Stream, error) {
		return stream, nil
	}, func(filename string, opts ast.LoadOptions, spec SourceLoadSpec) (*table.Table, error) {
		return nil, fmt.Errorf("materialized load should not be used")
	}, nil)
	if err != nil {
		t.Fatalf("execute adaptive filter/head query: %v", err)
	}
	if result.NumRows != 1 || result.GetAt(0, result.ColIndex("id")).Int != 3 {
		t.Fatalf("filter/head result: rows=%d table=%s, want one row with id=3", result.NumRows, result.String())
	}
	if stream.nextCalls != 3 || stream.rowsRead != 3 {
		t.Fatalf("source reads: nextCalls=%d rowsRead=%d, want exactly through first kept row", stream.nextCalls, stream.rowsRead)
	}
	if stream.closeCalls != 1 {
		t.Fatalf("source close calls: got %d, want 1", stream.closeCalls)
	}
}

func TestExecuteSourceAdaptiveQueryInstrumentationCountScansToEOF(t *testing.T) {
	q := parseSourceStreamPlanQuery(t, `input.csv | count`)
	source := sourceStreamPlanInfo()
	source.DisablePushdown = true
	stream := newSourceStreamPlanInstrumentedStream(1, 2, 3, 4)

	result, err := ExecuteSourceAdaptiveQuery(q, source, func(filename string, opts ast.LoadOptions, spec SourceLoadSpec) (rowstream.Stream, error) {
		return stream, nil
	}, func(filename string, opts ast.LoadOptions, spec SourceLoadSpec) (*table.Table, error) {
		return nil, fmt.Errorf("materialized load should not be used")
	}, nil)
	if err != nil {
		t.Fatalf("execute adaptive count query: %v", err)
	}
	if result.NumRows != 1 || result.GetAt(0, 0).Int != 4 {
		t.Fatalf("count result: rows=%d table=%s, want count=4", result.NumRows, result.String())
	}
	if stream.nextCalls != 5 || stream.rowsRead != 4 {
		t.Fatalf("source reads: nextCalls=%d rowsRead=%d, want four rows plus EOF", stream.nextCalls, stream.rowsRead)
	}
	if stream.closeCalls != 1 {
		t.Fatalf("source close calls: got %d, want 1", stream.closeCalls)
	}
}

func TestExecuteSourceAdaptiveQueryInstrumentationFilteredBoundaryRetainsOnlyKeptRows(t *testing.T) {
	q := parseSourceStreamPlanQuery(t, `input.csv | filter { id == 2 } | sort id | count`)
	source := sourceStreamPlanInfo()
	source.DisablePushdown = true
	stream := newSourceStreamPlanInstrumentedStream(1, 2, 3, 4)

	result, err := ExecuteSourceAdaptiveQuery(q, source, func(filename string, opts ast.LoadOptions, spec SourceLoadSpec) (rowstream.Stream, error) {
		return stream, nil
	}, func(filename string, opts ast.LoadOptions, spec SourceLoadSpec) (*table.Table, error) {
		return nil, fmt.Errorf("materialized load should not be used")
	}, nil)
	if err != nil {
		t.Fatalf("execute adaptive filtered boundary query: %v", err)
	}
	if result.NumRows != 1 || result.GetAt(0, 0).Int != 1 {
		t.Fatalf("filtered boundary count: rows=%d table=%s, want count=1", result.NumRows, result.String())
	}
	if stream.nextCalls != 5 || stream.rowsRead != 4 {
		t.Fatalf("source reads: nextCalls=%d rowsRead=%d, want full scan before blocking sort", stream.nextCalls, stream.rowsRead)
	}
	if stream.closeCalls != 1 {
		t.Fatalf("source close calls: got %d, want 1", stream.closeCalls)
	}
}

func TestShouldLoadSourceMaterializedForStreamingBranches(t *testing.T) {
	schema := sourceStreamPlanInfo().Schema
	rowSpan := plannedTransform{plannedBase: plannedBase{output: schema}}
	droppingSpan := plannedFilter{plannedBase: plannedBase{output: schema}}
	cases := []struct {
		name string
		ops  []plannedOp
		want bool
	}{
		{name: "empty", ops: nil, want: false},
		{name: "head", ops: []plannedOp{plannedHead{plannedBase: plannedBase{output: schema}, n: 1}}, want: false},
		{name: "count", ops: []plannedOp{plannedCount{plannedBase: plannedBase{output: schema}}}, want: false},
		{name: "describe", ops: []plannedOp{plannedDescribe{plannedBase: plannedBase{output: schema}}}, want: false},
		{name: "tail", ops: []plannedOp{plannedTail{plannedBase: plannedBase{output: schema}, n: 1}}, want: true},
		{name: "join", ops: []plannedOp{plannedJoin{plannedBase: plannedBase{output: schema}}}, want: true},
		{name: "row_span_final", ops: []plannedOp{rowSpan}, want: false},
		{name: "row_span_before_head", ops: []plannedOp{rowSpan, plannedHead{plannedBase: plannedBase{output: schema}, n: 1}}, want: false},
		{name: "row_span_before_count", ops: []plannedOp{rowSpan, plannedCount{plannedBase: plannedBase{output: schema}}}, want: false},
		{name: "row_span_before_describe", ops: []plannedOp{rowSpan, plannedDescribe{plannedBase: plannedBase{output: schema}}}, want: false},
		{name: "row_span_before_join", ops: []plannedOp{rowSpan, plannedJoin{plannedBase: plannedBase{output: schema}}}, want: true},
		{name: "dropping_span_before_join", ops: []plannedOp{droppingSpan, plannedJoin{plannedBase: plannedBase{output: schema}}}, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldLoadSourceMaterializedForStreaming(tc.ops); got != tc.want {
				t.Fatalf("shouldLoadSourceMaterializedForStreaming: got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestValidateSourceStreamSchemaNilStreamBranches(t *testing.T) {
	if err := validateSourceStreamSchema(table.NewSchema(nil, nil), nil); err != nil {
		t.Fatalf("nil stream with empty schema: %v", err)
	}
	err := validateSourceStreamSchema(sourceStreamPlanInfo().Schema, nil)
	if err == nil || !strings.Contains(err.Error(), "got 0") {
		t.Fatalf("nil stream with non-empty schema error: got %v", err)
	}
}

type sourceStreamPlanSpecStream struct {
	schema    table.Schema
	rows      []rowstream.Row
	at        int
	predicate SourcePredicate
	closed    bool
}

func (s *sourceStreamPlanSpecStream) Schema() table.Schema {
	return s.schema
}

func (s *sourceStreamPlanSpecStream) Next() (rowstream.Row, bool, error) {
	for s.at < len(s.rows) {
		row := s.rows[s.at]
		s.at++
		if s.predicate != nil {
			keep, err := s.predicate(row)
			if err != nil {
				return nil, false, err
			}
			if !keep {
				continue
			}
		}
		return rowstream.Row{row[0]}, true, nil
	}
	return nil, false, nil
}

func (s *sourceStreamPlanSpecStream) Close() error {
	s.closed = true
	return nil
}

type sourceStreamPlanErrorStream struct {
	schema table.Schema
	err    error
}

func (s *sourceStreamPlanErrorStream) Schema() table.Schema {
	return s.schema
}

func (s *sourceStreamPlanErrorStream) Next() (rowstream.Row, bool, error) {
	return nil, false, s.err
}

func (s *sourceStreamPlanErrorStream) Close() error {
	return nil
}

type sourceStreamPlanInstrumentedStream struct {
	schema     table.Schema
	rows       []rowstream.Row
	at         int
	nextCalls  int
	rowsRead   int
	closeCalls int
}

func newSourceStreamPlanInstrumentedStream(ids ...int64) *sourceStreamPlanInstrumentedStream {
	rows := make([]rowstream.Row, len(ids))
	for i, id := range ids {
		rows[i] = rowstream.Row{table.IntVal(id), table.StrVal(fmt.Sprintf("name-%d", id))}
	}
	return &sourceStreamPlanInstrumentedStream{
		schema: sourceStreamPlanInfo().Schema,
		rows:   rows,
	}
}

func (s *sourceStreamPlanInstrumentedStream) Schema() table.Schema {
	return s.schema
}

func (s *sourceStreamPlanInstrumentedStream) Next() (rowstream.Row, bool, error) {
	s.nextCalls++
	if s.at >= len(s.rows) {
		return nil, false, nil
	}
	row := s.rows[s.at]
	s.at++
	s.rowsRead++
	return row, true, nil
}

func (s *sourceStreamPlanInstrumentedStream) Close() error {
	s.closeCalls++
	return nil
}

func parseSourceStreamPlanQuery(t *testing.T, query string) *ast.Query {
	t.Helper()
	q, err := parser.Parse(query)
	if err != nil {
		t.Fatalf("parse %q: %v", query, err)
	}
	return q
}

func sourceStreamPlanInfo() SourceInfo {
	return SourceInfo{
		Filename: "input.csv",
		Schema: table.NewSchema(
			[]string{"id", "name"},
			[]*table.TypeDescriptor{{Kind: table.TypeInt}, {Kind: table.TypeString}},
		),
	}
}

func sourceStreamPlanFullTable() *table.Table {
	t := table.NewTableWithSchemas(
		[]string{"id", "name"},
		[]*table.TypeDescriptor{{Kind: table.TypeInt}, {Kind: table.TypeString}},
	)
	if err := t.AddRowTyped([]table.Value{table.IntVal(1), table.StrVal("Alice")}); err != nil {
		panic(err)
	}
	if err := t.AddRowTyped([]table.Value{table.IntVal(2), table.StrVal("Bob")}); err != nil {
		panic(err)
	}
	return t
}

func sourceStreamPlanTableForSpec(tb testing.TB, spec SourceLoadSpec) *table.Table {
	tb.Helper()
	full := sourceStreamPlanFullTable()
	if spec.OutputColumns.IsAll() {
		return full
	}

	outputColumns := spec.OutputColumns.Names()
	schema := sourceStreamPlanSchemaForColumns(tb, outputColumns)
	cols := make([]string, len(schema.Columns))
	schemas := make([]*table.TypeDescriptor, len(schema.Columns))
	for i, col := range schema.Columns {
		cols[i] = col.Name
		schemas[i] = col.Type
	}
	out := table.NewTableWithSchemas(cols, schemas)
	for row := 0; row < full.NumRows; row++ {
		vals := make([]table.Value, len(outputColumns))
		for i, col := range outputColumns {
			idx := full.ColIndex(col)
			if idx < 0 {
				tb.Fatalf("sourceStreamPlanTableForSpec: missing output column %q", col)
			}
			vals[i] = full.GetAt(row, idx)
		}
		if err := out.AddRowTyped(vals); err != nil {
			tb.Fatalf("sourceStreamPlanTableForSpec: add row: %v", err)
		}
	}
	return out
}

func sourceStreamPlanSchemaForColumns(tb testing.TB, columns []string) table.Schema {
	tb.Helper()
	source := sourceStreamPlanInfo().Schema
	if columns == nil {
		return source
	}
	sourceIndex := make(map[string]table.SchemaColumn, len(source.Columns))
	for _, col := range source.Columns {
		sourceIndex[col.Name] = col
	}
	out := make([]table.SchemaColumn, len(columns))
	for i, name := range columns {
		col, ok := sourceIndex[name]
		if !ok {
			tb.Fatalf("sourceStreamPlanSchemaForColumns: missing output column %q", name)
		}
		out[i] = col
	}
	return table.Schema{Columns: out}
}
