package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	dq "github.com/razeghi71/dq"
	"github.com/razeghi71/dq/ast"
	"github.com/razeghi71/dq/loader"
)

var (
	errHelp    = errors.New("help")
	errGuide   = errors.New("guide")
	errVersion = errors.New("version")
)

var version = "dev"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "mcp" {
		if len(os.Args) > 2 {
			fmt.Fprintf(os.Stderr, "error: dq mcp does not accept extra arguments: %s\n", strings.Join(os.Args[2:], " "))
			os.Exit(1)
		}
		if err := runMCPServer(os.Stdin, os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "mcp error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	query, err := parseArgs(os.Args[1:])
	if err == errHelp {
		printUsage()
		os.Exit(0)
	}
	if err == errGuide {
		fmt.Fprint(os.Stdout, dq.AgentGuide)
		os.Exit(0)
	}
	if err == errVersion {
		fmt.Fprintln(os.Stdout, version)
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

	if err := runQueryString(query, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
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
		case "-v", "-version", "--version":
			return "", errVersion
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
	fmt.Fprintln(os.Stderr, "       dq mcp")
	fmt.Fprintln(os.Stderr, "example: dq 'users.csv | filter { age > 20 } | select name, age'")
	fmt.Fprintln(os.Stderr, "         dq 'users.csv | select name, age | csv'")
	fmt.Fprintln(os.Stderr, "         dq 'logs/**/*.csv | count'")
	fmt.Fprintln(os.Stderr, "         cat users.csv | dq '- with format=csv | count'")
	fmt.Fprintln(os.Stderr, "subcommands:")
	fmt.Fprintln(os.Stderr, "  mcp")
	fmt.Fprintln(os.Stderr, "        start a stdio MCP server")
	fmt.Fprintln(os.Stderr, "flags:")
	fmt.Fprintln(os.Stderr, "  -agent-guide")
	fmt.Fprintln(os.Stderr, "        print an AI agent friendly guide")
	fmt.Fprintln(os.Stderr, "  -v, --version")
	fmt.Fprintln(os.Stderr, "        print the current program version")
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
