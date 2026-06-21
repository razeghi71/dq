package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/razeghi71/dq/loader"
	"github.com/razeghi71/dq/parser"
	"github.com/razeghi71/dq/table"
)

var nestedListLenFiles = []string{
	testdataDir + "/nested.json",
	testdataDir + "/nested.jsonl",
	testdataDir + "/nested.avro",
	testdataDir + "/nested.parquet",
}

func TestStrLenString(t *testing.T) {
	result := runQuery(t, typedValuesTable(), "transform n = str_len(s) | select n")
	if result.GetAt(0, 0).Int != 5 {
		t.Errorf("str_len(\"hello\"): want 5, got %d", result.GetAt(0, 0).Int)
	}
}

func TestStrLenEmptyString(t *testing.T) {
	tbl := table.NewTable([]string{"s"})
	tbl.AddRow([]table.Value{table.StrVal("")})
	result := runQuery(t, tbl, "transform n = str_len(s) | select n")
	if result.GetAt(0, 0).Int != 0 {
		t.Errorf("str_len(\"\"): want 0, got %d", result.GetAt(0, 0).Int)
	}
}

func TestStrLenNull(t *testing.T) {
	result := runQuery(t, typedValuesTable(), "transform n = str_len(nilcol) | select n")
	if !result.GetAt(0, 0).IsNull() {
		t.Errorf("str_len(null): want null, got %v", result.GetAt(0, 0).AsString())
	}
}

func TestStrLenWrongTypeErrors(t *testing.T) {
	cases := []struct {
		name    string
		query   string
		wantErr string
	}{
		{"list", "transform n = str_len(xs)", "str_len() requires a string, got list"},
		{"int", "transform n = str_len(n)", "str_len() requires a string, got int"},
		{"float", "transform n = str_len(price)", "str_len() requires a string, got float"},
		{"record", "transform n = str_len(rec)", "str_len() requires a string, got record"},
		{"bool", "transform n = str_len(flag)", "str_len() requires a string, got bool"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expectQueryErrContains(t, typedValuesTable(), tc.query, tc.wantErr)
		})
	}
}

func TestStrLenFilterWrongTypeErrors(t *testing.T) {
	cases := []struct {
		name    string
		query   string
		wantErr string
	}{
		{"int", "filter { str_len(n) > 0 }", "str_len() requires a string, got int"},
		{"list", "filter { str_len(xs) > 0 }", "str_len() requires a string, got list"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expectQueryErrContains(t, typedValuesTable(), tc.query, tc.wantErr)
		})
	}
}

func TestListLenList(t *testing.T) {
	result := runQuery(t, typedValuesTable(), "transform n = list_len(xs) | select n")
	if result.GetAt(0, 0).Int != 3 {
		t.Errorf("list_len(3-element list): want 3, got %d", result.GetAt(0, 0).Int)
	}
}

func TestListLenEmptyList(t *testing.T) {
	tbl := table.NewTable([]string{"xs"})
	tbl.AddRow([]table.Value{table.ListVal(nil)})
	result := runQuery(t, tbl, "transform n = list_len(xs) | select n")
	if result.GetAt(0, 0).Int != 0 {
		t.Errorf("list_len([]): want 0, got %d", result.GetAt(0, 0).Int)
	}
}

func TestListLenNull(t *testing.T) {
	result := runQuery(t, typedValuesTable(), "transform n = list_len(nilcol) | select n")
	if !result.GetAt(0, 0).IsNull() {
		t.Errorf("list_len(null): want null, got %v", result.GetAt(0, 0).AsString())
	}
}

