package loader

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/razeghi71/dq/rowstream"
	"github.com/razeghi71/dq/table"
)

func TestPrepareStreamTDDCSVPredicateProjectionAndEOF(t *testing.T) {
	path := writeSourceProjectionTDDFile(t, "stream.csv", "id,name\n1,Alice\n2,Bob\n3,Cara\n")
	prepared, err := Prepare(path, Options{InferRows: 1, InferRowsSet: true})
	if err != nil {
		t.Fatalf("prepare csv: %v", err)
	}
	stream, err := prepared.StreamSpec(SourceLoadSpec{
		ReadColumns:   table.SelectedColumns("id", "name"),
		OutputColumns: table.SelectedColumns("name", "id"),
		Predicate: func(row []table.Value) (bool, error) {
			return row[0].Type == table.TypeInt && row[0].Int >= 2, nil
		},
	})
	if err != nil {
		t.Fatalf("stream csv: %v", err)
	}
	defer stream.Close()

	requirePrepareStreamSchema(t, stream.Schema(), "name:string?", "id:int?")
	row := requirePrepareStreamNext(t, stream)
	if row[0].Str != "Bob" || row[1].Int != 2 {
		t.Fatalf("first kept row: got %v, want Bob/2", row)
	}
	row = requirePrepareStreamNext(t, stream)
	if row[0].Str != "Cara" || row[1].Int != 3 {
		t.Fatalf("second kept row: got %v, want Cara/3", row)
	}
	requirePrepareStreamEOF(t, stream)

	if _, err := prepared.StreamSpec(SourceLoadSpec{}); err == nil || !strings.Contains(err.Error(), "already loaded") {
		t.Fatalf("second stream error: got %v", err)
	}
}

func TestPrepareStreamTDDCSVCloseStopsBeforeLateBadRecord(t *testing.T) {
	path := writeSourceProjectionTDDFile(t, "stream-late-bad.csv", "id,unused\n1,10\n2,bad\n")
	prepared, err := Prepare(path, Options{InferRows: 1, InferRowsSet: true})
	if err != nil {
		t.Fatalf("prepare csv: %v", err)
	}
	stream, err := prepared.StreamSpec(SourceLoadSpec{OutputColumns: table.SelectedColumns("id")})
	if err != nil {
		t.Fatalf("stream projected id: %v", err)
	}
	row := requirePrepareStreamNext(t, stream)
	if row[0].Int != 1 {
		t.Fatalf("first id: got %v, want 1", row[0])
	}
	if err := stream.Close(); err != nil {
		t.Fatalf("close before late bad record: %v", err)
	}

	prepared, err = Prepare(path, Options{InferRows: 1, InferRowsSet: true})
	if err != nil {
		t.Fatalf("prepare csv again: %v", err)
	}
	stream, err = prepared.StreamSpec(SourceLoadSpec{OutputColumns: table.SelectedColumns("unused")})
	if err != nil {
		t.Fatalf("stream projected unused: %v", err)
	}
	defer stream.Close()
	requirePrepareStreamNext(t, stream)
	_, _, err = stream.Next()
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "unused") {
		t.Fatalf("late bad record error: got %v", err)
	}
}

func TestPrepareStreamTDDStdinCSVCloseStopsBeforeLateBadRecord(t *testing.T) {
	prepared, err := PrepareInput("-", Options{Format: "csv", InferRows: 1, InferRowsSet: true}, strings.NewReader("id,amount\n1,10\n2,bad\n"))
	if err != nil {
		t.Fatalf("prepare stdin csv: %v", err)
	}
	stream, err := prepared.StreamSpec(SourceLoadSpec{OutputColumns: table.SelectedColumns("amount")})
	if err != nil {
		t.Fatalf("stream stdin amount: %v", err)
	}
	row := requirePrepareStreamNext(t, stream)
	if row[0].Int != 10 {
		t.Fatalf("first amount: got %v, want 10", row[0])
	}
	if err := stream.Close(); err != nil {
		t.Fatalf("close stdin before late bad record: %v", err)
	}

	prepared, err = PrepareInput("-", Options{Format: "csv", InferRows: 1, InferRowsSet: true}, strings.NewReader("id,amount\n1,10\n2,bad\n"))
	if err != nil {
		t.Fatalf("prepare stdin csv again: %v", err)
	}
	stream, err = prepared.StreamSpec(SourceLoadSpec{OutputColumns: table.SelectedColumns("amount")})
	if err != nil {
		t.Fatalf("stream stdin amount again: %v", err)
	}
	defer stream.Close()
	requirePrepareStreamNext(t, stream)
	_, _, err = stream.Next()
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "amount") {
		t.Fatalf("late stdin bad record error: got %v", err)
	}
}

func TestPrepareStreamTDDStdinJSONStreamsLiveReader(t *testing.T) {
	cases := []struct {
		name   string
		format string
		input  string
	}{
		{name: "json", format: "json", input: `[{"id":1},{"id":2}]`},
		{name: "jsonl", format: "jsonl", input: "{\"id\":1}\n{\"id\":2}\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prepared, err := PrepareInput("-", Options{Format: tc.format, InferRows: 1, InferRowsSet: true}, strings.NewReader(tc.input))
			if err != nil {
				t.Fatalf("prepare stdin %s: %v", tc.format, err)
			}
			stream, err := prepared.StreamSpec(SourceLoadSpec{OutputColumns: table.SelectedColumns("id")})
			if err != nil {
				t.Fatalf("stream stdin %s: %v", tc.format, err)
			}
			defer stream.Close()
			requirePrepareStreamSchema(t, stream.Schema(), "id:int?")
			if row := requirePrepareStreamNext(t, stream); row[0].Int != 1 {
				t.Fatalf("first id: got %v, want 1", row[0])
			}
			if row := requirePrepareStreamNext(t, stream); row[0].Int != 2 {
				t.Fatalf("second id: got %v, want 2", row[0])
			}
			requirePrepareStreamEOF(t, stream)
		})
	}
}

func TestPrepareStreamTDDStdinJSONCloseBeforeStream(t *testing.T) {
	prepared, err := PrepareInput("-", Options{Format: "jsonl", InferRows: 1, InferRowsSet: true}, strings.NewReader("{\"id\":1}\n{\"id\":2}\n"))
	if err != nil {
		t.Fatalf("prepare stdin jsonl: %v", err)
	}
	if err := prepared.Close(); err != nil {
		t.Fatalf("close prepared stdin jsonl: %v", err)
	}
	if err := prepared.Close(); err != nil {
		t.Fatalf("second close prepared stdin jsonl: %v", err)
	}
}

func TestPrepareStreamTDDStdinJSONLoadSpecMaterializesLiveReader(t *testing.T) {
	prepared, err := PrepareInput("-", Options{Format: "jsonl", InferRows: -1, InferRowsSet: true}, strings.NewReader("{\"id\":1}\n{\"id\":2}\n"))
	if err != nil {
		t.Fatalf("prepare stdin jsonl: %v", err)
	}
	tbl, err := prepared.LoadSpec(SourceLoadSpec{OutputColumns: table.SelectedColumns("id")})
	if err != nil {
		t.Fatalf("load stdin jsonl: %v", err)
	}
	if tbl.NumRows != 2 || tbl.GetAt(1, 0).Int != 2 {
		t.Fatalf("stdin jsonl load rows: got %s", tbl.String())
	}
}

