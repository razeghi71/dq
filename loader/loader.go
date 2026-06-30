package loader

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/klauspost/compress/zstd"
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
		cfg := jsonConfigFromOptions(opts, filename)
		cfg.compression = compression
		return loadJSON(filename, cfg)
	case "jsonl":
		cfg := jsonConfigFromOptions(opts, filename)
		cfg.compression = compression
		return loadJSONL(filename, cfg)
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
	if resolved == "json" || resolved == "jsonl" {
		return loadGlobJSON(pattern, matches, resolved, opts, compression)
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

	var anchor []string
	var columnSets [][]string
	var groups []csvRowGroup
	for _, path := range matches {
		var (
			partCols []string
			group    csvRowGroup
			err      error
		)
		if len(anchor) == 0 {
			partCols, group, err = collectCSVFileRows(path, cfg)
		} else {
			partCols, group, err = collectCSVGlobShardRows(path, anchor, cfg)
		}
		if err != nil {
			return nil, fmt.Errorf("loading glob %q: loading %q: %w", pattern, path, err)
		}
		if len(anchor) == 0 && hasNonEmptyColumnName(partCols) {
			anchor = append([]string(nil), partCols...)
		}
		if hasNonEmptyColumnName(partCols) {
			columnSets = append(columnSets, partCols)
		}
		groups = append(groups, group)
	}
	columns := table.UnionColumns(columnSets...)
	return materializeCSVGroups(columns, groups, cfg)
}

func loadGlobCSVHeaderless(pattern string, matches []string, cfg csvLoadConfig) (*table.Table, error) {
	var anchor []string
	var groups []csvRowGroup
	for _, path := range matches {
		var (
			partCols []string
			group    csvRowGroup
			err      error
		)
		if len(anchor) == 0 {
			partCols, group, err = collectCSVFileRows(path, cfg)
			if err != nil {
				return nil, fmt.Errorf("loading glob %q: loading %q: %w", pattern, path, err)
			}
			if hasNonEmptyColumnName(partCols) {
				anchor = append([]string(nil), partCols...)
			}
		} else {
			partCols = append([]string(nil), anchor...)
			group, err = collectCSVPositionalRows(path, anchor, cfg)
			if err != nil {
				return nil, fmt.Errorf("loading glob %q: loading %q: %w", pattern, path, err)
			}
		}
		groups = append(groups, group)
	}
	return materializeCSVGroups(anchor, groups, cfg)
}

func collectCSVFileRows(path string, cfg csvLoadConfig) ([]string, csvRowGroup, error) {
	f, err := openInputReader(path, cfg.compression)
	if err != nil {
		return nil, csvRowGroup{}, err
	}
	defer f.Close()
	cfg.source = path
	return collectCSVReaderRows(f, cfg)
}

func collectCSVPositionalRows(path string, columns []string, cfg csvLoadConfig) (csvRowGroup, error) {
	f, err := openInputReader(path, cfg.compression)
	if err != nil {
		return csvRowGroup{}, err
	}
	defer f.Close()
	cfg.source = path
	rows, err := collectCSVRows(newCSVReader(f, cfg.delim), columns, cfg, 1)
	if err != nil {
		return csvRowGroup{}, err
	}
	return csvRowGroup{columns: append([]string(nil), columns...), source: path, rows: rows}, nil
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
			return nil, fmt.Errorf("%s: %w", compressionOpenAction(opts.Compression), err)
		}
		defer wrapped.Close()
		r = wrapped
	}
	switch opts.Format {
	case "csv":
		return loadCSVReader(r, csvConfigFromOptions(opts, nil))
	case "json":
		return loadJSONReader(r, jsonConfigFromOptions(opts, ""))
	case "jsonl":
		return loadJSONLReader(r, jsonConfigFromOptions(opts, ""))
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
	inferRows           int
	maxBadRecords       int
	source              string
}

type jsonLoadConfig struct {
	compression   string
	inferRows     int
	maxBadRecords int
	source        string
}

func jsonConfigFromOptions(opts Options, source string) jsonLoadConfig {
	return jsonLoadConfig{
		compression:   opts.Compression,
		inferRows:     opts.InferRows,
		maxBadRecords: opts.MaxBadRecords,
		source:        source,
	}
}

