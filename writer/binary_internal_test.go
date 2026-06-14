package writer

import (
	"reflect"
	"strings"
	"testing"

	"github.com/razeghi71/dq/table"
)

func TestInferTableTypesMergesNumbersAndRecordFields(t *testing.T) {
	tbl := table.NewTable([]string{"num", "obj"})
	tbl.AddRow([]table.Value{
		table.IntVal(1),
		table.RecordVal([]table.RecordField{
			{Name: "z", Value: table.IntVal(10)},
		}),
	})
	tbl.AddRow([]table.Value{
		table.FloatVal(2.5),
		table.RecordVal([]table.RecordField{
			{Name: "a", Value: table.StrVal("x")},
			{Name: "z", Value: table.Null()},
		}),
	})

	types := inferTableTypes(tbl)
	if got := types[0]; got.typ != table.TypeFloat || got.nullable {
		t.Fatalf("num type: want non-null float, got %#v", got)
	}

	obj := types[1]
	if obj.typ != table.TypeRecord {
		t.Fatalf("obj type: want record, got %#v", obj)
	}
	if len(obj.fields) != 2 {
		t.Fatalf("obj fields: want 2, got %#v", obj.fields)
	}
	if obj.fields[0].name != "a" || obj.fields[1].name != "z" {
		t.Fatalf("record fields should be sorted by name, got %#v", obj.fields)
	}
	if !obj.fields[0].typ.nullable {
		t.Fatalf("field a should be nullable because it is missing from the first row")
	}
	if !obj.fields[1].typ.nullable {
		t.Fatalf("field z should be nullable because one row has null")
	}
}

func TestAvroNameHelpersBoundaryCases(t *testing.T) {
	pathCases := map[string]string{
		"":        "field_",
		"1 bad":   "field_1_bad",
		"ok-name": "ok_name",
		"_ok9":    "_ok9",
	}
	for in, want := range pathCases {
		if got := avroPath(in); got != want {
			t.Fatalf("avroPath(%q): want %q, got %q", in, want, got)
		}
	}

	valid := []string{"A", "_", "_x9", "name_2"}
	for _, s := range valid {
		if !isAvroName(s) {
			t.Fatalf("%q should be a valid Avro name", s)
		}
	}

	invalid := []string{"", "9name", "bad-name", "bad name"}
	for _, s := range invalid {
		if isAvroName(s) {
			t.Fatalf("%q should not be a valid Avro name", s)
		}
	}
}

func TestValidateAvroNestedFieldsChecksListRecordItems(t *testing.T) {
	typ := &inferredType{
		typ: table.TypeList,
		elem: &inferredType{
			typ: table.TypeRecord,
			fields: []inferredField{
				{name: "ok", typ: &inferredType{typ: table.TypeInt}},
				{name: "bad-name", typ: &inferredType{typ: table.TypeString}},
			},
		},
	}

	err := validateAvroNestedFields(typ, "items")
	if err == nil {
		t.Fatal("expected invalid nested field name error")
	}
	if !strings.Contains(err.Error(), "bad-name") || !strings.Contains(err.Error(), "items[]") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAssignAvroRecordNamesDisambiguatesRepeatedPaths(t *testing.T) {
	types := []*inferredType{
		{typ: table.TypeRecord},
		{typ: table.TypeRecord},
	}

	assignAvroRecordNames([]string{"a-b", "a_b"}, types)
	if types[0].avroName == "" || types[1].avroName == "" {
		t.Fatalf("record names were not assigned: %#v", types)
	}
	if types[0].avroName == types[1].avroName {
		t.Fatalf("record names should be unique, got %q", types[0].avroName)
	}
}

func TestParquetExportedFieldNamesAreValidAndUnique(t *testing.T) {
	cases := map[string]string{
		"":         "Field",
		"field":    "Field",
		"Field":    "Field",
		"1st":      "Field_1st",
		"bad name": "Bad_name",
		"_hidden":  "Fieldhidden",
	}
	for in, want := range cases {
		if got := exportedFieldName(in); got != want {
			t.Fatalf("exportedFieldName(%q): want %q, got %q", in, want, got)
		}
	}

	used := map[string]bool{}
	got := []string{
		uniqueExportedFieldName("field", used),
		uniqueExportedFieldName("Field", used),
		uniqueExportedFieldName("Field_2", used),
		uniqueExportedFieldName("", used),
	}
	want := []string{"Field", "Field_2", "Field_2_2", "Field_3"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unique name %d: want %q, got %q (all=%v)", i, want[i], got[i], got)
		}
	}
}

