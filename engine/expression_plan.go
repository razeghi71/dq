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

type logicalBoundExpr interface {
	logicalBoundExprNode()
}

type logicalBoundLiteral struct {
	raw *ast.LiteralExpr
}

type logicalBoundColumn struct {
	rawPath []string
	typ     *table.TypeDescriptor
}

type logicalBoundBinary struct {
	raw         *ast.BinaryExpr
	left, right logicalBoundExpr
}

type logicalBoundUnary struct {
	raw     *ast.UnaryExpr
	operand logicalBoundExpr
}

type logicalBoundCall struct {
	raw  *ast.FuncCallExpr
	args []logicalBoundExpr
}

type logicalBoundStructField struct {
	name string
	raw  ast.StructField
	expr logicalBoundExpr
}

type logicalBoundStruct struct {
	raw    *ast.StructExpr
	fields []logicalBoundStructField
}

type logicalBoundList struct {
	raw      *ast.ListExpr
	elements []logicalBoundExpr
}

type logicalBoundIsNull struct {
	raw     *ast.IsNullExpr
	operand logicalBoundExpr
}

type logicalBoundCoerce struct{}

func (*logicalBoundLiteral) logicalBoundExprNode() {}
func (*logicalBoundColumn) logicalBoundExprNode()  {}
func (*logicalBoundBinary) logicalBoundExprNode()  {}
func (*logicalBoundUnary) logicalBoundExprNode()   {}
func (*logicalBoundCall) logicalBoundExprNode()    {}
func (*logicalBoundStruct) logicalBoundExprNode()  {}
func (*logicalBoundList) logicalBoundExprNode()    {}
func (*logicalBoundIsNull) logicalBoundExprNode()  {}
func (*logicalBoundCoerce) logicalBoundExprNode()  {}

