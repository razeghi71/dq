package rowstream

import (
	"fmt"

	"github.com/razeghi71/dq/table"
)

// Row is the value-level representation passed between streaming operators.
// Implementations may reuse the backing slice; consumers must copy rows they
// retain past the next call to Next.
type Row []table.Value

// Stream is a pull-based row stream with an already-known logical schema.
type Stream interface {
	Schema() table.Schema
	Next() (Row, bool, error)
	Close() error
}

type tableStream struct {
	table  *table.Table
	schema table.Schema
	row    int
}

// FromTable returns a stream over a materialized table.
func FromTable(t *table.Table) Stream {
	if t == nil {
		return &tableStream{schema: table.NewSchema(nil, nil)}
	}
	return &tableStream{table: t, schema: t.Schema()}
}

// AsTable returns the underlying table when s is an unconsumed FromTable stream.
func AsTable(s Stream) (*table.Table, bool) {
	ts, ok := s.(*tableStream)
	if !ok || ts.table == nil || ts.row != 0 {
		return nil, false
	}
	return ts.table, true
}

func (s *tableStream) Schema() table.Schema {
	return s.schema
}

func (s *tableStream) Next() (Row, bool, error) {
	if s.table == nil || s.row >= s.table.NumRows {
		return nil, false, nil
	}
	vals := make([]table.Value, len(s.table.Columns))
	for i := range s.table.Columns {
		vals[i] = s.table.Col(i).Get(s.row)
	}
	s.row++
	return vals, true, nil
}

func (s *tableStream) Close() error {
	return nil
}

// Materialize consumes a stream into a fixed-schema table.
func Materialize(s Stream) (*table.Table, error) {
	if s == nil {
		return table.NewTable(nil), nil
	}
	cols, schemas := schemaColumns(s.Schema())
	out := table.NewTableWithSchemas(cols, schemas)
	for {
		row, ok, err := s.Next()
		if err != nil {
			_ = s.Close()
			return nil, err
		}
		if !ok {
			if err := s.Close(); err != nil {
				return nil, err
			}
			return out, nil
		}
		if err := out.AddRowTyped([]table.Value(row)); err != nil {
			_ = s.Close()
			return nil, fmt.Errorf("materialize stream: %w", err)
		}
	}
}

func schemaColumns(schema table.Schema) ([]string, []*table.TypeDescriptor) {
	cols := make([]string, len(schema.Columns))
	schemas := make([]*table.TypeDescriptor, len(schema.Columns))
	for i, col := range schema.Columns {
		cols[i] = col.Name
		schemas[i] = col.Type
	}
	return cols, schemas
}
