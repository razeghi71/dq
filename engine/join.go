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

func execJoin(o *ast.JoinOp, left *table.Table, load LoadFunc) (*table.Table, error) {
	if load == nil {
		return nil, fmt.Errorf("join: loader not configured")
	}
	if o.Filename == "-" {
		return nil, fmt.Errorf("join: stdin is not supported as join source")
	}

	right, err := load(o.Filename, o.Load)
	if err != nil {
		return nil, fmt.Errorf("join: load %q: %w", o.Filename, err)
	}

	leftKeys, rightKeys, err := resolveJoinKeys(o.Keys, left, right)
	if err != nil {
		return nil, err
	}

	outCols, leftKeyOutIdx, rightColMap := buildJoinSchema(left, right, leftKeys, rightKeys, o.Filename)
	outSchemas, useTypedAppend := buildJoinOutputSchemas(left, right, leftKeys, rightKeys, leftKeyOutIdx, rightColMap, len(outCols), o.Kind)
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
		for i := range left.Columns {
			if leftRow >= 0 {
				vals[i] = left.Col(i).Get(leftRow)
			} else {
				vals[i] = table.Null()
			}
		}
		for i := range leftKeys {
			outIdx := leftKeyOutIdx[i]
			switch {
			case leftRow >= 0:
				// Real left columns are already copied above; only synthetic
				// (dot-path) key columns appended past the left schema need filling.
				if outIdx >= len(left.Columns) {
					v, err := resolveColumnPath(leftKeys[i].path, left, leftRow)
					if err != nil {
						return err
					}
					vals[outIdx] = v
				}
			case rightRow >= 0:
				v, err := resolveColumnPath(rightKeys[i].path, right, rightRow)
				if err != nil {
					return err
				}
				vals[outIdx] = v
			default:
				vals[outIdx] = table.Null()
			}
		}
		for outIdx, ri := range rightColMap {
			if rightRow >= 0 {
				vals[outIdx] = right.Col(ri).Get(rightRow)
			} else {
				vals[outIdx] = table.Null()
			}
		}
		if useTypedAppend {
			if err := result.AddRowTyped(vals); err != nil {
				return err
			}
		} else {
			result.AddRow(vals)
		}
		return nil
	}

	keepLeftUnmatched := o.Kind == "left" || o.Kind == "full"
	keepRightUnmatched := o.Kind == "right" || o.Kind == "full"

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

type resolvedJoinKey struct {
	path    []string
	colName string // dot-path flattened to underscores (matches select/group convention)
	typ     *table.TypeDescriptor
}

func resolveJoinKeys(keys []ast.JoinKey, left, right *table.Table) ([]resolvedJoinKey, []resolvedJoinKey, error) {
	leftKeys := make([]resolvedJoinKey, len(keys))
	rightKeys := make([]resolvedJoinKey, len(keys))
	for i, k := range keys {
		lk, err := resolveJoinKeySide(k.Left, left, "left")
		if err != nil {
			return nil, nil, fmt.Errorf("join: %w", err)
		}
		rk, err := resolveJoinKeySide(k.Right, right, "right")
		if err != nil {
			return nil, nil, fmt.Errorf("join: %w", err)
		}
		leftKeys[i] = lk
		rightKeys[i] = rk
	}
	return leftKeys, rightKeys, nil
}

func resolveJoinKeySide(path []string, t *table.Table, side string) (resolvedJoinKey, error) {
	if t.ColIndex(path[0]) < 0 {
		return resolvedJoinKey{}, fmt.Errorf("%s join key column %q not found", side, path[0])
	}
	bound, err := bindColumnPath(t, path, &ast.ColumnExpr{Path: path})
	if err != nil {
		return resolvedJoinKey{}, fmt.Errorf("%s join key %q: %w", side, strings.Join(path, "."), err)
	}
	return resolvedJoinKey{path: path, colName: pathToColumnName(path), typ: bound.typ}, nil
}

