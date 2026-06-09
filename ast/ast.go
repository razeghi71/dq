package ast

// Expr represents an expression tree used in filter, transform, and reduce.
type Expr interface {
	exprNode()
}

// LiteralExpr represents a literal value: number, string, bool, null.
type LiteralExpr struct {
	// Kind: "int", "float", "string", "bool", "null"
	Kind  string
	Int   int64
	Float float64
	Str   string
	Bool  bool
}

func (e *LiteralExpr) exprNode() {}

// ColumnExpr references a column by name or a nested field via dot path.
type ColumnExpr struct {
	Path []string // e.g. ["address"] or ["address", "city"]
}

func (e *ColumnExpr) exprNode() {}

// BinaryExpr represents a binary operation: a op b.
type BinaryExpr struct {
	Op    string // +, -, *, /, ==, !=, <, >, <=, >=, and, or
	Left  Expr
	Right Expr
}

func (e *BinaryExpr) exprNode() {}

// UnaryExpr represents a unary operation (e.g. not, unary minus).
type UnaryExpr struct {
	Op      string // "not", "-"
	Operand Expr
}

func (e *UnaryExpr) exprNode() {}

// FuncCallExpr represents a function call: func(arg1, arg2, ...).
type FuncCallExpr struct {
	Name string
	Args []Expr
}

func (e *FuncCallExpr) exprNode() {}

// IsNullExpr represents "col is null" or "col is not null".
type IsNullExpr struct {
	Operand Expr
	Negated bool // true = "is not null"
}

func (e *IsNullExpr) exprNode() {}

// Assignment represents "col = expr" in transform/reduce.
type Assignment struct {
	Column string
	Expr   Expr
}

// --- Operations (pipeline stages) ---

// Op represents a single operation in the pipeline.
type Op interface {
	opNode()
}

// SourceOp represents the input file reference.
type SourceOp struct {
	Filename string
}

func (o *SourceOp) opNode() {}

// HeadOp returns the first N rows.
type HeadOp struct {
	N int
}

func (o *HeadOp) opNode() {}

// TailOp returns the last N rows.
type TailOp struct {
	N int
}

func (o *TailOp) opNode() {}

// SortKey is one column to sort by, with a direction.
type SortKey struct {
	Path []string
	Desc bool
}

// SortOp sorts rows by an ordered list of keys.
type SortOp struct {
	Keys []SortKey
}

func (o *SortOp) opNode() {}

// SelectOp projects specific columns.
type SelectOp struct {
	Columns [][]string
}

func (o *SelectOp) opNode() {}

// FilterOp filters rows by an expression.
type FilterOp struct {
	Expr Expr
}

func (o *FilterOp) opNode() {}

// GroupOp groups rows by columns, nesting the rest.
type GroupOp struct {
	Columns    [][]string
	NestedName string // default "grouped"
}

func (o *GroupOp) opNode() {}

// TransformOp creates or overwrites columns with computed values.
type TransformOp struct {
	Assignments []Assignment
}

func (o *TransformOp) opNode() {}

// ReduceOp aggregates over the nested table from a group.
type ReduceOp struct {
	NestedName  string // which nested column to reduce over; default "grouped"
	Assignments []Assignment
}

func (o *ReduceOp) opNode() {}

// CountOp returns a single-row table with the row count.
type CountOp struct{}

func (o *CountOp) opNode() {}

// DistinctOp deduplicates rows.
type DistinctOp struct {
	Columns [][]string // empty = all columns
}

func (o *DistinctOp) opNode() {}

// RenameOp renames columns.
type RenameOp struct {
	Pairs []RenamePair
}

type RenamePair struct {
	Old string
	New string
}

func (o *RenameOp) opNode() {}

// RemoveOp removes columns.
type RemoveOp struct {
	Columns [][]string
}

func (o *RemoveOp) opNode() {}

// JoinKey is one join condition: left_path == right_path (Right == Left when omitted).
type JoinKey struct {
	Left  []string
	Right []string
}

// JoinOp joins the current table with another file.
type JoinOp struct {
	Kind     string // inner, left, right, full
	Filename string
	Keys     []JoinKey
}

func (o *JoinOp) opNode() {}

// Query represents a full parsed query: source + pipeline of operations.
type Query struct {
	Source *SourceOp
	Ops    []Op
}
