package main

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/klauspost/compress/zstd"
	dq "github.com/razeghi71/dq"
	"github.com/razeghi71/dq/loader"
)

var (
	cliBuildOnce sync.Once
	cliBuildBin  string
	cliBuildDir  string
	cliBuildOut  []byte
	cliBuildErr  error
)

func TestMain(m *testing.M) {
	code := m.Run()
	if cliBuildDir != "" {
		_ = os.RemoveAll(cliBuildDir)
	}
	os.Exit(code)
}

func gzipCLIBytes(t *testing.T, content string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func zstdCLIBytes(t *testing.T, content string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw, err := zstd.NewWriter(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := zw.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func deflateCLIBytes(t *testing.T, content string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	if _, err := zw.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func buildCLI(t *testing.T) string {
	t.Helper()
	cliBuildOnce.Do(func() {
		cliBuildDir, cliBuildErr = os.MkdirTemp("", "dq-cli-test-*")
		if cliBuildErr != nil {
			return
		}
		cliBuildBin = filepath.Join(cliBuildDir, "dq")
		cliBuildOut, cliBuildErr = exec.Command("go", "build", "-o", cliBuildBin, ".").CombinedOutput()
	})
	if cliBuildErr != nil {
		t.Fatalf("build cli: %v\n%s", cliBuildErr, cliBuildOut)
	}
	return cliBuildBin
}

func runCLIQuery(t *testing.T, bin, query string) []byte {
	t.Helper()
	cmd := exec.Command(bin, query)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run cli %q: %v\n%s", query, err, out)
	}
	return out
}

func runCLIQueryExpectError(t *testing.T, bin, query string) []byte {
	t.Helper()
	cmd := exec.Command(bin, query)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected cli failure for %q, got output:\n%s", query, out)
	}
	return out
}

func writeCLIUsersCSV(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "users.csv")
	data := "name,age,city\nAlice,30,NY\nBob,25,LA\nCara,27,SF\nDan,41,NY\n"
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func assertNoCLIStdout(t *testing.T, out []byte) {
	t.Helper()
	if len(bytes.TrimSpace(out)) != 0 {
		t.Fatalf("expected no stdout when output destination is set, got:\n%s", out)
	}
}

func assertLoadedRowCount(t *testing.T, path string, want int) {
	t.Helper()
	tbl, err := loader.Load(path, loader.Options{})
	if err != nil {
		t.Fatalf("reload %s: %v", path, err)
	}
	if tbl.NumRows != want {
		t.Fatalf("%s row count: got %d, want %d", path, tbl.NumRows, want)
	}
}

func cliFlatUserInputFiles() []struct {
	name string
	path string
} {
	return []struct {
		name string
		path string
	}{
		{"csv", "../../testdata/users.csv"},
		{"json", "../../testdata/users.json"},
		{"jsonl", "../../testdata/users.jsonl"},
		{"avro", "../../testdata/users.avro"},
		{"parquet", "../../testdata/users.parquet"},
	}
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

func TestCLIGzipCSVDoubleExtension(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rows.csv.gz")
	if err := os.WriteFile(path, gzipCLIBytes(t, "name,level\nAlice,INFO\nBob,ERROR\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	bin := buildCLI(t)
	cmd := exec.Command(bin, path+` | filter { level == "ERROR" } | select name | csv`)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run cli: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "name\nBob" {
		t.Fatalf("expected Bob only, got:\n%s", got)
	}
}

func TestCLIGzipJSONLExplicitCompression(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.data")
	data := "{\"level\":\"INFO\",\"msg\":\"start\"}\n{\"level\":\"ERROR\",\"msg\":\"timeout\"}\n"
	if err := os.WriteFile(path, gzipCLIBytes(t, data), 0o644); err != nil {
		t.Fatal(err)
	}

	bin := buildCLI(t)
	cmd := exec.Command(bin, path+` with format=jsonl, compression=gzip | filter { level == "ERROR" } | select msg | jsonl`)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run cli: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); !strings.Contains(got, `"msg":"timeout"`) {
		t.Fatalf("expected timeout JSONL row, got:\n%s", got)
	}
}

func TestCLIGzipCSVStdinExplicitCompression(t *testing.T) {
	bin := buildCLI(t)
	cmd := exec.Command(bin, `- with format=csv, compression=gzip | filter { level == "ERROR" } | select name | csv`)
	cmd.Stdin = bytes.NewReader(gzipCLIBytes(t, "name,level\nAlice,INFO\nBob,ERROR\n"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run cli: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "name\nBob" {
		t.Fatalf("expected Bob only, got:\n%s", got)
	}
}

func TestCLIBadGzipStdinReportsGzipError(t *testing.T) {
	bin := buildCLI(t)
	cmd := exec.Command(bin, `- with format=csv, compression=gzip | count`)
	cmd.Stdin = strings.NewReader("name\nAlice\n")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected cli failure, got output:\n%s", out)
	}
	if !strings.Contains(strings.ToLower(string(out)), "gzip") {
		t.Fatalf("expected gzip error, got:\n%s", out)
	}
}

func TestCLIGzipCSVGlobWithFormat(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "part-001.csv.gz"), gzipCLIBytes(t, "id,name\n1,Alice\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "part-002.csv.gz"), gzipCLIBytes(t, "id,name\n2,Bob\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	bin := buildCLI(t)
	cmd := exec.Command(bin, filepath.Join(dir, "part-*.csv.gz")+` with format=csv | count | json`)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run cli: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), `"count": 2`) {
		t.Fatalf("expected count 2, got:\n%s", out)
	}
}

func TestCLIBadGzipInputReportsGzipError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.csv.gz")
	if err := os.WriteFile(path, []byte("name\nAlice\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	bin := buildCLI(t)
	cmd := exec.Command(bin, path+` | count`)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected cli failure, got output:\n%s", out)
	}
	if !strings.Contains(strings.ToLower(string(out)), "gzip") {
		t.Fatalf("expected gzip error, got:\n%s", out)
	}
}

func TestCLIZstdCSVDoubleExtension(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rows.csv.zst")
	if err := os.WriteFile(path, zstdCLIBytes(t, "name,level\nAlice,INFO\nBob,ERROR\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	bin := buildCLI(t)
	cmd := exec.Command(bin, path+` | filter { level == "ERROR" } | select name | csv`)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run cli: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "name\nBob" {
		t.Fatalf("expected Bob only, got:\n%s", got)
	}
}

func TestCLIZstdJSONLExplicitCompression(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.data")
	data := "{\"level\":\"INFO\",\"msg\":\"start\"}\n{\"level\":\"ERROR\",\"msg\":\"timeout\"}\n"
	if err := os.WriteFile(path, zstdCLIBytes(t, data), 0o644); err != nil {
		t.Fatal(err)
	}

	bin := buildCLI(t)
	cmd := exec.Command(bin, path+` with format=jsonl, compression=zstd | filter { level == "ERROR" } | select msg | jsonl`)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run cli: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); !strings.Contains(got, `"msg":"timeout"`) {
		t.Fatalf("expected timeout JSONL row, got:\n%s", got)
	}
}

func TestCLIZstdCSVStdinExplicitCompression(t *testing.T) {
	bin := buildCLI(t)
	cmd := exec.Command(bin, `- with format=csv, compression=zstd | filter { level == "ERROR" } | select name | csv`)
	cmd.Stdin = bytes.NewReader(zstdCLIBytes(t, "name,level\nAlice,INFO\nBob,ERROR\n"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run cli: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "name\nBob" {
		t.Fatalf("expected Bob only, got:\n%s", got)
	}
}

func TestCLIZstdCSVGlobWithFormat(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "part-001.csv.zst"), zstdCLIBytes(t, "id,name\n1,Alice\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "part-002.csv.zst"), zstdCLIBytes(t, "id,name\n2,Bob\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	bin := buildCLI(t)
	cmd := exec.Command(bin, filepath.Join(dir, "part-*.csv.zst")+` with format=csv | count | json`)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run cli: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), `"count": 2`) {
		t.Fatalf("expected count 2, got:\n%s", out)
	}
}

func TestCLIZstdJoinSource(t *testing.T) {
	dir := t.TempDir()
	usersPath := filepath.Join(dir, "users.csv")
	ordersPath := filepath.Join(dir, "orders.csv.zst")
	if err := os.WriteFile(usersPath, []byte("user_id,name\n1,Alice\n2,Bob\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ordersPath, zstdCLIBytes(t, "user_id,total\n1,10\n2,20\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	bin := buildCLI(t)
	cmd := exec.Command(bin, usersPath+` | join `+ordersPath+` on user_id | sort user_id | select name, total | csv`)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run cli: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "name,total\nAlice,10\nBob,20" {
		t.Fatalf("unexpected join output:\n%s", out)
	}
}

func TestCLIBadZstdInputReportsZstdError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.csv.zst")
	if err := os.WriteFile(path, []byte("name\nAlice\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	bin := buildCLI(t)
	cmd := exec.Command(bin, path+` | count`)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected cli failure, got output:\n%s", out)
	}
	lower := strings.ToLower(string(out))
	if !strings.Contains(lower, "zstd") && !strings.Contains(lower, "zstandard") {
		t.Fatalf("expected zstd error, got:\n%s", out)
	}
}

func TestCLIDeflateCSVDoubleExtension(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rows.csv.deflate")
	if err := os.WriteFile(path, deflateCLIBytes(t, "name,level\nAlice,INFO\nBob,ERROR\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	bin := buildCLI(t)
	cmd := exec.Command(bin, path+` | filter { level == "ERROR" } | select name | csv`)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run cli: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "name\nBob" {
		t.Fatalf("expected Bob only, got:\n%s", got)
	}
}

func TestCLIDeflateZlibJSONLDoubleExtension(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl.zlib")
	data := "{\"level\":\"INFO\",\"msg\":\"start\"}\n{\"level\":\"ERROR\",\"msg\":\"timeout\"}\n"
	if err := os.WriteFile(path, deflateCLIBytes(t, data), 0o644); err != nil {
		t.Fatal(err)
	}

	bin := buildCLI(t)
	cmd := exec.Command(bin, path+` | filter { level == "ERROR" } | select msg | jsonl`)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run cli: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); !strings.Contains(got, `"msg":"timeout"`) {
		t.Fatalf("expected timeout JSONL row, got:\n%s", got)
	}
}

func TestCLIDeflateJSONLExplicitCompression(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.data")
	data := "{\"level\":\"INFO\",\"msg\":\"start\"}\n{\"level\":\"ERROR\",\"msg\":\"timeout\"}\n"
	if err := os.WriteFile(path, deflateCLIBytes(t, data), 0o644); err != nil {
		t.Fatal(err)
	}

	bin := buildCLI(t)
	cmd := exec.Command(bin, path+` with format=jsonl, compression=deflate | filter { level == "ERROR" } | select msg | jsonl`)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run cli: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); !strings.Contains(got, `"msg":"timeout"`) {
		t.Fatalf("expected timeout JSONL row, got:\n%s", got)
	}
}

func TestCLIDeflateCSVStdinExplicitCompression(t *testing.T) {
	bin := buildCLI(t)
	cmd := exec.Command(bin, `- with format=csv, compression=deflate | filter { level == "ERROR" } | select name | csv`)
	cmd.Stdin = bytes.NewReader(deflateCLIBytes(t, "name,level\nAlice,INFO\nBob,ERROR\n"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run cli: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "name\nBob" {
		t.Fatalf("expected Bob only, got:\n%s", got)
	}
}

func TestCLIDeflateCSVGlobWithFormat(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "part-001.csv.deflate"), deflateCLIBytes(t, "id,name\n1,Alice\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "part-002.csv.deflate"), deflateCLIBytes(t, "id,name\n2,Bob\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	bin := buildCLI(t)
	cmd := exec.Command(bin, filepath.Join(dir, "part-*.csv.deflate")+` with format=csv | count | json`)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run cli: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), `"count": 2`) {
		t.Fatalf("expected count 2, got:\n%s", out)
	}
}

func TestCLIDeflateJoinSource(t *testing.T) {
	dir := t.TempDir()
	usersPath := filepath.Join(dir, "users.csv")
	ordersPath := filepath.Join(dir, "orders.csv.deflate")
	if err := os.WriteFile(usersPath, []byte("user_id,name\n1,Alice\n2,Bob\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ordersPath, deflateCLIBytes(t, "user_id,total\n1,10\n2,20\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	bin := buildCLI(t)
	cmd := exec.Command(bin, usersPath+` | join `+ordersPath+` on user_id | sort user_id | select name, total | csv`)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run cli: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "name,total\nAlice,10\nBob,20" {
		t.Fatalf("unexpected join output:\n%s", out)
	}
}

func TestCLIBadDeflateInputReportsDeflateError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.csv.deflate")
	if err := os.WriteFile(path, []byte("name\nAlice\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	bin := buildCLI(t)
	cmd := exec.Command(bin, path+` | count`)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected cli failure, got output:\n%s", out)
	}
	lower := strings.ToLower(string(out))
	if !strings.Contains(lower, "deflate") && !strings.Contains(lower, "zlib") {
		t.Fatalf("expected deflate/zlib error, got:\n%s", out)
	}
}

func TestCLIBadDeflateStdinReportsDeflateError(t *testing.T) {
	bin := buildCLI(t)
	cmd := exec.Command(bin, `- with format=csv, compression=deflate | count`)
	cmd.Stdin = strings.NewReader("name\nAlice\n")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected cli failure, got output:\n%s", out)
	}
	lower := strings.ToLower(string(out))
	if !strings.Contains(lower, "deflate") && !strings.Contains(lower, "zlib") {
		t.Fatalf("expected deflate/zlib error, got:\n%s", out)
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

func TestCLIAgentGuidePrintsREADME(t *testing.T) {
	bin := buildCLI(t)
	cmd := exec.Command(bin, "-agent-guide")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run cli: %v\n%s", err, out)
	}

	readme, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != string(readme) {
		t.Fatal("-agent-guide output is not README.md")
	}
}

func TestCLIHelpMentionsMCPSubcommand(t *testing.T) {
	bin := buildCLI(t)
	cmd := exec.Command(bin, "-h")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run cli: %v\n%s", err, out)
	}
	s := string(out)
	for _, want := range []string{"usage: dq '<query>'", "dq mcp", "subcommands:", "start a stdio MCP server"} {
		if !strings.Contains(s, want) {
			t.Fatalf("help output missing %q:\n%s", want, s)
		}
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

func TestCLIFunctionCallsWithoutTrailingCommaAllInputFormats(t *testing.T) {
	bin := buildCLI(t)

	for _, tc := range cliFlatUserInputFiles() {
		t.Run(tc.name, func(t *testing.T) {
			out := runCLIQuery(t, bin, tc.path+` | transform norm = upper(trim(name)), city2 = coalesce(city, "unknown") | filter { starts_with(norm, "A") } | select norm, city2 | json`)
			var rows []map[string]any
			if err := json.Unmarshal(out, &rows); err != nil {
				t.Fatalf("invalid JSON output:\n%s", out)
			}
			if len(rows) != 1 {
				t.Fatalf("expected one Alice row, got %d:\n%s", len(rows), out)
			}
			if rows[0]["norm"] != "ALICE" || rows[0]["city2"] != "NY" {
				t.Fatalf("unexpected transformed row: %#v", rows[0])
			}
		})
	}
}

func TestCLIFunctionCallsWithoutTrailingCommaAllOutputFormats(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	input := "../../testdata/users.csv"
	formats := []struct {
		name string
		ext  string
	}{
		{"table", ".txt"},
		{"csv", ".csv"},
		{"json", ".json"},
		{"jsonl", ".jsonl"},
		{"avro", ".avro"},
		{"parquet", ".parquet"},
	}

	for _, tc := range formats {
		t.Run(tc.name, func(t *testing.T) {
			outPath := filepath.Join(dir, "function-calls-"+tc.name+tc.ext)
			stdout := runCLIQuery(t, bin, input+` | transform norm = upper(trim(name)) | filter { starts_with(norm, "A") } | select norm | `+tc.name+` to `+outPath)
			assertNoCLIStdout(t, stdout)

			info, err := os.Stat(outPath)
			if err != nil {
				t.Fatalf("expected output file %s: %v", outPath, err)
			}
			if info.Size() == 0 {
				t.Fatalf("expected non-empty output file %s", outPath)
			}
			if tc.name == "table" {
				data, err := os.ReadFile(outPath)
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(string(data), "ALICE") {
					t.Fatalf("expected table output to contain ALICE, got:\n%s", data)
				}
				return
			}
			assertLoadedRowCount(t, outPath, 1)
		})
	}
}

func TestCLIFunctionCallTrailingCommaErrorsAllInputFormats(t *testing.T) {
	bin := buildCLI(t)

	for _, tc := range cliFlatUserInputFiles() {
		t.Run(tc.name, func(t *testing.T) {
			out := runCLIQueryExpectError(t, bin, tc.path+` | transform bad = upper(name,) | json`)
			s := string(out)
			if !strings.Contains(s, "parse error") || !strings.Contains(s, "expected expression after ','") {
				t.Fatalf("expected clear trailing-comma parse error, got:\n%s", out)
			}
		})
	}
}

func TestCLIFunctionCallTrailingCommaDoesNotCreateOutputFile(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	outPath := filepath.Join(dir, "bad.csv")

	out := runCLIQueryExpectError(t, bin, "../../testdata/users.csv | transform bad = upper(name,) | csv to "+outPath)
	if !strings.Contains(string(out), "expected expression after ','") {
		t.Fatalf("expected clear trailing-comma parse error, got:\n%s", out)
	}
	if _, err := os.Stat(outPath); !os.IsNotExist(err) {
		t.Fatalf("parse failure should not create %s, stat err=%v", outPath, err)
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

func TestCLIDescribeDefaultTable(t *testing.T) {
	bin := buildCLI(t)
	cmd := exec.Command(bin, "../../testdata/users.csv | describe")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run cli: %v\n%s", err, out)
	}
	s := string(out)
	for _, want := range []string{"column", "type", "row_count", "name", "string", "age", "int", "6"} {
		if !strings.Contains(s, want) {
			t.Fatalf("describe table output missing %q:\n%s", want, s)
		}
	}
	if !strings.Contains(s, " | ") {
		t.Fatalf("expected table separators, got:\n%s", s)
	}
}

func TestCLIDescribeJSONCanBeFiltered(t *testing.T) {
	bin := buildCLI(t)
	cmd := exec.Command(bin, `../../testdata/users.csv | describe | filter { type == "string" } | sort column | json`)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run cli: %v\n%s", err, out)
	}

	var rows []map[string]any
	if err := json.Unmarshal(out, &rows); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 string columns, got %d: %#v", len(rows), rows)
	}
	if rows[0]["column"] != "city" || rows[1]["column"] != "name" {
		t.Fatalf("unexpected string columns: %#v", rows)
	}
	for _, row := range rows {
		if row["type"] != "string" || row["row_count"] != float64(6) {
			t.Fatalf("unexpected describe row: %#v", row)
		}
	}
}

func TestCLIDescribeAfterFilterCSV(t *testing.T) {
	bin := buildCLI(t)
	cmd := exec.Command(bin, `../../testdata/users.csv | filter { city == "NY" } | describe | csv`)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run cli: %v\n%s", err, out)
	}
	got := strings.TrimSpace(string(out))
	wantLines := []string{
		"column,type,row_count",
		"name,string,3",
		"age,int,3",
		"city,string,3",
	}
	for _, want := range wantLines {
		if !strings.Contains(got, want) {
			t.Fatalf("CSV describe output missing %q:\n%s", want, got)
		}
	}
}

func TestCLIDescribeStdinJSONL(t *testing.T) {
	bin := buildCLI(t)
	cmd := exec.Command(bin, `- with format=csv | describe | jsonl`)
	cmd.Stdin = strings.NewReader("name,active\nAlice,true\nBob,false\n")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run cli: %v\n%s", err, out)
	}
	s := strings.TrimSpace(string(out))
	if !strings.Contains(s, `"column":"name"`) || !strings.Contains(s, `"type":"string"`) {
		t.Fatalf("expected name string metadata, got:\n%s", s)
	}
	if !strings.Contains(s, `"column":"active"`) || !strings.Contains(s, `"type":"bool"`) {
		t.Fatalf("expected active bool metadata, got:\n%s", s)
	}
	if !strings.Contains(s, `"row_count":2`) {
		t.Fatalf("expected row_count 2, got:\n%s", s)
	}
}

func TestCLIDescribeRejectsArguments(t *testing.T) {
	bin := buildCLI(t)
	cmd := exec.Command(bin, "../../testdata/users.csv | describe stats")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected parse error, got output:\n%s", out)
	}
	if !strings.Contains(string(out), "parse error") || !strings.Contains(string(out), "unexpected token") {
		t.Fatalf("expected unexpected-token parse error, got:\n%s", out)
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

func TestCLIOutputToFileAllFormats(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	input := writeCLIUsersCSV(t, dir)

	cases := []struct {
		format string
		ext    string
		reload bool
	}{
		{"table", ".txt", false},
		{"csv", ".csv", true},
		{"json", ".json", true},
		{"jsonl", ".jsonl", true},
		{"avro", ".avro", true},
		{"parquet", ".parquet", true},
	}
	for _, tc := range cases {
		t.Run(tc.format, func(t *testing.T) {
			outPath := filepath.Join(dir, "out-"+tc.format+tc.ext)
			stdout := runCLIQuery(t, bin, input+" | select name, age | head 2 | "+tc.format+" to "+outPath)
			assertNoCLIStdout(t, stdout)
			info, err := os.Stat(outPath)
			if err != nil {
				t.Fatalf("expected output file %s: %v", outPath, err)
			}
			if info.Size() == 0 {
				t.Fatalf("expected non-empty output file %s", outPath)
			}
			if tc.reload {
				assertLoadedRowCount(t, outPath, 2)
			}
		})
	}
}

func TestCLIOutputToDirectoryDefaultBasenameAllFormats(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	input := writeCLIUsersCSV(t, dir)

	cases := []struct {
		format string
		ext    string
		reload bool
	}{
		{"table", ".txt", false},
		{"csv", ".csv", true},
		{"json", ".json", true},
		{"jsonl", ".jsonl", true},
		{"avro", ".avro", true},
		{"parquet", ".parquet", true},
	}
	for _, tc := range cases {
		t.Run(tc.format, func(t *testing.T) {
			outDir := filepath.Join(dir, "dir-"+tc.format)
			stdout := runCLIQuery(t, bin, input+" | head 3 | "+tc.format+" to "+outDir+"/")
			assertNoCLIStdout(t, stdout)
			outPath := filepath.Join(outDir, "output"+tc.ext)
			if _, err := os.Stat(outPath); err != nil {
				t.Fatalf("expected default output file %s: %v", outPath, err)
			}
			if tc.reload {
				assertLoadedRowCount(t, outPath, 3)
			}
		})
	}
}

func TestCLIOutputAppendsMissingExtensionAndRejectsWrongExtension(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	input := writeCLIUsersCSV(t, dir)

	noExt := filepath.Join(dir, "users-export")
	stdout := runCLIQuery(t, bin, input+" | select name | csv to "+noExt)
	assertNoCLIStdout(t, stdout)
	assertLoadedRowCount(t, noExt+".csv", 4)

	out := runCLIQueryExpectError(t, bin, input+" | csv to "+filepath.Join(dir, "wrong.json"))
	if !strings.Contains(strings.ToLower(string(out)), "extension") {
		t.Fatalf("expected extension mismatch error, got:\n%s", out)
	}
}

func TestCLIOutputAcceptsUppercaseExtension(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	input := writeCLIUsersCSV(t, dir)

	csvPath := filepath.Join(dir, "up.CSV")
	stdout := runCLIQuery(t, bin, input+" | select name | CSV to "+csvPath)
	assertNoCLIStdout(t, stdout)
	assertLoadedRowCount(t, csvPath, 4)

	jsonPath := filepath.Join(dir, "data.JSON")
	stdout = runCLIQuery(t, bin, input+" | select name | json to "+jsonPath)
	assertNoCLIStdout(t, stdout)
	assertLoadedRowCount(t, jsonPath, 4)
}

func TestCLIOutputPathWithoutTrailingSlashIgnoresExistingDirectory(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	input := writeCLIUsersCSV(t, dir)
	outBase := filepath.Join(dir, "out")
	if err := os.Mkdir(outBase, 0o755); err != nil {
		t.Fatal(err)
	}

	stdout := runCLIQuery(t, bin, input+" | select name | csv to "+outBase)
	assertNoCLIStdout(t, stdout)
	assertLoadedRowCount(t, outBase+".csv", 4)
	if _, err := os.Stat(filepath.Join(outBase, "output.csv")); !os.IsNotExist(err) {
		t.Fatalf("expected no directory-style output file, stat err=%v", err)
	}
}

func TestCLIOutputCreatesParentDirectoriesAndRefusesExistingFile(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	input := writeCLIUsersCSV(t, dir)
	outPath := filepath.Join(dir, "new", "nested", "users.csv")

	stdout := runCLIQuery(t, bin, input+" | select name | csv to "+outPath)
	assertNoCLIStdout(t, stdout)
	assertLoadedRowCount(t, outPath, 4)

	out := runCLIQueryExpectError(t, bin, input+" | select name | csv to "+outPath)
	if !strings.Contains(strings.ToLower(string(out)), "exist") {
		t.Fatalf("expected existing-file error, got:\n%s", out)
	}
}

func TestCLIOutputOverwriteOptionOverwritesExistingFileAllFormats(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	input := writeCLIUsersCSV(t, dir)

	cases := []struct {
		format string
		ext    string
		reload bool
	}{
		{"table", ".txt", false},
		{"csv", ".csv", true},
		{"json", ".json", true},
		{"jsonl", ".jsonl", true},
		{"avro", ".avro", true},
		{"parquet", ".parquet", true},
	}
	for _, tc := range cases {
		t.Run(tc.format, func(t *testing.T) {
			outPath := filepath.Join(dir, "overwrite-"+tc.format+tc.ext)
			if err := os.WriteFile(outPath, []byte("old\n"), 0o644); err != nil {
				t.Fatal(err)
			}

			stdout := runCLIQuery(t, bin, input+" | select name | "+tc.format+" with overwrite=true to "+outPath)
			assertNoCLIStdout(t, stdout)
			if tc.reload {
				assertLoadedRowCount(t, outPath, 4)
			}
			data, err := os.ReadFile(outPath)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), "old") {
				t.Fatalf("expected overwrite=true to replace existing file, got:\n%s", data)
			}
		})
	}
}

func TestCLIOutputOverwriteOptionOverwritesExistingSplitParts(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	input := writeCLIUsersCSV(t, dir)
	outDir := filepath.Join(dir, "split")
	if err := os.Mkdir(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"output-1.csv", "output-2.csv"} {
		if err := os.WriteFile(filepath.Join(outDir, name), []byte("old\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	stdout := runCLIQuery(t, bin, input+" | csv with split_rows=2, overwrite=true to "+outDir+"/")
	assertNoCLIStdout(t, stdout)
	assertLoadedRowCount(t, filepath.Join(outDir, "output-1.csv"), 2)
	assertLoadedRowCount(t, filepath.Join(outDir, "output-2.csv"), 2)
	for _, name := range []string{"output-1.csv", "output-2.csv"} {
		data, err := os.ReadFile(filepath.Join(outDir, name))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "old") {
			t.Fatalf("expected overwrite=true to replace %s, got:\n%s", name, data)
		}
	}
}

func TestCLIOutputSplitRowsAllStructuredFormats(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	input := writeCLIUsersCSV(t, dir)

	cases := []struct {
		format string
		ext    string
	}{
		{"csv", ".csv"},
		{"json", ".json"},
		{"jsonl", ".jsonl"},
		{"avro", ".avro"},
		{"parquet", ".parquet"},
	}
	for _, tc := range cases {
		t.Run(tc.format, func(t *testing.T) {
			outDir := filepath.Join(dir, "split-"+tc.format)
			stdout := runCLIQuery(t, bin, input+" | "+tc.format+" with split_rows=2 to "+outDir+"/")
			assertNoCLIStdout(t, stdout)

			part1 := filepath.Join(outDir, "output-1"+tc.ext)
			part2 := filepath.Join(outDir, "output-2"+tc.ext)
			assertLoadedRowCount(t, part1, 2)
			assertLoadedRowCount(t, part2, 2)
			if _, err := os.Stat(filepath.Join(outDir, "output-3"+tc.ext)); !os.IsNotExist(err) {
				t.Fatalf("unexpected third split part, stat err=%v", err)
			}
		})
	}
}

func TestCLIOutputSplitRowsExplicitTemplate(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	input := writeCLIUsersCSV(t, dir)
	template := filepath.Join(dir, "chunks", "users-{n}.csv")

	stdout := runCLIQuery(t, bin, input+" | csv with split_rows=3 to "+template)
	assertNoCLIStdout(t, stdout)
	assertLoadedRowCount(t, filepath.Join(dir, "chunks", "users-1.csv"), 3)
	assertLoadedRowCount(t, filepath.Join(dir, "chunks", "users-2.csv"), 1)
}

func TestCLIOutputSplitRowsEmptyResultWritesOneValidPart(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	input := writeCLIUsersCSV(t, dir)
	outDir := filepath.Join(dir, "empty")

	stdout := runCLIQuery(t, bin, input+` | filter { age > 1000 } | csv with split_rows=2 to `+outDir+"/")
	assertNoCLIStdout(t, stdout)
	assertLoadedRowCount(t, filepath.Join(outDir, "output-1.csv"), 0)
}

func TestCLIOutputToPathParseErrors(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	input := writeCLIUsersCSV(t, dir)

	cases := []struct {
		name    string
		query   string
		wantMsg string
	}{
		{"missing_path", input + " | csv to", "path"},
		{"overwrite_without_to", input + " | csv with overwrite=true", "to"},
		{"split_without_to", input + " | csv with split_rows=2", "to"},
		{"unknown_option", input + " | csv with basename=report to " + filepath.Join(dir, "out") + "/", "unknown"},
		{"split_file_without_template", input + " | csv with split_rows=2 to " + filepath.Join(dir, "out.csv"), "{n}"},
		{"output_not_last", input + " | csv to " + filepath.Join(dir, "out.csv") + " | head 1", "last"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := runCLIQueryExpectError(t, bin, tc.query)
			if !strings.Contains(strings.ToLower(string(out)), strings.ToLower(tc.wantMsg)) {
				t.Fatalf("expected error containing %q, got:\n%s", tc.wantMsg, out)
			}
		})
	}
}
