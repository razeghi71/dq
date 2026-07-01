package engine

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/razeghi71/dq/ast"
	"github.com/razeghi71/dq/table"
)

// LoadFunc loads a table from a filename with per-join load options.
type LoadFunc func(filename string, opts ast.LoadOptions) (*table.Table, error)

type loadFuncJoinSourceProvider struct {
	load LoadFunc
}

func newLoadFuncJoinSourceProvider(load LoadFunc) JoinSourceProvider {
	if load == nil {
		return nil
	}
	return &loadFuncJoinSourceProvider{load: load}
}

func (p *loadFuncJoinSourceProvider) PrepareJoinSource(filename string, opts ast.LoadOptions) (PreparedJoinSource, error) {
	if p == nil || p.load == nil {
		return PreparedJoinSource{}, fmt.Errorf("join source loader not configured")
	}
	tbl, err := p.load(filename, opts)
	if err != nil {
		return PreparedJoinSource{}, err
	}
	return newPreparedJoinSourceFromSnapshot(filename, tbl.Schema(), func(spec JoinSourceLoadSpec) (*table.Table, error) {
		return projectLoadedJoinSourceForSpec(filename, tbl, spec)
	})
}

func projectLoadedJoinSourceForSpec(source string, input *table.Table, spec JoinSourceLoadSpec) (*table.Table, error) {
	if input == nil {
		return nil, fmt.Errorf("join source %q loaded nil table", source)
	}
	outputIdx, outputCols, outputSchemas, err := tableColumnSelection(input, spec.Columns, source)
	if err != nil {
		return nil, err
	}
	return input.SelectColsWithSchema(outputIdx, table.NewSchema(outputCols, outputSchemas))
}

func tableColumnSelection(input *table.Table, selection table.ColumnSelection, source string) ([]int, []string, []*table.TypeDescriptor, error) {
	if selection.IsAll() {
		idx := make([]int, len(input.Columns))
		cols := make([]string, len(input.Columns))
		schemas := make([]*table.TypeDescriptor, len(input.Columns))
		for i, name := range input.Columns {
			idx[i] = i
			cols[i] = name
			schemas[i] = input.Col(i).RawSchema()
			if schemas[i] == nil {
				schemas[i] = input.Col(i).Schema()
			}
		}
		return idx, cols, schemas, nil
	}
	names := selection.Names()
	idx := make([]int, len(names))
	cols := make([]string, len(names))
	schemas := make([]*table.TypeDescriptor, len(names))
	seen := make(map[string]bool, len(names))
	for i, name := range names {
		if seen[name] {
			return nil, nil, nil, fmt.Errorf("join source %q: projected column %q requested more than once", source, name)
		}
		seen[name] = true
		colIdx := input.ColIndex(name)
		if colIdx < 0 {
			return nil, nil, nil, fmt.Errorf("join source %q: projected column %q not found", source, name)
		}
		idx[i] = colIdx
		cols[i] = name
		schemas[i] = input.Col(colIdx).RawSchema()
		if schemas[i] == nil {
			schemas[i] = input.Col(colIdx).Schema()
		}
	}
	return idx, cols, schemas, nil
}

func execPlannedJoin(p plannedJoin, left *table.Table) (*table.Table, error) {
	right, err := p.right.source.Load(p.right.spec)
	if err != nil {
		return nil, fmt.Errorf("join: load %q: %w", p.right.source.Filename(), err)
	}
	if err := validateJoinRightInputSchema(p.right.env, right); err != nil {
		return nil, err
	}
	leftKeys := p.leftKeys
	rightKeys := p.rightKeys
	outputs := p.outputs

	outCols, outSchemas := outputEnvColumns(p.OutputEnv())
	result := table.NewTableWithSchemas(outCols, outSchemas)

	rightIndex, err := buildJoinIndex(right, rightKeys)
	if err != nil {
		return nil, fmt.Errorf("join: %w", err)
	}
	matchedRight := make([]bool, right.NumRows)

	// emit appends one output row built from the given left/right row indices.
	// A negative index means "no row on that side" (filled with nulls).
	emit := func(leftRow, rightRow int) error {
		vals := make([]table.Value, len(outCols))
		for outIdx, output := range outputs {
			switch output.kind {
			case plannedJoinOutputLeft:
				if leftRow >= 0 {
					vals[outIdx] = left.Col(output.leftIndex).Get(leftRow)
				} else {
					vals[outIdx] = table.Null()
				}
			case plannedJoinOutputKey:
				switch {
				case leftRow >= 0:
					v, err := resolveBoundColumn(leftKeys[output.keyIndex].column, left, leftRow)
					if err != nil {
						return err
					}
					vals[outIdx] = v
				case rightRow >= 0:
					v, err := resolveBoundColumn(rightKeys[output.keyIndex].column, right, rightRow)
					if err != nil {
						return err
					}
					vals[outIdx] = v
				default:
					vals[outIdx] = table.Null()
				}
			case plannedJoinOutputRight:
				if rightRow >= 0 {
					vals[outIdx] = right.Col(output.rightIndex).Get(rightRow)
				} else {
					vals[outIdx] = table.Null()
				}
			default:
				return fmt.Errorf("join: output column %d has unknown source kind", outIdx)
			}
		}
		if err := result.AddRowTyped(vals); err != nil {
			return err
		}
		return nil
	}

	keepLeftUnmatched := p.kind == "left" || p.kind == "full"
	keepRightUnmatched := p.kind == "right" || p.kind == "full"

	for li := 0; li < left.NumRows; li++ {
		key, ok, err := joinKeyAt(left, leftKeys, li)
		if err != nil {
			return nil, fmt.Errorf("join: %w", err)
		}
		var matches []int
		if ok {
			matches = rightIndex[key]
		}
		if len(matches) == 0 {
			if keepLeftUnmatched {
				if err := emit(li, -1); err != nil {
					return nil, err
				}
			}
			continue
		}
		for _, ri := range matches {
			if err := emit(li, ri); err != nil {
				return nil, err
			}
			matchedRight[ri] = true
		}
	}

	if keepRightUnmatched {
		for ri := 0; ri < right.NumRows; ri++ {
			if !matchedRight[ri] {
				if err := emit(-1, ri); err != nil {
					return nil, err
				}
			}
		}
	}

	return result, nil
}

