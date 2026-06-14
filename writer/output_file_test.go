package writer

import (
	"bytes"
	"encoding/csv"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	goavro "github.com/linkedin/goavro/v2"
	parquet "github.com/parquet-go/parquet-go"
	"github.com/razeghi71/dq/ast"
	"github.com/razeghi71/dq/loader"
	"github.com/razeghi71/dq/table"
)

func outputFileTable(rows int) *table.Table {
	t := table.NewTable([]string{"name"})
	for i := 1; i <= rows; i++ {
		t.AddRow([]table.Value{table.StrVal(string(rune('a' + i - 1)))})
	}
	return t
}

func assertOutputFileAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected %s to be absent, stat err=%v", path, err)
	}
}

func assertOutputPathLstatAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("expected %s to be absent, lstat err=%v", path, err)
	}
}

func assertOutputPathLstatExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("expected %s to exist, lstat err=%v", path, err)
	}
}

func assertOutputFileTempFilesAbsent(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read output dir: %v", err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".tmp") || strings.Contains(entry.Name(), ".partial") {
			t.Fatalf("unexpected temporary output file left behind: %s", filepath.Join(dir, entry.Name()))
		}
	}
}

func assertOutputFileRowCount(t *testing.T, path string, want int) {
	t.Helper()
	tbl, err := loader.Load(path, loader.Options{})
	if err != nil {
		t.Fatalf("load %s: %v", path, err)
	}
	if tbl.NumRows != want {
		t.Fatalf("%s rows = %d, want %d", path, tbl.NumRows, want)
	}
}

func outputFileBinarySchemaTable() *table.Table {
	t := table.NewTable([]string{"mixed", "obj"})
	t.AddRow([]table.Value{
		table.Null(),
		table.RecordVal([]table.RecordField{
			{Name: "x", Value: table.IntVal(1)},
		}),
	})
	t.AddRow([]table.Value{
		table.IntVal(2),
		table.RecordVal([]table.RecordField{
			{Name: "x", Value: table.IntVal(2)},
			{Name: "y", Value: table.StrVal("hi")},
		}),
	})
	t.AddRow([]table.Value{
		table.FloatVal(3.5),
		table.Null(),
	})
	return t
}

func createBrokenOutputSymlink(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	target := filepath.Join(dir, "missing-"+name)
	if err := os.Symlink(target, path); err != nil {
		t.Skipf("symlinks are not available: %v", err)
	}
	return path
}

func outputFileRecordField(v table.Value, name string) (table.Value, bool) {
	if v.Type != table.TypeRecord {
		return table.Null(), false
	}
	for _, field := range v.Fields {
		if field.Name == name {
			return field.Value, true
		}
	}
	return table.Null(), false
}

func outputFileBinarySchema(t *testing.T, path, format string) string {
	t.Helper()
	switch format {
	case "avro":
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read Avro file: %v", err)
		}
		reader, err := goavro.NewOCFReader(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("read Avro OCF: %v", err)
		}
		return reader.Codec().Schema()
	case "parquet":
		f, err := os.Open(path)
		if err != nil {
			t.Fatalf("open Parquet file: %v", err)
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil {
			t.Fatalf("stat Parquet file: %v", err)
		}
		pf, err := parquet.OpenFile(f, info.Size())
		if err != nil {
			t.Fatalf("open Parquet file: %v", err)
		}
		return pf.Schema().String()
	default:
		t.Fatalf("unsupported binary format %q", format)
		return ""
	}
}

