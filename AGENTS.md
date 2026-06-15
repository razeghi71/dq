## Query Language Overview

### Core structure

```
dq 'filename | op [args] | op2 [args] ... [| output_format]'
```

* The entire query is passed as a **single-quoted string** to avoid shell interpretation of `|`, `{`, `}`, `>`, `<`, and backticks.
* Takes a file (csv, avro, json, etc.) as input. Gzip-compressed CSV/JSON/JSONL text files are supported via double extensions such as `.csv.gz` or explicit `with compression=gzip`. Globs are supported (`logs/**/*.csv`, `orders/part-*.csv`) — matched files are concatenated. All matched files are loaded into memory before the pipeline runs. A zero-byte CSV (or BOM-/whitespace-only with no data rows) loads as an empty table (0 columns, 0 rows). Empty glob shards are skipped when establishing the column schema; the first non-empty shard defines the anchor columns. CSV rows must match the schema width by default (same rules for single files and glob shards); use `with allow_jagged_rows=true` for missing trailing columns (null-filled) and `with ignore_unknown_values=true` to drop extra columns. On glob sources, CSV-only options require `with format=csv` at parse time (e.g. `logs/part-* with format=csv, allow_jagged_rows=true`) — format cannot be inferred from the pattern alone. CSV glob shards without a detectable header row are read positionally under the first file's columns. Extended headers require shared column names plus new lowercase identifiers (`email`, not `Email`); renamed columns with no anchor overlap are positional.
* Optional **`with key=value, ...`** on the primary source or join file sets load format, compression, and CSV options (see Load options below).
* Optional **output format command** at the end of the query (`table`, `csv`, `json`, `jsonl`, `avro`, `parquet`); omitted means pretty table output. Output commands can write to a path with `to`, and can split files with `with split_rows=N`.
* Everything is **pipe-based** — each op takes a table and returns a table.
* **Column lists** (`select`, `sort`, `group`, `distinct`, `remove`) and **assignment lists** (`transform`, `reduce`, `rename`) are **comma-separated**. A single item needs no comma (see Syntax rules below).
* Default state: all columns are selected unless explicitly changed.

**Why single quotes?** Characters like `|`, `{`, `}`, `>`, `` ` `` are special in most shells. Wrapping the query in single quotes passes it through to `dq` untouched, similar to how `jq` works.

**If your query contains single quotes** (e.g. string `"O'Brien"`), use double quotes for the outer wrapper instead:

```
dq "users.csv | filter { name == \"O'Brien\" }"
```

---

## MCP / Agent Integration

`dq mcp` starts a stdio Model Context Protocol server for local agent hosts. Messages are newline-delimited JSON-RPC objects on stdin/stdout, as required by the MCP stdio transport:

```
dq mcp
```

Host configuration example:

```
{
  "mcpServers": {
    "dq": {
      "command": "dq",
      "args": ["mcp"]
    }
  }
}
```

### MCP surface

The MCP server intentionally exposes the query language directly instead of creating a second API.

| Surface | Name | Purpose |
|---------|------|---------|
| Tool | `query` | Run one complete `dq` query string through the same execution path as the CLI |
| Resource | `dq://guide` | Return `README.md` as Markdown |

There is no separate MCP `describe`, `head`, `sample`, export, or format tool. Use normal query syntax:

```
users.csv | describe | json
users.csv | head 5 | json
users.csv | filter { city == "NY" } | select name | json
users.csv | csv to out/users.csv
users.csv | csv with split_rows=50000 to out/
```

The `query` tool input schema is:

```
{
  "query": "users.csv | filter { age > 25 } | select name, city | json"
}
```

The query string is exactly the same language accepted by the CLI: source, load options, pipeline operations, and an optional terminal output command. This keeps agent behavior and CLI behavior from drifting.

### Output and errors

For stdout queries, the MCP tool returns the stdout payload as text content. The payload may be table text, CSV, JSON, JSONL, or any other textual output chosen by the query.

