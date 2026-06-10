package writer

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/razeghi71/dq/table"
)

// Write writes the table to w in the specified format.
// Supported formats: "table" (default), "csv", "json", "jsonl", "avro", "parquet".
func Write(w io.Writer, t *table.Table, format string) error {
	switch format {
	case "", "table":
		return writeTable(w, t)
	case "csv":
		return writeCSV(w, t)
	case "json":
		return writeJSON(w, t)
	case "jsonl":
		return writeJSONL(w, t)
	case "avro":
		return writeAvro(w, t)
	case "parquet":
		return writeParquet(w, t)
	default:
		return fmt.Errorf("unsupported output format %q (supported: table, csv, json, jsonl, avro, parquet)", format)
	}
}

// --- table (pretty-print) ---

func writeTable(w io.Writer, t *table.Table) error {
	if len(t.Columns) == 0 {
		return nil
	}

	widths := make([]int, len(t.Columns))
	for i, col := range t.Columns {
		widths[i] = len(col)
	}

	cells := make([][]string, t.NumRows)
	for i := 0; i < t.NumRows; i++ {
		cells[i] = make([]string, len(t.Columns))
		for j := range t.Columns {
			cells[i][j] = t.Col(j).Get(i).AsString()
			if len(cells[i][j]) > widths[j] {
				widths[j] = len(cells[i][j])
			}
		}
	}

	// Header
	headerParts := make([]string, len(t.Columns))
	for i, col := range t.Columns {
		headerParts[i] = padRight(col, widths[i])
	}
	fmt.Fprintln(w, strings.Join(headerParts, " | "))

	// Separator
	sepParts := make([]string, len(t.Columns))
	for i := range t.Columns {
		sepParts[i] = strings.Repeat("-", widths[i])
	}
	fmt.Fprintln(w, strings.Join(sepParts, "-+-"))

	// Rows
	for _, row := range cells {
		parts := make([]string, len(t.Columns))
		for i := range t.Columns {
			parts[i] = padRight(row[i], widths[i])
		}
		fmt.Fprintln(w, strings.Join(parts, " | "))
	}
	return nil
}

func padRight(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}

// --- CSV ---

func writeCSV(w io.Writer, t *table.Table) error {
	cw := csv.NewWriter(w)
	defer cw.Flush()

	if err := cw.Write(t.Columns); err != nil {
		return err
	}

	for i := 0; i < t.NumRows; i++ {
		record := make([]string, len(t.Columns))
		for j := range t.Columns {
			v := t.Col(j).Get(i)
			if v.IsNull() {
				record[j] = ""
			} else {
				record[j] = v.AsString()
			}
		}
		if err := cw.Write(record); err != nil {
			return err
		}
	}
	return cw.Error()
}

// --- JSON ---

func writeJSON(w io.Writer, t *table.Table) error {
	rows := make([]map[string]interface{}, t.NumRows)
	for i := 0; i < t.NumRows; i++ {
		obj := make(map[string]interface{}, len(t.Columns))
		for j, col := range t.Columns {
			obj[col] = valueToJSON(t.Col(j).Get(i))
		}
		rows[i] = obj
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(rows)
}

// --- JSONL ---

func writeJSONL(w io.Writer, t *table.Table) error {
	enc := json.NewEncoder(w)
	for i := 0; i < t.NumRows; i++ {
		obj := make(map[string]interface{}, len(t.Columns))
		for j, col := range t.Columns {
			obj[col] = valueToJSON(t.Col(j).Get(i))
		}
		if err := enc.Encode(obj); err != nil {
			return err
		}
	}
	return nil
}

// --- helpers ---

// valueToJSON converts a table.Value to a Go value suitable for json.Marshal.
func valueToJSON(v table.Value) interface{} {
	switch v.Type {
	case table.TypeNull:
		return nil
	case table.TypeInt:
		return v.Int
	case table.TypeFloat:
		return v.Float
	case table.TypeString:
		return v.Str
	case table.TypeBool:
		return v.Bool
	case table.TypeList:
		elems := make([]interface{}, len(v.List))
		for i, e := range v.List {
			elems[i] = valueToJSON(e)
		}
		return elems
	case table.TypeRecord:
		obj := make(map[string]interface{}, len(v.Fields))
		for _, f := range v.Fields {
			obj[f.Name] = valueToJSON(f.Value)
		}
		return obj
	default:
		return v.AsString()
	}
}
