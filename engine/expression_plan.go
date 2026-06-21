package engine

import (
	"fmt"
	"strings"

	"github.com/razeghi71/dq/ast"
	"github.com/razeghi71/dq/table"
)

type boundExpr interface {
	boundExprNode()
}

type boundLiteral struct {
	raw *ast.LiteralExpr
}

type boundColumn struct {
	raw        *ast.ColumnExpr
	rawPath    []string
	topIndex   int
	nestedPath []string
	typ        *table.TypeDescriptor
}

type boundBinary struct {
	raw         *ast.BinaryExpr
	left, right boundExpr
}

type boundUnary struct {
	raw     *ast.UnaryExpr
	operand boundExpr
}

type boundCall struct {
	raw  *ast.FuncCallExpr
	args []boundExpr
}

type boundStructField struct {
	name string
	raw  ast.StructField
	expr boundExpr
}

type boundStruct struct {
	raw    *ast.StructExpr
	fields []boundStructField
}

type boundList struct {
	raw      *ast.ListExpr
	elements []boundExpr
}

type boundIsNull struct {
	raw     *ast.IsNullExpr
	operand boundExpr
}

type boundCoerce struct{}

func (*boundLiteral) boundExprNode() {}
func (*boundColumn) boundExprNode()  {}
func (*boundBinary) boundExprNode()  {}
func (*boundUnary) boundExprNode()   {}
func (*boundCall) boundExprNode()    {}
func (*boundStruct) boundExprNode()  {}
func (*boundList) boundExprNode()    {}
func (*boundIsNull) boundExprNode()  {}
func (*boundCoerce) boundExprNode()  {}

type typedExpr struct {
	bound    boundExpr
	raw      ast.Expr
	typ      *table.TypeDescriptor
	left     *typedExpr
	right    *typedExpr
	operand  *typedExpr
	args     []typedExpr
	fields   []typedStructField
	elements []typedExpr
	coerceTo *table.TypeDescriptor
}

type typedStructField struct {
	name string
	raw  ast.StructField
	expr typedExpr
}

type functionCategory int

const (
	scalarFunction functionCategory = iota
	specialFormFunction
	aggregateFunction
)

type functionSpec struct {
	name     string
	category functionCategory
	check    func(args []typedExpr) (*table.TypeDescriptor, error)
}

var scalarFunctionSpecs = map[string]functionSpec{
	"upper":        unaryStringSpec("upper"),
	"lower":        unaryStringSpec("lower"),
	"trim":         unaryStringSpec("trim"),
	"str_len":      unaryStringToIntSpec("str_len"),
	"year":         unaryStringToIntSpec("year"),
	"month":        unaryStringToIntSpec("month"),
	"day":          unaryStringToIntSpec("day"),
	"substr":       {name: "substr", category: scalarFunction, check: checkSubstrSignature},
	"str_contains": binaryStringToBoolSpec("str_contains", "substring"),
	"starts_with":  binaryStringToBoolSpec("starts_with", "prefix"),
	"ends_with":    binaryStringToBoolSpec("ends_with", "suffix"),
	"matches":      binaryStringToBoolSpec("matches", "regex"),
	"list_len":     {name: "list_len", category: scalarFunction, check: checkListLenSignature},
	"list_contains": {
		name:     "list_contains",
		category: scalarFunction,
		check:    checkListContainsSignature,
	},
	"coalesce": {name: "coalesce", category: specialFormFunction, check: checkCoalesceSignature},
	"if":       {name: "if", category: specialFormFunction, check: checkIfSignature},
	"count":    {name: "count", category: aggregateFunction},
	"sum":      {name: "sum", category: aggregateFunction},
	"avg":      {name: "avg", category: aggregateFunction},
	"min":      {name: "min", category: aggregateFunction},
	"max":      {name: "max", category: aggregateFunction},
	"first":    {name: "first", category: aggregateFunction},
	"last":     {name: "last", category: aggregateFunction},
}

