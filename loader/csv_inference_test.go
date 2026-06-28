package loader

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/razeghi71/dq/table"
)

func TestLoadCSVInferenceDefaultLateBadRecordErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "late-bad.csv")
	data := "id,amount\n"
	for i := 1; i <= 20480; i++ {
		data += "1,10\n"
	}
	data += "20481,abc\n"
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path, Options{})
	if err == nil {
		t.Fatal("expected type-conversion load error")
	}
	for _, want := range []string{path, "csv row 20482", `column "amount"`, "int", `"abc"`} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not contain %q", err.Error(), want)
		}
	}
}

func TestLoadCSVRejectsDuplicateHeaders(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts Options
	}{
		{name: "streaming", opts: Options{Format: "csv"}},
		{name: "full_inference", opts: Options{Format: "csv", InferRows: -1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadReader(strings.NewReader("a,a\n1,2\n"), tc.opts)
			if err == nil {
				t.Fatal("expected duplicate header error")
			}
			if got, want := err.Error(), `csv header: duplicate column name "a"`; !strings.Contains(got, want) {
				t.Fatalf("error %q does not contain %q", got, want)
			}
		})
	}
}

func TestLoadCSVDefaultInferRowsSamples20480Rows(t *testing.T) {
	var data strings.Builder
	data.WriteString("id,amount\n")
	for i := 1; i <= 20479; i++ {
		data.WriteString("1,10\n")
	}
	data.WriteString("20480,abc\n")
	data.WriteString("20481,20\n")

	tbl, err := LoadReader(strings.NewReader(data.String()), Options{Format: "csv"})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := tbl.Col(tbl.ColIndex("amount")).ColType(); got != table.TypeString {
		t.Fatalf("amount type: got %v, want string because row 20480 participates in default inference", got)
	}
	if tbl.NumRows != 20481 {
		t.Fatalf("row count: got %d, want 20481", tbl.NumRows)
	}
}

func TestLoadCSVBoundedInferenceNullabilityTracksEOF(t *testing.T) {
	t.Run("rows_exactly_equal_infer_rows_are_precise", func(t *testing.T) {
		tbl, err := LoadReader(strings.NewReader("id\n1\n2\n"), Options{
			Format:       "csv",
			InferRows:    2,
			InferRowsSet: true,
		})
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		requireCSVInferenceColumnSchema(t, tbl, "id", "int")
		if tbl.NumRows != 2 {
			t.Fatalf("row count: got %d, want 2", tbl.NumRows)
		}
	})

	t.Run("post_sample_probe_row_is_not_lost", func(t *testing.T) {
		tbl, err := LoadReader(strings.NewReader("id\n1\n2\n3\n"), Options{
			Format:       "csv",
			InferRows:    2,
			InferRowsSet: true,
		})
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		requireCSVInferenceColumnSchema(t, tbl, "id", "int?")
		if tbl.NumRows != 3 {
			t.Fatalf("row count: got %d, want 3", tbl.NumRows)
		}
		if got := tbl.Get(2, "id"); got.Type != table.TypeInt || got.Int != 3 {
			t.Fatalf("pending row value: got %v, want int 3", got)
		}
	})

	t.Run("infer_rows_zero_header_only_is_precise", func(t *testing.T) {
		tbl, err := LoadReader(strings.NewReader("id\n"), Options{
			Format:       "csv",
			InferRows:    0,
			InferRowsSet: true,
		})
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		requireCSVInferenceColumnSchema(t, tbl, "id", "string")
		if tbl.NumRows != 0 {
			t.Fatalf("row count: got %d, want 0", tbl.NumRows)
		}
	})

	t.Run("glob_infer_rows_zero_header_only_is_precise", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "part-1.csv")
		if err := os.WriteFile(path, []byte("id\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		tbl, err := Load(filepath.Join(dir, "part-*.csv"), Options{
			Format:       "csv",
			InferRows:    0,
			InferRowsSet: true,
		})
		if err != nil {
			t.Fatalf("load glob: %v", err)
		}
		requireCSVInferenceColumnSchema(t, tbl, "id", "string")
		if tbl.NumRows != 0 {
			t.Fatalf("row count: got %d, want 0", tbl.NumRows)
		}
	})
}

