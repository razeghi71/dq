package ast

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

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

// OutputExtension returns the canonical file extension for an output format.
func OutputExtension(format string) (string, error) {
	format, err := CanonicalOutputFormat(format)
	if err != nil {
		return "", err
	}
	switch format {
	case "", "table":
		return ".txt", nil
	}
	return "." + format, nil
}

// IsOutputDirectoryPath reports whether path is syntactically a directory output.
// Directory output is query text only: a trailing separator, never filesystem state.
func IsOutputDirectoryPath(path string) bool {
	if strings.HasSuffix(path, "/") {
		return true
	}
	separator := string(os.PathSeparator)
	return separator != "/" && strings.HasSuffix(path, separator)
}

// NormalizeOutputFilePath appends or validates the extension for format.
func NormalizeOutputFilePath(format, path string) (string, error) {
	wantExt, err := OutputExtension(format)
	if err != nil {
		return "", err
	}
	ext := filepath.Ext(path)
	if ext == "" {
		return path + wantExt, nil
	}
	if strings.ToLower(ext) != wantExt {
		return "", fmt.Errorf("output path extension %q does not match format %q (expected %s)", ext, NormalizeOutputFormat(format), wantExt)
	}
	return path, nil
}

// ResolveSingleOutputPath resolves the final path for a non-split output.
func ResolveSingleOutputPath(format, path string) (string, error) {
	ext, err := OutputExtension(format)
	if err != nil {
		return "", err
	}
	if IsOutputDirectoryPath(path) {
		return filepath.Join(path, "output"+ext), nil
	}
	return NormalizeOutputFilePath(format, path)
}

// ResolveSplitOutputPath resolves the final path for one split output part.
func ResolveSplitOutputPath(format, path string, part int) (string, error) {
	ext, err := OutputExtension(format)
	if err != nil {
		return "", err
	}
	if IsOutputDirectoryPath(path) {
		return filepath.Join(path, fmt.Sprintf("output-%d%s", part, ext)), nil
	}
	resolved := strings.ReplaceAll(path, "{n}", fmt.Sprintf("%d", part))
	return NormalizeOutputFilePath(format, resolved)
}

// ValidateOutputSpec checks cross-field rules for terminal output options.
func ValidateOutputSpec(spec OutputSpec) error {
	if err := ValidateOutputFormat(spec.Format); err != nil {
		return err
	}
	if spec.Options.Overwrite && spec.Path == "" {
		return fmt.Errorf("output option overwrite requires to path")
	}
	if spec.Options.SplitRows != 0 {
		if spec.Path == "" {
			return fmt.Errorf("output option split_rows requires to path")
		}
		if spec.Options.SplitRows <= 0 {
			return fmt.Errorf("output option split_rows must be greater than 0")
		}
		if !IsOutputDirectoryPath(spec.Path) && !strings.Contains(spec.Path, "{n}") {
			return fmt.Errorf("output split_rows requires {n} in file path template or a directory destination")
		}
	}
	return nil
}
