package loader

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/razeghi71/dq/table"
)

func writeGlobTestFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestExpandGlobMatches(t *testing.T) {
	dir := t.TempDir()
	writeGlobTestFiles(t, dir, map[string]string{
		"logs/a.csv":      "id\n1\n",
		"logs/b.csv":      "id\n2\n",
		"logs/nested/c.csv": "id\n3\n",
	})

	matches, err := expandGlob(filepath.Join(dir, "logs", "*.csv"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %v", matches)
	}

	matches, err = expandGlob(filepath.Join(dir, "logs", "**", "*.csv"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 3 {
		t.Fatalf("expected 3 recursive matches, got %v", matches)
	}
}

func TestExpandGlobNoMatches(t *testing.T) {
	dir := t.TempDir()
	_, err := expandGlob(filepath.Join(dir, "*.csv"))
	if err == nil || !strings.Contains(err.Error(), "no files matched") {
		t.Fatalf("expected no files matched error, got %v", err)
	}
}

func TestLoadGlobCSV(t *testing.T) {
	dir := t.TempDir()
	writeGlobTestFiles(t, dir, map[string]string{
		"a.csv": "id,name\n1,Alice\n",
		"b.csv": "id,name\n2,Bob\n",
	})

	tbl, err := Load(filepath.Join(dir, "*.csv"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if tbl.NumRows != 2 {
		t.Fatalf("expected 2 rows, got %d", tbl.NumRows)
	}
}

func TestLoadGlobCSVRepeatedHeaderSkipped(t *testing.T) {
	dir := t.TempDir()
	writeGlobTestFiles(t, dir, map[string]string{
		"a.csv": "id,name\n1,Alice\n",
		"b.csv": "id,name\n2,Bob\n",
	})

	tbl, err := Load(filepath.Join(dir, "*.csv"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < tbl.NumRows; i++ {
		if tbl.Get(i, "id").Int == 0 && tbl.Get(i, "name").Str == "name" {
			t.Fatalf("header row leaked into data at row %d", i)
		}
	}
}

func TestLoadGlobCSVUTF8BOMFirstShard(t *testing.T) {
	dir := t.TempDir()
	writeGlobTestFiles(t, dir, map[string]string{
		"a.csv": "\ufeffid,name\n1,Alice\n",
		"b.csv": "id,name\n2,Bob\n",
	})

	tbl, err := Load(filepath.Join(dir, "*.csv"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if tbl.ColIndex("id") < 0 || tbl.ColIndex("name") < 0 {
		t.Fatalf("expected id and name columns, got %v", tbl.Columns)
	}
	if tbl.ColIndex("\ufeffid") >= 0 {
		t.Fatalf("BOM must not appear in column names; got %v", tbl.Columns)
	}
	if len(tbl.Columns) != 2 {
		t.Fatalf("expected 2 columns, got %v", tbl.Columns)
	}
	if tbl.NumRows != 2 {
		t.Fatalf("expected 2 rows, got %d", tbl.NumRows)
	}
	if tbl.Get(0, "id").Int != 1 {
		t.Errorf("row 0 id: want 1, got %v", tbl.Get(0, "id"))
	}
	if tbl.Get(0, "name").Str != "Alice" {
		t.Errorf("row 0 name: want Alice, got %q", tbl.Get(0, "name").Str)
	}
	if tbl.Get(1, "id").Int != 2 {
		t.Errorf("row 1 id: want 2, got %v", tbl.Get(1, "id"))
	}
	if tbl.Get(1, "name").Str != "Bob" {
		t.Errorf("row 1 name: want Bob, got %q", tbl.Get(1, "name").Str)
	}
}

func TestLoadGlobCSVUTF8BOMRepeatedHeaderOnLaterShard(t *testing.T) {
	dir := t.TempDir()
	writeGlobTestFiles(t, dir, map[string]string{
		"a.csv": "id,name\n1,Alice\n",
		"b.csv": "\ufeffid,name\n2,Bob\n",
	})

	tbl, err := Load(filepath.Join(dir, "*.csv"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if tbl.ColIndex("id") < 0 || tbl.ColIndex("name") < 0 {
		t.Fatalf("expected id and name columns, got %v", tbl.Columns)
	}
	if tbl.ColIndex("\ufeffid") >= 0 {
		t.Fatalf("BOM must not appear in column names; got %v", tbl.Columns)
	}
	if tbl.NumRows != 2 {
		t.Fatalf("expected 2 rows, got %d", tbl.NumRows)
	}
	for i := 0; i < tbl.NumRows; i++ {
		if tbl.Get(i, "id").Int == 0 && tbl.Get(i, "name").Str == "name" {
			t.Fatalf("header row leaked into data at row %d", i)
		}
	}
	if tbl.Get(1, "id").Int != 2 {
		t.Errorf("row 1 id: want 2, got %v", tbl.Get(1, "id"))
	}
	if tbl.Get(1, "name").Str != "Bob" {
		t.Errorf("row 1 name: want Bob, got %q", tbl.Get(1, "name").Str)
	}
}

func TestLoadGlobCSVHeaderlessShard(t *testing.T) {
	dir := t.TempDir()
	writeGlobTestFiles(t, dir, map[string]string{
		"a.csv": "id,name\n1,Alice\n",
		"b.csv": "2,Bob\n",
	})

	tbl, err := Load(filepath.Join(dir, "*.csv"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if tbl.NumRows != 2 {
		t.Fatalf("expected 2 rows, got %d", tbl.NumRows)
	}
	if tbl.Get(1, "name").Str != "Bob" {
		t.Errorf("second row: got %v", tbl.Get(1, "name"))
	}
}

func TestLoadGlobCSVExtendedHeaderExplicit(t *testing.T) {
	dir := t.TempDir()
	writeGlobTestFiles(t, dir, map[string]string{
		"a.csv": "id,name\n1,Alice\n",
		"b.csv": "id,name,email\n2,Bob,bob@x.com\n",
	})

	tbl, err := Load(filepath.Join(dir, "*.csv"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if tbl.ColIndex("email") < 0 {
		t.Fatalf("expected email column from extended header, got %v", tbl.Columns)
	}
	if tbl.NumRows != 2 {
		t.Fatalf("expected 2 rows, got %d", tbl.NumRows)
	}
	if tbl.Get(0, "email").Type != table.TypeNull {
		t.Errorf("row 0 email should be null")
	}
	if tbl.Get(1, "email").Str != "bob@x.com" {
		t.Errorf("row 1 email: got %v", tbl.Get(1, "email"))
	}
}

func TestLoadGlobCSVExtendedHeaderPascalCaseLimitation(t *testing.T) {
	dir := t.TempDir()
	writeGlobTestFiles(t, dir, map[string]string{
		"a.csv": "id,name\n1,Alice\n",
		"b.csv": "id,name,Email\n2,Bob,bob@x.com\n",
	})

	tbl, err := Load(filepath.Join(dir, "*.csv"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if tbl.ColIndex("Email") >= 0 || tbl.ColIndex("email") >= 0 {
		t.Fatalf("PascalCase new columns are not detected as headers; got %v", tbl.Columns)
	}
	if tbl.NumRows != 3 {
		t.Fatalf("expected 3 rows (header row read as data), got %d: %s", tbl.NumRows, tbl.String())
	}
}

func TestLoadGlobCSVRenamedColumnsPositional(t *testing.T) {
	dir := t.TempDir()
	writeGlobTestFiles(t, dir, map[string]string{
		"a.csv": "id,name\n1,Alice\n",
		"b.csv": "user_id,full_name\n2,Bob\n",
	})

	tbl, err := Load(filepath.Join(dir, "*.csv"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if tbl.ColIndex("user_id") >= 0 || tbl.ColIndex("full_name") >= 0 {
		t.Fatalf("renamed columns with no anchor overlap are read positionally; got %v", tbl.Columns)
	}
	if tbl.NumRows != 3 {
		t.Fatalf("expected 3 rows, got %d: %s", tbl.NumRows, tbl.String())
	}
	if tbl.Get(1, "id").Str != "user_id" || tbl.Get(1, "name").Str != "full_name" {
		t.Errorf("misclassified header row: got %s", tbl.String())
	}
	if tbl.Get(2, "id").AsString() != "2" || tbl.Get(2, "name").Str != "Bob" {
		t.Errorf("data row: got %s", tbl.String())
	}
}

func TestLoadGlobCSVPositionalExtraColumnsDropped(t *testing.T) {
	dir := t.TempDir()
	writeGlobTestFiles(t, dir, map[string]string{
		"a.csv": "id,name\n1,Alice\n",
		"b.csv": "2,Bob,extra@x.com\n",
	})

	tbl, err := Load(filepath.Join(dir, "*.csv"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(tbl.Columns) != 2 {
		t.Fatalf("expected anchor columns only, got %v", tbl.Columns)
	}
	if tbl.NumRows != 2 {
		t.Fatalf("expected 2 rows, got %d", tbl.NumRows)
	}
	if tbl.Get(1, "id").Int != 2 || tbl.Get(1, "name").Str != "Bob" {
		t.Errorf("positional row: got %s", tbl.String())
	}
}

func TestLoadGlobCSVExtraColumnsUnion(t *testing.T) {
	dir := t.TempDir()
	writeGlobTestFiles(t, dir, map[string]string{
		"a.csv": "id,name\n1,Alice\n",
		"b.csv": "id,email\n2,bob@x.com\n",
	})

	tbl, err := Load(filepath.Join(dir, "*.csv"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if tbl.ColIndex("email") < 0 {
		t.Fatalf("expected email column, got %v", tbl.Columns)
	}
	if tbl.NumRows != 2 {
		t.Fatalf("expected 2 rows, got %d", tbl.NumRows)
	}
	if tbl.Get(0, "email").Type != table.TypeNull {
		t.Errorf("row 0 email should be null")
	}
	if tbl.Get(1, "email").Str != "bob@x.com" {
		t.Errorf("row 1 email: got %v", tbl.Get(1, "email"))
	}
}

func TestLoadGlobCSVColumnReorder(t *testing.T) {
	dir := t.TempDir()
	writeGlobTestFiles(t, dir, map[string]string{
		"a.csv": "id,name\n1,Alice\n",
		"b.csv": "name,id\nBob,2\n",
	})

	tbl, err := Load(filepath.Join(dir, "*.csv"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if tbl.NumRows != 2 {
		t.Fatalf("expected 2 rows, got %d", tbl.NumRows)
	}
	if tbl.Get(0, "id").Int != 1 || tbl.Get(0, "name").Str != "Alice" {
		t.Errorf("row 0: got %s", tbl.String())
	}
	if tbl.Get(1, "id").Int != 2 || tbl.Get(1, "name").Str != "Bob" {
		t.Errorf("row 1: got id=%v name=%v", tbl.Get(1, "id"), tbl.Get(1, "name"))
	}
}

func TestLoadGlobCSVHeaderlessStringShard(t *testing.T) {
	dir := t.TempDir()
	writeGlobTestFiles(t, dir, map[string]string{
		"a.csv": "id,name\n1,Alice\n",
		"b.csv": "Bob,Charlie\n",
	})

	tbl, err := Load(filepath.Join(dir, "*.csv"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if tbl.NumRows != 2 {
		t.Fatalf("expected 2 rows, got %d", tbl.NumRows)
	}
	if tbl.Get(1, "id").Str != "Bob" || tbl.Get(1, "name").Str != "Charlie" {
		t.Errorf("second row: got %s", tbl.String())
	}
}

func TestLoadGlobCSVOverlapFalsePositive(t *testing.T) {
	dir := t.TempDir()
	writeGlobTestFiles(t, dir, map[string]string{
		"a.csv": "id,name\n1,Alice\n",
		"b.csv": "name,Bob\n",
	})

	tbl, err := Load(filepath.Join(dir, "*.csv"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if tbl.ColIndex("Bob") >= 0 {
		t.Fatalf("should not invent columns from data row, got %v", tbl.Columns)
	}
	if tbl.NumRows != 2 {
		t.Fatalf("expected 2 rows, got %d: %s", tbl.NumRows, tbl.String())
	}
	if tbl.Get(1, "id").Str != "name" || tbl.Get(1, "name").Str != "Bob" {
		t.Errorf("misread headerless row: got %s", tbl.String())
	}
}

func TestLoadGlobCSVMixedTypePositionalShard(t *testing.T) {
	dir := t.TempDir()
	writeGlobTestFiles(t, dir, map[string]string{
		"a.csv": "id,name\n1,Alice\n",
		"b.csv": "name,Bob\n2,Charlie\n",
	})

	tbl, err := Load(filepath.Join(dir, "*.csv"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if tbl.NumRows != 3 {
		t.Fatalf("expected 3 rows, got %d: %s", tbl.NumRows, tbl.String())
	}
	if tbl.Get(2, "id").AsString() != "2" || tbl.Get(2, "name").Str != "Charlie" {
		t.Errorf("second positional data row: got %s", tbl.String())
	}
}

func TestLoadGlobCSVUnrelatedStringShard(t *testing.T) {
	dir := t.TempDir()
	writeGlobTestFiles(t, dir, map[string]string{
		"a.csv": "id,name\n1,Alice\n",
		"b.csv": "foo,bar\nx,y\n",
	})

	tbl, err := Load(filepath.Join(dir, "*.csv"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if tbl.NumRows != 3 {
		t.Fatalf("expected 3 rows, got %d", tbl.NumRows)
	}
	if tbl.Get(1, "id").Str != "foo" || tbl.Get(1, "name").Str != "bar" {
		t.Errorf("row 1: got %s", tbl.String())
	}
}

func TestLoadGlobOrder(t *testing.T) {
	dir := t.TempDir()
	writeGlobTestFiles(t, dir, map[string]string{
		"part-010.csv": "id\n2\n",
		"part-002.csv": "id\n1\n",
	})

	tbl, err := Load(filepath.Join(dir, "part-*.csv"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if tbl.NumRows != 2 {
		t.Fatalf("expected 2 rows, got %d", tbl.NumRows)
	}
	if tbl.Get(0, "id").Int != 1 || tbl.Get(1, "id").Int != 2 {
		t.Errorf("expected lexicographic order, got %s", tbl.String())
	}
}

func TestLoadGlobEmptyMiddleShard(t *testing.T) {
	dir := t.TempDir()
	writeGlobTestFiles(t, dir, map[string]string{
		"a.csv": "id,name\n1,Alice\n",
		"b.csv": "",
		"c.csv": "2,Bob\n",
	})

	tbl, err := Load(filepath.Join(dir, "*.csv"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if tbl.NumRows != 2 {
		t.Fatalf("expected 2 rows, got %d", tbl.NumRows)
	}
	if tbl.Get(1, "id").Int != 2 || tbl.Get(1, "name").Str != "Bob" {
		t.Errorf("second row: got %s", tbl.String())
	}
}

func TestLoadGlobEmptyFirstShard(t *testing.T) {
	dir := t.TempDir()
	writeGlobTestFiles(t, dir, map[string]string{
		"a.csv": "",
		"b.csv": "id,name\n2,Bob\n",
	})

	tbl, err := Load(filepath.Join(dir, "*.csv"), Options{})
	if err != nil {
		t.Fatalf("empty first glob shard should not fail load: %v", err)
	}
	if tbl.NumRows != 1 {
		t.Fatalf("expected 1 row, got %d", tbl.NumRows)
	}
	if tbl.Get(0, "id").Int != 2 || tbl.Get(0, "name").Str != "Bob" {
		t.Errorf("row: got %s", tbl.String())
	}
}

func TestLoadGlobEmptyOnlyFile(t *testing.T) {
	dir := t.TempDir()
	writeGlobTestFiles(t, dir, map[string]string{
		"only.csv": "",
	})

	tbl, err := Load(filepath.Join(dir, "*.csv"), Options{})
	if err != nil {
		t.Fatalf("glob of empty-only CSV should load: %v", err)
	}
	if tbl.NumRows != 0 || len(tbl.Columns) != 0 {
		t.Fatalf("expected empty table, got %s", tbl.String())
	}
}

func TestLoadGlobAllEmptyShards(t *testing.T) {
	dir := t.TempDir()
	writeGlobTestFiles(t, dir, map[string]string{
		"a.csv": "",
		"b.csv": "",
	})

	tbl, err := Load(filepath.Join(dir, "*.csv"), Options{})
	if err != nil {
		t.Fatalf("glob of all-empty CSV shards should load: %v", err)
	}
	if tbl.NumRows != 0 || len(tbl.Columns) != 0 {
		t.Fatalf("expected empty table, got %s", tbl.String())
	}
}

func TestLoadGlobMultipleEmptyLeadingShards(t *testing.T) {
	dir := t.TempDir()
	writeGlobTestFiles(t, dir, map[string]string{
		"a.csv": "",
		"b.csv": "",
		"c.csv": "id\n1\n",
	})

	tbl, err := Load(filepath.Join(dir, "*.csv"), Options{})
	if err != nil {
		t.Fatalf("glob with leading empty shards should load: %v", err)
	}
	if tbl.NumRows != 1 || tbl.ColIndex("id") < 0 {
		t.Fatalf("expected one row with id column, got %s", tbl.String())
	}
	if tbl.Get(0, "id").Int != 1 {
		t.Errorf("row: got %s", tbl.String())
	}
}

func TestLoadGlobBOMOnlyFirstShard(t *testing.T) {
	dir := t.TempDir()
	writeGlobTestFiles(t, dir, map[string]string{
		"a.csv": "\ufeff",
		"b.csv": "id,name\n1,Alice\n",
	})

	tbl, err := Load(filepath.Join(dir, "*.csv"), Options{})
	if err != nil {
		t.Fatalf("BOM-only first glob shard should not break load: %v", err)
	}
	if tbl.NumRows != 1 {
		t.Fatalf("expected 1 row, got %d", tbl.NumRows)
	}
	if tbl.ColIndex("id") < 0 || tbl.ColIndex("name") < 0 {
		t.Fatalf("expected id and name columns, got %v", tbl.Columns)
	}
	if tbl.Get(0, "id").Int != 1 || tbl.Get(0, "name").Str != "Alice" {
		t.Errorf("row: got %s", tbl.String())
	}
}

func TestLoadGlobJSON(t *testing.T) {
	dir := t.TempDir()
	writeGlobTestFiles(t, dir, map[string]string{
		"a.json": `[{"id":1,"name":"Alice"}]`,
		"b.json": `[{"id":2,"email":"bob@x.com"}]`,
	})

	tbl, err := Load(filepath.Join(dir, "*.json"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if tbl.ColIndex("email") < 0 || tbl.NumRows != 2 {
		t.Fatalf("unexpected table: %s", tbl.String())
	}
	if tbl.Get(1, "email").Str != "bob@x.com" {
		t.Errorf("row 1 email: got %v", tbl.Get(1, "email"))
	}
}

func TestLoadGlobFormatOverride(t *testing.T) {
	dir := t.TempDir()
	writeGlobTestFiles(t, dir, map[string]string{
		"a.dat": "id\n1\n",
		"b.dat": "id\n2\n",
	})

	tbl, err := Load(filepath.Join(dir, "*.dat"), Options{Format: "csv"})
	if err != nil {
		t.Fatal(err)
	}
	if tbl.NumRows != 2 || tbl.Get(1, "id").Int != 2 {
		t.Fatalf("unexpected table: %s", tbl.String())
	}
}

func TestLoadGlobMixedFormatsError(t *testing.T) {
	dir := t.TempDir()
	writeGlobTestFiles(t, dir, map[string]string{
		"a.csv": "id\n1\n",
		"b.json": "[{\"id\":2}]\n",
	})

	_, err := Load(filepath.Join(dir, "*"), Options{})
	if err == nil || !strings.Contains(err.Error(), "mixed formats") {
		t.Fatalf("expected mixed formats error, got %v", err)
	}
}

func TestLoadGlobFixture(t *testing.T) {
	tbl, err := Load(testdataDir+"/glob/users-*.csv", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if tbl.NumRows != 2 {
		t.Fatalf("expected 2 rows, got %d", tbl.NumRows)
	}
}

func TestLoadLiteralBracketFilename(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data[1].csv")
	if err := os.WriteFile(path, []byte("id\n1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tbl, err := Load(path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if tbl.NumRows != 1 || tbl.Get(0, "id").Int != 1 {
		t.Fatalf("unexpected table: %s", tbl.String())
	}
}

func TestHasGlobMeta(t *testing.T) {
	if HasGlobMeta("data[1].csv") {
		t.Error("bracket-only path should not trigger glob")
	}
	if !HasGlobMeta("logs/*.csv") {
		t.Error("expected glob meta")
	}
}

func TestLoadGlobCSVHeaderFalse(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "part-001.dat"), []byte("1,2\n10,20\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "part-002.dat"), []byte("30,40\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tbl, err := Load(filepath.Join(dir, "part-*.dat"), Options{Format: "csv", Header: BoolPtr(false)})
	if err != nil {
		t.Fatalf("load glob: %v", err)
	}
	if tbl.ColIndex("col1") < 0 || tbl.ColIndex("col2") < 0 {
		t.Fatalf("expected col1/col2, got %v", tbl.Columns)
	}
	if tbl.NumRows != 3 {
		t.Fatalf("expected 3 rows, got %d: %s", tbl.NumRows, tbl.String())
	}
}
