## Query Language Overview

### Core structure

```
dq 'filename | op [args] | op2 [args] ... [| output_format]'
```

* The entire query is passed as a **single-quoted string** to avoid shell interpretation of `|`, `{`, `}`, `>`, `<`, and backticks.
* Takes a file (csv, avro, json, etc.) as input. Gzip-, Zstandard-, and zlib-wrapped deflate-compressed CSV/JSON/JSONL text files are supported via double extensions such as `.csv.gz` / `.jsonl.zst` / `.jsonl.deflate` or explicit `with compression=gzip` / `with compression=zstd` / `with compression=deflate`. CSV, JSON, and JSONL infer schemas from the first 20480 logical records by default; use `with infer_rows=-1` to infer from all rows, `with infer_rows=0` to load all non-null CSV cells as strings, and `with max_bad_records=N` to skip up to `N` bad logical records after or during inference. If bounded inference does not reach EOF, known CSV/JSON/JSONL schema positions are nullable so planning uses a sound upper bound on later nulls or missing fields; `infer_rows=-1` or a bounded sample that reaches EOF can keep exact nullability. `infer_rows=0` is invalid for JSON/JSONL. JSON and JSONL infer recursive schemas from native JSON values before materialization; compatible ints/floats promote to float, missing fields become nullable, and incompatible native types such as `{"s":{"x":1}}` followed by `{"s":{"x":"bad"}}` are bad records with a path such as `s.x`. Globs are supported (`logs/**/*.csv`, `orders/part-*.csv`) — matched files stream in deterministic expanded order after schema acquisition. A zero-byte CSV (or BOM-/whitespace-only with no data rows) loads as an empty table (0 columns, 0 rows). CSV header names must be unique. Empty glob shards are skipped when establishing the column schema; the first non-empty shard defines the anchor columns. CSV rows must match the schema width by default (same rules for single files and glob shards); use `with allow_jagged_rows=true` for missing trailing columns (null-filled) and `with ignore_unknown_values=true` to drop extra columns. On glob sources, CSV-only options require `with format=csv` at parse time (e.g. `logs/part-* with format=csv, allow_jagged_rows=true`) — format cannot be inferred from the pattern alone. CSV glob shards without a detectable header row are read positionally under the first file's columns. Extended headers require shared column names plus new lowercase identifiers (`email`, not `Email`); renamed columns with no anchor overlap are positional.
* Primary sources enter source-aware planning for CSV, JSON, JSONL, Avro, and Parquet when they are replayable files, globs, or CLI stdin text streams. Schema acquisition is the only format-specific work before typed planning: CSV/JSON/JSONL inspect the configured inference window, while Avro/Parquet read file metadata. Full source rows are streamed through row-wise physical operators after typed logical planning, optimization, and physical planning. If JSON/JSONL schema acquisition reaches EOF (including `infer_rows=-1`), replayable prepared sources retain and reuse the parsed logical records instead of parsing the file again at source stream time; stdin uses the live reader and must buffer the full logical input for `infer_rows=-1` because it cannot be rewound. Bounded stdin `format=json` array inference also retains the remaining array records after the sample so source-wide top-level array syntax can be validated before execution returns rows. The optimizer first may satisfy an adjacent source-safe `select`/`filter` prefix through source output columns and source predicates for any prepared single-file format; only top-level identity projections and source-safe predicates are eligible for scanner predicates. It then fuses adjacent typed `group | reduce` pairs over the group's nested column into a semantic `logicalGroupReduce` node. A later demand-pruning pass walks the logical pipeline backward and derives the minimum top-level source columns needed by the remaining operations. Unsupported filters are not reordered or pushed into scanner predicates, but their referenced columns remain demanded and are read. `sort` keys, join keys, group keys, live aggregate inputs, demanded join output columns, retained transform inputs, predicate inputs, selected outputs consumed by later operations, and output format demand all participate in that read set. `count` can demand zero data columns, so an intermediate `select bad_col | count` does not by itself require `bad_col` values to be validated or materialized. Join output names are planned from the current left-side schema before pruning, so public right-side collision names remain stable even when the collision-causing left column is not read. Use an explicit upstream `select` before a join when the public join schema itself should be narrowed. Adjacent fused `group | reduce` is a read-all barrier only when the original nested payload column remains demanded; otherwise it demands group key roots plus live aggregate argument roots. Standalone `group`, explicit-list `reduce`, full-row `distinct`, and `describe` remain conservative read-all barriers; right-hand join sources are still materialized read-all during planning. Single-file source pruning represents source columns as an explicit `AllColumns | SelectedColumns(names)` value; `SelectedColumns()` means read/output zero data columns and is distinct from all columns. The physical source read set is `output columns ∪ source-predicate columns`, preserving output order and appending predicate-only columns in source-schema order. Truly unreferenced late type/schema failures are not evaluated for a pruned single-file query; the same failure remains observable when the column is consumed by final output, filtered, joined, grouped, sorted, used by a live transform or reduce assignment, kept in a demanded group payload, or otherwise read. Dead transform and fused reduce assignments may be removed, so runtime errors from expressions whose outputs are not demanded are intentionally not evaluated; static planning/type errors still happen before optimization. JSON/JSONL currently still parse each demanded logical record before applying the read set, so pruning saves field schema validation/materialization but is not a selective JSON parser; late extra fields outside the read set may not be reported under non-consuming plans such as `count`. Glob and CLI stdin sources intentionally disable source column pruning and source predicate pushdown for now: they stream natively, but read all source columns so existing bad-record visibility is preserved. Source-wide structure errors that are independent of the read set, such as CSV duplicate headers and row-width errors discovered before rows can stream, remain observable. MCP stdin remains unsupported.
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
events.jsonl.zst | count
metrics.csv.zlib | head 5
data.dat with format=csv, compression=gzip | count
events.data with format=jsonl, compression=zstd | count
events.data with format=jsonl, compression=deflate | count
logs/part-* with format=csv | count
logs/part-* with format=csv, allow_jagged_rows=true, ignore_unknown_values=true | count
sales.csv with infer_rows=-1 | describe
ids.csv with infer_rows=0 | json
sales.csv with max_bad_records=10 | count
events.jsonl with infer_rows=-1 | describe
events.jsonl with infer_rows=1000, max_bad_records=10 | count
- with format=csv | filter { age > 25 }
- with format=csv, compression=gzip | count
- with format=jsonl, compression=zstd | count
- with format=jsonl, compression=deflate | count
users.csv | join left orders/part-*.dat with format=csv, delim=";" on user_id == customer_id
users.csv | join orders.data with format=csv, compression=gzip on user_id
users.csv | join orders.jsonl.zst with format=jsonl on user_id
users.csv | join orders.data with format=csv, compression=deflate on user_id
```

| Key | Applies to | Values | Notes |
|-----|------------|--------|-------|
| `format` | all | `csv`, `json`, `jsonl`, `avro`, `parquet` | Overrides extension; required when extension missing |
| `compression` | csv/json/jsonl inputs | `gzip`, `zstd`, `deflate` | File-level wrapper; inferred from `.csv.gz`, `.json.gz`, `.jsonl.gz`, `.csv.zst`, `.json.zst`, `.jsonl.zst`, `.csv.deflate`, `.json.deflate`, `.jsonl.deflate`, `.csv.zlib`, `.json.zlib`, `.jsonl.zlib`, and `.zstd`/`.zlib` equivalents; explicit compression also works for stdin when `format=...` is set; not supported for avro or parquet |
| `header` | csv | `true`, `false` | Default `true`; `false` uses `col1`, `col2`, … from first row width |
| `delim` | csv | string, e.g. `delim=";"` | Default `,`; only the first character is used as the separator |
| `allow_jagged_rows` | csv | `true`, `false` | Default `false`; when `true`, rows with fewer fields than the schema get null-filled trailing columns |
| `ignore_unknown_values` | csv | `true`, `false` | Default `false`; when `true`, extra fields beyond the schema are dropped |
| `infer_rows` | csv/json/jsonl | `-1` or integer `>= 0` | Default `20480`; number of data/logical records sampled for type/schema inference. `-1` scans all records; `0` is CSV-only and loads every non-null CSV cell as `TypeString`; `0` is invalid for JSON/JSONL |
| `max_bad_records` | csv/json/jsonl | integer `>= 0` | Default `0`; maximum number of bad logical records to skip. Each bad record skips the whole row/JSON record |

Format resolution: explicit `format=` → file extension → error (`use with format=...`). Literal gzip, zstd, and zlib-wrapped deflate double extensions infer both pieces: `.csv.gz` means `format=csv, compression=gzip`, `.jsonl.zst` means `format=jsonl, compression=zstd`, `.json.zstd` means `format=json, compression=zstd`, `.csv.deflate` means `format=csv, compression=deflate`, and `.jsonl.zlib` means `format=jsonl, compression=deflate`. `compression=deflate` means a zlib-wrapped deflate stream, not raw RFC1951 DEFLATE bytes. Use explicit `with compression=...` when the compressed file name is ambiguous or extensionless, e.g. `events.data with format=jsonl, compression=deflate`. Explicit `format=` overrides the inner suffix while suffix-based compression inference still applies, so `events.csv.deflate with format=jsonl` reads zlib-wrapped deflate JSONL. Stdin (`-`) requires `with format=...`; compressed stdin must be explicit (`- with format=csv, compression=gzip`, `- with format=jsonl, compression=zstd`, or `- with format=jsonl, compression=deflate`). Globs and extensionless paths cannot infer format at parse time — use `with format=...` before format-specific options when the format is not clear, e.g. `part-* with format=csv, allow_jagged_rows=true` or `part-* with format=jsonl, infer_rows=1000`. Use `with format=...` when a glob matches mixed or missing extensions at load time.

### CSV type inference and bad records

CSV cells are text, but `dq` loads CSV columns into typed table storage. The loader uses a DuckDB-style covering-type inference pass:

1. Establish the CSV column schema and collect data rows.
2. Infer each column from the first `infer_rows` data rows (`20480` by default). `infer_rows=-1` samples all data rows. `infer_rows=0` skips inference and makes every column `TypeString`.
3. Materialize every row against the fixed inferred schema.

Inference chooses the narrowest compatible type that covers all sampled non-null values:

| Sampled non-null values | Inferred type |
|-------------------------|---------------|
| no non-null values | `TypeString` |
| all ints | `TypeInt` |
| ints + floats, or all floats | `TypeFloat` |
| all booleans | `TypeBool` |
| strings mixed with anything | `TypeString` |
| booleans mixed with numeric values | `TypeString` |

This is not majority voting and not first-value strictness. If one sampled value is a string in an otherwise numeric-looking column, the column infers as `TypeString`.

Numeric-looking values are parsed as numbers under normal inference, so text identifiers such as `007` load as integer `7`. Use `infer_rows=0` when CSV values must remain strings, such as zip codes, account IDs, or other identifiers with leading zeros.

Columns with no sampled non-null values also infer as `TypeString`. This includes all-null CSV columns and header-only CSVs such as `a,b\n`: `describe` reports their top-level type as `string`, with `row_count` reflecting the actual number of data rows.

CSV inference has two separate facts: the base type proven by sampled non-null values, and the nullability proven by sample coverage. If `infer_rows` is bounded and the sample does not reach EOF, every known column schema is marked nullable because unsampled rows may contain empty cells or case-insensitive `null`. If `infer_rows=-1`, or a bounded sample reaches EOF, nullability is exact for the rows that exist. `infer_rows=0` makes every column `TypeString`; because it reads no data rows for type evidence, data-bearing sources report nullable string columns unless there are no data rows.

After inference, non-null values must parse as the inferred type. Empty cells and case-insensitive `null` remain null. A type-conversion failure is a bad record:

* `max_bad_records=0` (default) fails the load on the first bad record.
* `max_bad_records=N` skips up to `N` bad records; each bad record skips the whole row.
* If the next bad record exceeds the limit, loading fails with the source path (when available), physical CSV row number, column name, expected type, and offending value.
* `max_bad_records` does not apply to CSV row-width errors. Missing and extra fields are still controlled by `allow_jagged_rows=true` and `ignore_unknown_values=true`.
* On optimized source-aware single-file loads, post-inference value/schema validation is evaluated only for columns in the physical source read set. An invalid value in an unreferenced column does not count as a bad record for that pruned query. The same value is still a bad record when the column is selected into a consumed output, filtered, joined, grouped, sorted, used by a live transform assignment, used by a live fused reduce aggregate input, kept in a demanded group payload, described through a read-all barrier, or otherwise read by the physical plan. `count` does not demand data columns by itself, so `select bad_col | count` may read zero data columns after planning; use `select bad_col | json` or another consuming operation when the selected values must be materialized. This is intentional demand-driven execution: projection changes schema, while downstream consumption decides whether projected values are evaluated. Glob and CLI stdin sources currently read all source columns and keep the normal read-all bad-record behavior.

For glob CSV sources, inference samples data rows across the deterministic expanded-file order after repeated headers and empty shards are handled. Header rows do not count toward `infer_rows`.

### JSON and JSONL recursive schemas

JSON and JSONL values carry native types, so `dq` infers a recursive schema from sampled parsed values before materializing rows. This prevents native JSON type conflicts from being silently widened to strings.

JSON/JSONL inference and bad-record controls:

* `infer_rows` defaults to `20480` logical records. JSONL non-empty lines and JSON array elements count as logical records.
* `infer_rows=N` samples the first `N` logical records in deterministic source order. Bad records inside the sample window count toward the fixed window; skipped bad sample records are not replaced by later records.
* `infer_rows=-1` scans the full logical source before materialization, preserving full-source strict inference behavior.
* `infer_rows=0` is invalid for JSON/JSONL because JSON values already have native types. CSV's "all strings" mode does not apply.
* `max_bad_records=0` (default) fails on the first malformed or schema-incompatible logical record.
* `max_bad_records=N` skips up to `N` bad logical records. Skipping is whole-record: the entire JSONL line or JSON array element is omitted.
* Malformed JSONL lines count as bad records when line boundaries are recoverable. Malformed top-level JSON array syntax fails the whole JSON file.
* If bounded inference does not cover all logical records, every known schema position is marked nullable, recursively. For example, a sampled `record<x:int>` becomes `record<x:int?>?`, and `list<record<amount:int>>` becomes `list<record<amount:int?>?>?`. This is a planning contract: later explicit nulls or missing fields must not widen a schema that downstream operations were checked against.
* Fields first seen after the sampled JSON/JSONL schema is fixed are bad records, including nested fields such as `profile.email`; they are not silently dropped and do not widen the schema. Use `infer_rows=-1` or a larger sample when late sparse fields are expected.
* For JSON/JSONL globs, logical records are sampled across deterministic expanded-file order under one schema and one bad-record budget.

Schema inference rules:

| Values | Inferred schema |
|--------|-----------------|
| ints only | `int` |
| ints + floats | `float` |
| floats only | `float` |
| strings only | `string` |
| booleans only | `bool` |
| objects | `record<...>` with fields merged by name |
| arrays | `list<...>` with homogeneous elements merged recursively |
| heterogeneous values inside one array | `mixed` at the affected element or field |
| null / missing only | nullable string fallback when finalized |

Rules:
* JSON object fields match by name, not position. Field order in `schema` strings is deterministic by field name.
* Missing object fields and explicit nulls both materialize as `null` and mark the field nullable.
* Missing top-level columns also materialize as `null` for that row.
* Empty arrays do not determine element type. If every observed array is empty or null-only, the element type falls back to nullable string.
* Compatible nested numeric values promote recursively (`int` + `float` -> `float`).
* Heterogeneous values inside one JSON array are preserved instead of stringified. The affected schema position is `mixed`: `[1, "two"]` is `list<mixed>`, and `[{"amt":1}, {"amt":"x"}]` is `list<record<amt:mixed>>`.
* Outside the single-array `mixed` case, incompatible native types are bad records, not automatic string widening. Examples: the same field as `int` vs `string` across rows, a field as `record` vs `int` across rows, or `orders[].amount` as numeric in one row's typed list and string in another row's typed list.
* The `mixed` marker is limited to heterogeneity observed within a single array value. A typed array field that is `list<int>` in one JSON/JSONL row and `list<string>` in another row remains a schema conflict.
* JSON array bad-record errors report a row index such as `row 2`; JSONL bad-record errors report a line number such as `line 2`. Errors include the source path when available, nested path, expected schema, and actual schema where possible. When the bad-record limit is exceeded, the reported record is the record that exceeded the limit.
* This recursive schema and bad-record behavior is specific to JSON/JSONL loading. Avro and Parquet readers seed the table schema from file metadata, including empty files with columns. Avro unions with incompatible non-null branches seed `union<...>` schemas and preserve the active branch value for each row by dq value type and structure; compatible numeric Avro unions still collapse to `float`, and structurally identical named branches collapse because `table.TypeDescriptor` does not store Avro branch tags. Recursive Avro named records are rejected with a load error because `table.TypeDescriptor` has no recursive-reference node. Parquet currently has no equivalent union type in `dq`.

Examples:

```jsonl
{"s":{"x":1}}
{"s":{"y":"yes"}}
```

This loads as `schema = record<x:int?, y:string?>`.

```jsonl
{"xs":[1,"two"]}
```

This loads as `schema = list<mixed>` and preserves both the integer and string elements.

```jsonl
{"s":{"x":1}}
{"s":{"x":"bad"}}
```

This fails at load time with a path such as `s.x` because JSON number and string values are distinct native types.

Avro and Parquet internal compression codecs are discovered from the file metadata. Do not use load options such as `compression=snappy`, `compression=deflate`, `compression=zstd`, or `compression=brotli` for Avro/Parquet; those are not Avro/Parquet query syntax. The `compression=` load option means a file-level wrapper and is currently limited to gzip-, zstd-, or zlib-wrapped deflate-compressed CSV/JSON/JSONL text inputs, not `.avro.gz`, `.avro.zst`, `.avro.deflate`, `.parquet.gz`, `.parquet.zst`, or `.parquet.deflate`.

---

## Internal type and schema model

`table.TypeDescriptor` is the canonical logical type model. Display strings such as `record<x:int?>` are renderings of that model, not the model itself. Code that needs type reasoning should use the central helpers in `table/type_system.go` and `table/schema.go` instead of ad-hoc string comparisons or local kind checks.

Canonical type kinds:

| Kind | Meaning |
|------|---------|
| `TypeNull` | null-only / not yet determined |
| `TypeInt` | signed integer |
| `TypeFloat` | floating-point number |
| `TypeString` | UTF-8 string |
| `TypeBool` | boolean |
| `TypeList` | ordered list with an element schema |
| `TypeRecord` | named-field record |
| `TypeUnion` | ordered set of possible branch schemas; each row stores one active branch value |
| `TypeMixed` | schema marker for explicitly heterogeneous list contents |

Nullability is structural on every type node:

* top-level nullable int: `int?`
* nullable record with nullable field: `record<x:int?>?`
* list with nullable elements: `list<int?>`
* nullable list with non-null elements: `list<int>?`
* nullable union: `union<int,string>?`

The central helpers are intentionally split by purpose:

* `NormalizeSchema(t)` returns the deterministic canonical descriptor for dq's structural type system. Record fields are sorted; union branches are normalized but remain ordered because branch order is observable when multiple branches can accept a coerced value.
* `Render(t)` renders a deterministic compact schema string. Record fields render by field name.
* `Same(a, b)` / `EquivalentSchema(a, b)` compare logical type shape and nullability, ignoring record field order and pointer identity. Union branch order remains significant.
* `WithNullable(t)` and `WithoutNull(t)` return modified copies and must not mutate the input descriptor.
* `WithDeepNullable(t)` returns a modified copy with nullability enabled at every structural position. Use it when a bounded external-source inference window proves type shape but not absence of later nulls or missing fields.
* `UnionSchema(branches, nullable)` / `UnionOf(branches, nullable)` build ordered union descriptors, collapsing storage-equivalent branches. This includes storage-compatible scalar branches such as `int|float -> float` and structurally identical record/list branches; Avro branch names/tags are not retained in `TypeDescriptor`.
* `UnifySchemas(a, b, mode)`, `SchemaAssignable(target, actual, mode)`, and `CoerceValueToSchemaMode(v, schema, mode)` are the mode-based type-system APIs. Use strict/exact modes when declared schemas decide legality; use coercive modes only where storage compatibility is explicitly documented; use permissive mode only for legacy AddRow-style widening or for row values entering a schema that was itself produced by `UnifyPermissiveMode` / `MergeSchemasPermissive`.
* `IsNumeric`, `IsBooleanLike`, `IsStringLike`, `IsComparable`, and `IsOrderable` are predicate helpers for future planning/type checking.
* `UnifyStrict` and `UnifyAllStrict` merge compatible logical types and reject incompatible non-null types.
* `UnifyListLiteralElems` is the narrow helper that may produce `mixed` for explicitly heterogeneous list literals or single JSON array values.
* `NumericResult` returns arithmetic result types (`int + int -> int`, `int + float -> float`) while preserving nullable operands.
* `Table.Schema()` returns a logical `table.Schema` snapshot with cloned column descriptors; callers must not depend on physical column internals.

Strict unification rules:

| Inputs | Result |
|--------|--------|
| `int`, `int` | `int` |
| `int`, `float` | `float` |
| `float`, `float` | `float` |
| `null`, `T` | `T?` |
| matching records | fields unified by name, missing fields marked nullable |
| matching lists | element schema unified recursively |
| explicit union with matching branch | union schema is preserved |
| `int`, `string` | error |
| `bool`, `int` | error |
| `record<...>`, scalar | error |
| `list<int>`, `list<string>` | error under strict expression/schema unification |

`union` and `mixed` have different meanings. `union<...>` is a declared schema, currently produced by Avro unions, and each row stores an active branch value. `mixed` is a schema marker for explicitly heterogeneous contents inside a single list value, such as `[1, "two"] -> list<mixed>` or `[{"amt":1}, {"amt":"x"}] -> list<record<amt:mixed>>`. `mixed` must not become a top-level scalar escape hatch for incompatible expression branches: future checks for `if(cond, 1, "x")` or `coalesce(1, "x")` should use strict unification and reject or apply an explicitly documented rule.

Strict and permissive APIs must remain distinct:

* Loader inference and documented compatibility paths can still use permissive table append/widening. `Column.Append` may widen incompatible values to `string`.
* JSON/JSONL recursive loading uses strict schema checks for native type conflicts, with the single-array `mixed` exception described above.
* `AddRowTyped` validates against a known schema and must not stringify incompatible values.
* `TypeUnion` storage preserves each active branch value instead of stringifying it. JSON/JSONL/table/CSV outputs render the active value; `group`, `distinct`, and `join` keys use the active value's exact dq type and structural key, so `int(7)` and `string("7")` do not collide. They do not include Avro branch names/tags, so two named Avro record branches with the same dq schema and value are indistinguishable.
* Direct comparison and ordering over union-typed expressions are rejected unless an explicit future branch-normalization operation is added. Dot paths may project a union-typed field, but they must not step through a union branch such as `select u.x` when `u` is `union<record<x:int>,string>`.
* Engine operators that preserve or can plan their output schema use fixed-schema result tables and strict append for those planned columns. This includes `filter`, `select`, `group`, `count`, `describe`, `distinct`, `rename`, `remove`, typed `transform` assignments, typed `reduce` assignments, and joins with exact-compatible key schemas.
* Planned row-wise/schema operations (`head`, `tail`, `filter`, `transform`, `group`, `reduce`, `select`, `sort`, `rename`, `remove`, `distinct`, `count`, `describe`, and `join`) are planned as one schema-planned pipeline. For prepared primary sources, the phase split is `prepared source schema -> typed logical plan including source -> optimized logical IR -> physical plan including source bindings -> source stream or materialized source load -> streaming/materialized execution`. Typed logical planning validates operation-specific schema rules, type-checks expressions, loads join right-hand sources when needed, and computes each next schema without inspecting left-side rows beyond the prepared source schema. Schema acquisition and planning errors happen before execution/materialization errors; for example, a missing join file can be reported before a primary source row outside the inference window fails conversion.
* The optimizer has three ordered rewrites. First, an adjacent prefix made only of eligible `select` and `filter` operations may be represented as source output columns plus source predicates for prepared primary single-file sources. This prefix rewrite is format-agnostic: it reads only the source schema and logical operators, never CSV/JSON/Parquet details. It is valid for top-level identity projections with unchanged output names (`select id, status`, not `select id, id` or `select address.city`) and source-safe filters. A source-safe filter is composed only from top-level columns, literals, comparisons (`==`, `!=`, `<`, `>`, `<=`, `>=`), boolean `and`/`or`/`not`, and `is null` / `is not null`; function calls, arithmetic, nested paths, constructors, and other expression forms stay as normal pipeline filters. Eligible select/filter operations are removed from the optimized operation list only when the source fully satisfies them. The prefix rewrite stops scanning at the first unsupported operation and does not push later `select` or `filter` operations across it. Second, adjacent `logicalGroup` followed immediately by `logicalReduce` over the group's nested column is replaced with `logicalGroupReduce`. Standalone `group`, non-adjacent group/reduce, and explicit-list `reduce orders ...` remain on the compatibility path.
* After source-prefix rewrite and group/reduce fusion, demand-driven pruning walks the optimized logical pipeline backward and computes the set of top-level input columns demanded by the final output and each operation. It is a semantic rewrite over logical names and schemas only. `filter` demands its predicate columns plus demanded pass-through columns; `sort` demands sort keys; `select` maps demanded output names back to source paths only when those selected outputs are themselves demanded downstream; `rename` maps demanded output names back positionally; `remove` intersects demand with kept columns; `transform` keeps only demanded assignments and demands their right-hand-side inputs while preserving simultaneous assignment semantics; `count` demands no data columns. Therefore `select bad_col | count` and `transform unused = erroring_expr | select name | count` may not evaluate the projected value or dead assignment. Join demands left keys plus demanded left-side output columns; demanded right-side output columns do not demand same-named left columns. The logical join carries semantic output-source metadata from the original full join schema so right-side collision names remain exactly the names produced by the unoptimized logical pipeline. Physical planning turns that metadata into output bindings after pruning. Fused group/reduce always demands group key paths. If the original nested payload output is demanded, it demands all fused input columns and materializes the payload. If the payload is not demanded, it demands only live aggregate argument roots plus group key roots, and dead reduce assignments may be removed. A reduce assignment that overwrites the nested column name, such as `reduce grouped = count()`, is an aggregate output demand rather than a payload demand. `describe`, full-row `distinct`, standalone `group`, and explicit-list `reduce` are read-all barriers. Projected `distinct` keeps all projection keys because dropping an apparently unused key would change distinct row identity.
* Optimized source requirements are semantic. The optimized IR stores requested source output columns and logical predicate expressions; it does not store physical loader columns or compiled evaluators. Physical planning derives the source read set as `output columns ∪ source-predicate referenced columns`, preserving requested output order and appending predicate-only columns in source-schema order. Source output/read requirements use `table.ColumnSelection`, an explicit `AllColumns | SelectedColumns(names)` value. Its zero value is `AllColumns`; `SelectedColumns()` is the finite empty set and means "zero data columns." Physical planning binds predicate column references against the derived read set and compiles a source predicate.
* Format-specific source stream code consumes the physical source spec. Output columns and predicate columns are validated/emitted before the pushed predicate is evaluated, so bad records in those columns remain observable even when the predicate would reject the row. Type/schema errors in truly unreferenced columns are skipped on the optimized single-file source path. For JSON/JSONL, "skipped" here means skipped from schema validation and table materialization after the logical JSON record has been parsed; the current decoder still constructs the demanded row's field values. Glob and CLI stdin sources disable source column pruning and source predicate pushdown, read all source columns, and keep existing bad-record visibility. Source structure validation such as duplicate headers and row-width checks remains observable independent of the read set.
* The optimizer must not reorder operations, push predicates across transforms/joins/groups/sorts, or move runtime errors from demanded expressions across semantic barriers. The only group/reduce payload elision allowed is inside a fused adjacent `logicalGroupReduce` whose original nested payload column is not demanded. Dead transform assignments and dead fused reduce assignments whose outputs are not demanded may be removed, so their runtime errors intentionally disappear; static planning/type errors still happen before optimization. Optimized rewrites must allocate replacement optimized nodes instead of mutating typed logical plan internals; no-op portions may share immutable logical facts to avoid planning-time allocation regressions.
* Typed logical and optimized logical IRs carry semantic facts only: column paths/identities, expression result schemas, function signatures, operation ordering, operation output schemas, join output source identities, source output-column requirements, and source logical predicates. They must not contain stage-local table column indexes, projection index lists, transform/reduce target indexes, join key indexes, physical join output maps, physical source read sets, or compiled evaluators. With the current loader API, logical join planning materializes the right-hand source once for schema acquisition and key validation; that loaded source is not a physical binding.
* Physical planning happens after the optimizer. It assigns executor-ready metadata such as top-level column indexes, projection index lists, bound sort/filter/group/join accessors, transform/reduce target indexes, fixed `count`/`describe` schemas, rename/remove result shapes, join key bindings, join output column maps, join output schemas, fused group/reduce aggregate state slots, and compiled evaluator choices.
* Physical plan execution must consume physical metadata instead of rediscovering schemas or rebinding paths. Execution must not mutate typed logical or optimized logical IR in ways that can leave stale indexes. `dq` has two interpreters for the same physical plan: the materialized table interpreter and the streaming/materializing interpreter. Streaming execution must be implemented as an evaluation strategy over planned physical nodes, not as parser, CLI, or source-specific shortcuts.
* Streaming execution partitions the physical plan into row-local spans and materialization boundaries. `filter`, `select`, `remove`, `rename`, and row-local `transform` fuse into a single row program for each span. Eligible spans may run with bounded ordered parallelism, but output rows and runtime errors must remain in input order. `head` is a streaming boundary rather than a parallel row program because it must not read ahead.
* `count` and `describe` are bounded-memory folds: they consume the upstream stream before emitting their small result table. `tail`, `sort`, `distinct`, `group`, `reduce`, fused `group_reduce`, and `join` are materialization boundaries and then run through the existing materialized executor. Fused `group_reduce` scans input rows once, keeps first-seen group order, updates aggregate state slots directly, and builds nested payload records only when demanded. After a materialized boundary finishes, the result table can re-enter row streaming for later row-local spans. Non-dropping row-local spans immediately before a materialized boundary may run through the materialized table interpreter to keep table fast paths such as append-only typed transform.
* Prepared single-file source execution is adaptive. If the physical plan has an early streaming benefit (`head`, bounded folds, or a row-dropping span before materialization), it opens the native source row stream. If the plan would immediately materialize before a blocker without dropping rows, it uses the prepared materialized loader path and then enters the same streaming/materializing executor over the resulting table. This preserves source projection/predicate semantics while avoiding generic stream materialization on table-fast-path workloads.
* Glob and CLI stdin source execution uses native source row streams, but source column pruning and source predicate pushdown are disabled for those source classes. They therefore read all source columns before row-wise pipeline operations, preserving existing bad-record visibility while still allowing `head` and later row-local streaming to stop upstream early. Later logical pruning may still remove dead downstream computations that do not affect source read visibility. Glob streams traverse matched shards in deterministic expanded order. When a glob source builds a global schema by permissively concatenating shard schemas, each shard row is mapped into that global schema with the matching permissive value coercion; for example, Avro/Parquet shards with `id:int` and `id:string` produce a global `id:string` and stringify the integer shard values. CLI stdin streams CSV/JSON/JSONL from the live reader; `infer_rows=-1` necessarily buffers all logical stdin records during schema acquisition. Bounded stdin `format=json` arrays also retain the unsampled tail because top-level array syntax is a source-wide error and stdin cannot be rewound after validation.
* `head N` is lazy and short-circuiting. Once N rows have been emitted, upstream is closed and later row-content errors are not evaluated. Source-wide errors required before rows can stream remain observable. For example, duplicate CSV headers still fail before `head 1`, while `transform y = year(raw) | head 1` does not evaluate `year(raw)` on row 2. Because execution can re-enter streaming after a materialized boundary, `sort id | transform y = year(raw) | head 1` also short-circuits the row-local suffix after the sort.
* Streaming `transform` preserves simultaneous assignment semantics. All right-hand sides evaluate against the input row (`EvalContext.RowValues`) before any assignment target is written to the output row. Later assignments in the same `transform` must not see earlier assignment results.
* `transform`, `group`, `reduce`, and `join` participate in the same schema-planned pipeline as surrounding operations. Downstream operations bind against upstream output schemas before any rows are evaluated, so a query like `transform y = year("bad") | join right.csv on id | select missing` reports the missing-column planning error before the data-dependent `year()` runtime error, and `group g | reduce total = sum(id) | select missing` reports the missing-column planning error before an overflowing `sum(id)` can execute.
* Planned expression operators must not use permissive append to hide type mistakes. Incompatible `if` branches, incompatible `coalesce` arguments, bad function argument types, and incompatible comparisons fail during planning even when the current table has zero rows.
* Joins must not use permissive append to hide key or output-schema mistakes. Join key schemas are compared exactly after recursively ignoring nullability; `string` vs `string?` is legal, while `int` vs `float`, `int` vs `string`, `bool` vs `int`, record/scalar mismatches, nested record/list mismatches, and union branch/order mismatches are planning errors. Matching record, list, and ordered union schemas can be used as keys because runtime equality uses exact structural values.
* Schema-preserving operations must keep column schemas even when they produce zero rows. For example, `users.csv | filter { false } | describe` reports the loaded `name:string` and `age:int` schemas with `row_count = 0`, not `null` types.
* Dot-path projections, expression column references, sort keys, distinct keys, group keys, and join keys bind against the current logical schema before row execution. A missing top-level column or a missing field inside a known record schema is an error. A nullable existing parent field makes the projected child nullable and produces runtime nulls when the parent is null. Dot paths must not step through lists or union branches.

Null-only finalization remains part of the user-visible contract: when a schema position is still `TypeNull` at finalization, it renders as nullable string. This is why all-null CSV columns with data rows, standalone empty list literals, and null-only nested fields fall back to `string?` in schema strings where a concrete type is required. Header-only CSV columns report non-null `string` with `row_count = 0`, including `infer_rows=0`, because source inspection proves EOF before any data row. Before finalization, an empty list literal has no determining element schema and can adopt a typed list context through strict expression unification.

---

## Syntax rules

Every operation uses one of three argument styles. The style is fixed per op — do not mix them.

Comma-separated syntaxes are strict: do **not** write trailing commas. This
applies to column lists, assignment/binding lists, function argument lists,
`list(...)` elements, and `struct(...)` fields. For example,
`upper(name,)`, `coalesce(a, b,)`, `select name,`, `list(1,)`, and
`struct(a = 1,)` are parse errors.

### Parser fail-closed and spans

The parser must fail closed: if a token belongs to an expression, operation, or
output stage, it must either be consumed intentionally or reported as a parse or
lex error. In particular, lexer errors discovered during lookahead are fatal
even when the grammar would otherwise appear complete. A query such as
`users.csv | transform out = age % 2 | json` must not run as
`transform out = age` and must not silently drop the `| json` output stage.

Expression parse sites have explicit legal boundaries:

* `filter { expr }` ends at `}`.
* `transform` and `reduce` assignment RHS expressions end at `,`, `|`, or EOF.
* Function arguments, `list(...)` elements, `struct(...)` field values, and
  parenthesized expressions end at their delimiter or closing `)`.

The AST stores source spans for syntactic nodes used in later diagnostics.
Spans are byte offsets into the original query string with exclusive `End`.
`lexer.Token.Pos`, `lexer.Token.End`, and AST spans must stay byte-based rather
than rune-based so escaped strings and Unicode input can be highlighted against
the original query bytes. Token end offsets are explicit; do not derive token
ends from decoded token values because `"a\n"` and backtick/Unicode identifiers
can have source lengths that differ from their decoded values.

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
* Dot paths are one item for operations that accept paths: `address.city` is a single item, not two. `remove` is the exception here: it removes top-level columns only and rejects dot paths such as `remove address.city`.
* Backticks for names with spaces: `` select `first name`, age ``
* `sort`: prefix `-` on a key for descending (`sort city, -age`).
* `group`: optional `as nested_name` comes after the column list. The nested name must not collide with any output group-key column name; use `as rows` or another distinct name when grouping by a key such as `grouped`.
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
* `transform` and `reduce` assignment targets must be unique within one operation. `transform x = 1, x = 2` and `reduce x = count(), x = sum(age)` are errors rather than last-write-wins.
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

### Expression binding, IR, and type checking

The parser produces the raw AST only. It must not resolve column names, choose function overloads, infer expression result types, or choose source projections. Literal primary sources are inspected for a sound source schema before typed planning; full source rows are not materialized until after optimization and physical planning. This source schema acquisition step is format-specific only because some formats need inference and others have metadata. Typed planning, optimization, and physical planning are format-agnostic and operate on schema plus source requirements. For bounded CSV/JSON/JSONL inference windows that do not reach EOF, that source schema is intentionally nullable at known positions so later nulls or missing fields cannot invalidate already-planned operators. General semantic planning starts from the source schema and then from each preceding pipeline stage's output schema:

```
tokens -> raw AST -> source schema acquisition -> typed logical expression/plan with source -> optimized logical IR with source output/predicate requirements -> physical expression/source plan with derived read set -> source row stream -> streaming/materialized execution
```

The typed logical expression IR covers the whole expression tree, not only function calls. It resolves every column/dot-path reference to a stable path/identity and logical schema, but deliberately does not store a stage-local table column index. The typed expression annotates every logical node with its result schema. Physical planning later rebinds those logical references against the optimized stage schemas to assign current top-level column indexes and other executor metadata before iterating rows.

Binding and type checking are stage-local:
* `users.csv | transform age2 = age + 1 | filter { age2 > 20 }` is valid because the `filter` sees the table produced by the previous `transform`.
* `users.csv | transform age2 = age + 1, age3 = age2 + 1` is invalid because all RHS expressions in one `transform` see only the input schema, not sibling assignments from the same operation.
* `users.csv | group city | reduce total = sum(age), doubled = total * 2` is invalid for the same reason: all RHS expressions in one `reduce` see only the nested row schema and aggregate results inside their own expression, not sibling assignments.
* `users.csv | group city | reduce x = count(), x = sum(age)` is invalid because assignment targets in one `reduce` must be unique.
* `users.csv | filter { false } | transform out = upper(age)` still fails because `upper` requires a string even though no row will execute.
* `users.csv | filter { false } | group city | reduce bad = upper(name)` still fails because reduce expressions must use aggregate functions over nested row columns.
* `nested.json | select address.missing` fails when `address` has a known record schema without `missing`. Missing JSON object fields observed during loading are represented as nullable fields; absent fields after schema inference are not invented by later expressions.

Type checking uses the central `table.TypeDescriptor` helpers. Numeric expressions may promote `int` to `float` for schemas and arithmetic. Runtime evaluation of planned expressions must use explicit coercion nodes with finalized target schemas only where the planner needs a common runtime shape, such as promoted `if`, `coalesce`, and list literal results; do not coerce every expression node unconditionally. Direct filters such as `coalesce(id, 0.0) == 1.0` and staged transforms such as `transform y = coalesce(id, 0.0) | filter { y == 1.0 }` must use the same values. Runtime `int + int`, `int - int`, and `int * int` evaluate in `int64` and must error on overflow rather than wrapping or rounding through `float64`; unary `-` over `math.MinInt64` and `sum(int)` overflow also error. Division is planned and evaluated as float-valued; division by zero returns null. Runtime comparisons and `list_contains` must allow planned-compatible `int`/`float` pairs, including inside nested list/record equality, but compare exact represented numeric values rather than rounding `int64` through `float64`. Top-level comparison operators (`==`, `!=`, `<`, `>`, `<=`, `>=`) use three-valued logic: if either operand evaluates to `null`, the comparison result is `null`; use `is null` / `is not null` for definite null checks. Record equality compares by field name over the union of field names and treats missing fields as `null`, matching strict record unification where missing fields become nullable. `if` and `coalesce` use strict common-type unification and must not silently widen incompatible branches to `string` or top-level `mixed`. `list_contains(list<T>, value)` requires `T` and `value` to be strictly unifiable/comparable unless the list element schema is already `mixed`; for records, missing fields compare as `null`, so comparable records with extra nullable fields are valid but may still compare false at runtime. Filters must type-check to `bool`, `bool?`, or the null-only expression schema; any other result type is a planning error.

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

Return cheap metadata for the current materialized table. The output has one row per input column and four columns: `column`, `type`, `row_count`, and `schema`.

```
dq 'users.csv | describe'
dq 'users.csv | filter { city == "NY" } | describe | json'
dq 'users.csv | describe | filter { type == "string" }'
```

Output:

```
column | type   | row_count | schema
------ | ------ | --------- | ------
name   | string | 6         | string
age    | int    | 6         | int
city   | string | 6         | string
```

Rules:
* `describe` is a normal pipeline operation, not an output format command. It can be followed by `filter`, `select`, `sort`, output formats, etc.
* It takes no arguments.
* `row_count` repeats the current table row count on every metadata row.
* Type names are `null`, `int`, `float`, `string`, `bool`, `list`, `record`, and `union`.
* `type` is the top-level storage type. For nested values it remains `list`, `record`, or `union`.
* `schema` is a deterministic recursive type string for the current column schema. Examples: `string`, `int`, `record<city:string, zip:string>`, `list<record<amount:float, order_id:int>>`, `union<int,string>`.
* Nullable schema positions are suffixed with `?`, such as `record<x:int?, y:string?>?`. Missing JSON/JSONL object fields and explicit nulls both materialize as null and make the affected schema position nullable.
* Types are current table storage types after preceding pipeline stages, not historical physical values from earlier stages.
* Source loaders may set concrete storage types even when no non-null values exist. CSV inference uses `TypeString` for columns with no sampled non-null values, so freshly loaded all-null CSV columns and header-only CSV columns report `string`.
* Avro and Parquet loaders seed schemas from file metadata, so empty binary files with columns can report their declared field types. Empty Avro files with incompatible unions report the declared `union<...>` schema. Recursive Avro named records are unsupported and must fail clearly at load time rather than falling back to `null` schemas or row-value inference.
* If a column widened to `string` and rows survive a later rebuilding operation, copied values remain `string`.
* Schema-preserving operations keep their planned column schemas even when no rows survive. A zero-row `filter`, `select`, `distinct`, `rename`, `remove`, `group`, typed `transform`, typed `reduce`, or exact-compatible join should still produce meaningful `describe` output for its columns.
* A zero-column table returns zero metadata rows. Use `count` if row cardinality must be visible for a zero-column table.
* `describe` keeps one row per top-level column. It does not emit one row per nested field; use the `schema` string to inspect nested fields.

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

Sort by columns (comma-separated). Ascending by default; prefix a column with `-` to sort it descending. Directions can be mixed per column. Sort keys must be orderable (`int`, `float`, or `string`); bool, list, record, union, and mixed schemas are rejected. Nulls sort last.

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

The expression is bound and type-checked before row execution. It must return `bool`, `bool?`, or null-only; numbers, strings, lists, and records are invalid filter predicates.

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

Group rows by columns; nested rows stored under a nested column. The `as nested_name` part is optional -- if omitted, defaults to `grouped`. The nested column name must be distinct from every output group-key column name. For example, `group grouped` is invalid because both the key and nested list would be named `grouped`; use `group grouped as rows`.

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

All assignment right-hand sides bind against the input table schema for this `transform`. Newly created or overwritten columns are visible to later pipeline stages, but not to sibling assignments in the same `transform`. Assignment target names must be unique within one `transform`.

**Arithmetic propagates nulls by default:**
```
dq 'sales.csv | transform total = quantity * price'  // null if either is null
```

Integer arithmetic is exact within `int64`. Overflow in `+`, `-`, `*`,
unary `-`, or `sum(int)` is a query error; division by zero returns null.

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
a    | [ {name:a,age:20,city:x}, {name:a,age:22,city:y} ] | 22 | 2
b    | [ {name:b,age:25,city:z} ]                       | 25 | 1
```

