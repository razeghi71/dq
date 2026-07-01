package main

import (
	"fmt"
	"io"

	"github.com/razeghi71/dq/ast"
	"github.com/razeghi71/dq/engine"
	"github.com/razeghi71/dq/loader"
	"github.com/razeghi71/dq/parser"
	"github.com/razeghi71/dq/rowstream"
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

	sourceOpts := loader.FromAST(q.Source.Load)
	if !loader.CanPrepare(q.Source.Filename, sourceOpts) {
		return runMaterializedQuery(q, stdout)
	}

	prepared, err := loader.PrepareInput(q.Source.Filename, sourceOpts, nil)
	if err != nil {
		return fmt.Errorf("load error: %w", err)
	}
	defer prepared.Close()
	joinSources := newPreparedJoinSourceProvider()
	defer joinSources.Close()

	result, err := engine.ExecuteSourceAdaptiveQuery(q, engine.SourceInfo{
		Filename:        q.Source.Filename,
		Load:            q.Source.Load,
		Schema:          prepared.Schema,
		DisablePushdown: loader.IsStdin(q.Source.Filename) || loader.HasGlobMeta(q.Source.Filename),
	}, func(filename string, opts ast.LoadOptions, spec engine.SourceLoadSpec) (rowstream.Stream, error) {
		return prepared.StreamSpec(loader.SourceLoadSpec{
			ReadColumns:   spec.ReadColumns,
			OutputColumns: spec.OutputColumns,
			Predicate:     loader.RowPredicate(spec.Predicate),
		})
	}, func(filename string, opts ast.LoadOptions, spec engine.SourceLoadSpec) (*table.Table, error) {
		return prepared.LoadSpec(loader.SourceLoadSpec{
			ReadColumns:   spec.ReadColumns,
			OutputColumns: spec.OutputColumns,
			Predicate:     loader.RowPredicate(spec.Predicate),
		})
	}, joinSources)
	if err != nil {
		return fmt.Errorf("error: %w", err)
	}

	return writeQueryResult(q, result, stdout)
}

type preparedJoinSourceProvider struct {
	sources []*loader.PreparedSource
}

func newPreparedJoinSourceProvider() *preparedJoinSourceProvider {
	return &preparedJoinSourceProvider{}
}

func (p *preparedJoinSourceProvider) PrepareJoinSource(filename string, opts ast.LoadOptions) (engine.PreparedJoinSource, error) {
	prepared, err := loader.PrepareInput(filename, loader.FromAST(opts), nil)
	if err != nil {
		return engine.PreparedJoinSource{}, err
	}
	source, err := engine.NewPreparedJoinSource(filename, prepared.Schema, func(spec engine.JoinSourceLoadSpec) (*table.Table, error) {
		return prepared.LoadSpec(loader.SourceLoadSpec{
			ReadColumns:   spec.Columns,
			OutputColumns: spec.Columns,
		})
	})
	if err != nil {
		_ = prepared.Close()
		return engine.PreparedJoinSource{}, err
	}
	p.sources = append(p.sources, prepared)
	return source, nil
}

func (p *preparedJoinSourceProvider) Close() {
	for _, prepared := range p.sources {
		_ = prepared.Close()
	}
	p.sources = nil
}

func runMaterializedQuery(q *ast.Query, stdout io.Writer) error {
	inputOpts := loader.FromAST(q.Source.Load)
	input, err := loader.LoadInput(q.Source.Filename, inputOpts, nil)
	if err != nil {
		return fmt.Errorf("load error: %w", err)
	}
	result, err := engine.ExecuteStreaming(q, input, func(filename string, opts ast.LoadOptions) (*table.Table, error) {
		return loader.Load(filename, loader.FromAST(opts))
	})
	if err != nil {
		return fmt.Errorf("error: %w", err)
	}
	return writeQueryResult(q, result, stdout)
}

func writeQueryResult(q *ast.Query, result *table.Table, stdout io.Writer) error {
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
