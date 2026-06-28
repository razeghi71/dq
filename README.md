# dq

Query CSV, JSON, Avro, and Parquet files from the command line. Pipe operations together, like `jq` but for tables.

```bash
dq 'users.csv | filter { age > 25 } | select name, city | sort name'
dq 'users.json | filter { age > 25 } | csv' > filtered.csv
```

## Install

**Homebrew:**

```bash
brew tap razeghi71/tap
brew install dq
```

**Go:**

```bash
go install github.com/razeghi71/dq/cmd/dq@latest
```

Or build from source:

```bash
git clone https://github.com/razeghi71/dq.git
cd dq
go build -ldflags="-s -w" -o dq ./cmd/dq
```

## How It Works

Every query starts with a file or stdin and pipes it through operations. Each operation takes a table in and returns a table out.

```
dq 'data.dat [with format=..., delim=..., ...] | operation1 | operation2 | ... [| output_format [with ...] [to path]]'
```

Wrap queries in single quotes so your shell doesn't interpret `|`, `{`, `}`, or `>`.

## Syntax Is Strict

`dq` does not guess when a query has extra tokens or unknown operators. Output format commands must be last, comma-separated lists cannot have trailing commas, and malformed expressions fail instead of running a shortened query.

```bash
dq 'users.csv | transform x = age * 2 | json'      # valid
dq 'users.csv | transform x = age % 2 | json'      # error: % is not an operator
dq 'users.csv | transform x = upper(name,) | json' # error: trailing comma
dq 'users.csv | csv | head 1'                      # error: output format is not last
```

## MCP / AI Agents

`dq` can run as a local MCP server for agent tools that support stdio MCP:

```bash
dq mcp
```

Example host config:

```json
{
  "mcpServers": {
    "dq": {
      "command": "dq",
      "args": ["mcp"]
    }
  }
}
```

The MCP server exposes one tool, `query`, which takes the same query string you would pass on the command line:

```json
{
  "query": "users.csv | describe | json"
}
```

Use normal query syntax for inspection, filtering, joins, output formats, and file writes. To print this quick guide from the CLI, run:

```bash
dq -agent-guide
```

For the detailed language and design contract, see `AGENTS.md`.

## Type and Schema Basics

`type` is the top-level storage type: `int`, `float`, `string`, `bool`, `list`, `record`, `union`, or `null`. `schema` shows nested detail and nullability:

```bash
dq 'nested.json | describe | select column, type, schema'
# address -> record<city:string, street:string, zip:string>
# orders  -> list<record<amount:float, order_id:int, status:string>>
```

Nullable schema positions end in `?`. Missing JSON fields and explicit nulls both make the affected position nullable:

```bash
dq 'events.jsonl | describe | select column, schema'
# profile -> record<email:string?, id:int>
```

Pipeline operations keep known schemas even when no rows survive:

```bash
dq 'users.csv | filter { false } | describe'
# name -> string, row_count 0
# age  -> int,    row_count 0
```

Operations such as `filter`, `transform`, `group`, `reduce`, `select`, `sort`, `rename`, `remove`, `distinct`, `count`, and `describe` are planned against the current schema before rows run. Expressions are checked there too. Misspelled columns, missing fields in known records, wrong function argument types, and unavailable columns from earlier projections fail even if the table currently has zero rows:

```bash
dq 'users.csv | filter { agge > 20 }'                         # error: column "agge" not found
dq 'users.csv | filter { false } | transform x = upper(age)'  # error: upper() needs string
dq 'nested.json | select address.missing'                     # error: field not found
dq 'users.csv | distinct city | select age'                   # error: age was projected away
dq 'users.csv | transform age2 = age + 1 | select age2'       # age2 is available downstream
dq 'users.csv | group city | reduce n = count() | select n'   # reduce output is available downstream
```

When reading a single file source, a simple leading `select` and/or `filter` can reduce memory use on wide files:

