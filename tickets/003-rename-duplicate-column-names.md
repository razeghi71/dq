# Ticket 003: Prevent duplicate column names from `rename`

## Summary

Renaming a column to a name that already exists creates a table with **duplicate column names**. Output formats handle this badly — JSON silently drops data, CSV emits duplicate headers, and the table view is ambiguous.

## Background

`rename` pairs old and new column names:

```bash
dq 'users.csv | rename name username'
dq 'users.csv | rename `first name` first_name `last name` last_name'
```

There is no validation that the new name is unique.

## Problem

Renaming one column to another column's existing name produces duplicate headers with no error.

### Failing example (current behavior)

Using `testdata/users.csv` (columns: `name`, `age`, `city`):

```bash
dq 'testdata/users.csv | rename name age'
```

**Current table output:**

```
age   | age | city
------+-----+-----
Alice | 30  | NY
Bob   | 25  | LA
...
```

Two columns are both named `age`. The first holds former `name` values (Alice, Bob, …); the second holds numeric ages (30, 25, …). This is confusing and error-prone.

### JSON output loses data silently

```bash
dq -o json 'testdata/users.csv | rename name age | head 1'
```

**Current output:**

```json
[
  {
    "age": 30,
    "city": "NY"
  }
]
```

The name `"Alice"` is **lost**. JSON object keys must be unique; the second `age` column (30) overwrote the first (`Alice`).

### CSV output has duplicate headers

```bash
dq -o csv 'testdata/users.csv | rename name age | head 1'
```

**Current output:**

```csv
age,age,city
Alice,30,NY
```

Duplicate `age` header makes the file hard to consume in other tools.

## Expected behavior after fix

Reject any `rename` operation whose final output schema would contain duplicate column names.

```bash
dq 'testdata/users.csv | rename name age'
```

**Expected output:**

```
error: rename: duplicate column name "age" in result; pick a unique name
```

No silent data loss. User must choose a unique name.

### Multi-pair renames are simultaneous

All source columns are resolved against the original input schema, then all renames are applied together. This avoids order-dependent behavior and makes swaps valid:

```bash
dq 'testdata/users.csv | rename name age age name'
# → columns become: age, name, city
```

The values stay in their original positions; only the column names are swapped.

### Explicit no-op renames are allowed

```bash
dq 'testdata/users.csv | rename name name'
# → columns remain: name, age, city
```

This does not create duplicates, so it succeeds.

### Ambiguous repeated source names are rejected

```bash
dq 'testdata/users.csv | rename name first_name name full_name'
```

**Expected output:**

```
error: rename: column "name" renamed more than once
```

## Test cases to add

### Should fail

| Query | Expected |
|-------|----------|
| `users.csv \| rename name age` | Error: duplicate result column `age` |
| `users.csv \| rename city name` when `name` exists | Error: duplicate result column `name` |
| `users.csv \| rename name x city x` | Error: duplicate result column `x` |
| `users.csv \| rename name first_name name full_name` | Error: source column renamed more than once |

### Should still work

```bash
dq 'testdata/users.csv | rename name username'
# → columns: username, age, city

dq 'testdata/users.csv | rename name username | rename city location'
# → columns: username, age, location

dq 'testdata/users.csv | rename name age age name'
# → columns: age, name, city

dq 'testdata/users.csv | rename name name'
# → columns: name, age, city
```

### Output integrity

After fix, the `rename` path should not produce:
- duplicate column names in the table schema
- duplicate JSON keys
- duplicate CSV headers

## Acceptance criteria

- [x] `rename` to an existing column name is rejected with a clear duplicate-result error.
- [x] Multi-pair renames are applied simultaneously using the original input schema.
- [x] `rename name name` succeeds as a no-op.
- [x] Repeated source columns in the same `rename` are rejected as ambiguous.
- [x] JSON output no longer silently drops data for duplicate columns produced by `rename`.
- [x] CSV/table output no longer has ambiguous duplicate headers produced by `rename`.
- [x] Error message tells the user which result name collided and suggests picking a unique name.
- [x] Tests cover rename collision, valid renames, swaps, no-op renames, and repeated source names.

## Notes for implementer

- Fix is in `execRename()` in `engine/engine.go`.
- Runtime validation is required because the parser does not know the input schema.
- Check whether `transform col = expr` overwriting an existing column has the same issue (that is intentional overwrite — different from rename collision).
- Follow-up: `group city as city` can still create duplicate output columns and should be handled separately.
