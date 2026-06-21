package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/razeghi71/dq/loader"
)

func TestCLIFixedSchemaContractZeroRowDescribeAcrossFlatFormats(t *testing.T) {
	bin := buildCLI(t)

	for _, input := range cliFlatUserInputFiles() {
		t.Run(input.name+"_filter", func(t *testing.T) {
			rows := readCLIDescribeRows(t, runCLIQuery(t, bin, input.path+" | filter { false } | describe | json"))
			requireCLIDescribeSchema(t, rows, "name", "string", "string", 0)
			requireCLIDescribeSchema(t, rows, "age", "int", "int", 0)
			requireCLIDescribeSchema(t, rows, "city", "string", "string", 0)
		})

		t.Run(input.name+"_filter_select", func(t *testing.T) {
			rows := readCLIDescribeRows(t, runCLIQuery(t, bin, input.path+" | filter { false } | select name, age | describe | json"))
			requireCLIDescribeSchema(t, rows, "name", "string", "string", 0)
			requireCLIDescribeSchema(t, rows, "age", "int", "int", 0)
		})
	}
}

func TestCLIFixedSchemaContractZeroRowNestedProjectionAcrossFormats(t *testing.T) {
	bin := buildCLI(t)
	inputs := []struct {
		name string
		path string
	}{
		{"json", "../../testdata/nested.json"},
		{"jsonl", "../../testdata/nested.jsonl"},
		{"avro", "../../testdata/nested.avro"},
		{"parquet", "../../testdata/nested.parquet"},
	}

	for _, input := range inputs {
		t.Run(input.name, func(t *testing.T) {
			rows := readCLIDescribeRows(t, runCLIQuery(t, bin, input.path+" | filter { false } | select address.city, orders | describe | json"))
			requireCLIDescribeSchema(t, rows, "address_city", "string", "string", 0)
			requireCLIDescribeSchema(t, rows, "orders", "list", "list<record<amount:float, order_id:int, status:string>>", 0)
		})
	}
}

func TestCLIFixedSchemaContractZeroRowOperationCombinations(t *testing.T) {
	bin := buildCLI(t)

	t.Run("distinct_after_empty_filter", func(t *testing.T) {
		rows := readCLIDescribeRows(t, runCLIQuery(t, bin, "../../testdata/users.csv | filter { false } | distinct | describe | json"))
		requireCLIDescribeSchema(t, rows, "name", "string", "string", 0)
		requireCLIDescribeSchema(t, rows, "age", "int", "int", 0)
		requireCLIDescribeSchema(t, rows, "city", "string", "string", 0)
	})

	t.Run("rename_remove_after_empty_filter", func(t *testing.T) {
		rows := readCLIDescribeRows(t, runCLIQuery(t, bin, "../../testdata/users.csv | filter { false } | rename name=person | remove city | describe | json"))
		requireCLIDescribeSchema(t, rows, "person", "string", "string", 0)
		requireCLIDescribeSchema(t, rows, "age", "int", "int", 0)
	})

	t.Run("group_empty_input", func(t *testing.T) {
		rows := readCLIDescribeRows(t, runCLIQuery(t, bin, "../../testdata/users.csv | filter { false } | group city | describe | json"))
		requireCLIDescribeSchema(t, rows, "city", "string", "string", 0)
		requireCLIDescribeSchema(t, rows, "grouped", "list", "list<record<age:int, city:string, name:string>>", 0)
	})
}

