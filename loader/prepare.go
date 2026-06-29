package loader

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	goavro "github.com/linkedin/goavro/v2"
	parquet "github.com/parquet-go/parquet-go"
	"github.com/razeghi71/dq/ast"
	"github.com/razeghi71/dq/rowstream"
	"github.com/razeghi71/dq/table"
)

// PreparedSource holds a replayable literal source after schema acquisition
// and before materialization. Formats that need inference inspect only the
// inference window here; formats with embedded metadata read that metadata.
// Physical row materialization happens later through LoadSpec.
type PreparedSource struct {
	Schema table.Schema

	csv    *preparedCSVSource
	js     *preparedJSONSource
	file   *preparedFileSource
	liveJS *preparedLiveJSONSource
	glob   *preparedGlobSource
}

type RowPredicate func(row []table.Value) (bool, error)

type SourceLoadSpec struct {
	ReadColumns   []string
	OutputColumns []string
	Predicate     RowPredicate
}

type preparedSourceLoadPlan struct {
	sourceColumns     []string
	sourceSchemas     []*table.TypeDescriptor
	readColumns       []string
	readSchemas       []*table.TypeDescriptor
	readSourceIndexes []int
	readAll           bool
	outputColumns     []string
	outputSchemas     []*table.TypeDescriptor
	outputFromRead    []int
	predicate         RowPredicate
}

type preparedJSONSource struct {
	filename        string
	format          string
	cfg             jsonLoadConfig
	state           jsonSchemaInference
	records         []jsonLogicalRecord
	recordsComplete bool
	loaded          bool
}

type preparedFileSource struct {
	filename string
	opts     Options
	schema   table.Schema
	loaded   bool
}

type preparedCSVSource struct {
	closer      io.Closer
	reader      *csv.Reader
	cfg         csvLoadConfig
	columns     []string
	sampleRows  []csvRawRow
	pendingRows []csvRawRow
	types       []table.ValueType
	schemas     []*table.TypeDescriptor
	startRow    int
	empty       bool
	loaded      bool
}

// CanPrepare reports whether a source class can enter prepared planning without
// materializing full source rows. It is not full load validation; glob formats
// are validated after expansion by Prepare.
func CanPrepare(filename string, opts Options) bool {
	if filename == "" {
		return false
	}
	opts = normalizeOptions(opts)
	if IsStdin(filename) {
		return canPrepareFormat(opts.Format, true)
	}
	if HasGlobMeta(filename) && opts.Format == "" {
		return true
	}
	format, _ := resolveFormatCompression(filename, opts)
	return canPrepareFormat(format, false)
}

func canPrepareFormat(format string, stdin bool) bool {
	switch format {
	case "csv", "json", "jsonl", "avro", "parquet":
		if stdin {
			return ast.IsStreamLoadFormat(format)
		}
		return true
	default:
		return false
	}
}

// Prepare inspects a replayable literal source and returns a one-shot prepared
// source for later materialization.
func Prepare(filename string, opts Options) (*PreparedSource, error) {
	opts = normalizeOptions(opts)
	if IsStdin(filename) {
		return nil, fmt.Errorf("prepare source: stdin is not prepareable without consuming it")
	}
	return prepareReplayable(filename, opts)
}

// PrepareInput inspects a source, using stdin when filename is "-". Stdin
// prepared sources are one-shot: schema acquisition retains the live stream
// state needed by LoadSpec or StreamSpec.
func PrepareInput(filename string, opts Options, stdin io.Reader) (*PreparedSource, error) {
	opts = normalizeOptions(opts)
	if IsStdin(filename) {
		return prepareStdin(stdin, opts)
	}
	return prepareReplayable(filename, opts)
}

func prepareReplayable(filename string, opts Options) (*PreparedSource, error) {
	if HasGlobMeta(filename) {
		return prepareGlob(filename, opts)
	}
	format, compression := resolveFormatCompression(filename, opts)
	if err := validateOptionsForFormat(opts, format); err != nil {
		return nil, err
	}
	switch format {
	case "csv":
		cfg := csvConfigFromOptions(opts, nil)
		cfg.compression = compression
		prepared, err := prepareCSV(filename, cfg)
		if err != nil {
			return nil, err
		}
		return prepared, nil
	case "json", "jsonl":
		cfg := jsonConfigFromOptions(opts, filename)
		cfg.compression = compression
		return prepareJSONLike(filename, format, cfg)
	case "avro":
		if compression != "" {
			return nil, fmt.Errorf("compression=%s applies only to csv, json, and jsonl formats", compression)
		}
		schema, err := inspectAvroSchema(filename)
		if err != nil {
			return nil, err
		}
		return &PreparedSource{Schema: schema, file: &preparedFileSource{filename: filename, opts: opts, schema: schema}}, nil
	case "parquet":
		if compression != "" {
			return nil, fmt.Errorf("compression=%s applies only to csv, json, and jsonl formats", compression)
		}
		schema, err := inspectParquetSchema(filename)
		if err != nil {
			return nil, err
		}
		return &PreparedSource{Schema: schema, file: &preparedFileSource{filename: filename, opts: opts, schema: schema}}, nil
	default:
		if format == "" {
			return nil, fmt.Errorf("cannot determine file format for %q: use with format=... in query (%s)", filename, ast.LoadFormatsList())
		}
		return nil, fmt.Errorf("prepare source: unsupported format %q", format)
	}
}

func prepareCSV(filename string, cfg csvLoadConfig) (*PreparedSource, error) {
	f, err := openInputReader(filename, cfg.compression)
	if err != nil {
		return nil, err
	}
	cfg.source = filename
	prepared, err := prepareCSVSourceReader(f, cfg)
	if err != nil {
		f.Close()
		return nil, err
	}
	return prepared, nil
}

func prepareCSVSourceReader(f io.ReadCloser, cfg csvLoadConfig) (*PreparedSource, error) {
	reader := newCSVReader(f, cfg.delim)
	columns, buffered, startRow, empty, err := prepareCSVReader(reader, cfg)
	if err != nil {
		return nil, err
	}
	if empty {
		return &PreparedSource{
			Schema: table.NewSchema(nil, nil),
			csv: &preparedCSVSource{
				closer: f,
				cfg:    cfg,
				empty:  true,
			},
		}, nil
	}
	if err := validateCSVHeaderColumns(columns, cfg.source); err != nil {
		return nil, err
	}

	window, err := readCSVInferenceWindow(reader, columns, buffered, startRow, cfg)
	if err != nil {
		return nil, err
	}

	groups := []csvRowGroup{{columns: append([]string(nil), columns...), source: cfg.source, rows: window.sampleRows}}
	types := inferCSVColumnTypes(columns, groups, cfg.inferRows)
	totalRows := len(window.sampleRows) + len(window.pendingRows)
	nullableAll := csvStreamingInferenceNeedsConservativeNullability(cfg.inferRows, totalRows, window.sampleExhausted)
	schema := csvSchemaFromTypes(columns, types, csvNullableColumns(columns, groups), nullableAll)
	return &PreparedSource{
		Schema: schema,
		csv: &preparedCSVSource{
			closer:      f,
			reader:      reader,
			cfg:         cfg,
			columns:     append([]string(nil), columns...),
			sampleRows:  window.sampleRows,
			pendingRows: window.pendingRows,
			types:       types,
			schemas:     schemaTypeDescriptors(schema),
			startRow:    window.nextRow,
		},
	}, nil
}

