package engine

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/razeghi71/dq/ast"
	"github.com/razeghi71/dq/table"
)

type scalarArgEvaluator func(int) (table.Value, error)

func evalScalarFunction(name string, argc int, evalArg scalarArgEvaluator) (table.Value, error) {
	switch name {
	case "upper":
		return evalStringUnaryFunction(name, argc, evalArg, strings.ToUpper)
	case "lower":
		return evalStringUnaryFunction(name, argc, evalArg, strings.ToLower)
	case "trim":
		return evalStringUnaryFunction(name, argc, evalArg, strings.TrimSpace)
	case "str_len":
		return evalStringToIntFunction(name, argc, evalArg, stringCodePointCount)
	case "list_len":
		return evalListLenFunction(argc, evalArg)
	case "substr":
		return evalSubstrFunction(argc, evalArg)
	case "str_contains":
		return evalStringPredicateFunction(name, "substring", argc, evalArg, strings.Contains)
	case "list_contains":
		return evalListContainsFunction(argc, evalArg)
	case "starts_with":
		return evalStringPredicateFunction(name, "prefix", argc, evalArg, strings.HasPrefix)
	case "ends_with":
		return evalStringPredicateFunction(name, "suffix", argc, evalArg, strings.HasSuffix)
	case "matches":
		return evalMatchesFunction(argc, evalArg)
	case "coalesce":
		return evalCoalesceFunction(argc, evalArg)
	case "if":
		return evalIfFunction(argc, evalArg)
	case "year", "month", "day":
		return evalDatePartFunction(name, argc, evalArg)
	case "count", "sum", "avg", "min", "max", "first", "last":
		return table.Null(), fmt.Errorf("aggregate function %q can only be used inside 'reduce'", name)
	default:
		return table.Null(), fmt.Errorf("unknown function %q", name)
	}
}

func valueTypeName(v table.Value) string {
	switch v.Type {
	case table.TypeNull:
		return "null"
	case table.TypeInt:
		return "int"
	case table.TypeFloat:
		return "float"
	case table.TypeString:
		return "string"
	case table.TypeBool:
		return "bool"
	case table.TypeList:
		return "list"
	case table.TypeRecord:
		return "record"
	default:
		return "unknown"
	}
}

func evalStringUnaryFunction(name string, argc int, evalArg scalarArgEvaluator, fn func(string) string) (table.Value, error) {
	if argc != 1 {
		return table.Null(), fmt.Errorf("%s() takes 1 argument, got %d", name, argc)
	}
	v, err := evalArg(0)
	if err != nil {
		return table.Null(), err
	}
	if v.IsNull() {
		return table.Null(), nil
	}
	if v.Type != table.TypeString {
		return table.Null(), fmt.Errorf("%s() requires a string, got %s", name, valueTypeName(v))
	}
	return table.StrVal(fn(v.Str)), nil
}

func evalStringToIntFunction(name string, argc int, evalArg scalarArgEvaluator, fn func(string) int) (table.Value, error) {
	if argc != 1 {
		return table.Null(), fmt.Errorf("%s() takes 1 argument, got %d", name, argc)
	}
	v, err := evalArg(0)
	if err != nil {
		return table.Null(), err
	}
	if v.IsNull() {
		return table.Null(), nil
	}
	if v.Type != table.TypeString {
		return table.Null(), fmt.Errorf("%s() requires a string, got %s", name, valueTypeName(v))
	}
	return table.IntVal(int64(fn(v.Str))), nil
}

func evalListLenFunction(argc int, evalArg scalarArgEvaluator) (table.Value, error) {
	if argc != 1 {
		return table.Null(), fmt.Errorf("list_len() takes 1 argument, got %d", argc)
	}
	v, err := evalArg(0)
	if err != nil {
		return table.Null(), err
	}
	if v.IsNull() {
		return table.Null(), nil
	}
	if v.Type != table.TypeList {
		return table.Null(), fmt.Errorf("list_len() requires a list, got %s", valueTypeName(v))
	}
	return table.IntVal(int64(len(v.List))), nil
}

// stringCodePointCount returns the number of Unicode code points in s.
func stringCodePointCount(s string) int {
	return utf8.RuneCountInString(s)
}

// normalizeCodePointStart converts a 0-based code point index, counting from the end
// when negative (Python-style). Values below 0 after adjustment clamp to 0.
func normalizeCodePointStart(start, n int) int {
	if start < 0 {
		start += n
	}
	if start < 0 {
		return 0
	}
	return start
}

