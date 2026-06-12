## Query Language Overview

### Core structure

```
dq 'filename | op [args] | op2 [args] ... [| output_format]'
```

* The entire query is passed as a **single-quoted string** to avoid shell interpretation of `|`, `{`, `}`, `>`, `<`, and backticks.
* Takes a file (csv, avro, json, etc.) as input. Globs are supported (`logs/**/*.csv`, `orders/part-*.csv`) — matched files are concatenated. All matched files are loaded into memory before the pipeline runs. A zero-byte CSV (or BOM-/whitespace-only with no data rows) loads as an empty table (0 columns, 0 rows). Empty glob shards are skipped when establishing the column schema; the first non-empty shard defines the anchor columns. CSV glob shards without a detectable header row are read positionally under the first file's columns; extra cells per row are dropped. Extended headers require shared column names plus new lowercase identifiers (`email`, not `Email`); renamed columns with no anchor overlap are positional.
* Optional **`with key=value, ...`** on the primary source or join file sets load format and CSV options (see Load options below).
* Optional **output format command** at the end of the query (`table`, `csv`, `json`, `jsonl`, `avro`, `parquet`); omitted means pretty table output.
* Everything is **pipe-based** — each op takes a table and returns a table.
* **Column lists** (`select`, `sort`, `group`, `distinct`, `remove`) and **assignment lists** (`transform`, `reduce`, `rename`) are **comma-separated**. A single item needs no comma (see Syntax rules below).
* Default state: all columns are selected unless explicitly changed.

**Why single quotes?** Characters like `|`, `{`, `}`, `>`, `` ` `` are special in most shells. Wrapping the query in single quotes passes it through to `dq` untouched, similar to how `jq` works.

**If your query contains single quotes** (e.g. string `"O'Brien"`), use double quotes for the outer wrapper instead:

```
dq "users.csv | filter { name == \"O'Brien\" }"
```

---

## Load options (`with`)

Optional on any source or join file, before `|` or `on`:

```
source    ::= filename [ with load_opts ]
join      ::= "join" [ kind ] filename [ with load_opts ] "on" join_keys
load_opts ::= "with" load_opt ( "," load_opt )*
load_opt  ::= ident "=" value
```

Examples:

```
data.dat with format=csv | head 5
logs/part-* with format=csv | count
- with format=csv | filter { age > 25 }
users.csv | join left orders/part-*.dat with format=csv, delim=";" on user_id == customer_id
```

| Key | Applies to | Values | Notes |
|-----|------------|--------|-------|
| `format` | all | `csv`, `json`, `jsonl`, `avro`, `parquet` | Overrides extension; required when extension missing |
| `header` | csv | `true`, `false` | Default `true`; `false` uses `col1`, `col2`, … from first row width |
| `delim` | csv | string, e.g. `delim=";"` | Default `,`; only the first character is used as the separator |

Format resolution: explicit `format=` → file extension → error (`use with format=...`). Stdin (`-`) requires `with format=...`. Use `with format=...` when a glob matches mixed or missing extensions.

---

## Syntax rules

Every operation uses one of three argument styles. The style is fixed per op — do not mix them.

### Lists (comma-separated)

Separate columns or sort keys with **commas**. A single item needs no comma.

| Op | Example |
|----|---------|
| `select` | `select name, age, address.city` |
| `sort` | `sort -created_at, id` |
| `group` | `group city, department as entries` |
| `distinct` | `distinct city, age` |
| `remove` | `remove password, ssn` |

Rules:
* Dot paths are one item: `address.city` is a single column, not two.
* Backticks for names with spaces: `` select `first name`, age ``
* `sort`: prefix `-` on a key for descending (`sort city, -age`).
* `group`: optional `as nested_name` comes after the column list.
* `distinct` with no columns deduplicates the full row.

### Bindings (comma-separated, single `=`)

Separate assignments with **commas**. Use a single **`=`** (not `==`).

| Op | Example |
|----|---------|
| `transform` | `transform age2 = age * 2, city = upper(city)` |
| `reduce` | `reduce total = sum(amount), n = count()` |
| `rename` | `` rename `first name`=first_name, city=location `` |

Rules:
* `rename` pairs use `old=new` bindings (same `=` style as `transform`; whitespace around `=` is ignored).
* `reduce` takes an optional nested column name as a **single identifier** before the assignments: `reduce entries max_age = max(age), count = count()`.

### Comparisons (double `==`, not comma lists)

Inside `{ ... }` filters and join `on` clauses, equality is **`==`**. Single `=` is not comparison syntax.

```
filter { age > 20 and city == "NY" }
join orders.csv on id == customer_id and region == region
```

Rules:
* Join keys are separated by **`and`**, not commas.
* Shorthand when names match: `join orders.csv on user_id` (same as `user_id == user_id`).
* String literals must be double-quoted: `"NY"`.

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
* Logical `and`, `or`, `not` use SQL three-valued logic (see below)

**Three-valued logic (`and`, `or`, `not`):**

`null` in boolean context means *unknown*, not false.

| Expression | Result |
|------------|--------|
| `true and true` | true |
| `true and false` | false |
| `true and null` | null |
| `false and null` | false |
| `null and null` | null |
| `true or false` | true |
| `false or null` | null |
| `null or null` | null |
| `not true` | false |
| `not false` | true |
| `not null` | null |

`and` binds tighter than `or` (same as SQL): `null and true or false` is `(null and true) or false`.

In `filter`, a row is kept only when the expression is explicitly `true`; `false` and `null` both drop the row. In `if(cond, then, else)`, only explicit `true` takes the then branch; `false` and `null` take else.

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

### 3. `sort [-]col1, [-]col2, ...`

Sort by columns (comma-separated). Ascending by default; prefix a column with `-` to sort it descending. Directions can be mixed per column.

```
dq 'users.csv | sort age, name'         // both ascending
dq 'users.csv | sort -created_at, id'   // created_at descending, id ascending
```

### 4. `select col1, col2, ...`

Project specific columns. All columns selected by default.

```
dq 'users.csv | select name, age'
```

### 5. `filter { expression }`

Filter rows by expression. Expression is wrapped in braces `{ }` for clear boundaries.

```
dq 'users.csv | filter { age > 20 and city == "NY" }'
```

**Null and boolean handling:**

`filter` keeps a row only when the expression is explicitly `true`. Comparisons and predicates that yield `null` drop the row (same as `false`). Use `is null` / `is not null` for definite null checks. A bare boolean column is a valid predicate (`filter { active }` keeps rows where `active` is explicitly `true`; `false` and `null` drop).

```
dq 'users.csv | filter { age is not null }'
dq 'users.csv | filter { city is null }'
dq 'users.csv | filter { active }'              // true keeps; false/null drop
dq 'users.csv | filter { age > null }'          // null → drops row
dq 'users.csv | filter { null and true }'       // null → drops row
dq 'users.csv | filter { null or true }'        // true → keeps row
```

### 6. `group col1, col2, ... [as nested_name]`

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
dq 'users.csv | group city, department'
```

