package loader

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	goavro "github.com/linkedin/goavro/v2"
	"github.com/razeghi71/dq/table"
)

func TestPrepareTDDCSVProjectedLoadUsesPreparedSchema(t *testing.T) {
	path := writeSourceProjectionTDDFile(t, "wide.csv", "id,name,status,unused\n1,Alice,active,100\n2,Bob,paused,200\n")

	prepared, err := Prepare(path, Options{})
	if err != nil {
		t.Fatalf("prepare csv: %v", err)
	}
	defer prepared.Close()
	requireSourceProjectionTDDSchema(t, prepared.Schema, "id:int", "name:string", "status:string", "unused:int")

	tbl, err := prepared.Load([]string{"status", "id"})
	if err != nil {
		t.Fatalf("load prepared projection: %v", err)
	}
	requireSourceProjectionTDDColumns(t, tbl, "status", "id")
	if got := tbl.GetAt(0, 0); got.Type != table.TypeString || got.Str != "active" {
		t.Fatalf("row 0 status: got %v", got)
	}
	if got := tbl.GetAt(1, 1); got.Type != table.TypeInt || got.Int != 2 {
		t.Fatalf("row 1 id: got %v", got)
	}

	if _, err := prepared.Load([]string{"id"}); err == nil || !strings.Contains(err.Error(), "already loaded") {
		t.Fatalf("second prepared load error: got %v", err)
	}
}

func TestPrepareTDDCSVFullLoadAndClose(t *testing.T) {
	path := writeSourceProjectionTDDFile(t, "users.csv", "id,name\n1,Alice\n2,Bob\n")

	prepared, err := Prepare(path, Options{InferRows: 1, InferRowsSet: true})
	if err != nil {
		t.Fatalf("prepare csv: %v", err)
	}
	tbl, err := prepared.Load(nil)
	if err != nil {
		t.Fatalf("load prepared full csv: %v", err)
	}
	requireSourceProjectionTDDColumns(t, tbl, "id", "name")
	if err := prepared.Close(); err != nil {
		t.Fatalf("close loaded prepared source: %v", err)
	}
}

func TestPrepareTDDCloseBeforeLoad(t *testing.T) {
	path := writeSourceProjectionTDDFile(t, "users.csv", "id,name\n1,Alice\n")

	prepared, err := Prepare(path, Options{})
	if err != nil {
		t.Fatalf("prepare csv: %v", err)
	}
	if err := prepared.Close(); err != nil {
		t.Fatalf("close prepared source before load: %v", err)
	}
	if err := prepared.Close(); err != nil {
		t.Fatalf("second close prepared source: %v", err)
	}
}

