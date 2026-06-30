package loader

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/razeghi71/dq/ast"
	"github.com/razeghi71/dq/rowstream"
	"github.com/razeghi71/dq/table"
)

type preparedLiveJSONSource struct {
	format          string
	cfg             jsonLoadConfig
	state           jsonSchemaInference
	records         []jsonLogicalRecord
	recordsComplete bool
	rest            jsonRecordStream
	loaded          bool
}

func prepareStdin(stdin io.Reader, opts Options) (*PreparedSource, error) {
	if opts.Format == "" {
		return nil, fmt.Errorf("reading from stdin requires with format=... in query (%s)", ast.StreamFormatsList())
	}
	format := opts.Format
	if err := validateOptionsForFormat(opts, format); err != nil {
		return nil, err
	}
	if !ast.IsStreamLoadFormat(format) {
		return nil, fmt.Errorf("stdin source: unsupported format %q (supported: %s)", format, ast.StreamFormatsList())
	}
	if stdin == nil {
		stdin = os.Stdin
	}
	reader, err := wrapInputReadCloser(io.NopCloser(stdin), opts.Compression)
	if err != nil {
		return nil, fmt.Errorf("%s stdin: %w", compressionOpenAction(opts.Compression), err)
	}

	switch format {
	case "csv":
		cfg := csvConfigFromOptions(opts, nil)
		cfg.compression = ""
		cfg.source = StdinSource
		prepared, err := prepareCSVSourceReader(reader, cfg)
		if err != nil {
			_ = reader.Close()
			return nil, err
		}
		return prepared, nil
	case "json", "jsonl":
		cfg := jsonConfigFromOptions(opts, StdinSource)
		cfg.compression = ""
		prepared, err := prepareLiveJSONSource(reader, format, cfg)
		if err != nil {
			_ = reader.Close()
			return nil, err
		}
		return prepared, nil
	default:
		_ = reader.Close()
		return nil, fmt.Errorf("stdin source: unsupported format %q (supported: %s)", format, ast.StreamFormatsList())
	}
}

func prepareLiveJSONSource(reader io.ReadCloser, format string, cfg jsonLoadConfig) (*PreparedSource, error) {
	records, rest, complete, state, err := inspectLiveJSONSource(reader, format, cfg)
	if err != nil {
		if rest != nil {
			_ = rest.Close()
		}
		return nil, err
	}
	return &PreparedSource{
		Schema: table.NewSchema(state.columns, state.schemas),
		liveJS: &preparedLiveJSONSource{
			format:          format,
			cfg:             cfg,
			state:           cloneJSONSchemaInference(state),
			records:         records,
			recordsComplete: complete,
			rest:            rest,
		},
	}, nil
}

func inspectLiveJSONSource(reader io.ReadCloser, format string, cfg jsonLoadConfig) ([]jsonLogicalRecord, jsonRecordStream, bool, jsonSchemaInference, error) {
	stream, err := openJSONRecordStream(reader, format, cfg)
	if err != nil {
		return nil, nil, false, jsonSchemaInference{}, err
	}

	state := jsonSchemaInference{index: map[string]int{}}
	var records []jsonLogicalRecord
	badRecords := 0
	inferSeen := 0
	sampleExhausted := true
	complete := false

	for {
		if cfg.inferRows >= 0 && inferSeen >= cfg.inferRows {
			sampleExhausted = false
			if format == "json" {
				tail, err := collectJSONStreamRemainder(stream)
				if err != nil {
					return nil, nil, false, jsonSchemaInference{}, err
				}
				records = append(records, tail...)
				complete = true
			}
			break
		}
		rec, ok, err := stream.Next()
		if err != nil {
			_ = stream.Close()
			return nil, nil, false, jsonSchemaInference{}, err
		}
		if !ok {
			complete = true
			break
		}
		rowIdx := inferSeen
		inferSeen++
		records = append(records, rec)
		if err := inferPreparedJSONRecord(&state, rec, rowIdx, cfg, &badRecords); err != nil {
			_ = stream.Close()
			return nil, nil, false, jsonSchemaInference{}, err
		}
	}
	if jsonPreparedInferenceNeedsConservativeNullability(cfg.inferRows, inferSeen, sampleExhausted) {
		state.schemas = deepNullableSchemas(state.schemas)
	}
	if complete {
		return records, nil, true, state, nil
	}
	return records, stream, false, state, nil
}