func prepareJSONLike(filename, format string, cfg jsonLoadConfig) (*PreparedSource, error) {
	inspection, err := inspectJSONLikeSchema(filename, format, cfg)
	if err != nil {
		return nil, err
	}
	return &PreparedSource{
		Schema: table.NewSchema(inspection.state.columns, inspection.state.schemas),
		js: &preparedJSONSource{
			filename:        filename,
			format:          format,
			cfg:             cfg,
			state:           cloneJSONSchemaInference(inspection.state),
			records:         inspection.records,
			recordsComplete: inspection.recordsComplete,
		},
	}, nil
}

func csvSchemaFromTypes(columns []string, types []table.ValueType, nullable []bool, nullableAll bool) table.Schema {
	return table.NewSchema(columns, csvSchemasFromTypes(columns, types, nullable, nullableAll))
}

func csvSchemasFromTypes(columns []string, types []table.ValueType, nullable []bool, nullableAll bool) []*table.TypeDescriptor {
	schemas := make([]*table.TypeDescriptor, len(columns))
	for i := range columns {
		typ := table.TypeString
		if i < len(types) {
			typ = types[i]
		}
		schemas[i] = table.ScalarSchema(typ)
		if nullableAll || (i < len(nullable) && nullable[i]) {
			schemas[i] = table.WithNullable(schemas[i])
		}
	}
	return schemas
}

func schemaTypeDescriptors(schema table.Schema) []*table.TypeDescriptor {
	out := make([]*table.TypeDescriptor, len(schema.Columns))
	for i, col := range schema.Columns {
		out[i] = col.Type
	}
	return out
}

func csvNullableColumns(columns []string, groups []csvRowGroup) []bool {
	nullable := make([]bool, len(columns))
	for _, group := range groups {
		mapping := csvColumnMapping(columns, group.columns)
		for _, row := range group.rows {
			seen := make([]bool, len(columns))
			for srcIdx, dst := range mapping {
				if dst < 0 || dst >= len(columns) {
					continue
				}
				seen[dst] = true
				if srcIdx >= len(row.record) || isCSVNull(strings.TrimSpace(row.record[srcIdx])) {
					nullable[dst] = true
				}
			}
			for dst, ok := range seen {
				if !ok {
					nullable[dst] = true
				}
			}
		}
	}
	return nullable
}

// Load materializes the prepared source once with the same columns used for
// physical reads and output.
func (p *PreparedSource) Load(projectColumns []string) (*table.Table, error) {
	return p.LoadSpec(SourceLoadSpec{
		ReadColumns:   append([]string(nil), projectColumns...),
		OutputColumns: append([]string(nil), projectColumns...),
	})
}

func (p *PreparedSource) LoadSpec(spec SourceLoadSpec) (*table.Table, error) {
	if p == nil {
		return nil, fmt.Errorf("prepared source is not configured")
	}
	switch {
	case p.csv != nil:
		return p.csv.load(spec)
	case p.js != nil:
		return p.js.load(spec)
	case p.file != nil:
		return p.file.load(spec)
	case p.liveJS != nil:
		return p.liveJS.load(spec)
	case p.glob != nil:
		return p.glob.load(spec)
	default:
		return nil, fmt.Errorf("prepared source is not configured")
	}
}

func (p *PreparedSource) StreamSpec(spec SourceLoadSpec) (rowstream.Stream, error) {
	if p == nil {
		return nil, fmt.Errorf("prepared source is not configured")
	}
	switch {
	case p.csv != nil:
		return p.csv.stream(spec)
	case p.js != nil:
		return p.js.stream(spec)
	case p.file != nil:
		return p.file.stream(spec)
	case p.liveJS != nil:
		return p.liveJS.stream(spec)
	case p.glob != nil:
		return p.glob.stream(spec)
	default:
		return nil, fmt.Errorf("prepared source is not configured")
	}
}

// Close releases the prepared source when planning fails before Load is called.
func (p *PreparedSource) Close() error {
	if p == nil {
		return nil
	}
	switch {
	case p.csv != nil:
		return p.csv.close()
	case p.liveJS != nil:
		return p.liveJS.close()
	default:
		return nil
	}
}

func preparedSourceLoadPlanFor(schema table.Schema, spec SourceLoadSpec, source string) (preparedSourceLoadPlan, error) {
	readColumns := append([]string(nil), spec.ReadColumns...)
	outputColumns := append([]string(nil), spec.OutputColumns...)
	if outputColumns == nil {
		readColumns = nil
	} else if readColumns == nil {
		readColumns = append([]string(nil), outputColumns...)
	}

	readAll := readColumns == nil
	readCols, readSchemas, readSourceIndexes, err := sourceColumnProjection(schema, readColumns, source)
	if err != nil {
		return preparedSourceLoadPlan{}, err
	}
	outCols, outSchemas, outSourceIndexes, err := sourceColumnProjection(schema, outputColumns, source)
	if err != nil {
		return preparedSourceLoadPlan{}, err
	}

	readPositionBySource := make(map[int]int, len(readSourceIndexes))
	for readIdx, sourceIdx := range readSourceIndexes {
		readPositionBySource[sourceIdx] = readIdx
	}
	outputFromRead := make([]int, len(outSourceIndexes))
	for outIdx, sourceIdx := range outSourceIndexes {
		readIdx, ok := readPositionBySource[sourceIdx]
		if !ok {
			return preparedSourceLoadPlan{}, fmt.Errorf("%s: projected column %q not found in source read set", sourcePrefix(source), outCols[outIdx])
		}
		outputFromRead[outIdx] = readIdx
	}

	sourceColumns := make([]string, len(schema.Columns))
	sourceSchemas := make([]*table.TypeDescriptor, len(schema.Columns))
	for i, col := range schema.Columns {
		sourceColumns[i] = col.Name
		sourceSchemas[i] = col.Type
	}
	return preparedSourceLoadPlan{
		sourceColumns:     sourceColumns,
		sourceSchemas:     sourceSchemas,
		readColumns:       readCols,
		readSchemas:       readSchemas,
		readSourceIndexes: readSourceIndexes,
		readAll:           readAll,
		outputColumns:     outCols,
		outputSchemas:     outSchemas,
		outputFromRead:    outputFromRead,
		predicate:         spec.Predicate,
	}, nil
}