func TestPrepareTDDCSVInferenceModes(t *testing.T) {
	t.Run("infer_all", func(t *testing.T) {
		path := writeSourceProjectionTDDFile(t, "all.csv", "id,amount\n1,10\n2,\n")
		prepared, err := Prepare(path, Options{InferRows: -1, InferRowsSet: true})
		if err != nil {
			t.Fatalf("prepare infer all: %v", err)
		}
		defer prepared.Close()
		requireSourceProjectionTDDSchema(t, prepared.Schema, "id:int", "amount:int?")
		tbl, err := prepared.Load([]string{"amount"})
		if err != nil {
			t.Fatalf("load infer all prepared: %v", err)
		}
		requireSourceProjectionTDDColumns(t, tbl, "amount")
	})

	t.Run("infer_zero_headerless", func(t *testing.T) {
		header := false
		path := writeSourceProjectionTDDFile(t, "headerless.dat", "007,Alice\n010,Bob\n")
		prepared, err := Prepare(path, Options{Format: "csv", Header: &header, InferRows: 0, InferRowsSet: true})
		if err != nil {
			t.Fatalf("prepare headerless infer zero: %v", err)
		}
		defer prepared.Close()
		requireSourceProjectionTDDSchema(t, prepared.Schema, "col1:string?", "col2:string?")
		tbl, err := prepared.Load([]string{"col1"})
		if err != nil {
			t.Fatalf("load headerless infer zero: %v", err)
		}
		if got := tbl.GetAt(0, 0); got.Type != table.TypeString || got.Str != "007" {
			t.Fatalf("row 0 col1: got %v", got)
		}
	})

	t.Run("infer_zero_headerless_single_data_row_is_nullable", func(t *testing.T) {
		header := false
		path := writeSourceProjectionTDDFile(t, "headerless-single.dat", "007,Alice\n")
		prepared, err := Prepare(path, Options{Format: "csv", Header: &header, InferRows: 0, InferRowsSet: true})
		if err != nil {
			t.Fatalf("prepare headerless single-row infer zero: %v", err)
		}
		defer prepared.Close()
		requireSourceProjectionTDDSchema(t, prepared.Schema, "col1:string?", "col2:string?")
		tbl, err := prepared.Load([]string{"col1"})
		if err != nil {
			t.Fatalf("load headerless single-row infer zero: %v", err)
		}
		if tbl.NumRows != 1 {
			t.Fatalf("row count: got %d, want 1", tbl.NumRows)
		}
		if got := tbl.GetAt(0, 0); got.Type != table.TypeString || got.Str != "007" {
			t.Fatalf("row 0 col1: got %v, want string 007", got)
		}
	})

	t.Run("bounded_sample_without_eof_is_nullable", func(t *testing.T) {
		path := writeSourceProjectionTDDFile(t, "bounded.csv", "id\n1\n2\n")
		prepared, err := Prepare(path, Options{InferRows: 1, InferRowsSet: true})
		if err != nil {
			t.Fatalf("prepare bounded sample: %v", err)
		}
		defer prepared.Close()
		requireSourceProjectionTDDSchema(t, prepared.Schema, "id:int?")
	})

	t.Run("bounded_sample_exactly_reaches_eof_stays_precise", func(t *testing.T) {
		path := writeSourceProjectionTDDFile(t, "bounded-exact.csv", "id\n1\n2\n")
		prepared, err := Prepare(path, Options{InferRows: 2, InferRowsSet: true})
		if err != nil {
			t.Fatalf("prepare bounded exact sample: %v", err)
		}
		defer prepared.Close()
		requireSourceProjectionTDDSchema(t, prepared.Schema, "id:int")
		tbl, err := prepared.Load([]string{"id"})
		if err != nil {
			t.Fatalf("load bounded exact sample: %v", err)
		}
		if tbl.NumRows != 2 {
			t.Fatalf("row count: got %d, want 2", tbl.NumRows)
		}
	})

	t.Run("post_sample_probe_row_is_not_lost", func(t *testing.T) {
		path := writeSourceProjectionTDDFile(t, "bounded-pending.csv", "id\n1\n2\n3\n")
		prepared, err := Prepare(path, Options{InferRows: 2, InferRowsSet: true})
		if err != nil {
			t.Fatalf("prepare bounded pending sample: %v", err)
		}
		defer prepared.Close()
		requireSourceProjectionTDDSchema(t, prepared.Schema, "id:int?")
		tbl, err := prepared.Load([]string{"id"})
		if err != nil {
			t.Fatalf("load bounded pending sample: %v", err)
		}
		if tbl.NumRows != 3 {
			t.Fatalf("row count: got %d, want 3", tbl.NumRows)
		}
		if got := tbl.GetAt(2, 0); got.Type != table.TypeInt || got.Int != 3 {
			t.Fatalf("pending row value: got %v, want int 3", got)
		}
	})

	t.Run("bounded_sample_that_reaches_eof_stays_precise", func(t *testing.T) {
		path := writeSourceProjectionTDDFile(t, "bounded-eof.csv", "id\n1\n")
		prepared, err := Prepare(path, Options{InferRows: 10, InferRowsSet: true})
		if err != nil {
			t.Fatalf("prepare bounded eof sample: %v", err)
		}
		defer prepared.Close()
		requireSourceProjectionTDDSchema(t, prepared.Schema, "id:int")
	})

	t.Run("infer_zero_header_only_stays_precise", func(t *testing.T) {
		path := writeSourceProjectionTDDFile(t, "header-only.csv", "id\n")
		prepared, err := Prepare(path, Options{InferRows: 0, InferRowsSet: true})
		if err != nil {
			t.Fatalf("prepare infer zero header-only: %v", err)
		}
		defer prepared.Close()
		requireSourceProjectionTDDSchema(t, prepared.Schema, "id:string")
		tbl, err := prepared.Load([]string{"id"})
		if err != nil {
			t.Fatalf("load infer zero header-only: %v", err)
		}
		if tbl.NumRows != 0 {
			t.Fatalf("row count: got %d, want 0", tbl.NumRows)
		}
	})
}

