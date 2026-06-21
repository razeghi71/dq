package engine

import (
	"fmt"
	"testing"

	"github.com/razeghi71/dq/parser"
	"github.com/razeghi71/dq/table"
)

var benchmarkResult *table.Table

func benchmarkPipeline(b *testing.B, input *table.Table, pipeline string) {
	b.Helper()
	q, err := parser.Parse("bench.csv | " + pipeline)
	if err != nil {
		b.Fatalf("parse benchmark query: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result, err := Execute(q, input, nil)
		if err != nil {
			b.Fatalf("execute benchmark query: %v", err)
		}
		benchmarkResult = result
	}
}

func benchmarkFlatRows(rows int) *table.Table {
	tbl := table.NewTable([]string{"name", "age"})
	for i := 0; i < rows; i++ {
		tbl.AddRow([]table.Value{
			table.StrVal(fmt.Sprintf("user-%06d", i)),
			table.IntVal(int64(i % 100)),
		})
	}
	return tbl
}

func benchmarkNestedRows(rows int) *table.Table {
	tbl := table.NewTable([]string{"name", "profile"})
	for i := 0; i < rows; i++ {
		tbl.AddRow([]table.Value{
			table.StrVal(fmt.Sprintf("user-%06d", i)),
			table.RecordVal([]table.RecordField{
				{Name: "score", Value: table.IntVal(int64(i % 100))},
				{Name: "city", Value: table.StrVal(fmt.Sprintf("city-%02d", i%20))},
			}),
		})
	}
	return tbl
}

func BenchmarkSimpleIntFilter(b *testing.B) {
	benchmarkPipeline(b, benchmarkFlatRows(10000), "filter { age > 40 } | count")
}

func BenchmarkAppendOnlyTransform(b *testing.B) {
	benchmarkPipeline(b, benchmarkFlatRows(10000), "transform age2 = age + 1 | select age2 | count")
}

func BenchmarkStringPredicateFilter(b *testing.B) {
	benchmarkPipeline(b, benchmarkFlatRows(10000), `filter { str_contains(name, "999") } | count`)
}

func BenchmarkNestedDotPathFilter(b *testing.B) {
	benchmarkPipeline(b, benchmarkNestedRows(10000), "filter { profile.score > 40 } | count")
}
