package main

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

type cliRightJoinDemandInput struct {
	name   string
	source string
}

func TestCLIRightJoinDemandPruningTDDHappyPathsAcrossRightFormats(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	left := writeCLISourceProjectionTDDFile(t, dir, "users.csv", "id,name\n1,Alice\n2,Bob\n3,Cara\n")

	for _, right := range cliRightJoinDemandPruningAllFormatInputs(t, bin, dir, "orders", false) {
		t.Run(right.name, func(t *testing.T) {
			rows := readCLIJSONMaps(t, runCLIQuery(t, bin,
				left+` | join `+right.source+` on id == user_id | sort name | select name, amount | json`,
			))
			requireCLIJSONColumns(t, rows, "name", "amount")
			if len(rows) != 2 {
				t.Fatalf("rows: got %#v, want Alice and Bob", rows)
			}
			if rows[0]["name"] != "Alice" || rows[0]["amount"] != float64(10) ||
				rows[1]["name"] != "Bob" || rows[1]["amount"] != float64(20) {
				t.Fatalf("rows: got %#v, want Alice=10 Bob=20", rows)
			}
		})
	}
}

func TestCLIRightJoinDemandPruningTDDSkipsUnreferencedRightLateBadRowsAcrossTextFormats(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	left := writeCLISourceProjectionTDDFile(t, dir, "users.csv", "id,name\n1,Alice\n2,Bob\n")

	for _, right := range cliRightJoinDemandPruningBadTextInputs(t, dir, "orders") {
		t.Run(right.name, func(t *testing.T) {
			rows := readCLIJSONMaps(t, runCLIQuery(t, bin,
				left+` | join `+right.source+` with infer_rows=1 on id == user_id | sort name | select name, amount | json`,
			))
			requireCLIJSONColumns(t, rows, "name", "amount")
			if len(rows) != 2 || rows[0]["amount"] != float64(10) || rows[1]["amount"] != float64(20) {
				t.Fatalf("rows: got %#v, want both joined amounts while unused_bad is pruned", rows)
			}
		})
	}
}

func TestCLIRightJoinDemandPruningTDDDemandedRightLateBadRowsRemainObservableAcrossTextFormats(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	left := writeCLISourceProjectionTDDFile(t, dir, "users.csv", "id,name\n1,Alice\n2,Bob\n")

	for _, right := range cliRightJoinDemandPruningBadTextInputs(t, dir, "orders-demanded") {
		t.Run(right.name+"_select", func(t *testing.T) {
			out := runCLIQueryExpectError(t, bin,
				left+` | join `+right.source+` with infer_rows=1 on id == user_id | select unused_bad | json`,
			)
			requireCLIDemandPruningTDDErrorContains(t, out, "unused_bad")
		})

		t.Run(right.name+"_filter", func(t *testing.T) {
			out := runCLIQueryExpectError(t, bin,
				left+` | join `+right.source+` with infer_rows=1 on id == user_id | filter { unused_bad > 0 } | select name | json`,
			)
			requireCLIDemandPruningTDDErrorContains(t, out, "unused_bad")
		})
	}
}