Output:

```
city | department | grouped
---- | ---------- | -------------------------
NY   | sales      | [ {name:a,age:20}, {name:b,age:22} ]
NY   | engineering| [ {name:c,age:25} ]
LA   | sales      | [ {name:d,age:30} ]
```

### 7. `transform col = expr, col2 = expr2, ...`

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

### 8. `reduce [nested_name] col = expr, col2 = expr2, ...`

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

### 9. `count`

Return the number of rows as a single-row, single-column table.

```
dq 'users.csv | count'
dq 'users.csv | filter { age > 20 } | count'
```

### 10. `distinct [col1, col2, ...]`

Return unique rows. If columns are specified, deduplicates by those columns. If no columns are given, deduplicates by the entire row.

```
dq 'users.csv | distinct'                // unique rows
dq 'users.csv | distinct city'           // unique cities
dq 'users.csv | distinct city, age'      // unique combinations
```

### 11. `rename old=new [, old2=new2 ...]`

Rename one or more columns. Comma-separated `old=new` bindings.

```
dq 'users.csv | rename name=first_name'
dq 'users.csv | rename `first name`=first_name, `last name`=last_name'
```

### 12. `remove col1, col2, ...`

Remove columns from output.

```
dq 'users.csv | remove password, ssn'
dq 'users.csv | group name | reduce total = sum(amount) | remove grouped'
```

### 13. `join [kind] file on key [and key ...]`

Join with another file. Kind is optional: `inner` (default), `left`, `right`, `full`.

```
dq 'users.csv | join orders.csv on name == user_name'
dq 'users.csv | join left orders.csv on user_id'
dq 'users.csv | join full orders.csv on id == customer_id and region == region'
```

