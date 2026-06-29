package table

import (
	"strings"
	"testing"
)

func td(kind ValueType) *TypeDescriptor {
	return &TypeDescriptor{Kind: kind}
}

func nullable(kind ValueType) *TypeDescriptor {
	return &TypeDescriptor{Kind: kind, Nullable: true}
}

func listOf(elem *TypeDescriptor) *TypeDescriptor {
	return &TypeDescriptor{Kind: TypeList, Elem: elem}
}

func recordOf(fields ...FieldDescriptor) *TypeDescriptor {
	return &TypeDescriptor{Kind: TypeRecord, Fields: fields}
}

func field(name string, typ *TypeDescriptor) FieldDescriptor {
	return FieldDescriptor{Name: name, Type: typ}
}

func requireSchemaString(t *testing.T, got *TypeDescriptor, want string) {
	t.Helper()
	if got == nil {
		t.Fatalf("schema: got nil, want %s", want)
	}
	if got.String() != want {
		t.Fatalf("schema: got %s, want %s", got.String(), want)
	}
}

func requireSchemaError(t *testing.T, err error, wantPath, wantExpected, wantActual string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected schema error")
	}
	se, ok := err.(*SchemaError)
	if !ok {
		t.Fatalf("error type: got %T, want *SchemaError: %v", err, err)
	}
	if se.Path != wantPath {
		t.Fatalf("error path: got %q, want %q", se.Path, wantPath)
	}
	if se.Expected.String() != wantExpected {
		t.Fatalf("expected schema: got %s, want %s", se.Expected.String(), wantExpected)
	}
	if se.Actual.String() != wantActual {
		t.Fatalf("actual schema: got %s, want %s", se.Actual.String(), wantActual)
	}
}

func TestMergeSchemasStrictPromotesNumericAtDepth(t *testing.T) {
	a := recordOf(field("s", recordOf(field("x", td(TypeInt)))))
	b := recordOf(field("s", recordOf(field("x", td(TypeFloat)))))

	got, err := MergeSchemasStrict(a, b)
	if err != nil {
		t.Fatalf("MergeSchemasStrict returned error: %v", err)
	}

	requireSchemaString(t, got, "record<s:record<x:float>>")
}

func TestMergeSchemasStrictUnionsRecordFieldsWithPresenceNullability(t *testing.T) {
	a := recordOf(
		field("id", td(TypeInt)),
		field("s", recordOf(field("x", td(TypeInt)))),
	)
	b := recordOf(
		field("extra", td(TypeBool)),
		field("id", td(TypeInt)),
		field("s", recordOf(field("y", td(TypeString)))),
	)

	got, err := MergeSchemasStrict(a, b)
	if err != nil {
		t.Fatalf("MergeSchemasStrict returned error: %v", err)
	}

	requireSchemaString(t, got, "record<extra:bool?, id:int, s:record<x:int?, y:string?>>")
}

func TestMergeSchemasStrictMergesListOfList(t *testing.T) {
	a := listOf(listOf(td(TypeInt)))
	b := listOf(listOf(td(TypeFloat)))

	got, err := MergeSchemasStrict(a, b)
	if err != nil {
		t.Fatalf("MergeSchemasStrict returned error: %v", err)
	}

	requireSchemaString(t, got, "list<list<float>>")
}

func TestUnionSchemaNormalizationBranches(t *testing.T) {
	requireSchemaString(t, UnionSchema([]*TypeDescriptor{nil, td(TypeNull)}, false), "string?")
	requireSchemaString(t, UnionSchema([]*TypeDescriptor{td(TypeInt), td(TypeFloat)}, true), "float?")
	requireSchemaString(t, UnionSchema([]*TypeDescriptor{td(TypeInt), nullable(TypeInt)}, false), "int?")
	requireSchemaString(t, UnionSchema([]*TypeDescriptor{td(TypeInt), nullable(TypeString)}, false), "union<int,string>?")

	nested := UnionSchema([]*TypeDescriptor{
		{Kind: TypeUnion, Nullable: true, Branches: []*TypeDescriptor{td(TypeString), td(TypeBool)}},
		td(TypeString),
	}, false)
	requireSchemaString(t, nested, "union<string,bool>?")

	records := UnionSchema([]*TypeDescriptor{
		recordOf(field("x", td(TypeInt))),
		recordOf(field("y", td(TypeInt))),
		recordOf(field("x", td(TypeInt))),
	}, false)
	requireSchemaString(t, records, "union<record<x:int>,record<y:int>>")

	nullableRecordBranch := UnionSchema([]*TypeDescriptor{
		recordOf(field("x", td(TypeInt)), field("y", td(TypeString))),
		td(TypeString),
		recordOf(field("x", td(TypeInt)), field("y", nullable(TypeString))),
	}, false)
	requireSchemaString(t, nullableRecordBranch, "union<record<x:int, y:string?>,string>")

	sparseRecordBranch := UnionSchema([]*TypeDescriptor{
		recordOf(field("x", td(TypeInt))),
		td(TypeString),
		recordOf(field("x", td(TypeInt)), field("y", td(TypeString))),
	}, false)
	requireSchemaString(t, sparseRecordBranch, "union<record<x:int>,string,record<x:int, y:string>>")
}

func TestNormalizeSchemaCanonicalizesUnions(t *testing.T) {
	raw := &TypeDescriptor{Kind: TypeUnion, Branches: []*TypeDescriptor{
		nullable(TypeInt),
		td(TypeString),
	}}
	canonical := UnionOf([]*TypeDescriptor{nullable(TypeInt), td(TypeString)}, false)

	requireSchemaString(t, NormalizeSchema(raw), "union<int,string>?")
	requireSchemaString(t, canonical, "union<int,string>?")
	if !EquivalentSchema(raw, canonical) {
		t.Fatalf("EquivalentSchema returned false for raw %s and canonical %s", raw, canonical)
	}
}

func TestUnifyUnionStrictAtPathBranches(t *testing.T) {
	left := UnionSchema([]*TypeDescriptor{td(TypeInt), td(TypeString)}, false)
	right := UnionSchema([]*TypeDescriptor{td(TypeString), td(TypeBool)}, true)

	got, err := unifyUnionStrictAtPath(left, right, "u")
	if err != nil {
		t.Fatalf("unify union/union: %v", err)
	}
	requireSchemaString(t, got, "union<int,string,bool>?")

	got, err = unifyUnionStrictAtPath(left, td(TypeInt), "u")
	if err != nil {
		t.Fatalf("unify union/schema: %v", err)
	}
	requireSchemaString(t, got, "union<int,string>")

	got, err = unifyUnionStrictAtPath(td(TypeInt), left, "u")
	if err != nil {
		t.Fatalf("unify schema/union: %v", err)
	}
	requireSchemaString(t, got, "union<int,string>")

	_, err = unifyUnionStrictAtPath(left, td(TypeBool), "u")
	requireSchemaError(t, err, "u", "union<int,string>", "bool")
}

func TestUnifyUnionStrictMergesCompatibleBranchNullability(t *testing.T) {
	left := UnionSchema([]*TypeDescriptor{
		recordOf(field("x", td(TypeInt)), field("y", td(TypeString))),
		td(TypeString),
	}, false)
	right := recordOf(field("x", td(TypeInt)), field("y", nullable(TypeString)))

	got, err := UnifyStrict(left, right)
	if err != nil {
		t.Fatalf("UnifyStrict returned error: %v", err)
	}
	requireSchemaString(t, got, "union<record<x:int, y:string?>,string>")
}

func TestUnifyUnionStrictLiftsBranchNullabilityToUnion(t *testing.T) {
	left := UnionSchema([]*TypeDescriptor{td(TypeInt), td(TypeString)}, false)

	got, err := UnifyStrict(left, nullable(TypeInt))
	if err != nil {
		t.Fatalf("UnifyStrict returned error: %v", err)
	}
	requireSchemaString(t, got, "union<int,string>?")
}

