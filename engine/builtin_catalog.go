package engine

import (
	"strings"

	"github.com/razeghi71/dq/table"
)

type builtinCategory int

const (
	builtinScalar builtinCategory = iota
	builtinSpecialForm
	builtinAggregate
)

type builtinSpec struct {
	Name      string
	Category  builtinCategory
	Check     func(args []typedExpr) (*table.TypeDescriptor, error)
	TypedEval typedCallEvaluator
	Aggregate *aggregateSpec
}

type aggregateSpec struct {
	Arity          int
	NewAccumulator func(args []rowValueEvaluator) (aggregateAccumulator, error)
}

var builtinCatalog = map[string]builtinSpec{
	"upper":         scalarBuiltin("upper", unaryStringSpec("upper"), typedStringUnaryEval("upper", strings.ToUpper)),
	"lower":         scalarBuiltin("lower", unaryStringSpec("lower"), typedStringUnaryEval("lower", strings.ToLower)),
	"trim":          scalarBuiltin("trim", unaryStringSpec("trim"), typedStringUnaryEval("trim", strings.TrimSpace)),
	"str_len":       scalarBuiltin("str_len", unaryStringToIntSpec("str_len"), typedStringToIntEval("str_len", stringCodePointCount)),
	"year":          scalarBuiltin("year", unaryStringToIntSpec("year"), typedDatePartEval("year")),
	"month":         scalarBuiltin("month", unaryStringToIntSpec("month"), typedDatePartEval("month")),
	"day":           scalarBuiltin("day", unaryStringToIntSpec("day"), typedDatePartEval("day")),
	"substr":        scalarBuiltin("substr", checkSubstrSignature, typedSubstrEval),
	"str_contains":  scalarBuiltin("str_contains", binaryStringToBoolSpec("str_contains", "substring"), typedStringPredicateEval("str_contains", "substring", strings.Contains)),
	"starts_with":   scalarBuiltin("starts_with", binaryStringToBoolSpec("starts_with", "prefix"), typedStringPredicateEval("starts_with", "prefix", strings.HasPrefix)),
	"ends_with":     scalarBuiltin("ends_with", binaryStringToBoolSpec("ends_with", "suffix"), typedStringPredicateEval("ends_with", "suffix", strings.HasSuffix)),
	"matches":       scalarBuiltin("matches", binaryStringToBoolSpec("matches", "regex"), typedMatchesEval),
	"list_len":      scalarBuiltin("list_len", checkListLenSignature, typedListLenEval),
	"list_contains": scalarBuiltin("list_contains", checkListContainsSignature, typedListContainsEval),
	"coalesce":      specialFormBuiltin("coalesce", checkCoalesceSignature, typedCoalesceEval),
	"if":            specialFormBuiltin("if", checkIfSignature, typedIfEval),
	"count":         aggregateBuiltin("count", 0, aggregateSignature("count"), newCountAccumulator),
	"sum":           aggregateBuiltin("sum", 1, aggregateSignature("sum"), newSumAccumulator),
	"avg":           aggregateBuiltin("avg", 1, aggregateSignature("avg"), newAvgAccumulator),
	"min":           aggregateBuiltin("min", 1, aggregateSignature("min"), newMinAccumulator),
	"max":           aggregateBuiltin("max", 1, aggregateSignature("max"), newMaxAccumulator),
	"first":         aggregateBuiltin("first", 1, aggregateSignature("first"), newFirstAccumulator),
	"last":          aggregateBuiltin("last", 1, aggregateSignature("last"), newLastAccumulator),
}

func scalarBuiltin(name string, check func([]typedExpr) (*table.TypeDescriptor, error), typedEval typedCallEvaluator) builtinSpec {
	return builtinSpec{Name: name, Category: builtinScalar, Check: check, TypedEval: typedEval}
}

func specialFormBuiltin(name string, check func([]typedExpr) (*table.TypeDescriptor, error), typedEval typedCallEvaluator) builtinSpec {
	return builtinSpec{Name: name, Category: builtinSpecialForm, Check: check, TypedEval: typedEval}
}

func aggregateBuiltin(name string, arity int, check func([]typedExpr) (*table.TypeDescriptor, error), newAccumulator func([]rowValueEvaluator) (aggregateAccumulator, error)) builtinSpec {
	return builtinSpec{
		Name:      name,
		Category:  builtinAggregate,
		Check:     check,
		Aggregate: &aggregateSpec{Arity: arity, NewAccumulator: newAccumulator},
	}
}

func aggregateSignature(name string) func([]typedExpr) (*table.TypeDescriptor, error) {
	return func(args []typedExpr) (*table.TypeDescriptor, error) {
		return checkAggregateSignature(name, args)
	}
}
