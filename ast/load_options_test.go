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
	}

	for _, tc := range cases {
		t.Run(tc.filename, func(t *testing.T) {
			if got := EffectiveFormat(tc.filename, ""); got != tc.want {
				t.Fatalf("EffectiveFormat(%q): got %q, want %q", tc.filename, got, tc.want)
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