func TestPrepareStreamTDDPrepareInputErrors(t *testing.T) {
	if _, err := PrepareInput("-", Options{}, strings.NewReader("id\n1\n")); err == nil || !strings.Contains(err.Error(), "format") {
		t.Fatalf("missing stdin format error: got %v", err)
	}
	if _, err := PrepareInput("-", Options{Format: "parquet"}, strings.NewReader("not parquet")); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("unsupported stdin format error: got %v", err)
	}
	if _, err := PrepareInput("-", Options{Format: "csv", Compression: "gzip"}, strings.NewReader("not gzip")); err == nil || !strings.Contains(strings.ToLower(err.Error()), "gzip") {
		t.Fatalf("bad gzip stdin error: got %v", err)
	}
	if _, err := PrepareInput("-", Options{Format: "json", InferRows: 0, InferRowsSet: true}, strings.NewReader("[]")); err == nil || !strings.Contains(err.Error(), "infer_rows=0") {
		t.Fatalf("json infer_rows=0 stdin error: got %v", err)
	}
	if _, err := PrepareInput("-", Options{Format: "json"}, strings.NewReader(`{"id":1}`)); err == nil || !strings.Contains(err.Error(), "expected array") {
		t.Fatalf("invalid stdin json shape error: got %v", err)
	}
	if _, err := PrepareInput("-", Options{Format: "json", InferRows: 1, InferRowsSet: true}, strings.NewReader(`[{"id":1}, bad]`)); err == nil || !strings.Contains(strings.ToLower(err.Error()), "invalid character") {
		t.Fatalf("malformed stdin json array tail error: got %v", err)
	}
	prepared, err := PrepareInput("-", Options{Format: "jsonl"}, strings.NewReader("{\"id\":1}\n"))
	if err != nil {
		t.Fatalf("prepare stdin jsonl: %v", err)
	}
	if _, err := prepared.StreamSpec(SourceLoadSpec{OutputColumns: table.SelectedColumns("missing")}); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("stdin missing projection error: got %v", err)
	}
	if _, err := prepared.StreamSpec(SourceLoadSpec{}); err == nil || !strings.Contains(err.Error(), "already loaded") {
		t.Fatalf("stdin second stream error: got %v", err)
	}
}

func TestPrepareStreamTDDPrepareInputDelegatesReplayableFiles(t *testing.T) {
	path := writeSourceProjectionTDDFile(t, "prepare-input-file.csv", "id\n1\n")
	prepared, err := PrepareInput(path, Options{}, strings.NewReader("ignored\n"))
	if err != nil {
		t.Fatalf("prepare input file: %v", err)
	}
	tbl, err := prepared.LoadSpec(SourceLoadSpec{OutputColumns: table.SelectedColumns("id")})
	if err != nil {
		t.Fatalf("load prepared input file: %v", err)
	}
	if tbl.NumRows != 1 || tbl.GetAt(0, 0).Int != 1 {
		t.Fatalf("prepare input file rows: got %s", tbl.String())
	}
}

func TestPrepareStreamTDDStdinJSONLoadSpecErrorBranches(t *testing.T) {
	prepared, err := PrepareInput("-", Options{Format: "jsonl"}, strings.NewReader("{\"id\":1}\n"))
	if err != nil {
		t.Fatalf("prepare stdin jsonl: %v", err)
	}
	if _, err := prepared.LoadSpec(SourceLoadSpec{OutputColumns: table.SelectedColumns("missing")}); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("stdin jsonl missing load projection error: got %v", err)
	}
	if _, err := prepared.LoadSpec(SourceLoadSpec{}); err == nil || !strings.Contains(err.Error(), "already loaded") {
		t.Fatalf("stdin jsonl second load error: got %v", err)
	}

	prepared, err = PrepareInput("-", Options{Format: "jsonl", InferRows: 1, InferRowsSet: true}, strings.NewReader("{\"id\":1}\nnot-json\n"))
	if err != nil {
		t.Fatalf("prepare stdin jsonl with late bad line: %v", err)
	}
	if _, err := prepared.LoadSpec(SourceLoadSpec{OutputColumns: table.SelectedColumns("id")}); err == nil || !strings.Contains(strings.ToLower(err.Error()), "invalid json") {
		t.Fatalf("stdin jsonl load materialize error: got %v", err)
	}
}

func TestPrepareStreamTDDGlobCSVCloseStopsBeforeLateShardBadRecord(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "part-001.csv"), []byte("id,amount\n1,10\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "part-002.csv"), []byte("id,amount\n2,bad\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pattern := filepath.Join(dir, "part-*.csv")

	prepared, err := Prepare(pattern, Options{Format: "csv", InferRows: 1, InferRowsSet: true})
	if err != nil {
		t.Fatalf("prepare glob csv: %v", err)
	}
	stream, err := prepared.StreamSpec(SourceLoadSpec{OutputColumns: table.SelectedColumns("amount")})
	if err != nil {
		t.Fatalf("stream glob amount: %v", err)
	}
	row := requirePrepareStreamNext(t, stream)
	if row[0].Int != 10 {
		t.Fatalf("first glob amount: got %v, want 10", row[0])
	}
	if err := stream.Close(); err != nil {
		t.Fatalf("close glob before late bad shard: %v", err)
	}

	prepared, err = Prepare(pattern, Options{Format: "csv", InferRows: 1, InferRowsSet: true})
	if err != nil {
		t.Fatalf("prepare glob csv again: %v", err)
	}
	stream, err = prepared.StreamSpec(SourceLoadSpec{OutputColumns: table.SelectedColumns("amount")})
	if err != nil {
		t.Fatalf("stream glob amount again: %v", err)
	}
	defer stream.Close()
	requirePrepareStreamNext(t, stream)
	_, _, err = stream.Next()
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "amount") {
		t.Fatalf("late glob bad record error: got %v", err)
	}
}

func TestPrepareStreamTDDGlobCSVInferenceEdges(t *testing.T) {
	t.Run("all_empty", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "part-001.csv"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "part-002.csv"), []byte(" \n"), 0o644); err != nil {
			t.Fatal(err)
		}
		prepared, err := Prepare(filepath.Join(dir, "part-*.csv"), Options{Format: "csv"})
		if err != nil {
			t.Fatalf("prepare empty glob: %v", err)
		}
		stream, err := prepared.StreamSpec(SourceLoadSpec{})
		if err != nil {
			t.Fatalf("stream empty glob: %v", err)
		}
		requirePrepareStreamSchema(t, stream.Schema())
		requirePrepareStreamEOF(t, stream)
	})

	t.Run("infer_zero_data_is_nullable_string", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "part-001.csv"), []byte("id\n007\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		prepared, err := Prepare(filepath.Join(dir, "part-*.csv"), Options{Format: "csv", InferRows: 0, InferRowsSet: true})
		if err != nil {
			t.Fatalf("prepare infer zero glob: %v", err)
		}
		stream, err := prepared.StreamSpec(SourceLoadSpec{OutputColumns: table.SelectedColumns("id")})
		if err != nil {
			t.Fatalf("stream infer zero glob: %v", err)
		}
		defer stream.Close()
		requirePrepareStreamSchema(t, stream.Schema(), "id:string?")
		row := requirePrepareStreamNext(t, stream)
		if row[0].Type != table.TypeString || row[0].Str != "007" {
			t.Fatalf("infer zero value: got %v, want string 007", row[0])
		}
		requirePrepareStreamEOF(t, stream)
	})

	t.Run("infer_all_sees_late_string", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "part-001.csv"), []byte("id,amount\n1,10\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "part-002.csv"), []byte("id,amount\n2,bad\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		prepared, err := Prepare(filepath.Join(dir, "part-*.csv"), Options{Format: "csv", InferRows: -1, InferRowsSet: true})
		if err != nil {
			t.Fatalf("prepare infer all glob: %v", err)
		}
		stream, err := prepared.StreamSpec(SourceLoadSpec{OutputColumns: table.SelectedColumns("amount")})
		if err != nil {
			t.Fatalf("stream infer all glob: %v", err)
		}
		defer stream.Close()
		requirePrepareStreamSchema(t, stream.Schema(), "amount:string")
		requirePrepareStreamNext(t, stream)
		row := requirePrepareStreamNext(t, stream)
		if row[0].Str != "bad" {
			t.Fatalf("infer all late string: got %v, want bad", row[0])
		}
		requirePrepareStreamEOF(t, stream)
	})
}

