package ast

import (
	"fmt"
	"path/filepath"
	"strings"
)

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
	return isSupportedFormat(supportedLoadFormats, format)
}

// ValidateLoadOptions checks format and CSV-only options when format is explicit.
func ValidateLoadOptions(opts LoadOptions) error {
	if opts.Format != "" {
		if !IsSupportedLoadFormat(opts.Format) {
			return fmt.Errorf("with: unsupported format %q (supported: %s)", opts.Format, LoadFormatsList())
		}
	}
	if opts.Format != "" && opts.Format != "csv" {
		return validateCSVOnlyOptions(opts, opts.Format, "with: ")
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
	return validateCSVOnlyOptions(opts, format, "with: ")
}

// ValidateCSVOnlyOptionsForFormat checks CSV-only load options against a resolved format (load-time).
func ValidateCSVOnlyOptionsForFormat(opts LoadOptions, format, prefix string) error {
	return validateCSVOnlyOptions(opts, format, prefix)
}

func validateCSVOnlyOptions(opts LoadOptions, format, prefix string) error {
	if opts.Header == nil && opts.Delim == "" && opts.AllowJaggedRows == nil && opts.IgnoreUnknownValues == nil {
		return nil
	}
	if format == "csv" {
		return nil
	}
	if format == "" || !IsSupportedLoadFormat(format) {
		return fmt.Errorf("%scannot determine file format: use with format=... in query (%s)", prefix, LoadFormatsList())
	}
	if opts.Header != nil {
		return fmt.Errorf("%sheader applies only to csv format", prefix)
	}
	if opts.Delim != "" {
		return fmt.Errorf("%sdelim applies only to csv format", prefix)
	}
	if opts.AllowJaggedRows != nil {
		return fmt.Errorf("%sallow_jagged_rows applies only to csv format", prefix)
	}
	return fmt.Errorf("%signore_unknown_values applies only to csv format", prefix)
}