func TestNativeValueConvertsRecordsAndLists(t *testing.T) {
	typ := &inferredType{
		typ: table.TypeRecord,
		fields: []inferredField{
			{name: "active", typ: &inferredType{typ: table.TypeBool}},
			{name: "scores", typ: &inferredType{typ: table.TypeList, elem: &inferredType{typ: table.TypeFloat}}},
			{name: "tags", typ: &inferredType{typ: table.TypeList, elem: &inferredType{typ: table.TypeString, nullable: true}}},
		},
	}
	v := table.RecordVal([]table.RecordField{
		{Name: "active", Value: table.BoolVal(true)},
		{Name: "scores", Value: table.ListVal([]table.Value{table.IntVal(1), table.FloatVal(2.5)})},
		{Name: "tags", Value: table.ListVal([]table.Value{table.StrVal("a"), table.Null(), table.IntVal(3)})},
	})

	got, err := nativeValue(v, typ)
	if err != nil {
		t.Fatalf("nativeValue returned error: %v", err)
	}
	row, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("expected map row, got %T", got)
	}
	if row["active"] != true {
		t.Fatalf("active: want true, got %#v", row["active"])
	}
	if scores, ok := row["scores"].([]float64); !ok || !reflect.DeepEqual(scores, []float64{1, 2.5}) {
		t.Fatalf("scores: want []float64{1, 2.5}, got %#v", row["scores"])
	}
	tags, ok := row["tags"].([]*string)
	if !ok {
		t.Fatalf("tags: want []*string, got %T", row["tags"])
	}
	if tags[0] == nil || *tags[0] != "a" || tags[1] != nil || tags[2] == nil || *tags[2] != "3" {
		t.Fatalf("tags values were not preserved: %#v", tags)
	}
}

func TestNativePrimitiveListConversions(t *testing.T) {
	cases := []struct {
		name  string
		items []table.Value
		elem  *inferredType
		want  any
	}{
		{name: "int", items: []table.Value{table.IntVal(1), table.IntVal(2)}, elem: &inferredType{typ: table.TypeInt}, want: []int64{1, 2}},
		{name: "string", items: []table.Value{table.StrVal("a"), table.IntVal(2)}, elem: &inferredType{typ: table.TypeString}, want: []string{"a", "2"}},
		{name: "bool", items: []table.Value{table.BoolVal(true), table.BoolVal(false)}, elem: &inferredType{typ: table.TypeBool}, want: []bool{true, false}},
	}

	for _, tc := range cases {
		got, err := nativeList(tc.items, tc.elem)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", tc.name, err)
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%s: want %#v, got %#v", tc.name, tc.want, got)
		}
	}

	if _, err := nativeList([]table.Value{table.StrVal("x")}, &inferredType{typ: table.TypeInt}); err == nil {
		t.Fatal("expected int list with string item to fail")
	}
	if _, err := nativeList([]table.Value{table.StrVal("x")}, &inferredType{typ: table.TypeFloat}); err == nil {
		t.Fatal("expected float list with string item to fail")
	}
	if _, err := nativeList([]table.Value{table.IntVal(1)}, &inferredType{typ: table.TypeBool}); err == nil {
		t.Fatal("expected bool list with int item to fail")
	}
}

