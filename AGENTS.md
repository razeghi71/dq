## Query Language Overview

### Core structure

```
dq 'filename | op [args] | op2 [args] ...'
```

* The entire query is passed as a **single-quoted string** to avoid shell interpretation of `|`, `{`, `}`, `>`, `<`, and backticks.
* Takes a file (csv, avro, json, etc.) as input.
* Everything is **pipe-based** — each op takes a table and returns a table.
* Arguments are **space-separated**, strings use double quotes.
* Column lists use spaces, not commas (avoids escaping issues).
* `transform` and `reduce` use commas to separate assignments (because expressions contain spaces).
* Default state: all columns are selected unless explicitly changed.

**Why single quotes?** Characters like `|`, `{`, `}`, `>`, `` ` `` are special in most shells. Wrapping the query in single quotes passes it through to `dq` untouched, similar to how `jq` works.

**If your query contains single quotes** (e.g. string `"O'Brien"`), use double quotes for the outer wrapper instead:

```
dq "users.csv | filter { name == \"O'Brien\" }"
```

---

## Expression Grammar

Before operations, understand expressions used in `filter` and `transform`:

```
expr ::= literal | column | expr op expr | func(expr, expr, ...)
literal ::= number | "string" | true | false | null
column ::= identifier | `identifier with spaces`
op ::= + | - | * | / | == | != | < | > | <= | >= | and | or | not
```

**Key rules:**
* String literals MUST be quoted: `"NY"` (unquoted `NY` is a column reference)
* Comparisons use `==`: `age == 20` (single `=` is invalid in expressions)
* Backticks for column names with special chars: `` `first name` ``
* Null checks: `age is null`, `age is not null` (do NOT use `== null`)

---

## Operations

### 1. `head n`

Return the first `n` rows.

```
dq 'users.csv | head 10'
```

### 2. `tail n`

Return the last `n` rows.

```
dq 'users.csv | tail 5'
```

### 3. `sorta col1 col2 ...`

Sort ascending by columns (space-separated).

```
dq 'users.csv | sorta age name'
```

### 4. `sortd col1 col2 ...`

Sort descending by columns.

```
dq 'users.csv | sortd created_at id'
```

### 5. `select col1 col2 ...`

Project specific columns. All columns selected by default.

```
dq 'users.csv | select name age'
```

### 6. `filter { expression }`

Filter rows by expression. Expression is wrapped in braces `{ }` for clear boundaries.

```
dq 'users.csv | filter { age > 20 and city == "NY" }'
```

**Null handling:**
```
dq 'users.csv | filter { age is not null }'
dq 'users.csv | filter { city is null }'
```

### 7. `group col1 col2 ... [as nested_name]`

Group rows by columns; nested rows stored under a nested column. The `as nested_name` part is optional -- if omitted, defaults to `grouped`.

```
dq 'users.csv | group name'
```

Output:

```
name | grouped
---- | -------------------------
a    | [ {age:20,city:x}, {age:22,city:y} ]
b    | [ {age:25,city:z} ]
```

**With a custom nested name:**

```
dq 'users.csv | group name as entries'
```

**Multi-column grouping:**

```
dq 'users.csv | group city department'
```

Output:

```
city | department | grouped
---- | ---------- | -------------------------
NY   | sales      | [ {name:a,age:20}, {name:b,age:22} ]
NY   | engineering| [ {name:c,age:25} ]
LA   | sales      | [ {name:d,age:30} ]
```

### 8. `transform col = expr, col2 = expr2, ...`

Row-wise transformation — create or overwrite columns with computed values.

```
dq 'users.csv | transform age2 = age * 2, city = upper(city)'
```

**Arithmetic propagates nulls by default:**
```
dq 'sales.csv | transform total = quantity * price'  // null if either is null
```

Use `coalesce` for defaults:
```
dq 'sales.csv | transform total = coalesce(quantity, 0) * coalesce(price, 0)'
```

### 9. `reduce [nested_name] col = expr, col2 = expr2, ...`

Apply aggregations over nested table. The nested name is optional -- if omitted, defaults to `grouped`. Nested field is **kept** after reduction.

```
dq 'users.csv | group name | reduce max_age = max(age), count = count()'
```

Output:

```
name | grouped                            | max_age | count
---- | ---------------------------------- | --------| ------
a    | [ {age:20,city:x}, {age:22,city:y}] | 22      | 2
b    | [ {age:25,city:z} ]                | 25      | 1
```

**With a custom nested name (must match the name used in `group`):**

```
dq 'users.csv | group name as entries | reduce entries max_age = max(age), count = count()'
```

To drop the nested field, use `remove`:

```
dq 'users.csv | group name | reduce max_age = max(age), count = count() | remove grouped'
```

### 10. `count`

Return the number of rows as a single-row, single-column table.

```
dq 'users.csv | count'
dq 'users.csv | filter { age > 20 } | count'
```

### 11. `distinct [col1 col2 ...]`

Return unique rows. If columns are specified, deduplicates by those columns. If no columns are given, deduplicates by the entire row.

```
dq 'users.csv | distinct'                // unique rows
dq 'users.csv | distinct city'           // unique cities
dq 'users.csv | distinct city age'       // unique combinations
```

### 12. `rename old_name new_name [old_name2 new_name2 ...]`

Rename one or more columns. Names are paired: old then new.

```
dq 'users.csv | rename `first name` first_name'
dq 'users.csv | rename `first name` first_name `last name` last_name'
```

### 13. `remove col1 col2 ...`

Remove columns from output.

```
dq 'users.csv | remove password ssn'
dq 'users.csv | group name | reduce total = sum(amount) | remove grouped'
```

---

## Built-in Functions

These are available in `transform` and `reduce` expressions:

**Aggregations (for `reduce` only):**
* `count()` — number of rows in group
* `sum(col)` — sum of values (nulls ignored)
* `avg(col)` — average (nulls ignored)
* `min(col)`, `max(col)` — min/max (nulls ignored)
* `first(col)`, `last(col)` — first/last value in group

**Transformations (for `transform` only):**
* `upper(s)`, `lower(s)` — case conversion
* `len(s)` — string length
* `substr(s, start, len)` — substring
* `trim(s)` — remove whitespace
* `coalesce(a, b, ...)` — first non-null value
* `if(cond, then, else)` — conditional
* `year(date)`, `month(date)`, `day(date)` — date extraction

**Operators work in both:**
* Arithmetic: `+`, `-`, `*`, `/`
* Comparison: `==`, `!=`, `<`, `>`, `<=`, `>=`
* Logical: `and`, `or`, `not`

---

## Example: Full Query

Given `sales.csv` with columns: `date, product, category, quantity, price, city`

Find the top 3 categories by revenue in 2024, showing only cities with total revenue over 1000:

```
dq 'sales.csv
  | filter { year(date) == 2024 }
  | transform revenue = coalesce(quantity, 0) * coalesce(price, 0)
  | group category city
  | reduce total_revenue = sum(revenue), order_count = count()
  | remove grouped
  | filter { total_revenue > 1000 }
  | sortd total_revenue
  | head 3
  | select category city total_revenue order_count'
```

---

## Philosophy

* Everything is a **table-in, table-out** operation.
* **Explicit boundaries** — braces for expressions, no ambiguous parsing.
* **Type clarity** — quoted strings, `==` for equality, no guessing.
* **Null-safe by design** — explicit null handling, predictable propagation.
* **Flat by default, nested when grouped** — use `remove` to drop nested fields.
* **Composable** — operations chain cleanly without edge cases.
