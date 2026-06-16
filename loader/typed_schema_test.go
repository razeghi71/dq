package loader

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/razeghi71/dq/table"
)

func writeTypedSchemaInput(t *testing.T, dir, name, data string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func assertLoadSchemaError(t *testing.T, path string, want ...string) {
	t.Helper()
	_, err := Load(path, Options{})
	if err == nil {
		t.Fatalf("expected schema error loading %s", path)
	}
	msg := strings.ToLower(err.Error())
	for _, part := range want {
		if !strings.Contains(msg, strings.ToLower(part)) {
			t.Fatalf("error %q missing %q", err.Error(), part)
		}
	}
}

func TestLoadJSONRejectsNestedRecordSchemaConflicts(t *testing.T) {
	dir := t.TempDir()

	t.Run("json_record_field_type_conflict", func(t *testing.T) {
		path := writeTypedSchemaInput(t, dir, "record-field-conflict.json", `[
			{"id":1,"s":{"x":1}},
			{"id":2,"s":{"x":"bad"}}
		]`)
		assertLoadSchemaError(t, path, "row 2", "s.x", "int", "string")
	})

	t.Run("json_record_vs_scalar_conflict", func(t *testing.T) {
		path := writeTypedSchemaInput(t, dir, "record-scalar-conflict.json", `[
			{"id":1,"s":{"x":1}},
			{"id":2,"s":2}
		]`)
		assertLoadSchemaError(t, path, "row 2", "s", "record", "int")
	})

	t.Run("json_list_record_field_type_conflict", func(t *testing.T) {
		path := writeTypedSchemaInput(t, dir, "list-record-field-conflict.json", `[
			{"id":1,"orders":[{"amount":1}]},
			{"id":2,"orders":[{"amount":"bad"}]}
		]`)
		assertLoadSchemaError(t, path, "row 2", "orders[].amount", "int", "string")
	})
}

func TestLoadJSONPreservesWithinRowHeterogeneousLists(t *testing.T) {
	dir := t.TempDir()

	cases := []struct {
		name   string
		data   string
		column string
		schema string
		check  func(t *testing.T, v table.Value)
	}{
		{
			name:   "scalar_mixed_list",
			data:   `[{"id":1,"xs":[1,"two"]}]`,
			column: "xs",
			schema: "list<mixed>",
			check: func(t *testing.T, v table.Value) {
				t.Helper()
				if v.Type != table.TypeList || len(v.List) != 2 {
					t.Fatalf("xs: want two-element list, got %v", v)
				}
				if v.List[0].Type != table.TypeInt || v.List[0].Int != 1 {
					t.Fatalf("xs[0]: want int 1, got %v", v.List[0])
				}
				if v.List[1].Type != table.TypeString || v.List[1].Str != "two" {
					t.Fatalf("xs[1]: want string two, got %v", v.List[1])
				}
			},
		},
		{
			name:   "scalar_record_mixed_list",
			data:   `[{"id":1,"xs":[1,{"a":2}]}]`,
			column: "xs",
			schema: "list<mixed>",
			check: func(t *testing.T, v table.Value) {
				t.Helper()
				if v.Type != table.TypeList || len(v.List) != 2 {
					t.Fatalf("xs: want two-element list, got %v", v)
				}
				if v.List[0].Type != table.TypeInt || v.List[0].Int != 1 {
					t.Fatalf("xs[0]: want int 1, got %v", v.List[0])
				}
				if v.List[1].Type != table.TypeRecord {
					t.Fatalf("xs[1]: want record, got %v", v.List[1])
				}
			},
		},
		{
			name:   "record_field_mixed_within_same_list",
			data:   `[{"orders":[{"amt":1},{"amt":"x"}]}]`,
			column: "orders",
			schema: "list<record<amt:mixed>>",
			check: func(t *testing.T, v table.Value) {
				t.Helper()
				if v.Type != table.TypeList || len(v.List) != 2 {
					t.Fatalf("orders: want two records, got %v", v)
				}
				first := v.List[0].Fields[0].Value
				second := v.List[1].Fields[0].Value
				if first.Type != table.TypeInt || first.Int != 1 {
					t.Fatalf("orders[0].amt: want int 1, got %v", first)
				}
				if second.Type != table.TypeString || second.Str != "x" {
					t.Fatalf("orders[1].amt: want string x, got %v", second)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeTypedSchemaInput(t, dir, tc.name+".json", tc.data)
			tbl, err := Load(path, Options{})
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			if got := tbl.Col(tbl.ColIndex(tc.column)).Schema().String(); got != tc.schema {
				t.Fatalf("schema: got %q, want %q", got, tc.schema)
			}
			tc.check(t, tbl.Get(0, tc.column))
		})
	}
}

func TestLoadJSONLRejectsNestedRecordSchemaConflicts(t *testing.T) {
	dir := t.TempDir()

	t.Run("jsonl_record_field_type_conflict", func(t *testing.T) {
		path := writeTypedSchemaInput(t, dir, "record-field-conflict.jsonl", "{\"id\":1,\"s\":{\"x\":1}}\n{\"id\":2,\"s\":{\"x\":\"bad\"}}\n")
		assertLoadSchemaError(t, path, "line 2", "s.x", "int", "string")
	})

	t.Run("jsonl_record_vs_scalar_conflict", func(t *testing.T) {
		path := writeTypedSchemaInput(t, dir, "record-scalar-conflict.jsonl", "{\"id\":1,\"s\":{\"x\":1}}\n{\"id\":2,\"s\":2}\n")
		assertLoadSchemaError(t, path, "line 2", "s", "record", "int")
	})

	t.Run("jsonl_list_record_field_type_conflict", func(t *testing.T) {
		path := writeTypedSchemaInput(t, dir, "list-record-field-conflict.jsonl", "{\"id\":1,\"orders\":[{\"amount\":1}]}\n{\"id\":2,\"orders\":[{\"amount\":\"bad\"}]}\n")
		assertLoadSchemaError(t, path, "line 2", "orders[].amount", "int", "string")
	})
}

func TestLoadJSONLRejectsCrossRowTypedListConflicts(t *testing.T) {
	dir := t.TempDir()

	t.Run("scalar_list_element_conflict_across_rows", func(t *testing.T) {
		path := writeTypedSchemaInput(t, dir, "list-scalar-conflict.jsonl", "{\"xs\":[1]}\n{\"xs\":[\"two\"]}\n")
		assertLoadSchemaError(t, path, "line 2", "xs[]", "int", "string")
	})

	t.Run("list_record_field_conflict_across_rows", func(t *testing.T) {
		path := writeTypedSchemaInput(t, dir, "list-record-field-conflict-across-rows.jsonl", "{\"orders\":[{\"amt\":1}]}\n{\"orders\":[{\"amt\":\"x\"}]}\n")
		assertLoadSchemaError(t, path, "line 2", "orders[].amt", "int", "string")
	})
}

func TestLoadJSONLGlobRejectsCrossShardNestedSchemaConflicts(t *testing.T) {
	dir := t.TempDir()
	writeTypedSchemaInput(t, dir, "a.jsonl", "{\"id\":1,\"s\":{\"x\":1}}\n")
	writeTypedSchemaInput(t, dir, "b.jsonl", "{\"id\":2,\"s\":{\"x\":\"bad\"}}\n")

	assertLoadSchemaError(t, filepath.Join(dir, "*.jsonl"), "b.jsonl", "s.x", "int", "string")
}

func TestLoadJSONLGlobMergesSparseNestedSchemas(t *testing.T) {
	dir := t.TempDir()
	writeTypedSchemaInput(t, dir, "a.jsonl", "{\"id\":1,\"s\":{\"x\":1}}\n")
	writeTypedSchemaInput(t, dir, "b.jsonl", "{\"id\":2,\"s\":{\"y\":\"yes\"}}\n")

	tbl, err := Load(filepath.Join(dir, "*.jsonl"), Options{})
	if err != nil {
		t.Fatalf("load sparse glob: %v", err)
	}
	if got := tbl.Col(tbl.ColIndex("s")).Schema().String(); got != "record<x:int?, y:string?>" {
		t.Fatalf("schema: got %q", got)
	}
}

func TestLoadJSONLGlobMergesNullOnlyShardBeforeTypedShard(t *testing.T) {
	dir := t.TempDir()
	writeTypedSchemaInput(t, dir, "a.jsonl", "{\"id\":1,\"s\":null}\n")
	writeTypedSchemaInput(t, dir, "b.jsonl", "{\"id\":2,\"s\":{\"x\":1}}\n")

	tbl, err := Load(filepath.Join(dir, "*.jsonl"), Options{})
	if err != nil {
		t.Fatalf("load null-only shard glob: %v", err)
	}
	if got := tbl.Col(tbl.ColIndex("s")).Schema().String(); got != "record<x:int>?" {
		t.Fatalf("schema: got %q", got)
	}
	if v := tbl.Get(0, "s"); !v.IsNull() {
		t.Fatalf("row 0 s: want null, got %v", v)
	}
	if v := tbl.Get(1, "s"); v.Type != table.TypeRecord || len(v.Fields) != 1 || v.Fields[0].Name != "x" || v.Fields[0].Value.Int != 1 {
		t.Fatalf("row 1 s: want record<x:1>, got %v", v)
	}
}

func TestLoadJSONLGlobMergesMissingColumnShardBeforeTypedShard(t *testing.T) {
	dir := t.TempDir()
	writeTypedSchemaInput(t, dir, "a.jsonl", "{\"id\":1}\n")
	writeTypedSchemaInput(t, dir, "b.jsonl", "{\"id\":2,\"s\":{\"x\":1}}\n")

	tbl, err := Load(filepath.Join(dir, "*.jsonl"), Options{})
	if err != nil {
		t.Fatalf("load missing-column shard glob: %v", err)
	}
	if got := tbl.Col(tbl.ColIndex("s")).Schema().String(); got != "record<x:int>?" {
		t.Fatalf("schema: got %q", got)
	}
	if v := tbl.Get(0, "s"); !v.IsNull() {
		t.Fatalf("row 0 s: want null, got %v", v)
	}
}

func TestLoadJSONGlobMergesNullOnlyShardBeforeTypedShard(t *testing.T) {
	dir := t.TempDir()
	writeTypedSchemaInput(t, dir, "a.json", `[{"id":1,"s":null}]`)
	writeTypedSchemaInput(t, dir, "b.json", `[{"id":2,"s":{"x":1}}]`)

	tbl, err := Load(filepath.Join(dir, "*.json"), Options{})
	if err != nil {
		t.Fatalf("load null-only JSON shard glob: %v", err)
	}
	if got := tbl.Col(tbl.ColIndex("s")).Schema().String(); got != "record<x:int>?" {
		t.Fatalf("schema: got %q", got)
	}
}
