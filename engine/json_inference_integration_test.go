package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/razeghi71/dq/ast"
	"github.com/razeghi71/dq/loader"
	"github.com/razeghi71/dq/parser"
	"github.com/razeghi71/dq/table"
)

func writeJSONInferenceIntegrationFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestIntegrationJSONLInferenceWithPipelineOps(t *testing.T) {
	dir := t.TempDir()
	path := writeJSONInferenceIntegrationFile(t, dir, "events.jsonl",
		"{\"id\":1,\"category\":\"a\",\"amount\":10}\n"+
			"{\"id\":2,\"category\":\"a\",\"amount\":\"bad\"}\n"+
			"{\"id\":3,\"category\":\"a\",\"amount\":30}\n"+
			"{\"id\":4,\"category\":\"b\",\"amount\":5}\n")
	source := path + " with infer_rows=1, max_bad_records=1"

	t.Run("filter_and_select_after_skipped_record", func(t *testing.T) {
		result := loadAndQuery(t, source, "filter { amount > 15 } | select id, amount")
		if result.NumRows != 1 || result.Get(0, "id").Int != 3 || result.Get(0, "amount").Int != 30 {
			t.Fatalf("unexpected filtered result: %s", result.String())
		}
	})

	t.Run("describe_reports_materialized_row_count_and_sampled_schema", func(t *testing.T) {
		result := loadAndQuery(t, source, `describe | filter { column == "amount" }`)
		if result.NumRows != 1 {
			t.Fatalf("describe rows: got %d, want 1\n%s", result.NumRows, result.String())
		}
		if got := result.Get(0, "type").Str; got != "int" {
			t.Fatalf("amount type: got %q, want int", got)
		}
		if got := result.Get(0, "row_count").Int; got != 3 {
			t.Fatalf("row_count after skipped bad record: got %d, want 3", got)
		}
	})

	t.Run("group_reduce_ignores_skipped_bad_records", func(t *testing.T) {
		result := loadAndQuery(t, source, "group category | reduce total = sum(amount), n = count() | remove grouped | sort category")
		if result.NumRows != 2 {
			t.Fatalf("group rows: got %d, want 2\n%s", result.NumRows, result.String())
		}
		if result.Get(0, "category").Str != "a" || result.Get(0, "total").Int != 40 || result.Get(0, "n").Int != 2 {
			t.Fatalf("category a aggregate: got %s", result.String())
		}
		if result.Get(1, "category").Str != "b" || result.Get(1, "total").Int != 5 || result.Get(1, "n").Int != 1 {
			t.Fatalf("category b aggregate: got %s", result.String())
		}
	})
}

func TestIntegrationJSONLPostSampleNullabilityDescribe(t *testing.T) {
	dir := t.TempDir()
	path := writeJSONInferenceIntegrationFile(t, dir, "events.jsonl",
		"{\"id\":1,\"s\":{\"x\":1},\"orders\":[{\"amount\":10}]}\n"+
			"{\"id\":null,\"s\":{},\"orders\":[{}]}\n")

	result := loadAndQuery(t, path+" with infer_rows=1", "describe")
	assertDescribeSchemaRows(t, result, map[string]describeSchemaMeta{
		"id":     {typ: "int", rows: 2, schema: "int?"},
		"s":      {typ: "record", rows: 2, schema: "record<x:int?>?"},
		"orders": {typ: "list", rows: 2, schema: "list<record<amount:int?>?>?"},
	})
}

func TestIntegrationJSONInferenceJoinSourceSkipsBadRecords(t *testing.T) {
	dir := t.TempDir()
	leftPath := writeJSONInferenceIntegrationFile(t, dir, "left.csv", "id\n1\n2\n3\n")
	rightPath := writeJSONInferenceIntegrationFile(t, dir, "right.jsonl",
		"{\"id\":1,\"score\":10}\n"+
			"{\"id\":\"bad\",\"score\":20}\n"+
			"{\"id\":3,\"score\":30}\n")

	q, err := parser.Parse(leftPath + ` | join left ` + rightPath + ` with infer_rows=1, max_bad_records=1 on id | sort id`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	left, err := loader.Load(q.Source.Filename, loader.FromAST(q.Source.Load))
	if err != nil {
		t.Fatalf("load left: %v", err)
	}
	result, err := Execute(q, left, func(filename string, opts ast.LoadOptions) (*table.Table, error) {
		return loader.Load(filename, loader.FromAST(opts))
	})
	if err != nil {
		t.Fatalf("join exec: %v", err)
	}
	if result.NumRows != 3 {
		t.Fatalf("joined rows: got %d, want 3\n%s", result.NumRows, result.String())
	}
	if result.Get(0, "score").Int != 10 {
		t.Fatalf("id=1 should join score 10, got %s", result.String())
	}
	if !result.Get(1, "score").IsNull() {
		t.Fatalf("id=2 should not join skipped bad record, got %s", result.String())
	}
	if result.Get(2, "score").Int != 30 {
		t.Fatalf("id=3 should join score 30, got %s", result.String())
	}
}