func sourceColumnProjection(schema table.Schema, columns []string, source string) ([]string, []*table.TypeDescriptor, []int, error) {
	if columns == nil {
		outCols := make([]string, len(schema.Columns))
		outSchemas := make([]*table.TypeDescriptor, len(schema.Columns))
		sourceIndexes := make([]int, len(schema.Columns))
		for i, col := range schema.Columns {
			outCols[i] = col.Name
			outSchemas[i] = col.Type
			sourceIndexes[i] = i
		}
		return outCols, outSchemas, sourceIndexes, nil
	}

	index := make(map[string]int, len(schema.Columns))
	for i, col := range schema.Columns {
		index[col.Name] = i
	}
	seen := make(map[string]bool, len(columns))
	outCols := make([]string, len(columns))
	outSchemas := make([]*table.TypeDescriptor, len(columns))
	sourceIndexes := make([]int, len(columns))
	for outIdx, col := range columns {
		if seen[col] {
			return nil, nil, nil, fmt.Errorf("%s: projected column %q requested more than once", sourcePrefix(source), col)
		}
		seen[col] = true
		sourceIdx, ok := index[col]
		if !ok {
			return nil, nil, nil, fmt.Errorf("%s: projected column %q not found", sourcePrefix(source), col)
		}
		outCols[outIdx] = col
		outSchemas[outIdx] = schema.Columns[sourceIdx].Type
		sourceIndexes[outIdx] = sourceIdx
	}
	return outCols, outSchemas, sourceIndexes, nil
}

func addPreparedSourceReadRow(t *table.Table, readVals []table.Value, plan preparedSourceLoadPlan) error {
	vals, keep, err := preparedSourceOutputRow(readVals, plan)
	if err != nil || !keep {
		return err
	}
	return t.AddRowTyped(vals)
}

func preparedSourceOutputRow(readVals []table.Value, plan preparedSourceLoadPlan) ([]table.Value, bool, error) {
	if plan.predicate != nil {
		keep, err := plan.predicate(readVals)
		if err != nil {
			return nil, false, err
		}
		if !keep {
			return nil, false, nil
		}
	}
	vals := make([]table.Value, len(plan.outputFromRead))
	for i, readIdx := range plan.outputFromRead {
		vals[i] = readVals[readIdx]
	}
	return vals, true, nil
}

type csvPreparedLoadPlan struct {
	read           csvMaterialization
	output         csvMaterialization
	outputFromRead []int
	predicate      RowPredicate
}

func csvPreparedLoadPlanFor(columns []string, types []table.ValueType, schemas []*table.TypeDescriptor, spec SourceLoadSpec, source string) (csvPreparedLoadPlan, error) {
	readColumns := append([]string(nil), spec.ReadColumns...)
	outputColumns := append([]string(nil), spec.OutputColumns...)
	if outputColumns == nil {
		readColumns = nil
	} else if readColumns == nil {
		readColumns = append([]string(nil), outputColumns...)
	}

	read, err := csvMaterializationFor(columns, types, schemas, readColumns, source)
	if err != nil {
		return csvPreparedLoadPlan{}, err
	}
	output, err := csvMaterializationFor(columns, types, schemas, outputColumns, source)
	if err != nil {
		return csvPreparedLoadPlan{}, err
	}

	sourceIndex := make(map[string]int, len(columns))
	for i, col := range columns {
		sourceIndex[col] = i
	}
	outputFromRead := make([]int, len(output.columns))
	for i, col := range output.columns {
		srcIdx, ok := sourceIndex[col]
		if !ok || srcIdx >= len(read.positionByColumn) {
			return csvPreparedLoadPlan{}, fmt.Errorf("%s: projected column %q not found in source read set", sourcePrefix(source), col)
		}
		readIdx := read.positionByColumn[srcIdx]
		if readIdx < 0 {
			return csvPreparedLoadPlan{}, fmt.Errorf("%s: projected column %q not found in source read set", sourcePrefix(source), col)
		}
		outputFromRead[i] = readIdx
	}

	return csvPreparedLoadPlan{
		read:           read,
		output:         output,
		outputFromRead: outputFromRead,
		predicate:      spec.Predicate,
	}, nil
}

func (p *preparedCSVSource) load(spec SourceLoadSpec) (*table.Table, error) {
	if p.loaded {
		return nil, fmt.Errorf("prepared source already loaded")
	}
	p.loaded = true
	defer p.close()

	if p.empty {
		return table.NewTable(nil), nil
	}

	cfg := p.cfg
	plan, err := csvPreparedLoadPlanFor(p.columns, p.types, p.schemas, spec, cfg.source)
	if err != nil {
		return nil, err
	}
	t := table.NewTableWithSchemas(plan.output.columns, plan.output.schemas)
	mapping := csvColumnMapping(p.columns, p.columns)
	badRecords := 0
	for _, row := range p.sampleRows {
		vals, keep, err := preparedCSVOutputRow(row, mapping, cfg.source, p.columns, p.types, plan, cfg, &badRecords)
		if err != nil {
			return nil, err
		}
		if keep {
			if err := t.AddRowTypedExact(vals); err != nil {
				return nil, err
			}
		}
	}
	for _, row := range p.pendingRows {
		vals, keep, err := preparedCSVOutputRow(row, mapping, cfg.source, p.columns, p.types, plan, cfg, &badRecords)
		if err != nil {
			return nil, err
		}
		if keep {
			if err := t.AddRowTypedExact(vals); err != nil {
				return nil, err
			}
		}
	}

	rowNum := p.startRow
	for {
		record, err := p.reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("error reading CSV row: %w", err)
		}
		if err := validateCSVRecord(record, len(p.columns), cfg, rowNum); err != nil {
			return nil, err
		}
		row := csvRawRow{record: record, rowNum: rowNum}
		vals, keep, err := preparedCSVOutputRow(row, mapping, cfg.source, p.columns, p.types, plan, cfg, &badRecords)
		if err != nil {
			return nil, err
		}
		if keep {
			if err := t.AddRowTypedExact(vals); err != nil {
				return nil, err
			}
		}
		rowNum++
	}
	return t, nil
}

func preparedCSVOutputRow(row csvRawRow, mapping []int, source string, columns []string, types []table.ValueType, plan csvPreparedLoadPlan, cfg csvLoadConfig, badRecords *int) ([]table.Value, bool, error) {
	readVals, err := csvTypedRowValues(row, mapping, source, columns, types, plan.read)
	if err != nil {
		(*badRecords)++
		if *badRecords > cfg.maxBadRecords {
			return nil, false, err
		}
		return nil, false, nil
	}
	if plan.predicate != nil {
		keep, err := plan.predicate(readVals)
		if err != nil {
			return nil, false, err
		}
		if !keep {
			return nil, false, nil
		}
	}
	vals := make([]table.Value, len(plan.outputFromRead))
	for i, readIdx := range plan.outputFromRead {
		vals[i] = readVals[readIdx]
	}
	return vals, true, nil
}

