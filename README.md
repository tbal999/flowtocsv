# flowtocsv

Flatten UK energy industry dataflows (MRASCO / DTC flat files such as D0150,
D0300, and Gas SPAA flows) into tables you can load straight into SQL.

It's for analysts and BI teams at smaller suppliers who receive dataflows
every day and want them in a database (to track read disputes, meter changes
and so on) without paying for a bespoke parser per flow.

You don't write a parser. You give flowtocsv one sample of a flow. It learns
the structure, then turns every file of that flow into one flat table with
the same columns every time.

MIT licensed.

## Install

```sh
go install github.com/tbal999/flowtocsv/cmd/flowtocsv@latest
```

## Quick start

**1. Learn a flow from a sample.**

```sh
flowtocsv learn -d '|' -o instructions/D0150.json samples/D0150.txt
```

The flow name defaults to the sample's file name (`D0150`). A file belongs to
the flow when one of the first three header fields contains that name, e.g.
`D0150001`. Use `-d ,` for comma-delimited Gas SPAA flows.

**2. Edit the instructions.**

```json
{
  "flow": "D0150",
  "delimiter": "|",
  "header": { "id": "ZHV", "fields": ["hdr_1", "hdr_2", "..."] },
  "footer": { "id": "ZPT", "fields": ["ftr_1", "ftr_2"] },
  "groups": [
    { "id": "288", "fields": ["g288_1", "g288_2"] },
    { "id": "290", "parent": "288", "fields": ["g290_1", "g290_2"] },
    { "id": "293", "parent": "290", "fields": ["g293_1", "g293_2"] },
    { "id": "08A", "parent": "288", "fields": ["g08a_1", "g08a_2"] }
  ]
}
```

- **Rename fields** to meaningful column names (`mpan_core`, `meter_id`,
  ...). Each name becomes a column, so keep them unique and SQL-friendly.
  `source_file`, `row_index`, `id`, `parent_id` and `line` are reserved.
- **Check `types`.** Learn marks a column `date`, `datetime` or `decimal`
  only when every sample value fits. Keep the `types` keys in step when you
  rename. Everything else is text, so codes like `0158` keep their leading
  zeros.
- **Check each `parent`.** See [How rows are built](#how-rows-are-built).

**3. Convert any number of files.**

```sh
flowtocsv convert -i instructions/ -o out/ incoming/
```

Each input file is checked against every instruction file. Files that match
no flow are reported and skipped. A file that matches but can't be converted
is reported, contributes no rows, and makes the command exit non-zero.

Every flow's output is rewritten on each run, header-only if nothing
matched, so yesterday's output is never loaded twice. Use `-format json` for
JSON output.

Typed values are written in ISO form (`2024-01-01`, `2024-01-01 12:00:00`),
which SQL Server, Postgres and BI tools all load without format strings.

## Output layouts

### `-layout wide` (default): one table per flow

`out/d0150.csv` has one row per combination of records. It's easy to query
straight away. See [How rows are built](#how-rows-are-built).

```
source_file,row_index,hdr_1,...,g288_1,g290_1,g293_1,g293_2,g08a_1,...
flow1.txt,1,99999,...,1200012345678,S95A012345,01,9.00,KXXA 12387,...
flow1.txt,2,99999,...,1200012345678,S95A012345,02,7.00,KXXA 12387,...
flow1.txt,3,99999,...,1200012345678,S95A067890,01,5.00,KXXA 12387,...
```

The primary key is (`source_file`, `row_index`).

### `-layout tables`: one table per group, with keys

Each record appears exactly once, and the tables link like a normal
database. Every key is a single column, so the tables also relate directly
in Power BI and Tableau.

| Table | Primary key | Foreign keys |
|---|---|---|
| `d0150_file`: header and footer, one row per file | `source_file` | |
| `d0150_288`: the root group | `id` | `source_file` → `d0150_file.source_file` |
| `d0150_<group>`: every other group | `id` | `source_file` → `d0150_file.source_file`, `parent_id` → `<parent table>.id` |

```
== d0150_290.csv
id,parent_id,source_file,line,g290_1,g290_2
flow1.txt:3,flow1.txt:2,flow1.txt,3,S95A012345,F
flow1.txt:6,flow1.txt:2,flow1.txt,6,S95A067890,F
== d0150_293.csv
id,parent_id,source_file,line,g293_1,g293_2
flow1.txt:4,flow1.txt:3,flow1.txt,4,01,9.00
flow1.txt:5,flow1.txt:3,flow1.txt,5,02,7.00
flow1.txt:7,flow1.txt:6,flow1.txt,7,01,5.00
```

`id` is `<source_file>:<line>`, and `line` is the record's line in the
original file. All keys derive from the file name, so input file names must
be unique across everything you load. Dataflow file names from the gateway
are. With `-format json`, each table also lists its `primary_key` and
`foreign_keys`.

## Daily runs

```sh
flowtocsv convert -state flowtocsv-state.json -layout tables -i instructions/ -o out/ incoming/
```

With `-state`, each converted file is recorded by its content hash. Later
runs skip it, even if it's re-delivered under another name. Each run's
output therefore holds only new data: load it into staging, then merge.

Files that fail to convert are not recorded and are retried next run. The
state is saved only after all output is written. Load each run's output
before the next run, because the next run replaces it.

## How rows are built

A dataflow is a hierarchy: a 288 MPAN has 290 meters, and a meter has 293
registers. In the tables layout that hierarchy becomes `parent_id`. In the
wide layout, flowtocsv produces one row per combination of records:

- Each record of the **root group** (the first group) starts a new block of rows.
- Records of the same child group are alternatives: two registers make two rows.
- Different child groups are combined, so a group with one record (like 08A
  above) is **copied onto every row** of its parent.
- A group with no record in a block leaves its columns empty.

When learning, a group is nested under another (e.g. `"parent": "290"`) only
if the sample proves it, by showing meter, register, meter. Otherwise it
hangs off the root group. If a group's records belong to individual parent
records, either learn from a sample where the parent repeats, or set
`parent` by hand. Without that, two meters with registers would be
cross-multiplied.

Conversion fails loudly rather than producing wrong rows. It fails on group
IDs the instructions don't know, on records with more non-empty fields than
learned, on child records without their parent, on values that don't fit
their column type, and on files with a missing footer. Re-learn from a
sample that includes the new group, or edit the JSON.

## Library

```go
f, err := flowtocsv.Parse(r, "|")
ins, err := flowtocsv.Learn("D0150", f)   // or flowtocsv.ReadInstructions(jsonFile)
table, err := ins.Convert(f, "D0150001.txt")         // wide layout
tables, err := ins.ConvertTables(f, "D0150001.txt")  // tables layout, with keys

table.WriteCSV(w)          // flowtocsv.ReadCSV reads columns and rows back
json.Marshal(table)        // {"name", "primary_key", "foreign_keys", "columns", "rows"}
```

Docs: https://pkg.go.dev/github.com/tbal999/flowtocsv

## Development

```sh
make check   # gofmt, go vet, go test
make build   # bin/flowtocsv
```
