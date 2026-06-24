package engine

import (
	"fmt"
	"testing"

	"github.com/razeghi71/dq/ast"
	"github.com/razeghi71/dq/parser"
	"github.com/razeghi71/dq/table"
)

var benchmarkResult *table.Table

func benchmarkPipeline(b *testing.B, input *table.Table, pipeline string) {
	benchmarkPipelineWithLoad(b, input, pipeline, nil)
}

func benchmarkPipelineWithLoad(b *testing.B, input *table.Table, pipeline string, load LoadFunc) {
	b.Helper()
	q, err := parser.Parse("bench.csv | " + pipeline)
	if err != nil {
		b.Fatalf("parse benchmark query: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result, err := Execute(q, input, load)
		if err != nil {
			b.Fatalf("execute benchmark query: %v", err)
		}
		benchmarkResult = result
	}
}

func benchmarkJoinRows(rows int) (*table.Table, *table.Table) {
	left := table.NewTable([]string{"id", "name", "city"})
	right := table.NewTable([]string{"id", "amount"})
	for i := 0; i < rows; i++ {
		id := int64(i)
		left.AddRow([]table.Value{
			table.IntVal(id),
			table.StrVal(fmt.Sprintf("user-%06d", i)),
			table.StrVal(fmt.Sprintf("city-%02d", i%50)),
		})
		if i%2 == 0 {
			right.AddRow([]table.Value{
				table.IntVal(id),
				table.FloatVal(float64(i%1000) / 10),
			})
		}
	}
	return left, right
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

func benchmarkDistinctFlatRows(rows int) *table.Table {
	tbl := table.NewTable([]string{"name", "age", "city"})
	for i := 0; i < rows; i++ {
		tbl.AddRow([]table.Value{
			table.StrVal(fmt.Sprintf("user-%06d", i)),
			table.IntVal(int64(i % 100)),
			table.StrVal(fmt.Sprintf("city-%02d", i%20)),
		})
	}
	return tbl
}

func benchmarkGroupRows(rows int) *table.Table {
	tbl := table.NewTable([]string{"name", "age", "city", "amount"})
	for i := 0; i < rows; i++ {
		tbl.AddRow([]table.Value{
			table.StrVal(fmt.Sprintf("user-%06d", i)),
			table.IntVal(int64(i % 100)),
			table.StrVal(fmt.Sprintf("city-%02d", i%50)),
			table.FloatVal(float64(i%1000) / 10),
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

func BenchmarkInterpretedUpperTransform(b *testing.B) {
	benchmarkPipeline(b, benchmarkFlatRows(10000), "transform u = upper(name) | select u | count")
}

func BenchmarkInterpretedRegexFilter(b *testing.B) {
	benchmarkPipeline(b, benchmarkFlatRows(10000), `filter { matches(name, "9$") } | count`)
}

func BenchmarkInterpretedNestedStringTransform(b *testing.B) {
	benchmarkPipeline(b, benchmarkFlatRows(10000), "transform u = upper(trim(name)) | select u | count")
}

func BenchmarkNestedDotPathFilter(b *testing.B) {
	benchmarkPipeline(b, benchmarkNestedRows(10000), "filter { profile.score > 40 } | count")
}

func BenchmarkDistinctTopLevelSingleKey(b *testing.B) {
	benchmarkPipeline(b, benchmarkDistinctFlatRows(10000), "distinct city | count")
}

func BenchmarkDistinctTopLevelMultiKey(b *testing.B) {
	benchmarkPipeline(b, benchmarkDistinctFlatRows(10000), "distinct city, age | count")
}

func BenchmarkDistinctNestedSingleKey(b *testing.B) {
	benchmarkPipeline(b, benchmarkNestedRows(10000), "distinct profile.city | count")
}

func BenchmarkDistinctFullRow(b *testing.B) {
	benchmarkPipeline(b, benchmarkDistinctFlatRows(10000), "distinct | count")
}

func BenchmarkGroupOnly(b *testing.B) {
	benchmarkPipeline(b, benchmarkGroupRows(10000), "group city | count")
}

func BenchmarkGroupReduce(b *testing.B) {
	benchmarkPipeline(b, benchmarkGroupRows(10000), "group city | reduce total = sum(age), avg_amount = avg(amount), n = count() | remove grouped | count")
}

func BenchmarkTransformGroupReducePipeline(b *testing.B) {
	benchmarkPipeline(b, benchmarkGroupRows(10000), `transform bucket = if(age > 50, "high", "low") | group bucket | reduce total = sum(age), n = count() | remove grouped | filter { total > 0 } | count`)
}

func BenchmarkJoinInner(b *testing.B) {
	left, right := benchmarkJoinRows(10000)
	benchmarkPipelineWithLoad(b, left, "join right.csv on id | count", func(filename string, _ ast.LoadOptions) (*table.Table, error) {
		if filename != "right.csv" {
			b.Fatalf("unexpected benchmark join source %q", filename)
		}
		return right, nil
	})
}

func BenchmarkJoinTransformSelectPipeline(b *testing.B) {
	left, right := benchmarkJoinRows(10000)
	benchmarkPipelineWithLoad(b, left, `transform bucket = if(id > 5000, "high", "low") | join right.csv on id | select bucket, amount | filter { amount > 10 } | count`, func(filename string, _ ast.LoadOptions) (*table.Table, error) {
		if filename != "right.csv" {
			b.Fatalf("unexpected benchmark join source %q", filename)
		}
		return right, nil
	})
}