func TestWriteOutputPathExtensionValidation(t *testing.T) {
	tbl := outputFileTable(1)
	dir := t.TempDir()

	cases := []struct {
		name   string
		format string
		path   string
	}{
		{"csv_uppercase", "csv", filepath.Join(dir, "out.CSV")},
		{"json_uppercase", "json", filepath.Join(dir, "out.JSON")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := WriteOutput(tbl, ast.OutputSpec{Format: tc.format, Path: tc.path}); err != nil {
				t.Fatalf("WriteOutput: %v", err)
			}
			if _, err := os.Stat(tc.path); err != nil {
				t.Fatalf("expected output at original path %s: %v", tc.path, err)
			}
		})
	}

	err := WriteOutput(tbl, ast.OutputSpec{
		Format: "csv",
		Path:   filepath.Join(dir, "mismatch.JSON"),
	})
	if err == nil {
		t.Fatal("expected extension mismatch error")
	}
	if !strings.Contains(err.Error(), "does not match format") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWriteOutputDirectoryIsTrailingSeparatorOnly(t *testing.T) {
	tbl := outputFileTable(1)
	dir := t.TempDir()
	existingDir := filepath.Join(dir, "out")
	if err := os.Mkdir(existingDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := WriteOutput(tbl, ast.OutputSpec{Format: "csv", Path: existingDir}); err != nil {
		t.Fatalf("WriteOutput: %v", err)
	}
	if _, err := os.Stat(existingDir + ".csv"); err != nil {
		t.Fatalf("expected extension-appended file beside existing directory: %v", err)
	}
	assertOutputFileAbsent(t, filepath.Join(existingDir, "output.csv"))
}

func TestWriteOutputSplitRowsGreaterThanRowCountWritesOnePart(t *testing.T) {
	dir := t.TempDir()
	err := WriteOutput(outputFileTable(2), ast.OutputSpec{
		Format: "csv",
		Path:   dir + string(os.PathSeparator),
		Options: ast.OutputOptions{
			SplitRows: 10,
		},
	})
	if err != nil {
		t.Fatalf("WriteOutput: %v", err)
	}
	assertOutputFileRowCount(t, filepath.Join(dir, "output-1.csv"), 2)
	assertOutputFileAbsent(t, filepath.Join(dir, "output-2.csv"))
}

func TestWriteOutputRejectsInvalidSplitSpecDirectly(t *testing.T) {
	cases := []struct {
		name string
		spec ast.OutputSpec
		want string
	}{
		{
			name: "negative_split_rows",
			spec: ast.OutputSpec{
				Format: "csv",
				Path:   filepath.Join(t.TempDir(), "out/"),
				Options: ast.OutputOptions{
					SplitRows: -1,
				},
			},
			want: "greater than 0",
		},
		{
			name: "overwrite_without_path",
			spec: ast.OutputSpec{
				Format: "csv",
				Options: ast.OutputOptions{
					Overwrite: true,
				},
			},
			want: "requires to path",
		},
		{
			name: "split_file_without_template",
			spec: ast.OutputSpec{
				Format: "csv",
				Path:   filepath.Join(t.TempDir(), "out.csv"),
				Options: ast.OutputOptions{
					SplitRows: 2,
					Overwrite: true,
				},
			},
			want: "{n}",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := WriteOutput(outputFileTable(3), tc.spec)
			if err == nil {
				t.Fatal("expected invalid output spec error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.want)
			}
		})
	}
}

func TestCommitStagedOutputsCleansCurrentFinalWhenTempRemovalFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod-based directory removal failure is Unix-specific")
	}
	if os.Geteuid() == 0 {
		t.Skip("root can remove files from non-writable directories")
	}
	srcDir := filepath.Join(t.TempDir(), "src")
	if err := os.Mkdir(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	temp := filepath.Join(srcDir, "temp.csv")
	if err := os.WriteFile(temp, []byte("name\na\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(srcDir, 0o555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(srcDir, 0o755)

	final := filepath.Join(t.TempDir(), "out.csv")
	err := commitStagedOutputs([]stagedOutputFile{{final: final, temp: temp}}, false, nil)
	if err == nil {
		t.Fatal("expected temp removal failure")
	}
	if !strings.Contains(err.Error(), "remove temporary output file") {
		t.Fatalf("unexpected error: %v", err)
	}
	assertOutputFileAbsent(t, final)
}

func TestWriteOutputSplitExistingPartFailsWithoutPartials(t *testing.T) {
	dir := t.TempDir()
	preexisting := filepath.Join(dir, "output-2.csv")
	if err := os.WriteFile(preexisting, []byte("preexisting\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := WriteOutput(outputFileTable(4), ast.OutputSpec{
		Format: "csv",
		Path:   dir + string(os.PathSeparator),
		Options: ast.OutputOptions{
			SplitRows: 2,
		},
	})
	if err == nil {
		t.Fatal("expected existing output part error")
	}
	assertOutputFileAbsent(t, filepath.Join(dir, "output-1.csv"))
	assertOutputFileTempFilesAbsent(t, dir)
	got, err := os.ReadFile(preexisting)
	if err != nil {
		t.Fatalf("read preexisting part: %v", err)
	}
	if string(got) != "preexisting\n" {
		t.Fatalf("preexisting part changed: %q", got)
	}
}

func TestWriteOutputSplitDirectoryStalePartFailsWithoutOverwrite(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, "output-3.csv")
	if err := os.WriteFile(stale, []byte("stale\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := WriteOutput(outputFileTable(3), ast.OutputSpec{
		Format: "csv",
		Path:   dir + string(os.PathSeparator),
		Options: ast.OutputOptions{
			SplitRows: 2,
		},
	})
	if err == nil {
		t.Fatal("expected stale split part to fail without overwrite")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("unexpected error: %v", err)
	}
	assertOutputFileAbsent(t, filepath.Join(dir, "output-1.csv"))
	assertOutputFileAbsent(t, filepath.Join(dir, "output-2.csv"))
	got, readErr := os.ReadFile(stale)
	if readErr != nil {
		t.Fatalf("stale file should remain: %v", readErr)
	}
	if string(got) != "stale\n" {
		t.Fatalf("stale file changed: %q", got)
	}
	assertOutputFileTempFilesAbsent(t, dir)
}

func TestWriteOutputSplitDirectoryBrokenSymlinkStalePartFailsWithoutOverwrite(t *testing.T) {
	dir := t.TempDir()
	stale := createBrokenOutputSymlink(t, dir, "output-2.csv")

	err := WriteOutput(outputFileTable(1), ast.OutputSpec{
		Format: "csv",
		Path:   dir + string(os.PathSeparator),
		Options: ast.OutputOptions{
			SplitRows: 10,
		},
	})
	if err == nil {
		t.Fatal("expected stale broken symlink to fail without overwrite")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("unexpected error: %v", err)
	}
	assertOutputFileAbsent(t, filepath.Join(dir, "output-1.csv"))
	assertOutputPathLstatExists(t, stale)
	assertOutputFileTempFilesAbsent(t, dir)
}

func TestWriteOutputSplitDirectoryOverwriteRemovesStaleParts(t *testing.T) {
	dir := t.TempDir()
	first := ast.OutputSpec{
		Format: "csv",
		Path:   dir + string(os.PathSeparator),
		Options: ast.OutputOptions{
			SplitRows: 2,
		},
	}
	if err := WriteOutput(outputFileTable(5), first); err != nil {
		t.Fatalf("first WriteOutput: %v", err)
	}
	assertOutputFileRowCount(t, filepath.Join(dir, "output-3.csv"), 1)

	second := first
	second.Options.Overwrite = true
	if err := WriteOutput(outputFileTable(3), second); err != nil {
		t.Fatalf("second WriteOutput: %v", err)
	}
	assertOutputFileRowCount(t, filepath.Join(dir, "output-1.csv"), 2)
	assertOutputFileRowCount(t, filepath.Join(dir, "output-2.csv"), 1)
	assertOutputFileAbsent(t, filepath.Join(dir, "output-3.csv"))
	assertOutputFileTempFilesAbsent(t, dir)
}

func TestWriteOutputSplitDirectoryOverwriteRemovesBrokenSymlinkStalePart(t *testing.T) {
	dir := t.TempDir()
	stale := createBrokenOutputSymlink(t, dir, "output-2.csv")

	err := WriteOutput(outputFileTable(1), ast.OutputSpec{
		Format: "csv",
		Path:   dir + string(os.PathSeparator),
		Options: ast.OutputOptions{
			SplitRows: 10,
			Overwrite: true,
		},
	})
	if err != nil {
		t.Fatalf("WriteOutput: %v", err)
	}
	assertOutputFileRowCount(t, filepath.Join(dir, "output-1.csv"), 1)
	assertOutputPathLstatAbsent(t, stale)
	assertOutputFileTempFilesAbsent(t, dir)
}

func TestWriteOutputSingleWriteFailureCleansTempAndAllowsRetry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.avro")
	bad := table.NewTable([]string{"bad name"})
	bad.AddRow([]table.Value{table.StrVal("Alice")})

	err := WriteOutput(bad, ast.OutputSpec{Format: "avro", Path: path})
	if err == nil {
		t.Fatal("expected invalid Avro field name error")
	}
	assertOutputFileAbsent(t, path)
	assertOutputFileTempFilesAbsent(t, dir)

	if err := WriteOutput(outputFileTable(1), ast.OutputSpec{Format: "avro", Path: path}); err != nil {
		t.Fatalf("retry after failed write should succeed: %v", err)
	}
	assertOutputFileRowCount(t, path, 1)
}

