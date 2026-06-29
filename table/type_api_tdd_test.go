package table

import (
	"strings"
	"testing"
)

func mixedType() *TypeDescriptor {
	return &TypeDescriptor{Kind: TypeMixed}
}

func nullLiteralType() *TypeDescriptor {
	return &TypeDescriptor{Kind: TypeNull, Nullable: true}
}

func TestCentralTypeAPIHelpersDoNotMutateInputs(t *testing.T) {
	base := recordOf(
		field("id", td(TypeInt)),
		field("meta", recordOf(field("tag", nullable(TypeString)))),
	)

	withNull := WithNullable(base)
	requireSchemaString(t, withNull, "record<id:int, meta:record<tag:string?>>?")
	requireSchemaString(t, base, "record<id:int, meta:record<tag:string?>>")

	withoutNull := WithoutNull(withNull)
	requireSchemaString(t, withoutNull, "record<id:int, meta:record<tag:string?>>")
	requireSchemaString(t, withNull, "record<id:int, meta:record<tag:string?>>?")

	if !Same(base, withoutNull) {
		t.Fatalf("Same should compare logical structure, got base=%s withoutNull=%s", Render(base), Render(withoutNull))
	}
	if Same(base, withNull) {
		t.Fatalf("Same should include nullability, got base=%s withNull=%s", Render(base), Render(withNull))
	}
}

func TestCentralTypeAPIRenderAndSameAreDeterministic(t *testing.T) {
	left := recordOf(
		field("y", td(TypeString)),
		field("x", nullable(TypeInt)),
	)
	right := recordOf(
		field("x", nullable(TypeInt)),
		field("y", td(TypeString)),
	)

	if got, want := Render(left), "record<x:int?, y:string>"; got != want {
		t.Fatalf("Render should sort record fields by name: got %s, want %s", got, want)
	}
	if got, want := Render(right), "record<x:int?, y:string>"; got != want {
		t.Fatalf("Render should be independent of input field order: got %s, want %s", got, want)
	}
	if !Same(left, right) {
		t.Fatalf("Same should ignore record field input order: left=%s right=%s", Render(left), Render(right))
	}
}

func TestCentralTypeAPIValidateSchemaRestrictsMixedToListElements(t *testing.T) {
	valid := []struct {
		name   string
		schema *TypeDescriptor
	}{
		{name: "list_mixed", schema: listOf(mixedType())},
		{name: "list_record_field_mixed", schema: listOf(recordOf(field("amount", mixedType())))},
		{name: "record_list_mixed", schema: recordOf(field("items", listOf(recordOf(field("amount", mixedType())))))},
	}

	for _, tc := range valid {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateSchema(tc.schema); err != nil {
				t.Fatalf("ValidateSchema(%s) returned error: %v", Render(tc.schema), err)
			}
		})
	}

	invalid := []struct {
		name   string
		path   string
		schema *TypeDescriptor
	}{
		{name: "top_level_mixed", path: "<value>", schema: mixedType()},
		{name: "record_field_mixed", path: "payload", schema: recordOf(field("payload", mixedType()))},
		{name: "union_branch_mixed", path: "<value>", schema: UnionOf([]*TypeDescriptor{td(TypeString), mixedType()}, false)},
	}

	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateSchema(tc.schema)
			if err == nil {
				t.Fatal("expected ValidateSchema to reject mixed outside list elements")
			}
			want := tc.path + " mixed schema is only valid inside list elements"
			if err.Error() != want {
				t.Fatalf("ValidateSchema error: got %q, want %q", err.Error(), want)
			}
		})
	}
}

