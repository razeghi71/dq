package engine

import (
	"fmt"
	"testing"

	"github.com/razeghi71/dq/ast"
	"github.com/razeghi71/dq/parser"
	"github.com/razeghi71/dq/table"
)

func BenchmarkSchemaEnvPlannerNarrowFilterSelect(b *testing.B) {
	benchmarkSchemaEnvPlanner(b, schemaEnvBenchmarkSchema(8), `filter { c006 > 10 } | select c001, c006 | count`, nil)
}

func BenchmarkSchemaEnvPlannerWideFilterSelect(b *testing.B) {
	benchmarkSchemaEnvPlanner(b, schemaEnvBenchmarkSchema(256), `filter { c199 > 10 and c201 < 500 } | select c003, c128, c199, c201 | count`, nil)
}

func BenchmarkSchemaEnvPlannerWideTransformExpressions(b *testing.B) {
	benchmarkSchemaEnvPlanner(b, schemaEnvBenchmarkSchema(256), `transform score = c199 + c201, flag = c199 > c003, ratio = c201 / c005 | select score, flag, ratio | count`, nil)
}

func BenchmarkSchemaEnvPlannerVeryWideRepeatedLookups(b *testing.B) {
	benchmarkSchemaEnvPlanner(b, schemaEnvBenchmarkSchema(1024), `filter { c900 > 10 and c901 < 500 and c902 > 20 and c903 < 800 } | transform a = c900 + c901, b = c902 + c903, c = c904 + c905, d = c906 + c907, e = c908 + c909, f = c910 + c911 | select a, b, c, d, e, f | count`, nil)
}

func BenchmarkSchemaEnvPlannerWideJoin(b *testing.B) {
	right := table.NewTableWithSchemas(
		[]string{"r_key", "r_status", "r_amount", "r_bucket"},
		[]*table.TypeDescriptor{
			schemaEnvBenchmarkType(table.TypeInt),
			schemaEnvBenchmarkType(table.TypeString),
			schemaEnvBenchmarkType(table.TypeFloat),
			schemaEnvBenchmarkType(table.TypeString),
		},
	)
	load := func(filename string, _ ast.LoadOptions) (*table.Table, error) {
		if filename != "right.csv" {
			b.Fatalf("unexpected join source %q", filename)
		}
		return right, nil
	}
	benchmarkSchemaEnvPlanner(b, schemaEnvBenchmarkSchema(256), `filter { c199 > 10 } | join right.csv on c200 == r_key | select c003, r_status, r_amount | count`, load)
}

func BenchmarkSchemaEnvPlannerProofOutputStorage(b *testing.B) {
	cases := []struct {
		name     string
		cols     int
		pipeline string
	}{
		{
			name:     "short_one_op_filter",
			cols:     16,
			pipeline: `filter { c006 > 10 }`,
		},
		{
			name:     "short_select",
			cols:     16,
			pipeline: `select c001, c006`,
		},
		{
			name:     "row_span_filter_select_transform",
			cols:     16,
			pipeline: `filter { c006 > 10 } | select c001, c006 | transform c007 = c006 + 1 | rename c007=score | remove c006`,
		},
		{
			name:     "wide_multi_op",
			cols:     256,
			pipeline: `filter { c199 > 10 and c201 < 500 } | select c003, c128, c199, c201 | transform score = c199 + c201, flag = c199 > c003 | select c128, score, flag | count`,
		},
		{
			name:     "group_reduce_boundary",
			cols:     32,
			pipeline: `group c000 | reduce n = count(), total = sum(c003) | remove grouped | sort c000`,
		},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			benchmarkSchemaEnvPlanner(b, schemaEnvBenchmarkSchema(tc.cols), tc.pipeline, nil)
		})
	}
}

func benchmarkSchemaEnvPlanner(b *testing.B, input table.Schema, pipeline string, load LoadFunc) {
	b.Helper()
	q, err := parser.Parse("bench.csv | " + pipeline)
	if err != nil {
		b.Fatalf("parse benchmark query: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logical, err := planLogicalPipeline(input, q.Ops, load)
		if err != nil {
			b.Fatalf("logical benchmark plan: %v", err)
		}
		var optimized optimizedLogicalPipeline
		if err := optimizeLogicalPipelineInto(logical, &optimized); err != nil {
			b.Fatalf("optimize benchmark plan: %v", err)
		}
		var physical physicalPipeline
		if err := planPhysicalPipelineInto(&optimized, &physical); err != nil {
			b.Fatalf("physical benchmark plan: %v", err)
		}
		benchmarkPhysical = physical
	}
}

func schemaEnvBenchmarkSchema(cols int) table.Schema {
	names := make([]string, cols)
	types := make([]*table.TypeDescriptor, cols)
	for i := range names {
		names[i] = fmt.Sprintf("c%03d", i)
		switch i % 4 {
		case 0:
			types[i] = schemaEnvBenchmarkType(table.TypeString)
		case 1:
			types[i] = schemaEnvBenchmarkType(table.TypeFloat)
		case 2:
			types[i] = schemaEnvBenchmarkType(table.TypeBool)
		default:
			types[i] = schemaEnvBenchmarkType(table.TypeInt)
		}
	}
	if cols > 6 {
		types[6] = schemaEnvBenchmarkType(table.TypeInt)
	}
	if cols > 201 {
		types[3] = schemaEnvBenchmarkType(table.TypeInt)
		types[5] = schemaEnvBenchmarkType(table.TypeFloat)
		types[128] = schemaEnvBenchmarkType(table.TypeString)
		types[199] = schemaEnvBenchmarkType(table.TypeInt)
		types[200] = schemaEnvBenchmarkType(table.TypeInt)
		types[201] = schemaEnvBenchmarkType(table.TypeFloat)
	}
	if cols > 911 {
		for _, idx := range []int{900, 901, 902, 903, 904, 905, 906, 907, 908, 909, 910, 911} {
			types[idx] = schemaEnvBenchmarkType(table.TypeInt)
		}
	}
	return table.NewSchema(names, types)
}

func schemaEnvBenchmarkType(kind table.ValueType) *table.TypeDescriptor {
	return &table.TypeDescriptor{Kind: kind}
}