func TestPrepareStreamTDDGlobCSVHeaderlessLoadSpec(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "part-001.csv"), []byte("1,Alice\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "part-002.csv"), []byte("2,Bob\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	header := false
	prepared, err := Prepare(filepath.Join(dir, "part-*.csv"), Options{Format: "csv", Header: &header, InferRows: 1, InferRowsSet: true})
	if err != nil {
		t.Fatalf("prepare headerless glob: %v", err)
	}
	tbl, err := prepared.LoadSpec(SourceLoadSpec{OutputColumns: table.SelectedColumns("col2", "col1")})
	if err != nil {
		t.Fatalf("load headerless glob: %v", err)
	}
	requireSourceProjectionTDDColumns(t, tbl, "col2", "col1")
	if tbl.NumRows != 2 || tbl.GetAt(1, 0).Str != "Bob" || tbl.GetAt(1, 1).Int != 2 {
		t.Fatalf("headerless glob rows: got %s", tbl.String())
	}
}

func TestPrepareStreamTDDGlobJSONBadRecordBudget(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "part-001.jsonl"), []byte("not-json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "part-002.jsonl"), []byte("{\"id\":1}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	prepared, err := Prepare(filepath.Join(dir, "part-*.jsonl"), Options{
		Format:           "jsonl",
		InferRows:        2,
		InferRowsSet:     true,
		MaxBadRecords:    1,
		MaxBadRecordsSet: true,
	})
	if err != nil {
		t.Fatalf("prepare glob jsonl with bad record budget: %v", err)
	}
	tbl, err := prepared.LoadSpec(SourceLoadSpec{OutputColumns: table.SelectedColumns("id")})
	if err != nil {
		t.Fatalf("load glob jsonl with bad record budget: %v", err)
	}
	if tbl.NumRows != 1 || tbl.GetAt(0, 0).Int != 1 {
		t.Fatalf("glob jsonl bad budget rows: got %s", tbl.String())
	}
}

func TestPrepareStreamTDDGlobJSONRuntimeBadRecordErrors(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "part-001.jsonl"), []byte("{\"id\":1}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "part-002.jsonl"), []byte("not-json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	prepared, err := Prepare(filepath.Join(dir, "part-*.jsonl"), Options{Format: "jsonl", InferRows: 1, InferRowsSet: true})
	if err != nil {
		t.Fatalf("prepare glob jsonl: %v", err)
	}
	stream, err := prepared.StreamSpec(SourceLoadSpec{OutputColumns: table.SelectedColumns("id")})
	if err != nil {
		t.Fatalf("stream glob jsonl: %v", err)
	}
	defer stream.Close()
	requirePrepareStreamNext(t, stream)
	_, _, err = stream.Next()
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "invalid json") {
		t.Fatalf("glob json runtime bad record error: got %v", err)
	}
}