func (p *preparedCSVSource) stream(spec SourceLoadSpec) (rowstream.Stream, error) {
	if p.loaded {
		return nil, fmt.Errorf("prepared source already loaded")
	}
	p.loaded = true
	if p.empty {
		return &csvPreparedStream{
			closer: p.closer,
			schema: table.NewSchema(nil, nil),
			closed: false,
		}, nil
	}
	plan, err := csvPreparedLoadPlanFor(p.columns, p.types, p.schemas, spec, p.cfg.source)
	if err != nil {
		_ = p.close()
		return nil, err
	}
	buffered := make([]csvRawRow, 0, len(p.sampleRows)+len(p.pendingRows))
	buffered = append(buffered, p.sampleRows...)
	buffered = append(buffered, p.pendingRows...)
	return &csvPreparedStream{
		closer:   p.closer,
		reader:   p.reader,
		cfg:      p.cfg,
		columns:  append([]string(nil), p.columns...),
		types:    append([]table.ValueType(nil), p.types...),
		mapping:  csvColumnMapping(p.columns, p.columns),
		plan:     plan,
		buffered: buffered,
		rowNum:   p.startRow,
		schema:   table.NewSchema(plan.output.columns, plan.output.schemas),
	}, nil
}

type csvPreparedStream struct {
	closer     io.Closer
	reader     *csv.Reader
	cfg        csvLoadConfig
	columns    []string
	types      []table.ValueType
	mapping    []int
	plan       csvPreparedLoadPlan
	buffered   []csvRawRow
	bufferedAt int
	rowNum     int
	badRecords int
	schema     table.Schema
	closed     bool
}

func (s *csvPreparedStream) Schema() table.Schema {
	return s.schema
}

func (s *csvPreparedStream) Next() (rowstream.Row, bool, error) {
	for {
		var row csvRawRow
		if s.bufferedAt < len(s.buffered) {
			row = s.buffered[s.bufferedAt]
			s.bufferedAt++
		} else {
			if s.reader == nil {
				if err := s.Close(); err != nil {
					return nil, false, err
				}
				return nil, false, nil
			}
			record, err := s.reader.Read()
			if err == io.EOF {
				if err := s.Close(); err != nil {
					return nil, false, err
				}
				return nil, false, nil
			}
			if err != nil {
				return nil, false, fmt.Errorf("error reading CSV row: %w", err)
			}
			if err := validateCSVRecord(record, len(s.columns), s.cfg, s.rowNum); err != nil {
				return nil, false, err
			}
			row = csvRawRow{record: record, rowNum: s.rowNum}
			s.rowNum++
		}
		vals, keep, err := preparedCSVOutputRow(row, s.mapping, s.cfg.source, s.columns, s.types, s.plan, s.cfg, &s.badRecords)
		if err != nil {
			return nil, false, err
		}
		if keep {
			return vals, true, nil
		}
	}
}

func (s *csvPreparedStream) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	if s.closer == nil {
		return nil
	}
	err := s.closer.Close()
	s.closer = nil
	return err
}

func (p *preparedCSVSource) close() error {
	if p.closer == nil {
		return nil
	}
	err := p.closer.Close()
	p.closer = nil
	return err
}

func (p *preparedJSONSource) load(spec SourceLoadSpec) (*table.Table, error) {
	if p.loaded {
		return nil, fmt.Errorf("prepared source already loaded")
	}
	p.loaded = true

	plan, err := preparedSourceLoadPlanFor(table.NewSchema(p.state.columns, p.state.schemas), spec, p.filename)
	if err != nil {
		return nil, err
	}

	records := p.records
	if !p.recordsComplete {
		switch p.format {
		case "json":
			records, err = collectJSONFileRecords(p.filename, p.cfg)
		case "jsonl":
			records, err = collectJSONLFileRecords(p.filename, p.cfg)
		default:
			return nil, fmt.Errorf("prepared json source: unsupported format %q", p.format)
		}
		if err != nil {
			return nil, err
		}
	}
	return buildTableFromJSONRecordsWithPlan(records, p.cfg, plan)
}

func (p *preparedJSONSource) stream(spec SourceLoadSpec) (rowstream.Stream, error) {
	if p.loaded {
		return nil, fmt.Errorf("prepared source already loaded")
	}
	p.loaded = true

	plan, err := preparedSourceLoadPlanFor(table.NewSchema(p.state.columns, p.state.schemas), spec, p.filename)
	if err != nil {
		return nil, err
	}
	return &preparedJSONStream{
		filename:        p.filename,
		format:          p.format,
		cfg:             p.cfg,
		plan:            plan,
		buffered:        append([]jsonLogicalRecord(nil), p.records...),
		recordsComplete: p.recordsComplete,
		schema:          table.NewSchema(plan.outputColumns, plan.outputSchemas),
	}, nil
}

type preparedJSONStream struct {
	filename        string
	format          string
	cfg             jsonLoadConfig
	plan            preparedSourceLoadPlan
	buffered        []jsonLogicalRecord
	bufferedAt      int
	recordsComplete bool
	rest            jsonRecordStream
	restOpened      bool
	nextRow         int
	badRecords      int
	schema          table.Schema
	closed          bool
}

type jsonRecordStream interface {
	Next() (jsonLogicalRecord, bool, error)
	Close() error
}

func (s *preparedJSONStream) Schema() table.Schema {
	return s.schema
}

func (s *preparedJSONStream) Next() (rowstream.Row, bool, error) {
	for {
		rec, ok, err := s.nextRecord()
		if err != nil || !ok {
			return nil, ok, err
		}
		rowIdx := s.nextRow
		s.nextRow++
		vals, keep, err := preparedJSONOutputRow(rec, rowIdx, s.cfg, s.plan, &s.badRecords)
		if err != nil {
			return nil, false, err
		}
		if keep {
			return vals, true, nil
		}
	}
}

func (s *preparedJSONStream) nextRecord() (jsonLogicalRecord, bool, error) {
	if s.bufferedAt < len(s.buffered) {
		rec := s.buffered[s.bufferedAt]
		s.bufferedAt++
		return rec, true, nil
	}
	if s.recordsComplete {
		if err := s.Close(); err != nil {
			return jsonLogicalRecord{}, false, err
		}
		return jsonLogicalRecord{}, false, nil
	}
	if !s.restOpened {
		rest, err := openJSONRestStream(s.filename, s.format, s.cfg, len(s.buffered))
		if err != nil {
			return jsonLogicalRecord{}, false, err
		}
		s.rest = rest
		s.restOpened = true
	}
	rec, ok, err := s.rest.Next()
	if err != nil || !ok {
		if !ok {
			if err := s.Close(); err != nil {
				return jsonLogicalRecord{}, false, err
			}
		}
		return rec, ok, err
	}
	return rec, true, nil
}

