package ast

import (
	"strings"
	"testing"
)

func TestFormatRegistryLoadAndOutput(t *testing.T) {
	for _, format := range DataFormatNames() {
		if !IsSupportedLoadFormat(format) {
			t.Errorf("data format %q missing from load formats", format)
		}
		if !IsSupportedOutputFormat(format) {
			t.Errorf("data format %q missing from output formats", format)
		}
	}
	if !IsSupportedOutputFormat("table") {
		t.Fatal("table missing from output formats")
	}
	if IsSupportedLoadFormat("table") {
		t.Fatal("table should not be a load format")
	}
}

func TestFormatListsMatchRegistry(t *testing.T) {
	cases := []struct {
		list  string
		names []string
		isSup func(string) bool
	}{
		{LoadFormatsList(), DataFormatNames(), IsSupportedLoadFormat},
		{OutputFormatsList(), OutputFormatNames(), IsSupportedOutputFormat},
	}
	for _, tc := range cases {
		names := strings.Split(tc.list, ", ")
		if len(names) != len(tc.names) {
			t.Fatalf("list %q length %d != names length %d", tc.list, len(names), len(tc.names))
		}
		for i, name := range names {
			if name != tc.names[i] {
				t.Errorf("list order at %d: got %q, want %q", i, name, tc.names[i])
			}
			if !tc.isSup(name) {
				t.Errorf("list entry %q not supported", name)
			}
		}
	}
}

func TestOutputFormatNames(t *testing.T) {
	names := OutputFormatNames()
	if len(names) != len(supportedOutputFormats) {
		t.Fatalf("OutputFormatNames() length %d != %d", len(names), len(supportedOutputFormats))
	}
	if names[0] != "table" {
		t.Fatalf("first output format: got %q, want table", names[0])
	}
}

func TestDataFormatNames(t *testing.T) {
	names := DataFormatNames()
	if len(names) != len(supportedLoadFormats) {
		t.Fatalf("DataFormatNames() length %d != %d", len(names), len(supportedLoadFormats))
	}
}

func TestStreamFormatsAreLoadFormats(t *testing.T) {
	for _, format := range streamDataFormatNames {
		if !IsSupportedLoadFormat(format) {
			t.Errorf("stream format %q missing from load formats", format)
		}
	}
	if !strings.Contains(StreamFormatsList(), "csv") {
		t.Fatalf("stream list should include csv: %q", StreamFormatsList())
	}
}
