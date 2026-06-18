package ast

import "testing"

func TestMergeSpans(t *testing.T) {
	cases := []struct {
		name string
		a    Span
		b    Span
		want Span
	}{
		{"both", Span{Start: 10, End: 15}, Span{Start: 20, End: 25}, Span{Start: 10, End: 25}},
		{"reverse_order", Span{Start: 20, End: 25}, Span{Start: 10, End: 15}, Span{Start: 10, End: 25}},
		{"overlap", Span{Start: 10, End: 20}, Span{Start: 15, End: 25}, Span{Start: 10, End: 25}},
		{"empty_left", Span{}, Span{Start: 3, End: 8}, Span{Start: 3, End: 8}},
		{"empty_right", Span{Start: 3, End: 8}, Span{}, Span{Start: 3, End: 8}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := MergeSpans(tc.a, tc.b); got != tc.want {
				t.Fatalf("MergeSpans(%v, %v): got %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

func TestExprSpanMethods(t *testing.T) {
	want := Span{Start: 4, End: 9}
	exprs := []struct {
		name string
		expr Expr
	}{
		{"literal", &LiteralExpr{SourceSpan: want.Pack()}},
		{"column", &ColumnExpr{SourceSpan: want.Pack()}},
		{"binary", &BinaryExpr{SourceSpan: want.Pack()}},
		{"unary", &UnaryExpr{SourceSpan: want.Pack()}},
		{"function", &FuncCallExpr{SourceSpan: want.Pack()}},
		{"struct", &StructExpr{SourceSpan: want.Pack()}},
		{"list", &ListExpr{SourceSpan: want.Pack()}},
		{"is_null", &IsNullExpr{SourceSpan: want.Pack()}},
	}

	for _, tc := range exprs {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.expr.Span(); got != want {
				t.Fatalf("%s Span(): got %v, want %v", tc.name, got, want)
			}
		})
	}
}

func TestExprSpanNilReceivers(t *testing.T) {
	var literal *LiteralExpr
	var column *ColumnExpr
	var binary *BinaryExpr
	var unary *UnaryExpr
	var call *FuncCallExpr
	var strct *StructExpr
	var list *ListExpr
	var isNull *IsNullExpr

	cases := []struct {
		name string
		got  Span
	}{
		{"literal", literal.Span()},
		{"column", column.Span()},
		{"binary", binary.Span()},
		{"unary", unary.Span()},
		{"function", call.Span()},
		{"struct", strct.Span()},
		{"list", list.Span()},
		{"is_null", isNull.Span()},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != (Span{}) {
				t.Fatalf("%s nil Span(): got %v, want zero span", tc.name, tc.got)
			}
		})
	}
}

func TestOpSpanMethods(t *testing.T) {
	want := Span{Start: 7, End: 19}
	ops := []struct {
		name string
		op   Op
	}{
		{"source", &SourceOp{SourceSpan: want.Pack()}},
		{"head", &HeadOp{SourceSpan: want.Pack()}},
		{"tail", &TailOp{SourceSpan: want.Pack()}},
		{"sort", &SortOp{SourceSpan: want.Pack()}},
		{"select", &SelectOp{SourceSpan: want.Pack()}},
		{"filter", &FilterOp{SourceSpan: want.Pack()}},
		{"group", &GroupOp{SourceSpan: want.Pack()}},
		{"transform", &TransformOp{SourceSpan: want.Pack()}},
		{"reduce", &ReduceOp{SourceSpan: want.Pack()}},
		{"count", &CountOp{SourceSpan: want.Pack()}},
		{"describe", &DescribeOp{SourceSpan: want.Pack()}},
		{"distinct", &DistinctOp{SourceSpan: want.Pack()}},
		{"rename", &RenameOp{SourceSpan: want.Pack()}},
		{"remove", &RemoveOp{SourceSpan: want.Pack()}},
		{"join", &JoinOp{SourceSpan: want.Pack()}},
	}

	for _, tc := range ops {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.op.Span(); got != want {
				t.Fatalf("%s Span(): got %v, want %v", tc.name, got, want)
			}
		})
	}
}

func TestOpSpanNilReceivers(t *testing.T) {
	var source *SourceOp
	var head *HeadOp
	var tail *TailOp
	var sort *SortOp
	var selectOp *SelectOp
	var filter *FilterOp
	var group *GroupOp
	var transform *TransformOp
	var reduce *ReduceOp
	var count *CountOp
	var describe *DescribeOp
	var distinct *DistinctOp
	var rename *RenameOp
	var remove *RemoveOp
	var join *JoinOp

	cases := []struct {
		name string
		got  Span
	}{
		{"source", source.Span()},
		{"head", head.Span()},
		{"tail", tail.Span()},
		{"sort", sort.Span()},
		{"select", selectOp.Span()},
		{"filter", filter.Span()},
		{"group", group.Span()},
		{"transform", transform.Span()},
		{"reduce", reduce.Span()},
		{"count", count.Span()},
		{"describe", describe.Span()},
		{"distinct", distinct.Span()},
		{"rename", rename.Span()},
		{"remove", remove.Span()},
		{"join", join.Span()},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != (Span{}) {
				t.Fatalf("%s nil Span(): got %v, want zero span", tc.name, tc.got)
			}
		})
	}
}

func TestAssignmentSpan(t *testing.T) {
	want := Span{Start: 2, End: 12}
	assignment := &Assignment{SourceSpan: want.Pack()}
	if got := assignment.Span(); got != want {
		t.Fatalf("Assignment Span(): got %v, want %v", got, want)
	}

	var nilAssignment *Assignment
	if got := nilAssignment.Span(); got != (Span{}) {
		t.Fatalf("nil Assignment Span(): got %v, want zero span", got)
	}
}
