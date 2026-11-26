# Examples

> **These files are synthetic.** They have the overall shape of UK energy
> dataflows: a header, nested group records and a footer. All values are
> invented. The MPRNs, MPANs and meter serials are fake.
>
> - `GASREAD` is a made-up comma-delimited flow, shaped like gas (SPAA-style)
>   files with an `A00` header and a `Z99` trailer. It is **not** a real SPAA
>   flow, and its field layout is invented.
> - `D0150` uses the real flow's group IDs (288, 289, 290, 293, 08A), but the
>   field contents are illustrative, not a copy of the DTC layout.
>
> Use them to try the tool, not as a reference for the industry formats.

```
examples/
  gas/samples/GASREAD.csv          sample to learn from (two supply points, three meters)
  gas/inbox/                       files to convert, including two broken ones
  electricity/samples/D0150.txt    sample to learn from (two meters with registers)
  electricity/inbox/               files to convert, including one broken one
  instructions/GASREAD.json        learned, then columns renamed by hand
  instructions/D0150.json          learned, generated column names left as-is
```

The broken inputs show what gets rejected, and why:

| File | Rejected because |
|---|---|
| `gas/inbox/GASREAD_000104_bad_date.csv` | `read_date` is `2024-01-05`, not the learned `YYYYMMDD` date type |
| `gas/inbox/GASREAD_000105_truncated.csv` | no `Z99` trailer, so the file is probably cut short |
| `electricity/inbox/D0150_0000000004_unknown_group.txt` | group `299` isn't in the instructions |

## Try it

Run these from the repository root.

**Learn** (this regenerates the instructions with generated column names):

```sh
go run ./cmd learn -d , -o /tmp/GASREAD.json examples/gas/samples/GASREAD.csv
```

**Convert** into linked tables:

```sh
go run ./cmd convert -layout tables -i examples/instructions -o /tmp/flowtocsv-out examples/gas/inbox examples/electricity/inbox
```

Expect this to exit with code 3: some files were rejected, and the rest were
written. You get `gasread_file.csv`, `gasread_s10.csv` … `gasread_s40.csv`,
and the same set for `d0150`.

**Use the GUI** with these instructions:

```sh
go run ./cmd gui -dir examples/instructions
```

`-archive` and `-rejected` move input files, so if you try them, point the
command at a copy of the inbox folders.