func collectJSONStreamRemainder(stream jsonRecordStream) ([]jsonLogicalRecord, error) {
	var records []jsonLogicalRecord
	for {
		rec, ok, err := stream.Next()
		if err != nil {
			_ = stream.Close()
			return nil, err
		}
		if !ok {
			return records, nil
		}
		records = append(records, rec)
	}
}

func openJSONRecordStream(reader io.ReadCloser, format string, cfg jsonLoadConfig) (jsonRecordStream, error) {
	switch format {
	case "json":
		return newJSONArrayRecordStream(reader, cfg, 0)
	case "jsonl":
		return newJSONLRecordStream(reader, cfg, 0), nil
	default:
		return nil, fmt.Errorf("prepared json source: unsupported format %q", format)
	}
}

func (p *preparedLiveJSONSource) load(spec SourceLoadSpec) (*table.Table, error) {
	stream, err := p.stream(spec)
	if err != nil {
		return nil, err
	}
	return rowstream.Materialize(stream)
}

func (p *preparedLiveJSONSource) stream(spec SourceLoadSpec) (rowstream.Stream, error) {
	if p.loaded {
		return nil, fmt.Errorf("prepared source already loaded")
	}
	p.loaded = true

	plan, err := preparedSourceLoadPlanFor(table.NewSchema(p.state.columns, p.state.schemas), spec, StdinSource)
	if err != nil {
		_ = p.close()
		return nil, err
	}
	restOpened := p.rest != nil
	return &preparedJSONStream{
		filename:        StdinSource,
		format:          p.format,
		cfg:             p.cfg,
		plan:            plan,
		buffered:        append([]jsonLogicalRecord(nil), p.records...),
		recordsComplete: p.recordsComplete,
		rest:            p.rest,
		restOpened:      restOpened,
		schema:          table.NewSchema(plan.outputColumns, plan.outputSchemas),
	}, nil
}

func (p *preparedLiveJSONSource) close() error {
	if p == nil || p.rest == nil {
		return nil
	}
	err := p.rest.Close()
	p.rest = nil
	return err
}

type preparedGlobSource struct {
	pattern string
	matches []string
	format  string
	opts    Options
	schema  table.Schema
	loaded  bool

	csv  *preparedGlobCSVSource
	js   *preparedGlobJSONSource
	file *preparedGlobFileSource
}

type preparedGlobCSVSource struct {
	cfg     csvLoadConfig
	anchor  []string
	shards  []csvGlobShardPlan
	columns []string
	types   []table.ValueType
	schemas []*table.TypeDescriptor
}

type csvGlobShardPlan struct {
	path    string
	columns []string
	empty   bool
}

type preparedGlobJSONSource struct {
	cfg jsonLoadConfig
}

type preparedGlobFileSource struct {
	schemas []table.Schema
}

func prepareGlob(pattern string, opts Options) (*PreparedSource, error) {
	matches, err := expandGlob(pattern)
	if err != nil {
		return nil, err
	}
	format, compression, err := validateUniformLoad(matches, opts)
	if err != nil {
		return nil, err
	}
	opts.Format = format
	opts.Compression = compression
	if err := validateOptionsForFormat(opts, format); err != nil {
		return nil, err
	}

	switch format {
	case "csv":
		return prepareGlobCSV(pattern, matches, opts)
	case "json", "jsonl":
		return prepareGlobJSON(pattern, matches, format, opts)
	case "avro", "parquet":
		if compression != "" {
			return nil, fmt.Errorf("compression=%s applies only to csv, json, and jsonl formats", compression)
		}
		return prepareGlobFile(pattern, matches, format, opts)
	default:
		return nil, fmt.Errorf("prepare source: unsupported format %q", format)
	}
}

func (p *preparedGlobSource) load(spec SourceLoadSpec) (*table.Table, error) {
	stream, err := p.stream(spec)
	if err != nil {
		return nil, err
	}
	return rowstream.Materialize(stream)
}

func (p *preparedGlobSource) stream(spec SourceLoadSpec) (rowstream.Stream, error) {
	if p.loaded {
		return nil, fmt.Errorf("prepared source already loaded")
	}
	p.loaded = true
	switch {
	case p.csv != nil:
		return p.streamCSV(spec)
	case p.js != nil:
		return p.streamJSON(spec)
	case p.file != nil:
		return p.streamFile(spec)
	default:
		return nil, fmt.Errorf("prepared source is not configured")
	}
}

