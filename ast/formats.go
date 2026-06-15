package ast

import "strings"

const outputFormatTable = "table"

// dataFormatNames is the canonical ordered list of file serialization formats.
var dataFormatNames = []string{"csv", "json", "jsonl", "avro", "parquet"}

// streamDataFormatNames are data formats readable from io.Reader (stdin).
var streamDataFormatNames = []string{"csv", "json", "jsonl"}

// compressionFormatNames are file-level compression wrappers.
var compressionFormatNames = []string{"gzip", "zstd", "deflate"}

var (
	supportedLoadFormats   map[string]bool
	supportedStreamFormats map[string]bool
	loadFormatsList        string
	supportedOutputFormats map[string]bool
	outputFormatsList      string
	streamFormatsList      string
	supportedCompressions  map[string]bool
	compressionsList       string
)

func init() {
	supportedLoadFormats = makeFormatSet(dataFormatNames)
	supportedStreamFormats = makeFormatSet(streamDataFormatNames)
	loadFormatsList = joinFormatNames(dataFormatNames)

	outputNames := append([]string{outputFormatTable}, dataFormatNames...)
	supportedOutputFormats = makeFormatSet(outputNames)
	outputFormatsList = joinFormatNames(outputNames)

	streamFormatsList = joinFormatNames(streamDataFormatNames)

	supportedCompressions = makeFormatSet(compressionFormatNames)
	compressionsList = joinFormatNames(compressionFormatNames)
}

// DataFormatNames returns a copy of the canonical data format names.
func DataFormatNames() []string {
	return append([]string(nil), dataFormatNames...)
}

// OutputFormatNames returns a copy of the canonical output format command names.
func OutputFormatNames() []string {
	names := append([]string(nil), dataFormatNames...)
	return append([]string{outputFormatTable}, names...)
}

// LoadFormatsList returns the user-facing comma-separated list of load format names.
func LoadFormatsList() string {
	return loadFormatsList
}

// OutputFormatsList returns the user-facing comma-separated list of output format command names.
func OutputFormatsList() string {
	return outputFormatsList
}

// StreamFormatsList returns the user-facing comma-separated list of stdin/stream load formats.
func StreamFormatsList() string {
	return streamFormatsList
}

// CompressionFormatsList returns the user-facing comma-separated list of load compression names.
func CompressionFormatsList() string {
	return compressionsList
}

// IsSupportedCompression reports whether name is a recognized load compression wrapper.
func IsSupportedCompression(compression string) bool {
	return isSupportedFormat(supportedCompressions, compression)
}

// IsStreamLoadFormat reports whether format can be loaded through an io.Reader.
func IsStreamLoadFormat(format string) bool {
	return isSupportedFormat(supportedStreamFormats, format)
}

func makeFormatSet(names []string) map[string]bool {
	set := make(map[string]bool, len(names))
	for _, name := range names {
		set[name] = true
	}
	return set
}

func joinFormatNames(names []string) string {
	if len(names) == 0 {
		return ""
	}
	b := strings.Builder{}
	for i, name := range names {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(name)
	}
	return b.String()
}

func normalizeFormat(format string) string {
	if format == "" {
		return ""
	}
	return strings.ToLower(format)
}

func isSupportedFormat(set map[string]bool, format string) bool {
	return set[strings.ToLower(format)]
}
