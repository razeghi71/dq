package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func BenchmarkRunQueryStringSchemaEnvWideCSV(b *testing.B) {
	dir := b.TempDir()
	left := writeSchemaEnvBenchmarkWideCSV(b, dir, "wide.csv", 256, 1000)
	right := writeSchemaEnvBenchmarkJoinCSV(b, dir, "right.csv", 1000)
	cases := []struct {
		name  string
		query string
	}{
		{
			name:  "wide_filter_select_count",
			query: left + ` | filter { c199 > 300 and c201 < 900 } | select c003, c199, c201 | count | json`,
		},
		{
			name:  "wide_transform_select_count",
			query: left + ` | transform score = c199 + c201, ratio = c201 / c005, keep = c003 | select score, ratio, keep | count | json`,
		},
		{
			name:  "wide_join_late_key_count",
			query: left + ` | filter { c199 > 300 } | join ` + right + ` on c200 == rkey | select c003, total | count | json`,
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

func BenchmarkRunQueryStringSchemaEnvPlannerProofCSV(b *testing.B) {
	path := writeRunBenchmarkCSV(b, 32, 1000)
	cases := []struct {
		name  string
		query string
	}{
		{
			name:  "short_filter",
			query: path + ` | filter { c06 > 10 } | count | json`,
		},
		{
			name:  "short_select",
			query: path + ` | select c01, c06 | count | json`,
		},
		{
			name:  "row_span_filter_select_transform",
			query: path + ` | filter { c06 > 10 } | select c01, c06 | transform score = c06 + 1 | rename score=score2 | remove c06 | count | json`,
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

func writeSchemaEnvBenchmarkWideCSV(b *testing.B, dir, name string, cols, rows int) string {
	b.Helper()
	var sb strings.Builder
	for c := 0; c < cols; c++ {
		if c > 0 {
			sb.WriteByte(',')
		}
		fmt.Fprintf(&sb, "c%03d", c)
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
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		b.Fatal(err)
	}
	return path
}

func writeSchemaEnvBenchmarkJoinCSV(b *testing.B, dir, name string, rows int) string {
	b.Helper()
	var sb strings.Builder
	sb.WriteString("rkey,total\n")
	for r := 0; r < rows; r++ {
		fmt.Fprintf(&sb, "%d,%d\n", r+200, r*10)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		b.Fatal(err)
	}
	return path
}
