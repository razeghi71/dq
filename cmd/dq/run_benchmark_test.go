package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func BenchmarkRunQueryStringLiteralCSV(b *testing.B) {
	path := writeRunBenchmarkCSV(b, 16, 1000)
	cases := []struct {
		name  string
		query string
	}{
		{
			name:  "projection_eligible",
			query: path + " | select c00, c15 | count | json",
		},
		{
			name:  "filter_before_select",
			query: path + " | filter { c00 > 100 } | select c00 | count | json",
		},
		{
			name:  "unsupported_filter_read_all_once",
			query: path + " | filter { c00 + 1 > 100 } | select c00 | count | json",
		},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			var stdout bytes.Buffer
			for i := 0; i < b.N; i++ {
				stdout.Reset()
				if err := runQueryString(tc.query, &stdout); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkRunQueryStringLiteralJSONLInferAll(b *testing.B) {
	path := writeRunBenchmarkJSONL(b, 5000)
	cases := []struct {
		name  string
		query string
	}{
		{
			name:  "read_all",
			query: path + " with infer_rows=-1 | count | json",
		},
		{
			name:  "projection_eligible",
			query: path + " with infer_rows=-1 | select id | count | json",
		},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			var stdout bytes.Buffer
			for i := 0; i < b.N; i++ {
				stdout.Reset()
				if err := runQueryString(tc.query, &stdout); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func writeRunBenchmarkCSV(b *testing.B, cols, rows int) string {
	b.Helper()
	var sb strings.Builder
	for c := 0; c < cols; c++ {
		if c > 0 {
			sb.WriteByte(',')
		}
		fmt.Fprintf(&sb, "c%02d", c)
	}
	sb.WriteByte('\n')
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			if c > 0 {
				sb.WriteByte(',')
			}
			fmt.Fprintf(&sb, "%d", r+c)
		}
		sb.WriteByte('\n')
	}
	path := filepath.Join(b.TempDir(), "wide.csv")
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		b.Fatal(err)
	}
	return path
}

func writeRunBenchmarkJSONL(b *testing.B, rows int) string {
	b.Helper()
	var sb strings.Builder
	for r := 0; r < rows; r++ {
		fmt.Fprintf(&sb, "{\"id\":%d,\"status\":\"active\",\"amount\":%d,\"unused\":\"value-%d\"}\n", r, r*10, r)
	}
	path := filepath.Join(b.TempDir(), "wide.jsonl")
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		b.Fatal(err)
	}
	return path
}
