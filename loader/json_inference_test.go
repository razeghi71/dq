package loader

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/razeghi71/dq/table"
)

func writeJSONInferenceFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func expectJSONInferenceLoadError(t *testing.T, source string, opts Options, parts ...string) {
	t.Helper()
	_, err := Load(source, opts)
	if err == nil {
		t.Fatalf("expected load error for %s", source)
	}
	for _, part := range parts {
		if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(part)) {
			t.Fatalf("error %q does not contain %q", err.Error(), part)
		}
	}
}

func expectJSONInferenceReaderError(t *testing.T, input string, opts Options, parts ...string) {
	t.Helper()
	_, err := LoadReader(strings.NewReader(input), opts)
	if err == nil {
		t.Fatal("expected load error")
	}
	for _, part := range parts {
		if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(part)) {
			t.Fatalf("error %q does not contain %q", err.Error(), part)
		}
	}
}

func TestLoadJSONLInferRowsAndBadRecords(t *testing.T) {
	t.Run("post_inference_schema_conflict_errors_by_default", func(t *testing.T) {
		input := "{\"id\":1,\"amount\":10}\n{\"id\":2,\"amount\":\"bad\"}\n"
		expectJSONInferenceReaderError(t, input, Options{
			Format:       "jsonl",
			InferRows:    1,
			InferRowsSet: true,
		}, "line 2", "amount", "int", "string")
	})

	t.Run("post_inference_schema_conflict_skips_whole_record", func(t *testing.T) {
		input := "{\"id\":1,\"amount\":10}\n{\"id\":2,\"amount\":\"bad\"}\n{\"id\":3,\"amount\":30}\n"
		tbl, err := LoadReader(strings.NewReader(input), Options{
			Format:        "jsonl",
			InferRows:     1,
			InferRowsSet:  true,
			MaxBadRecords: 1,
		})
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if tbl.NumRows != 2 || tbl.Get(0, "id").Int != 1 || tbl.Get(1, "id").Int != 3 {
			t.Fatalf("bad JSONL record should be skipped whole, got %s", tbl.String())
		}
		if got := tbl.Col(tbl.ColIndex("amount")).ColType(); got != table.TypeInt {
			t.Fatalf("amount type: got %v, want int", got)
		}
	})

	t.Run("inference_window_conflict_counts_as_bad_record", func(t *testing.T) {
		input := "{\"id\":1,\"amount\":10}\n{\"id\":2,\"amount\":\"bad\"}\n{\"id\":3,\"amount\":30}\n"
		tbl, err := LoadReader(strings.NewReader(input), Options{
			Format:        "jsonl",
			InferRows:     3,
			InferRowsSet:  true,
			MaxBadRecords: 1,
		})
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if tbl.NumRows != 2 || tbl.Get(0, "id").Int != 1 || tbl.Get(1, "id").Int != 3 {
			t.Fatalf("inference-time bad record should be excluded, got %s", tbl.String())
		}
	})

	t.Run("bad_record_limit_reports_record_that_exceeded_limit", func(t *testing.T) {
		input := "{\"id\":1,\"amount\":10}\n{\"id\":2,\"amount\":\"bad\"}\n{\"id\":3,\"amount\":true}\n"
		expectJSONInferenceReaderError(t, input, Options{
			Format:        "jsonl",
			InferRows:     1,
			InferRowsSet:  true,
			MaxBadRecords: 1,
		}, "line 3", "amount", "int", "bool")
	})

	t.Run("infer_all_rows_preserves_strict_full_scan_behavior", func(t *testing.T) {
		input := "{\"id\":1,\"amount\":10}\n{\"id\":2,\"amount\":\"bad\"}\n"
		expectJSONInferenceReaderError(t, input, Options{
			Format:       "jsonl",
			InferRows:    -1,
			InferRowsSet: true,
		}, "line 2", "amount", "int", "string")
	})
}

