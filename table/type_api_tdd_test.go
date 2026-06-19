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
		{name: "top_level_mixed_is_not_scalar_escape_hatch", a: mixedType(), b: td(TypeInt), wantPath: "", wantExpected: "mixed", wantActual: "int"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := UnifyStrict(tc.a, tc.b)
			requireSchemaError(t, err, tc.wantPath, tc.wantExpected, tc.wantActual)
		})
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
		{name: "empty_literal", in: nil, want: "string?"},
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