func TestNativeNullableListConversions(t *testing.T) {
	ints, err := nativeNullableList([]table.Value{table.IntVal(1), table.Null()}, &inferredType{typ: table.TypeInt, nullable: true})
	if err != nil {
		t.Fatalf("nullable int list: %v", err)
	}
	if !reflect.DeepEqual(ints, []any{int64(1), nil}) {
		t.Fatalf("nullable int list: got %#v", ints)
	}

	floats, err := nativeNullableList([]table.Value{table.IntVal(1), table.Null(), table.FloatVal(2.5)}, &inferredType{typ: table.TypeFloat, nullable: true})
	if err != nil {
		t.Fatalf("nullable float list: %v", err)
	}
	floatPtrs := floats.([]*float64)
	if floatPtrs[0] == nil || *floatPtrs[0] != 1 || floatPtrs[1] != nil || floatPtrs[2] == nil || *floatPtrs[2] != 2.5 {
		t.Fatalf("nullable float list values: %#v", floatPtrs)
	}

	bools, err := nativeNullableList([]table.Value{table.BoolVal(true), table.Null()}, &inferredType{typ: table.TypeBool, nullable: true})
	if err != nil {
		t.Fatalf("nullable bool list: %v", err)
	}
	boolPtrs := bools.([]*bool)
	if boolPtrs[0] == nil || *boolPtrs[0] != true || boolPtrs[1] != nil {
		t.Fatalf("nullable bool list values: %#v", boolPtrs)
	}

	records, err := nativeNullableList([]table.Value{
		table.RecordVal([]table.RecordField{{Name: "n", Value: table.IntVal(7)}}),
		table.Null(),
	}, &inferredType{typ: table.TypeRecord, nullable: true, fields: []inferredField{{name: "n", typ: &inferredType{typ: table.TypeInt}}}})
	if err != nil {
		t.Fatalf("nullable record list: %v", err)
	}
	recordItems := records.([]any)
	first := recordItems[0].(map[string]any)
	if first["n"] != int64(7) || recordItems[1] != nil {
		t.Fatalf("nullable record list values: %#v", recordItems)
	}

	if _, err := nativeNullableList([]table.Value{table.StrVal("x")}, &inferredType{typ: table.TypeInt, nullable: true}); err == nil {
		t.Fatal("expected nullable int list with string item to fail")
	}
}

func TestNativeValueErrorBranches(t *testing.T) {
	if got, err := nativeValue(table.Null(), &inferredType{typ: table.TypeInt}); err != nil || got != nil {
		t.Fatalf("null native value: want nil, got %#v err=%v", got, err)
	}

	errorCases := []struct {
		name string
		v    table.Value
		typ  *inferredType
	}{
		{name: "int_type", v: table.FloatVal(1.5), typ: &inferredType{typ: table.TypeInt}},
		{name: "float_type", v: table.BoolVal(true), typ: &inferredType{typ: table.TypeFloat}},
		{name: "bool_type", v: table.IntVal(1), typ: &inferredType{typ: table.TypeBool}},
		{name: "record_type", v: table.StrVal("x"), typ: &inferredType{typ: table.TypeRecord}},
		{name: "list_type", v: table.StrVal("x"), typ: &inferredType{typ: table.TypeList, elem: &inferredType{typ: table.TypeInt}}},
		{
			name: "record_field",
			v: table.RecordVal([]table.RecordField{
				{Name: "n", Value: table.StrVal("x")},
			}),
			typ: &inferredType{typ: table.TypeRecord, fields: []inferredField{
				{name: "n", typ: &inferredType{typ: table.TypeInt}},
			}},
		},
		{
			name: "list_item",
			v:    table.ListVal([]table.Value{table.StrVal("x")}),
			typ:  &inferredType{typ: table.TypeList, elem: &inferredType{typ: table.TypeInt}},
		},
		{
			name: "nullable_record_list_item",
			v:    table.ListVal([]table.Value{table.StrVal("x")}),
			typ: &inferredType{typ: table.TypeList, elem: &inferredType{
				typ:      table.TypeRecord,
				nullable: true,
				fields:   []inferredField{{name: "n", typ: &inferredType{typ: table.TypeInt}}},
			}},
		},
	}
	for _, tc := range errorCases {
		if _, err := nativeValue(tc.v, tc.typ); err == nil {
			t.Fatalf("%s: expected error", tc.name)
		}
	}
}