func TestLoadJSONLDefaultInferRowsSamples20480Records(t *testing.T) {
	var data strings.Builder
	for i := 1; i <= 20479; i++ {
		data.WriteString("{\"id\":1,\"amount\":10}\n")
	}
	data.WriteString("{\"id\":20480,\"amount\":10.5}\n")
	data.WriteString("{\"id\":20481,\"amount\":20}\n")

	tbl, err := LoadReader(strings.NewReader(data.String()), Options{Format: "jsonl"})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := tbl.Col(tbl.ColIndex("amount")).Schema().String(); got != "float?" {
		t.Fatalf("amount schema: got %q, want float? because default bounded inference did not prove later nullability", got)
	}
	if got := tbl.Get(20480, "amount"); got.Type != table.TypeFloat || got.Float != 20 {
		t.Fatalf("record 20481 amount: got %v, want coerced float 20", got)
	}
}

func TestLoadJSONInferRowsAndBadRecords(t *testing.T) {
	t.Run("array_post_inference_conflict_skips_element", func(t *testing.T) {
		input := `[{"id":1,"amount":10},{"id":2,"amount":"bad"},{"id":3,"amount":30}]`
		tbl, err := LoadReader(strings.NewReader(input), Options{
			Format:        "json",
			InferRows:     1,
			InferRowsSet:  true,
			MaxBadRecords: 1,
		})
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if tbl.NumRows != 2 || tbl.Get(0, "id").Int != 1 || tbl.Get(1, "id").Int != 3 {
			t.Fatalf("bad JSON array element should be skipped whole, got %s", tbl.String())
		}
	})

	t.Run("array_bad_record_limit_reports_row_index", func(t *testing.T) {
		input := `[{"id":1,"amount":10},{"id":2,"amount":"bad"},{"id":3,"amount":true}]`
		expectJSONInferenceReaderError(t, input, Options{
			Format:        "json",
			InferRows:     1,
			InferRowsSet:  true,
			MaxBadRecords: 1,
		}, "row 3", "amount", "int", "bool")
	})

	t.Run("array_non_object_element_counts_as_bad_record", func(t *testing.T) {
		input := `[{"id":1},[1,2],{"id":2}]`
		tbl, err := LoadReader(strings.NewReader(input), Options{
			Format:        "json",
			InferRows:     1,
			InferRowsSet:  true,
			MaxBadRecords: 1,
		})
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if tbl.NumRows != 2 || tbl.Get(1, "id").Int != 2 {
			t.Fatalf("non-object JSON array element should be skipped, got %s", tbl.String())
		}
	})

	t.Run("infer_all_rows_preserves_strict_full_scan_behavior", func(t *testing.T) {
		input := `[{"id":1,"amount":10},{"id":2,"amount":"bad"}]`
		expectJSONInferenceReaderError(t, input, Options{
			Format:       "json",
			InferRows:    -1,
			InferRowsSet: true,
		}, "row 2", "amount", "int", "string")
	})

	t.Run("malformed_top_level_array_syntax_fails_whole_file", func(t *testing.T) {
		input := `[{"id":1},{"id":2`
		expectJSONInferenceReaderError(t, input, Options{
			Format:        "json",
			InferRows:     1,
			InferRowsSet:  true,
			MaxBadRecords: 10,
		}, "cannot parse json")
	})

	t.Run("top_level_trailing_value_fails_whole_file", func(t *testing.T) {
		input := `[{"id":1}] {"id":2}`
		expectJSONInferenceReaderError(t, input, Options{
			Format:        "json",
			InferRows:     1,
			InferRowsSet:  true,
			MaxBadRecords: 10,
		}, "cannot parse json", "trailing")
	})
}

func TestLoadJSONTopLevelShape(t *testing.T) {
	t.Run("null_is_not_an_empty_array", func(t *testing.T) {
		expectJSONInferenceReaderError(t, "null", Options{Format: "json"}, "expected array of objects")
	})

	t.Run("empty_array_loads_empty_table", func(t *testing.T) {
		tbl, err := LoadReader(strings.NewReader("[]"), Options{Format: "json"})
		if err != nil {
			t.Fatalf("load empty array: %v", err)
		}
		if tbl.NumRows != 0 || len(tbl.Columns) != 0 {
			t.Fatalf("empty array: got rows=%d columns=%v, want empty table", tbl.NumRows, tbl.Columns)
		}
	})
}

