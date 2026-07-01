package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func BenchmarkRunQueryStringRightJoinDemandPruningWideCSV(b *testing.B) {
	dir := b.TempDir()
	left := writeRightJoinDemandBenchmarkLeftCSV(b, dir, "users.csv", 5000)
	right := writeRightJoinDemandBenchmarkRightCSV(b, dir, "orders.csv", 96, 5000)

	cases := []struct {
		name  string
		query string
	}{
		{
			name:  "right_key_only_count",
			query: left + ` | join ` + right + ` on id == user_id | count | json`,
		},
		{
			name:  "right_key_and_one_output",
			query: left + ` | join ` + right + ` on id == user_id | select name, orders_amount | json`,
		},
		{
			name:  "right_filter_demands_one_extra_column",
			query: left + ` | join ` + right + ` on id == user_id | filter { status == "open" } | select name | count | json`,
		},
		{
			name:  "right_collision_name_predicate",
			query: left + ` | join ` + right + ` on id == user_id | filter { orders_amount > 0 } | count | json`,
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

func BenchmarkRunQueryStringRightJoinDemandPruningTextFormats(b *testing.B) {
	dir := b.TempDir()
	left := writeRightJoinDemandBenchmarkLeftCSV(b, dir, "users.csv", 2000)
	csv := writeRightJoinDemandBenchmarkRightCSV(b, dir, "orders.csv", 32, 2000)
	jsonl := writeRightJoinDemandBenchmarkRightJSONL(b, dir, "orders.jsonl", 32, 2000)

	cases := []struct {
		name  string
		query string
	}{
		{
			name:  "csv_right_projected",
			query: left + ` | join ` + csv + ` on id == user_id | select name, orders_amount | json`,
		},
		{
			name:  "jsonl_right_projected",
			query: left + ` | join ` + jsonl + ` on id == user_id | select name, orders_amount | json`,
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

func writeRightJoinDemandBenchmarkLeftCSV(b *testing.B, dir, name string, rows int) string {
	b.Helper()
	var sb strings.Builder
	sb.WriteString("id,amount,name\n")
	for r := 0; r < rows; r++ {
		fmt.Fprintf(&sb, "%d,%d,user-%05d\n", r, r*3, r)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		b.Fatal(err)
	}
	return path
}

func writeRightJoinDemandBenchmarkRightCSV(b *testing.B, dir, name string, extraCols, rows int) string {
	b.Helper()
	var sb strings.Builder
	sb.WriteString("user_id,amount,status")
	for c := 0; c < extraCols; c++ {
		fmt.Fprintf(&sb, ",extra_%03d", c)
	}
	sb.WriteByte('\n')
	for r := 0; r < rows; r++ {
		status := "closed"
		if r%2 == 0 {
			status = "open"
		}
		fmt.Fprintf(&sb, "%d,%d,%s", r, r*10, status)
		for c := 0; c < extraCols; c++ {
			fmt.Fprintf(&sb, ",%d", r+c)
		}
		sb.WriteByte('\n')
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		b.Fatal(err)
	}
	return path
}

func writeRightJoinDemandBenchmarkRightJSONL(b *testing.B, dir, name string, extraCols, rows int) string {
	b.Helper()
	var sb strings.Builder
	for r := 0; r < rows; r++ {
		status := "closed"
		if r%2 == 0 {
			status = "open"
		}
		fmt.Fprintf(&sb, `{"user_id":%d,"amount":%d,"status":%q`, r, r*10, status)
		for c := 0; c < extraCols; c++ {
			fmt.Fprintf(&sb, `,"extra_%03d":%d`, c, r+c)
		}
		sb.WriteString("}\n")
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		b.Fatal(err)
	}
	return path
}