func (s *preparedJSONStream) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	if s.rest != nil {
		return s.rest.Close()
	}
	return nil
}

func openJSONRestStream(filename, format string, cfg jsonLoadConfig, skip int) (jsonRecordStream, error) {
	f, err := openInputReader(filename, cfg.compression)
	if err != nil {
		return nil, err
	}
	switch format {
	case "json":
		stream, err := newJSONArrayRecordStream(f, cfg, skip)
		if err != nil {
			_ = f.Close()
			return nil, err
		}
		return stream, nil
	case "jsonl":
		return newJSONLRecordStream(f, cfg, skip), nil
	default:
		_ = f.Close()
		return nil, fmt.Errorf("prepared json source: unsupported format %q", format)
	}
}

type jsonArrayRecordStream struct {
	closer io.Closer
	dec    *json.Decoder
	cfg    jsonLoadConfig
	rowIdx int
	closed bool
}

func newJSONArrayRecordStream(closer io.Closer, cfg jsonLoadConfig, skip int) (*jsonArrayRecordStream, error) {
	r, ok := closer.(io.Reader)
	if !ok {
		return nil, fmt.Errorf("json stream: source is not readable")
	}
	dec := json.NewDecoder(r)
	dec.UseNumber()
	tok, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("cannot parse JSON: %w (expected array of objects)", err)
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '[' {
		return nil, fmt.Errorf("cannot parse JSON: expected array of objects")
	}
	stream := &jsonArrayRecordStream{closer: closer, dec: dec, cfg: cfg}
	for i := 0; i < skip; i++ {
		if !dec.More() {
			if err := stream.consumeEnd(); err != nil {
				return nil, err
			}
			return stream, nil
		}
		if _, err := decodeJSONElementRecord(dec, cfg.source, i); err != nil {
			return nil, err
		}
		stream.rowIdx++
	}
	return stream, nil
}

func (s *jsonArrayRecordStream) Next() (jsonLogicalRecord, bool, error) {
	if s.closed {
		return jsonLogicalRecord{}, false, nil
	}
	if !s.dec.More() {
		if err := s.consumeEnd(); err != nil {
			return jsonLogicalRecord{}, false, err
		}
		if err := s.Close(); err != nil {
			return jsonLogicalRecord{}, false, err
		}
		return jsonLogicalRecord{}, false, nil
	}
	rec, err := decodeJSONElementRecord(s.dec, s.cfg.source, s.rowIdx)
	if err != nil {
		return jsonLogicalRecord{}, false, err
	}
	s.rowIdx++
	return rec, true, nil
}

func (s *jsonArrayRecordStream) consumeEnd() error {
	return validateJSONArrayEnd(s.dec)
}

func (s *jsonArrayRecordStream) DrainSyntax() error {
	if s.closed {
		return nil
	}
	if err := validateJSONArraySyntaxRemainder(s.dec); err != nil {
		_ = s.Close()
		return err
	}
	return s.Close()
}

func (s *jsonArrayRecordStream) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	return s.closer.Close()
}

type jsonlRecordStream struct {
	closer  io.Closer
	scanner *bufio.Scanner
	source  string
	lineNum int
	closed  bool
}

func newJSONLRecordStream(closer io.Closer, cfg jsonLoadConfig, skip int) *jsonlRecordStream {
	r := closer.(io.Reader)
	stream := &jsonlRecordStream{closer: closer, scanner: newJSONLScanner(r), source: cfg.source}
	for skipped := 0; skipped < skip && stream.scanner.Scan(); {
		stream.lineNum++
		if strings.TrimSpace(stream.scanner.Text()) == "" {
			continue
		}
		skipped++
	}
	return stream
}

func (s *jsonlRecordStream) Next() (jsonLogicalRecord, bool, error) {
	if s.closed {
		return jsonLogicalRecord{}, false, nil
	}
	for s.scanner.Scan() {
		s.lineNum++
		line := strings.TrimSpace(s.scanner.Text())
		if line == "" {
			continue
		}
		return decodeJSONLLineRecord(line, s.source, s.lineNum), true, nil
	}
	if err := s.scanner.Err(); err != nil {
		return jsonLogicalRecord{}, false, fmt.Errorf("error reading JSONL: %w", err)
	}
	if err := s.Close(); err != nil {
		return jsonLogicalRecord{}, false, err
	}
	return jsonLogicalRecord{}, false, nil
}

func (s *jsonlRecordStream) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	return s.closer.Close()
}

func (p *preparedFileSource) load(spec SourceLoadSpec) (*table.Table, error) {
	if p.loaded {
		return nil, fmt.Errorf("prepared source already loaded")
	}
	p.loaded = true
	plan, err := preparedSourceLoadPlanFor(p.schema, spec, p.filename)
	if err != nil {
		return nil, err
	}
	format, _ := resolveFormatCompression(p.filename, p.opts)
	switch format {
	case "avro":
		return loadPreparedAvroSource(p.filename, plan)
	case "parquet":
		return loadPreparedParquetSource(p.filename, plan)
	default:
		return nil, fmt.Errorf("prepared source: unsupported metadata format %q", format)
	}
}

func (p *preparedFileSource) stream(spec SourceLoadSpec) (rowstream.Stream, error) {
	if p.loaded {
		return nil, fmt.Errorf("prepared source already loaded")
	}
	p.loaded = true
	plan, err := preparedSourceLoadPlanFor(p.schema, spec, p.filename)
	if err != nil {
		return nil, err
	}
	format, _ := resolveFormatCompression(p.filename, p.opts)
	switch format {
	case "avro":
		return streamPreparedAvroSource(p.filename, plan)
	case "parquet":
		return streamPreparedParquetSource(p.filename, plan)
	default:
		return nil, fmt.Errorf("prepared source: unsupported metadata format %q", format)
	}
}

type preparedJSONInspection struct {
	state           jsonSchemaInference
	records         []jsonLogicalRecord
	recordsComplete bool
}

func inspectJSONLikeSchema(filename, format string, cfg jsonLoadConfig) (preparedJSONInspection, error) {
	f, err := openInputReader(filename, cfg.compression)
	if err != nil {
		return preparedJSONInspection{}, err
	}
	defer f.Close()

	switch format {
	case "json":
		return inspectJSONSchemaFromReader(f, cfg)
	case "jsonl":
		return inspectJSONLSchemaFromReader(f, cfg)
	default:
		return preparedJSONInspection{}, fmt.Errorf("prepare source: unsupported format %q", format)
	}
}

