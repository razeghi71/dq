package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIDemandPruningTDDOutputPreservedAcrossFlatFormats(t *testing.T) {
	bin := buildCLI(t)
	wantRows := map[string]int{
		"csv":     3,
		"json":    2,
		"jsonl":   2,
		"avro":    3,
		"parquet": 3,
	}

	for _, input := range cliFlatUserInputFiles() {
		t.Run(input.name, func(t *testing.T) {
			rows := readCLIJSONMaps(t, runCLIQuery(t, bin,
				input.path+` | filter { upper(city) == "NY" } | transform age2 = age + 1, dead = upper(name) | sort -age | select name, age2 | json`,
			))
			if len(rows) != wantRows[input.name] {
				t.Fatalf("rows: got %#v, want %d NY rows", rows, wantRows[input.name])
			}
			requireCLIJSONColumns(t, rows, "name", "age2")
		})
	}
}

func TestCLIDemandPruningTDDUnsupportedFilterSkipsUnreferencedLateBadRows(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	inputs := []struct {
		name    string
		file    string
		content []byte
	}{
		{name: "csv", file: "bad.csv", content: []byte("id,status,unused\n1,active,10\n2,paused,bad\n")},
		{name: "csv_gzip", file: "bad.csv.gz", content: gzipCLIBytes(t, "id,status,unused\n1,active,10\n2,paused,bad\n")},
		{name: "csv_zstd", file: "bad.csv.zst", content: zstdCLIBytes(t, "id,status,unused\n1,active,10\n2,paused,bad\n")},
		{name: "csv_deflate", file: "bad.csv.deflate", content: deflateCLIBytes(t, "id,status,unused\n1,active,10\n2,paused,bad\n")},
		{name: "json", file: "bad.json", content: []byte(`[{"id":1,"status":"active","unused":10},{"id":2,"status":"paused","unused":"bad"}]`)},
		{name: "jsonl", file: "bad.jsonl", content: []byte("{\"id\":1,\"status\":\"active\",\"unused\":10}\n{\"id\":2,\"status\":\"paused\",\"unused\":\"bad\"}\n")},
	}

	for _, input := range inputs {
		t.Run(input.name, func(t *testing.T) {
			path := writeCLIDemandPruningTDDBytes(t, dir, input.file, input.content)
			rows := readCLIJSONMaps(t, runCLIQuery(t, bin,
				path+` with infer_rows=1 | filter { upper(status) == "ACTIVE" } | select id | count | json`,
			))
			requireCLIDemandPruningTDDCount(t, rows, 1)
		})
	}
}

func TestCLIDemandPruningTDDDeadTransformAssignmentsSkipRuntimeErrorsAcrossFormats(t *testing.T) {
	bin := buildCLI(t)

	for _, input := range cliDemandPruningTDDInvalidRawInputs(t, bin) {
		t.Run(input.name, func(t *testing.T) {
			rows := readCLIJSONMaps(t, runCLIQuery(t, bin,
				input.path+` | transform unused = year(raw), kept = id + 1 | select kept | sort kept | json`,
			))
			if len(rows) != 2 {
				t.Fatalf("rows: got %#v, want two kept rows", rows)
			}
			requireCLIJSONColumns(t, rows, "kept")
			if rows[0]["kept"] != float64(2) || rows[1]["kept"] != float64(3) {
				t.Fatalf("kept values: got %#v, want 2 and 3", rows)
			}
		})
	}
}

func TestCLIDemandPruningTDDSortAndDistinctSkipUnusedLateBadRows(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()

	t.Run("sort_keeps_sort_key_but_skips_unused", func(t *testing.T) {
		input := writeCLISourceProjectionTDDFile(t, dir, "sort-bad.csv", "id,created_at,unused\n1,20,10\n2,10,bad\n")
		rows := readCLIJSONMaps(t, runCLIQuery(t, bin,
			input+` with infer_rows=1 | sort created_at | select id | json`,
		))
		if len(rows) != 2 || rows[0]["id"] != float64(2) || rows[1]["id"] != float64(1) {
			t.Fatalf("sorted rows: got %#v, want ids 2,1", rows)
		}
		requireCLIJSONColumns(t, rows, "id")
	})

	t.Run("projected_distinct_keeps_all_keys_but_skips_unused", func(t *testing.T) {
		input := writeCLISourceProjectionTDDFile(t, dir, "distinct-bad.csv", "id,status,unused\n1,10,100\n1,20,bad\n")
		rows := readCLIJSONMaps(t, runCLIQuery(t, bin,
			input+` with infer_rows=1 | distinct id, status | select id | sort id | json`,
		))
		if len(rows) != 2 {
			t.Fatalf("distinct rows: got %#v, want two rows because status is a distinct key", rows)
		}
		requireCLIJSONColumns(t, rows, "id")
	})

}

