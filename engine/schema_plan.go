package engine

import (
	"fmt"

	"github.com/razeghi71/dq/ast"
	"github.com/razeghi71/dq/table"
)

func inferExprSchema(expr ast.Expr, t *table.Table) *table.TypeDescriptor {
	switch e := expr.(type) {
	case *ast.LiteralExpr:
		switch e.Kind {
		case "int":
			return &table.TypeDescriptor{Kind: table.TypeInt}
		case "float":
			return &table.TypeDescriptor{Kind: table.TypeFloat}
		case "string":
			return &table.TypeDescriptor{Kind: table.TypeString}
		case "bool":
			return &table.TypeDescriptor{Kind: table.TypeBool}
		case "null":
			return &table.TypeDescriptor{Kind: table.TypeNull, Nullable: true}
		default:
			return nil
		}
	case *ast.ColumnExpr:
		return schemaForPath(t, e.Path)
	case *ast.BinaryExpr:
		return inferBinaryExprSchema(e, t)
	case *ast.UnaryExpr:
		return inferUnaryExprSchema(e, t)
	case *ast.FuncCallExpr:
		return inferFuncExprSchema(e, t)
	case *ast.StructExpr:
		fields := make([]table.FieldDescriptor, len(e.Fields))
		for i, field := range e.Fields {
			schema := inferExprSchema(field.Expr, t)
			if schema == nil {
				return nil
			}
			fields[i] = table.FieldDescriptor{Name: field.Name, Type: schema}
		}
		return &table.TypeDescriptor{Kind: table.TypeRecord, Fields: fields}
	case *ast.ListExpr:
		elems := make([]*table.TypeDescriptor, len(e.Elements))
		for i, elem := range e.Elements {
			schema := inferExprSchema(elem, t)
			if schema == nil {
				return nil
			}
			elems[i] = schema
		}
		return &table.TypeDescriptor{Kind: table.TypeList, Elem: table.UnifyListLiteralElems(elems)}
	case *ast.IsNullExpr:
		return &table.TypeDescriptor{Kind: table.TypeBool}
	default:
		return nil
	}
}

func plannedAssignmentSchema(expr ast.Expr, t *table.Table) *table.TypeDescriptor {
	schema := inferExprSchema(expr, t)
	if schema == nil {
		return nil
	}
	return table.FinalizeSchema(schema)
}

func inferBinaryExprSchema(e *ast.BinaryExpr, t *table.Table) *table.TypeDescriptor {
	switch e.Op {
	case "+", "-", "*", "/":
		left := inferExprSchema(e.Left, t)
		right := inferExprSchema(e.Right, t)
		if e.Op == "+" && table.IsStringLike(left) && table.IsStringLike(right) {
			out, err := table.UnifyStrict(left, right)
			if err != nil {
				return nil
			}
			return out
		}
		out, err := table.NumericResult(left, right)
		if err != nil {
			return nil
		}
		if e.Op == "/" {
			return plannedDivisionSchema(out, e.Right)
		}
		return out
	case "==", "!=":
		return &table.TypeDescriptor{Kind: table.TypeBool}
	case "<", ">", "<=", ">=":
		left := inferExprSchema(e.Left, t)
		right := inferExprSchema(e.Right, t)
		if left == nil || right == nil {
			return nil
		}
		return nullableSchema(table.TypeBool, left, right)
	case "and", "or":
		left := inferExprSchema(e.Left, t)
		right := inferExprSchema(e.Right, t)
		if left == nil || right == nil {
			return nil
		}
		return nullableSchema(table.TypeBool, left, right)
	default:
		return nil
	}
}

func inferUnaryExprSchema(e *ast.UnaryExpr, t *table.Table) *table.TypeDescriptor {
	switch e.Op {
	case "not":
		operand := inferExprSchema(e.Operand, t)
		if operand == nil {
			return nil
		}
		return nullableSchema(table.TypeBool, operand)
	case "-":
		schema := inferExprSchema(e.Operand, t)
		if table.IsNumeric(schema) || (schema != nil && schema.Kind == table.TypeNull) {
			return schema
		}
		return nil
	default:
		return nil
	}
}

