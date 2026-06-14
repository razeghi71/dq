package table

import "testing"

func TestCanonicalKeyDistinctForDifferentTypes(t *testing.T) {
	a := ListVal([]Value{StrVal("1"), IntVal(2)})
	b := ListVal([]Value{IntVal(1), StrVal("2")})

	if a.AsString() != b.AsString() {
		t.Fatalf("setup error: expected same string form, got %q and %q", a.AsString(), b.AsString())
	}
	if Equal(a, b) {
		t.Fatal("expected different typed lists to be unequal")
	}
	if CanonicalKey(a) == CanonicalKey(b) {
		t.Fatalf("expected distinct canonical keys, got %q", CanonicalKey(a))
	}
}

func TestCanonicalKeyIgnoresRecordFieldOrder(t *testing.T) {
	a := RecordVal([]RecordField{
		{Name: "a", Value: IntVal(1)},
		{Name: "b", Value: StrVal("x")},
	})
	b := RecordVal([]RecordField{
		{Name: "b", Value: StrVal("x")},
		{Name: "a", Value: IntVal(1)},
	})

	if !Equal(a, b) {
		t.Fatal("expected records with the same fields to be equal regardless of order")
	}
	if CanonicalKey(a) != CanonicalKey(b) {
		t.Fatalf("expected identical canonical keys, got %q and %q", CanonicalKey(a), CanonicalKey(b))
	}
}

func TestEqualStrict(t *testing.T) {
	if ok, err := EqualStrict(IntVal(1), IntVal(1)); err != nil || !ok {
		t.Fatalf("expected equal ints, got ok=%v err=%v", ok, err)
	}
	if _, err := EqualStrict(IntVal(1), StrVal("1")); err == nil {
		t.Fatal("expected type mismatch error")
	}
}

func TestCompareStrict(t *testing.T) {
	if cmp, err := CompareStrict(IntVal(1), IntVal(2)); err != nil || cmp >= 0 {
		t.Fatalf("expected 1 < 2, got cmp=%d err=%v", cmp, err)
	}
	if cmp, err := CompareStrict(IntVal(2), IntVal(1)); err != nil || cmp <= 0 {
		t.Fatalf("expected 2 > 1, got cmp=%d err=%v", cmp, err)
	}
	if cmp, err := CompareStrict(IntVal(2), IntVal(2)); err != nil || cmp != 0 {
		t.Fatalf("expected 2 == 2, got cmp=%d err=%v", cmp, err)
	}
	if cmp, err := CompareStrict(FloatVal(1.5), FloatVal(2.5)); err != nil || cmp >= 0 {
		t.Fatalf("expected 1.5 < 2.5, got cmp=%d err=%v", cmp, err)
	}
	if cmp, err := CompareStrict(FloatVal(2.5), FloatVal(1.5)); err != nil || cmp <= 0 {
		t.Fatalf("expected 2.5 > 1.5, got cmp=%d err=%v", cmp, err)
	}
	if cmp, err := CompareStrict(FloatVal(2.5), FloatVal(2.5)); err != nil || cmp != 0 {
		t.Fatalf("expected 2.5 == 2.5, got cmp=%d err=%v", cmp, err)
	}
	if cmp, err := CompareStrict(StrVal("a"), StrVal("b")); err != nil || cmp >= 0 {
		t.Fatalf("expected a < b, got cmp=%d err=%v", cmp, err)
	}
	if _, err := CompareStrict(IntVal(1), StrVal("1")); err == nil {
		t.Fatal("expected type mismatch error")
	}
	if _, err := CompareStrict(BoolVal(true), BoolVal(false)); err == nil {
		t.Fatal("expected bool ordering to be rejected")
	}
	if _, err := CompareStrict(ListVal(nil), ListVal(nil)); err == nil {
		t.Fatal("expected list ordering to be rejected")
	}
	if _, err := CompareStrict(RecordVal(nil), RecordVal(nil)); err == nil {
		t.Fatal("expected record ordering to be rejected")
	}
	if _, err := CompareStrict(Null(), Null()); err == nil {
		t.Fatal("expected null ordering to be rejected")
	}
	if _, err := CompareStrict(Value{Type: ValueType(99)}, Value{Type: ValueType(99)}); err == nil {
		t.Fatal("expected unknown type ordering to be rejected")
	}
}

func TestTypeNameAndCanonicalKeyUnknown(t *testing.T) {
	if got := TypeName(ValueType(99)); got != "unknown" {
		t.Fatalf("unknown TypeName: want unknown, got %q", got)
	}
	if got := CanonicalKey(Value{Type: ValueType(99)}); got != "unknown" {
		t.Fatalf("unknown CanonicalKey: want unknown, got %q", got)
	}
}
