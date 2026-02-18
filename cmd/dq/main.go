package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/razeghi71/dq/engine"
	"github.com/razeghi71/dq/loader"
	"github.com/razeghi71/dq/parser"
	"github.com/razeghi71/dq/table"
)

func main() {
	var format string
	flag.StringVar(&format, "f", "", "file format: csv, json, jsonl, avro (overrides file extension)")
	flag.StringVar(&format, "format", "", "file format: csv, json, jsonl, avro (overrides file extension)")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: dq [-f format] '<query>'")
		fmt.Fprintln(os.Stderr, "example: dq 'users.csv | filter { age > 20 } | select name age'")
		fmt.Fprintln(os.Stderr, "         dq -f csv 'mydata | select name age'")
		flag.PrintDefaults()
	}
	flag.Parse()

	if flag.NArg() < 1 {
		flag.Usage()
		os.Exit(1)
	}

	query := flag.Arg(0)

	q, err := parser.Parse(query)
	if err != nil {
		fmt.Fprintf(os.Stderr, "parse error: %v\n", err)
		os.Exit(1)
	}

	// Load the source file
	input, err := loader.Load(q.Source.Filename, format)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load error: %v\n", err)
		os.Exit(1)
	}

	// Execute the pipeline
	result, err := engine.Execute(q, input)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// Print the result as a formatted table
	printTable(result)
}

func printTable(t *table.Table) {
	if len(t.Columns) == 0 {
		return
	}

	// Calculate column widths
	widths := make([]int, len(t.Columns))
	for i, col := range t.Columns {
		widths[i] = len(col)
	}

	// Format all cell values
	cells := make([][]string, len(t.Rows))
	for i, row := range t.Rows {
		cells[i] = make([]string, len(t.Columns))
		for j := range t.Columns {
			if j < len(row.Values) {
				cells[i][j] = row.Values[j].AsString()
			} else {
				cells[i][j] = "null"
			}
			if len(cells[i][j]) > widths[j] {
				widths[j] = len(cells[i][j])
			}
		}
	}

	// Print header
	headerParts := make([]string, len(t.Columns))
	for i, col := range t.Columns {
		headerParts[i] = padRight(col, widths[i])
	}
	fmt.Println(strings.Join(headerParts, " | "))

	// Print separator
	sepParts := make([]string, len(t.Columns))
	for i := range t.Columns {
		sepParts[i] = strings.Repeat("-", widths[i])
	}
	fmt.Println(strings.Join(sepParts, "-+-"))

	// Print rows
	for _, row := range cells {
		parts := make([]string, len(t.Columns))
		for i := range t.Columns {
			parts[i] = padRight(row[i], widths[i])
		}
		fmt.Println(strings.Join(parts, " | "))
	}
}

func padRight(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}
