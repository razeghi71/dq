package loader

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/razeghi71/dq/table"
)

func TestLoadCSVWhitespaceOnlyMultipleLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "blank.csv")
	if err := os.WriteFile(path, []byte("  \n\n  \n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tbl, err := Load(path, Options{})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if tbl.NumRows != 0 || len(tbl.Columns) != 0 {
		t.Fatalf("expected empty table, got %s", tbl.String())
	}
}

func TestLoadCSVWhitespaceOnlyMultipleLinesReader(t *testing.T) {
	tbl, err := LoadReader(strings.NewReader("\ufeff\n  \n\n"), Options{Format: "csv"})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if tbl.NumRows != 0 || len(tbl.Columns) != 0 {
		t.Fatalf("expected empty table, got %s", tbl.String())
	}
}

func TestLoadGlobWhitespaceOnlyLeadingShard(t *testing.T) {
	dir := t.TempDir()
	writeGlobTestFiles(t, dir, map[string]string{
		"a.csv": "  \n\n",
		"b.csv": "id,name\n1,Alice\n",
	})

	tbl, err := Load(filepath.Join(dir, "*.csv"), Options{})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(tbl.Columns) != 2 || tbl.ColIndex("id") < 0 || tbl.ColIndex("name") < 0 {
		t.Fatalf("expected id/name columns only, got %v", tbl.Columns)
	}
	if tbl.NumRows != 1 {
		t.Fatalf("expected 1 row, got %d: %s", tbl.NumRows, tbl.String())
	}
	if tbl.Get(0, "id").Int != 1 || tbl.Get(0, "name").Str != "Alice" {
		t.Errorf("row: got %s", tbl.String())
	}
}

func TestLoadGlobWhitespaceOnlyMiddleShard(t *testing.T) {
	dir := t.TempDir()
	writeGlobTestFiles(t, dir, map[string]string{
		"a.csv": "id,name\n1,Alice\n",
		"b.csv": "\n  \n",
		"c.csv": "2,Bob\n",
	})

	tbl, err := Load(filepath.Join(dir, "*.csv"), Options{})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if tbl.NumRows != 2 {
		t.Fatalf("expected 2 rows, got %d: %s", tbl.NumRows, tbl.String())
	}
}