func bindExpression(expr ast.Expr, t *table.Table) (boundExpr, error) {
	switch e := expr.(type) {
	case *ast.LiteralExpr:
		return &boundLiteral{raw: e}, nil
	case *ast.ColumnExpr:
		return bindColumnExpr(e, t)
	case *ast.BinaryExpr:
		left, err := bindExpression(e.Left, t)
		if err != nil {
			return nil, err
		}
		right, err := bindExpression(e.Right, t)
		if err != nil {
			return nil, err
		}
		return &boundBinary{raw: e, left: left, right: right}, nil
	case *ast.UnaryExpr:
		operand, err := bindExpression(e.Operand, t)
		if err != nil {
			return nil, err
		}
		return &boundUnary{raw: e, operand: operand}, nil
	case *ast.FuncCallExpr:
		args := make([]boundExpr, len(e.Args))
		for i, arg := range e.Args {
			bound, err := bindExpression(arg, t)
			if err != nil {
				return nil, err
			}
			args[i] = bound
		}
		return &boundCall{raw: e, args: args}, nil
	case *ast.StructExpr:
		fields := make([]boundStructField, len(e.Fields))
		for i, field := range e.Fields {
			bound, err := bindExpression(field.Expr, t)
			if err != nil {
				return nil, err
			}
			fields[i] = boundStructField{name: field.Name, raw: field, expr: bound}
		}
		return &boundStruct{raw: e, fields: fields}, nil
	case *ast.ListExpr:
		elements := make([]boundExpr, len(e.Elements))
		for i, elem := range e.Elements {
			bound, err := bindExpression(elem, t)
			if err != nil {
				return nil, err
			}
			elements[i] = bound
		}
		return &boundList{raw: e, elements: elements}, nil
	case *ast.IsNullExpr:
		operand, err := bindExpression(e.Operand, t)
		if err != nil {
			return nil, err
		}
		return &boundIsNull{raw: e, operand: operand}, nil
	default:
		return nil, fmt.Errorf("unknown expression type %T", expr)
	}
}

func bindColumnExpr(e *ast.ColumnExpr, t *table.Table) (*boundColumn, error) {
	return bindColumnPath(t, e.Path, e)
}

func bindColumnPath(t *table.Table, path []string, raw *ast.ColumnExpr) (*boundColumn, error) {
	if t == nil || len(path) == 0 {
		return nil, fmt.Errorf("empty column path")
	}
	idx := t.ColIndex(path[0])
	if idx < 0 {
		return nil, columnNotFoundError(path[0], t)
	}
	col := t.Col(idx)
	typ := col.Schema()
	if len(path) == 1 && col.ColType() == table.TypeNull {
		typ = &table.TypeDescriptor{Kind: table.TypeNull, Nullable: true}
	}
	parentNullable := false
	for _, seg := range path[1:] {
		if typ == nil {
			return nil, fmt.Errorf("field %q not found in column %q: parent schema is unknown", seg, strings.Join(path, "."))
		}
		if typ.Nullable || typ.Kind == table.TypeNull {
			parentNullable = true
		}
		current := table.FinalizeSchema(typ)
		switch current.Kind {
		case table.TypeUnion:
			return nil, unionPathTraversalError(path, current)
		case table.TypeList:
			return nil, fmt.Errorf("cannot access field %q through list column %q of type %s", seg, strings.Join(path, "."), current.String())
		case table.TypeRecord:
			var next *table.TypeDescriptor
			for _, field := range current.Fields {
				if field.Name == seg {
					next = field.Type
					break
				}
			}
			if next == nil {
				return nil, fmt.Errorf("field %q not found in column %q of type %s", seg, path[0], current.String())
			}
			typ = next
		default:
			return nil, fmt.Errorf("cannot access field %q in column %q of type %s", seg, path[0], current.String())
		}
	}
	typ = table.NormalizeSchema(typ)
	if parentNullable {
		typ = table.WithNullable(typ)
	}
	return &boundColumn{
		raw:        raw,
		rawPath:    append([]string(nil), path...),
		topIndex:   idx,
		nestedPath: append([]string(nil), path[1:]...),
		typ:        typ,
	}, nil
}