func inspectJSONSchemaFromReader(r io.Reader, cfg jsonLoadConfig) (preparedJSONInspection, error) {
	dec := json.NewDecoder(r)
	dec.UseNumber()

	tok, err := dec.Token()
	if err != nil {
		return preparedJSONInspection{}, fmt.Errorf("cannot parse JSON: %w (expected array of objects)", err)
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '[' {
		return preparedJSONInspection{}, fmt.Errorf("cannot parse JSON: expected array of objects")
	}

	state := jsonSchemaInference{index: map[string]int{}}
	var records []jsonLogicalRecord
	badRecords := 0
	inferSeen := 0
	sampleExhausted := true
	for dec.More() {
		if cfg.inferRows >= 0 && inferSeen >= cfg.inferRows {
			sampleExhausted = false
			break
		}
		rowIdx := inferSeen
		inferSeen++
		rec, err := decodeJSONElementRecord(dec, cfg.source, rowIdx)
		if err != nil {
			return preparedJSONInspection{}, err
		}
		records = append(records, rec)
		if err := inferPreparedJSONRecord(&state, rec, rowIdx, cfg, &badRecords); err != nil {
			return preparedJSONInspection{}, err
		}
	}
	if sampleExhausted {
		if err := validateJSONArrayEnd(dec); err != nil {
			return preparedJSONInspection{}, err
		}
	} else if err := validateJSONArraySyntaxRemainder(dec); err != nil {
		return preparedJSONInspection{}, err
	}
	if jsonPreparedInferenceNeedsConservativeNullability(cfg.inferRows, inferSeen, sampleExhausted) {
		state.schemas = deepNullableSchemas(state.schemas)
	}
	return preparedJSONInspection{
		state:           state,
		records:         records,
		recordsComplete: sampleExhausted,
	}, nil
}

func validateJSONArraySyntaxRemainder(dec *json.Decoder) error {
	for dec.More() {
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return fmt.Errorf("cannot parse JSON: %w (expected array of objects)", err)
		}
	}
	return validateJSONArrayEnd(dec)
}

func validateJSONArrayEnd(dec *json.Decoder) error {
	if _, err := dec.Token(); err != nil {
		return fmt.Errorf("cannot parse JSON: %w (expected array of objects)", err)
	}
	if err := requireJSONDecoderEOF(dec); err != nil {
		return fmt.Errorf("cannot parse JSON: %w (expected array of objects)", err)
	}
	return nil
}

func decodeJSONElementRecord(dec *json.Decoder, source string, rowIdx int) (jsonLogicalRecord, error) {
	loc := fmt.Sprintf("row %d", rowIdx+1)
	var value interface{}
	if err := dec.Decode(&value); err != nil {
		return jsonLogicalRecord{}, fmt.Errorf("cannot parse JSON: %w (expected array of objects)", err)
	}
	rec, ok := value.(map[string]interface{})
	if !ok || rec == nil {
		return jsonLogicalRecord{loc: loc, source: source, err: fmt.Errorf("expected JSON object")}, nil
	}
	fields, err := buildJSONRecordFields(rec)
	if err != nil {
		return jsonLogicalRecord{loc: loc, source: source, err: err}, nil
	}
	return jsonLogicalRecord{fields: fields, loc: loc, source: source}, nil
}

func inspectJSONLSchemaFromReader(r io.Reader, cfg jsonLoadConfig) (preparedJSONInspection, error) {
	scanner := newJSONLScanner(r)
	state := jsonSchemaInference{index: map[string]int{}}
	var records []jsonLogicalRecord
	badRecords := 0
	inferSeen := 0
	lineNum := 0
	sampleExhausted := true

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if cfg.inferRows >= 0 && inferSeen >= cfg.inferRows {
			sampleExhausted = false
			break
		}
		rowIdx := inferSeen
		inferSeen++
		rec := decodeJSONLLineRecord(line, cfg.source, lineNum)
		records = append(records, rec)
		if err := inferPreparedJSONRecord(&state, rec, rowIdx, cfg, &badRecords); err != nil {
			return preparedJSONInspection{}, err
		}
	}
	if err := scanner.Err(); err != nil {
		return preparedJSONInspection{}, fmt.Errorf("error reading JSONL: %w", err)
	}
	if jsonPreparedInferenceNeedsConservativeNullability(cfg.inferRows, inferSeen, sampleExhausted) {
		state.schemas = deepNullableSchemas(state.schemas)
	}
	return preparedJSONInspection{
		state:           state,
		records:         records,
		recordsComplete: sampleExhausted,
	}, nil
}

func decodeJSONLLineRecord(line, source string, lineNum int) jsonLogicalRecord {
	loc := fmt.Sprintf("line %d", lineNum)
	var value interface{}
	dec := json.NewDecoder(strings.NewReader(line))
	dec.UseNumber()
	if err := dec.Decode(&value); err != nil {
		return jsonLogicalRecord{loc: loc, source: source, err: fmt.Errorf("invalid JSON: %w", err)}
	}
	if err := requireJSONDecoderEOF(dec); err != nil {
		return jsonLogicalRecord{loc: loc, source: source, err: fmt.Errorf("invalid JSON: %w", err)}
	}
	rec, ok := value.(map[string]interface{})
	if !ok || rec == nil {
		return jsonLogicalRecord{loc: loc, source: source, err: fmt.Errorf("expected JSON object")}
	}
	fields, err := buildJSONRecordFields(rec)
	if err != nil {
		return jsonLogicalRecord{loc: loc, source: source, err: err}
	}
	return jsonLogicalRecord{fields: fields, loc: loc, source: source}
}

func inferPreparedJSONRecord(state *jsonSchemaInference, rec jsonLogicalRecord, rowIdx int, cfg jsonLoadConfig, badRecords *int) error {
	if rec.err != nil {
		return countJSONBadRecord(rec, rowIdx, rec.err, cfg.maxBadRecords, badRecords)
	}
	var err error
	if cfg.maxBadRecords == 0 {
		err = state.inferRecordInPlace(rec.fields)
	} else {
		err = state.inferRecord(rec.fields)
	}
	if err != nil {
		return countJSONBadRecord(rec, rowIdx, err, cfg.maxBadRecords, badRecords)
	}
	return nil
}

func jsonPreparedInferenceNeedsConservativeNullability(inferRows, inferSeen int, sampleExhausted bool) bool {
	if inferRows < 0 || inferSeen == 0 {
		return false
	}
	return !sampleExhausted
}

func buildTableFromJSONRecordsWithPlan(records []jsonLogicalRecord, cfg jsonLoadConfig, plan preparedSourceLoadPlan) (*table.Table, error) {
	t := table.NewTableWithSchemas(plan.outputColumns, plan.outputSchemas)
	badRecords := 0
	for i, rec := range records {
		vals, keep, err := preparedJSONOutputRow(rec, i, cfg, plan, &badRecords)
		if err != nil {
			return nil, err
		}
		if keep {
			if err := t.AddRowTyped(vals); err != nil {
				return nil, err
			}
		}
	}
	return t, nil
}