func TestUnifyUnionStrictRejectsSparseRecordBranchMerge(t *testing.T) {
	left := UnionSchema([]*TypeDescriptor{
		recordOf(field("x", td(TypeInt)), field("y", td(TypeString))),
		td(TypeString),
	}, false)
	right := recordOf(field("x", td(TypeInt)))

	_, err := UnifyStrict(left, right)
	requireSchemaError(t, err, "", "union<record<x:int, y:string>,string>", "record<x:int>")
}

func TestMergeSchemasStrictReportsNestedConflictPaths(t *testing.T) {
	t.Run("record field", func(t *testing.T) {
		a := recordOf(field("s", recordOf(field("x", td(TypeInt)))))
		b := recordOf(field("s", recordOf(field("x", td(TypeString)))))

		_, err := MergeSchemasStrict(a, b)

		requireSchemaError(t, err, "s.x", "int", "string")
		if got, want := err.Error(), "s.x expected int, got string"; got != want {
			t.Fatalf("Error(): got %q, want %q", got, want)
		}
	})

	t.Run("list record field", func(t *testing.T) {
		a := listOf(recordOf(field("amt", td(TypeInt))))
		b := listOf(recordOf(field("amt", td(TypeString))))

		_, err := MergeSchemasStrictAtPath(a, b, "orders")

		requireSchemaError(t, err, "orders[].amt", "int", "string")
	})
}

func TestMergeSchemasStrictRejectsDuplicateFieldDescriptors(t *testing.T) {
	invalid := recordOf(
		field("x", td(TypeInt)),
		field("x", td(TypeInt)),
	)

	_, err := MergeSchemasStrict(invalid, cloneTypeDescriptor(invalid))
	if err == nil {
		t.Fatal("expected duplicate field descriptor error")
	}
	if got, want := err.Error(), "x duplicate record field"; got != want {
		t.Fatalf("error: got %q, want %q", got, want)
	}
}

func TestInferValueSchemaUsesMixedOnlyWithinOneList(t *testing.T) {
	tests := []struct {
		name string
		v    Value
		want string
	}{
		{
			name: "scalar conflict",
			v:    ListVal([]Value{IntVal(1), StrVal("two")}),
			want: "list<mixed>",
		},
		{
			name: "scalar record conflict",
			v:    ListVal([]Value{IntVal(1), RecordVal([]RecordField{{Name: "a", Value: IntVal(2)}})}),
			want: "list<mixed>",
		},
		{
			name: "record field conflict",
			v: ListVal([]Value{
				RecordVal([]RecordField{{Name: "amt", Value: IntVal(1)}}),
				RecordVal([]RecordField{{Name: "amt", Value: StrVal("x")}}),
			}),
			want: "list<record<amt:mixed>>",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			requireSchemaString(t, InferValueSchema(tc.v), tc.want)
		})
	}
}

func TestInferValueSchemaDuplicateRecordFieldsDefensiveFallback(t *testing.T) {
	v := RecordVal([]RecordField{
		{Name: "x", Value: IntVal(1)},
		{Name: "x", Value: IntVal(2)},
	})

	requireSchemaString(t, InferValueSchema(v), "string?")
}

func TestMergeSchemasStrictRejectsCrossRowListElementConflict(t *testing.T) {
	a := InferValueSchema(ListVal([]Value{IntVal(1)}))
	b := InferValueSchema(ListVal([]Value{StrVal("two")}))

	_, err := MergeSchemasStrictAtPath(a, b, "xs")

	requireSchemaError(t, err, "xs[]", "int", "string")
}

func TestMergeValueSchemaStrictScalarsNullsAndConflicts(t *testing.T) {
	var schema *TypeDescriptor
	var err error

	schema, err = MergeValueSchemaStrictAtPath(schema, Null(), "v")
	if err != nil {
		t.Fatalf("merge null: %v", err)
	}
	requireSchemaString(t, FinalizeSchema(schema), "string?")

	schema, err = MergeValueSchemaStrictAtPath(schema, IntVal(1), "v")
	if err != nil {
		t.Fatalf("merge int: %v", err)
	}
	requireSchemaString(t, schema, "int?")

	schema, err = MergeValueSchemaStrictAtPath(schema, FloatVal(1.5), "v")
	if err != nil {
		t.Fatalf("merge float: %v", err)
	}
	requireSchemaString(t, schema, "float?")

	_, err = MergeValueSchemaStrictAtPath(schema, StrVal("bad"), "v")
	requireSchemaError(t, err, "v", "float?", "string")
}

func TestMergeValueSchemaStrictSparseRecordsAndNestedConflicts(t *testing.T) {
	schema, err := MergeValueSchemaStrictAtPath(nil, RecordVal([]RecordField{
		{Name: "id", Value: IntVal(1)},
		{Name: "s", Value: RecordVal([]RecordField{{Name: "x", Value: IntVal(2)}})},
	}), "root")
	if err != nil {
		t.Fatalf("merge first record: %v", err)
	}

	schema, err = MergeValueSchemaStrictAtPath(schema, RecordVal([]RecordField{
		{Name: "extra", Value: BoolVal(true)},
		{Name: "id", Value: IntVal(2)},
		{Name: "s", Value: RecordVal([]RecordField{{Name: "y", Value: StrVal("yes")}})},
	}), "root")
	if err != nil {
		t.Fatalf("merge sparse record: %v", err)
	}
	requireSchemaString(t, schema, "record<extra:bool?, id:int, s:record<x:int?, y:string?>>")

	_, err = MergeValueSchemaStrictAtPath(schema, RecordVal([]RecordField{
		{Name: "id", Value: IntVal(3)},
		{Name: "s", Value: RecordVal([]RecordField{{Name: "x", Value: StrVal("bad")}})},
	}), "root")
	requireSchemaError(t, err, "root.s.x", "int?", "string")

	_, err = MergeValueSchemaStrictAtPath(schema, IntVal(4), "root")
	requireSchemaError(t, err, "root", "record<extra:bool?, id:int, s:record<x:int?, y:string?>>", "int")
}