```bash
dq 'wide.csv | select id, status | json'
dq 'events.jsonl | filter { status == "active" } | select id | json'
dq 'wide.csv | filter { status == "active" } | select id | json'
dq 'wide.csv | select id, status | filter { status == "active" } | json'
```

For columns the query actually reads, rows and values match the unoptimized pipeline. This optimization only applies to the leading simple prefix; later `select` operations are not pushed across a non-simple filter such as `filter { id + 1 > 0 }`. Source-wide structure errors, such as duplicate CSV headers or malformed row widths, are still reported. After schema inference, values are checked for columns used by the source output or pushed filter; truly unused columns may not be validated and are not materialized into the output table for that optimized prefix. JSON/JSONL inputs still parse each logical record with the JSON decoder, but projected-away fields are not validated into table columns.

Each pipeline stage sees the columns produced by previous stages. Within one `transform` or `reduce`, assignment target names must be unique and all right-hand sides see the input schema only:

```bash
dq 'users.csv | transform age2 = age + 1 | filter { age2 > 20 }'
dq 'users.csv | transform age2 = age + 1, age3 = age2 + 1'    # error: age2 not visible yet
dq 'users.csv | group city | reduce x = count(), x = sum(age)' # error: duplicate x target
```

Numeric types promote from `int` to `float` when needed for schemas and arithmetic. Planned expression evaluation applies those schemas before runtime comparison, so an inline expression such as `coalesce(id, 0.0)` behaves the same as materializing it with `transform` first. Comparisons and `list_contains` allow `int`/`float` pairs, but compare the exact represented numeric values rather than rounding integers through `float64`. If either top-level comparison operand is null, the result is null; use `is null` / `is not null` for definite null checks. Integer `+`, `-`, `*`, `sum(int)`, and unary `-` fail on `int64` overflow. Division `/` is always float-valued, so very large integers can lose precision when divided; division by zero returns null. Heterogeneous values inside one JSON/list value are preserved as `mixed`, such as `[1, "two"] -> list<mixed>`. Outside that explicit list heterogeneity case, incompatible native JSON types are bad records instead of silently becoming strings.

Avro unions with incompatible branches load as `union<...>` from file metadata, even for empty files:

```bash
dq 'events.avro | describe | select column, type, schema'
# u -> union, union<int,string>
# maybe_u -> union, union<int,string>?
```

Union values output as their active branch value in table, CSV, JSON, and JSONL. `group`, `distinct`, and `join` keys use the active value's exact type and structural key, so integer `7` is different from string `"7"`. Avro branch names/tags are not stored today; two named Avro branches with the same dq schema are indistinguishable after loading. Direct ordering or comparison on a union column is intentionally rejected.
Recursive Avro named records are rejected with a clear load error because `dq` schemas cannot represent recursive types yet.

## Operations

### `describe` - Show columns, types, row count, and schema

Returns one metadata row per column: `column`, `type`, `row_count`, and `schema`.

```bash
dq 'users.csv | describe'
dq 'users.csv | filter { city == "NY" } | describe | json'
dq 'users.csv | describe | filter { type == "string" }'
dq 'nested.json | describe | select column, schema'
```

### `head` / `tail` - Get rows from the start or end

```bash
dq 'users.csv | head'          # first 10 rows (default)
dq 'users.csv | head 20'       # first 20 rows
dq 'users.csv | tail'          # last 10 rows (default)
dq 'users.csv | tail 5'        # last 5 rows
```

### `select` - Keep only the columns you want

```bash
dq 'users.csv | select name, age, city'
dq 'data.json | select name, address.city'        # nested field -> column "address_city"
```

### `remove` - Drop columns you don't need

`remove` drops top-level columns only; it does not delete nested fields inside records.

```bash
dq 'users.csv | remove password, ssn'
dq 'nested.json | remove address.city'   # error: remove only accepts top-level columns
```

### `filter` - Keep rows that match a condition

