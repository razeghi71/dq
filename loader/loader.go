package loader

import (
	"bufio"
	"compress/gzip"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"

	goavro "github.com/linkedin/goavro/v2"
	parquet "github.com/parquet-go/parquet-go"
	"github.com/razeghi71/dq/ast"
	"github.com/razeghi71/dq/table"
)

// StdinSource is the query source sentinel for reading from stdin.
const StdinSource = "-"

// IsStdin reports whether filename denotes stdin.
func IsStdin(filename string) bool {
	return filename == StdinSource
}

// LoadInput reads from filename or from stdin when filename is "-".
// When reading from stdin, opts.Format must be set (csv, json, or jsonl).
// Pass nil for stdin to use os.Stdin.
func LoadInput(filename string, opts Options, stdin io.Reader) (*table.Table, error) {
	opts = normalizeOptions(opts)
	if IsStdin(filename) {
		if opts.Format == "" {
			return nil, fmt.Errorf("reading from stdin requires with format=... in query (%s)", ast.StreamFormatsList())
		}
		if err := validateOptionsForFormat(opts, opts.Format); err != nil {
			return nil, err
		}
		if stdin == nil {
			stdin = os.Stdin
		}
		return LoadReader(stdin, opts)
	}
	return Load(filename, opts)
}

// Load reads a file and returns a Table. opts.Format overrides the file extension
// when non-empty; otherwise the extension is used. Patterns containing *, ?, or {
// expand to all matching files and are concatenated.
func Load(filename string, opts Options) (*table.Table, error) {
	opts = normalizeOptions(opts)
	if IsStdin(filename) {
		return LoadInput(filename, opts, nil)
	}
	if HasGlobMeta(filename) {
		return loadGlob(filename, opts)
	}
	return loadFile(filename, opts, nil)
}

func loadFile(filename string, opts Options, csvColumns []string) (*table.Table, error) {
	format, compression := resolveFormatCompression(filename, opts)
	if err := validateOptionsForFormat(opts, format); err != nil {
		return nil, err
	}
	switch format {
	case "csv":
		cfg := csvConfigFromOptions(opts, csvColumns)
		cfg.compression = compression
		return loadCSV(filename, cfg)
	case "json":
		return loadJSON(filename, compression)
	case "jsonl":
		return loadJSONL(filename, compression)
	case "avro":
		if compression != "" {
			return nil, fmt.Errorf("compression=%s applies only to csv, json, and jsonl formats", compression)
		}
		return loadAvro(filename)
	case "parquet":
		if compression != "" {
			return nil, fmt.Errorf("compression=%s applies only to csv, json, and jsonl formats", compression)
		}
		return loadParquet(filename)
	default:
		if format == "" {
			return nil, fmt.Errorf("cannot determine file format for %q: use with format=... in query (%s)", filename, ast.LoadFormatsList())
		}
		return nil, fmt.Errorf("unsupported format %q (supported: %s)", format, ast.LoadFormatsList())
	}
}

func loadGlob(pattern string, opts Options) (*table.Table, error) {
	matches, err := expandGlob(pattern)
	if err != nil {
		return nil, err
	}
	resolved, compression, err := validateUniformLoad(matches, opts)
	if err != nil {
		return nil, err
	}
	opts.Compression = compression
	if err := validateOptionsForFormat(opts, resolved); err != nil {
		return nil, err
	}

	if resolved == "csv" {
		return loadGlobCSV(pattern, matches, opts)
	}

	var parts []*table.Table
	partOpts := opts
	partOpts.Format = resolved
	partOpts.Compression = compression
	for _, path := range matches {
		tbl, err := loadFile(path, partOpts, nil)
		if err != nil {
			return nil, fmt.Errorf("loading glob %q: loading %q: %w", pattern, path, err)
		}
		parts = append(parts, tbl)
	}
	return table.Concat(parts)
}

