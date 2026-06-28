package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/razeghi71/dq/loader"
)

func writeRunCSV(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "users.csv")
	if err := os.WriteFile(path, []byte("name,age\nAlice,30\nBob,25\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunQueryStringWritesStdout(t *testing.T) {
	path := writeRunCSV(t)
	var stdout bytes.Buffer
	if err := runQueryString(path+" | filter { age > 25 } | select name | csv", &stdout); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(stdout.String()); got != "name\nAlice" {
		t.Fatalf("stdout: got %q", got)
	}
}

func TestRunQueryStringWritesOutputPath(t *testing.T) {
	path := writeRunCSV(t)
	out := filepath.Join(t.TempDir(), "out.json")
	var stdout bytes.Buffer
	if err := runQueryString(path+" | select name | json to "+out, &stdout); err != nil {
		t.Fatal(err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected no stdout for output path, got %q", stdout.String())
	}
	tbl, err := loader.Load(out, loader.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if tbl.NumRows != 2 {
		t.Fatalf("written output row count: got %d, want 2", tbl.NumRows)
	}
}

func TestRunQueryStringPreparedCSVSelectWritesStdout(t *testing.T) {
	path := writeRunCSV(t)

	var stdout bytes.Buffer
	if err := runQueryString(path+" | select age, name | json", &stdout); err != nil {
		t.Fatal(err)
	}
	out := stdout.String()
	for _, want := range []string{`"age": 30`, `"name": "Alice"`, `"age": 25`, `"name": "Bob"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout: got %q, missing %q", out, want)
		}
	}
}

func TestRunQueryStringMaterializedGlobWritesStdout(t *testing.T) {
	dir := t.TempDir()
	for name, content := range map[string]string{
		"part-1.csv": "name,age\nAlice,30\n",
		"part-2.csv": "name,age\nBob,25\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	var stdout bytes.Buffer
	query := filepath.Join(dir, "part-*.csv") + " with format=csv | select name | json"
	if err := runQueryString(query, &stdout); err != nil {
		t.Fatal(err)
	}
	out := stdout.String()
	for _, want := range []string{`"name": "Alice"`, `"name": "Bob"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout: got %q, missing %q", out, want)
		}
	}
}

func TestRunQueryStringPreparedPathLoadError(t *testing.T) {
	var stdout bytes.Buffer
	err := runQueryString(filepath.Join(t.TempDir(), "missing.csv")+" | select name | json", &stdout)
	if err == nil {
		t.Fatal("expected load error")
	}
	if !strings.Contains(err.Error(), "load error:") {
		t.Fatalf("error: got %v, want load error prefix", err)
	}
}

func TestRunQueryStringPreparedPathPlanningError(t *testing.T) {
	path := writeRunCSV(t)
	var stdout bytes.Buffer
	err := runQueryString(path+" | select missing | json", &stdout)
	if err == nil {
		t.Fatal("expected planning error")
	}
	for _, want := range []string{"error:", "missing", "not found"} {
		if !strings.Contains(strings.ToLower(err.Error()), want) {
			t.Fatalf("error %q missing %q", err, want)
		}
	}
}

func TestRunQueryStringSelectJoinReportsJoinSchemaAcquisitionBeforePrimaryRuntimeBadRecord(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "badsource.csv")
	if err := os.WriteFile(source, []byte("id,unused\n1,10\n2,bad\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	missingJoin := filepath.Join(dir, "missing.csv")

	var stdout bytes.Buffer
	err := runQueryString(source+" with infer_rows=1 | select id | join "+missingJoin+" on id | json", &stdout)
	if err == nil {
		t.Fatal("expected join schema acquisition error")
	}
	msg := strings.ToLower(err.Error())
	for _, want := range []string{"error:", "join", "missing.csv"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error %q missing %q", err, want)
		}
	}
	for _, notWant := range []string{"unused", "bad"} {
		if strings.Contains(msg, notWant) {
			t.Fatalf("join schema acquisition error should precede primary runtime bad record, got %q", err)
		}
	}
}

func TestRunQueryStringSelectJoinSkipsUnreferencedPrimaryTypeBadRecords(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "badsource.csv")
	if err := os.WriteFile(source, []byte("id,unused\n1,10\n2,bad\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	right := filepath.Join(dir, "right.csv")
	if err := os.WriteFile(right, []byte("id,label\n1,ok\n2,later\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	err := runQueryString(source+" with infer_rows=1 | select id | join "+right+" on id | sort id | json", &stdout)
	if err != nil {
		t.Fatalf("unreferenced primary bad record should be skipped: %v", err)
	}
	out := stdout.String()
	for _, want := range []string{`"id": 1`, `"label": "ok"`, `"id": 2`, `"label": "later"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout: got %q, missing %q", out, want)
		}
	}
}

func TestRunQueryStringSelectJoinValidatesReferencedPrimaryTypeBadRecords(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "badsource.csv")
	if err := os.WriteFile(source, []byte("id,unused\n1,10\n2,bad\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	right := filepath.Join(dir, "right.csv")
	if err := os.WriteFile(right, []byte("id,label\n1,ok\n2,later\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	err := runQueryString(source+" with infer_rows=1 | select id, unused | join "+right+" on id | json", &stdout)
	if err == nil {
		t.Fatal("expected referenced primary source runtime bad-record error")
	}
	msg := strings.ToLower(err.Error())
	for _, want := range []string{"error:", "load error", "unused", "int", "bad"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error %q missing %q", err, want)
		}
	}
}

func TestRunQueryUsesPreparedSourceForReplayableLiteralFiles(t *testing.T) {
	cases := []struct {
		name     string
		filename string
		opts     loader.Options
		want     bool
	}{
		{name: "csv", filename: "users.csv", want: true},
		{name: "json", filename: "users.json", want: true},
		{name: "jsonl", filename: "users.jsonl", want: true},
		{name: "avro", filename: "users.avro", want: true},
		{name: "parquet", filename: "users.parquet", want: true},
		{name: "extensionless_with_format", filename: "users.data", opts: loader.Options{Format: "jsonl"}, want: true},
		{name: "stdin", filename: "-", opts: loader.Options{Format: "csv"}, want: false},
		{name: "glob", filename: "users-*.csv", opts: loader.Options{Format: "csv"}, want: false},
		{name: "unknown", filename: "users.unknown", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := loader.CanPrepare(tc.filename, tc.opts); got != tc.want {
				t.Fatalf("loader.CanPrepare(%q, %+v): got %v, want %v", tc.filename, tc.opts, got, tc.want)
			}
		})
	}
}

func TestRunMCPQueryRejectsStdinSource(t *testing.T) {
	var stdout bytes.Buffer
	err := runMCPQuery("- with format=csv | count", &stdout)
	if err == nil || !strings.Contains(err.Error(), "stdin") {
		t.Fatalf("expected stdin rejection, got %v", err)
	}
}

func TestRunQueryStringErrorPrefixes(t *testing.T) {
	path := writeRunCSV(t)
	existing := filepath.Join(t.TempDir(), "existing.csv")
	if err := os.WriteFile(existing, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name  string
		query string
		want  string
	}{
		{"parse", path + " | csv | head 1", "parse error:"},
		{"load", filepath.Join(t.TempDir(), "missing.csv") + " | count", "load error:"},
		{"engine", path + " | transform bad = str_len(age)", "error:"},
		{"output", path + " | csv to " + existing, "output error:"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout bytes.Buffer
			err := runQueryString(tc.query, &stdout)
			if err == nil {
				t.Fatalf("expected error for %q", tc.query)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.want)
			}
		})
	}
}