func bindReduceExpression(expr ast.Expr, nestedSchema *table.TypeDescriptor) (boundExpr, error) {
	switch e := expr.(type) {
	case *ast.LiteralExpr:
		return &boundLiteral{raw: e}, nil
	case *ast.ColumnExpr:
		return nil, fmt.Errorf("column %q cannot be used directly in reduce; use an aggregate such as first(%s)", strings.Join(e.Path, "."), strings.Join(e.Path, "."))
	case *ast.BinaryExpr:
		left, err := bindReduceExpression(e.Left, nestedSchema)
		if err != nil {
			return nil, err
		}
		right, err := bindReduceExpression(e.Right, nestedSchema)
		if err != nil {
			return nil, err
		}
		return &boundBinary{raw: e, left: left, right: right}, nil
	case *ast.UnaryExpr:
		operand, err := bindReduceExpression(e.Operand, nestedSchema)
		if err != nil {
			return nil, err
		}
		return &boundUnary{raw: e, operand: operand}, nil
	case *ast.FuncCallExpr:
		spec, ok := scalarFunctionSpecs[e.Name]
		if !ok {
			return nil, fmt.Errorf("unknown function %q", e.Name)
		}
		if spec.category != aggregateFunction {
			return nil, fmt.Errorf("non-aggregate function %q in reduce context", e.Name)
		}
		args, err := bindAggregateArgs(e, nestedSchema)
		if err != nil {
			return nil, err
		}
		return &boundCall{raw: e, args: args}, nil
	case *ast.StructExpr:
		return nil, fmt.Errorf("struct constructor is not supported in reduce")
	case *ast.ListExpr:
		return nil, fmt.Errorf("list constructor is not supported in reduce")
	case *ast.IsNullExpr:
		operand, err := bindReduceExpression(e.Operand, nestedSchema)
		if err != nil {
			return nil, err
		}
		return &boundIsNull{raw: e, operand: operand}, nil
	default:
		return nil, fmt.Errorf("unknown expression type %T", expr)
	}
}

func bindAggregateArgs(e *ast.FuncCallExpr, nestedSchema *table.TypeDescriptor) ([]boundExpr, error) {
	if err := validateAggregateFunctionArity(e); err != nil {
		return nil, err
	}
	if e.Name == "count" {
		return nil, nil
	}
	if len(e.Args) != 1 {
		return nil, fmt.Errorf("%s() takes 1 argument, got %d", e.Name, len(e.Args))
	}
	col, ok := e.Args[0].(*ast.ColumnExpr)
	if !ok {
		return nil, fmt.Errorf("%s() argument must be a column reference", e.Name)
	}
	bound, err := bindAggregateColumnPath(nestedSchema, col.Path, col)
	if err != nil {
		return nil, fmt.Errorf("%s(%s): %w", e.Name, strings.Join(col.Path, "."), err)
	}
	return []boundExpr{bound}, nil
}

func bindAggregateColumnPath(nestedSchema *table.TypeDescriptor, path []string, raw *ast.ColumnExpr) (*boundColumn, error) {
	t, err := tableForRecordSchema(nestedSchema)
	if err != nil {
		return nil, err
	}
	return bindColumnPath(t, path, raw)
}

func tableForRecordSchema(schema *table.TypeDescriptor) (*table.Table, error) {
	if schema == nil {
		return nil, fmt.Errorf("nested row schema is unknown")
	}
	rec := table.FinalizeSchema(schema)
	if rec.Kind != table.TypeRecord {
		return nil, fmt.Errorf("nested row schema must be a record, got %s", rec.String())
	}
	cols := make([]string, len(rec.Fields))
	schemas := make([]*table.TypeDescriptor, len(rec.Fields))
	for i, field := range rec.Fields {
		cols[i] = field.Name
		schemas[i] = field.Type
	}
	return table.NewTableWithSchemas(cols, schemas), nil
}

func columnNotFoundError(name string, t *table.Table) error {
	msg := fmt.Sprintf("column %q not found", name)
	if t != nil && len(t.Columns) > 0 {
		msg += "; available columns: " + strings.Join(t.Columns, ", ")
		if hint := closestColumnName(name, t.Columns); hint != "" {
			msg += fmt.Sprintf("; hint: did you mean %q?", hint)
		}
	}
	return fmt.Errorf("%s", msg)
}

func closestColumnName(name string, columns []string) string {
	best := ""
	bestDist := 4
	for _, col := range columns {
		dist := levenshtein(name, col)
		if dist < bestDist {
			bestDist = dist
			best = col
		}
	}
	return best
}

func levenshtein(a, b string) int {
	ar := []rune(a)
	br := []rune(b)
	prev := make([]int, len(br)+1)
	for j := range prev {
		prev[j] = j
	}
	for i, ra := range ar {
		cur := make([]int, len(br)+1)
		cur[0] = i + 1
		for j, rb := range br {
			cost := 0
			if ra != rb {
				cost = 1
			}
			cur[j+1] = minInt(cur[j]+1, prev[j+1]+1, prev[j]+cost)
		}
		prev = cur
	}
	return prev[len(br)]
}

func minInt(values ...int) int {
	out := values[0]
	for _, v := range values[1:] {
		if v < out {
			out = v
		}
	}
	return out
}

