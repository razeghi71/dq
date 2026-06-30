package engine

func collectLogicalTypedExprColumns(expr logicalTypedExpr, out map[string]bool) {
	switch b := expr.bound.(type) {
	case *logicalBoundColumn:
		if len(b.rawPath) > 0 {
			out[b.rawPath[0]] = true
		}
	}
	if expr.left != nil {
		collectLogicalTypedExprColumns(*expr.left, out)
	}
	if expr.right != nil {
		collectLogicalTypedExprColumns(*expr.right, out)
	}
	if expr.operand != nil {
		collectLogicalTypedExprColumns(*expr.operand, out)
	}
	for i := range expr.args {
		collectLogicalTypedExprColumns(expr.args[i], out)
	}
	for i := range expr.fields {
		collectLogicalTypedExprColumns(expr.fields[i].expr, out)
	}
	for i := range expr.elements {
		collectLogicalTypedExprColumns(expr.elements[i], out)
	}
}
