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
dq 'data.dat [with format=..., delim=..., ...] | operation1 | operation2 | ... [| output_format]'
```

Wrap queries in single quotes so your shell doesn't interpret `|`, `{`, `}`, or `>`.

## Operations

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

```bash
dq 'users.csv | remove password, ssn'
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

Ascending by default. Prefix a column with `-` to sort it descending. Mix directions across multiple columns in one `sort`.

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
dq 'users.csv | distinct city'        # unique cities (keeps first occurrence)
dq 'users.csv | distinct city, age'    # unique city+age combinations
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

```bash
dq 'users.csv | group city as people'       # custom nested column name
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

Join keys can use dot paths for nested fields; a dot-path key gets its own flattened output column (`address.city` -> `address_city`, suffixed with `_2` if taken). If both tables share a column name, the right table's column is prefixed with the join file's basename (e.g. `orders_amount` from `orders.csv`).

Notes:

- Null keys never match (rows with null keys still appear in left/right/full joins, with the other side null).
- Keys match by exact type and structural value, consistent with `group` and `distinct` -- e.g. integer `1` does not match string `"1"` across files of different formats.

## Functions

**`reduce`** — aggregate over nested rows:
`count()`, `sum(col)`, `avg(col)`, `min(col)`, `max(col)`, `first(col)`, `last(col)`

**`transform`** — per-row values:

Strings — `upper(s)`, `lower(s)`, `trim(s)`, `substr(s, start, length)`, `str_len(s)`, `str_contains(s, sub)`, `starts_with(s, prefix)`, `ends_with(s, suffix)`, `matches(s, regex)`

Lists — `list(expr, ...)`, `list_len(xs)`, `list_contains(xs, x)`

General — `coalesce(a, b, ...)`, `if(cond, then, else)`, `struct(field = expr, ...)`

Dates — `year(d)`, `month(d)`, `day(d)`

Indexes are 0-based. `matches()` uses RE2 regex (unanchored by default; use `^...$` for a full-string match).

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
```

## Nested records

Nested objects from JSON, Avro, or Parquet. Use dot paths in `filter`, `transform`, `select`, and `group`:

```bash
dq 'data.json | filter { address.city == "Chicago" }'
dq 'data.json | transform city = address.city | select name, city'
dq 'data.json | select name, address.city'                    # -> column address_city
dq 'data.json | group address.city | reduce n = count() | remove grouped'
```

Use `struct(field = expr, ...)` in expressions to build nested records row-by-row:

```bash
dq 'users.csv | transform profile = struct(name = name, age = age, meta = struct(source = "csv")) | select profile | json'
dq 'users.csv | transform empty = struct(), nullable = struct(a = null)'
```

Struct field names are identifiers; use backticks for names with spaces or keywords such as `` `and` ``. `select` and `group` flatten dot paths to underscore names (`address.city` → `address_city`). Missing sub-fields return null. Null fields inside constructed records are preserved; schema-based writers infer their concrete field type from other non-null values, or fall back to nullable string when every value is null.

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

Use `list(expr, ...)` to construct list values row-by-row. `list()` returns an empty list and null elements are preserved. Lists and records use exact structural equality. Use `list_contains(xs, x)` for membership and `list_len(xs)` for size checks; `tags == "admin"` and `tags == 1` error on type mismatch, and `filter { tags }` is an error.

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

Output format commands (`table`, `csv`, `json`, `jsonl`, `avro`, `parquet`) must be the last stage in the query — nothing may follow except end of query.

| Format  | Command   | Notes                                                    |
|---------|-----------|----------------------------------------------------------|
| `table` | *(default)* or `\| table` | Pretty-printed ASCII table                   |
| `csv`   | `\| csv`  | Standard CSV. Nulls render as empty strings.             |
| `json`  | `\| json` | JSON array. Preserves types (ints, bools, nulls, nested).|
| `jsonl` | `\| jsonl`| One JSON object per line. Same type preservation as JSON. |
| `avro`  | `\| avro` | Avro object container file. Field names must match `[A-Za-z_][A-Za-z0-9_]*`. Requires at least one output column. |
| `parquet` | `\| parquet` | Parquet file. Requires at least one output column. Column order is preserved via file metadata. |

## Supported Input Formats

CSV (`.csv`), JSON (`.json`), JSONL (`.jsonl`), Avro (`.avro`), Parquet (`.parquet`)

If the extension isn't clear, add `with format=...` after the source — or after a join file:

```bash
dq 'data.dat with format=csv | head 5'
cat users.csv | dq '- with format=csv | count'
dq 'users.csv | join orders.dat with format=csv, delim=";" on user_id'
```

### Glob patterns

Primary sources and join files support shell-style globs, including recursive `**`:

```bash
dq 'logs/**/*.csv | filter { level == "ERROR" } | count'
dq 'users.csv | join left orders/part-*.csv on user_id'
```

- Patterns are matched relative to the current working directory.
- Matched files are loaded and concatenated (column union; missing values are null).
- Matched paths are sorted lexicographically (use zero-padded partition names like `part-001` for correct order).
- CSV shards after the first: repeated headers are skipped; reordered or extended headers are detected when the first row is clearly a header (shared column names, new lowercase identifiers such as `email`, not `Email`). Otherwise rows are read positionally under the first file's columns.
- Positional shards: values map to the first file's columns by position. CSV row-width rules match single-file loading (strict by default): use `with format=csv, allow_jagged_rows=true` and/or `with format=csv, ignore_unknown_values=true` on globs (format is required at parse time for CSV-only options).
- Renamed columns with no overlap with the first file's header (e.g. `user_id` vs anchor `id`) are read positionally, not by name.
- Literal paths with `[` (e.g. `data[1].csv`) are not globs unless `*`, `?`, or `{` is present.
- All matched files are loaded into memory before the pipeline runs.
