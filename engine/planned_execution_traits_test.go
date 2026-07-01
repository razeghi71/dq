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
		plannedSelect{plannedBase: plannedBaseFromTestSchema(schema)},
		plannedRename{plannedBase: plannedBaseFromTestSchema(schema)},
	)

	cases := []struct {
		name       string
		op         plannedOp
		wantClass  plannedExecutionClass
		wantLocal  bool
		wantDrops  bool
		wantParRun bool
	}{
		{name: "head", op: plannedHead{plannedBase: plannedBaseFromTestSchema(schema)}, wantClass: plannedExecutionEarlyStop},
		{name: "tail", op: plannedTail{plannedBase: plannedBaseFromTestSchema(schema)}, wantClass: plannedExecutionMaterializedBoundary},
		{name: "filter", op: plannedFilter{plannedBase: plannedBaseFromTestSchema(schema)}, wantClass: plannedExecutionRowLocal, wantLocal: true, wantDrops: true},
		{name: "transform_empty", op: plannedTransform{plannedBase: plannedBaseFromTestSchema(schema)}, wantClass: plannedExecutionRowLocal, wantLocal: true},
		{name: "transform_assignments", op: plannedTransform{plannedBase: plannedBaseFromTestSchema(schema), assignments: []plannedTransformAssignment{{name: "id"}}}, wantClass: plannedExecutionRowLocal, wantLocal: true, wantParRun: true},
		{name: "row_span", op: rowSpan, wantClass: plannedExecutionRowSpan},
		{name: "group", op: plannedGroup{plannedBase: plannedBaseFromTestSchema(schema)}, wantClass: plannedExecutionMaterializedBoundary},
		{name: "reduce", op: plannedReduce{plannedBase: plannedBaseFromTestSchema(schema)}, wantClass: plannedExecutionMaterializedBoundary},
		{name: "group_reduce", op: plannedGroupReduce{plannedBase: plannedBaseFromTestSchema(schema)}, wantClass: plannedExecutionMaterializedBoundary},
		{name: "sort", op: plannedSort{plannedBase: plannedBaseFromTestSchema(schema)}, wantClass: plannedExecutionMaterializedBoundary},
		{name: "select", op: plannedSelect{plannedBase: plannedBaseFromTestSchema(schema)}, wantClass: plannedExecutionRowLocal, wantLocal: true},
		{name: "rename", op: plannedRename{plannedBase: plannedBaseFromTestSchema(schema)}, wantClass: plannedExecutionRowLocal, wantLocal: true},
		{name: "remove", op: plannedRemove{plannedBase: plannedBaseFromTestSchema(schema)}, wantClass: plannedExecutionRowLocal, wantLocal: true},
		{name: "distinct", op: plannedDistinct{plannedBase: plannedBaseFromTestSchema(schema)}, wantClass: plannedExecutionMaterializedBoundary},
		{name: "count", op: plannedCount{plannedBase: plannedBaseFromTestSchema(schema)}, wantClass: plannedExecutionStreamingFold},
		{name: "describe", op: plannedDescribe{plannedBase: plannedBaseFromTestSchema(schema)}, wantClass: plannedExecutionStreamingFold},
		{name: "join", op: plannedJoin{plannedBase: plannedBaseFromTestSchema(schema)}, wantClass: plannedExecutionMaterializedBoundary},
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