Each key is either a column path (same name on both sides) or `left_path == right_path`. Join key columns appear once under the left-side name; dot-path keys get a flattened column (`address.city` -> `address_city`, suffixed if taken). Colliding right-side columns are prefixed with the join file basename (for globs, derived from the pattern — e.g. `orders/*.csv` with colliding column `note` -> `orders_note`).

The join file's format comes from its extension unless overridden with `with format=...` on the join path. Join sources support globs (`orders/part-*.csv`); matched files are concatenated before the join. Null keys never match. Keys match by value representation (consistent with `group`/`distinct`), so `1` matches `"1"` across formats.

---

## Output format commands

Optional terminal stage after the pipeline. At most one per query; must be last (reject `| csv | head`).

```
output_cmd ::= "table" | "csv" | "json" | "jsonl" | "avro" | "parquet"
```

Omitted `output_cmd` → pretty **table** (same as `| table` for rendering).

```
dq 'users.csv | select name, age'
dq 'users.csv | select name, age | csv'
dq 'users.csv | count | json'
cat data.csv | dq '- with format=csv | count | jsonl'
```

Not lexer keywords — only recognized as the final `|`-stage. Column names like `csv` in `{ csv == "x" }` are unaffected.

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
* `upper(s)`, `lower(s)` — case conversion (`TypeString` only)
* `str_len(s)` — string length in Unicode code points, 0-based indexing companion (`TypeString` only)
* `list_len(xs)` — list element count (`TypeList` only)
* `substr(s, start, length)` — substring by **0-based code point** index and length (`TypeString` only for `s`; `start` and `length` must be `TypeInt`); negative `start` counts from the end (Python-style); `length` must be non-negative
* `trim(s)` — remove whitespace (`TypeString` only)
* `coalesce(a, b, ...)` — first non-null value
* `if(cond, then, else)` — conditional; only explicit `true` takes then, `false` and `null` take else
* `year(date)`, `month(date)`, `day(date)` — date extraction

**String predicates (return booleans; usable in `filter` and `transform`; `TypeString` only):**
* `contains(s, sub)` — true if `s` contains substring `sub`
* `starts_with(s, prefix)` — true if `s` starts with `prefix`
* `ends_with(s, suffix)` — true if `s` ends with `suffix`
* `matches(s, regex)` — true if `s` contains a match for the (RE2) regular expression `regex` (unanchored; use `^...$` for full-string match)

Predicates match on **UTF-8 text** (contiguous substring or regex over the string bytes), not by code-point index. Only `str_len` / `substr` use code-point units.

Matching is **case-sensitive** (`"ERROR"` does not match `"error"`).

Null arguments produce null. In `filter`, a null result drops the row (same as `false`).

```
dq 'users.csv | transform name_len = str_len(name)'
dq 'nested.json | transform order_count = list_len(orders)'
dq 'nested.json | filter { list_len(orders) > 1 } | select name'
```

Invalid regex in `matches()` fails the query when that row is evaluated (including patterns taken from a column).

```
dq 'logs.csv | filter { contains(upper(message), "ERROR") }'
dq 'logs.csv | filter { starts_with(level, "WARN") }'
dq 'access.log.csv | filter { matches(message, "timeout|refused") }'
```

**Operators work in both:**
* Arithmetic: `+`, `-`, `*`, `/`
* Comparison: `==`, `!=`, `<`, `>`, `<=`, `>=`
* Logical: `and`, `or`, `not` (SQL three-valued logic; see Expression Grammar)

---

## Example: Full Query

Given `sales.csv` with columns: `date, product, category, quantity, price, city`

Find the top 3 categories by revenue in 2024, showing only cities with total revenue over 1000:

```
dq 'sales.csv
  | filter { year(date) == 2024 }
  | transform revenue = coalesce(quantity, 0) * coalesce(price, 0)
  | group category, city
  | reduce total_revenue = sum(revenue), order_count = count()
  | remove grouped
  | filter { total_revenue > 1000 }
  | sort -total_revenue
  | head 3
  | select category, city, total_revenue, order_count'
```

---

## Philosophy

* Everything is a **table-in, table-out** operation.
* **Explicit boundaries** — braces for expressions, no ambiguous parsing.
* **Type clarity** — quoted strings, `==` for equality, no guessing.
* **Null-safe by design** — explicit null handling, predictable propagation.
* **Flat by default, nested when grouped** — use `remove` to drop nested fields.
* **Composable** — operations chain cleanly without edge cases.