Grouped nested records preserve all original columns, including the group keys.

**With a custom nested name (must match the name used in `group`):**

```
dq 'users.csv | group name as entries | reduce entries max_age = max(age), count = count()'
```

All assignment right-hand sides bind and type-check against the nested row schema before group rows are executed. Aggregate arguments must be column references or dot paths in that nested schema (`sum(amount)`, `first(address.city)`), not arbitrary expressions (`sum(amount + 1)`) or scalar row functions (`upper(name)`). Newly created reduce columns are visible to later pipeline stages, but not to sibling assignments in the same `reduce`. Assignment target names must be unique within one `reduce`. `group` and `reduce` are planned in the same schema-planned pipeline as surrounding operations, so downstream schema errors are reported before aggregate row execution.

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

Return unique rows. With no columns, `distinct` deduplicates the entire current row and preserves the full schema. With columns, `distinct col1, col2` first projects to those columns using the same path binding, output naming, and schema rules as `select`, then deduplicates the projected tuples. First-seen key order is preserved, but non-key columns are not retained.

```
dq 'users.csv | distinct'                // unique rows
dq 'users.csv | distinct city'           // one-column table of unique cities
dq 'users.csv | distinct city, age'      // city and age columns only
```

### 12. `rename old=new [, old2=new2 ...]`

