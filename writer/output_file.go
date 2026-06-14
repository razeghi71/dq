package writer

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/razeghi71/dq/ast"
	"github.com/razeghi71/dq/table"
)

type stagedOutputFile struct {
	final string
	temp  string
}

type committedOverwriteOutput struct {
	final  string
	backup string
}

// WriteOutput writes t to one or more files according to spec.
// Callers that want stdout-style output should call Write directly.
func WriteOutput(t *table.Table, spec ast.OutputSpec) error {
	if err := ast.ValidateOutputSpec(spec); err != nil {
		return err
	}
	if spec.Path == "" {
		return fmt.Errorf("output path is empty")
	}
	format, err := ast.CanonicalOutputFormat(spec.Format)
	if err != nil {
		return err
	}
	if format == "" {
		format = "table"
	}
	if spec.Options.SplitRows > 0 {
		return writeSplitOutput(t, format, spec)
	}
	path, err := ast.ResolveSingleOutputPath(format, spec.Path)
	if err != nil {
		return err
	}
	return writeOneOutputFile(path, t, format, spec.Options.Overwrite)
}

func writeSplitOutput(t *table.Table, format string, spec ast.OutputSpec) error {
	if format == "table" {
		return fmt.Errorf("table output does not support split_rows")
	}
	split := spec.Options.SplitRows
	parts := (t.NumRows + split - 1) / split
	if parts == 0 {
		parts = 1
	}
	finalPaths := make([]string, parts)
	for part := 1; part <= parts; part++ {
		path, err := ast.ResolveSplitOutputPath(format, spec.Path, part)
		if err != nil {
			return err
		}
		finalPaths[part-1] = path
	}
	stalePaths, err := staleSplitDirectoryOutputPaths(format, spec.Path, parts)
	if err != nil {
		return err
	}
	if err := preflightOutputPaths(append(finalPaths, stalePaths...), spec.Options.Overwrite); err != nil {
		return err
	}

	types := fixedSplitOutputTypes(format, t)
	staged := make([]stagedOutputFile, 0, parts)
	for part := 1; part <= parts; part++ {
		from := (part - 1) * split
		to := from + split
		shard := t.SliceRows(from, to)
		stage, err := stageOneOutputFile(finalPaths[part-1], shard, format, types)
		if err != nil {
			cleanupStagedOutputs(staged)
			return err
		}
		staged = append(staged, stage)
	}
	if err := commitStagedOutputs(staged, spec.Options.Overwrite, stalePaths); err != nil {
		cleanupStagedOutputs(staged)
		return err
	}
	return nil
}

func writeOneOutputFile(path string, t *table.Table, format string, overwrite bool) error {
	if err := preflightOutputPaths([]string{path}, overwrite); err != nil {
		return err
	}
	stage, err := stageOneOutputFile(path, t, format, nil)
	if err != nil {
		return err
	}
	if err := commitStagedOutputs([]stagedOutputFile{stage}, overwrite, nil); err != nil {
		cleanupStagedOutputs([]stagedOutputFile{stage})
		return err
	}
	return nil
}

func preflightOutputPaths(paths []string, overwrite bool) error {
	if overwrite {
		return nil
	}
	seen := make(map[string]bool, len(paths))
	for _, path := range paths {
		if seen[path] {
			return fmt.Errorf("duplicate output path: %s", path)
		}
		seen[path] = true
		if _, err := os.Lstat(path); err == nil {
			return fmt.Errorf("output file already exists: %s", path)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("check output file: %w", err)
		}
	}
	return nil
}

func fixedSplitOutputTypes(format string, t *table.Table) []*inferredType {
	switch format {
	case "avro", "parquet":
		return inferTableTypes(t)
	default:
		return nil
	}
}

func stageOneOutputFile(path string, t *table.Table, format string, types []*inferredType) (stagedOutputFile, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return stagedOutputFile{}, fmt.Errorf("create output directory: %w", err)
	}
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return stagedOutputFile{}, fmt.Errorf("create temporary output file: %w", err)
	}
	temp := f.Name()
	if err := writeOutputFileContent(f, t, format, types); err != nil {
		_ = f.Close()
		_ = os.Remove(temp)
		return stagedOutputFile{}, err
	}
	if err := f.Chmod(0o644); err != nil {
		_ = f.Close()
		_ = os.Remove(temp)
		return stagedOutputFile{}, fmt.Errorf("set temporary output file permissions: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(temp)
		return stagedOutputFile{}, fmt.Errorf("close temporary output file: %w", err)
	}
	return stagedOutputFile{final: path, temp: temp}, nil
}

