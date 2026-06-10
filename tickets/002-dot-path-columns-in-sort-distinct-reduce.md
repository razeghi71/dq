# Ticket 002: Fix dot-path support in `sort`, `distinct`, and `reduce`

## Summary

Dot notation (e.g. `address.city`, `profile.stats.logins`) works in `filter`, `transform`, `select`, and `group`, but is **broken or incomplete** in `sort`, `distinct`, and aggregate expressions inside `reduce`. These operations should resolve nested fields the same way everywhere.

Status: validated and implemented. The fix uses the existing full-path resolver for `sort`, `distinct`, and reduce aggregate column collection, with tests covering the regression cases.

## Background

JSON/Avro/Parquet files can contain nested records. Users access nested fields with dot paths:

```bash
dq 'data.json | filter { address.city == "Chicago" }'
dq 'data.json | select name address.city'
dq 'data.json | group address.city | reduce n = count()'
```

README documents dot paths in filter, transform, select, and group. Users reasonably expect the same paths to work in `sort`, `distinct`, and `reduce` aggregates.

## Problem

The parser accepts dot paths in `sort` and `distinct`, but the engine only uses the **first path segment** (the top-level column). For `reduce`, aggregate functions like `avg(profile.stats.score)` fail because only the root column is looked up.

---

### Bug A: `sort` ignores nested field

**Failing example**

Create `/tmp/loginbug.json`:

```json
[
  {"name": "low",  "profile": {"stats": {"logins": 2}}},
  {"name": "high", "profile": {"stats": {"logins": 100}}}
]
```

```bash
dq '/tmp/loginbug.json | sort profile.stats.logins | select name profile.stats.logins'
```

**Current output (wrong order — sorted as stringified record, not numeric logins):**

```
name | profile_stats_logins
-----+---------------------
high | 100
low  | 2
```

**Correct order (numeric ascending):**

```
name | profile_stats_logins
-----+---------------------
low  | 2
high | 100
```

Workaround that proves the data is fine:

```bash
dq '/tmp/loginbug.json | transform logins=profile.stats.logins | sort logins | select name logins'
# → low (2), then high (100) ✓
```

**Root cause:** `execSort` in `engine/engine.go` uses only `k.Path[0]` and compares the whole `profile` record value.

---

### Bug B: `distinct` deduplicates by parent record, not nested field

**Failing example**

Create `/tmp/same_city.json`:

```json
[
  {"address": {"city": "NY", "street": "A"}},
  {"address": {"city": "NY", "street": "B"}}
]
```

```bash
dq '/tmp/same_city.json | distinct address.city | count'
```

**Current output:** `count = 2` (two different address records)

**Expected output:** `count = 1` (one unique city: NY)

For comparison, `group` already works:

```bash
dq '/tmp/same_city.json | group address.city | reduce n = count() | remove grouped'
# → address_city=NY, n=2 ✓
```

**Root cause:** `execDistinct` uses `path[0]` column index and `Get(i).AsString()` on the root record, not `resolveColumnPath`.

---

### Bug C: `reduce` aggregates cannot use dot-path columns

**Failing example**

Using `testdata/nested.json`:

```bash
dq 'testdata/nested.json | group address.city | reduce avg_score = avg(profile.stats.score) | remove grouped'
```

**Current output:**

```
error: reduce "avg_score": avg: non-numeric value {history:[...], stats:{logins:42, score:9.5}}
```

The engine fetched the entire `profile` record instead of `profile.stats.score`.

Similarly:

```bash
dq 'testdata/nested.json | group address.city | reduce total = sum(profile.stats.logins) | remove grouped'
# → same class of error
```

**Expected output:** one row per city with numeric aggregate, e.g.:

```
address_city | avg_score
-------------+----------
New York     | 9.5
Los Angeles  | 6.2
Chicago      | 0
```

**Root cause:** `getColValues()` in `engine/functions.go` only resolves `colExpr.Path[0]`.

---

## Expected behavior after fix