func TestCentralTypeAPIStructuralUnionContract(t *testing.T) {
	one := UnionOf([]*TypeDescriptor{td(TypeInt), nullable(TypeInt), nil, nullLiteralType()}, false)
	requireSchemaString(t, one, "int?")

	nested := UnionOf([]*TypeDescriptor{
		UnionOf([]*TypeDescriptor{td(TypeInt), td(TypeString)}, false),
		UnionOf([]*TypeDescriptor{td(TypeString), td(TypeBool)}, true),
	}, false)
	requireSchemaString(t, nested, "union<int,string,bool>?")

	again := UnionOf([]*TypeDescriptor{nested, nested}, false)
	if !EquivalentSchema(nested, again) {
		t.Fatalf("UnionOf should be idempotent: got %s then %s", Render(nested), Render(again))
	}

	leftGrouped := UnionOf([]*TypeDescriptor{
		UnionOf([]*TypeDescriptor{td(TypeInt), td(TypeString)}, false),
		td(TypeBool),
	}, true)
	rightGrouped := UnionOf([]*TypeDescriptor{
		td(TypeInt),
		UnionOf([]*TypeDescriptor{td(TypeString), td(TypeBool)}, true),
	}, false)
	if !EquivalentSchema(leftGrouped, rightGrouped) {
		t.Fatalf("UnionOf should be associative for stable branch order: left=%s right=%s", Render(leftGrouped), Render(rightGrouped))
	}

	reordered := UnionOf([]*TypeDescriptor{td(TypeString), td(TypeInt)}, false)
	if EquivalentSchema(UnionOf([]*TypeDescriptor{td(TypeInt), td(TypeString)}, false), reordered) {
		t.Fatal("union branch order is intentionally significant for coercion behavior")
	}
}

func TestCentralTypeAPIAssignabilityModes(t *testing.T) {
	if SchemaAssignable(td(TypeFloat), td(TypeInt), AssignExactMode) {
		t.Fatal("exact assignability should not allow int into float")
	}
	if !SchemaAssignable(td(TypeFloat), td(TypeInt), AssignCoerciveMode) {
		t.Fatal("coercive assignability should allow int into float")
	}

	target := UnionOf([]*TypeDescriptor{
		recordOf(field("x", td(TypeFloat))),
		td(TypeString),
	}, false)
	actual := recordOf(field("x", td(TypeInt)))
	if SchemaAssignable(target, actual, AssignExactMode) {
		t.Fatal("exact union assignability should preserve numeric branch identity")
	}
	if !SchemaAssignable(target, actual, AssignCoerciveMode) {
		t.Fatal("coercive union assignability should accept int value into float branch")
	}

	recordTarget := recordOf(
		field("x", td(TypeInt)),
		field("y", nullable(TypeString)),
	)
	recordActual := recordOf(field("x", td(TypeInt)))
	if SchemaAssignable(recordTarget, recordActual, AssignExactMode) {
		t.Fatal("exact record assignability should require all fields")
	}
	if !SchemaAssignable(recordTarget, recordActual, AssignCoerciveMode) {
		t.Fatal("coercive record assignability should allow missing nullable fields")
	}

	if SchemaAssignable(mixedType(), td(TypeBool), AssignCoerciveMode) {
		t.Fatal("top-level mixed assignability should be rejected by schema validation")
	}
	if SchemaAssignable(recordOf(field("payload", mixedType())), recordOf(field("payload", td(TypeBool))), AssignCoerciveMode) {
		t.Fatal("record-field mixed assignability should be rejected outside list elements")
	}
	if !SchemaAssignable(listOf(mixedType()), listOf(td(TypeBool)), AssignCoerciveMode) {
		t.Fatal("list<mixed> assignability should accept heterogeneous list element values")
	}

	value := RecordVal([]RecordField{{Name: "x", Value: IntVal(1)}})
	coerced, err := CoerceValueToSchemaMode(value, recordTarget, CoerceCoerciveMode)
	if err != nil {
		t.Fatalf("coercive record value returned error: %v", err)
	}
	requireRecordValue(t, coerced, []RecordField{
		{Name: "x", Value: IntVal(1)},
		{Name: "y", Value: Null()},
	})
}