func TestAvroUnionNameCoversAllTypes(t *testing.T) {
	cases := []struct {
		typ  *inferredType
		want string
	}{
		{typ: &inferredType{typ: table.TypeInt}, want: "long"},
		{typ: &inferredType{typ: table.TypeFloat}, want: "double"},
		{typ: &inferredType{typ: table.TypeString}, want: "string"},
		{typ: &inferredType{typ: table.TypeNull}, want: "string"},
		{typ: &inferredType{typ: table.TypeBool}, want: "boolean"},
		{typ: &inferredType{typ: table.TypeList}, want: "array"},
		{typ: &inferredType{typ: table.TypeRecord, avroName: "Row"}, want: "Row"},
		{typ: &inferredType{typ: table.ValueType(99)}, want: "string"},
	}
	for _, tc := range cases {
		if got := avroUnionName(tc.typ); got != tc.want {
			t.Fatalf("avroUnionName(%v): want %q, got %q", tc.typ.typ, tc.want, got)
		}
	}
}

func TestParquetScalarAndListElementBranches(t *testing.T) {
	intField := reflect.New(reflect.TypeOf(int64(0))).Elem()
	if err := setParquetScalar(intField, table.IntVal(7), &inferredType{typ: table.TypeInt}); err != nil {
		t.Fatalf("set int scalar: %v", err)
	}
	if intField.Int() != 7 {
		t.Fatalf("int scalar: want 7, got %d", intField.Int())
	}
	if err := setParquetScalar(intField, table.StrVal("x"), &inferredType{typ: table.TypeInt}); err == nil {
		t.Fatal("expected int scalar with string value to fail")
	}

	floatField := reflect.New(reflect.TypeOf(float64(0))).Elem()
	if err := setParquetScalar(floatField, table.IntVal(2), &inferredType{typ: table.TypeFloat}); err != nil {
		t.Fatalf("set float scalar from int: %v", err)
	}
	if floatField.Float() != 2 {
		t.Fatalf("float scalar: want 2, got %v", floatField.Float())
	}
	if err := setParquetScalar(floatField, table.StrVal("x"), &inferredType{typ: table.TypeFloat}); err == nil {
		t.Fatal("expected float scalar with string value to fail")
	}

	boolField := reflect.New(reflect.TypeOf(false)).Elem()
	if err := setParquetScalar(boolField, table.BoolVal(true), &inferredType{typ: table.TypeBool}); err != nil {
		t.Fatalf("set bool scalar: %v", err)
	}
	if !boolField.Bool() {
		t.Fatal("bool scalar: want true")
	}
	if err := setParquetScalar(boolField, table.IntVal(1), &inferredType{typ: table.TypeBool}); err == nil {
		t.Fatal("expected bool scalar with int value to fail")
	}

	wrapperType := parquetNullablePrimitiveWrapperType(reflect.TypeOf(int64(0)))
	wrapper := reflect.New(wrapperType).Elem()
	if err := setParquetListElement(wrapper, table.IntVal(9), &inferredType{typ: table.TypeInt, nullable: true}); err != nil {
		t.Fatalf("set nullable primitive list element: %v", err)
	}
	if wrapper.Field(0).IsNil() || wrapper.Field(0).Elem().Int() != 9 {
		t.Fatalf("nullable primitive wrapper: got %#v", wrapper.Interface())
	}
	if err := setParquetListElement(wrapper, table.Null(), &inferredType{typ: table.TypeInt, nullable: true}); err != nil {
		t.Fatalf("set null nullable primitive list element: %v", err)
	}
	if !wrapper.Field(0).IsNil() {
		t.Fatal("nullable primitive wrapper should clear null element")
	}

	ptrField := reflect.New(reflect.PointerTo(reflect.TypeOf(int64(0)))).Elem()
	if err := setParquetListElement(ptrField, table.IntVal(3), &inferredType{typ: table.TypeInt, nullable: true}); err != nil {
		t.Fatalf("set pointer list element: %v", err)
	}
	if ptrField.IsNil() || ptrField.Elem().Int() != 3 {
		t.Fatalf("pointer list element: got %#v", ptrField.Interface())
	}
}