func TestCSVNumericCandidateHelpers(t *testing.T) {
	cases := []struct {
		value       string
		wantNumber  bool
		wantInteger bool
	}{
		{value: "123", wantNumber: true, wantInteger: true},
		{value: "-123", wantNumber: true, wantInteger: true},
		{value: "+123", wantNumber: true, wantInteger: true},
		{value: "12.5", wantNumber: true, wantInteger: false},
		{value: "1e5", wantNumber: true, wantInteger: false},
		{value: "NaN", wantNumber: true, wantInteger: false},
		{value: "-", wantNumber: true, wantInteger: false},
		{value: "city1", wantNumber: false, wantInteger: false},
	}
	for _, tc := range cases {
		t.Run(tc.value, func(t *testing.T) {
			if got := csvNumericCandidate(tc.value); got != tc.wantNumber {
				t.Fatalf("csvNumericCandidate(%q): got %v, want %v", tc.value, got, tc.wantNumber)
			}
			if tc.wantNumber {
				if got := csvIntegerCandidate(tc.value); got != tc.wantInteger {
					t.Fatalf("csvIntegerCandidate(%q): got %v, want %v", tc.value, got, tc.wantInteger)
				}
			}
		})
	}
}

func requireCSVInferenceColumnSchema(t *testing.T, tbl *table.Table, column, want string) {
	t.Helper()
	idx := tbl.ColIndex(column)
	if idx < 0 {
		t.Fatalf("missing column %q in %v", column, tbl.Columns)
	}
	if got := tbl.Col(idx).Schema().String(); got != want {
		t.Fatalf("%s schema: got %s, want %s", column, got, want)
	}
}

func TestLoadCSVInferenceLateFloatInIntColumnErrors(t *testing.T) {
	var data strings.Builder
	data.WriteString("amount\n")
	for i := 1; i <= 20480; i++ {
		data.WriteString("10\n")
	}
	data.WriteString("2.5\n")

	_, err := LoadReader(strings.NewReader(data.String()), Options{Format: "csv"})
	if err == nil {
		t.Fatal("expected float conversion error after int inference sample")
	}
	for _, want := range []string{"csv row 20482", `column "amount"`, "int", `"2.5"`} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not contain %q", err.Error(), want)
		}
	}
}

func TestLoadCSVMaxBadRecordsSkipsWholeRows(t *testing.T) {
	tbl, err := LoadReader(strings.NewReader("id,amount\n1,10\n2,bad\n3,30\n"), Options{
		Format:        "csv",
		InferRows:     1,
		InferRowsSet:  true,
		MaxBadRecords: 1,
	})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if tbl.NumRows != 2 {
		t.Fatalf("row count after skipped bad row: got %d, want 2", tbl.NumRows)
	}
	if tbl.Get(0, "id").Int != 1 || tbl.Get(1, "id").Int != 3 {
		t.Fatalf("bad row should be skipped whole, got %s", tbl.String())
	}
	if tbl.Get(0, "amount").Int != 10 || tbl.Get(1, "amount").Int != 30 {
		t.Fatalf("amount values: got %s", tbl.String())
	}
}

func TestLoadCSVInferRowsZeroLoadsAllScalarsAsStrings(t *testing.T) {
	tbl, err := LoadReader(strings.NewReader("id,flag,amount,empty,null_lower,null_upper\n1,true,2.5,,null,NULL\n"), Options{
		Format:       "csv",
		InferRows:    0,
		InferRowsSet: true,
	})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	for _, col := range []string{"id", "flag", "amount"} {
		if got := tbl.Get(0, col); got.Type != table.TypeString {
			t.Fatalf("%s: got %v, want string", col, got.Type)
		}
	}
	if !tbl.Get(0, "empty").IsNull() {
		t.Fatalf("empty field should stay null, got %s", tbl.Get(0, "empty").AsString())
	}
	if !tbl.Get(0, "null_lower").IsNull() {
		t.Fatalf("literal null should stay null, got %s", tbl.Get(0, "null_lower").AsString())
	}
	if !tbl.Get(0, "null_upper").IsNull() {
		t.Fatalf("literal NULL should stay null, got %s", tbl.Get(0, "null_upper").AsString())
	}
}