| Operation | Dot path input | Should |
|-----------|----------------|--------|
| `sort profile.stats.logins` | Numeric nested field | Sort by login count numerically |
| `sort address.city` | String nested field | Sort by city name alphabetically |
| `distinct address.city` | Nested field | Deduplicate by city value, not full address record |
| `reduce avg(profile.stats.score)` | Nested numeric field | Average the score values in each group |
| `reduce max(profile.stats.logins)` | Nested numeric field | Max of login counts in each group |

All should use the same `resolveColumnPath()` logic already used by `filter`, `select`, and `group`.

## Implementation update

The bug is real. The original examples reproduced against the pre-fix engine:

- `sort profile.stats.logins` sorted by the stringified top-level `profile` record, not by `profile.stats.logins`.
- `distinct address.city` deduplicated by the top-level `address` record, so rows with the same city but different street were treated as distinct.
- `avg(profile.stats.score)` fetched the top-level `profile` record and failed as a non-numeric aggregate input.

The implemented fix is:

- `sort`: precompute every requested sort key with `resolveColumnPath()` before calling `sort.SliceStable`, then compare the cached values. This is important because Go's sort comparator cannot return errors; resolving inside the comparator would either hide path errors or require unsafe side channels. Precomputing also avoids repeated path walks during O(n log n) comparisons.
- `distinct`: change `execDistinct` to return an error and build dedup keys from resolved full path values. This intentionally makes invalid dot paths error instead of silently returning an empty table for a missing top-level column.
- `reduce`: update aggregate column collection (`getColValues`) to resolve the full `ColumnExpr.Path` for each nested row, so `sum`, `avg`, `min`, `max`, `first`, and `last` all inherit dot-path support.

Problems with a naive version of the proposed solution:

- Calling `resolveColumnPath()` directly inside the sort comparator cannot cleanly surface errors.
- `distinct` needed a signature change because path resolution can fail.
- Missing nested leaf fields remain `null`, while attempting to traverse through a non-record still errors. This matches existing `select`/`group` semantics.
- Tests need a duplicate nested key fixture for `distinct`; `testdata/nested.json` has three unique cities, so it does not catch the original distinct bug by itself.

## Test cases to add

### Sort

```bash
# Numeric nested sort
dq '/tmp/loginbug.json | sort profile.stats.logins | select name profile.stats.logins'
# Expected row order: low, high

# String nested sort
dq 'testdata/nested.json | sort address.city | select name address.city'
# Expected: Bob (Los Angeles), Charlie (Chicago), Alice (New York) — alphabetical by city
```

### Distinct

```bash
dq '/tmp/same_city.json | distinct address.city | count'
# Expected: 1

dq '/tmp/same_city.json | distinct address.city'
# Expected: one row (any representative row, or key column only if select is used)
```

### Reduce

```bash
dq 'testdata/nested.json | group address.city | reduce avg_score = avg(profile.stats.score) | remove grouped'
# Expected: 3 rows with numeric avg_score per city

dq 'testdata/nested.json | group address.city | reduce max_logins = max(profile.stats.logins) | remove grouped'
# Expected: max logins per city
```

### Regression (must still pass)

```bash
dq 'testdata/nested.json | filter { profile.stats.logins > 10 }'
dq 'testdata/nested.json | group address.city | reduce n = count() | remove grouped'
dq 'testdata/nested.json | select name profile.stats.score'
```

## Acceptance criteria

- [x] `sort` resolves full dot paths and compares extracted values (not parent records).
- [x] `distinct` on dot paths deduplicates by extracted nested values.
- [x] `reduce` aggregates (`sum`, `avg`, `min`, `max`, `first`, `last`, `count`) accept dot-path column references.
- [x] Flat column behavior unchanged (`sort age`, `distinct city`, `reduce avg(age)`).
- [x] Tests added for all three operations. `sort` and `reduce` are covered through `testdata/nested.*`; `distinct` has a duplicate nested-key unit regression because the existing nested fixture has unique cities.
- [ ] README examples using nested paths in sort/distinct/reduce work without workarounds.

## Notes for implementer

- `resolveColumnPath()` already exists in `engine/engine.go`.
- `group` is the reference implementation for dot-path key extraction.
- `sort` should precompute resolved values per row before sorting, not resolve inside the comparator.
- `distinct` should build dedup keys from resolved path values, like `group` does.
- `getColValues()` should resolve full paths, not only `Path[0]`.
