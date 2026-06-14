package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	dq "github.com/razeghi71/dq"
	"github.com/razeghi71/dq/ast"
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
	query, err := parseArgs(os.Args[1:])
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

	inputOpts := loader.FromAST(q.Source.Load)
	input, err := loader.LoadInput(q.Source.Filename, inputOpts, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load error: %v\n", err)
		os.Exit(1)
	}

	result, err := engine.Execute(q, input, func(filename string, opts ast.LoadOptions) (*table.Table, error) {
		return loader.Load(filename, loader.FromAST(opts))
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if q.Output.Path != "" {
		if err := writer.WriteOutput(result, q.Output); err != nil {
			fmt.Fprintf(os.Stderr, "output error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if err := writer.Write(os.Stdout, result, q.Output.Format); err != nil {
		fmt.Fprintf(os.Stderr, "output error: %v\n", err)
		os.Exit(1)
	}
}

// parseArgs extracts the query string from argv.
// The query may start with '-' (stdin); manual parsing avoids the std flag
// package treating it as a flag.
func parseArgs(args []string) (query string, err error) {
	i := 0
	for i < len(args) {
		switch args[i] {
		case "-h", "-help", "--help":
			return "", errHelp
		case "-agent-guide", "--agent-guide":
			return "", errGuide
		case "--":
			return strings.Join(args[i+1:], " "), nil
		default:
			return strings.Join(args[i:], " "), nil
		}
	}
	return "", nil
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "usage: dq '<query>'")
	fmt.Fprintln(os.Stderr, "example: dq 'users.csv | filter { age > 20 } | select name, age'")
	fmt.Fprintln(os.Stderr, "         dq 'users.csv | select name, age | csv'")
	fmt.Fprintln(os.Stderr, "         dq 'logs/**/*.csv | count'")
	fmt.Fprintln(os.Stderr, "         cat users.csv | dq '- with format=csv | count'")
	fmt.Fprintln(os.Stderr, "flags:")
	fmt.Fprintln(os.Stderr, "  -agent-guide")
	fmt.Fprintln(os.Stderr, "        print an AI agent friendly guide")
	fmt.Fprintln(os.Stderr, "output formats (terminal pipeline commands):")
	fmt.Fprintf(os.Stderr, "        %s — e.g. '| csv' at end of query (table is default when omitted)\n", ast.OutputFormatsList())
}

// stdinPiped reports whether stdin is not a terminal (e.g. data piped from cat).
func stdinPiped() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) == 0
}
