package parser

import (
	"strings"
	"testing"

	"github.com/razeghi71/dq/ast"
)

func assertQueryOutput(t *testing.T, q *ast.Query, want string) {
	t.Helper()
	if q.Output.Format != want {
		t.Errorf("Output.Format: got %q, want %q", q.Output.Format, want)
	}
}

func assertQueryOutputSpec(t *testing.T, q *ast.Query, wantFormat, wantPath string, wantSplitRows int, wantOverwrite bool) {
	t.Helper()
	if q.Output.Format != wantFormat {
		t.Errorf("Output.Format: got %q, want %q", q.Output.Format, wantFormat)
	}
	if q.Output.Path != wantPath {
		t.Errorf("Output.Path: got %q, want %q", q.Output.Path, wantPath)
	}
	if q.Output.Options.SplitRows != wantSplitRows {
		t.Errorf("Output.Options.SplitRows: got %d, want %d", q.Output.Options.SplitRows, wantSplitRows)
	}
	if q.Output.Options.Overwrite != wantOverwrite {
		t.Errorf("Output.Options.Overwrite: got %v, want %v", q.Output.Options.Overwrite, wantOverwrite)
	}
}

func assertOpCount(t *testing.T, q *ast.Query, want int) {
	t.Helper()
	if len(q.Ops) != want {
		t.Fatalf("expected %d ops, got %d", want, len(q.Ops))
	}
}

// --- Output format commands (017) ---

func TestParseOutputFormatDefaultTable(t *testing.T) {
	cases := []string{
		"users.csv",
		"users.csv | head 10",
		"users.csv | filter { age > 20 } | select name, age",
		"- with format=csv | count",
		"users.csv | join orders.csv on id",
	}
	for _, query := range cases {
		t.Run(query, func(t *testing.T) {
			q, err := Parse(query)
			if err != nil {
				t.Fatal(err)
			}
			assertQueryOutput(t, q, "")
		})
	}
}

func TestParseOutputFormatAllCommands(t *testing.T) {
	formats := ast.OutputFormatNames()
	for _, format := range formats {
		t.Run(format, func(t *testing.T) {
			q, err := Parse("users.csv | select name, age | " + format)
			if err != nil {
				t.Fatal(err)
			}
			assertOpCount(t, q, 1)
			assertQueryOutput(t, q, format)
		})
	}
}

func TestParseOutputFormatCaseInsensitive(t *testing.T) {
	cases := []struct {
		query string
		want  string
	}{
		{"users.csv | CSV", "csv"},
		{"users.csv | Json", "json"},
		{"users.csv | JSONL", "jsonl"},
		{"users.csv | TABLE", "table"},
		{"users.csv | Parquet", "parquet"},
	}
	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			q, err := Parse(tc.query)
			if err != nil {
				t.Fatal(err)
			}
			assertQueryOutput(t, q, tc.want)
		})
	}
}

func TestParseOutputFormatZeroOps(t *testing.T) {
	// Critical: source | csv must work without any pipeline ops before the format command.
	cases := []struct {
		query string
		want  string
	}{
		{"users.csv | csv", "csv"},
		{"users.csv | table", "table"},
		{"users.csv | json", "json"},
		{"- with format=csv | jsonl", "jsonl"},
	}
	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			q, err := Parse(tc.query)
			if err != nil {
				t.Fatal(err)
			}
			assertOpCount(t, q, 0)
			assertQueryOutput(t, q, tc.want)
		})
	}
}

func TestParseOutputFormatAfterFullPipeline(t *testing.T) {
	q, err := Parse(`sales.csv | filter { year(date) == 2024 } | group category, city | reduce total = sum(revenue) | remove grouped | sort -total | head 3 | csv`)
	if err != nil {
		t.Fatal(err)
	}
	if len(q.Ops) != 6 {
		t.Fatalf("expected 6 ops, got %d", len(q.Ops))
	}
	assertQueryOutput(t, q, "csv")
}

func TestParseOutputFormatAfterJoinWithLoadOptions(t *testing.T) {
	q, err := Parse(`users.csv | join orders.dat with format=csv on id | select name | parquet`)
	if err != nil {
		t.Fatal(err)
	}
	assertOpCount(t, q, 2)
	assertQueryOutput(t, q, "parquet")
}