func TestPrepareStreamTDDGlobJSONBoundedInferenceValidatesArraySyntaxTail(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "part-001.json"), []byte(`[{"id":1}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "part-002.json"), []byte(`[bad]`), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Prepare(filepath.Join(dir, "part-*.json"), Options{InferRows: 1, InferRowsSet: true})
	if err == nil {
		t.Fatal("expected malformed JSON glob shard to fail despite bounded inference")
	}
	msg := strings.ToLower(err.Error())
	for _, want := range []string{"cannot parse json", "invalid character"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("bounded malformed JSON glob error should mention %q, got %v", want, err)
		}
	}
}

func TestPrepareStreamTDDGlobStreamPredicates(t *testing.T) {
	t.Run("csv_predicate_filters_and_errors", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "part-001.csv"), []byte("id\n1\n2\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		prepared, err := Prepare(filepath.Join(dir, "part-*.csv"), Options{Format: "csv"})
		if err != nil {
			t.Fatalf("prepare csv glob: %v", err)
		}
		stream, err := prepared.StreamSpec(SourceLoadSpec{
			ReadColumns:   table.SelectedColumns("id"),
			OutputColumns: table.SelectedColumns("id"),
			Predicate: func(row []table.Value) (bool, error) {
				return row[0].Int == 2, nil
			},
		})
		if err != nil {
			t.Fatalf("stream csv glob predicate: %v", err)
		}
		defer stream.Close()
		row := requirePrepareStreamNext(t, stream)
		if row[0].Int != 2 {
			t.Fatalf("predicate row: got %v, want id 2", row)
		}
		requirePrepareStreamEOF(t, stream)

		prepared, err = Prepare(filepath.Join(dir, "part-*.csv"), Options{Format: "csv"})
		if err != nil {
			t.Fatalf("prepare csv glob again: %v", err)
		}
		stream, err = prepared.StreamSpec(SourceLoadSpec{
			ReadColumns:   table.SelectedColumns("id"),
			OutputColumns: table.SelectedColumns("id"),
			Predicate: func(row []table.Value) (bool, error) {
				return false, errPrepareTDDPredicate
			},
		})
		if err != nil {
			t.Fatalf("stream csv glob predicate error: %v", err)
		}
		defer stream.Close()
		_, _, err = stream.Next()
		if err == nil || !strings.Contains(err.Error(), errPrepareTDDPredicate.Error()) {
			t.Fatalf("csv glob predicate error: got %v", err)
		}
	})

	t.Run("json_predicate_filters", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "part-001.jsonl"), []byte("{\"id\":1}\n{\"id\":2}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		prepared, err := Prepare(filepath.Join(dir, "part-*.jsonl"), Options{Format: "jsonl"})
		if err != nil {
			t.Fatalf("prepare json glob: %v", err)
		}
		stream, err := prepared.StreamSpec(SourceLoadSpec{
			ReadColumns:   table.SelectedColumns("id"),
			OutputColumns: table.SelectedColumns("id"),
			Predicate: func(row []table.Value) (bool, error) {
				return row[0].Int == 2, nil
			},
		})
		if err != nil {
			t.Fatalf("stream json glob predicate: %v", err)
		}
		defer stream.Close()
		row := requirePrepareStreamNext(t, stream)
		if row[0].Int != 2 {
			t.Fatalf("json predicate row: got %v, want id 2", row)
		}
		requirePrepareStreamEOF(t, stream)
	})
}

func TestPrepareStreamTDDGlobPrepareErrorBranches(t *testing.T) {
	dir := t.TempDir()
	if _, err := Prepare(filepath.Join(dir, "missing-*.csv"), Options{Format: "csv"}); err == nil || !strings.Contains(err.Error(), "no files") {
		t.Fatalf("glob no matches error: got %v", err)
	}

	mixedDir := filepath.Join(dir, "mixed")
	if err := os.MkdirAll(mixedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mixedDir, "part-001.csv"), []byte("id\n1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mixedDir, "part-002.jsonl"), []byte("{\"id\":2}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Prepare(filepath.Join(mixedDir, "part-*"), Options{}); err == nil || !strings.Contains(err.Error(), "mixed formats") {
		t.Fatalf("glob mixed format error: got %v", err)
	}

	invalidAvro := filepath.Join(dir, "bad.avro")
	if err := os.WriteFile(invalidAvro, []byte("not avro"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Prepare(filepath.Join(dir, "*.avro"), Options{}); err == nil || !strings.Contains(strings.ToLower(err.Error()), "avro") {
		t.Fatalf("glob invalid avro error: got %v", err)
	}

	badJSONDir := filepath.Join(dir, "bad-json")
	if err := os.MkdirAll(badJSONDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(badJSONDir, "part-001.json"), []byte(`{"id":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Prepare(filepath.Join(badJSONDir, "part-*.json"), Options{}); err == nil || !strings.Contains(err.Error(), "expected array") {
		t.Fatalf("glob bad json error: got %v", err)
	}
}

func TestPrepareStreamTDDGlobMissingProjectionErrors(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "part-001.csv"), []byte("id\n1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	prepared, err := Prepare(filepath.Join(dir, "part-*.csv"), Options{Format: "csv"})
	if err != nil {
		t.Fatalf("prepare csv glob: %v", err)
	}
	if _, err := prepared.StreamSpec(SourceLoadSpec{OutputColumns: table.SelectedColumns("missing")}); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("glob csv missing projection error: got %v", err)
	}

	jsonDir := filepath.Join(dir, "json")
	if err := os.MkdirAll(jsonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(jsonDir, "part-001.jsonl"), []byte("{\"id\":1}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	prepared, err = Prepare(filepath.Join(jsonDir, "part-*.jsonl"), Options{Format: "jsonl"})
	if err != nil {
		t.Fatalf("prepare jsonl glob: %v", err)
	}
	if _, err := prepared.StreamSpec(SourceLoadSpec{OutputColumns: table.SelectedColumns("missing")}); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("glob json missing projection error: got %v", err)
	}

	parquetDir := filepath.Join(dir, "parquet")
	if err := os.MkdirAll(parquetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(testdataDir + "/users.parquet")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parquetDir, "part-001.parquet"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	prepared, err = Prepare(filepath.Join(parquetDir, "part-*.parquet"), Options{})
	if err != nil {
		t.Fatalf("prepare parquet glob: %v", err)
	}
	if _, err := prepared.StreamSpec(SourceLoadSpec{OutputColumns: table.SelectedColumns("missing")}); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("glob parquet missing projection error: got %v", err)
	}
	if _, err := prepared.StreamSpec(SourceLoadSpec{}); err == nil || !strings.Contains(err.Error(), "already loaded") {
		t.Fatalf("glob parquet second stream error: got %v", err)
	}
}

func TestPrepareStreamTDDGlobLoadSpecErrorBranches(t *testing.T) {
	t.Run("missing_projection_is_one_shot", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "part-001.csv"), []byte("id\n1\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		prepared, err := Prepare(filepath.Join(dir, "part-*.csv"), Options{Format: "csv"})
		if err != nil {
			t.Fatalf("prepare csv glob: %v", err)
		}
		if _, err := prepared.LoadSpec(SourceLoadSpec{OutputColumns: table.SelectedColumns("missing")}); err == nil || !strings.Contains(err.Error(), "missing") {
			t.Fatalf("glob load missing projection error: got %v", err)
		}
		if _, err := prepared.LoadSpec(SourceLoadSpec{}); err == nil || !strings.Contains(err.Error(), "already loaded") {
			t.Fatalf("glob second load error: got %v", err)
		}
	})

	t.Run("materialize_reports_lazy_json_error", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "part-001.jsonl"), []byte("{\"id\":1}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "part-002.jsonl"), []byte("not-json\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		prepared, err := Prepare(filepath.Join(dir, "part-*.jsonl"), Options{Format: "jsonl", InferRows: 1, InferRowsSet: true})
		if err != nil {
			t.Fatalf("prepare jsonl glob: %v", err)
		}
		if _, err := prepared.LoadSpec(SourceLoadSpec{OutputColumns: table.SelectedColumns("id")}); err == nil || !strings.Contains(strings.ToLower(err.Error()), "invalid json") {
			t.Fatalf("glob load lazy json error: got %v", err)
		}
	})
}

func TestPrepareStreamTDDGlobHelperBranches(t *testing.T) {
	schema := concatPreparedSchemas([]table.Schema{
		table.NewSchema([]string{"id"}, []*table.TypeDescriptor{{Kind: table.TypeInt}}),
		table.NewSchema([]string{"name"}, []*table.TypeDescriptor{{Kind: table.TypeString}}),
	})
	requireSourceProjectionTDDSchema(t, schema, "id:int?", "name:string?")

	plan := preparedSourceLoadPlan{
		sourceColumns:     []string{"id", "name"},
		sourceSchemas:     []*table.TypeDescriptor{{Kind: table.TypeInt}, {Kind: table.TypeString, Nullable: true}},
		readSourceIndexes: []int{0, 1},
		outputFromRead:    []int{0, 1},
	}
	vals, err := globalReadValuesFromShardRow(rowstream.Row{table.IntVal(7)}, map[string]int{"id": 0}, plan)
	if err != nil {
		t.Fatalf("global read values: %v", err)
	}
	if vals[0].Int != 7 || vals[1].Type != table.TypeNull {
		t.Fatalf("global read values: got %v", vals)
	}
	if got := intersectColumns([]string{"id", "missing", "name"}, []string{"name", "id"}); strings.Join(got, ",") != "id,name" {
		t.Fatalf("intersect columns: got %v", got)
	}
	if idx := schemaColumnIndex(schema, "missing"); idx != -1 {
		t.Fatalf("missing schema index: got %d, want -1", idx)
	}
	nullable := []bool{false, false, false}
	applyCSVNullabilityRow(nullable, []int{1, -1, 2}, csvRawRow{record: []string{"", "x"}})
	if !nullable[0] || !nullable[1] || !nullable[2] {
		t.Fatalf("nullable row: got %v, want all true", nullable)
	}
	if _, err := openJSONRecordStream(io.NopCloser(strings.NewReader("{}")), "xml", jsonLoadConfig{}); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("unsupported json record stream error: got %v", err)
	}
	if got := intersectColumns(nil, []string{"name", "id"}); strings.Join(got, ",") != "name,id" {
		t.Fatalf("intersect nil columns: got %v", got)
	}
	if _, err := (&preparedGlobSource{}).stream(SourceLoadSpec{}); err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("unconfigured glob stream error: got %v", err)
	}

	widened := concatPreparedSchemas([]table.Schema{
		table.NewSchema([]string{"id", "payload"}, []*table.TypeDescriptor{
			{Kind: table.TypeInt},
			{Kind: table.TypeRecord, Fields: []table.FieldDescriptor{
				{Name: "amount", Type: &table.TypeDescriptor{Kind: table.TypeInt}},
			}},
		}),
		table.NewSchema([]string{"id", "payload"}, []*table.TypeDescriptor{
			{Kind: table.TypeString},
			{Kind: table.TypeRecord, Fields: []table.FieldDescriptor{
				{Name: "amount", Type: &table.TypeDescriptor{Kind: table.TypeString}},
				{Name: "status", Type: &table.TypeDescriptor{Kind: table.TypeString}},
			}},
		}),
	})
	requireSourceProjectionTDDSchema(t, widened, "id:string", "payload:record<amount:string, status:string?>")
	widenPlan := preparedSourceLoadPlan{
		sourceColumns:     []string{"id", "payload"},
		sourceSchemas:     []*table.TypeDescriptor{widened.Columns[0].Type, widened.Columns[1].Type},
		readSourceIndexes: []int{0, 1},
		outputFromRead:    []int{0, 1},
	}
	widenVals, err := globalReadValuesFromShardRow(rowstream.Row{
		table.IntVal(7),
		table.RecordVal([]table.RecordField{{Name: "amount", Value: table.IntVal(9)}}),
	}, map[string]int{"id": 0, "payload": 1}, widenPlan)
	if err != nil {
		t.Fatalf("global read widened values: %v", err)
	}
	if got := widenVals[0]; got.Type != table.TypeString || got.Str != "7" {
		t.Fatalf("widened id: got %v, want string 7", got)
	}
	wantPayload := table.RecordVal([]table.RecordField{
		{Name: "amount", Value: table.StrVal("9")},
		{Name: "status", Value: table.Null()},
	})
	if !table.Equal(widenVals[1], wantPayload) {
		t.Fatalf("widened payload: got %s, want %s", widenVals[1].AsString(), wantPayload.AsString())
	}
}

