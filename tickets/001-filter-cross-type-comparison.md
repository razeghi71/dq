# Ticket 001: Unify cross-type value comparison in `filter`

> **Status: Resolved.** See [Resolution](#resolution) at the bottom for what
> was actually implemented and how it differs from the original proposal.

## Summary

Comparisons in `filter` should treat values the same way as `join`, `group`, and `distinct`: matching by value representation, not strict Go types. Today, a column widened to string (common in CSV) cannot be compared to numeric literals.

## Background

`dq` loads CSV columns with type widening. If a column starts as integer and later rows introduce floats or strings, the whole column becomes `string`. That is expected and tested.

README also documents:

> Keys match by value representation, consistent with `group` and `distinct` — e.g. integer `1` matches string `"1"` across files of different formats.

That behavior works for join/group/distinct, but **not** for `filter`.

## Problem

When a column holds string `"1"` and the filter compares it to integer literal `1`, the query errors instead of matching the row.

### Failing examples (current behavior)

Using `testdata/mixed_types.csv`:

```
id,val
1,1
2,2.5
3,something
```

After load, `val` is widened to string for all rows.

```bash
dq 'testdata/mixed_types.csv | filter { val == 1 }'
```

**Current output:**

```
error: filter: cannot compare 1 with 1
```

```bash
dq 'testdata/mixed_types.csv | filter { val == 2.5 }'
```

**Current output:**

```
error: filter: cannot compare 1 with 2.5
```

These fail even though the values are semantically equal.

### What works today (inconsistent)

```bash
# String literal works
dq 'testdata/mixed_types.csv | filter { val == "1" }'
# → returns row id=1

# Join matches int 1 with string "2" across files
dq '/tmp/join_left.csv | join /tmp/join_right.csv on id == user_id'
# left id=1 joins right user_id="1" successfully
```

## Expected behavior after fix

`filter` comparisons should use the same value-matching rules as join/group/distinct:

1. If both sides are numeric (int/float), compare numerically.
2. If types differ but values represent the same thing (e.g. int `1` and string `"1"`), treat them as equal.
3. If values are not comparable, return a clear error (same as today for truly incompatible types like string vs record).

### Expected results

```bash
dq 'testdata/mixed_types.csv | filter { val == 1 }'
# → 1 row: id=1, val=1

dq 'testdata/mixed_types.csv | filter { val == 2.5 }'
# → 1 row: id=2, val=2.5

dq 'testdata/mixed_types.csv | filter { val == "1" }'
# → still 1 row: id=1, val=1

dq 'testdata/mixed_types.csv | filter { val > 1 }'
# → should include rows where numeric value > 1 (e.g. 2.5), not error
```

## Test cases to add

| Query | Expected rows |
|-------|----------------|
| `mixed_types.csv \| filter { val == 1 }` | 1 |
| `mixed_types.csv \| filter { val == 2.5 }` | 1 |
| `mixed_types.csv \| filter { val == "something" }` | 1 |
| `mixed_types.csv \| filter { val != 1 }` | 2 |
| `users.csv \| filter { age == 30 }` | 1 (Alice) — age stays int, should still work |
| Join int/string keys across CSV files | unchanged, still passes |

## Acceptance criteria

- [x] `filter { val == 1 }` works on widened string columns when the stored value is `"1"`.
- [x] Float/string numeric comparisons work (`val == 2.5` matches `"2.5"`).
- [x] Existing strict-type comparisons still work (int column vs int literal).
- [x] Behavior is consistent with join/group/distinct key matching (for equality — see caveat).
- [x] Truly incompatible comparisons (e.g. string vs list/record) still error clearly.
- [x] Unit/integration tests cover widened CSV columns and cross-format joins.

## Notes for implementer

Fix location: `engine/expr.go` → `evalComparison()`.

⚠️ **Do _not_ simply reuse the join/group/distinct `Value.AsString()` normalization.**
Those operations only ever test **equality** (they build hash keys), so `AsString`
is fine for them. `filter` also supports **ordering** (`<`, `>`, `<=`, `>=`), and
string-key comparison there is lexicographic, which is wrong for numbers
(e.g. `"10" < "9"`). The widened column is also a *string*, so "compare
numerically when both numeric" requires *parsing* the string to a number, which
`AsString` cannot do. See [Resolution](#resolution) for the approach used.

## Resolution

Implemented in `engine/expr.go`. Instead of normalizing both sides to strings,
`evalComparison` now coerces operands to numbers *for comparison only*:

1. Null handling (unchanged).
2. Lists/records are not comparable → clear error (e.g. string vs record).
3. `bool == bool` / `bool != bool` (unchanged).
4. **Numeric path:** a new `cmpFloat` helper coerces `int`, `float`, **and
   numeric strings** to `float64`. If *both* sides coerce, compare numerically.
   This fixes both equality and ordering (`"2.5" > 1`, `"10" > "9"`).
5. **String fallback:** remaining scalar combinations (e.g. `val == "something"`,
   or numeric vs non-numeric string) compare by value representation
   (`AsString`), matching join/group/distinct for equality.

`cmpFloat` is intentionally **local to comparison**; `Value.AsFloat` (used by
arithmetic) is left unchanged so that string-concatenation (`"a" + "b"`) and
arithmetic semantics are not affected.

### Caveats / intentional behavior changes

- **Ordering with a non-numeric string falls back to lexicographic compare and
  no longer errors.** Previously `filter { age > name }` (int vs string) errored;
  now it string-compares. This is the intended trade-off of value-representation
  matching. Only lists/records remain "truly incompatible". The existing
  `TestComparisonErrorOnTypeMismatch` was updated to assert the string-vs-record
  case instead.
- **Numeric coercion can diverge from join in edge cases.** `filter` treats
  `1 == "01"` as equal (numeric), whereas join's `AsString` keys treat `"01"`
  and `1` as distinct. Consistency holds for canonical representations.

### Tests added

- `engine/integration_test.go` → `TestIntegrationFilterCrossTypeComparison`
  covers the table below plus `val > 1` and `val > 9` (ordering).
- `TestComparisonErrorOnTypeMismatch` updated to the string-vs-record case.
