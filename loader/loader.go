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

// Load reads a file and returns a Table. If format is non-empty it overrides
// the file extension; otherwise the extension is used. An error is returned if
// neither provides a recognisable format.
func Load(filename, format string) (*table.Table, error) {
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

var avroPrimitives = map[string]bool{
	"null": true, "boolean": true, "int": true, "long": true,
	"float": true, "double": true, "bytes": true, "string": true,
}

func avroValue(v interface{}) table.Value {
	m, isMap := v.(map[string]interface{})
	if isMap && len(m) == 1 {
		for k, inner := range m {
			if avroPrimitives[k] || (len(k) > 0 && k[0] >= 'A' && k[0] <= 'Z') {
				return avroValue(inner) // union: unwrap
			}
		}
	}
	return anyToValue(v)
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
		Fields []struct {
			Name string `json:"name"`
		} `json:"fields"`
	}
	if err := json.Unmarshal([]byte(schema), &schemaDef); err != nil {
		return nil, fmt.Errorf("cannot parse Avro schema: %w", err)
	}

	columns := make([]string, len(schemaDef.Fields))
	for i, field := range schemaDef.Fields {
		columns[i] = field.Name
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
			vals[i] = avroValue(v)
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
	var columns []string
	for _, field := range schema.Fields() {
		columns = append(columns, field.Name())
	}
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
