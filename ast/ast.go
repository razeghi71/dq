package ast

// Span identifies a byte range in the original query string.
// End is exclusive.
type Span struct {
	Start int
	End   int
}

// PackedSpan stores a source span compactly inside AST nodes.
type PackedSpan uint64

// PackSpan converts a byte range to compact AST storage.
func PackSpan(start, end int) PackedSpan {
	if start < 0 {
		start = 0
	}
	if end < start {
		end = start
	}
	return PackedSpan(uint64(uint32(start))<<32 | uint64(uint32(end)))
}

// Pack converts a public Span to compact AST storage.
func (s Span) Pack() PackedSpan {
	return PackSpan(s.Start, s.End)
}

// Unpack returns the public Span value.
func (s PackedSpan) Unpack() Span {
	return Span{
		Start: int(uint32(uint64(s) >> 32)),
		End:   int(uint32(uint64(s))),
	}
}

// MergeSpans returns the byte range covering a and b.
func MergeSpans(a, b Span) Span {
	if a.Start == 0 && a.End == 0 {
		return b
	}
	if b.Start == 0 && b.End == 0 {
		return a
	}
	if b.Start < a.Start {
		a.Start = b.Start
	}
	if b.End > a.End {
		a.End = b.End
	}
	return a
}

// Expr represents an expression tree used in filter, transform, and reduce.
type Expr interface {
	exprNode()
	Span() Span
}

// LiteralExpr represents a literal value: number, string, bool, null.
type LiteralExpr struct {
	// Kind: "int", "float", "string", "bool", "null"
	Kind       string
	Int        int64
	Float      float64
	Str        string
	Bool       bool
	SourceSpan PackedSpan
}

func (e *LiteralExpr) exprNode() {}
func (e *LiteralExpr) Span() Span {
	if e == nil {
		return Span{}
	}
	return e.SourceSpan.Unpack()
}

// ColumnExpr references a column by name or a nested field via dot path.
type ColumnExpr struct {
	Path       []string // e.g. ["address"] or ["address", "city"]
	SourceSpan PackedSpan
}

func (e *ColumnExpr) exprNode() {}
func (e *ColumnExpr) Span() Span {
	if e == nil {
		return Span{}
	}
	return e.SourceSpan.Unpack()
}

// BinaryExpr represents a binary operation: a op b.
type BinaryExpr struct {
	Op         string // +, -, *, /, ==, !=, <, >, <=, >=, and, or
	Left       Expr
	Right      Expr
	SourceSpan PackedSpan
}

func (e *BinaryExpr) exprNode() {}
func (e *BinaryExpr) Span() Span {
	if e == nil {
		return Span{}
	}
	return e.SourceSpan.Unpack()
}

// UnaryExpr represents a unary operation (e.g. not, unary minus).
type UnaryExpr struct {
	Op         string // "not", "-"
	Operand    Expr
	SourceSpan PackedSpan
}

func (e *UnaryExpr) exprNode() {}
func (e *UnaryExpr) Span() Span {
	if e == nil {
		return Span{}
	}
	return e.SourceSpan.Unpack()
}

// FuncCallExpr represents a function call: func(arg1, arg2, ...).
type FuncCallExpr struct {
	Name       string
	Args       []Expr
	SourceSpan PackedSpan
}

func (e *FuncCallExpr) exprNode() {}
func (e *FuncCallExpr) Span() Span {
	if e == nil {
		return Span{}
	}
	return e.SourceSpan.Unpack()
}

// StructExpr constructs a record value from ordered named fields.
type StructExpr struct {
	Fields     []StructField
	SourceSpan PackedSpan
}

func (e *StructExpr) exprNode() {}
func (e *StructExpr) Span() Span {
	if e == nil {
		return Span{}
	}
	return e.SourceSpan.Unpack()
}

// ListExpr constructs a list value from ordered element expressions.
type ListExpr struct {
	Elements   []Expr
	SourceSpan PackedSpan
}

