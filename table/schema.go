package table

import (
	"fmt"
	"sort"
	"strings"
)

// TypeDescriptor describes a possibly nested table value type.
type TypeDescriptor struct {
	Kind     ValueType
	Nullable bool
	Fields   []FieldDescriptor
	Elem     *TypeDescriptor
	Branches []*TypeDescriptor
}

// FieldDescriptor describes one named record field.
type FieldDescriptor struct {
	Name string
	Type *TypeDescriptor
}

// Schema describes the logical columns in a table.
type Schema struct {
	Columns []SchemaColumn
}

// SchemaColumn describes one logical table column.
type SchemaColumn struct {
	Name string
	Type *TypeDescriptor
}

// NewSchema creates a logical schema from column names and recursive type
// descriptors. Descriptors are normalized and cloned, but not finalized; callers
// that model planning-time unknown/null-only columns can preserve TypeNull until
// a real table storage schema is needed.
func NewSchema(columns []string, types []*TypeDescriptor) Schema {
	out := Schema{Columns: make([]SchemaColumn, len(columns))}
	for i, name := range columns {
		var typ *TypeDescriptor
		if i < len(types) && types[i] != nil {
			typ = NormalizeSchema(types[i])
		}
		out.Columns[i] = SchemaColumn{Name: name, Type: typ}
	}
	return out
}

// SchemaError reports a mismatch while merging or coercing nested schemas.
type SchemaError struct {
	Path     string
	Expected *TypeDescriptor
	Actual   *TypeDescriptor
}

func (e *SchemaError) Error() string {
	path := e.Path
	if path == "" {
		path = "<value>"
	}
	return fmt.Sprintf("%s expected %s, got %s", path, e.Expected.String(), e.Actual.String())
}

// ScalarSchema returns a finalized descriptor for a shallow value type.
func ScalarSchema(kind ValueType) *TypeDescriptor {
	if kind == TypeNull {
		return &TypeDescriptor{Kind: TypeString, Nullable: true}
	}
	return &TypeDescriptor{Kind: kind}
}

// InferValueSchema infers a descriptor from one value. Null values are
// non-determining until finalized.
func InferValueSchema(v Value) *TypeDescriptor {
	switch v.Type {
	case TypeNull:
		return &TypeDescriptor{Kind: TypeNull, Nullable: true}
	case TypeRecord:
		fields := make([]FieldDescriptor, len(v.Fields))
		seen := map[string]bool{}
		for i, f := range v.Fields {
			if seen[f.Name] {
				// Parser-created structs and loader-created records should already
				// have unique field names. Keep this fallback because Value is a
				// public type and duplicate record fields cannot be addressed
				// unambiguously by path.
				return &TypeDescriptor{Kind: TypeString, Nullable: true}
			}
			seen[f.Name] = true
			fields[i] = FieldDescriptor{Name: f.Name, Type: InferValueSchema(f.Value)}
		}
		sort.Slice(fields, func(i, j int) bool { return fields[i].Name < fields[j].Name })
		return &TypeDescriptor{Kind: TypeRecord, Fields: fields}
	case TypeList:
		var elem *TypeDescriptor
		for _, item := range v.List {
			itemSchema := InferValueSchema(item)
			if elem == nil {
				elem = itemSchema
				continue
			}
			merged, err := mergeListElementSchemas(elem, itemSchema)
			if err != nil {
				elem = &TypeDescriptor{Kind: TypeMixed, Nullable: true}
				break
			}
			elem = merged
		}
		return &TypeDescriptor{Kind: TypeList, Elem: elem}
	default:
		return &TypeDescriptor{Kind: v.Type}
	}
}

// MergeSchemasPermissive merges schemas using existing dq widening behavior.
func MergeSchemasPermissive(a, b *TypeDescriptor) (*TypeDescriptor, error) {
	return UnifySchemas(a, b, UnifyPermissiveMode)
}

// MergeSchemasStrict merges schemas and rejects incompatible non-null types.
func MergeSchemasStrict(a, b *TypeDescriptor) (*TypeDescriptor, error) {
	return UnifySchemas(a, b, UnifyStrictMode)
}

// MergeSchemasStrictAtPath is MergeSchemasStrict with a diagnostic path prefix.
func MergeSchemasStrictAtPath(a, b *TypeDescriptor, path string) (*TypeDescriptor, error) {
	return UnifySchemasAtPath(a, b, UnifyStrictMode, path)
}

// Render returns a deterministic compact recursive type string.
func Render(t *TypeDescriptor) string {
	if t == nil {
		return "null"
	}
	var b strings.Builder
	writeSchemaString(&b, NormalizeSchema(t))
	return b.String()
}

// Same reports whether two descriptors have the same logical shape.
// Record field order is ignored; nullability is significant.
func Same(a, b *TypeDescriptor) bool {
	return EquivalentSchema(a, b)
}

// WithoutNull returns a copy of t with top-level nullability removed.
func WithoutNull(t *TypeDescriptor) *TypeDescriptor {
	out := cloneTypeDescriptor(t)
	if out != nil {
		out.Nullable = false
	}
	return out
}

// WithNullable returns a copy of t with top-level nullability enabled.
func WithNullable(t *TypeDescriptor) *TypeDescriptor {
	if t == nil {
		return &TypeDescriptor{Kind: TypeNull, Nullable: true}
	}
	out := cloneTypeDescriptor(t)
	out.Nullable = true
	return out
}

// UnionSchema builds a normalized union descriptor from branch descriptors.
// Storage-equivalent branches are collapsed. Distinct structural branches stay
// ordered, but source-level branch names/tags are not represented.
func UnionSchema(branches []*TypeDescriptor, nullable bool) *TypeDescriptor {
	return UnionOf(branches, nullable)
}

func unionSchema(branches []*TypeDescriptor, nullable bool) *TypeDescriptor {
	var out []*TypeDescriptor
	for _, branch := range branches {
		branch = NormalizeSchema(branch)
		if branch == nil {
			continue
		}
		if branch.Kind == TypeNull {
			nullable = true
			continue
		}
		if branch.Kind == TypeUnion {
			nullable = nullable || branch.Nullable
			for _, nested := range branch.Branches {
				nested = NormalizeSchema(nested)
				if nested == nil {
					continue
				}
				if nested.Kind == TypeNull {
					nullable = true
					continue
				}
				if nested.Nullable {
					nullable = true
					nested = WithoutNull(nested)
				}
				out = addUnionBranch(out, nested)
			}
			continue
		}
		if branch.Nullable {
			nullable = true
			branch = WithoutNull(branch)
		}
		out = addUnionBranch(out, branch)
	}
	if len(out) == 0 {
		return &TypeDescriptor{Kind: TypeString, Nullable: true}
	}
	if len(out) == 1 {
		only := cloneTypeDescriptor(out[0])
		only.Nullable = only.Nullable || nullable
		return only
	}
	return &TypeDescriptor{Kind: TypeUnion, Nullable: nullable, Branches: out}
}

func addUnionBranch(branches []*TypeDescriptor, next *TypeDescriptor) []*TypeDescriptor {
	next = NormalizeSchema(next)
	for i, existing := range branches {
		if merged, ok := mergeCompatibleUnionBranch(existing, next); ok {
			branches[i] = merged
			return branches
		}
	}
	return append(branches, next)
}

func mergeCompatibleUnionBranch(a, b *TypeDescriptor) (*TypeDescriptor, bool) {
	return mergeCompatibleUnionBranchWith(a, b, true)
}