func typeCheckExpression(expr boundExpr) (typedExpr, error) {
	switch e := expr.(type) {
	case *boundLiteral:
		return typedExpr{bound: expr, raw: e.raw, typ: literalType(e.raw)}, nil
	case *boundColumn:
		return typedExpr{bound: expr, raw: e.raw, typ: table.NormalizeSchema(e.typ)}, nil
	case *boundBinary:
		left, err := typeCheckExpression(e.left)
		if err != nil {
			return typedExpr{}, err
		}
		right, err := typeCheckExpression(e.right)
		if err != nil {
			return typedExpr{}, err
		}
		typ, err := checkBinarySignature(e.raw.Op, left, right)
		if err != nil {
			return typedExpr{}, err
		}
		return typedExpr{bound: expr, raw: e.raw, typ: typ, left: &left, right: &right}, nil
	case *boundUnary:
		operand, err := typeCheckExpression(e.operand)
		if err != nil {
			return typedExpr{}, err
		}
		typ, err := checkUnarySignature(e.raw.Op, operand)
		if err != nil {
			return typedExpr{}, err
		}
		return typedExpr{bound: expr, raw: e.raw, typ: typ, operand: &operand}, nil
	case *boundCall:
		args := make([]typedExpr, len(e.args))
		for i, arg := range e.args {
			typed, err := typeCheckExpression(arg)
			if err != nil {
				return typedExpr{}, err
			}
			args[i] = typed
		}
		spec, ok := scalarFunctionSpecs[e.raw.Name]
		if !ok {
			return typedExpr{}, fmt.Errorf("unknown function %q", e.raw.Name)
		}
		if spec.category == aggregateFunction {
			return typedExpr{}, fmt.Errorf("aggregate function %q can only be used inside 'reduce'", e.raw.Name)
		}
		typ, err := spec.check(args)
		if err != nil {
			return typedExpr{}, err
		}
		out := typedExpr{bound: expr, raw: e.raw, typ: typ, args: args}
		switch e.raw.Name {
		case "coalesce":
			if typedArgsNeedCoercion(args, typ) {
				out = coerceTypedExpression(out, typ)
			}
		case "if":
			if len(args) == 3 && (runtimeCoercionNeeded(args[1].typ, typ) || runtimeCoercionNeeded(args[2].typ, typ)) {
				out = coerceTypedExpression(out, typ)
			}
		}
		return out, nil
	case *boundStruct:
		seen := make(map[string]bool, len(e.fields))
		fields := make([]table.FieldDescriptor, len(e.fields))
		typedFields := make([]typedStructField, len(e.fields))
		for i, field := range e.fields {
			if seen[field.name] {
				return typedExpr{}, fmt.Errorf("struct() duplicate field %q", field.name)
			}
			seen[field.name] = true
			typed, err := typeCheckExpression(field.expr)
			if err != nil {
				return typedExpr{}, err
			}
			fields[i] = table.FieldDescriptor{Name: field.name, Type: typed.typ}
			typedFields[i] = typedStructField{name: field.name, raw: field.raw, expr: typed}
		}
		return typedExpr{bound: expr, raw: e.raw, typ: &table.TypeDescriptor{Kind: table.TypeRecord, Fields: fields}, fields: typedFields}, nil
	case *boundList:
		elems := make([]*table.TypeDescriptor, len(e.elements))
		typedElems := make([]typedExpr, len(e.elements))
		for i, elem := range e.elements {
			typed, err := typeCheckExpression(elem)
			if err != nil {
				return typedExpr{}, err
			}
			elems[i] = typed.typ
			typedElems[i] = typed
		}
		elemSchema := table.UnifyListLiteralElems(elems)
		out := typedExpr{bound: expr, raw: e.raw, typ: &table.TypeDescriptor{Kind: table.TypeList, Elem: elemSchema}, elements: typedElems}
		if typedArgsNeedCoercion(typedElems, elemSchema) {
			out = coerceTypedExpression(out, out.typ)
		}
		return out, nil
	case *boundIsNull:
		operand, err := typeCheckExpression(e.operand)
		if err != nil {
			return typedExpr{}, err
		}
		return typedExpr{bound: expr, raw: e.raw, typ: &table.TypeDescriptor{Kind: table.TypeBool}, operand: &operand}, nil
	default:
		return typedExpr{}, fmt.Errorf("unknown bound expression type %T", expr)
	}
}

