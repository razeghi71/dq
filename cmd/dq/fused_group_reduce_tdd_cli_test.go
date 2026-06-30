package main

import (
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCLIFusedGroupReduceTDDNoPayloadHappyPathAcrossFlatFormats(t *testing.T) {
	bin := buildCLI(t)
	want := []map[string]any{
		{"city": "LA", "n": float64(2), "total": float64(47), "first_name": "Bob"},
		{"city": "NY", "n": float64(3), "total": float64(105), "first_name": "Alice"},
		{"city": "SF", "n": float64(1), "total": float64(28), "first_name": "Diana"},
	}

	for _, input := range cliFusedGroupReduceTDDCanonicalFlatInputFiles(t, bin) {
		t.Run(input.name, func(t *testing.T) {
			rows := readCLIJSONMaps(t, runCLIQuery(t, bin,
				input.path+` | group city | reduce n = count(), total = sum(age), first_name = first(name) | remove grouped | sort city | select city, n, total, first_name | json`,
			))
			requireCLIFusedGroupReduceTDDJSONRows(t, rows, want)
		})
	}
}

func TestCLIFusedGroupReduceTDDNestedAggregatePathsAcrossNestedFormats(t *testing.T) {
	bin := buildCLI(t)
	want := []map[string]any{
		{"address_city": "Chicago", "avg_score": float64(0), "first_tags": []any{"moderator", "user", "beta"}, "n": float64(1)},
		{"address_city": "Los Angeles", "avg_score": float64(6.2), "first_tags": []any{"user"}, "n": float64(1)},
		{"address_city": "New York", "avg_score": float64(9.5), "first_tags": []any{"admin", "user"}, "n": float64(1)},
	}

	for _, input := range cliNestedUserInputFiles() {
		t.Run(input.name, func(t *testing.T) {
			rows := readCLIJSONMaps(t, runCLIQuery(t, bin,
				input.path+` | group address.city as rows | reduce rows avg_score = avg(profile.stats.score), first_tags = first(tags), n = count() | remove rows | sort address_city | select address_city, avg_score, first_tags, n | json`,
			))
			requireCLIFusedGroupReduceTDDJSONRows(t, rows, want)
		})
	}
}

func TestCLIFusedGroupReduceTDDNoPayloadSkipsUnreferencedLateBadRows(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()

	inputs := []struct {
		name    string
		file    string
		content string
		options string
	}{
		{
			name:    "csv",
			file:    "bad.csv",
			content: "city,age,unused\nNY,10,100\nNY,20,bad\nSF,5,300\n",
			options: " with infer_rows=1",
		},
		{
			name:    "json",
			file:    "bad.json",
			content: `[{"city":"NY","age":10,"unused":100},{"city":"NY","age":20,"unused":"bad"},{"city":"SF","age":5,"unused":300}]`,
			options: " with infer_rows=1",
		},
		{
			name:    "jsonl",
			file:    "bad.jsonl",
			content: "{\"city\":\"NY\",\"age\":10,\"unused\":100}\n{\"city\":\"NY\",\"age\":20,\"unused\":\"bad\"}\n{\"city\":\"SF\",\"age\":5,\"unused\":300}\n",
			options: " with infer_rows=1",
		},
	}

	for _, input := range inputs {
		t.Run(input.name, func(t *testing.T) {
			path := writeCLISourceProjectionTDDFile(t, dir, input.file, input.content)
			rows := readCLIJSONMaps(t, runCLIQuery(t, bin,
				path+input.options+` | group city | reduce n = count(), total = sum(age) | remove grouped | sort city | select city, n, total | json`,
			))
			requireCLIJSONColumns(t, rows, "city", "n", "total")
			if len(rows) != 2 {
				t.Fatalf("grouped rows: got %#v, want NY and SF", rows)
			}
		})
	}
}

func TestCLIFusedGroupReduceTDDNestedNoPayloadSkipsUnreferencedLateBadRows(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()

	inputs := []struct {
		name    string
		file    string
		content string
	}{
		{
			name: "json",
			file: "nested-bad.json",
			content: `[` +
				`{"address":{"city":"NY","zip":10001},"profile":{"stats":{"score":9.5}},"unused":1},` +
				`{"address":{"city":"NY","zip":10002},"profile":{"stats":{"score":8.5}},"unused":"bad"}` +
				`]`,
		},
		{
			name: "jsonl",
			file: "nested-bad.jsonl",
			content: "{\"address\":{\"city\":\"NY\",\"zip\":10001},\"profile\":{\"stats\":{\"score\":9.5}},\"unused\":1}\n" +
				"{\"address\":{\"city\":\"NY\",\"zip\":10002},\"profile\":{\"stats\":{\"score\":8.5}},\"unused\":\"bad\"}\n",
		},
	}

	for _, input := range inputs {
		t.Run(input.name, func(t *testing.T) {
			path := writeCLISourceProjectionTDDFile(t, dir, input.file, input.content)
			rows := readCLIJSONMaps(t, runCLIQuery(t, bin,
				path+` with infer_rows=1 | group address.city as rows | reduce rows avg_score = avg(profile.stats.score), first_zip = first(address.zip) | remove rows | select address_city, avg_score, first_zip | json`,
			))
			requireCLIJSONColumns(t, rows, "address_city", "avg_score", "first_zip")
			if len(rows) != 1 || rows[0]["address_city"] != "NY" {
				t.Fatalf("nested grouped rows: got %#v, want one NY row", rows)
			}
		})
	}
}

func TestCLIFusedGroupReduceTDDPayloadDemandStillReadsUnreferencedColumns(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	path := writeCLISourceProjectionTDDFile(t, dir, "payload-bad.csv", "city,age,unused\nNY,10,100\nNY,20,bad\n")

	t.Run("full_output_keeps_payload", func(t *testing.T) {
		out := runCLIQueryExpectError(t, bin,
			path+` with infer_rows=1 | group city | reduce n = count() | json`,
		)
		requireCLIDemandPruningTDDErrorContains(t, out, "unused", "int", "bad")
	})

	t.Run("payload_filter_keeps_payload", func(t *testing.T) {
		out := runCLIQueryExpectError(t, bin,
			path+` with infer_rows=1 | group city | reduce n = count() | filter { list_len(grouped) > 1 } | select city | json`,
		)
		requireCLIDemandPruningTDDErrorContains(t, out, "unused", "int", "bad")
	})
}

func TestCLIFusedGroupReduceTDDDeadReduceAssignmentsSkipRuntimeErrorsAcrossFlatFormats(t *testing.T) {
	bin := buildCLI(t)

	for _, input := range cliFusedGroupReduceTDDOverflowInputs(t, bin) {
		t.Run(input.name, func(t *testing.T) {
			rows := readCLIJSONMaps(t, runCLIQuery(t, bin,
				input.path+` | group g | reduce n = count(), too_big = sum(id) | remove grouped | select g, n | json`,
			))
			requireCLIJSONColumns(t, rows, "g", "n")
			if len(rows) != 1 || rows[0]["n"] != float64(2) {
				t.Fatalf("dead overflow result: got %#v, want one group with n=2", rows)
			}
		})
	}
}

func TestCLIFusedGroupReduceTDDDemandedReduceRuntimeErrorsStillSurfaceAcrossFlatFormats(t *testing.T) {
	bin := buildCLI(t)

	for _, input := range cliFusedGroupReduceTDDOverflowInputs(t, bin) {
		t.Run(input.name, func(t *testing.T) {
			out := runCLIQueryExpectError(t, bin,
				input.path+` | group g | reduce too_big = sum(id) | remove grouped | select g, too_big | json`,
			)
			requireCLIDemandPruningTDDErrorContains(t, out, "overflow")
		})
	}
}

func TestCLIFusedGroupReduceTDDStaticErrorsAndCompatibilityPaths(t *testing.T) {
	bin := buildCLI(t)

	for _, input := range cliFlatUserInputFiles() {
		t.Run(input.name+"_dead_static_reduce_type_error", func(t *testing.T) {
			out := runCLIQueryExpectError(t, bin,
				input.path+` | group city | reduce n = count(), bad = sum(name) | remove grouped | select city, n | json`,
			)
			assertCLIExpressionErrorContains(t, out, "reduce", "bad", "sum", "numeric")
		})
	}

	for _, input := range cliNestedUserInputFiles() {
		t.Run(input.name+"_existing_list_reduce_still_works", func(t *testing.T) {
			rows := readCLIJSONMaps(t, runCLIQuery(t, bin,
				input.path+` | reduce orders total = sum(amount), n = count() | remove orders | select name, total, n | json`,
			))
			if len(rows) == 0 {
				t.Fatalf("expected existing-list reduce rows for %s", input.name)
			}
			requireCLIJSONColumns(t, rows, "name", "total", "n")
		})
	}
}

func TestCLIFusedGroupReduceTDDGlobAndStdinKeepReadAllSourceBehavior(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()

	t.Run("glob_csv_still_reads_all_columns", func(t *testing.T) {
		writeCLISourceProjectionTDDFile(t, dir, "part-001.csv", "city,age,unused\nNY,10,100\n")
		writeCLISourceProjectionTDDFile(t, dir, "part-002.csv", "city,age,unused\nNY,20,bad\n")
		out := runCLIQueryExpectError(t, bin,
			filepath.Join(dir, "part-*.csv")+` with format=csv, infer_rows=1 | group city | reduce n = count(), total = sum(age) | remove grouped | select city, n, total | json`,
		)
		requireCLIDemandPruningTDDErrorContains(t, out, "unused", "int", "bad")
	})

	t.Run("stdin_csv_still_reads_all_columns", func(t *testing.T) {
		cmd := exec.Command(bin, `- with format=csv, infer_rows=1 | group city | reduce n = count(), total = sum(age) | remove grouped | select city, n, total | json`)
		cmd.Stdin = strings.NewReader("city,age,unused\nNY,10,100\nNY,20,bad\n")
		out, err := cmd.CombinedOutput()
		if err == nil {
			t.Fatalf("expected stdin read-all failure, got:\n%s", out)
		}
		requireCLIDemandPruningTDDErrorContains(t, out, "unused", "int", "bad")
	})
}

func cliFusedGroupReduceTDDOverflowInputs(t *testing.T, bin string) []struct {
	name string
	path string
} {
	t.Helper()
	dir := t.TempDir()

	csvPath := writeCLISourceProjectionTDDFile(t, dir, "overflow.csv", "g,id\na,9223372036854775807\na,1\n")
	jsonPath := writeCLISourceProjectionTDDFile(t, dir, "overflow.json", `[{"g":"a","id":9223372036854775807},{"g":"a","id":1}]`)
	jsonlPath := writeCLISourceProjectionTDDFile(t, dir, "overflow.jsonl", "{\"g\":\"a\",\"id\":9223372036854775807}\n{\"g\":\"a\",\"id\":1}\n")

	avroPath := filepath.Join(dir, "overflow.avro")
	runCLIQuery(t, bin, csvPath+` | avro to `+avroPath)

	parquetPath := filepath.Join(dir, "overflow.parquet")
	runCLIQuery(t, bin, csvPath+` | parquet to `+parquetPath)

	return []struct {
		name string
		path string
	}{
		{name: "csv", path: csvPath},
		{name: "json", path: jsonPath},
		{name: "jsonl", path: jsonlPath},
		{name: "avro", path: avroPath},
		{name: "parquet", path: parquetPath},
	}
}

func cliFusedGroupReduceTDDCanonicalFlatInputFiles(t *testing.T, bin string) []struct {
	name string
	path string
} {
	t.Helper()
	dir := t.TempDir()
	csv := writeCLISourceProjectionTDDFile(t, dir, "users.csv", "name,age,city\nAlice,30,NY\nBob,25,LA\nCharlie,35,NY\nDiana,28,SF\nEve,22,LA\nFrank,40,NY\n")
	json := writeCLISourceProjectionTDDFile(t, dir, "users.json", `[
{"name":"Alice","age":30,"city":"NY"},
{"name":"Bob","age":25,"city":"LA"},
{"name":"Charlie","age":35,"city":"NY"},
{"name":"Diana","age":28,"city":"SF"},
{"name":"Eve","age":22,"city":"LA"},
{"name":"Frank","age":40,"city":"NY"}
]`)
	jsonl := writeCLISourceProjectionTDDFile(t, dir, "users.jsonl",
		"{\"name\":\"Alice\",\"age\":30,\"city\":\"NY\"}\n"+
			"{\"name\":\"Bob\",\"age\":25,\"city\":\"LA\"}\n"+
			"{\"name\":\"Charlie\",\"age\":35,\"city\":\"NY\"}\n"+
			"{\"name\":\"Diana\",\"age\":28,\"city\":\"SF\"}\n"+
			"{\"name\":\"Eve\",\"age\":22,\"city\":\"LA\"}\n"+
			"{\"name\":\"Frank\",\"age\":40,\"city\":\"NY\"}\n",
	)
	avro := filepath.Join(dir, "users.avro")
	runCLIQuery(t, bin, csv+` | avro to `+avro)
	parquet := filepath.Join(dir, "users.parquet")
	runCLIQuery(t, bin, csv+` | parquet to `+parquet)

	return []struct {
		name string
		path string
	}{
		{name: "csv", path: csv},
		{name: "json", path: json},
		{name: "jsonl", path: jsonl},
		{name: "avro", path: avro},
		{name: "parquet", path: parquet},
	}
}

func requireCLIFusedGroupReduceTDDJSONRows(t *testing.T, got, want []map[string]any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("json rows:\ngot  %#v\nwant %#v", got, want)
	}
}

func TestCLIFusedGroupReduceTDDOverwriteTargets(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	path := writeCLISourceProjectionTDDFile(t, dir, "overwrite.csv", "city,age,unused\nNY,10,100\nNY,20,bad\n")

	t.Run("overwrite_group_key_still_demands_original_key_but_not_payload", func(t *testing.T) {
		rows := readCLIJSONMaps(t, runCLIQuery(t, bin,
			path+` with infer_rows=1 | group city | reduce city = count() | remove grouped | select city | json`,
		))
		requireCLIJSONColumns(t, rows, "city")
		if len(rows) != 1 || rows[0]["city"] != float64(2) {
			t.Fatalf("overwrite group key result: got %#v, want city count 2", rows)
		}
	})

	t.Run("overwrite_grouped_name_is_aggregate_output_not_payload", func(t *testing.T) {
		rows := readCLIJSONMaps(t, runCLIQuery(t, bin,
			path+` with infer_rows=1 | group city | reduce grouped = count() | select city, grouped | json`,
		))
		requireCLIJSONColumns(t, rows, "city", "grouped")
		if len(rows) != 1 || rows[0]["grouped"] != float64(2) {
			t.Fatalf("overwrite grouped result: got %#v, want grouped count 2", rows)
		}
	})
}
