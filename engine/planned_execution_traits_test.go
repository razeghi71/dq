package engine

import (
	"testing"

	"github.com/razeghi71/dq/table"
)

func TestPlannedExecutionTraitsClassifyEveryPlannedOp(t *testing.T) {
	schema := table.NewSchema(
		[]string{"id", "name"},
		[]*table.TypeDescriptor{{Kind: table.TypeInt}, {Kind: table.TypeString}},
	)
	rowSpan := mustSourceStreamPlanTDDRowSpan(t,
		plannedSelect{plannedBase: plannedBase{output: schema}},
		plannedRename{plannedBase: plannedBase{output: schema}},
	)

	cases := []struct {
		name       string
		op         plannedOp
		wantClass  plannedExecutionClass
		wantLocal  bool
		wantDrops  bool
		wantParRun bool
	}{
		{name: "head", op: plannedHead{plannedBase: plannedBase{output: schema}}, wantClass: plannedExecutionEarlyStop},
		{name: "tail", op: plannedTail{plannedBase: plannedBase{output: schema}}, wantClass: plannedExecutionMaterializedBoundary},
		{name: "filter", op: plannedFilter{plannedBase: plannedBase{output: schema}}, wantClass: plannedExecutionRowLocal, wantLocal: true, wantDrops: true},
		{name: "transform_empty", op: plannedTransform{plannedBase: plannedBase{output: schema}}, wantClass: plannedExecutionRowLocal, wantLocal: true},
		{name: "transform_assignments", op: plannedTransform{plannedBase: plannedBase{output: schema}, assignments: []plannedTransformAssignment{{name: "id"}}}, wantClass: plannedExecutionRowLocal, wantLocal: true, wantParRun: true},
		{name: "row_span", op: rowSpan, wantClass: plannedExecutionRowSpan},
		{name: "group", op: plannedGroup{plannedBase: plannedBase{output: schema}}, wantClass: plannedExecutionMaterializedBoundary},
		{name: "reduce", op: plannedReduce{plannedBase: plannedBase{output: schema}}, wantClass: plannedExecutionMaterializedBoundary},
		{name: "group_reduce", op: plannedGroupReduce{plannedBase: plannedBase{output: schema}}, wantClass: plannedExecutionMaterializedBoundary},
		{name: "sort", op: plannedSort{plannedBase: plannedBase{output: schema}}, wantClass: plannedExecutionMaterializedBoundary},
		{name: "select", op: plannedSelect{plannedBase: plannedBase{output: schema}}, wantClass: plannedExecutionRowLocal, wantLocal: true},
		{name: "rename", op: plannedRename{plannedBase: plannedBase{output: schema}}, wantClass: plannedExecutionRowLocal, wantLocal: true},
		{name: "remove", op: plannedRemove{plannedBase: plannedBase{output: schema}}, wantClass: plannedExecutionRowLocal, wantLocal: true},
		{name: "distinct", op: plannedDistinct{plannedBase: plannedBase{output: schema}}, wantClass: plannedExecutionMaterializedBoundary},
		{name: "count", op: plannedCount{plannedBase: plannedBase{output: schema}}, wantClass: plannedExecutionStreamingFold},
		{name: "describe", op: plannedDescribe{plannedBase: plannedBase{output: schema}}, wantClass: plannedExecutionStreamingFold},
		{name: "join", op: plannedJoin{plannedBase: plannedBase{output: schema}}, wantClass: plannedExecutionMaterializedBoundary},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			traits := tc.op.executionTraits()
			if traits.class != tc.wantClass {
				t.Fatalf("execution class: got %v, want %v", traits.class, tc.wantClass)
			}
			info, ok := plannedRowLocalInfoForOp(tc.op)
			if ok != tc.wantLocal {
				t.Fatalf("row-local info presence: got %v, want %v", ok, tc.wantLocal)
			}
			if info.dropsRows != tc.wantDrops {
				t.Fatalf("dropsRows: got %v, want %v", info.dropsRows, tc.wantDrops)
			}
			if info.parallelCandidate != tc.wantParRun {
				t.Fatalf("parallelCandidate: got %v, want %v", info.parallelCandidate, tc.wantParRun)
			}
		})
	}
}