Rename one or more columns. Comma-separated `old=new` bindings.

```
dq 'users.csv | rename name=first_name'
dq 'users.csv | rename `first name`=first_name, `last name`=last_name'
```

### 13. `remove col1, col2, ...`

Remove top-level columns from output. `remove` does not delete nested record fields; `remove address.city` is an error rather than removing either the nested field or the top-level `address` column.

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

Each key is either a column path (same name on both sides) or `left_path == right_path`. Join key paths bind against each side's logical schema during upfront pipeline planning; a missing nested field such as `address.missing` is an error, not a nullable synthetic key. Join key columns appear once under the left-side name; dot-path keys get a flattened column (`address.city` -> `address_city`, suffixed if taken). Colliding right-side columns are prefixed with the join file basename (for globs, derived from the pattern — e.g. `orders/*.csv` with colliding column `note` -> `orders_note`).

The join file's format comes from its extension unless overridden with `with format=...` on the join path. Join sources support globs (`orders/part-*.csv`); matched files are concatenated during planning before the join executes. Null keys never match. Key schemas must be exact-compatible after recursively ignoring nullability: `string` and `string?` are compatible, but `int` vs `string`, `int` vs `float`, `bool` vs `int`, record/scalar mismatches, nested record/list mismatches, ordered union mismatches, and any schema containing `mixed` are planning errors. Matching keys compare by exact dq value and structural value (consistent with `group`/`distinct`), so integer `1` does not match string `"1"` or float `1.0` at runtime. Avro branch names/tags are not part of the key.