func TestPrepareTDDCSVErrors(t *testing.T) {
	t.Run("duplicate_header", func(t *testing.T) {
		path := writeSourceProjectionTDDFile(t, "dupe.csv", "id,id\n1,2\n")
		_, err := Prepare(path, Options{})
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "duplicate") {
			t.Fatalf("prepare duplicate header error: got %v", err)
		}
	})

	t.Run("sample_row_width", func(t *testing.T) {
		path := writeSourceProjectionTDDFile(t, "bad-width.csv", "id,name\n1,Alice\n2,Bob,extra\n")
		_, err := Prepare(path, Options{InferRows: 2, InferRowsSet: true})
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "expected 2 field") {
			t.Fatalf("prepare row-width error: got %v", err)
		}
	})

	t.Run("missing_projected_column", func(t *testing.T) {
		path := writeSourceProjectionTDDFile(t, "users.csv", "id,name\n1,Alice\n")
		prepared, err := Prepare(path, Options{})
		if err != nil {
			t.Fatalf("prepare csv: %v", err)
		}
		defer prepared.Close()
		_, err = prepared.Load([]string{"missing"})
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "missing") {
			t.Fatalf("load missing projection error: got %v", err)
		}
	})

	t.Run("post_sample_unreferenced_bad_record", func(t *testing.T) {
		path := writeSourceProjectionTDDFile(t, "bad-type.csv", "id,unused\n1,10\n2,bad\n")
		prepared, err := Prepare(path, Options{InferRows: 1, InferRowsSet: true})
		if err != nil {
			t.Fatalf("prepare bad type csv: %v", err)
		}
		defer prepared.Close()
		tbl, err := prepared.Load([]string{"id"})
		if err != nil {
			t.Fatalf("load projected id should skip unreferenced bad record: %v", err)
		}
		requireSourceProjectionTDDColumns(t, tbl, "id")
		if tbl.NumRows != 2 {
			t.Fatalf("row count: got %d, want 2", tbl.NumRows)
		}
	})

	t.Run("post_sample_referenced_bad_record", func(t *testing.T) {
		path := writeSourceProjectionTDDFile(t, "bad-type.csv", "id,unused\n1,10\n2,bad\n")
		prepared, err := Prepare(path, Options{InferRows: 1, InferRowsSet: true})
		if err != nil {
			t.Fatalf("prepare bad type csv: %v", err)
		}
		defer prepared.Close()
		_, err = prepared.Load([]string{"unused"})
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "unused") {
			t.Fatalf("referenced bad record error: got %v", err)
		}
	})
}

func TestPrepareTDDCSVEmptySource(t *testing.T) {
	path := writeSourceProjectionTDDFile(t, "empty.csv", "")
	prepared, err := Prepare(path, Options{})
	if err != nil {
		t.Fatalf("prepare empty csv: %v", err)
	}
	tbl, err := prepared.Load([]string{"ignored"})
	if err != nil {
		t.Fatalf("load empty prepared csv: %v", err)
	}
	if tbl.NumRows != 0 || len(tbl.Columns) != 0 {
		t.Fatalf("empty prepared table: rows=%d cols=%v", tbl.NumRows, tbl.Columns)
	}
}

func TestPrepareTDDCSVLoadSpecUsesStrictFixedSchemaAppend(t *testing.T) {
	path := writeSourceProjectionTDDFile(t, "strict-append.csv", "id\n1\n")
	prepared, err := Prepare(path, Options{})
	if err != nil {
		t.Fatalf("prepare csv: %v", err)
	}
	defer prepared.Close()

	_, err = prepared.LoadSpec(SourceLoadSpec{
		ReadColumns:   []string{"id"},
		OutputColumns: []string{"id"},
		Predicate: func(row []table.Value) (bool, error) {
			row[0] = table.StrVal("not-an-int")
			return true, nil
		},
	})
	if err == nil {
		t.Fatal("expected fixed-schema append error")
	}
	msg := strings.ToLower(err.Error())
	for _, want := range []string{"id", "int", "string"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("strict append error should mention %q, got %v", want, err)
		}
	}
}

