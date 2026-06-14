package ast

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsSupportedOutputFormat(t *testing.T) {
	supported := append(OutputFormatNames(), "CSV", "TABLE", "JSONL")
	for _, format := range supported {
		if !IsSupportedOutputFormat(format) {
			t.Errorf("IsSupportedOutputFormat(%q) = false, want true", format)
		}
	}

	unsupported := []string{"", "xlsx", "tsv", "txt", "html", "csvv", "tables"}
	for _, format := range unsupported {
		if format == "" {
			continue
		}
		if IsSupportedOutputFormat(format) {
			t.Errorf("IsSupportedOutputFormat(%q) = true, want false", format)
		}
	}
}

func TestNormalizeOutputFormat(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"table", "table"},
		{"CSV", "csv"},
		{"JSONL", "jsonl"},
		{"Parquet", "parquet"},
	}
	for _, tc := range cases {
		if got := NormalizeOutputFormat(tc.in); got != tc.want {
			t.Errorf("NormalizeOutputFormat(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestValidateOutputFormat(t *testing.T) {
	t.Run("empty_implicit_table", func(t *testing.T) {
		if err := ValidateOutputFormat(""); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("all_supported", func(t *testing.T) {
		for _, format := range OutputFormatNames() {
			if err := ValidateOutputFormat(format); err != nil {
				t.Errorf("ValidateOutputFormat(%q): %v", format, err)
			}
		}
	})

	t.Run("case_insensitive", func(t *testing.T) {
		if err := ValidateOutputFormat("CSV"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("unsupported", func(t *testing.T) {
		err := ValidateOutputFormat("xlsx")
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "unsupported output format") {
			t.Fatalf("error %q should mention unsupported output format", err.Error())
		}
		if !strings.Contains(err.Error(), OutputFormatsList()) {
			t.Fatalf("error %q should list supported formats", err.Error())
		}
	})

	t.Run("unsupported_normalized_in_error", func(t *testing.T) {
		err := ValidateOutputFormat("XLSX")
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), `"xlsx"`) {
			t.Fatalf("error should quote normalized format, got: %v", err)
		}
	})
}

func TestCanonicalOutputFormat(t *testing.T) {
	t.Run("implicit_table", func(t *testing.T) {
		got, err := CanonicalOutputFormat("")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "" {
			t.Fatalf("got %q, want empty", got)
		}
	})

	t.Run("normalized_supported", func(t *testing.T) {
		got, err := CanonicalOutputFormat("CSV")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "csv" {
			t.Fatalf("got %q, want csv", got)
		}
	})

	t.Run("unsupported", func(t *testing.T) {
		_, err := CanonicalOutputFormat("xml")
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "unsupported output format") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestOutputExtension(t *testing.T) {
	cases := []struct {
		format string
		want   string
	}{
		{"", ".txt"},
		{"table", ".txt"},
		{"CSV", ".csv"},
		{"json", ".json"},
		{"jsonl", ".jsonl"},
		{"avro", ".avro"},
		{"parquet", ".parquet"},
	}
	for _, tc := range cases {
		got, err := OutputExtension(tc.format)
		if err != nil {
			t.Fatalf("OutputExtension(%q): %v", tc.format, err)
		}
		if got != tc.want {
			t.Fatalf("OutputExtension(%q) = %q, want %q", tc.format, got, tc.want)
		}
	}

	if _, err := OutputExtension("xlsx"); err == nil {
		t.Fatal("expected unsupported format error")
	}
}

func TestNormalizeOutputFilePath(t *testing.T) {
	cases := []struct {
		name   string
		format string
		path   string
		want   string
	}{
		{"append_extension", "csv", "out", "out.csv"},
		{"preserve_matching_extension", "json", "data.json", "data.json"},
		{"accept_uppercase_extension", "json", "data.JSON", "data.JSON"},
		{"table_extension", "table", "result", "result.txt"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeOutputFilePath(tc.format, tc.path)
			if err != nil {
				t.Fatalf("NormalizeOutputFilePath(%q, %q): %v", tc.format, tc.path, err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}

	err := func() error {
		_, err := NormalizeOutputFilePath("csv", "out.json")
		return err
	}()
	if err == nil {
		t.Fatal("expected extension mismatch error")
	}
	if !strings.Contains(err.Error(), "does not match format") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestIsOutputDirectoryPath(t *testing.T) {
	backslashIsPlatformSeparator := string(os.PathSeparator) == "\\"
	cases := []struct {
		path string
		want bool
	}{
		{"out/", true},
		{"out\\", backslashIsPlatformSeparator},
		{"out", false},
		{"out.csv", false},
	}
	for _, tc := range cases {
		if got := IsOutputDirectoryPath(tc.path); got != tc.want {
			t.Fatalf("IsOutputDirectoryPath(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestResolveOutputPaths(t *testing.T) {
	t.Run("single_directory", func(t *testing.T) {
		got, err := ResolveSingleOutputPath("csv", "out/")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := filepath.Join("out/", "output.csv")
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("single_file", func(t *testing.T) {
		got, err := ResolveSingleOutputPath("parquet", "out")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "out.parquet" {
			t.Fatalf("got %q, want out.parquet", got)
		}
	})

	t.Run("split_directory", func(t *testing.T) {
		got, err := ResolveSplitOutputPath("jsonl", "out/", 3)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := filepath.Join("out/", "output-3.jsonl")
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("split_template", func(t *testing.T) {
		got, err := ResolveSplitOutputPath("avro", "part-{n}", 12)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "part-12.avro" {
			t.Fatalf("got %q, want part-12.avro", got)
		}
	})

	t.Run("split_template_replaces_all_markers", func(t *testing.T) {
		got, err := ResolveSplitOutputPath("csv", "p-{n}-{n}.csv", 4)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "p-4-4.csv" {
			t.Fatalf("got %q, want p-4-4.csv", got)
		}
	})
}

func TestValidateOutputSpec(t *testing.T) {
	t.Run("split_requires_path", func(t *testing.T) {
		err := ValidateOutputSpec(OutputSpec{Format: "csv", Options: OutputOptions{SplitRows: 2}})
		if err == nil || !strings.Contains(err.Error(), "requires to path") {
			t.Fatalf("expected split path error, got %v", err)
		}
	})

	t.Run("overwrite_requires_path", func(t *testing.T) {
		err := ValidateOutputSpec(OutputSpec{Format: "csv", Options: OutputOptions{Overwrite: true}})
		if err == nil || !strings.Contains(err.Error(), "requires to path") {
			t.Fatalf("expected overwrite path error, got %v", err)
		}
	})

	t.Run("split_requires_directory_or_template", func(t *testing.T) {
		err := ValidateOutputSpec(OutputSpec{Format: "csv", Path: "out", Options: OutputOptions{SplitRows: 2}})
		if err == nil || !strings.Contains(err.Error(), "{n}") {
			t.Fatalf("expected split target error, got %v", err)
		}
	})

	t.Run("split_accepts_directory", func(t *testing.T) {
		err := ValidateOutputSpec(OutputSpec{Format: "csv", Path: "out/", Options: OutputOptions{SplitRows: 2}})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("split_accepts_template", func(t *testing.T) {
		err := ValidateOutputSpec(OutputSpec{Format: "csv", Path: "part-{n}.csv", Options: OutputOptions{SplitRows: 2}})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}