func (e *ListExpr) exprNode() {}
func (e *ListExpr) Span() Span {
	if e == nil {
		return Span{}
	}
	return e.SourceSpan.Unpack()
}

// StructField is one named expression in a StructExpr.
type StructField struct {
	Name       string
	Expr       Expr
	SourceSpan PackedSpan
}

// IsNullExpr represents "col is null" or "col is not null".
type IsNullExpr struct {
	Operand    Expr
	Negated    bool // true = "is not null"
	SourceSpan PackedSpan
}

func (e *IsNullExpr) exprNode() {}
func (e *IsNullExpr) Span() Span {
	if e == nil {
		return Span{}
	}
	return e.SourceSpan.Unpack()
}

// Assignment represents "col = expr" in transform/reduce.
type Assignment struct {
	Column     string
	Expr       Expr
	SourceSpan PackedSpan
}

func (a *Assignment) Span() Span {
	if a == nil {
		return Span{}
	}
	return a.SourceSpan.Unpack()
}

// LoadOptions configures how a source file is loaded.
// Zero value keeps extension-based inference and CSV defaults (header row, comma delim).
type LoadOptions struct {
	Format              string // optional override: csv, json, jsonl, avro, parquet
	Compression         string // optional file-level compression wrapper: gzip, zstd, deflate
	Header              *bool  // csv only; nil = default (true)
	Delim               string // csv only; "" = comma
	AllowJaggedRows     *bool  // csv only; nil = default (false)
	IgnoreUnknownValues *bool  // csv only; nil = default (false)
	InferRows           *int   // csv/json/jsonl; nil = default (20480), -1 = all rows, 0 = csv all strings
	MaxBadRecords       *int   // csv/json/jsonl; nil = default (0)
}

// --- Operations (pipeline stages) ---

// Op represents a single operation in the pipeline.
type Op interface {
	opNode()
	Span() Span
}

// SourceOp represents the input file reference.
type SourceOp struct {
	Filename   string
	Load       LoadOptions
	SourceSpan PackedSpan
}

func (o *SourceOp) opNode() {}
func (o *SourceOp) Span() Span {
	if o == nil {
		return Span{}
	}
	return o.SourceSpan.Unpack()
}

// HeadOp returns the first N rows.
type HeadOp struct {
	N          int
	SourceSpan PackedSpan
}

func (o *HeadOp) opNode() {}
func (o *HeadOp) Span() Span {
	if o == nil {
		return Span{}
	}
	return o.SourceSpan.Unpack()
}

// TailOp returns the last N rows.
type TailOp struct {
	N          int
	SourceSpan PackedSpan
}

func (o *TailOp) opNode() {}
func (o *TailOp) Span() Span {
	if o == nil {
		return Span{}
	}
	return o.SourceSpan.Unpack()
}

// SortKey is one column to sort by, with a direction.
type SortKey struct {
	Path []string
	Desc bool
}

// SortOp sorts rows by an ordered list of keys.
type SortOp struct {
	Keys       []SortKey
	SourceSpan PackedSpan
}

func (o *SortOp) opNode() {}
func (o *SortOp) Span() Span {
	if o == nil {
		return Span{}
	}
	return o.SourceSpan.Unpack()
}

// SelectOp projects specific columns.
type SelectOp struct {
	Columns    [][]string
	SourceSpan PackedSpan
}

func (o *SelectOp) opNode() {}
func (o *SelectOp) Span() Span {
	if o == nil {
		return Span{}
	}
	return o.SourceSpan.Unpack()
}

// FilterOp filters rows by an expression.
type FilterOp struct {
	Expr       Expr
	SourceSpan PackedSpan
}

func (o *FilterOp) opNode() {}
func (o *FilterOp) Span() Span {
	if o == nil {
		return Span{}
	}
	return o.SourceSpan.Unpack()
}

