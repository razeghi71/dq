package table

import (
	"fmt"
	"sort"
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
	TypeMixed  // schema-only marker for heterogeneous nested list elements
)

// RecordField is a named field in a record value.
type RecordField struct {
	Name  string
	Value Value
}

// Value is a dynamically-typed cell used during computation.
// It is NOT used for table storage; Table stores data in typed Column slices.
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

// AsBool coerces to boolean. TypeBool returns its value; TypeNull returns (false, true).
// Prefer EvalTruthAnd/Or/Not for logical operations (SQL three-valued logic).
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

// IsExplicitTrue reports whether v is TypeBool with value true (SQL WHERE / CASE WHEN).
func (v Value) IsExplicitTrue() bool {
	return v.Type == TypeBool && v.Bool
}

// IsBoolOrNull reports whether v is a valid operand for and/or/not (bool or unknown null).
func (v Value) IsBoolOrNull() bool {
	return v.Type == TypeBool || v.Type == TypeNull
}

type triBool int

const (
	triFalse triBool = iota
	triTrue
	triUnknown
)

func valueToTri(v Value) (triBool, bool) {
	switch v.Type {
	case TypeBool:
		if v.Bool {
			return triTrue, true
		}
		return triFalse, true
	case TypeNull:
		return triUnknown, true
	default:
		return triFalse, false
	}
}

func triToValue(t triBool) Value {
	switch t {
	case triTrue:
		return BoolVal(true)
	case triFalse:
		return BoolVal(false)
	default:
		return Null()
	}
}

func triAnd(a, b triBool) triBool {
	switch {
	case a == triFalse || b == triFalse:
		return triFalse
	case a == triTrue && b == triTrue:
		return triTrue
	default:
		return triUnknown
	}
}

func triOr(a, b triBool) triBool {
	switch {
	case a == triTrue || b == triTrue:
		return triTrue
	case a == triFalse && b == triFalse:
		return triFalse
	default:
		return triUnknown
	}
}

func triNot(a triBool) triBool {
	switch a {
	case triTrue:
		return triFalse
	case triFalse:
		return triTrue
	default:
		return triUnknown
	}
}

// EvalTruthAnd applies SQL three-valued AND to bool/null operands.
func EvalTruthAnd(left, right Value) (Value, error) {
	la, lok := valueToTri(left)
	ra, rok := valueToTri(right)
	if !lok || !rok {
		return Null(), fmt.Errorf("'and' requires boolean operands")
	}
	return triToValue(triAnd(la, ra)), nil
}

// EvalTruthOr applies SQL three-valued OR to bool/null operands.
func EvalTruthOr(left, right Value) (Value, error) {
	la, lok := valueToTri(left)
	ra, rok := valueToTri(right)
	if !lok || !rok {
		return Null(), fmt.Errorf("'or' requires boolean operands")
	}
	return triToValue(triOr(la, ra)), nil
}