func loadGlobCSV(pattern string, matches []string, opts Options) (*table.Table, error) {
	cfg := csvConfigFromOptions(opts, nil)
	if !cfg.header {
		return loadGlobCSVHeaderless(pattern, matches, cfg)
	}

	var parts []*table.Table
	var anchor []string
	for _, path := range matches {
		var (
			tbl *table.Table
			err error
		)
		if len(anchor) == 0 {
			tbl, err = loadCSV(path, cfg)
		} else {
			tbl, err = loadCSVGlobShard(path, anchor, cfg)
		}
		if err != nil {
			return nil, fmt.Errorf("loading glob %q: loading %q: %w", pattern, path, err)
		}
		if len(anchor) == 0 && hasNonEmptyColumnName(tbl.Columns) {
			anchor = append([]string(nil), tbl.Columns...)
		}
		parts = append(parts, tbl)
	}
	return table.Concat(parts)
}

func loadGlobCSVHeaderless(pattern string, matches []string, cfg csvLoadConfig) (*table.Table, error) {
	var parts []*table.Table
	var anchor []string
	for _, path := range matches {
		var (
			tbl *table.Table
			err error
		)
		if len(anchor) == 0 {
			tbl, err = loadCSV(path, cfg)
			if err != nil {
				return nil, fmt.Errorf("loading glob %q: loading %q: %w", pattern, path, err)
			}
			if hasNonEmptyColumnName(tbl.Columns) {
				anchor = append([]string(nil), tbl.Columns...)
			}
		} else {
			tbl, err = loadCSVPositional(path, anchor, cfg)
			if err != nil {
				return nil, fmt.Errorf("loading glob %q: loading %q: %w", pattern, path, err)
			}
		}
		parts = append(parts, tbl)
	}
	return table.Concat(parts)
}

// LoadReader reads a table from r. opts.Format must be csv, json, or jsonl.
func LoadReader(r io.Reader, opts Options) (*table.Table, error) {
	opts = normalizeOptions(opts)
	if err := validateOptionsForFormat(opts, opts.Format); err != nil {
		return nil, err
	}
	if opts.Compression != "" {
		wrapped, err := wrapInputReader(r, opts.Compression)
		if err != nil {
			if opts.Compression == "gzip" {
				return nil, fmt.Errorf("cannot read gzip stream: %w", err)
			}
			return nil, err
		}
		defer wrapped.Close()
		r = wrapped
	}
	switch opts.Format {
	case "csv":
		return loadCSVReader(r, csvConfigFromOptions(opts, nil))
	case "json":
		return loadJSONReader(r)
	case "jsonl":
		return loadJSONLReader(r)
	default:
		return nil, fmt.Errorf("LoadReader: unsupported format %q (supported: %s)", opts.Format, ast.StreamFormatsList())
	}
}

type csvLoadConfig struct {
	columns             []string
	header              bool
	delim               rune
	allowJaggedRows     bool
	ignoreUnknownValues bool
	compression         string
}

func csvConfigFromOptions(opts Options, columns []string) csvLoadConfig {
	cfg := csvLoadConfig{
		columns:     columns,
		header:      true,
		delim:       ',',
		compression: opts.Compression,
	}
	if opts.Header != nil {
		cfg.header = *opts.Header
	}
	if opts.Delim != "" {
		cfg.delim = []rune(opts.Delim)[0]
	}
	if opts.AllowJaggedRows != nil {
		cfg.allowJaggedRows = *opts.AllowJaggedRows
	}
	if opts.IgnoreUnknownValues != nil {
		cfg.ignoreUnknownValues = *opts.IgnoreUnknownValues
	}
	return cfg
}

func synthesizeColumns(n int) []string {
	cols := make([]string, n)
	for i := range cols {
		cols[i] = fmt.Sprintf("col%d", i+1)
	}
	return cols
}

