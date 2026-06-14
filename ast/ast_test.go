package ast

import "testing"

func TestASTNodesImplementInterfaces(t *testing.T) {
	exprs := []Expr{
		&LiteralExpr{},
		&ColumnExpr{},
		&BinaryExpr{},
		&UnaryExpr{},
		&FuncCallExpr{},
		&StructExpr{},
		&ListExpr{},
		&IsNullExpr{},
	}
	for _, expr := range exprs {
		expr.exprNode()
	}

	ops := []Op{
		&SourceOp{},
		&HeadOp{},
		&TailOp{},
		&SortOp{},
		&SelectOp{},
		&FilterOp{},
		&GroupOp{},
		&TransformOp{},
		&ReduceOp{},
		&CountOp{},
		&DistinctOp{},
		&RenameOp{},
		&RemoveOp{},
		&JoinOp{},
	}
	for _, op := range ops {
		op.opNode()
	}
}