Expressions go inside `{ }`. Use `==` for equality, `and`/`or` for logic.

```bash
dq 'users.csv | filter { age > 25 }'
dq 'users.csv | filter { age > 25 and city == "NY" }'
dq 'users.csv | filter { email is not null }'
dq 'data.json | filter { address.city == "NY" }'    # nested field access
```

### `sort` - Sort rows

Ascending by default. Prefix a column with `-` to sort it descending. Mix directions across multiple columns in one `sort`. Sort keys must be orderable: `int`, `float`, or `string`. Nulls sort last.

```bash
dq 'users.csv | sort age'              # youngest first (ascending)
dq 'users.csv | sort -age'             # oldest first (descending)
dq 'users.csv | sort city, age'        # city ascending, then age ascending
dq 'users.csv | sort city, -age'       # city ascending, then age descending
```

### `count` - Count how many rows

```bash
dq 'users.csv | count'
dq 'users.csv | filter { age > 30 } | count'
```

### `distinct` - Remove duplicate rows

```bash
dq 'users.csv | distinct'             # unique rows
dq 'users.csv | distinct city'        # one-column table of unique cities
dq 'users.csv | distinct city, age'    # unique city+age pairs only
```

### `rename` - Rename columns

Use `old=new` bindings, comma-separated. Whitespace around `=` is optional. Backticks for column names with spaces.

```bash
dq 'users.csv | rename name=username'
dq 'users.csv | rename name = username'
dq 'users.csv | rename `first name`=first_name, `last name`=last_name'
```

### `transform` - Create or overwrite columns with computed values

Assignments are comma-separated. Works row by row.

```bash
dq 'users.csv | transform age2 = age * 2'
dq 'users.csv | transform name = upper(name), age_months = age * 12'
dq 'sales.csv | transform total = coalesce(quantity, 0) * coalesce(price, 0)'
```

Later stages in the same pipeline see transformed columns:

```bash
dq 'users.csv | transform age2 = age + 1 | filter { age2 > 30 } | select name, age2'
```

Assignment target names must be unique within one `transform`. Right-hand sides see the input table for that `transform`, not sibling assignments:

```bash
dq 'users.csv | transform age2 = age + 1, age3 = age2 + 1'  # error unless age2 already existed
```

### `group` - Group rows by column values

Collects rows that share the same value(s) into groups. The non-grouped columns are nested into a column called `grouped` (or a custom name with `as`).

```bash
dq 'users.csv | group city'
```

```
city | grouped
---- | -------------------------
NY   | [ {name:alice,age:30,city:NY}, {name:bob,age:25,city:NY} ]
LA   | [ {name:carol,age:28,city:LA} ]
```

All original columns (including group keys) are preserved in the nested records.
The nested column name must be distinct from the output group-key columns. If your key is named `grouped`, choose a nested name explicitly.

```bash
dq 'users.csv | group city as people'       # custom nested column name
dq 'events.csv | group grouped as rows'      # avoid colliding with the key output
dq 'users.csv | group city, department'       # group by multiple columns
dq 'data.json | group address.city'          # group by nested field -> key column "address_city"
```

### `reduce` - Aggregate over grouped rows

Runs aggregation functions (`sum`, `avg`, `count`, etc.) over the nested rows created by `group`. The nested column is kept after reduction -- use `remove` to drop it.

```bash
dq 'users.csv | group city | reduce avg_age = avg(age), n = count()'
```

```
city | grouped                                                | avg_age | n
---- | ------------------------------------------------------ | ------- | -
NY   | [ {name:alice,age:30,city:NY}, {name:bob,age:25,city:NY} ] | 27.5    | 2
LA   | [ {name:carol,age:28,city:LA} ]                        | 28      | 1
```

By default `reduce` operates on the column named `grouped`. If you used a custom name with `group ... as`, or if you have a pre-existing list column (e.g. from a Parquet/JSON file), pass the column name as the first argument:

```bash
dq 'users.csv | group city as people | reduce people avg_age = avg(age)'
dq 'orders.parquet | reduce orders total = sum(amount), n = count()'
```

Reduce expressions are checked against the nested row schema before groups run, and later pipeline stages are checked against the reduced schema before aggregate rows execute. Aggregate arguments must be column paths from the nested rows, so mistakes fail even when there are no groups:

```bash
dq 'users.csv | filter { false } | group city | reduce bad = upper(name)' # error
dq 'users.csv | group city | reduce x = count(), x = sum(age)'            # error
dq 'users.csv | group city | reduce n = count() | select missing'         # error before reduce runs
dq 'users.csv | filter { false } | group city | reduce total = sum(age)'  # valid
```

### `group` + `reduce` - Putting them together

```bash
# average age per city, clean output
dq 'users.csv | group city | reduce avg_age = avg(age) | remove grouped'

# total revenue per category, top 3
dq 'sales.csv | group category | reduce total = sum(price), n = count() | remove grouped | sort -total | head 3'
```

### `join` - Combine two files

Join the current table with another file. Kind is optional: `inner` (default), `left`, `right`, or `full`.

```bash
dq 'users.csv | join orders.csv on name == user_name'
dq 'users.csv | join left orders.csv on name == user_name'
dq 'users.csv | join full orders.csv on id == customer_id and region == region'
dq 'users.csv | join orders.dat with format=csv, delim=";" on user_id'
```

Join keys can use dot paths for nested fields; a dot-path key gets its own flattened output column (`address.city` -> `address_city`, suffixed with `_2` if taken). Dot-path keys must exist in the current schemas, so a misspelled key such as `address.missing` fails instead of producing zero matches. If both tables share a column name, the right table's column is prefixed with the join file's basename (e.g. `orders_amount` from `orders.csv`).

Notes:

- Null keys never match (rows with null keys still appear in left/right/full joins, with the other side null).
- Outer-join schemas reflect possible null padding even when no rows survive: left/full joins make right-side output columns nullable, and right/full joins make non-key left-side output columns nullable. Merged join keys keep their key schema.
- Key schemas are checked before rows run. The two sides must have the same dq type shape except for nullability: `string` and `string?` are compatible, but `int` vs `string`, `int` vs `float`, and `mixed` key schemas are errors. Matching record, list, and ordered union schemas are allowed.
- Matching rows use exact structural values, consistent with `group` and `distinct`: integer `1` is different from string `"1"` and from float `1.0`. Avro branch names/tags are not part of the key.
- `join` participates in upfront planning with later commands, so downstream mistakes such as `select missing` are reported before earlier row-time expression errors execute.

## Functions

**`reduce`** — aggregate over nested rows:
`count()`, `sum(col)` / `avg(col)` for numeric columns, `min(col)` / `max(col)` for orderable columns (`int`, `float`, `string`), `first(col)`, `last(col)`

**`transform`** — per-row values:

Strings — `upper(s)`, `lower(s)`, `trim(s)`, `substr(s, start, length)`, `str_len(s)`, `str_contains(s, sub)`, `starts_with(s, prefix)`, `ends_with(s, suffix)`, `matches(s, regex)`

Lists — `list(expr, ...)`, `list_len(xs)`, `list_contains(xs, x)`

General — `coalesce(a, b, ...)`, `if(cond, then, else)`, `struct(field = expr, ...)`

Dates — `year(d)`, `month(d)`, `day(d)`

Indexes are 0-based. `matches()` uses RE2 regex (unanchored by default; use `^...$` for a full-string match).
Function names are case-sensitive, and aggregate functions are only valid in `reduce`.

**Operators** — in any expression:
`+`, `-`, `*`, `/`, `==`, `!=`, `<`, `>`, `<=`, `>=`, `and`, `or`, `not`

