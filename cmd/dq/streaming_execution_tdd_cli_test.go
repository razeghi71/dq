package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type cliStreamingTDDInput struct {
	name string
	path string
}

func TestCLIStreamingExecutionTDDHeadShortCircuitsRowLocalTransformAcrossAllSourceFamilies(t *testing.T) {
	bin := buildCLI(t)
	for _, input := range writeCLIStreamingTDDDateInputs(t, bin) {
		t.Run(input.name, func(t *testing.T) {
			out := runCLIQuery(t, bin,
				input.path+` | filter { id >= 1 } | select id, raw | transform y = year(raw) | head 1 | select id, y | json`,
			)
			requireCLIStreamingTDDOneIDYear(t, out)
		})
	}
}

func TestCLIStreamingExecutionTDDHeadSkipsLateSourceMaterializationErrorsForTextSources(t *testing.T) {
	bin := buildCLI(t)
	for _, tc := range writeCLIStreamingTDDLateBadTextInputs(t) {
		t.Run(tc.name, func(t *testing.T) {
			out := runCLIQuery(t, bin, tc.path+` with infer_rows=1 | head 1 | select id | json`)
			rows := readCLIJSONMaps(t, out)
			if len(rows) != 1 || rows[0]["id"] != float64(1) {
				t.Fatalf("rows: got %#v, want one row with id=1", rows)
			}
			requireCLIJSONColumns(t, rows, "id")
		})
	}
}

func TestCLIStreamingExecutionTDDHeadBeforeBlockingBoundaryMaterializesOnlyPrefix(t *testing.T) {
	bin := buildCLI(t)
	for _, input := range writeCLIStreamingTDDDateInputs(t, bin) {
		t.Run(input.name, func(t *testing.T) {
			out := runCLIQuery(t, bin,
				input.path+` | transform y = year(raw) | head 1 | sort id | select id, y | json`,
			)
			requireCLIStreamingTDDOneIDYear(t, out)
		})
	}
}

func TestCLIStreamingExecutionTDDCountIsBlockingEvenThoughBoundedMemory(t *testing.T) {
	bin := buildCLI(t)
	for _, input := range writeCLIStreamingTDDDateInputs(t, bin) {
		t.Run(input.name, func(t *testing.T) {
			out := runCLIQueryExpectError(t, bin,
				input.path+` | transform y = year(raw) | count | json`,
			)
			requireCLIStreamingTDDDateError(t, out)
		})
	}
}

func TestCLIStreamingExecutionTDDBlockingOpBeforeHeadReentersStreamingSuffix(t *testing.T) {
	bin := buildCLI(t)
	for _, input := range writeCLIStreamingTDDDateInputs(t, bin) {
		t.Run(input.name, func(t *testing.T) {
			out := runCLIQuery(t, bin,
				input.path+` | sort id | transform y = year(raw) | head 1 | select id, y | json`,
			)
			requireCLIStreamingTDDOneIDYear(t, out)
		})
	}
}

func TestCLIStreamingExecutionTDDFilterDropAvoidsDownstreamRuntimeWork(t *testing.T) {
	bin := buildCLI(t)
	for _, input := range writeCLIStreamingTDDDateInputs(t, bin) {
		t.Run(input.name, func(t *testing.T) {
			rows := readCLIJSONMaps(t, runCLIQuery(t, bin,
				input.path+` | filter { false } | transform y = year(raw) | count | json`,
			))
			if len(rows) != 1 || rows[0]["count"] != float64(0) {
				t.Fatalf("rows: got %#v, want count=0", rows)
			}
			requireCLIJSONColumns(t, rows, "count")
		})
	}
}

func TestCLIStreamingExecutionTDDTransformAssignmentsRemainSimultaneous(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	path := writeCLIStreamingTDDFile(t, dir, "rows.csv", "id,raw\n1,2024-01-02\n")

	rows := readCLIJSONMaps(t, runCLIQuery(t, bin,
		path+` | transform id = id + 1, snapshot = id + 10 | head 1 | select id, snapshot | json`,
	))
	if len(rows) != 1 {
		t.Fatalf("rows: got %#v, want one row", rows)
	}
	requireCLIJSONColumns(t, rows, "id", "snapshot")
	if rows[0]["id"] != float64(2) || rows[0]["snapshot"] != float64(11) {
		t.Fatalf("simultaneous transform row: got %#v, want id=2 snapshot=11", rows[0])
	}
}

