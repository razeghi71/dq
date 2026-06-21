package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	goavro "github.com/linkedin/goavro/v2"
)

func TestCLIAvroUnionTypesTDD(t *testing.T) {
	bin := buildCLI(t)

	t.Run("empty_union_describe_uses_metadata_schema", func(t *testing.T) {
		path := writeCLIAvroUnionTDDFile(t, cliAvroUnionTDDRowSchema(`{"name":"u","type":["int","string"]}`), nil)
		rows := readCLIDescribeRows(t, runCLIQuery(t, bin, path+" | describe | json"))
		requireCLIDescribeSchema(t, rows, "u", "union", "union<int,string>", 0)
	})

	t.Run("row_bearing_union_describe_preserves_union_schema", func(t *testing.T) {
		path := writeCLIAvroUnionTDDScalarFile(t, []map[string]any{
			{"u": goavro.Union("int", int32(7))},
			{"u": goavro.Union("string", "seven")},
		})
		rows := readCLIDescribeRows(t, runCLIQuery(t, bin, path+" | describe | json"))
		requireCLIDescribeSchema(t, rows, "u", "union", "union<int,string>", 2)
	})

	t.Run("nullable_empty_union_describe_marks_union_nullable", func(t *testing.T) {
		path := writeCLIAvroUnionTDDFile(t, cliAvroUnionTDDRowSchema(`{"name":"u","type":["null","int","string"],"default":null}`), nil)
		rows := readCLIDescribeRows(t, runCLIQuery(t, bin, path+" | describe | json"))
		requireCLIDescribeSchema(t, rows, "u", "union", "union<int,string>?", 0)
	})

	t.Run("nullable_row_bearing_union_describe_preserves_nullability", func(t *testing.T) {
		path := writeCLIAvroUnionTDDFile(t, cliAvroUnionTDDRowSchema(`{"name":"u","type":["null","int","string"],"default":null}`), []map[string]any{
			{"u": nil},
			{"u": goavro.Union("int", int32(7))},
			{"u": goavro.Union("string", "seven")},
		})
		rows := readCLIDescribeRows(t, runCLIQuery(t, bin, path+" | describe | json"))
		requireCLIDescribeSchema(t, rows, "u", "union", "union<int,string>?", 3)
	})

	t.Run("filter_false_preserves_union_schema", func(t *testing.T) {
		path := writeCLIAvroUnionTDDScalarFile(t, nil)
		rows := readCLIDescribeRows(t, runCLIQuery(t, bin, path+" | filter { false } | describe | json"))
		requireCLIDescribeSchema(t, rows, "u", "union", "union<int,string>", 0)
	})

	t.Run("select_preserves_union_schema", func(t *testing.T) {
		path := writeCLIAvroUnionTDDScalarFile(t, []map[string]any{{"u": goavro.Union("int", int32(7))}})
		rows := readCLIDescribeRows(t, runCLIQuery(t, bin, path+" | select u | describe | json"))
		requireCLIDescribeSchema(t, rows, "u", "union", "union<int,string>", 1)
	})

	t.Run("head_zero_preserves_union_schema", func(t *testing.T) {
		path := writeCLIAvroUnionTDDScalarFile(t, []map[string]any{{"u": goavro.Union("string", "seven")}})
		rows := readCLIDescribeRows(t, runCLIQuery(t, bin, path+" | head 0 | describe | json"))
		requireCLIDescribeSchema(t, rows, "u", "union", "union<int,string>", 0)
	})

	t.Run("distinct_keeps_branch_identity", func(t *testing.T) {
		path := writeCLIAvroUnionTDDScalarFile(t, []map[string]any{
			{"u": goavro.Union("int", int32(7))},
			{"u": goavro.Union("string", "7")},
			{"u": goavro.Union("int", int32(7))},
		})
		rows := readCLIDescribeRows(t, runCLIQuery(t, bin, path+" | distinct u | describe | json"))
		requireCLIDescribeSchema(t, rows, "u", "union", "union<int,string>", 2)
	})

	t.Run("group_keeps_branch_identity", func(t *testing.T) {
		path := writeCLIAvroUnionTDDScalarFile(t, []map[string]any{
			{"u": goavro.Union("int", int32(7))},
			{"u": goavro.Union("string", "7")},
			{"u": goavro.Union("int", int32(7))},
		})
		rows := readCLIDescribeRows(t, runCLIQuery(t, bin, path+" | group u | reduce n = count() | remove grouped | describe | json"))
		requireCLIDescribeSchema(t, rows, "u", "union", "union<int,string>", 2)
		requireCLIDescribeSchema(t, rows, "n", "int", "int", 2)
	})

	t.Run("nested_record_field_union_describe", func(t *testing.T) {
		schema := cliAvroUnionTDDRowSchema(`{"name":"payload","type":{"type":"record","name":"Payload","fields":[{"name":"u","type":["int","string"]}]}}`)
		path := writeCLIAvroUnionTDDFile(t, schema, []map[string]any{
			{"payload": map[string]any{"u": goavro.Union("int", int32(7))}},
			{"payload": map[string]any{"u": goavro.Union("string", "seven")}},
		})
		rows := readCLIDescribeRows(t, runCLIQuery(t, bin, path+" | describe | json"))
		requireCLIDescribeSchema(t, rows, "payload", "record", "record<u:union<int,string>>", 2)
	})

	t.Run("array_item_union_describe", func(t *testing.T) {
		schema := cliAvroUnionTDDRowSchema(`{"name":"xs","type":{"type":"array","items":["int","string"]}}`)
		path := writeCLIAvroUnionTDDFile(t, schema, []map[string]any{
			{"xs": []any{goavro.Union("int", int32(1)), goavro.Union("string", "one")}},
		})
		rows := readCLIDescribeRows(t, runCLIQuery(t, bin, path+" | describe | json"))
		requireCLIDescribeSchema(t, rows, "xs", "list", "list<union<int,string>>", 1)
	})

	t.Run("record_branch_union_describe", func(t *testing.T) {
		schema := cliAvroUnionTDDRowSchema(`{"name":"u","type":[{"type":"record","name":"Inner","fields":[{"name":"x","type":"long"}]},"string"]}`)
		path := writeCLIAvroUnionTDDFile(t, schema, []map[string]any{
			{"u": goavro.Union("Inner", map[string]any{"x": int64(9)})},
			{"u": goavro.Union("string", "nine")},
		})
		rows := readCLIDescribeRows(t, runCLIQuery(t, bin, path+" | describe | json"))
		requireCLIDescribeSchema(t, rows, "u", "union", "union<record<x:int>,string>", 2)
	})

	t.Run("named_record_reference_union_empty_describe", func(t *testing.T) {
		schema := cliAvroUnionTDDNamespacedReferenceSchema()
		path := writeCLIAvroUnionTDDFile(t, schema, nil)
		rows := readCLIDescribeRows(t, runCLIQuery(t, bin, path+" | describe | json"))
		requireCLIDescribeSchema(t, rows, "template", "record", "record<label:string, x:int>", 0)
		requireCLIDescribeSchema(t, rows, "u", "record", "record<label:string, x:int>?", 0)
		requireCLIDescribeSchema(t, rows, "fq", "record", "record<label:string, x:int>?", 0)
	})

	t.Run("recursive_named_record_errors_clearly", func(t *testing.T) {
		path := writeCLIAvroUnionTDDFile(t, cliAvroRecursiveNodeSchemaTDD(), nil)
		out := runCLIQueryExpectError(t, bin, path+" | describe | json")
		for _, want := range []string{"recursive Avro schema", "node", "Node"} {
			if !strings.Contains(string(out), want) {
				t.Fatalf("recursive Avro CLI error missing %q:\n%s", want, out)
			}
		}
	})

	t.Run("named_record_reference_union_json_output", func(t *testing.T) {
		schema := cliAvroUnionTDDNamespacedReferenceSchema()
		path := writeCLIAvroUnionTDDFile(t, schema, []map[string]any{
			{
				"template": map[string]any{"x": int64(0), "label": "seed"},
				"u":        goavro.Union("com.example.Inner", map[string]any{"x": int64(7), "label": "short"}),
				"fq":       goavro.Union("com.example.Inner", map[string]any{"x": int64(8), "label": "full"}),
			},
		})
		rows := decodeCLIUnionJSONRows(t, runCLIQuery(t, bin, path+" | select u, fq | json"))
		if len(rows) != 1 {
			t.Fatalf("json rows: got %#v, want one row", rows)
		}
		requireCLIUnionJSONObject(t, rows[0]["u"], map[string]any{"label": "short", "x": float64(7)})
		requireCLIUnionJSONObject(t, rows[0]["fq"], map[string]any{"label": "full", "x": float64(8)})
	})

	t.Run("json_output_preserves_multiple_record_union_branches", func(t *testing.T) {
		schema := cliAvroUnionTDDRowSchema(`{"name":"u","type":[{"type":"record","name":"XBranch","fields":[{"name":"x","type":"long"}]},{"type":"record","name":"YBranch","fields":[{"name":"y","type":"long"}]}]}`)
		path := writeCLIAvroUnionTDDFile(t, schema, []map[string]any{
			{"u": goavro.Union("XBranch", map[string]any{"x": int64(1)})},
			{"u": goavro.Union("YBranch", map[string]any{"y": int64(2)})},
		})
		rows := decodeCLIUnionJSONRows(t, runCLIQuery(t, bin, path+" | json"))
		if len(rows) != 2 {
			t.Fatalf("json union row count: got %#v", rows)
		}
		requireCLIUnionJSONObject(t, rows[0]["u"], map[string]any{"x": float64(1)})
		requireCLIUnionJSONObject(t, rows[1]["u"], map[string]any{"y": float64(2)})
	})

	t.Run("distinct_preserves_null_record_union_branches", func(t *testing.T) {
		schema := cliAvroUnionTDDRowSchema(`{"name":"u","type":[{"type":"record","name":"XBranch","fields":[{"name":"x","type":["null","long"],"default":null}]},{"type":"record","name":"YBranch","fields":[{"name":"y","type":["null","long"],"default":null}]}]}`)
		path := writeCLIAvroUnionTDDFile(t, schema, []map[string]any{
			{"u": goavro.Union("XBranch", map[string]any{"x": nil})},
			{"u": goavro.Union("YBranch", map[string]any{"y": nil})},
		})
		rows := decodeCLIUnionJSONRows(t, runCLIQuery(t, bin, path+" | distinct u | count | json"))
		if len(rows) != 1 || rows[0]["count"].(float64) != 2 {
			t.Fatalf("distinct count: got %#v, want 2", rows)
		}

		out := strings.ToLower(string(runCLIQueryExpectError(t, bin, path+" | transform eq = u == u | json")))
		for _, part := range []string{"union", "compare"} {
			if !strings.Contains(out, part) {
				t.Fatalf("union comparison error missing %q:\n%s", part, out)
			}
		}
	})

	t.Run("json_output_preserves_shared_field_record_union_branch", func(t *testing.T) {
		schema := cliAvroUnionTDDRowSchema(`{"name":"u","type":[{"type":"record","name":"XBranch","fields":[{"name":"kind","type":"string"},{"name":"x","type":"long"}]},{"type":"record","name":"YBranch","fields":[{"name":"kind","type":"string"},{"name":"y","type":"long"}]}]}`)
		path := writeCLIAvroUnionTDDFile(t, schema, []map[string]any{
			{"u": goavro.Union("YBranch", map[string]any{"kind": "right", "y": int64(2)})},
		})
		rows := decodeCLIUnionJSONRows(t, runCLIQuery(t, bin, path+" | json"))
		if len(rows) != 1 {
			t.Fatalf("json union row count: got %#v", rows)
		}
		requireCLIUnionJSONObject(t, rows[0]["u"], map[string]any{"kind": "right", "y": float64(2)})
	})

	t.Run("coercible_record_union_branches_keep_distinct_identity", func(t *testing.T) {
		schema := cliAvroUnionTDDRowSchema(`{"name":"u","type":[{"type":"record","name":"FloatBranch","fields":[{"name":"x","type":"double"}]},{"type":"record","name":"IntBranch","fields":[{"name":"x","type":"long"}]}]}`)
		path := writeCLIAvroUnionTDDFile(t, schema, []map[string]any{
			{"u": goavro.Union("IntBranch", map[string]any{"x": int64(1)})},
			{"u": goavro.Union("FloatBranch", map[string]any{"x": float64(1)})},
			{"u": goavro.Union("IntBranch", map[string]any{"x": int64(1)})},
		})
		rows := decodeCLIUnionJSONRows(t, runCLIQuery(t, bin, path+" | distinct u | count | json"))
		if len(rows) != 1 || rows[0]["count"].(float64) != 2 {
			t.Fatalf("distinct count: got %#v, want 2", rows)
		}

		rows = decodeCLIUnionJSONRows(t, runCLIQuery(t, bin, path+" | group u | reduce n = count() | remove grouped | sort n | json"))
		if len(rows) != 2 {
			t.Fatalf("group rows: got %#v, want 2", rows)
		}
		counts := map[float64]bool{}
		for _, row := range rows {
			counts[row["n"].(float64)] = true
		}
		if !counts[1] || !counts[2] {
			t.Fatalf("group counts: got %#v, want one singleton and one duplicate group", rows)
		}
	})

	t.Run("coercible_array_record_union_branches_keep_distinct_identity", func(t *testing.T) {
		schema := cliAvroUnionTDDRowSchema(`{"name":"xs","type":{"type":"array","items":[{"type":"record","name":"ArrayFloatBranch","fields":[{"name":"x","type":"double"}]},{"type":"record","name":"ArrayIntBranch","fields":[{"name":"x","type":"long"}]}]}}`)
		path := writeCLIAvroUnionTDDFile(t, schema, []map[string]any{
			{"xs": []any{
				goavro.Union("ArrayIntBranch", map[string]any{"x": int64(1)}),
				goavro.Union("ArrayFloatBranch", map[string]any{"x": float64(1)}),
			}},
			{"xs": []any{
				goavro.Union("ArrayIntBranch", map[string]any{"x": int64(1)}),
				goavro.Union("ArrayIntBranch", map[string]any{"x": int64(1)}),
			}},
		})
		rows := decodeCLIUnionJSONRows(t, runCLIQuery(t, bin, path+" | distinct xs | count | json"))
		if len(rows) != 1 || rows[0]["count"].(float64) != 2 {
			t.Fatalf("array distinct count: got %#v, want 2", rows)
		}
	})

	t.Run("json_output_preserves_array_item_record_union_branches", func(t *testing.T) {
		schema := cliAvroUnionTDDRowSchema(`{"name":"xs","type":{"type":"array","items":[{"type":"record","name":"XBranch","fields":[{"name":"x","type":"long"}]},{"type":"record","name":"YBranch","fields":[{"name":"y","type":"long"}]}]}}`)
		path := writeCLIAvroUnionTDDFile(t, schema, []map[string]any{
			{"xs": []any{
				goavro.Union("YBranch", map[string]any{"y": int64(2)}),
				goavro.Union("XBranch", map[string]any{"x": int64(1)}),
			}},
		})
		rows := decodeCLIUnionJSONRows(t, runCLIQuery(t, bin, path+" | json"))
		if len(rows) != 1 {
			t.Fatalf("json union row count: got %#v", rows)
		}
		xs, ok := rows[0]["xs"].([]any)
		if !ok || len(xs) != 2 {
			t.Fatalf("xs: got %#v, want two objects", rows[0]["xs"])
		}
		requireCLIUnionJSONObject(t, xs[0], map[string]any{"y": float64(2)})
		requireCLIUnionJSONObject(t, xs[1], map[string]any{"x": float64(1)})
	})

	t.Run("list_len_accepts_list_with_union_elements", func(t *testing.T) {
		schema := cliAvroUnionTDDRowSchema(`{"name":"xs","type":{"type":"array","items":["int","string"]}}`)
		path := writeCLIAvroUnionTDDFile(t, schema, []map[string]any{
			{"xs": []any{
				goavro.Union("int", int32(1)),
				goavro.Union("string", "two"),
			}},
		})
		rows := decodeCLIUnionJSONRows(t, runCLIQuery(t, bin, path+" | transform n = list_len(xs) | select n | json"))
		if len(rows) != 1 {
			t.Fatalf("list_len rows: got %#v", rows)
		}
		requireCLIUnionJSONNumber(t, rows[0]["n"], 2)
	})

	t.Run("select_accepts_nullable_record_union_branch_value", func(t *testing.T) {
		schema := cliAvroUnionTDDRowSchema(`{"name":"u","type":[{"type":"record","name":"Payload","fields":[{"name":"x","type":["null","long"],"default":null}]},"string"]}`)
		path := writeCLIAvroUnionTDDFile(t, schema, []map[string]any{
			{"u": goavro.Union("Payload", map[string]any{"x": goavro.Union("long", int64(1))})},
			{"u": goavro.Union("Payload", map[string]any{"x": nil})},
			{"u": goavro.Union("string", "text")},
		})
		rows := decodeCLIUnionJSONRows(t, runCLIQuery(t, bin, path+" | select u | json"))
		if len(rows) != 3 {
			t.Fatalf("select rows: got %#v", rows)
		}
		requireCLIUnionJSONObject(t, rows[0]["u"], map[string]any{"x": float64(1)})
		requireCLIUnionJSONObject(t, rows[1]["u"], map[string]any{"x": nil})
		requireCLIUnionJSONString(t, rows[2]["u"], "text")
	})

	t.Run("schema_rebuilding_ops_accept_nullable_record_union_branch_value", func(t *testing.T) {
		schema := cliAvroUnionTDDRowSchema(`{"name":"u","type":[{"type":"record","name":"Payload","fields":[{"name":"x","type":["null","long"],"default":null}]},"string"]}`)
		path := writeCLIAvroUnionTDDFile(t, schema, []map[string]any{
			{"u": goavro.Union("Payload", map[string]any{"x": goavro.Union("long", int64(1))})},
			{"u": goavro.Union("string", "text")},
		})
		queries := []struct {
			name string
			q    string
			want int64
		}{
			{"distinct", path + " | distinct u | count | json", 2},
			{"group", path + " | group u | reduce n = count() | remove grouped | count | json", 2},
			{"transform_copy_through", path + " | transform ok = 1 | count | json", 2},
			{"filter_then_select", path + " | filter { true } | select u | count | json", 2},
		}
		for _, tc := range queries {
			t.Run(tc.name, func(t *testing.T) {
				rows := decodeCLIUnionJSONRows(t, runCLIQuery(t, bin, tc.q))
				if len(rows) != 1 || rows[0]["count"].(float64) != float64(tc.want) {
					t.Fatalf("%s count: got %#v, want %d", tc.name, rows, tc.want)
				}
			})
		}
	})

	t.Run("multiple_nullable_record_union_branches_survive_select", func(t *testing.T) {
		schema := cliAvroUnionTDDRowSchema(`{"name":"u","type":[{"type":"record","name":"XBranch","fields":[{"name":"x","type":["null","long"],"default":null}]},{"type":"record","name":"YBranch","fields":[{"name":"y","type":["null","long"],"default":null}]},"string"]}`)
		path := writeCLIAvroUnionTDDFile(t, schema, []map[string]any{
			{"u": goavro.Union("YBranch", map[string]any{"y": goavro.Union("long", int64(2))})},
			{"u": goavro.Union("XBranch", map[string]any{"x": goavro.Union("long", int64(1))})},
			{"u": goavro.Union("string", "text")},
		})
		rows := decodeCLIUnionJSONRows(t, runCLIQuery(t, bin, path+" | select u | json"))
		if len(rows) != 3 {
			t.Fatalf("select rows: got %#v", rows)
		}
		requireCLIUnionJSONObject(t, rows[0]["u"], map[string]any{"y": float64(2)})
		requireCLIUnionJSONObject(t, rows[1]["u"], map[string]any{"x": float64(1)})
		requireCLIUnionJSONString(t, rows[2]["u"], "text")
	})

	t.Run("shared_field_nullable_record_union_branch_survives_select", func(t *testing.T) {
		schema := cliAvroUnionTDDRowSchema(`{"name":"u","type":[{"type":"record","name":"XBranch","fields":[{"name":"kind","type":"string"},{"name":"x","type":["null","long"],"default":null}]},{"type":"record","name":"YBranch","fields":[{"name":"kind","type":"string"},{"name":"y","type":["null","long"],"default":null}]}]}`)
		path := writeCLIAvroUnionTDDFile(t, schema, []map[string]any{
			{"u": goavro.Union("YBranch", map[string]any{"kind": "right", "y": goavro.Union("long", int64(2))})},
		})
		rows := decodeCLIUnionJSONRows(t, runCLIQuery(t, bin, path+" | select u | json"))
		if len(rows) != 1 {
			t.Fatalf("select rows: got %#v", rows)
		}
		requireCLIUnionJSONObject(t, rows[0]["u"], map[string]any{"kind": "right", "y": float64(2)})
	})

	t.Run("array_item_nullable_record_union_survives_select", func(t *testing.T) {
		schema := cliAvroUnionTDDRowSchema(`{"name":"xs","type":{"type":"array","items":[{"type":"record","name":"Payload","fields":[{"name":"x","type":["null","long"],"default":null}]},"string"]}}`)
		path := writeCLIAvroUnionTDDFile(t, schema, []map[string]any{
			{"xs": []any{
				goavro.Union("Payload", map[string]any{"x": goavro.Union("long", int64(1))}),
				goavro.Union("string", "ok"),
				goavro.Union("Payload", map[string]any{"x": nil}),
			}},
		})
		rows := decodeCLIUnionJSONRows(t, runCLIQuery(t, bin, path+" | select xs | json"))
		if len(rows) != 1 {
			t.Fatalf("select rows: got %#v", rows)
		}
		xs, ok := rows[0]["xs"].([]any)
		if !ok || len(xs) != 3 {
			t.Fatalf("xs: got %#v, want three branch values", rows[0]["xs"])
		}
		requireCLIUnionJSONObject(t, xs[0], map[string]any{"x": float64(1)})
		requireCLIUnionJSONString(t, xs[1], "ok")
		requireCLIUnionJSONObject(t, xs[2], map[string]any{"x": nil})
	})

	t.Run("nested_nullable_record_union_field_survives_select_and_transform", func(t *testing.T) {
		schema := cliAvroUnionTDDRowSchema(`{"name":"payload","type":{"type":"record","name":"Container","fields":[{"name":"u","type":[{"type":"record","name":"PayloadBranch","fields":[{"name":"x","type":["null","long"],"default":null}]},"string"]}]}}`)
		path := writeCLIAvroUnionTDDFile(t, schema, []map[string]any{
			{"payload": map[string]any{"u": goavro.Union("PayloadBranch", map[string]any{"x": goavro.Union("long", int64(1))})}},
		})
		rows := decodeCLIUnionJSONRows(t, runCLIQuery(t, bin, path+" | transform ok = 1 | select payload, ok | json"))
		if len(rows) != 1 {
			t.Fatalf("select rows: got %#v", rows)
		}
		payload, ok := rows[0]["payload"].(map[string]any)
		if !ok {
			t.Fatalf("payload: got %#v, want object", rows[0]["payload"])
		}
		requireCLIUnionJSONObject(t, payload["u"], map[string]any{"x": float64(1)})
		requireCLIUnionJSONNumber(t, rows[0]["ok"], 1)
	})

	t.Run("glob_empty_metadata_and_nullable_record_union_value_survive_select", func(t *testing.T) {
		dir := t.TempDir()
		schema := cliAvroUnionTDDRowSchema(`{"name":"u","type":[{"type":"record","name":"Payload","fields":[{"name":"x","type":["null","long"],"default":null}]},"string"]}`)
		writeCLIAvroUnionTDDFileTo(t, filepath.Join(dir, "part-0.avro"), schema, nil)
		writeCLIAvroUnionTDDFileTo(t, filepath.Join(dir, "part-1.avro"), schema, []map[string]any{
			{"u": goavro.Union("Payload", map[string]any{"x": goavro.Union("long", int64(1))})},
		})
		rows := decodeCLIUnionJSONRows(t, runCLIQuery(t, bin, filepath.Join(dir, "part-*.avro")+" | select u | json"))
		if len(rows) != 1 {
			t.Fatalf("glob select rows: got %#v", rows)
		}
		requireCLIUnionJSONObject(t, rows[0]["u"], map[string]any{"x": float64(1)})
	})

	t.Run("json_output_preserves_branch_values", func(t *testing.T) {
		path := writeCLIAvroUnionTDDScalarFile(t, []map[string]any{
			{"u": goavro.Union("int", int32(7))},
			{"u": goavro.Union("string", "seven")},
		})
		rows := decodeCLIUnionJSONRows(t, runCLIQuery(t, bin, path+" | json"))
		if len(rows) != 2 {
			t.Fatalf("json union row count: got %#v", rows)
		}
		requireCLIUnionJSONNumber(t, rows[0]["u"], 7)
		requireCLIUnionJSONString(t, rows[1]["u"], "seven")
	})

	t.Run("jsonl_output_preserves_branch_values", func(t *testing.T) {
		path := writeCLIAvroUnionTDDScalarFile(t, []map[string]any{
			{"u": goavro.Union("int", int32(7))},
			{"u": goavro.Union("string", "seven")},
		})
		lines := strings.Split(strings.TrimSpace(string(runCLIQuery(t, bin, path+" | jsonl"))), "\n")
		if len(lines) != 2 {
			t.Fatalf("jsonl lines: got %d lines %#v", len(lines), lines)
		}
		var first, second map[string]any
		if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(lines[1]), &second); err != nil {
			t.Fatal(err)
		}
		requireCLIUnionJSONNumber(t, first["u"], 7)
		requireCLIUnionJSONString(t, second["u"], "seven")
	})

	t.Run("csv_output_formats_branch_values_without_schema_stringification", func(t *testing.T) {
		path := writeCLIAvroUnionTDDScalarFile(t, []map[string]any{
			{"u": goavro.Union("int", int32(7))},
			{"u": goavro.Union("string", "seven")},
		})
		if got, want := strings.TrimSpace(string(runCLIQuery(t, bin, path+" | csv"))), "u\n7\nseven"; got != want {
			t.Fatalf("csv output:\n%s\nwant:\n%s", got, want)
		}
	})

	t.Run("avro_output_rejects_union_schema_instead_of_stringifying", func(t *testing.T) {
		path := writeCLIAvroUnionTDDScalarFile(t, []map[string]any{
			{"u": goavro.Union("int", int32(7))},
			{"u": goavro.Union("string", "seven")},
		})
		outPath := filepath.Join(t.TempDir(), "out.avro")
		out := strings.ToLower(string(runCLIQueryExpectError(t, bin, path+" | avro to "+outPath)))
		for _, part := range []string{"avro", "union", "u"} {
			if !strings.Contains(out, part) {
				t.Fatalf("avro union output error missing %q:\n%s", part, out)
			}
		}
	})

	t.Run("parquet_output_rejects_union_schema_instead_of_stringifying", func(t *testing.T) {
		path := writeCLIAvroUnionTDDScalarFile(t, []map[string]any{
			{"u": goavro.Union("int", int32(7))},
			{"u": goavro.Union("string", "seven")},
		})
		outPath := filepath.Join(t.TempDir(), "out.parquet")
		out := strings.ToLower(string(runCLIQueryExpectError(t, bin, path+" | parquet to "+outPath)))
		for _, part := range []string{"parquet", "union", "u"} {
			if !strings.Contains(out, part) {
				t.Fatalf("parquet union output error missing %q:\n%s", part, out)
			}
		}
	})

	t.Run("is_not_null_filter_works_on_nullable_union", func(t *testing.T) {
		path := writeCLIAvroUnionTDDFile(t, cliAvroUnionTDDRowSchema(`{"name":"u","type":["null","int","string"],"default":null}`), []map[string]any{
			{"u": nil},
			{"u": goavro.Union("int", int32(7))},
			{"u": goavro.Union("string", "seven")},
		})
		rows := decodeCLIUnionJSONRows(t, runCLIQuery(t, bin, path+" | filter { u is not null } | count | json"))
		if len(rows) != 1 {
			t.Fatalf("count row count: got %#v", rows)
		}
		requireCLIUnionJSONNumber(t, rows[0]["count"], 2)
	})

	t.Run("reduce_first_last_preserves_union_active_branch_values", func(t *testing.T) {
		schema := cliAvroUnionTDDRowSchema(`{"name":"k","type":"string"},{"name":"u","type":["int","string"]}`)
		path := writeCLIAvroUnionTDDFile(t, schema, []map[string]any{
			{"k": "a", "u": goavro.Union("int", int32(7))},
			{"k": "a", "u": goavro.Union("int", int32(8))},
			{"k": "a", "u": goavro.Union("string", "seven")},
		})
		rows := decodeCLIUnionJSONRows(t, runCLIQuery(t, bin, path+" | group k | reduce first_u = first(u), last_u = last(u) | select first_u, last_u | json"))
		if len(rows) != 1 {
			t.Fatalf("reduce union row count: got %#v", rows)
		}
		requireCLIUnionJSONNumber(t, rows[0]["first_u"], 7)
		requireCLIUnionJSONString(t, rows[0]["last_u"], "seven")
	})

	t.Run("sort_union_errors_instead_of_using_branch_text_order", func(t *testing.T) {
		path := writeCLIAvroUnionTDDScalarFile(t, []map[string]any{
			{"u": goavro.Union("int", int32(7))},
			{"u": goavro.Union("string", "seven")},
		})
		out := strings.ToLower(string(runCLIQueryExpectError(t, bin, path+" | sort u | json")))
		for _, part := range []string{"sort", "union"} {
			if !strings.Contains(out, part) {
				t.Fatalf("sort union error missing %q:\n%s", part, out)
			}
		}
	})

	t.Run("sort_containers_with_nested_unions_errors", func(t *testing.T) {
		cases := []struct {
			name   string
			schema string
			rows   []map[string]any
			query  string
		}{
			{
				name:   "list_union",
				schema: cliAvroUnionTDDRowSchema(`{"name":"xs","type":{"type":"array","items":["int","string"]}}`),
				rows: []map[string]any{
					{"xs": []any{goavro.Union("int", int32(7)), goavro.Union("string", "seven")}},
					{"xs": []any{goavro.Union("string", "eight"), goavro.Union("int", int32(8))}},
				},
				query: "sort xs | json",
			},
			{
				name:   "record_union",
				schema: cliAvroUnionTDDRowSchema(`{"name":"p","type":{"type":"record","name":"SortPayload","fields":[{"name":"u","type":["int","string"]}]}}`),
				rows: []map[string]any{
					{"p": map[string]any{"u": goavro.Union("int", int32(7))}},
					{"p": map[string]any{"u": goavro.Union("string", "seven")}},
				},
				query: "sort p | json",
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				path := writeCLIAvroUnionTDDFile(t, tc.schema, tc.rows)
				out := strings.ToLower(string(runCLIQueryExpectError(t, bin, path+" | "+tc.query)))
				for _, part := range []string{"sort", "union"} {
					if !strings.Contains(out, part) {
						t.Fatalf("sort nested union error missing %q:\n%s", part, out)
					}
				}
			})
		}
	})

	t.Run("comparison_union_errors_instead_of_cross_branch_coercion", func(t *testing.T) {
		path := writeCLIAvroUnionTDDScalarFile(t, []map[string]any{
			{"u": goavro.Union("int", int32(7))},
			{"u": goavro.Union("string", "seven")},
		})
		out := strings.ToLower(string(runCLIQueryExpectError(t, bin, path+" | filter { u == 7 } | json")))
		for _, part := range []string{"filter", "union"} {
			if !strings.Contains(out, part) {
				t.Fatalf("comparison union error missing %q:\n%s", part, out)
			}
		}
	})

	t.Run("comparison_union_errors_when_operand_schema_inference_fails", func(t *testing.T) {
		intOnly := writeCLIAvroUnionTDDScalarFile(t, []map[string]any{
			{"u": goavro.Union("int", int32(7))},
			{"u": goavro.Union("int", int32(8))},
		})
		mixed := writeCLIAvroUnionTDDScalarFile(t, []map[string]any{
			{"u": goavro.Union("int", int32(7))},
			{"u": goavro.Union("string", "seven")},
		})
		queries := []struct {
			name string
			q    string
			want []string
		}{
			{"filter_arithmetic_int_only", intOnly + " | filter { u + 0 == 7 } | count | json", []string{"union", "numeric operands"}},
			{"filter_arithmetic_mixed", mixed + " | filter { u + 0 == 7 } | count | json", []string{"union", "numeric operands"}},
			{"filter_coalesce_int_only", intOnly + " | filter { coalesce(u, 1.5) == 7 } | count | json", []string{"union", "compare"}},
			{"filter_coalesce_mixed", mixed + " | filter { coalesce(u, 1.5) == 7 } | count | json", []string{"union", "compare"}},
			{"transform_arithmetic", intOnly + " | transform eq = u + 0 == 7 | json", []string{"union", "numeric operands"}},
			{"transform_coalesce", intOnly + " | transform eq = coalesce(u, 1.5) == 7 | json", []string{"union", "compare"}},
			{"transform_if_condition", intOnly + ` | transform label = if(u + 0 == 7, "yes", "no") | json`, []string{"union", "numeric operands"}},
		}
		for _, tc := range queries {
			t.Run(tc.name, func(t *testing.T) {
				out := strings.ToLower(string(runCLIQueryExpectError(t, bin, tc.q)))
				for _, part := range tc.want {
					if !strings.Contains(out, part) {
						t.Fatalf("%s union comparison error missing %q:\n%s", tc.name, part, out)
					}
				}
			})
		}
	})

	t.Run("comparison_of_union_null_check_result_still_works", func(t *testing.T) {
		path := writeCLIAvroUnionTDDScalarFile(t, []map[string]any{
			{"u": goavro.Union("int", int32(7))},
			{"u": goavro.Union("string", "seven")},
		})
		rows := decodeCLIUnionJSONRows(t, runCLIQuery(t, bin, path+" | filter { (u is not null) == true } | count | json"))
		if len(rows) != 1 {
			t.Fatalf("count row count: got %#v", rows)
		}
		requireCLIUnionJSONNumber(t, rows[0]["count"], 2)
	})

	t.Run("comparison_union_errors_in_transform_and_reduce", func(t *testing.T) {
		schema := cliAvroUnionTDDRowSchema(`{"name":"k","type":"string"},{"name":"u","type":["int","string"]}`)
		path := writeCLIAvroUnionTDDFile(t, schema, []map[string]any{
			{"k": "a", "u": goavro.Union("int", int32(7))},
			{"k": "a", "u": goavro.Union("string", "seven")},
		})
		queries := []struct {
			name string
			q    string
		}{
			{"transform_self_eq", path + " | transform eq = u == u | json"},
			{"transform_literal_eq", path + " | transform eq = u == 7 | json"},
			{"transform_if_condition", path + ` | transform s = if(u == u, "yes", "no") | json`},
			{"transform_struct_field", path + " | transform wrapped = struct(eq = u == u) | json"},
			{"transform_list_element", path + " | transform xs = list(u == u) | json"},
			{"reduce_first_self_eq", path + " | group k | reduce eq = first(u) == first(u) | json"},
			{"reduce_first_literal_neq", path + ` | group k | reduce neq = first(u) != "x" | json`},
		}
		for _, tc := range queries {
			t.Run(tc.name, func(t *testing.T) {
				out := strings.ToLower(string(runCLIQueryExpectError(t, bin, tc.q)))
				for _, part := range []string{"union", "compare"} {
					if !strings.Contains(out, part) {
						t.Fatalf("%s union comparison error missing %q:\n%s", tc.name, part, out)
					}
				}
			})
		}
	})

	t.Run("dot_path_on_union_record_branch_errors_clearly", func(t *testing.T) {
		schema := cliAvroUnionTDDRowSchema(`{"name":"u","type":[{"type":"record","name":"Inner","fields":[{"name":"x","type":"long"}]},"string"]}`)
		path := writeCLIAvroUnionTDDFile(t, schema, []map[string]any{
			{"u": goavro.Union("Inner", map[string]any{"x": int64(9)})},
			{"u": goavro.Union("string", "nine")},
		})
		out := strings.ToLower(string(runCLIQueryExpectError(t, bin, path+" | select u.x | json")))
		for _, part := range []string{"u.x", "union"} {
			if !strings.Contains(out, part) {
				t.Fatalf("dot-path union error missing %q:\n%s", part, out)
			}
		}
	})

	t.Run("dot_path_on_union_record_branch_errors_in_expressions", func(t *testing.T) {
		schema := cliAvroUnionTDDRowSchema(`{"name":"k","type":"string"},{"name":"u","type":[{"type":"record","name":"Inner","fields":[{"name":"x","type":"long"}]},"string"]}`)
		path := writeCLIAvroUnionTDDFile(t, schema, []map[string]any{
			{"k": "a", "u": goavro.Union("Inner", map[string]any{"x": int64(9)})},
			{"k": "a", "u": goavro.Union("Inner", map[string]any{"x": int64(10)})},
		})
		mixedPath := writeCLIAvroUnionTDDFile(t, schema, []map[string]any{
			{"k": "a", "u": goavro.Union("Inner", map[string]any{"x": int64(9)})},
			{"k": "b", "u": goavro.Union("string", "nine")},
		})
		queries := []struct {
			name string
			q    string
		}{
			{"transform_direct", path + " | transform x = u.x | json"},
			{"transform_struct", path + " | transform wrapped = struct(x = u.x) | json"},
			{"transform_list", path + " | transform xs = list(u.x) | json"},
			{"transform_function", path + ` | transform hit = str_contains(u.x, "9") | json`},
			{"filter_comparison", path + " | filter { u.x == 9 } | json"},
			{"filter_ordering", path + " | filter { u.x > 9 } | json"},
			{"filter_is_null", path + " | filter { u.x is not null } | json"},
			{"filter_function", path + " | filter { list_contains(list(u.x), 9) } | json"},
			{"filter_after_head_keeps_schema", mixedPath + " | head 1 | filter { u.x == 9 } | json"},
			{"transform_after_zero_rows_keeps_schema", mixedPath + " | filter { false } | transform x = u.x | json"},
			{"reduce_aggregate", path + " | group k | reduce total = sum(u.x) | json"},
			{"reduce_binary_aggregate", path + " | group k | reduce total = sum(u.x) + count() | json"},
		}
		for _, tc := range queries {
			t.Run(tc.name, func(t *testing.T) {
				out := strings.ToLower(string(runCLIQueryExpectError(t, bin, tc.q)))
				for _, part := range []string{"u.x", "union"} {
					if !strings.Contains(out, part) {
						t.Fatalf("%s union dot-path error missing %q:\n%s", tc.name, part, out)
					}
				}
			})
		}
	})

	t.Run("nested_dot_path_on_union_record_branch_errors_in_expressions", func(t *testing.T) {
		schema := cliAvroUnionTDDRowSchema(`{"name":"payload","type":{"type":"record","name":"Container","fields":[{"name":"u","type":[{"type":"record","name":"NestedInner","fields":[{"name":"x","type":"long"}]},"string"]}]}}`)
		path := writeCLIAvroUnionTDDFile(t, schema, []map[string]any{
			{"payload": map[string]any{"u": goavro.Union("NestedInner", map[string]any{"x": int64(9)})}},
		})
		for _, q := range []string{
			path + " | transform x = payload.u.x | json",
			path + " | filter { payload.u.x == 9 } | json",
		} {
			out := strings.ToLower(string(runCLIQueryExpectError(t, bin, q)))
			for _, part := range []string{"payload.u.x", "union"} {
				if !strings.Contains(out, part) {
					t.Fatalf("nested union dot-path error missing %q:\n%s", part, out)
				}
			}
		}
	})

	t.Run("join_union_key_matches_same_branch_only", func(t *testing.T) {
		left := writeCLIAvroUnionTDDScalarFile(t, []map[string]any{
			{"u": goavro.Union("int", int32(7))},
			{"u": goavro.Union("string", "7")},
		})
		right := writeCLIAvroUnionTDDFile(t, cliAvroUnionTDDRowSchema(
			`{"name":"u","type":["int","string"]},{"name":"label","type":"string"}`,
		), []map[string]any{
			{"u": goavro.Union("int", int32(7)), "label": "numeric"},
			{"u": goavro.Union("string", "7"), "label": "text"},
		})
		rows := decodeCLIUnionJSONRows(t, runCLIQuery(t, bin, left+" | join "+right+" on u | select label | sort label | json"))
		if len(rows) != 2 {
			t.Fatalf("join row count: got %#v", rows)
		}
		requireCLIUnionJSONString(t, rows[0]["label"], "numeric")
		requireCLIUnionJSONString(t, rows[1]["label"], "text")
	})
}