func validateOptionsForFormat(opts Options, format string) error {
	if opts.Compression != "" {
		if !ast.IsSupportedCompression(opts.Compression) {
			return fmt.Errorf("unsupported compression %q (supported: %s)", opts.Compression, ast.CompressionFormatsList())
		}
		if !ast.IsStreamLoadFormat(format) {
			return fmt.Errorf("compression=%s applies only to csv, json, and jsonl formats", opts.Compression)
		}
	}
	return ast.ValidateCSVOnlyOptionsForFormat(ast.LoadOptions{
		Compression:         opts.Compression,
		Header:              opts.Header,
		Delim:               opts.Delim,
		AllowJaggedRows:     opts.AllowJaggedRows,
		IgnoreUnknownValues: opts.IgnoreUnknownValues,
	}, format, "")
}

func resolveFormatCompression(filename string, opts Options) (format, compression string) {
	format = opts.Format
	if format == "" {
		format = ast.EffectiveFormat(filename, "")
	}
	compression = opts.Compression
	if compression == "" && strings.EqualFold(filepath.Ext(filename), ".gz") {
		compression = "gzip"
	}
	return format, compression
}

type multiReadCloser struct {
	first  io.ReadCloser
	second io.Closer
}

func (m multiReadCloser) Read(p []byte) (int, error) {
	return m.first.Read(p)
}

func (m multiReadCloser) Close() error {
	err1 := m.first.Close()
	err2 := m.second.Close()
	return errors.Join(err1, err2)
}

func openInputReader(filename, compression string) (io.ReadCloser, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("cannot open %s: %w", filename, err)
	}
	wrapped, err := wrapInputReadCloser(f, compression)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("%s %s: %w", compressionOpenAction(compression), filename, err)
	}
	return wrapped, nil
}

func wrapInputReader(r io.Reader, compression string) (io.ReadCloser, error) {
	return wrapInputReadCloser(io.NopCloser(r), compression)
}

func wrapInputReadCloser(r io.ReadCloser, compression string) (io.ReadCloser, error) {
	switch compression {
	case "":
		return r, nil
	case "gzip":
		gr, err := gzip.NewReader(r)
		if err != nil {
			return nil, err
		}
		return multiReadCloser{first: gr, second: r}, nil
	default:
		return nil, fmt.Errorf("unsupported compression %q (supported: %s)", compression, ast.CompressionFormatsList())
	}
}

func compressionOpenAction(compression string) string {
	if compression == "gzip" {
		return "cannot read gzip stream"
	}
	return "cannot open compressed stream"
}

func loadCSV(filename string, cfg csvLoadConfig) (*table.Table, error) {
	f, err := openInputReader(filename, cfg.compression)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return loadCSVReader(f, cfg)
}

func loadCSVPositional(path string, columns []string, cfg csvLoadConfig) (*table.Table, error) {
	f, err := openInputReader(path, cfg.compression)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	return readCSVRows(newCSVReader(f, cfg.delim), columns, cfg, 1)
}

const utf8BOM = "\ufeff"

func stripUTF8BOM(s string) string {
	return strings.TrimPrefix(s, utf8BOM)
}

func isRepeatedHeader(row, columns []string) bool {
	norm := trimmedCSVFields(row)
	if len(norm) != len(columns) {
		return false
	}
	for i := range columns {
		if norm[i] != columns[i] {
			return false
		}
	}
	return true
}

type csvGlobShardKind int

const (
	csvGlobShardData csvGlobShardKind = iota
	csvGlobShardRepeated
	csvGlobShardNewHeader
)

func trimmedCSVFields(row []string) []string {
	out := make([]string, len(row))
	for i, f := range row {
		f = strings.TrimSpace(f)
		if i == 0 {
			f = stripUTF8BOM(f)
		}
		out[i] = f
	}
	return out
}

// isPhysicalBlankCSVLine reports a delimiter-free whitespace/BOM-only line (single empty
// unquoted field). Used only before schema/header is established to skip padding lines.
// Structured records — including comma-only rows like "," — are never treated as blank.
func isPhysicalBlankCSVLine(record []string) bool {
	if len(record) != 1 {
		return false
	}
	for _, col := range trimmedCSVFields(record) {
		if col != "" {
			return false
		}
	}
	return true
}