func TestCLIStreamingExecutionTDDGlobStreamsInDeterministicOrderAndStopsEarly(t *testing.T) {
	bin := buildCLI(t)
	for _, input := range writeCLIStreamingTDDGlobDateInputs(t, bin) {
		t.Run(input.name, func(t *testing.T) {
			out := runCLIQuery(t, bin,
				input.path+` | transform y = year(raw) | head 1 | select id, y | json`,
			)
			requireCLIStreamingTDDOneIDYear(t, out)
		})
	}
}

func TestCLIStreamingExecutionTDDImplicitCSVGlobHeadSkipsLateSourceConversionError(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()

	var good strings.Builder
	good.WriteString("id,name\n")
	for i := 1; i <= 20480; i++ {
		fmt.Fprintf(&good, "%d,user-%d\n", i, i)
	}
	writeCLIStreamingTDDFile(t, dir, "part-001.csv", good.String())
	writeCLIStreamingTDDFile(t, dir, "part-002.csv", "id,name\nbad,Bob\n")

	pattern := filepath.Join(dir, "part-*.csv")
	rows := readCLIJSONMaps(t, runCLIQuery(t, bin, pattern+` | head 1 | select id | json`))
	if len(rows) != 1 || rows[0]["id"] != float64(1) {
		t.Fatalf("rows: got %#v, want one row with id=1", rows)
	}
	requireCLIJSONColumns(t, rows, "id")
}

func TestCLIStreamingExecutionTDDStdinStreamsAndStopsEarly(t *testing.T) {
	bin := buildCLI(t)
	cases := []struct {
		name  string
		query string
		stdin []byte
	}{
		{
			name:  "csv",
			query: `- with format=csv | transform y = year(raw) | head 1 | select id, y | json`,
			stdin: []byte("id,raw\n1,2024-01-02\n2,not-a-date\n"),
		},
		{
			name:  "json",
			query: `- with format=json | transform y = year(raw) | head 1 | select id, y | json`,
			stdin: []byte(`[{"id":1,"raw":"2024-01-02"},{"id":2,"raw":"not-a-date"}]`),
		},
		{
			name:  "jsonl",
			query: `- with format=jsonl | transform y = year(raw) | head 1 | select id, y | json`,
			stdin: []byte("{\"id\":1,\"raw\":\"2024-01-02\"}\n{\"id\":2,\"raw\":\"not-a-date\"}\n"),
		},
		{
			name:  "csv_gzip",
			query: `- with format=csv, compression=gzip | transform y = year(raw) | head 1 | select id, y | json`,
			stdin: gzipCLIBytes(t, "id,raw\n1,2024-01-02\n2,not-a-date\n"),
		},
		{
			name:  "jsonl_zstd",
			query: `- with format=jsonl, compression=zstd | transform y = year(raw) | head 1 | select id, y | json`,
			stdin: zstdCLIBytes(t, "{\"id\":1,\"raw\":\"2024-01-02\"}\n{\"id\":2,\"raw\":\"not-a-date\"}\n"),
		},
		{
			name:  "json_deflate",
			query: `- with format=json, compression=deflate | transform y = year(raw) | head 1 | select id, y | json`,
			stdin: deflateCLIBytes(t, `[{"id":1,"raw":"2024-01-02"},{"id":2,"raw":"not-a-date"}]`),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := runCLIQueryWithStdinBytes(t, bin, tc.query, tc.stdin)
			requireCLIStreamingTDDOneIDYear(t, out)
		})
	}
}

