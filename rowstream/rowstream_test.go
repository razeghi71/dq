package rowstream

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/razeghi71/dq/table"
)

func TestFromTableMaterializeRoundTrip(t *testing.T) {
	input := table.NewTableWithSchemas(
		[]string{"id", "name"},
		[]*table.TypeDescriptor{{Kind: table.TypeInt}, {Kind: table.TypeString}},
	)
	if err := input.AddRowTyped([]table.Value{table.IntVal(1), table.StrVal("Alice")}); err != nil {
		t.Fatalf("seed row 1: %v", err)
	}
	if err := input.AddRowTyped([]table.Value{table.IntVal(2), table.StrVal("Bob")}); err != nil {
		t.Fatalf("seed row 2: %v", err)
	}

	stream := FromTable(input)
	if got := len(stream.Schema().Columns); got != 2 {
		t.Fatalf("schema columns: got %d, want 2", got)
	}
	output, err := Materialize(stream)
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	if output.NumRows != 2 || output.GetAt(1, 1).Str != "Bob" {
		t.Fatalf("round trip output: rows=%d row1.name=%s", output.NumRows, output.GetAt(1, 1).Str)
	}
	if got := output.Col(output.ColIndex("id")).Schema().String(); got != "int" {
		t.Fatalf("id schema: got %q, want int", got)
	}
}

func TestFromTableNilMaterializesEmptyTable(t *testing.T) {
	output, err := Materialize(FromTable(nil))
	if err != nil {
		t.Fatalf("materialize nil table stream: %v", err)
	}
	if output.NumRows != 0 || len(output.Columns) != 0 {
		t.Fatalf("nil table stream output: rows=%d cols=%v", output.NumRows, output.Columns)
	}
}

func TestMaterializeClosesOnNextError(t *testing.T) {
	stream := &rowstreamFailingStream{
		schema: table.NewSchema([]string{"id"}, []*table.TypeDescriptor{{Kind: table.TypeInt}}),
		err:    errors.New("next failed"),
	}
	_, err := Materialize(stream)
	if err == nil || !strings.Contains(err.Error(), "next failed") {
		t.Fatalf("materialize error: got %v", err)
	}
	if !stream.closed {
		t.Fatal("stream should be closed after Next error")
	}
}

func TestMaterializeReturnsCloseErrorAtEOF(t *testing.T) {
	stream := &rowstreamCloseErrorStream{
		schema: table.NewSchema([]string{"id"}, []*table.TypeDescriptor{{Kind: table.TypeInt}}),
		err:    errors.New("close failed"),
	}
	_, err := Materialize(stream)
	if err == nil || !strings.Contains(err.Error(), "close failed") {
		t.Fatalf("close error: got %v", err)
	}
}

func TestMaterializeRejectsRowsOutsideSchema(t *testing.T) {
	stream := &rowstreamRows{
		schema: table.NewSchema([]string{"id"}, []*table.TypeDescriptor{{Kind: table.TypeInt}}),
		rows:   []Row{{table.StrVal("bad")}},
	}
	_, err := Materialize(stream)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "materialize stream") {
		t.Fatalf("typed materialize error: got %v", err)
	}
	if !stream.closed {
		t.Fatal("stream should be closed after typed append error")
	}
}

func TestAsTableRecognizesOnlyUnconsumedTableStreams(t *testing.T) {
	input := table.NewTableWithSchemas(
		[]string{"id"},
		[]*table.TypeDescriptor{{Kind: table.TypeInt}},
	)
	if err := input.AddRowTyped([]table.Value{table.IntVal(1)}); err != nil {
		t.Fatalf("seed row: %v", err)
	}

	stream := FromTable(input)
	got, ok := AsTable(stream)
	if !ok || got != input {
		t.Fatalf("AsTable unconsumed stream: got ok=%v table=%p, want original %p", ok, got, input)
	}
	if _, ok, err := stream.Next(); err != nil || !ok {
		t.Fatalf("consume table stream: ok=%v err=%v", ok, err)
	}
	if got, ok := AsTable(stream); ok || got != nil {
		t.Fatalf("AsTable consumed stream: got ok=%v table=%p, want none", ok, got)
	}
	if got, ok := AsTable(Map(FromTable(input), input.Schema(), nil)); ok || got != nil {
		t.Fatalf("AsTable map stream: got ok=%v table=%p, want none", ok, got)
	}
}

func TestMapFiltersRowsAndClosesInput(t *testing.T) {
	schema := table.NewSchema([]string{"id"}, []*table.TypeDescriptor{{Kind: table.TypeInt}})
	input := &rowstreamRows{
		schema: schema,
		rows: []Row{
			{table.IntVal(1)},
			{table.IntVal(2)},
		},
	}
	stream := Map(input, schema, func(row Row) (Row, bool, error) {
		return row, row[0].Int == 2, nil
	})
	if got := stream.Schema(); len(got.Columns) != 1 || got.Columns[0].Name != "id" {
		t.Fatalf("map schema: got %#v, want id", got)
	}
	output, err := Materialize(stream)
	if err != nil {
		t.Fatalf("materialize map: %v", err)
	}
	if output.NumRows != 1 || output.GetAt(0, 0).Int != 2 {
		t.Fatalf("map output: rows=%d first=%v, want id 2", output.NumRows, output.GetAt(0, 0))
	}
	if !input.closed {
		t.Fatal("map input should be closed at EOF")
	}
}

