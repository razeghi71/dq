package loader

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func BenchmarkCSVLoadWideFullVsProjected(b *testing.B) {
	path := writeCSVProjectionBenchmarkFile(b, 32, 2000)

	b.Run("full", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			tbl, err := Load(path, Options{})
			if err != nil {
				b.Fatal(err)
			}
			if len(tbl.Columns) != 32 {
				b.Fatalf("columns: got %d, want 32", len(tbl.Columns))
			}
		}
	})

	b.Run("projected", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			prepared, err := Prepare(path, Options{})
			if err != nil {
				b.Fatal(err)
			}
			tbl, err := prepared.Load([]string{"c00", "c31"})
			if err != nil {
				b.Fatal(err)
			}
			if len(tbl.Columns) != 2 {
				b.Fatalf("columns: got %d, want 2", len(tbl.Columns))
			}
		}
	})
}

func BenchmarkJSONLInferAllFullVsPreparedProjected(b *testing.B) {
	path := writeJSONLProjectionBenchmarkFile(b, 10000)
	opts := Options{InferRows: -1, InferRowsSet: true}

	b.Run("full", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			tbl, err := Load(path, opts)
			if err != nil {
				b.Fatal(err)
			}
			if len(tbl.Columns) != 4 {
				b.Fatalf("columns: got %d, want 4", len(tbl.Columns))
			}
		}
	})

	b.Run("prepared_projected", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			prepared, err := Prepare(path, opts)
			if err != nil {
				b.Fatal(err)
			}
			tbl, err := prepared.Load([]string{"id"})
			if err != nil {
				b.Fatal(err)
			}
			if len(tbl.Columns) != 1 {
				b.Fatalf("columns: got %d, want 1", len(tbl.Columns))
			}
		}
	})
}

func writeCSVProjectionBenchmarkFile(b *testing.B, cols, rows int) string {
	b.Helper()
	var sb strings.Builder
	for c := 0; c < cols; c++ {
		if c > 0 {
			sb.WriteByte(',')
		}
		fmt.Fprintf(&sb, "c%02d", c)
	}
	sb.WriteByte('\n')
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			if c > 0 {
				sb.WriteByte(',')
			}
			sb.WriteString("123")
		}
		sb.WriteByte('\n')
	}
	path := filepath.Join(b.TempDir(), "wide.csv")
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		b.Fatal(err)
	}
	return path
}

func writeJSONLProjectionBenchmarkFile(b *testing.B, rows int) string {
	b.Helper()
	var sb strings.Builder
	for r := 0; r < rows; r++ {
		fmt.Fprintf(&sb, "{\"id\":%d,\"status\":\"active\",\"amount\":%d,\"unused\":\"value-%d\"}\n", r, r*10, r)
	}
	path := filepath.Join(b.TempDir(), "wide.jsonl")
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		b.Fatal(err)
	}
	return path
}