func TestCLIStreamingExecutionTDDJSONStdinValidatesMalformedArrayTailBeforeHead(t *testing.T) {
	bin := buildCLI(t)
	out := runCLIQueryWithStdinBytesExpectError(t, bin, `- with format=json, infer_rows=1 | head 1 | json`, []byte(`[{"id":1}, bad]`))
	msg := strings.ToLower(string(out))
	for _, want := range []string{"cannot parse json", "invalid character"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("malformed stdin JSON array error should mention %q, got:\n%s", want, out)
		}
	}
}

func TestCLIStreamingExecutionTDDOutputToPathUsesMaterializedHeadResultOnly(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	input := writeCLIStreamingTDDFile(t, dir, "dates.csv", "id,raw\n1,2024-01-02\n2,not-a-date\n")
	outPath := filepath.Join(dir, "out.csv")

	assertNoCLIStdout(t, runCLIQuery(t, bin,
		input+` | transform y = year(raw) | head 1 | select id, y | csv to `+outPath,
	))
	assertLoadedRowCount(t, outPath, 1)
}

func TestCLIStreamingExecutionTDDSourceWideErrorsStillWinBeforeRows(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()

	dupHeader := writeCLIStreamingTDDFile(t, dir, "dup.csv", "id,id\n1,2\n")
	out := runCLIQueryExpectError(t, bin, dupHeader+` | head 1 | json`)
	msg := strings.ToLower(string(out))
	for _, want := range []string{"duplicate", "id"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("duplicate header error should mention %q, got:\n%s", want, out)
		}
	}

	malformedJSON := writeCLIStreamingTDDFile(t, dir, "bad.json", `[{"id":1}, bad]`)
	out = runCLIQueryExpectError(t, bin, malformedJSON+` | head 1 | json`)
	msg = strings.ToLower(string(out))
	for _, want := range []string{"cannot parse json", "invalid character"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("malformed JSON array error should mention %q, got:\n%s", want, out)
		}
	}

	boundedMalformedJSON := writeCLIStreamingTDDFile(t, dir, "bounded-bad.json", `[{"id":1}, bad]`)
	out = runCLIQueryExpectError(t, bin, boundedMalformedJSON+` with infer_rows=1 | head 1 | json`)
	msg = strings.ToLower(string(out))
	for _, want := range []string{"cannot parse json", "invalid character"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("bounded malformed JSON array error should mention %q, got:\n%s", want, out)
		}
	}
}

func writeCLIStreamingTDDDateInputs(t *testing.T, bin string) []cliStreamingTDDInput {
	t.Helper()
	dir := t.TempDir()
	csv := writeCLIStreamingTDDFile(t, dir, "dates.csv", "id,raw\n1,2024-01-02\n2,not-a-date\n")
	jsonPath := writeCLIStreamingTDDFile(t, dir, "dates.json", `[{"id":1,"raw":"2024-01-02"},{"id":2,"raw":"not-a-date"}]`)
	jsonl := writeCLIStreamingTDDFile(t, dir, "dates.jsonl", "{\"id\":1,\"raw\":\"2024-01-02\"}\n{\"id\":2,\"raw\":\"not-a-date\"}\n")

	csvGzip := writeCLIStreamingTDDBytes(t, dir, "dates.csv.gz", gzipCLIBytes(t, "id,raw\n1,2024-01-02\n2,not-a-date\n"))
	jsonZstd := writeCLIStreamingTDDBytes(t, dir, "dates.json.zst", zstdCLIBytes(t, `[{"id":1,"raw":"2024-01-02"},{"id":2,"raw":"not-a-date"}]`))
	jsonlDeflate := writeCLIStreamingTDDBytes(t, dir, "dates.jsonl.deflate", deflateCLIBytes(t, "{\"id\":1,\"raw\":\"2024-01-02\"}\n{\"id\":2,\"raw\":\"not-a-date\"}\n"))

	avro := filepath.Join(dir, "dates.avro")
	runCLIQuery(t, bin, csv+` | avro to `+avro)
	parquet := filepath.Join(dir, "dates.parquet")
	runCLIQuery(t, bin, csv+` | parquet to `+parquet)

	return []cliStreamingTDDInput{
		{name: "csv", path: csv},
		{name: "json", path: jsonPath},
		{name: "jsonl", path: jsonl},
		{name: "csv_gzip", path: csvGzip},
		{name: "json_zstd", path: jsonZstd},
		{name: "jsonl_deflate", path: jsonlDeflate},
		{name: "avro", path: avro},
		{name: "parquet", path: parquet},
	}
}

