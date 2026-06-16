package parser

import (
	"reflect"
	"strings"
	"testing"

	"github.com/razeghi71/dq/ast"
)

func assertLoadIntOption(t *testing.T, opts ast.LoadOptions, field string, want int64) {
	t.Helper()
	v := reflect.ValueOf(opts)
	f := v.FieldByName(field)
	if !f.IsValid() {
		t.Fatalf("LoadOptions missing field %s", field)
	}
	if f.Kind() == reflect.Pointer {
		if f.IsNil() {
			t.Fatalf("LoadOptions.%s is nil, want %d", field, want)
		}
		f = f.Elem()
	}
	if f.Kind() < reflect.Int || f.Kind() > reflect.Int64 {
		t.Fatalf("LoadOptions.%s has kind %s, want int kind", field, f.Kind())
	}
	if got := f.Int(); got != want {
		t.Fatalf("LoadOptions.%s: got %d, want %d", field, got, want)
	}
}

func TestParseCSVInferenceLoadOptions(t *testing.T) {
	t.Run("source", func(t *testing.T) {
		q, err := Parse(`data.csv with infer_rows=50, max_bad_records=2 | count`)
		if err != nil {
			t.Fatal(err)
		}
		assertLoadIntOption(t, q.Source.Load, "InferRows", 50)
		assertLoadIntOption(t, q.Source.Load, "MaxBadRecords", 2)
	})

	t.Run("infer_all_rows", func(t *testing.T) {
		q, err := Parse(`data.csv with infer_rows=-1 | describe`)
		if err != nil {
			t.Fatal(err)
		}
		assertLoadIntOption(t, q.Source.Load, "InferRows", -1)
	})

	t.Run("infer_no_rows_all_strings", func(t *testing.T) {
		q, err := Parse(`data.csv with infer_rows=0 | describe`)
		if err != nil {
			t.Fatal(err)
		}
		assertLoadIntOption(t, q.Source.Load, "InferRows", 0)
	})

	t.Run("join_source", func(t *testing.T) {
		q, err := Parse(`users.csv | join left orders.csv with infer_rows=10, max_bad_records=1 on user_id`)
		if err != nil {
			t.Fatal(err)
		}
		j, ok := q.Ops[0].(*ast.JoinOp)
		if !ok {
			t.Fatalf("expected JoinOp, got %T", q.Ops[0])
		}
		assertLoadIntOption(t, j.Load, "InferRows", 10)
		assertLoadIntOption(t, j.Load, "MaxBadRecords", 1)
	})
}

func TestParseJSONInferenceLoadOptions(t *testing.T) {
	t.Run("json_source", func(t *testing.T) {
		q, err := Parse(`data.json with infer_rows=20480, max_bad_records=2 | count`)
		if err != nil {
			t.Fatal(err)
		}
		assertLoadIntOption(t, q.Source.Load, "InferRows", 20480)
		assertLoadIntOption(t, q.Source.Load, "MaxBadRecords", 2)
	})

	t.Run("jsonl_source", func(t *testing.T) {
		q, err := Parse(`data.jsonl with infer_rows=-1, max_bad_records=0 | describe`)
		if err != nil {
			t.Fatal(err)
		}
		assertLoadIntOption(t, q.Source.Load, "InferRows", -1)
		assertLoadIntOption(t, q.Source.Load, "MaxBadRecords", 0)
	})

	t.Run("jsonl_join_source", func(t *testing.T) {
		q, err := Parse(`users.csv | join left events.jsonl with infer_rows=10, max_bad_records=1 on user_id`)
		if err != nil {
			t.Fatal(err)
		}
		j, ok := q.Ops[0].(*ast.JoinOp)
		if !ok {
			t.Fatalf("expected JoinOp, got %T", q.Ops[0])
		}
		assertLoadIntOption(t, j.Load, "InferRows", 10)
		assertLoadIntOption(t, j.Load, "MaxBadRecords", 1)
	})
}

func TestParseCSVInferenceLoadOptionsRejected(t *testing.T) {
	cases := []struct {
		name  string
		query string
		msg   string
	}{
		{"infer_rows_negative_other_than_all", `data.csv with infer_rows=-2 | count`, "infer_rows"},
		{"max_bad_records_negative", `data.csv with max_bad_records=-1 | count`, "max_bad_records"},
		{"infer_rows_non_integer", `data.csv with infer_rows="50" | count`, "infer_rows"},
		{"max_bad_records_non_integer", `data.csv with max_bad_records=true | count`, "max_bad_records"},
		{"duplicate_infer_rows", `data.csv with infer_rows=10, infer_rows=20 | count`, "duplicate"},
		{"duplicate_max_bad_records", `data.csv with max_bad_records=0, max_bad_records=1 | count`, "duplicate"},
		{"infer_rows_zero_on_json", `data.json with infer_rows=0 | count`, "infer_rows=0"},
		{"infer_rows_zero_on_jsonl", `data.jsonl with infer_rows=0 | count`, "infer_rows=0"},
		{"infer_rows_on_avro", `data.avro with infer_rows=10 | count`, "infer_rows applies only"},
		{"max_bad_records_on_avro", `data.avro with max_bad_records=1 | count`, "max_bad_records applies only"},
		{"infer_rows_on_parquet", `data.parquet with infer_rows=10 | count`, "infer_rows applies only"},
		{"max_bad_records_on_parquet", `data.parquet with max_bad_records=1 | count`, "max_bad_records applies only"},
		{"glob_unknown_extension_without_format", `part-*.dat with infer_rows=10 | count`, "with format"},
		{"join_infer_rows_zero_on_json", `users.csv | join orders.json with infer_rows=0 on user_id`, "infer_rows=0"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(tc.query)
			if err == nil {
				t.Fatalf("expected parse error for %q", tc.query)
			}
			if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tc.msg)) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.msg)
			}
		})
	}
}
