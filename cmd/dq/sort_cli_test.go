package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCLISortNullsLastInDescendingOrder(t *testing.T) {
	bin := buildCLI(t)
	path := filepath.Join(t.TempDir(), "nullsort.json")
	if err := os.WriteFile(path, []byte(`[{"a":2},{"a":null},{"a":1}]`), 0o644); err != nil {
		t.Fatalf("write null sort fixture: %v", err)
	}

	out := runCLIQuery(t, bin, path+` | sort -a | json`)
	var rows []map[string]any
	if err := json.Unmarshal(out, &rows); err != nil {
		t.Fatalf("invalid JSON output:\n%s", out)
	}
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3:\n%s", len(rows), out)
	}
	if rows[0]["a"] != float64(2) || rows[1]["a"] != float64(1) || rows[2]["a"] != nil {
		t.Fatalf("descending sort should keep null last, got %#v", rows)
	}
}