func preparedJSONOutputRow(rec jsonLogicalRecord, rowIdx int, cfg jsonLoadConfig, plan preparedSourceLoadPlan, badRecords *int) ([]table.Value, bool, error) {
	if rec.err != nil {
		if err := countJSONBadRecord(rec, rowIdx, rec.err, cfg.maxBadRecords, badRecords); err != nil {
			return nil, false, err
		}
		return nil, false, nil
	}
	readVals, err := jsonRecordReadValues(rec.fields, plan)
	if err != nil {
		if err := countJSONBadRecord(rec, rowIdx, err, cfg.maxBadRecords, badRecords); err != nil {
			return nil, false, err
		}
		return nil, false, nil
	}
	if plan.predicate != nil {
		keep, err := plan.predicate(readVals)
		if err != nil {
			return nil, false, err
		}
		if !keep {
			return nil, false, nil
		}
	}
	vals := make([]table.Value, len(plan.outputFromRead))
	for i, readIdx := range plan.outputFromRead {
		vals[i] = readVals[readIdx]
	}
	return vals, true, nil
}

func jsonRecordReadValues(fields []table.RecordField, plan preparedSourceLoadPlan) ([]table.Value, error) {
	if plan.readAll {
		for _, field := range fields {
			if !sourceColumnNameExists(plan.sourceColumns, field.Name) {
				return nil, jsonUnknownFieldError{Path: field.Name}
			}
		}
	}
	vals := make([]table.Value, len(plan.readSourceIndexes))
	for readIdx, sourceIdx := range plan.readSourceIndexes {
		name := plan.sourceColumns[sourceIdx]
		schema := plan.sourceSchemas[sourceIdx]
		v := table.Null()
		if field, ok := recordFieldByName(fields, name); ok {
			v = field.Value
			if err := rejectUnknownJSONFields(v, schema, name); err != nil {
				return nil, err
			}
		}
		cv, err := table.CoerceValueToSchemaAtPath(v, schema, name)
		if err != nil {
			return nil, err
		}
		vals[readIdx] = cv
	}
	return vals, nil
}

func sourceColumnNameExists(columns []string, name string) bool {
	for _, col := range columns {
		if col == name {
			return true
		}
	}
	return false
}

func cloneJSONSchemaInference(state jsonSchemaInference) jsonSchemaInference {
	schema := table.NewSchema(state.columns, state.schemas)
	return jsonSchemaInference{
		columns:  schemaColumns(schema),
		schemas:  schemaTypeDescriptors(schema),
		index:    cloneStringIntMap(state.index),
		goodRows: state.goodRows,
	}
}

func schemaColumns(schema table.Schema) []string {
	cols := make([]string, len(schema.Columns))
	for i, col := range schema.Columns {
		cols[i] = col.Name
	}
	return cols
}

func cloneStringIntMap(in map[string]int) map[string]int {
	out := make(map[string]int, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func inspectAvroSchema(filename string) (table.Schema, error) {
	f, err := os.Open(filename)
	if err != nil {
		return table.Schema{}, fmt.Errorf("cannot open %s: %w", filename, err)
	}
	defer f.Close()

	ocfr, err := goavro.NewOCFReader(f)
	if err != nil {
		return table.Schema{}, fmt.Errorf("cannot read Avro OCF from %s: %w", filename, err)
	}
	columns, schemas, _, err := avroSchemaParts(ocfr.Codec().Schema())
	if err != nil {
		return table.Schema{}, err
	}
	return table.NewSchema(columns, schemas), nil
}

func inspectParquetSchema(filename string) (table.Schema, error) {
	f, err := os.Open(filename)
	if err != nil {
		return table.Schema{}, fmt.Errorf("cannot open %s: %w", filename, err)
	}
	defer f.Close()

	reader := parquet.NewGenericReader[any](f)
	defer reader.Close()

	schema := reader.Schema()
	columns := parquetColumns(schema, reader)
	schemas := parquetColumnSchemas(schema, columns)
	return table.NewSchema(columns, schemas), nil
}

func loadPreparedAvroSource(filename string, plan preparedSourceLoadPlan) (*table.Table, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("cannot open %s: %w", filename, err)
	}
	defer f.Close()

	ocfr, err := goavro.NewOCFReader(f)
	if err != nil {
		return nil, fmt.Errorf("cannot read Avro OCF from %s: %w", filename, err)
	}
	columns, schemas, fieldSchemas, err := avroSchemaParts(ocfr.Codec().Schema())
	if err != nil {
		return nil, err
	}
	if err := validatePreparedSourceSchema(plan, table.NewSchema(columns, schemas)); err != nil {
		return nil, err
	}

	t := table.NewTableWithSchemas(plan.outputColumns, plan.outputSchemas)
	for ocfr.Scan() {
		datum, err := ocfr.Read()
		if err != nil {
			return nil, fmt.Errorf("error reading Avro record: %w", err)
		}
		rec, ok := datum.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("unexpected Avro record type %T", datum)
		}
		readVals := make([]table.Value, len(plan.readSourceIndexes))
		for readIdx, sourceIdx := range plan.readSourceIndexes {
			col := plan.sourceColumns[sourceIdx]
			v, exists := rec[col]
			if !exists || v == nil {
				readVals[readIdx] = table.Null()
				continue
			}
			val := fieldSchemas.context.value(v, fieldSchemas.schemas[col], fieldSchemas.rootNamespace)
			cv, err := table.CoerceValueToSchemaAtPath(val, plan.sourceSchemas[sourceIdx], col)
			if err != nil {
				return nil, fmt.Errorf("error materializing Avro record: %w", err)
			}
			readVals[readIdx] = cv
		}
		if err := addPreparedSourceReadRow(t, readVals, plan); err != nil {
			return nil, fmt.Errorf("error materializing Avro record: %w", err)
		}
	}
	if err := ocfr.Err(); err != nil {
		return nil, fmt.Errorf("error reading Avro file: %w", err)
	}
	return t, nil
}