func TestCSVWhitespaceOnlyRowsSkipped(t *testing.T) {
	cases := []struct {
		name    string
		content string
		header  bool
		wantErr bool
		rows    int
		check   func(t *testing.T, tbl *table.Table)
	}{
		{
			name:    "header_false_leading",
			content: "  \n1,2\n",
			header:  false,
			rows:    1,
			check: func(t *testing.T, tbl *table.Table) {
				if tbl.Get(0, "col1").Int != 1 || tbl.Get(0, "col2").Int != 2 {
					t.Fatalf("row 0: got %s", tbl.String())
				}
			},
		},
		{
			name:    "header_false_middle",
			content: "1,2\n,\n3,4\n",
			header:  false,
			rows:    3,
			check: func(t *testing.T, tbl *table.Table) {
				if !tbl.Get(1, "col1").IsNull() || !tbl.Get(1, "col2").IsNull() {
					t.Fatalf("row 1 should be null,null: %s", tbl.String())
				}
				if tbl.Get(2, "col1").Int != 3 || tbl.Get(2, "col2").Int != 4 {
					t.Fatalf("row 2: got %s", tbl.String())
				}
			},
		},
		{
			name:    "header_false_trailing",
			content: "1,2\n,\n",
			header:  false,
			rows:    2,
			check: func(t *testing.T, tbl *table.Table) {
				if !tbl.Get(1, "col1").IsNull() || !tbl.Get(1, "col2").IsNull() {
					t.Fatalf("row 1 should be null,null: %s", tbl.String())
				}
			},
		},
		{
			name:    "header_true_leading_before_header",
			content: "  \nid,name\n1,Alice\n",
			header:  true,
			rows:    1,
			check: func(t *testing.T, tbl *table.Table) {
				if tbl.ColIndex("id") < 0 || tbl.Get(0, "name").Str != "Alice" {
					t.Fatalf("got %s", tbl.String())
				}
			},
		},
		{
			name:    "header_true_middle",
			content: "id,name\n1,Alice\n,\n2,Bob\n",
			header:  true,
			rows:    3,
			check: func(t *testing.T, tbl *table.Table) {
				if !tbl.Get(1, "id").IsNull() || !tbl.Get(1, "name").IsNull() {
					t.Fatalf("row 1 should be null,null: %s", tbl.String())
				}
				if tbl.Get(2, "name").Str != "Bob" {
					t.Fatalf("row 2: got %s", tbl.String())
				}
			},
		},
		{
			name:    "header_true_trailing",
			content: "id,name\n1,Alice\n,\n",
			header:  true,
			rows:    2,
			check: func(t *testing.T, tbl *table.Table) {
				if !tbl.Get(1, "id").IsNull() || !tbl.Get(1, "name").IsNull() {
					t.Fatalf("row 1 should be null,null: %s", tbl.String())
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := Options{Format: "csv"}
			if !tc.header {
				opts.Header = BoolPtr(false)
			}
			tbl, err := LoadReader(strings.NewReader(tc.content), opts)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			if tbl.NumRows != tc.rows {
				t.Fatalf("expected %d rows, got %d: %s", tc.rows, tbl.NumRows, tbl.String())
			}
			if tc.check != nil {
				tc.check(t, tbl)
			}
		})
	}
}

func TestLoadGlobCSVValidationReportsPhysicalRowAfterLeadingBlanks(t *testing.T) {
	dir := t.TempDir()
	writeGlobTestFiles(t, dir, map[string]string{
		"a.csv": "id,name\n1,Alice\n",
		"b.csv": "  \n  \n2,Bob,extra\n",
	})

	_, err := Load(filepath.Join(dir, "*.csv"), Options{})
	if err == nil {
		t.Fatal("expected validation error for extra column")
	}
	if !strings.Contains(err.Error(), "csv row 3:") {
		t.Fatalf("error should cite physical row 3, got: %v", err)
	}
}

func TestLoadCSVStructuredEmptyFieldsPreserved(t *testing.T) {
	tbl, err := LoadReader(strings.NewReader("a,b\n,\n1,2\n"), Options{Format: "csv"})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if tbl.NumRows != 2 {
		t.Fatalf("expected 2 rows, got %d: %s", tbl.NumRows, tbl.String())
	}
	if !tbl.Get(0, "a").IsNull() || !tbl.Get(0, "b").IsNull() {
		t.Fatalf("row 0 should be null,null: %s", tbl.String())
	}
	if tbl.Get(1, "a").Int != 1 || tbl.Get(1, "b").Int != 2 {
		t.Fatalf("row 1: got %s", tbl.String())
	}
}

func TestLoadCSVQuotedEmptyFieldPreserved(t *testing.T) {
	tbl, err := LoadReader(strings.NewReader("a\n\"\"\n1\n"), Options{Format: "csv"})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if tbl.NumRows != 2 {
		t.Fatalf("expected 2 rows, got %d: %s", tbl.NumRows, tbl.String())
	}
	if !tbl.Get(0, "a").IsNull() {
		t.Fatalf("row 0 should be null: %s", tbl.String())
	}
	if tbl.Get(1, "a").Int != 1 {
		t.Fatalf("row 1: got %s", tbl.String())
	}
}

func TestLoadCSVHeaderlessStructuredEmptyRowEstablishesWidth(t *testing.T) {
	tbl, err := LoadReader(strings.NewReader("  \n,\n1,2\n"), Options{Format: "csv", Header: BoolPtr(false)})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if tbl.NumRows != 2 {
		t.Fatalf("expected 2 rows, got %d: %s", tbl.NumRows, tbl.String())
	}
	if !tbl.Get(0, "col1").IsNull() || !tbl.Get(0, "col2").IsNull() {
		t.Fatalf("row 0 should be null,null: %s", tbl.String())
	}
	if tbl.Get(1, "col1").Int != 1 || tbl.Get(1, "col2").Int != 2 {
		t.Fatalf("row 1: got %s", tbl.String())
	}
}

func TestLoadCSVPhysicalBlankLineAfterSchemaIsJagged(t *testing.T) {
	_, err := LoadReader(strings.NewReader("a,b\n1,2\n  \n"), Options{Format: "csv"})
	if err == nil {
		t.Fatal("expected jagged-row error for delimiter-free blank line after header")
	}
	if !strings.Contains(err.Error(), "allow_jagged_rows=true") {
		t.Fatalf("error should suggest allow_jagged_rows: %v", err)
	}
}

func TestIsPhysicalBlankCSVLine(t *testing.T) {
	cases := []struct {
		record []string
		want   bool
	}{
		{[]string{""}, true},
		{[]string{"  "}, true},
		{[]string{"\ufeff"}, true},
		{[]string{"", ""}, false},
		{[]string{"a", ""}, false},
	}
	for _, tc := range cases {
		if got := isPhysicalBlankCSVLine(tc.record); got != tc.want {
			t.Errorf("isPhysicalBlankCSVLine(%v) = %v, want %v", tc.record, got, tc.want)
		}
	}
}