For `to path` queries, the file write is the primary result. The MCP tool returns a successful text response with no table payload.

Parse, load, engine, and output failures are returned as MCP tool errors. Examples:

```
users.csv | csv | head 1          # output command not last -> tool error
missing.csv | count               # load error
users.csv | csv to existing.csv   # output error unless overwrite=true
```

### Stdin and trust model

Stdin source queries are not supported through MCP:

```
- with format=csv | count
```

MCP already uses stdio for JSON-RPC transport, so data should be read from files, globs, or future explicit content tools instead.

The MCP server runs with the same OS privileges as the host process. Relative paths and globs resolve from the server process working directory. File output can write wherever that process has permission. Only enable `dq mcp` for trusted workspaces.

### Agent guide

`dq -agent-guide` prints `README.md`. The MCP `dq://guide` resource returns the same content.

`README.md` is intentionally shorter and example-led. `AGENTS.md` remains the detailed language and design contract for agents and maintainers.

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
data.csv.gz | head 5
data.dat with format=csv, compression=gzip | count
logs/part-* with format=csv | count
logs/part-* with format=csv, allow_jagged_rows=true, ignore_unknown_values=true | count
- with format=csv | filter { age > 25 }
- with format=csv, compression=gzip | count
users.csv | join left orders/part-*.dat with format=csv, delim=";" on user_id == customer_id
users.csv | join orders.data with format=csv, compression=gzip on user_id
```

| Key | Applies to | Values | Notes |
|-----|------------|--------|-------|
| `format` | all | `csv`, `json`, `jsonl`, `avro`, `parquet` | Overrides extension; required when extension missing |
| `compression` | csv/json/jsonl inputs | `gzip` | File-level gzip wrapper; inferred from `.csv.gz`, `.json.gz`, `.jsonl.gz`; explicit `compression=gzip` also works for stdin when `format=...` is set; not supported for avro or parquet |
| `header` | csv | `true`, `false` | Default `true`; `false` uses `col1`, `col2`, … from first row width |
| `delim` | csv | string, e.g. `delim=";"` | Default `,`; only the first character is used as the separator |
| `allow_jagged_rows` | csv | `true`, `false` | Default `false`; when `true`, rows with fewer fields than the schema get null-filled trailing columns |
| `ignore_unknown_values` | csv | `true`, `false` | Default `false`; when `true`, extra fields beyond the schema are dropped |

Format resolution: explicit `format=` → file extension → error (`use with format=...`). Literal gzip double extensions infer both pieces: `.csv.gz` means `format=csv, compression=gzip`, `.json.gz` means `format=json, compression=gzip`, and `.jsonl.gz` means `format=jsonl, compression=gzip`. Use `with compression=gzip` when the compressed file name is ambiguous or extensionless, e.g. `events.data with format=jsonl, compression=gzip`. Stdin (`-`) requires `with format=...`; gzip-compressed stdin must be explicit (`- with format=csv, compression=gzip`). Globs and extensionless paths cannot infer format at parse time — use `with format=...` before any CSV-only option (`header`, `delim`, `allow_jagged_rows`, `ignore_unknown_values`), e.g. `part-* with format=csv, allow_jagged_rows=true`. Use `with format=...` when a glob matches mixed or missing extensions at load time.

Avro and Parquet internal compression codecs are discovered from the file metadata. Do not use load options such as `compression=snappy`, `compression=deflate`, `compression=zstd`, or `compression=brotli` for Avro/Parquet; those are not query syntax. The `compression=` load option means a file-level wrapper and is currently limited to gzip-compressed CSV/JSON/JSONL text inputs, not `.avro.gz` or `.parquet.gz`.

---

## Syntax rules

Every operation uses one of three argument styles. The style is fixed per op — do not mix them.

Comma-separated syntaxes are strict: do **not** write trailing commas. This
applies to column lists, assignment/binding lists, function argument lists,
`list(...)` elements, and `struct(...)` fields. For example,
`upper(name,)`, `coalesce(a, b,)`, `select name,`, `list(1,)`, and
`struct(a = 1,)` are parse errors.

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
* A trailing comma is invalid: `select name,` and `sort age,` are parse errors.

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
* A trailing comma is invalid: `transform x = upper(name),` and `rename name=first_name,` are parse errors.

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
* Function arguments are comma-separated expressions. `func()`, `func(a)`, and
  `func(a, b)` are valid shapes; `func(a,)`, `func(,a)`, and `func(a,, b)` are
  parse errors.
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

### 1. `describe`

Return cheap metadata for the current materialized table. The output has one row per input column and three columns: `column`, `type`, and `row_count`.

```
dq 'users.csv | describe'
dq 'users.csv | filter { city == "NY" } | describe | json'
dq 'users.csv | describe | filter { type == "string" }'
```

Output:

```
column | type   | row_count
------ | ------ | ---------
name   | string | 6
age    | int    | 6
city   | string | 6
```

Rules:
* `describe` is a normal pipeline operation, not an output format command. It can be followed by `filter`, `select`, `sort`, output formats, etc.
* It takes no arguments.
* `row_count` repeats the current table row count on every metadata row.
* Type names are `null`, `int`, `float`, `string`, `bool`, `list`, and `record`.
* Types are current table storage types after preceding pipeline stages, not source-declared types and not historical types from earlier stages.
* If a column widened to `string` and rows survive a later rebuilding operation, copied values remain `string`.
* If no rows survive a rebuilding operation such as `filter { false }`, the rebuilt columns have no non-null values and report `null`.
* A zero-column table returns zero metadata rows. Use `count` if row cardinality must be visible for a zero-column table.
* Nested values are reported only at the top level as `list` or `record`; `describe` does not recursively expand nested schemas.

### 2. `head n`

Return the first `n` rows.

```
dq 'users.csv | head 10'
```

### 3. `tail n`

Return the last `n` rows.

```
dq 'users.csv | tail 5'
```

### 4. `sort [-]col1, [-]col2, ...`

Sort by columns (comma-separated). Ascending by default; prefix a column with `-` to sort it descending. Directions can be mixed per column.

```
dq 'users.csv | sort age, name'         // both ascending
dq 'users.csv | sort -created_at, id'   // created_at descending, id ascending
```

### 5. `select col1, col2, ...`

Project specific columns. All columns selected by default.

```
dq 'users.csv | select name, age'
```

### 6. `filter { expression }`

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

### 7. `group col1, col2, ... [as nested_name]`

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

### 11. `distinct [col1, col2, ...]`

Return unique rows. If columns are specified, deduplicates by those columns. If no columns are given, deduplicates by the entire row.

```
dq 'users.csv | distinct'                // unique rows
dq 'users.csv | distinct city'           // unique cities
dq 'users.csv | distinct city, age'      // unique combinations
```

### 12. `rename old=new [, old2=new2 ...]`

Rename one or more columns. Comma-separated `old=new` bindings.

```
dq 'users.csv | rename name=first_name'
dq 'users.csv | rename `first name`=first_name, `last name`=last_name'
```

### 13. `remove col1, col2, ...`

Remove columns from output.

```
dq 'users.csv | remove password, ssn'
dq 'users.csv | group name | reduce total = sum(amount) | remove grouped'
```

### 14. `join [kind] file on key [and key ...]`

Join with another file. Kind is optional: `inner` (default), `left`, `right`, `full`.

```
dq 'users.csv | join orders.csv on name == user_name'
dq 'users.csv | join left orders.csv on user_id'
dq 'users.csv | join full orders.csv on id == customer_id and region == region'
```

Each key is either a column path (same name on both sides) or `left_path == right_path`. Join key columns appear once under the left-side name; dot-path keys get a flattened column (`address.city` -> `address_city`, suffixed if taken). Colliding right-side columns are prefixed with the join file basename (for globs, derived from the pattern — e.g. `orders/*.csv` with colliding column `note` -> `orders_note`).

The join file's format comes from its extension unless overridden with `with format=...` on the join path. Join sources support globs (`orders/part-*.csv`); matched files are concatenated before the join. Null keys never match. Keys match by exact type and structural value (consistent with `group`/`distinct`), so integer `1` does not match string `"1"` across formats.

---

## Output format commands

Optional terminal stage after the pipeline. At most one per query; must be last (reject `| csv | head`).

```
output_cmd  ::= format [ "with" output_opts ] [ "to" path ]
format      ::= "table" | "csv" | "json" | "jsonl" | "avro" | "parquet"
output_opts ::= output_opt ( "," output_opt )*
output_opt  ::= ident "=" value
```

Omitted `output_cmd` → pretty **table** (same as `| table` for rendering).

```
dq 'users.csv | select name, age'
dq 'users.csv | select name, age | csv'
dq 'users.csv | count | json'
cat data.csv | dq '- with format=csv | count | jsonl'
dq 'users.csv | select name, age | csv to out/users.csv'
dq 'users.csv | json to out/'                           # out/output.json
dq 'users.csv | table to out/'                          # out/output.txt
dq 'big.csv | csv with split_rows=50000 to out/'         # out/output-1.csv, out/output-2.csv, ...
dq 'big.csv | parquet with split_rows=100000 to out/part-{n}.parquet'
```

Output stage rules:

* At most one output command per query; it must be last (reject `| csv | head` and `| csv to out.csv | head`).
* `to path` writes to a file or directory instead of stdout. When `to` is set, nothing is printed to stdout.
* Parent directories are created. Existing output files fail rather than being overwritten.
* If a file path has no extension, the format extension is appended (`| csv to out/users` → `out/users.csv`). `table` file output uses `.txt`.
* If a file path extension disagrees with the format command, the query fails (`| csv to out/users.json`).
* A destination is a directory only when the path ends with `/` or the platform path separator. Directory detection does not depend on whether a path already exists. A directory destination writes `output.<ext>` (`| parquet to out/` → `out/output.parquet`, `| table to out/` → `out/output.txt`).
* `with split_rows=N` writes row-bounded output parts. `N` must be a positive integer and requires `to`.
* `with overwrite=true` replaces existing output files. `overwrite` is optional, defaults to `false`, and requires `to path` because stdout output has nothing to overwrite.
* Split directory destinations write `output-1.<ext>`, `output-2.<ext>`, ...
* Split directory output owns the full `output-N.<ext>` sequence for that directory and format. A later run that produces fewer parts treats higher-numbered matching files as stale output: without `overwrite=true` they cause the write to fail; with `overwrite=true` they are removed as part of the staged commit and restored on rollback.
* Split file destinations must contain `{n}` as the part-number placeholder (`part-{n}.csv`, `chunk-{n}.jsonl`, etc.). `{n}` works for all split-capable formats.
* `table` output can be written to a single file, but `table with split_rows=...` is rejected by the writer after parsing/execution. Parser-level split validation checks generic output-shape rules (`to` is required, `N > 0`, and non-directory split file paths must contain `{n}`); the writer owns format-specific split support.
* File outputs are staged through temporary files in the destination directory and then committed with rename/link operations. Failed serialization should not leave corrupt final files or temporary files behind. Split output stages all parts before committing final paths.

Format-specific split behavior:

| Format | Split behavior |
|--------|----------------|
| `csv` | Header row is written in every part file |
| `json` | Each part is a standalone JSON array |
| `jsonl` | Each part is standalone JSONL |
| `avro` | Each part is a standalone Avro OCF file |
| `parquet` | Each part is a standalone Parquet file |

Output format names are not lexer keywords — only recognized as the final `|`-stage. Column names like `csv` in `{ csv == "x" }` are unaffected.

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
* `list(expr, ...)` — construct an ordered list value row-by-row. `list()` returns an empty list. Elements are positional, evaluated left-to-right, and null elements are preserved. Lists may contain mixed element types, including records from `struct(...)`.
* `list_len(xs)` — list element count (`TypeList` only)
* `list_contains(xs, x)` — true if list `xs` contains `x`, using strict structural equality (`TypeList` only for `xs`)
* `substr(s, start, length)` — substring by **0-based code point** index and length (`TypeString` only for `s`; `start` and `length` must be `TypeInt`); negative `start` counts from the end (Python-style); `length` must be non-negative
* `trim(s)` — remove whitespace (`TypeString` only)
* `coalesce(a, b, ...)` — first non-null value
* `if(cond, then, else)` — conditional; only explicit `true` takes then, `false` and `null` take else
* `struct(field = expr, ...)` — construct an ordered nested record. Field names are identifiers; use backticks for names with spaces or keywords such as `` `and` ``. `struct()` returns an empty record. Null field values are preserved; schema-based writers infer null-only field types from other rows, or fall back to nullable string when all values are null.
* `year(date)`, `month(date)`, `day(date)` — extract year, month, or day from a date string (`TypeString` only). Supported formats include `YYYY-MM-DD`, ISO timestamps, `YYYY-MM-DD HH:MM:SS`, and common slash-separated forms (see `engine/functions.go` `dateFormats`). Null input → null. Unparseable strings **error** and fail the query (same strict parse semantics as BigQuery `PARSE_DATE`, Trino `date_parse`, PostgreSQL `::date` — not silent null).

**String predicates (return booleans; usable in `filter` and `transform`; `TypeString` only):**
* `str_contains(s, sub)` — true if `s` contains substring `sub`
* `starts_with(s, prefix)` — true if `s` starts with `prefix`
* `ends_with(s, suffix)` — true if `s` ends with `suffix`
* `matches(s, regex)` — true if `s` contains a match for the (RE2) regular expression `regex` (unanchored; use `^...$` for full-string match)

Predicates match on **UTF-8 text** (contiguous substring or regex over the string bytes), not by code-point index. Only `str_len` / `substr` use code-point units.

Matching is **case-sensitive** (`"ERROR"` does not match `"error"`).

Null arguments produce null. In `filter`, a null result drops the row (same as `false`).

**Strict builtins — invalid content errors (not null):** Some functions accept the right type but reject invalid *content*. The query fails on the first bad row (in `filter` / `transform`), matching strict SQL engines (BigQuery/Trino default parse; PostgreSQL cast). This is intentional — unlike missing data (null in → null out) or arithmetic edge cases (e.g. division by zero → null).

| Function | Null input | Invalid content |
|----------|------------|-----------------|
| `year` / `month` / `day` | null | unparseable date string → error |
| `matches` | null | invalid RE2 regex → error |

Wrong *type* (e.g. `year(quantity)`, `matches(age, "x")`) also errors. For messy string columns, clean or filter upstream; opt-in `try_*` / `safe_*` helpers may be added later for the same pattern (Trino `TRY(...)`, BigQuery `SAFE.*`).

```
dq 'users.csv | transform name_len = str_len(name)'
dq 'nested.json | transform order_count = list_len(orders)'
dq 'nested.json | filter { list_len(orders) > 1 } | select name'
dq 'nested.json | filter { list_contains(tags, "admin") } | select name'
dq 'users.csv | transform profile = struct(name = name, age = age, meta = struct(source = "csv")) | select profile | json'
dq 'users.csv | transform tags = list("user", city, null), bundle = list(struct(name = name, age = age)) | select tags, bundle | json'
dq 'dates.csv | transform y = year(d)'   # "2024-13-99" → error, not null
```

```
dq 'logs.csv | filter { str_contains(upper(message), "ERROR") }'
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