func TestWriteOutputSplitEmptyResultWritesValidBinaryPart(t *testing.T) {
	formats := []string{"avro", "parquet"}
	for _, format := range formats {
		t.Run(format, func(t *testing.T) {
			dir := t.TempDir()
			err := WriteOutput(table.NewTable([]string{"name"}), ast.OutputSpec{
				Format: format,
				Path:   dir + string(os.PathSeparator),
				Options: ast.OutputOptions{
					SplitRows: 2,
				},
			})
			if err != nil {
				t.Fatalf("WriteOutput: %v", err)
			}
			assertOutputFileRowCount(t, filepath.Join(dir, "output-1."+format), 0)
			assertOutputFileAbsent(t, filepath.Join(dir, "output-2."+format))
		})
	}
}

func TestWriteOutputSplitBinaryUsesFullTableSchema(t *testing.T) {
	for _, format := range []string{"avro", "parquet"} {
		t.Run(format, func(t *testing.T) {
			dir := t.TempDir()
			err := WriteOutput(outputFileBinarySchemaTable(), ast.OutputSpec{
				Format: format,
				Path:   dir + string(os.PathSeparator),
				Options: ast.OutputOptions{
					SplitRows: 1,
				},
			})
			if err != nil {
				t.Fatalf("WriteOutput: %v", err)
			}

			part1Path := filepath.Join(dir, "output-1."+format)
			part2Path := filepath.Join(dir, "output-2."+format)
			part3Path := filepath.Join(dir, "output-3."+format)
			schema1 := outputFileBinarySchema(t, part1Path, format)
			schema2 := outputFileBinarySchema(t, part2Path, format)
			schema3 := outputFileBinarySchema(t, part3Path, format)
			if schema2 != schema1 {
				t.Fatalf("part 2 schema differs from part 1\npart1:\n%s\npart2:\n%s", schema1, schema2)
			}
			if schema3 != schema1 {
				t.Fatalf("part 3 schema differs from part 1\npart1:\n%s\npart3:\n%s", schema1, schema3)
			}

			part1, err := loader.Load(part1Path, loader.Options{Format: format})
			if err != nil {
				t.Fatalf("load part 1: %v", err)
			}
			if got := part1.Get(0, "mixed"); got.Type != table.TypeNull {
				t.Fatalf("part 1 mixed: want null, got %v", got)
			}
			y, ok := outputFileRecordField(part1.Get(0, "obj"), "y")
			if !ok {
				t.Fatalf("part 1 obj should include later field y in schema: %v", part1.Get(0, "obj"))
			}
			if y.Type != table.TypeNull {
				t.Fatalf("part 1 obj.y: want null, got %v", y)
			}

			part2, err := loader.Load(part2Path, loader.Options{Format: format})
			if err != nil {
				t.Fatalf("load part 2: %v", err)
			}
			if got := part2.Get(0, "mixed"); got.Type != table.TypeInt && got.Type != table.TypeFloat {
				t.Fatalf("part 2 mixed: want numeric value, got %v", got)
			}
			y, ok = outputFileRecordField(part2.Get(0, "obj"), "y")
			if !ok || y.Type != table.TypeString || y.Str != "hi" {
				t.Fatalf("part 2 obj.y: want hi, got %v (ok=%v)", y, ok)
			}

			part3, err := loader.Load(part3Path, loader.Options{Format: format})
			if err != nil {
				t.Fatalf("load part 3: %v", err)
			}
			if got := part3.Get(0, "mixed"); got.Type != table.TypeFloat || got.Float != 3.5 {
				t.Fatalf("part 3 mixed: want float 3.5, got %v", got)
			}
			if got := part3.Get(0, "obj"); got.Type != table.TypeNull {
				t.Fatalf("part 3 obj: want null, got %v", got)
			}
		})
	}
}