func writeCLIStreamingTDDGlobDateInputs(t *testing.T, bin string) []cliStreamingTDDInput {
	t.Helper()
	dir := t.TempDir()

	csvDir := filepath.Join(dir, "csv")
	mkdirCLIStreamingTDD(t, csvDir)
	writeCLIStreamingTDDFile(t, csvDir, "part-001.csv", "id,raw\n1,2024-01-02\n")
	writeCLIStreamingTDDFile(t, csvDir, "part-002.csv", "id,raw\n2,not-a-date\n")

	jsonDir := filepath.Join(dir, "json")
	mkdirCLIStreamingTDD(t, jsonDir)
	writeCLIStreamingTDDFile(t, jsonDir, "part-001.json", `[{"id":1,"raw":"2024-01-02"}]`)
	writeCLIStreamingTDDFile(t, jsonDir, "part-002.json", `[{"id":2,"raw":"not-a-date"}]`)

	jsonlDir := filepath.Join(dir, "jsonl")
	mkdirCLIStreamingTDD(t, jsonlDir)
	writeCLIStreamingTDDFile(t, jsonlDir, "part-001.jsonl", "{\"id\":1,\"raw\":\"2024-01-02\"}\n")
	writeCLIStreamingTDDFile(t, jsonlDir, "part-002.jsonl", "{\"id\":2,\"raw\":\"not-a-date\"}\n")

	gzipDir := filepath.Join(dir, "csv-gzip")
	mkdirCLIStreamingTDD(t, gzipDir)
	writeCLIStreamingTDDBytes(t, gzipDir, "part-001.csv.gz", gzipCLIBytes(t, "id,raw\n1,2024-01-02\n"))
	writeCLIStreamingTDDBytes(t, gzipDir, "part-002.csv.gz", gzipCLIBytes(t, "id,raw\n2,not-a-date\n"))

	zstdDir := filepath.Join(dir, "jsonl-zstd")
	mkdirCLIStreamingTDD(t, zstdDir)
	writeCLIStreamingTDDBytes(t, zstdDir, "part-001.jsonl.zst", zstdCLIBytes(t, "{\"id\":1,\"raw\":\"2024-01-02\"}\n"))
	writeCLIStreamingTDDBytes(t, zstdDir, "part-002.jsonl.zst", zstdCLIBytes(t, "{\"id\":2,\"raw\":\"not-a-date\"}\n"))

	avroDir := filepath.Join(dir, "avro")
	mkdirCLIStreamingTDD(t, avroDir)
	writeCLIStreamingTDDGlobConvertedPart(t, bin, avroDir, "part-001.avro", "avro", "id,raw\n1,2024-01-02\n")
	writeCLIStreamingTDDGlobConvertedPart(t, bin, avroDir, "part-002.avro", "avro", "id,raw\n2,not-a-date\n")

	parquetDir := filepath.Join(dir, "parquet")
	mkdirCLIStreamingTDD(t, parquetDir)
	writeCLIStreamingTDDGlobConvertedPart(t, bin, parquetDir, "part-001.parquet", "parquet", "id,raw\n1,2024-01-02\n")
	writeCLIStreamingTDDGlobConvertedPart(t, bin, parquetDir, "part-002.parquet", "parquet", "id,raw\n2,not-a-date\n")

	return []cliStreamingTDDInput{
		{name: "csv", path: filepath.Join(csvDir, "part-*.csv")},
		{name: "json", path: filepath.Join(jsonDir, "part-*.json")},
		{name: "jsonl", path: filepath.Join(jsonlDir, "part-*.jsonl")},
		{name: "csv_gzip", path: filepath.Join(gzipDir, "part-*.csv.gz")},
		{name: "jsonl_zstd", path: filepath.Join(zstdDir, "part-*.jsonl.zst")},
		{name: "avro", path: filepath.Join(avroDir, "part-*.avro")},
		{name: "parquet", path: filepath.Join(parquetDir, "part-*.parquet")},
	}
}