func TestLoadJSONDefaultInferRowsSamples20480Elements(t *testing.T) {
	var data strings.Builder
	data.WriteByte('[')
	for i := 1; i <= 20479; i++ {
		if i > 1 {
			data.WriteByte(',')
		}
		data.WriteString("{\"id\":1,\"amount\":10}")
	}
	data.WriteString(",{\"id\":20480,\"amount\":10.5}")
	data.WriteString(",{\"id\":20481,\"amount\":20}]")

	tbl, err := LoadReader(strings.NewReader(data.String()), Options{Format: "json"})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := tbl.Col(tbl.ColIndex("amount")).Schema().String(); got != "float?" {
		t.Fatalf("amount schema: got %q, want float? because default bounded inference did not prove later nullability", got)
	}
	if got := tbl.Get(20480, "amount"); got.Type != table.TypeFloat || got.Float != 20 {
		t.Fatalf("element 20481 amount: got %v, want coerced float 20", got)
	}
}

func TestLoadJSONNumbersPreserveLargeIntegersAndDecimals(t *testing.T) {
	tests := []struct {
		name   string
		format string
		input  string
	}{
		{
			name:   "json_array",
			format: "json",
			input:  `[{"id":9007199254740993,"amount":1.25,"whole_decimal":2.0,"scientific":1e3}]`,
		},
		{
			name:   "jsonl",
			format: "jsonl",
			input:  "{\"id\":9007199254740993,\"amount\":1.25,\"whole_decimal\":2.0,\"scientific\":1e3}\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tbl, err := LoadReader(strings.NewReader(tc.input), Options{Format: tc.format})
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			if got := tbl.Get(0, "id"); got.Type != table.TypeInt || got.Int != 9007199254740993 {
				t.Fatalf("id: got %v, want exact int 9007199254740993", got)
			}
			for _, col := range []string{"amount", "whole_decimal", "scientific"} {
				if got := tbl.Col(tbl.ColIndex(col)).Schema().String(); got != "float" {
					t.Fatalf("%s schema: got %q, want float", col, got)
				}
				if got := tbl.Get(0, col); got.Type != table.TypeFloat {
					t.Fatalf("%s value: got type %v, want float", col, got.Type)
				}
			}
			if got := tbl.Get(0, "amount").Float; got != 1.25 {
				t.Fatalf("amount: got %v, want 1.25", got)
			}
			if got := tbl.Get(0, "whole_decimal").Float; got != 2 {
				t.Fatalf("whole_decimal: got %v, want 2", got)
			}
			if got := tbl.Get(0, "scientific").Float; got != 1000 {
				t.Fatalf("scientific: got %v, want 1000", got)
			}
		})
	}
}

func TestLoadJSONUnrepresentableNumbersAreBadRecords(t *testing.T) {
	t.Run("json_array_errors_by_default", func(t *testing.T) {
		input := `[{"x":1e10000},{"x":"1e10000"}]`
		expectJSONInferenceReaderError(t, input, Options{
			Format: "json",
		}, "row 1", "x", "unrepresentable JSON number", "1e10000")
	})

	t.Run("json_array_skips_bad_record_within_limit", func(t *testing.T) {
		input := `[{"x":1},{"x":1e10000},{"x":2}]`
		tbl, err := LoadReader(strings.NewReader(input), Options{
			Format:        "json",
			MaxBadRecords: 1,
		})
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if tbl.NumRows != 2 || tbl.Get(0, "x").Int != 1 || tbl.Get(1, "x").Int != 2 {
			t.Fatalf("unrepresentable JSON number record should be skipped, got %s", tbl.String())
		}
		if got := tbl.Col(tbl.ColIndex("x")).Schema().String(); got != "int" {
			t.Fatalf("x schema: got %q, want int", got)
		}
	})

	t.Run("jsonl_errors_by_default", func(t *testing.T) {
		input := "{\"x\":1e10000}\n{\"x\":\"1e10000\"}\n"
		expectJSONInferenceReaderError(t, input, Options{
			Format: "jsonl",
		}, "line 1", "x", "unrepresentable JSON number", "1e10000")
	})

	t.Run("jsonl_skips_bad_record_within_limit", func(t *testing.T) {
		input := "{\"x\":1}\n{\"x\":1e10000}\n{\"x\":2}\n"
		tbl, err := LoadReader(strings.NewReader(input), Options{
			Format:        "jsonl",
			MaxBadRecords: 1,
		})
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if tbl.NumRows != 2 || tbl.Get(0, "x").Int != 1 || tbl.Get(1, "x").Int != 2 {
			t.Fatalf("unrepresentable JSONL number record should be skipped, got %s", tbl.String())
		}
		if got := tbl.Col(tbl.ColIndex("x")).Schema().String(); got != "int" {
			t.Fatalf("x schema: got %q, want int", got)
		}
	})
}

