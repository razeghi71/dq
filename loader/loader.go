package loader

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	goavro "github.com/linkedin/goavro/v2"
	"github.com/razeghi71/dq/table"
)

// Load reads a file and returns a Table.
func Load(filename string) (*table.Table, error) {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".csv":
		return loadCSV(filename)
	case ".json":
		return loadJSON(filename)
	case ".jsonl":
		return loadJSONL(filename)
	case ".avro":
		return loadAvro(filename)
	default:
		return nil, fmt.Errorf("unsupported file format %q (supported: .csv, .json, .jsonl, .avro)", ext)
	}
}

func loadCSV(filename string) (*table.Table, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("cannot open %s: %w", filename, err)
	}
	defer f.Close()

	reader := csv.NewReader(f)
	reader.TrimLeadingSpace = true

	// Read header
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("cannot read CSV header from %s: %w", filename, err)
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
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", filename, err)
	}

	var records []map[string]interface{}
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, fmt.Errorf("cannot parse JSON from %s: %w (expected array of objects)", filename, err)
	}

	return buildTableFromRecords(records), nil
}

func loadJSONL(filename string) (*table.Table, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("cannot open %s: %w", filename, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
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
		return nil, fmt.Errorf("error reading %s: %w", filename, err)
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
			vals[i] = jsonValue(v)
		}
		t.AddRow(vals)
	}

	return t
}

func jsonValue(v interface{}) table.Value {
	switch val := v.(type) {
	case float64:
		// JSON numbers are float64; check if it's actually an integer
		if val == float64(int64(val)) {
			return table.IntVal(int64(val))
		}
		return table.FloatVal(val)
	case string:
		return table.StrVal(val)
	case bool:
		return table.BoolVal(val)
	case nil:
		return table.Null()
	default:
		// For nested objects/arrays, just stringify
		b, _ := json.Marshal(val)
		return table.StrVal(string(b))
	}
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

func avroValue(v interface{}) table.Value {
	if v == nil {
		return table.Null()
	}
	switch val := v.(type) {
	case int32:
		return table.IntVal(int64(val))
	case int64:
		return table.IntVal(val)
	case float32:
		return table.FloatVal(float64(val))
	case float64:
		return table.FloatVal(val)
	case string:
		return table.StrVal(val)
	case bool:
		return table.BoolVal(val)
	case []byte:
		return table.StrVal(string(val))
	case map[string]interface{}:
		// Avro unions decode as {"type": value} - extract the value
		for _, inner := range val {
			return avroValue(inner)
		}
		return table.Null()
	default:
		return table.StrVal(fmt.Sprintf("%v", val))
	}
}