func mergeCompatibleUnionBranchWith(a, b *TypeDescriptor, allowNumericPromotion bool) (*TypeDescriptor, bool) {
	if a == nil || b == nil {
		return nil, false
	}
	a = NormalizeSchema(a)
	b = NormalizeSchema(b)
	if a.Kind == TypeNull {
		out := cloneTypeDescriptor(b)
		out.Nullable = true
		return out, true
	}
	if b.Kind == TypeNull {
		out := cloneTypeDescriptor(a)
		out.Nullable = true
		return out, true
	}
	if allowNumericPromotion && (a.Kind == TypeInt && b.Kind == TypeFloat || a.Kind == TypeFloat && b.Kind == TypeInt) {
		return &TypeDescriptor{Kind: TypeFloat, Nullable: a.Nullable || b.Nullable}, true
	}
	if a.Kind != b.Kind {
		return nil, false
	}
	out := &TypeDescriptor{Kind: a.Kind, Nullable: a.Nullable || b.Nullable}
	switch a.Kind {
	case TypeRecord:
		aByName := make(map[string]*TypeDescriptor, len(a.Fields))
		bByName := make(map[string]*TypeDescriptor, len(b.Fields))
		for i := range a.Fields {
			if aByName[a.Fields[i].Name] != nil {
				return nil, false
			}
			aByName[a.Fields[i].Name] = a.Fields[i].Type
		}
		for i := range b.Fields {
			if bByName[b.Fields[i].Name] != nil {
				return nil, false
			}
			bByName[b.Fields[i].Name] = b.Fields[i].Type
		}
		if len(aByName) != len(bByName) {
			return nil, false
		}
		names := make([]string, 0, len(aByName))
		for name := range aByName {
			if bByName[name] == nil {
				return nil, false
			}
			names = append(names, name)
		}
		sort.Strings(names)
		fields := make([]FieldDescriptor, 0, len(names))
		for _, name := range names {
			merged, ok := mergeCompatibleUnionBranchWith(aByName[name], bByName[name], false)
			if !ok {
				return nil, false
			}
			fields = append(fields, FieldDescriptor{Name: name, Type: merged})
		}
		out.Fields = fields
	case TypeList:
		merged, ok := mergeCompatibleUnionBranchWith(a.Elem, b.Elem, false)
		if !ok {
			return nil, false
		}
		out.Elem = merged
	case TypeUnion:
		branches := make([]*TypeDescriptor, 0, len(a.Branches)+len(b.Branches))
		for _, branch := range a.Branches {
			branches = addUnionBranch(branches, branch)
		}
		for _, branch := range b.Branches {
			branches = addUnionBranch(branches, branch)
		}
		out = UnionOf(branches, out.Nullable)
	}
	return out, true
}

// SchemaContainsUnion reports whether a descriptor contains a union node.
func SchemaContainsUnion(schema *TypeDescriptor) bool {
	if schema == nil {
		return false
	}
	switch schema.Kind {
	case TypeUnion:
		return true
	case TypeRecord:
		for _, field := range schema.Fields {
			if SchemaContainsUnion(field.Type) {
				return true
			}
		}
	case TypeList:
		return SchemaContainsUnion(schema.Elem)
	}
	return false
}

// IsNumeric reports whether t is int/float, ignoring nullability.
func IsNumeric(t *TypeDescriptor) bool {
	if t == nil {
		return false
	}
	return t.Kind == TypeInt || t.Kind == TypeFloat
}

// IsBooleanLike reports whether t is bool or bool?.
func IsBooleanLike(t *TypeDescriptor) bool {
	return t != nil && t.Kind == TypeBool
}

// IsStringLike reports whether t is string or string?.
func IsStringLike(t *TypeDescriptor) bool {
	return t != nil && t.Kind == TypeString
}

// IsComparable reports whether equality is defined for values of t.
func IsComparable(t *TypeDescriptor) bool {
	if t == nil {
		return false
	}
	switch t.Kind {
	case TypeInt, TypeFloat, TypeString, TypeBool, TypeList, TypeRecord:
		return true
	default:
		return false
	}
}

// IsOrderable reports whether ordering comparisons are defined for values of t.
func IsOrderable(t *TypeDescriptor) bool {
	if t == nil {
		return false
	}
	switch t.Kind {
	case TypeInt, TypeFloat, TypeString:
		return true
	default:
		return false
	}
}

// UnifyStrict merges two logical types and rejects incompatible non-null types.
// It does not mutate either input.
func UnifyStrict(a, b *TypeDescriptor) (*TypeDescriptor, error) {
	return UnifySchemas(a, b, UnifyStrictMode)
}

// UnifyAllStrict merges all logical types and rejects incompatible non-null
// types. It does not mutate the input descriptors.
func UnifyAllStrict(ts []*TypeDescriptor) (*TypeDescriptor, error) {
	var out *TypeDescriptor
	for _, typ := range ts {
		next, err := UnifyStrict(out, typ)
		if err != nil {
			return nil, err
		}
		out = next
	}
	return out, nil
}

// UnifyListLiteralElems merges element types for one list literal. Unlike
// UnifyStrict, it may produce mixed for explicitly heterogeneous literal
// contents. An empty list has no determining element schema yet; callers should
// keep the nil element until a surrounding expression or finalization provides
// a concrete schema.
func UnifyListLiteralElems(ts []*TypeDescriptor) *TypeDescriptor {
	if len(ts) == 0 {
		return nil
	}
	var out *TypeDescriptor
	for _, typ := range ts {
		merged, err := UnifySchemas(out, typ, UnifyListLiteralMode)
		if err != nil {
			return &TypeDescriptor{Kind: TypeMixed}
		}
		out = merged
	}
	return NormalizeSchema(out)
}

// NumericResult returns the result type for numeric arithmetic.
func NumericResult(a, b *TypeDescriptor) (*TypeDescriptor, error) {
	return numericResultAtPath(a, b, "")
}

// MergeValueSchemaStrictAtPath merges one materialized value into an existing
// descriptor and rejects incompatible non-null types. The returned descriptor
// may share storage with the input descriptor.
func MergeValueSchemaStrictAtPath(schema *TypeDescriptor, v Value, path string) (*TypeDescriptor, error) {
	if err := ValidateSchemaAtPath(schema, path); err != nil {
		return nil, err
	}
	return mergeValueSchemaStrict(schema, v, path)
}

func unifyStrictAtPath(a, b *TypeDescriptor, path string, allowMixed bool) (*TypeDescriptor, error) {
	if a == nil {
		return NormalizeSchema(cloneTypeDescriptor(b)), nil
	}
	if b == nil {
		return NormalizeSchema(cloneTypeDescriptor(a)), nil
	}
	a = NormalizeSchema(a)
	b = NormalizeSchema(b)

	if a.Kind == TypeNull {
		out := cloneTypeDescriptor(b)
		out.Nullable = true
		return out, nil
	}
	if b.Kind == TypeNull {
		out := cloneTypeDescriptor(a)
		out.Nullable = true
		return out, nil
	}
	if a.Kind == TypeMixed || b.Kind == TypeMixed {
		if allowMixed || (a.Kind == TypeMixed && b.Kind == TypeMixed) {
			return &TypeDescriptor{Kind: TypeMixed, Nullable: a.Nullable || b.Nullable}, nil
		}
		return nil, &SchemaError{Path: path, Expected: a, Actual: b}
	}
	if a.Kind == TypeUnion || b.Kind == TypeUnion {
		return unifyUnionStrictAtPath(a, b, path)
	}
	if a.Kind == TypeInt && b.Kind == TypeFloat || a.Kind == TypeFloat && b.Kind == TypeInt {
		return &TypeDescriptor{Kind: TypeFloat, Nullable: a.Nullable || b.Nullable}, nil
	}
	if a.Kind != b.Kind {
		return nil, &SchemaError{Path: path, Expected: a, Actual: b}
	}

	out := &TypeDescriptor{Kind: a.Kind, Nullable: a.Nullable || b.Nullable}
	switch a.Kind {
	case TypeRecord:
		fields, err := unifyRecordFieldsStrict(a.Fields, b.Fields, path, allowMixed)
		if err != nil {
			return nil, err
		}
		out.Fields = fields
	case TypeList:
		elem, err := unifyStrictAtPath(a.Elem, b.Elem, appendSchemaPath(path, "[]"), true)
		if err != nil {
			return nil, err
		}
		out.Elem = elem
	}
	return out, nil
}