func prepareGlobCSV(pattern string, matches []string, opts Options) (*PreparedSource, error) {
	cfg := csvConfigFromOptions(opts, nil)
	cfg.compression = opts.Compression
	cfg.source = pattern

	columns, anchor, shards, err := inspectCSVGlobShardPlans(matches, cfg)
	if err != nil {
		return nil, err
	}
	types, nullable, nullableAll, err := inferCSVGlobSchema(columns, anchor, shards, cfg)
	if err != nil {
		return nil, err
	}
	schemas := csvSchemasFromTypes(columns, types, nullable, nullableAll)
	schema := table.NewSchema(columns, schemas)
	return &PreparedSource{
		Schema: schema,
		glob: &preparedGlobSource{
			pattern: pattern,
			matches: append([]string(nil), matches...),
			format:  "csv",
			opts:    opts,
			schema:  schema,
			csv: &preparedGlobCSVSource{
				cfg:     cfg,
				anchor:  append([]string(nil), anchor...),
				shards:  cloneCSVGlobShardPlans(shards),
				columns: append([]string(nil), columns...),
				types:   append([]table.ValueType(nil), types...),
				schemas: append([]*table.TypeDescriptor(nil), schemas...),
			},
		},
	}, nil
}

func inspectCSVGlobShardPlans(matches []string, cfg csvLoadConfig) ([]string, []string, []csvGlobShardPlan, error) {
	if cfg.header {
		return inspectCSVGlobHeaderPlans(matches, cfg)
	}
	return inspectCSVGlobHeaderlessPlans(matches, cfg)
}

func inspectCSVGlobHeaderPlans(matches []string, cfg csvLoadConfig) ([]string, []string, []csvGlobShardPlan, error) {
	var anchor []string
	var columnSets [][]string
	shards := make([]csvGlobShardPlan, 0, len(matches))

	for _, path := range matches {
		rows, err := openCSVGlobShardRows(path, anchor, cfg)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("loading glob %q: loading %q: %w", cfg.source, path, err)
		}
		if rows.empty {
			shards = append(shards, csvGlobShardPlan{path: path, columns: append([]string(nil), rows.columns...), empty: true})
			_ = rows.Close()
			continue
		}
		if len(anchor) == 0 && hasNonEmptyColumnName(rows.columns) {
			anchor = append([]string(nil), rows.columns...)
		}
		if hasNonEmptyColumnName(rows.columns) {
			columnSets = append(columnSets, append([]string(nil), rows.columns...))
		}
		shards = append(shards, csvGlobShardPlan{path: path, columns: append([]string(nil), rows.columns...)})
		_ = rows.Close()
	}
	columns := table.UnionColumns(columnSets...)
	if err := validateCSVHeaderColumns(columns, cfg.source); err != nil {
		return nil, nil, nil, err
	}
	for _, shard := range shards {
		if err := validateCSVHeaderColumns(shard.columns, shard.path); err != nil {
			return nil, nil, nil, err
		}
	}
	return columns, anchor, shards, nil
}

func inspectCSVGlobHeaderlessPlans(matches []string, cfg csvLoadConfig) ([]string, []string, []csvGlobShardPlan, error) {
	var anchor []string
	var columnSets [][]string
	shards := make([]csvGlobShardPlan, 0, len(matches))

	for _, path := range matches {
		rows, err := openCSVGlobShardRows(path, anchor, cfg)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("loading glob %q: loading %q: %w", cfg.source, path, err)
		}
		if rows.empty {
			shards = append(shards, csvGlobShardPlan{path: path, columns: append([]string(nil), rows.columns...), empty: true})
			_ = rows.Close()
			continue
		}
		if len(anchor) == 0 {
			anchor = append([]string(nil), rows.columns...)
			columnSets = append(columnSets, append([]string(nil), anchor...))
		}
		shards = append(shards, csvGlobShardPlan{path: path, columns: append([]string(nil), rows.columns...)})
		_ = rows.Close()
	}
	columns := table.UnionColumns(columnSets...)
	return columns, anchor, shards, nil
}