func TestLoadCSVInferRowsAllSeesLateString(t *testing.T) {
	tbl, err := LoadReader(strings.NewReader("id,invoice_id\n1,1001\n2,1002\n3,X-1003\n"), Options{
		Format:       "csv",
		InferRows:    -1,
		InferRowsSet: true,
	})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	idx := tbl.ColIndex("invoice_id")
	if got := tbl.Col(idx).ColType(); got != table.TypeString {
		t.Fatalf("invoice_id type: got %v, want string", got)
	}
	if tbl.Get(0, "invoice_id").Str != "1001" || tbl.Get(2, "invoice_id").Str != "X-1003" {
		t.Fatalf("invoice_id values: got %s", tbl.String())
	}
}

func TestLoadCSVInferenceStringPreservesRawSampleCells(t *testing.T) {
	tbl, err := LoadReader(strings.NewReader("code\n001\nA2\n"), Options{Format: "csv"})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := tbl.Col(tbl.ColIndex("code")).ColType(); got != table.TypeString {
		t.Fatalf("code type: got %v, want string", got)
	}
	if got := tbl.Get(0, "code").Str; got != "001" {
		t.Fatalf("sample string value: got %q, want 001", got)
	}
}

func TestLoadCSVAllNullColumnsInferString(t *testing.T) {
	tbl, err := LoadReader(strings.NewReader("name,age\n,\nnull,NULL\n"), Options{Format: "csv"})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if tbl.NumRows != 2 {
		t.Fatalf("row count: got %d, want 2", tbl.NumRows)
	}
	for _, col := range []string{"name", "age"} {
		idx := tbl.ColIndex(col)
		if idx < 0 {
			t.Fatalf("missing column %q: %v", col, tbl.Columns)
		}
		if got := tbl.Col(idx).ColType(); got != table.TypeString {
			t.Fatalf("%s type: got %v, want string", col, got)
		}
		for row := 0; row < tbl.NumRows; row++ {
			if !tbl.Get(row, col).IsNull() {
				t.Fatalf("%s row %d: want null, got %s", col, row, tbl.Get(row, col).AsString())
			}
		}
	}
}

func TestLoadCSVInferenceFloatAndBoolColumns(t *testing.T) {
	tbl, err := LoadReader(strings.NewReader("ratio,flag\n1,true\n2.5,false\n3,true\n"), Options{Format: "csv"})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := tbl.Col(tbl.ColIndex("ratio")).ColType(); got != table.TypeFloat {
		t.Fatalf("ratio type: got %v, want float", got)
	}
	if got := tbl.Col(tbl.ColIndex("flag")).ColType(); got != table.TypeBool {
		t.Fatalf("flag type: got %v, want bool", got)
	}
	if tbl.Get(1, "ratio").Float != 2.5 || tbl.Get(2, "flag").Bool != true {
		t.Fatalf("unexpected values: %s", tbl.String())
	}
}

func TestLoadCSVHeaderlessInferRowsAllUsesGeneratedColumns(t *testing.T) {
	tbl, err := LoadReader(strings.NewReader("1,true\n2,false\n"), Options{
		Format:       "csv",
		Header:       BoolPtr(false),
		InferRows:    -1,
		InferRowsSet: true,
	})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(tbl.Columns) != 2 || tbl.Columns[0] != "col1" || tbl.Columns[1] != "col2" {
		t.Fatalf("columns: got %v, want [col1 col2]", tbl.Columns)
	}
	if got := tbl.Col(tbl.ColIndex("col1")).ColType(); got != table.TypeInt {
		t.Fatalf("col1 type: got %v, want int", got)
	}
	if got := tbl.Col(tbl.ColIndex("col2")).ColType(); got != table.TypeBool {
		t.Fatalf("col2 type: got %v, want bool", got)
	}
}