func typeCheckReduceExpression(expr boundExpr) (typedExpr, error) {
	switch e := expr.(type) {
	case *boundLiteral:
		return typedExpr{bound: expr, raw: e.raw, typ: literalType(e.raw)}, nil
	case *boundColumn:
		return typedExpr{bound: expr, raw: e.raw, typ: table.NormalizeSchema(e.typ)}, nil
	case *boundBinary:
		left, err := typeCheckReduceExpression(e.left)
		if err != nil {
			return typedExpr{}, err
		}
		right, err := typeCheckReduceExpression(e.right)
		if err != nil {
			return typedExpr{}, err
		}
		typ, err := checkBinarySignature(e.raw.Op, left, right)
		if err != nil {
			return typedExpr{}, err
		}
		return typedExpr{bound: expr, raw: e.raw, typ: typ, left: &left, right: &right}, nil
	case *boundUnary:
		operand, err := typeCheckReduceExpression(e.operand)
		if err != nil {
			return typedExpr{}, err
		}
		typ, err := checkUnarySignature(e.raw.Op, operand)
		if err != nil {
			return typedExpr{}, err
		}
		return typedExpr{bound: expr, raw: e.raw, typ: typ, operand: &operand}, nil
	case *boundCall:
		args := make([]typedExpr, len(e.args))
		for i, arg := range e.args {
			typed, err := typeCheckReduceExpression(arg)
			if err != nil {
				return typedExpr{}, err
			}
			args[i] = typed
		}
		spec, ok := scalarFunctionSpecs[e.raw.Name]
		if !ok {
			return typedExpr{}, fmt.Errorf("unknown function %q", e.raw.Name)
		}
		if spec.category != aggregateFunction {
			return typedExpr{}, fmt.Errorf("non-aggregate function %q in reduce context", e.raw.Name)
		}
		typ, err := checkAggregateSignature(e.raw.Name, args)
		if err != nil {
			return typedExpr{}, err
		}
		return typedExpr{bound: expr, raw: e.raw, typ: typ, args: args}, nil
	case *boundStruct:
		return typedExpr{}, fmt.Errorf("struct constructor is not supported in reduce")
	case *boundList:
		return typedExpr{}, fmt.Errorf("list constructor is not supported in reduce")
	case *boundIsNull:
		operand, err := typeCheckReduceExpression(e.operand)
		if err != nil {
			return typedExpr{}, err
		}
		return typedExpr{bound: expr, raw: e.raw, typ: &table.TypeDescriptor{Kind: table.TypeBool}, operand: &operand}, nil
	default:
		return typedExpr{}, fmt.Errorf("unknown bound expression type %T", expr)
	}
}

func literalType(e *ast.LiteralExpr) *table.TypeDescriptor {
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
}

func checkBinarySignature(op string, left, right typedExpr) (*table.TypeDescriptor, error) {
	switch op {
	case "+", "-", "*", "/":
		if op == "+" && schemaKindOrNull(left.typ, table.TypeString) && schemaKindOrNull(right.typ, table.TypeString) {
			out, err := unifyExpressionStrict(left.typ, right.typ)
			if err != nil {
				return nil, fmt.Errorf("operator + requires both operands to be numeric or string, got %s and %s", schemaString(left.typ), schemaString(right.typ))
			}
			return out, nil
		}
		out, err := table.NumericResult(left.typ, right.typ)
		if err != nil {
			return nil, fmt.Errorf("operator %s requires numeric operands, got %s and %s", op, schemaString(left.typ), schemaString(right.typ))
		}
		if op == "/" {
			return plannedDivisionSchema(out, right.raw), nil
		}
		return out, nil
	case "==", "!=":
		if isNullOnly(left.typ) || isNullOnly(right.typ) {
			return nil, fmt.Errorf("%s null is not comparison syntax; use is null or is not null", op)
		}
		if table.SchemaContainsUnion(left.typ) || table.SchemaContainsUnion(right.typ) {
			return nil, fmt.Errorf("cannot compare union values")
		}
		if !table.IsComparable(table.FinalizeSchema(left.typ)) || !table.IsComparable(table.FinalizeSchema(right.typ)) {
			return nil, fmt.Errorf("cannot compare %s with %s", schemaTypeName(left.typ), schemaTypeName(right.typ))
		}
		if _, err := unifyExpressionStrict(left.typ, right.typ); err != nil {
			return nil, fmt.Errorf("cannot compare %s with %s", schemaTypeName(left.typ), schemaTypeName(right.typ))
		}
		return nullableSchema(table.TypeBool, left.typ, right.typ), nil
	case "<", ">", "<=", ">=":
		if table.SchemaContainsUnion(left.typ) || table.SchemaContainsUnion(right.typ) {
			return nil, fmt.Errorf("cannot compare union values")
		}
		if !schemaOrderableOrNull(left.typ) || !schemaOrderableOrNull(right.typ) {
			return nil, fmt.Errorf("cannot compare %s with %s", schemaTypeName(left.typ), schemaTypeName(right.typ))
		}
		if _, err := unifyExpressionStrict(left.typ, right.typ); err != nil {
			return nil, fmt.Errorf("cannot compare %s with %s", schemaTypeName(left.typ), schemaTypeName(right.typ))
		}
		return nullableSchema(table.TypeBool, left.typ, right.typ), nil
	case "and", "or":
		if !schemaBoolOrNull(left.typ) || !schemaBoolOrNull(right.typ) {
			return nil, fmt.Errorf("'%s' requires boolean operands, got %s and %s", op, schemaString(left.typ), schemaString(right.typ))
		}
		return nullableSchema(table.TypeBool, left.typ, right.typ), nil
	default:
		return nil, fmt.Errorf("unknown operator %q", op)
	}
}