func TestCLIFixedSchemaContractZeroRowNullOnlyExpressionSchemas(t *testing.T) {
	bin := buildCLI(t)

	t.Run("transform_null_literal", func(t *testing.T) {
		rows := readCLIDescribeRows(t, runCLIQuery(t, bin, `../../testdata/users.csv | filter { false } | transform x = null | describe | json`))
		requireCLIDescribeSchema(t, rows, "x", "string", "string?", 0)
	})

	t.Run("transform_null_only_expressions_and_nested_shapes", func(t *testing.T) {
		rows := readCLIDescribeRows(t, runCLIQuery(t, bin, `../../testdata/users.csv | filter { false } | transform c = coalesce(null, null), branch = if(age > 0, null, null), rec = struct(x = null), xs = list(null) | describe | json`))
		requireCLIDescribeSchema(t, rows, "c", "string", "string?", 0)
		requireCLIDescribeSchema(t, rows, "branch", "string", "string?", 0)
		requireCLIDescribeSchema(t, rows, "rec", "record", "record<x:string?>", 0)
		requireCLIDescribeSchema(t, rows, "xs", "list", "list<string?>", 0)
	})

	t.Run("reduce_null_literal", func(t *testing.T) {
		rows := readCLIDescribeRows(t, runCLIQuery(t, bin, `../../testdata/users.csv | filter { false } | group city | reduce x = null | describe | json`))
		requireCLIDescribeSchema(t, rows, "x", "string", "string?", 0)
	})
}

func TestCLIFixedSchemaContractZeroRowDivisionNullability(t *testing.T) {
	bin := buildCLI(t)

	t.Run("literal_zero_divisor", func(t *testing.T) {
		rows := readCLIDescribeRows(t, runCLIQuery(t, bin, `../../testdata/users.csv | filter { false } | transform div = age / 0 | describe | json`))
		requireCLIDescribeSchema(t, rows, "div", "float", "float?", 0)
	})

	t.Run("maybe_zero_column_divisor", func(t *testing.T) {
		rows := readCLIDescribeRows(t, runCLIQuery(t, bin, `../../testdata/users.csv | filter { false } | transform div = age / age | describe | json`))
		requireCLIDescribeSchema(t, rows, "div", "float", "float?", 0)
	})

	t.Run("nonzero_literal_divisor", func(t *testing.T) {
		rows := readCLIDescribeRows(t, runCLIQuery(t, bin, `../../testdata/users.csv | filter { false } | transform div = age / 2 | describe | json`))
		requireCLIDescribeSchema(t, rows, "div", "float", "float", 0)
	})

	t.Run("reduce_literal_zero_divisor", func(t *testing.T) {
		rows := readCLIDescribeRows(t, runCLIQuery(t, bin, `../../testdata/users.csv | filter { false } | group city | reduce div = count() / 0 | describe | json`))
		requireCLIDescribeSchema(t, rows, "div", "float", "float?", 0)
	})
}

