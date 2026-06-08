package main

import "testing"

func TestParseArgsStdinQuery(t *testing.T) {
	format, output, query, err := parseArgs([]string{"-f", "csv", "- | sorta city"})
	if err != nil {
		t.Fatal(err)
	}
	if format != "csv" || output != "" || query != "- | sorta city" {
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

func TestParseArgsDoubleDash(t *testing.T) {
	_, _, query, err := parseArgs([]string{"-f", "csv", "--", "- | head"})
	if err != nil {
		t.Fatal(err)
	}
	if query != "- | head" {
		t.Fatalf("got query=%q", query)
	}
}
