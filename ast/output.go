package ast

import "fmt"

// IsSupportedOutputFormat reports whether name is a recognized output format command.
func IsSupportedOutputFormat(format string) bool {
	return isSupportedFormat(supportedOutputFormats, format)
}

// NormalizeOutputFormat lowercases a non-empty output format name.
func NormalizeOutputFormat(format string) string {
	return normalizeFormat(format)
}

// CanonicalOutputFormat normalizes and validates an output format command name.
// Empty string means implicit table.
func CanonicalOutputFormat(format string) (string, error) {
	format = NormalizeOutputFormat(format)
	if format == "" {
		return "", nil
	}
	if !IsSupportedOutputFormat(format) {
		return "", fmt.Errorf("unsupported output format %q (supported: %s)", format, OutputFormatsList())
	}
	return format, nil
}

// ValidateOutputFormat checks a parsed output format command name.
// Empty format is valid (implicit table). Non-empty names are normalized first.
func ValidateOutputFormat(format string) error {
	_, err := CanonicalOutputFormat(format)
	return err
}
