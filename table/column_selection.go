package table

// ColumnSelection describes a source-column projection as an explicit sum type:
// either all columns, or exactly the selected column names. The zero value is
// AllColumns so existing empty source-load specs keep their read-all behavior.
type ColumnSelection struct {
	selected bool
	names    []string
}

// AllColumns returns a selection that represents the full source schema.
func AllColumns() ColumnSelection {
	return ColumnSelection{}
}

// SelectedColumns returns a selection for exactly the provided column names.
// Passing no names is meaningful: it selects zero data columns.
func SelectedColumns(names ...string) ColumnSelection {
	out := make([]string, len(names))
	copy(out, names)
	return ColumnSelection{selected: true, names: out}
}

// ColumnSelectionFromNames converts the legacy source-planning convention into
// ColumnSelection. A nil list means all columns; a non-nil list means exactly
// those names, including an empty list.
func ColumnSelectionFromNames(names []string) ColumnSelection {
	if names == nil {
		return AllColumns()
	}
	return SelectedColumns(names...)
}

// IsAll reports whether this selection represents the full source schema.
func (s ColumnSelection) IsAll() bool {
	return !s.selected
}

// IsSelected reports whether this selection represents an explicit finite set.
func (s ColumnSelection) IsSelected() bool {
	return s.selected
}

// Names returns a defensive copy of the selected names. For AllColumns it
// returns nil; for SelectedColumns() it returns a non-nil empty slice.
func (s ColumnSelection) Names() []string {
	if !s.selected {
		return nil
	}
	out := make([]string, len(s.names))
	copy(out, s.names)
	return out
}