// readFirstNonBlankCSVRow skips physical blank lines and returns the first structured row.
// empty is true when only blank lines remain until EOF.
func readFirstNonBlankCSVRow(reader *csv.Reader, startRow int) (record []string, rowNum int, empty bool, err error) {
	rowNum = startRow
	for {
		record, err = reader.Read()
		if err == io.EOF {
			return nil, 0, true, nil
		}
		if err != nil {
			return nil, 0, false, fmt.Errorf("error reading CSV row: %w", err)
		}
		if isPhysicalBlankCSVLine(record) {
			rowNum++
			continue
		}
		return record, rowNum, false, nil
	}
}

func csvRowLooksLikeData(cells []string) bool {
	for _, c := range cells {
		if c == "" {
			continue
		}
		if _, err := strconv.ParseInt(c, 10, 64); err == nil {
			return true
		}
		if _, err := strconv.ParseFloat(c, 64); err == nil {
			return true
		}
	}
	return false
}

func looksLikeColumnName(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if i == 0 {
			if r != '_' && !unicode.IsLower(r) {
				return false
			}
			continue
		}
		if r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func isAnchorColumnPermutation(row, anchor []string) bool {
	if len(row) != len(anchor) {
		return false
	}
	counts := make(map[string]int, len(anchor))
	for _, col := range anchor {
		counts[col]++
	}
	for _, col := range row {
		counts[col]--
		if counts[col] < 0 {
			return false
		}
	}
	for _, n := range counts {
		if n != 0 {
			return false
		}
	}
	return true
}

func isExtendedHeaderRow(row, anchor []string) bool {
	if csvRowLooksLikeData(row) {
		return false
	}
	anchorSet := make(map[string]bool, len(anchor))
	for _, col := range anchor {
		anchorSet[col] = true
	}
	overlap := 0
	for _, col := range row {
		if anchorSet[col] {
			overlap++
		}
	}
	if overlap == 0 {
		return false
	}
	for _, col := range row {
		if anchorSet[col] {
			continue
		}
		if !looksLikeColumnName(col) {
			return false
		}
	}
	return true
}

func classifyCSVGlobFirstRow(peek, anchor []string) csvGlobShardKind {
	peekCols := trimmedCSVFields(peek)
	if isRepeatedHeader(peek, anchor) {
		return csvGlobShardRepeated
	}
	if isAnchorColumnPermutation(peekCols, anchor) {
		return csvGlobShardNewHeader
	}
	if csvRowLooksLikeData(peekCols) {
		return csvGlobShardData
	}
	if isExtendedHeaderRow(peekCols, anchor) {
		return csvGlobShardNewHeader
	}
	return csvGlobShardData
}

func loadCSVGlobShard(path string, anchor []string, cfg csvLoadConfig) (*table.Table, error) {
	f, err := openInputReader(path, cfg.compression)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	reader := newCSVReader(f, cfg.delim)

	peekRowNum := 1
	peek, err := reader.Read()
	if err == io.EOF {
		return table.NewTable(append([]string(nil), anchor...)), nil
	}
	if err != nil {
		return nil, fmt.Errorf("error reading CSV row: %w", err)
	}

	if isPhysicalBlankCSVLine(peek) {
		var empty bool
		peek, peekRowNum, empty, err = readFirstNonBlankCSVRow(reader, 2)
		if err != nil {
			return nil, err
		}
		if empty {
			return table.NewTable(append([]string(nil), anchor...)), nil
		}
	}

	switch classifyCSVGlobFirstRow(peek, anchor) {
	case csvGlobShardRepeated:
		return readCSVRows(reader, anchor, cfg, peekRowNum+1)
	case csvGlobShardNewHeader:
		columns := trimmedCSVFields(peek)
		return readCSVRows(reader, columns, cfg, peekRowNum+1)
	default:
		if err := validateCSVRecord(peek, len(anchor), cfg, peekRowNum); err != nil {
			return nil, err
		}
		t := table.NewTable(append([]string(nil), anchor...))
		t.AddRow(csvRowValues(peek, anchor))
		rest, err := readCSVRows(reader, anchor, cfg, peekRowNum+1)
		if err != nil {
			return nil, err
		}
		for row := 0; row < rest.NumRows; row++ {
			vals := make([]table.Value, len(anchor))
			for i, col := range anchor {
				vals[i] = rest.Get(row, col)
			}
			t.AddRow(vals)
		}
		return t, nil
	}
}

func newCSVReader(r io.Reader, delim rune) *csv.Reader {
	reader := csv.NewReader(r)
	reader.TrimLeadingSpace = true
	reader.Comma = delim
	reader.FieldsPerRecord = -1
	return reader
}

func validateCSVRecord(record []string, numColumns int, cfg csvLoadConfig, rowNum int) error {
	n := len(record)
	if n == numColumns {
		return nil
	}
	if n > numColumns {
		if cfg.ignoreUnknownValues {
			return nil
		}
		return fmt.Errorf(
			"csv row %d: expected %d field(s), got %d (%d extra); use with ignore_unknown_values=true to ignore extra columns",
			rowNum, numColumns, n, n-numColumns,
		)
	}
	if cfg.allowJaggedRows {
		return nil
	}
	return fmt.Errorf(
		"csv row %d: expected %d field(s), got %d (%d missing); use with allow_jagged_rows=true to treat missing columns as null",
		rowNum, numColumns, n, numColumns-n,
	)
}

func readCSVRows(reader *csv.Reader, columns []string, cfg csvLoadConfig, startRow int) (*table.Table, error) {
	t := table.NewTable(append([]string(nil), columns...))
	rowNum := startRow
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("error reading CSV row: %w", err)
		}
		if err := validateCSVRecord(record, len(columns), cfg, rowNum); err != nil {
			return nil, err
		}
		t.AddRow(csvRowValues(record, columns))
		rowNum++
	}
	return t, nil
}

