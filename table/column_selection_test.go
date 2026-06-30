package table

import "testing"

func TestColumnSelectionAlgebraicCases(t *testing.T) {
	var zero ColumnSelection
	if !zero.IsAll() || zero.IsSelected() || zero.Names() != nil {
		t.Fatalf("zero selection: got all=%v selected=%v names=%#v, want all columns", zero.IsAll(), zero.IsSelected(), zero.Names())
	}

	empty := SelectedColumns()
	if empty.IsAll() || !empty.IsSelected() {
		t.Fatalf("empty selection: got all=%v selected=%v, want selected zero columns", empty.IsAll(), empty.IsSelected())
	}
	if names := empty.Names(); names == nil || len(names) != 0 {
		t.Fatalf("empty selection names: got %#v, want non-nil empty slice", names)
	}

	if fromNil := ColumnSelectionFromNames(nil); !fromNil.IsAll() {
		t.Fatalf("nil names should convert to all columns")
	}
	if fromEmpty := ColumnSelectionFromNames([]string{}); !fromEmpty.IsSelected() || fromEmpty.Names() == nil || len(fromEmpty.Names()) != 0 {
		t.Fatalf("empty names should convert to selected zero columns, got %#v", fromEmpty.Names())
	}
}

func TestColumnSelectionDefensiveCopies(t *testing.T) {
	input := []string{"id", "status"}
	selection := ColumnSelectionFromNames(input)
	input[0] = "mutated"
	if got := selection.Names(); len(got) != 2 || got[0] != "id" || got[1] != "status" {
		t.Fatalf("selection should not observe input mutation, got %#v", got)
	}

	names := selection.Names()
	names[1] = "mutated"
	if got := selection.Names(); len(got) != 2 || got[0] != "id" || got[1] != "status" {
		t.Fatalf("selection should not expose mutable names, got %#v", got)
	}
}