func TestPrepareTDDCanPrepareMatrix(t *testing.T) {
	cases := []struct {
		name     string
		filename string
		opts     Options
		want     bool
	}{
		{name: "empty", filename: "", want: false},
		{name: "stdin", filename: "-", opts: Options{Format: "csv"}, want: true},
		{name: "glob", filename: "part-*.csv", opts: Options{Format: "csv"}, want: true},
		{name: "glob_implicit_extension", filename: "part-*.csv", want: true},
		{name: "glob_implicit_extensionless_pattern", filename: "part-*", want: true},
		{name: "csv", filename: "users.csv", want: true},
		{name: "json", filename: "users.json", want: true},
		{name: "jsonl", filename: "users.jsonl", want: true},
		{name: "avro", filename: "users.avro", want: true},
		{name: "parquet", filename: "users.parquet", want: true},
		{name: "extensionless_with_format", filename: "users.data", opts: Options{Format: "jsonl"}, want: true},
		{name: "unknown", filename: "users.unknown", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CanPrepare(tc.filename, tc.opts); got != tc.want {
				t.Fatalf("CanPrepare(%q, %+v): got %v, want %v", tc.filename, tc.opts, got, tc.want)
			}
		})
	}
}

func TestPrepareTDDSourceLoadPlanValidation(t *testing.T) {
	schema := table.NewSchema(
		[]string{"id", "name"},
		[]*table.TypeDescriptor{{Kind: table.TypeInt}, {Kind: table.TypeString}},
	)

	plan, err := preparedSourceLoadPlanFor(schema, SourceLoadSpec{
		ReadColumns:   []string{"id", "name"},
		OutputColumns: []string{"name"},
	}, "users.jsonl")
	if err != nil {
		t.Fatalf("load plan: %v", err)
	}
	if plan.readAll {
		t.Fatal("projected read plan should not be read-all")
	}
	if strings.Join(plan.readColumns, ",") != "id,name" || strings.Join(plan.outputColumns, ",") != "name" {
		t.Fatalf("plan columns: read=%v output=%v", plan.readColumns, plan.outputColumns)
	}
	if len(plan.outputFromRead) != 1 || plan.outputFromRead[0] != 1 {
		t.Fatalf("outputFromRead: got %v, want [1]", plan.outputFromRead)
	}

	errorCases := []struct {
		name string
		spec SourceLoadSpec
		want string
	}{
		{name: "duplicate_read", spec: SourceLoadSpec{ReadColumns: []string{"id", "id"}, OutputColumns: []string{"id"}}, want: "more than once"},
		{name: "missing_output", spec: SourceLoadSpec{OutputColumns: []string{"missing"}}, want: "not found"},
		{name: "output_not_in_read", spec: SourceLoadSpec{ReadColumns: []string{"id"}, OutputColumns: []string{"name"}}, want: "read set"},
	}
	for _, tc := range errorCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := preparedSourceLoadPlanFor(schema, tc.spec, "users.jsonl")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error: got %v, want %q", err, tc.want)
			}
		})
	}
}

func TestPrepareTDDAddPreparedSourceReadRowPredicateBranches(t *testing.T) {
	plan := preparedSourceLoadPlan{
		outputColumns:  []string{"id"},
		outputSchemas:  []*table.TypeDescriptor{{Kind: table.TypeInt}},
		outputFromRead: []int{0},
		predicate: func(row []table.Value) (bool, error) {
			return row[0].Type == table.TypeInt && row[0].Int > 1, nil
		},
	}
	tbl := table.NewTableWithSchemas(plan.outputColumns, plan.outputSchemas)
	if err := addPreparedSourceReadRow(tbl, []table.Value{table.IntVal(1)}, plan); err != nil {
		t.Fatalf("predicate false row: %v", err)
	}
	if tbl.NumRows != 0 {
		t.Fatalf("predicate false should not append, rows=%d", tbl.NumRows)
	}
	if err := addPreparedSourceReadRow(tbl, []table.Value{table.IntVal(2)}, plan); err != nil {
		t.Fatalf("predicate true row: %v", err)
	}
	if tbl.NumRows != 1 {
		t.Fatalf("predicate true should append one row, rows=%d", tbl.NumRows)
	}

	plan.predicate = func(row []table.Value) (bool, error) {
		return false, errPrepareTDDPredicate
	}
	if err := addPreparedSourceReadRow(tbl, []table.Value{table.IntVal(3)}, plan); err == nil || !strings.Contains(err.Error(), errPrepareTDDPredicate.Error()) {
		t.Fatalf("predicate error: got %v", err)
	}
}

