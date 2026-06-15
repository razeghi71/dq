package main

import (
	"fmt"
	"io"

	"github.com/razeghi71/dq/ast"
	"github.com/razeghi71/dq/engine"
	"github.com/razeghi71/dq/loader"
	"github.com/razeghi71/dq/parser"
	"github.com/razeghi71/dq/table"
	"github.com/razeghi71/dq/writer"
)

func runQueryString(query string, stdout io.Writer) error {
	return runQuery(query, stdout, false)
}

func runMCPQuery(query string, stdout io.Writer) error {
	return runQuery(query, stdout, true)
}

func runQuery(query string, stdout io.Writer, disallowStdin bool) error {
	q, err := parser.Parse(query)
	if err != nil {
		return fmt.Errorf("parse error: %w", err)
	}
	if disallowStdin && q.Source.Filename == loader.StdinSource {
		return fmt.Errorf("stdin source is not supported in MCP query")
	}

	inputOpts := loader.FromAST(q.Source.Load)
	input, err := loader.LoadInput(q.Source.Filename, inputOpts, nil)
	if err != nil {
		return fmt.Errorf("load error: %w", err)
	}

	result, err := engine.Execute(q, input, func(filename string, opts ast.LoadOptions) (*table.Table, error) {
		return loader.Load(filename, loader.FromAST(opts))
	})
	if err != nil {
		return fmt.Errorf("error: %w", err)
	}

	if q.Output.Path != "" {
		if err := writer.WriteOutput(result, q.Output); err != nil {
			return fmt.Errorf("output error: %w", err)
		}
		return nil
	}

	if err := writer.Write(stdout, result, q.Output.Format); err != nil {
		return fmt.Errorf("output error: %w", err)
	}
	return nil
}