func checkUnarySignature(op string, operand typedExpr) (*table.TypeDescriptor, error) {
	switch op {
	case "not":
		if !schemaBoolOrNull(operand.typ) {
			return nil, fmt.Errorf("'not' requires boolean operand, got %s", schemaString(operand.typ))
		}
		return nullableSchema(table.TypeBool, operand.typ), nil
	case "-":
		if isNullOnly(operand.typ) {
			return &table.TypeDescriptor{Kind: table.TypeNull, Nullable: true}, nil
		}
		if !table.IsNumeric(operand.typ) {
			return nil, fmt.Errorf("operator - requires numeric operand, got %s", schemaString(operand.typ))
		}
		return table.NormalizeSchema(operand.typ), nil
	default:
		return nil, fmt.Errorf("unknown unary operator %q", op)
	}
}

func unaryStringSpec(name string) functionSpec {
	return functionSpec{name: name, category: scalarFunction, check: func(args []typedExpr) (*table.TypeDescriptor, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("%s() takes 1 argument, got %d", name, len(args))
		}
		if !schemaKindOrNull(args[0].typ, table.TypeString) {
			return nil, fmt.Errorf("%s() requires a string, got %s", name, schemaString(args[0].typ))
		}
		return nullableSchema(table.TypeString, args[0].typ), nil
	}}
}

func unaryStringToIntSpec(name string) functionSpec {
	return functionSpec{name: name, category: scalarFunction, check: func(args []typedExpr) (*table.TypeDescriptor, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("%s() takes 1 argument, got %d", name, len(args))
		}
		if !schemaKindOrNull(args[0].typ, table.TypeString) {
			return nil, fmt.Errorf("%s() requires a string, got %s", name, schemaString(args[0].typ))
		}
		return nullableSchema(table.TypeInt, args[0].typ), nil
	}}
}

func binaryStringToBoolSpec(name, secondArgLabel string) functionSpec {
	return functionSpec{name: name, category: scalarFunction, check: func(args []typedExpr) (*table.TypeDescriptor, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("%s() takes 2 arguments, got %d", name, len(args))
		}
		if !schemaKindOrNull(args[0].typ, table.TypeString) {
			return nil, fmt.Errorf("%s() requires a string, got %s", name, schemaString(args[0].typ))
		}
		if !schemaKindOrNull(args[1].typ, table.TypeString) {
			return nil, fmt.Errorf("%s() requires a string %s, got %s", name, secondArgLabel, schemaString(args[1].typ))
		}
		return nullableSchema(table.TypeBool, args[0].typ, args[1].typ), nil
	}}
}

func checkSubstrSignature(args []typedExpr) (*table.TypeDescriptor, error) {
	if len(args) != 3 {
		return nil, fmt.Errorf("substr() takes 3 arguments (string, start, length), got %d", len(args))
	}
	if !schemaKindOrNull(args[0].typ, table.TypeString) {
		return nil, fmt.Errorf("substr() requires a string, got %s", schemaString(args[0].typ))
	}
	for i := 1; i < 3; i++ {
		if !schemaKindOrNull(args[i].typ, table.TypeInt) {
			if i == 1 {
				return nil, fmt.Errorf("substr: start must be an int, got %s", schemaString(args[i].typ))
			}
			return nil, fmt.Errorf("substr: length must be an int, got %s", schemaString(args[i].typ))
		}
	}
	return nullableSchema(table.TypeString, args[0].typ, args[1].typ, args[2].typ), nil
}

func checkListLenSignature(args []typedExpr) (*table.TypeDescriptor, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("list_len() takes 1 argument, got %d", len(args))
	}
	if !schemaKindOrNull(args[0].typ, table.TypeList) {
		return nil, fmt.Errorf("list_len() requires a list, got %s", schemaString(args[0].typ))
	}
	return nullableSchema(table.TypeInt, args[0].typ), nil
}