func inferFuncExprSchema(e *ast.FuncCallExpr, t *table.Table) *table.TypeDescriptor {
	switch e.Name {
	case "upper", "lower", "trim":
		if len(e.Args) != 1 {
			return nil
		}
		return schemaForUnaryStringResult(e.Args[0], t)
	case "substr":
		if len(e.Args) != 3 {
			return nil
		}
		argSchemas, ok := inferArgSchemas(e.Args, t)
		if !ok {
			return nil
		}
		return nullableSchema(table.TypeString, argSchemas...)
	case "str_len", "list_len", "year", "month", "day":
		if len(e.Args) != 1 {
			return nil
		}
		argSchemas, ok := inferArgSchemas(e.Args, t)
		if !ok {
			return nil
		}
		return nullableSchema(table.TypeInt, argSchemas...)
	case "str_contains", "list_contains", "starts_with", "ends_with", "matches":
		if len(e.Args) != 2 {
			return nil
		}
		argSchemas, ok := inferArgSchemas(e.Args, t)
		if !ok {
			return nil
		}
		return nullableSchema(table.TypeBool, argSchemas...)
	case "coalesce":
		if len(e.Args) == 0 {
			return nil
		}
		schemas := make([]*table.TypeDescriptor, len(e.Args))
		for i, arg := range e.Args {
			schema := inferExprSchema(arg, t)
			if schema == nil {
				return nil
			}
			schemas[i] = schema
		}
		out, err := table.UnifyAllStrict(schemas)
		if err != nil {
			return nil
		}
		return out
	case "if":
		if len(e.Args) != 3 {
			return nil
		}
		thenSchema := inferExprSchema(e.Args[1], t)
		elseSchema := inferExprSchema(e.Args[2], t)
		if thenSchema == nil || elseSchema == nil {
			return nil
		}
		out, err := table.UnifyStrict(thenSchema, elseSchema)
		if err != nil {
			return nil
		}
		return out
	default:
		return nil
	}
}

func schemaForUnaryStringResult(arg ast.Expr, t *table.Table) *table.TypeDescriptor {
	argSchema := inferExprSchema(arg, t)
	out := &table.TypeDescriptor{Kind: table.TypeString}
	if argSchema != nil && argSchema.Nullable {
		out.Nullable = true
	}
	return out
}

func inferArgSchemas(args []ast.Expr, t *table.Table) ([]*table.TypeDescriptor, bool) {
	schemas := make([]*table.TypeDescriptor, len(args))
	for i, arg := range args {
		schema := inferExprSchema(arg, t)
		if schema == nil {
			return nil, false
		}
		schemas[i] = schema
	}
	return schemas, true
}

func nullableSchema(kind table.ValueType, inputs ...*table.TypeDescriptor) *table.TypeDescriptor {
	return &table.TypeDescriptor{Kind: kind, Nullable: anySchemaMayBeNull(inputs...)}
}

func anySchemaMayBeNull(schemas ...*table.TypeDescriptor) bool {
	for _, schema := range schemas {
		if schema != nil && (schema.Nullable || schema.Kind == table.TypeNull) {
			return true
		}
	}
	return false
}

