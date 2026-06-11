package ast

import (
	"fmt"
	"path/filepath"
	"strings"
)

// SupportedLoadFormatsList is the user-facing list of load format names.
const SupportedLoadFormatsList = "csv, json, jsonl, avro, parquet"

// SupportedLoadFormats is the set of recognized load format names.
var SupportedLoadFormats = map[string]bool{
	"csv": true, "json": true, "jsonl": true, "avro": true, "parquet": true,
}

// EffectiveFormat returns the explicit format or inferred file extension.
// Returns "" for stdin, globs, or extensionless literal paths when format is not set.
func EffectiveFormat(filename, explicitFormat string) string {
	if explicitFormat != "" {
		return strings.ToLower(explicitFormat)
	}
	if filename == "-" || strings.ContainsAny(filename, "*?{") {
		return ""
	}
	return strings.TrimPrefix(strings.ToLower(filepath.Ext(filename)), ".")
}

// IsSupportedLoadFormat reports whether name is a recognized load format.
func IsSupportedLoadFormat(format string) bool {
	return SupportedLoadFormats[strings.ToLower(format)]
}

// ValidateLoadOptions checks format and CSV-only options when format is explicit.
func ValidateLoadOptions(opts LoadOptions) error {
	if opts.Format != "" {
		if !IsSupportedLoadFormat(opts.Format) {
			return fmt.Errorf("with: unsupported format %q (supported: %s)", opts.Format, SupportedLoadFormatsList)
		}
	}
	if opts.Format != "" && opts.Format != "csv" {
		return validateCSVOnlyOptions(opts.Header, opts.Delim, opts.Format, "with: ")
	}
	return nil
}

// ValidateLoadOptionsForFilename checks options against a source or join filename.
func ValidateLoadOptionsForFilename(filename string, opts LoadOptions) error {
	if err := ValidateLoadOptions(opts); err != nil {
		return err
	}
	if opts.Format != "" {
		return nil
	}
	format := EffectiveFormat(filename, "")
	return validateCSVOnlyOptions(opts.Header, opts.Delim, format, "with: ")
}

// ValidateCSVOnlyOptionsForFormat checks header/delim against a resolved format (load-time).
func ValidateCSVOnlyOptionsForFormat(header *bool, delim, format, prefix string) error {
	return validateCSVOnlyOptions(header, delim, format, prefix)
}

func validateCSVOnlyOptions(header *bool, delim, format, prefix string) error {
	if header == nil && delim == "" {
		return nil
	}
	if format == "csv" {
		return nil
	}
	if format == "" || !IsSupportedLoadFormat(format) {
		return fmt.Errorf("%scannot determine file format: use with format=... in query (%s)", prefix, SupportedLoadFormatsList)
	}
	if header != nil {
		return fmt.Errorf("%sheader applies only to csv format", prefix)
	}
	return fmt.Errorf("%sdelim applies only to csv format", prefix)
}
