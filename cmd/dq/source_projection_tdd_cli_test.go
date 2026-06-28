package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/razeghi71/dq/loader"
)

func TestCLISourceProjectionTDDImmediateCSVSelectPreservesOutput(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	input := writeCLISourceProjectionTDDFile(t, dir, "wide.csv", "id,name,status,unused\n1,Alice,active,100\n2,Bob,paused,200\n")

	rows := readCLIJSONMaps(t, runCLIQuery(t, bin, input+` | select status, id | json`))
	if len(rows) != 2 {
		t.Fatalf("rows: got %#v, want 2 rows", rows)
	}
	requireCLIJSONColumns(t, rows, "status", "id")
	if rows[0]["status"] != "active" || rows[0]["id"] != float64(1) {
		t.Fatalf("first row: got %#v, want status=active id=1", rows[0])
	}
}

func TestCLISourceProjectionTDDDirectCSVSelectStillPreservesJoinSemantics(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	input := writeCLISourceProjectionTDDFile(t, dir, "users.csv", "id,name,city,unused\n1,Alice,NY,100\n2,Bob,LA,200\n3,Charlie,NY,300\n")
	orders := writeCLISourceProjectionTDDFile(t, dir, "orders.csv", "user_name,amount\nAlice,10\nCharlie,20\n")

	rows := readCLIJSONMaps(t, runCLIQuery(t, bin,
		input+` | select name, city | join `+orders+` on name == user_name | sort name | json`,
	))
	if len(rows) != 2 {
		t.Fatalf("rows: got %#v, want Alice and Charlie", rows)
	}
	requireCLIJSONColumns(t, rows, "name", "city", "amount")
	if rows[0]["name"] != "Alice" || rows[1]["name"] != "Charlie" {
		t.Fatalf("rows: got %#v, want sorted Alice/Charlie", rows)
	}
}

func TestCLISourceProjectionTDDCSVIneligiblePatternsFallbackAndPreserveOutput(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	input := writeCLISourceProjectionTDDFile(t, dir, "wide.csv", "id,status,age\n1,active,30\n2,paused,40\n")

	cases := []struct {
		name     string
		query    string
		wantCols []string
	}{
		{
			name:     "duplicate_select_keeps_select_for_output_names",
			query:    input + ` | select id, id | json`,
			wantCols: []string{"id", "id_2"},
		},
		{
			name:     "filter_before_select_can_be_satisfied_by_source",
			query:    input + ` | filter { age > 35 } | select id | json`,
			wantCols: []string{"id"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rows := readCLIJSONMaps(t, runCLIQuery(t, bin, tc.query))
			requireCLIJSONColumns(t, rows, tc.wantCols...)
		})
	}
}

func TestCLISourceProjectionTDDUnsupportedFilterBeforeSelectKeepsColumnBindings(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	input := writeCLISourceProjectionTDDFile(t, dir, "wrong.csv", "d,id,unused\n10,1,x\n20,2,y\n")

	rows := readCLIJSONMaps(t, runCLIQuery(t, bin,
		input+` | filter { d + 1 > 0 } | select id | json`,
	))
	if len(rows) != 2 {
		t.Fatalf("rows: got %#v, want 2 rows", rows)
	}
	requireCLIJSONColumns(t, rows, "id")
	if rows[0]["id"] != float64(1) || rows[1]["id"] != float64(2) {
		t.Fatalf("rows: got %#v, want id values 1 and 2", rows)
	}
}

func TestCLISourceProjectionTDDCSVGlobFallbackPreservesOutput(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	writeCLISourceProjectionTDDFile(t, dir, "part-001.csv", "id,status,unused\n1,active,100\n")
	writeCLISourceProjectionTDDFile(t, dir, "part-002.csv", "id,status,unused\n2,paused,200\n")
	pattern := filepath.Join(dir, "part-*.csv")

	rows := readCLIJSONMaps(t, runCLIQuery(t, bin,
		pattern+` with format=csv | select status, id | sort id | json`,
	))
	if len(rows) != 2 {
		t.Fatalf("rows: got %#v, want 2 rows", rows)
	}
	requireCLIJSONColumns(t, rows, "status", "id")
}