func TestLoadCSVReaderPresetColumnsInferRowsAll(t *testing.T) {
	tbl, err := loadCSVReader(strings.NewReader("1,10\n2,20\n"), csvLoadConfig{
		columns:   []string{"id", "amount"},
		header:    true,
		delim:     ',',
		inferRows: -1,
		source:    "preset.csv",
	})
	if err != nil {
		t.Fatalf("loadCSVReader: %v", err)
	}
	if tbl.NumRows != 2 {
		t.Fatalf("row count: got %d, want 2", tbl.NumRows)
	}
	if tbl.Get(0, "id").Int != 1 || tbl.Get(1, "amount").Int != 20 {
		t.Fatalf("values: got %s", tbl.String())
	}
}

func TestLoadCSVReaderPresetColumnsStreamingInference(t *testing.T) {
	tbl, err := loadCSVReader(strings.NewReader("1,10\n2,20\n"), csvLoadConfig{
		columns:   []string{"id", "amount"},
		header:    true,
		delim:     ',',
		inferRows: 1,
		source:    "preset.csv",
	})
	if err != nil {
		t.Fatalf("loadCSVReader: %v", err)
	}
	if tbl.NumRows != 2 {
		t.Fatalf("row count: got %d, want 2", tbl.NumRows)
	}
	if got := tbl.Col(tbl.ColIndex("amount")).ColType(); got != table.TypeInt {
		t.Fatalf("amount type: got %v, want int", got)
	}
	if tbl.Get(1, "amount").Int != 20 {
		t.Fatalf("amount row 1: got %s", tbl.Get(1, "amount").AsString())
	}
}

func TestLoadCSVTypeErrorsNameExpectedFloatAndBool(t *testing.T) {
	cases := []struct {
		name string
		data string
		want string
	}{
		{name: "float", data: "x\n1.5\nbad\n", want: "float"},
		{name: "bool", data: "x\ntrue\nmaybe\n", want: "bool"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadReader(strings.NewReader(tc.data), Options{
				Format:       "csv",
				InferRows:    1,
				InferRowsSet: true,
			})
			if err == nil {
				t.Fatal("expected type-conversion load error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.want)
			}
			if !strings.Contains(err.Error(), "csv row 3") {
				t.Fatalf("error should cite physical row 3, got %v", err)
			}
		})
	}
}

func TestCSVCellParsingHelpers(t *testing.T) {
	cases := []struct {
		name string
		cell string
		typ  table.ValueType
		want table.ValueType
	}{
		{name: "null", cell: "", typ: table.TypeInt, want: table.TypeNull},
		{name: "string", cell: "001", typ: table.TypeString, want: table.TypeString},
		{name: "int", cell: "42", typ: table.TypeInt, want: table.TypeInt},
		{name: "float", cell: "1.25", typ: table.TypeFloat, want: table.TypeFloat},
		{name: "bool", cell: "false", typ: table.TypeBool, want: table.TypeBool},
		{name: "fallback", cell: "7", typ: table.TypeNull, want: table.TypeInt},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseCSVCellAsType(tc.cell, tc.typ)
			if err != nil {
				t.Fatalf("parseCSVCellAsType: %v", err)
			}
			if got.Type != tc.want {
				t.Fatalf("type: got %v, want %v", got.Type, tc.want)
			}
		})
	}

	if _, err := parseCSVCellAsType("nope", table.TypeBool); err == nil {
		t.Fatal("expected bool parse error")
	}
}

func TestCSVTypeNameHelper(t *testing.T) {
	cases := map[table.ValueType]string{
		table.TypeInt:    "int",
		table.TypeFloat:  "float",
		table.TypeString: "string",
		table.TypeBool:   "bool",
		table.TypeNull:   "null",
	}
	for typ, want := range cases {
		if got := csvTypeName(typ); got != want {
			t.Fatalf("csvTypeName(%v): got %q, want %q", typ, got, want)
		}
	}
}
