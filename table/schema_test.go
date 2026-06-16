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

func TestCoerceValueToSchemaReportsNestedPath(t *testing.T) {
	schema := listOf(recordOf(field("amt", td(TypeInt))))
	value := ListVal([]Value{
		RecordVal([]RecordField{{Name: "amt", Value: StrVal("x")}}),
	})

	_, err := CoerceValueToSchemaAtPath(value, schema, "orders")

	requireSchemaError(t, err, "orders[].amt", "int", "string")
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