// buildJoinSchema computes the output schema. It returns the output column
// names, the output index where each join key is merged (left value, or right
// value for right-only rows), and a map from output index to the source right
// column index for every retained right column.
func buildJoinSchema(left, right *table.Table, leftKeys, rightKeys []resolvedJoinKey, filename string) ([]string, []int, map[int]int) {
	// Only the right-side join key columns are dropped (their values are merged
	// into the left key column). Dot-path keys have no flat column to drop.
	rightKeyDrop := make(map[string]bool)
	for _, rk := range rightKeys {
		if len(rk.path) == 1 {
			rightKeyDrop[rk.path[0]] = true
		}
	}

	prefix := joinBasename(filename)
	outCols := append([]string(nil), left.Columns...)

	leftKeyOutIdx := make([]int, len(leftKeys))
	for i, lk := range leftKeys {
		if len(lk.path) == 1 {
			// Flat key: merge into the real left column (validated to exist).
			leftKeyOutIdx[i] = left.ColIndex(lk.path[0])
			continue
		}
		// Dot-path key: always append a synthetic column, suffixed if the
		// flattened name is taken, so it never aliases an unrelated left
		// column (same convention as select/group).
		name := uniqueColumnName(lk.colName, outCols)
		leftKeyOutIdx[i] = len(outCols)
		outCols = append(outCols, name)
	}

	taken := make(map[string]bool, len(outCols)+len(right.Columns))
	for _, c := range outCols {
		taken[c] = true
	}
	rightColMap := make(map[int]int)
	for i, col := range right.Columns {
		if rightKeyDrop[col] {
			continue
		}
		name := col
		if taken[name] {
			name = uniqueColumnName(prefix+"_"+col, outCols)
		}
		taken[name] = true
		rightColMap[len(outCols)] = i
		outCols = append(outCols, name)
	}
	return outCols, leftKeyOutIdx, rightColMap
}

func buildJoinOutputSchemas(left, right *table.Table, leftKeys, rightKeys []resolvedJoinKey, leftKeyOutIdx []int, rightColMap map[int]int, outLen int, joinKind string) ([]*table.TypeDescriptor, bool) {
	schemas := make([]*table.TypeDescriptor, outLen)
	useTypedAppend := true

	keyOutIdx := make(map[int]bool, len(leftKeyOutIdx))
	for _, outIdx := range leftKeyOutIdx {
		keyOutIdx[outIdx] = true
	}

	for i := range left.Columns {
		schema := left.Col(i).Schema()
		if (joinKind == "right" || joinKind == "full") && !keyOutIdx[i] {
			schema = table.WithNullable(schema)
		}
		schemas[i] = schema
	}

	for i := range leftKeys {
		outIdx := leftKeyOutIdx[i]
		leftSchema := leftKeys[i].typ
		rightSchema := rightKeys[i].typ
		if leftSchema == nil {
			leftSchema = rightSchema
		}
		if rightSchema != nil {
			if merged, err := table.UnifyStrict(leftSchema, rightSchema); err == nil {
				leftSchema = merged
			} else {
				useTypedAppend = false
				if joinKind == "right" || joinKind == "full" {
					// In fallback mode, retained right-only rows supply the merged
					// key value. Do not pre-seed that key with the incompatible
					// left schema, or permissive append will stringify it. Keep the
					// right schema so zero-row outputs still describe the side that
					// would supply retained right-only keys.
					leftSchema = rightSchema
				}
			}
		}
		if outIdx >= 0 && outIdx < len(schemas) {
			schemas[outIdx] = leftSchema
		}
	}

	for outIdx, rightIdx := range rightColMap {
		if outIdx >= 0 && outIdx < len(schemas) {
			schema := right.Col(rightIdx).Schema()
			if joinKind == "left" || joinKind == "full" {
				schema = table.WithNullable(schema)
			}
			schemas[outIdx] = schema
		}
	}

	for _, schema := range schemas {
		if schema == nil {
			useTypedAppend = false
			break
		}
	}
	return schemas, useTypedAppend
}

// joinBasename derives a sanitized column prefix from the join filename:
// extension stripped, camelCase split on lower-to-upper boundaries, other
// characters replaced with underscores ("data/OrderItems.csv" -> "order_items").
// Glob metacharacters are stripped first; if the basename is empty afterward,
// the parent directory name is used ("orders/*.csv" -> "orders").
func joinBasename(filename string) string {
	stripMeta := strings.ContainsAny(filename, "*?{")
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
		v, err := resolveColumnPath(k.path, t, row)
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