// codePointIndex converts a stored int64 index to int for string slicing.
func codePointIndex(v int64, field string) (int, error) {
	if v > int64(int(^uint(0)>>1)) {
		return 0, fmt.Errorf("substr: %s out of range", field)
	}
	return int(v), nil
}

// substrByCodePoints slices s by 0-based code point start and length without
// materializing the full rune slice.
func substrByCodePoints(s string, start, length int) string {
	n := stringCodePointCount(s)
	start = normalizeCodePointStart(start, n)
	if start >= n || length == 0 {
		return ""
	}
	end := start + length
	if end > n {
		end = n
	}
	var b strings.Builder
	i := 0
	for _, r := range s {
		if i >= end {
			break
		}
		if i >= start {
			b.WriteRune(r)
		}
		i++
	}
	return b.String()
}

func evalSubstrFunction(argc int, evalArg scalarArgEvaluator) (table.Value, error) {
	if argc != 3 {
		return table.Null(), fmt.Errorf("substr() takes 3 arguments (string, start, length), got %d", argc)
	}
	sv, err := evalArg(0)
	if err != nil {
		return table.Null(), err
	}
	if sv.IsNull() {
		return table.Null(), nil
	}
	if sv.Type != table.TypeString {
		return table.Null(), fmt.Errorf("substr() requires a string, got %s", valueTypeName(sv))
	}
	startV, err := evalArg(1)
	if err != nil {
		return table.Null(), err
	}
	lenV, err := evalArg(2)
	if err != nil {
		return table.Null(), err
	}
	if startV.IsNull() || lenV.IsNull() {
		return table.Null(), nil
	}

	if startV.Type != table.TypeInt {
		return table.Null(), fmt.Errorf("substr: start must be an int, got %s", valueTypeName(startV))
	}
	if lenV.Type != table.TypeInt {
		return table.Null(), fmt.Errorf("substr: length must be an int, got %s", valueTypeName(lenV))
	}

	start, err := codePointIndex(startV.Int, "start")
	if err != nil {
		return table.Null(), err
	}
	length, err := codePointIndex(lenV.Int, "length")
	if err != nil {
		return table.Null(), err
	}
	if length < 0 {
		return table.Null(), fmt.Errorf("substr: length must not be negative")
	}
	return table.StrVal(substrByCodePoints(sv.Str, start, length)), nil
}

// strPredicateArgs evaluates the two arguments of a binary string predicate.
// Both arguments must be TypeString (non-string types error); null yields ok=false
// so the caller can propagate null. On success, returns the haystack and needle strings.
func evalStringPredicateArgs(name, secondArgLabel string, argc int, evalArg scalarArgEvaluator) (s, sub string, ok bool, err error) {
	if argc != 2 {
		return "", "", false, fmt.Errorf("%s() takes 2 arguments (string, %s), got %d", name, secondArgLabel, argc)
	}
	sv, err := evalArg(0)
	if err != nil {
		return "", "", false, err
	}
	subv, err := evalArg(1)
	if err != nil {
		return "", "", false, err
	}
	if sv.IsNull() || subv.IsNull() {
		return "", "", false, nil
	}
	if sv.Type != table.TypeString {
		return "", "", false, fmt.Errorf("%s() requires a string, got %s", name, valueTypeName(sv))
	}
	if subv.Type != table.TypeString {
		return "", "", false, fmt.Errorf("%s() requires a string %s, got %s", name, secondArgLabel, valueTypeName(subv))
	}
	return sv.Str, subv.Str, true, nil
}

func evalStringPredicateFunction(name, secondArgLabel string, argc int, evalArg scalarArgEvaluator, fn func(string, string) bool) (table.Value, error) {
	s, sub, ok, err := evalStringPredicateArgs(name, secondArgLabel, argc, evalArg)
	if err != nil {
		return table.Null(), err
	}
	if !ok {
		return table.Null(), nil
	}
	return table.BoolVal(fn(s, sub)), nil
}

func evalListContainsFunction(argc int, evalArg scalarArgEvaluator) (table.Value, error) {
	if argc != 2 {
		return table.Null(), fmt.Errorf("list_contains() takes 2 arguments (list, element), got %d", argc)
	}
	listV, err := evalArg(0)
	if err != nil {
		return table.Null(), err
	}
	elemV, err := evalArg(1)
	if err != nil {
		return table.Null(), err
	}
	if listV.IsNull() || elemV.IsNull() {
		return table.Null(), nil
	}
	if listV.Type != table.TypeList {
		return table.Null(), fmt.Errorf("list_contains() requires a list, got %s", valueTypeName(listV))
	}
	for _, elem := range listV.List {
		eq, err := expressionValuesEqual(elem, elemV, false)
		if err != nil {
			return table.Null(), err
		}
		if eq {
			return table.BoolVal(true), nil
		}
	}
	return table.BoolVal(false), nil
}

