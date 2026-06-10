package main

import (
	"os"
	"testing"

	dq "github.com/razeghi71/dq"
)

func TestParseArgsStdinQuery(t *testing.T) {
	format, output, query, err := parseArgs([]string{"-f", "csv", "- | sort city"})
	if err != nil {
		t.Fatal(err)
	}
	if format != "csv" || output != "" || query != "- | sort city" {
		t.Fatalf("got format=%q output=%q query=%q", format, output, query)
	}
}

func TestParseArgsFileQuery(t *testing.T) {
	_, _, query, err := parseArgs([]string{"users.csv | head 10"})
	if err != nil {
		t.Fatal(err)
	}
	if query != "users.csv | head 10" {
		t.Fatalf("got query=%q", query)
	}
}

func TestParseArgsNoQuery(t *testing.T) {
	format, _, query, err := parseArgs([]string{"-f", "csv"})
	if err != nil {
		t.Fatal(err)
	}
	if format != "csv" || query != "" {
		t.Fatalf("got format=%q query=%q", format, query)
	}
}

func TestParseArgsAgentGuide(t *testing.T) {
	_, _, query, err := parseArgs([]string{"-agent-guide"})
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
	_, _, query, err := parseArgs([]string{"-f", "csv", "--", "- | head"})
	if err != nil {
		t.Fatal(err)
	}
	if query != "- | head" {
		t.Fatalf("got query=%q", query)
	}
}