func TestLoadJSONLateFieldsAfterSample(t *testing.T) {
	t.Run("jsonl_late_field_errors_by_default", func(t *testing.T) {
		input := "{\"id\":1}\n{\"id\":2,\"email\":\"bob@example.com\"}\n"
		expectJSONInferenceReaderError(t, input, Options{
			Format:       "jsonl",
			InferRows:    1,
			InferRowsSet: true,
		}, "line 2", "email", "unknown")
	})

	t.Run("jsonl_late_field_skips_record_within_bad_record_limit", func(t *testing.T) {
		input := "{\"id\":1}\n{\"id\":2,\"email\":\"bob@example.com\"}\n{\"id\":3}\n"
		tbl, err := LoadReader(strings.NewReader(input), Options{
			Format:        "jsonl",
			InferRows:     1,
			InferRowsSet:  true,
			MaxBadRecords: 1,
		})
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if tbl.NumRows != 2 || tbl.Get(0, "id").Int != 1 || tbl.Get(1, "id").Int != 3 {
			t.Fatalf("late-field record should be skipped whole, got %s", tbl.String())
		}
		if idx := tbl.ColIndex("email"); idx >= 0 {
			t.Fatalf("late unknown field should not widen sampled schema, columns=%v", tbl.Columns)
		}
	})

	t.Run("json_array_late_field_errors_by_default", func(t *testing.T) {
		input := `[{"id":1},{"id":2,"email":"bob@example.com"}]`
		expectJSONInferenceReaderError(t, input, Options{
			Format:       "json",
			InferRows:    1,
			InferRowsSet: true,
		}, "row 2", "email", "unknown")
	})

	t.Run("nested_late_record_field_reports_full_path", func(t *testing.T) {
		input := "{\"id\":1,\"s\":{\"x\":1}}\n{\"id\":2,\"s\":{\"x\":2,\"y\":3}}\n"
		expectJSONInferenceReaderError(t, input, Options{
			Format:       "jsonl",
			InferRows:    1,
			InferRowsSet: true,
		}, "line 2", "s.y", "unknown")
	})

	t.Run("nested_late_list_record_field_can_skip", func(t *testing.T) {
		input := "{\"id\":1,\"orders\":[{\"amount\":10}]}\n{\"id\":2,\"orders\":[{\"amount\":20,\"note\":\"late\"}]}\n{\"id\":3,\"orders\":[{\"amount\":30}]}\n"
		tbl, err := LoadReader(strings.NewReader(input), Options{
			Format:        "jsonl",
			InferRows:     1,
			InferRowsSet:  true,
			MaxBadRecords: 1,
		})
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if tbl.NumRows != 2 || tbl.Get(1, "id").Int != 3 {
			t.Fatalf("late nested-list field record should be skipped, got %s", tbl.String())
		}
		if got := tbl.Col(tbl.ColIndex("orders")).Schema().String(); got != "list<record<amount:int?>?>?" {
			t.Fatalf("orders schema: got %q, want list<record<amount:int?>?>?", got)
		}
	})
}

