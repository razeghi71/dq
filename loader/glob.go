package loader

import (
	"fmt"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/razeghi71/dq/ast"
)

// HasGlobMeta reports whether pattern should be expanded as a glob.
// Literal paths with brackets (e.g. data[1].csv) are not globs unless *, ?, or { appear.
func HasGlobMeta(pattern string) bool {
	return ast.HasGlobMeta(pattern)
}

func expandGlob(pattern string) ([]string, error) {
	matches, err := doublestar.FilepathGlob(pattern, doublestar.WithFilesOnly())
	if err != nil {
		return nil, fmt.Errorf("glob %q: %w", pattern, err)
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("glob %q: no files matched", pattern)
	}
	sort.Strings(matches)
	return matches, nil
}

func validateUniformLoad(paths []string, opts Options) (string, string, error) {
	if len(paths) == 0 {
		return "", "", fmt.Errorf("glob: no files to load")
	}

	format := opts.Format
	compression := opts.Compression

	if format == "" {
		seen := make(map[string]bool)
		for _, path := range paths {
			ext := ast.EffectiveFormat(path, "")
			if ext == "" {
				return "", "", fmt.Errorf("cannot determine file format for %q: use with format=... in query (%s)", path, ast.LoadFormatsList())
			}
			seen[ext] = true
		}
		if len(seen) > 1 {
			exts := make([]string, 0, len(seen))
			for ext := range seen {
				exts = append(exts, ext)
			}
			sort.Strings(exts)
			return "", "", fmt.Errorf("glob matched mixed formats (%s); use with format=... in query", strings.Join(exts, ", "))
		}
		for ext := range seen {
			format = ext
		}
	}

	if compression == "" {
		seen := make(map[string]bool)
		for _, path := range paths {
			_, inferred := resolveFormatCompression(path, Options{})
			seen[inferred] = true
		}
		if len(seen) > 1 {
			names := make([]string, 0, len(seen))
			for c := range seen {
				if c == "" {
					names = append(names, "none")
				} else {
					names = append(names, c)
				}
			}
			sort.Strings(names)
			return "", "", fmt.Errorf("glob matched mixed compression (%s); use with compression=... in query", strings.Join(names, ", "))
		}
		for c := range seen {
			compression = c
		}
	}

	return format, compression, nil
}
