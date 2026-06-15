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
