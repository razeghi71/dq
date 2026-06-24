package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIJoinPlanningTDDExactKeySchemasAcrossFlatFormats(t *testing.T) {
	bin := buildCLI(t)
	wantRows := map[string]float64{
		"csv":     6,
		"json":    3,
		"jsonl":   3,
		"avro":    6,
		"parquet": 6,
	}

	for _, input := range cliFlatUserInputFiles() {
		t.Run(input.name, func(t *testing.T) {
			rows := readCLIJSONMaps(t, runCLIQuery(t, bin, input.path+` | join `+input.path+` on name | count | json`))
			if len(rows) != 1 || rows[0]["count"] != wantRows[input.name] {
				t.Fatalf("self-join count: got %#v, want %.0f", rows, wantRows[input.name])
			}
		})
	}
}

func TestCLIJoinPlanningTDDRejectsMismatchedLeftKeySchemasAcrossFlatFormats(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	right := writeCLIJoinPlanningFixture(t, dir, "right-string-id.csv", "age,label\n30,thirty\n")

	for _, input := range cliFlatUserInputFiles() {
		t.Run(input.name, func(t *testing.T) {
			out := runCLIQueryExpectError(t, bin, input.path+` | join `+right+` with infer_rows=0 on age | count | json`)
			assertCLIExpressionErrorContains(t, out, "join", "key", "type", "age", "int", "string")
		})
	}
}

func TestCLIJoinPlanningTDDRejectsMismatchedRightKeySchemasAcrossFormats(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	left := writeCLIJoinPlanningFixture(t, dir, "left-int-id.csv", "id,name\n1,Alice\n")
	rights := writeCLIJoinPlanningStringIDRightFixtures(t, bin, dir)

	for _, right := range rights {
		t.Run(right.name, func(t *testing.T) {
			out := runCLIQueryExpectError(t, bin, left+` | join `+right.path+right.opts+` on id | count | json`)
			assertCLIExpressionErrorContains(t, out, "join", "key", "type", "id", "int", "string")
		})
	}
}

func TestCLIJoinPlanningTDDAllowsBothCSVKeysAsStringsWithInferRowsZero(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	left := writeCLIJoinPlanningFixture(t, dir, "left-string-id.csv", "id,name\n007,Alice\n")
	right := writeCLIJoinPlanningFixture(t, dir, "right-string-id.csv", "id,amount\n007,10\n")

	rows := readCLIJSONMaps(t, runCLIQuery(t, bin, left+` with infer_rows=0 | join `+right+` with infer_rows=0 on id | count | json`))
	if len(rows) != 1 || rows[0]["count"] != float64(1) {
		t.Fatalf("string id join count: got %#v, want 1", rows)
	}
}

func TestCLIJoinPlanningTDDRejectsNumericAndBoolKeyMismatches(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	left := writeCLIJoinPlanningFixture(t, dir, "left.csv", "id,name\n1,Alice\n")
	floatRight := writeCLIJoinPlanningFixture(t, dir, "right-float.jsonl", "{\"id\":1.0,\"amount\":10}\n")
	boolRight := writeCLIJoinPlanningFixture(t, dir, "right-bool.jsonl", "{\"id\":true,\"amount\":10}\n")

	for _, tc := range []struct {
		name string
		path string
		want string
	}{
		{"int_vs_float", floatRight, "float"},
		{"int_vs_bool", boolRight, "bool"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := runCLIQueryExpectError(t, bin, left+` | join `+tc.path+` on id | count | json`)
			assertCLIExpressionErrorContains(t, out, "join", "key", "type", "id", "int", tc.want)
		})
	}
}

func TestCLIJoinPlanningTDDNestedDotPathKeysAcrossFormats(t *testing.T) {
	bin := buildCLI(t)

	for _, input := range cliNestedUserInputFiles() {
		t.Run(input.name, func(t *testing.T) {
			rows := readCLIDescribeRows(t, runCLIQuery(t, bin, input.path+` | join `+input.path+` on address.city | filter { false } | describe | json`))
			requireCLIDescribeSchema(t, rows, "address_city", "string", "string", 0)
			requireCLIDescribeSchema(t, rows, "address", "record", "record<city:string, street:string, zip:string>", 0)
		})
	}
}

func TestCLIJoinPlanningTDDOuterJoinNullabilityStaysConcrete(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	left := writeCLIJoinPlanningFixture(t, dir, "left.csv", "id,name\n1,Alice\n")
	right := writeCLIJoinPlanningFixture(t, dir, "right.csv", "id,amount\n2,99.5\n")

	rows := readCLIDescribeRows(t, runCLIQuery(t, bin, left+` | join full `+right+` on id | filter { false } | describe | json`))
	requireCLIDescribeSchema(t, rows, "id", "int", "int", 0)
	requireCLIDescribeSchema(t, rows, "name", "string", "string?", 0)
	requireCLIDescribeSchema(t, rows, "amount", "float", "float?", 0)
}