var errPrepareTDDPredicate = prepareTDDError("predicate boom")

type prepareTDDError string

func (e prepareTDDError) Error() string { return string(e) }

func TestPrepareTDDJSONLikeSourceAcquiresSchemaBeforeMaterialization(t *testing.T) {
	cases := []struct {
		name    string
		file    string
		content string
		format  string
	}{
		{name: "json", file: "ids.json", content: `[{"id":1},{"id":2}]`, format: "json"},
		{name: "jsonl", file: "ids.jsonl", content: "{\"id\":1}\n{\"id\":2}\n", format: "jsonl"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSourceProjectionTDDFile(t, tc.file, tc.content)
			prepared, err := Prepare(path, Options{InferRows: 1, InferRowsSet: true})
			if err != nil {
				t.Fatalf("prepare %s: %v", tc.format, err)
			}
			defer prepared.Close()
			requireSourceProjectionTDDSchema(t, prepared.Schema, "id:int?")

			tbl, err := prepared.LoadSpec(SourceLoadSpec{})
			if err != nil {
				t.Fatalf("load prepared %s: %v", tc.format, err)
			}
			requireSourceProjectionTDDColumns(t, tbl, "id")
			if tbl.NumRows != 2 {
				t.Fatalf("row count: got %d, want 2", tbl.NumRows)
			}
			if got := tbl.Col(tbl.ColIndex("id")).Schema().String(); got != "int?" {
				t.Fatalf("materialized schema: got %q, want int?", got)
			}
		})
	}
}

func TestPrepareTDDJSONLikeCompleteInspectionReusesParsedRecords(t *testing.T) {
	cases := []struct {
		name    string
		file    string
		content string
		opts    Options
	}{
		{name: "json_infer_all", file: "ids.json", content: `[{"id":1,"unused":10},{"id":2,"unused":20}]`, opts: Options{InferRows: -1, InferRowsSet: true}},
		{name: "jsonl_infer_all", file: "ids.jsonl", content: "{\"id\":1,\"unused\":10}\n{\"id\":2,\"unused\":20}\n", opts: Options{InferRows: -1, InferRowsSet: true}},
		{name: "json_bounded_reaches_eof", file: "ids-exact.json", content: `[{"id":1},{"id":2}]`, opts: Options{InferRows: 2, InferRowsSet: true}},
		{name: "jsonl_bounded_reaches_eof", file: "ids-exact.jsonl", content: "{\"id\":1}\n{\"id\":2}\n", opts: Options{InferRows: 2, InferRowsSet: true}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSourceProjectionTDDFile(t, tc.file, tc.content)
			prepared, err := Prepare(path, tc.opts)
			if err != nil {
				t.Fatalf("prepare %s: %v", tc.name, err)
			}
			if err := os.Remove(path); err != nil {
				t.Fatalf("remove prepared source: %v", err)
			}

			tbl, err := prepared.Load([]string{"id"})
			if err != nil {
				t.Fatalf("load should use cached parsed records after complete inspection: %v", err)
			}
			requireSourceProjectionTDDColumns(t, tbl, "id")
			if tbl.NumRows != 2 {
				t.Fatalf("row count: got %d, want 2", tbl.NumRows)
			}
			if got := tbl.GetAt(1, 0); got.Type != table.TypeInt || got.Int != 2 {
				t.Fatalf("second id: got %v, want int 2", got)
			}
		})
	}
}

func TestPrepareTDDJSONLikePartialInspectionReopensSourceAtLoad(t *testing.T) {
	cases := []struct {
		name    string
		file    string
		content string
	}{
		{name: "json", file: "ids-partial.json", content: `[{"id":1},{"id":2}]`},
		{name: "jsonl", file: "ids-partial.jsonl", content: "{\"id\":1}\n{\"id\":2}\n"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSourceProjectionTDDFile(t, tc.file, tc.content)
			prepared, err := Prepare(path, Options{InferRows: 1, InferRowsSet: true})
			if err != nil {
				t.Fatalf("prepare %s: %v", tc.name, err)
			}
			if err := os.Remove(path); err != nil {
				t.Fatalf("remove prepared source: %v", err)
			}

			_, err = prepared.Load([]string{"id"})
			if err == nil {
				t.Fatal("expected partial inspection to reopen the source at load time")
			}
			if !strings.Contains(strings.ToLower(err.Error()), "open") {
				t.Fatalf("load error should come from reopening removed source, got %v", err)
			}
		})
	}
}

