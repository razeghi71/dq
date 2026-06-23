package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIGroupReduceSpanTDDHappyPathAcrossFlatFormats(t *testing.T) {
	bin := buildCLI(t)

	for _, input := range cliFlatUserInputFiles() {
		t.Run(input.name, func(t *testing.T) {
			rows := readCLIDescribeRows(t, runCLIQuery(t, bin,
				input.path+` | filter { age > 20 } | transform bucket = if(age > 30, "senior", "standard") | group bucket | reduce total = sum(age), avg_age = avg(age), n = count(), first_name = first(name) | remove grouped | filter { n > 0 } | sort bucket | select bucket, total, avg_age, n, first_name | describe | json`,
			))
			requireCLIDescribeSchema(t, rows, "bucket", "string", "string", 2)
			requireCLIDescribeSchema(t, rows, "total", "int", "int?", 2)
			requireCLIDescribeSchema(t, rows, "avg_age", "float", "float?", 2)
			requireCLIDescribeSchema(t, rows, "n", "int", "int", 2)
			requireCLIDescribeSchema(t, rows, "first_name", "string", "string?", 2)
			if len(rows) != 5 {
				t.Fatalf("describe columns: got %#v, want bucket/total/avg_age/n/first_name", rows)
			}
		})
	}
}

func TestCLIGroupReduceSpanTDDHappyPathAcrossNestedFormats(t *testing.T) {
	bin := buildCLI(t)

	for _, input := range cliNestedUserInputFiles() {
		t.Run(input.name, func(t *testing.T) {
			rows := readCLIDescribeRows(t, runCLIQuery(t, bin,
				input.path+` | filter { profile.stats.score > 0 } | group address.city as entries | reduce entries avg_score = avg(profile.stats.score), max_logins = max(profile.stats.logins), first_tags = first(tags), n = count() | remove entries | transform score_per_login = avg_score / max_logins | sort address_city | select address_city, avg_score, max_logins, first_tags, n, score_per_login | describe | json`,
			))
			requireCLIDescribeSchema(t, rows, "address_city", "string", "string", 2)
			requireCLIDescribeSchema(t, rows, "avg_score", "float", "float?", 2)
			requireCLIDescribeSchema(t, rows, "max_logins", "int", "int?", 2)
			requireCLIDescribeSchema(t, rows, "first_tags", "list", "list<string>?", 2)
			requireCLIDescribeSchema(t, rows, "n", "int", "int", 2)
			requireCLIDescribeSchema(t, rows, "score_per_login", "float", "float?", 2)
			if len(rows) != 6 {
				t.Fatalf("describe columns: got %#v, want nested group/reduce/transform projection", rows)
			}
		})
	}
}

func TestCLIGroupReduceSpanTDDDownstreamSchemaErrorWinsBeforeAggregateRuntimeAcrossFlatFormats(t *testing.T) {
	bin := buildCLI(t)

	for _, input := range writeCLIGroupReduceSpanOverflowInputs(t, bin) {
		t.Run(input.name, func(t *testing.T) {
			out := runCLIQueryExpectError(t, bin, input.path+` | group g | reduce total = sum(id) | select missing | json`)
			assertCLIExpressionErrorContains(t, out, "missing", "not found")
			if strings.Contains(strings.ToLower(string(out)), "integer overflow") {
				t.Fatalf("full-span planning should catch missing column before executing overflowing aggregate, got:\n%s", out)
			}
		})
	}
}

func TestCLIGroupReduceSpanTDDInvalidSchemasBeforeRowsAcrossFormats(t *testing.T) {
	bin := buildCLI(t)

	for _, input := range cliFlatUserInputFiles() {
		t.Run(input.name+"_bad_reduce_type", func(t *testing.T) {
			out := runCLIQueryExpectError(t, bin, input.path+` | filter { false } | group city | reduce bad = sum(name) | select bad | json`)
			assertCLIExpressionErrorContains(t, out, "reduce", "bad", "sum", "numeric")
		})

		t.Run(input.name+"_downstream_missing_after_reduce", func(t *testing.T) {
			out := runCLIQueryExpectError(t, bin, input.path+` | filter { false } | group city | reduce total = sum(age) | select missing | json`)
			assertCLIExpressionErrorContains(t, out, "missing", "not found")
		})
	}

	for _, input := range cliNestedUserInputFiles() {
		t.Run(input.name+"_bad_nested_reduce_path", func(t *testing.T) {
			out := runCLIQueryExpectError(t, bin, input.path+` | filter { false } | group address.city | reduce bad = first(orders.amount) | select bad | json`)
			assertCLIExpressionErrorContains(t, out, "orders", "list")
		})

		t.Run(input.name+"_missing_nested_group_key", func(t *testing.T) {
			out := runCLIQueryExpectError(t, bin, input.path+` | filter { false } | group address.missing | reduce n = count() | json`)
			assertCLIExpressionErrorContains(t, out, "group", "missing", "not found")
		})
	}
}

