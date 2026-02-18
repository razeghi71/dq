# dq

Query CSV, JSON, and Avro files from the command line. Pipe operations together, like `jq` but for tables.

```bash
dq 'users.csv | filter { age > 25 } | select name city | sorta name'
```

## Install

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
NY   | [ {name:alice,age:30}, {name:bob,age:25} ]
LA   | [ {name:carol,age:28} ]
```

```bash
dq 'users.csv | group city as people'       # custom nested column name
dq 'users.csv | group city department'       # group by multiple columns
```

### `reduce` - Aggregate over grouped rows

Runs aggregation functions (`sum`, `avg`, `count`, etc.) over the nested rows created by `group`. The nested column is kept after reduction -- use `remove` to drop it.

```bash
dq 'users.csv | group city | reduce avg_age = avg(age), n = count()'
```

```
city | grouped                                      | avg_age | n
---- | -------------------------------------------- | ------- | -
NY   | [ {name:alice,age:30}, {name:bob,age:25} ]   | 27.5    | 2
LA   | [ {name:carol,age:28} ]                      | 28      | 1
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

## Supported Formats

CSV (`.csv`), JSON (`.json`), Avro (`.avro`)

## License

MIT