func TestCLISourceProjectionTDDSupportedFormatsPreserveOutput(t *testing.T) {
	bin := buildCLI(t)

	for _, input := range cliFlatUserInputFiles() {
		t.Run(input.name, func(t *testing.T) {
			rows := readCLIJSONMaps(t, runCLIQuery(t, bin,
				input.path+` | select name, age | sort name | json`,
			))
			if len(rows) == 0 {
				t.Fatalf("expected rows for %s", input.name)
			}
			requireCLIJSONColumns(t, rows, "name", "age")
		})
	}
}

func TestCLISourcePlanningTDDJSONLikePlanningErrorsWinBeforeLateBadRows(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	cases := []struct {
		name    string
		file    string
		content string
	}{
		{name: "json", file: "bad.json", content: `[{"id":1},{"id":"bad"}]`},
		{name: "jsonl", file: "bad.jsonl", content: "{\"id\":1}\n{\"id\":\"bad\"}\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := writeCLISourceProjectionTDDFile(t, dir, tc.file, tc.content)
			out := runCLIQueryExpectError(t, bin,
				input+` with infer_rows=1 | select missing | json`,
			)
			msg := strings.ToLower(string(out))
			for _, want := range []string{"select", "missing", "not found"} {
				if !strings.Contains(msg, want) {
					t.Fatalf("planning error should mention %q, got:\n%s", want, out)
				}
			}
			for _, notWant := range []string{"bad", "expected int"} {
				if strings.Contains(msg, notWant) {
					t.Fatalf("planning error should occur before late bad row, got:\n%s", out)
				}
			}
		})
	}
}

func TestCLISourcePlanningTDDJSONLikeLateBadRowsRemainRuntimeErrors(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	cases := []struct {
		name    string
		file    string
		content string
	}{
		{name: "json", file: "bad.json", content: `[{"id":1},{"id":"bad"}]`},
		{name: "jsonl", file: "bad.jsonl", content: "{\"id\":1}\n{\"id\":\"bad\"}\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := writeCLISourceProjectionTDDFile(t, dir, tc.file, tc.content)
			out := runCLIQueryExpectError(t, bin,
				input+` with infer_rows=1 | select id | count | json`,
			)
			msg := strings.ToLower(string(out))
			for _, want := range []string{"load error", "id", "int", "string"} {
				if !strings.Contains(msg, want) {
					t.Fatalf("runtime source error should mention %q, got:\n%s", want, out)
				}
			}
		})
	}
}

func TestCLISourcePlanningTDDJSONMalformedArraySyntaxFailsWholeFile(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	input := writeCLISourceProjectionTDDFile(t, dir, "malformed.json", `[{"id":1}, bad, {"id":2}]`)

	out := runCLIQueryExpectError(t, bin,
		input+` with max_bad_records=10 | select id | json`,
	)
	msg := strings.ToLower(string(out))
	for _, want := range []string{"load error", "cannot parse json", "invalid character"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("malformed JSON array syntax error should mention %q, got:\n%s", want, out)
		}
	}
	if strings.Contains(msg, "row 12") {
		t.Fatalf("malformed JSON array syntax should not be counted repeatedly, got:\n%s", out)
	}
}

func TestCLISourceProjectionTDDJSONLikeSkipsUnreferencedLateBadRows(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	cases := []struct {
		name    string
		file    string
		content string
	}{
		{name: "json", file: "bad-unused.json", content: `[{"id":1,"unused":10},{"id":2,"unused":"bad"}]`},
		{name: "jsonl", file: "bad-unused.jsonl", content: "{\"id\":1,\"unused\":10}\n{\"id\":2,\"unused\":\"bad\"}\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := writeCLISourceProjectionTDDFile(t, dir, tc.file, tc.content)
			rows := readCLIJSONMaps(t, runCLIQuery(t, bin,
				input+` with infer_rows=1 | select id | count | json`,
			))
			if len(rows) != 1 || rows[0]["count"] != float64(2) {
				t.Fatalf("count rows: got %#v, want count=2", rows)
			}
			requireCLIJSONColumns(t, rows, "count")
		})
	}
}

func TestCLISourceProjectionTDDJSONLikePredicateReadBadRowsRemainObservable(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	cases := []struct {
		name    string
		file    string
		content string
	}{
		{name: "json", file: "bad-predicate.json", content: `[{"id":1,"flag":1},{"id":2,"flag":"bad"}]`},
		{name: "jsonl", file: "bad-predicate.jsonl", content: "{\"id\":1,\"flag\":1}\n{\"id\":2,\"flag\":\"bad\"}\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := writeCLISourceProjectionTDDFile(t, dir, tc.file, tc.content)
			out := runCLIQueryExpectError(t, bin,
				input+` with infer_rows=1 | filter { flag == 1 } | select id | count | json`,
			)
			msg := strings.ToLower(string(out))
			for _, want := range []string{"load error", "flag", "int", "string"} {
				if !strings.Contains(msg, want) {
					t.Fatalf("predicate read error should mention %q, got:\n%s", want, out)
				}
			}
		})
	}
}