func TestListLenNullJSONField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "null_list.json")
	data := `[{"name":"has_empty","orders":[]},{"name":"has_null","orders":null}]`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	result := loadAndQuery(t, path, "transform n = list_len(orders) | select name, n | sort name")
	if result.NumRows != 2 {
		t.Fatalf("expected 2 rows, got %d", result.NumRows)
	}
	nameIdx := result.ColIndex("name")
	nIdx := result.ColIndex("n")
	if result.GetAt(0, nameIdx).Str != "has_empty" || result.GetAt(0, nIdx).Int != 0 {
		t.Errorf("empty list: want has_empty/0, got %q/%v",
			result.GetAt(0, nameIdx).Str, result.GetAt(0, nIdx).AsString())
	}
	if !result.GetAt(1, nIdx).IsNull() {
		t.Errorf("null list field: want null, got %v", result.GetAt(1, nIdx).AsString())
	}
}

func TestListLenWrongTypeErrors(t *testing.T) {
	cases := []struct {
		name    string
		query   string
		wantErr string
	}{
		{"string", "transform n = list_len(s)", "list_len() requires a list, got string"},
		{"int", "transform n = list_len(n)", "list_len() requires a list, got int"},
		{"float", "transform n = list_len(price)", "list_len() requires a list, got float"},
		{"record", "transform n = list_len(rec)", "list_len() requires a list, got record"},
		{"bool", "transform n = list_len(flag)", "list_len() requires a list, got bool"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expectQueryErrContains(t, typedValuesTable(), tc.query, tc.wantErr)
		})
	}
}

func TestListLenFilterWrongTypeErrors(t *testing.T) {
	cases := []struct {
		name    string
		query   string
		wantErr string
	}{
		{"string", "filter { list_len(s) > 0 }", "list_len() requires a list, got string"},
		{"int", "filter { list_len(n) > 0 }", "list_len() requires a list, got int"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expectQueryErrContains(t, typedValuesTable(), tc.query, tc.wantErr)
		})
	}
}

func TestLengthFunctionsArity(t *testing.T) {
	cases := []struct {
		name    string
		query   string
		wantErr string
	}{
		{"str_len_0_args", "transform n = str_len()", "str_len() takes 1 argument, got 0"},
		{"str_len_2_args", "transform n = str_len(s, xs)", "str_len() takes 1 argument, got 2"},
		{"list_len_0_args", "transform n = list_len()", "list_len() takes 1 argument, got 0"},
		{"list_len_2_args", "transform n = list_len(xs, s)", "list_len() takes 1 argument, got 2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expectQueryErrContains(t, typedValuesTable(), tc.query, tc.wantErr)
		})
	}
}

func TestListLenCrossFormat(t *testing.T) {
	want := map[string]int64{"Alice": 2, "Bob": 1, "Charlie": 0}
	for _, file := range nestedListLenFiles {
		t.Run(filepath.Base(file), func(t *testing.T) {
			result := loadAndQuery(t, file, "transform n = list_len(orders) | select name, n")
			assertIntColByName(t, result, "name", "n", want)
		})
	}
}

func TestListLenDotPathCrossFormat(t *testing.T) {
	want := map[string]int64{"Alice": 2, "Bob": 1, "Charlie": 0}
	for _, file := range nestedListLenFiles {
		t.Run(filepath.Base(file), func(t *testing.T) {
			result := loadAndQuery(t, file, "transform n = list_len(profile.history) | select name, n")
			assertIntColByName(t, result, "name", "n", want)
		})
	}
}

func TestListLenMissingDotPath(t *testing.T) {
	expectLoadAndQueryErrContains(t, testdataDir+"/nested.json", "transform n = list_len(profile.missing) | select name, n", `field "missing" not found`)
}

func TestListLenDotPathRecordError(t *testing.T) {
	expectQueryErrContains(t, loadNestedTable(t), "transform n = list_len(profile.stats)", "list_len() requires a list, got record")
}

func TestUnknownLenFunction(t *testing.T) {
	expectQueryErrContains(t, typedValuesTable(), "transform n = len(s)", `unknown function "len"`)
}

func loadNestedTable(t *testing.T) *table.Table {
	t.Helper()
	q, err := parser.Parse(testdataDir + "/nested.json")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	tbl, err := loader.Load(q.Source.Filename, loader.FromAST(q.Source.Load))
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	return tbl
}