Outer-join output schemas reflect possible null padding from the join kind, not only rows that happen to be emitted. `left` and `full` joins mark retained right-side output columns nullable; `right` and `full` joins mark non-key left-side output columns nullable. Merged join-key output columns keep the unified key schema and are not made nullable solely because the opposite side can be padded.

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

Avro and Parquet writers derive file schemas from `Table.Schema()`, then refine from row values. Empty non-zero-column outputs must preserve the planned result schema, including schemas produced by `filter { false } | select ...`, joins with no matches, and other zero-row pipeline results. A zero-column Avro or Parquet output remains invalid. Union-typed schemas are currently rejected by Avro and Parquet writers rather than silently stringified; add explicit branch-aware writer support before changing that behavior.

Output format names are not lexer keywords — only recognized as the final `|`-stage. Column names like `csv` in `{ csv == "x" }` are unaffected.

---

## Built-in Functions

These are available in `transform` and `reduce` expressions:

Built-in function names are case-sensitive. Internally, scalar function calls,
lazy special-form calls, and aggregate function calls are declared in one builtin
catalog. That catalog is the source of truth for call-builtin existence,
category, planning-time signature checks, interpreted typed runtime hooks, and
aggregate runtime hooks. Expression constructs that are not function calls, such
as `list(...)` and `struct(...)`, remain parser AST forms outside the catalog.
Adding or changing a call builtin must update the catalog entry rather than
adding an independent planner or runtime switch arm.