func TestCentralTypeAPICoercionModes(t *testing.T) {
	schema := recordOf(field("x", td(TypeFloat)))
	value := RecordVal([]RecordField{{Name: "x", Value: IntVal(7)}})

	if _, err := CoerceValueToSchemaMode(value, schema, CoerceExactMode); err == nil {
		t.Fatal("exact coercion should reject int into float field")
	}
	got, err := CoerceValueToSchemaMode(value, schema, CoerceCoerciveMode)
	if err != nil {
		t.Fatalf("coercive mode returned error: %v", err)
	}
	fields := map[string]Value{}
	for _, field := range got.Fields {
		fields[field.Name] = field.Value
	}
	if got := fields["x"]; got.Type != TypeFloat || got.Float != 7 {
		t.Fatalf("coercive mode: got %v, want float 7", got)
	}

	finalSchema := FinalizeSchema(schema)
	got, err = CoerceValueToFinalSchemaMode(value, finalSchema, CoerceCoerciveMode)
	if err != nil {
		t.Fatalf("final-schema coercive mode returned error: %v", err)
	}
	fields = map[string]Value{}
	for _, field := range got.Fields {
		fields[field.Name] = field.Value
	}
	if got := fields["x"]; got.Type != TypeFloat || got.Float != 7 {
		t.Fatalf("final-schema coercive mode: got %v, want float 7", got)
	}
	if _, err := CoerceValueToFinalSchemaMode(value, finalSchema, CoerceExactMode); err == nil {
		t.Fatal("final-schema exact coercion should reject int into float field")
	}

	permissiveSchema := recordOf(
		field("x", td(TypeString)),
		field("items", listOf(recordOf(field("amount", td(TypeString))))),
		field("missing", nullable(TypeString)),
	)
	permissiveValue := RecordVal([]RecordField{
		{Name: "x", Value: IntVal(7)},
		{Name: "items", Value: ListVal([]Value{
			RecordVal([]RecordField{{Name: "amount", Value: IntVal(9)}}),
		})},
	})
	got, err = CoerceValueToSchemaMode(permissiveValue, permissiveSchema, CoercePermissiveMode)
	if err != nil {
		t.Fatalf("permissive mode returned error: %v", err)
	}
	if !Equal(got, RecordVal([]RecordField{
		{Name: "x", Value: StrVal("7")},
		{Name: "items", Value: ListVal([]Value{
			RecordVal([]RecordField{{Name: "amount", Value: StrVal("9")}}),
		})},
		{Name: "missing", Value: Null()},
	})) {
		t.Fatalf("permissive mode: got %s", got.AsString())
	}

	got, err = CoerceValueToFinalSchemaMode(IntVal(42), td(TypeString), CoercePermissiveMode)
	if err != nil {
		t.Fatalf("final-schema permissive mode returned error: %v", err)
	}
	if got.Type != TypeString || got.Str != "42" {
		t.Fatalf("final-schema permissive mode: got %v, want string 42", got)
	}
	if _, err := CoerceValueToSchemaMode(IntVal(1), td(TypeBool), CoercePermissiveMode); err == nil {
		t.Fatal("permissive mode should reject values that cannot fit the target schema after widening")
	}

	if _, err := CoerceValueToSchema(BoolVal(true), mixedType()); err == nil {
		t.Fatal("top-level mixed coercion should be rejected by schema validation")
	} else if got, want := err.Error(), "<value> mixed schema is only valid inside list elements"; got != want {
		t.Fatalf("top-level mixed coercion error: got %q, want %q", got, want)
	}
}

