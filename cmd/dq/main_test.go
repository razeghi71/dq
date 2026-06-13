package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	dq "github.com/razeghi71/dq"
)

func buildCLI(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "dq")
	out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput()
	if err != nil {
		t.Fatalf("build cli: %v\n%s", err, out)
	}
	return bin
}

func TestCLIStdinWithFormat(t *testing.T) {
	bin := buildCLI(t)
	cmd := exec.Command(bin, "- with format=csv | count")
	cmd.Stdin = strings.NewReader("name\nAlice\nBob\n")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run cli: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "2") {
		t.Fatalf("expected count 2, got:\n%s", out)
	}
}

func TestCLIStdinWithOutputFormatJSONL(t *testing.T) {
	bin := buildCLI(t)
	cmd := exec.Command(bin, "- with format=csv | select name | head 1 | jsonl")
	cmd.Stdin = strings.NewReader("name,age\nAlice,30\nBob,25\n")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run cli: %v\n%s", err, out)
	}
	s := strings.TrimSpace(string(out))
	if !strings.HasPrefix(s, "{") || !strings.Contains(s, `"name"`) || !strings.Contains(s, "Alice") {
		t.Fatalf("expected JSONL object output, got:\n%s", s)
	}
	if strings.Contains(s, " | ") {
		t.Fatalf("expected JSONL not table, got:\n%s", s)
	}
}

func TestCLIFileWithHeaderFalse(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rows.csv")
	if err := os.WriteFile(path, []byte("1,2\n3,4\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	bin := buildCLI(t)
	cmd := exec.Command(bin, path+" with header=false | count")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run cli: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "2") {
		t.Fatalf("expected count 2, got:\n%s", out)
	}
}

func TestParseArgsFileQuery(t *testing.T) {
	query, err := parseArgs([]string{"users.csv | head 10"})
	if err != nil {
		t.Fatal(err)
	}
	if query != "users.csv | head 10" {
		t.Fatalf("got query=%q", query)
	}
}

func TestParseArgsQueryWithLoadOptions(t *testing.T) {
	query, err := parseArgs([]string{"- with format=csv | count"})
	if err != nil {
		t.Fatal(err)
	}
	if query != "- with format=csv | count" {
		t.Fatalf("got query=%q", query)
	}
}

func TestParseArgsNoQuery(t *testing.T) {
	query, err := parseArgs(nil)
	if err != nil {
		t.Fatal(err)
	}
	if query != "" {
		t.Fatalf("got query=%q", query)
	}
}

func TestParseArgsAgentGuide(t *testing.T) {
	query, err := parseArgs([]string{"-agent-guide"})
	if err != errGuide {
		t.Fatalf("got err=%v, want errGuide", err)
	}
	if query != "" {
		t.Fatalf("got query=%q", query)
	}
}

func TestAgentGuideMatchesREADME(t *testing.T) {
	readme, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	if dq.AgentGuide != string(readme) {
		t.Fatal("embedded guide is out of sync with README.md")
	}
}

func TestParseArgsDoubleDash(t *testing.T) {
	query, err := parseArgs([]string{"--", "- with format=csv | head"})
	if err != nil {
		t.Fatal(err)
	}
	if query != "- with format=csv | head" {
		t.Fatalf("got query=%q", query)
	}
}

func TestCLIOutputFormatDefaultTable(t *testing.T) {
	bin := buildCLI(t)
	cmd := exec.Command(bin, "../../testdata/users.csv | select name, age | head 2")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run cli: %v\n%s", err, out)
	}
	s := string(out)
	if !strings.Contains(s, "name") || !strings.Contains(s, "age") {
		t.Fatalf("expected pretty table headers, got:\n%s", s)
	}
	if !strings.Contains(s, " | ") {
		t.Fatalf("expected table column separator, got:\n%s", s)
	}
	if strings.HasPrefix(strings.TrimSpace(s), "name,age") {
		t.Fatalf("expected table not CSV, got:\n%s", s)
	}
}

func TestCLIOutputFormatExplicitTable(t *testing.T) {
	bin := buildCLI(t)
	cmd := exec.Command(bin, "../../testdata/users.csv | select name, age | head 2 | table")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run cli: %v\n%s", err, out)
	}
	s := string(out)
	if !strings.Contains(s, " | ") {
		t.Fatalf("expected pretty table output, got:\n%s", s)
	}
}

func TestCLIOutputFormatCSV(t *testing.T) {
	bin := buildCLI(t)
	cmd := exec.Command(bin, "../../testdata/users.csv | select name, age | head 2 | csv")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run cli: %v\n%s", err, out)
	}
	s := strings.TrimSpace(string(out))
	if !strings.HasPrefix(s, "name,age") {
		t.Fatalf("expected CSV header, got:\n%s", s)
	}
	if strings.Contains(s, " | ") {
		t.Fatalf("expected CSV not table, got:\n%s", s)
	}
}