// EvalTruthNot applies SQL three-valued NOT to a bool/null operand.
func EvalTruthNot(v Value) (Value, error) {
	a, ok := valueToTri(v)
	if !ok {
		return Null(), fmt.Errorf("'not' requires boolean operand")
	}
	return triToValue(triNot(a)), nil
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

// Column stores all values for one column as a typed slice plus a null bitmap.
// The Type field records the inferred type; TypeNull means no non-null data has
// been appended yet. Only the slice matching Type is populated.
type Column struct {
	name    string
	typ     ValueType
	schema  *TypeDescriptor
	nulls   []bool          // true = null at that row index
	ints    []int64         // TypeInt
	floats  []float64       // TypeFloat
	strs    []string        // TypeString
	bools   []bool          // TypeBool
	lists   [][]Value       // TypeList
	records [][]RecordField // TypeRecord
}

// Get materializes a Value for row i (used during computation).
func (c *Column) Get(i int) Value {
	if i < 0 || i >= len(c.nulls) || c.nulls[i] {
		return Null()
	}
	switch c.typ {
	case TypeInt:
		return IntVal(c.ints[i])
	case TypeFloat:
		return FloatVal(c.floats[i])
	case TypeString:
		return StrVal(c.strs[i])
	case TypeBool:
		return BoolVal(c.bools[i])
	case TypeList:
		return ListVal(c.lists[i])
	case TypeRecord:
		return RecordVal(c.records[i])
	default:
		return Null()
	}
}

// Append adds a value to the column, widening the column type if necessary.
func (c *Column) Append(v Value) {
	c.mergeSchema(v)
	if v.Type == TypeNull {
		c.appendNull()
		return
	}
	newType := widenType(c.typ, v.Type)
	if newType != c.typ {
		c.convertTo(newType)
	}
	c.nulls = append(c.nulls, false)
	switch c.typ {
	case TypeInt:
		c.ints = append(c.ints, v.Int)
	case TypeFloat:
		f, _ := v.AsFloat()
		c.floats = append(c.floats, f)
	case TypeString:
		c.strs = append(c.strs, v.AsString())
	case TypeBool:
		c.bools = append(c.bools, v.Bool)
	case TypeList:
		c.lists = append(c.lists, v.List)
	case TypeRecord:
		c.records = append(c.records, v.Fields)
	}
}

func (c *Column) mergeSchema(v Value) {
	if v.Type == TypeNull {
		if c.schema == nil {
			c.schema = &TypeDescriptor{Kind: TypeNull, Nullable: true}
			return
		}
		c.schema.Nullable = true
		return
	}

	if v.Type != TypeList && v.Type != TypeRecord {
		c.mergeScalarSchema(v.Type)
		return
	}

	if c.schema != nil {
		switch c.schema.Kind {
		case TypeString:
			return
		case TypeNull:
			c.schema = &TypeDescriptor{Kind: v.Type, Nullable: true}
			return
		case TypeInt, TypeFloat, TypeBool:
			c.schema = &TypeDescriptor{Kind: TypeString, Nullable: c.schema.Nullable}
			return
		}
	}

	next, err := MergeSchemasPermissive(c.schema, InferValueSchema(v))
	if err != nil {
		c.schema = ScalarSchema(TypeString)
		return
	}
	c.schema = next
}

func (c *Column) mergeScalarSchema(kind ValueType) {
	if c.schema == nil {
		c.schema = &TypeDescriptor{Kind: kind}
		return
	}
	nullable := c.schema.Nullable
	switch c.schema.Kind {
	case TypeNull:
		c.schema = &TypeDescriptor{Kind: kind, Nullable: true}
	case kind:
		return
	case TypeInt:
		if kind == TypeFloat {
			c.schema = &TypeDescriptor{Kind: TypeFloat, Nullable: nullable}
			return
		}
		c.schema = &TypeDescriptor{Kind: TypeString, Nullable: nullable}
	case TypeFloat:
		if kind == TypeInt {
			return
		}
		c.schema = &TypeDescriptor{Kind: TypeString, Nullable: nullable}
	case TypeString:
		return
	default:
		c.schema = &TypeDescriptor{Kind: TypeString, Nullable: nullable}
	}
}

func (c *Column) appendNull() {
	c.nulls = append(c.nulls, true)
	// keep typed slice length in sync (TypeNull has no slice yet)
	switch c.typ {
	case TypeInt:
		c.ints = append(c.ints, 0)
	case TypeFloat:
		c.floats = append(c.floats, 0)
	case TypeString:
		c.strs = append(c.strs, "")
	case TypeBool:
		c.bools = append(c.bools, false)
	case TypeList:
		c.lists = append(c.lists, nil)
	case TypeRecord:
		c.records = append(c.records, nil)
	}
}

// widenType returns the type that can represent both existing and incoming.
func widenType(existing, incoming ValueType) ValueType {
	if existing == TypeNull {
		return incoming
	}
	if existing == incoming {
		return existing
	}
	if (existing == TypeInt && incoming == TypeFloat) || (existing == TypeFloat && incoming == TypeInt) {
		return TypeFloat
	}
	return TypeString
}

// convertTo changes the column's storage type, converting existing data in-place.
func (c *Column) convertTo(newType ValueType) {
	n := len(c.nulls)
	oldType := c.typ

	switch newType {
	case TypeInt:
		c.ints = make([]int64, n)
	case TypeFloat:
		newFloats := make([]float64, n)
		if oldType == TypeInt {
			for i, v := range c.ints {
				newFloats[i] = float64(v)
			}
			c.ints = nil
		}
		c.floats = newFloats
	case TypeString:
		newStrs := make([]string, n)
		for i := range c.nulls {
			if !c.nulls[i] {
				switch oldType {
				case TypeInt:
					newStrs[i] = fmt.Sprintf("%d", c.ints[i])
				case TypeFloat:
					newStrs[i] = fmt.Sprintf("%g", c.floats[i])
				case TypeBool:
					if c.bools[i] {
						newStrs[i] = "true"
					} else {
						newStrs[i] = "false"
					}
				case TypeList:
					newStrs[i] = ListVal(c.lists[i]).AsString()
				case TypeRecord:
					newStrs[i] = RecordVal(c.records[i]).AsString()
				}
			}
		}
		c.strs = newStrs
		c.ints = nil
		c.floats = nil
		c.bools = nil
		c.lists = nil
		c.records = nil
	case TypeBool:
		c.bools = make([]bool, n)
	case TypeList:
		c.lists = make([][]Value, n)
	case TypeRecord:
		c.records = make([][]RecordField, n)
	}
	c.typ = newType
}

// Len returns the number of rows in this column.
func (c *Column) Len() int { return len(c.nulls) }

// ColType returns the column's ValueType.
func (c *Column) ColType() ValueType { return c.typ }

// Schema returns the column's recursive type descriptor, when known.
func (c *Column) Schema() *TypeDescriptor {
	if c == nil {
		return nil
	}
	if c.schema != nil {
		return FinalizeSchema(c.schema)
	}
	if c.typ == TypeNull {
		return &TypeDescriptor{Kind: TypeNull}
	}
	return ScalarSchema(c.typ)
}

// RawSchema returns the column descriptor before null-only portions are finalized.
func (c *Column) RawSchema() *TypeDescriptor {
	if c == nil {
		return nil
	}
	return cloneTypeDescriptor(c.schema)
}

// ColName returns the column's name.
func (c *Column) ColName() string { return c.name }

// Table is the core data structure: named columns with per-column typed storage.
type Table struct {
	Columns []string  // column names (kept exported for backward compat)
	cols    []*Column // typed column storage (one per column)
	NumRows int
}

// NewTable creates an empty table with the given column names.
func NewTable(columns []string) *Table {
	t := &Table{
		Columns: make([]string, len(columns)),
		cols:    make([]*Column, len(columns)),
	}
	copy(t.Columns, columns)
	for i, name := range columns {
		t.cols[i] = &Column{name: name, typ: TypeNull}
	}
	return t
}

// NewTableWithTypes creates an empty table with fixed initial column storage
// types. Loaders use this after schema inference so all-null columns can still
// report the inferred type.
func NewTableWithTypes(columns []string, types []ValueType) *Table {
	t := &Table{
		Columns: make([]string, len(columns)),
		cols:    make([]*Column, len(columns)),
	}
	copy(t.Columns, columns)
	for i, name := range columns {
		typ := TypeNull
		if i < len(types) {
			typ = types[i]
		}
		t.cols[i] = &Column{name: name, typ: typ, schema: ScalarSchema(typ)}
	}
	return t
}

// NewTableWithSchemas creates an empty table with fixed recursive column schemas.
func NewTableWithSchemas(columns []string, schemas []*TypeDescriptor) *Table {
	t := &Table{
		Columns: make([]string, len(columns)),
		cols:    make([]*Column, len(columns)),
	}
	copy(t.Columns, columns)
	for i, name := range columns {
		var schema *TypeDescriptor
		typ := TypeNull
		if i < len(schemas) && schemas[i] != nil {
			schema = cloneTypeDescriptor(schemas[i])
			typ = FinalizeSchema(schema).Kind
		}
		t.cols[i] = &Column{name: name, typ: typ, schema: schema}
	}
	return t
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

// Col returns the Column at index i, or nil if out of range.
func (t *Table) Col(i int) *Column {
	if i < 0 || i >= len(t.cols) {
		return nil
	}
	return t.cols[i]
}

// AddRow appends a row, distributing values to typed column slices.
// If values is shorter than the number of columns, remaining columns get null.
func (t *Table) AddRow(values []Value) {
	for i, col := range t.cols {
		if i < len(values) {
			col.Append(values[i])
		} else {
			col.Append(Null())
		}
	}
	t.NumRows++
}

// AddRowTyped appends a row after validating values against existing column
// schemas. It never widens concrete types; every non-null value must already fit
// the column schema and storage type. Accepted nulls are merged into the schema
// so describe can report top-level and nested nullability accurately. Use AddRow
// for permissive widening.
func (t *Table) AddRowTyped(values []Value) error {
	coerced := make([]Value, len(t.cols))
	nextSchemas := make([]*TypeDescriptor, len(t.cols))
	for i, col := range t.cols {
		v := Null()
		if i < len(values) {
			v = values[i]
		}
		cv, err := CoerceValueToSchemaAtPath(v, col.schema, col.name)
		if err != nil {
			return err
		}
		if cv.Type != TypeNull && col.typ == TypeNull {
			return fmt.Errorf("column %q has no concrete type for non-null %s value", col.name, TypeName(cv.Type))
		}
		if col.schema != nil {
			nextSchema, err := MergeValueSchemaStrictAtPath(cloneTypeDescriptor(col.schema), cv, col.name)
			if err != nil {
				return err
			}
			nextSchemas[i] = nextSchema
		}
		coerced[i] = cv
	}
	for i, col := range t.cols {
		if nextSchemas[i] != nil {
			col.schema = nextSchemas[i]
		}
		col.appendCoerced(coerced[i])
	}
	t.NumRows++
	return nil
}

func (c *Column) appendCoerced(v Value) {
	if v.Type == TypeNull {
		c.appendNull()
		return
	}
	if c.typ == TypeFloat && v.Type == TypeInt {
		v = FloatVal(float64(v.Int))
	}
	c.nulls = append(c.nulls, false)
	switch c.typ {
	case TypeInt:
		c.ints = append(c.ints, v.Int)
	case TypeFloat:
		f, _ := v.AsFloat()
		c.floats = append(c.floats, f)
	case TypeString:
		c.strs = append(c.strs, v.AsString())
	case TypeBool:
		c.bools = append(c.bools, v.Bool)
	case TypeList:
		c.lists = append(c.lists, v.List)
	case TypeRecord:
		c.records = append(c.records, v.Fields)
	}
}

// GetAt returns the Value at row row, column col (by index).
func (t *Table) GetAt(row, col int) Value {
	if col < 0 || col >= len(t.cols) || row < 0 || row >= t.NumRows {
		return Null()
	}
	return t.cols[col].Get(row)
}

// Get returns the value at a given row and column name.
func (t *Table) Get(row int, col string) Value {
	return t.GetAt(row, t.ColIndex(col))
}

// SliceRows returns a new Table containing only rows [from, to).
// Column data is copied (not shared).
func (t *Table) SliceRows(from, to int) *Table {
	if from < 0 {
		from = 0
	}
	if to > t.NumRows {
		to = t.NumRows
	}
	result := &Table{
		Columns: make([]string, len(t.Columns)),
		cols:    make([]*Column, len(t.cols)),
		NumRows: to - from,
	}
	copy(result.Columns, t.Columns)
	for i, c := range t.cols {
		result.cols[i] = sliceColumn(c, from, to)
	}
	return result
}

func sliceColumn(c *Column, from, to int) *Column {
	n := to - from
	nc := &Column{name: c.name, typ: c.typ, schema: FinalizeSchema(c.schema), nulls: make([]bool, n)}
	copy(nc.nulls, c.nulls[from:to])
	switch c.typ {
	case TypeInt:
		nc.ints = make([]int64, n)
		copy(nc.ints, c.ints[from:to])
	case TypeFloat:
		nc.floats = make([]float64, n)
		copy(nc.floats, c.floats[from:to])
	case TypeString:
		nc.strs = make([]string, n)
		copy(nc.strs, c.strs[from:to])
	case TypeBool:
		nc.bools = make([]bool, n)
		copy(nc.bools, c.bools[from:to])
	case TypeList:
		nc.lists = make([][]Value, n)
		copy(nc.lists, c.lists[from:to])
	case TypeRecord:
		nc.records = make([][]RecordField, n)
		copy(nc.records, c.records[from:to])
	}
	return nc
}

// ApplyPermutation returns a new Table with rows reordered by perm.
func (t *Table) ApplyPermutation(perm []int) *Table {
	n := len(perm)
	result := &Table{
		Columns: make([]string, len(t.Columns)),
		cols:    make([]*Column, len(t.cols)),
		NumRows: n,
	}
	copy(result.Columns, t.Columns)
	for i, c := range t.cols {
		result.cols[i] = permuteColumn(c, perm)
	}
	return result
}

func permuteColumn(c *Column, perm []int) *Column {
	n := len(perm)
	nc := &Column{name: c.name, typ: c.typ, schema: FinalizeSchema(c.schema), nulls: make([]bool, n)}
	for i, p := range perm {
		nc.nulls[i] = c.nulls[p]
	}
	switch c.typ {
	case TypeInt:
		nc.ints = make([]int64, n)
		for i, p := range perm {
			nc.ints[i] = c.ints[p]
		}
	case TypeFloat:
		nc.floats = make([]float64, n)
		for i, p := range perm {
			nc.floats[i] = c.floats[p]
		}
	case TypeString:
		nc.strs = make([]string, n)
		for i, p := range perm {
			nc.strs[i] = c.strs[p]
		}
	case TypeBool:
		nc.bools = make([]bool, n)
		for i, p := range perm {
			nc.bools[i] = c.bools[p]
		}
	case TypeList:
		nc.lists = make([][]Value, n)
		for i, p := range perm {
			nc.lists[i] = c.lists[p]
		}
	case TypeRecord:
		nc.records = make([][]RecordField, n)
		for i, p := range perm {
			nc.records[i] = c.records[p]
		}
	}
	return nc
}

// SelectCols returns a new Table with only the specified column indices (data shared).
// Used by execRemove and similar operations that don't need a data copy.
func (t *Table) SelectCols(indices []int, names []string) *Table {
	result := &Table{
		Columns: make([]string, len(indices)),
		cols:    make([]*Column, len(indices)),
		NumRows: t.NumRows,
	}
	copy(result.Columns, names)
	for i, idx := range indices {
		result.cols[i] = t.cols[idx]
	}
	return result
}

// ShallowClone returns a new Table with the same column data but new column names.
// Column data pointers are shared -- use only when the result's data won't be mutated.
func (t *Table) ShallowClone(newColNames []string) *Table {
	result := &Table{
		Columns: make([]string, len(newColNames)),
		cols:    make([]*Column, len(t.cols)),
		NumRows: t.NumRows,
	}
	copy(result.Columns, newColNames)
	copy(result.cols, t.cols)
	return result
}

// UnionColumns combines column sets using the same ordering policy as table
// concatenation: the first set keeps its order, and new columns from later sets
// are appended in sorted order.
func UnionColumns(columnSets ...[]string) []string {
	if len(columnSets) == 0 {
		return nil
	}

	firstCols := append([]string(nil), columnSets[0]...)
	firstSet := make(map[string]bool, len(firstCols))
	for _, col := range firstCols {
		firstSet[col] = true
	}
	extraSet := make(map[string]bool)
	for _, columns := range columnSets[1:] {
		for _, col := range columns {
			if !firstSet[col] && !extraSet[col] {
				extraSet[col] = true
			}
		}
	}
	extra := make([]string, 0, len(extraSet))
	for col := range extraSet {
		extra = append(extra, col)
	}
	sort.Strings(extra)
	return append(firstCols, extra...)
}

// Concat combines tables by column union. Columns from the first table keep
// their order; new columns from later tables are appended in sorted order.
// Missing values are null-filled. Rows are copied via AddRow so column types widen.
func Concat(tables []*Table) (*Table, error) {
	if len(tables) == 0 {
		return nil, fmt.Errorf("concat: no tables")
	}

	columnSets := make([][]string, len(tables))
	for i, tbl := range tables {
		columnSets[i] = tbl.Columns
	}
	unionCols := UnionColumns(columnSets...)

	result := NewTable(unionCols)
	for _, tbl := range tables {
		for row := 0; row < tbl.NumRows; row++ {
			vals := make([]Value, len(unionCols))
			for i, col := range unionCols {
				if idx := tbl.ColIndex(col); idx >= 0 {
					vals[i] = tbl.Col(idx).Get(row)
				} else {
					vals[i] = Null()
				}
			}
			result.AddRow(vals)
		}
	}
	return result, nil
}

// Clone creates a deep copy of the table.
func (t *Table) Clone() *Table {
	result := &Table{
		Columns: make([]string, len(t.Columns)),
		cols:    make([]*Column, len(t.cols)),
		NumRows: t.NumRows,
	}
	copy(result.Columns, t.Columns)
	for i, c := range t.cols {
		result.cols[i] = cloneColumn(c)
	}
	return result
}

func cloneColumn(c *Column) *Column {
	n := c.Len()
	nc := &Column{name: c.name, typ: c.typ, schema: FinalizeSchema(c.schema), nulls: make([]bool, n)}
	copy(nc.nulls, c.nulls)
	switch c.typ {
	case TypeInt:
		nc.ints = make([]int64, n)
		copy(nc.ints, c.ints)
	case TypeFloat:
		nc.floats = make([]float64, n)
		copy(nc.floats, c.floats)
	case TypeString:
		nc.strs = make([]string, n)
		copy(nc.strs, c.strs)
	case TypeBool:
		nc.bools = make([]bool, n)
		copy(nc.bools, c.bools)
	case TypeList:
		nc.lists = make([][]Value, n)
		copy(nc.lists, c.lists)
	case TypeRecord:
		nc.records = make([][]RecordField, n)
		copy(nc.records, c.records)
	}
	return nc
}

// String returns a compact representation of the table.
func (t *Table) String() string {
	if t.NumRows == 0 {
		return "[" + strings.Join(t.Columns, ", ") + "] (0 rows)"
	}
	var sb strings.Builder
	sb.WriteString("[ ")
	for i := 0; i < t.NumRows; i++ {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString("{")
		for j, col := range t.Columns {
			if j > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(col)
			sb.WriteString(":")
			sb.WriteString(t.cols[j].Get(i).AsString())
		}
		sb.WriteString("}")
	}
	sb.WriteString(" ]")
	return sb.String()
}