func TestCentralTypeAPIPredicates(t *testing.T) {
	tests := []struct {
		name       string
		schema     *TypeDescriptor
		numeric    bool
		boolLike   bool
		stringLike bool
		comparable bool
		orderable  bool
	}{
		{name: "int", schema: td(TypeInt), numeric: true, comparable: true, orderable: true},
		{name: "nullable_float", schema: nullable(TypeFloat), numeric: true, comparable: true, orderable: true},
		{name: "string", schema: td(TypeString), stringLike: true, comparable: true, orderable: true},
		{name: "nullable_bool", schema: nullable(TypeBool), boolLike: true, comparable: true},
		{name: "record", schema: recordOf(field("x", td(TypeInt))), comparable: true},
		{name: "list", schema: listOf(td(TypeString)), comparable: true},
		{name: "mixed", schema: mixedType()},
		{name: "null_literal", schema: nullLiteralType()},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsNumeric(tc.schema); got != tc.numeric {
				t.Fatalf("IsNumeric(%s) = %v, want %v", Render(tc.schema), got, tc.numeric)
			}
			if got := IsBooleanLike(tc.schema); got != tc.boolLike {
				t.Fatalf("IsBooleanLike(%s) = %v, want %v", Render(tc.schema), got, tc.boolLike)
			}
			if got := IsStringLike(tc.schema); got != tc.stringLike {
				t.Fatalf("IsStringLike(%s) = %v, want %v", Render(tc.schema), got, tc.stringLike)
			}
			if got := IsComparable(tc.schema); got != tc.comparable {
				t.Fatalf("IsComparable(%s) = %v, want %v", Render(tc.schema), got, tc.comparable)
			}
			if got := IsOrderable(tc.schema); got != tc.orderable {
				t.Fatalf("IsOrderable(%s) = %v, want %v", Render(tc.schema), got, tc.orderable)
			}
		})
	}
}

func TestCentralTypeAPIUnifyStrictHappyPaths(t *testing.T) {
	tests := []struct {
		name string
		a    *TypeDescriptor
		b    *TypeDescriptor
		want string
	}{
		{name: "int_int", a: td(TypeInt), b: td(TypeInt), want: "int"},
		{name: "int_float", a: td(TypeInt), b: td(TypeFloat), want: "float"},
		{name: "nullable_int_int", a: nullable(TypeInt), b: td(TypeInt), want: "int?"},
		{name: "null_string", a: nullLiteralType(), b: td(TypeString), want: "string?"},
		{
			name: "record_numeric_promotion",
			a:    recordOf(field("x", td(TypeInt))),
			b:    recordOf(field("x", td(TypeFloat))),
			want: "record<x:float>",
		},
		{
			name: "record_field_union_marks_missing_nullable",
			a:    recordOf(field("x", td(TypeInt))),
			b:    recordOf(field("y", td(TypeString))),
			want: "record<x:int?, y:string?>",
		},
		{name: "list_numeric_promotion", a: listOf(td(TypeInt)), b: listOf(td(TypeFloat)), want: "list<float>"},
		{name: "list_mixed_schema_remains_mixed", a: listOf(mixedType()), b: listOf(td(TypeInt)), want: "list<mixed>"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			origA := Render(tc.a)
			origB := Render(tc.b)

			got, err := UnifyStrict(tc.a, tc.b)
			if err != nil {
				t.Fatalf("UnifyStrict returned error: %v", err)
			}
			requireSchemaString(t, got, tc.want)

			if Render(tc.a) != origA || Render(tc.b) != origB {
				t.Fatalf("UnifyStrict mutated inputs: before=(%s,%s) after=(%s,%s)", origA, origB, Render(tc.a), Render(tc.b))
			}
		})
	}
}