var (
	regexCacheMu sync.Mutex
	// regexCache is unbounded; fine for one-shot CLI queries with literal patterns,
	// but patterns from column values are cached for the process lifetime.
	regexCache = map[string]*regexp.Regexp{}
)

// compileRegex compiles and caches a regular expression pattern.
func compileRegex(pattern string) (*regexp.Regexp, error) {
	regexCacheMu.Lock()
	defer regexCacheMu.Unlock()
	if re, ok := regexCache[pattern]; ok {
		return re, nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	regexCache[pattern] = re
	return re, nil
}

func evalMatchesFunction(argc int, evalArg scalarArgEvaluator) (table.Value, error) {
	s, pattern, ok, err := evalStringPredicateArgs("matches", "regex", argc, evalArg)
	if err != nil {
		return table.Null(), err
	}
	if !ok {
		return table.Null(), nil
	}
	re, err := compileRegex(pattern)
	if err != nil {
		return table.Null(), fmt.Errorf("matches(): invalid regex %q: %v", pattern, err)
	}
	return table.BoolVal(re.MatchString(s)), nil
}

func evalCoalesceFunction(argc int, evalArg scalarArgEvaluator) (table.Value, error) {
	if argc == 0 {
		return table.Null(), fmt.Errorf("coalesce() requires at least 1 argument")
	}
	for i := 0; i < argc; i++ {
		v, err := evalArg(i)
		if err != nil {
			return table.Null(), err
		}
		if !v.IsNull() {
			return v, nil
		}
	}
	return table.Null(), nil
}

func evalIfFunction(argc int, evalArg scalarArgEvaluator) (table.Value, error) {
	if argc != 3 {
		return table.Null(), fmt.Errorf("if() takes 3 arguments (condition, then, else), got %d", argc)
	}
	cond, err := evalArg(0)
	if err != nil {
		return table.Null(), err
	}
	if !cond.IsBoolOrNull() {
		return table.Null(), fmt.Errorf("if: condition must be boolean")
	}
	if cond.IsExplicitTrue() {
		return evalArg(1)
	}
	return evalArg(2)
}

var dateFormats = []string{
	"2006-01-02",
	"2006-01-02T15:04:05",
	"2006-01-02T15:04:05Z07:00",
	"2006-01-02 15:04:05",
	"01/02/2006",
	"1/2/2006",
	"2006/01/02",
}

func evalDatePartFunction(part string, argc int, evalArg scalarArgEvaluator) (table.Value, error) {
	if argc != 1 {
		return table.Null(), fmt.Errorf("%s() takes 1 argument, got %d", part, argc)
	}
	v, err := evalArg(0)
	if err != nil {
		return table.Null(), err
	}
	if v.IsNull() {
		return table.Null(), nil
	}
	if v.Type != table.TypeString {
		return table.Null(), fmt.Errorf("%s() requires a string, got %s", part, valueTypeName(v))
	}

	var t time.Time
	parsed := false
	for _, fmt := range dateFormats {
		if t, err = time.Parse(fmt, v.Str); err == nil {
			parsed = true
			break
		}
	}
	if !parsed {
		return table.Null(), fmt.Errorf("%s(): cannot parse %q as a date", part, v.Str)
	}

	switch part {
	case "year":
		return table.IntVal(int64(t.Year())), nil
	case "month":
		return table.IntVal(int64(t.Month())), nil
	case "day":
		return table.IntVal(int64(t.Day())), nil
	}
	return table.Null(), nil
}

// --- Aggregate evaluation (used by reduce) ---

func evalAggregateCall(e *ast.FuncCallExpr, nested *table.Table) (table.Value, error) {
	if err := validateAggregateFunctionArity(e); err != nil {
		return table.Null(), err
	}
	switch e.Name {
	case "count":
		return table.IntVal(int64(nested.NumRows)), nil
	case "sum":
		return aggSum(e, nested)
	case "avg":
		return aggAvg(e, nested)
	case "min":
		return aggMin(e, nested)
	case "max":
		return aggMax(e, nested)
	case "first":
		return aggFirst(e, nested)
	case "last":
		return aggLast(e, nested)
	default:
		return table.Null(), fmt.Errorf("non-aggregate function %q in reduce context", e.Name)
	}
}

func validateAggregateFunctionArity(e *ast.FuncCallExpr) error {
	switch e.Name {
	case "count":
		if len(e.Args) != 0 {
			return fmt.Errorf("count() takes no arguments, got %d", len(e.Args))
		}
	case "sum", "avg", "min", "max", "first", "last":
		if len(e.Args) != 1 {
			return fmt.Errorf("%s() takes 1 argument, got %d", e.Name, len(e.Args))
		}
	}
	return nil
}

func getColValues(e *ast.FuncCallExpr, nested *table.Table) ([]table.Value, error) {
	if len(e.Args) != 1 {
		return nil, fmt.Errorf("%s() takes 1 argument, got %d", e.Name, len(e.Args))
	}
	colExpr, ok := e.Args[0].(*ast.ColumnExpr)
	if !ok {
		return nil, fmt.Errorf("%s() argument must be a column reference", e.Name)
	}
	if nested.NumRows == 0 {
		return nil, nil
	}
	vals := make([]table.Value, nested.NumRows)
	for i := range vals {
		v, err := resolveColumnPath(colExpr.Path, nested, i)
		if err != nil {
			return nil, fmt.Errorf("%s(%s): %w", e.Name, strings.Join(colExpr.Path, "."), err)
		}
		vals[i] = v
	}
	return vals, nil
}

func aggSum(e *ast.FuncCallExpr, nested *table.Table) (table.Value, error) {
	vals, err := getColValues(e, nested)
	if err != nil {
		return table.Null(), err
	}
	var sum float64
	hasInt := true
	var intSum int64
	var intOverflow error
	any := false
	for _, v := range vals {
		if v.IsNull() {
			continue
		}
		f, ok := v.AsFloat()
		if !ok {
			return table.Null(), fmt.Errorf("sum: non-numeric value %v", v.AsString())
		}
		sum += f
		any = true
		if v.Type == table.TypeInt {
			if hasInt && intOverflow == nil {
				next, err := evalIntArith("+", intSum, v.Int)
				if err != nil {
					intOverflow = fmt.Errorf("sum: %w", err)
				} else {
					intSum = next
				}
			}
		} else {
			hasInt = false
		}
	}
	if !any {
		return table.Null(), nil
	}
	if hasInt {
		if intOverflow != nil {
			return table.Null(), intOverflow
		}
		return table.IntVal(intSum), nil
	}
	return table.FloatVal(sum), nil
}

func aggAvg(e *ast.FuncCallExpr, nested *table.Table) (table.Value, error) {
	vals, err := getColValues(e, nested)
	if err != nil {
		return table.Null(), err
	}
	var sum float64
	count := 0
	for _, v := range vals {
		if v.IsNull() {
			continue
		}
		f, ok := v.AsFloat()
		if !ok {
			return table.Null(), fmt.Errorf("avg: non-numeric value %v", v.AsString())
		}
		sum += f
		count++
	}
	if count == 0 {
		return table.Null(), nil
	}
	return table.FloatVal(sum / float64(count)), nil
}

func aggMin(e *ast.FuncCallExpr, nested *table.Table) (table.Value, error) {
	return aggOrderableExtremum(e, nested, "min", func(cmp int) bool { return cmp < 0 })
}

func aggMax(e *ast.FuncCallExpr, nested *table.Table) (table.Value, error) {
	return aggOrderableExtremum(e, nested, "max", func(cmp int) bool { return cmp > 0 })
}

func aggOrderableExtremum(e *ast.FuncCallExpr, nested *table.Table, name string, better func(cmp int) bool) (table.Value, error) {
	vals, err := getColValues(e, nested)
	if err != nil {
		return table.Null(), err
	}
	var best table.Value
	any := false
	for _, v := range vals {
		if v.IsNull() {
			continue
		}
		if !any {
			best = v
			any = true
			continue
		}
		cmp, unordered, err := expressionValuesCompare(v, best)
		if err != nil {
			return table.Null(), fmt.Errorf("%s: %s", name, err)
		}
		if unordered {
			continue
		}
		if better(cmp) {
			best = v
		}
	}
	if !any {
		return table.Null(), nil
	}
	return best, nil
}

func aggFirst(e *ast.FuncCallExpr, nested *table.Table) (table.Value, error) {
	vals, err := getColValues(e, nested)
	if err != nil {
		return table.Null(), err
	}
	if len(vals) == 0 {
		return table.Null(), nil
	}
	return vals[0], nil
}

func aggLast(e *ast.FuncCallExpr, nested *table.Table) (table.Value, error) {
	vals, err := getColValues(e, nested)
	if err != nil {
		return table.Null(), err
	}
	if len(vals) == 0 {
		return table.Null(), nil
	}
	return vals[len(vals)-1], nil
}
