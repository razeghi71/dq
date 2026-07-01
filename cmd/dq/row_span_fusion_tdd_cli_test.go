package main

import (
	"strings"
	"testing"
)

func TestCLIRowSpanFusionTDDSemanticsAcrossFlatFormats(t *testing.T) {
	bin := buildCLI(t)

	for _, input := range cliFlatUserInputFiles() {
		t.Run(input.name, func(t *testing.T) {
			rows := readCLIJSONMaps(t, runCLIQuery(t, bin,
				input.path+` | filter { age >= 30 } | transform label = upper(name), decade = age / 10 | rename decade=age_bucket | select label, age_bucket | sort label | json`,
			))
			if len(rows) == 0 {
				t.Fatalf("rows: got %#v, want at least one age >= 30 row", rows)
			}
			requireCLIJSONColumns(t, rows, "label", "age_bucket")
			for _, row := range rows {
				label, ok := row["label"].(string)
				if !ok || label == "" || label != strings.ToUpper(label) {
					t.Fatalf("row label: got %#v, want uppercase string", row)
				}
			}
		})
	}
}

func TestCLIRowSpanFusionTDDAggregateBarrierCompositionAcrossFlatFormats(t *testing.T) {
	bin := buildCLI(t)

	for _, input := range cliFlatUserInputFiles() {
		t.Run(input.name, func(t *testing.T) {
			rows := readCLIJSONMaps(t, runCLIQuery(t, bin,
				input.path+` | filter { age > 20 } | group city | reduce n = count(), total = sum(age) | transform avg_age = total / n | remove grouped | select city, n, total, avg_age | sort city | json`,
			))
			if len(rows) == 0 {
				t.Fatalf("rows: got %#v, want grouped rows", rows)
			}
			requireCLIJSONColumns(t, rows, "city", "n", "total", "avg_age")
			for _, row := range rows {
				if row["n"] == float64(0) {
					t.Fatalf("group row has zero count: %#v", row)
				}
			}
		})
	}
}

func TestCLIRowSpanFusionTDDNestedRowLocalSuffixAcrossNestedFormats(t *testing.T) {
	bin := buildCLI(t)

	for _, input := range cliNestedUserInputFiles() {
		t.Run(input.name, func(t *testing.T) {
			rows := readCLIJSONMaps(t, runCLIQuery(t, bin,
				input.path+` | filter { address.city is not null } | select name, address.city | transform city_label = upper(address_city) | select name, city_label | sort name | json`,
			))
			if len(rows) == 0 {
				t.Fatalf("rows: got %#v, want nested rows", rows)
			}
			requireCLIJSONColumns(t, rows, "name", "city_label")
		})
	}
}

func TestCLIRowSpanFusionTDDSchemaBoundaryErrorsAcrossFlatFormats(t *testing.T) {
	bin := buildCLI(t)

	for _, input := range cliFlatUserInputFiles() {
		t.Run(input.name+"_remove_hides_column", func(t *testing.T) {
			out := runCLIQueryExpectError(t, bin,
				input.path+` | remove age | filter { age > 30 } | json`,
			)
			requireCLIDemandPruningTDDErrorContains(t, out, "filter", "age", "not found")
		})

		t.Run(input.name+"_rename_hides_original", func(t *testing.T) {
			out := runCLIQueryExpectError(t, bin,
				input.path+` | rename age=years | filter { age > 30 } | json`,
			)
			requireCLIDemandPruningTDDErrorContains(t, out, "filter", "age", "not found")
		})

		t.Run(input.name+"_rename_new_name_still_binds", func(t *testing.T) {
			rows := readCLIJSONMaps(t, runCLIQuery(t, bin,
				input.path+` | rename age=years | filter { years > 30 } | select name, years | json`,
			))
			requireCLIJSONColumns(t, rows, "name", "years")
		})
	}
}

func TestCLIRowSpanFusionTDDRuntimeErrorOrderingAcrossFlatFormats(t *testing.T) {
	bin := buildCLI(t)

	for _, input := range cliDemandPruningTDDInvalidRawInputs(t, bin) {
		t.Run(input.name+"_filter_first_suppresses_error", func(t *testing.T) {
			rows := readCLIJSONMaps(t, runCLIQuery(t, bin,
				input.path+` | filter { false } | transform y = year(raw) | json`,
			))
			if len(rows) != 0 {
				t.Fatalf("rows: got %#v, want no rows and no transform error", rows)
			}
		})

		t.Run(input.name+"_transform_first_errors", func(t *testing.T) {
			out := runCLIQueryExpectError(t, bin,
				input.path+` | transform y = year(raw) | filter { false } | json`,
			)
			requireCLIDemandPruningTDDErrorContains(t, out, "transform", "y", "year")
		})
	}
}

func TestCLIRowSpanFusionTDDTextLateBadRecordVisibility(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()

	inputs := []struct {
		name    string
		file    string
		content []byte
	}{
		{
			name:    "csv",
			file:    "late-bad.csv",
			content: []byte("id,status,unused\n1,active,10\n2,active,bad\n"),
		},
		{
			name:    "csv_gzip",
			file:    "late-bad.csv.gz",
			content: gzipCLIBytes(t, "id,status,unused\n1,active,10\n2,active,bad\n"),
		},
		{
			name:    "json",
			file:    "late-bad.json",
			content: []byte(`[{"id":1,"status":"active","unused":10},{"id":2,"status":"active","unused":"bad"}]`),
		},
		{
			name:    "json_deflate",
			file:    "late-bad.json.deflate",
			content: deflateCLIBytes(t, `[{"id":1,"status":"active","unused":10},{"id":2,"status":"active","unused":"bad"}]`),
		},
		{
			name:    "jsonl",
			file:    "late-bad.jsonl",
			content: []byte("{\"id\":1,\"status\":\"active\",\"unused\":10}\n{\"id\":2,\"status\":\"active\",\"unused\":\"bad\"}\n"),
		},
		{
			name:    "jsonl_zstd",
			file:    "late-bad.jsonl.zst",
			content: zstdCLIBytes(t, "{\"id\":1,\"status\":\"active\",\"unused\":10}\n{\"id\":2,\"status\":\"active\",\"unused\":\"bad\"}\n"),
		},
	}

	for _, input := range inputs {
		t.Run(input.name+"_unreferenced_late_bad_column_skipped", func(t *testing.T) {
			path := writeCLIDemandPruningTDDBytes(t, dir, input.file, input.content)
			rows := readCLIJSONMaps(t, runCLIQuery(t, bin,
				path+` with infer_rows=1 | filter { upper(status) == "ACTIVE" } | transform id2 = id + 1 | select id2 | count | json`,
			))
			requireCLIDemandPruningTDDCount(t, rows, 2)
		})

		t.Run(input.name+"_demanded_late_bad_column_still_errors", func(t *testing.T) {
			path := writeCLIDemandPruningTDDBytes(t, dir, "demanded-"+input.file, input.content)
			out := runCLIQueryExpectError(t, bin,
				path+` with infer_rows=1 | filter { upper(status) == "ACTIVE" } | transform unused2 = unused + 1 | select unused2 | json`,
			)
			requireCLIDemandPruningTDDErrorContains(t, out, "unused", "int", "bad")
		})
	}
}
