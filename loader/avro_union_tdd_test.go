package loader

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	goavro "github.com/linkedin/goavro/v2"
	"github.com/razeghi71/dq/table"
)

func TestAvroUnionSchemaDescriptorTDD(t *testing.T) {
	recordBranch := map[string]any{
		"type": "record",
		"name": "Payload",
		"fields": []any{
			map[string]any{"name": "x", "type": "long"},
		},
	}
	otherRecordBranch := map[string]any{
		"type": "record",
		"name": "Other",
		"fields": []any{
			map[string]any{"name": "y", "type": "string"},
		},
	}
	cases := []struct {
		name   string
		schema any
		want   string
	}{
		{name: "int_string", schema: []any{"int", "string"}, want: "union<int,string>"},
		{name: "string_int_preserves_declared_order", schema: []any{"string", "int"}, want: "union<string,int>"},
		{name: "null_int_string", schema: []any{"null", "int", "string"}, want: "union<int,string>?"},
		{name: "int_string_null", schema: []any{"int", "string", "null"}, want: "union<int,string>?"},
		{name: "bool_string", schema: []any{"boolean", "string"}, want: "union<bool,string>"},
		{name: "int_bool_string", schema: []any{"int", "boolean", "string"}, want: "union<int,bool,string>"},
		{name: "record_string", schema: []any{recordBranch, "string"}, want: "union<record<x:int>,string>"},
		{name: "null_record_string", schema: []any{"null", recordBranch, "string"}, want: "union<record<x:int>,string>?"},
		{name: "array_string", schema: []any{map[string]any{"type": "array", "items": "long"}, "string"}, want: "union<list<int>,string>"},
		{name: "array_item_union", schema: map[string]any{"type": "array", "items": []any{"int", "string"}}, want: "list<union<int,string>>"},
		{name: "array_item_nullable_union", schema: map[string]any{"type": "array", "items": []any{"null", "int", "string"}}, want: "list<union<int,string>?>"},
		{name: "record_field_union", schema: map[string]any{"type": "record", "name": "Row", "fields": []any{map[string]any{"name": "u", "type": []any{"int", "string"}}}}, want: "record<u:union<int,string>>"},
		{name: "record_field_nullable_union", schema: map[string]any{"type": "record", "name": "Row", "fields": []any{map[string]any{"name": "u", "type": []any{"null", "int", "string"}}}}, want: "record<u:union<int,string>?>"},
		{name: "map_type_union", schema: map[string]any{"type": []any{"int", "string"}}, want: "union<int,string>"},
		{name: "nested_map_type_nullable_union", schema: map[string]any{"type": map[string]any{"type": []any{"null", "boolean", "string"}}}, want: "union<bool,string>?"},
		{name: "numeric_union_collapses_to_float", schema: []any{"int", "double"}, want: "float"},
		{name: "nullable_numeric_union_collapses_to_float", schema: []any{"null", "int", "double"}, want: "float?"},
		{name: "int_long_union_collapses_to_int", schema: []any{"int", "long"}, want: "int"},
		{name: "string_bytes_union_collapses_to_string", schema: []any{"string", "bytes"}, want: "string"},
		{name: "enum_string_union_collapses_to_string", schema: []any{"enum", "string"}, want: "string"},
		{name: "two_record_branches", schema: []any{recordBranch, otherRecordBranch}, want: "union<record<x:int>,record<y:string>>"},
		{name: "nullable_two_record_branches", schema: []any{"null", recordBranch, otherRecordBranch}, want: "union<record<x:int>,record<y:string>>?"},
		{name: "nested_union_in_list_of_records", schema: map[string]any{"type": "array", "items": map[string]any{"type": "record", "name": "Item", "fields": []any{map[string]any{"name": "u", "type": []any{"int", "string"}}}}}, want: "list<record<u:union<int,string>>>"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := avroFieldSchemaDescriptor(tc.schema, "example")
			requireLoaderSchemaString(t, got, tc.want)
		})
	}
}