func inferCSVGlobSchema(columns, anchor []string, shards []csvGlobShardPlan, cfg csvLoadConfig) ([]table.ValueType, []bool, bool, error) {
	types := make([]table.ValueType, len(columns))
	if cfg.inferRows == 0 {
		for i := range types {
			types[i] = table.TypeString
		}
	}
	nullable := make([]bool, len(columns))
	sampled := 0
	dataSeen := false
	sampleExhausted := true
	stop := false

	for _, shard := range shards {
		if stop {
			break
		}
		rows, err := openCSVGlobShardRows(shard.path, anchor, cfg)
		if err != nil {
			return nil, nil, false, fmt.Errorf("loading glob %q: loading %q: %w", cfg.source, shard.path, err)
		}
		if !sameColumns(rows.columns, shard.columns) {
			_ = rows.Close()
			return nil, nil, false, fmt.Errorf("%s: glob shard schema changed after planning", shard.path)
		}
		mapping := csvColumnMapping(columns, rows.columns)
		for {
			row, ok, err := rows.Next()
			if err != nil {
				_ = rows.Close()
				return nil, nil, false, fmt.Errorf("loading glob %q: loading %q: %w", cfg.source, shard.path, err)
			}
			if !ok {
				break
			}
			dataSeen = true
			if cfg.inferRows == 0 {
				sampleExhausted = false
				stop = true
				break
			}
			if cfg.inferRows < 0 || sampled < cfg.inferRows {
				applyCSVInferenceRow(types, mapping, row)
				applyCSVNullabilityRow(nullable, mapping, row)
				sampled++
				continue
			}
			sampleExhausted = false
			stop = true
			break
		}
		if err := rows.Close(); err != nil {
			return nil, nil, false, err
		}
	}
	for i := range types {
		if types[i] == table.TypeNull {
			types[i] = table.TypeString
		}
	}
	nullableAll := false
	switch {
	case !dataSeen:
		nullableAll = false
	case cfg.inferRows < 0:
		nullableAll = false
	case cfg.inferRows == 0:
		nullableAll = true
	default:
		nullableAll = !sampleExhausted
	}
	return types, nullable, nullableAll, nil
}

