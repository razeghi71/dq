package loader

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	goavro "github.com/linkedin/goavro/v2"
	parquet "github.com/parquet-go/parquet-go"
	"github.com/razeghi71/dq/table"
)

// StdinSource is the query source sentinel for reading from stdin.
const StdinSource = "-"

// IsStdin reports whether filename denotes stdin.
func IsStdin(filename string) bool {
	return filename == StdinSource
}

// LoadInput reads from filename or from stdin when filename is "-".
// When reading from stdin, format must be set (csv, json, or jsonl).
// Pass nil for stdin to use os.Stdin.
func LoadInput(filename, format string, stdin io.Reader) (*table.Table, error) {
	if IsStdin(filename) {
		if format == "" {
			return nil, fmt.Errorf("reading from stdin requires -f format (csv, json, jsonl)")
		}
		if stdin == nil {
			stdin = os.Stdin
		}
		return LoadReader(stdin, format)
	}
	return Load(filename, format)
}

// Load reads a file and returns a Table. If format is non-empty it overrides
// the file extension; otherwise the extension is used. An error is returned if
// neither provides a recognisable format.
func Load(filename, format string) (*table.Table, error) {
	if IsStdin(filename) {
		return LoadInput(filename, format, nil)
	}
	if format == "" {
		format = strings.TrimPrefix(strings.ToLower(filepath.Ext(filename)), ".")
	}
	switch format {
	case "csv":
		return loadCSV(filename)
	case "json":
		return loadJSON(filename)
	case "jsonl":
		return loadJSONL(filename)
	case "avro":
		return loadAvro(filename)
	case "parquet":
		return loadParquet(filename)
	default:
		if format == "" {
			return nil, fmt.Errorf("cannot determine file format for %q: use -f to specify (csv, json, jsonl, avro, parquet)", filename)
		}
		return nil, fmt.Errorf("unsupported format %q (supported: csv, json, jsonl, avro, parquet)", format)
	}
}

// LoadReader reads a table from r in the given format.
// Supported formats: csv, json, jsonl.
func LoadReader(r io.Reader, format string) (*table.Table, error) {
	switch format {
	case "csv":
		return loadCSVReader(r)
	case "json":
		return loadJSONReader(r)
	case "jsonl":
		return loadJSONLReader(r)
	default:
		return nil, fmt.Errorf("LoadReader: unsupported format %q (supported: csv, json, jsonl)", format)
	}
}

func loadCSV(filename string) (*table.Table, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("cannot open %s: %w", filename, err)
	}
	defer f.Close()
	return loadCSVReader(f)
}

func loadCSVReader(r io.Reader) (*table.Table, error) {
	reader := csv.NewReader(r)
	reader.TrimLeadingSpace = true

	// Read header
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("cannot read CSV header: %w", err)
	}

	// Trim whitespace from column names
	columns := make([]string, len(header))
	for i, h := range header {
		columns[i] = strings.TrimSpace(h)
	}

	t := table.NewTable(columns)

	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("error reading CSV row: %w", err)
		}

		vals := make([]table.Value, len(columns))
		for i := range columns {
			if i < len(record) {
				vals[i] = parseValue(strings.TrimSpace(record[i]))
			} else {
				vals[i] = table.Null()
			}
		}
		t.AddRow(vals)
	}

	return t, nil
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

func loadJSON(filename string) (*table.Table, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("cannot open %s: %w", filename, err)
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

func loadJSONL(filename string) (*table.Table, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("cannot open %s: %w", filename, err)
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
