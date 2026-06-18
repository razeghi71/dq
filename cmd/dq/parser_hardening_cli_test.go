package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIParserHardeningValidExpressionBoundariesAllInputFormats(t *testing.T) {
	bin := buildCLI(t)

	for _, tc := range cliFlatUserInputFiles() {
		t.Run(tc.name, func(t *testing.T) {
			out := runCLIQuery(t, bin, tc.path+` | transform norm = upper(name), plus = age + 1, tag = if(age > 30, "senior", "junior") | filter { plus > 25 and norm is not null } | select norm, plus, tag | json`)
			var rows []map[string]any
			if err := json.Unmarshal(out, &rows); err != nil {
				t.Fatalf("invalid JSON output:\n%s", out)
			}
			if len(rows) == 0 {
				t.Fatalf("expected at least one row for %s, got:\n%s", tc.name, out)
			}
			for _, row := range rows {
				if _, ok := row["norm"].(string); !ok {
					t.Fatalf("row missing string norm: %#v", row)
				}
				if _, ok := row["plus"].(float64); !ok {
					t.Fatalf("row missing numeric plus: %#v", row)
				}
				if tag, ok := row["tag"].(string); !ok || (tag != "senior" && tag != "junior") {
					t.Fatalf("row has unexpected tag: %#v", row)
				}
			}
		})
	}
}

func TestCLIParserHardeningRejectsSwallowedLexerErrorsAllInputFormats(t *testing.T) {
	bin := buildCLI(t)

	for _, tc := range cliFlatUserInputFiles() {
		t.Run(tc.name, func(t *testing.T) {
			out := runCLIQueryExpectError(t, bin, tc.path+` | transform out = age % 2 | json`)
			assertCLIParseErrorContains(t, out, "lex error", "%")
		})
	}
}

func TestCLIParserHardeningCompressedTextInputs(t *testing.T) {
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

	for _, tc := range inputs {
		if err := os.WriteFile(tc.path, tc.data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	for _, tc := range inputs {
		t.Run(tc.name+"_valid", func(t *testing.T) {
			out := runCLIQuery(t, bin, tc.path+` | transform label = upper(name), plus = age + 1 | filter { plus > 26 } | select label, plus | json`)
			var rows []map[string]any
			if err := json.Unmarshal(out, &rows); err != nil {
				t.Fatalf("invalid JSON output:\n%s", out)
			}
			if len(rows) != 1 || rows[0]["label"] != "ALICE" {
				t.Fatalf("expected one ALICE row, got:\n%s", out)
			}
		})

		t.Run(tc.name+"_invalid", func(t *testing.T) {
			out := runCLIQueryExpectError(t, bin, tc.path+` | transform out = age @ 2 | json`)
			assertCLIParseErrorContains(t, out, "lex error", "@")
		})
	}
}

func TestCLIParserHardeningRejectsMalformedExpressionBeforeOutputDestination(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	outPath := filepath.Join(dir, "bad.csv")

	out := runCLIQueryExpectError(t, bin, "../../testdata/users.csv | transform out = age % 2 | csv to "+outPath)
	assertCLIParseErrorContains(t, out, "lex error", "%")
	if _, err := os.Stat(outPath); !os.IsNotExist(err) {
		t.Fatalf("parse failure should not create %s, stat err=%v", outPath, err)
	}
}

func TestCLIParserHardeningRejectsMalformedExpressionBeforeAllOutputFormats(t *testing.T) {
	bin := buildCLI(t)
	formats := []string{"table", "csv", "json", "jsonl", "avro", "parquet"}

	for _, format := range formats {
		t.Run(format, func(t *testing.T) {
			out := runCLIQueryExpectError(t, bin, "../../testdata/users.csv | transform out = age % 2 | "+format)
			assertCLIParseErrorContains(t, out, "lex error", "%")
		})
	}
}

func TestCLIParserHardeningRejectsMalformedExpressionFromStdin(t *testing.T) {
	bin := buildCLI(t)
	cmd := exec.Command(bin, `- with format=csv | transform out = age % 2 | json`)
	cmd.Stdin = strings.NewReader("name,age,city\nAlice,30,NY\nBob,25,LA\n")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected parse failure, got output:\n%s", out)
	}
	assertCLIParseErrorContains(t, out, "lex error", "%")
}

func TestCLIParserHardeningDelimitedLexerErrors(t *testing.T) {
	bin := buildCLI(t)
	cases := []struct {
		name  string
		query string
		wants []string
	}{
		{
			name:  "unterminated_quoted_source_path",
			query: `"unterminated.csv | count`,
			wants: []string{"unterminated string"},
		},
		{
			name:  "unterminated_join_path",
			query: `../../testdata/users.csv | join "orders.csv on name`,
			wants: []string{"unterminated string"},
		},
		{
			name:  "unterminated_output_path",
			query: `../../testdata/users.csv | csv to "out.csv`,
			wants: []string{"unterminated string"},
		},
		{
			name:  "unterminated_string_literal",
			query: `../../testdata/users.csv | filter { name == "Alice } | json`,
			wants: []string{"lex error", "unterminated string"},
		},
		{
			name:  "unterminated_backtick_column",
			query: "../../testdata/users.csv | select `first name",
			wants: []string{"lex error", "unterminated backtick"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := runCLIQueryExpectError(t, bin, tc.query)
			assertCLIParseErrorContains(t, out, tc.wants...)
		})
	}
}

func assertCLIParseErrorContains(t *testing.T, out []byte, wants ...string) {
	t.Helper()
	msg := string(out)
	if !strings.Contains(msg, "parse error") {
		t.Fatalf("expected parse error, got:\n%s", out)
	}
	for _, want := range wants {
		if !strings.Contains(msg, want) {
			t.Fatalf("expected parse error to contain %q, got:\n%s", want, out)
		}
	}
}
