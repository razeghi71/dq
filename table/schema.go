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
}

// FieldDescriptor describes one named record field.
type FieldDescriptor struct {
	Name string
	Type *TypeDescriptor
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
	return mergeSchemas(a, b, false, "")
}

// MergeSchemasStrict merges schemas and rejects incompatible non-null types.
func MergeSchemasStrict(a, b *TypeDescriptor) (*TypeDescriptor, error) {
	return mergeSchemas(a, b, true, "")
}

// MergeSchemasStrictAtPath is MergeSchemasStrict with a diagnostic path prefix.
func MergeSchemasStrictAtPath(a, b *TypeDescriptor, path string) (*TypeDescriptor, error) {
	return mergeSchemas(a, b, true, path)
}

// MergeValueSchemaStrictAtPath merges one materialized value into an existing
// descriptor and rejects incompatible non-null types. The returned descriptor
// may share storage with the input descriptor.
func MergeValueSchemaStrictAtPath(schema *TypeDescriptor, v Value, path string) (*TypeDescriptor, error) {
	return mergeValueSchemaStrict(schema, v, path)
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
		return mergeRecordValueSchemaStrict(schema, v.Fields, path)
	case TypeList:
		next := InferValueSchema(v)
		return mergeSchemaDescriptorStrict(schema, next, path)
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
		schema.Kind = TypeMixed
		schema.Nullable = schema.Nullable || next.Nullable
		schema.Fields = nil
		schema.Elem = nil
		return schema, nil
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
		return &TypeDescriptor{Kind: TypeMixed, Nullable: a.Nullable || b.Nullable}, nil
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
	}
}

// CoerceValueToSchema validates and coerces a value against a finalized schema.
func CoerceValueToSchema(v Value, schema *TypeDescriptor) (Value, error) {
	return coerceValueToSchema(v, FinalizeSchema(schema), "")
}

// CoerceValueToSchemaAtPath validates and coerces with a diagnostic path prefix.
func CoerceValueToSchemaAtPath(v Value, schema *TypeDescriptor, path string) (Value, error) {
	return coerceValueToSchema(v, FinalizeSchema(schema), path)
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

func coerceRecordToSchema(v Value, schema *TypeDescriptor, path string) (Value, error) {
	if len(v.Fields) == len(schema.Fields) {
		var fields []RecordField
		sameShape := true
		for i, field := range schema.Fields {
			if v.Fields[i].Name != field.Name {
				sameShape = false
				break
			}
			cv, err := coerceValueToSchema(v.Fields[i].Value, field.Type, joinSchemaPath(path, field.Name))
			if err != nil {
				return Null(), err
			}
			if fields != nil {
				fields[i] = RecordField{Name: field.Name, Value: cv}
				continue
			}
			if !sameCoercedValue(cv, v.Fields[i].Value) {
				fields = make([]RecordField, len(schema.Fields))
				copy(fields, v.Fields[:i])
				fields[i] = RecordField{Name: field.Name, Value: cv}
			}
		}
		if sameShape {
			if fields != nil {
				return RecordVal(fields), nil
			}
			return v, nil
		}
	}

	values := make(map[string]Value, len(v.Fields))
	for _, f := range v.Fields {
		values[f.Name] = f.Value
	}
	fields := make([]RecordField, len(schema.Fields))
	for i, field := range schema.Fields {
		fv, ok := values[field.Name]
		if !ok {
			fields[i] = RecordField{Name: field.Name, Value: Null()}
			continue
		}
		cv, err := coerceValueToSchema(fv, field.Type, joinSchemaPath(path, field.Name))
		if err != nil {
			return Null(), err
		}
		fields[i] = RecordField{Name: field.Name, Value: cv}
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
	if schema == nil {
		return "null"
	}
	var b strings.Builder
	writeSchemaString(&b, schema)
	return b.String()
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
	default:
		b.WriteString(TypeName(schema.Kind))
	}
	if schema.Nullable {
		b.WriteString("?")
	}
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
	return out
}