func TestCLIJoinPlanningTDDCompressedRightSources(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	left := writeCLIJoinPlanningFixture(t, dir, "left.csv", "id,name\n1,Alice\n2,Bob\n")
	inputs := []struct {
		name string
		path string
		data []byte
	}{
		{"csv_gzip", filepath.Join(dir, "right.csv.gz"), gzipCLIBytes(t, "id,amount\n1,10\n2,20\n")},
		{"csv_zstd", filepath.Join(dir, "right.csv.zst"), zstdCLIBytes(t, "id,amount\n1,10\n2,20\n")},
		{"csv_deflate", filepath.Join(dir, "right.csv.deflate"), deflateCLIBytes(t, "id,amount\n1,10\n2,20\n")},
		{"jsonl_gzip", filepath.Join(dir, "right.jsonl.gz"), gzipCLIBytes(t, "{\"id\":1,\"amount\":10}\n{\"id\":2,\"amount\":20}\n")},
		{"jsonl_zstd", filepath.Join(dir, "right.jsonl.zst"), zstdCLIBytes(t, "{\"id\":1,\"amount\":10}\n{\"id\":2,\"amount\":20}\n")},
		{"jsonl_deflate", filepath.Join(dir, "right.jsonl.deflate"), deflateCLIBytes(t, "{\"id\":1,\"amount\":10}\n{\"id\":2,\"amount\":20}\n")},
	}

	for _, input := range inputs {
		t.Run(input.name, func(t *testing.T) {
			if err := os.WriteFile(input.path, input.data, 0o644); err != nil {
				t.Fatalf("write compressed right fixture: %v", err)
			}
			rows := readCLIJSONMaps(t, runCLIQuery(t, bin, left+` | join `+input.path+` on id | count | json`))
			if len(rows) != 1 || rows[0]["count"] != float64(2) {
				t.Fatalf("compressed join count: got %#v, want 2", rows)
			}
		})
	}
}

func TestCLIJoinPlanningTDDUnionKeySchemasMustMatchExactly(t *testing.T) {
	bin := buildCLI(t)
	left := writeCLIAvroUnionTDDFile(t, cliAvroUnionTDDRowSchema(`{"name":"u","type":["int","string"]}`), nil)
	right := writeCLIAvroUnionTDDFile(t, cliAvroUnionTDDRowSchema(`{"name":"u","type":"int"}`), nil)

	out := runCLIQueryExpectError(t, bin, left+` | join `+right+` on u | count | json`)
	assertCLIExpressionErrorContains(t, out, "join", "key", "type", "u", "union", "int")
}

func TestCLIJoinPlanningTDDPipelinePlanningDiagnosticsAcrossJoin(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	left := writeCLIJoinPlanningFixture(t, dir, "left.csv", "id,name\n1,Alice\n")
	right := writeCLIJoinPlanningFixture(t, dir, "right.csv", "id,amount\n1,10\n")

	out := runCLIQueryExpectError(t, bin, left+` | transform y = year("bad-date") | join `+right+` on id | select missing | json`)
	assertCLIExpressionErrorContains(t, out, "select", "missing", "not found")
	if strings.Contains(strings.ToLower(string(out)), "year") {
		t.Fatalf("expected downstream planning error before year() runtime error, got:\n%s", out)
	}
}

func TestCLIJoinPlanningTDDJoinComposesWithGroupReduceAndDistinct(t *testing.T) {
	bin := buildCLI(t)
	rows := readCLIJSONMaps(t, runCLIQuery(t, bin,
		`../../testdata/users.csv | join ../../testdata/orders.csv on name == user_name | group city | reduce total = sum(amount), n = count() | remove grouped | distinct city, total, n | sort city | json`,
	))
	if len(rows) != 2 {
		t.Fatalf("joined group/reduce rows: got %#v, want 2 cities", rows)
	}
	want := []map[string]any{
		{"city": "LA", "total": float64(15), "n": float64(1)},
		{"city": "NY", "total": float64(55), "n": float64(3)},
	}
	for i := range want {
		for key, value := range want[i] {
			if rows[i][key] != value {
				t.Fatalf("joined group/reduce row %d %s: got %#v, want %#v", i, key, rows[i], want[i])
			}
		}
	}
}

func writeCLIJoinPlanningFixture(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

func writeCLIJoinPlanningStringIDRightFixtures(t *testing.T, bin, dir string) []struct {
	name string
	path string
	opts string
} {
	t.Helper()
	csv := writeCLIJoinPlanningFixture(t, dir, "right-string-id.csv", "id,amount\n1,10\n")
	json := writeCLIJoinPlanningFixture(t, dir, "right-string-id.json", `[{"id":"1","amount":10}]`)
	jsonl := writeCLIJoinPlanningFixture(t, dir, "right-string-id.jsonl", "{\"id\":\"1\",\"amount\":10}\n")

	avro := filepath.Join(dir, "right-string-id.avro")
	assertNoCLIStdout(t, runCLIQuery(t, bin, csv+` with infer_rows=0 | avro to `+avro))
	parquet := filepath.Join(dir, "right-string-id.parquet")
	assertNoCLIStdout(t, runCLIQuery(t, bin, csv+` with infer_rows=0 | parquet to `+parquet))

	return []struct {
		name string
		path string
		opts string
	}{
		{"csv", csv, " with infer_rows=0"},
		{"json", json, ""},
		{"jsonl", jsonl, ""},
		{"avro", avro, ""},
		{"parquet", parquet, ""},
	}
}
