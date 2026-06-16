package loader

import (
	"strings"

	"github.com/razeghi71/dq/ast"
)

const defaultCSVInferRows = 50

// Options configures how a file or stdin stream is loaded.
// Zero value keeps extension-based inference and CSV defaults.
type Options struct {
	Format              string
	Compression         string
	Header              *bool
	Delim               string
	AllowJaggedRows     *bool
	IgnoreUnknownValues *bool
	InferRows           int  // csv only; default 50. Set InferRowsSet=true to use 0.
	InferRowsSet        bool // distinguishes explicit infer_rows=0 from the default.
	MaxBadRecords       int
	MaxBadRecordsSet    bool
}

func normalizeOptions(o Options) Options {
	if o.Format != "" {
		o.Format = strings.ToLower(o.Format)
	}
	if o.Compression != "" {
		o.Compression = strings.ToLower(o.Compression)
	}
	if !o.InferRowsSet && o.InferRows == 0 {
		o.InferRows = defaultCSVInferRows
	}
	return o
}

// FromAST converts parser load options to loader options.
func FromAST(o ast.LoadOptions) Options {
	opts := Options{
		Format:              o.Format,
		Compression:         o.Compression,
		Header:              o.Header,
		Delim:               o.Delim,
		AllowJaggedRows:     o.AllowJaggedRows,
		IgnoreUnknownValues: o.IgnoreUnknownValues,
	}
	if o.InferRows != nil {
		opts.InferRows = *o.InferRows
		opts.InferRowsSet = true
	}
	if o.MaxBadRecords != nil {
		opts.MaxBadRecords = *o.MaxBadRecords
		opts.MaxBadRecordsSet = true
	}
	return normalizeOptions(opts)
}
