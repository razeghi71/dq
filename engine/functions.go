package engine

import (
	"fmt"
	"math"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/razeghi71/dq/ast"
	"github.com/razeghi71/dq/table"
)

// evalFunc dispatches function calls to the appropriate implementation.
func evalFunc(e *ast.FuncCallExpr, ctx *EvalContext) (table.Value, error) {
	switch e.Name {
	// Transform functions
	case "upper":
		return callUpper(e.Args, ctx)
	case "lower":
		return callLower(e.Args, ctx)
	case "len":
		return callLen(e.Args, ctx)
	case "substr":
		return callSubstr(e.Args, ctx)
	case "trim":
		return callTrim(e.Args, ctx)
	case "contains":
		return callContains(e.Args, ctx)
	case "starts_with":
		return callStartsWith(e.Args, ctx)
	case "ends_with":
		return callEndsWith(e.Args, ctx)
	case "matches":
		return callMatches(e.Args, ctx)
	case "coalesce":
		return callCoalesce(e.Args, ctx)
	case "if":
		return callIf(e.Args, ctx)
	case "year":
		return callDatePart(e.Args, ctx, "year")
	case "month":
		return callDatePart(e.Args, ctx, "month")
	case "day":
		return callDatePart(e.Args, ctx, "day")

	// Aggregate functions (only valid inside reduce, handled by engine)
	case "count", "sum", "avg", "min", "max", "first", "last":
		return table.Null(), fmt.Errorf("aggregate function %q can only be used inside 'reduce'", e.Name)

	default:
		return table.Null(), fmt.Errorf("unknown function %q", e.Name)
	}
}

func callUpper(args []ast.Expr, ctx *EvalContext) (table.Value, error) {
	if len(args) != 1 {
		return table.Null(), fmt.Errorf("upper() takes 1 argument, got %d", len(args))
	}
	v, err := Eval(args[0], ctx)
	if err != nil {
		return table.Null(), err
	}
	if v.IsNull() {
		return table.Null(), nil
	}
	return table.StrVal(strings.ToUpper(v.AsString())), nil
}

func callLower(args []ast.Expr, ctx *EvalContext) (table.Value, error) {
	if len(args) != 1 {
		return table.Null(), fmt.Errorf("lower() takes 1 argument, got %d", len(args))
	}
	v, err := Eval(args[0], ctx)
	if err != nil {
		return table.Null(), err
	}
	if v.IsNull() {
		return table.Null(), nil
	}
	return table.StrVal(strings.ToLower(v.AsString())), nil
}

func callLen(args []ast.Expr, ctx *EvalContext) (table.Value, error) {
	if len(args) != 1 {
		return table.Null(), fmt.Errorf("len() takes 1 argument, got %d", len(args))
	}
	v, err := Eval(args[0], ctx)
	if err != nil {
		return table.Null(), err
	}
	if v.IsNull() {
		return table.Null(), nil
	}
	return table.IntVal(int64(len(v.AsString()))), nil
}

func callSubstr(args []ast.Expr, ctx *EvalContext) (table.Value, error) {
	if len(args) != 3 {
		return table.Null(), fmt.Errorf("substr() takes 3 arguments (string, start, length), got %d", len(args))
	}
	sv, err := Eval(args[0], ctx)
	if err != nil {
		return table.Null(), err
	}
	if sv.IsNull() {
		return table.Null(), nil
	}
	s := sv.AsString()

	startV, err := Eval(args[1], ctx)
	if err != nil {
		return table.Null(), err
	}
	lenV, err := Eval(args[2], ctx)
	if err != nil {
		return table.Null(), err
	}

	startF, ok := startV.AsFloat()
	if !ok {
		return table.Null(), fmt.Errorf("substr: start must be a number")
	}
	lenF, ok := lenV.AsFloat()
	if !ok {
		return table.Null(), fmt.Errorf("substr: length must be a number")
	}

	start := int(startF)
	length := int(lenF)
	if start < 0 {
		start = 0
	}
	if start >= len(s) {
		return table.StrVal(""), nil
	}
	end := start + length
	if end > len(s) {
		end = len(s)
	}
	return table.StrVal(s[start:end]), nil
}

func callTrim(args []ast.Expr, ctx *EvalContext) (table.Value, error) {
	if len(args) != 1 {
		return table.Null(), fmt.Errorf("trim() takes 1 argument, got %d", len(args))
	}
	v, err := Eval(args[0], ctx)
	if err != nil {
		return table.Null(), err
	}
	if v.IsNull() {
		return table.Null(), nil
	}
	return table.StrVal(strings.TrimSpace(v.AsString())), nil
}

// strPredicateArgs evaluates the two arguments of a binary string predicate,
// returning the haystack and needle as strings. If either argument is null,
// ok is false so the caller can propagate null.
func strPredicateArgs(name, secondArgLabel string, args []ast.Expr, ctx *EvalContext) (s, sub string, ok bool, err error) {
	if len(args) != 2 {
		return "", "", false, fmt.Errorf("%s() takes 2 arguments (string, %s), got %d", name, secondArgLabel, len(args))
	}
	sv, err := Eval(args[0], ctx)
	if err != nil {
		return "", "", false, err
	}
	subv, err := Eval(args[1], ctx)
	if err != nil {
		return "", "", false, err
	}
	if sv.IsNull() || subv.IsNull() {
		return "", "", false, nil
	}
	return sv.AsString(), subv.AsString(), true, nil
}