func TestPrepareStreamTDDCSVGlobShardRowClassification(t *testing.T) {
	dir := t.TempDir()
	anchor := []string{"id", "name"}
	cfg := csvConfigFromOptions(Options{Format: "csv"}, nil)

	cases := []struct {
		name        string
		content     string
		wantColumns []string
		wantRowNum  int
		wantFirst   string
	}{
		{
			name:        "repeated_header",
			content:     "id,name\n1,Alice\n",
			wantColumns: []string{"id", "name"},
			wantRowNum:  2,
			wantFirst:   "1",
		},
		{
			name:        "new_header",
			content:     "name,id,email\nAlice,1,a@example.com\n",
			wantColumns: []string{"name", "id", "email"},
			wantRowNum:  2,
			wantFirst:   "Alice",
		},
		{
			name:        "data_first",
			content:     "2,Bob\n",
			wantColumns: []string{"id", "name"},
			wantRowNum:  1,
			wantFirst:   "2",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, tc.name+".csv")
			if err := os.WriteFile(path, []byte(tc.content), 0o644); err != nil {
				t.Fatal(err)
			}
			rows, err := openCSVGlobShardRows(path, anchor, cfg)
			if err != nil {
				t.Fatalf("open shard rows: %v", err)
			}
			defer rows.Close()
			if !sameColumns(rows.columns, tc.wantColumns) {
				t.Fatalf("columns: got %v, want %v", rows.columns, tc.wantColumns)
			}
			row, ok, err := rows.Next()
			if err != nil || !ok {
				t.Fatalf("first row: ok=%v err=%v", ok, err)
			}
			if row.rowNum != tc.wantRowNum || row.record[0] != tc.wantFirst {
				t.Fatalf("row: got num=%d record=%v", row.rowNum, row.record)
			}
			if _, ok, err := rows.Next(); err != nil || ok {
				t.Fatalf("eof: ok=%v err=%v", ok, err)
			}
		})
	}
}

