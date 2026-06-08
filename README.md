# dq

Query CSV, JSON, Avro, and Parquet files from the command line. Pipe operations together, like `jq` but for tables.

```bash
dq 'users.csv | filter { age > 25 } | select name city | sorta name'
dq -o csv 'users.json | filter { age > 25 }' > filtered.csv
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

Every query starts with a file and pipes it through operations. Each operation takes a table in and returns a table out.

```
dq 'file.csv | operation1 | operation2 | ...'
```

Wrap queries in single quotes so your shell doesn't interpret `|`, `{`, `}`, or `>`.

### Reading from stdin

Use `-` as the source to read from a pipe, or omit the query when stdin is piped. The `-f` flag is required (csv, json, or jsonl):

```bash
cat users.csv | dq -f csv
cat users.csv | dq -f csv '- | filter { age > 25 } | select name'
curl -s https://api.example.com/users | dq -f json '- | head 10'
```

Rebuild after pulling: `go build -o dq ./cmd/dq`

Avro and Parquet are not supported on stdin (they require seekable files).

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
dq 'users.csv | select name age city'
dq 'data.json | select name address.city'        # nested field -> column "address_city"
```

### `remove` - Drop columns you don't need

```bash
dq 'users.csv | remove password ssn'
```

### `filter` - Keep rows that match a condition

Expressions go inside `{ }`. Use `==` for equality, `and`/`or` for logic.

```bash
dq 'users.csv | filter { age > 25 }'
dq 'users.csv | filter { age > 25 and city == "NY" }'
dq 'users.csv | filter { email is not null }'
dq 'data.json | filter { address.city == "NY" }'    # nested field access
```

### `sorta` / `sortd` - Sort rows ascending or descending

```bash
dq 'users.csv | sorta age'          # youngest first
dq 'users.csv | sortd age'          # oldest first
dq 'users.csv | sorta city age'     # sort by city, then age
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
dq 'users.csv | distinct city age'    # unique city+age combinations
```

### `rename` - Rename columns

Names are paired: old then new. Use backticks for column names with spaces.

```bash
dq 'users.csv | rename name username'
dq 'users.csv | rename `first name` first_name `last name` last_name'
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
dq 'users.csv | group city department'       # group by multiple columns
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
dq 'sales.csv | group category | reduce total = sum(price), n = count() | remove grouped | sortd total | head 3'
```

## Functions

**For `reduce`** (aggregate across rows):
`count()`, `sum(col)`, `avg(col)`, `min(col)`, `max(col)`, `first(col)`, `last(col)`

**For `transform`** (compute per row):
`upper(s)`, `lower(s)`, `len(s)`, `trim(s)`, `substr(s, start, len)`, `coalesce(a, b, ...)`, `if(cond, then, else)`, `year(d)`, `month(d)`, `day(d)`

**Operators** (work everywhere):
`+`, `-`, `*`, `/`, `==`, `!=`, `<`, `>`, `<=`, `>=`, `and`, `or`, `not`

## Nested Fields

JSON, Avro, and Parquet files can contain nested records. Use dot notation to access sub-fields in `filter`, `transform`, `select`, and `group`:

```bash
dq 'data.json | filter { address.city == "Chicago" }'
dq 'data.json | transform city = address.city | select name city'
dq 'data.json | filter { profile.stats.logins > 10 }'
dq 'data.json | select name address.city'                          # -> columns: name, address_city
dq 'data.json | group address.city | reduce n = count() | remove grouped'
```

Dot paths in `select` and `group` flatten to underscore-separated column names (e.g., `address.city` becomes `address_city`). If a column with that name already exists, a numeric suffix is added (`address_city_2`).

Missing sub-fields return null.

## Output Formats

By default `dq` prints a pretty ASCII table. Use `-o` to change the output format:

```bash
dq 'users.csv | select name age'                        # table (default)
dq -o csv  'users.csv | select name age' > out.csv      # CSV
dq -o json 'users.csv | select name age' > out.json     # JSON array of objects
dq -o jsonl 'users.csv | select name age' > out.jsonl   # one JSON object per line
```

| Format  | Flag          | Notes                                                    |
|---------|---------------|----------------------------------------------------------|
| `table` | default       | Pretty-printed ASCII table                               |
| `csv`   | `-o csv`      | Standard CSV. Nulls render as empty strings.             |
| `json`  | `-o json`     | JSON array. Preserves types (ints, bools, nulls, nested).|
| `jsonl` | `-o jsonl`    | One JSON object per line. Same type preservation as JSON. |

## Supported Input Formats

CSV (`.csv`), JSON (`.json`), JSONL (`.jsonl`), Avro (`.avro`), Parquet (`.parquet`)

### CSV type inference

CSV cells are parsed as int, float, bool, or string. When a column contains mixed numeric types, it widens automatically: `int` + `float` stays float; adding a string widens the whole column to string (e.g. `1`, `2.5`, `something` all become strings). Once widened to string, use quoted literals in filters (`val == "1"`) rather than numeric comparisons (`val > 1`).

## License

MIT