func inferAggregateSchema(expr ast.Expr, nestedSchema *table.TypeDescriptor) *table.TypeDescriptor {
	switch e := expr.(type) {
	case *ast.FuncCallExpr:
		if validateAggregateFunctionArity(e) != nil {
			return nil
		}
		switch e.Name {
		case "count":
			return &table.TypeDescriptor{Kind: table.TypeInt}
		case "sum", "min", "max":
			argSchema := aggregateColumnArgSchema(e, nestedSchema)
			if !table.IsNumeric(argSchema) {
				return nil
			}
			return table.WithNullable(argSchema)
		case "avg":
			argSchema := aggregateColumnArgSchema(e, nestedSchema)
			if !table.IsNumeric(argSchema) {
				return nil
			}
			return &table.TypeDescriptor{Kind: table.TypeFloat, Nullable: true}
		case "first", "last":
			argSchema := aggregateColumnArgSchema(e, nestedSchema)
			if argSchema == nil {
				return nil
			}
			return table.WithNullable(argSchema)
		default:
			return nil
		}
	case *ast.BinaryExpr:
		switch e.Op {
		case "+", "-", "*", "/":
			left := inferAggregateSchema(e.Left, nestedSchema)
			right := inferAggregateSchema(e.Right, nestedSchema)
			out, err := table.NumericResult(left, right)
			if err != nil {
				return nil
			}
			if e.Op == "/" {
				return plannedDivisionSchema(out, e.Right)
			}
			return out
		case "==", "!=":
			return &table.TypeDescriptor{Kind: table.TypeBool}
		case "<", ">", "<=", ">=":
			left := inferAggregateSchema(e.Left, nestedSchema)
			right := inferAggregateSchema(e.Right, nestedSchema)
			if left == nil || right == nil {
				return nil
			}
			return nullableSchema(table.TypeBool, left, right)
		case "and", "or":
			left := inferAggregateSchema(e.Left, nestedSchema)
			right := inferAggregateSchema(e.Right, nestedSchema)
			if left == nil || right == nil {
				return nil
			}
			return nullableSchema(table.TypeBool, left, right)
		default:
			return nil
		}
	case *ast.UnaryExpr:
		switch e.Op {
		case "not":
			operand := inferAggregateSchema(e.Operand, nestedSchema)
			if operand == nil {
				return nil
			}
			return nullableSchema(table.TypeBool, operand)
		case "-":
			schema := inferAggregateSchema(e.Operand, nestedSchema)
			if table.IsNumeric(schema) || (schema != nil && schema.Kind == table.TypeNull) {
				return schema
			}
			return nil
		default:
			return nil
		}
	case *ast.LiteralExpr:
		return inferExprSchema(e, nil)
	case *ast.IsNullExpr:
		return &table.TypeDescriptor{Kind: table.TypeBool}
	default:
		return nil
	}
}

func plannedAggregateSchema(expr ast.Expr, nestedSchema *table.TypeDescriptor) *table.TypeDescriptor {
	schema := inferAggregateSchema(expr, nestedSchema)
	if schema == nil {
		return nil
	}
	return table.FinalizeSchema(schema)
}

func plannedDivisionSchema(out *table.TypeDescriptor, denominator ast.Expr) *table.TypeDescriptor {
	if out == nil {
		return nil
	}
	if out.Kind == table.TypeInt {
		// Runtime integer division can still produce float when not evenly
		// divisible, so planned division over ints must be float.
		out.Kind = table.TypeFloat
	}
	if !numericExprKnownNonZero(denominator) {
		out.Nullable = true
	}
	return out
}

func numericExprKnownNonZero(expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.LiteralExpr:
		switch e.Kind {
		case "int":
			return e.Int != 0
		case "float":
			return e.Float != 0
		default:
			return false
		}
	case *ast.UnaryExpr:
		if e.Op == "-" {
			return numericExprKnownNonZero(e.Operand)
		}
	}
	return false
}

func aggregateColumnArgSchema(e *ast.FuncCallExpr, nestedSchema *table.TypeDescriptor) *table.TypeDescriptor {
	if len(e.Args) != 1 {
		return nil
	}
	col, ok := e.Args[0].(*ast.ColumnExpr)
	if !ok {
		return nil
	}
	return table.SchemaAtPath(nestedSchema, col.Path)
}

func nestedRecordSchemaFromList(schema *table.TypeDescriptor) *table.TypeDescriptor {
	if schema == nil || schema.Kind != table.TypeList || schema.Elem == nil {
		return nil
	}
	elem := table.FinalizeSchema(schema.Elem)
	if elem.Kind != table.TypeRecord {
		return nil
	}
	return elem
}