```bash
dq 'users.csv | transform name = upper(name), name_len = str_len(name)'
dq 'nested.json | transform n = list_len(orders)'
dq 'sales.csv | transform total = coalesce(qty, 0) * price, y = year(date)'
dq 'users.csv | transform profile = struct(name = name, age = age)'
dq 'users.csv | transform tags = list("user", city, null)'
dq 'logs.csv | filter { str_contains(upper(message), "ERROR") }'
dq 'logs.csv | filter { starts_with(level, "WARN") }'
dq 'access.csv | filter { ends_with(path, ".json") }'
dq 'logs.csv | filter { matches(message, "timeout|refused") }'
dq 'users.csv | transform x = Upper(name)' # error: unknown function "Upper"
dq 'users.csv | transform n = count()'     # error: count is reduce-only
```

## Nested records

Nested objects from JSON, Avro, or Parquet. Use dot paths in `filter`, `transform`, `select`, and `group`:

```bash
dq 'data.json | filter { address.city == "Chicago" }'
dq 'data.json | transform city = address.city | select name, city'
dq 'data.json | select name, address.city'                    # -> column address_city
dq 'data.json | group address.city | reduce n = count() | remove grouped'
dq 'data.json | describe | select column, schema'             # inspect nested field types
```

Use `struct(field = expr, ...)` in expressions to build nested records row-by-row:

```bash
dq 'users.csv | transform profile = struct(name = name, age = age, meta = struct(source = "csv")) | select profile | json'
dq 'users.csv | transform empty = struct(), nullable = struct(a = null)'
```

Struct field names are identifiers; use backticks for names with spaces or keywords such as `` `and` ``. `select`, `group`, and dot-path join keys flatten dot paths to underscore names (`address.city` → `address_city`). Missing fields in a known record schema are errors; nullable existing fields still return null for rows where the parent or field is null. Null fields inside constructed records are preserved; schema-based writers infer their concrete field type from other non-null values, or fall back to nullable string when every value is null.

Dot paths cannot step through a union branch because different rows may hold different branch shapes. Select the union column itself, or normalize it first once branch-specific helpers exist.

## List columns

JSON/Avro/Parquet arrays load as **lists**.

```bash
dq 'users.csv | transform tags = list("user", city, null)'
dq 'users.csv | transform bundle = list(struct(name = name, age = age)) | select bundle | json'
dq 'nested.json | transform n = list_len(orders) | select name, n'
dq 'nested.json | filter { list_len(tags) >= 2 } | select name'
dq 'nested.json | filter { list_contains(tags, "admin") } | select name'
dq 'nested.json | reduce orders total = sum(amount) | select name, total'
```

Use `list(expr, ...)` to construct list values row-by-row. `list()` returns an empty list; it can adopt a typed list context such as `if(cond, orders, list())` or `coalesce(orders, list())`, and standalone `list()` describes as `list<string?>`. Null elements are preserved. Lists and records use structural equality; numeric `int`/`float` pairs compare by exact represented value. Use `list_contains(xs, x)` for membership and `list_len(xs)` for size checks; `tags == "admin"` and `tags == 1` error on type mismatch, and `filter { tags }` is an error.

## Output Formats

By default `dq` prints a pretty ASCII table. Append a terminal format command to change how results are written:

```bash
dq 'users.csv | select name, age'                        # table (default)
dq 'users.csv | select name, age | table'                  # explicit table
dq 'users.csv | select name, age | csv' > out.csv          # CSV
dq 'users.csv | select name, age | json' > out.json        # JSON array of objects
dq 'users.csv | select name, age | jsonl' > out.jsonl      # one JSON object per line
dq 'users.csv | select name, age | avro' > out.avro        # Avro object container file
dq 'users.csv | select name, age | parquet' > out.parquet  # Parquet file
```

Use `to` to write files from inside the query. Parent directories are created, and existing files are not overwritten unless you opt in:

```bash
dq 'users.csv | select name, age | csv to out/users.csv'
dq 'users.csv | select name, age | csv with overwrite=true to out/users.csv'
dq 'users.csv | group city | reduce n = count() | remove grouped | parquet to reports/by-city.parquet'
dq 'users.csv | json to out/'              # writes out/output.json
dq 'users.csv | table to out/'             # writes out/output.txt
dq 'users.csv | csv to out/users'          # writes out/users.csv
```

A destination is treated as a directory only when it ends with `/` or the platform path separator. Directory output uses `output.<ext>`; table output uses `.txt`.

Use `split_rows` for multiple output files. A directory destination uses `output-1.ext`, `output-2.ext`, ...; use `{n}` in the path for custom names:

```bash
dq 'big.csv | csv with split_rows=50000 to out/'                  # out/output-1.csv, ...
dq 'big.csv | parquet with split_rows=100000 to out/part-{n}.parquet'
```

Output format commands (`table`, `csv`, `json`, `jsonl`, `avro`, `parquet`) must be the last stage in the query — nothing may follow except end of query.

| Format  | Command   | Notes                                                    |
|---------|-----------|----------------------------------------------------------|
| `table` | *(default)* or `\| table` | Pretty-printed ASCII table                   |
| `csv`   | `\| csv`  | Standard CSV. Nulls render as empty strings.             |
| `json`  | `\| json` | JSON array. Preserves types (ints, bools, nulls, nested).|
| `jsonl` | `\| jsonl`| One JSON object per line. Same type preservation as JSON. |
| `avro`  | `\| avro` | Avro object container file. Field names must match `[A-Za-z_][A-Za-z0-9_]*`. Requires at least one output column and currently rejects `union` schemas. |
| `parquet` | `\| parquet` | Parquet file. Requires at least one output column, currently rejects `union` schemas, and preserves column order via file metadata. |

Avro and Parquet outputs use the result table schema, so empty results still carry selected column types:

```bash
dq 'users.csv | filter { false } | select name, age | parquet to empty.parquet'
dq 'empty.parquet | describe'
```

## Supported Input Formats

CSV (`.csv`), JSON (`.json`), JSONL (`.jsonl`), Avro (`.avro`), Parquet (`.parquet`)

Gzip-, Zstandard-, and zlib-wrapped deflate-compressed CSV/JSON/JSONL inputs work by suffix, or with an explicit load option:

```bash
dq 'data.csv.gz | head 5'
dq 'data.dat with format=csv, compression=gzip | count'
dq 'events.jsonl.zst | filter { level == "ERROR" }'
dq 'events.data with format=jsonl, compression=zstd | count'
dq 'metrics.csv.zlib | filter { value > 0 }'
dq 'events.data with format=jsonl, compression=deflate | count'
```

Avro and Parquet internal compression codecs are read from the file metadata. The `compression=` load option is only for file-level gzip/zstd/deflate wrappers on CSV/JSON/JSONL, not `.avro.gz`, `.avro.zst`, `.avro.deflate`, `.parquet.gz`, `.parquet.zst`, or `.parquet.deflate`.

If the extension isn't clear, add `with format=...` after the source — or after a join file:

```bash
dq 'data.dat with format=csv | head 5'
cat users.csv | dq '- with format=csv | count'
dq 'users.csv | join orders.dat with format=csv, delim=";" on user_id'
```

JSON and JSONL carry native types, including nested records and lists. `dq` preserves those nested schemas and reports them through `describe`:

```bash
dq 'nested.json | describe | select column, schema'
# address -> record<city:string, street:string, zip:string>
# orders  -> list<record<amount:float, order_id:int, status:string>>
```