func TestParseOutputFormatDoesNotAddEngineOp(t *testing.T) {
	q, err := Parse("users.csv | count | json")
	if err != nil {
		t.Fatal(err)
	}
	assertOpCount(t, q, 1)
	if _, ok := q.Ops[0].(*ast.CountOp); !ok {
		t.Fatalf("expected CountOp, got %T", q.Ops[0])
	}
	assertQueryOutput(t, q, "json")
}

func TestParseOutputFormatExplicitTableEquivalentSpec(t *testing.T) {
	// Implicit default and explicit | table are both valid; AST distinguishes them.
	qDefault, err := Parse("users.csv | count")
	if err != nil {
		t.Fatal(err)
	}
	qExplicit, err := Parse("users.csv | count | table")
	if err != nil {
		t.Fatal(err)
	}
	assertQueryOutput(t, qDefault, "")
	assertQueryOutput(t, qExplicit, "table")
	if len(qDefault.Ops) != len(qExplicit.Ops) {
		t.Fatalf("op count mismatch: default=%d explicit=%d", len(qDefault.Ops), len(qExplicit.Ops))
	}
}

func TestParseOutputFormatMidPipelineRejected(t *testing.T) {
	cases := []struct {
		name  string
		query string
	}{
		{"csv_before_head", "users.csv | csv | head 5"},
		{"csv_before_filter", "users.csv | csv | filter { age > 20 }"},
		{"json_before_count", "users.csv | json | count"},
		{"table_before_select", "users.csv | table | select name"},
		{"csv_before_another_format", "users.csv | csv | json"},
		{"format_after_ops_then_more", "users.csv | count | csv | head"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(tc.query)
			if err == nil {
				t.Fatalf("expected parse error for %q", tc.query)
			}
			msg := strings.ToLower(err.Error())
			if !strings.Contains(msg, "last") && !strings.Contains(msg, "output format") {
				t.Errorf("error should mention output format must be last, got: %v", err)
			}
		})
	}
}

