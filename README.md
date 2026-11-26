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

Build from a checkout:

```sh
make build        # bin/flowtocsv (bin/flowtocsv.exe on Windows)
```

`go install github.com/tbal999/flowtocsv/cmd@latest` also works, but Go
names the binary after its folder, so you get `cmd`/`cmd.exe`. Rename it.

To try it with sample data, see [examples/](examples/).

## Two ways to run it

- **GUI.** Double-click `flowtocsv.exe`, or run `flowtocsv` with no
  arguments, and it opens in your browser. Learn flows, rename columns next
  to sample values, set types and parents, then preview and download
  conversions. It also builds the command line for unattended runs.
- **Unattended, once.** `flowtocsv convert …` runs with no web page. It
  converts the inbox, writes the output, moves the inputs, and exits with a
  status code, for SSIS, Task Scheduler or cron. See [Daily runs](#daily-runs).

The steps below use the command line. The GUI does the same things.

## GUI

```sh
flowtocsv gui [-dir instructions] [-addr 127.0.0.1:8765] [-open=false]
```

It reads and saves instructions in `-dir`, the same folder the unattended run
uses. The page is built into the binary and works offline. It only answers
on localhost and refuses requests from other websites. Files you convert
there are uploaded to the local process only.

1. **Instructions:** learn from sample files, or open saved ones. Rename
   columns (each shows a sample value), pick types and each group's parent,
   then save.
2. **Convert:** pick files and a layout. Preview every table with its keys
   and see which files were skipped or failed. Download all tables as a zip
   of CSV or JSON.
3. **Automate:** fill in your folders and copy the exact `convert` command
   for SSIS or a scheduler.

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

`convert` is a run-once process with no web page, made for SSIS, Task
Scheduler or cron:

```sh
flowtocsv convert -layout tables -i C:\flows\instructions -o C:\flows\out ^
  -archive C:\flows\archive -rejected C:\flows\rejected ^
  -state C:\flows\flowtocsv-state.json -log C:\flows\flowtocsv.log ^
  C:\flows\inbox
```

| Flag | What it does |
|---|---|
| `-archive DIR` | Moves converted input files here, including re-deliveries of files already converted. A name that already exists gets a numeric suffix, so nothing is overwritten. |
| `-rejected DIR` | Moves files that failed to convert here. The log says why. Files matching no instructions stay in the inbox. |
| `-state FILE` | Records converted files by content hash and skips them later, even under a new name. A safety net on top of `-archive`. Failed files aren't recorded, so they are retried. |
| `-log FILE` | Appends a timestamped log of each run. Console output is still printed too. |

Order of work: convert, then write all output files, then save state, then
move inputs. If a run dies partway, nothing is lost, and nothing gets loaded
twice. Every flow's output files are rewritten on each run, header-only if
nothing new arrived, so load each run's output before the next run.

| Exit code | Meaning |
|---|---|
| `0` | Every matching file converted |
| `3` | Some files were rejected (moved to `-rejected`). Everything else was converted and written. |
| `1` | Fatal: bad instructions, unreadable folders, or output couldn't be written |
| `2` | Bad command-line usage |

**SSIS:** add an Execute Process Task, set `Executable` to `flowtocsv.exe`
and `Arguments` to everything after it. It fails the task on any non-zero
exit by default. To let partial runs continue, set `SuccessValue` to 0 and
handle exit code 3 in a precedence constraint, or route the task's
`StandardErrorVariable` to your alerting. Follow it with Flat File sources
(UTF-8, code page 65001) reading the output folder.

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

The CLI and GUI live in [cmd/](cmd/); the page is [cmd/gui/index.html](cmd/gui/index.html), embedded at build time.