func TestMapReturnsFunctionError(t *testing.T) {
	schema := table.NewSchema([]string{"id"}, []*table.TypeDescriptor{{Kind: table.TypeInt}})
	input := &rowstreamRows{schema: schema, rows: []Row{{table.IntVal(1)}}}
	stream := Map(input, schema, func(Row) (Row, bool, error) {
		return nil, false, errors.New("map failed")
	})
	_, _, err := stream.Next()
	if err == nil || !strings.Contains(err.Error(), "map failed") {
		t.Fatalf("map error: got %v", err)
	}
}

func TestParallelMapOrderedSerialFallbackNormalizesOptions(t *testing.T) {
	schema := table.NewSchema([]string{"id"}, []*table.TypeDescriptor{{Kind: table.TypeInt}})
	input := &rowstreamRows{schema: schema, rows: []Row{{table.IntVal(1)}, {table.IntVal(2)}}}
	stream := ParallelMapOrdered(input, schema, func(row Row) (Row, bool, error) {
		return Row{table.IntVal(row[0].Int + 10)}, true, nil
	}, ParallelOptions{Workers: 0, Buffer: 0})

	output, err := Materialize(stream)
	if err != nil {
		t.Fatalf("materialize serial fallback: %v", err)
	}
	if output.NumRows != 2 || output.GetAt(0, 0).Int != 11 || output.GetAt(1, 0).Int != 12 {
		t.Fatalf("serial fallback output: got %s", output.String())
	}
}

func TestParallelMapOrderedFiltersRowsAndUsesIdentityDefault(t *testing.T) {
	schema := table.NewSchema([]string{"id"}, []*table.TypeDescriptor{{Kind: table.TypeInt}})
	if opts := DefaultParallelOptions(); opts.Workers < 1 || opts.Buffer < opts.Workers {
		t.Fatalf("default parallel options: got %+v", opts)
	}

	identityInput := &rowstreamRows{schema: schema, rows: []Row{{table.IntVal(7)}}}
	identity := ParallelMapOrdered(identityInput, schema, nil, ParallelOptions{Workers: 2})
	identityOutput, err := Materialize(identity)
	if err != nil {
		t.Fatalf("materialize identity parallel map: %v", err)
	}
	if identityOutput.NumRows != 1 || identityOutput.GetAt(0, 0).Int != 7 {
		t.Fatalf("identity output: rows=%d first=%v, want id 7", identityOutput.NumRows, identityOutput.GetAt(0, 0))
	}

	filterInput := &rowstreamRows{
		schema: schema,
		rows: []Row{
			{table.IntVal(1)},
			{table.IntVal(2)},
			{table.IntVal(3)},
		},
	}
	filtered := ParallelMapOrdered(filterInput, schema, func(row Row) (Row, bool, error) {
		return row, row[0].Int%2 == 1, nil
	}, ParallelOptions{Workers: 2, Buffer: 2})
	filteredOutput, err := Materialize(filtered)
	if err != nil {
		t.Fatalf("materialize filtered parallel map: %v", err)
	}
	if filteredOutput.NumRows != 2 || filteredOutput.GetAt(0, 0).Int != 1 || filteredOutput.GetAt(1, 0).Int != 3 {
		t.Fatalf("filtered output: rows=%d values=[%v,%v], want 1,3", filteredOutput.NumRows, filteredOutput.GetAt(0, 0), filteredOutput.GetAt(1, 0))
	}
}

func TestParallelMapOrderedReturnsSourceErrorAfterPriorRows(t *testing.T) {
	schema := table.NewSchema([]string{"id"}, []*table.TypeDescriptor{{Kind: table.TypeInt}})
	input := &rowstreamRowsThenError{
		schema: schema,
		rows:   []Row{{table.IntVal(1)}},
		err:    errors.New("source failed"),
	}
	stream := ParallelMapOrdered(input, schema, func(row Row) (Row, bool, error) {
		return row, true, nil
	}, ParallelOptions{Workers: 2, Buffer: 2})

	row, ok, err := stream.Next()
	if err != nil || !ok || row[0].Int != 1 {
		t.Fatalf("first row: row=%v ok=%v err=%v, want id 1", row, ok, err)
	}
	_, _, err = stream.Next()
	if err == nil || !strings.Contains(err.Error(), "source failed") {
		t.Fatalf("source error: got %v, want source failed", err)
	}
}

