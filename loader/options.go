package loader

import (
	"strings"

	"github.com/razeghi71/dq/ast"
)

// Options configures how a file or stdin stream is loaded.
// Zero value keeps extension-based inference and CSV defaults.
type Options struct {
	Format              string
	Compression         string
	Header              *bool
	Delim               string
	AllowJaggedRows     *bool
	IgnoreUnknownValues *bool
}

func normalizeOptions(o Options) Options {
	if o.Format != "" {
		o.Format = strings.ToLower(o.Format)
	}
	if o.Compression != "" {
		o.Compression = strings.ToLower(o.Compression)
	}
	return o
}

// FromAST converts parser load options to loader options.
func FromAST(o ast.LoadOptions) Options {
	return normalizeOptions(Options{
		Format:              o.Format,
		Compression:         o.Compression,
		Header:              o.Header,
		Delim:               o.Delim,
		AllowJaggedRows:     o.AllowJaggedRows,
		IgnoreUnknownValues: o.IgnoreUnknownValues,
	})
}