func TestCLIRightJoinDemandPruningTDDRightKeysRemainSemanticDemand(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	left := writeCLISourceProjectionTDDFile(t, dir, "users.csv", "id,name\n1,Alice\n2,Bob\n3,Cara\n")

	t.Run("count_after_inner_join_reads_only_keys", func(t *testing.T) {
		right := writeCLISourceProjectionTDDFile(t, dir, "orders-count.csv", "user_id,amount,unused_bad\n1,10,1\n1,11,bad\n2,20,3\n")
		rows := readCLIJSONMaps(t, runCLIQuery(t, bin,
			left+` | join `+right+` with infer_rows=1 on id == user_id | count | json`,
		))
		requireCLIDemandPruningTDDCount(t, rows, 3)
	})

	t.Run("left_join_without_right_outputs_still_preserves_duplicate_matches", func(t *testing.T) {
		right := writeCLISourceProjectionTDDFile(t, dir, "orders-left.csv", "user_id,amount,unused_bad\n1,10,1\n1,11,bad\n")
		rows := readCLIJSONMaps(t, runCLIQuery(t, bin,
			left+` | join left `+right+` with infer_rows=1 on id == user_id | sort name | select name | json`,
		))
		requireCLIJSONColumns(t, rows, "name")
		got := cliRightJoinDemandPruningColumnStrings(rows, "name")
		want := []string{"Alice", "Alice", "Bob", "Cara"}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("left join names: got %#v, want %#v", got, want)
		}
	})

	t.Run("right_join_uses_right_key_for_unmatched_merged_key_output", func(t *testing.T) {
		right := writeCLISourceProjectionTDDFile(t, dir, "orders-right.csv", "user_id,unused_bad\n1,1\n4,bad\n")
		rows := readCLIJSONMaps(t, runCLIQuery(t, bin,
			left+` | join right `+right+` with infer_rows=1 on id == user_id | sort id | select id | json`,
		))
		requireCLIJSONColumns(t, rows, "id")
		if got := cliRightJoinDemandPruningColumnNumbers(rows, "id"); strings.Join(got, ",") != "1,4" {
			t.Fatalf("right join ids: got %#v, want [1 4]", got)
		}
	})

	t.Run("full_join_uses_both_keys_for_unmatched_merged_key_output", func(t *testing.T) {
		right := writeCLISourceProjectionTDDFile(t, dir, "orders-full.csv", "user_id,unused_bad\n1,1\n4,bad\n")
		rows := readCLIJSONMaps(t, runCLIQuery(t, bin,
			left+` | join full `+right+` with infer_rows=1 on id == user_id | sort id | select id | json`,
		))
		requireCLIJSONColumns(t, rows, "id")
		if got := cliRightJoinDemandPruningColumnNumbers(rows, "id"); strings.Join(got, ",") != "1,2,3,4" {
			t.Fatalf("full join ids: got %#v, want [1 2 3 4]", got)
		}
	})
}

func TestCLIRightJoinDemandPruningTDDCollisionNamesStayStableWhileRightIsPruned(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	left := writeCLISourceProjectionTDDFile(t, dir, "left.csv", "id,amount,name\n1,100,Alice\n2,bad,Bob\n")
	right := writeCLISourceProjectionTDDFile(t, dir, "orders.csv", "user_id,amount,unused_bad\n1,50,1\n2,60,bad\n")

	rows := readCLIJSONMaps(t, runCLIQuery(t, bin,
		left+` with infer_rows=1 | join `+right+` with infer_rows=1 on id == user_id | sort id | select orders_amount | json`,
	))
	requireCLIJSONColumns(t, rows, "orders_amount")
	if len(rows) != 2 || rows[0]["orders_amount"] != float64(50) || rows[1]["orders_amount"] != float64(60) {
		t.Fatalf("orders_amount rows: got %#v, want 50 and 60", rows)
	}
}

func TestCLIRightJoinDemandPruningTDDRightSourceWideErrorsRemainObservable(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	left := writeCLISourceProjectionTDDFile(t, dir, "users.csv", "id,name\n1,Alice\n")

	t.Run("duplicate_csv_header", func(t *testing.T) {
		right := writeCLISourceProjectionTDDFile(t, dir, "dupe.csv", "user_id,user_id,unused_bad\n1,1,bad\n")
		out := runCLIQueryExpectError(t, bin,
			left+` | join `+right+` on id == user_id | select name | json`,
		)
		requireCLIDemandPruningTDDErrorContains(t, out, "duplicate", "user_id")
	})

	t.Run("csv_row_width", func(t *testing.T) {
		right := writeCLISourceProjectionTDDFile(t, dir, "wide-row.csv", "user_id,amount,unused_bad\n1,10,1\n2,20,2,extra\n")
		out := runCLIQueryExpectError(t, bin,
			left+` | join `+right+` with infer_rows=1 on id == user_id | select name | json`,
		)
		requireCLIDemandPruningTDDErrorContains(t, out, "expected", "field")
	})

	t.Run("malformed_json_array", func(t *testing.T) {
		right := writeCLISourceProjectionTDDFile(t, dir, "malformed.json", `[{"user_id":1,"amount":10}, bad, {"user_id":2}]`)
		out := runCLIQueryExpectError(t, bin,
			left+` | join `+right+` on id == user_id | select name | json`,
		)
		requireCLIDemandPruningTDDErrorContains(t, out, "cannot parse json", "invalid character")
	})
}