func TestCLISourceProjectionTDDCSVSkipsUnreferencedTypeBadRecords(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	input := writeCLISourceProjectionTDDFile(t, dir, "bad.csv", "id,unused\n1,10\n2,bad\n")

	rows := readCLIJSONMaps(t, runCLIQuery(t, bin,
		input+` with infer_rows=1 | select id | count | json`,
	))
	if len(rows) != 1 || rows[0]["count"] != float64(2) {
		t.Fatalf("count rows: got %#v, want count=2", rows)
	}
	requireCLIJSONColumns(t, rows, "count")
}

func TestCLISourceProjectionTDDCSVFilterSelectSkipsUnusedBadRecords(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	input := writeCLISourceProjectionTDDFile(t, dir, "bad.csv", "id,amount,unused\n1,10,100\n2,bad,bad\n")

	for _, query := range []string{
		input + ` with infer_rows=1 | filter { id == 1 } | select id | count | json`,
		input + ` with infer_rows=1 | select id | filter { id == 1 } | count | json`,
	} {
		rows := readCLIJSONMaps(t, runCLIQuery(t, bin, query))
		if len(rows) != 1 || rows[0]["count"] != float64(1) {
			t.Fatalf("count rows for %q: got %#v, want count=1", query, rows)
		}
		requireCLIJSONColumns(t, rows, "count")
	}
}

func TestCLISourceProjectionTDDCSVFilterReadBadRecordsRemainObservable(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	input := writeCLISourceProjectionTDDFile(t, dir, "bad.csv", "id,amount,flag\n1,10,1\n2,bad,bad\n")

	cases := []struct {
		name  string
		query string
		want  []string
	}{
		{
			name:  "predicate_column",
			query: input + ` with infer_rows=1 | filter { flag == 1 } | select id | count | json`,
			want:  []string{"flag", "int", "bad"},
		},
		{
			name:  "output_column_before_predicate_drop",
			query: input + ` with infer_rows=1 | filter { id == 1 } | select amount | count | json`,
			want:  []string{"amount", "int", "bad"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := runCLIQueryExpectError(t, bin, tc.query)
			msg := strings.ToLower(string(out))
			for _, want := range tc.want {
				if !strings.Contains(msg, want) {
					t.Fatalf("error should preserve read-column bad-record detail %q, got:\n%s", want, out)
				}
			}
		})
	}
}

func TestCLISourceProjectionTDDUnsupportedFilterBeforeSelectReadsAllBadRecords(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	input := writeCLISourceProjectionTDDFile(t, dir, "bad.csv", "id,unused\n1,10\n2,bad\n")

	out := runCLIQueryExpectError(t, bin,
		input+` with infer_rows=1 | filter { id + 1 > 0 } | select id | count | json`,
	)
	msg := strings.ToLower(string(out))
	for _, want := range []string{"unused", "int", "bad"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("unsupported filter should preserve read-all bad record %q, got:\n%s", want, out)
		}
	}
}

func TestCLISourceProjectionTDDRetainedFilterBlocksLaterPredicateReordering(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	input := writeCLISourceProjectionTDDFile(t, dir, "dates.csv", "id,name\n1,2024-01-01\n2,not-a-date\n")

	out := runCLIQueryExpectError(t, bin,
		input+` | filter { year(name) == 2024 } | filter { id == 1 } | select id | json`,
	)
	msg := strings.ToLower(string(out))
	for _, want := range []string{"year", "not-a-date", "cannot parse"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("retained filter runtime error should mention %q, got:\n%s", want, out)
		}
	}
}

func TestCLISourceProjectionTDDCSVReferencedTypeBadRecordsRemainObservable(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	input := writeCLISourceProjectionTDDFile(t, dir, "bad.csv", "id,unused\n1,10\n2,bad\n")

	out := runCLIQueryExpectError(t, bin,
		input+` with infer_rows=1 | select unused | count | json`,
	)
	msg := strings.ToLower(string(out))
	for _, want := range []string{"unused", "int", "bad"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error should preserve referenced-column bad-record detail %q, got:\n%s", want, out)
		}
	}
}