func cliAvroUnionTDDRowSchema(fields string) string {
	return `{"type":"record","name":"Row","fields":[` + fields + `]}`
}

func cliAvroUnionTDDNamespacedReferenceSchema() string {
	return `{
	  "type":"record","name":"Row","namespace":"com.example",
	  "fields":[
	    {"name":"template","type":{"type":"record","name":"Inner","fields":[{"name":"x","type":"long"},{"name":"label","type":"string"}]}},
	    {"name":"u","type":["null","Inner"],"default":null},
	    {"name":"fq","type":["null","com.example.Inner"],"default":null}
	  ]}`
}

func cliAvroRecursiveNodeSchemaTDD() string {
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

func writeCLIAvroUnionTDDScalarFile(t *testing.T, rows []map[string]any) string {
	t.Helper()
	return writeCLIAvroUnionTDDFile(t, cliAvroUnionTDDRowSchema(`{"name":"u","type":["int","string"]}`), rows)
}

func writeCLIAvroUnionTDDFile(t *testing.T, schema string, rows []map[string]any) string {
	t.Helper()
	data := cliAvroUnionTDDFileBytes(t, schema, rows)
	path := filepath.Join(t.TempDir(), "union.avro")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func cliAvroUnionTDDFileBytes(t *testing.T, schema string, rows []map[string]any) []byte {
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
	return buf.Bytes()
}

func decodeCLIUnionJSONRows(t *testing.T, out []byte) []map[string]any {
	t.Helper()
	var rows []map[string]any
	if err := json.Unmarshal(out, &rows); err != nil {
		t.Fatalf("json output: %v\n%s", err, out)
	}
	return rows
}

func requireCLIUnionJSONNumber(t *testing.T, got any, want float64) {
	t.Helper()
	v, ok := got.(float64)
	if !ok || v != want {
		t.Fatalf("json number: got %#v, want %g", got, want)
	}
}

func requireCLIUnionJSONString(t *testing.T, got any, want string) {
	t.Helper()
	v, ok := got.(string)
	if !ok || v != want {
		t.Fatalf("json string: got %#v, want %q", got, want)
	}
}

func requireCLIUnionJSONObject(t *testing.T, got any, want map[string]any) {
	t.Helper()
	obj, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("json object: got %#v, want %#v", got, want)
	}
	if len(obj) != len(want) {
		t.Fatalf("json object: got %#v, want %#v", obj, want)
	}
	for key, wantValue := range want {
		if obj[key] != wantValue {
			t.Fatalf("json object field %q: got %#v in %#v, want %#v", key, obj[key], obj, wantValue)
		}
	}
}