func unifyUnionStrictAtPath(a, b *TypeDescriptor, path string) (*TypeDescriptor, error) {
	if a.Kind == TypeUnion && b.Kind == TypeUnion {
		branches := make([]*TypeDescriptor, 0, len(a.Branches)+len(b.Branches))
		for _, branch := range a.Branches {
			branches = addUnionBranch(branches, branch)
		}
		for _, branch := range b.Branches {
			branches = addUnionBranch(branches, branch)
		}
		return UnionOf(branches, a.Nullable || b.Nullable), nil
	}
	if a.Kind == TypeUnion {
		if out, ok := mergeSchemaIntoUnion(a, b); ok {
			return out, nil
		}
		return nil, &SchemaError{Path: path, Expected: a, Actual: b}
	}
	if out, ok := mergeSchemaIntoUnion(b, a); ok {
		return out, nil
	}
	return nil, &SchemaError{Path: path, Expected: a, Actual: b}
}

func mergeSchemaIntoUnion(union, schema *TypeDescriptor) (*TypeDescriptor, bool) {
	if union == nil || union.Kind != TypeUnion || schema == nil {
		return nil, false
	}
	branches := make([]*TypeDescriptor, 0, len(union.Branches))
	merged := false
	for _, branch := range union.Branches {
		if !merged {
			if branchAcceptsSchema(branch, schema) {
				branches = append(branches, branch)
				merged = true
				continue
			}
			if nextBranch, ok := mergeSchemaIntoUnionBranch(branch, schema); ok {
				branches = append(branches, nextBranch)
				merged = true
				continue
			}
		}
		branches = addUnionBranch(branches, branch)
	}
	if !merged {
		return nil, false
	}
	return UnionOf(branches, union.Nullable), true
}

func mergeSchemaIntoUnionBranch(branch, schema *TypeDescriptor) (*TypeDescriptor, bool) {
	branch = NormalizeSchema(branch)
	schema = NormalizeSchema(schema)
	if branch == nil || schema == nil {
		return nil, false
	}
	if branch.Kind == TypeRecord && schema.Kind == TypeRecord {
		return mergeRecordBranchForSchemaFit(branch, schema)
	}
	if branch.Kind == TypeList && schema.Kind == TypeList {
		elem, ok := mergeSchemaIntoUnionBranch(branch.Elem, schema.Elem)
		if !ok {
			return nil, false
		}
		return &TypeDescriptor{Kind: TypeList, Nullable: branch.Nullable || schema.Nullable, Elem: elem}, true
	}
	return mergeCompatibleUnionBranch(branch, schema)
}

func mergeRecordBranchForSchemaFit(branch, schema *TypeDescriptor) (*TypeDescriptor, bool) {
	branchByName := make(map[string]*TypeDescriptor, len(branch.Fields))
	schemaByName := make(map[string]*TypeDescriptor, len(schema.Fields))
	for _, field := range branch.Fields {
		if branchByName[field.Name] != nil {
			return nil, false
		}
		branchByName[field.Name] = field.Type
	}
	for _, field := range schema.Fields {
		if schemaByName[field.Name] != nil {
			return nil, false
		}
		schemaByName[field.Name] = field.Type
	}
	if len(branchByName) != len(schemaByName) {
		return nil, false
	}
	names := make([]string, 0, len(branchByName))
	for name := range branchByName {
		if schemaByName[name] == nil {
			return nil, false
		}
		names = append(names, name)
	}
	sort.Strings(names)

	fields := make([]FieldDescriptor, 0, len(names))
	for _, name := range names {
		merged, ok := mergeCompatibleUnionBranchWith(branchByName[name], schemaByName[name], false)
		if !ok {
			return nil, false
		}
		fields = append(fields, FieldDescriptor{Name: name, Type: merged})
	}
	return &TypeDescriptor{Kind: TypeRecord, Nullable: branch.Nullable || schema.Nullable, Fields: fields}, true
}

func unionHasBranchForSchema(union, schema *TypeDescriptor) bool {
	return unionHasBranchForSchemaMode(union, schema, AssignCoerciveMode)
}

func unionHasBranchForSchemaMode(union, schema *TypeDescriptor, mode SchemaAssignMode) bool {
	if union == nil || union.Kind != TypeUnion || schema == nil {
		return false
	}
	for _, branch := range union.Branches {
		if schemaAssignable(branch, schema, mode) {
			return true
		}
	}
	return false
}

func branchAcceptsSchema(branch, schema *TypeDescriptor) bool {
	branch = NormalizeSchema(branch)
	schema = NormalizeSchema(schema)
	if branch != nil && schema != nil && branch.Kind == TypeRecord && schema.Kind == TypeRecord && len(branch.Fields) > 0 && len(schema.Fields) == 0 {
		return false
	}
	return SchemaAssignable(branch, schema, AssignCoerciveMode)
}

func schemaFitsTarget(target, actual *TypeDescriptor) bool {
	return SchemaAssignable(target, actual, AssignCoerciveMode)
}

func schemaAssignable(target, actual *TypeDescriptor, mode SchemaAssignMode) bool {
	target = NormalizeSchema(target)
	actual = NormalizeSchema(actual)
	if target == nil || actual == nil {
		return target == nil && actual == nil
	}
	if actual.Kind == TypeNull {
		return target.Kind == TypeNull || target.Nullable || target.Kind == TypeMixed
	}
	if actual.Nullable && !target.Nullable {
		return false
	}
	if target.Kind == TypeMixed {
		return true
	}
	if target.Kind == TypeUnion && actual.Kind == TypeUnion {
		for _, branch := range actual.Branches {
			if !unionHasBranchForSchemaMode(target, branch, mode) {
				return false
			}
		}
		return true
	}
	if target.Kind == TypeUnion {
		return unionHasBranchForSchemaMode(target, actual, mode)
	}
	if actual.Kind == TypeUnion {
		for _, branch := range actual.Branches {
			if !schemaAssignable(target, branch, mode) {
				return false
			}
		}
		return true
	}
	if mode == AssignCoerciveMode && target.Kind == TypeFloat && actual.Kind == TypeInt {
		return true
	}
	if target.Kind != actual.Kind {
		return false
	}

	switch target.Kind {
	case TypeRecord:
		if mode == AssignExactMode && len(target.Fields) != len(actual.Fields) {
			return false
		}
		targetFields := make(map[string]*TypeDescriptor, len(target.Fields))
		for i := range target.Fields {
			field := &target.Fields[i]
			targetFields[field.Name] = field.Type
		}
		actualFields := make(map[string]bool, len(actual.Fields))
		for i := range actual.Fields {
			field := &actual.Fields[i]
			targetField, ok := targetFields[field.Name]
			if !ok || !schemaAssignable(targetField, field.Type, mode) {
				return false
			}
			actualFields[field.Name] = true
		}
		if mode == AssignCoerciveMode {
			for i := range target.Fields {
				field := &target.Fields[i]
				if actualFields[field.Name] {
					continue
				}
				fieldSchema := FinalizeSchema(field.Type)
				if fieldSchema == nil || !fieldSchema.Nullable {
					return false
				}
			}
		}
	case TypeList:
		return schemaAssignable(target.Elem, actual.Elem, mode)
	}
	return true
}

