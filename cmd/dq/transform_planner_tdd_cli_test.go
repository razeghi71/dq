package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLITransformPlannerTDDTransformParticipatesInSchemaPlannedFlatPipeline(t *testing.T) {
	bin := buildCLI(t)

	for _, input := range cliFlatUserInputFiles() {
		t.Run(input.name, func(t *testing.T) {
			rows := readCLIDescribeRows(t, runCLIQuery(t, bin,
				input.path+` | filter { false } | transform age2 = age + 1, label = upper(name), profile = struct(name = name, age = age), tags = list(city, null) | filter { age2 > 30 and label is not null } | select age2, label, profile, tags | describe | json`,
			))
			requireCLIDescribeSchema(t, rows, "age2", "int", "int", 0)
			requireCLIDescribeSchema(t, rows, "label", "string", "string", 0)
			requireCLIDescribeSchema(t, rows, "profile", "record", "record<age:int, name:string>", 0)
			requireCLIDescribeSchema(t, rows, "tags", "list", "list<string?>", 0)
			if len(rows) != 4 {
				t.Fatalf("describe columns: got %#v, want age2/label/profile/tags", rows)
			}
		})
	}
}

func TestCLITransformPlannerTDDTransformParticipatesInSchemaPlannedNestedPipeline(t *testing.T) {
	bin := buildCLI(t)

	for _, input := range cliNestedUserInputFiles() {
		t.Run(input.name, func(t *testing.T) {
			rows := readCLIDescribeRows(t, runCLIQuery(t, bin,
				input.path+` | filter { false } | transform city = address.city, order_count = list_len(orders), route = if(list_len(orders) > 0, "has_orders", "empty") | filter { order_count >= 0 } | select name, city, order_count, route | describe | json`,
			))
			requireCLIDescribeSchema(t, rows, "name", "string", "string", 0)
			requireCLIDescribeSchema(t, rows, "city", "string", "string", 0)
			requireCLIDescribeSchema(t, rows, "order_count", "int", "int", 0)
			requireCLIDescribeSchema(t, rows, "route", "string", "string", 0)
			if len(rows) != 4 {
				t.Fatalf("describe columns: got %#v, want name/city/order_count/route", rows)
			}
		})
	}
}

func TestCLITransformPlannerTDDDownstreamSchemaErrorsWinBeforeTransformRuntimeFlatFormats(t *testing.T) {
	bin := buildCLI(t)

	for _, input := range cliFlatUserInputFiles() {
		t.Run(input.name+"_select_missing_after_runtime_error_transform", func(t *testing.T) {
			out := runCLIQueryExpectError(t, bin, input.path+` | transform y = year("not-a-date") | select missing | json`)
			assertCLIExpressionErrorContains(t, out, "select", "missing", "not found")
			if strings.Contains(strings.ToLower(string(out)), "not-a-date") {
				t.Fatalf("downstream schema planning should run before transform runtime evaluation, got:\n%s", out)
			}
		})

		t.Run(input.name+"_sort_missing_after_runtime_error_transform", func(t *testing.T) {
			out := runCLIQueryExpectError(t, bin, input.path+` | transform y = year("not-a-date") | sort missing | json`)
			assertCLIExpressionErrorContains(t, out, "sort", "missing", "not found")
			if strings.Contains(strings.ToLower(string(out)), "not-a-date") {
				t.Fatalf("downstream schema planning should run before transform runtime evaluation, got:\n%s", out)
			}
		})
	}
}

func TestCLITransformPlannerTDDDownstreamSchemaErrorsWinBeforeTransformRuntimeNestedFormats(t *testing.T) {
	bin := buildCLI(t)

	for _, input := range cliNestedUserInputFiles() {
		t.Run(input.name+"_missing_nested_after_runtime_error_transform", func(t *testing.T) {
			out := runCLIQueryExpectError(t, bin, input.path+` | transform y = year("not-a-date") | select address.missing | json`)
			assertCLIExpressionErrorContains(t, out, "select", "missing", "not found")
			if strings.Contains(strings.ToLower(string(out)), "not-a-date") {
				t.Fatalf("downstream schema planning should run before transform runtime evaluation, got:\n%s", out)
			}
		})

		t.Run(input.name+"_list_traversal_after_runtime_error_transform", func(t *testing.T) {
			out := runCLIQueryExpectError(t, bin, input.path+` | transform y = year("not-a-date") | select orders.amount | json`)
			assertCLIExpressionErrorContains(t, out, "orders", "list")
			if strings.Contains(strings.ToLower(string(out)), "not-a-date") {
				t.Fatalf("downstream schema planning should run before transform runtime evaluation, got:\n%s", out)
			}
		})
	}
}

func TestCLITransformPlannerTDDDownstreamSchemaErrorsWinBeforeTransformRuntimeCompressedTextInputs(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	jsonl := "{\"name\":\"Alice\",\"age\":30,\"city\":\"NY\"}\n{\"name\":\"Bob\",\"age\":25,\"city\":\"LA\"}\n"

	inputs := []struct {
		name string
		path string
		data []byte
	}{
		{
			name: "csv_gzip",
			path: filepath.Join(dir, "users.csv.gz"),
			data: gzipCLIBytes(t, "name,age,city\nAlice,30,NY\nBob,25,LA\n"),
		},
		{
			name: "jsonl_zstd",
			path: filepath.Join(dir, "users.jsonl.zst"),
			data: zstdCLIBytes(t, jsonl),
		},
		{
			name: "jsonl_deflate",
			path: filepath.Join(dir, "users.jsonl.deflate"),
			data: deflateCLIBytes(t, jsonl),
		},
	}

	for _, input := range inputs {
		if err := os.WriteFile(input.path, input.data, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Run(input.name, func(t *testing.T) {
			out := runCLIQueryExpectError(t, bin, input.path+` | transform y = year("not-a-date") | select missing | json`)
			assertCLIExpressionErrorContains(t, out, "select", "missing", "not found")
			if strings.Contains(strings.ToLower(string(out)), "not-a-date") {
				t.Fatalf("downstream schema planning should run before transform runtime evaluation, got:\n%s", out)
			}
		})
	}
}