func TestPrepareTDDJSONLikeLateBadRecordsAreRuntimeLoadErrors(t *testing.T) {
	cases := []struct {
		name    string
		file    string
		content string
	}{
		{name: "json", file: "bad.json", content: `[{"id":1},{"id":"bad"}]`},
		{name: "jsonl", file: "bad.jsonl", content: "{\"id\":1}\n{\"id\":\"bad\"}\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSourceProjectionTDDFile(t, tc.file, tc.content)
			prepared, err := Prepare(path, Options{InferRows: 1, InferRowsSet: true})
			if err != nil {
				t.Fatalf("prepare should only inspect inference window: %v", err)
			}
			_, err = prepared.LoadSpec(SourceLoadSpec{})
			if err == nil {
				t.Fatal("expected late bad record at source load time")
			}
			for _, want := range []string{"id", "int", "string"} {
				if !strings.Contains(strings.ToLower(err.Error()), want) {
					t.Fatalf("error %q missing %q", err, want)
				}
			}
		})
	}
}

func TestPrepareTDDJSONLikeInspectionErrorsAndBadRecordBudget(t *testing.T) {
	nonArray := writeSourceProjectionTDDFile(t, "object.json", `{"id":1}`)
	if _, err := Prepare(nonArray, Options{}); err == nil || !strings.Contains(err.Error(), "expected array") {
		t.Fatalf("non-array JSON prepare error: got %v", err)
	}

	trailing := writeSourceProjectionTDDFile(t, "trailing.json", `[{"id":1}] {"id":2}`)
	if _, err := Prepare(trailing, Options{}); err == nil || !strings.Contains(strings.ToLower(err.Error()), "trailing") {
		t.Fatalf("trailing JSON prepare error: got %v", err)
	}

	malformedArray := writeSourceProjectionTDDFile(t, "malformed-array.json", `[{"id":1}, bad, {"id":2}]`)
	if _, err := Prepare(malformedArray, Options{MaxBadRecords: 10, MaxBadRecordsSet: true}); err == nil {
		t.Fatal("expected malformed JSON array syntax to fail the whole prepared source")
	} else {
		msg := strings.ToLower(err.Error())
		for _, want := range []string{"cannot parse json", "invalid character"} {
			if !strings.Contains(msg, want) {
				t.Fatalf("malformed JSON array error should mention %q, got %v", want, err)
			}
		}
		if strings.Contains(msg, "row 12") {
			t.Fatalf("malformed JSON array syntax should not be counted repeatedly, got %v", err)
		}
	}

	malformedBoundedArray := writeSourceProjectionTDDFile(t, "malformed-bounded-array.json", `[{"id":1}, bad]`)
	if _, err := Prepare(malformedBoundedArray, Options{InferRows: 1, InferRowsSet: true}); err == nil {
		t.Fatal("expected malformed JSON array tail to fail despite bounded inference")
	} else {
		msg := strings.ToLower(err.Error())
		for _, want := range []string{"cannot parse json", "invalid character"} {
			if !strings.Contains(msg, want) {
				t.Fatalf("bounded malformed JSON array error should mention %q, got %v", want, err)
			}
		}
	}

	nonObject := writeSourceProjectionTDDFile(t, "scalar.json", `[1]`)
	if _, err := Prepare(nonObject, Options{}); err == nil || !strings.Contains(err.Error(), "expected JSON object") {
		t.Fatalf("non-object JSON prepare error: got %v", err)
	}

	badLine := writeSourceProjectionTDDFile(t, "bad.jsonl", "not-json\n{\"id\":1}\n")
	if _, err := Prepare(badLine, Options{InferRows: 1, InferRowsSet: true}); err == nil || !strings.Contains(strings.ToLower(err.Error()), "invalid json") {
		t.Fatalf("bad JSONL prepare error: got %v", err)
	}
	scalarLine := writeSourceProjectionTDDFile(t, "scalar.jsonl", "1\n")
	if _, err := Prepare(scalarLine, Options{}); err == nil || !strings.Contains(err.Error(), "expected JSON object") {
		t.Fatalf("scalar JSONL prepare error: got %v", err)
	}
	prepared, err := Prepare(badLine, Options{InferRows: 2, InferRowsSet: true, MaxBadRecords: 1, MaxBadRecordsSet: true})
	if err != nil {
		t.Fatalf("bad JSONL within budget should prepare from later good record: %v", err)
	}
	requireSourceProjectionTDDSchema(t, prepared.Schema, "id:int")
}