func unifyRecordFieldsStrict(a, b []FieldDescriptor, path string, allowMixed bool) ([]FieldDescriptor, error) {
	aByName := make(map[string]*TypeDescriptor, len(a))
	bByName := make(map[string]*TypeDescriptor, len(b))
	namesSet := make(map[string]bool, len(a)+len(b))
	for _, field := range a {
		aByName[field.Name] = field.Type
		namesSet[field.Name] = true
	}
	for _, field := range b {
		bByName[field.Name] = field.Type
		namesSet[field.Name] = true
	}
	names := make([]string, 0, len(namesSet))
	for name := range namesSet {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]FieldDescriptor, 0, len(names))
	for _, name := range names {
		at, aOK := aByName[name]
		bt, bOK := bByName[name]
		switch {
		case aOK && bOK:
			merged, err := unifyStrictAtPath(at, bt, joinSchemaPath(path, name), allowMixed)
			if err != nil {
				return nil, err
			}
			out = append(out, FieldDescriptor{Name: name, Type: merged})
		case aOK:
			typ := cloneTypeDescriptor(at)
			if typ != nil {
				typ.Nullable = true
			}
			out = append(out, FieldDescriptor{Name: name, Type: typ})
		case bOK:
			typ := cloneTypeDescriptor(bt)
			if typ != nil {
				typ.Nullable = true
			}
			out = append(out, FieldDescriptor{Name: name, Type: typ})
		}
	}
	return out, nil
}

func numericResultAtPath(a, b *TypeDescriptor, path string) (*TypeDescriptor, error) {
	a = NormalizeSchema(a)
	b = NormalizeSchema(b)
	if a == nil || b == nil {
		return nil, fmt.Errorf("numeric result requires numeric operands")
	}
	if a.Kind == TypeNull {
		if !IsNumeric(b) && b.Kind != TypeNull {
			return nil, fmt.Errorf("numeric result requires numeric operands, got %s", Render(b))
		}
		out := cloneTypeDescriptor(b)
		out.Nullable = true
		return out, nil
	}
	if b.Kind == TypeNull {
		if !IsNumeric(a) {
			return nil, fmt.Errorf("numeric result requires numeric operands, got %s", Render(a))
		}
		out := cloneTypeDescriptor(a)
		out.Nullable = true
		return out, nil
	}
	if !IsNumeric(a) {
		return nil, fmt.Errorf("numeric result requires numeric operands, got %s", Render(a))
	}
	if !IsNumeric(b) {
		return nil, fmt.Errorf("numeric result requires numeric operands, got %s", Render(b))
	}
	if a.Kind == TypeFloat || b.Kind == TypeFloat {
		return &TypeDescriptor{Kind: TypeFloat, Nullable: a.Nullable || b.Nullable}, nil
	}
	return &TypeDescriptor{Kind: TypeInt, Nullable: a.Nullable || b.Nullable}, nil
}

func appendSchemaPath(path, suffix string) string {
	if path == "" {
		return suffix
	}
	return path + suffix
}

func mergeValueSchemaStrict(schema *TypeDescriptor, v Value, path string) (*TypeDescriptor, error) {
	switch v.Type {
	case TypeNull:
		if schema == nil {
			return &TypeDescriptor{Kind: TypeNull, Nullable: true}, nil
		}
		schema.Nullable = true
		return schema, nil
	case TypeInt, TypeFloat, TypeString, TypeBool:
		return mergeSchemaKindStrict(schema, v.Type, path)
	case TypeRecord:
		if err := validateStrictMergeRecordFields(v, path); err != nil {
			return nil, err
		}
		return mergeRecordValueSchemaStrict(schema, v.Fields, path)
	case TypeList:
		if err := validateStrictMergeRecordFields(v, path); err != nil {
			return nil, err
		}
		next := InferValueSchema(v)
		return mergeSchemaDescriptorStrict(schema, next, path)
	case TypeUnion:
		return mergeSchemaKindStrict(schema, v.Type, path)
	default:
		return mergeSchemaKindStrict(schema, TypeString, path)
	}
}

func mergeSchemaKindStrict(schema *TypeDescriptor, kind ValueType, path string) (*TypeDescriptor, error) {
	if schema == nil {
		return &TypeDescriptor{Kind: kind}, nil
	}
	switch schema.Kind {
	case TypeNull:
		schema.Kind = kind
		schema.Nullable = true
		return schema, nil
	case TypeUnion:
		if unionHasBranchForSchema(schema, ScalarSchema(kind)) {
			return schema, nil
		}
	case kind:
		return schema, nil
	case TypeMixed:
		return schema, nil
	case TypeInt:
		if kind == TypeFloat {
			schema.Kind = TypeFloat
			return schema, nil
		}
	case TypeFloat:
		if kind == TypeInt {
			return schema, nil
		}
	}
	return nil, &SchemaError{Path: path, Expected: FinalizeSchema(schema), Actual: ScalarSchema(kind)}
}