func TestAvroUnionSchemaDescriptorRejectsUnsupportedBranchesTDD(t *testing.T) {
	cases := []struct {
		name   string
		schema any
	}{
		{name: "union_with_unknown_primitive", schema: []any{"int", "decimal"}},
		{name: "union_with_unsupported_map", schema: []any{"string", map[string]any{"type": "map", "values": "string"}}},
		{name: "array_item_union_with_unknown_branch", schema: map[string]any{"type": "array", "items": []any{"int", "decimal"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := avroFieldSchemaDescriptor(tc.schema, "example"); got != nil {
				t.Fatalf("got %s, want nil", got.String())
			}
		})
	}
}

func TestAvroPrimitiveValueUsesDeclaredSchemaType(t *testing.T) {
	cases := []struct {
		name string
		typ  string
		raw  any
		want table.Value
	}{
		{name: "double_whole_number_stays_float", typ: "double", raw: float64(1), want: table.FloatVal(1)},
		{name: "float_whole_number_stays_float", typ: "float", raw: float32(2), want: table.FloatVal(2)},
		{name: "long_stays_int", typ: "long", raw: int64(3), want: table.IntVal(3)},
		{name: "int_stays_int", typ: "int", raw: int32(4), want: table.IntVal(4)},
		{name: "boolean", typ: "boolean", raw: true, want: table.BoolVal(true)},
		{name: "string", typ: "string", raw: "x", want: table.StrVal("x")},
		{name: "bytes", typ: "bytes", raw: []byte("x"), want: table.StrVal("x")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := avroPrimitiveValue(tc.raw, tc.typ); !table.Equal(got, tc.want) {
				t.Fatalf("avroPrimitiveValue(%#v, %q): got %s, want %s", tc.raw, tc.typ, got.AsString(), tc.want.AsString())
			}
		})
	}
}

func TestAvroPrimitiveValueAdditionalBranchesTDD(t *testing.T) {
	cases := []struct {
		name string
		typ  string
		raw  any
		want table.Value
	}{
		{name: "null", typ: "null", raw: "ignored", want: table.Null()},
		{name: "int_from_int", typ: "int", raw: int(4), want: table.IntVal(4)},
		{name: "int_from_int64", typ: "int", raw: int64(5), want: table.IntVal(5)},
		{name: "long_from_int", typ: "long", raw: int(6), want: table.IntVal(6)},
		{name: "long_from_int32", typ: "long", raw: int32(7), want: table.IntVal(7)},
		{name: "float_from_float32", typ: "float", raw: float32(1.5), want: table.FloatVal(1.5)},
		{name: "double_from_int64", typ: "double", raw: int64(8), want: table.FloatVal(8)},
		{name: "double_from_int32", typ: "double", raw: int32(9), want: table.FloatVal(9)},
		{name: "double_from_int", typ: "double", raw: int(10), want: table.FloatVal(10)},
		{name: "enum_string", typ: "enum", raw: "A", want: table.StrVal("A")},
		{name: "fallback_bad_int", typ: "int", raw: "not-int", want: table.StrVal("not-int")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := avroPrimitiveValue(tc.raw, tc.typ); !table.Equal(got, tc.want) {
				t.Fatalf("avroPrimitiveValue(%#v, %q): got %s, want %s", tc.raw, tc.typ, got.AsString(), tc.want.AsString())
			}
		})
	}
}

func TestAvroValueWrapperEntryPointsTDD(t *testing.T) {
	recordSchema := map[string]any{
		"type": "record",
		"name": "Payload",
		"fields": []any{
			map[string]any{"name": "x", "type": "long"},
			map[string]any{"name": "label", "type": "string"},
		},
	}
	rec := avroRecordValue(map[string]any{"x": int64(7), "label": "ok"}, recordSchema, "")
	requireAvroRecordValue(t, rec,
		table.RecordField{Name: "label", Value: table.StrVal("ok")},
		table.RecordField{Name: "x", Value: table.IntVal(7)},
	)

	xs := avroArrayValue([]any{int64(1), int64(2)}, "long", "")
	if xs.Type != table.TypeList || len(xs.List) != 2 || xs.List[0].Int != 1 || xs.List[1].Int != 2 {
		t.Fatalf("avroArrayValue: got %s, want [1, 2]", xs.AsString())
	}
}