func TestParallelMapOrderedDoesNotEmitLaterRowsFirst(t *testing.T) {
	schema := table.NewSchema([]string{"id"}, []*table.TypeDescriptor{{Kind: table.TypeInt}})
	input := &rowstreamRows{
		schema: schema,
		rows: []Row{
			{table.IntVal(0)},
			{table.IntVal(1)},
		},
	}
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	secondFinished := make(chan struct{})

	stream := ParallelMapOrdered(input, schema, func(row Row) (Row, bool, error) {
		switch row[0].Int {
		case 0:
			close(firstStarted)
			<-releaseFirst
		case 1:
			close(secondFinished)
		}
		return row, true, nil
	}, ParallelOptions{Workers: 2, Buffer: 2})

	type nextResult struct {
		row Row
		ok  bool
		err error
	}
	gotCh := make(chan nextResult, 1)
	go func() {
		row, ok, err := stream.Next()
		gotCh <- nextResult{row: row, ok: ok, err: err}
	}()

	<-firstStarted
	<-secondFinished
	select {
	case got := <-gotCh:
		t.Fatalf("emitted out of order before first row completed: %+v", got)
	default:
	}

	close(releaseFirst)
	got := <-gotCh
	if got.err != nil || !got.ok || got.row[0].Int != 0 {
		t.Fatalf("first emitted row: got row=%v ok=%v err=%v, want id 0", got.row, got.ok, got.err)
	}
}

func TestParallelMapOrderedReturnsEarliestInputError(t *testing.T) {
	schema := table.NewSchema([]string{"id"}, []*table.TypeDescriptor{{Kind: table.TypeInt}})
	input := &rowstreamRows{
		schema: schema,
		rows: []Row{
			{table.IntVal(0)},
			{table.IntVal(1)},
		},
	}
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	secondErrored := make(chan struct{})

	stream := ParallelMapOrdered(input, schema, func(row Row) (Row, bool, error) {
		switch row[0].Int {
		case 0:
			close(firstStarted)
			<-releaseFirst
			return nil, false, errors.New("first failed")
		case 1:
			close(secondErrored)
			return nil, false, errors.New("second failed")
		default:
			return row, true, nil
		}
	}, ParallelOptions{Workers: 2, Buffer: 2})

	errCh := make(chan error, 1)
	go func() {
		_, _, err := stream.Next()
		errCh <- err
	}()

	<-firstStarted
	<-secondErrored
	select {
	case err := <-errCh:
		t.Fatalf("returned later error before first row completed: %v", err)
	default:
	}

	close(releaseFirst)
	err := <-errCh
	if err == nil || !strings.Contains(err.Error(), "first failed") {
		t.Fatalf("ordered error: got %v, want first failed", err)
	}
}

func TestParallelMapOrderedCloseClosesInput(t *testing.T) {
	schema := table.NewSchema([]string{"id"}, []*table.TypeDescriptor{{Kind: table.TypeInt}})
	input := &rowstreamRows{
		schema: schema,
		rows:   []Row{{table.IntVal(1)}, {table.IntVal(2)}},
	}
	stream := ParallelMapOrdered(input, schema, func(row Row) (Row, bool, error) {
		time.Sleep(5 * time.Millisecond)
		return row, true, nil
	}, ParallelOptions{Workers: 2, Buffer: 2})

	if err := stream.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if !input.closed {
		t.Fatal("input stream should be closed")
	}
}

func TestParallelMapOrderedInternalClosedDoneBranches(t *testing.T) {
	done := make(chan struct{})
	close(done)
	stream := &parallelMapStream{
		done:    done,
		results: make(chan parallelResult),
		slots:   make(chan struct{}, 1),
	}
	if stream.sendResult(parallelResult{}) {
		t.Fatal("sendResult should report false when stream is closed")
	}
	stream.releaseSlot()
	if row := cloneRow(nil); row != nil {
		t.Fatalf("clone nil row: got %v, want nil", row)
	}
}

type rowstreamRows struct {
	schema table.Schema
	rows   []Row
	at     int
	closed bool
}

func (s *rowstreamRows) Schema() table.Schema { return s.schema }

func (s *rowstreamRows) Next() (Row, bool, error) {
	if s.at >= len(s.rows) {
		return nil, false, nil
	}
	row := s.rows[s.at]
	s.at++
	return row, true, nil
}

func (s *rowstreamRows) Close() error {
	s.closed = true
	return nil
}

type rowstreamRowsThenError struct {
	schema table.Schema
	rows   []Row
	at     int
	err    error
}

func (s *rowstreamRowsThenError) Schema() table.Schema { return s.schema }

func (s *rowstreamRowsThenError) Next() (Row, bool, error) {
	if s.at >= len(s.rows) {
		return nil, false, s.err
	}
	row := s.rows[s.at]
	s.at++
	return row, true, nil
}

func (s *rowstreamRowsThenError) Close() error { return nil }

type rowstreamFailingStream struct {
	schema table.Schema
	err    error
	closed bool
}

func (s *rowstreamFailingStream) Schema() table.Schema { return s.schema }

func (s *rowstreamFailingStream) Next() (Row, bool, error) {
	return nil, false, s.err
}

func (s *rowstreamFailingStream) Close() error {
	s.closed = true
	return nil
}

type rowstreamCloseErrorStream struct {
	schema table.Schema
	err    error
}

func (s *rowstreamCloseErrorStream) Schema() table.Schema { return s.schema }

func (s *rowstreamCloseErrorStream) Next() (Row, bool, error) {
	return nil, false, nil
}

func (s *rowstreamCloseErrorStream) Close() error {
	return s.err
}