func validateStrictMergeRecordFields(v Value, path string) error {
	switch v.Type {
	case TypeRecord:
		if err := validateUniqueRecordFieldNames(v.Fields, path); err != nil {
			return err
		}
		for _, field := range v.Fields {
			if err := validateStrictMergeRecordFields(field.Value, joinSchemaPath(path, field.Name)); err != nil {
				return err
			}
		}
	case TypeList:
		for _, item := range v.List {
			if err := validateStrictMergeRecordFields(item, appendSchemaPath(path, "[]")); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateUniqueRecordFieldNames(fields []RecordField, path string) error {
	seen := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		if _, ok := seen[field.Name]; ok {
			return fmt.Errorf("%s duplicate record field", joinSchemaPath(path, field.Name))
		}
		seen[field.Name] = struct{}{}
	}
	return nil
}

func mergeRecordValueSchemaStrict(schema *TypeDescriptor, fields []RecordField, path string) (*TypeDescriptor, error) {
	if schema == nil {
		return newRecordSchemaFromFields(fields, path, false)
	}
	if schema.Kind == TypeNull {
		out, err := newRecordSchemaFromFields(fields, path, true)
		if err != nil {
			return nil, err
		}
		return out, nil
	}
	if schema.Kind == TypeMixed {
		return schema, nil
	}
	if schema.Kind == TypeUnion {
		next := InferValueSchema(RecordVal(fields))
		merged, err := unifyUnionStrictAtPath(schema, next, path)
		if err != nil {
			return nil, err
		}
		return cloneTypeDescriptor(merged), nil
	}
	if schema.Kind != TypeRecord {
		return nil, &SchemaError{
			Path:     path,
			Expected: FinalizeSchema(schema),
			Actual:   FinalizeSchema(InferValueSchema(RecordVal(fields))),
		}
	}

	if sameRecordFieldNames(schema.Fields, fields) {
		for i, field := range fields {
			merged, err := mergeValueSchemaStrict(schema.Fields[i].Type, field.Value, joinSchemaPath(path, field.Name))
			if err != nil {
				return nil, err
			}
			schema.Fields[i].Type = merged
		}
		return schema, nil
	}

	values := make(map[string]Value, len(fields))
	for _, field := range fields {
		values[field.Name] = field.Value
	}

	for i := range schema.Fields {
		field := &schema.Fields[i]
		if v, ok := values[field.Name]; ok {
			merged, err := mergeValueSchemaStrict(field.Type, v, joinSchemaPath(path, field.Name))
			if err != nil {
				return nil, err
			}
			field.Type = merged
		} else {
			field.Type.Nullable = true
		}
	}

	existing := make(map[string]bool, len(schema.Fields))
	for _, field := range schema.Fields {
		existing[field.Name] = true
	}
	for _, field := range fields {
		if existing[field.Name] {
			continue
		}
		typ, err := mergeValueSchemaStrict(nil, field.Value, joinSchemaPath(path, field.Name))
		if err != nil {
			return nil, err
		}
		typ.Nullable = true
		schema.Fields = append(schema.Fields, FieldDescriptor{Name: field.Name, Type: typ})
	}
	sort.Slice(schema.Fields, func(i, j int) bool { return schema.Fields[i].Name < schema.Fields[j].Name })
	return schema, nil
}

func newRecordSchemaFromFields(fields []RecordField, path string, nullable bool) (*TypeDescriptor, error) {
	out := &TypeDescriptor{Kind: TypeRecord, Nullable: nullable, Fields: make([]FieldDescriptor, len(fields))}
	for i, field := range fields {
		typ, err := mergeValueSchemaStrict(nil, field.Value, joinSchemaPath(path, field.Name))
		if err != nil {
			return nil, err
		}
		out.Fields[i] = FieldDescriptor{Name: field.Name, Type: typ}
	}
	sort.Slice(out.Fields, func(i, j int) bool { return out.Fields[i].Name < out.Fields[j].Name })
	return out, nil
}

func sameRecordFieldNames(schemaFields []FieldDescriptor, fields []RecordField) bool {
	if len(schemaFields) != len(fields) {
		return false
	}
	for i := range schemaFields {
		if schemaFields[i].Name != fields[i].Name {
			return false
		}
	}
	return true
}

func mergeSchemaDescriptorStrict(schema, next *TypeDescriptor, path string) (*TypeDescriptor, error) {
	if schema == nil {
		return cloneTypeDescriptor(next), nil
	}
	if next == nil {
		return schema, nil
	}
	if next.Kind == TypeNull {
		schema.Nullable = true
		return schema, nil
	}
	if schema.Kind == TypeNull {
		out := cloneTypeDescriptor(next)
		out.Nullable = true
		return out, nil
	}
	if schema.Kind == TypeMixed || next.Kind == TypeMixed {
		if schema.Kind == TypeUnion {
			return schema, nil
		}
		if next.Kind == TypeUnion {
			return next, nil
		}
		schema.Kind = TypeMixed
		schema.Nullable = schema.Nullable || next.Nullable
		schema.Fields = nil
		schema.Elem = nil
		schema.Branches = nil
		return schema, nil
	}
	if schema.Kind == TypeUnion || next.Kind == TypeUnion {
		merged, err := unifyUnionStrictAtPath(schema, next, path)
		if err != nil {
			return nil, err
		}
		return cloneTypeDescriptor(merged), nil
	}
	if schema.Kind == TypeInt && next.Kind == TypeFloat || schema.Kind == TypeFloat && next.Kind == TypeInt {
		schema.Kind = TypeFloat
		schema.Nullable = schema.Nullable || next.Nullable
		return schema, nil
	}
	if schema.Kind != next.Kind {
		return nil, &SchemaError{Path: path, Expected: FinalizeSchema(schema), Actual: FinalizeSchema(next)}
	}

	schema.Nullable = schema.Nullable || next.Nullable
	switch schema.Kind {
	case TypeRecord:
		if sameDescriptorFieldNames(schema.Fields, next.Fields) {
			for i := range schema.Fields {
				merged, err := mergeSchemaDescriptorStrict(schema.Fields[i].Type, next.Fields[i].Type, joinSchemaPath(path, schema.Fields[i].Name))
				if err != nil {
					return nil, err
				}
				schema.Fields[i].Type = merged
			}
			return schema, nil
		}
		return mergeRecordDescriptorFieldsStrict(schema, next, path)
	case TypeList:
		elem, err := mergeSchemaDescriptorStrict(schema.Elem, next.Elem, path+"[]")
		if err != nil {
			return nil, err
		}
		schema.Elem = elem
	}
	return schema, nil
}

func sameDescriptorFieldNames(a, b []FieldDescriptor) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Name != b[i].Name {
			return false
		}
	}
	return true
}

func mergeRecordDescriptorFieldsStrict(schema, next *TypeDescriptor, path string) (*TypeDescriptor, error) {
	nextByName := make(map[string]*TypeDescriptor, len(next.Fields))
	for _, field := range next.Fields {
		nextByName[field.Name] = field.Type
	}
	existing := make(map[string]bool, len(schema.Fields))
	for i := range schema.Fields {
		field := &schema.Fields[i]
		existing[field.Name] = true
		if nextField, ok := nextByName[field.Name]; ok {
			merged, err := mergeSchemaDescriptorStrict(field.Type, nextField, joinSchemaPath(path, field.Name))
			if err != nil {
				return nil, err
			}
			field.Type = merged
		} else {
			field.Type.Nullable = true
		}
	}
	for _, field := range next.Fields {
		if existing[field.Name] {
			continue
		}
		typ := cloneTypeDescriptor(field.Type)
		typ.Nullable = true
		schema.Fields = append(schema.Fields, FieldDescriptor{Name: field.Name, Type: typ})
	}
	sort.Slice(schema.Fields, func(i, j int) bool { return schema.Fields[i].Name < schema.Fields[j].Name })
	return schema, nil
}

func mergeSchemas(a, b *TypeDescriptor, strict bool, path string) (*TypeDescriptor, error) {
	if a == nil {
		return cloneTypeDescriptor(b), nil
	}
	if b == nil {
		return cloneTypeDescriptor(a), nil
	}
	if a.Kind == TypeNull {
		out := cloneTypeDescriptor(b)
		out.Nullable = true
		return out, nil
	}
	if b.Kind == TypeNull {
		out := cloneTypeDescriptor(a)
		out.Nullable = true
		return out, nil
	}
	if a.Kind == TypeMixed || b.Kind == TypeMixed {
		return &TypeDescriptor{Kind: TypeMixed, Nullable: a.Nullable || b.Nullable}, nil
	}
	if a.Kind == TypeUnion || b.Kind == TypeUnion {
		return mergeUnionSchemas(a, b, strict, path)
	}

	if a.Kind == TypeInt && b.Kind == TypeFloat || a.Kind == TypeFloat && b.Kind == TypeInt {
		return &TypeDescriptor{Kind: TypeFloat, Nullable: a.Nullable || b.Nullable}, nil
	}

	if a.Kind != b.Kind {
		if strict {
			return nil, &SchemaError{Path: path, Expected: FinalizeSchema(a), Actual: FinalizeSchema(b)}
		}
		return &TypeDescriptor{Kind: TypeString, Nullable: a.Nullable || b.Nullable}, nil
	}

	out := &TypeDescriptor{Kind: a.Kind, Nullable: a.Nullable || b.Nullable}
	switch a.Kind {
	case TypeRecord:
		fields, err := mergeRecordFields(a.Fields, b.Fields, strict, path)
		if err != nil {
			return nil, err
		}
		out.Fields = fields
	case TypeList:
		elem, err := mergeSchemas(a.Elem, b.Elem, strict, path+"[]")
		if err != nil {
			return nil, err
		}
		out.Elem = elem
	}
	return out, nil
}

