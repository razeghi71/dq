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

func planJoin(o *ast.JoinOp, left schemaEnv, load LoadFunc) (plannedJoin, error) {
	if load == nil {
		return plannedJoin{}, fmt.Errorf("join: loader not configured")
	}
	if o.Filename == "-" {
		return plannedJoin{}, fmt.Errorf("join: stdin is not supported as join source")
	}

	right, err := load(o.Filename, o.Load)
	if err != nil {
		return plannedJoin{}, fmt.Errorf("join: load %q: %w", o.Filename, err)
	}

	rightEnv := schemaEnvFromTable(right)
	leftKeys, rightKeys, err := resolveJoinKeys(o.Keys, left, rightEnv)
	if err != nil {
		return plannedJoin{}, err
	}

	outCols, leftKeyOutIdx, rightColMap := buildJoinSchema(left.columns, rightEnv.columns, leftKeys, rightKeys, o.Filename)
	outSchemas, err := buildJoinOutputSchemas(left, rightEnv, leftKeys, rightKeys, leftKeyOutIdx, rightColMap, len(outCols), o.Kind)
	if err != nil {
		return plannedJoin{}, err
	}

	return plannedJoin{
		plannedBase:   plannedBase{output: rawSchemaFromColumns(outCols, outSchemas)},
		kind:          o.Kind,
		right:         right,
		leftKeys:      leftKeys,
		rightKeys:     rightKeys,
		leftKeyOutIdx: leftKeyOutIdx,
		rightColMap:   rightColMap,
	}, nil
}

func execPlannedJoin(p plannedJoin, left *table.Table) (*table.Table, error) {
	right := p.right
	leftKeys := p.leftKeys
	rightKeys := p.rightKeys
	leftKeyOutIdx := p.leftKeyOutIdx
	rightColMap := p.rightColMap

	outCols, outSchemas := outputSchemaColumns(p.OutputSchema())
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
					v, err := resolveBoundColumn(leftKeys[i].column, left, leftRow)
					if err != nil {
						return err
					}
					vals[outIdx] = v
				}
			case rightRow >= 0:
				v, err := resolveBoundColumn(rightKeys[i].column, right, rightRow)
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

type resolvedJoinKey struct {
	colName string // dot-path flattened to underscores (matches select/group convention)
	column  boundColumn
}

func resolveJoinKeys(keys []ast.JoinKey, left, right schemaEnv) ([]resolvedJoinKey, []resolvedJoinKey, error) {
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

func resolveJoinKeySide(path []string, env schemaEnv, side string) (resolvedJoinKey, error) {
	if env.colIndex(path[0]) < 0 {
		return resolvedJoinKey{}, fmt.Errorf("%s join key column %q not found", side, path[0])
	}
	bound, err := bindColumnPathInEnv(env, path, &ast.ColumnExpr{Path: path})
	if err != nil {
		return resolvedJoinKey{}, fmt.Errorf("%s join key %q: %w", side, strings.Join(path, "."), err)
	}
	return resolvedJoinKey{
		colName: pathToColumnName(path),
		column:  *bound,
	}, nil
}

// buildJoinSchema computes the output schema. It returns the output column
// names, the output index where each join key is merged (left value, or right
// value for right-only rows), and a map from output index to the source right
// column index for every retained right column.
func buildJoinSchema(leftCols, rightCols []string, leftKeys, rightKeys []resolvedJoinKey, filename string) ([]string, []int, map[int]int) {
	// Only the right-side join key columns are dropped (their values are merged
	// into the left key column). Dot-path keys have no flat column to drop.
	rightKeyDrop := make(map[string]bool)
	for _, rk := range rightKeys {
		if len(rk.column.rawPath) == 1 {
			rightKeyDrop[rk.column.rawPath[0]] = true
		}
	}

	prefix := joinBasename(filename)
	outCols := append([]string(nil), leftCols...)

	leftKeyOutIdx := make([]int, len(leftKeys))
	for i, lk := range leftKeys {
		if len(lk.column.rawPath) == 1 {
			// Flat key: merge into the real left column (validated to exist).
			leftKeyOutIdx[i] = lk.column.topIndex
			continue
		}
		// Dot-path key: always append a synthetic column, suffixed if the
		// flattened name is taken, so it never aliases an unrelated left
		// column (same convention as select/group).
		name := uniqueColumnName(lk.colName, outCols)
		leftKeyOutIdx[i] = len(outCols)
		outCols = append(outCols, name)
	}

	taken := make(map[string]bool, len(outCols)+len(rightCols))
	for _, c := range outCols {
		taken[c] = true
	}
	rightColMap := make(map[int]int)
	for i, col := range rightCols {
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

func buildJoinOutputSchemas(left, right schemaEnv, leftKeys, rightKeys []resolvedJoinKey, leftKeyOutIdx []int, rightColMap map[int]int, outLen int, joinKind string) ([]*table.TypeDescriptor, error) {
	schemas := make([]*table.TypeDescriptor, outLen)

	keyOutIdx := make(map[int]bool, len(leftKeyOutIdx))
	for _, outIdx := range leftKeyOutIdx {
		keyOutIdx[outIdx] = true
	}

	for i := range left.columns {
		schema := left.rawSchema(i)
		if (joinKind == "right" || joinKind == "full") && !keyOutIdx[i] {
			schema = table.WithNullable(schema)
		}
		schemas[i] = schema
	}

	for i := range leftKeys {
		outIdx := leftKeyOutIdx[i]
		leftSchema := leftKeys[i].column.typ
		rightSchema := rightKeys[i].column.typ
		if err := validateJoinKeySchemas(leftKeys[i], rightKeys[i]); err != nil {
			return nil, err
		}
		merged, err := table.UnifyStrict(leftSchema, rightSchema)
		if err != nil {
			return nil, joinKeyTypeError(leftKeys[i], rightKeys[i])
		}
		if outIdx >= 0 && outIdx < len(schemas) {
			schemas[outIdx] = merged
		}
	}

	for outIdx, rightIdx := range rightColMap {
		if outIdx >= 0 && outIdx < len(schemas) {
			schema := right.rawSchema(rightIdx)
			if joinKind == "left" || joinKind == "full" {
				schema = table.WithNullable(schema)
			}
			schemas[outIdx] = schema
		}
	}

	for _, schema := range schemas {
		if schema == nil {
			return nil, fmt.Errorf("join: planned output schema is incomplete")
		}
	}
	return schemas, nil
}

func validateJoinKeySchemas(left, right resolvedJoinKey) error {
	if schemaContainsMixed(left.column.typ) || schemaContainsMixed(right.column.typ) {
		return fmt.Errorf("join: key type mismatch for %s and %s: mixed key schemas are not supported",
			strings.Join(left.column.rawPath, "."), strings.Join(right.column.rawPath, "."))
	}
	if !table.EquivalentSchema(joinKeyComparableSchema(left.column.typ), joinKeyComparableSchema(right.column.typ)) {
		return joinKeyTypeError(left, right)
	}
	return nil
}

func joinKeyTypeError(left, right resolvedJoinKey) error {
	return fmt.Errorf("join: key type mismatch for %s and %s: %s vs %s",
		strings.Join(left.column.rawPath, "."),
		strings.Join(right.column.rawPath, "."),
		table.Render(left.column.typ),
		table.Render(right.column.typ),
	)
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