Catalog category invariants:
* Scalar builtins have a signature checker and `TypedEval` interpreted runtime
  hook; they must not have an aggregate hook.
* Special forms have a signature checker and lazy `TypedEval` interpreted
  runtime hook. `if` and `coalesce` type-check all arguments during planning
  but evaluate only the selected/needed runtime arguments.
* Aggregate builtins have a signature checker and aggregate metadata/runtime
  hook; they must not have `TypedEval`. Aggregates are valid only in `reduce`,
  and scalar/special-form functions are rejected in reduce expressions unless
  wrapped by an aggregate that accepts a column path.
* Aggregate runtime behavior is a catalog-owned fold: `aggregateSpec`
  provides the accumulator factory used by both materialized `reduce` and
  fused adjacent `group | reduce`. Do not add executor-local switch arms over
  aggregate names; adding or changing an aggregate must update its catalog
  entry.
* Compiled fast paths may optimize only cataloged scalar builtins with the
  expected typed shape and a `TypedEval` hook. Fast paths are hand-written
  switch arms gated by the catalog; the compiled result must remain equivalent
  to the interpreted catalog evaluator.

**Aggregations (for `reduce` only):**
* `count()` — number of rows in group
* `sum(col)` — sum of numeric values (nulls ignored)
* `avg(col)` — average of numeric values (nulls ignored)
* `min(col)`, `max(col)` — min/max of orderable values (`int`, `float`, `string`; nulls ignored)
* `first(col)`, `last(col)` — first/last value in group