func TestCLIGroupReduceSpanTDDRejectsGroupNestedNameCollision(t *testing.T) {
	bin := buildCLI(t)

	dir := t.TempDir()
	path := filepath.Join(dir, "grouped.csv")
	if err := os.WriteFile(path, []byte("grouped,city,value\na,NY,1\na,NY,2\nb,SF,3\n"), 0o644); err != nil {
		t.Fatalf("write grouped csv: %v", err)
	}

	t.Run("default_grouped_name_collides_with_key", func(t *testing.T) {
		out := runCLIQueryExpectError(t, bin, path+` | group grouped | reduce n = count() | json`)
		assertCLIExpressionErrorContains(t, out, "group", "nested column name", "grouped", "collides", "as")
	})

	t.Run("explicit_nested_name_collides_with_key", func(t *testing.T) {
		out := runCLIQueryExpectError(t, bin, path+` | group city as city | reduce city n = count() | json`)
		assertCLIExpressionErrorContains(t, out, "group", "nested column name", "city", "collides", "as")
	})
}

func TestCLIGroupReduceSpanTDDAllowsDistinctNestedNameForGroupedKey(t *testing.T) {
	bin := buildCLI(t)

	dir := t.TempDir()
	path := filepath.Join(dir, "grouped.csv")
	if err := os.WriteFile(path, []byte("grouped,value\na,1\na,2\nb,3\n"), 0o644); err != nil {
		t.Fatalf("write grouped csv: %v", err)
	}

	rows := readCLIJSONMaps(t, runCLIQuery(t, bin, path+` | group grouped as rows | reduce rows n = count() | remove rows | sort grouped | json`))
	if got, want := len(rows), 2; got != want {
		t.Fatalf("rows: got %#v, want %d rows", rows, want)
	}
	if got, want := rows[0]["grouped"], "a"; got != want {
		t.Fatalf("first grouped: got %#v, want %q", got, want)
	}
	if got, want := rows[0]["n"], float64(2); got != want {
		t.Fatalf("first count: got %#v, want %#v", got, want)
	}
	if got, want := rows[1]["grouped"], "b"; got != want {
		t.Fatalf("second grouped: got %#v, want %q", got, want)
	}
	if got, want := rows[1]["n"], float64(1); got != want {
		t.Fatalf("second count: got %#v, want %#v", got, want)
	}
}

func writeCLIGroupReduceSpanOverflowInputs(t *testing.T, bin string) []struct {
	name string
	path string
} {
	t.Helper()
	dir := t.TempDir()

	csvPath := filepath.Join(dir, "overflow.csv")
	if err := os.WriteFile(csvPath, []byte("g,id\na,9223372036854775807\na,1\n"), 0o644); err != nil {
		t.Fatalf("write overflow csv: %v", err)
	}

	jsonPath := filepath.Join(dir, "overflow.json")
	if err := os.WriteFile(jsonPath, []byte(`[{"g":"a","id":9223372036854775807},{"g":"a","id":1}]`), 0o644); err != nil {
		t.Fatalf("write overflow json: %v", err)
	}

	jsonlPath := filepath.Join(dir, "overflow.jsonl")
	if err := os.WriteFile(jsonlPath, []byte("{\"g\":\"a\",\"id\":9223372036854775807}\n{\"g\":\"a\",\"id\":1}\n"), 0o644); err != nil {
		t.Fatalf("write overflow jsonl: %v", err)
	}

	avroPath := filepath.Join(dir, "overflow.avro")
	runCLIQuery(t, bin, csvPath+` | avro to `+avroPath)

	parquetPath := filepath.Join(dir, "overflow.parquet")
	runCLIQuery(t, bin, csvPath+` | parquet to `+parquetPath)

	return []struct {
		name string
		path string
	}{
		{"csv", csvPath},
		{"json", jsonPath},
		{"jsonl", jsonlPath},
		{"avro", avroPath},
		{"parquet", parquetPath},
	}
}