func TestPrepareStreamTDDCSVGlobShardRowValidationBranches(t *testing.T) {
	dir := t.TempDir()
	cfg := csvConfigFromOptions(Options{Format: "csv"}, nil)

	path := filepath.Join(dir, "bad-width.csv")
	if err := os.WriteFile(path, []byte("id\n1\n2,extra\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rows, err := openCSVGlobShardRows(path, nil, cfg)
	if err != nil {
		t.Fatalf("open bad-width shard: %v", err)
	}
	defer rows.Close()
	if row, ok, err := rows.Next(); err != nil || !ok || row.record[0] != "1" {
		t.Fatalf("first shard row: row=%v ok=%v err=%v", row, ok, err)
	}
	if _, _, err := rows.Next(); err == nil || !strings.Contains(strings.ToLower(err.Error()), "columns") {
		t.Fatalf("second shard width error: got %v", err)
	}

	headerless := cfg
	headerless.header = false
	emptyPath := filepath.Join(dir, "empty-headerless.csv")
	if err := os.WriteFile(emptyPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	rows, err = openCSVGlobShardRows(emptyPath, []string{"id"}, headerless)
	if err != nil {
		t.Fatalf("open empty headerless shard: %v", err)
	}
	if !rows.empty || !sameColumns(rows.columns, []string{"id"}) {
		t.Fatalf("empty headerless shard: empty=%v columns=%v", rows.empty, rows.columns)
	}
	requireCSVGlobShardEOF(t, rows)
	if err := rows.Close(); err != nil {
		t.Fatalf("close empty headerless shard: %v", err)
	}

	wrongWidthPath := filepath.Join(dir, "headerless-wrong-width.csv")
	if err := os.WriteFile(wrongWidthPath, []byte("1,extra\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := openCSVGlobShardRows(wrongWidthPath, []string{"id"}, headerless); err == nil || !strings.Contains(strings.ToLower(err.Error()), "columns") {
		t.Fatalf("headerless width error: got %v", err)
	}
}

func TestPrepareStreamTDDGlobLazySourceMutationErrors(t *testing.T) {
	t.Run("csv_schema_changed", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "part-001.csv")
		if err := os.WriteFile(path, []byte("id\n1\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		prepared, err := Prepare(filepath.Join(dir, "part-*.csv"), Options{Format: "csv"})
		if err != nil {
			t.Fatalf("prepare csv glob: %v", err)
		}
		if err := os.WriteFile(path, []byte("other\n1\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		stream, err := prepared.StreamSpec(SourceLoadSpec{})
		if err != nil {
			t.Fatalf("stream csv glob: %v", err)
		}
		defer stream.Close()
		_, _, err = stream.Next()
		if err == nil || !strings.Contains(err.Error(), "column \"id\"") {
			t.Fatalf("csv changed source error: got %v", err)
		}
	})

	t.Run("json_file_removed", func(t *testing.T) {
		dir := t.TempDir()
		first := filepath.Join(dir, "part-001.jsonl")
		second := filepath.Join(dir, "part-002.jsonl")
		if err := os.WriteFile(first, []byte("{\"id\":1}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(second, []byte("{\"id\":2}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		prepared, err := Prepare(filepath.Join(dir, "part-*.jsonl"), Options{Format: "jsonl", InferRows: 1, InferRowsSet: true})
		if err != nil {
			t.Fatalf("prepare json glob: %v", err)
		}
		if err := os.Remove(second); err != nil {
			t.Fatal(err)
		}
		stream, err := prepared.StreamSpec(SourceLoadSpec{OutputColumns: table.SelectedColumns("id")})
		if err != nil {
			t.Fatalf("stream json glob: %v", err)
		}
		defer stream.Close()
		requirePrepareStreamNext(t, stream)
		_, _, err = stream.Next()
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "open") {
			t.Fatalf("json removed file error: got %v", err)
		}
	})

	t.Run("metadata_file_removed", func(t *testing.T) {
		dir := t.TempDir()
		data, err := os.ReadFile(testdataDir + "/users.parquet")
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "part-001.parquet")
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		prepared, err := Prepare(filepath.Join(dir, "part-*.parquet"), Options{})
		if err != nil {
			t.Fatalf("prepare parquet glob: %v", err)
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		stream, err := prepared.StreamSpec(SourceLoadSpec{OutputColumns: table.SelectedColumns("name")})
		if err != nil {
			t.Fatalf("stream parquet glob: %v", err)
		}
		defer stream.Close()
		_, _, err = stream.Next()
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "open") {
			t.Fatalf("metadata removed file error: got %v", err)
		}
	})
}

func TestPrepareStreamTDDGlobJSONStreamsAndLoads(t *testing.T) {
	cases := []struct {
		name    string
		format  string
		ext     string
		partOne string
		partTwo string
	}{
		{name: "json", format: "json", ext: "json", partOne: `[{"id":1}]`, partTwo: `[{"id":2}]`},
		{name: "jsonl", format: "jsonl", ext: "jsonl", partOne: "{\"id\":1}\n", partTwo: "{\"id\":2}\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "part-001."+tc.ext), []byte(tc.partOne), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "part-002."+tc.ext), []byte(tc.partTwo), 0o644); err != nil {
				t.Fatal(err)
			}
			pattern := filepath.Join(dir, "part-*."+tc.ext)
			prepared, err := Prepare(pattern, Options{Format: tc.format, InferRows: 1, InferRowsSet: true})
			if err != nil {
				t.Fatalf("prepare glob %s: %v", tc.format, err)
			}
			stream, err := prepared.StreamSpec(SourceLoadSpec{OutputColumns: table.SelectedColumns("id")})
			if err != nil {
				t.Fatalf("stream glob %s: %v", tc.format, err)
			}
			defer stream.Close()
			requirePrepareStreamSchema(t, stream.Schema(), "id:int?")
			if row := requirePrepareStreamNext(t, stream); row[0].Int != 1 {
				t.Fatalf("first id: got %v, want 1", row[0])
			}
			if row := requirePrepareStreamNext(t, stream); row[0].Int != 2 {
				t.Fatalf("second id: got %v, want 2", row[0])
			}
			requirePrepareStreamEOF(t, stream)

			prepared, err = Prepare(pattern, Options{Format: tc.format, InferRows: 1, InferRowsSet: true})
			if err != nil {
				t.Fatalf("prepare glob %s for load: %v", tc.format, err)
			}
			tbl, err := prepared.LoadSpec(SourceLoadSpec{OutputColumns: table.SelectedColumns("id")})
			if err != nil {
				t.Fatalf("load glob %s: %v", tc.format, err)
			}
			if tbl.NumRows != 2 || tbl.GetAt(1, 0).Int != 2 {
				t.Fatalf("glob %s load rows: got %s", tc.format, tbl.String())
			}
		})
	}
}

func TestPrepareStreamTDDGlobMetadataStreams(t *testing.T) {
	cases := []struct {
		name string
		ext  string
		src  string
	}{
		{name: "avro", ext: "avro", src: testdataDir + "/users.avro"},
		{name: "parquet", ext: "parquet", src: testdataDir + "/users.parquet"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			data, err := os.ReadFile(tc.src)
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			for _, name := range []string{"part-001." + tc.ext, "part-002." + tc.ext} {
				if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			prepared, err := Prepare(filepath.Join(dir, "part-*."+tc.ext), Options{})
			if err != nil {
				t.Fatalf("prepare glob %s: %v", tc.name, err)
			}
			stream, err := prepared.StreamSpec(SourceLoadSpec{OutputColumns: table.SelectedColumns("name")})
			if err != nil {
				t.Fatalf("stream glob %s: %v", tc.name, err)
			}
			defer stream.Close()
			requirePrepareStreamSchema(t, stream.Schema(), "name:string")
			rows := 0
			for {
				row, ok, err := stream.Next()
				if err != nil {
					t.Fatalf("read glob %s: %v", tc.name, err)
				}
				if !ok {
					break
				}
				rows++
				if row[0].Type != table.TypeString || row[0].Str == "" {
					t.Fatalf("glob %s name: got %v", tc.name, row[0])
				}
			}
			if rows == 0 {
				t.Fatalf("glob %s should stream rows", tc.name)
			}

			prepared, err = Prepare(filepath.Join(dir, "part-*."+tc.ext), Options{})
			if err != nil {
				t.Fatalf("prepare glob %s for load: %v", tc.name, err)
			}
			tbl, err := prepared.LoadSpec(SourceLoadSpec{OutputColumns: table.SelectedColumns("name")})
			if err != nil {
				t.Fatalf("load glob %s: %v", tc.name, err)
			}
			if tbl.NumRows != rows {
				t.Fatalf("glob %s load rows: got %d, want %d", tc.name, tbl.NumRows, rows)
			}
		})
	}
}

func TestPrepareStreamTDDCSVEmptySourceAndInvalidPreparedSource(t *testing.T) {
	path := writeSourceProjectionTDDFile(t, "stream-empty.csv", "")
	prepared, err := Prepare(path, Options{})
	if err != nil {
		t.Fatalf("prepare empty csv: %v", err)
	}
	stream, err := prepared.StreamSpec(SourceLoadSpec{})
	if err != nil {
		t.Fatalf("stream empty csv: %v", err)
	}
	requirePrepareStreamSchema(t, stream.Schema())
	requirePrepareStreamEOF(t, stream)
	if err := stream.Close(); err != nil {
		t.Fatalf("second close empty csv stream: %v", err)
	}

	var nilPrepared *PreparedSource
	if _, err := nilPrepared.StreamSpec(SourceLoadSpec{}); err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("nil prepared stream error: got %v", err)
	}
	if _, err := (&PreparedSource{}).StreamSpec(SourceLoadSpec{}); err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("empty prepared stream error: got %v", err)
	}
}

func TestPrepareStreamTDDJSONArrayReopensRestAfterBoundedInference(t *testing.T) {
	path := writeSourceProjectionTDDFile(t, "stream.json", `[{"id":1,"name":"Alice"},{"id":2,"name":"Bob"}]`)
	prepared, err := Prepare(path, Options{InferRows: 1, InferRowsSet: true})
	if err != nil {
		t.Fatalf("prepare json: %v", err)
	}
	stream, err := prepared.StreamSpec(SourceLoadSpec{OutputColumns: table.SelectedColumns("id")})
	if err != nil {
		t.Fatalf("stream json: %v", err)
	}
	defer stream.Close()

	requirePrepareStreamSchema(t, stream.Schema(), "id:int?")
	if row := requirePrepareStreamNext(t, stream); row[0].Int != 1 {
		t.Fatalf("first id: got %v, want 1", row[0])
	}
	if row := requirePrepareStreamNext(t, stream); row[0].Int != 2 {
		t.Fatalf("second id: got %v, want 2", row[0])
	}
	requirePrepareStreamEOF(t, stream)
}

func TestPrepareStreamTDDJSONLReopensRestAndReachesEOF(t *testing.T) {
	path := writeSourceProjectionTDDFile(t, "stream-partial.jsonl", "{\"id\":1}\n\n{\"id\":2}\n")
	prepared, err := Prepare(path, Options{InferRows: 1, InferRowsSet: true})
	if err != nil {
		t.Fatalf("prepare jsonl: %v", err)
	}
	stream, err := prepared.StreamSpec(SourceLoadSpec{OutputColumns: table.SelectedColumns("id")})
	if err != nil {
		t.Fatalf("stream jsonl: %v", err)
	}
	defer stream.Close()

	if row := requirePrepareStreamNext(t, stream); row[0].Int != 1 {
		t.Fatalf("first id: got %v, want 1", row[0])
	}
	if row := requirePrepareStreamNext(t, stream); row[0].Int != 2 {
		t.Fatalf("second id: got %v, want 2", row[0])
	}
	requirePrepareStreamEOF(t, stream)
}

func TestPrepareStreamTDDJSONLCompleteInspectionUsesBufferedRecords(t *testing.T) {
	path := writeSourceProjectionTDDFile(t, "stream-complete.jsonl", "{\"id\":1}\n{\"id\":2}\n")
	prepared, err := Prepare(path, Options{InferRows: -1, InferRowsSet: true})
	if err != nil {
		t.Fatalf("prepare jsonl: %v", err)
	}
	stream, err := prepared.StreamSpec(SourceLoadSpec{OutputColumns: table.SelectedColumns("id")})
	if err != nil {
		t.Fatalf("stream jsonl: %v", err)
	}
	defer stream.Close()

	if row := requirePrepareStreamNext(t, stream); row[0].Int != 1 {
		t.Fatalf("first id: got %v, want 1", row[0])
	}
	if row := requirePrepareStreamNext(t, stream); row[0].Int != 2 {
		t.Fatalf("second id: got %v, want 2", row[0])
	}
	requirePrepareStreamEOF(t, stream)
}

func TestPrepareStreamTDDJSONLCloseStopsBeforeLateMalformedLine(t *testing.T) {
	path := writeSourceProjectionTDDFile(t, "stream-late-bad.jsonl", "{\"id\":1}\nnot-json\n")
	prepared, err := Prepare(path, Options{InferRows: 1, InferRowsSet: true})
	if err != nil {
		t.Fatalf("prepare jsonl: %v", err)
	}
	stream, err := prepared.StreamSpec(SourceLoadSpec{OutputColumns: table.SelectedColumns("id")})
	if err != nil {
		t.Fatalf("stream jsonl: %v", err)
	}
	row := requirePrepareStreamNext(t, stream)
	if row[0].Int != 1 {
		t.Fatalf("first id: got %v, want 1", row[0])
	}
	if err := stream.Close(); err != nil {
		t.Fatalf("close before malformed line: %v", err)
	}

	prepared, err = Prepare(path, Options{InferRows: 1, InferRowsSet: true})
	if err != nil {
		t.Fatalf("prepare jsonl again: %v", err)
	}
	stream, err = prepared.StreamSpec(SourceLoadSpec{OutputColumns: table.SelectedColumns("id")})
	if err != nil {
		t.Fatalf("stream jsonl again: %v", err)
	}
	defer stream.Close()
	requirePrepareStreamNext(t, stream)
	_, _, err = stream.Next()
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "invalid json") {
		t.Fatalf("late malformed line error: got %v", err)
	}
}

func TestPrepareStreamTDDJSONPredicateFalseAndError(t *testing.T) {
	path := writeSourceProjectionTDDFile(t, "stream-predicate.jsonl", "{\"id\":1}\n{\"id\":2}\n")
	prepared, err := Prepare(path, Options{InferRows: -1, InferRowsSet: true})
	if err != nil {
		t.Fatalf("prepare jsonl: %v", err)
	}
	stream, err := prepared.StreamSpec(SourceLoadSpec{
		ReadColumns:   table.SelectedColumns("id"),
		OutputColumns: table.SelectedColumns("id"),
		Predicate: func(row []table.Value) (bool, error) {
			return false, nil
		},
	})
	if err != nil {
		t.Fatalf("stream jsonl predicate false: %v", err)
	}
	requirePrepareStreamEOF(t, stream)

	prepared, err = Prepare(path, Options{InferRows: -1, InferRowsSet: true})
	if err != nil {
		t.Fatalf("prepare jsonl again: %v", err)
	}
	stream, err = prepared.StreamSpec(SourceLoadSpec{
		ReadColumns:   table.SelectedColumns("id"),
		OutputColumns: table.SelectedColumns("id"),
		Predicate: func(row []table.Value) (bool, error) {
			return false, errPrepareTDDPredicate
		},
	})
	if err != nil {
		t.Fatalf("stream jsonl predicate error: %v", err)
	}
	defer stream.Close()
	_, _, err = stream.Next()
	if err == nil || !strings.Contains(err.Error(), errPrepareTDDPredicate.Error()) {
		t.Fatalf("predicate error: got %v", err)
	}
}

func TestPrepareStreamTDDMetadataSourcesStreamRows(t *testing.T) {
	cases := []struct {
		name string
		path string
	}{
		{name: "avro", path: testdataDir + "/users.avro"},
		{name: "parquet", path: testdataDir + "/users.parquet"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prepared, err := Prepare(tc.path, Options{})
			if err != nil {
				t.Fatalf("prepare %s: %v", tc.name, err)
			}
			stream, err := prepared.StreamSpec(SourceLoadSpec{
				ReadColumns:   table.SelectedColumns("name", "age"),
				OutputColumns: table.SelectedColumns("age"),
				Predicate: func(row []table.Value) (bool, error) {
					return row[1].Type == table.TypeInt && row[1].Int >= 30, nil
				},
			})
			if err != nil {
				t.Fatalf("stream %s: %v", tc.name, err)
			}
			defer stream.Close()

			requirePrepareStreamSchema(t, stream.Schema(), "age:int")
			row := requirePrepareStreamNext(t, stream)
			if row[0].Type != table.TypeInt || row[0].Int < 30 {
				t.Fatalf("first kept age: got %v, want int >= 30", row[0])
			}
			for {
				_, ok, err := stream.Next()
				if err != nil {
					t.Fatalf("read remaining %s rows: %v", tc.name, err)
				}
				if !ok {
					break
				}
			}
			if err := stream.Close(); err != nil {
				t.Fatalf("second close %s stream: %v", tc.name, err)
			}
		})
	}
}

func TestPrepareStreamTDDErrorBranches(t *testing.T) {
	t.Run("csv_missing_stream_projection", func(t *testing.T) {
		path := writeSourceProjectionTDDFile(t, "stream-missing.csv", "id\n1\n")
		prepared, err := Prepare(path, Options{})
		if err != nil {
			t.Fatalf("prepare csv: %v", err)
		}
		_, err = prepared.StreamSpec(SourceLoadSpec{OutputColumns: table.SelectedColumns("missing")})
		if err == nil || !strings.Contains(err.Error(), "missing") {
			t.Fatalf("missing csv projection error: got %v", err)
		}
	})

	t.Run("json_missing_stream_projection", func(t *testing.T) {
		path := writeSourceProjectionTDDFile(t, "stream-missing.json", `[{"id":1}]`)
		prepared, err := Prepare(path, Options{})
		if err != nil {
			t.Fatalf("prepare json: %v", err)
		}
		_, err = prepared.StreamSpec(SourceLoadSpec{OutputColumns: table.SelectedColumns("missing")})
		if err == nil || !strings.Contains(err.Error(), "missing") {
			t.Fatalf("missing json projection error: got %v", err)
		}
	})

	t.Run("metadata_missing_stream_projection", func(t *testing.T) {
		prepared, err := Prepare(testdataDir+"/users.avro", Options{})
		if err != nil {
			t.Fatalf("prepare avro: %v", err)
		}
		_, err = prepared.StreamSpec(SourceLoadSpec{OutputColumns: table.SelectedColumns("missing")})
		if err == nil || !strings.Contains(err.Error(), "missing") {
			t.Fatalf("missing metadata projection error: got %v", err)
		}
	})

	t.Run("unsupported_prepared_file_stream_format", func(t *testing.T) {
		p := &preparedFileSource{
			filename: "unsupported.csv",
			opts:     Options{Format: "csv"},
			schema:   table.NewSchema(nil, nil),
		}
		_, err := p.stream(SourceLoadSpec{})
		if err == nil || !strings.Contains(err.Error(), "unsupported metadata format") {
			t.Fatalf("unsupported prepared file stream error: got %v", err)
		}
	})

	t.Run("avro_stream_open_error", func(t *testing.T) {
		_, err := streamPreparedAvroSource("missing.avro", preparedSourceLoadPlan{})
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "cannot open") {
			t.Fatalf("missing avro stream error: got %v", err)
		}
	})

	t.Run("avro_stream_invalid_file", func(t *testing.T) {
		path := writeSourceProjectionTDDFile(t, "not-avro.avro", "not avro")
		_, err := streamPreparedAvroSource(path, preparedSourceLoadPlan{})
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "cannot read avro") {
			t.Fatalf("invalid avro stream error: got %v", err)
		}
	})

	t.Run("parquet_stream_open_error", func(t *testing.T) {
		_, err := streamPreparedParquetSource("missing.parquet", preparedSourceLoadPlan{})
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "cannot open") {
			t.Fatalf("missing parquet stream error: got %v", err)
		}
	})

	t.Run("metadata_stream_schema_mismatch", func(t *testing.T) {
		plan := preparedSourceLoadPlan{
			sourceColumns: []string{"wrong"},
			sourceSchemas: []*table.TypeDescriptor{
				{Kind: table.TypeInt},
			},
		}
		_, err := streamPreparedParquetSource(testdataDir+"/users.parquet", plan)
		if err == nil || !strings.Contains(err.Error(), "schema column count changed") {
			t.Fatalf("metadata schema mismatch error: got %v", err)
		}
	})

	t.Run("json_stream_after_load", func(t *testing.T) {
		path := writeSourceProjectionTDDFile(t, "json-loaded.jsonl", "{\"id\":1}\n")
		prepared, err := Prepare(path, Options{})
		if err != nil {
			t.Fatalf("prepare jsonl: %v", err)
		}
		if _, err := prepared.LoadSpec(SourceLoadSpec{}); err != nil {
			t.Fatalf("load jsonl: %v", err)
		}
		_, err = prepared.StreamSpec(SourceLoadSpec{})
		if err == nil || !strings.Contains(err.Error(), "already loaded") {
			t.Fatalf("json stream after load error: got %v", err)
		}
	})

	t.Run("metadata_stream_after_load", func(t *testing.T) {
		prepared, err := Prepare(testdataDir+"/users.parquet", Options{})
		if err != nil {
			t.Fatalf("prepare parquet: %v", err)
		}
		if _, err := prepared.Load([]string{"name"}); err != nil {
			t.Fatalf("load parquet: %v", err)
		}
		_, err = prepared.StreamSpec(SourceLoadSpec{})
		if err == nil || !strings.Contains(err.Error(), "already loaded") {
			t.Fatalf("metadata stream after load error: got %v", err)
		}
	})

	t.Run("avro_stream_schema_mismatch", func(t *testing.T) {
		plan := preparedSourceLoadPlan{
			sourceColumns: []string{"wrong"},
			sourceSchemas: []*table.TypeDescriptor{
				{Kind: table.TypeInt},
			},
		}
		_, err := streamPreparedAvroSource(testdataDir+"/users.avro", plan)
		if err == nil || !strings.Contains(err.Error(), "schema column count changed") {
			t.Fatalf("avro schema mismatch error: got %v", err)
		}
	})

	t.Run("csv_output_not_in_read_set", func(t *testing.T) {
		_, err := csvPreparedLoadPlanFor(
			[]string{"id", "name"},
			[]table.ValueType{table.TypeInt, table.TypeString},
			[]*table.TypeDescriptor{{Kind: table.TypeInt}, {Kind: table.TypeString}},
			SourceLoadSpec{ReadColumns: table.SelectedColumns("id"), OutputColumns: table.SelectedColumns("name")},
			"users.csv",
		)
		if err == nil || !strings.Contains(err.Error(), "read set") {
			t.Fatalf("csv read-set error: got %v", err)
		}
	})

	t.Run("json_rest_stream_unsupported_format", func(t *testing.T) {
		path := writeSourceProjectionTDDFile(t, "rest.data", "[]")
		_, err := openJSONRestStream(path, "yaml", jsonLoadConfig{source: path}, 0)
		if err == nil || !strings.Contains(err.Error(), "unsupported format") {
			t.Fatalf("unsupported rest stream error: got %v", err)
		}
	})

	t.Run("json_array_stream_requires_array", func(t *testing.T) {
		_, err := newJSONArrayRecordStream(io.NopCloser(strings.NewReader(`{"id":1}`)), jsonLoadConfig{source: "inline"}, 0)
		if err == nil || !strings.Contains(err.Error(), "expected array") {
			t.Fatalf("json array stream shape error: got %v", err)
		}
	})

	t.Run("json_array_stream_skip_past_eof", func(t *testing.T) {
		stream, err := newJSONArrayRecordStream(io.NopCloser(strings.NewReader(`[{"id":1}]`)), jsonLoadConfig{source: "inline"}, 2)
		if err != nil {
			t.Fatalf("json array stream skip: %v", err)
		}
		if err := stream.Close(); err != nil {
			t.Fatalf("close skipped json array stream: %v", err)
		}
		if _, ok, err := stream.Next(); err != nil || ok {
			t.Fatalf("closed json array stream: ok=%v err=%v", ok, err)
		}
	})
}

