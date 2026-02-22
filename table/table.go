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
	TypeList   // List  []Value
	TypeRecord // Fields []RecordField
)

// RecordField is a named field in a record value.
type RecordField struct {
	Name  string
	Value Value
}

// Value is a dynamically-typed cell in a table.
type Value struct {
	Type   ValueType
	Int    int64
	Float  float64
	Str    string
	Bool   bool
	List   []Value
	Fields []RecordField
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

// ListVal creates a list value.
func ListVal(elems []Value) Value {
	return Value{Type: TypeList, List: elems}
}

// RecordVal creates a record value.
func RecordVal(fields []RecordField) Value {
	return Value{Type: TypeRecord, Fields: fields}
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
	case TypeList:
		parts := make([]string, len(v.List))
		for i, e := range v.List {
			parts[i] = e.AsString()
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case TypeRecord:
		parts := make([]string, len(v.Fields))
		for i, f := range v.Fields {
			parts[i] = f.Name + ":" + f.Value.AsString()
		}
		return "{" + strings.Join(parts, ", ") + "}"
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

// ListToTable converts a TypeList of TypeRecord values into a *Table.
// Used internally by execReduce.
func ListToTable(v Value) (*Table, error) {
	if v.Type != TypeList {
		return nil, fmt.Errorf("expected TypeList, got %v", v.Type)
	}
	if len(v.List) == 0 {
		return NewTable(nil), nil
	}

	first := v.List[0]
	if first.Type != TypeRecord {
		return nil, fmt.Errorf("expected list of TypeRecord, got element of type %v", first.Type)
	}

	columns := make([]string, len(first.Fields))
	for i, f := range first.Fields {
		columns[i] = f.Name
	}

	t := NewTable(columns)
	for _, elem := range v.List {
		if elem.Type != TypeRecord {
			return nil, fmt.Errorf("list element is not a TypeRecord")
		}
		vals := make([]Value, len(columns))
		// build index from field name -> position in columns
		for j, col := range columns {
			found := false
			for _, f := range elem.Fields {
				if f.Name == col {
					vals[j] = f.Value
					found = true
					break
				}
			}
			if !found {
				vals[j] = Null()
			}
		}
		t.AddRow(vals)
	}
	return t, nil
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

