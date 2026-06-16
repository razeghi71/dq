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
	return inferFormatFromFilename(filename)
}

// EffectiveCompression returns the explicit compression or inferred file-level wrapper.
// Returns "" for stdin, globs, extensionless paths, or uncompressed literal paths.
func EffectiveCompression(filename, explicitCompression string) string {
	if explicitCompression != "" {
		return strings.ToLower(explicitCompression)
	}
	if filename == "-" || strings.ContainsAny(filename, "*?{") {
		return ""
	}
	compression, _ := inferCompressionFromFilename(filename)
	return compression
}

// IsSupportedLoadFormat reports whether name is a recognized load format.
func IsSupportedLoadFormat(format string) bool {
	return isSupportedFormat(supportedLoadFormats, format)
}

// ValidateLoadOptions checks format and format-specific options when format is explicit.
func ValidateLoadOptions(opts LoadOptions) error {
	if opts.Format != "" {
		if !IsSupportedLoadFormat(opts.Format) {
			return fmt.Errorf("with: unsupported format %q (supported: %s)", opts.Format, LoadFormatsList())
		}
	}
	if opts.Compression != "" {
		if !IsSupportedCompression(opts.Compression) {
			return fmt.Errorf("with: unsupported compression %q (supported: %s)", opts.Compression, CompressionFormatsList())
		}
		if opts.Format != "" && !IsStreamLoadFormat(opts.Format) {
			return fmt.Errorf("with: compression=%s applies only to csv, json, and jsonl formats", opts.Compression)
		}
	}
	if err := validateLoadOptionValues(opts, "with: "); err != nil {
		return err
	}
	if opts.Format != "" {
		return validateFormatSpecificOptions(opts, opts.Format, "with: ")
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
	if opts.Compression != "" {
		if format == "" || !IsSupportedLoadFormat(format) {
			return fmt.Errorf("with: cannot determine file format: use with format=... in query (%s)", LoadFormatsList())
		}
		if !IsStreamLoadFormat(format) {
			return fmt.Errorf("with: compression=%s applies only to csv, json, and jsonl formats", opts.Compression)
		}
	}
	return validateFormatSpecificOptions(opts, format, "with: ")
}

// ValidateLoadOptionsForFormat checks load options against a resolved format (load-time).
func ValidateLoadOptionsForFormat(opts LoadOptions, format, prefix string) error {
	if err := validateLoadOptionValues(opts, prefix); err != nil {
		return err
	}
	return validateFormatSpecificOptions(opts, format, prefix)
}

// ValidateCSVOnlyOptionsForFormat checks load options against a resolved format.
//
// Deprecated: use ValidateLoadOptionsForFormat.
func ValidateCSVOnlyOptionsForFormat(opts LoadOptions, format, prefix string) error {
	return ValidateLoadOptionsForFormat(opts, format, prefix)
}

func validateLoadOptionValues(opts LoadOptions, prefix string) error {
	if opts.InferRows != nil && *opts.InferRows < -1 {
		return fmt.Errorf("%sinfer_rows must be -1 or greater", prefix)
	}
	if opts.MaxBadRecords != nil && *opts.MaxBadRecords < 0 {
		return fmt.Errorf("%smax_bad_records must be greater than or equal to 0", prefix)
	}
	return nil
}

func validateFormatSpecificOptions(opts LoadOptions, format, prefix string) error {
	if opts.Header == nil && opts.Delim == "" && opts.AllowJaggedRows == nil && opts.IgnoreUnknownValues == nil && opts.InferRows == nil && opts.MaxBadRecords == nil {
		return nil
	}
	if format == "" || !IsSupportedLoadFormat(format) {
		return fmt.Errorf("%scannot determine file format: use with format=... in query (%s)", prefix, LoadFormatsList())
	}
	if format == "csv" {
		return nil
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
	if opts.IgnoreUnknownValues != nil {
		return fmt.Errorf("%signore_unknown_values applies only to csv format", prefix)
	}
	if format == "json" || format == "jsonl" {
		if opts.InferRows != nil && *opts.InferRows == 0 {
			return fmt.Errorf("%sinfer_rows=0 is invalid for %s format", prefix, format)
		}
		return nil
	}
	if opts.InferRows != nil {
		return fmt.Errorf("%sinfer_rows applies only to csv, json, and jsonl formats", prefix)
	}
	return fmt.Errorf("%smax_bad_records applies only to csv, json, and jsonl formats", prefix)
}

func inferFormatFromFilename(filename string) string {
	lower := strings.ToLower(filename)
	ext := strings.TrimPrefix(filepath.Ext(lower), ".")
	if _, ok := compressionFromExtension(ext); ok {
		base := strings.TrimSuffix(lower, filepath.Ext(lower))
		inner := strings.TrimPrefix(filepath.Ext(base), ".")
		return inner
	}
	return ext
}

func inferCompressionFromFilename(filename string) (string, bool) {
	lower := strings.ToLower(filename)
	ext := strings.TrimPrefix(filepath.Ext(lower), ".")
	return compressionFromExtension(ext)
}

func compressionFromExtension(ext string) (string, bool) {
	switch strings.ToLower(ext) {
	case "gz":
		return "gzip", true
	case "zst", "zstd":
		return "zstd", true
	case "deflate", "zlib":
		return "deflate", true
	default:
		return "", false
	}
}