func hasNonEmptyColumnName(columns []string) bool {
	for _, col := range columns {
		if col != "" {
			return true
		}
	}
	return false
}

func loadCSVReader(r io.Reader, cfg csvLoadConfig) (*table.Table, error) {
	reader := newCSVReader(r, cfg.delim)

	if len(cfg.columns) == 0 && !cfg.header {
		first, firstRowNum, empty, err := readFirstNonBlankCSVRow(reader, 1)
		if err != nil {
			return nil, err
		}
		if empty {
			return table.NewTable(nil), nil
		}
		cfg.columns = synthesizeColumns(len(first))
		if err := validateCSVRecord(first, len(cfg.columns), cfg, firstRowNum); err != nil {
			return nil, err
		}
		t := table.NewTable(cfg.columns)
		t.AddRow(csvRowValues(first, cfg.columns))
		rest, err := readCSVRows(reader, cfg.columns, cfg, firstRowNum+1)
		if err != nil {
			return nil, err
		}
		for row := 0; row < rest.NumRows; row++ {
			vals := make([]table.Value, len(cfg.columns))
			for i, col := range cfg.columns {
				vals[i] = rest.Get(row, col)
			}
			t.AddRow(vals)
		}
		return t, nil
	}

	if len(cfg.columns) == 0 && cfg.header {
		header, headerRowNum, empty, err := readFirstNonBlankCSVRow(reader, 1)
		if err != nil {
			return nil, err
		}
		if empty {
			return table.NewTable(nil), nil
		}
		cfg.columns = trimmedCSVFields(header)
		t, err := readCSVRows(reader, cfg.columns, cfg, headerRowNum+1)
		if err != nil {
			return nil, err
		}
		return t, nil
	}

	return readCSVRows(reader, cfg.columns, cfg, 2)
}