func TestCLISourceProjectionTDDPreparedBoundedSchemaIsSoundlyNullable(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	input := writeCLISourceProjectionTDDFile(t, dir, "ids.csv", "id\n1\n2\n")

	rows := readCLIDescribeRows(t, runCLIQuery(t, bin,
		input+` with infer_rows=1 | select id | transform y = id + 1 | describe | json`,
	))
	requireCLIDescribeSchema(t, rows, "id", "int", "int?", 2)
	requireCLIDescribeSchema(t, rows, "y", "int", "int?", 2)
}

func TestCLISourceProjectionTDDPreparedSchemaKeepsActualLoadedNullability(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	input := writeCLISourceProjectionTDDFile(t, dir, "ids.csv", "id,other\n1,a\n,b\n")

	rows := readCLIDescribeRows(t, runCLIQuery(t, bin,
		input+` with infer_rows=1 | select id | transform y = id + 1 | describe | json`,
	))
	requireCLIDescribeSchema(t, rows, "id", "int", "int?", 2)
	requireCLIDescribeSchema(t, rows, "y", "int", "int?", 2)
}

func TestCLISourceProjectionTDDSelectAfterFilterBoundedSchemasStaySound(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	cases := []struct {
		name    string
		file    string
		content string
	}{
		{name: "csv", file: "ids.csv", content: "id\n1\n2\n"},
		{name: "json", file: "ids.json", content: `[{"id":1},{"id":2}]`},
		{name: "jsonl", file: "ids.jsonl", content: "{\"id\":1}\n{\"id\":2}\n"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := writeCLISourceProjectionTDDFile(t, dir, tc.file, tc.content)
			rows := readCLIDescribeRows(t, runCLIQuery(t, bin,
				input+` with infer_rows=1 | filter { id > 0 } | select id | describe | json`,
			))
			requireCLIDescribeSchema(t, rows, "id", "int", "int?", 2)
			if len(rows) != 1 {
				t.Fatalf("describe rows: got %#v, want only id", rows)
			}
		})
	}
}

func TestCLISourceProjectionTDDPreparedSchemaPreservedInBinaryWriters(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	input := writeCLISourceProjectionTDDFile(t, dir, "ids.csv", "id\n1\n2\n")

	for _, format := range []string{"avro", "parquet"} {
		t.Run(format, func(t *testing.T) {
			outPath := filepath.Join(dir, "ids."+format)
			runCLIQuery(t, bin,
				input+` with infer_rows=1 | select id | transform y = id + 1 | `+format+` to `+outPath,
			)

			tbl, err := loader.Load(outPath, loader.Options{Format: format})
			if err != nil {
				t.Fatalf("reload %s output: %v", format, err)
			}
			if tbl.NumRows != 2 {
				t.Fatalf("row count: got %d, want 2", tbl.NumRows)
			}
			if strings.Join(tbl.Columns, ",") != "id,y" {
				t.Fatalf("columns: got %v, want [id y]", tbl.Columns)
			}
			if got := tbl.Col(tbl.ColIndex("id")).Schema().String(); got != "int?" {
				t.Fatalf("id schema: got %q, want int?", got)
			}
			if got := tbl.Col(tbl.ColIndex("y")).Schema().String(); got != "int?" {
				t.Fatalf("y schema: got %q, want int?", got)
			}
		})
	}
}

func TestCLISourceProjectionTDDRetainedOpsAcceptPreparedBoundedNullability(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	input := writeCLISourceProjectionTDDFile(t, dir, "ids.csv", "id\n1\nnull\n")

	t.Run("duplicate_select", func(t *testing.T) {
		rows := readCLIDescribeRows(t, runCLIQuery(t, bin,
			input+` with infer_rows=1 | select id, id | describe | json`,
		))
		requireCLIDescribeSchema(t, rows, "id", "int", "int?", 2)
		requireCLIDescribeSchema(t, rows, "id_2", "int", "int?", 2)
	})

	t.Run("rename_after_filter", func(t *testing.T) {
		rows := readCLIDescribeRows(t, runCLIQuery(t, bin,
			input+` with infer_rows=1 | filter { true } | rename id=x | describe | json`,
		))
		requireCLIDescribeSchema(t, rows, "x", "int", "int?", 2)
	})
}

func writeCLISourceProjectionTDDFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
