package main

import (
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

func TestParseArgsRejectsFormatFlag(t *testing.T) {
	cases := []string{
		"-f csv users.csv | count",
		"-format csv users.csv | count",
		"-f csv",
		"-f csv -- - | head",
	}
	for _, args := range cases {
		t.Run(args, func(t *testing.T) {
			_, _, err := parseArgs(strings.Fields(args))
			if err == nil {
				t.Fatalf("expected error for %q", args)
			}
			if !strings.Contains(err.Error(), "-f") && !strings.Contains(err.Error(), "format") {
				t.Errorf("error should mention removed flag: %v", err)
			}
		})
	}
}

func TestParseArgsFileQuery(t *testing.T) {
	_, query, err := parseArgs([]string{"users.csv | head 10"})
	if err != nil {
		t.Fatal(err)
	}
	if query != "users.csv | head 10" {
		t.Fatalf("got query=%q", query)
	}
}

func TestParseArgsQueryWithLoadOptions(t *testing.T) {
	_, query, err := parseArgs([]string{"- with format=csv | count"})
	if err != nil {
		t.Fatal(err)
	}
	if query != "- with format=csv | count" {
		t.Fatalf("got query=%q", query)
	}
}

func TestParseArgsNoQuery(t *testing.T) {
	_, query, err := parseArgs(nil)
	if err != nil {
		t.Fatal(err)
	}
	if query != "" {
		t.Fatalf("got query=%q", query)
	}
}

func TestParseArgsAgentGuide(t *testing.T) {
	_, query, err := parseArgs([]string{"-agent-guide"})
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
	_, query, err := parseArgs([]string{"--", "- with format=csv | head"})
	if err != nil {
		t.Fatal(err)
	}
	if query != "- with format=csv | head" {
		t.Fatalf("got query=%q", query)
	}
}

func TestParseArgsOutputFlag(t *testing.T) {
	output, query, err := parseArgs([]string{"-o", "csv", "users.csv | count"})
	if err != nil {
		t.Fatal(err)
	}
	if output != "csv" || query != "users.csv | count" {
		t.Fatalf("got output=%q query=%q", output, query)
	}
}