func recordSchemaForTable(t *table.Table) *table.TypeDescriptor {
	fields := make([]table.FieldDescriptor, len(t.Columns))
	for i, name := range t.Columns {
		fields[i] = table.FieldDescriptor{Name: name, Type: t.Col(i).Schema()}
	}
	return &table.TypeDescriptor{Kind: table.TypeRecord, Fields: fields}
}

func allSchemasKnown(schemas []*table.TypeDescriptor, indexes []int) bool {
	for _, idx := range indexes {
		if idx < 0 || idx >= len(schemas) || schemas[idx] == nil {
			return false
		}
	}
	return true
}

func checkFilterExpr(expr ast.Expr, t *table.Table) error {
	if err := validateExprDoesNotTraverseUnion(expr, t); err != nil {
		return err
	}
	if err := validateUnionComparisons(expr, t); err != nil {
		return err
	}
	return validateFilterUnionUse(expr, t)
}

func checkTransformExpr(expr ast.Expr, t *table.Table) error {
	if err := validateExprDoesNotTraverseUnion(expr, t); err != nil {
		return err
	}
	if err := validateUnionComparisons(expr, t); err != nil {
		return err
	}
	return validateExprUnionUse(expr, t)
}

func checkReduceExpr(expr ast.Expr, nestedSchema *table.TypeDescriptor) error {
	if err := validateExprDoesNotTraverseUnionInSchema(expr, nestedSchema); err != nil {
		return err
	}
	if err := validateUnionComparisonsInSchema(expr, nestedSchema); err != nil {
		return err
	}
	return validateAggregateUnionUse(expr, nestedSchema)
}

func validateExprUnionUse(expr ast.Expr, t *table.Table) error {
	return validateUnionUseWith(expr, func(expr ast.Expr) *table.TypeDescriptor {
		return inferExprSchema(expr, t)
	}, false)
}

func validateAggregateUnionUse(expr ast.Expr, nestedSchema *table.TypeDescriptor) error {
	return validateUnionUseWith(expr, func(expr ast.Expr) *table.TypeDescriptor {
		return inferAggregateValueSchema(expr, nestedSchema)
	}, true)
}

func inferAggregateValueSchema(expr ast.Expr, nestedSchema *table.TypeDescriptor) *table.TypeDescriptor {
	if col, ok := expr.(*ast.ColumnExpr); ok {
		return table.SchemaAtPath(nestedSchema, col.Path)
	}
	return inferAggregateSchema(expr, nestedSchema)
}

func validateFilterUnionUse(expr ast.Expr, t *table.Table) error {
	if err := validateExprUnionUse(expr, t); err != nil {
		return err
	}
	schema := inferExprSchema(expr, t)
	if table.SchemaContainsUnion(schema) || (schema == nil && exprTreeContainsUnion(expr, func(expr ast.Expr) *table.TypeDescriptor {
		return inferExprSchema(expr, t)
	})) {
		return fmt.Errorf("cannot use union values as boolean")
	}
	return nil
}