func TestCLIDemandPruningTDDJoinOutputNamesStayStableWhenLeftCollisionColumnOtherwiseUnused(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	left := writeCLISourceProjectionTDDFile(t, dir, "left.csv", "id,amount,name\n1,100,Alice\n2,200,Bob\n")
	right := writeCLISourceProjectionTDDFile(t, dir, "orders.csv", "user_id,amount\n1,50\n2,60\n")

	t.Run("select_prefixed_right_column", func(t *testing.T) {
		rows := readCLIJSONMaps(t, runCLIQuery(t, bin,
			left+` | join `+right+` on id == user_id | select orders_amount | json`,
		))
		if len(rows) != 2 || rows[0]["orders_amount"] != float64(50) || rows[1]["orders_amount"] != float64(60) {
			t.Fatalf("orders_amount rows: got %#v, want 50 and 60", rows)
		}
		requireCLIJSONColumns(t, rows, "orders_amount")
	})

	t.Run("filter_prefixed_right_column", func(t *testing.T) {
		rows := readCLIJSONMaps(t, runCLIQuery(t, bin,
			left+` | join `+right+` on id == user_id | filter { orders_amount > 50 } | select name | json`,
		))
		if len(rows) != 1 || rows[0]["name"] != "Bob" {
			t.Fatalf("filtered rows: got %#v, want Bob", rows)
		}
		requireCLIJSONColumns(t, rows, "name")
	})

	t.Run("unused_left_collision_column_is_not_materialized", func(t *testing.T) {
		leftBad := writeCLISourceProjectionTDDFile(t, dir, "left-bad.csv", "id,amount,name\n1,100,Alice\n2,bad,Bob\n")
		rows := readCLIJSONMaps(t, runCLIQuery(t, bin,
			leftBad+` with infer_rows=1 | join `+right+` on id == user_id | select orders_amount | json`,
		))
		if len(rows) != 2 || rows[0]["orders_amount"] != float64(50) || rows[1]["orders_amount"] != float64(60) {
			t.Fatalf("orders_amount rows: got %#v, want 50 and 60", rows)
		}
		requireCLIJSONColumns(t, rows, "orders_amount")
	})
}

func TestCLIDemandPruningTDDDemandedColumnsStillSurfaceLateBadRows(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()

	cases := []struct {
		name  string
		data  string
		query string
		want  []string
	}{
		{
			name:  "retained_filter_column",
			data:  "id,status\n1,10\n2,bad\n",
			query: ` with infer_rows=1 | filter { status + 1 > 0 } | select id | count | json`,
			want:  []string{"status", "int", "bad"},
		},
		{
			name:  "sort_key",
			data:  "id,created_at\n1,20\n2,bad\n",
			query: ` with infer_rows=1 | sort created_at | select id | count | json`,
			want:  []string{"created_at", "int", "bad"},
		},
		{
			name:  "distinct_key",
			data:  "id,status\n1,10\n1,bad\n",
			query: ` with infer_rows=1 | distinct id, status | select id | count | json`,
			want:  []string{"status", "int", "bad"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := writeCLISourceProjectionTDDFile(t, dir, tc.name+".csv", tc.data)
			out := runCLIQueryExpectError(t, bin, input+tc.query)
			msg := strings.ToLower(string(out))
			for _, want := range tc.want {
				if !strings.Contains(msg, want) {
					t.Fatalf("demanded bad-record error should mention %q, got:\n%s", want, out)
				}
			}
		})
	}
}

func TestCLIDemandPruningTDDBarriersAndSourceWideErrorsRemainObservable(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()

	t.Run("describe_demands_all_input_columns", func(t *testing.T) {
		input := writeCLISourceProjectionTDDFile(t, dir, "describe-bad.csv", "id,unused\n1,10\n2,bad\n")
		out := runCLIQueryExpectError(t, bin,
			input+` with infer_rows=1 | describe | select column | json`,
		)
		requireCLIDemandPruningTDDErrorContains(t, out, "unused", "int", "bad")
	})

	t.Run("full_row_distinct_demands_all_input_columns", func(t *testing.T) {
		input := writeCLISourceProjectionTDDFile(t, dir, "distinct-full-bad.csv", "id,unused\n1,10\n2,bad\n")
		out := runCLIQueryExpectError(t, bin,
			input+` with infer_rows=1 | distinct | select id | json`,
		)
		requireCLIDemandPruningTDDErrorContains(t, out, "unused", "int", "bad")
	})

	t.Run("duplicate_csv_headers_remain_source_wide", func(t *testing.T) {
		input := writeCLISourceProjectionTDDFile(t, dir, "dupe-header.csv", "id,id,unused\n1,1,bad\n")
		out := runCLIQueryExpectError(t, bin,
			input+` | filter { upper(id) == "1" } | select id | json`,
		)
		requireCLIDemandPruningTDDErrorContains(t, out, "duplicate", "id")
	})

	t.Run("csv_row_width_errors_remain_source_wide", func(t *testing.T) {
		input := writeCLISourceProjectionTDDFile(t, dir, "wide-row.csv", "id,status,unused\n1,active,ok\n2,active,ok,extra\n")
		out := runCLIQueryExpectError(t, bin,
			input+` | filter { upper(status) == "ACTIVE" } | select id | json`,
		)
		requireCLIDemandPruningTDDErrorContains(t, out, "expected", "field")
	})

	t.Run("malformed_json_array_syntax_remains_source_wide", func(t *testing.T) {
		input := writeCLISourceProjectionTDDFile(t, dir, "malformed.json", `[{"id":1,"status":"active"}, bad, {"id":2}]`)
		out := runCLIQueryExpectError(t, bin,
			input+` | filter { upper(status) == "ACTIVE" } | select id | json`,
		)
		requireCLIDemandPruningTDDErrorContains(t, out, "cannot parse json", "invalid character")
	})
}