func TestLoadAvroUnsupportedMapSchemaErrorsTDD(t *testing.T) {
	path := writeAvroUnionTDDFile(t, avroUnionTDDRowSchema(`{"name":"attrs","type":{"type":"map","values":"string"}}`), nil)
	_, err := Load(path, Options{Format: "avro"})
	if err == nil {
		t.Fatal("expected unsupported Avro map schema error")
	}
	if got, want := err.Error(), `unsupported Avro schema for field "attrs"`; !strings.Contains(got, want) {
		t.Fatalf("error: got %q, want substring %q", got, want)
	}
}

func TestLoadAvroUnionColumnsTDD(t *testing.T) {
	t.Run("empty_int_string_union_seeds_schema", func(t *testing.T) {
		path := writeAvroUnionTDDFile(t, avroUnionTDDRowSchema(`{"name":"u","type":["int","string"]}`), nil)
		tbl, err := Load(path, Options{Format: "avro"})
		if err != nil {
			t.Fatal(err)
		}
		requireAvroUnionColumn(t, tbl, "u", 0, "union", "union<int,string>")
	})

	t.Run("row_bearing_int_string_union_preserves_branch_values", func(t *testing.T) {
		path := writeAvroUnionTDDFile(t, avroUnionTDDRowSchema(`{"name":"u","type":["int","string"]}`), []map[string]any{
			{"u": goavro.Union("int", int32(7))},
			{"u": goavro.Union("string", "seven")},
		})
		tbl, err := Load(path, Options{Format: "avro"})
		if err != nil {
			t.Fatal(err)
		}
		requireAvroUnionColumn(t, tbl, "u", 2, "union", "union<int,string>")
		requireAvroUnionValue(t, tbl.Get(0, "u"), table.TypeInt, int64(7), "seven")
		requireAvroUnionValue(t, tbl.Get(1, "u"), table.TypeString, int64(7), "seven")
	})

	t.Run("nullable_empty_union_seeds_nullable_schema", func(t *testing.T) {
		path := writeAvroUnionTDDFile(t, avroUnionTDDRowSchema(`{"name":"u","type":["null","int","string"],"default":null}`), nil)
		tbl, err := Load(path, Options{Format: "avro"})
		if err != nil {
			t.Fatal(err)
		}
		requireAvroUnionColumn(t, tbl, "u", 0, "union", "union<int,string>?")
	})

	t.Run("nullable_row_bearing_union_preserves_null_and_branch_values", func(t *testing.T) {
		path := writeAvroUnionTDDFile(t, avroUnionTDDRowSchema(`{"name":"u","type":["null","int","string"],"default":null}`), []map[string]any{
			{"u": nil},
			{"u": goavro.Union("int", int32(7))},
			{"u": goavro.Union("string", "seven")},
		})
		tbl, err := Load(path, Options{Format: "avro"})
		if err != nil {
			t.Fatal(err)
		}
		requireAvroUnionColumn(t, tbl, "u", 3, "union", "union<int,string>?")
		if got := tbl.Get(0, "u"); !got.IsNull() {
			t.Fatalf("row 0 u: got %v, want null", got)
		}
		requireAvroUnionValue(t, tbl.Get(1, "u"), table.TypeInt, int64(7), "seven")
		requireAvroUnionValue(t, tbl.Get(2, "u"), table.TypeString, int64(7), "seven")
	})

	t.Run("nested_record_union_field", func(t *testing.T) {
		schema := avroUnionTDDRowSchema(`{"name":"payload","type":{"type":"record","name":"Payload","fields":[{"name":"u","type":["int","string"]}]}}`)
		path := writeAvroUnionTDDFile(t, schema, []map[string]any{
			{"payload": map[string]any{"u": goavro.Union("int", int32(7))}},
			{"payload": map[string]any{"u": goavro.Union("string", "seven")}},
		})
		tbl, err := Load(path, Options{Format: "avro"})
		if err != nil {
			t.Fatal(err)
		}
		requireAvroUnionColumn(t, tbl, "payload", 2, "record", "record<u:union<int,string>>")
	})

	t.Run("array_item_union_field", func(t *testing.T) {
		schema := avroUnionTDDRowSchema(`{"name":"xs","type":{"type":"array","items":["int","string"]}}`)
		path := writeAvroUnionTDDFile(t, schema, []map[string]any{
			{"xs": []any{goavro.Union("int", int32(1)), goavro.Union("string", "one")}},
		})
		tbl, err := Load(path, Options{Format: "avro"})
		if err != nil {
			t.Fatal(err)
		}
		requireAvroUnionColumn(t, tbl, "xs", 1, "list", "list<union<int,string>>")
		xs := tbl.Get(0, "xs")
		if xs.Type != table.TypeList || len(xs.List) != 2 {
			t.Fatalf("xs: got %v, want two branch values", xs)
		}
		requireAvroUnionValue(t, xs.List[0], table.TypeInt, int64(1), "one")
		requireAvroUnionValue(t, xs.List[1], table.TypeString, int64(1), "one")
	})

	t.Run("record_branch_union_field", func(t *testing.T) {
		schema := avroUnionTDDRowSchema(`{"name":"u","type":[{"type":"record","name":"Inner","fields":[{"name":"x","type":"long"}]},"string"]}`)
		path := writeAvroUnionTDDFile(t, schema, []map[string]any{
			{"u": goavro.Union("Inner", map[string]any{"x": int64(9)})},
			{"u": goavro.Union("string", "nine")},
		})
		tbl, err := Load(path, Options{Format: "avro"})
		if err != nil {
			t.Fatal(err)
		}
		requireAvroUnionColumn(t, tbl, "u", 2, "union", "union<record<x:int>,string>")
		if got := tbl.Get(0, "u"); got.Type != table.TypeRecord || fieldVal(t, got, "x").Int != 9 {
			t.Fatalf("record branch: got %v, want record x=9", got)
		}
		requireAvroUnionValue(t, tbl.Get(1, "u"), table.TypeString, 0, "nine")
	})

	t.Run("multiple_record_branches_preserve_disjoint_branch_values", func(t *testing.T) {
		schema := avroUnionTDDRowSchema(`{"name":"u","type":[{"type":"record","name":"XBranch","fields":[{"name":"x","type":"long"}]},{"type":"record","name":"YBranch","fields":[{"name":"y","type":"long"}]}]}`)
		path := writeAvroUnionTDDFile(t, schema, []map[string]any{
			{"u": goavro.Union("XBranch", map[string]any{"x": int64(1)})},
			{"u": goavro.Union("YBranch", map[string]any{"y": int64(2)})},
		})
		tbl, err := Load(path, Options{Format: "avro"})
		if err != nil {
			t.Fatal(err)
		}
		requireAvroUnionColumn(t, tbl, "u", 2, "union", "union<record<x:int>,record<y:int>>")
		requireAvroRecordValue(t, tbl.Get(0, "u"), table.RecordField{Name: "x", Value: table.IntVal(1)})
		requireAvroRecordValue(t, tbl.Get(1, "u"), table.RecordField{Name: "y", Value: table.IntVal(2)})
	})

	t.Run("coercible_record_branches_preserve_exact_active_value_type", func(t *testing.T) {
		schema := avroUnionTDDRowSchema(`{"name":"u","type":[{"type":"record","name":"FloatBranch","fields":[{"name":"x","type":"double"}]},{"type":"record","name":"IntBranch","fields":[{"name":"x","type":"long"}]}]}`)
		path := writeAvroUnionTDDFile(t, schema, []map[string]any{
			{"u": goavro.Union("IntBranch", map[string]any{"x": int64(1)})},
			{"u": goavro.Union("FloatBranch", map[string]any{"x": float64(1)})},
		})
		tbl, err := Load(path, Options{Format: "avro"})
		if err != nil {
			t.Fatal(err)
		}
		requireAvroUnionColumn(t, tbl, "u", 2, "union", "union<record<x:float>,record<x:int>>")
		requireAvroRecordValue(t, tbl.Get(0, "u"), table.RecordField{Name: "x", Value: table.IntVal(1)})
		requireAvroRecordValue(t, tbl.Get(1, "u"), table.RecordField{Name: "x", Value: table.FloatVal(1)})
	})

	t.Run("nested_union_record_branch_preserves_exact_active_value_type", func(t *testing.T) {
		schema := avroUnionTDDRowSchema(`{"name":"u","type":[{"type":"record","name":"Wide","fields":[{"name":"u","type":["double","string"]}]},{"type":"record","name":"Narrow","fields":[{"name":"u","type":"long"}]}]}`)
		path := writeAvroUnionTDDFile(t, schema, []map[string]any{
			{"u": goavro.Union("Narrow", map[string]any{"u": int64(7)})},
			{"u": goavro.Union("Wide", map[string]any{"u": goavro.Union("double", float64(1.5))})},
		})
		tbl, err := Load(path, Options{Format: "avro"})
		if err != nil {
			t.Fatal(err)
		}
		requireAvroUnionColumn(t, tbl, "u", 2, "union", "union<record<u:union<float,string>>,record<u:int>>")
		requireAvroRecordValue(t, tbl.Get(0, "u"), table.RecordField{Name: "u", Value: table.IntVal(7)})
		requireAvroRecordValue(t, tbl.Get(1, "u"), table.RecordField{Name: "u", Value: table.FloatVal(1.5)})
	})

	t.Run("coercible_array_record_branches_preserve_exact_active_value_type", func(t *testing.T) {
		schema := avroUnionTDDRowSchema(`{"name":"xs","type":{"type":"array","items":[{"type":"record","name":"ArrayFloatBranch","fields":[{"name":"x","type":"double"}]},{"type":"record","name":"ArrayIntBranch","fields":[{"name":"x","type":"long"}]}]}}`)
		path := writeAvroUnionTDDFile(t, schema, []map[string]any{
			{"xs": []any{
				goavro.Union("ArrayIntBranch", map[string]any{"x": int64(1)}),
				goavro.Union("ArrayFloatBranch", map[string]any{"x": float64(1)}),
			}},
		})
		tbl, err := Load(path, Options{Format: "avro"})
		if err != nil {
			t.Fatal(err)
		}
		requireAvroUnionColumn(t, tbl, "xs", 1, "list", "list<union<record<x:float>,record<x:int>>>")
		xs := tbl.Get(0, "xs")
		if xs.Type != table.TypeList || len(xs.List) != 2 {
			t.Fatalf("xs: got %v, want two branch values", xs)
		}
		requireAvroRecordValue(t, xs.List[0], table.RecordField{Name: "x", Value: table.IntVal(1)})
		requireAvroRecordValue(t, xs.List[1], table.RecordField{Name: "x", Value: table.FloatVal(1)})
	})

	t.Run("nullable_record_branch_values_preserve_narrow_values", func(t *testing.T) {
		schema := avroUnionTDDRowSchema(`{"name":"u","type":[{"type":"record","name":"Payload","fields":[{"name":"x","type":["null","long"],"default":null}]},"string"]}`)
		path := writeAvroUnionTDDFile(t, schema, []map[string]any{
			{"u": goavro.Union("Payload", map[string]any{"x": goavro.Union("long", int64(1))})},
			{"u": goavro.Union("Payload", map[string]any{"x": nil})},
			{"u": goavro.Union("string", "text")},
		})
		tbl, err := Load(path, Options{Format: "avro"})
		if err != nil {
			t.Fatal(err)
		}
		requireAvroUnionColumn(t, tbl, "u", 3, "union", "union<record<x:int?>,string>")
		requireAvroRecordValue(t, tbl.Get(0, "u"), table.RecordField{Name: "x", Value: table.IntVal(1)})
		requireAvroRecordValue(t, tbl.Get(1, "u"), table.RecordField{Name: "x", Value: table.Null()})
		requireAvroUnionValue(t, tbl.Get(2, "u"), table.TypeString, 0, "text")
	})

	t.Run("multiple_nullable_record_branches_preserve_sparse_values", func(t *testing.T) {
		schema := avroUnionTDDRowSchema(`{"name":"u","type":[{"type":"record","name":"XBranch","fields":[{"name":"x","type":["null","long"],"default":null}]},{"type":"record","name":"YBranch","fields":[{"name":"y","type":["null","long"],"default":null}]},"string"]}`)
		path := writeAvroUnionTDDFile(t, schema, []map[string]any{
			{"u": goavro.Union("YBranch", map[string]any{"y": goavro.Union("long", int64(2))})},
			{"u": goavro.Union("XBranch", map[string]any{"x": goavro.Union("long", int64(1))})},
			{"u": goavro.Union("string", "text")},
		})
		tbl, err := Load(path, Options{Format: "avro"})
		if err != nil {
			t.Fatal(err)
		}
		requireAvroUnionColumn(t, tbl, "u", 3, "union", "union<record<x:int?>,record<y:int?>,string>")
		requireAvroRecordValue(t, tbl.Get(0, "u"), table.RecordField{Name: "y", Value: table.IntVal(2)})
		requireAvroRecordValue(t, tbl.Get(1, "u"), table.RecordField{Name: "x", Value: table.IntVal(1)})
		requireAvroUnionValue(t, tbl.Get(2, "u"), table.TypeString, 0, "text")
	})

	t.Run("multiple_record_branches_preserve_shared_field_branch_values", func(t *testing.T) {
		schema := avroUnionTDDRowSchema(`{"name":"u","type":[{"type":"record","name":"XBranch","fields":[{"name":"kind","type":"string"},{"name":"x","type":"long"}]},{"type":"record","name":"YBranch","fields":[{"name":"kind","type":"string"},{"name":"y","type":"long"}]}]}`)
		path := writeAvroUnionTDDFile(t, schema, []map[string]any{
			{"u": goavro.Union("YBranch", map[string]any{"kind": "right", "y": int64(2)})},
		})
		tbl, err := Load(path, Options{Format: "avro"})
		if err != nil {
			t.Fatal(err)
		}
		requireAvroUnionColumn(t, tbl, "u", 1, "union", "union<record<kind:string, x:int>,record<kind:string, y:int>>")
		requireAvroRecordValue(t, tbl.Get(0, "u"),
			table.RecordField{Name: "kind", Value: table.StrVal("right")},
			table.RecordField{Name: "y", Value: table.IntVal(2)},
		)
	})

	t.Run("shared_field_nullable_record_branch_preserves_sparse_value", func(t *testing.T) {
		schema := avroUnionTDDRowSchema(`{"name":"u","type":[{"type":"record","name":"XBranch","fields":[{"name":"kind","type":"string"},{"name":"x","type":["null","long"],"default":null}]},{"type":"record","name":"YBranch","fields":[{"name":"kind","type":"string"},{"name":"y","type":["null","long"],"default":null}]}]}`)
		path := writeAvroUnionTDDFile(t, schema, []map[string]any{
			{"u": goavro.Union("YBranch", map[string]any{"kind": "right", "y": goavro.Union("long", int64(2))})},
		})
		tbl, err := Load(path, Options{Format: "avro"})
		if err != nil {
			t.Fatal(err)
		}
		requireAvroUnionColumn(t, tbl, "u", 1, "union", "union<record<kind:string, x:int?>,record<kind:string, y:int?>>")
		requireAvroRecordValue(t, tbl.Get(0, "u"),
			table.RecordField{Name: "kind", Value: table.StrVal("right")},
			table.RecordField{Name: "y", Value: table.IntVal(2)},
		)
	})

	t.Run("array_item_record_union_preserves_sparse_record_branches", func(t *testing.T) {
		schema := avroUnionTDDRowSchema(`{"name":"xs","type":{"type":"array","items":[{"type":"record","name":"XBranch","fields":[{"name":"x","type":"long"}]},{"type":"record","name":"YBranch","fields":[{"name":"y","type":"long"}]}]}}`)
		path := writeAvroUnionTDDFile(t, schema, []map[string]any{
			{"xs": []any{
				goavro.Union("YBranch", map[string]any{"y": int64(2)}),
				goavro.Union("XBranch", map[string]any{"x": int64(1)}),
			}},
		})
		tbl, err := Load(path, Options{Format: "avro"})
		if err != nil {
			t.Fatal(err)
		}
		requireAvroUnionColumn(t, tbl, "xs", 1, "list", "list<union<record<x:int>,record<y:int>>>")
		xs := tbl.Get(0, "xs")
		if xs.Type != table.TypeList || len(xs.List) != 2 {
			t.Fatalf("xs: got %v, want two record branch values", xs)
		}
		requireAvroRecordValue(t, xs.List[0], table.RecordField{Name: "y", Value: table.IntVal(2)})
		requireAvroRecordValue(t, xs.List[1], table.RecordField{Name: "x", Value: table.IntVal(1)})
	})

	t.Run("array_item_nullable_record_union_preserves_narrow_values", func(t *testing.T) {
		schema := avroUnionTDDRowSchema(`{"name":"xs","type":{"type":"array","items":[{"type":"record","name":"Payload","fields":[{"name":"x","type":["null","long"],"default":null}]},"string"]}}`)
		path := writeAvroUnionTDDFile(t, schema, []map[string]any{
			{"xs": []any{
				goavro.Union("Payload", map[string]any{"x": goavro.Union("long", int64(1))}),
				goavro.Union("string", "ok"),
				goavro.Union("Payload", map[string]any{"x": nil}),
			}},
		})
		tbl, err := Load(path, Options{Format: "avro"})
		if err != nil {
			t.Fatal(err)
		}
		requireAvroUnionColumn(t, tbl, "xs", 1, "list", "list<union<record<x:int?>,string>>")
		xs := tbl.Get(0, "xs")
		if xs.Type != table.TypeList || len(xs.List) != 3 {
			t.Fatalf("xs: got %v, want three branch values", xs)
		}
		requireAvroRecordValue(t, xs.List[0], table.RecordField{Name: "x", Value: table.IntVal(1)})
		requireAvroUnionValue(t, xs.List[1], table.TypeString, 0, "ok")
		requireAvroRecordValue(t, xs.List[2], table.RecordField{Name: "x", Value: table.Null()})
	})

	t.Run("nested_nullable_record_union_field_preserves_narrow_value", func(t *testing.T) {
		schema := avroUnionTDDRowSchema(`{"name":"payload","type":{"type":"record","name":"Container","fields":[{"name":"u","type":[{"type":"record","name":"PayloadBranch","fields":[{"name":"x","type":["null","long"],"default":null}]},"string"]}]}}`)
		path := writeAvroUnionTDDFile(t, schema, []map[string]any{
			{"payload": map[string]any{"u": goavro.Union("PayloadBranch", map[string]any{"x": goavro.Union("long", int64(1))})}},
			{"payload": map[string]any{"u": goavro.Union("string", "ok")}},
		})
		tbl, err := Load(path, Options{Format: "avro"})
		if err != nil {
			t.Fatal(err)
		}
		requireAvroUnionColumn(t, tbl, "payload", 2, "record", "record<u:union<record<x:int?>,string>>")
		requireAvroRecordValue(t, tbl.Get(0, "payload"),
			table.RecordField{Name: "u", Value: table.RecordVal([]table.RecordField{{Name: "x", Value: table.IntVal(1)}})},
		)
		requireAvroRecordValue(t, tbl.Get(1, "payload"),
			table.RecordField{Name: "u", Value: table.StrVal("ok")},
		)
	})

	t.Run("numeric_union_still_collapses_to_float", func(t *testing.T) {
		path := writeAvroUnionTDDFile(t, avroUnionTDDRowSchema(`{"name":"n","type":["int","double"]}`), []map[string]any{
			{"n": goavro.Union("int", int32(1))},
			{"n": goavro.Union("double", float64(1.5))},
		})
		tbl, err := Load(path, Options{Format: "avro"})
		if err != nil {
			t.Fatal(err)
		}
		requireAvroUnionColumn(t, tbl, "n", 2, "float", "float")
	})
}

