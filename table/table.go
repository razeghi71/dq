package table

import (
	"fmt"
	"strings"
)

// ValueType represents the type of a Value.
type ValueType int

const (
	TypeNull ValueType = iota
	TypeInt
	TypeFloat
	TypeString
	TypeBool
	TypeNested // nested table (from group)
)

// Value is a dynamically-typed cell in a table.
type Value struct {
	Type   ValueType
	Int    int64
	Float  float64
	Str    string
	Bool   bool
	Nested *Table
}

// Null returns a null value.
func Null() Value {
	return Value{Type: TypeNull}
}

// IntVal creates an integer value.
func IntVal(v int64) Value {
	return Value{Type: TypeInt, Int: v}
}

// FloatVal creates a float value.
func FloatVal(v float64) Value {
	return Value{Type: TypeFloat, Float: v}
}

// StrVal creates a string value.
func StrVal(v string) Value {
	return Value{Type: TypeString, Str: v}
}

// BoolVal creates a boolean value.
func BoolVal(v bool) Value {
	return Value{Type: TypeBool, Bool: v}
}

// NestedVal creates a nested table value.
func NestedVal(t *Table) Value {
	return Value{Type: TypeNested, Nested: t}
}

// IsNull returns true if the value is null.
func (v Value) IsNull() bool {
	return v.Type == TypeNull
}

// AsFloat attempts to coerce to float64 for arithmetic.
func (v Value) AsFloat() (float64, bool) {
	switch v.Type {
	case TypeInt:
		return float64(v.Int), true
	case TypeFloat:
		return v.Float, true
	default:
		return 0, false
	}
}

// AsString returns the string representation.
func (v Value) AsString() string {
	switch v.Type {
	case TypeNull:
		return "null"
	case TypeInt:
		return fmt.Sprintf("%d", v.Int)
	case TypeFloat:
		return fmt.Sprintf("%g", v.Float)
	case TypeString:
		return v.Str
	case TypeBool:
		if v.Bool {
			return "true"
		}
		return "false"
	case TypeNested:
		return v.Nested.String()
	default:
		return "?"
	}
}

// AsBool coerces to boolean for logical operations.
func (v Value) AsBool() (bool, bool) {
	switch v.Type {
	case TypeBool:
		return v.Bool, true
	case TypeNull:
		return false, true
	default:
		return false, false
	}
}

// Row is a single row in a table, mapping column index to value.
type Row struct {
	Values []Value
}

// Table is the core data structure: columns + rows.
type Table struct {
	Columns []string
	Rows    []Row
}

// NewTable creates an empty table with the given columns.
func NewTable(columns []string) *Table {
	return &Table{
		Columns: columns,
		Rows:    nil,
	}
}

// ColIndex returns the index of a column by name, or -1.
func (t *Table) ColIndex(name string) int {
	for i, c := range t.Columns {
		if c == name {
			return i
		}
	}
	return -1
}

// AddRow appends a row to the table.
func (t *Table) AddRow(values []Value) {
	t.Rows = append(t.Rows, Row{Values: values})
}

// Get returns the value at a given row and column name.
func (t *Table) Get(row int, col string) Value {
	idx := t.ColIndex(col)
	if idx < 0 || row < 0 || row >= len(t.Rows) {
		return Null()
	}
	return t.Rows[row].Values[idx]
}

// Clone creates a deep copy of the table structure (shares Value data).
func (t *Table) Clone() *Table {
	cols := make([]string, len(t.Columns))
	copy(cols, t.Columns)
	rows := make([]Row, len(t.Rows))
	for i, r := range t.Rows {
		vals := make([]Value, len(r.Values))
		copy(vals, r.Values)
		rows[i] = Row{Values: vals}
	}
	return &Table{Columns: cols, Rows: rows}
}

// String returns a compact representation of the table.
func (t *Table) String() string {
	if len(t.Rows) == 0 {
		return "[" + strings.Join(t.Columns, ", ") + "] (0 rows)"
	}

	var sb strings.Builder
	sb.WriteString("[ ")
	for i, r := range t.Rows {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString("{")
		for j, v := range r.Values {
			if j > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(t.Columns[j])
			sb.WriteString(":")
			sb.WriteString(v.AsString())
		}
		sb.WriteString("}")
	}
	sb.WriteString(" ]")
	return sb.String()
}