func checkListContainsSignature(args []typedExpr) (*table.TypeDescriptor, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("list_contains() takes 2 arguments (list, element), got %d", len(args))
	}
	if !schemaKindOrNull(args[0].typ, table.TypeList) {
		return nil, fmt.Errorf("list_contains() requires a list, got %s", schemaString(args[0].typ))
	}
	listSchema := table.NormalizeSchema(args[0].typ)
	if listSchema.Kind == table.TypeList && listSchema.Elem != nil && listSchema.Elem.Kind != table.TypeMixed && !isNullOnly(args[1].typ) {
		if _, err := unifyExpressionStrict(listSchema.Elem, args[1].typ); err != nil {
			return nil, fmt.Errorf("list_contains() element type mismatch: list has %s, got %s", schemaString(listSchema.Elem), schemaString(args[1].typ))
		}
	}
	return nullableSchema(table.TypeBool, args[0].typ, args[1].typ), nil
}

func checkAggregateSignature(name string, args []typedExpr) (*table.TypeDescriptor, error) {
	switch name {
	case "count":
		if len(args) != 0 {
			return nil, fmt.Errorf("count() takes no arguments, got %d", len(args))
		}
		return &table.TypeDescriptor{Kind: table.TypeInt}, nil
	case "sum":
		if len(args) != 1 {
			return nil, fmt.Errorf("%s() takes 1 argument, got %d", name, len(args))
		}
		if !schemaNumericOrNull(args[0].typ) {
			return nil, fmt.Errorf("%s() requires a numeric column, got %s", name, schemaString(args[0].typ))
		}
		return table.WithNullable(args[0].typ), nil
	case "min", "max":
		if len(args) != 1 {
			return nil, fmt.Errorf("%s() takes 1 argument, got %d", name, len(args))
		}
		if !schemaOrderableOrNull(args[0].typ) {
			return nil, fmt.Errorf("%s() requires an orderable column, got %s", name, schemaString(args[0].typ))
		}
		return table.WithNullable(args[0].typ), nil
	case "avg":
		if len(args) != 1 {
			return nil, fmt.Errorf("avg() takes 1 argument, got %d", len(args))
		}
		if !schemaNumericOrNull(args[0].typ) {
			return nil, fmt.Errorf("avg() requires a numeric column, got %s", schemaString(args[0].typ))
		}
		return &table.TypeDescriptor{Kind: table.TypeFloat, Nullable: true}, nil
	case "first", "last":
		if len(args) != 1 {
			return nil, fmt.Errorf("%s() takes 1 argument, got %d", name, len(args))
		}
		return table.WithNullable(args[0].typ), nil
	default:
		return nil, fmt.Errorf("unknown aggregate function %q", name)
	}
}

func checkCoalesceSignature(args []typedExpr) (*table.TypeDescriptor, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("coalesce() requires at least 1 argument")
	}
	out := args[0].typ
	nullable := schemaMayBeNull(args[0].typ)
	for i := 1; i < len(args); i++ {
		merged, err := unifyExpressionStrict(out, args[i].typ)
		if err != nil {
			return nil, fmt.Errorf("coalesce() arguments do not have one common type: argument 1 has type %s, argument %d has type %s", schemaString(out), i+1, schemaString(args[i].typ))
		}
		out = merged
		nullable = nullable && schemaMayBeNull(args[i].typ)
	}
	if out != nil {
		if nullable {
			out = table.WithNullable(out)
		} else {
			out = table.WithoutNull(out)
		}
	}
	return out, nil
}

func checkIfSignature(args []typedExpr) (*table.TypeDescriptor, error) {
	if len(args) != 3 {
		return nil, fmt.Errorf("if() takes 3 arguments (condition, then, else), got %d", len(args))
	}
	if !schemaBoolOrNull(args[0].typ) {
		return nil, fmt.Errorf("if() condition must be boolean, got %s", schemaString(args[0].typ))
	}
	out, err := unifyExpressionStrict(args[1].typ, args[2].typ)
	if err != nil {
		return nil, fmt.Errorf("if() branches do not have one common type: then branch has type %s, else branch has type %s", schemaString(args[1].typ), schemaString(args[2].typ))
	}
	return out, nil
}