Mixed numeric JSON values promote from int to float. Heterogeneous values inside a single JSON array are preserved and described as `mixed`, for example `[1, "two"]` has schema `list<mixed>`.
Outside that single-array `mixed` case, incompatible native JSON types are bad records instead of silent string widening, including nested fields such as `s.x` or cross-row typed-list conflicts such as `orders[].amount`.
Avro and Parquet are schema-bound readers. They seed table schemas from file metadata, including empty files with columns. Avro unions with incompatible non-null branches seed `union<...>` schemas and preserve active branch values by dq value type and structure; compatible numeric unions still collapse to `float`, and structurally identical named branches collapse because Avro branch tags are not represented. Recursive Avro named records are rejected because `dq` schemas cannot represent recursive types yet. Parquet has no equivalent union type in `dq` today.

JSON/JSONL schema inference samples the first 20480 logical records by default. Use `infer_rows=-1` when late sparse fields matter more than startup cost, and `max_bad_records=N` to skip a limited number of malformed or schema-incompatible records:

```bash
dq 'events.jsonl with infer_rows=-1 | describe'
dq 'events.jsonl with infer_rows=1000, max_bad_records=10 | count'
```

If a bounded sample does not reach the end of the source, known JSON/JSONL schema positions are reported nullable because later records may contain nulls or missing fields. Late fields outside the sampled schema are still bad records; they are not silently dropped.

### CSV type inference

CSV has no native column types, so `dq` infers them from the first 20480 data rows by default:

```bash
dq 'sales.csv | describe'
dq 'sales.csv with infer_rows=-1 | describe'      # scan all rows before choosing types
dq 'ids.csv with infer_rows=0 | json'             # strings for non-null cells; empty/null stay null
```

Inference chooses the narrowest type that covers the sampled values: ints stay ints, int+float becomes float, and mixed string/numeric values become string. If a column has no sampled non-null values, including a header-only CSV, it is treated as string. After inference, later rows must fit the chosen type. A mismatch fails the load by default:

```bash
dq 'sales.csv with max_bad_records=10 | count'    # skip up to 10 bad rows
```

When a bounded CSV inference sample does not reach the end of the file, column schemas are reported nullable because later rows may contain empty or `null` cells. Use `infer_rows=-1`, or a large enough sample to reach EOF, when you need exact nullability in `describe` or schema-based writers.

`max_bad_records` skips whole rows, not individual cells. CSV row-width errors are still controlled separately with `allow_jagged_rows=true` and `ignore_unknown_values=true`.

### Glob patterns

Primary sources and join files support shell-style globs, including recursive `**`:

```bash
dq 'logs/**/*.csv | filter { level == "ERROR" } | count'
dq 'users.csv | join left orders/part-*.csv on user_id'
```

- Patterns are matched relative to the current working directory.
- Matched files are loaded and concatenated.
- CSV glob shards follow the CSV header rules below; compatible extended headers create a column union and missing values are null.
- CSV header names must be unique; duplicate header names fail at load time.
- JSON/JSONL glob shards use one sampled schema in deterministic path order. Fields first seen after the sample are bad records; use `with format=jsonl, infer_rows=-1` (or a larger `infer_rows`) when sparse late fields should be part of the schema.
- Matched paths are sorted lexicographically (use zero-padded partition names like `part-001` for correct order).
- CSV shards after the first: repeated headers are skipped; reordered or extended headers are detected when the first row is clearly a header (shared column names, new lowercase identifiers such as `email`, not `Email`). Otherwise rows are read positionally under the first file's columns.
- Positional shards: values map to the first file's columns by position. CSV row-width rules match single-file loading (strict by default): use `with format=csv, allow_jagged_rows=true` and/or `with format=csv, ignore_unknown_values=true` on globs (format is required at parse time for CSV-only options).
- Renamed columns with no overlap with the first file's header (e.g. `user_id` vs anchor `id`) are read positionally, not by name.
- Literal paths with `[` (e.g. `data[1].csv`) are not globs unless `*`, `?`, or `{` is present.
- All matched files are loaded into memory before the pipeline runs.
- Single-file sources can avoid storing unneeded columns for an immediate simple `select`/`filter` prefix; glob sources currently load all columns before pipeline execution.