func TestMergeValueSchemaStrictRejectsDuplicateRecordFields(t *testing.T) {
	cases := []struct {
		name     string
		schema   *TypeDescriptor
		value    Value
		basePath string
		want     string
	}{
		{
			name:     "nil_schema_top_level_record",
			basePath: "payload",
			want:     "payload.x duplicate record field",
			value: RecordVal([]RecordField{
				{Name: "x", Value: IntVal(1)},
				{Name: "x", Value: IntVal(2)},
			}),
		},
		{
			name:     "null_schema_top_level_record",
			schema:   nullable(TypeNull),
			basePath: "payload",
			want:     "payload.x duplicate record field",
			value: RecordVal([]RecordField{
				{Name: "x", Value: IntVal(1)},
				{Name: "x", Value: IntVal(2)},
			}),
		},
		{
			name:     "existing_record_schema",
			schema:   recordOf(field("x", td(TypeInt))),
			basePath: "payload",
			want:     "payload.x duplicate record field",
			value: RecordVal([]RecordField{
				{Name: "x", Value: IntVal(1)},
				{Name: "x", Value: IntVal(2)},
			}),
		},
		{
			name:     "existing_record_with_new_duplicate_field",
			schema:   recordOf(field("id", td(TypeInt))),
			basePath: "payload",
			want:     "payload.x duplicate record field",
			value: RecordVal([]RecordField{
				{Name: "id", Value: IntVal(1)},
				{Name: "x", Value: IntVal(2)},
				{Name: "x", Value: IntVal(3)},
			}),
		},
		{
			name:     "nested_record",
			basePath: "payload",
			want:     "payload.meta.score duplicate record field",
			value: RecordVal([]RecordField{
				{Name: "meta", Value: RecordVal([]RecordField{
					{Name: "score", Value: IntVal(1)},
					{Name: "score", Value: IntVal(2)},
				})},
			}),
		},
		{
			name:     "list_record_element",
			basePath: "items",
			want:     "items[].sku duplicate record field",
			value: ListVal([]Value{
				RecordVal([]RecordField{
					{Name: "sku", Value: StrVal("a")},
					{Name: "sku", Value: StrVal("b")},
				}),
			}),
		},
		{
			name:     "nested_list_record_element",
			basePath: "payload",
			want:     "payload.items[].sku duplicate record field",
			value: RecordVal([]RecordField{
				{Name: "items", Value: ListVal([]Value{
					RecordVal([]RecordField{
						{Name: "sku", Value: StrVal("a")},
						{Name: "sku", Value: StrVal("b")},
					}),
				})},
			}),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := MergeValueSchemaStrictAtPath(cloneTypeDescriptor(tc.schema), tc.value, tc.basePath)
			if err == nil {
				t.Fatal("expected duplicate record field error")
			}
			if got := err.Error(); got != tc.want {
				t.Fatalf("error: got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestMergeValueSchemaStrictListsAndMixedElements(t *testing.T) {
	schema, err := MergeValueSchemaStrictAtPath(nil, ListVal([]Value{
		RecordVal([]RecordField{{Name: "amount", Value: IntVal(1)}}),
	}), "orders")
	if err != nil {
		t.Fatalf("merge list: %v", err)
	}

	schema, err = MergeValueSchemaStrictAtPath(schema, ListVal([]Value{
		RecordVal([]RecordField{{Name: "amount", Value: FloatVal(2.5)}}),
	}), "orders")
	if err != nil {
		t.Fatalf("merge list numeric promotion: %v", err)
	}
	requireSchemaString(t, schema, "list<record<amount:float>>")

	_, err = MergeValueSchemaStrictAtPath(schema, ListVal([]Value{
		RecordVal([]RecordField{{Name: "amount", Value: StrVal("bad")}}),
	}), "orders")
	requireSchemaError(t, err, "orders[].amount", "float", "string")

	mixed, err := MergeValueSchemaStrictAtPath(nil, ListVal([]Value{IntVal(1), StrVal("two")}), "xs")
	if err != nil {
		t.Fatalf("merge mixed list: %v", err)
	}
	requireSchemaString(t, mixed, "list<mixed>")

	mixed, err = MergeValueSchemaStrictAtPath(mixed, ListVal([]Value{BoolVal(true)}), "xs")
	if err != nil {
		t.Fatalf("merge into mixed list: %v", err)
	}
	requireSchemaString(t, mixed, "list<mixed>")
}

func TestMergeValueSchemaStrictListRecordFieldUnion(t *testing.T) {
	schema, err := MergeValueSchemaStrictAtPath(nil, ListVal([]Value{
		RecordVal([]RecordField{{Name: "a", Value: IntVal(1)}}),
	}), "items")
	if err != nil {
		t.Fatalf("merge first list: %v", err)
	}

	schema, err = MergeValueSchemaStrictAtPath(schema, ListVal([]Value{
		RecordVal([]RecordField{{Name: "b", Value: StrVal("x")}}),
	}), "items")
	if err != nil {
		t.Fatalf("merge sparse list record: %v", err)
	}

	requireSchemaString(t, schema, "list<record<a:int?, b:string?>>")
}

func TestSameCoercedValueCoversAllTypes(t *testing.T) {
	xs := []Value{IntVal(1)}
	fields := []RecordField{{Name: "x", Value: IntVal(1)}}
	tests := []struct {
		name string
		a    Value
		b    Value
		want bool
	}{
		{"null", Null(), Null(), true},
		{"type mismatch", IntVal(1), FloatVal(1), false},
		{"int same", IntVal(1), IntVal(1), true},
		{"int different", IntVal(1), IntVal(2), false},
		{"float same", FloatVal(1.5), FloatVal(1.5), true},
		{"string same", StrVal("x"), StrVal("x"), true},
		{"bool same", BoolVal(true), BoolVal(true), true},
		{"same list backing", ListVal(xs), ListVal(xs), true},
		{"different list backing", ListVal([]Value{IntVal(1)}), ListVal([]Value{IntVal(1)}), false},
		{"same record backing", RecordVal(fields), RecordVal(fields), true},
		{"different record backing", RecordVal([]RecordField{{Name: "x", Value: IntVal(1)}}), RecordVal([]RecordField{{Name: "x", Value: IntVal(1)}}), false},
		{"empty list", ListVal(nil), ListVal([]Value{}), true},
		{"empty record", RecordVal(nil), RecordVal([]RecordField{}), true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := sameCoercedValue(tc.a, tc.b); got != tc.want {
				t.Fatalf("sameCoercedValue() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestFinalizeSchemaNullOnlyPortions(t *testing.T) {
	tests := []struct {
		name   string
		schema *TypeDescriptor
		want   string
	}{
		{
			name:   "top level null",
			schema: &TypeDescriptor{Kind: TypeNull, Nullable: true},
			want:   "string?",
		},
		{
			name:   "empty list",
			schema: listOf(nil),
			want:   "list<string?>",
		},
		{
			name: "record field null",
			schema: recordOf(
				field("s", &TypeDescriptor{Kind: TypeNull, Nullable: true}),
			),
			want: "record<s:string?>",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			requireSchemaString(t, FinalizeSchema(tc.schema), tc.want)
		})
	}
}

func TestSchemaStringRendersNestedNullabilityPlacement(t *testing.T) {
	schema := recordOf(
		field("id", td(TypeInt)),
		field("meta", &TypeDescriptor{
			Kind:     TypeRecord,
			Nullable: true,
			Fields: []FieldDescriptor{
				field("tag", nullable(TypeString)),
			},
		}),
		field("xs", &TypeDescriptor{
			Kind:     TypeList,
			Nullable: true,
			Elem: recordOf(
				field("amount", nullable(TypeFloat)),
			),
		}),
	)
	schema.Nullable = true

	if got, want := schema.String(), "record<id:int, meta:record<tag:string?>?, xs:list<record<amount:float?>>?>?"; got != want {
		t.Fatalf("String(): got %s, want %s", got, want)
	}
}

func TestSchemaAtPathReturnsNestedClone(t *testing.T) {
	schema := recordOf(
		field("id", td(TypeInt)),
		field("s", recordOf(field("x", td(TypeFloat)))),
	)

	got := SchemaAtPath(schema, []string{"s", "x"})
	requireSchemaString(t, got, "float")
	got.Kind = TypeString

	requireSchemaString(t, SchemaAtPath(schema, []string{"s", "x"}), "float")
	if missing := SchemaAtPath(schema, []string{"s", "missing"}); missing != nil {
		t.Fatalf("missing path: got %s, want nil", missing.String())
	}
	if notRecord := SchemaAtPath(schema, []string{"id", "x"}); notRecord != nil {
		t.Fatalf("non-record path: got %s, want nil", notRecord.String())
	}
}

func TestCoerceValueToSchemaRecursesRecordsListsAndNumericPromotion(t *testing.T) {
	schema := recordOf(
		field("amount", td(TypeFloat)),
		field("items", listOf(recordOf(
			field("sku", td(TypeString)),
			field("qty", td(TypeFloat)),
		))),
		field("missing", nullable(TypeString)),
	)
	value := RecordVal([]RecordField{
		{Name: "items", Value: ListVal([]Value{
			RecordVal([]RecordField{
				{Name: "qty", Value: IntVal(2)},
				{Name: "sku", Value: StrVal("a")},
			}),
		})},
		{Name: "amount", Value: IntVal(3)},
	})

	got, err := CoerceValueToSchema(value, schema)
	if err != nil {
		t.Fatalf("CoerceValueToSchema returned error: %v", err)
	}

	want := RecordVal([]RecordField{
		{Name: "amount", Value: FloatVal(3)},
		{Name: "items", Value: ListVal([]Value{
			RecordVal([]RecordField{
				{Name: "sku", Value: StrVal("a")},
				{Name: "qty", Value: FloatVal(2)},
			}),
		})},
		{Name: "missing", Value: Null()},
	})
	if !Equal(got, want) {
		t.Fatalf("coerced value:\ngot  %s\nwant %s", got.AsString(), want.AsString())
	}
}

func TestCoerceValueToSchemaPreservesMixedListValues(t *testing.T) {
	schema := listOf(&TypeDescriptor{Kind: TypeMixed, Nullable: true})
	value := ListVal([]Value{IntVal(1), StrVal("two"), RecordVal([]RecordField{{Name: "a", Value: IntVal(2)}})})

	got, err := CoerceValueToSchema(value, schema)
	if err != nil {
		t.Fatalf("CoerceValueToSchema returned error: %v", err)
	}
	if !Equal(got, value) {
		t.Fatalf("mixed list was not preserved: got %s, want %s", got.AsString(), value.AsString())
	}
}

func TestCoerceValueToSchemaUnionRecordBranchesPreserveExactBranch(t *testing.T) {
	schema := &TypeDescriptor{Kind: TypeUnion, Branches: []*TypeDescriptor{
		recordOf(field("x", td(TypeInt))),
		recordOf(field("y", td(TypeInt))),
	}}

	got, err := CoerceValueToSchema(RecordVal([]RecordField{{Name: "y", Value: IntVal(2)}}), schema)
	if err != nil {
		t.Fatalf("CoerceValueToSchema returned error: %v", err)
	}
	requireRecordValue(t, got, []RecordField{{Name: "y", Value: IntVal(2)}})

	got, err = CoerceValueToSchema(RecordVal([]RecordField{{Name: "x", Value: IntVal(1)}}), schema)
	if err != nil {
		t.Fatalf("CoerceValueToSchema returned error: %v", err)
	}
	requireRecordValue(t, got, []RecordField{{Name: "x", Value: IntVal(1)}})
}

func TestCoerceValueToSchemaUnionCanonicalRecordBranchFillsNullableFields(t *testing.T) {
	schema := &TypeDescriptor{Kind: TypeUnion, Branches: []*TypeDescriptor{
		recordOf(field("x", nullable(TypeInt)), field("y", nullable(TypeInt))),
		td(TypeString),
	}}

	got, err := CoerceValueToSchema(RecordVal([]RecordField{{Name: "y", Value: IntVal(2)}}), schema)
	if err != nil {
		t.Fatalf("CoerceValueToSchema returned error: %v", err)
	}
	requireRecordValue(t, got, []RecordField{
		{Name: "x", Value: Null()},
		{Name: "y", Value: IntVal(2)},
	})

	_, err = CoerceValueToSchema(RecordVal([]RecordField{{Name: "z", Value: IntVal(1)}}), schema)
	requireSchemaError(t, err, "", "union<record<x:int?, y:int?>,string>", "record<z:int>")
}

func TestCoerceValueToSchemaUnionCanonicalRecordBranchRejectsMissingRequiredField(t *testing.T) {
	schema := &TypeDescriptor{Kind: TypeUnion, Branches: []*TypeDescriptor{
		recordOf(field("x", td(TypeInt)), field("y", nullable(TypeInt))),
		td(TypeString),
	}}

	_, err := CoerceValueToSchemaAtPath(RecordVal([]RecordField{{Name: "y", Value: IntVal(2)}}), schema, "u")
	requireSchemaError(t, err, "u", "union<record<x:int, y:int?>,string>", "record<y:int>")
}

func TestCoerceValueToSchemaUnionRecordBranchesPreserveSharedFieldBranch(t *testing.T) {
	schema := &TypeDescriptor{Kind: TypeUnion, Branches: []*TypeDescriptor{
		recordOf(field("kind", td(TypeString)), field("x", td(TypeInt))),
		recordOf(field("kind", td(TypeString)), field("y", td(TypeInt))),
	}}
	value := RecordVal([]RecordField{
		{Name: "kind", Value: StrVal("right")},
		{Name: "y", Value: IntVal(2)},
	})

	got, err := CoerceValueToSchema(value, schema)
	if err != nil {
		t.Fatalf("CoerceValueToSchema returned error: %v", err)
	}
	requireRecordValue(t, got, []RecordField{
		{Name: "kind", Value: StrVal("right")},
		{Name: "y", Value: IntVal(2)},
	})
}

func TestCoerceValueToSchemaUnionRecordBranchesPreferExactOverEarlierSuperset(t *testing.T) {
	schema := &TypeDescriptor{Kind: TypeUnion, Branches: []*TypeDescriptor{
		recordOf(field("x", td(TypeInt)), field("y", nullable(TypeInt))),
		recordOf(field("x", td(TypeInt))),
	}}

	got, err := CoerceValueToSchema(RecordVal([]RecordField{{Name: "x", Value: IntVal(1)}}), schema)
	if err != nil {
		t.Fatalf("CoerceValueToSchema returned error: %v", err)
	}
	requireRecordValue(t, got, []RecordField{{Name: "x", Value: IntVal(1)}})
}

func TestCoerceValueToSchemaUnionRecordBranchesPreferExactOverEarlierNumericCoercion(t *testing.T) {
	schema := &TypeDescriptor{Kind: TypeUnion, Branches: []*TypeDescriptor{
		recordOf(field("x", td(TypeFloat))),
		recordOf(field("x", td(TypeInt))),
	}}

	got, err := CoerceValueToSchema(RecordVal([]RecordField{{Name: "x", Value: IntVal(1)}}), schema)
	if err != nil {
		t.Fatalf("CoerceValueToSchema returned error: %v", err)
	}
	requireRecordValue(t, got, []RecordField{{Name: "x", Value: IntVal(1)}})

	got, err = CoerceValueToSchema(RecordVal([]RecordField{{Name: "x", Value: FloatVal(1)}}), schema)
	if err != nil {
		t.Fatalf("CoerceValueToSchema returned error: %v", err)
	}
	requireRecordValue(t, got, []RecordField{{Name: "x", Value: FloatVal(1)}})
}

func TestCoerceValueToSchemaUnionNestedExactPassDoesNotCoerce(t *testing.T) {
	schema := UnionOf([]*TypeDescriptor{
		recordOf(field("u", UnionOf([]*TypeDescriptor{td(TypeFloat), td(TypeString)}, false))),
		recordOf(field("u", td(TypeInt))),
	}, false)
	value := RecordVal([]RecordField{{Name: "u", Value: IntVal(7)}})

	got, err := CoerceValueToSchema(value, schema)
	if err != nil {
		t.Fatalf("CoerceValueToSchema returned error: %v", err)
	}
	requireRecordValue(t, got, []RecordField{{Name: "u", Value: IntVal(7)}})

	got, err = CoerceValueToSchemaMode(value, schema, CoerceExactMode)
	if err != nil {
		t.Fatalf("CoerceExactMode returned error: %v", err)
	}
	requireRecordValue(t, got, []RecordField{{Name: "u", Value: IntVal(7)}})
}

func TestCoerceValueToSchemaUnionListBranchesPreferExactOverEarlierNumericCoercion(t *testing.T) {
	schema := listOf(&TypeDescriptor{Kind: TypeUnion, Branches: []*TypeDescriptor{
		recordOf(field("x", td(TypeFloat))),
		recordOf(field("x", td(TypeInt))),
	}})
	value := ListVal([]Value{
		RecordVal([]RecordField{{Name: "x", Value: IntVal(1)}}),
		RecordVal([]RecordField{{Name: "x", Value: FloatVal(2)}}),
	})

	got, err := CoerceValueToSchema(value, schema)
	if err != nil {
		t.Fatalf("CoerceValueToSchema returned error: %v", err)
	}
	if got.Type != TypeList || len(got.List) != 2 {
		t.Fatalf("coerced list: got %v, want two elements", got)
	}
	requireRecordValue(t, got.List[0], []RecordField{{Name: "x", Value: IntVal(1)}})
	requireRecordValue(t, got.List[1], []RecordField{{Name: "x", Value: FloatVal(2)}})
}

func TestCoerceValueToSchemaUnionListRecordBranchesPreserveElementBranches(t *testing.T) {
	schema := listOf(&TypeDescriptor{Kind: TypeUnion, Branches: []*TypeDescriptor{
		recordOf(field("x", td(TypeInt))),
		recordOf(field("y", td(TypeInt))),
	}})
	value := ListVal([]Value{
		RecordVal([]RecordField{{Name: "y", Value: IntVal(2)}}),
		RecordVal([]RecordField{{Name: "x", Value: IntVal(1)}}),
	})

	got, err := CoerceValueToSchema(value, schema)
	if err != nil {
		t.Fatalf("CoerceValueToSchema returned error: %v", err)
	}
	if got.Type != TypeList || len(got.List) != 2 {
		t.Fatalf("coerced list: got %v, want two elements", got)
	}
	requireRecordValue(t, got.List[0], []RecordField{{Name: "y", Value: IntVal(2)}})
	requireRecordValue(t, got.List[1], []RecordField{{Name: "x", Value: IntVal(1)}})
}

func TestCoerceUnionBranchHelpersCoverEdgeCases(t *testing.T) {
	got, err := coerceValueToExactUnionBranch(IntVal(1), nil, "u")
	if err != nil || !Equal(got, IntVal(1)) {
		t.Fatalf("exact nil schema: got %s, err %v", got.AsString(), err)
	}

	got, err = coerceValueToExactUnionBranch(Null(), td(TypeInt), "u")
	if err != nil || !got.IsNull() {
		t.Fatalf("exact null value: got %s, err %v", got.AsString(), err)
	}

	got, err = coerceValueToExactUnionBranch(StrVal("x"), &TypeDescriptor{Kind: TypeMixed}, "u")
	if err != nil || !Equal(got, StrVal("x")) {
		t.Fatalf("exact mixed schema: got %s, err %v", got.AsString(), err)
	}

	nestedUnion := &TypeDescriptor{Kind: TypeUnion, Branches: []*TypeDescriptor{td(TypeInt), td(TypeString)}}
	got, err = coerceValueToExactUnionBranch(IntVal(2), nestedUnion, "u")
	if err != nil || !Equal(got, IntVal(2)) {
		t.Fatalf("exact nested union: got %s, err %v", got.AsString(), err)
	}

	got, err = coerceValueToExactUnionBranch(ListVal([]Value{IntVal(3)}), listOf(td(TypeInt)), "xs")
	if err != nil || got.Type != TypeList || got.List[0].Int != 3 {
		t.Fatalf("exact list branch: got %s, err %v", got.AsString(), err)
	}

	got, err = coerceValueToUnionBranch(IntVal(4), &TypeDescriptor{Kind: TypeFloat}, "u")
	if err != nil || got.Type != TypeFloat || got.Float != 4 {
		t.Fatalf("coercive numeric branch: got %s, err %v", got.AsString(), err)
	}

	got, err = coerceValueToUnionBranch(ListVal([]Value{IntVal(5)}), listOf(td(TypeFloat)), "xs")
	if err != nil || got.Type != TypeList || got.List[0].Type != TypeFloat || got.List[0].Float != 5 {
		t.Fatalf("coercive list branch: got %s, err %v", got.AsString(), err)
	}

	_, err = coerceValueToUnionBranch(ListVal([]Value{StrVal("bad")}), listOf(td(TypeInt)), "xs")
	requireSchemaError(t, err, "xs[]", "int", "string")
}

func TestCoerceValueToSchemaUnionRecordBranchesRejectIncompatibleRecord(t *testing.T) {
	schema := &TypeDescriptor{Kind: TypeUnion, Branches: []*TypeDescriptor{
		recordOf(field("x", td(TypeInt))),
		recordOf(field("y", td(TypeInt))),
	}}

	_, err := CoerceValueToSchemaAtPath(RecordVal([]RecordField{{Name: "y", Value: StrVal("bad")}}), schema, "u")
	if err == nil {
		t.Fatal("expected incompatible union record branch error")
	}
	if !strings.Contains(err.Error(), "u expected union<record<x:int>,record<y:int>>, got record<y:string>") {
		t.Fatalf("error: got %q", err.Error())
	}
}

func TestMergeValueSchemaStrictAcceptsNarrowerUnionBranchValues(t *testing.T) {
	cases := []struct {
		name   string
		schema *TypeDescriptor
		value  Value
		want   string
	}{
		{
			name: "nullable_record_field_with_non_null_value",
			schema: &TypeDescriptor{Kind: TypeUnion, Branches: []*TypeDescriptor{
				recordOf(field("x", nullable(TypeInt))),
				td(TypeString),
			}},
			value: RecordVal([]RecordField{{Name: "x", Value: IntVal(1)}}),
			want:  "union<record<x:int?>,string>",
		},
		{
			name: "nullable_record_field_with_null_value",
			schema: &TypeDescriptor{Kind: TypeUnion, Branches: []*TypeDescriptor{
				recordOf(field("x", nullable(TypeInt))),
				td(TypeString),
			}},
			value: RecordVal([]RecordField{{Name: "x", Value: Null()}}),
			want:  "union<record<x:int?>,string>",
		},
		{
			name: "string_branch",
			schema: &TypeDescriptor{Kind: TypeUnion, Branches: []*TypeDescriptor{
				recordOf(field("x", nullable(TypeInt))),
				td(TypeString),
			}},
			value: StrVal("ok"),
			want:  "union<record<x:int?>,string>",
		},
		{
			name: "numeric_promotion_inside_nullable_record_branch",
			schema: &TypeDescriptor{Kind: TypeUnion, Branches: []*TypeDescriptor{
				recordOf(field("x", nullable(TypeFloat))),
				td(TypeString),
			}},
			value: RecordVal([]RecordField{{Name: "x", Value: IntVal(1)}}),
			want:  "union<record<x:float?>,string>",
		},
		{
			name: "multiple_record_branches",
			schema: &TypeDescriptor{Kind: TypeUnion, Branches: []*TypeDescriptor{
				recordOf(field("x", nullable(TypeInt))),
				recordOf(field("y", nullable(TypeInt))),
				td(TypeString),
			}},
			value: RecordVal([]RecordField{{Name: "y", Value: IntVal(2)}}),
			want:  "union<record<x:int?>,record<y:int?>,string>",
		},
		{
			name: "shared_field_record_branch",
			schema: &TypeDescriptor{Kind: TypeUnion, Branches: []*TypeDescriptor{
				recordOf(field("kind", td(TypeString)), field("x", nullable(TypeInt))),
				recordOf(field("kind", td(TypeString)), field("y", nullable(TypeInt))),
			}},
			value: RecordVal([]RecordField{{Name: "kind", Value: StrVal("right")}, {Name: "y", Value: IntVal(2)}}),
			want:  "union<record<kind:string, x:int?>,record<kind:string, y:int?>>",
		},
		{
			name: "list_element_union_branch",
			schema: listOf(&TypeDescriptor{Kind: TypeUnion, Branches: []*TypeDescriptor{
				recordOf(field("x", nullable(TypeInt))),
				td(TypeString),
			}}),
			value: ListVal([]Value{
				RecordVal([]RecordField{{Name: "x", Value: IntVal(1)}}),
				StrVal("ok"),
				RecordVal([]RecordField{{Name: "x", Value: Null()}}),
			}),
			want: "list<union<record<x:int?>,string>>",
		},
		{
			name: "nested_union_field",
			schema: recordOf(field("u", &TypeDescriptor{Kind: TypeUnion, Branches: []*TypeDescriptor{
				recordOf(field("x", nullable(TypeInt))),
				td(TypeString),
			}})),
			value: RecordVal([]RecordField{{Name: "u", Value: RecordVal([]RecordField{{Name: "x", Value: IntVal(1)}})}}),
			want:  "record<u:union<record<x:int?>,string>>",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := MergeValueSchemaStrictAtPath(cloneTypeDescriptor(tc.schema), tc.value, "u")
			if err != nil {
				t.Fatalf("MergeValueSchemaStrictAtPath returned error: %v", err)
			}
			requireSchemaString(t, got, tc.want)
		})
	}
}

func TestMergeValueSchemaStrictRejectsInvalidUnionBranchValues(t *testing.T) {
	schema := &TypeDescriptor{Kind: TypeUnion, Branches: []*TypeDescriptor{
		recordOf(field("x", nullable(TypeInt))),
		recordOf(field("y", nullable(TypeInt))),
	}}

	cases := []struct {
		name  string
		value Value
		want  string
	}{
		{
			name:  "bad_field_type",
			value: RecordVal([]RecordField{{Name: "x", Value: StrVal("bad")}}),
			want:  "u expected union<record<x:int?>,record<y:int?>>, got record<x:string>",
		},
		{
			name:  "wrong_record_shape",
			value: RecordVal([]RecordField{{Name: "z", Value: IntVal(1)}}),
			want:  "u expected union<record<x:int?>,record<y:int?>>, got record<z:int>",
		},
		{
			name:  "missing_nullable_branch_field_still_wrong_shape",
			value: RecordVal(nil),
			want:  "u expected union<record<x:int?>,record<y:int?>>, got record<>",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := MergeValueSchemaStrictAtPath(cloneTypeDescriptor(schema), tc.value, "u")
			if err == nil {
				t.Fatal("expected union branch merge error")
			}
			if got := err.Error(); got != tc.want {
				t.Fatalf("error: got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCoerceValueToSchemaReportsNestedPath(t *testing.T) {
	schema := listOf(recordOf(field("amt", td(TypeInt))))
	value := ListVal([]Value{
		RecordVal([]RecordField{{Name: "amt", Value: StrVal("x")}}),
	})

	_, err := CoerceValueToSchemaAtPath(value, schema, "orders")

	requireSchemaError(t, err, "orders[].amt", "int", "string")
}

func requireRecordValue(t *testing.T, got Value, fields []RecordField) {
	t.Helper()
	want := RecordVal(fields)
	if !Equal(got, want) {
		t.Fatalf("record value:\ngot  %s\nwant %s", got.AsString(), want.AsString())
	}
}

func TestCoerceValueToSchemaRejectsDuplicateRecordFields(t *testing.T) {
	cases := []struct {
		name     string
		schema   *TypeDescriptor
		value    Value
		basePath string
		wantPath string
	}{
		{
			name:     "top_level_record",
			schema:   recordOf(field("x", td(TypeInt))),
			basePath: "payload",
			wantPath: "payload.x",
			value: RecordVal([]RecordField{
				{Name: "x", Value: IntVal(1)},
				{Name: "x", Value: IntVal(2)},
			}),
		},
		{
			name:     "nested_record",
			basePath: "payload",
			wantPath: "payload.meta.score",
			schema: recordOf(field("meta", recordOf(
				field("score", td(TypeFloat)),
			))),
			value: RecordVal([]RecordField{
				{Name: "meta", Value: RecordVal([]RecordField{
					{Name: "score", Value: IntVal(1)},
					{Name: "score", Value: IntVal(2)},
				})},
			}),
		},
		{
			name:     "list_record",
			schema:   listOf(recordOf(field("sku", td(TypeString)))),
			basePath: "items",
			wantPath: "items[].sku",
			value: ListVal([]Value{
				RecordVal([]RecordField{
					{Name: "sku", Value: StrVal("a")},
					{Name: "sku", Value: StrVal("b")},
				}),
			}),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := CoerceValueToSchemaAtPath(tc.value, tc.schema, tc.basePath)
			if err == nil {
				t.Fatal("expected duplicate record field error")
			}
			if got, want := err.Error(), tc.wantPath+" duplicate record field"; got != want {
				t.Fatalf("error: got %q, want %q", got, want)
			}
		})
	}
}

func TestNewTableWithSchemasColumnSchemaAndAddRowTyped(t *testing.T) {
	schemas := []*TypeDescriptor{
		{Kind: TypeNull, Nullable: true},
		recordOf(field("x", td(TypeFloat))),
	}
	tbl := NewTableWithSchemas([]string{"empty", "s"}, schemas)

	requireSchemaString(t, tbl.Col(0).RawSchema(), "null?")
	requireSchemaString(t, tbl.Col(0).Schema(), "string?")
	requireSchemaString(t, tbl.Col(1).Schema(), "record<x:float>")

	err := tbl.AddRowTyped([]Value{
		Null(),
		RecordVal([]RecordField{{Name: "x", Value: IntVal(4)}}),
	})
	if err != nil {
		t.Fatalf("AddRowTyped returned error: %v", err)
	}

	if got := tbl.GetAt(0, 1); got.Type != TypeRecord || got.Fields[0].Value.Type != TypeFloat || got.Fields[0].Value.Float != 4 {
		t.Fatalf("typed row was not coerced to schema: %v", got)
	}
	if got := tbl.Col(1).ColType(); got != TypeRecord {
		t.Fatalf("column type: got %v, want record", got)
	}
}

func TestNewSchemaPreservesNullOnlyDescriptor(t *testing.T) {
	schema := NewSchema([]string{"empty"}, []*TypeDescriptor{{Kind: TypeNull, Nullable: true}})

	requireSchemaString(t, schema.Columns[0].Type, "null?")
}

func TestSelectColsWithSchemaUsesPlannedNamesAndSchema(t *testing.T) {
	tbl := NewTableWithSchemas(
		[]string{"name", "age"},
		[]*TypeDescriptor{{Kind: TypeString}, {Kind: TypeInt}},
	)
	if err := tbl.AddRowTyped([]Value{StrVal("Alice"), IntVal(30)}); err != nil {
		t.Fatalf("AddRowTyped: %v", err)
	}

	projected, err := tbl.SelectColsWithSchema([]int{1}, Schema{Columns: []SchemaColumn{
		{Name: "years", Type: &TypeDescriptor{Kind: TypeInt, Nullable: true}},
	}})
	if err != nil {
		t.Fatalf("SelectColsWithSchema: %v", err)
	}

	if len(projected.Columns) != 1 || projected.Columns[0] != "years" {
		t.Fatalf("columns: got %v, want [years]", projected.Columns)
	}
	requireSchemaString(t, projected.Col(0).Schema(), "int?")
	if got := projected.GetAt(0, 0); got.Type != TypeInt || got.Int != 30 {
		t.Fatalf("projected value: got %v, want int 30", got)
	}
}

func TestSelectColsWithSchemaAllowsNullOnlyStorageForNullablePlannedSchema(t *testing.T) {
	tbl := NewTable([]string{"nilcol"})
	tbl.AddRow([]Value{Null()})
	tbl.AddRow([]Value{Null()})

	projected, err := tbl.SelectColsWithSchema([]int{0}, Schema{Columns: []SchemaColumn{
		{Name: "renamed", Type: &TypeDescriptor{Kind: TypeString, Nullable: true}},
	}})
	if err != nil {
		t.Fatalf("SelectColsWithSchema: %v", err)
	}
	if got := projected.Columns; len(got) != 1 || got[0] != "renamed" {
		t.Fatalf("columns: got %v, want [renamed]", got)
	}
	requireSchemaString(t, projected.Col(0).Schema(), "string?")
	if got := projected.Col(0).ColType(); got != TypeNull {
		t.Fatalf("storage type: got %s, want null", TypeName(got))
	}
}

func TestSelectColsWithSchemaRejectsIncompatibleStorageKind(t *testing.T) {
	tbl := NewTableWithSchemas(
		[]string{"age"},
		[]*TypeDescriptor{{Kind: TypeInt}},
	)
	if err := tbl.AddRowTyped([]Value{IntVal(30)}); err != nil {
		t.Fatalf("AddRowTyped: %v", err)
	}

	_, err := tbl.SelectColsWithSchema([]int{0}, Schema{Columns: []SchemaColumn{
		{Name: "age_text", Type: &TypeDescriptor{Kind: TypeString}},
	}})
	if err == nil {
		t.Fatal("expected incompatible storage kind error")
	}
	if got, want := err.Error(), "storage type int incompatible with planned schema string"; !strings.Contains(got, want) {
		t.Fatalf("error: got %q, want substring %q", got, want)
	}
}

func TestSelectColsWithSchemaRejectsCoerciveNestedRecordSchema(t *testing.T) {
	tbl := NewTableWithSchemas(
		[]string{"payload"},
		[]*TypeDescriptor{recordOf(field("x", td(TypeInt)))},
	)
	if err := tbl.AddRowTyped([]Value{
		RecordVal([]RecordField{{Name: "x", Value: IntVal(7)}}),
	}); err != nil {
		t.Fatalf("AddRowTyped: %v", err)
	}

	_, err := tbl.SelectColsWithSchema([]int{0}, Schema{Columns: []SchemaColumn{
		{Name: "payload", Type: recordOf(field("x", td(TypeFloat)))},
	}})
	if err == nil {
		t.Fatal("expected nested record schema mismatch")
	}
	if got, want := err.Error(), "storage schema record<x:int> incompatible with planned schema record<x:float>"; !strings.Contains(got, want) {
		t.Fatalf("error: got %q, want substring %q", got, want)
	}
}

func TestSelectColsWithSchemaRejectsCoerciveNestedListSchema(t *testing.T) {
	tbl := NewTableWithSchemas(
		[]string{"xs"},
		[]*TypeDescriptor{listOf(td(TypeInt))},
	)
	if err := tbl.AddRowTyped([]Value{ListVal([]Value{IntVal(1), IntVal(2)})}); err != nil {
		t.Fatalf("AddRowTyped: %v", err)
	}

	_, err := tbl.SelectColsWithSchema([]int{0}, Schema{Columns: []SchemaColumn{
		{Name: "xs", Type: listOf(td(TypeFloat))},
	}})
	if err == nil {
		t.Fatal("expected nested list schema mismatch")
	}
	if got, want := err.Error(), "storage schema list<int> incompatible with planned schema list<float>"; !strings.Contains(got, want) {
		t.Fatalf("error: got %q, want substring %q", got, want)
	}
}

func TestSelectColsWithSchemaAllowsExactNestedRenameWithNullableWidening(t *testing.T) {
	tbl := NewTableWithSchemas(
		[]string{"payload"},
		[]*TypeDescriptor{recordOf(field("x", td(TypeInt)))},
	)
	if err := tbl.AddRowTyped([]Value{
		RecordVal([]RecordField{{Name: "x", Value: IntVal(7)}}),
	}); err != nil {
		t.Fatalf("AddRowTyped: %v", err)
	}

	projected, err := tbl.SelectColsWithSchema([]int{0}, Schema{Columns: []SchemaColumn{
		{Name: "renamed", Type: recordOf(field("x", nullable(TypeInt)))},
	}})
	if err != nil {
		t.Fatalf("SelectColsWithSchema: %v", err)
	}
	if len(projected.Columns) != 1 || projected.Columns[0] != "renamed" {
		t.Fatalf("columns: got %v, want [renamed]", projected.Columns)
	}
	requireSchemaString(t, projected.Col(0).Schema(), "record<x:int?>")
	requireRecordValue(t, projected.GetAt(0, 0), []RecordField{{Name: "x", Value: IntVal(7)}})
}

func TestNewTableWithSchemasDuplicateRecordFieldSchemaCannotAppendTypedValues(t *testing.T) {
	tbl := NewTableWithSchemas([]string{"payload"}, []*TypeDescriptor{
		recordOf(
			field("x", td(TypeInt)),
			field("x", td(TypeInt)),
		),
	})
	requireSchemaString(t, tbl.Col(0).RawSchema(), "record<x:int, x:int>")
	if got := tbl.Col(0).ColType(); got != TypeNull {
		t.Fatalf("invalid schema storage type: got %s, want null", TypeName(got))
	}

	err := tbl.AddRowTyped([]Value{
		RecordVal([]RecordField{{Name: "x", Value: IntVal(1)}}),
	})
	if err == nil {
		t.Fatal("expected typed append error")
	}
	if got, want := err.Error(), "payload.x duplicate record field"; got != want {
		t.Fatalf("error: got %q, want %q", got, want)
	}
}

func TestSchemaValidationBoundariesRejectInvalidDescriptors(t *testing.T) {
	invalid := recordOf(
		field("x", td(TypeInt)),
		field("x", td(TypeInt)),
	)

	if SchemaAssignable(invalid, cloneTypeDescriptor(invalid), AssignCoerciveMode) {
		t.Fatal("SchemaAssignable accepted duplicate field descriptors")
	}
	if schemaFitsTarget(invalid, cloneTypeDescriptor(invalid)) {
		t.Fatal("schemaFitsTarget accepted duplicate field descriptors")
	}

	_, err := UnifySchemasAtPath(td(TypeInt), invalid, UnifyStrictMode, "payload")
	if err == nil {
		t.Fatal("expected UnifySchemasAtPath to reject invalid actual schema")
	}
	if got, want := err.Error(), "payload.x duplicate record field"; got != want {
		t.Fatalf("UnifySchemasAtPath error: got %q, want %q", got, want)
	}

	_, err = MergeValueSchemaStrictAtPath(invalid, Null(), "payload")
	if err == nil {
		t.Fatal("expected MergeValueSchemaStrictAtPath to reject invalid schema")
	}
	if got, want := err.Error(), "payload.x duplicate record field"; got != want {
		t.Fatalf("MergeValueSchemaStrictAtPath error: got %q, want %q", got, want)
	}
}

func TestAppendTypedComputedColumnsCoversStorageKinds(t *testing.T) {
	base := NewTableWithSchemas([]string{"id"}, []*TypeDescriptor{td(TypeInt)})
	if err := base.AddRowTyped([]Value{IntVal(1)}); err != nil {
		t.Fatalf("seed row 1: %v", err)
	}
	if err := base.AddRowTyped([]Value{IntVal(2)}); err != nil {
		t.Fatalf("seed row 2: %v", err)
	}

	names := []string{"i", "f", "s", "b", "xs", "rec", "u"}
	schemas := []*TypeDescriptor{
		td(TypeInt),
		td(TypeFloat),
		td(TypeString),
		td(TypeBool),
		listOf(td(TypeInt)),
		recordOf(field("x", td(TypeInt))),
		UnionOf([]*TypeDescriptor{td(TypeInt), td(TypeString)}, false),
	}
	values := [][]Value{
		{IntVal(10), IntVal(20)},
		{IntVal(1), FloatVal(2.5)},
		{StrVal("a"), StrVal("b")},
		{BoolVal(true), BoolVal(false)},
		{ListVal([]Value{IntVal(1)}), ListVal([]Value{IntVal(2), IntVal(3)})},
		{RecordVal([]RecordField{{Name: "x", Value: IntVal(1)}}), RecordVal([]RecordField{{Name: "x", Value: IntVal(2)}})},
		{IntVal(7), StrVal("seven")},
	}

	got, err := base.AppendTypedComputedColumns(names, schemas, values)
	if err != nil {
		t.Fatalf("AppendTypedComputedColumns returned error: %v", err)
	}
	if got.NumRows != 2 || len(got.Columns) != 8 {
		t.Fatalf("computed table shape: rows=%d columns=%v", got.NumRows, got.Columns)
	}
	if v := got.Get(0, "f"); v.Type != TypeFloat || v.Float != 1 {
		t.Fatalf("float computed value: got %v", v)
	}
	if v := got.Get(1, "b"); v.Type != TypeBool || v.Bool {
		t.Fatalf("bool computed value: got %v", v)
	}
	if v := got.Get(1, "xs"); v.Type != TypeList || len(v.List) != 2 || v.List[1].Int != 3 {
		t.Fatalf("list computed value: got %v", v)
	}
	requireRecordValue(t, got.Get(0, "rec"), []RecordField{{Name: "x", Value: IntVal(1)}})
	if v := got.Get(0, "u"); v.Type != TypeInt || v.Int != 7 {
		t.Fatalf("union int computed value: got %v", v)
	}
	if v := got.Get(1, "u"); v.Type != TypeString || v.Str != "seven" {
		t.Fatalf("union string computed value: got %v", v)
	}
}

func TestAddRowTypedAcceptsNarrowerUnionBranchValues(t *testing.T) {
	schemas := []*TypeDescriptor{
		{Kind: TypeUnion, Branches: []*TypeDescriptor{
			recordOf(field("x", nullable(TypeInt))),
			td(TypeString),
		}},
		{Kind: TypeUnion, Branches: []*TypeDescriptor{
			recordOf(field("x", nullable(TypeFloat))),
			td(TypeString),
		}},
		listOf(&TypeDescriptor{Kind: TypeUnion, Branches: []*TypeDescriptor{
			recordOf(field("x", nullable(TypeInt))),
			td(TypeString),
		}}),
		recordOf(field("u", &TypeDescriptor{Kind: TypeUnion, Branches: []*TypeDescriptor{
			recordOf(field("x", nullable(TypeInt))),
			td(TypeString),
		}})),
	}
	tbl := NewTableWithSchemas([]string{"u", "f", "xs", "payload"}, schemas)

	err := tbl.AddRowTyped([]Value{
		RecordVal([]RecordField{{Name: "x", Value: IntVal(1)}}),
		RecordVal([]RecordField{{Name: "x", Value: IntVal(2)}}),
		ListVal([]Value{
			RecordVal([]RecordField{{Name: "x", Value: IntVal(3)}}),
			StrVal("ok"),
			RecordVal([]RecordField{{Name: "x", Value: Null()}}),
		}),
		RecordVal([]RecordField{{Name: "u", Value: RecordVal([]RecordField{{Name: "x", Value: IntVal(4)}})}}),
	})
	if err != nil {
		t.Fatalf("AddRowTyped returned error: %v", err)
	}

	requireRecordValue(t, tbl.Get(0, "u"), []RecordField{{Name: "x", Value: IntVal(1)}})
	requireRecordValue(t, tbl.Get(0, "f"), []RecordField{{Name: "x", Value: FloatVal(2)}})
	xs := tbl.Get(0, "xs")
	if xs.Type != TypeList || len(xs.List) != 3 {
		t.Fatalf("xs: got %v, want three union branch values", xs)
	}
	requireRecordValue(t, xs.List[0], []RecordField{{Name: "x", Value: IntVal(3)}})
	if xs.List[1].Type != TypeString || xs.List[1].Str != "ok" {
		t.Fatalf("xs[1]: got %v, want string ok", xs.List[1])
	}
	requireRecordValue(t, xs.List[2], []RecordField{{Name: "x", Value: Null()}})
	payload := tbl.Get(0, "payload")
	requireRecordValue(t, payload, []RecordField{{Name: "u", Value: RecordVal([]RecordField{{Name: "x", Value: IntVal(4)}})}})
}

func TestAddRowTypedUpdatesSchemaNullabilityForAcceptedNulls(t *testing.T) {
	tbl := NewTableWithSchemas([]string{"id", "s", "orders"}, []*TypeDescriptor{
		td(TypeInt),
		recordOf(field("x", td(TypeInt))),
		listOf(recordOf(field("amount", td(TypeInt)))),
	})

	if err := tbl.AddRowTyped([]Value{
		IntVal(1),
		RecordVal([]RecordField{{Name: "x", Value: IntVal(2)}}),
		ListVal([]Value{RecordVal([]RecordField{{Name: "amount", Value: IntVal(3)}})}),
	}); err != nil {
		t.Fatalf("initial AddRowTyped returned error: %v", err)
	}
	if err := tbl.AddRowTyped([]Value{
		Null(),
		RecordVal(nil),
		ListVal([]Value{RecordVal(nil)}),
	}); err != nil {
		t.Fatalf("null AddRowTyped returned error: %v", err)
	}

	requireSchemaString(t, tbl.Col(0).Schema(), "int?")
	requireSchemaString(t, tbl.Col(1).Schema(), "record<x:int?>")
	requireSchemaString(t, tbl.Col(2).Schema(), "list<record<amount:int?>>")
}

func TestAddRowTypedReportsCoerceFailureDuringMaterialization(t *testing.T) {
	schema := recordOf(field("x", td(TypeInt)))
	tbl := NewTableWithSchemas([]string{"s"}, []*TypeDescriptor{schema})

	err := tbl.AddRowTyped([]Value{
		RecordVal([]RecordField{{Name: "x", Value: StrVal("bad")}}),
	})

	requireSchemaError(t, err, "s.x", "int", "string")
	if tbl.NumRows != 0 {
		t.Fatalf("failed typed append should not increment row count, got %d", tbl.NumRows)
	}
}

func TestAddRowTypedRejectsNonNullValueWithoutConcreteColumnType(t *testing.T) {
	tbl := NewTableWithSchemas([]string{"id", "a"}, []*TypeDescriptor{td(TypeInt), nil})

	if err := tbl.AddRowTyped([]Value{IntVal(1), Null()}); err != nil {
		t.Fatalf("AddRowTyped with null value returned error: %v", err)
	}

	err := tbl.AddRowTyped([]Value{IntVal(2), IntVal(42)})

	if err == nil {
		t.Fatal("expected AddRowTyped to reject non-null value for nil schema column")
	}
	if got, want := err.Error(), `column "a" has no concrete type for non-null int value`; got != want {
		t.Fatalf("error: got %q, want %q", got, want)
	}
	if tbl.NumRows != 1 {
		t.Fatalf("failed typed append changed row count: got %d, want 1", tbl.NumRows)
	}
	for i, colName := range tbl.Columns {
		if got := tbl.Col(i).Len(); got != tbl.NumRows {
			t.Fatalf("column %q length changed after failed append: got %d, want %d", colName, got, tbl.NumRows)
		}
	}
	if got := tbl.Get(0, "id"); got.Type != TypeInt || got.Int != 1 {
		t.Fatalf("existing row id changed: got %v", got)
	}
	if got := tbl.Get(0, "a"); got.Type != TypeNull {
		t.Fatalf("nil schema column stored non-null value: got %v", got)
	}
}

func TestAddRowTypedColumnsFailureDoesNotAppendTrustedColumns(t *testing.T) {
	tbl := NewTableWithSchemas([]string{"amount", "total"}, []*TypeDescriptor{td(TypeInt), td(TypeInt)})

	err := tbl.AddRowTypedColumns([]Value{IntVal(10), StrVal("bad")}, []int{1})

	requireSchemaError(t, err, "total", "int", "string")
	if tbl.NumRows != 0 {
		t.Fatalf("failed selective typed append changed row count: got %d", tbl.NumRows)
	}
	for i, colName := range tbl.Columns {
		if got := tbl.Col(i).Len(); got != 0 {
			t.Fatalf("column %q length changed after failed append: got %d", colName, got)
		}
	}
}

func TestAddRowTypedFailureDoesNotPartiallyAppendEarlierColumns(t *testing.T) {
	tbl := NewTableWithSchemas([]string{"id", "s"}, []*TypeDescriptor{
		td(TypeInt),
		recordOf(field("x", td(TypeInt))),
	})
	if err := tbl.AddRowTyped([]Value{
		IntVal(1),
		RecordVal([]RecordField{{Name: "x", Value: IntVal(2)}}),
	}); err != nil {
		t.Fatalf("initial AddRowTyped returned error: %v", err)
	}

	err := tbl.AddRowTyped([]Value{
		IntVal(2),
		RecordVal([]RecordField{{Name: "x", Value: StrVal("bad")}}),
	})

	requireSchemaError(t, err, "s.x", "int", "string")
	if tbl.NumRows != 1 {
		t.Fatalf("failed typed append changed row count: got %d, want 1", tbl.NumRows)
	}
	for i, colName := range tbl.Columns {
		if got := tbl.Col(i).Len(); got != tbl.NumRows {
			t.Fatalf("column %q length changed after failed append: got %d, want %d", colName, got, tbl.NumRows)
		}
	}
	if got := tbl.Get(0, "id"); got.Type != TypeInt || got.Int != 1 {
		t.Fatalf("existing row id changed: got %v", got)
	}
}

func TestSchemaErrorErrorUsesValueForEmptyPath(t *testing.T) {
	err := (&SchemaError{Expected: td(TypeInt), Actual: td(TypeString)}).Error()

	if !strings.Contains(err, "<value> expected int, got string") {
		t.Fatalf("SchemaError.Error() = %q", err)
	}
}