func unifyExpressionStrict(a, b *table.TypeDescriptor) (*table.TypeDescriptor, error) {
	aHadMixed := schemaContainsMixed(a)
	bHadMixed := schemaContainsMixed(b)
	out, err := table.UnifyStrict(a, b)
	if err != nil {
		return nil, err
	}
	if schemaContainsMixed(out) && !aHadMixed && !bHadMixed {
		return nil, fmt.Errorf("strict expression unification produced mixed from %s and %s", schemaString(a), schemaString(b))
	}
	return out, nil
}

func coerceTypedExpression(expr typedExpr, target *table.TypeDescriptor) typedExpr {
	target = table.FinalizeSchema(target)
	child := expr
	return typedExpr{
		bound:    &boundCoerce{},
		raw:      expr.raw,
		typ:      target,
		operand:  &child,
		coerceTo: target,
	}
}

func typedArgsNeedCoercion(args []typedExpr, target *table.TypeDescriptor) bool {
	for _, arg := range args {
		if runtimeCoercionNeeded(arg.typ, target) {
			return true
		}
	}
	return false
}

func runtimeCoercionNeeded(from, target *table.TypeDescriptor) bool {
	if from == nil || target == nil || isNullOnly(from) {
		return false
	}
	return !sameRuntimeSchema(table.FinalizeSchema(from), table.FinalizeSchema(target))
}

func sameRuntimeSchema(a, b *table.TypeDescriptor) bool {
	if a == nil || b == nil || a.Kind == table.TypeNull || b.Kind == table.TypeNull {
		return true
	}
	if a.Kind != b.Kind {
		return false
	}
	switch a.Kind {
	case table.TypeList:
		return sameRuntimeSchema(a.Elem, b.Elem)
	case table.TypeRecord:
		if len(a.Fields) != len(b.Fields) {
			return false
		}
		fields := make(map[string]*table.TypeDescriptor, len(a.Fields))
		for i := range a.Fields {
			fields[a.Fields[i].Name] = a.Fields[i].Type
		}
		for i := range b.Fields {
			left, ok := fields[b.Fields[i].Name]
			if !ok || !sameRuntimeSchema(left, b.Fields[i].Type) {
				return false
			}
		}
		return true
	case table.TypeUnion:
		if len(a.Branches) != len(b.Branches) {
			return false
		}
		for i := range a.Branches {
			if !sameRuntimeSchema(a.Branches[i], b.Branches[i]) {
				return false
			}
		}
	}
	return true
}

func schemaContainsMixed(schema *table.TypeDescriptor) bool {
	if schema == nil {
		return false
	}
	switch schema.Kind {
	case table.TypeMixed:
		return true
	case table.TypeRecord:
		for _, field := range schema.Fields {
			if schemaContainsMixed(field.Type) {
				return true
			}
		}
	case table.TypeList:
		return schemaContainsMixed(schema.Elem)
	case table.TypeUnion:
		for _, branch := range schema.Branches {
			if schemaContainsMixed(branch) {
				return true
			}
		}
	}
	return false
}

func schemaKindOrNull(schema *table.TypeDescriptor, kind table.ValueType) bool {
	if schema == nil {
		return false
	}
	return schema.Kind == kind || schema.Kind == table.TypeNull
}

func schemaBoolOrNull(schema *table.TypeDescriptor) bool {
	return schemaKindOrNull(schema, table.TypeBool)
}

func schemaNumericOrNull(schema *table.TypeDescriptor) bool {
	return schemaKindOrNull(schema, table.TypeInt) || schemaKindOrNull(schema, table.TypeFloat)
}

func schemaOrderableOrNull(schema *table.TypeDescriptor) bool {
	if schema == nil {
		return false
	}
	return schema.Kind == table.TypeNull || table.IsOrderable(table.FinalizeSchema(schema))
}

func isNullOnly(schema *table.TypeDescriptor) bool {
	return schema != nil && schema.Kind == table.TypeNull
}

func schemaMayBeNull(schema *table.TypeDescriptor) bool {
	return schema != nil && (schema.Nullable || schema.Kind == table.TypeNull)
}

func schemaString(schema *table.TypeDescriptor) string {
	return table.Render(table.FinalizeSchema(schema))
}

func schemaTypeName(schema *table.TypeDescriptor) string {
	final := table.FinalizeSchema(schema)
	if final == nil {
		return table.TypeName(table.TypeNull)
	}
	return table.TypeName(final.Kind)
}

func expressionLabel(expr boundExpr) string {
	switch e := expr.(type) {
	case *boundColumn:
		return strings.Join(e.rawPath, ".")
	case *boundCall:
		return e.raw.Name + "()"
	case *boundBinary:
		return e.raw.Op
	case *boundUnary:
		return e.raw.Op
	default:
		return "expression"
	}
}