**Transformations (for `transform` only):**
* `upper(s)`, `lower(s)` — case conversion (`TypeString` only)
* `str_len(s)` — string length in Unicode code points, 0-based indexing companion (`TypeString` only)
* `list(expr, ...)` — construct an ordered list value row-by-row. `list()` returns an empty list with no determining element schema until contextual unification or finalization resolves it. It can adopt the other typed list schema in `if`, `coalesce`, equality, and `list_contains`; when no context determines the element schema, finalization renders it as `list<string?>`. Elements are positional, evaluated left-to-right, and null elements are preserved. Lists may contain mixed element types, including records from `struct(...)`.
* `list_len(xs)` — list element count (`TypeList` only)
* `list_contains(xs, x)` — true if list `xs` contains `x`, using structural equality with exact represented-value comparison for numeric `int`/`float` pairs (`TypeList` only for `xs`; `x` must be strictly unifiable/comparable with the list element schema unless the element schema is already `mixed`; missing record fields compare as `null`)
* `substr(s, start, length)` — substring by **0-based code point** index and length (`TypeString` only for `s`; `start` and `length` must be `TypeInt`); negative `start` counts from the end (Python-style); `length` must be non-negative
* `trim(s)` — remove whitespace (`TypeString` only)
* `coalesce(a, b, ...)` — first non-null value; arguments must have one strict common type
* `if(cond, then, else)` — conditional; only explicit `true` takes then, `false` and `null` take else; branches must have one strict common type
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