func validateJoinRightInputSchema(want schemaEnv, got *table.Table) error {
	if got == nil {
		return fmt.Errorf("join: right input is nil")
	}
	env, err := schemaEnvFromTable(got)
	if err != nil {
		return err
	}
	if len(want.columns) != len(env.columns) {
		return fmt.Errorf("join: right input schema column count mismatch: planned %d columns, got %d", len(want.columns), len(env.columns))
	}
	for i := range want.columns {
		w := want.columns[i]
		gotCol := env.columns[i]
		if w.name != gotCol.name {
			return fmt.Errorf("join: right input schema column %d mismatch: planned %q, got %q", i, w.name, gotCol.name)
		}
		if !table.SchemaAssignable(w.planningSchema(), gotCol.raw, table.AssignExactMode) {
			return fmt.Errorf("join: right input schema for column %q mismatch: planned %s, got %s", w.name, table.Render(w.planningSchema()), table.Render(gotCol.raw))
		}
	}
	return nil
}

type resolvedJoinKey struct {
	colName string // dot-path flattened to underscores (matches select/group convention)
	column  boundColumn
}

func joinKeyComparableSchema(schema *table.TypeDescriptor) *table.TypeDescriptor {
	schema = table.NormalizeSchema(schema)
	if schema == nil {
		return nil
	}
	out := *schema
	out.Nullable = false
	switch out.Kind {
	case table.TypeList:
		out.Elem = joinKeyComparableSchema(out.Elem)
	case table.TypeRecord:
		out.Fields = make([]table.FieldDescriptor, len(schema.Fields))
		for i, field := range schema.Fields {
			out.Fields[i] = table.FieldDescriptor{Name: field.Name, Type: joinKeyComparableSchema(field.Type)}
		}
	case table.TypeUnion:
		out.Branches = make([]*table.TypeDescriptor, len(schema.Branches))
		for i, branch := range schema.Branches {
			out.Branches[i] = joinKeyComparableSchema(branch)
		}
	}
	return table.NormalizeSchema(&out)
}

// joinBasename derives a sanitized column prefix from the join filename:
// extension stripped, camelCase split on lower-to-upper boundaries, other
// characters replaced with underscores ("data/OrderItems.csv" -> "order_items").
// Glob metacharacters are stripped first; if the basename is empty afterward,
// the parent directory name is used ("orders/*.csv" -> "orders").
func joinBasename(filename string) string {
	stripMeta := ast.HasGlobMeta(filename)
	if s := sanitizeJoinBasename(filepath.Base(filename), stripMeta); s != "" {
		return s
	}
	dir := filepath.Dir(filename)
	for dir != "." && dir != string(filepath.Separator) && dir != "" {
		if s := sanitizeJoinBasename(filepath.Base(dir), stripMeta); s != "" {
			return s
		}
		dir = filepath.Dir(dir)
	}
	return "right"
}

func sanitizeJoinBasename(name string, stripMeta bool) string {
	name = strings.TrimSuffix(name, filepath.Ext(name))
	if stripMeta {
		name = stripGlobMeta(name)
	}
	var b strings.Builder
	prevLower := false
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if unicode.IsUpper(r) && prevLower {
				b.WriteByte('_')
			}
			b.WriteRune(unicode.ToLower(r))
			prevLower = unicode.IsLower(r) || unicode.IsDigit(r)
		} else {
			b.WriteByte('_')
			prevLower = false
		}
	}
	return strings.Trim(b.String(), "_")
}

func stripGlobMeta(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '*', '?', '{', '}', '[', ']', '\\':
			continue
		default:
			b.WriteRune(r)
		}
	}
	return strings.Trim(b.String(), "_")
}

// joinKeyAt builds the composite key for a row. ok is false when any key part
// is null (null keys never match). Resolution failures (e.g. dot path through
// a non-record value) are returned as errors, matching the rest of the engine.
func joinKeyAt(t *table.Table, keys []resolvedJoinKey, row int) (string, bool, error) {
	parts := make([]string, len(keys))
	for i, k := range keys {
		v, err := resolveBoundColumn(k.column, t, row)
		if err != nil {
			return "", false, err
		}
		if v.IsNull() {
			return "", false, nil
		}
		parts[i] = table.CanonicalKey(v)
	}
	return strings.Join(parts, "\x00"), true, nil
}

func buildJoinIndex(t *table.Table, keys []resolvedJoinKey) (map[string][]int, error) {
	index := make(map[string][]int)
	for i := 0; i < t.NumRows; i++ {
		key, ok, err := joinKeyAt(t, keys, i)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		index[key] = append(index[key], i)
	}
	return index, nil
}