func mergeUnionSchemas(a, b *TypeDescriptor, strict bool, path string) (*TypeDescriptor, error) {
	if strict {
		return unifyUnionStrictAtPath(a, b, path)
	}
	branches := make([]*TypeDescriptor, 0)
	nullable := false
	if a.Kind == TypeUnion {
		nullable = nullable || a.Nullable
		for _, branch := range a.Branches {
			branches = addUnionBranch(branches, branch)
		}
	} else {
		nullable = nullable || a.Nullable
		branches = addUnionBranch(branches, a)
	}
	if b.Kind == TypeUnion {
		nullable = nullable || b.Nullable
		for _, branch := range b.Branches {
			branches = addUnionBranch(branches, branch)
		}
	} else {
		nullable = nullable || b.Nullable
		branches = addUnionBranch(branches, b)
	}
	return UnionOf(branches, nullable), nil
}

func mergeListElementSchemas(a, b *TypeDescriptor) (*TypeDescriptor, error) {
	return mergeSchemasForMixedList(a, b, "")
}

func mergeSchemasForMixedList(a, b *TypeDescriptor, path string) (*TypeDescriptor, error) {
	if a == nil {
		return cloneTypeDescriptor(b), nil
	}
	if b == nil {
		return cloneTypeDescriptor(a), nil
	}
	if a.Kind == TypeNull {
		out := cloneTypeDescriptor(b)
		out.Nullable = true
		return out, nil
	}
	if b.Kind == TypeNull {
		out := cloneTypeDescriptor(a)
		out.Nullable = true
		return out, nil
	}
	if a.Kind == TypeMixed || b.Kind == TypeMixed {
		if a.Kind == TypeUnion {
			return a, nil
		}
		if b.Kind == TypeUnion {
			return b, nil
		}
		return &TypeDescriptor{Kind: TypeMixed, Nullable: a.Nullable || b.Nullable}, nil
	}
	if a.Kind == TypeUnion || b.Kind == TypeUnion {
		return mergeUnionSchemas(a, b, false, path)
	}
	if a.Kind == TypeInt && b.Kind == TypeFloat || a.Kind == TypeFloat && b.Kind == TypeInt {
		return &TypeDescriptor{Kind: TypeFloat, Nullable: a.Nullable || b.Nullable}, nil
	}
	if a.Kind != b.Kind {
		return &TypeDescriptor{Kind: TypeMixed, Nullable: a.Nullable || b.Nullable}, nil
	}

	out := &TypeDescriptor{Kind: a.Kind, Nullable: a.Nullable || b.Nullable}
	switch a.Kind {
	case TypeRecord:
		fields, err := mergeRecordFieldsForMixedList(a.Fields, b.Fields, path)
		if err != nil {
			return nil, err
		}
		out.Fields = fields
	case TypeList:
		elem, err := mergeSchemasForMixedList(a.Elem, b.Elem, path+"[]")
		if err != nil {
			return nil, err
		}
		out.Elem = elem
	}
	return out, nil
}

func mergeRecordFieldsForMixedList(a, b []FieldDescriptor, path string) ([]FieldDescriptor, error) {
	byName := map[string]*TypeDescriptor{}
	presentA := map[string]bool{}
	presentB := map[string]bool{}
	for _, f := range a {
		byName[f.Name] = cloneTypeDescriptor(f.Type)
		presentA[f.Name] = true
	}
	for _, f := range b {
		fieldPath := joinSchemaPath(path, f.Name)
		if existing, ok := byName[f.Name]; ok {
			merged, err := mergeSchemasForMixedList(existing, f.Type, fieldPath)
			if err != nil {
				return nil, err
			}
			byName[f.Name] = merged
		} else {
			ft := cloneTypeDescriptor(f.Type)
			ft.Nullable = true
			byName[f.Name] = ft
		}
		presentB[f.Name] = true
	}
	for name, typ := range byName {
		if !presentA[name] || !presentB[name] {
			typ.Nullable = true
		}
	}
	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]FieldDescriptor, len(names))
	for i, name := range names {
		out[i] = FieldDescriptor{Name: name, Type: byName[name]}
	}
	return out, nil
}

func mergeRecordFields(a, b []FieldDescriptor, strict bool, path string) ([]FieldDescriptor, error) {
	byName := map[string]*TypeDescriptor{}
	presentA := map[string]bool{}
	presentB := map[string]bool{}
	for _, f := range a {
		byName[f.Name] = cloneTypeDescriptor(f.Type)
		presentA[f.Name] = true
	}
	for _, f := range b {
		fieldPath := joinSchemaPath(path, f.Name)
		if existing, ok := byName[f.Name]; ok {
			merged, err := mergeSchemas(existing, f.Type, strict, fieldPath)
			if err != nil {
				return nil, err
			}
			byName[f.Name] = merged
		} else {
			ft := cloneTypeDescriptor(f.Type)
			ft.Nullable = true
			byName[f.Name] = ft
		}
		presentB[f.Name] = true
	}
	for name, typ := range byName {
		if !presentA[name] || !presentB[name] {
			typ.Nullable = true
		}
	}
	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]FieldDescriptor, len(names))
	for i, name := range names {
		out[i] = FieldDescriptor{Name: name, Type: byName[name]}
	}
	return out, nil
}

func joinSchemaPath(base, name string) string {
	if base == "" {
		return name
	}
	return base + "." + name
}

// FinalizeSchema converts non-determining null-only portions to nullable string.
func FinalizeSchema(schema *TypeDescriptor) *TypeDescriptor {
	if schema == nil {
		return nil
	}
	out := cloneTypeDescriptor(schema)
	finalizeSchemaInPlace(out)
	return out
}

func finalizeSchemaInPlace(schema *TypeDescriptor) {
	if schema == nil {
		return
	}
	if schema.Kind == TypeNull {
		schema.Kind = TypeString
		schema.Nullable = true
	}
	switch schema.Kind {
	case TypeRecord:
		for i := range schema.Fields {
			schema.Fields[i].Type = FinalizeSchema(schema.Fields[i].Type)
		}
	case TypeList:
		if schema.Elem == nil {
			schema.Elem = &TypeDescriptor{Kind: TypeString, Nullable: true}
		} else {
			schema.Elem = FinalizeSchema(schema.Elem)
		}
	case TypeUnion:
		for i := range schema.Branches {
			schema.Branches[i] = FinalizeSchema(schema.Branches[i])
		}
	}
}

// CoerceValueToSchema validates and coerces a value against a finalized schema.
func CoerceValueToSchema(v Value, schema *TypeDescriptor) (Value, error) {
	return CoerceValueToSchemaMode(v, schema, CoerceCoerciveMode)
}

// CoerceValueToSchemaAtPath validates and coerces with a diagnostic path prefix.
func CoerceValueToSchemaAtPath(v Value, schema *TypeDescriptor, path string) (Value, error) {
	return CoerceValueToSchemaModeAtPath(v, schema, CoerceCoerciveMode, path)
}

