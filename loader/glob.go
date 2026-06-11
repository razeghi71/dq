package loader

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// HasGlobMeta reports whether pattern should be expanded as a glob.
// Literal paths with brackets (e.g. data[1].csv) are not globs unless *, ?, or { appear.
func HasGlobMeta(pattern string) bool {
	return strings.ContainsAny(pattern, "*?{")
}

// JoinLoadFormat returns the format to pass to Load for a join source.
// -f applies to join sources only when the join path is a glob pattern.
func JoinLoadFormat(filename, cliFormat string) string {
	if cliFormat != "" && HasGlobMeta(filename) {
		return cliFormat
	}
	return ""
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

func validateUniformFormat(paths []string, format string) (string, error) {
	if format != "" {
		return format, nil
	}
	if len(paths) == 0 {
		return "", fmt.Errorf("glob: no files to load")
	}

	resolved := make([]string, len(paths))
	seen := make(map[string]bool)
	for i, path := range paths {
		ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
		if ext == "" {
			return "", fmt.Errorf("cannot determine file format for %q: use -f to specify (csv, json, jsonl, avro, parquet)", path)
		}
		resolved[i] = ext
		seen[ext] = true
	}
	if len(seen) > 1 {
		exts := make([]string, 0, len(seen))
		for ext := range seen {
			exts = append(exts, ext)
		}
		sort.Strings(exts)
		return "", fmt.Errorf("glob matched mixed formats (%s); use -f to specify", strings.Join(exts, ", "))
	}
	return resolved[0], nil
}