func TestCLIRightJoinDemandPruningTDDStaticJoinErrorsAcrossRightFormats(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	left := writeCLISourceProjectionTDDFile(t, dir, "users.csv", "id,name\n1,Alice\n")

	t.Run("missing_right_key", func(t *testing.T) {
		for _, right := range cliRightJoinDemandPruningMissingKeyInputs(t, bin, dir, "missing-key") {
			t.Run(right.name, func(t *testing.T) {
				out := runCLIQueryExpectError(t, bin,
					left+` | join `+right.source+` on id == user_id | select name | json`,
				)
				requireCLIDemandPruningTDDErrorContains(t, out, "right join key", "user_id", "not found")
			})
		}
	})

	t.Run("incompatible_key_schema", func(t *testing.T) {
		for _, right := range cliRightJoinDemandPruningStringKeyInputs(t, bin, dir, "string-key") {
			t.Run(right.name, func(t *testing.T) {
				out := runCLIQueryExpectError(t, bin,
					left+` | join `+right.source+` on id == user_id | select name | json`,
				)
				requireCLIDemandPruningTDDErrorContains(t, out, "join", "key", "type", "id", "user_id")
			})
		}
	})
}

func TestCLIRightJoinDemandPruningTDDDeterministicGlobRightSourceIsPrunable(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	left := writeCLISourceProjectionTDDFile(t, dir, "users.csv", "id,name\n1,Alice\n2,Bob\n")
	writeCLISourceProjectionTDDFile(t, dir, "orders-001.csv", "user_id,amount,unused_bad\n1,10,1\n")
	writeCLISourceProjectionTDDFile(t, dir, "orders-002.csv", "user_id,amount,unused_bad\n2,20,bad\n")
	pattern := filepath.Join(dir, "orders-*.csv")

	rows := readCLIJSONMaps(t, runCLIQuery(t, bin,
		left+` | join `+pattern+` with format=csv, infer_rows=1 on id == user_id | sort name | select name, amount | json`,
	))
	requireCLIJSONColumns(t, rows, "name", "amount")
	if len(rows) != 2 || rows[0]["name"] != "Alice" || rows[1]["name"] != "Bob" {
		t.Fatalf("glob join rows: got %#v, want Alice and Bob", rows)
	}
}

func cliRightJoinDemandPruningAllFormatInputs(t *testing.T, bin, dir, base string, badUnused bool) []cliRightJoinDemandInput {
	t.Helper()
	csvContent := "user_id,amount,status,unused_bad\n1,10,open,1\n2,20,closed,2\n4,40,open,3\n"
	if badUnused {
		csvContent = "user_id,amount,status,unused_bad\n1,10,open,1\n2,20,closed,bad\n4,40,open,3\n"
	}
	csv := writeCLISourceProjectionTDDFile(t, dir, base+".csv", csvContent)
	json := writeCLISourceProjectionTDDFile(t, dir, base+".json", `[{"user_id":1,"amount":10,"status":"open","unused_bad":1},{"user_id":2,"amount":20,"status":"closed","unused_bad":2},{"user_id":4,"amount":40,"status":"open","unused_bad":3}]`)
	jsonl := writeCLISourceProjectionTDDFile(t, dir, base+".jsonl", "{\"user_id\":1,\"amount\":10,\"status\":\"open\",\"unused_bad\":1}\n{\"user_id\":2,\"amount\":20,\"status\":\"closed\",\"unused_bad\":2}\n{\"user_id\":4,\"amount\":40,\"status\":\"open\",\"unused_bad\":3}\n")
	avro := filepath.Join(dir, base+".avro")
	parquet := filepath.Join(dir, base+".parquet")
	runCLIQuery(t, bin, csv+` | avro to `+avro)
	runCLIQuery(t, bin, csv+` | parquet to `+parquet)
	return []cliRightJoinDemandInput{
		{name: "csv", source: csv},
		{name: "json", source: json},
		{name: "jsonl", source: jsonl},
		{name: "avro", source: avro},
		{name: "parquet", source: parquet},
	}
}