func coerceValueToSchema(v Value, schema *TypeDescriptor, path string) (Value, error) {
	if schema == nil {
		return v, nil
	}
	if v.Type == TypeNull {
		return Null(), nil
	}
	if schema.Kind == TypeMixed {
		return v, nil
	}
	if schema.Kind == TypeUnion {
		return coerceUnionValueToSchema(v, schema, path)
	}
	if schema.Kind == TypeFloat && v.Type == TypeInt {
		return FloatVal(float64(v.Int)), nil
	}
	if v.Type != schema.Kind {
		return Null(), &SchemaError{Path: path, Expected: schema, Actual: FinalizeSchema(InferValueSchema(v))}
	}
	switch schema.Kind {
	case TypeRecord:
		return coerceRecordToSchema(v, schema, path)
	case TypeList:
		var items []Value
		for i, item := range v.List {
			cv, err := coerceValueToSchema(item, schema.Elem, path+"[]")
			if err != nil {
				return Null(), err
			}
			if items != nil {
				items[i] = cv
				continue
			}
			if !sameCoercedValue(cv, item) {
				items = make([]Value, len(v.List))
				copy(items, v.List[:i])
				items[i] = cv
			}
		}
		if items != nil {
			return ListVal(items), nil
		}
		return v, nil
	default:
		return v, nil
	}
}

func coerceUnionValueToSchema(v Value, schema *TypeDescriptor, path string) (Value, error) {
	for _, branch := range schema.Branches {
		cv, err := coerceValueToExactUnionBranch(v, branch, path)
		if err == nil {
			return cv, nil
		}
	}
	for _, branch := range schema.Branches {
		cv, err := coerceValueToUnionBranch(v, branch, path)
		if err == nil {
			return cv, nil
		}
	}
	return Null(), &SchemaError{Path: path, Expected: schema, Actual: FinalizeSchema(InferValueSchema(v))}
}

func coerceUnionValueToExactSchema(v Value, schema *TypeDescriptor, path string) (Value, error) {
	for _, branch := range schema.Branches {
		cv, err := coerceValueToExactUnionBranch(v, branch, path)
		if err == nil {
			return cv, nil
		}
	}
	return Null(), &SchemaError{Path: path, Expected: schema, Actual: FinalizeSchema(InferValueSchema(v))}
}

func coerceValueToExactUnionBranch(v Value, schema *TypeDescriptor, path string) (Value, error) {
	if schema == nil {
		return v, nil
	}
	if v.Type == TypeNull {
		return Null(), nil
	}
	if schema.Kind == TypeMixed {
		return v, nil
	}
	if schema.Kind == TypeUnion {
		return coerceUnionValueToExactSchema(v, schema, path)
	}
	if v.Type != schema.Kind {
		return Null(), &SchemaError{Path: path, Expected: schema, Actual: FinalizeSchema(InferValueSchema(v))}
	}
	switch schema.Kind {
	case TypeRecord:
		return coerceRecordToExactSchemaNoTypeCoercion(v, schema, path)
	case TypeList:
		var items []Value
		for i, item := range v.List {
			cv, err := coerceValueToExactUnionBranch(item, schema.Elem, path+"[]")
			if err != nil {
				return Null(), err
			}
			if items != nil {
				items[i] = cv
				continue
			}
			if !sameCoercedValue(cv, item) {
				items = make([]Value, len(v.List))
				copy(items, v.List[:i])
				items[i] = cv
			}
		}
		if items != nil {
			return ListVal(items), nil
		}
		return v, nil
	default:
		return v, nil
	}
}

func coerceValueToUnionBranch(v Value, schema *TypeDescriptor, path string) (Value, error) {
	if schema == nil {
		return v, nil
	}
	if v.Type == TypeNull {
		return Null(), nil
	}
	if schema.Kind == TypeMixed {
		return v, nil
	}
	if schema.Kind == TypeUnion {
		return coerceUnionValueToSchema(v, schema, path)
	}
	if schema.Kind == TypeFloat && v.Type == TypeInt {
		return FloatVal(float64(v.Int)), nil
	}
	if v.Type != schema.Kind {
		return Null(), &SchemaError{Path: path, Expected: schema, Actual: FinalizeSchema(InferValueSchema(v))}
	}
	switch schema.Kind {
	case TypeRecord:
		return coerceRecordToAssignableSchema(v, schema, path)
	case TypeList:
		var items []Value
		for i, item := range v.List {
			cv, err := coerceValueToUnionBranch(item, schema.Elem, path+"[]")
			if err != nil {
				return Null(), err
			}
			if items != nil {
				items[i] = cv
				continue
			}
			if !sameCoercedValue(cv, item) {
				items = make([]Value, len(v.List))
				copy(items, v.List[:i])
				items[i] = cv
			}
		}
		if items != nil {
			return ListVal(items), nil
		}
		return v, nil
	default:
		return v, nil
	}
}

func coerceRecordToExactSchemaNoTypeCoercion(v Value, schema *TypeDescriptor, path string) (Value, error) {
	return coerceRecordToExactSchemaWith(v, schema, path, coerceValueToExactUnionBranch)
}

func coerceRecordToExactSchema(v Value, schema *TypeDescriptor, path string) (Value, error) {
	return coerceRecordToExactSchemaWith(v, schema, path, coerceValueToUnionBranch)
}

func coerceRecordToAssignableSchema(v Value, schema *TypeDescriptor, path string) (Value, error) {
	schemaFields := make(map[string]*TypeDescriptor, len(schema.Fields))
	for i := range schema.Fields {
		field := &schema.Fields[i]
		schemaFields[field.Name] = field.Type
	}
	values := make(map[string]Value, len(v.Fields))
	for _, field := range v.Fields {
		if _, exists := values[field.Name]; exists {
			return Null(), fmt.Errorf("%s duplicate record field", joinSchemaPath(path, field.Name))
		}
		if _, ok := schemaFields[field.Name]; !ok {
			return Null(), &SchemaError{Path: path, Expected: schema, Actual: FinalizeSchema(InferValueSchema(v))}
		}
		values[field.Name] = field.Value
	}

	fields := make([]RecordField, 0, len(schema.Fields))
	changed := len(v.Fields) != len(schema.Fields)
	for _, field := range schema.Fields {
		raw, ok := values[field.Name]
		if !ok {
			fieldSchema := FinalizeSchema(field.Type)
			if fieldSchema == nil || !fieldSchema.Nullable {
				return Null(), &SchemaError{Path: path, Expected: schema, Actual: FinalizeSchema(InferValueSchema(v))}
			}
			fields = append(fields, RecordField{Name: field.Name, Value: Null()})
			changed = true
			continue
		}
		cv, err := coerceValueToUnionBranch(raw, field.Type, joinSchemaPath(path, field.Name))
		if err != nil {
			return Null(), err
		}
		if !sameCoercedValue(cv, raw) {
			changed = true
		}
		fields = append(fields, RecordField{Name: field.Name, Value: cv})
	}
	if !changed && len(fields) == len(v.Fields) {
		for i := range fields {
			if v.Fields[i].Name != fields[i].Name {
				changed = true
				break
			}
		}
		if !changed {
			return v, nil
		}
	}
	return RecordVal(fields), nil
}