func TestCLIOutputFormatJSON(t *testing.T) {
	bin := buildCLI(t)
	cmd := exec.Command(bin, "../../testdata/users.csv | select name, age | head 1 | json")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run cli: %v\n%s", err, out)
	}
	s := strings.TrimSpace(string(out))
	if !strings.HasPrefix(s, "[") || !strings.Contains(s, `"name"`) {
		t.Fatalf("expected JSON array output, got:\n%s", s)
	}
}

func TestCLIListConstructionJSONFromStdin(t *testing.T) {
	bin := buildCLI(t)
	cmd := exec.Command(bin, `- with format=csv | transform tags = list("user", city, null), pair = list(name, upper(city)), empty = list() | select name, tags, pair, empty | json`)
	cmd.Stdin = strings.NewReader("name,city\nAlice,NY\nBob,LA\n")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run cli: %v\n%s", err, out)
	}

	var rows []map[string]any
	if err := json.Unmarshal(out, &rows); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	tags, ok := rows[0]["tags"].([]any)
	if !ok {
		t.Fatalf("tags: expected array, got %T", rows[0]["tags"])
	}
	if len(tags) != 3 || tags[0] != "user" || tags[1] != "NY" || tags[2] != nil {
		t.Fatalf("unexpected tags: %#v", tags)
	}
	pair, ok := rows[0]["pair"].([]any)
	if !ok {
		t.Fatalf("pair: expected array, got %T", rows[0]["pair"])
	}
	if len(pair) != 2 || pair[0] != "Alice" || pair[1] != "NY" {
		t.Fatalf("unexpected pair: %#v", pair)
	}
	empty, ok := rows[0]["empty"].([]any)
	if !ok || len(empty) != 0 {
		t.Fatalf("empty: expected empty array, got %#v", rows[0]["empty"])
	}
}

func TestCLIListConstructionWithListContains(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rows.csv")
	if err := os.WriteFile(path, []byte("name,city\nAlice,NY\nBob,LA\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	bin := buildCLI(t)
	cmd := exec.Command(bin, path+` | filter { list_contains(list(city, lower(city)), "la") } | select name | csv`)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run cli: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "name\nBob" {
		t.Fatalf("expected Bob only, got:\n%s", got)
	}
}

func TestCLIOutputFormatJSONL(t *testing.T) {
	bin := buildCLI(t)
	cmd := exec.Command(bin, "../../testdata/users.csv | select name | head 1 | jsonl")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run cli: %v\n%s", err, out)
	}
	s := strings.TrimSpace(string(out))
	if !strings.HasPrefix(s, "{") || !strings.Contains(s, `"name"`) {
		t.Fatalf("expected JSONL object output, got:\n%s", s)
	}
}

func TestCLIOutputFormatZeroOpsCSV(t *testing.T) {
	bin := buildCLI(t)
	cmd := exec.Command(bin, "../../testdata/users.csv | csv")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run cli: %v\n%s", err, out)
	}
	if !strings.HasPrefix(strings.TrimSpace(string(out)), "name,age,city") {
		t.Fatalf("expected CSV with header row, got:\n%s", out)
	}
}

func TestCLIOutputFormatAfterCount(t *testing.T) {
	bin := buildCLI(t)
	cmd := exec.Command(bin, "../../testdata/users.csv | count | json")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run cli: %v\n%s", err, out)
	}
	s := strings.TrimSpace(string(out))
	if !strings.Contains(s, "[") {
		t.Fatalf("expected JSON count output, got:\n%s", s)
	}
}

func TestCLIOutputFormatAvro(t *testing.T) {
	bin := buildCLI(t)
	cmd := exec.Command(bin, "../../testdata/users.csv | select name, age | head 2 | avro")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run cli: %v\n%s", err, out)
	}
	if len(out) == 0 {
		t.Fatal("expected non-empty Avro output")
	}
	if !bytes.HasPrefix(out, []byte("Obj\x01")) {
		t.Fatalf("expected Avro OCF header, got %d bytes", len(out))
	}
}

func TestCLIOutputFormatParquet(t *testing.T) {
	bin := buildCLI(t)
	cmd := exec.Command(bin, "../../testdata/users.csv | select name, age | head 2 | parquet")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run cli: %v\n%s", err, out)
	}
	if len(out) == 0 {
		t.Fatal("expected non-empty Parquet output")
	}
	if !bytes.HasPrefix(out, []byte("PAR1")) {
		t.Fatalf("expected Parquet magic bytes, got %d bytes", len(out))
	}
}

func TestCLIParseErrorOutputFormatNotLast(t *testing.T) {
	bin := buildCLI(t)
	cmd := exec.Command(bin, "../../testdata/users.csv | csv | head 2")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected parse error, got output:\n%s", out)
	}
	if !strings.Contains(string(out), "parse error") {
		t.Fatalf("expected parse error message, got:\n%s", out)
	}
}

func TestCLILengthFunctionsSmoke(t *testing.T) {
	bin := buildCLI(t)
	cmd := exec.Command(bin, `../../testdata/nested.json | transform n = list_len(orders) | filter { n > 1 } | count | csv`)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run cli: %v\n%s", err, out)
	}
	s := strings.TrimSpace(string(out))
	if s != "count\n1" {
		t.Fatalf("expected count\\n1, got:\n%s", s)
	}
}