func TestLoadJSONNestedSchemaBadRecords(t *testing.T) {
	t.Run("nested_record_conflict_reports_path", func(t *testing.T) {
		input := "{\"id\":1,\"s\":{\"x\":1}}\n{\"id\":2,\"s\":{\"x\":\"bad\"}}\n"
		expectJSONInferenceReaderError(t, input, Options{
			Format:       "jsonl",
			InferRows:    1,
			InferRowsSet: true,
		}, "line 2", "s.x", "int", "string")
	})

	t.Run("list_record_conflict_reports_path_and_can_skip", func(t *testing.T) {
		input := "{\"id\":1,\"orders\":[{\"amount\":10}]}\n{\"id\":2,\"orders\":[{\"amount\":\"bad\"}]}\n{\"id\":3,\"orders\":[{\"amount\":30}]}\n"
		tbl, err := LoadReader(strings.NewReader(input), Options{
			Format:        "jsonl",
			InferRows:     1,
			InferRowsSet:  true,
			MaxBadRecords: 1,
		})
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if tbl.NumRows != 2 || tbl.Get(1, "id").Int != 3 {
			t.Fatalf("nested bad record should be skipped, got %s", tbl.String())
		}
		if got := tbl.Col(tbl.ColIndex("orders")).Schema().String(); got != "list<record<amount:int?>?>?" {
			t.Fatalf("orders schema: got %q, want list<record<amount:int?>?>?", got)
		}
	})

	t.Run("heterogeneous_values_inside_one_array_remain_mixed", func(t *testing.T) {
		input := "{\"id\":1,\"xs\":[1,\"two\"]}\n{\"id\":2,\"xs\":[true]}\n"
		tbl, err := LoadReader(strings.NewReader(input), Options{
			Format:        "jsonl",
			InferRows:     1,
			InferRowsSet:  true,
			MaxBadRecords: 0,
		})
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if got := tbl.Col(tbl.ColIndex("xs")).Schema().String(); got != "list<mixed?>?" {
			t.Fatalf("xs schema: got %q, want list<mixed?>?", got)
		}
	})

	t.Run("skipped_sample_record_does_not_partially_mutate_schema", func(t *testing.T) {
		input := "{\"id\":1,\"s\":{\"a\":1,\"b\":1}}\n{\"id\":2,\"s\":{\"a\":2.5,\"b\":\"bad\"}}\n{\"id\":3,\"s\":{\"a\":3,\"b\":3}}\n"
		tbl, err := LoadReader(strings.NewReader(input), Options{
			Format:        "jsonl",
			InferRows:     3,
			InferRowsSet:  true,
			MaxBadRecords: 1,
		})
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if tbl.NumRows != 2 || tbl.Get(1, "id").Int != 3 {
			t.Fatalf("bad sampled record should be skipped, got %s", tbl.String())
		}
		if got := tbl.Col(tbl.ColIndex("s")).Schema().String(); got != "record<a:int, b:int>" {
			t.Fatalf("schema should not include partial float widening from skipped row: got %q", got)
		}
	})
}

func TestLoadJSONNullAndMissingInference(t *testing.T) {
	input := "{\"id\":1,\"amount\":null}\n{\"id\":2}\n{\"id\":3,\"amount\":30}\n"
	tbl, err := LoadReader(strings.NewReader(input), Options{
		Format:       "jsonl",
		InferRows:    -1,
		InferRowsSet: true,
	})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := tbl.Col(tbl.ColIndex("amount")).Schema().String(); got != "int?" {
		t.Fatalf("amount schema: got %q, want int?", got)
	}
	if !tbl.Get(0, "amount").IsNull() || !tbl.Get(1, "amount").IsNull() || tbl.Get(2, "amount").Int != 30 {
		t.Fatalf("unexpected amount values: %s", tbl.String())
	}
}

func TestLoadJSONPostSampleNullabilityUpdatesSchema(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		column     string
		wantSchema string
		check      func(t *testing.T, tbl *table.Table)
	}{
		{
			name:       "explicit_top_level_null",
			input:      "{\"id\":1}\n{\"id\":null}\n",
			column:     "id",
			wantSchema: "int?",
			check: func(t *testing.T, tbl *table.Table) {
				t.Helper()
				if !tbl.Get(1, "id").IsNull() {
					t.Fatalf("row 2 id: got %v, want null", tbl.Get(1, "id"))
				}
			},
		},
		{
			name:       "missing_top_level_field",
			input:      "{\"id\":1}\n{}\n",
			column:     "id",
			wantSchema: "int?",
			check: func(t *testing.T, tbl *table.Table) {
				t.Helper()
				if !tbl.Get(1, "id").IsNull() {
					t.Fatalf("row 2 id: got %v, want null", tbl.Get(1, "id"))
				}
			},
		},
		{
			name:       "missing_nested_record_field",
			input:      "{\"s\":{\"x\":1}}\n{\"s\":{}}\n",
			column:     "s",
			wantSchema: "record<x:int?>?",
			check: func(t *testing.T, tbl *table.Table) {
				t.Helper()
				x := tbl.Get(1, "s").Fields[0].Value
				if !x.IsNull() {
					t.Fatalf("row 2 s.x: got %v, want null", x)
				}
			},
		},
		{
			name:       "missing_list_record_field",
			input:      "{\"orders\":[{\"amount\":10}]}\n{\"orders\":[{}]}\n",
			column:     "orders",
			wantSchema: "list<record<amount:int?>?>?",
			check: func(t *testing.T, tbl *table.Table) {
				t.Helper()
				amount := tbl.Get(1, "orders").List[0].Fields[0].Value
				if !amount.IsNull() {
					t.Fatalf("row 2 orders[].amount: got %v, want null", amount)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tbl, err := LoadReader(strings.NewReader(tc.input), Options{
				Format:       "jsonl",
				InferRows:    1,
				InferRowsSet: true,
			})
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			if got := tbl.Col(tbl.ColIndex(tc.column)).Schema().String(); got != tc.wantSchema {
				t.Fatalf("%s schema: got %q, want %q", tc.column, got, tc.wantSchema)
			}
			tc.check(t, tbl)
		})
	}
}