func coerceRecordToExactSchemaWith(
	v Value,
	schema *TypeDescriptor,
	path string,
	coerceField func(Value, *TypeDescriptor, string) (Value, error),
) (Value, error) {
	schemaFields := make(map[string]*TypeDescriptor, len(schema.Fields))
	for i := range schema.Fields {
		field := &schema.Fields[i]
		schemaFields[field.Name] = field.Type
	}
	values := make(map[string]Value, len(v.Fields))
	for _, field := range v.Fields {
		if _, exists := values[field.Name]; exists {
			return Null(), fmt.Errorf("%s duplicate record field", joinSchemaPath(path, field.Name))
		}
		if _, ok := schemaFields[field.Name]; !ok {
			return Null(), &SchemaError{Path: path, Expected: schema, Actual: FinalizeSchema(InferValueSchema(v))}
		}
		values[field.Name] = field.Value
	}
	if len(values) != len(schema.Fields) {
		return Null(), &SchemaError{Path: path, Expected: schema, Actual: FinalizeSchema(InferValueSchema(v))}
	}

	fields := make([]RecordField, 0, len(schema.Fields))
	changed := false
	for _, field := range schema.Fields {
		raw := values[field.Name]
		cv, err := coerceField(raw, field.Type, joinSchemaPath(path, field.Name))
		if err != nil {
			return Null(), err
		}
		if !sameCoercedValue(cv, raw) {
			changed = true
		}
		fields = append(fields, RecordField{Name: field.Name, Value: cv})
	}
	if !changed && len(fields) == len(v.Fields) {
		for i := range fields {
			if v.Fields[i].Name != fields[i].Name {
				changed = true
				break
			}
		}
		if !changed {
			return v, nil
		}
	}
	return RecordVal(fields), nil
}

func coerceRecordToSchema(v Value, schema *TypeDescriptor, path string) (Value, error) {
	schemaFields := make(map[string]*TypeDescriptor, len(schema.Fields))
	for i := range schema.Fields {
		field := &schema.Fields[i]
		schemaFields[field.Name] = field.Type
	}
	seen := make(map[string]bool, len(v.Fields))
	fields := make([]RecordField, 0, len(schema.Fields))
	changed := false
	for _, field := range v.Fields {
		if seen[field.Name] {
			return Null(), fmt.Errorf("%s duplicate record field", joinSchemaPath(path, field.Name))
		}
		seen[field.Name] = true
		fieldSchema, ok := schemaFields[field.Name]
		if !ok {
			changed = true
			continue
		}
		cv, err := coerceValueToSchema(field.Value, fieldSchema, joinSchemaPath(path, field.Name))
		if err != nil {
			return Null(), err
		}
		if !sameCoercedValue(cv, field.Value) {
			changed = true
		}
		fields = append(fields, RecordField{Name: field.Name, Value: cv})
	}
	for _, field := range schema.Fields {
		if seen[field.Name] {
			continue
		}
		fields = append(fields, RecordField{Name: field.Name, Value: Null()})
		changed = true
	}
	if !changed && len(fields) == len(v.Fields) {
		return v, nil
	}
	return RecordVal(fields), nil
}

func sameCoercedValue(a, b Value) bool {
	if a.Type != b.Type {
		return false
	}
	switch a.Type {
	case TypeNull:
		return true
	case TypeInt:
		return a.Int == b.Int
	case TypeFloat:
		return a.Float == b.Float
	case TypeString:
		return a.Str == b.Str
	case TypeBool:
		return a.Bool == b.Bool
	case TypeList:
		if len(a.List) != len(b.List) {
			return false
		}
		return len(a.List) == 0 || &a.List[0] == &b.List[0]
	case TypeRecord:
		if len(a.Fields) != len(b.Fields) {
			return false
		}
		return len(a.Fields) == 0 || &a.Fields[0] == &b.Fields[0]
	default:
		return false
	}
}

// SchemaAtPath resolves a nested field schema through record descriptors.
func SchemaAtPath(schema *TypeDescriptor, path []string) *TypeDescriptor {
	if schema == nil || len(path) == 0 {
		return cloneTypeDescriptor(schema)
	}
	cur := schema
	for _, seg := range path {
		if cur == nil || cur.Kind != TypeRecord {
			return nil
		}
		var next *TypeDescriptor
		for _, field := range cur.Fields {
			if field.Name == seg {
				next = field.Type
				break
			}
		}
		if next == nil {
			return nil
		}
		cur = next
	}
	return cloneTypeDescriptor(cur)
}

// String returns a deterministic compact recursive type string.
func (schema *TypeDescriptor) String() string {
	return Render(schema)
}

func writeSchemaString(b *strings.Builder, schema *TypeDescriptor) {
	if schema == nil {
		b.WriteString("null")
		return
	}
	switch schema.Kind {
	case TypeRecord:
		b.WriteString("record<")
		for i, field := range schema.Fields {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(field.Name)
			b.WriteString(":")
			writeSchemaString(b, field.Type)
		}
		b.WriteString(">")
	case TypeList:
		b.WriteString("list<")
		writeSchemaString(b, schema.Elem)
		b.WriteString(">")
	case TypeUnion:
		b.WriteString("union<")
		for i, branch := range schema.Branches {
			if i > 0 {
				b.WriteString(",")
			}
			writeSchemaString(b, branch)
		}
		b.WriteString(">")
	default:
		b.WriteString(TypeName(schema.Kind))
	}
	if schema.Nullable {
		b.WriteString("?")
	}
}

func normalizeDescriptorForCompare(schema *TypeDescriptor) *TypeDescriptor {
	if schema == nil {
		return nil
	}
	out := cloneTypeDescriptor(schema)
	normalizeDescriptorInPlace(out)
	return out
}

func normalizeDescriptorInPlace(schema *TypeDescriptor) {
	if schema == nil {
		return
	}
	switch schema.Kind {
	case TypeRecord:
		for i := range schema.Fields {
			schema.Fields[i].Type = normalizeDescriptorForCompare(schema.Fields[i].Type)
		}
		sort.SliceStable(schema.Fields, func(i, j int) bool {
			if schema.Fields[i].Name == schema.Fields[j].Name {
				return i < j
			}
			return schema.Fields[i].Name < schema.Fields[j].Name
		})
	case TypeList:
		schema.Elem = normalizeDescriptorForCompare(schema.Elem)
	case TypeUnion:
		canonical := unionSchema(schema.Branches, schema.Nullable)
		*schema = *canonical
	}
}

func sameNormalized(a, b *TypeDescriptor) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	if a.Kind != b.Kind || a.Nullable != b.Nullable {
		return false
	}
	switch a.Kind {
	case TypeRecord:
		if len(a.Fields) != len(b.Fields) {
			return false
		}
		for i := range a.Fields {
			if a.Fields[i].Name != b.Fields[i].Name || !sameNormalized(a.Fields[i].Type, b.Fields[i].Type) {
				return false
			}
		}
	case TypeList:
		return sameNormalized(a.Elem, b.Elem)
	case TypeUnion:
		if len(a.Branches) != len(b.Branches) {
			return false
		}
		for i := range a.Branches {
			if !sameNormalized(a.Branches[i], b.Branches[i]) {
				return false
			}
		}
	}
	return true
}

func cloneTypeDescriptor(schema *TypeDescriptor) *TypeDescriptor {
	if schema == nil {
		return nil
	}
	out := &TypeDescriptor{
		Kind:     schema.Kind,
		Nullable: schema.Nullable,
	}
	if len(schema.Fields) > 0 {
		out.Fields = make([]FieldDescriptor, len(schema.Fields))
		for i, field := range schema.Fields {
			out.Fields[i] = FieldDescriptor{Name: field.Name, Type: cloneTypeDescriptor(field.Type)}
		}
	}
	out.Elem = cloneTypeDescriptor(schema.Elem)
	if len(schema.Branches) > 0 {
		out.Branches = make([]*TypeDescriptor, len(schema.Branches))
		for i, branch := range schema.Branches {
			out.Branches[i] = cloneTypeDescriptor(branch)
		}
	}
	return out
}