func TestCentralTypeAPIUnifyStrictRejectsIncompatibleTypes(t *testing.T) {
	cases := []struct {
		name         string
		a            *TypeDescriptor
		b            *TypeDescriptor
		wantPath     string
		wantExpected string
		wantActual   string
	}{
		{name: "int_string", a: td(TypeInt), b: td(TypeString), wantPath: "", wantExpected: "int", wantActual: "string"},
		{name: "bool_int", a: td(TypeBool), b: td(TypeInt), wantPath: "", wantExpected: "bool", wantActual: "int"},
		{name: "record_scalar", a: recordOf(field("x", td(TypeInt))), b: td(TypeInt), wantPath: "", wantExpected: "record<x:int>", wantActual: "int"},
		{
			name:         "record_field_conflict",
			a:            recordOf(field("s", recordOf(field("x", td(TypeInt))))),
			b:            recordOf(field("s", recordOf(field("x", td(TypeString))))),
			wantPath:     "s.x",
			wantExpected: "int",
			wantActual:   "string",
		},
		{
			name:         "list_element_conflict",
			a:            listOf(td(TypeInt)),
			b:            listOf(td(TypeString)),
			wantPath:     "[]",
			wantExpected: "int",
			wantActual:   "string",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := UnifyStrict(tc.a, tc.b)
			requireSchemaError(t, err, tc.wantPath, tc.wantExpected, tc.wantActual)
		})
	}
}

func TestCentralTypeAPIUnifyStrictRejectsTopLevelMixedAsInvalidSchema(t *testing.T) {
	_, err := UnifyStrict(mixedType(), td(TypeInt))
	if err == nil {
		t.Fatal("expected UnifyStrict to reject top-level mixed before unification")
	}
	if got, want := err.Error(), "<value> mixed schema is only valid inside list elements"; got != want {
		t.Fatalf("UnifyStrict top-level mixed error: got %q, want %q", got, want)
	}
}

func TestCentralTypeAPIUnifyAllStrict(t *testing.T) {
	inputs := []*TypeDescriptor{
		td(TypeInt),
		nullable(TypeInt),
		td(TypeFloat),
		nullLiteralType(),
	}

	got, err := UnifyAllStrict(inputs)
	if err != nil {
		t.Fatalf("UnifyAllStrict returned error: %v", err)
	}
	requireSchemaString(t, got, "float?")

	_, err = UnifyAllStrict([]*TypeDescriptor{td(TypeInt), td(TypeString)})
	if err == nil {
		t.Fatal("expected UnifyAllStrict to reject incompatible scalar types")
	}
	if !strings.Contains(err.Error(), "expected int, got string") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCentralTypeAPIUnifyListLiteralElemsUsesMixedOnlyForLiteralHeterogeneity(t *testing.T) {
	tests := []struct {
		name string
		in   []*TypeDescriptor
		want string
	}{
		{name: "numeric_promotion", in: []*TypeDescriptor{td(TypeInt), td(TypeFloat)}, want: "float"},
		{name: "nullability", in: []*TypeDescriptor{td(TypeInt), nullLiteralType()}, want: "int?"},
		{name: "scalar_heterogeneous", in: []*TypeDescriptor{td(TypeInt), td(TypeString)}, want: "mixed"},
		{
			name: "record_field_heterogeneous",
			in: []*TypeDescriptor{
				recordOf(field("amount", td(TypeInt))),
				recordOf(field("amount", td(TypeString))),
			},
			want: "record<amount:mixed>",
		},
		{
			name: "record_missing_fields",
			in: []*TypeDescriptor{
				recordOf(field("a", td(TypeInt))),
				recordOf(field("b", td(TypeString))),
			},
			want: "record<a:int?, b:string?>",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := UnifyListLiteralElems(tc.in)
			requireSchemaString(t, got, tc.want)
		})
	}
}

func TestCentralTypeAPIEmptyListLiteralElementSchemaIsNonDetermining(t *testing.T) {
	elem := UnifyListLiteralElems(nil)
	if elem != nil {
		t.Fatalf("empty list literal element schema: got %s, want nil", elem.String())
	}
	requireSchemaString(t, FinalizeSchema(&TypeDescriptor{Kind: TypeList, Elem: elem}), "list<string?>")

	got, err := UnifyStrict(
		&TypeDescriptor{Kind: TypeList, Elem: elem},
		&TypeDescriptor{Kind: TypeList, Elem: td(TypeInt)},
	)
	if err != nil {
		t.Fatalf("empty list should unify with typed list: %v", err)
	}
	requireSchemaString(t, got, "list<int>")
}