func TestLoadJSONPreservesElementNamedObjects(t *testing.T) {
	tests := []struct {
		name   string
		format string
		input  string
	}{
		{
			name:   "json_array",
			format: "json",
			input:  `[{"x":{"element":1}}]`,
		},
		{
			name:   "jsonl",
			format: "jsonl",
			input:  "{\"x\":{\"element\":1}}\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tbl, err := LoadReader(strings.NewReader(tc.input), Options{Format: tc.format})
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			if got := tbl.Col(tbl.ColIndex("x")).Schema().String(); got != "record<element:int>" {
				t.Fatalf("x schema: got %q, want record<element:int>", got)
			}
			x := tbl.Get(0, "x")
			if x.Type != table.TypeRecord || len(x.Fields) != 1 || x.Fields[0].Name != "element" || x.Fields[0].Value.Int != 1 {
				t.Fatalf("x value should stay record<element:int>, got %s", x.AsString())
			}
		})
	}
}

func TestLoadJSONLMalformedLinesBadRecords(t *testing.T) {
	t.Run("malformed_line_skipped_within_limit", func(t *testing.T) {
		input := "{\"id\":1}\nnot-json\n{\"id\":2}\n"
		tbl, err := LoadReader(strings.NewReader(input), Options{
			Format:        "jsonl",
			InferRows:     1,
			InferRowsSet:  true,
			MaxBadRecords: 1,
		})
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if tbl.NumRows != 2 || tbl.Get(1, "id").Int != 2 {
			t.Fatalf("malformed line should be skipped, got %s", tbl.String())
		}
	})

	t.Run("malformed_line_exceeding_limit_errors", func(t *testing.T) {
		input := "{\"id\":1}\nnot-json\n{\"id\":2,\"amount\":\"late\"}\n"
		expectJSONInferenceReaderError(t, input, Options{
			Format:        "jsonl",
			InferRows:     1,
			InferRowsSet:  true,
			MaxBadRecords: 1,
		}, "line 3", "amount")
	})

	t.Run("valid_jsonl_non_object_line_counts_as_bad_record", func(t *testing.T) {
		input := "{\"id\":1}\n[1,2]\n{\"id\":2}\n"
		tbl, err := LoadReader(strings.NewReader(input), Options{
			Format:        "jsonl",
			InferRows:     1,
			InferRowsSet:  true,
			MaxBadRecords: 1,
		})
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if tbl.NumRows != 2 || tbl.Get(1, "id").Int != 2 {
			t.Fatalf("non-object JSONL line should be skipped, got %s", tbl.String())
		}
	})

	t.Run("valid_jsonl_non_object_line_errors_by_default", func(t *testing.T) {
		for _, input := range []string{"[1,2]\n", "\"x\"\n", "123\n"} {
			_, err := LoadReader(strings.NewReader(input), Options{Format: "jsonl"})
			if err == nil {
				t.Fatalf("expected non-object JSONL line to error for %q", input)
			}
			msg := strings.ToLower(err.Error())
			if !strings.Contains(msg, "line 1") || !strings.Contains(msg, "expected json object") {
				t.Fatalf("error %q should report expected JSON object", err.Error())
			}
			if strings.Contains(msg, "invalid json") {
				t.Fatalf("error %q should not classify a valid non-object JSON value as invalid JSON", err.Error())
			}
		}
	})

	t.Run("trailing_token_line_errors_by_default", func(t *testing.T) {
		input := "{\"id\":1} {\"id\":2}\n"
		expectJSONInferenceReaderError(t, input, Options{
			Format:       "jsonl",
			InferRows:    1,
			InferRowsSet: true,
		}, "line 1", "invalid json", "trailing")
	})

	t.Run("trailing_token_line_counts_as_bad_record", func(t *testing.T) {
		input := "{\"id\":1}\n{\"id\":2} {\"id\":3}\n{\"id\":4}\n"
		tbl, err := LoadReader(strings.NewReader(input), Options{
			Format:        "jsonl",
			InferRows:     1,
			InferRowsSet:  true,
			MaxBadRecords: 1,
		})
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if tbl.NumRows != 2 || tbl.Get(0, "id").Int != 1 || tbl.Get(1, "id").Int != 4 {
			t.Fatalf("trailing-token JSONL line should be skipped, got %s", tbl.String())
		}
	})
}