func loadPreparedParquetSource(filename string, plan preparedSourceLoadPlan) (*table.Table, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("cannot open %s: %w", filename, err)
	}
	defer f.Close()

	reader := parquet.NewGenericReader[any](f)
	defer reader.Close()

	schema := reader.Schema()
	columns := parquetColumns(schema, reader)
	schemas := parquetColumnSchemas(schema, columns)
	if err := validatePreparedSourceSchema(plan, table.NewSchema(columns, schemas)); err != nil {
		return nil, err
	}

	t := table.NewTableWithSchemas(plan.outputColumns, plan.outputSchemas)
	buf := make([]any, 128)
	for {
		n, err := reader.Read(buf)
		for i := 0; i < n; i++ {
			row, ok := buf[i].(map[string]any)
			if !ok {
				return nil, fmt.Errorf("unexpected parquet row type %T", buf[i])
			}
			readVals := make([]table.Value, len(plan.readSourceIndexes))
			for readIdx, sourceIdx := range plan.readSourceIndexes {
				col := plan.sourceColumns[sourceIdx]
				val := parquetValue(row[col], plan.sourceSchemas[sourceIdx])
				cv, err := table.CoerceValueToSchemaAtPath(val, plan.sourceSchemas[sourceIdx], col)
				if err != nil {
					return nil, fmt.Errorf("error materializing Parquet row: %w", err)
				}
				readVals[readIdx] = cv
			}
			if err := addPreparedSourceReadRow(t, readVals, plan); err != nil {
				return nil, fmt.Errorf("error materializing Parquet row: %w", err)
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

func streamPreparedAvroSource(filename string, plan preparedSourceLoadPlan) (rowstream.Stream, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("cannot open %s: %w", filename, err)
	}
	ocfr, err := goavro.NewOCFReader(f)
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("cannot read Avro OCF from %s: %w", filename, err)
	}
	columns, schemas, fieldSchemas, err := avroSchemaParts(ocfr.Codec().Schema())
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	if err := validatePreparedSourceSchema(plan, table.NewSchema(columns, schemas)); err != nil {
		_ = f.Close()
		return nil, err
	}
	return &avroPreparedStream{
		closer:       f,
		reader:       ocfr,
		plan:         plan,
		fieldSchemas: fieldSchemas,
		schema:       table.NewSchema(plan.outputColumns, plan.outputSchemas),
	}, nil
}

type avroPreparedStream struct {
	closer       io.Closer
	reader       *goavro.OCFReader
	plan         preparedSourceLoadPlan
	fieldSchemas avroFieldSchemas
	schema       table.Schema
	closed       bool
}

func (s *avroPreparedStream) Schema() table.Schema {
	return s.schema
}

func (s *avroPreparedStream) Next() (rowstream.Row, bool, error) {
	for s.reader.Scan() {
		datum, err := s.reader.Read()
		if err != nil {
			return nil, false, fmt.Errorf("error reading Avro record: %w", err)
		}
		rec, ok := datum.(map[string]interface{})
		if !ok {
			return nil, false, fmt.Errorf("unexpected Avro record type %T", datum)
		}
		readVals := make([]table.Value, len(s.plan.readSourceIndexes))
		for readIdx, sourceIdx := range s.plan.readSourceIndexes {
			col := s.plan.sourceColumns[sourceIdx]
			v, exists := rec[col]
			if !exists || v == nil {
				readVals[readIdx] = table.Null()
				continue
			}
			val := s.fieldSchemas.context.value(v, s.fieldSchemas.schemas[col], s.fieldSchemas.rootNamespace)
			cv, err := table.CoerceValueToSchemaAtPath(val, s.plan.sourceSchemas[sourceIdx], col)
			if err != nil {
				return nil, false, fmt.Errorf("error materializing Avro record: %w", err)
			}
			readVals[readIdx] = cv
		}
		vals, keep, err := preparedSourceOutputRow(readVals, s.plan)
		if err != nil {
			return nil, false, fmt.Errorf("error materializing Avro record: %w", err)
		}
		if keep {
			return vals, true, nil
		}
	}
	if err := s.reader.Err(); err != nil {
		return nil, false, fmt.Errorf("error reading Avro file: %w", err)
	}
	if err := s.Close(); err != nil {
		return nil, false, err
	}
	return nil, false, nil
}

func (s *avroPreparedStream) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	return s.closer.Close()
}

func streamPreparedParquetSource(filename string, plan preparedSourceLoadPlan) (rowstream.Stream, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("cannot open %s: %w", filename, err)
	}
	reader := parquet.NewGenericReader[any](f)
	schema := reader.Schema()
	columns := parquetColumns(schema, reader)
	schemas := parquetColumnSchemas(schema, columns)
	if err := validatePreparedSourceSchema(plan, table.NewSchema(columns, schemas)); err != nil {
		_ = reader.Close()
		_ = f.Close()
		return nil, err
	}
	return &parquetPreparedStream{
		file:   f,
		reader: reader,
		plan:   plan,
		buf:    make([]any, 128),
		schema: table.NewSchema(plan.outputColumns, plan.outputSchemas),
	}, nil
}

type parquetPreparedStream struct {
	file     io.Closer
	reader   *parquet.GenericReader[any]
	plan     preparedSourceLoadPlan
	buf      []any
	bufN     int
	bufAt    int
	schema   table.Schema
	closed   bool
	finished bool
}

func (s *parquetPreparedStream) Schema() table.Schema {
	return s.schema
}

func (s *parquetPreparedStream) Next() (rowstream.Row, bool, error) {
	for {
		if s.bufAt < s.bufN {
			row, ok := s.buf[s.bufAt].(map[string]any)
			s.bufAt++
			if !ok {
				return nil, false, fmt.Errorf("unexpected parquet row type %T", s.buf[s.bufAt-1])
			}
			readVals := make([]table.Value, len(s.plan.readSourceIndexes))
			for readIdx, sourceIdx := range s.plan.readSourceIndexes {
				col := s.plan.sourceColumns[sourceIdx]
				val := parquetValue(row[col], s.plan.sourceSchemas[sourceIdx])
				cv, err := table.CoerceValueToSchemaAtPath(val, s.plan.sourceSchemas[sourceIdx], col)
				if err != nil {
					return nil, false, fmt.Errorf("error materializing Parquet row: %w", err)
				}
				readVals[readIdx] = cv
			}
			vals, keep, err := preparedSourceOutputRow(readVals, s.plan)
			if err != nil {
				return nil, false, fmt.Errorf("error materializing Parquet row: %w", err)
			}
			if keep {
				return vals, true, nil
			}
			continue
		}
		if s.finished {
			if err := s.Close(); err != nil {
				return nil, false, err
			}
			return nil, false, nil
		}
		n, err := s.reader.Read(s.buf)
		s.bufN = n
		s.bufAt = 0
		if err == io.EOF {
			s.finished = true
			if n == 0 {
				continue
			}
		} else if err != nil {
			return nil, false, fmt.Errorf("error reading Parquet rows: %w", err)
		}
	}
}

func (s *parquetPreparedStream) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	err := s.reader.Close()
	if fileErr := s.file.Close(); err == nil {
		err = fileErr
	}
	return err
}

func validatePreparedSourceSchema(plan preparedSourceLoadPlan, got table.Schema) error {
	if len(plan.sourceColumns) != len(got.Columns) {
		return fmt.Errorf("prepared source schema column count changed: planned %d columns, got %d", len(plan.sourceColumns), len(got.Columns))
	}
	for i, col := range got.Columns {
		if plan.sourceColumns[i] != col.Name {
			return fmt.Errorf("prepared source schema column %d changed: planned %q, got %q", i, plan.sourceColumns[i], col.Name)
		}
		if !table.SchemaAssignable(plan.sourceSchemas[i], col.Type, table.AssignExactMode) {
			return fmt.Errorf("prepared source schema for column %q changed: planned %s, got %s", col.Name, table.Render(plan.sourceSchemas[i]), table.Render(col.Type))
		}
	}
	return nil
}