type typedExpr struct {
	bound    boundExpr
	raw      ast.Expr
	typ      *table.TypeDescriptor
	callEval typedCallEvaluator
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

type logicalTypedExpr struct {
	bound    logicalBoundExpr
	raw      ast.Expr
	typ      *table.TypeDescriptor
	left     *logicalTypedExpr
	right    *logicalTypedExpr
	operand  *logicalTypedExpr
	args     []logicalTypedExpr
	fields   []logicalTypedStructField
	elements []logicalTypedExpr
	coerceTo *table.TypeDescriptor
}

type logicalTypedStructField struct {
	name string
	raw  ast.StructField
	expr logicalTypedExpr
}

type schemaEnv struct {
	columns []string
	raw     []*table.TypeDescriptor
	final   []*table.TypeDescriptor
}

func schemaEnvFromTable(t *table.Table) schemaEnv {
	if t == nil {
		return schemaEnv{}
	}
	env := schemaEnv{
		columns: append([]string(nil), t.Columns...),
		raw:     make([]*table.TypeDescriptor, len(t.Columns)),
		final:   make([]*table.TypeDescriptor, len(t.Columns)),
	}
	for i := range t.Columns {
		col := t.Col(i)
		if col == nil {
			continue
		}
		env.raw[i] = col.RawSchema()
		env.final[i] = col.Schema()
		if env.raw[i] == nil {
			env.raw[i] = env.final[i]
		}
	}
	return env
}

func schemaEnvFromSchema(schema table.Schema) schemaEnv {
	env := schemaEnv{
		columns: make([]string, len(schema.Columns)),
		raw:     make([]*table.TypeDescriptor, len(schema.Columns)),
	}
	for i, col := range schema.Columns {
		env.columns[i] = col.Name
		env.raw[i] = normalizePlanningSchema(col.Type)
	}
	return env
}

// schemaEnvFromOwnedColumns normalizes schemas in place and keeps the supplied
// slices. Callers must pass slices they own; use schemaEnvFromSchema when a
// defensive copy is needed.
func schemaEnvFromOwnedColumns(columns []string, schemas []*table.TypeDescriptor, final bool) schemaEnv {
	for i := range columns {
		var typ *table.TypeDescriptor
		if i < len(schemas) {
			typ = normalizePlanningSchema(schemas[i])
			if final {
				typ = finalizePlanningSchema(typ)
			}
			schemas[i] = typ
		}
	}
	return schemaEnv{columns: columns, raw: schemas}
}

func (e schemaEnv) colIndex(name string) int {
	for i, col := range e.columns {
		if col == name {
			return i
		}
	}
	return -1
}

func (e schemaEnv) rawSchema(i int) *table.TypeDescriptor {
	if i < 0 || i >= len(e.raw) {
		return nil
	}
	return e.raw[i]
}

func (e schemaEnv) finalSchema(i int) *table.TypeDescriptor {
	if i < 0 || i >= len(e.columns) {
		return nil
	}
	if i >= len(e.final) {
		return finalizePlanningSchema(e.rawSchema(i))
	}
	if e.final[i] == nil {
		raw := e.rawSchema(i)
		if raw == nil {
			return nil
		}
		// schemaEnv is intentionally passed by value throughout planning, but
		// the final slice's backing array is shared. This memoizes finalized
		// schemas without copying the environment on every lookup; planning is
		// single-threaded.
		e.final[i] = finalizePlanningSchema(raw)
	}
	return e.final[i]
}

func (e schemaEnv) schema() table.Schema {
	columns := make([]table.SchemaColumn, len(e.columns))
	for i, name := range e.columns {
		typ := e.rawSchema(i)
		if typ == nil {
			typ = e.finalSchema(i)
		}
		columns[i] = table.SchemaColumn{Name: name, Type: typ}
	}
	return table.Schema{Columns: columns}
}

func (e schemaEnv) rawSchemas() []*table.TypeDescriptor {
	schemas := make([]*table.TypeDescriptor, len(e.columns))
	for i := range e.columns {
		schemas[i] = e.rawSchema(i)
		if schemas[i] == nil {
			schemas[i] = e.finalSchema(i)
		}
	}
	return schemas
}

func normalizePlanningSchema(schema *table.TypeDescriptor) *table.TypeDescriptor {
	if schema == nil {
		return nil
	}
	if schemaIsNormalized(schema) {
		return schema
	}
	return table.NormalizeSchema(schema)
}

func schemaIsNormalized(schema *table.TypeDescriptor) bool {
	if schema == nil {
		return true
	}
	switch schema.Kind {
	case table.TypeRecord:
		for i, field := range schema.Fields {
			if i > 0 && schema.Fields[i-1].Name >= field.Name {
				return false
			}
			if !schemaIsNormalized(field.Type) {
				return false
			}
		}
	case table.TypeList:
		return schemaIsNormalized(schema.Elem)
	case table.TypeUnion:
		return false
	}
	return true
}

func finalizePlanningSchema(schema *table.TypeDescriptor) *table.TypeDescriptor {
	if schema == nil {
		return nil
	}
	if !schemaNeedsFinalization(schema) {
		return schema
	}
	return table.FinalizeSchema(schema)
}

func schemaNeedsFinalization(schema *table.TypeDescriptor) bool {
	if schema == nil {
		return false
	}
	switch schema.Kind {
	case table.TypeNull:
		return true
	case table.TypeRecord:
		for _, field := range schema.Fields {
			if schemaNeedsFinalization(field.Type) {
				return true
			}
		}
	case table.TypeList:
		return schema.Elem == nil || schemaNeedsFinalization(schema.Elem)
	case table.TypeUnion:
		for _, branch := range schema.Branches {
			if schemaNeedsFinalization(branch) {
				return true
			}
		}
	}
	return false
}

func bindLogicalExpressionInEnv(expr ast.Expr, env schemaEnv) (logicalBoundExpr, error) {
	switch e := expr.(type) {
	case *ast.LiteralExpr:
		return &logicalBoundLiteral{raw: e}, nil
	case *ast.ColumnExpr:
		return bindColumnPathLogicalInEnv(env, e.Path)
	case *ast.BinaryExpr:
		left, err := bindLogicalExpressionInEnv(e.Left, env)
		if err != nil {
			return nil, err
		}
		right, err := bindLogicalExpressionInEnv(e.Right, env)
		if err != nil {
			return nil, err
		}
		return &logicalBoundBinary{raw: e, left: left, right: right}, nil
	case *ast.UnaryExpr:
		operand, err := bindLogicalExpressionInEnv(e.Operand, env)
		if err != nil {
			return nil, err
		}
		return &logicalBoundUnary{raw: e, operand: operand}, nil
	case *ast.FuncCallExpr:
		args := make([]logicalBoundExpr, len(e.Args))
		for i, arg := range e.Args {
			bound, err := bindLogicalExpressionInEnv(arg, env)
			if err != nil {
				return nil, err
			}
			args[i] = bound
		}
		return &logicalBoundCall{raw: e, args: args}, nil
	case *ast.StructExpr:
		fields := make([]logicalBoundStructField, len(e.Fields))
		for i, field := range e.Fields {
			bound, err := bindLogicalExpressionInEnv(field.Expr, env)
			if err != nil {
				return nil, err
			}
			fields[i] = logicalBoundStructField{name: field.Name, raw: field, expr: bound}
		}
		return &logicalBoundStruct{raw: e, fields: fields}, nil
	case *ast.ListExpr:
		elements := make([]logicalBoundExpr, len(e.Elements))
		for i, elem := range e.Elements {
			bound, err := bindLogicalExpressionInEnv(elem, env)
			if err != nil {
				return nil, err
			}
			elements[i] = bound
		}
		return &logicalBoundList{raw: e, elements: elements}, nil
	case *ast.IsNullExpr:
		operand, err := bindLogicalExpressionInEnv(e.Operand, env)
		if err != nil {
			return nil, err
		}
		return &logicalBoundIsNull{raw: e, operand: operand}, nil
	default:
		return nil, fmt.Errorf("unknown expression type %T", expr)
	}
}

func bindColumnPathInEnv(env schemaEnv, path []string) (*boundColumn, error) {
	idx, typ, err := resolveColumnPathInEnv(env, path)
	if err != nil {
		return nil, err
	}
	rawPath := clonePath(path)
	return &boundColumn{
		rawPath:    rawPath,
		topIndex:   idx,
		nestedPath: clonePath(path[1:]),
		typ:        typ,
	}, nil
}

func bindColumnPathLogicalInEnv(env schemaEnv, path []string) (*logicalBoundColumn, error) {
	_, typ, err := resolveColumnPathInEnv(env, path)
	if err != nil {
		return nil, err
	}
	return &logicalBoundColumn{rawPath: clonePath(path), typ: typ}, nil
}

func resolveColumnPathInEnv(env schemaEnv, path []string) (int, *table.TypeDescriptor, error) {
	if len(path) == 0 {
		return -1, nil, fmt.Errorf("empty column path")
	}
	idx := env.colIndex(path[0])
	if idx < 0 {
		return -1, nil, columnNotFoundError(path[0], env.columns)
	}
	typ := env.rawSchema(idx)
	if typ == nil {
		typ = env.finalSchema(idx)
	}
	parentNullable := false
	for _, seg := range path[1:] {
		if typ == nil {
			return -1, nil, fmt.Errorf("field %q not found in column %q: parent schema is unknown", seg, strings.Join(path, "."))
		}
		if typ.Nullable || typ.Kind == table.TypeNull {
			parentNullable = true
		}
		current := finalizePlanningSchema(typ)
		switch current.Kind {
		case table.TypeUnion:
			return -1, nil, unionPathTraversalError(path, current)
		case table.TypeList:
			return -1, nil, fmt.Errorf("cannot access field %q through list column %q of type %s", seg, strings.Join(path, "."), current.String())
		case table.TypeRecord:
			var next *table.TypeDescriptor
			for _, field := range current.Fields {
				if field.Name == seg {
					next = field.Type
					break
				}
			}
			if next == nil {
				return -1, nil, fmt.Errorf("field %q not found in column %q of type %s", seg, path[0], current.String())
			}
			typ = next
		default:
			return -1, nil, fmt.Errorf("cannot access field %q in column %q of type %s", seg, path[0], current.String())
		}
	}
	typ = normalizePlanningSchema(typ)
	if parentNullable {
		typ = table.WithNullable(typ)
	}
	return idx, typ, nil
}

func bindLogicalReduceExpression(expr ast.Expr, nestedSchema *table.TypeDescriptor) (logicalBoundExpr, error) {
	switch e := expr.(type) {
	case *ast.LiteralExpr:
		return &logicalBoundLiteral{raw: e}, nil
	case *ast.ColumnExpr:
		return nil, fmt.Errorf("column %q cannot be used directly in reduce; use an aggregate such as first(%s)", strings.Join(e.Path, "."), strings.Join(e.Path, "."))
	case *ast.BinaryExpr:
		left, err := bindLogicalReduceExpression(e.Left, nestedSchema)
		if err != nil {
			return nil, err
		}
		right, err := bindLogicalReduceExpression(e.Right, nestedSchema)
		if err != nil {
			return nil, err
		}
		return &logicalBoundBinary{raw: e, left: left, right: right}, nil
	case *ast.UnaryExpr:
		operand, err := bindLogicalReduceExpression(e.Operand, nestedSchema)
		if err != nil {
			return nil, err
		}
		return &logicalBoundUnary{raw: e, operand: operand}, nil
	case *ast.FuncCallExpr:
		spec, ok := builtinCatalog[e.Name]
		if !ok {
			return nil, fmt.Errorf("unknown function %q", e.Name)
		}
		if spec.Category != builtinAggregate {
			return nil, nonAggregateReduceFunctionError(e.Name)
		}
		args, err := bindLogicalAggregateArgs(e, nestedSchema)
		if err != nil {
			return nil, err
		}
		return &logicalBoundCall{raw: e, args: args}, nil
	case *ast.StructExpr:
		return nil, fmt.Errorf("struct constructor is not supported in reduce")
	case *ast.ListExpr:
		return nil, fmt.Errorf("list constructor is not supported in reduce")
	case *ast.IsNullExpr:
		operand, err := bindLogicalReduceExpression(e.Operand, nestedSchema)
		if err != nil {
			return nil, err
		}
		return &logicalBoundIsNull{raw: e, operand: operand}, nil
	default:
		return nil, fmt.Errorf("unknown expression type %T", expr)
	}
}

func bindLogicalAggregateArgs(e *ast.FuncCallExpr, nestedSchema *table.TypeDescriptor) ([]logicalBoundExpr, error) {
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
	bound, err := bindLogicalAggregateColumnPath(nestedSchema, col.Path)
	if err != nil {
		return nil, fmt.Errorf("%s(%s): %w", e.Name, strings.Join(col.Path, "."), err)
	}
	return []logicalBoundExpr{bound}, nil
}

func bindLogicalAggregateColumnPath(nestedSchema *table.TypeDescriptor, path []string) (*logicalBoundColumn, error) {
	env, err := envForRecordSchema(nestedSchema)
	if err != nil {
		return nil, err
	}
	return bindColumnPathLogicalInEnv(env, path)
}

func envForRecordSchema(schema *table.TypeDescriptor) (schemaEnv, error) {
	if schema == nil {
		return schemaEnv{}, fmt.Errorf("nested row schema is unknown")
	}
	rec := finalizePlanningSchema(normalizePlanningSchema(schema))
	if rec.Kind != table.TypeRecord {
		return schemaEnv{}, fmt.Errorf("nested row schema must be a record, got %s", rec.String())
	}
	env := schemaEnv{
		columns: make([]string, len(rec.Fields)),
		raw:     make([]*table.TypeDescriptor, len(rec.Fields)),
		final:   make([]*table.TypeDescriptor, len(rec.Fields)),
	}
	for i, field := range rec.Fields {
		env.columns[i] = field.Name
		env.raw[i] = normalizePlanningSchema(field.Type)
	}
	return env, nil
}

func columnNotFoundError(name string, columns []string) error {
	msg := fmt.Sprintf("column %q not found", name)
	if len(columns) > 0 {
		msg += "; available columns: " + strings.Join(columns, ", ")
		if hint := closestColumnName(name, columns); hint != "" {
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

func typeCheckLogicalExpression(expr logicalBoundExpr) (logicalTypedExpr, error) {
	switch e := expr.(type) {
	case *logicalBoundLiteral:
		return logicalTypedExpr{bound: expr, raw: e.raw, typ: literalType(e.raw)}, nil
	case *logicalBoundColumn:
		return logicalTypedExpr{bound: expr, raw: &ast.ColumnExpr{Path: e.rawPath}, typ: normalizePlanningSchema(e.typ)}, nil
	case *logicalBoundBinary:
		left, err := typeCheckLogicalExpression(e.left)
		if err != nil {
			return logicalTypedExpr{}, err
		}
		right, err := typeCheckLogicalExpression(e.right)
		if err != nil {
			return logicalTypedExpr{}, err
		}
		typ, err := checkBinarySignature(e.raw.Op, logicalSignatureExpr(left), logicalSignatureExpr(right))
		if err != nil {
			return logicalTypedExpr{}, err
		}
		return logicalTypedExpr{bound: expr, raw: e.raw, typ: typ, left: &left, right: &right}, nil
	case *logicalBoundUnary:
		operand, err := typeCheckLogicalExpression(e.operand)
		if err != nil {
			return logicalTypedExpr{}, err
		}
		typ, err := checkUnarySignature(e.raw.Op, logicalSignatureExpr(operand))
		if err != nil {
			return logicalTypedExpr{}, err
		}
		return logicalTypedExpr{bound: expr, raw: e.raw, typ: typ, operand: &operand}, nil
	case *logicalBoundCall:
		args := make([]logicalTypedExpr, len(e.args))
		for i, arg := range e.args {
			typed, err := typeCheckLogicalExpression(arg)
			if err != nil {
				return logicalTypedExpr{}, err
			}
			args[i] = typed
		}
		spec, ok := builtinCatalog[e.raw.Name]
		if !ok {
			return logicalTypedExpr{}, fmt.Errorf("unknown function %q", e.raw.Name)
		}
		if spec.Category == builtinAggregate {
			return logicalTypedExpr{}, aggregateOutsideReduceError(e.raw.Name)
		}
		signatureArgs := logicalSignatureExprs(args)
		typ, err := spec.Check(signatureArgs)
		if err != nil {
			return logicalTypedExpr{}, err
		}
		out := logicalTypedExpr{bound: expr, raw: e.raw, typ: typ, args: args}
		switch e.raw.Name {
		case "coalesce":
			if typedArgsNeedCoercion(signatureArgs, typ) {
				out = coerceLogicalTypedExpression(out, typ)
			}
		case "if":
			if len(args) == 3 && (runtimeCoercionNeeded(args[1].typ, typ) || runtimeCoercionNeeded(args[2].typ, typ)) {
				out = coerceLogicalTypedExpression(out, typ)
			}
		}
		return out, nil
	case *logicalBoundStruct:
		seen := make(map[string]bool, len(e.fields))
		fields := make([]table.FieldDescriptor, len(e.fields))
		typedFields := make([]logicalTypedStructField, len(e.fields))
		for i, field := range e.fields {
			if seen[field.name] {
				return logicalTypedExpr{}, fmt.Errorf("struct() duplicate field %q", field.name)
			}
			seen[field.name] = true
			typed, err := typeCheckLogicalExpression(field.expr)
			if err != nil {
				return logicalTypedExpr{}, err
			}
			fields[i] = table.FieldDescriptor{Name: field.name, Type: typed.typ}
			typedFields[i] = logicalTypedStructField{name: field.name, raw: field.raw, expr: typed}
		}
		return logicalTypedExpr{bound: expr, raw: e.raw, typ: &table.TypeDescriptor{Kind: table.TypeRecord, Fields: fields}, fields: typedFields}, nil
	case *logicalBoundList:
		elems := make([]*table.TypeDescriptor, len(e.elements))
		typedElems := make([]logicalTypedExpr, len(e.elements))
		for i, elem := range e.elements {
			typed, err := typeCheckLogicalExpression(elem)
			if err != nil {
				return logicalTypedExpr{}, err
			}
			elems[i] = typed.typ
			typedElems[i] = typed
		}
		elemSchema := table.UnifyListLiteralElems(elems)
		out := logicalTypedExpr{bound: expr, raw: e.raw, typ: &table.TypeDescriptor{Kind: table.TypeList, Elem: elemSchema}, elements: typedElems}
		if typedArgsNeedCoercion(logicalSignatureExprs(typedElems), elemSchema) {
			out = coerceLogicalTypedExpression(out, out.typ)
		}
		return out, nil
	case *logicalBoundIsNull:
		operand, err := typeCheckLogicalExpression(e.operand)
		if err != nil {
			return logicalTypedExpr{}, err
		}
		return logicalTypedExpr{bound: expr, raw: e.raw, typ: &table.TypeDescriptor{Kind: table.TypeBool}, operand: &operand}, nil
	default:
		return logicalTypedExpr{}, fmt.Errorf("unknown logical bound expression type %T", expr)
	}
}

func typeCheckLogicalReduceExpression(expr logicalBoundExpr) (logicalTypedExpr, error) {
	switch e := expr.(type) {
	case *logicalBoundLiteral:
		return logicalTypedExpr{bound: expr, raw: e.raw, typ: literalType(e.raw)}, nil
	case *logicalBoundColumn:
		return logicalTypedExpr{bound: expr, raw: &ast.ColumnExpr{Path: e.rawPath}, typ: normalizePlanningSchema(e.typ)}, nil
	case *logicalBoundBinary:
		left, err := typeCheckLogicalReduceExpression(e.left)
		if err != nil {
			return logicalTypedExpr{}, err
		}
		right, err := typeCheckLogicalReduceExpression(e.right)
		if err != nil {
			return logicalTypedExpr{}, err
		}
		typ, err := checkBinarySignature(e.raw.Op, logicalSignatureExpr(left), logicalSignatureExpr(right))
		if err != nil {
			return logicalTypedExpr{}, err
		}
		return logicalTypedExpr{bound: expr, raw: e.raw, typ: typ, left: &left, right: &right}, nil
	case *logicalBoundUnary:
		operand, err := typeCheckLogicalReduceExpression(e.operand)
		if err != nil {
			return logicalTypedExpr{}, err
		}
		typ, err := checkUnarySignature(e.raw.Op, logicalSignatureExpr(operand))
		if err != nil {
			return logicalTypedExpr{}, err
		}
		return logicalTypedExpr{bound: expr, raw: e.raw, typ: typ, operand: &operand}, nil
	case *logicalBoundCall:
		args := make([]logicalTypedExpr, len(e.args))
		for i, arg := range e.args {
			typed, err := typeCheckLogicalReduceExpression(arg)
			if err != nil {
				return logicalTypedExpr{}, err
			}
			args[i] = typed
		}
		spec, ok := builtinCatalog[e.raw.Name]
		if !ok {
			return logicalTypedExpr{}, fmt.Errorf("unknown function %q", e.raw.Name)
		}
		if spec.Category != builtinAggregate {
			return logicalTypedExpr{}, nonAggregateReduceFunctionError(e.raw.Name)
		}
		typ, err := spec.Check(logicalSignatureExprs(args))
		if err != nil {
			return logicalTypedExpr{}, err
		}
		return logicalTypedExpr{bound: expr, raw: e.raw, typ: typ, args: args}, nil
	case *logicalBoundStruct:
		return logicalTypedExpr{}, fmt.Errorf("struct constructor is not supported in reduce")
	case *logicalBoundList:
		return logicalTypedExpr{}, fmt.Errorf("list constructor is not supported in reduce")
	case *logicalBoundIsNull:
		operand, err := typeCheckLogicalReduceExpression(e.operand)
		if err != nil {
			return logicalTypedExpr{}, err
		}
		return logicalTypedExpr{bound: expr, raw: e.raw, typ: &table.TypeDescriptor{Kind: table.TypeBool}, operand: &operand}, nil
	default:
		return logicalTypedExpr{}, fmt.Errorf("unknown logical bound expression type %T", expr)
	}
}

func logicalSignatureExpr(expr logicalTypedExpr) typedExpr {
	return typedExpr{raw: expr.raw, typ: expr.typ}
}

func logicalSignatureExprs(args []logicalTypedExpr) []typedExpr {
	out := make([]typedExpr, len(args))
	for i, arg := range args {
		out[i] = logicalSignatureExpr(arg)
	}
	return out
}

func coerceLogicalTypedExpression(expr logicalTypedExpr, target *table.TypeDescriptor) logicalTypedExpr {
	target = finalizePlanningSchema(target)
	child := expr
	return logicalTypedExpr{
		bound:    &logicalBoundCoerce{},
		raw:      expr.raw,
		typ:      target,
		operand:  &child,
		coerceTo: target,
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
		if !table.IsComparable(finalizePlanningSchema(left.typ)) || !table.IsComparable(finalizePlanningSchema(right.typ)) {
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
		return normalizePlanningSchema(operand.typ), nil
	default:
		return nil, fmt.Errorf("unknown unary operator %q", op)
	}
}

func unaryStringSpec(name string) func(args []typedExpr) (*table.TypeDescriptor, error) {
	return func(args []typedExpr) (*table.TypeDescriptor, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("%s() takes 1 argument, got %d", name, len(args))
		}
		if !schemaKindOrNull(args[0].typ, table.TypeString) {
			return nil, fmt.Errorf("%s() requires a string, got %s", name, schemaString(args[0].typ))
		}
		return nullableSchema(table.TypeString, args[0].typ), nil
	}
}

func unaryStringToIntSpec(name string) func(args []typedExpr) (*table.TypeDescriptor, error) {
	return func(args []typedExpr) (*table.TypeDescriptor, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("%s() takes 1 argument, got %d", name, len(args))
		}
		if !schemaKindOrNull(args[0].typ, table.TypeString) {
			return nil, fmt.Errorf("%s() requires a string, got %s", name, schemaString(args[0].typ))
		}
		return nullableSchema(table.TypeInt, args[0].typ), nil
	}
}

func binaryStringToBoolSpec(name, secondArgLabel string) func(args []typedExpr) (*table.TypeDescriptor, error) {
	return func(args []typedExpr) (*table.TypeDescriptor, error) {
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
	}
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
	listSchema := normalizePlanningSchema(args[0].typ)
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
	return !sameRuntimeSchema(finalizePlanningSchema(from), finalizePlanningSchema(target))
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
	return schema.Kind == table.TypeNull || table.IsOrderable(finalizePlanningSchema(schema))
}

func isNullOnly(schema *table.TypeDescriptor) bool {
	return schema != nil && schema.Kind == table.TypeNull
}

func schemaMayBeNull(schema *table.TypeDescriptor) bool {
	return schema != nil && (schema.Nullable || schema.Kind == table.TypeNull)
}

func schemaString(schema *table.TypeDescriptor) string {
	return table.Render(finalizePlanningSchema(schema))
}

func schemaTypeName(schema *table.TypeDescriptor) string {
	final := finalizePlanningSchema(schema)
	if final == nil {
		return table.TypeName(table.TypeNull)
	}
	return table.TypeName(final.Kind)
}

func logicalExpressionLabel(expr logicalBoundExpr) string {
	switch e := expr.(type) {
	case *logicalBoundColumn:
		return strings.Join(e.rawPath, ".")
	case *logicalBoundCall:
		return e.raw.Name + "()"
	case *logicalBoundBinary:
		return e.raw.Op
	case *logicalBoundUnary:
		return e.raw.Op
	default:
		return "expression"
	}
}
