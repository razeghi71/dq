package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/razeghi71/dq/engine"
	"github.com/razeghi71/dq/loader"
	"github.com/razeghi71/dq/parser"
	"github.com/razeghi71/dq/writer"
)

func main() {
	var format string
	var output string
	flag.StringVar(&format, "f", "", "input format: csv, json, jsonl, avro, parquet (overrides file extension)")
	flag.StringVar(&format, "format", "", "input format: csv, json, jsonl, avro, parquet (overrides file extension)")
	flag.StringVar(&output, "o", "", "output format: table (default), csv, json, jsonl")
	flag.StringVar(&output, "output", "", "output format: table (default), csv, json, jsonl")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: dq [-f format] [-o output] '<query>'")
		fmt.Fprintln(os.Stderr, "example: dq 'users.csv | filter { age > 20 } | select name age'")
		fmt.Fprintln(os.Stderr, "         dq -o csv 'users.json | select name age'")
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

	// Write output
	if err := writer.Write(os.Stdout, result, output); err != nil {
		fmt.Fprintf(os.Stderr, "output error: %v\n", err)
		os.Exit(1)
	}
}