func TestParquetReflectBaseTypes(t *testing.T) {
	cases := []struct {
		name string
		typ  *inferredType
		kind reflect.Kind
	}{
		{name: "nil", typ: nil, kind: reflect.String},
		{name: "int", typ: &inferredType{typ: table.TypeInt}, kind: reflect.Int64},
		{name: "float", typ: &inferredType{typ: table.TypeFloat}, kind: reflect.Float64},
		{name: "string", typ: &inferredType{typ: table.TypeString}, kind: reflect.String},
		{name: "null", typ: &inferredType{typ: table.TypeNull}, kind: reflect.String},
		{name: "bool", typ: &inferredType{typ: table.TypeBool}, kind: reflect.Bool},
		{name: "list", typ: &inferredType{typ: table.TypeList, elem: &inferredType{typ: table.TypeInt}}, kind: reflect.Slice},
		{name: "record", typ: &inferredType{typ: table.TypeRecord, fields: []inferredField{{name: "n", typ: &inferredType{typ: table.TypeInt}}}}, kind: reflect.Struct},
		{name: "unknown", typ: &inferredType{typ: table.ValueType(99)}, kind: reflect.String},
	}

	for _, tc := range cases {
		if got := parquetReflectBaseType(tc.typ); got.Kind() != tc.kind {
			t.Fatalf("%s: want kind %v, got %v", tc.name, tc.kind, got.Kind())
		}
	}
}

func TestSetParquetValueRecordListNullAndErrors(t *testing.T) {
	recordType := &inferredType{typ: table.TypeRecord, fields: []inferredField{
		{name: "flag", typ: &inferredType{typ: table.TypeBool}},
		{name: "n", typ: &inferredType{typ: table.TypeInt, nullable: true}},
	}}
	recordField := reflect.New(parquetReflectBaseType(recordType)).Elem()
	recordValue := table.RecordVal([]table.RecordField{
		{Name: "flag", Value: table.BoolVal(true)},
		{Name: "n", Value: table.IntVal(5)},
	})
	if err := setParquetValue(recordField, recordValue, recordType); err != nil {
		t.Fatalf("set record value: %v", err)
	}
	if !recordField.Field(0).Bool() || recordField.Field(1).IsNil() || recordField.Field(1).Elem().Int() != 5 {
		t.Fatalf("record field values were not set: %#v", recordField.Interface())
	}
	if err := setParquetValue(recordField, table.IntVal(1), recordType); err == nil {
		t.Fatal("expected non-record value to fail for record type")
	}

	listType := &inferredType{typ: table.TypeList, elem: &inferredType{typ: table.TypeInt}}
	listField := reflect.New(parquetReflectType(listType)).Elem()
	if err := setParquetValue(listField, table.ListVal([]table.Value{table.IntVal(1), table.IntVal(2)}), listType); err != nil {
		t.Fatalf("set list value: %v", err)
	}
	if listField.Len() != 2 || listField.Index(0).Int() != 1 || listField.Index(1).Int() != 2 {
		t.Fatalf("list field values were not set: %#v", listField.Interface())
	}
	if err := setParquetValue(listField, table.IntVal(1), listType); err == nil {
		t.Fatal("expected non-list value to fail for list type")
	}
	if err := setParquetValue(listField, table.ListVal([]table.Value{table.StrVal("x")}), listType); err == nil {
		t.Fatal("expected invalid list item to fail")
	}
	if err := setParquetValue(listField, table.Null(), listType); err != nil {
		t.Fatalf("set null list value: %v", err)
	}
	if listField.Len() != 0 {
		t.Fatalf("null list value should become empty slice, got len %d", listField.Len())
	}

	stringField := reflect.New(reflect.TypeOf("")).Elem()
	if err := setParquetValue(stringField, table.Null(), &inferredType{typ: table.TypeString}); err != nil {
		t.Fatalf("set null scalar value: %v", err)
	}
	if stringField.String() != "" {
		t.Fatalf("null scalar should set zero value, got %q", stringField.String())
	}
}