func csvRowValues(record, columns []string) []table.Value {
	vals := make([]table.Value, len(columns))
	for i := range columns {
		if i < len(record) {
			vals[i] = parseValue(strings.TrimSpace(record[i]))
		} else {
			vals[i] = table.Null()
		}
	}
	return vals
}

// parseValue infers the type of a CSV cell value.
func parseValue(s string) table.Value {
	if s == "" || strings.EqualFold(s, "null") {
		return table.Null()
	}

	// Try integer
	if v, err := strconv.ParseInt(s, 10, 64); err == nil {
		return table.IntVal(v)
	}

	// Try float
	if v, err := strconv.ParseFloat(s, 64); err == nil {
		return table.FloatVal(v)
	}

	// Try boolean
	lower := strings.ToLower(s)
	if lower == "true" {
		return table.BoolVal(true)
	}
	if lower == "false" {
		return table.BoolVal(false)
	}

	return table.StrVal(s)
}

func loadJSON(filename, compression string) (*table.Table, error) {
	f, err := openInputReader(filename, compression)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return loadJSONReader(f)
}

func loadJSONReader(r io.Reader) (*table.Table, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("cannot read JSON: %w", err)
	}

	var records []map[string]interface{}
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, fmt.Errorf("cannot parse JSON: %w (expected array of objects)", err)
	}

	return buildTableFromRecords(records), nil
}

func loadJSONL(filename, compression string) (*table.Table, error) {
	f, err := openInputReader(filename, compression)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return loadJSONLReader(f)
}

func loadJSONLReader(r io.Reader) (*table.Table, error) {
	scanner := bufio.NewScanner(r)
	var records []map[string]interface{}
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var rec map[string]interface{}
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			return nil, fmt.Errorf("invalid JSON on line %d: %w", lineNum, err)
		}
		records = append(records, rec)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading JSONL: %w", err)
	}

	return buildTableFromRecords(records), nil
}

func buildTableFromRecords(records []map[string]interface{}) *table.Table {
	if len(records) == 0 {
		return table.NewTable(nil)
	}

	colSet := make(map[string]bool)
	var columns []string
	for _, rec := range records {
		for k := range rec {
			if !colSet[k] {
				colSet[k] = true
				columns = append(columns, k)
			}
		}
	}

	t := table.NewTable(columns)
	for _, rec := range records {
		vals := make([]table.Value, len(columns))
		for i, col := range columns {
			v, ok := rec[col]
			if !ok || v == nil {
				vals[i] = table.Null()
				continue
			}
			vals[i] = anyToValue(v)
		}
		t.AddRow(vals)
	}

	return t
}

// anyToValue converts any Go value (from JSON, Avro, Parquet generic reader) to a table.Value.
func anyToValue(v interface{}) table.Value {
	switch val := v.(type) {
	case nil:
		return table.Null()
	case bool:
		return table.BoolVal(val)
	case float64:
		// JSON numbers are float64; check if it's actually an integer
		if val == float64(int64(val)) {
			return table.IntVal(int64(val))
		}
		return table.FloatVal(val)
	case string:
		return table.StrVal(val)
	case int32:
		return table.IntVal(int64(val))
	case int64:
		return table.IntVal(val)
	case float32:
		return table.FloatVal(float64(val))
	case []byte:
		return table.StrVal(string(val))
	case []interface{}:
		elems := make([]table.Value, len(val))
		for i, e := range val {
			elems[i] = anyToValue(e)
		}
		return table.ListVal(elems)
	case map[string]interface{}:
		if elem, ok := val["element"]; ok && len(val) == 1 {
			return anyToValue(elem)
		}
		fields := buildRecordFields(val)
		return table.RecordVal(fields)
	default:
		b, _ := json.Marshal(val)
		return table.StrVal(string(b))
	}
}

// buildRecordFields creates a sorted []RecordField from a map.
func buildRecordFields(m map[string]interface{}) []table.RecordField {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fields := make([]table.RecordField, len(keys))
	for i, k := range keys {
		fields[i] = table.RecordField{Name: k, Value: anyToValue(m[k])}
	}
	return fields
}