func TestWriteOutputSplitMultipleTemplateMarkers(t *testing.T) {
	dir := t.TempDir()
	template := filepath.Join(dir, "p-{n}-{n}.csv")
	err := WriteOutput(outputFileTable(3), ast.OutputSpec{
		Format: "csv",
		Path:   template,
		Options: ast.OutputOptions{
			SplitRows: 2,
		},
	})
	if err != nil {
		t.Fatalf("WriteOutput: %v", err)
	}
	assertOutputFileRowCount(t, filepath.Join(dir, "p-1-1.csv"), 2)
	assertOutputFileRowCount(t, filepath.Join(dir, "p-2-2.csv"), 1)
}

func TestWriteOutputSplitOverwriteExistingParts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "output-1.csv")
	if err := os.WriteFile(path, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := WriteOutput(outputFileTable(1), ast.OutputSpec{
		Format: "csv",
		Path:   dir + string(os.PathSeparator),
		Options: ast.OutputOptions{
			SplitRows: 1,
			Overwrite: true,
		},
	})
	if err != nil {
		t.Fatalf("WriteOutput: %v", err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	records, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatalf("read csv: %v", err)
	}
	if len(records) != 2 || records[1][0] != "a" {
		t.Fatalf("unexpected overwritten CSV records: %#v", records)
	}
}

func TestWriteOutputSplitOverwriteRollbackRestoresOriginals(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "output-1.csv")
	second := filepath.Join(dir, "output-2.csv")
	if err := os.WriteFile(first, []byte("original-1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(second, 0o755); err != nil {
		t.Fatal(err)
	}

	err := WriteOutput(outputFileTable(4), ast.OutputSpec{
		Format: "csv",
		Path:   dir + string(os.PathSeparator),
		Options: ast.OutputOptions{
			SplitRows: 2,
			Overwrite: true,
		},
	})
	if err == nil {
		t.Fatal("expected overwrite split commit to fail on directory output path")
	}
	got, readErr := os.ReadFile(first)
	if readErr != nil {
		t.Fatalf("original first part should be restored: %v", readErr)
	}
	if string(got) != "original-1\n" {
		t.Fatalf("original first part was not restored, got %q", got)
	}
	info, statErr := os.Stat(second)
	if statErr != nil {
		t.Fatalf("second path should still exist: %v", statErr)
	}
	if !info.IsDir() {
		t.Fatalf("second path should remain a directory")
	}
	assertOutputFileTempFilesAbsent(t, dir)
}

func TestWriteOutputFilePermissions(t *testing.T) {
	t.Run("single_file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "out.csv")
		if err := WriteOutput(outputFileTable(1), ast.OutputSpec{Format: "csv", Path: path}); err != nil {
			t.Fatalf("WriteOutput: %v", err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o644 {
			t.Fatalf("mode = %o, want 644", got)
		}
	})

	t.Run("split_file", func(t *testing.T) {
		dir := t.TempDir()
		if err := WriteOutput(outputFileTable(1), ast.OutputSpec{
			Format: "csv",
			Path:   dir + string(os.PathSeparator),
			Options: ast.OutputOptions{
				SplitRows: 1,
			},
		}); err != nil {
			t.Fatalf("WriteOutput: %v", err)
		}
		info, err := os.Stat(filepath.Join(dir, "output-1.csv"))
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o644 {
			t.Fatalf("mode = %o, want 644", got)
		}
	})
}
