package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	dq "github.com/razeghi71/dq"
	"github.com/razeghi71/dq/engine"
	"github.com/razeghi71/dq/loader"
	"github.com/razeghi71/dq/parser"
	"github.com/razeghi71/dq/table"
	"github.com/razeghi71/dq/writer"
)

var (
	errHelp  = errors.New("help")
	errGuide = errors.New("guide")
)

func main() {
	format, output, query, err := parseArgs(os.Args[1:])
	if err == errHelp {
		printUsage()
		os.Exit(0)
	}
	if err == errGuide {
		fmt.Fprint(os.Stdout, dq.AgentGuide)
		os.Exit(0)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if query == "" {
		if !stdinPiped() {
			printUsage()
			os.Exit(1)
		}
		query = loader.StdinSource
	}

	q, err := parser.Parse(query)
	if err != nil {
		fmt.Fprintf(os.Stderr, "parse error: %v\n", err)
		os.Exit(1)
	}

	input, err := loader.LoadInput(q.Source.Filename, format, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load error: %v\n", err)
		os.Exit(1)
	}

	result, err := engine.Execute(q, input, func(filename string) (*table.Table, error) {
		return loader.Load(filename, "")
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if err := writer.Write(os.Stdout, result, output); err != nil {
		fmt.Fprintf(os.Stderr, "output error: %v\n", err)
		os.Exit(1)
	}
}

// parseArgs extracts -f/-o flags and the query string from argv.
// The query may start with '-' (stdin); manual parsing avoids the std flag
// package treating it as a flag.
func parseArgs(args []string) (format, output, query string, err error) {
	i := 0
	for i < len(args) {
		switch args[i] {
		case "-h", "-help", "--help":
			return "", "", "", errHelp
		case "-agent-guide", "--agent-guide":
			return "", "", "", errGuide
		case "-f", "-format":
			if i+1 >= len(args) {
				return "", "", "", fmt.Errorf("missing value for %s", args[i])
			}
			format = args[i+1]
			i += 2
		case "-o", "-output":
			if i+1 >= len(args) {
				return "", "", "", fmt.Errorf("missing value for %s", args[i])
			}
			output = args[i+1]
			i += 2
		case "--":
			return format, output, strings.Join(args[i+1:], " "), nil
		default:
			return format, output, strings.Join(args[i:], " "), nil
		}
	}
	return format, output, "", nil
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "usage: dq [-f format] [-o output] '<query>'")
	fmt.Fprintln(os.Stderr, "example: dq 'users.csv | filter { age > 20 } | select name age'")
	fmt.Fprintln(os.Stderr, "         dq -o csv 'users.json | select name age'")
	fmt.Fprintln(os.Stderr, "         cat users.csv | dq -f csv")
	fmt.Fprintln(os.Stderr, "         dq -f csv '- | filter { age > 20 }'")
	fmt.Fprintln(os.Stderr, "flags:")
	fmt.Fprintln(os.Stderr, "  -f, -format string")
	fmt.Fprintln(os.Stderr, "        input format: csv, json, jsonl, avro, parquet (overrides file extension)")
	fmt.Fprintln(os.Stderr, "  -o, -output string")
	fmt.Fprintln(os.Stderr, "        output format: table (default), csv, json, jsonl, avro, parquet")
	fmt.Fprintln(os.Stderr, "  -agent-guide")
	fmt.Fprintln(os.Stderr, "        print an AI agent friendly guide")
}

// stdinPiped reports whether stdin is not a terminal (e.g. data piped from cat).
func stdinPiped() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) == 0
}