func csvConfigFromOptions(opts Options, columns []string) csvLoadConfig {
	cfg := csvLoadConfig{
		columns:     columns,
		header:      true,
		delim:       ',',
		compression: opts.Compression,
		inferRows:   opts.InferRows,
	}
	cfg.maxBadRecords = opts.MaxBadRecords
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

func validateCSVHeaderColumns(columns []string, source string) error {
	seen := make(map[string]struct{}, len(columns))
	for _, col := range columns {
		if _, ok := seen[col]; ok {
			if source != "" {
				return fmt.Errorf("%s: csv header: duplicate column name %q", source, col)
			}
			return fmt.Errorf("csv header: duplicate column name %q", col)
		}
		seen[col] = struct{}{}
	}
	return nil
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
	return ast.ValidateLoadOptionsForFormat(ast.LoadOptions{
		Compression:         opts.Compression,
		Header:              opts.Header,
		Delim:               opts.Delim,
		AllowJaggedRows:     opts.AllowJaggedRows,
		IgnoreUnknownValues: opts.IgnoreUnknownValues,
		InferRows:           intPtrIfSet(opts.InferRows, opts.InferRowsSet || opts.InferRows != defaultInferRows),
		MaxBadRecords:       intPtrIfSet(opts.MaxBadRecords, opts.MaxBadRecordsSet || opts.MaxBadRecords != 0),
	}, format, "")
}

func intPtrIfSet(v int, set bool) *int {
	if !set {
		return nil
	}
	return &v
}

func resolveFormatCompression(filename string, opts Options) (format, compression string) {
	format = opts.Format
	if format == "" {
		format = ast.EffectiveFormat(filename, "")
	}
	compression = ast.EffectiveCompression(filename, opts.Compression)
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

type errorLabelReadCloser struct {
	r     io.ReadCloser
	label string
}

func (e errorLabelReadCloser) Read(p []byte) (int, error) {
	n, err := e.r.Read(p)
	if err != nil && !errors.Is(err, io.EOF) {
		return n, fmt.Errorf("%s stream: %w", e.label, err)
	}
	return n, err
}

func (e errorLabelReadCloser) Close() error {
	return e.r.Close()
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
	case "zstd":
		zr, err := zstd.NewReader(r)
		if err != nil {
			return nil, err
		}
		labeled := errorLabelReadCloser{r: zr.IOReadCloser(), label: "zstd"}
		return multiReadCloser{first: labeled, second: r}, nil
	case "deflate":
		zr, err := zlib.NewReader(r)
		if err != nil {
			return nil, err
		}
		labeled := errorLabelReadCloser{r: zr, label: "deflate/zlib"}
		return multiReadCloser{first: labeled, second: r}, nil
	default:
		return nil, fmt.Errorf("unsupported compression %q (supported: %s)", compression, ast.CompressionFormatsList())
	}
}

func compressionOpenAction(compression string) string {
	switch compression {
	case "gzip":
		return "cannot read gzip stream"
	case "zstd":
		return "cannot read zstd stream"
	case "deflate":
		return "cannot read deflate/zlib stream"
	default:
		return "cannot open compressed stream"
	}
}

func loadCSV(filename string, cfg csvLoadConfig) (*table.Table, error) {
	f, err := openInputReader(filename, cfg.compression)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	cfg.source = filename
	return loadCSVReader(f, cfg)
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

func collectCSVGlobShardRows(path string, anchor []string, cfg csvLoadConfig) ([]string, csvRowGroup, error) {
	f, err := openInputReader(path, cfg.compression)
	if err != nil {
		return nil, csvRowGroup{}, err
	}
	defer f.Close()
	cfg.source = path

	reader := newCSVReader(f, cfg.delim)

	peekRowNum := 1
	peek, err := reader.Read()
	if err == io.EOF {
		cols := append([]string(nil), anchor...)
		return cols, csvRowGroup{columns: cols, source: path}, nil
	}
	if err != nil {
		return nil, csvRowGroup{}, fmt.Errorf("error reading CSV row: %w", err)
	}

	if isPhysicalBlankCSVLine(peek) {
		var empty bool
		peek, peekRowNum, empty, err = readFirstNonBlankCSVRow(reader, 2)
		if err != nil {
			return nil, csvRowGroup{}, err
		}
		if empty {
			cols := append([]string(nil), anchor...)
			return cols, csvRowGroup{columns: cols, source: path}, nil
		}
	}

	switch classifyCSVGlobFirstRow(peek, anchor) {
	case csvGlobShardRepeated:
		rows, err := collectCSVRows(reader, anchor, cfg, peekRowNum+1)
		cols := append([]string(nil), anchor...)
		return cols, csvRowGroup{columns: cols, source: path, rows: rows}, err
	case csvGlobShardNewHeader:
		columns := trimmedCSVFields(peek)
		rows, err := collectCSVRows(reader, columns, cfg, peekRowNum+1)
		return columns, csvRowGroup{columns: columns, source: path, rows: rows}, err
	default:
		if err := validateCSVRecord(peek, len(anchor), cfg, peekRowNum); err != nil {
			return nil, csvRowGroup{}, err
		}
		rows := []csvRawRow{newCSVRawRow(peek, peekRowNum)}
		rest, err := collectCSVRows(reader, anchor, cfg, peekRowNum+1)
		if err != nil {
			return nil, csvRowGroup{}, err
		}
		rows = append(rows, rest...)
		cols := append([]string(nil), anchor...)
		return cols, csvRowGroup{columns: cols, source: path, rows: rows}, nil
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

type csvRawRow struct {
	record []string
	rowNum int
}

type csvRowGroup struct {
	columns []string
	source  string
	rows    []csvRawRow
}

func collectCSVRows(reader *csv.Reader, columns []string, cfg csvLoadConfig, startRow int) ([]csvRawRow, error) {
	var rows []csvRawRow
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
		rows = append(rows, newCSVRawRow(record, rowNum))
		rowNum++
	}
	return rows, nil
}

func newCSVRawRow(record []string, rowNum int) csvRawRow {
	return csvRawRow{record: record, rowNum: rowNum}
}

func materializeCSVGroups(columns []string, groups []csvRowGroup, cfg csvLoadConfig) (*table.Table, error) {
	if err := validateCSVHeaderColumns(columns, cfg.source); err != nil {
		return nil, err
	}
	for _, group := range groups {
		if err := validateCSVHeaderColumns(group.columns, group.source); err != nil {
			return nil, err
		}
	}
	types := inferCSVColumnTypes(columns, groups, cfg.inferRows)
	totalRows := csvRowGroupCount(groups)
	nullableAll := csvCollectedInferenceNeedsConservativeNullability(cfg.inferRows, totalRows)
	schemas := csvSchemasFromTypes(columns, types, csvNullableColumns(columns, groups), nullableAll)
	mat, err := csvMaterializationFor(columns, types, schemas, table.AllColumns(), cfg.source)
	if err != nil {
		return nil, err
	}
	t := table.NewTableWithSchemas(mat.columns, mat.schemas)
	badRecords := 0
	for _, group := range groups {
		mapping := csvColumnMapping(columns, group.columns)
		for _, row := range group.rows {
			if err := addCSVTypedRow(t, row, mapping, group.source, columns, types, mat, cfg, &badRecords); err != nil {
				return nil, err
			}
		}
	}
	return t, nil
}

type csvMaterialization struct {
	columns          []string
	schemas          []*table.TypeDescriptor
	positionByColumn []int
}

func csvMaterializationFor(columns []string, types []table.ValueType, schemas []*table.TypeDescriptor, projection table.ColumnSelection, source string) (csvMaterialization, error) {
	if schemas == nil {
		schemas = csvSchemasFromTypes(columns, types, nil, false)
	}
	if projection.IsAll() {
		positionByColumn := make([]int, len(columns))
		for i := range columns {
			positionByColumn[i] = i
		}
		return csvMaterialization{
			columns:          append([]string(nil), columns...),
			schemas:          append([]*table.TypeDescriptor(nil), schemas...),
			positionByColumn: positionByColumn,
		}, nil
	}

	projectColumns := projection.Names()
	index := make(map[string]int, len(columns))
	for i, col := range columns {
		index[col] = i
	}
	seen := make(map[string]bool, len(projectColumns))
	outCols := make([]string, len(projectColumns))
	outSchemas := make([]*table.TypeDescriptor, len(projectColumns))
	positionByColumn := make([]int, len(columns))
	for i := range positionByColumn {
		positionByColumn[i] = -1
	}
	for outIdx, col := range projectColumns {
		if seen[col] {
			return csvMaterialization{}, fmt.Errorf("%s: projected column %q requested more than once", sourcePrefix(source), col)
		}
		seen[col] = true
		srcIdx, ok := index[col]
		if !ok {
			return csvMaterialization{}, fmt.Errorf("%s: projected column %q not found", sourcePrefix(source), col)
		}
		outCols[outIdx] = col
		if srcIdx < len(schemas) {
			outSchemas[outIdx] = schemas[srcIdx]
		}
		positionByColumn[srcIdx] = outIdx
	}
	return csvMaterialization{
		columns:          outCols,
		schemas:          outSchemas,
		positionByColumn: positionByColumn,
	}, nil
}

func csvRowGroupCount(groups []csvRowGroup) int {
	total := 0
	for _, group := range groups {
		total += len(group.rows)
	}
	return total
}

func csvCollectedInferenceNeedsConservativeNullability(inferRows, totalRows int) bool {
	switch {
	case totalRows == 0:
		return false
	case inferRows < 0:
		return false
	case inferRows == 0:
		return true
	default:
		return inferRows < totalRows
	}
}

func csvStreamingInferenceNeedsConservativeNullability(inferRows, totalRows int, sampleExhausted bool) bool {
	switch {
	case totalRows == 0:
		return false
	case inferRows < 0:
		return false
	case inferRows == 0:
		return true
	default:
		return !sampleExhausted
	}
}

func sourcePrefix(source string) string {
	if source == "" {
		return "source"
	}
	return source
}

// CSV inference intentionally parallels parseValue and table widening without
// reusing table.Append: inference chooses a fixed load schema first, then
// materialization strictly converts every semantically read post-inference cell
// to that schema.
func inferCSVColumnTypes(columns []string, groups []csvRowGroup, inferRows int) []table.ValueType {
	types := make([]table.ValueType, len(columns))
	if inferRows == 0 {
		for i := range types {
			types[i] = table.TypeString
		}
		return types
	}
	sampled := 0
	for _, group := range groups {
		mapping := csvColumnMapping(columns, group.columns)
		for _, row := range group.rows {
			if inferRows > 0 && sampled >= inferRows {
				break
			}
			applyCSVInferenceRow(types, mapping, row)
			sampled++
		}
		if inferRows > 0 && sampled >= inferRows {
			break
		}
	}
	for i := range types {
		if types[i] == table.TypeNull {
			types[i] = table.TypeString
		}
	}
	return types
}

func applyCSVInferenceRow(types []table.ValueType, mapping []int, row csvRawRow) {
	for srcIdx, dst := range mapping {
		if dst < 0 || srcIdx >= len(row.record) {
			continue
		}
		if types[dst] == table.TypeString {
			continue
		}
		v := rowValueForInference(row, srcIdx)
		if v.Type == table.TypeNull {
			continue
		}
		types[dst] = csvWidenInferredType(types[dst], v.Type)
	}
}

func rowValueForInference(row csvRawRow, srcIdx int) table.Value {
	return parseValue(strings.TrimSpace(row.record[srcIdx]))
}

func csvWidenInferredType(existing, incoming table.ValueType) table.ValueType {
	if existing == table.TypeNull {
		return incoming
	}
	if existing == incoming {
		return existing
	}
	if (existing == table.TypeInt && incoming == table.TypeFloat) || (existing == table.TypeFloat && incoming == table.TypeInt) {
		return table.TypeFloat
	}
	return table.TypeString
}

func addCSVTypedRow(t *table.Table, row csvRawRow, mapping []int, source string, columns []string, types []table.ValueType, mat csvMaterialization, cfg csvLoadConfig, badRecords *int) error {
	vals, err := csvTypedRowValues(row, mapping, source, columns, types, mat)
	if err != nil {
		(*badRecords)++
		if *badRecords > cfg.maxBadRecords {
			return err
		}
		return nil
	}
	t.AddRow(vals)
	return nil
}

func csvTypedRowValues(row csvRawRow, mapping []int, source string, columns []string, types []table.ValueType, mat csvMaterialization) ([]table.Value, error) {
	vals := make([]table.Value, len(mat.columns))
	for srcIdx, dst := range mapping {
		if dst < 0 || srcIdx >= len(row.record) {
			continue
		}
		if dst >= len(mat.positionByColumn) {
			continue
		}
		outIdx := mat.positionByColumn[dst]
		if outIdx < 0 {
			continue
		}
		cell := strings.TrimSpace(row.record[srcIdx])
		v, err := csvCellValueAsType(cell, types[dst])
		if err != nil {
			return nil, csvTypeError(row, source, columns[dst], types[dst], cell)
		}
		vals[outIdx] = v
	}
	return vals, nil
}

func csvCellValueAsType(cell string, typ table.ValueType) (table.Value, error) {
	return parseCSVCellAsType(cell, typ)
}

func csvColumnMapping(columns, rowColumns []string) []int {
	mapping := make([]int, len(rowColumns))
	if sameColumns(columns, rowColumns) {
		for i := range rowColumns {
			mapping[i] = i
		}
		return mapping
	}
	index := make(map[string]int, len(columns))
	for i, col := range columns {
		index[col] = i
	}
	for i, col := range rowColumns {
		dst, ok := index[col]
		if !ok {
			mapping[i] = -1
		} else {
			mapping[i] = dst
		}
	}
	return mapping
}

func sameColumns(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func parseCSVCellAsType(cell string, typ table.ValueType) (table.Value, error) {
	if isCSVNull(cell) {
		return table.Null(), nil
	}
	switch typ {
	case table.TypeString:
		return table.StrVal(cell), nil
	case table.TypeInt:
		v, err := strconv.ParseInt(cell, 10, 64)
		if err != nil {
			return table.Null(), err
		}
		return table.IntVal(v), nil
	case table.TypeFloat:
		v, err := strconv.ParseFloat(cell, 64)
		if err != nil {
			return table.Null(), err
		}
		return table.FloatVal(v), nil
	case table.TypeBool:
		if strings.EqualFold(cell, "true") {
			return table.BoolVal(true), nil
		}
		if strings.EqualFold(cell, "false") {
			return table.BoolVal(false), nil
		}
		return table.Null(), fmt.Errorf("invalid bool")
	default:
		return parseValue(cell), nil
	}
}

func csvTypeError(row csvRawRow, source, column string, typ table.ValueType, value string) error {
	loc := fmt.Sprintf("csv row %d", row.rowNum)
	if source != "" {
		loc = fmt.Sprintf("%s: %s", source, loc)
	}
	return fmt.Errorf("%s: column %q expected %s, got %q", loc, column, csvTypeName(typ), value)
}

func csvTypeName(typ table.ValueType) string {
	switch typ {
	case table.TypeInt:
		return "int"
	case table.TypeFloat:
		return "float"
	case table.TypeString:
		return "string"
	case table.TypeBool:
		return "bool"
	default:
		return "null"
	}
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
	if cfg.inferRows == -1 {
		columns, group, err := collectCSVReaderRows(r, cfg)
		if err != nil {
			return nil, err
		}
		return materializeCSVGroups(columns, []csvRowGroup{group}, cfg)
	}
	return loadCSVReaderStreaming(r, cfg)
}

type csvInferenceWindow struct {
	sampleRows      []csvRawRow
	pendingRows     []csvRawRow
	nextRow         int
	sampleExhausted bool
}

func readCSVInferenceWindow(reader *csv.Reader, columns []string, buffered []csvRawRow, startRow int, cfg csvLoadConfig) (csvInferenceWindow, error) {
	window := csvInferenceWindow{
		sampleRows: append([]csvRawRow(nil), buffered...),
		nextRow:    startRow,
	}

	switch {
	case cfg.inferRows < 0:
		for {
			record, err := reader.Read()
			if err == io.EOF {
				window.sampleExhausted = true
				return window, nil
			}
			if err != nil {
				return csvInferenceWindow{}, fmt.Errorf("error reading CSV row: %w", err)
			}
			if err := validateCSVRecord(record, len(columns), cfg, window.nextRow); err != nil {
				return csvInferenceWindow{}, err
			}
			window.sampleRows = append(window.sampleRows, newCSVRawRow(record, window.nextRow))
			window.nextRow++
		}
	case cfg.inferRows > 0:
		initialCap := csvInferenceInitialCapacity(cfg.inferRows, len(window.sampleRows))
		if cap(window.sampleRows) < initialCap {
			prealloc := make([]csvRawRow, 0, initialCap)
			prealloc = append(prealloc, window.sampleRows...)
			window.sampleRows = prealloc
		}
		for len(window.sampleRows) < cfg.inferRows {
			record, err := reader.Read()
			if err == io.EOF {
				window.sampleExhausted = true
				return window, nil
			}
			if err != nil {
				return csvInferenceWindow{}, fmt.Errorf("error reading CSV row: %w", err)
			}
			if err := validateCSVRecord(record, len(columns), cfg, window.nextRow); err != nil {
				return csvInferenceWindow{}, err
			}
			window.sampleRows = append(window.sampleRows, newCSVRawRow(record, window.nextRow))
			window.nextRow++
		}
	}

	record, err := reader.Read()
	if err == io.EOF {
		window.sampleExhausted = true
		return window, nil
	}
	if err != nil {
		return csvInferenceWindow{}, fmt.Errorf("error reading CSV row: %w", err)
	}
	if err := validateCSVRecord(record, len(columns), cfg, window.nextRow); err != nil {
		return csvInferenceWindow{}, err
	}
	window.pendingRows = append(window.pendingRows, newCSVRawRow(record, window.nextRow))
	window.nextRow++
	return window, nil
}

func csvInferenceInitialCapacity(inferRows, bufferedRows int) int {
	if inferRows <= bufferedRows {
		return bufferedRows
	}
	const maxInitialCapacity = 1024
	if inferRows < maxInitialCapacity {
		return inferRows
	}
	return maxInitialCapacity
}

func loadCSVReaderStreaming(r io.Reader, cfg csvLoadConfig) (*table.Table, error) {
	reader := newCSVReader(r, cfg.delim)
	columns, buffered, startRow, empty, err := prepareCSVReader(reader, cfg)
	if err != nil {
		return nil, err
	}
	if empty {
		return table.NewTable(nil), nil
	}
	if err := validateCSVHeaderColumns(columns, cfg.source); err != nil {
		return nil, err
	}

	window, err := readCSVInferenceWindow(reader, columns, buffered, startRow, cfg)
	if err != nil {
		return nil, err
	}

	group := csvRowGroup{columns: append([]string(nil), columns...), source: cfg.source, rows: window.sampleRows}
	types := inferCSVColumnTypes(columns, []csvRowGroup{group}, cfg.inferRows)
	totalRows := len(window.sampleRows) + len(window.pendingRows)
	nullableAll := csvStreamingInferenceNeedsConservativeNullability(cfg.inferRows, totalRows, window.sampleExhausted)
	schemas := csvSchemasFromTypes(columns, types, csvNullableColumns(columns, []csvRowGroup{group}), nullableAll)
	mat, err := csvMaterializationFor(columns, types, schemas, table.AllColumns(), cfg.source)
	if err != nil {
		return nil, err
	}
	t := table.NewTableWithSchemas(mat.columns, mat.schemas)
	mapping := csvColumnMapping(columns, columns)
	badRecords := 0
	for _, row := range window.sampleRows {
		if err := addCSVTypedRow(t, row, mapping, cfg.source, columns, types, mat, cfg, &badRecords); err != nil {
			return nil, err
		}
	}
	for _, row := range window.pendingRows {
		if err := addCSVTypedRow(t, row, mapping, cfg.source, columns, types, mat, cfg, &badRecords); err != nil {
			return nil, err
		}
	}

	rowNum := window.nextRow
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
		row := csvRawRow{record: record, rowNum: rowNum}
		if err := addCSVTypedRow(t, row, mapping, cfg.source, columns, types, mat, cfg, &badRecords); err != nil {
			return nil, err
		}
		rowNum++
	}
	return t, nil
}

func prepareCSVReader(reader *csv.Reader, cfg csvLoadConfig) (columns []string, buffered []csvRawRow, startRow int, empty bool, err error) {
	if len(cfg.columns) == 0 && !cfg.header {
		first, firstRowNum, empty, err := readFirstNonBlankCSVRow(reader, 1)
		if err != nil {
			return nil, nil, 0, false, err
		}
		if empty {
			return nil, nil, 0, true, nil
		}
		columns = synthesizeColumns(len(first))
		if err := validateCSVRecord(first, len(columns), cfg, firstRowNum); err != nil {
			return nil, nil, 0, false, err
		}
		buffered = []csvRawRow{newCSVRawRow(first, firstRowNum)}
		return columns, buffered, firstRowNum + 1, false, nil
	}

	if len(cfg.columns) == 0 && cfg.header {
		header, headerRowNum, empty, err := readFirstNonBlankCSVRow(reader, 1)
		if err != nil {
			return nil, nil, 0, false, err
		}
		if empty {
			return nil, nil, 0, true, nil
		}
		return trimmedCSVFields(header), nil, headerRowNum + 1, false, nil
	}

	return append([]string(nil), cfg.columns...), nil, 2, false, nil
}

func collectCSVReaderRows(r io.Reader, cfg csvLoadConfig) ([]string, csvRowGroup, error) {
	reader := newCSVReader(r, cfg.delim)

	if len(cfg.columns) == 0 && !cfg.header {
		first, firstRowNum, empty, err := readFirstNonBlankCSVRow(reader, 1)
		if err != nil {
			return nil, csvRowGroup{}, err
		}
		if empty {
			return nil, csvRowGroup{source: cfg.source}, nil
		}
		cfg.columns = synthesizeColumns(len(first))
		if err := validateCSVRecord(first, len(cfg.columns), cfg, firstRowNum); err != nil {
			return nil, csvRowGroup{}, err
		}
		rows := []csvRawRow{newCSVRawRow(first, firstRowNum)}
		rest, err := collectCSVRows(reader, cfg.columns, cfg, firstRowNum+1)
		if err != nil {
			return nil, csvRowGroup{}, err
		}
		rows = append(rows, rest...)
		cols := append([]string(nil), cfg.columns...)
		return cols, csvRowGroup{columns: cols, source: cfg.source, rows: rows}, nil
	}

	if len(cfg.columns) == 0 && cfg.header {
		header, headerRowNum, empty, err := readFirstNonBlankCSVRow(reader, 1)
		if err != nil {
			return nil, csvRowGroup{}, err
		}
		if empty {
			return nil, csvRowGroup{source: cfg.source}, nil
		}
		cfg.columns = trimmedCSVFields(header)
		rows, err := collectCSVRows(reader, cfg.columns, cfg, headerRowNum+1)
		if err != nil {
			return nil, csvRowGroup{}, err
		}
		cols := append([]string(nil), cfg.columns...)
		return cols, csvRowGroup{columns: cols, source: cfg.source, rows: rows}, nil
	}

	rows, err := collectCSVRows(reader, cfg.columns, cfg, 2)
	if err != nil {
		return nil, csvRowGroup{}, err
	}
	cols := append([]string(nil), cfg.columns...)
	return cols, csvRowGroup{columns: cols, source: cfg.source, rows: rows}, nil
}

// parseValue infers the type of a CSV cell value.
func parseValue(s string) table.Value {
	if isCSVNull(s) {
		return table.Null()
	}
	switch s[0] {
	case 't', 'T':
		if strings.EqualFold(s, "true") {
			return table.BoolVal(true)
		}
	case 'f', 'F':
		if strings.EqualFold(s, "false") {
			return table.BoolVal(false)
		}
	}
	if csvNumericCandidate(s) {
		if csvIntegerCandidate(s) {
			if v, err := strconv.ParseInt(s, 10, 64); err == nil {
				return table.IntVal(v)
			}
		}
		if v, err := strconv.ParseFloat(s, 64); err == nil {
			return table.FloatVal(v)
		}
	}

	return table.StrVal(s)
}

func isCSVNull(s string) bool {
	return s == "" || (len(s) == 4 && (s[0] == 'n' || s[0] == 'N') && strings.EqualFold(s, "null"))
}

func csvNumericCandidate(s string) bool {
	if s == "" {
		return false
	}
	switch s[0] {
	case '+', '-', '.', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		return true
	case 'n', 'N', 'i', 'I':
		return strings.EqualFold(s, "nan") || strings.EqualFold(s, "inf") || strings.EqualFold(s, "infinity")
	default:
		return false
	}
}

func csvIntegerCandidate(s string) bool {
	start := 0
	if s[0] == '+' || s[0] == '-' {
		start = 1
	}
	if start == len(s) {
		return false
	}
	for _, r := range s[start:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

type jsonLogicalRecord struct {
	fields []table.RecordField
	loc    string
	source string
	err    error
}

type jsonSchemaInference struct {
	columns  []string
	schemas  []*table.TypeDescriptor
	index    map[string]int
	goodRows int
}

func loadGlobJSON(pattern string, matches []string, format string, opts Options, compression string) (*table.Table, error) {
	var records []jsonLogicalRecord
	for _, path := range matches {
		cfg := jsonConfigFromOptions(opts, path)
		cfg.compression = compression
		var part []jsonLogicalRecord
		var err error
		switch format {
		case "json":
			part, err = collectJSONFileRecords(path, cfg)
		case "jsonl":
			part, err = collectJSONLFileRecords(path, cfg)
		default:
			return nil, fmt.Errorf("unsupported format %q (supported: json, jsonl)", format)
		}
		if err != nil {
			return nil, fmt.Errorf("loading glob %q: loading %q: %w", pattern, path, err)
		}
		records = append(records, part...)
	}
	cfg := jsonConfigFromOptions(opts, pattern)
	return buildTableFromJSONRecords(records, cfg)
}

func loadJSON(filename string, cfg jsonLoadConfig) (*table.Table, error) {
	records, err := collectJSONFileRecords(filename, cfg)
	if err != nil {
		return nil, err
	}
	return buildTableFromJSONRecords(records, cfg)
}

func collectJSONFileRecords(filename string, cfg jsonLoadConfig) ([]jsonLogicalRecord, error) {
	f, err := openInputReader(filename, cfg.compression)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return collectJSONRecords(f, filename)
}

func loadJSONReader(r io.Reader, cfg jsonLoadConfig) (*table.Table, error) {
	records, err := collectJSONRecords(r, cfg.source)
	if err != nil {
		return nil, err
	}
	return buildTableFromJSONRecords(records, cfg)
}

func collectJSONRecords(r io.Reader, source string) ([]jsonLogicalRecord, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("cannot read JSON: %w", err)
	}

	var elems []interface{}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&elems); err != nil {
		return nil, fmt.Errorf("cannot parse JSON: %w (expected array of objects)", err)
	}
	if err := requireJSONDecoderEOF(dec); err != nil {
		return nil, fmt.Errorf("cannot parse JSON: %w (expected array of objects)", err)
	}
	if elems == nil {
		return nil, fmt.Errorf("cannot parse JSON: expected array of objects")
	}

	records := make([]jsonLogicalRecord, len(elems))
	for i, elem := range elems {
		loc := fmt.Sprintf("row %d", i+1)
		rec, ok := elem.(map[string]interface{})
		if !ok || rec == nil {
			records[i] = jsonLogicalRecord{loc: loc, source: source, err: fmt.Errorf("expected JSON object")}
			continue
		}
		fields, err := buildJSONRecordFields(rec)
		if err != nil {
			records[i] = jsonLogicalRecord{loc: loc, source: source, err: err}
			continue
		}
		records[i] = jsonLogicalRecord{fields: fields, loc: loc, source: source}
	}
	return records, nil
}

func loadJSONL(filename string, cfg jsonLoadConfig) (*table.Table, error) {
	records, err := collectJSONLFileRecords(filename, cfg)
	if err != nil {
		return nil, err
	}
	return buildTableFromJSONRecords(records, cfg)
}

func collectJSONLFileRecords(filename string, cfg jsonLoadConfig) ([]jsonLogicalRecord, error) {
	f, err := openInputReader(filename, cfg.compression)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return collectJSONLRecords(f, filename)
}

func loadJSONLReader(r io.Reader, cfg jsonLoadConfig) (*table.Table, error) {
	records, err := collectJSONLRecords(r, cfg.source)
	if err != nil {
		return nil, err
	}
	return buildTableFromJSONRecords(records, cfg)
}

func collectJSONLRecords(r io.Reader, source string) ([]jsonLogicalRecord, error) {
	scanner := newJSONLScanner(r)
	var records []jsonLogicalRecord
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var value interface{}
		dec := json.NewDecoder(strings.NewReader(line))
		dec.UseNumber()
		if err := dec.Decode(&value); err != nil {
			records = append(records, jsonLogicalRecord{
				loc:    fmt.Sprintf("line %d", lineNum),
				source: source,
				err:    fmt.Errorf("invalid JSON: %w", err),
			})
			continue
		}
		if err := requireJSONDecoderEOF(dec); err != nil {
			records = append(records, jsonLogicalRecord{
				loc:    fmt.Sprintf("line %d", lineNum),
				source: source,
				err:    fmt.Errorf("invalid JSON: %w", err),
			})
			continue
		}
		rec, ok := value.(map[string]interface{})
		if !ok || rec == nil {
			records = append(records, jsonLogicalRecord{
				loc:    fmt.Sprintf("line %d", lineNum),
				source: source,
				err:    fmt.Errorf("expected JSON object"),
			})
			continue
		}
		fields, err := buildJSONRecordFields(rec)
		if err != nil {
			records = append(records, jsonLogicalRecord{
				loc:    fmt.Sprintf("line %d", lineNum),
				source: source,
				err:    err,
			})
			continue
		}
		records = append(records, jsonLogicalRecord{
			fields: fields,
			loc:    fmt.Sprintf("line %d", lineNum),
			source: source,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading JSONL: %w", err)
	}

	return records, nil
}

func newJSONLScanner(r io.Reader) *bufio.Scanner {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 64*1024*1024)
	return scanner
}

func requireJSONDecoderEOF(dec *json.Decoder) error {
	var extra interface{}
	err := dec.Decode(&extra)
	if err == io.EOF {
		return nil
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("trailing JSON value after first value")
}

func buildTableFromJSONRecords(records []jsonLogicalRecord, cfg jsonLoadConfig) (*table.Table, error) {
	if len(records) == 0 {
		return table.NewTable(nil), nil
	}

	state := jsonSchemaInference{index: map[string]int{}}
	skip := make([]bool, len(records))
	inferredGood := make([]bool, len(records))
	badRecords := 0
	inferSeen := 0
	for i, rec := range records {
		if cfg.inferRows >= 0 && inferSeen >= cfg.inferRows {
			break
		}
		inferSeen++
		if rec.err != nil {
			if err := countJSONBadRecord(rec, i, rec.err, cfg.maxBadRecords, &badRecords); err != nil {
				return nil, err
			}
			skip[i] = true
			continue
		}
		var err error
		if cfg.maxBadRecords == 0 {
			err = state.inferRecordInPlace(rec.fields)
		} else {
			err = state.inferRecord(rec.fields)
		}
		if err != nil {
			if err := countJSONBadRecord(rec, i, err, cfg.maxBadRecords, &badRecords); err != nil {
				return nil, err
			}
			skip[i] = true
			continue
		}
		inferredGood[i] = true
	}

	if jsonInferenceNeedsConservativeNullability(cfg.inferRows, inferSeen, len(records)) {
		state.schemas = deepNullableSchemas(state.schemas)
	}
	t := table.NewTableWithSchemas(state.columns, state.schemas)
	for i, rec := range records {
		if skip[i] {
			continue
		}
		if rec.err != nil {
			if err := countJSONBadRecord(rec, i, rec.err, cfg.maxBadRecords, &badRecords); err != nil {
				return nil, err
			}
			continue
		}
		vals, err := state.recordValues(rec.fields, !inferredGood[i])
		if err == nil {
			err = t.AddRowTyped(vals)
		}
		if err != nil {
			if err := countJSONBadRecord(rec, i, err, cfg.maxBadRecords, &badRecords); err != nil {
				return nil, err
			}
			continue
		}
	}

	return t, nil
}

func jsonInferenceNeedsConservativeNullability(inferRows, inferSeen, totalRecords int) bool {
	if totalRecords == 0 || inferRows < 0 {
		return false
	}
	return inferSeen < totalRecords
}

func deepNullableSchemas(schemas []*table.TypeDescriptor) []*table.TypeDescriptor {
	out := make([]*table.TypeDescriptor, len(schemas))
	for i, schema := range schemas {
		out[i] = table.WithDeepNullable(schema)
	}
	return out
}

func (s *jsonSchemaInference) inferRecord(fields []table.RecordField) error {
	nextColumns := append([]string(nil), s.columns...)
	nextSchemas := append([]*table.TypeDescriptor(nil), s.schemas...)
	nextIndex := make(map[string]int, len(s.index)+len(fields))
	for k, v := range s.index {
		nextIndex[k] = v
	}

	for i, col := range s.columns {
		v := table.Null()
		if field, ok := recordFieldByName(fields, col); ok {
			v = field.Value
		}
		merged, err := table.MergeSchemasStrictAtPath(nextSchemas[i], table.InferValueSchema(v), col)
		if err != nil {
			return err
		}
		nextSchemas[i] = merged
	}

	for _, field := range fields {
		if _, ok := nextIndex[field.Name]; ok {
			continue
		}
		schema := table.InferValueSchema(field.Value)
		if s.goodRows > 0 {
			merged, err := table.MergeSchemasStrictAtPath(&table.TypeDescriptor{Kind: table.TypeNull, Nullable: true}, schema, field.Name)
			if err != nil {
				return err
			}
			schema = merged
		}
		nextIndex[field.Name] = len(nextColumns)
		nextColumns = append(nextColumns, field.Name)
		nextSchemas = append(nextSchemas, schema)
	}

	s.columns = nextColumns
	s.schemas = nextSchemas
	s.index = nextIndex
	s.goodRows++
	return nil
}

func (s *jsonSchemaInference) recordValues(fields []table.RecordField, rejectUnknown bool) ([]table.Value, error) {
	if rejectUnknown {
		for _, field := range fields {
			if _, ok := s.index[field.Name]; !ok {
				return nil, jsonUnknownFieldError{Path: field.Name}
			}
		}
	}
	vals := make([]table.Value, len(s.columns))
	for i, col := range s.columns {
		v := table.Null()
		if field, ok := recordFieldByName(fields, col); ok {
			v = field.Value
			if rejectUnknown {
				if err := rejectUnknownJSONFields(v, s.schemas[i], col); err != nil {
					return nil, err
				}
			}
		}
		vals[i] = v
	}
	return vals, nil
}

type jsonUnknownFieldError struct {
	Path string
}

func (e jsonUnknownFieldError) Error() string {
	return fmt.Sprintf("%s unknown field outside inferred schema", e.Path)
}

func rejectUnknownJSONFields(v table.Value, schema *table.TypeDescriptor, path string) error {
	schema = table.FinalizeSchema(schema)
	if schema == nil || schema.Kind == table.TypeMixed || v.Type == table.TypeNull {
		return nil
	}
	switch v.Type {
	case table.TypeRecord:
		if schema.Kind != table.TypeRecord {
			return nil
		}
		for _, field := range v.Fields {
			fieldSchema, ok := schemaFieldByName(schema.Fields, field.Name)
			fieldPath := path + "." + field.Name
			if !ok {
				return jsonUnknownFieldError{Path: fieldPath}
			}
			if err := rejectUnknownJSONFields(field.Value, fieldSchema, fieldPath); err != nil {
				return err
			}
		}
	case table.TypeList:
		if schema.Kind != table.TypeList {
			return nil
		}
		for _, item := range v.List {
			if err := rejectUnknownJSONFields(item, schema.Elem, path+"[]"); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *jsonSchemaInference) inferRecordInPlace(fields []table.RecordField) error {
	for i, col := range s.columns {
		v := table.Null()
		if field, ok := recordFieldByName(fields, col); ok {
			v = field.Value
		}
		merged, err := table.MergeValueSchemaStrictAtPath(s.schemas[i], v, col)
		if err != nil {
			return err
		}
		s.schemas[i] = merged
	}

	for _, field := range fields {
		if _, ok := s.index[field.Name]; ok {
			continue
		}
		schema, err := table.MergeValueSchemaStrictAtPath(nil, field.Value, field.Name)
		if err != nil {
			return err
		}
		if s.goodRows > 0 {
			schema.Nullable = true
		}
		s.index[field.Name] = len(s.columns)
		s.columns = append(s.columns, field.Name)
		s.schemas = append(s.schemas, schema)
	}

	s.goodRows++
	return nil
}

func recordFieldByName(fields []table.RecordField, name string) (table.RecordField, bool) {
	i := sort.Search(len(fields), func(i int) bool { return fields[i].Name >= name })
	if i < len(fields) && fields[i].Name == name {
		return fields[i], true
	}
	return table.RecordField{}, false
}

func schemaFieldByName(fields []table.FieldDescriptor, name string) (*table.TypeDescriptor, bool) {
	i := sort.Search(len(fields), func(i int) bool { return fields[i].Name >= name })
	if i < len(fields) && fields[i].Name == name {
		return fields[i].Type, true
	}
	return nil, false
}

func countJSONBadRecord(rec jsonLogicalRecord, rowIdx int, err error, maxBadRecords int, badRecords *int) error {
	*badRecords++
	if *badRecords > maxBadRecords {
		return jsonRecordError(rec, rowIdx, err)
	}
	return nil
}

func jsonRecordError(rec jsonLogicalRecord, rowIdx int, err error) error {
	loc := rec.loc
	if loc == "" {
		loc = fmt.Sprintf("row %d", rowIdx+1)
	}
	if rec.source != "" {
		return fmt.Errorf("%s: %s: %w", rec.source, loc, err)
	}
	return fmt.Errorf("%s: %w", loc, err)
}

// anyToValue converts generic Go values to table values without applying
// format-specific wrapper conventions.
func anyToValue(v interface{}) table.Value {
	switch val := v.(type) {
	case nil:
		return table.Null()
	case bool:
		return table.BoolVal(val)
	case json.Number:
		v, err := jsonNumberToValue(val)
		if err != nil {
			return table.Null()
		}
		return v
	case float64:
		// Non-JSON generic readers may surface numeric values as float64.
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
		fields := buildRecordFields(val)
		return table.RecordVal(fields)
	default:
		b, _ := json.Marshal(val)
		return table.StrVal(string(b))
	}
}

func jsonNumberToValue(n json.Number) (table.Value, error) {
	if i, err := n.Int64(); err == nil {
		return table.IntVal(i), nil
	}
	if f, err := n.Float64(); err == nil {
		return table.FloatVal(f), nil
	}
	return table.Null(), jsonNumberError{Value: n.String()}
}

type jsonNumberError struct {
	Path  string
	Value string
}

func (e jsonNumberError) Error() string {
	if e.Path == "" {
		return fmt.Sprintf("unrepresentable JSON number %q", e.Value)
	}
	return fmt.Sprintf("%s unrepresentable JSON number %q", e.Path, e.Value)
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

func buildJSONRecordFields(m map[string]interface{}) ([]table.RecordField, error) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fields := make([]table.RecordField, len(keys))
	for i, k := range keys {
		v, err := jsonValueToValue(m[k], k)
		if err != nil {
			return nil, err
		}
		fields[i] = table.RecordField{Name: k, Value: v}
	}
	return fields, nil
}

func jsonValueToValue(v interface{}, path string) (table.Value, error) {
	switch val := v.(type) {
	case nil:
		return table.Null(), nil
	case bool:
		return table.BoolVal(val), nil
	case json.Number:
		out, err := jsonNumberToValue(val)
		if err != nil {
			if numberErr, ok := err.(jsonNumberError); ok {
				numberErr.Path = path
				return table.Null(), numberErr
			}
			return table.Null(), err
		}
		return out, nil
	case string:
		return table.StrVal(val), nil
	case []interface{}:
		elems := make([]table.Value, len(val))
		for i, elem := range val {
			out, err := jsonValueToValue(elem, path+"[]")
			if err != nil {
				return table.Null(), err
			}
			elems[i] = out
		}
		return table.ListVal(elems), nil
	case map[string]interface{}:
		fields, err := buildJSONRecordFieldsAtPath(val, path)
		if err != nil {
			return table.Null(), err
		}
		return table.RecordVal(fields), nil
	default:
		b, _ := json.Marshal(val)
		return table.StrVal(string(b)), nil
	}
}

func buildJSONRecordFieldsAtPath(m map[string]interface{}, path string) ([]table.RecordField, error) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fields := make([]table.RecordField, len(keys))
	for i, k := range keys {
		fieldPath := k
		if path != "" {
			fieldPath = path + "." + k
		}
		v, err := jsonValueToValue(m[k], fieldPath)
		if err != nil {
			return nil, err
		}
		fields[i] = table.RecordField{Name: k, Value: v}
	}
	return fields, nil
}

func parquetValue(v any, schema *table.TypeDescriptor) table.Value {
	schema = table.FinalizeSchema(schema)
	if schema != nil {
		switch schema.Kind {
		case table.TypeList:
			return parquetListValue(v, schema.Elem)
		case table.TypeRecord:
			return parquetRecordValue(v, schema)
		}
	}
	return parquetUnknownValue(v)
}

func parquetListValue(v any, elemSchema *table.TypeDescriptor) table.Value {
	if v == nil {
		return table.Null()
	}
	val, ok := v.([]any)
	if !ok {
		return parquetUnknownValue(v)
	}
	elems := make([]table.Value, len(val))
	for i, elem := range val {
		elems[i] = parquetListElementValue(elem, elemSchema)
	}
	return table.ListVal(elems)
}

func parquetListElementValue(v any, schema *table.TypeDescriptor) table.Value {
	if m, ok := v.(map[string]any); ok && len(m) == 1 {
		if elem, ok := m["element"]; ok && shouldUnwrapParquetElementWrapper(elem, schema) {
			return parquetValue(elem, schema)
		}
	}
	return parquetValue(v, schema)
}

func shouldUnwrapParquetElementWrapper(elem any, schema *table.TypeDescriptor) bool {
	schema = table.FinalizeSchema(schema)
	if schema == nil || schema.Kind != table.TypeRecord {
		return true
	}
	elemMap, ok := elem.(map[string]any)
	return ok && parquetMapMatchesRecordSchema(elemMap, schema)
}

func parquetMapMatchesRecordSchema(m map[string]any, schema *table.TypeDescriptor) bool {
	for _, field := range schema.Fields {
		if _, ok := m[field.Name]; ok {
			return true
		}
	}
	return false
}

func parquetRecordValue(v any, schema *table.TypeDescriptor) table.Value {
	if v == nil {
		return table.Null()
	}
	val, ok := v.(map[string]any)
	if !ok {
		return parquetUnknownValue(v)
	}
	fieldSchemas := make(map[string]*table.TypeDescriptor, len(schema.Fields))
	for i := range schema.Fields {
		field := &schema.Fields[i]
		fieldSchemas[field.Name] = field.Type
	}
	keys := make([]string, 0, len(val))
	for k := range val {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fields := make([]table.RecordField, len(keys))
	for i, k := range keys {
		fields[i] = table.RecordField{Name: k, Value: parquetValue(val[k], fieldSchemas[k])}
	}
	return table.RecordVal(fields)
}

func parquetUnknownValue(v any) table.Value {
	switch val := v.(type) {
	case []any:
		elems := make([]table.Value, len(val))
		for i, elem := range val {
			elems[i] = parquetUnknownValue(elem)
		}
		return table.ListVal(elems)
	case map[string]any:
		if elem, ok := val["element"]; ok && len(val) == 1 {
			return parquetUnknownValue(elem)
		}
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		fields := make([]table.RecordField, len(keys))
		for i, k := range keys {
			fields[i] = table.RecordField{Name: k, Value: parquetUnknownValue(val[k])}
		}
		return table.RecordVal(fields)
	default:
		return anyToValue(v)
	}
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

type avroNamedType struct {
	schema    any
	namespace string
	fullName  string
}

type avroSchemaContext struct {
	names map[string]avroNamedType
}

func newAvroSchemaContext(schema any, namespace string) *avroSchemaContext {
	ctx := &avroSchemaContext{names: make(map[string]avroNamedType)}
	ctx.collectNamedTypes(schema, namespace)
	return ctx
}

func (ctx *avroSchemaContext) collectNamedTypes(schema any, namespace string) {
	switch s := schema.(type) {
	case []any:
		for _, branch := range s {
			ctx.collectNamedTypes(branch, namespace)
		}
	case map[string]any:
		switch typ := s["type"].(type) {
		case string:
			switch typ {
			case "record", "enum", "fixed":
				fullName := avroFullName(s, namespace)
				typeNamespace := avroTypeNamespace(s, namespace)
				if fullName != "" {
					ctx.names[fullName] = avroNamedType{schema: s, namespace: typeNamespace, fullName: fullName}
				}
				if typ == "record" {
					fieldsRaw, _ := asSlice(s["fields"])
					for _, fieldRaw := range fieldsRaw {
						field, ok := asMap(fieldRaw)
						if !ok {
							continue
						}
						ctx.collectNamedTypes(field["type"], typeNamespace)
					}
				}
			case "array":
				ctx.collectNamedTypes(s["items"], namespace)
			case "map":
				ctx.collectNamedTypes(s["values"], namespace)
			}
		case []any:
			ctx.collectNamedTypes(typ, namespace)
		case map[string]any:
			ctx.collectNamedTypes(typ, namespace)
		}
	}
}

func (ctx *avroSchemaContext) value(v any, schema any, namespace string) table.Value {
	if v == nil {
		return table.Null()
	}
	if s, ok := schema.(string); ok {
		if named, ok := ctx.resolveNamedType(s, namespace); ok {
			return ctx.value(v, named.schema, named.namespace)
		}
		if m, ok := asMap(v); ok && len(m) == 1 {
			for k, inner := range m {
				if avroNameMatches(ctx.schemaName(s, namespace), k) {
					return ctx.value(inner, s, namespace)
				}
			}
		}
		return avroPrimitiveValue(v, s)
	}
	if branches, ok := asSlice(schema); ok {
		if m, ok := asMap(v); ok && len(m) == 1 {
			for k, inner := range m {
				for _, branch := range branches {
					if avroNameMatches(ctx.schemaName(branch, namespace), k) {
						return ctx.value(inner, branch, ctx.schemaNamespace(branch, namespace))
					}
				}
			}
		}
		for _, branch := range branches {
			if ctx.schemaName(branch, namespace) != "null" {
				return ctx.value(v, branch, ctx.schemaNamespace(branch, namespace))
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
				return ctx.recordValue(v, s, namespace)
			case "array":
				return ctx.arrayValue(v, s["items"], namespace)
			default:
				return ctx.value(v, ts, namespace)
			}
		default:
			if nested, ok := asSlice(typ); ok {
				return ctx.value(v, nested, namespace)
			}
			if nested, ok := asMap(typ); ok {
				return ctx.value(v, nested, namespace)
			}
			return anyToValue(v)
		}
	}
	return anyToValue(v)
}

func avroPrimitiveValue(v any, typ string) table.Value {
	switch typ {
	case "null":
		return table.Null()
	case "boolean":
		if b, ok := v.(bool); ok {
			return table.BoolVal(b)
		}
	case "int":
		switch n := v.(type) {
		case int32:
			return table.IntVal(int64(n))
		case int:
			return table.IntVal(int64(n))
		case int64:
			return table.IntVal(n)
		}
	case "long":
		switch n := v.(type) {
		case int64:
			return table.IntVal(n)
		case int:
			return table.IntVal(int64(n))
		case int32:
			return table.IntVal(int64(n))
		}
	case "float", "double":
		switch n := v.(type) {
		case float64:
			return table.FloatVal(n)
		case float32:
			return table.FloatVal(float64(n))
		case int64:
			return table.FloatVal(float64(n))
		case int32:
			return table.FloatVal(float64(n))
		case int:
			return table.FloatVal(float64(n))
		}
	case "string", "enum":
		if s, ok := v.(string); ok {
			return table.StrVal(s)
		}
	case "bytes":
		if b, ok := v.([]byte); ok {
			return table.StrVal(string(b))
		}
	}
	return anyToValue(v)
}

func avroTypeNamespace(schema any, parentNamespace string) string {
	schemaMap, ok := asMap(schema)
	if !ok {
		return parentNamespace
	}
	if name, ok := schemaMap["name"].(string); ok && strings.Contains(name, ".") {
		return name[:strings.LastIndex(name, ".")]
	}
	if ns, ok := schemaMap["namespace"].(string); ok {
		return ns
	}
	return parentNamespace
}

func (ctx *avroSchemaContext) schemaName(schema any, namespace string) string {
	if s, ok := schema.(string); ok {
		if isAvroPrimitiveName(s) {
			return s
		}
		if named, ok := ctx.resolveNamedType(s, namespace); ok {
			return named.fullName
		}
		return avroReferenceFullName(s, namespace)
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
				if named, ok := ctx.resolveNamedType(typ, namespace); ok {
					return named.fullName
				}
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
	if strings.Contains(name, ".") {
		return name
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

func avroReferenceFullName(name, namespace string) string {
	if isAvroPrimitiveName(name) || strings.Contains(name, ".") || namespace == "" {
		return name
	}
	return namespace + "." + name
}

func isAvroPrimitiveName(name string) bool {
	switch name {
	case "null", "boolean", "int", "long", "float", "double", "bytes", "string", "enum", "fixed":
		return true
	default:
		return false
	}
}

func avroNameMatches(expected, actual string) bool {
	return expected == actual || avroShortName(expected) == actual || expected == avroShortName(actual)
}

func avroShortName(name string) string {
	if idx := strings.LastIndex(name, "."); idx >= 0 {
		return name[idx+1:]
	}
	return name
}

func (ctx *avroSchemaContext) resolveNamedType(name, namespace string) (avroNamedType, bool) {
	if isAvroPrimitiveName(name) {
		return avroNamedType{}, false
	}
	fullName := avroReferenceFullName(name, namespace)
	named, ok := ctx.names[fullName]
	if ok {
		return named, true
	}
	if namespace == "" && !strings.Contains(name, ".") {
		named, ok = ctx.names[name]
		if ok {
			return named, true
		}
	}
	return avroNamedType{}, false
}

func (ctx *avroSchemaContext) recursiveNamedReference(schema any, namespace string, resolving map[string]bool) (string, bool) {
	switch s := schema.(type) {
	case string:
		if isAvroPrimitiveName(s) {
			return "", false
		}
		named, ok := ctx.resolveNamedType(s, namespace)
		if !ok {
			return "", false
		}
		if resolving[named.fullName] {
			return named.fullName, true
		}
		return ctx.recursiveNamedReference(named.schema, named.namespace, resolving)
	case []any:
		for _, branch := range s {
			if name, ok := ctx.recursiveNamedReference(branch, ctx.schemaNamespace(branch, namespace), resolving); ok {
				return name, true
			}
		}
	case map[string]any:
		switch typ := s["type"].(type) {
		case string:
			switch typ {
			case "record":
				return ctx.recursiveRecordReference(s, namespace, resolving)
			case "array":
				return ctx.recursiveNamedReference(s["items"], namespace, resolving)
			case "map":
				return ctx.recursiveNamedReference(s["values"], namespace, resolving)
			default:
				return ctx.recursiveNamedReference(typ, namespace, resolving)
			}
		case []any:
			return ctx.recursiveNamedReference(typ, namespace, resolving)
		case map[string]any:
			return ctx.recursiveNamedReference(typ, namespace, resolving)
		}
	}
	return "", false
}

func (ctx *avroSchemaContext) recursiveRecordReference(schema map[string]any, namespace string, resolving map[string]bool) (string, bool) {
	recordNamespace := avroTypeNamespace(schema, namespace)
	fullName := avroFullName(schema, namespace)
	if fullName != "" {
		if resolving[fullName] {
			return fullName, true
		}
		resolving = cloneAvroResolving(resolving)
		resolving[fullName] = true
	}
	fieldsRaw, _ := asSlice(schema["fields"])
	for _, fieldRaw := range fieldsRaw {
		field, ok := asMap(fieldRaw)
		if !ok {
			continue
		}
		if name, ok := ctx.recursiveNamedReference(field["type"], recordNamespace, resolving); ok {
			return name, true
		}
	}
	return "", false
}

func (ctx *avroSchemaContext) schemaNamespace(schema any, parentNamespace string) string {
	if s, ok := schema.(string); ok {
		if named, ok := ctx.resolveNamedType(s, parentNamespace); ok {
			return named.namespace
		}
		return parentNamespace
	}
	return avroTypeNamespace(schema, parentNamespace)
}

func (ctx *avroSchemaContext) recordValue(v any, schema map[string]any, namespace string) table.Value {
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
			Value: ctx.value(rec[name], field["type"], recordNamespace),
		})
	}
	sort.Slice(fields, func(i, j int) bool { return fields[i].Name < fields[j].Name })
	return table.RecordVal(fields)
}

func (ctx *avroSchemaContext) arrayValue(v any, itemSchema any, namespace string) table.Value {
	items, ok := asSlice(v)
	if !ok {
		return anyToValue(v)
	}
	values := make([]table.Value, len(items))
	for i, item := range items {
		values[i] = ctx.value(item, itemSchema, namespace)
	}
	return table.ListVal(values)
}

func (ctx *avroSchemaContext) fieldSchemaDescriptor(schema any, namespace string, resolving map[string]bool) *table.TypeDescriptor {
	switch s := schema.(type) {
	case string:
		if primitive := avroPrimitiveSchemaDescriptor(s); primitive != nil {
			return primitive
		}
		named, ok := ctx.resolveNamedType(s, namespace)
		if !ok || resolving[named.fullName] {
			return nil
		}
		nextResolving := cloneAvroResolving(resolving)
		nextResolving[named.fullName] = true
		return ctx.fieldSchemaDescriptor(named.schema, named.namespace, nextResolving)
	case []any:
		branches := make([]*table.TypeDescriptor, 0, len(s))
		nullable := false
		for _, branch := range s {
			if ctx.schemaName(branch, namespace) == "null" {
				nullable = true
				continue
			}
			next := ctx.fieldSchemaDescriptor(branch, ctx.schemaNamespace(branch, namespace), resolving)
			if next == nil {
				return nil
			}
			branches = append(branches, next)
		}
		return table.UnionSchema(branches, nullable)
	case map[string]any:
		switch typ := s["type"].(type) {
		case string:
			switch typ {
			case "record":
				return ctx.recordSchemaDescriptor(s, namespace, resolving)
			case "array":
				elem := ctx.fieldSchemaDescriptor(s["items"], namespace, resolving)
				if elem == nil {
					return nil
				}
				return &table.TypeDescriptor{Kind: table.TypeList, Elem: elem}
			case "map":
				return nil
			default:
				return ctx.fieldSchemaDescriptor(typ, namespace, resolving)
			}
		case []any:
			return ctx.fieldSchemaDescriptor(typ, namespace, resolving)
		case map[string]any:
			return ctx.fieldSchemaDescriptor(typ, namespace, resolving)
		}
	}
	return nil
}

func cloneAvroResolving(in map[string]bool) map[string]bool {
	out := make(map[string]bool, len(in)+1)
	for k, v := range in {
		out[k] = v
	}
	return out
}

func avroPrimitiveSchemaDescriptor(name string) *table.TypeDescriptor {
	switch name {
	case "null":
		return &table.TypeDescriptor{Kind: table.TypeNull, Nullable: true}
	case "int", "long":
		return &table.TypeDescriptor{Kind: table.TypeInt}
	case "float", "double":
		return &table.TypeDescriptor{Kind: table.TypeFloat}
	case "boolean":
		return &table.TypeDescriptor{Kind: table.TypeBool}
	case "string", "bytes", "enum", "fixed":
		return &table.TypeDescriptor{Kind: table.TypeString}
	default:
		return nil
	}
}

func (ctx *avroSchemaContext) recordSchemaDescriptor(schema map[string]any, namespace string, resolving map[string]bool) *table.TypeDescriptor {
	fieldsRaw, ok := asSlice(schema["fields"])
	if !ok {
		return nil
	}
	recordNamespace := avroTypeNamespace(schema, namespace)
	fields := make([]table.FieldDescriptor, 0, len(fieldsRaw))
	for _, fieldRaw := range fieldsRaw {
		field, ok := asMap(fieldRaw)
		if !ok {
			return nil
		}
		name, ok := field["name"].(string)
		if !ok {
			return nil
		}
		typ := ctx.fieldSchemaDescriptor(field["type"], recordNamespace, resolving)
		if typ == nil {
			return nil
		}
		fields = append(fields, table.FieldDescriptor{Name: name, Type: typ})
	}
	sort.Slice(fields, func(i, j int) bool { return fields[i].Name < fields[j].Name })
	return &table.TypeDescriptor{Kind: table.TypeRecord, Fields: fields}
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

	columns, schemas, fieldSchemas, err := avroSchemaParts(schema)
	if err != nil {
		return nil, err
	}

	t := table.NewTableWithSchemas(columns, schemas)

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
			val := fieldSchemas.context.value(v, fieldSchemas.schemas[col], fieldSchemas.rootNamespace)
			vals[i] = val
		}
		if err := addMetadataTypedRow(t, vals, "Avro record"); err != nil {
			return nil, err
		}
	}

	if err := ocfr.Err(); err != nil {
		return nil, fmt.Errorf("error reading Avro file: %w", err)
	}

	return t, nil
}

type avroFieldSchemas struct {
	context       *avroSchemaContext
	rootNamespace string
	schemas       map[string]any
}

func avroSchemaParts(schema string) ([]string, []*table.TypeDescriptor, avroFieldSchemas, error) {
	var rootSchema any
	if err := json.Unmarshal([]byte(schema), &rootSchema); err != nil {
		return nil, nil, avroFieldSchemas{}, fmt.Errorf("cannot parse Avro schema: %w", err)
	}
	rootMap, _ := asMap(rootSchema)
	rootNamespace := avroTypeNamespace(rootMap, "")
	avroCtx := newAvroSchemaContext(rootSchema, rootNamespace)

	var schemaDef struct {
		Fields []struct {
			Name string `json:"name"`
			Type any    `json:"type"`
		} `json:"fields"`
	}
	if err := json.Unmarshal([]byte(schema), &schemaDef); err != nil {
		return nil, nil, avroFieldSchemas{}, fmt.Errorf("cannot parse Avro schema: %w", err)
	}

	columns := make([]string, len(schemaDef.Fields))
	schemas := make([]*table.TypeDescriptor, len(schemaDef.Fields))
	fieldSchemas := make(map[string]any, len(schemaDef.Fields))
	for i, field := range schemaDef.Fields {
		columns[i] = field.Name
		if recursiveName, ok := avroCtx.recursiveNamedReference(field.Type, rootNamespace, nil); ok {
			return nil, nil, avroFieldSchemas{}, fmt.Errorf("unsupported recursive Avro schema for field %q: named type %q is recursive", field.Name, recursiveName)
		}
		schemas[i] = avroCtx.fieldSchemaDescriptor(field.Type, rootNamespace, nil)
		if schemas[i] == nil {
			return nil, nil, avroFieldSchemas{}, fmt.Errorf("unsupported Avro schema for field %q", field.Name)
		}
		fieldSchemas[field.Name] = field.Type
	}
	return columns, schemas, avroFieldSchemas{context: avroCtx, rootNamespace: rootNamespace, schemas: fieldSchemas}, nil
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
	schemas := parquetColumnSchemas(schema, columns)
	t := table.NewTableWithSchemas(columns, schemas)

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
				vals[j] = parquetValue(row[col], schemas[j])
			}
			if err := addMetadataTypedRow(t, vals, "Parquet row"); err != nil {
				return nil, err
			}
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

func addMetadataTypedRow(t *table.Table, vals []table.Value, context string) error {
	if err := t.AddRowTyped(vals); err != nil {
		return fmt.Errorf("error materializing %s: %w", context, err)
	}
	return nil
}

func parquetColumnSchemas(schema *parquet.Schema, columns []string) []*table.TypeDescriptor {
	fieldsByName := make(map[string]parquet.Field, len(schema.Fields()))
	for _, field := range schema.Fields() {
		fieldsByName[field.Name()] = field
	}
	schemas := make([]*table.TypeDescriptor, len(columns))
	for i, column := range columns {
		schemas[i] = parquetNodeSchemaDescriptor(fieldsByName[column])
	}
	return schemas
}

func parquetNodeSchemaDescriptor(node parquet.Node) *table.TypeDescriptor {
	if node == nil {
		return nil
	}
	schema := parquetNodeSchemaDescriptorNonNull(node)
	if schema == nil {
		return nil
	}
	if node.Optional() {
		schema = table.WithNullable(schema)
	}
	return schema
}

func parquetNodeSchemaDescriptorNonNull(node parquet.Node) *table.TypeDescriptor {
	if node.Repeated() {
		elem := parquetNodeSchemaDescriptorRepeatedElem(node)
		if elem == nil {
			return nil
		}
		return &table.TypeDescriptor{Kind: table.TypeList, Elem: elem}
	}
	if !node.Leaf() {
		if node.Type().String() == "LIST" {
			elem := parquetListElementSchemaDescriptor(node)
			if elem == nil {
				elem = &table.TypeDescriptor{Kind: table.TypeString, Nullable: true}
			}
			return &table.TypeDescriptor{Kind: table.TypeList, Elem: elem}
		}
		fields := node.Fields()
		out := make([]table.FieldDescriptor, 0, len(fields))
		for _, field := range fields {
			typ := parquetNodeSchemaDescriptor(field)
			if typ == nil {
				return nil
			}
			out = append(out, table.FieldDescriptor{Name: field.Name(), Type: typ})
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
		return &table.TypeDescriptor{Kind: table.TypeRecord, Fields: out}
	}

	switch node.Type().Kind() {
	case parquet.Boolean:
		return &table.TypeDescriptor{Kind: table.TypeBool}
	case parquet.Int32, parquet.Int64, parquet.Int96:
		return &table.TypeDescriptor{Kind: table.TypeInt}
	case parquet.Float, parquet.Double:
		return &table.TypeDescriptor{Kind: table.TypeFloat}
	case parquet.ByteArray, parquet.FixedLenByteArray:
		return &table.TypeDescriptor{Kind: table.TypeString}
	default:
		return nil
	}
}

func parquetNodeSchemaDescriptorRepeatedElem(node parquet.Node) *table.TypeDescriptor {
	if node.Leaf() {
		switch node.Type().Kind() {
		case parquet.Boolean:
			return &table.TypeDescriptor{Kind: table.TypeBool}
		case parquet.Int32, parquet.Int64, parquet.Int96:
			return &table.TypeDescriptor{Kind: table.TypeInt}
		case parquet.Float, parquet.Double:
			return &table.TypeDescriptor{Kind: table.TypeFloat}
		case parquet.ByteArray, parquet.FixedLenByteArray:
			return &table.TypeDescriptor{Kind: table.TypeString}
		default:
			return nil
		}
	}
	fields := node.Fields()
	out := make([]table.FieldDescriptor, 0, len(fields))
	for _, field := range fields {
		typ := parquetNodeSchemaDescriptor(field)
		if typ == nil {
			return nil
		}
		out = append(out, table.FieldDescriptor{Name: field.Name(), Type: typ})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return &table.TypeDescriptor{Kind: table.TypeRecord, Fields: out}
}

func parquetListElementSchemaDescriptor(node parquet.Node) *table.TypeDescriptor {
	fields := node.Fields()
	if len(fields) == 0 {
		return nil
	}
	if len(fields) == 1 && fields[0].Name() == "list" {
		listFields := fields[0].Fields()
		if len(listFields) == 1 && listFields[0].Name() == "element" {
			elementFields := listFields[0].Fields()
			if len(elementFields) == 1 && elementFields[0].Name() == "element" && parquetNullablePrimitiveElementWrapper(elementFields[0]) {
				return parquetNodeSchemaDescriptor(elementFields[0])
			}
			return parquetNodeSchemaDescriptor(listFields[0])
		}
		if len(listFields) == 1 {
			return parquetNodeSchemaDescriptor(listFields[0])
		}
	}
	if len(fields) == 1 {
		return parquetNodeSchemaDescriptor(fields[0])
	}
	return nil
}

func parquetNullablePrimitiveElementWrapper(node parquet.Node) bool {
	return node != nil && node.Optional() && node.Leaf() && parquetNodeSchemaDescriptorNonNull(node) != nil
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
