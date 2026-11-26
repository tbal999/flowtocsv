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
- **Check each `parent`.** See [How rows are built](#how-rows-are-built).

**3. Convert any number of files.**

```sh
flowtocsv convert -i instructions/ -o out/ incoming/
```

Each input file is checked against every instruction file. All files of one
flow are combined into `out/<flow>.csv`; use `-format json` for JSON output.
Files that match no flow are reported and skipped. A file that matches but
can't be converted is reported, and the command exits non-zero.

```
source_file,row_index,hdr_1,...,g288_1,g290_1,g293_1,g293_2,g08a_1,...
flow1.txt,1,99999,...,1200012345678,S95A012345,01,9.00,KXXA 12387,...
flow1.txt,2,99999,...,1200012345678,S95A012345,02,7.00,KXXA 12387,...
flow1.txt,3,99999,...,1200012345678,S95A067890,01,5.00,KXXA 12387,...
```

Every row also carries `source_file` and `row_index` (1-based within that
file). Together they're a natural key for staging-table merges.

## How rows are built

A dataflow is a hierarchy: a 288 MPAN has 290 meters, and a meter has 293
registers. flowtocsv produces one row per combination of records:

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
learned, on child records without their parent, and on files with a missing
footer. Re-learn from a sample that includes the new group, or edit the JSON.

## Library

```go
f, err := flowtocsv.Parse(r, "|")
ins, err := flowtocsv.Learn("D0150", f)   // or flowtocsv.ReadInstructions(jsonFile)
table, err := ins.Convert(f, "D0150001.txt")

table.WriteCSV(w)          // flowtocsv.ReadCSV reads it back
json.Marshal(table)        // {"columns": [...], "rows": [[...]]}
```

Docs: https://pkg.go.dev/github.com/tbal999/flowtocsv

## Development

```sh
make check   # gofmt, go vet, go test
make build   # bin/flowtocsv
```
