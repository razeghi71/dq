package engine

import (
	"fmt"

	"github.com/razeghi71/dq/ast"
	"github.com/razeghi71/dq/table"
)

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

func nestedRecordSchemaForReduce(column string, schema *table.TypeDescriptor) (*table.TypeDescriptor, error) {
	schema = normalizePlanningSchema(schema)
	if schema == nil || schema.Kind == table.TypeNull {
		return nil, fmt.Errorf("reduce: nested column %q has unknown schema; expected list<record<...>>", column)
	}
	if schema.Kind != table.TypeList || schema.Elem == nil {
		return nil, fmt.Errorf("reduce: nested column %q must be list<record<...>>, got %s", column, table.Render(schema))
	}
	elem := finalizePlanningSchema(schema.Elem)
	if elem.Kind != table.TypeRecord {
		return nil, fmt.Errorf("reduce: nested column %q must contain record elements, got %s", column, table.Render(schema))
	}
	return elem, nil
}

func recordSchemaForEnv(env schemaEnv) *table.TypeDescriptor {
	fields := make([]table.FieldDescriptor, len(env.columns))
	for i, name := range env.columns {
		typ := env.rawSchema(i)
		if typ == nil {
			typ = env.finalSchema(i)
		}
		fields[i] = table.FieldDescriptor{Name: name, Type: typ}
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

func planLogicalFilterExprInEnv(expr ast.Expr, env schemaEnv) (logicalTypedExpr, error) {
	bound, err := bindLogicalExpressionInEnv(expr, env)
	if err != nil {
		return logicalTypedExpr{}, err
	}
	typed, err := typeCheckLogicalExpression(bound)
	if err != nil {
		return logicalTypedExpr{}, err
	}
	if !schemaBoolOrNull(typed.typ) {
		return logicalTypedExpr{}, fmt.Errorf("filter expression must return bool, got %s from %s", schemaString(typed.typ), logicalExpressionLabel(typed.bound))
	}
	return typed, nil
}

func planLogicalTransformExprInEnv(expr ast.Expr, env schemaEnv) (logicalTypedExpr, error) {
	bound, err := bindLogicalExpressionInEnv(expr, env)
	if err != nil {
		return logicalTypedExpr{}, err
	}
	return typeCheckLogicalExpression(bound)
}

func planLogicalReduceExpr(expr ast.Expr, nestedSchema *table.TypeDescriptor) (logicalTypedExpr, error) {
	bound, err := bindLogicalReduceExpression(expr, nestedSchema)
	if err != nil {
		return logicalTypedExpr{}, err
	}
	return typeCheckLogicalReduceExpression(bound)
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