func TestLoadAvroRejectsRecursiveNamedRecordsTDD(t *testing.T) {
	cases := []struct {
		name string
		rows []map[string]any
	}{
		{name: "empty"},
		{
			name: "row_bearing",
			rows: []map[string]any{
				{"node": map[string]any{
					"value": int64(1),
					"next": goavro.Union("Node", map[string]any{
						"value": int64(2),
						"next":  nil,
					}),
				}},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeAvroUnionTDDFile(t, avroRecursiveNodeSchemaTDD(), tc.rows)
			_, err := Load(path, Options{Format: "avro"})
			if err == nil {
				t.Fatal("expected recursive Avro schema load error")
			}
			for _, want := range []string{"recursive Avro schema", "node", "Node"} {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("recursive Avro error missing %q:\n%s", want, err)
				}
			}
		})
	}
}

func TestAvroRecursiveNamedReferenceShapesTDD(t *testing.T) {
	root := map[string]any{
		"type": "record",
		"name": "Row",
		"fields": []any{
			map[string]any{
				"name": "template",
				"type": map[string]any{
					"type": "record",
					"name": "Node",
					"fields": []any{
						map[string]any{"name": "value", "type": "long"},
						map[string]any{"name": "next", "type": []any{"null", "Node"}},
					},
				},
			},
		},
	}
	ctx := newAvroSchemaContext(root, "")

	for _, schema := range []any{
		"Node",
		map[string]any{"type": "array", "items": "Node"},
		map[string]any{"type": "map", "values": "Node"},
		map[string]any{"type": []any{"null", "Node"}},
		map[string]any{"type": map[string]any{"type": "array", "items": "Node"}},
	} {
		if got, ok := ctx.recursiveNamedReference(schema, "", nil); !ok || got != "Node" {
			t.Fatalf("recursiveNamedReference(%#v): got %q, %v; want Node, true", schema, got, ok)
		}
	}

	if got, ok := ctx.recursiveNamedReference("Missing", "", nil); ok || got != "" {
		t.Fatalf("missing reference: got %q, %v; want empty, false", got, ok)
	}
}