func validateUnionUseWith(expr ast.Expr, schemaOf func(ast.Expr) *table.TypeDescriptor, aggregate bool) error {
	switch e := expr.(type) {
	case *ast.BinaryExpr:
		if err := validateUnionUseWith(e.Left, schemaOf, aggregate); err != nil {
			return err
		}
		if err := validateUnionUseWith(e.Right, schemaOf, aggregate); err != nil {
			return err
		}
		switch e.Op {
		case "+", "-", "*", "/":
			if exprContainsUnionBySchema(e.Left, schemaOf) || exprContainsUnionBySchema(e.Right, schemaOf) {
				return fmt.Errorf("cannot use union values in arithmetic")
			}
		case "and", "or":
			if exprContainsUnionBySchema(e.Left, schemaOf) || exprContainsUnionBySchema(e.Right, schemaOf) {
				return fmt.Errorf("cannot use union values as boolean")
			}
		}
	case *ast.UnaryExpr:
		if err := validateUnionUseWith(e.Operand, schemaOf, aggregate); err != nil {
			return err
		}
		switch e.Op {
		case "not":
			if exprContainsUnionBySchema(e.Operand, schemaOf) {
				return fmt.Errorf("cannot use union values as boolean")
			}
		case "-":
			if exprContainsUnionBySchema(e.Operand, schemaOf) {
				return fmt.Errorf("cannot use union values in arithmetic")
			}
		}
	case *ast.FuncCallExpr:
		if aggregate {
			if err := validateAggregateFunctionUnionUse(e, schemaOf); err != nil {
				return err
			}
			return nil
		}
		if err := validateFunctionUnionUse(e, schemaOf); err != nil {
			return err
		}
	case *ast.StructExpr:
		for _, field := range e.Fields {
			if err := validateUnionUseWith(field.Expr, schemaOf, aggregate); err != nil {
				return err
			}
		}
	case *ast.ListExpr:
		for _, elem := range e.Elements {
			if err := validateUnionUseWith(elem, schemaOf, aggregate); err != nil {
				return err
			}
		}
	case *ast.IsNullExpr:
		return validateUnionUseWith(e.Operand, schemaOf, aggregate)
	}
	return nil
}

func validateFunctionUnionUse(e *ast.FuncCallExpr, schemaOf func(ast.Expr) *table.TypeDescriptor) error {
	for _, arg := range e.Args {
		if err := validateUnionUseWith(arg, schemaOf, false); err != nil {
			return err
		}
	}
	switch e.Name {
	case "if":
		if len(e.Args) == 3 && exprContainsUnionBySchema(e.Args[0], schemaOf) {
			return fmt.Errorf("cannot use union values as boolean")
		}
		if schemaOf(e) == nil && exprTreeContainsUnion(e, schemaOf) {
			return fmt.Errorf("cannot use union values in incompatible expression")
		}
	case "coalesce":
		if schemaOf(e) == nil && exprTreeContainsUnion(e, schemaOf) {
			return fmt.Errorf("cannot use union values in incompatible expression")
		}
	case "list_len":
		if len(e.Args) == 1 {
			argSchema := schemaOf(e.Args[0])
			if argSchema != nil && argSchema.Kind == table.TypeList {
				return nil
			}
		}
		for _, arg := range e.Args {
			if exprContainsUnionBySchema(arg, schemaOf) {
				return fmt.Errorf("cannot use union values with %s()", e.Name)
			}
		}
	case "upper", "lower", "trim", "substr", "str_len", "year", "month", "day",
		"str_contains", "list_contains", "starts_with", "ends_with", "matches":
		for _, arg := range e.Args {
			if exprContainsUnionBySchema(arg, schemaOf) {
				return fmt.Errorf("cannot use union values with %s()", e.Name)
			}
		}
	}
	return nil
}

func validateAggregateFunctionUnionUse(e *ast.FuncCallExpr, schemaOf func(ast.Expr) *table.TypeDescriptor) error {
	if err := validateAggregateFunctionArity(e); err != nil {
		return err
	}
	for _, arg := range e.Args {
		if err := validateUnionUseWith(arg, schemaOf, true); err != nil {
			return err
		}
	}
	switch e.Name {
	case "sum", "avg", "min", "max":
		if len(e.Args) == 1 && exprContainsUnionBySchema(e.Args[0], schemaOf) {
			return fmt.Errorf("cannot use union values in aggregate %s()", e.Name)
		}
	case "first", "last", "count":
		return nil
	default:
		if schemaOf(e) == nil && exprTreeContainsUnion(e, schemaOf) {
			return fmt.Errorf("cannot use union values in incompatible expression")
		}
	}
	return nil
}

func exprContainsUnionBySchema(expr ast.Expr, schemaOf func(ast.Expr) *table.TypeDescriptor) bool {
	schema := schemaOf(expr)
	if table.SchemaContainsUnion(schema) {
		return true
	}
	return schema == nil && exprTreeContainsUnion(expr, schemaOf)
}
