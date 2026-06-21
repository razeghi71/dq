package table

import "fmt"

// SchemaUnifyMode selects how two declared schemas are combined.
type SchemaUnifyMode int

const (
	// UnifyStrictMode rejects incompatible non-null types.
	UnifyStrictMode SchemaUnifyMode = iota
	// UnifyPermissiveMode preserves legacy AddRow widening semantics.
	UnifyPermissiveMode
	// UnifyListLiteralMode permits mixed only for explicit heterogeneous list
	// literals or single native array values.
	UnifyListLiteralMode
)

// SchemaAssignMode selects whether assignability allows coercive storage
// compatibility such as int values fitting float schemas.
type SchemaAssignMode int

const (
	// AssignExactMode requires exact structural shape apart from accepted nulls.
	AssignExactMode SchemaAssignMode = iota
	// AssignCoerciveMode allows documented storage-compatible coercions.
	AssignCoerciveMode
)

// ValueCoerceMode selects value coercion behavior against a declared schema.
type ValueCoerceMode int

const (
	// CoerceExactMode validates exact branch shape without numeric coercion.
	CoerceExactMode ValueCoerceMode = iota
	// CoerceCoerciveMode allows documented storage-compatible coercions.
	CoerceCoerciveMode
)

// NormalizeSchema returns the deterministic canonical descriptor for dq's
// structural type system. Record fields are sorted; union branches are
// normalized but remain ordered because branch order is observable when multiple
// branches can accept a coerced value.
func NormalizeSchema(schema *TypeDescriptor) *TypeDescriptor {
	return normalizeDescriptorForCompare(schema)
}

// EquivalentSchema reports whether two schemas are the same after
// normalization. Union branch order is significant by design.
func EquivalentSchema(a, b *TypeDescriptor) bool {
	return sameNormalized(NormalizeSchema(a), NormalizeSchema(b))
}

// UnionOf builds a normalized structural union descriptor. Source-level branch
// tags are not retained; structurally equivalent branches collapse.
func UnionOf(branches []*TypeDescriptor, nullable bool) *TypeDescriptor {
	return unionSchema(branches, nullable)
}

// UnifySchemas merges two schemas using the selected type-system mode.
func UnifySchemas(a, b *TypeDescriptor, mode SchemaUnifyMode) (*TypeDescriptor, error) {
	return UnifySchemasAtPath(a, b, mode, "")
}

// UnifySchemasAtPath is UnifySchemas with a diagnostic path prefix.
func UnifySchemasAtPath(a, b *TypeDescriptor, mode SchemaUnifyMode, path string) (*TypeDescriptor, error) {
	if err := ValidateSchemaAtPath(a, path); err != nil {
		return nil, err
	}
	if err := ValidateSchemaAtPath(b, path); err != nil {
		return nil, err
	}
	switch mode {
	case UnifyStrictMode:
		return unifyStrictAtPath(a, b, path, false)
	case UnifyPermissiveMode:
		return mergeSchemas(a, b, false, path)
	case UnifyListLiteralMode:
		return mergeSchemasForMixedList(a, b, path)
	default:
		return unifyStrictAtPath(a, b, path, false)
	}
}

// SchemaAssignable reports whether values with actual schema can be assigned to
// target under the selected mode.
func SchemaAssignable(target, actual *TypeDescriptor, mode SchemaAssignMode) bool {
	if ValidateSchema(target) != nil || ValidateSchema(actual) != nil {
		return false
	}
	return schemaAssignable(target, actual, mode)
}

// CoerceValueToSchemaMode validates and coerces a value against a finalized
// target schema using the selected coercion mode.
func CoerceValueToSchemaMode(v Value, schema *TypeDescriptor, mode ValueCoerceMode) (Value, error) {
	return CoerceValueToSchemaModeAtPath(v, schema, mode, "")
}

// CoerceValueToSchemaModeAtPath is CoerceValueToSchemaMode with a diagnostic
// path prefix.
func CoerceValueToSchemaModeAtPath(v Value, schema *TypeDescriptor, mode ValueCoerceMode, path string) (Value, error) {
	if err := ValidateSchemaAtPath(schema, path); err != nil {
		return Null(), err
	}
	schema = FinalizeSchema(schema)
	switch mode {
	case CoerceExactMode:
		return coerceValueToExactUnionBranch(v, schema, path)
	case CoerceCoerciveMode:
		return coerceValueToSchema(v, schema, path)
	default:
		return coerceValueToSchema(v, schema, path)
	}
}

// CoerceValueToFinalSchemaMode validates and coerces a value against a schema
// the caller has already finalized and cached.
func CoerceValueToFinalSchemaMode(v Value, schema *TypeDescriptor, mode ValueCoerceMode) (Value, error) {
	return CoerceValueToFinalSchemaModeAtPath(v, schema, mode, "")
}

// CoerceValueToFinalSchemaModeAtPath is CoerceValueToFinalSchemaMode with a
// diagnostic path prefix.
func CoerceValueToFinalSchemaModeAtPath(v Value, schema *TypeDescriptor, mode ValueCoerceMode, path string) (Value, error) {
	if err := ValidateSchemaAtPath(schema, path); err != nil {
		return Null(), err
	}
	switch mode {
	case CoerceExactMode:
		return coerceValueToExactUnionBranch(v, schema, path)
	case CoerceCoerciveMode:
		return coerceValueToSchema(v, schema, path)
	default:
		return coerceValueToSchema(v, schema, path)
	}
}

// ValidateSchema rejects ambiguous descriptors such as records with duplicate
// field names. Nil schemas are accepted as unknown schemas.
func ValidateSchema(schema *TypeDescriptor) error {
	return ValidateSchemaAtPath(schema, "")
}

// ValidateSchemaAtPath is ValidateSchema with a diagnostic path prefix.
func ValidateSchemaAtPath(schema *TypeDescriptor, path string) error {
	if schema == nil {
		return nil
	}
	switch schema.Kind {
	case TypeRecord:
		seen := make(map[string]struct{}, len(schema.Fields))
		for _, field := range schema.Fields {
			if _, ok := seen[field.Name]; ok {
				return fmt.Errorf("%s duplicate record field", joinSchemaPath(path, field.Name))
			}
			seen[field.Name] = struct{}{}
			if err := ValidateSchemaAtPath(field.Type, joinSchemaPath(path, field.Name)); err != nil {
				return err
			}
		}
	case TypeList:
		return ValidateSchemaAtPath(schema.Elem, appendSchemaPath(path, "[]"))
	case TypeUnion:
		for _, branch := range schema.Branches {
			if err := ValidateSchemaAtPath(branch, path); err != nil {
				return err
			}
		}
	}
	return nil
}
