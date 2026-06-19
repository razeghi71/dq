package writer

import (
	"strings"
	"testing"

	"github.com/razeghi71/dq/loader"
)

func TestWriterFixedSchemaContractEmptyBinaryOutputPreservesPlannedSchema(t *testing.T) {
	for _, format := range []string{"avro", "parquet"} {
		t.Run(format, func(t *testing.T) {
			out := queryAndWriteBytes(t, testdataDir+"/users.csv", "filter { age > 1000 } | select name, age", format)
			path := writeTempOutput(t, out, "empty."+format)

			tbl, err := loader.Load(path, loader.Options{Format: format})
			if err != nil {
				t.Fatalf("reload empty %s: %v", format, err)
			}
			if tbl.NumRows != 0 {
				t.Fatalf("row count: got %d, want 0", tbl.NumRows)
			}
			if got := strings.Join(tbl.Columns, ","); got != "name,age" {
				t.Fatalf("columns: got %q, want name,age", got)
			}
			if got := tbl.Col(tbl.ColIndex("name")).Schema().String(); got != "string" {
				t.Fatalf("name schema: got %q, want string", got)
			}
			if got := tbl.Col(tbl.ColIndex("age")).Schema().String(); got != "int" {
				t.Fatalf("age schema: got %q, want int", got)
			}
		})
	}
}
