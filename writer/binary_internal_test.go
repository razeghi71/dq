package writer

import (
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
