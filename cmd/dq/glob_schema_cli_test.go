package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	goavro "github.com/linkedin/goavro/v2"
)

func TestCLIGlobPreservesSchemasAcrossFormats(t *testing.T) {
	bin := buildCLI(t)

	t.Run("csv", func(t *testing.T) {
		dir := t.TempDir()
		writeCLIGlobSchemaFile(t, dir, "part-0.csv", "id,name\n1,Alice\n")
		writeCLIGlobSchemaFile(t, dir, "part-1.csv", "id,name\n2,Bob\n")

		rows := readCLIDescribeRows(t, runCLIQuery(t, bin, filepath.Join(dir, "part-*.csv")+" | describe | json"))
		requireCLIDescribeSchema(t, rows, "id", "int", "int", 2)
		requireCLIDescribeSchema(t, rows, "name", "string", "string", 2)
	})

	t.Run("json", func(t *testing.T) {
		dir := t.TempDir()
		writeCLIGlobSchemaFile(t, dir, "part-0.json", `[{"id":1,"name":"Alice"}]`)
		writeCLIGlobSchemaFile(t, dir, "part-1.json", `[{"id":2,"name":"Bob"}]`)

		rows := readCLIDescribeRows(t, runCLIQuery(t, bin, filepath.Join(dir, "part-*.json")+" | describe | json"))
		requireCLIDescribeSchema(t, rows, "id", "int", "int", 2)
		requireCLIDescribeSchema(t, rows, "name", "string", "string", 2)
	})

	t.Run("jsonl", func(t *testing.T) {
		dir := t.TempDir()
		writeCLIGlobSchemaFile(t, dir, "part-0.jsonl", "{\"id\":1,\"name\":\"Alice\"}\n")
		writeCLIGlobSchemaFile(t, dir, "part-1.jsonl", "{\"id\":2,\"name\":\"Bob\"}\n")

		rows := readCLIDescribeRows(t, runCLIQuery(t, bin, filepath.Join(dir, "part-*.jsonl")+" | describe | json"))
		requireCLIDescribeSchema(t, rows, "id", "int", "int", 2)
		requireCLIDescribeSchema(t, rows, "name", "string", "string", 2)
	})

	t.Run("avro_union", func(t *testing.T) {
		dir := t.TempDir()
		writeCLIAvroUnionTDDFileTo(t, filepath.Join(dir, "part-0.avro"), cliAvroUnionTDDRowSchema(`{"name":"u","type":["int","string"]}`), nil)
		writeCLIAvroUnionTDDFileTo(t, filepath.Join(dir, "part-1.avro"), cliAvroUnionTDDRowSchema(`{"name":"u","type":["int","string"]}`), []map[string]any{
			{"u": goavro.Union("int", int32(7))},
			{"u": goavro.Union("string", "7")},
		})

		glob := filepath.Join(dir, "part-*.avro")
		rows := readCLIDescribeRows(t, runCLIQuery(t, bin, glob+" | describe | json"))
		requireCLIDescribeSchema(t, rows, "u", "union", "union<int,string>", 2)

		countRows := decodeCLIGlobSchemaJSONRows(t, runCLIQuery(t, bin, glob+" | distinct u | count | json"))
		if len(countRows) != 1 || countRows[0]["count"].(float64) != 2 {
			t.Fatalf("distinct union branch count: got %#v, want count=2", countRows)
		}
	})

	t.Run("avro_empty_union", func(t *testing.T) {
		dir := t.TempDir()
		schema := cliAvroUnionTDDRowSchema(`{"name":"u","type":["int","string"]}`)
		writeCLIAvroUnionTDDFileTo(t, filepath.Join(dir, "part-0.avro"), schema, nil)
		writeCLIAvroUnionTDDFileTo(t, filepath.Join(dir, "part-1.avro"), schema, nil)

		rows := readCLIDescribeRows(t, runCLIQuery(t, bin, filepath.Join(dir, "part-*.avro")+" | describe | json"))
		requireCLIDescribeSchema(t, rows, "u", "union", "union<int,string>", 0)
	})

	t.Run("parquet", func(t *testing.T) {
		dir := t.TempDir()
		runCLIQuery(t, bin, "../../testdata/users.csv | head 1 | select age | parquet to "+filepath.Join(dir, "part-0.parquet"))
		runCLIQuery(t, bin, "../../testdata/users.csv | tail 1 | select age | parquet to "+filepath.Join(dir, "part-1.parquet"))

		rows := readCLIDescribeRows(t, runCLIQuery(t, bin, filepath.Join(dir, "part-*.parquet")+" | describe | json"))
		requireCLIDescribeSchema(t, rows, "age", "int", "int", 2)
	})

	t.Run("parquet_empty", func(t *testing.T) {
		dir := t.TempDir()
		runCLIQuery(t, bin, "../../testdata/users.csv | filter { false } | select age | parquet to "+filepath.Join(dir, "part-0.parquet"))
		runCLIQuery(t, bin, "../../testdata/users.csv | filter { false } | select age | parquet to "+filepath.Join(dir, "part-1.parquet"))

		rows := readCLIDescribeRows(t, runCLIQuery(t, bin, filepath.Join(dir, "part-*.parquet")+" | describe | json"))
		requireCLIDescribeSchema(t, rows, "age", "int", "int", 0)
	})
}

func TestCLIGlobPermissiveShardSchemaWideningAcrossBinaryFormats(t *testing.T) {
	bin := buildCLI(t)

	for _, format := range []string{"avro", "parquet"} {
		t.Run(format, func(t *testing.T) {
			dir := t.TempDir()
			intCSV := writeCLIGlobSchemaFile(t, dir, "int.csv", "id\n7\n")
			stringCSV := writeCLIGlobSchemaFile(t, dir, "string.csv", "id\neight\n")

			runCLIQuery(t, bin, intCSV+" | "+format+" to "+filepath.Join(dir, "part-0."+format))
			runCLIQuery(t, bin, stringCSV+" | "+format+" to "+filepath.Join(dir, "part-1."+format))

			glob := filepath.Join(dir, "part-*."+format)
			rows := readCLIDescribeRows(t, runCLIQuery(t, bin, glob+" | describe | json"))
			requireCLIDescribeSchema(t, rows, "id", "string", "string", 2)

			outRows := decodeCLIGlobSchemaJSONRows(t, runCLIQuery(t, bin, glob+" | select id | json"))
			if len(outRows) != 2 {
				t.Fatalf("%s widened rows: got %#v, want 2 rows", format, outRows)
			}
			for i, want := range []string{"7", "eight"} {
				got, ok := outRows[i]["id"].(string)
				if !ok || got != want {
					t.Fatalf("%s widened row %d: got %#v, want id=%q", format, i, outRows[i], want)
				}
			}
		})
	}
}

func writeCLIGlobSchemaFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeCLIAvroUnionTDDFileTo(t *testing.T, path, schema string, rows []map[string]any) {
	t.Helper()
	data := cliAvroUnionTDDFileBytes(t, schema, rows)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func decodeCLIGlobSchemaJSONRows(t *testing.T, out []byte) []map[string]any {
	t.Helper()
	var rows []map[string]any
	if err := json.Unmarshal(out, &rows); err != nil {
		t.Fatalf("json output: %v\n%s", err, out)
	}
	return rows
}