func TestLoadJSONInferenceCompressedGlobAndStdin(t *testing.T) {
	t.Run("gzip_jsonl_skips_bad_record", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "events.jsonl.gz")
		data := "{\"id\":1,\"amount\":10}\n{\"id\":2,\"amount\":\"bad\"}\n{\"id\":3,\"amount\":30}\n"
		if err := os.WriteFile(path, gzipTestBytes(t, data), 0o644); err != nil {
			t.Fatal(err)
		}
		tbl, err := Load(path, Options{
			InferRows:     1,
			InferRowsSet:  true,
			MaxBadRecords: 1,
		})
		if err != nil {
			t.Fatalf("load gzip jsonl: %v", err)
		}
		if tbl.NumRows != 2 || tbl.Get(1, "id").Int != 3 {
			t.Fatalf("bad compressed JSONL record should be skipped, got %s", tbl.String())
		}
	})

	t.Run("glob_jsonl_uses_single_deterministic_sample_and_bad_record_budget", func(t *testing.T) {
		dir := t.TempDir()
		writeJSONInferenceFile(t, dir, "a.jsonl", "{\"id\":1,\"amount\":10}\n")
		writeJSONInferenceFile(t, dir, "b.jsonl", "{\"id\":2,\"amount\":\"bad\"}\n{\"id\":3,\"amount\":30}\n")
		tbl, err := Load(filepath.Join(dir, "*.jsonl"), Options{
			Format:        "jsonl",
			InferRows:     1,
			InferRowsSet:  true,
			MaxBadRecords: 1,
		})
		if err != nil {
			t.Fatalf("load glob: %v", err)
		}
		if tbl.NumRows != 2 || tbl.Get(0, "id").Int != 1 || tbl.Get(1, "id").Int != 3 {
			t.Fatalf("glob should skip one bad record across shards, got %s", tbl.String())
		}
	})

	t.Run("glob_json_arrays_share_sample_and_bad_record_budget", func(t *testing.T) {
		dir := t.TempDir()
		writeJSONInferenceFile(t, dir, "a.json", `[{"id":1,"amount":10}]`)
		writeJSONInferenceFile(t, dir, "b.json", `[{"id":2,"amount":"bad"},{"id":3,"amount":30}]`)
		tbl, err := Load(filepath.Join(dir, "*.json"), Options{
			Format:        "json",
			InferRows:     1,
			InferRowsSet:  true,
			MaxBadRecords: 1,
		})
		if err != nil {
			t.Fatalf("load glob json arrays: %v", err)
		}
		if tbl.NumRows != 2 || tbl.Get(0, "id").Int != 1 || tbl.Get(1, "id").Int != 3 {
			t.Fatalf("glob JSON arrays should skip one bad record across shards, got %s", tbl.String())
		}
	})

	t.Run("stdin_jsonl_supports_inference_options", func(t *testing.T) {
		input := "{\"id\":1,\"amount\":10}\n{\"id\":2,\"amount\":\"bad\"}\n{\"id\":3,\"amount\":30}\n"
		tbl, err := LoadInput("-", Options{
			Format:        "jsonl",
			InferRows:     1,
			InferRowsSet:  true,
			MaxBadRecords: 1,
		}, strings.NewReader(input))
		if err != nil {
			t.Fatalf("load stdin jsonl: %v", err)
		}
		if tbl.NumRows != 2 || tbl.Get(1, "id").Int != 3 {
			t.Fatalf("stdin JSONL bad record should be skipped, got %s", tbl.String())
		}
	})
}
