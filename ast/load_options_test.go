package ast

import (
	"strings"
	"testing"
)

func boolPtr(b bool) *bool {
	v := b
	return &v
}

func TestValidateLoadOptionsForFilenameUnknownExtension(t *testing.T) {
	cases := []struct {
		name     string
		filename string
		opts     LoadOptions
		msg      string
	}{
		{
			name:     "header_on_dat",
			filename: "data.dat",
			opts:     LoadOptions{Header: boolPtr(false)},
			msg:      "with format",
		},
		{
			name:     "delim_on_dat",
			filename: "data.dat",
			opts:     LoadOptions{Delim: ";"},
			msg:      "with format",
		},
		{
			name:     "header_on_glob",
			filename: "part-*.dat",
			opts:     LoadOptions{Header: boolPtr(false)},
			msg:      "with format",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateLoadOptionsForFilename(tc.filename, tc.opts)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tc.msg)) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.msg)
			}
		})
	}
}

func TestValidateLoadOptionsForFilenameCSVExtension(t *testing.T) {
	cases := []struct {
		name     string
		filename string
		opts     LoadOptions
	}{
		{
			name:     "header_false",
			filename: "data.csv",
			opts:     LoadOptions{Header: boolPtr(false)},
		},
		{
			name:     "delim_only",
			filename: "data.csv",
			opts:     LoadOptions{Delim: ";"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateLoadOptionsForFilename(tc.filename, tc.opts); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestEffectiveFormatGzipDoubleExtensions(t *testing.T) {
	cases := []struct {
		filename string
		want     string
	}{
		{"data.csv.gz", "csv"},
		{"data.json.gz", "json"},
		{"data.jsonl.gz", "jsonl"},
		{"DATA.CSV.GZ", "csv"},
		{"data.csv.zst", "csv"},
		{"data.json.zst", "json"},
		{"data.jsonl.zst", "jsonl"},
		{"data.csv.zstd", "csv"},
		{"data.json.zstd", "json"},
		{"data.jsonl.zstd", "jsonl"},
		{"DATA.CSV.ZST", "csv"},
		{"data.csv.deflate", "csv"},
		{"data.json.deflate", "json"},
		{"data.jsonl.deflate", "jsonl"},
		{"data.csv.zlib", "csv"},
		{"data.json.zlib", "json"},
		{"data.jsonl.zlib", "jsonl"},
		{"DATA.CSV.ZLIB", "csv"},
	}

	for _, tc := range cases {
		t.Run(tc.filename, func(t *testing.T) {
			if got := EffectiveFormat(tc.filename, ""); got != tc.want {
				t.Fatalf("EffectiveFormat(%q): got %q, want %q", tc.filename, got, tc.want)
			}
		})
	}
}

func TestEffectiveCompression(t *testing.T) {
	cases := []struct {
		name     string
		filename string
		explicit string
		want     string
	}{
		{
			name:     "gzip_suffix",
			filename: "data.csv.gz",
			want:     "gzip",
		},
		{
			name:     "zst_suffix",
			filename: "data.jsonl.zst",
			want:     "zstd",
		},
		{
			name:     "zstd_suffix",
			filename: "data.json.zstd",
			want:     "zstd",
		},
		{
			name:     "case_insensitive_suffix",
			filename: "DATA.CSV.ZST",
			want:     "zstd",
		},
		{
			name:     "deflate_suffix",
			filename: "data.jsonl.deflate",
			want:     "deflate",
		},
		{
			name:     "zlib_suffix",
			filename: "data.json.zlib",
			want:     "deflate",
		},
		{
			name:     "deflate_case_insensitive_suffix",
			filename: "DATA.CSV.ZLIB",
			want:     "deflate",
		},
		{
			name:     "explicit_override",
			filename: "data.csv.gz",
			explicit: "deflate",
			want:     "deflate",
		},
		{
			name:     "extensionless",
			filename: "data",
			want:     "",
		},
		{
			name:     "glob_short_circuit",
			filename: "part-*.csv.zst",
			want:     "",
		},
		{
			name:     "stdin_short_circuit",
			filename: "-",
			want:     "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := EffectiveCompression(tc.filename, tc.explicit); got != tc.want {
				t.Fatalf("EffectiveCompression(%q, %q): got %q, want %q", tc.filename, tc.explicit, got, tc.want)
			}
		})
	}
}

func TestValidateLoadOptionsForFilenameDeflateCSVExtension(t *testing.T) {
	cases := []struct {
		name     string
		filename string
		opts     LoadOptions
	}{
		{
			name:     "header_false_deflate",
			filename: "data.csv.deflate",
			opts:     LoadOptions{Header: boolPtr(false)},
		},
		{
			name:     "delim_only_zlib",
			filename: "data.csv.zlib",
			opts:     LoadOptions{Delim: ";"},
		},
		{
			name:     "explicit_compression",
			filename: "data.csv",
			opts: LoadOptions{
				Compression: "deflate",
			},
		},
		{
			name:     "explicit_format_and_compression",
			filename: "data",
			opts: LoadOptions{
				Format:      "csv",
				Compression: "deflate",
				Header:      boolPtr(false),
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateLoadOptionsForFilename(tc.filename, tc.opts); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestValidateLoadOptionsDeflateRejectsUnsupportedFormats(t *testing.T) {
	for _, format := range []string{"avro", "parquet"} {
		t.Run(format, func(t *testing.T) {
			err := ValidateLoadOptions(LoadOptions{Format: format, Compression: "deflate"})
			if err == nil {
				t.Fatal("expected compression format restriction")
			}
			lower := strings.ToLower(err.Error())
			if !strings.Contains(lower, "compression=deflate") || !strings.Contains(lower, "csv") || !strings.Contains(lower, "jsonl") {
				t.Fatalf("expected compression format restriction, got %v", err)
			}
		})
	}
}

func TestValidateLoadOptionsForFilenameZstdCSVExtension(t *testing.T) {
	cases := []struct {
		name     string
		filename string
		opts     LoadOptions
	}{
		{
			name:     "header_false_zst",
			filename: "data.csv.zst",
			opts:     LoadOptions{Header: boolPtr(false)},
		},
		{
			name:     "delim_only_zstd",
			filename: "data.csv.zstd",
			opts:     LoadOptions{Delim: ";"},
		},
		{
			name:     "explicit_compression",
			filename: "data.data",
			opts: LoadOptions{
				Format:      "csv",
				Compression: "zstd",
			},
		},
		{
			name:     "row_shape_options",
			filename: "data.csv.zst",
			opts: LoadOptions{
				AllowJaggedRows:     boolPtr(true),
				IgnoreUnknownValues: boolPtr(true),
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateLoadOptionsForFilename(tc.filename, tc.opts); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestValidateLoadOptionsForFilenameGzipCSVExtension(t *testing.T) {
	cases := []struct {
		name     string
		filename string
		opts     LoadOptions
	}{
		{
			name:     "header_false",
			filename: "data.csv.gz",
			opts:     LoadOptions{Header: boolPtr(false)},
		},
		{
			name:     "delim_only",
			filename: "data.csv.gz",
			opts:     LoadOptions{Delim: ";"},
		},
		{
			name:     "row_shape_options",
			filename: "data.csv.gz",
			opts: LoadOptions{
				AllowJaggedRows:     boolPtr(true),
				IgnoreUnknownValues: boolPtr(true),
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateLoadOptionsForFilename(tc.filename, tc.opts); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestValidateCSVOnlyOptionsForFormat(t *testing.T) {
	if err := ValidateCSVOnlyOptionsForFormat(LoadOptions{Header: boolPtr(false)}, "dat", ""); err == nil {
		t.Fatal("expected error for csv option on unknown format")
	}
	if err := ValidateCSVOnlyOptionsForFormat(LoadOptions{Delim: ";"}, "csv", ""); err != nil {
		t.Fatalf("csv format should allow delim: %v", err)
	}
	if err := ValidateCSVOnlyOptionsForFormat(LoadOptions{IgnoreUnknownValues: boolPtr(true)}, "json", ""); err == nil {
		t.Fatal("expected error for ignore_unknown_values on json")
	}
}