func applyCSVNullabilityRow(nullable []bool, mapping []int, row csvRawRow) {
	seen := make([]bool, len(nullable))
	for srcIdx, dst := range mapping {
		if dst < 0 || dst >= len(nullable) {
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

type csvGlobShardRows struct {
	closer   io.Closer
	reader   *csv.Reader
	cfg      csvLoadConfig
	source   string
	columns  []string
	buffered []csvRawRow
	rowNum   int
	empty    bool
	closed   bool
}

func openCSVGlobShardRows(path string, anchor []string, cfg csvLoadConfig) (*csvGlobShardRows, error) {
	f, err := openInputReader(path, cfg.compression)
	if err != nil {
		return nil, err
	}
	pathCfg := cfg
	pathCfg.source = path
	reader := newCSVReader(f, cfg.delim)
	rows := &csvGlobShardRows{closer: f, reader: reader, cfg: pathCfg, source: path}

	if !cfg.header {
		first, firstRowNum, empty, err := readFirstNonBlankCSVRow(reader, 1)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		if empty {
			rows.empty = true
			rows.columns = append([]string(nil), anchor...)
			return rows, nil
		}
		columns := anchor
		if len(columns) == 0 {
			columns = synthesizeColumns(len(first))
		}
		if err := validateCSVRecord(first, len(columns), pathCfg, firstRowNum); err != nil {
			_ = rows.Close()
			return nil, err
		}
		rows.columns = append([]string(nil), columns...)
		rows.buffered = []csvRawRow{newCSVRawRow(first, firstRowNum)}
		rows.rowNum = firstRowNum + 1
		return rows, nil
	}

	peek, peekRowNum, empty, err := readFirstNonBlankCSVRow(reader, 1)
	if err != nil {
		_ = rows.Close()
		return nil, err
	}
	if empty {
		rows.empty = true
		rows.columns = append([]string(nil), anchor...)
		return rows, nil
	}
	if len(anchor) == 0 {
		rows.columns = trimmedCSVFields(peek)
		rows.rowNum = peekRowNum + 1
		return rows, nil
	}

	switch classifyCSVGlobFirstRow(peek, anchor) {
	case csvGlobShardRepeated:
		rows.columns = append([]string(nil), anchor...)
		rows.rowNum = peekRowNum + 1
	case csvGlobShardNewHeader:
		rows.columns = trimmedCSVFields(peek)
		rows.rowNum = peekRowNum + 1
	default:
		if err := validateCSVRecord(peek, len(anchor), pathCfg, peekRowNum); err != nil {
			_ = rows.Close()
			return nil, err
		}
		rows.columns = append([]string(nil), anchor...)
		rows.buffered = []csvRawRow{newCSVRawRow(peek, peekRowNum)}
		rows.rowNum = peekRowNum + 1
	}
	return rows, nil
}

func (r *csvGlobShardRows) Next() (csvRawRow, bool, error) {
	if len(r.buffered) > 0 {
		row := r.buffered[0]
		r.buffered = r.buffered[1:]
		return row, true, nil
	}
	if r.empty || r.reader == nil {
		return csvRawRow{}, false, nil
	}
	record, err := r.reader.Read()
	if err == io.EOF {
		return csvRawRow{}, false, nil
	}
	if err != nil {
		return csvRawRow{}, false, fmt.Errorf("error reading CSV row: %w", err)
	}
	if err := validateCSVRecord(record, len(r.columns), r.cfg, r.rowNum); err != nil {
		return csvRawRow{}, false, err
	}
	row := newCSVRawRow(record, r.rowNum)
	r.rowNum++
	return row, true, nil
}

func (r *csvGlobShardRows) Close() error {
	if r == nil || r.closed {
		return nil
	}
	r.closed = true
	if r.closer == nil {
		return nil
	}
	err := r.closer.Close()
	r.closer = nil
	return err
}

func (p *preparedGlobSource) streamCSV(spec SourceLoadSpec) (rowstream.Stream, error) {
	plan, err := csvPreparedLoadPlanFor(p.csv.columns, p.csv.types, p.csv.schemas, spec, p.pattern)
	if err != nil {
		return nil, err
	}
	return &csvGlobPreparedStream{
		pattern: p.pattern,
		shards:  p.csv.shards,
		cfg:     p.csv.cfg,
		anchor:  p.csv.anchor,
		columns: p.csv.columns,
		types:   p.csv.types,
		plan:    plan,
		schema:  table.NewSchema(plan.output.columns, plan.output.schemas),
	}, nil
}

type csvGlobPreparedStream struct {
	pattern    string
	shards     []csvGlobShardPlan
	cfg        csvLoadConfig
	anchor     []string
	columns    []string
	types      []table.ValueType
	plan       csvPreparedLoadPlan
	schema     table.Schema
	shardIdx   int
	current    *csvGlobShardRows
	mapping    []int
	badRecords int
	closed     bool
}

func (s *csvGlobPreparedStream) Schema() table.Schema { return s.schema }

func (s *csvGlobPreparedStream) Next() (rowstream.Row, bool, error) {
	for {
		if s.current == nil {
			if s.shardIdx >= len(s.shards) {
				if err := s.Close(); err != nil {
					return nil, false, err
				}
				return nil, false, nil
			}
			shard := s.shards[s.shardIdx]
			s.shardIdx++
			rows, err := openCSVGlobShardRows(shard.path, s.anchor, s.cfg)
			if err != nil {
				return nil, false, fmt.Errorf("loading glob %q: loading %q: %w", s.pattern, shard.path, err)
			}
			if !sameColumns(rows.columns, shard.columns) {
				_ = rows.Close()
				return nil, false, fmt.Errorf("%s: glob shard schema changed after planning", shard.path)
			}
			s.current = rows
			s.mapping = csvColumnMapping(s.columns, rows.columns)
		}

		row, ok, err := s.current.Next()
		if err != nil {
			return nil, false, fmt.Errorf("loading glob %q: loading %q: %w", s.pattern, s.current.source, err)
		}
		if !ok {
			if err := s.current.Close(); err != nil {
				return nil, false, err
			}
			s.current = nil
			s.mapping = nil
			continue
		}
		vals, keep, err := preparedCSVOutputRow(row, s.mapping, s.current.source, s.columns, s.types, s.plan, s.cfg, &s.badRecords)
		if err != nil {
			return nil, false, err
		}
		if keep {
			return vals, true, nil
		}
	}
}

func (s *csvGlobPreparedStream) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	if s.current != nil {
		return s.current.Close()
	}
	return nil
}

func prepareGlobJSON(pattern string, matches []string, format string, opts Options) (*PreparedSource, error) {
	cfg := jsonConfigFromOptions(opts, pattern)
	cfg.compression = opts.Compression
	state, err := inspectJSONGlobSchema(matches, format, cfg)
	if err != nil {
		return nil, err
	}
	schema := table.NewSchema(state.columns, state.schemas)
	return &PreparedSource{
		Schema: schema,
		glob: &preparedGlobSource{
			pattern: pattern,
			matches: append([]string(nil), matches...),
			format:  format,
			opts:    opts,
			schema:  schema,
			js:      &preparedGlobJSONSource{cfg: cfg},
		},
	}, nil
}

func inspectJSONGlobSchema(matches []string, format string, cfg jsonLoadConfig) (jsonSchemaInference, error) {
	state := jsonSchemaInference{index: map[string]int{}}
	badRecords := 0
	inferSeen := 0
	sampleExhausted := true

	for i, path := range matches {
		if cfg.inferRows >= 0 && inferSeen >= cfg.inferRows {
			sampleExhausted = false
			if err := validateJSONGlobSyntaxRemainder(matches[i:], format, cfg); err != nil {
				return jsonSchemaInference{}, err
			}
			break
		}
		pathCfg := cfg
		pathCfg.source = path
		stream, err := openJSONRestStream(path, format, pathCfg, 0)
		if err != nil {
			return jsonSchemaInference{}, fmt.Errorf("loading glob %q: loading %q: %w", cfg.source, path, err)
		}
		for {
			if cfg.inferRows >= 0 && inferSeen >= cfg.inferRows {
				sampleExhausted = false
				if err := closeJSONRecordStreamAfterBoundedInference(stream, format); err != nil {
					return jsonSchemaInference{}, fmt.Errorf("loading glob %q: loading %q: %w", cfg.source, path, err)
				}
				if err := validateJSONGlobSyntaxRemainder(matches[i+1:], format, cfg); err != nil {
					return jsonSchemaInference{}, err
				}
				if jsonPreparedInferenceNeedsConservativeNullability(cfg.inferRows, inferSeen, sampleExhausted) {
					state.schemas = deepNullableSchemas(state.schemas)
				}
				return state, nil
			}
			rec, ok, err := stream.Next()
			if err != nil {
				_ = stream.Close()
				return jsonSchemaInference{}, fmt.Errorf("loading glob %q: loading %q: %w", cfg.source, path, err)
			}
			if !ok {
				break
			}
			rowIdx := inferSeen
			inferSeen++
			if err := inferPreparedJSONRecord(&state, rec, rowIdx, pathCfg, &badRecords); err != nil {
				_ = stream.Close()
				return jsonSchemaInference{}, err
			}
		}
		if err := stream.Close(); err != nil {
			return jsonSchemaInference{}, err
		}
	}
	if jsonPreparedInferenceNeedsConservativeNullability(cfg.inferRows, inferSeen, sampleExhausted) {
		state.schemas = deepNullableSchemas(state.schemas)
	}
	return state, nil
}

type syntaxDrainableJSONRecordStream interface {
	DrainSyntax() error
}

func closeJSONRecordStreamAfterBoundedInference(stream jsonRecordStream, format string) error {
	if stream == nil {
		return nil
	}
	if format == "json" {
		if drainable, ok := stream.(syntaxDrainableJSONRecordStream); ok {
			return drainable.DrainSyntax()
		}
	}
	return stream.Close()
}

func validateJSONGlobSyntaxRemainder(matches []string, format string, cfg jsonLoadConfig) error {
	if format != "json" {
		return nil
	}
	for _, path := range matches {
		pathCfg := cfg
		pathCfg.source = path
		if err := validateJSONFileSyntax(path, format, pathCfg); err != nil {
			return fmt.Errorf("loading glob %q: loading %q: %w", cfg.source, path, err)
		}
	}
	return nil
}

func validateJSONFileSyntax(path, format string, cfg jsonLoadConfig) error {
	f, err := openInputReader(path, cfg.compression)
	if err != nil {
		return err
	}
	defer f.Close()

	switch format {
	case "json":
		dec := json.NewDecoder(f)
		dec.UseNumber()
		tok, err := dec.Token()
		if err != nil {
			return fmt.Errorf("cannot parse JSON: %w (expected array of objects)", err)
		}
		if delim, ok := tok.(json.Delim); !ok || delim != '[' {
			return fmt.Errorf("cannot parse JSON: expected array of objects")
		}
		return validateJSONArraySyntaxRemainder(dec)
	default:
		return nil
	}
}

func (p *preparedGlobSource) streamJSON(spec SourceLoadSpec) (rowstream.Stream, error) {
	plan, err := preparedSourceLoadPlanFor(p.schema, spec, p.pattern)
	if err != nil {
		return nil, err
	}
	return &jsonGlobPreparedStream{
		pattern: p.pattern,
		matches: p.matches,
		format:  p.format,
		cfg:     p.js.cfg,
		plan:    plan,
		schema:  table.NewSchema(plan.outputColumns, plan.outputSchemas),
	}, nil
}

type jsonGlobPreparedStream struct {
	pattern    string
	matches    []string
	format     string
	cfg        jsonLoadConfig
	plan       preparedSourceLoadPlan
	schema     table.Schema
	matchIdx   int
	current    jsonRecordStream
	currentSrc string
	nextRow    int
	badRecords int
	closed     bool
}

func (s *jsonGlobPreparedStream) Schema() table.Schema { return s.schema }

func (s *jsonGlobPreparedStream) Next() (rowstream.Row, bool, error) {
	for {
		if s.current == nil {
			if s.matchIdx >= len(s.matches) {
				if err := s.Close(); err != nil {
					return nil, false, err
				}
				return nil, false, nil
			}
			path := s.matches[s.matchIdx]
			s.matchIdx++
			cfg := s.cfg
			cfg.source = path
			stream, err := openJSONRestStream(path, s.format, cfg, 0)
			if err != nil {
				return nil, false, fmt.Errorf("loading glob %q: loading %q: %w", s.pattern, path, err)
			}
			s.current = stream
			s.currentSrc = path
		}
		rec, ok, err := s.current.Next()
		if err != nil {
			return nil, false, fmt.Errorf("loading glob %q: loading %q: %w", s.pattern, s.currentSrc, err)
		}
		if !ok {
			if err := s.current.Close(); err != nil {
				return nil, false, err
			}
			s.current = nil
			s.currentSrc = ""
			continue
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

func (s *jsonGlobPreparedStream) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	if s.current != nil {
		return s.current.Close()
	}
	return nil
}

func prepareGlobFile(pattern string, matches []string, format string, opts Options) (*PreparedSource, error) {
	schemas := make([]table.Schema, 0, len(matches))
	for _, path := range matches {
		var (
			schema table.Schema
			err    error
		)
		switch format {
		case "avro":
			schema, err = inspectAvroSchema(path)
		case "parquet":
			schema, err = inspectParquetSchema(path)
		default:
			err = fmt.Errorf("prepare source: unsupported metadata format %q", format)
		}
		if err != nil {
			return nil, fmt.Errorf("loading glob %q: loading %q: %w", pattern, path, err)
		}
		schemas = append(schemas, schema)
	}
	schema := concatPreparedSchemas(schemas)
	return &PreparedSource{
		Schema: schema,
		glob: &preparedGlobSource{
			pattern: pattern,
			matches: append([]string(nil), matches...),
			format:  format,
			opts:    opts,
			schema:  schema,
			file:    &preparedGlobFileSource{schemas: schemas},
		},
	}, nil
}

func concatPreparedSchemas(schemas []table.Schema) table.Schema {
	columnSets := make([][]string, len(schemas))
	for i, schema := range schemas {
		columnSets[i] = schemaColumns(schema)
	}
	columns := table.UnionColumns(columnSets...)
	types := make([]*table.TypeDescriptor, len(columns))
	for i, col := range columns {
		var merged *table.TypeDescriptor
		for _, schema := range schemas {
			idx := schemaColumnIndex(schema, col)
			var next *table.TypeDescriptor
			if idx < 0 {
				next = &table.TypeDescriptor{Kind: table.TypeNull, Nullable: true}
			} else {
				next = schema.Columns[idx].Type
			}
			var err error
			merged, err = table.MergeSchemasPermissive(merged, next)
			if err != nil {
				merged = table.ScalarSchema(table.TypeString)
			}
		}
		types[i] = table.FinalizeSchema(merged)
	}
	return table.NewSchema(columns, types)
}

func (p *preparedGlobSource) streamFile(spec SourceLoadSpec) (rowstream.Stream, error) {
	plan, err := preparedSourceLoadPlanFor(p.schema, spec, p.pattern)
	if err != nil {
		return nil, err
	}
	return &fileGlobPreparedStream{
		pattern: p.pattern,
		matches: p.matches,
		opts:    p.opts,
		plan:    plan,
		schema:  table.NewSchema(plan.outputColumns, plan.outputSchemas),
	}, nil
}

type fileGlobPreparedStream struct {
	pattern    string
	matches    []string
	opts       Options
	plan       preparedSourceLoadPlan
	schema     table.Schema
	matchIdx   int
	current    rowstream.Stream
	currentSrc string
	currentMap map[string]int
	closed     bool
}

func (s *fileGlobPreparedStream) Schema() table.Schema { return s.schema }

func (s *fileGlobPreparedStream) Next() (rowstream.Row, bool, error) {
	for {
		if s.current == nil {
			if s.matchIdx >= len(s.matches) {
				if err := s.Close(); err != nil {
					return nil, false, err
				}
				return nil, false, nil
			}
			path := s.matches[s.matchIdx]
			s.matchIdx++
			stream, err := openProjectedGlobFileShardStream(path, s.opts, s.plan)
			if err != nil {
				return nil, false, fmt.Errorf("loading glob %q: loading %q: %w", s.pattern, path, err)
			}
			s.current = stream
			s.currentSrc = path
			s.currentMap = schemaIndexMap(stream.Schema())
		}
		row, ok, err := s.current.Next()
		if err != nil {
			return nil, false, fmt.Errorf("loading glob %q: loading %q: %w", s.pattern, s.currentSrc, err)
		}
		if !ok {
			if err := s.current.Close(); err != nil {
				return nil, false, err
			}
			s.current = nil
			s.currentSrc = ""
			s.currentMap = nil
			continue
		}
		readVals, err := globalReadValuesFromShardRow(row, s.currentMap, s.plan)
		if err != nil {
			return nil, false, err
		}
		vals, keep, err := preparedSourceOutputRow(readVals, s.plan)
		if err != nil {
			return nil, false, err
		}
		if keep {
			return vals, true, nil
		}
	}
}

func (s *fileGlobPreparedStream) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	if s.current != nil {
		return s.current.Close()
	}
	return nil
}

func openProjectedGlobFileShardStream(path string, opts Options, plan preparedSourceLoadPlan) (rowstream.Stream, error) {
	prepared, err := Prepare(path, opts)
	if err != nil {
		return nil, err
	}
	shardColumns := schemaColumns(prepared.Schema)
	readColumns := shardColumns
	if !plan.readAll {
		readColumns = intersectColumns(plan.readColumns, shardColumns)
	}
	stream, err := prepared.StreamSpec(SourceLoadSpec{OutputColumns: table.SelectedColumns(readColumns...)})
	if err != nil {
		_ = prepared.Close()
		return nil, err
	}
	return stream, nil
}

func globalReadValuesFromShardRow(row rowstream.Row, shardIndex map[string]int, plan preparedSourceLoadPlan) ([]table.Value, error) {
	readVals := make([]table.Value, len(plan.readSourceIndexes))
	for readIdx, sourceIdx := range plan.readSourceIndexes {
		col := plan.sourceColumns[sourceIdx]
		val := table.Null()
		if shardIdx, ok := shardIndex[col]; ok && shardIdx < len(row) {
			val = row[shardIdx]
		}
		coerced, err := table.CoerceValueToSchemaModeAtPath(val, plan.sourceSchemas[sourceIdx], table.CoercePermissiveMode, col)
		if err != nil {
			return nil, err
		}
		readVals[readIdx] = coerced
	}
	return readVals, nil
}

func intersectColumns(want, have []string) []string {
	if want == nil {
		return append([]string(nil), have...)
	}
	haveSet := make(map[string]bool, len(have))
	for _, col := range have {
		haveSet[col] = true
	}
	out := make([]string, 0, len(want))
	for _, col := range want {
		if haveSet[col] {
			out = append(out, col)
		}
	}
	return out
}

func schemaColumnIndex(schema table.Schema, name string) int {
	for i, col := range schema.Columns {
		if col.Name == name {
			return i
		}
	}
	return -1
}

func schemaIndexMap(schema table.Schema) map[string]int {
	index := make(map[string]int, len(schema.Columns))
	for i, col := range schema.Columns {
		index[col.Name] = i
	}
	return index
}

func cloneCSVGlobShardPlans(in []csvGlobShardPlan) []csvGlobShardPlan {
	out := make([]csvGlobShardPlan, len(in))
	for i, shard := range in {
		out[i] = csvGlobShardPlan{
			path:    shard.path,
			columns: append([]string(nil), shard.columns...),
			empty:   shard.empty,
		}
	}
	return out
}
