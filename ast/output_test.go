package ast

import (
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