const parquetColumnOrderMetadataKey = "dq.column_order"

func asMap(v any) (map[string]any, bool) {
	m, ok := v.(map[string]any)
	return m, ok
}

func asSlice(v any) ([]any, bool) {
	s, ok := v.([]any)
	return s, ok
}

func avroValue(v any, schema any, namespace string) table.Value {
	if v == nil {
		return table.Null()
	}
	if s, ok := schema.(string); ok {
		if m, ok := asMap(v); ok && len(m) == 1 {
			for k, inner := range m {
				if k == s {
					return avroValue(inner, s, namespace)
				}
			}
		}
		return anyToValue(v)
	}
	if branches, ok := asSlice(schema); ok {
		if m, ok := asMap(v); ok && len(m) == 1 {
			for k, inner := range m {
				for _, branch := range branches {
					if avroSchemaName(branch, namespace) == k {
						return avroValue(inner, branch, avroTypeNamespace(branch, namespace))
					}
				}
			}
		}
		for _, branch := range branches {
			if avroSchemaName(branch, namespace) != "null" {
				return avroValue(v, branch, avroTypeNamespace(branch, namespace))
			}
		}
		return table.Null()
	}
	if s, ok := asMap(schema); ok {
		typ := s["type"]
		switch ts := typ.(type) {
		case string:
			switch ts {
			case "record":
				return avroRecordValue(v, s, namespace)
			case "array":
				return avroArrayValue(v, s["items"], namespace)
			default:
				return anyToValue(v)
			}
		default:
			if nested, ok := asSlice(typ); ok {
				return avroValue(v, nested, namespace)
			}
			if nested, ok := asMap(typ); ok {
				return avroValue(v, nested, namespace)
			}
			return anyToValue(v)
		}
	}
	return anyToValue(v)
}

func avroTypeNamespace(schema any, parentNamespace string) string {
	schemaMap, ok := asMap(schema)
	if !ok {
		return parentNamespace
	}
	if ns, ok := schemaMap["namespace"].(string); ok {
		return ns
	}
	return parentNamespace
}

func avroSchemaName(schema any, namespace string) string {
	if s, ok := schema.(string); ok {
		return s
	}
	if s, ok := asMap(schema); ok {
		switch typ := s["type"].(type) {
		case string:
			switch typ {
			case "record", "enum", "fixed":
				return avroFullName(s, namespace)
			case "array":
				return "array"
			case "map":
				return "map"
			default:
				return typ
			}
		}
	}
	return ""
}

func avroFullName(schema map[string]any, parentNamespace string) string {
	name, _ := schema["name"].(string)
	if name == "" {
		return ""
	}
	ns := parentNamespace
	if n, ok := schema["namespace"].(string); ok {
		ns = n
	}
	if ns != "" {
		return ns + "." + name
	}
	return name
}

func avroRecordValue(v any, schema map[string]any, namespace string) table.Value {
	rec, ok := asMap(v)
	if !ok {
		return anyToValue(v)
	}
	fieldsRaw, ok := asSlice(schema["fields"])
	if !ok {
		return anyToValue(v)
	}
	recordNamespace := avroTypeNamespace(schema, namespace)
	fields := make([]table.RecordField, 0, len(fieldsRaw))
	for _, fieldRaw := range fieldsRaw {
		field, ok := asMap(fieldRaw)
		if !ok {
			continue
		}
		name, ok := field["name"].(string)
		if !ok {
			continue
		}
		fields = append(fields, table.RecordField{
			Name:  name,
			Value: avroValue(rec[name], field["type"], recordNamespace),
		})
	}
	sort.Slice(fields, func(i, j int) bool { return fields[i].Name < fields[j].Name })
	return table.RecordVal(fields)
}