func TestCentralTypeAPINumericResult(t *testing.T) {
	tests := []struct {
		name string
		a    *TypeDescriptor
		b    *TypeDescriptor
		want string
	}{
		{name: "int_int", a: td(TypeInt), b: td(TypeInt), want: "int"},
		{name: "int_float", a: td(TypeInt), b: td(TypeFloat), want: "float"},
		{name: "nullable_int_float", a: nullable(TypeInt), b: td(TypeFloat), want: "float?"},
		{name: "null_int", a: nullLiteralType(), b: td(TypeInt), want: "int?"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NumericResult(tc.a, tc.b)
			if err != nil {
				t.Fatalf("NumericResult returned error: %v", err)
			}
			requireSchemaString(t, got, tc.want)
		})
	}

	_, err := NumericResult(td(TypeString), td(TypeInt))
	if err == nil {
		t.Fatal("expected NumericResult to reject string operands")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "numeric") || !strings.Contains(strings.ToLower(err.Error()), "string") {
		t.Fatalf("unexpected NumericResult error: %v", err)
	}
}

func TestCentralTypeAPITableSchemaReturnsLogicalColumns(t *testing.T) {
	tbl := NewTableWithSchemas([]string{"id", "profile"}, []*TypeDescriptor{
		td(TypeInt),
		recordOf(field("name", td(TypeString)), field("age", td(TypeInt))),
	})
	if err := tbl.AddRowTyped([]Value{
		IntVal(1),
		RecordVal([]RecordField{{Name: "name", Value: StrVal("Alice")}, {Name: "age", Value: IntVal(30)}}),
	}); err != nil {
		t.Fatalf("AddRowTyped returned error: %v", err)
	}

	schema := tbl.Schema()
	if len(schema.Columns) != 2 {
		t.Fatalf("schema columns: got %d, want 2", len(schema.Columns))
	}
	if schema.Columns[0].Name != "id" || Render(schema.Columns[0].Type) != "int" {
		t.Fatalf("first schema column: got %#v", schema.Columns[0])
	}
	if schema.Columns[1].Name != "profile" || Render(schema.Columns[1].Type) != "record<age:int, name:string>" {
		t.Fatalf("second schema column: got name=%q type=%s", schema.Columns[1].Name, Render(schema.Columns[1].Type))
	}

	schema.Columns[1].Type.Fields[0].Type.Kind = TypeBool
	if got := Render(tbl.Schema().Columns[1].Type); got != "record<age:int, name:string>" {
		t.Fatalf("mutating returned schema changed table schema: got %s", got)
	}
}

func TestCentralTypeAPITableSchemaUsesLogicalNamesAfterShallowClone(t *testing.T) {
	tbl := NewTable([]string{"old"})
	tbl.AddRow([]Value{IntVal(1)})

	renamed := tbl.ShallowClone([]string{"new"})
	schema := renamed.Schema()

	if got, want := renamed.Columns[0], "new"; got != want {
		t.Fatalf("logical column name: got %q, want %q", got, want)
	}
	if got, want := schema.Columns[0].Name, "new"; got != want {
		t.Fatalf("schema column name: got %q, want %q", got, want)
	}
	if got, want := schema.Columns[0].Type.String(), "int"; got != want {
		t.Fatalf("schema column type: got %q, want %q", got, want)
	}
}

func TestWithDeepNullableMarksEveryKnownSchemaPosition(t *testing.T) {
	schema := recordOf(
		field("id", td(TypeInt)),
		field("items", listOf(recordOf(field("amount", td(TypeFloat))))),
	)

	got := WithDeepNullable(schema)
	requireSchemaString(t, got, "record<id:int?, items:list<record<amount:float?>?>?>?")
	if schema.Nullable {
		t.Fatal("WithDeepNullable mutated input top-level nullability")
	}
}
