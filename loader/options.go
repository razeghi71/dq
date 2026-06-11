package loader

import (
	"strings"

	"github.com/razeghi71/dq/ast"
)

// Options configures how a file or stdin stream is loaded.
// Zero value keeps extension-based inference and CSV defaults.
type Options struct {
	Format string
	Header *bool
	Delim  string
}

func normalizeOptions(o Options) Options {
	if o.Format != "" {
		o.Format = strings.ToLower(o.Format)
	}
	return o
}

// FromAST converts parser load options to loader options.
func FromAST(o ast.LoadOptions) Options {
	return normalizeOptions(Options{
		Format: o.Format,
		Header: o.Header,
		Delim:  o.Delim,
	})
}