func avroUnionTDDRowSchema(field string) string {
	return `{"type":"record","name":"Row","fields":[` + field + `]}`
}

func avroRecursiveNodeSchemaTDD() string {
	return `{
	  "type":"record",
	  "name":"Row",
	  "fields":[{
	    "name":"node",
	    "type":{
	      "type":"record",
	      "name":"Node",
	      "fields":[
	        {"name":"value","type":"long"},
	        {"name":"next","type":["null","Node"],"default":null}
	      ]
	    }
	  }]
	}`
}

func writeAvroUnionTDDFile(t *testing.T, schema string, rows []map[string]any) string {
	t.Helper()
	var buf bytes.Buffer
	w, err := goavro.NewOCFWriter(goavro.OCFConfig{W: &buf, Schema: schema})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) > 0 {
		if err := w.Append(rows); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(t.TempDir(), "union.avro")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func requireAvroUnionColumn(t *testing.T, tbl *table.Table, column string, rows int, typ, schema string) {
	t.Helper()
	if tbl.NumRows != rows {
		t.Fatalf("row count: got %d, want %d", tbl.NumRows, rows)
	}
	idx := tbl.ColIndex(column)
	if idx < 0 {
		t.Fatalf("missing column %q in %v", column, tbl.Columns)
	}
	if got := table.TypeName(tbl.Col(idx).ColType()); got != typ {
		t.Fatalf("%s type: got %q, want %q", column, got, typ)
	}
	if got := tbl.Col(idx).Schema().String(); got != schema {
		t.Fatalf("%s schema: got %q, want %q", column, got, schema)
	}
}

func requireAvroRecordValue(t *testing.T, got table.Value, fields ...table.RecordField) {
	t.Helper()
	want := table.RecordVal(fields)
	if !table.Equal(got, want) {
		t.Fatalf("record branch value:\ngot  %s\nwant %s", got.AsString(), want.AsString())
	}
}

func requireAvroUnionValue(t *testing.T, got table.Value, typ table.ValueType, intWant int64, strWant string) {
	t.Helper()
	switch typ {
	case table.TypeInt:
		if got.Type != table.TypeInt || got.Int != intWant {
			t.Fatalf("value: got %v, want int %d", got, intWant)
		}
	case table.TypeString:
		if got.Type != table.TypeString || got.Str != strWant {
			t.Fatalf("value: got %v, want string %q", got, strWant)
		}
	default:
		t.Fatalf("unsupported test value type %v", typ)
	}
}