func TestParseOutputFormatUnsupportedRejected(t *testing.T) {
	cases := []string{
		"users.csv | xlsx",
		"users.csv | tsv",
		"users.csv | html",
		"users.csv | count | txt",
	}
	for _, query := range cases {
		t.Run(query, func(t *testing.T) {
			_, err := Parse(query)
			if err == nil {
				t.Fatalf("expected parse error for %q", query)
			}
			msg := strings.ToLower(err.Error())
			if !strings.Contains(msg, "unsupported output format") && !strings.Contains(msg, "unknown operation") {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestParseOutputFormatDoesNotBreakExpressions(t *testing.T) {
	// Format command names remain valid column identifiers in expressions.
	q, err := Parse(`users.csv | filter { csv == "x" and json == "y" } | select name`)
	if err != nil {
		t.Fatal(err)
	}
	assertQueryOutput(t, q, "")
	assertOpCount(t, q, 2)
}

func TestParseOutputFormatDoesNotBreakSourceFilename(t *testing.T) {
	// A source file named "json" is still a filename; terminal format is separate.
	q, err := Parse("json | count | csv")
	if err != nil {
		t.Fatal(err)
	}
	if q.Source.Filename != "json" {
		t.Errorf("filename: got %q, want json", q.Source.Filename)
	}
	assertOpCount(t, q, 1)
	assertQueryOutput(t, q, "csv")
}

func TestParseOutputFormatOnlyOneAllowed(t *testing.T) {
	_, err := Parse("users.csv | csv | json")
	if err == nil {
		t.Fatal("expected error for chained output formats")
	}
}

func TestParseOutputFormatNothingAfterCommand(t *testing.T) {
	// Trailing tokens after a valid terminal format are rejected.
	_, err := Parse("users.csv | csv extra")
	if err == nil {
		t.Fatal("expected error for tokens after output format command")
	}
}

// --- Output destination and writer options (018-020) ---

func TestParseOutputDestinationToFileAndDirectory(t *testing.T) {
	cases := []struct {
		name   string
		query  string
		format string
		path   string
	}{
		{"csv_file", "users.csv | select name, age | csv to out/users.csv", "csv", "out/users.csv"},
		{"json_file_no_ops", "users.csv | json to out/users.json", "json", "out/users.json"},
		{"jsonl_directory", "users.csv | jsonl to out/", "jsonl", "out/"},
		{"avro_directory", "users.csv | avro to out/", "avro", "out/"},
		{"parquet_quoted_path", `users.csv | parquet to "my reports/users.parquet"`, "parquet", "my reports/users.parquet"},
		{"table_text_path", "users.csv | table to out/users.txt", "table", "out/users.txt"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q, err := Parse(tc.query)
			if err != nil {
				t.Fatal(err)
			}
			assertQueryOutputSpec(t, q, tc.format, tc.path, 0, false)
		})
	}
}

func TestParseOutputWithOptionsBeforeTo(t *testing.T) {
	cases := []struct {
		name      string
		query     string
		format    string
		path      string
		splitRows int
	}{
		{
			name:      "split_template",
			query:     "users.csv | csv with split_rows=2 to out/part-{n}.csv",
			format:    "csv",
			path:      "out/part-{n}.csv",
			splitRows: 2,
		},
		{
			name:      "split_directory_default_basename",
			query:     "users.csv | jsonl with split_rows=1000 to out/",
			format:    "jsonl",
			path:      "out/",
			splitRows: 1000,
		},
		{
			name:      "overwrite_single_file",
			query:     "users.csv | csv with overwrite=true to out/users.csv",
			format:    "csv",
			path:      "out/users.csv",
			splitRows: 0,
		},
		{
			name:      "overwrite_split_template",
			query:     "users.csv | parquet with split_rows=100, overwrite=true to out/part-{n}.parquet",
			format:    "parquet",
			path:      "out/part-{n}.parquet",
			splitRows: 100,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q, err := Parse(tc.query)
			if err != nil {
				t.Fatal(err)
			}
			assertQueryOutputSpec(t, q, tc.format, tc.path, tc.splitRows, strings.Contains(tc.query, "overwrite=true"))
		})
	}
}

func TestParseOutputWithOptionsAndLoadOptionsTogether(t *testing.T) {
	q, err := Parse(`events.dat with format=jsonl, compression=gzip | filter { level == "ERROR" } | join users.dat with format=csv on user_id | parquet with split_rows=500 to reports/errors-{n}.parquet`)
	if err != nil {
		t.Fatal(err)
	}
	assertOpCount(t, q, 2)
	assertQueryOutputSpec(t, q, "parquet", "reports/errors-{n}.parquet", 500, false)
}

func TestParseOutputDestinationDoesNotAddEngineOp(t *testing.T) {
	q, err := Parse("users.csv | count | json to out/count.json")
	if err != nil {
		t.Fatal(err)
	}
	assertOpCount(t, q, 1)
	if _, ok := q.Ops[0].(*ast.CountOp); !ok {
		t.Fatalf("expected CountOp, got %T", q.Ops[0])
	}
	assertQueryOutputSpec(t, q, "json", "out/count.json", 0, false)
}

func TestParseOutputDestinationAndOptionsErrors(t *testing.T) {
	cases := []struct {
		name    string
		query   string
		wantMsg string
	}{
		{"to_without_path", "users.csv | csv to", "path"},
		{"with_without_options", "users.csv | csv with to out.csv", "option"},
		{"with_after_to", "users.csv | csv to out.csv with split_rows=10", "last"},
		{"option_without_value", "users.csv | csv with split_rows to out/", "="},
		{"unknown_option", "users.csv | csv with foo=bar to out/", "unknown"},
		{"duplicate_split_rows", "users.csv | csv with split_rows=10, split_rows=20 to out/", "duplicate"},
		{"duplicate_overwrite", "users.csv | csv with overwrite=true, overwrite=false to out.csv", "duplicate"},
		{"overwrite_non_bool", "users.csv | csv with overwrite=yes to out.csv", "overwrite"},
		{"overwrite_without_to", "users.csv | csv with overwrite=true", "to"},
		{"split_without_to", "users.csv | csv with split_rows=10", "to"},
		{"split_zero", "users.csv | csv with split_rows=0 to out/", "split_rows"},
		{"split_negative", "users.csv | csv with split_rows=-1 to out/", "split_rows"},
		{"split_non_integer", "users.csv | csv with split_rows=\"10\" to out/", "split_rows"},
		{"split_bare_path_without_template", "users.csv | csv with split_rows=10 to out", "{n}"},
		{"split_file_without_template", "users.csv | csv with split_rows=10 to out/users.csv", "{n}"},
		{"output_not_last_after_to", "users.csv | csv to out.csv | head 1", "last"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(tc.query)
			if err == nil {
				t.Fatalf("expected parse error for %q", tc.query)
			}
			if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tc.wantMsg)) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.wantMsg)
			}
		})
	}
}