func TestCLIFixedSchemaContractZeroRowOuterJoinNullability(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()

	emptyLeft := filepath.Join(dir, "empty-left.csv")
	left := filepath.Join(dir, "left.csv")
	emptyRight := filepath.Join(dir, "empty-right.csv")
	right := filepath.Join(dir, "right.csv")
	for path, content := range map[string]string{
		emptyLeft:  "id,name\n",
		left:       "id,name\na,Alice\n",
		emptyRight: "id,amount\n",
		right:      "id,amount\na,10\n",
	} {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	t.Run("left_join_marks_right_side_nullable", func(t *testing.T) {
		rows := readCLIDescribeRows(t, runCLIQuery(t, bin, emptyLeft+" | join left "+right+" on id | describe | json"))
		requireCLIDescribeSchema(t, rows, "id", "string", "string", 0)
		requireCLIDescribeSchema(t, rows, "name", "string", "string", 0)
		requireCLIDescribeSchema(t, rows, "amount", "int", "int?", 0)
	})

	t.Run("right_join_marks_left_side_nullable", func(t *testing.T) {
		rows := readCLIDescribeRows(t, runCLIQuery(t, bin, left+" | join right "+emptyRight+" on id | describe | json"))
		requireCLIDescribeSchema(t, rows, "id", "string", "string", 0)
		requireCLIDescribeSchema(t, rows, "name", "string", "string?", 0)
		requireCLIDescribeSchema(t, rows, "amount", "string", "string", 0)
	})

	t.Run("full_join_marks_both_padded_sides_nullable", func(t *testing.T) {
		rows := readCLIDescribeRows(t, runCLIQuery(t, bin, emptyLeft+" | join full "+emptyRight+" on id | describe | json"))
		requireCLIDescribeSchema(t, rows, "id", "string", "string", 0)
		requireCLIDescribeSchema(t, rows, "name", "string", "string?", 0)
		requireCLIDescribeSchema(t, rows, "amount", "string", "string?", 0)
	})
}

func TestCLIFixedSchemaContractIncompatibleJoinFallbackPreservesRightOnlyKeyTypes(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()

	emptyLeft := filepath.Join(dir, "empty-left.csv")
	nullKeyLeft := filepath.Join(dir, "null-key-left.csv")
	right := filepath.Join(dir, "right.csv")
	emptyRightParquet := filepath.Join(dir, "empty-right.parquet")
	for path, content := range map[string]string{
		emptyLeft:   "id,name\n",
		nullKeyLeft: "id,name\n,no-key\n",
		right:       "id,amount\n1,99\n",
	} {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	t.Run("right_join_empty_left_keeps_right_key_numeric", func(t *testing.T) {
		out := runCLIQuery(t, bin, emptyLeft+" | join right "+right+" on id | json")
		var rows []map[string]any
		if err := json.Unmarshal(out, &rows); err != nil {
			t.Fatalf("invalid JSON: %v\n%s", err, out)
		}
		if len(rows) != 1 {
			t.Fatalf("rows: got %#v, want one row", rows)
		}
		if got, ok := rows[0]["id"].(float64); !ok || got != 1 {
			t.Fatalf("id: got %#v, want numeric 1", rows[0]["id"])
		}
		if rows[0]["name"] != nil {
			t.Fatalf("name: got %#v, want null", rows[0]["name"])
		}
		if got, ok := rows[0]["amount"].(float64); !ok || got != 99 {
			t.Fatalf("amount: got %#v, want numeric 99", rows[0]["amount"])
		}

		describe := readCLIDescribeRows(t, runCLIQuery(t, bin, emptyLeft+" | join right "+right+" on id | describe | json"))
		requireCLIDescribeSchema(t, describe, "id", "int", "int", 1)
	})

	t.Run("full_join_left_null_key_does_not_stringify_right_key", func(t *testing.T) {
		out := runCLIQuery(t, bin, nullKeyLeft+" | join full "+right+" on id | json")
		var rows []map[string]any
		if err := json.Unmarshal(out, &rows); err != nil {
			t.Fatalf("invalid JSON: %v\n%s", err, out)
		}
		if len(rows) != 2 {
			t.Fatalf("rows: got %#v, want two rows", rows)
		}
		var rightOnly map[string]any
		for _, row := range rows {
			if row["amount"] != nil {
				rightOnly = row
				break
			}
		}
		if rightOnly == nil {
			t.Fatalf("right-only row not found in %#v", rows)
		}
		if got, ok := rightOnly["id"].(float64); !ok || got != 1 {
			t.Fatalf("right-only id: got %#v, want numeric 1", rightOnly["id"])
		}

		describe := readCLIDescribeRows(t, runCLIQuery(t, bin, nullKeyLeft+" | join full "+right+" on id | describe | json"))
		requireCLIDescribeSchema(t, describe, "id", "int", "int?", 2)
	})

	stdout := runCLIQuery(t, bin, "../../testdata/users.csv | filter { false } | select age | rename age=id | parquet to "+emptyRightParquet)
	assertNoCLIStdout(t, stdout)

	t.Run("zero_row_right_join_uses_right_parquet_key_schema", func(t *testing.T) {
		rows := readCLIDescribeRows(t, runCLIQuery(t, bin, emptyLeft+" | join right "+emptyRightParquet+" on id | describe | json"))
		requireCLIDescribeSchema(t, rows, "id", "int", "int", 0)
		requireCLIDescribeSchema(t, rows, "name", "string", "string?", 0)
	})

	t.Run("zero_row_full_join_uses_right_parquet_key_schema", func(t *testing.T) {
		rows := readCLIDescribeRows(t, runCLIQuery(t, bin, emptyLeft+" | join full "+emptyRightParquet+" on id | describe | json"))
		requireCLIDescribeSchema(t, rows, "id", "int", "int", 0)
		requireCLIDescribeSchema(t, rows, "name", "string", "string?", 0)
	})
}

func TestCLIFixedSchemaContractEmptyBinaryOutputsPreserveSchemas(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()

	for _, format := range []string{"avro", "parquet"} {
		t.Run(format, func(t *testing.T) {
			outPath := filepath.Join(dir, "empty."+format)
			stdout := runCLIQuery(t, bin, "../../testdata/users.csv | filter { age > 1000 } | select name, age | "+format+" to "+outPath)
			assertNoCLIStdout(t, stdout)

			tbl, err := loader.Load(outPath, loader.Options{Format: format})
			if err != nil {
				t.Fatalf("reload %s output: %v", format, err)
			}
			if tbl.NumRows != 0 {
				t.Fatalf("reloaded %s row count: got %d, want 0", format, tbl.NumRows)
			}
			if got := tbl.Col(tbl.ColIndex("name")).Schema().String(); got != "string" {
				t.Fatalf("name schema after %s round trip: got %q, want string", format, got)
			}
			if got := tbl.Col(tbl.ColIndex("age")).Schema().String(); got != "int" {
				t.Fatalf("age schema after %s round trip: got %q, want int", format, got)
			}
		})
	}
}

func TestCLIFixedSchemaContractIncompatibleTransformBranchesFailDuringPlanning(t *testing.T) {
	bin := buildCLI(t)

	cases := []struct {
		name  string
		query string
	}{
		{
			name:  "record_field_type_conflict",
			query: `../../testdata/users.csv | transform r = if(age == 30, struct(x = 1), struct(x = "a")) | filter { true } | count | json`,
		},
		{
			name:  "late_record_field_type_conflict",
			query: `../../testdata/users.csv | transform r = if(city == "LA", struct(x = "a"), struct(x = age)) | select r.x | json`,
		},
		{
			name:  "nested_record_field_type_conflict",
			query: `../../testdata/users.csv | transform r = if(age == 30, struct(x = 1, y = "z"), if(city == "LA", struct(x = "a", y = "b"), struct(x = age))) | select r.y | json`,
		},
		{
			name:  "numeric_string_field_conflict",
			query: `../../testdata/users.csv | transform r = if(age == 30, struct(x = 1, y = 1), if(city == "LA", struct(x = 2.5, y = "b"), struct(x = age, y = 2))) | filter { r.x == 1.0 } | count | json`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := runCLIQueryExpectError(t, bin, tc.query)
			assertCLIExpressionErrorContains(t, out, "if() branches", "common type")
		})
	}
}

func TestCLIFixedSchemaContractZeroRowNullPropagatingFunctionSchemas(t *testing.T) {
	bin := buildCLI(t)
	path := filepath.Join(t.TempDir(), "d.csv")
	if err := os.WriteFile(path, []byte("d\n2024-01-01\nnull\n"), 0o644); err != nil {
		t.Fatalf("write input csv: %v", err)
	}

	rows := readCLIDescribeRows(t, runCLIQuery(t, bin, path+` | filter { false } | transform y = year(d), ok = str_contains(d, "2024") | select y, ok | describe | json`))
	requireCLIDescribeSchema(t, rows, "y", "int", "int?", 0)
	requireCLIDescribeSchema(t, rows, "ok", "bool", "bool?", 0)
}

func TestCLIFixedSchemaContractZeroRowReduceAggregatesAreNullableExceptCount(t *testing.T) {
	bin := buildCLI(t)
	path := filepath.Join(t.TempDir(), "orders.jsonl")
	if err := os.WriteFile(path, []byte(`{"id":1,"orders":[]}
{"id":2,"orders":[{"amount":3,"note":"first"}]}
`), 0o644); err != nil {
		t.Fatalf("write input jsonl: %v", err)
	}

	rows := readCLIDescribeRows(t, runCLIQuery(t, bin, path+` | filter { false } | reduce orders n = count(), total = sum(amount), avg_amt = avg(amount), min_amt = min(amount), max_amt = max(amount), first_amt = first(amount), last_amt = last(amount), first_note = first(note), last_note = last(note), plus_one = sum(amount) + 1 | describe | json`))
	requireCLIDescribeSchema(t, rows, "n", "int", "int", 0)
	requireCLIDescribeSchema(t, rows, "total", "int", "int?", 0)
	requireCLIDescribeSchema(t, rows, "avg_amt", "float", "float?", 0)
	requireCLIDescribeSchema(t, rows, "min_amt", "int", "int?", 0)
	requireCLIDescribeSchema(t, rows, "max_amt", "int", "int?", 0)
	requireCLIDescribeSchema(t, rows, "first_amt", "int", "int?", 0)
	requireCLIDescribeSchema(t, rows, "last_amt", "int", "int?", 0)
	requireCLIDescribeSchema(t, rows, "first_note", "string", "string?", 0)
	requireCLIDescribeSchema(t, rows, "last_note", "string", "string?", 0)
	requireCLIDescribeSchema(t, rows, "plus_one", "int", "int?", 0)
}

func TestCLIFixedSchemaContractReduceComparisonAndLogicalOperators(t *testing.T) {
	bin := buildCLI(t)

	out := runCLIQuery(t, bin, `../../testdata/users.csv | group city | reduce eq2 = count() == 2, ne2 = count() != 2, gt1 = count() > 1, ge2 = count() >= 2, lt2 = count() < 2, le1 = count() <= 1, both = count() > 1 and count() < 3, either = count() == 1 or count() == 2, and_null = count() == 2 and null, or_null = count() == 2 or null, not2 = not (count() == 2) | remove grouped | sort city | json`)
	var rows []map[string]any
	if err := json.Unmarshal(out, &rows); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if len(rows) != 3 {
		t.Fatalf("rows: got %#v, want three city groups", rows)
	}

	want := map[string]map[string]any{
		"LA": {
			"eq2": true, "ne2": false, "gt1": true, "ge2": true,
			"lt2": false, "le1": false, "both": true, "either": true,
			"and_null": nil, "or_null": true, "not2": false,
		},
		"NY": {
			"eq2": false, "ne2": true, "gt1": true, "ge2": true,
			"lt2": false, "le1": false, "both": false, "either": false,
			"and_null": false, "or_null": nil, "not2": true,
		},
		"SF": {
			"eq2": false, "ne2": true, "gt1": false, "ge2": false,
			"lt2": true, "le1": true, "both": false, "either": true,
			"and_null": false, "or_null": nil, "not2": true,
		},
	}
	for _, row := range rows {
		city, ok := row["city"].(string)
		if !ok {
			t.Fatalf("city: got %#v, want string", row["city"])
		}
		fields, ok := want[city]
		if !ok {
			t.Fatalf("unexpected city row: %#v", row)
		}
		for col, expected := range fields {
			if row[col] != expected {
				t.Fatalf("%s.%s: got %#v, want %#v in row %#v", city, col, row[col], expected, row)
			}
		}
	}

	schemas := readCLIDescribeRows(t, runCLIQuery(t, bin, `../../testdata/users.csv | filter { false } | group city | reduce eq = count() == 0, neq = count() != 0, lt = count() < 1, ge = count() >= 0, both = count() == 0 and null, either = count() == 0 or null, not_eq = not (count() == 0) | select eq, neq, lt, ge, both, either, not_eq | describe | json`))
	requireCLIDescribeSchema(t, schemas, "eq", "bool", "bool", 0)
	requireCLIDescribeSchema(t, schemas, "neq", "bool", "bool", 0)
	requireCLIDescribeSchema(t, schemas, "lt", "bool", "bool", 0)
	requireCLIDescribeSchema(t, schemas, "ge", "bool", "bool", 0)
	requireCLIDescribeSchema(t, schemas, "both", "bool", "bool?", 0)
	requireCLIDescribeSchema(t, schemas, "either", "bool", "bool?", 0)
	requireCLIDescribeSchema(t, schemas, "not_eq", "bool", "bool", 0)
}

func TestCLIFixedSchemaContractListStructPreservesFieldOrder(t *testing.T) {
	bin := buildCLI(t)
	out := runCLIQuery(t, bin, `../../testdata/users.csv | head 1 | transform bundle = list(struct(name = name, age = age)) | select bundle | csv`)
	if want := "bundle\n\"[{name:Alice, age:30}]\"\n"; string(out) != want {
		t.Fatalf("csv output:\ngot:\n%q\nwant:\n%q", string(out), want)
	}
}