func TestPrepareTDDJSONPreparedInferenceNullabilityBranches(t *testing.T) {
	cases := []struct {
		name            string
		inferRows       int
		inferSeen       int
		sampleExhausted bool
		want            bool
	}{
		{name: "infer_all", inferRows: -1, inferSeen: 10, sampleExhausted: false, want: false},
		{name: "no_records", inferRows: 1, inferSeen: 0, sampleExhausted: false, want: false},
		{name: "bounded_exhausted", inferRows: 1, inferSeen: 1, sampleExhausted: true, want: false},
		{name: "bounded_not_exhausted", inferRows: 1, inferSeen: 1, sampleExhausted: false, want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := jsonPreparedInferenceNeedsConservativeNullability(tc.inferRows, tc.inferSeen, tc.sampleExhausted); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestPrepareTDDJSONLikeLoadSpecProjectsAndFilters(t *testing.T) {
	path := writeSourceProjectionTDDFile(t, "ids.jsonl", "{\"id\":1,\"status\":\"active\",\"unused\":10}\n{\"id\":2,\"status\":\"paused\",\"unused\":\"bad\"}\n")
	prepared, err := Prepare(path, Options{InferRows: 1, InferRowsSet: true})
	if err != nil {
		t.Fatalf("prepare jsonl: %v", err)
	}
	tbl, err := prepared.LoadSpec(SourceLoadSpec{
		ReadColumns:   []string{"id", "status"},
		OutputColumns: []string{"id"},
		Predicate: func(row []table.Value) (bool, error) {
			return row[1].Type == table.TypeString && row[1].Str == "active", nil
		},
	})
	if err != nil {
		t.Fatalf("load projected jsonl source: %v", err)
	}
	requireSourceProjectionTDDColumns(t, tbl, "id")
	if tbl.NumRows != 1 {
		t.Fatalf("row count: got %d, want 1", tbl.NumRows)
	}
	if got := tbl.GetAt(0, 0); got.Type != table.TypeInt || got.Int != 1 {
		t.Fatalf("row 0 id: got %v, want int 1", got)
	}
}

func TestPrepareTDDMetadataFormatsAcquireSchemaWithoutPushdown(t *testing.T) {
	avroPath := writePrepareTDDAvroFile(t)
	parquetPath := writeParquetFixture(t, []struct {
		ID   int64  `parquet:"id"`
		Name string `parquet:"name"`
	}{{ID: 1, Name: "Ada"}})

	cases := []struct {
		name string
		path string
		want []string
	}{
		{name: "avro", path: avroPath, want: []string{"id:int", "name:string"}},
		{name: "parquet", path: parquetPath, want: []string{"id:int", "name:string"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prepared, err := Prepare(tc.path, Options{})
			if err != nil {
				t.Fatalf("prepare %s: %v", tc.name, err)
			}
			requireSourceProjectionTDDSchema(t, prepared.Schema, tc.want...)
			tbl, err := prepared.LoadSpec(SourceLoadSpec{
				ReadColumns:   []string{"id", "name"},
				OutputColumns: []string{"name"},
				Predicate: func(row []table.Value) (bool, error) {
					return row[0].Type == table.TypeInt && row[0].Int == 1, nil
				},
			})
			if err != nil {
				t.Fatalf("load prepared %s: %v", tc.name, err)
			}
			requireSourceProjectionTDDColumns(t, tbl, "name")
			if tbl.NumRows != 1 {
				t.Fatalf("row count: got %d, want 1", tbl.NumRows)
			}
		})
	}
}

func TestPrepareTDDValidatePreparedSourceSchemaDiagnostics(t *testing.T) {
	plan := preparedSourceLoadPlan{
		sourceColumns: []string{"id"},
		sourceSchemas: []*table.TypeDescriptor{{Kind: table.TypeInt}},
	}
	cases := []struct {
		name string
		got  table.Schema
		want string
	}{
		{name: "count", got: table.NewSchema([]string{"id", "name"}, []*table.TypeDescriptor{{Kind: table.TypeInt}, {Kind: table.TypeString}}), want: "count"},
		{name: "name", got: table.NewSchema([]string{"other"}, []*table.TypeDescriptor{{Kind: table.TypeInt}}), want: "changed"},
		{name: "schema", got: table.NewSchema([]string{"id"}, []*table.TypeDescriptor{{Kind: table.TypeString}}), want: "schema"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validatePreparedSourceSchema(plan, tc.got)
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), tc.want) {
				t.Fatalf("error: got %v, want %q", err, tc.want)
			}
		})
	}
	if err := validatePreparedSourceSchema(plan, table.NewSchema([]string{"id"}, []*table.TypeDescriptor{{Kind: table.TypeInt}})); err != nil {
		t.Fatalf("matching schema should validate: %v", err)
	}
}

