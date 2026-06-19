package table

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// TypeName returns a human-readable name for a ValueType.
func TypeName(t ValueType) string {
	switch t {
	case TypeNull:
		return "null"
	case TypeInt:
		return "int"
	case TypeFloat:
		return "float"
	case TypeString:
		return "string"
	case TypeBool:
		return "bool"
	case TypeList:
		return "list"
	case TypeRecord:
		return "record"
	case TypeUnion:
		return "union"
	case TypeMixed:
		return "mixed"
	default:
		return "unknown"
	}
}

// CanonicalKey returns a stable structural key for v.
//
// The key is type-tagged so values only collide when both type and value match.
// Records are compared by field name/value pairs independent of field order.
func CanonicalKey(v Value) string {
	var b strings.Builder
	writeCanonicalKey(&b, v)
	return b.String()
}

func writeCanonicalKey(b *strings.Builder, v Value) {
	switch v.Type {
	case TypeNull:
		b.WriteString("null")
	case TypeInt:
		b.WriteString("int:")
		b.WriteString(strconv.FormatInt(v.Int, 10))
	case TypeFloat:
		b.WriteString("float:")
		b.WriteString(strconv.FormatFloat(v.Float, 'g', -1, 64))
	case TypeString:
		b.WriteString("string:")
		b.WriteString(strconv.Quote(v.Str))
	case TypeBool:
		b.WriteString("bool:")
		if v.Bool {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case TypeList:
		b.WriteString("list:[")
		for i, elem := range v.List {
			if i > 0 {
				b.WriteByte(',')
			}
			writeCanonicalKey(b, elem)
		}
		b.WriteByte(']')
	case TypeRecord:
		b.WriteString("record:{")
		if len(v.Fields) > 0 {
			idxs := make([]int, len(v.Fields))
			for i := range idxs {
				idxs[i] = i
			}
			sort.SliceStable(idxs, func(i, j int) bool {
				if v.Fields[idxs[i]].Name == v.Fields[idxs[j]].Name {
					return idxs[i] < idxs[j]
				}
				return v.Fields[idxs[i]].Name < v.Fields[idxs[j]].Name
			})
			for i, idx := range idxs {
				if i > 0 {
					b.WriteByte(',')
				}
				b.WriteString(strconv.Quote(v.Fields[idx].Name))
				b.WriteByte(':')
				writeCanonicalKey(b, v.Fields[idx].Value)
			}
		}
		b.WriteByte('}')
	default:
		b.WriteString("unknown")
	}
}

// Equal reports whether two values have the same type and structural value.
func Equal(a, b Value) bool {
	return CanonicalKey(a) == CanonicalKey(b)
}

// EqualStrict reports whether two values are equal, rejecting type mismatches.
func EqualStrict(a, b Value) (bool, error) {
	if a.Type != b.Type {
		return false, fmt.Errorf("type mismatch: %s vs %s", TypeName(a.Type), TypeName(b.Type))
	}
	return Equal(a, b), nil
}

// CompareStrict compares two values of the same comparable type.
//
// It returns an error when the values do not have the same type or the type is
// not orderable. Equality should use Equal instead.
func CompareStrict(a, b Value) (int, error) {
	if a.Type != b.Type {
		return 0, fmt.Errorf("type mismatch: %s vs %s", TypeName(a.Type), TypeName(b.Type))
	}

	switch a.Type {
	case TypeInt:
		switch {
		case a.Int < b.Int:
			return -1, nil
		case a.Int > b.Int:
			return 1, nil
		default:
			return 0, nil
		}
	case TypeFloat:
		switch {
		case a.Float < b.Float:
			return -1, nil
		case a.Float > b.Float:
			return 1, nil
		default:
			return 0, nil
		}
	case TypeString:
		return strings.Compare(a.Str, b.Str), nil
	case TypeBool:
		return 0, fmt.Errorf("bool values are not orderable")
	case TypeList, TypeRecord:
		return 0, fmt.Errorf("%s values are not orderable", TypeName(a.Type))
	case TypeNull:
		return 0, fmt.Errorf("null values are not orderable")
	default:
		return 0, fmt.Errorf("unknown value type")
	}
}