func cliRightJoinDemandPruningBadTextInputs(t *testing.T, dir, base string) []cliRightJoinDemandInput {
	t.Helper()
	csvContent := "user_id,amount,unused_bad\n1,10,1\n2,20,bad\n"
	jsonContent := `[{"user_id":1,"amount":10,"unused_bad":1},{"user_id":2,"amount":20,"unused_bad":"bad"}]`
	jsonlContent := "{\"user_id\":1,\"amount\":10,\"unused_bad\":1}\n{\"user_id\":2,\"amount\":20,\"unused_bad\":\"bad\"}\n"
	return []cliRightJoinDemandInput{
		{name: "csv", source: writeCLISourceProjectionTDDFile(t, dir, base+".csv", csvContent)},
		{name: "csv_gzip", source: writeCLIDemandPruningTDDBytes(t, dir, base+".csv.gz", gzipCLIBytes(t, csvContent))},
		{name: "csv_zstd", source: writeCLIDemandPruningTDDBytes(t, dir, base+".csv.zst", zstdCLIBytes(t, csvContent))},
		{name: "csv_deflate", source: writeCLIDemandPruningTDDBytes(t, dir, base+".csv.deflate", deflateCLIBytes(t, csvContent))},
		{name: "json", source: writeCLISourceProjectionTDDFile(t, dir, base+".json", jsonContent)},
		{name: "jsonl", source: writeCLISourceProjectionTDDFile(t, dir, base+".jsonl", jsonlContent)},
	}
}

func cliRightJoinDemandPruningMissingKeyInputs(t *testing.T, bin, dir, base string) []cliRightJoinDemandInput {
	t.Helper()
	csv := writeCLISourceProjectionTDDFile(t, dir, base+".csv", "account_id,amount\n1,10\n")
	json := writeCLISourceProjectionTDDFile(t, dir, base+".json", `[{"account_id":1,"amount":10}]`)
	jsonl := writeCLISourceProjectionTDDFile(t, dir, base+".jsonl", "{\"account_id\":1,\"amount\":10}\n")
	avro := filepath.Join(dir, base+".avro")
	parquet := filepath.Join(dir, base+".parquet")
	runCLIQuery(t, bin, csv+` | avro to `+avro)
	runCLIQuery(t, bin, csv+` | parquet to `+parquet)
	return []cliRightJoinDemandInput{
		{name: "csv", source: csv},
		{name: "json", source: json},
		{name: "jsonl", source: jsonl},
		{name: "avro", source: avro},
		{name: "parquet", source: parquet},
	}
}

func cliRightJoinDemandPruningStringKeyInputs(t *testing.T, bin, dir, base string) []cliRightJoinDemandInput {
	t.Helper()
	csv := writeCLISourceProjectionTDDFile(t, dir, base+".csv", "user_id,amount\n001,10\n")
	json := writeCLISourceProjectionTDDFile(t, dir, base+".json", `[{"user_id":"1","amount":10}]`)
	jsonl := writeCLISourceProjectionTDDFile(t, dir, base+".jsonl", "{\"user_id\":\"1\",\"amount\":10}\n")
	avro := filepath.Join(dir, base+".avro")
	parquet := filepath.Join(dir, base+".parquet")
	runCLIQuery(t, bin, csv+` with infer_rows=0 | avro to `+avro)
	runCLIQuery(t, bin, csv+` with infer_rows=0 | parquet to `+parquet)
	return []cliRightJoinDemandInput{
		{name: "csv", source: csv + " with infer_rows=0"},
		{name: "json", source: json},
		{name: "jsonl", source: jsonl},
		{name: "avro", source: avro},
		{name: "parquet", source: parquet},
	}
}

func cliRightJoinDemandPruningColumnStrings(rows []map[string]any, col string) []string {
	out := make([]string, len(rows))
	for i, row := range rows {
		out[i], _ = row[col].(string)
	}
	return out
}

func cliRightJoinDemandPruningColumnNumbers(rows []map[string]any, col string) []string {
	out := make([]string, len(rows))
	for i, row := range rows {
		if v, ok := row[col].(float64); ok {
			out[i] = strconv.FormatInt(int64(v), 10)
		}
	}
	return out
}