func TestPrepareTDDMetadataInspectAndLoadErrors(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.avro")
	if _, err := inspectAvroSchema(missing); err == nil || !strings.Contains(err.Error(), "cannot open") {
		t.Fatalf("missing avro inspect error: got %v", err)
	}
	invalidAvro := writeSourceProjectionTDDFile(t, "bad.avro", "not-avro")
	if _, err := inspectAvroSchema(invalidAvro); err == nil || !strings.Contains(strings.ToLower(err.Error()), "avro") {
		t.Fatalf("invalid avro inspect error: got %v", err)
	}
	plan := preparedSourceLoadPlan{
		sourceColumns:     []string{"id"},
		sourceSchemas:     []*table.TypeDescriptor{{Kind: table.TypeInt}},
		readSourceIndexes: []int{0},
		outputColumns:     []string{"id"},
		outputSchemas:     []*table.TypeDescriptor{{Kind: table.TypeInt}},
		outputFromRead:    []int{0},
	}
	if _, err := loadPreparedAvroSource(missing, plan); err == nil || !strings.Contains(err.Error(), "cannot open") {
		t.Fatalf("missing avro load error: got %v", err)
	}
	if _, err := (&preparedJSONSource{format: "xml"}).load(SourceLoadSpec{}); err == nil || !strings.Contains(err.Error(), "unsupported format") {
		t.Fatalf("unsupported json source load error: got %v", err)
	}
	if _, err := (&preparedFileSource{filename: invalidAvro, opts: Options{Format: "csv"}, schema: table.NewSchema(nil, nil)}).load(SourceLoadSpec{}); err == nil || !strings.Contains(err.Error(), "unsupported metadata") {
		t.Fatalf("unsupported metadata source load error: got %v", err)
	}
}

func writePrepareTDDAvroFile(t *testing.T) string {
	t.Helper()
	schema := `{"type":"record","name":"User","fields":[{"name":"id","type":"long"},{"name":"name","type":"string"}]}`
	var buf bytes.Buffer
	w, err := goavro.NewOCFWriter(goavro.OCFConfig{W: &buf, Schema: schema})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Append([]map[string]any{{"id": int64(1), "name": "Ada"}}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "users.avro")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPrepareTDDRejectsUnsupportedSources(t *testing.T) {
	cases := []struct {
		name     string
		filename string
		opts     Options
		want     string
	}{
		{name: "stdin", filename: "-", opts: Options{Format: "csv"}, want: "stdin"},
		{name: "glob", filename: filepath.Join(t.TempDir(), "*.csv"), opts: Options{Format: "csv"}, want: "glob"},
		{name: "unknown", filename: "data.unknown", want: "format"},
		{name: "missing", filename: filepath.Join(t.TempDir(), "missing.csv"), want: "cannot open"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Prepare(tc.filename, tc.opts)
			if err == nil {
				t.Fatal("expected prepare error")
			}
			if !strings.Contains(strings.ToLower(err.Error()), tc.want) {
				t.Fatalf("error %q missing %q", err, tc.want)
			}
		})
	}
}

func TestPrepareTDDNilPreparedSourceLoadAndClose(t *testing.T) {
	var prepared *PreparedSource
	if err := prepared.Close(); err != nil {
		t.Fatalf("nil prepared close: %v", err)
	}
	if _, err := prepared.Load(nil); err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("nil prepared load error: got %v", err)
	}

	prepared = &PreparedSource{}
	if err := prepared.Close(); err != nil {
		t.Fatalf("empty prepared close: %v", err)
	}
	if _, err := prepared.Load(nil); err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("empty prepared load error: got %v", err)
	}
}