func requirePrepareStreamNext(t *testing.T, stream rowstream.Stream) rowstream.Row {
	t.Helper()
	row, ok, err := stream.Next()
	if err != nil {
		t.Fatalf("stream next: %v", err)
	}
	if !ok {
		t.Fatal("stream ended before expected row")
	}
	return row
}

func requirePrepareStreamEOF(t *testing.T, stream rowstream.Stream) {
	t.Helper()
	row, ok, err := stream.Next()
	if err != nil {
		t.Fatalf("stream eof: %v", err)
	}
	if ok {
		t.Fatalf("stream next: got row %v, want eof", row)
	}
}

func requireCSVGlobShardEOF(t *testing.T, rows *csvGlobShardRows) {
	t.Helper()
	row, ok, err := rows.Next()
	if err != nil {
		t.Fatalf("csv glob shard eof: %v", err)
	}
	if ok {
		t.Fatalf("csv glob shard next: got row %v, want eof", row)
	}
}

func requirePrepareStreamSchema(t *testing.T, schema table.Schema, want ...string) {
	t.Helper()
	if len(schema.Columns) != len(want) {
		t.Fatalf("schema columns: got %v, want %v", schema.Columns, want)
	}
	for i, col := range schema.Columns {
		got := col.Name + ":" + table.Render(col.Type)
		if got != want[i] {
			t.Fatalf("schema column %d: got %q, want %q", i, got, want[i])
		}
	}
}