// GroupOp groups rows by columns, nesting the rest.
type GroupOp struct {
	Columns    [][]string
	NestedName string // default "grouped"
	SourceSpan PackedSpan
}

func (o *GroupOp) opNode() {}
func (o *GroupOp) Span() Span {
	if o == nil {
		return Span{}
	}
	return o.SourceSpan.Unpack()
}

// TransformOp creates or overwrites columns with computed values.
type TransformOp struct {
	Assignments []Assignment
	SourceSpan  PackedSpan
}

func (o *TransformOp) opNode() {}
func (o *TransformOp) Span() Span {
	if o == nil {
		return Span{}
	}
	return o.SourceSpan.Unpack()
}

// ReduceOp aggregates over the nested table from a group.
type ReduceOp struct {
	NestedName  string // which nested column to reduce over; default "grouped"
	Assignments []Assignment
	SourceSpan  PackedSpan
}

func (o *ReduceOp) opNode() {}
func (o *ReduceOp) Span() Span {
	if o == nil {
		return Span{}
	}
	return o.SourceSpan.Unpack()
}

// CountOp returns a single-row table with the row count.
type CountOp struct {
	SourceSpan PackedSpan
}

func (o *CountOp) opNode() {}
func (o *CountOp) Span() Span {
	if o == nil {
		return Span{}
	}
	return o.SourceSpan.Unpack()
}

// DescribeOp returns table metadata: column name, storage type, and row count.
type DescribeOp struct {
	SourceSpan PackedSpan
}

func (o *DescribeOp) opNode() {}
func (o *DescribeOp) Span() Span {
	if o == nil {
		return Span{}
	}
	return o.SourceSpan.Unpack()
}

// DistinctOp deduplicates rows.
type DistinctOp struct {
	Columns    [][]string // empty = all columns
	SourceSpan PackedSpan
}

func (o *DistinctOp) opNode() {}
func (o *DistinctOp) Span() Span {
	if o == nil {
		return Span{}
	}
	return o.SourceSpan.Unpack()
}

// RenameOp renames columns.
type RenameOp struct {
	Pairs      []RenamePair
	SourceSpan PackedSpan
}

type RenamePair struct {
	Old string
	New string
}

func (o *RenameOp) opNode() {}
func (o *RenameOp) Span() Span {
	if o == nil {
		return Span{}
	}
	return o.SourceSpan.Unpack()
}

// RemoveOp removes columns.
type RemoveOp struct {
	Columns    [][]string
	SourceSpan PackedSpan
}

func (o *RemoveOp) opNode() {}
func (o *RemoveOp) Span() Span {
	if o == nil {
		return Span{}
	}
	return o.SourceSpan.Unpack()
}

// JoinKey is one join condition: left_path == right_path (Right == Left when omitted).
type JoinKey struct {
	Left  []string
	Right []string
}

// JoinOp joins the current table with another file.
type JoinOp struct {
	Kind       string // inner, left, right, full
	Filename   string
	Keys       []JoinKey
	Load       LoadOptions
	SourceSpan PackedSpan
}

func (o *JoinOp) opNode() {}
func (o *JoinOp) Span() Span {
	if o == nil {
		return Span{}
	}
	return o.SourceSpan.Unpack()
}

// OutputOptions configures a terminal output format command.
// Zero value keeps writer defaults.
type OutputOptions struct {
	SplitRows int // 0 = single file; >0 writes row-bounded output parts
	Overwrite bool
}

// OutputSpec represents the terminal output format stage.
type OutputSpec struct {
	Format  string        // "" = implicit table; table, csv, json, jsonl, avro, parquet
	Path    string        // empty = stdout
	Options OutputOptions // zero value = defaults
}

// Query represents a full parsed query: source + pipeline of operations.
type Query struct {
	Source *SourceOp
	Ops    []Op
	Output OutputSpec
}