func writeCLIStreamingTDDGlobConvertedPart(t *testing.T, bin, dir, name, format, csvContent string) {
	t.Helper()
	source := writeCLIStreamingTDDFile(t, dir, name+".csv", csvContent)
	out := filepath.Join(dir, name)
	runCLIQuery(t, bin, source+` | `+format+` to `+out)
}

func writeCLIStreamingTDDLateBadTextInputs(t *testing.T) []cliStreamingTDDInput {
	t.Helper()
	dir := t.TempDir()
	csv := writeCLIStreamingTDDFile(t, dir, "late-bad.csv", "id,name\n1,Alice\nbad,Bob\n")
	jsonPath := writeCLIStreamingTDDFile(t, dir, "late-bad.json", `[{"id":1,"name":"Alice"},{"id":"bad","name":"Bob"}]`)
	jsonl := writeCLIStreamingTDDFile(t, dir, "late-bad.jsonl", "{\"id\":1,\"name\":\"Alice\"}\n{\"id\":\"bad\",\"name\":\"Bob\"}\n")
	malformedJSONL := writeCLIStreamingTDDFile(t, dir, "late-malformed.jsonl", "{\"id\":1,\"name\":\"Alice\"}\nnot-json\n")
	csvGzip := writeCLIStreamingTDDBytes(t, dir, "late-bad.csv.gz", gzipCLIBytes(t, "id,name\n1,Alice\nbad,Bob\n"))
	jsonlZstd := writeCLIStreamingTDDBytes(t, dir, "late-bad.jsonl.zst", zstdCLIBytes(t, "{\"id\":1,\"name\":\"Alice\"}\n{\"id\":\"bad\",\"name\":\"Bob\"}\n"))

	return []cliStreamingTDDInput{
		{name: "csv", path: csv},
		{name: "json", path: jsonPath},
		{name: "jsonl", path: jsonl},
		{name: "jsonl_malformed_line", path: malformedJSONL},
		{name: "csv_gzip", path: csvGzip},
		{name: "jsonl_zstd", path: jsonlZstd},
	}
}

func writeCLIStreamingTDDFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	return writeCLIStreamingTDDBytes(t, dir, name, []byte(content))
}

func writeCLIStreamingTDDBytes(t *testing.T, dir, name string, content []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func mkdirCLIStreamingTDD(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
}

func runCLIQueryWithStdin(t *testing.T, bin, query, stdin string) []byte {
	t.Helper()
	return runCLIQueryWithStdinBytes(t, bin, query, []byte(stdin))
}

func runCLIQueryWithStdinBytes(t *testing.T, bin, query string, stdin []byte) []byte {
	t.Helper()
	cmd := exec.Command(bin, query)
	cmd.Stdin = bytes.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run cli %q with stdin: %v\n%s", query, err, out)
	}
	return out
}

func runCLIQueryWithStdinBytesExpectError(t *testing.T, bin, query string, stdin []byte) []byte {
	t.Helper()
	cmd := exec.Command(bin, query)
	cmd.Stdin = bytes.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected cli failure for %q with stdin\n%s", query, out)
	}
	return out
}

func requireCLIStreamingTDDOneIDYear(t *testing.T, out []byte) {
	t.Helper()
	rows := readCLIJSONMaps(t, out)
	if len(rows) != 1 {
		t.Fatalf("rows: got %#v, want one row", rows)
	}
	requireCLIJSONColumns(t, rows, "id", "y")
	if rows[0]["id"] != float64(1) || rows[0]["y"] != float64(2024) {
		t.Fatalf("row: got %#v, want id=1 y=2024", rows[0])
	}
}

func requireCLIStreamingTDDDateError(t *testing.T, out []byte) {
	t.Helper()
	msg := strings.ToLower(string(out))
	for _, want := range []string{"year", "not-a-date", "cannot parse"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("date error should mention %q, got:\n%s", want, out)
		}
	}
}