func avroArrayValue(v any, itemSchema any, namespace string) table.Value {
	items, ok := asSlice(v)
	if !ok {
		return anyToValue(v)
	}
	values := make([]table.Value, len(items))
	for i, item := range items {
		values[i] = avroValue(item, itemSchema, namespace)
	}
	return table.ListVal(values)
}

func loadAvro(filename string) (*table.Table, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("cannot open %s: %w", filename, err)
	}
	defer f.Close()

	ocfr, err := goavro.NewOCFReader(f)
	if err != nil {
		return nil, fmt.Errorf("cannot read Avro OCF from %s: %w", filename, err)
	}

	// Extract column names from the schema
	codec := ocfr.Codec()
	schema := codec.Schema()

	var schemaDef struct {
		Namespace string `json:"namespace"`
		Fields    []struct {
			Name string `json:"name"`
			Type any    `json:"type"`
		} `json:"fields"`
	}
	if err := json.Unmarshal([]byte(schema), &schemaDef); err != nil {
		return nil, fmt.Errorf("cannot parse Avro schema: %w", err)
	}

	columns := make([]string, len(schemaDef.Fields))
	fieldSchemas := make(map[string]any, len(schemaDef.Fields))
	for i, field := range schemaDef.Fields {
		columns[i] = field.Name
		fieldSchemas[field.Name] = field.Type
	}

	t := table.NewTable(columns)

	for ocfr.Scan() {
		datum, err := ocfr.Read()
		if err != nil {
			return nil, fmt.Errorf("error reading Avro record: %w", err)
		}

		rec, ok := datum.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("unexpected Avro record type %T", datum)
		}

		vals := make([]table.Value, len(columns))
		for i, col := range columns {
			v, exists := rec[col]
			if !exists || v == nil {
				vals[i] = table.Null()
				continue
			}
			val := avroValue(v, fieldSchemas[col], schemaDef.Namespace)
			vals[i] = val
		}
		t.AddRow(vals)
	}

	if err := ocfr.Err(); err != nil {
		return nil, fmt.Errorf("error reading Avro file: %w", err)
	}

	return t, nil
}

func loadParquet(filename string) (*table.Table, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("cannot open %s: %w", filename, err)
	}
	defer f.Close()

	// Use NewGenericReader[any]: typeOf[any]() == nil so it uses the file's own schema
	// and Reconstruct populates each row as map[string]any.
	reader := parquet.NewGenericReader[any](f)
	defer reader.Close()

	schema := reader.Schema()
	columns := parquetColumns(schema, reader)
	t := table.NewTable(columns)

	buf := make([]any, 128)
	for {
		n, err := reader.Read(buf)
		for i := 0; i < n; i++ {
			row, ok := buf[i].(map[string]any)
			if !ok {
				return nil, fmt.Errorf("unexpected parquet row type %T", buf[i])
			}
			vals := make([]table.Value, len(columns))
			for j, col := range columns {
				vals[j] = anyToValue(row[col])
			}
			t.AddRow(vals)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("error reading Parquet rows: %w", err)
		}
	}
	return t, nil
}

func parquetColumns(schema *parquet.Schema, reader *parquet.GenericReader[any]) []string {
	schemaNames := make([]string, 0, len(schema.Fields()))
	schemaSet := make(map[string]bool, len(schema.Fields()))
	for _, field := range schema.Fields() {
		name := field.Name()
		schemaNames = append(schemaNames, name)
		schemaSet[name] = true
	}

	if file := reader.File(); file != nil {
		if order, ok := file.Lookup(parquetColumnOrderMetadataKey); ok && order != "" {
			columns := make([]string, 0, len(schemaNames))
			seen := make(map[string]bool, len(schemaNames))
			for _, name := range strings.Split(order, ",") {
				if name == "" || !schemaSet[name] || seen[name] {
					continue
				}
				columns = append(columns, name)
				seen[name] = true
			}
			for _, name := range schemaNames {
				if !seen[name] {
					columns = append(columns, name)
				}
			}
			if len(columns) > 0 {
				return columns
			}
		}
	}

	return schemaNames
}