func callContains(args []ast.Expr, ctx *EvalContext) (table.Value, error) {
	s, sub, ok, err := strPredicateArgs("contains", "substring", args, ctx)
	if err != nil {
		return table.Null(), err
	}
	if !ok {
		return table.Null(), nil
	}
	return table.BoolVal(strings.Contains(s, sub)), nil
}

func callStartsWith(args []ast.Expr, ctx *EvalContext) (table.Value, error) {
	s, sub, ok, err := strPredicateArgs("starts_with", "prefix", args, ctx)
	if err != nil {
		return table.Null(), err
	}
	if !ok {
		return table.Null(), nil
	}
	return table.BoolVal(strings.HasPrefix(s, sub)), nil
}

func callEndsWith(args []ast.Expr, ctx *EvalContext) (table.Value, error) {
	s, sub, ok, err := strPredicateArgs("ends_with", "suffix", args, ctx)
	if err != nil {
		return table.Null(), err
	}
	if !ok {
		return table.Null(), nil
	}
	return table.BoolVal(strings.HasSuffix(s, sub)), nil
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

func callMatches(args []ast.Expr, ctx *EvalContext) (table.Value, error) {
	s, pattern, ok, err := strPredicateArgs("matches", "regex", args, ctx)
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

func callCoalesce(args []ast.Expr, ctx *EvalContext) (table.Value, error) {
	if len(args) == 0 {
		return table.Null(), fmt.Errorf("coalesce() requires at least 1 argument")
	}
	for _, arg := range args {
		v, err := Eval(arg, ctx)
		if err != nil {
			return table.Null(), err
		}
		if !v.IsNull() {
			return v, nil
		}
	}
	return table.Null(), nil
}

func callIf(args []ast.Expr, ctx *EvalContext) (table.Value, error) {
	if len(args) != 3 {
		return table.Null(), fmt.Errorf("if() takes 3 arguments (condition, then, else), got %d", len(args))
	}
	cond, err := Eval(args[0], ctx)
	if err != nil {
		return table.Null(), err
	}
	b, ok := cond.AsBool()
	if !ok {
		return table.Null(), fmt.Errorf("if: condition must be boolean")
	}
	if b {
		return Eval(args[1], ctx)
	}
	return Eval(args[2], ctx)
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

func callDatePart(args []ast.Expr, ctx *EvalContext, part string) (table.Value, error) {
	if len(args) != 1 {
		return table.Null(), fmt.Errorf("%s() takes 1 argument, got %d", part, len(args))
	}
	v, err := Eval(args[0], ctx)
	if err != nil {
		return table.Null(), err
	}
	if v.IsNull() {
		return table.Null(), nil
	}
	s := v.AsString()

	var t time.Time
	parsed := false
	for _, fmt := range dateFormats {
		if t, err = time.Parse(fmt, s); err == nil {
			parsed = true
			break
		}
	}
	if !parsed {
		return table.Null(), fmt.Errorf("%s(): cannot parse %q as a date", part, s)
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

// EvalAggregate evaluates an aggregate expression over a nested table.
func EvalAggregate(expr ast.Expr, nested *table.Table) (table.Value, error) {
	switch e := expr.(type) {
	case *ast.FuncCallExpr:
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
			// Non-aggregate function: this shouldn't happen in reduce context
			// but if it does, try evaluating row-wise (error)
			return table.Null(), fmt.Errorf("non-aggregate function %q in reduce context", e.Name)
		}
	case *ast.BinaryExpr:
		left, err := EvalAggregate(e.Left, nested)
		if err != nil {
			return table.Null(), err
		}
		right, err := EvalAggregate(e.Right, nested)
		if err != nil {
			return table.Null(), err
		}
		if left.IsNull() || right.IsNull() {
			return table.Null(), nil
		}
		return evalArith(e.Op, left, right)
	case *ast.LiteralExpr:
		return evalLiteral(e), nil
	default:
		return table.Null(), fmt.Errorf("unsupported expression type %T in reduce", expr)
	}
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
			intSum += v.Int
		} else {
			hasInt = false
		}
	}
	if !any {
		return table.Null(), nil
	}
	if hasInt {
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
	vals, err := getColValues(e, nested)
	if err != nil {
		return table.Null(), err
	}
	minVal := math.Inf(1)
	any := false
	isInt := true
	var minInt int64
	for _, v := range vals {
		if v.IsNull() {
			continue
		}
		f, ok := v.AsFloat()
		if !ok {
			return table.Null(), fmt.Errorf("min: non-numeric value %v", v.AsString())
		}
		if !any || f < minVal {
			minVal = f
			if v.Type == table.TypeInt {
				minInt = v.Int
			} else {
				isInt = false
			}
		}
		any = true
	}
	if !any {
		return table.Null(), nil
	}
	if isInt {
		return table.IntVal(minInt), nil
	}
	return table.FloatVal(minVal), nil
}

func aggMax(e *ast.FuncCallExpr, nested *table.Table) (table.Value, error) {
	vals, err := getColValues(e, nested)
	if err != nil {
		return table.Null(), err
	}
	maxVal := math.Inf(-1)
	any := false
	isInt := true
	var maxInt int64
	for _, v := range vals {
		if v.IsNull() {
			continue
		}
		f, ok := v.AsFloat()
		if !ok {
			return table.Null(), fmt.Errorf("max: non-numeric value %v", v.AsString())
		}
		if !any || f > maxVal {
			maxVal = f
			if v.Type == table.TypeInt {
				maxInt = v.Int
			} else {
				isInt = false
			}
		}
		any = true
	}
	if !any {
		return table.Null(), nil
	}
	if isInt {
		return table.IntVal(maxInt), nil
	}
	return table.FloatVal(maxVal), nil
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