func TestCLIDemandPruningTDDGlobAndStdinRemainReadAllSourceKinds(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()

	t.Run("glob_csv_still_reads_all_columns", func(t *testing.T) {
		writeCLISourceProjectionTDDFile(t, dir, "part-001.csv", "id,status,unused\n1,active,10\n")
		writeCLISourceProjectionTDDFile(t, dir, "part-002.csv", "id,status,unused\n2,active,bad\n")
		out := runCLIQueryExpectError(t, bin,
			filepath.Join(dir, "part-*.csv")+` with format=csv, infer_rows=1 | filter { upper(status) == "ACTIVE" } | select id | count | json`,
		)
		requireCLIDemandPruningTDDErrorContains(t, out, "unused", "int", "bad")
	})

	t.Run("glob_csv_dead_transform_assignment_is_eliminated", func(t *testing.T) {
		writeCLISourceProjectionTDDFile(t, dir, "dead-001.csv", "id,name,raw\n1,Alice,bad-date\n")
		writeCLISourceProjectionTDDFile(t, dir, "dead-002.csv", "id,name,raw\n2,Bob,also-bad\n")
		rows := readCLIJSONMaps(t, runCLIQuery(t, bin,
			filepath.Join(dir, "dead-*.csv")+` with format=csv | transform unused = year(raw) | select name | count | json`,
		))
		requireCLIDemandPruningTDDCount(t, rows, 2)
	})

	t.Run("stdin_csv_still_reads_all_columns", func(t *testing.T) {
		cmd := exec.Command(bin, `- with format=csv, infer_rows=1 | filter { upper(status) == "ACTIVE" } | select id | count | json`)
		cmd.Stdin = strings.NewReader("id,status,unused\n1,active,10\n2,active,bad\n")
		out, err := cmd.CombinedOutput()
		if err == nil {
			t.Fatalf("expected stdin read-all failure, got:\n%s", out)
		}
		requireCLIDemandPruningTDDErrorContains(t, out, "unused", "int", "bad")
	})
}

func requireCLIDemandPruningTDDCount(t *testing.T, rows []map[string]any, want float64) {
	t.Helper()
	if len(rows) != 1 {
		t.Fatalf("count rows: got %#v, want one row", rows)
	}
	requireCLIJSONColumns(t, rows, "count")
	if rows[0]["count"] != want {
		t.Fatalf("count: got %#v, want %v", rows[0]["count"], want)
	}
}

func requireCLIDemandPruningTDDErrorContains(t *testing.T, out []byte, wants ...string) {
	t.Helper()
	msg := strings.ToLower(string(out))
	for _, want := range wants {
		if !strings.Contains(msg, strings.ToLower(want)) {
			t.Fatalf("error should mention %q, got:\n%s", want, out)
		}
	}
}

func writeCLIDemandPruningTDDBytes(t *testing.T, dir, name string, content []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func cliDemandPruningTDDInvalidRawInputs(t *testing.T, bin string) []struct {
	name string
	path string
} {
	t.Helper()
	dir := t.TempDir()
	csv := writeCLISourceProjectionTDDFile(t, dir, "raw.csv", "id,raw\n1,not-a-date\n2,also-bad\n")
	json := writeCLISourceProjectionTDDFile(t, dir, "raw.json", `[{"id":1,"raw":"not-a-date"},{"id":2,"raw":"also-bad"}]`)
	jsonl := writeCLISourceProjectionTDDFile(t, dir, "raw.jsonl", "{\"id\":1,\"raw\":\"not-a-date\"}\n{\"id\":2,\"raw\":\"also-bad\"}\n")
	avro := filepath.Join(dir, "raw.avro")
	parquet := filepath.Join(dir, "raw.parquet")
	runCLIQuery(t, bin, csv+` | avro to `+avro)
	runCLIQuery(t, bin, csv+` | parquet to `+parquet)
	return []struct {
		name string
		path string
	}{
		{name: "csv", path: csv},
		{name: "json", path: json},
		{name: "jsonl", path: jsonl},
		{name: "avro", path: avro},
		{name: "parquet", path: parquet},
	}
}