func writeOutputFileContent(f *os.File, t *table.Table, format string, types []*inferredType) error {
	if types == nil {
		return Write(f, t, format)
	}
	switch format {
	case "avro":
		return writeAvroWithTypes(f, t, types)
	case "parquet":
		return writeParquetWithTypes(f, t, types)
	default:
		return Write(f, t, format)
	}
}

func staleSplitDirectoryOutputPaths(format, path string, parts int) ([]string, error) {
	if !ast.IsOutputDirectoryPath(path) {
		return nil, nil
	}
	ext, err := ast.OutputExtension(format)
	if err != nil {
		return nil, err
	}
	dir := filepath.Clean(path)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read output directory: %w", err)
	}

	var stale []string
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "output-") || !strings.HasSuffix(name, ext) {
			continue
		}
		partText := strings.TrimSuffix(strings.TrimPrefix(name, "output-"), ext)
		part, err := strconv.Atoi(partText)
		if err != nil || part <= parts {
			continue
		}
		stale = append(stale, filepath.Join(dir, name))
	}
	sort.Strings(stale)
	return stale, nil
}

func commitStagedOutputs(staged []stagedOutputFile, overwrite bool, removePaths []string) error {
	if overwrite {
		return commitOverwriteStagedOutputs(staged, removePaths)
	}
	committed := make([]string, 0, len(staged))
	for _, stage := range staged {
		if err := os.Link(stage.temp, stage.final); err != nil {
			cleanupCommittedOutputs(committed)
			return fmt.Errorf("commit output file %s: %w", stage.final, err)
		}
		committed = append(committed, stage.final)
		if err := os.Remove(stage.temp); err != nil {
			cleanupCommittedOutputs(committed)
			return fmt.Errorf("remove temporary output file %s: %w", stage.temp, err)
		}
	}
	return nil
}

func commitOverwriteStagedOutputs(staged []stagedOutputFile, removePaths []string) error {
	committed := make([]committedOverwriteOutput, 0, len(staged))
	for _, stage := range staged {
		backup, err := backupExistingOutput(stage.final)
		if err != nil {
			rollbackOverwriteOutputs(committed)
			return fmt.Errorf("backup output file %s: %w", stage.final, err)
		}
		if err := os.Rename(stage.temp, stage.final); err != nil {
			restoreOverwriteOutput(committedOverwriteOutput{final: stage.final, backup: backup})
			rollbackOverwriteOutputs(committed)
			return fmt.Errorf("commit output file %s: %w", stage.final, err)
		}
		committed = append(committed, committedOverwriteOutput{final: stage.final, backup: backup})
	}
	for _, path := range removePaths {
		backup, err := backupExistingOutput(path)
		if err != nil {
			rollbackOverwriteOutputs(committed)
			return fmt.Errorf("backup stale output file %s: %w", path, err)
		}
		committed = append(committed, committedOverwriteOutput{final: path, backup: backup})
	}
	discardOverwriteBackups(committed)
	return nil
}

func backupExistingOutput(path string) (string, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("check output file: %w", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("output path is a directory")
	}

	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".bak-*")
	if err != nil {
		return "", fmt.Errorf("create backup output file: %w", err)
	}
	backup := f.Name()
	if err := f.Close(); err != nil {
		_ = os.Remove(backup)
		return "", fmt.Errorf("close backup output file: %w", err)
	}
	if err := os.Remove(backup); err != nil {
		return "", fmt.Errorf("prepare backup output file: %w", err)
	}
	if err := os.Rename(path, backup); err != nil {
		return "", fmt.Errorf("move existing output file to backup: %w", err)
	}
	return backup, nil
}

func rollbackOverwriteOutputs(committed []committedOverwriteOutput) {
	for i := len(committed) - 1; i >= 0; i-- {
		restoreOverwriteOutput(committed[i])
	}
}

func restoreOverwriteOutput(committed committedOverwriteOutput) {
	_ = os.Remove(committed.final)
	if committed.backup != "" {
		_ = os.Rename(committed.backup, committed.final)
	}
}

func discardOverwriteBackups(committed []committedOverwriteOutput) {
	for _, output := range committed {
		if output.backup != "" {
			_ = os.Remove(output.backup)
		}
	}
}

func cleanupCommittedOutputs(paths []string) {
	for _, path := range paths {
		_ = os.Remove(path)
	}
}

func cleanupStagedOutputs(staged []stagedOutputFile) {
	for _, stage := range staged {
		_ = os.Remove(stage.temp)
	}
}
