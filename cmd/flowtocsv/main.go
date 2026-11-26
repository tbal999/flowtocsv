// Command flowtocsv learns dataflow structures from samples and converts
// dataflow files into CSV or JSON tables.
//
//	flowtocsv learn   [-d DELIM] [-flow NAME] [-o FILE] SAMPLE...
//	flowtocsv convert -i INSTRUCTIONS [-i ...] [-layout wide|tables]
//	                  [-format csv|json] [-state FILE] [-o DIR] INPUT...
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/tbal999/flowtocsv"
)

const usage = `usage:
  flowtocsv learn   [-d DELIM] [-flow NAME] [-o FILE] SAMPLE...
  flowtocsv convert -i INSTRUCTIONS [-i ...] [-layout wide|tables]
                    [-format csv|json] [-state FILE] [-o DIR] INPUT...

learn    infers instructions (JSON) for one dataflow from sample files.
convert  converts every input that matches an instruction set. Every flow's
         output files are rewritten on each run, header-only if no input
         matched, so stale output is never loaded twice.
           -layout wide    one DIR/<flow>.csv per flow (default)
           -layout tables  DIR/<flow>_file.csv plus DIR/<flow>_<group>.csv
                           per group, linked by id / parent_id / source_file
           -state FILE     remember converted files (by content) and skip
                           them on later runs, so output holds only new data

INSTRUCTIONS and INPUT may be files or directories (not searched recursively).
Run "flowtocsv learn -h" or "flowtocsv convert -h" for flags.
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

var errUsage = errors.New("usage")

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	var err error
	switch args[0] {
	case "learn":
		err = learn(args[1:], stdout, stderr)
	case "convert":
		err = convert(args[1:], stdout, stderr)
	case "-h", "-help", "--help", "help":
		fmt.Fprint(stdout, usage)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n%s", args[0], usage)
		return 2
	}
	switch {
	case err == nil, errors.Is(err, flag.ErrHelp):
		return 0
	case errors.Is(err, errUsage):
		return 2
	default:
		fmt.Fprintln(stderr, "flowtocsv:", err)
		return 1
	}
}

func learn(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("learn", flag.ContinueOnError)
	fs.SetOutput(stderr)
	delim := fs.String("d", "|", "field delimiter (use , for Gas SPAA flows)")
	flow := fs.String("flow", "", "dataflow name matched against the header, e.g. D0150 (default: first sample's file name without extension)")
	out := fs.String("o", "", "output file, or - for stdout (default: <flow>.json)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	paths, err := expand(fs.Args())
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		fmt.Fprintln(stderr, "learn: no sample files given")
		return errUsage
	}

	var samples []*flowtocsv.File
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		f, err := flowtocsv.Parse(strings.NewReader(string(b)), *delim)
		if err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		samples = append(samples, f)
	}
	if *flow == "" {
		base := filepath.Base(paths[0])
		*flow = strings.TrimSuffix(base, filepath.Ext(base))
	}
	in, err := flowtocsv.Learn(*flow, samples...)
	if err != nil {
		return err
	}

	if *out == "-" {
		return in.WriteJSON(stdout)
	}
	if *out == "" {
		*out = in.Flow + ".json"
	}
	if err := writeFile(*out, in.WriteJSON); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "learned %s: %d groups, %d columns, %d typed -> %s\n", in.Flow, len(in.Groups), len(in.Columns()), len(in.Types), *out)
	return nil
}

type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

// input is one file given to convert.
type input struct {
	path, name, hash string
	data             []byte
	parsed           map[string]*flowtocsv.File // by delimiter; nil if it didn't parse
}

func (in *input) parse(delim string) *flowtocsv.File {
	if f, ok := in.parsed[delim]; ok {
		return f
	}
	f, err := flowtocsv.Parse(strings.NewReader(string(in.data)), delim)
	if err != nil {
		f = nil
	}
	in.parsed[delim] = f
	return f
}

func convert(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("convert", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var instr multiFlag
	fs.Var(&instr, "i", "instructions file or directory of *.json (repeatable)")
	layout := fs.String("layout", string(flowtocsv.LayoutWide), "output layout: wide or tables")
	format := fs.String("format", "csv", "output format: csv or json")
	statePath := fs.String("state", "", "state file recording converted files; inputs already in it are skipped")
	outDir := fs.String("o", ".", "output directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	switch {
	case *format != "csv" && *format != "json":
		fmt.Fprintf(stderr, "convert: unknown format %q\n", *format)
		return errUsage
	case *layout != string(flowtocsv.LayoutWide) && *layout != string(flowtocsv.LayoutTables):
		fmt.Fprintf(stderr, "convert: unknown layout %q\n", *layout)
		return errUsage
	case len(instr) == 0 || fs.NArg() == 0:
		fmt.Fprintln(stderr, "convert: need at least one -i and one input")
		return errUsage
	}

	all, err := readAllInstructions(instr)
	if err != nil {
		return err
	}
	st := &state{Files: map[string]stateEntry{}}
	if *statePath != "" {
		if st, err = loadState(*statePath); err != nil {
			return err
		}
	}
	inputs, err := readInputs(fs.Args(), st, stderr)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		return err
	}

	var failures int
	claimed := map[*input]bool{}
	converted := map[string]stateEntry{}
	for _, ins := range all {
		tables := ins.EmptyTables(flowtocsv.Layout(*layout))
		files := 0
		for _, inp := range inputs {
			f := inp.parse(ins.Delimiter)
			if f == nil || !ins.Matches(f) {
				continue
			}
			claimed[inp] = true
			got, err := convertOne(&ins, f, inp.name, flowtocsv.Layout(*layout))
			if err != nil {
				fmt.Fprintf(stderr, "%s: %v\n", inp.path, err)
				failures++
				continue
			}
			for i, t := range got {
				if err := tables[i].Append(t); err != nil {
					return err
				}
			}
			converted[inp.hash] = stateEntry{Name: inp.name, Flow: ins.Flow}
			files++
		}

		var summary []string
		for _, t := range tables {
			path := filepath.Join(*outDir, t.Name+"."+*format)
			if err := writeFile(path, writer(t, *format)); err != nil {
				return err
			}
			summary = append(summary, fmt.Sprintf("%s (%d rows)", path, len(t.Rows)))
		}
		fmt.Fprintf(stdout, "%s: %d files -> %s\n", ins.Flow, files, strings.Join(summary, ", "))
	}

	for _, inp := range inputs {
		if !claimed[inp] {
			fmt.Fprintf(stderr, "%s: skipped, matches no instructions\n", inp.path)
		}
	}
	if *statePath != "" && len(converted) > 0 {
		st.add(converted)
		if err := writeFile(*statePath, st.write); err != nil {
			return fmt.Errorf("output written but state not saved, so these files will be converted again next run: %w", err)
		}
	}
	if failures > 0 {
		return fmt.Errorf("%d file(s) failed to convert", failures)
	}
	return nil
}

func convertOne(ins *flowtocsv.Instructions, f *flowtocsv.File, source string, layout flowtocsv.Layout) ([]*flowtocsv.Table, error) {
	if layout == flowtocsv.LayoutTables {
		return ins.ConvertTables(f, source)
	}
	t, err := ins.Convert(f, source)
	if err != nil {
		return nil, err
	}
	return []*flowtocsv.Table{t}, nil
}

func writer(t *flowtocsv.Table, format string) func(io.Writer) error {
	if format == "json" {
		return func(w io.Writer) error { return json.NewEncoder(w).Encode(t) }
	}
	return t.WriteCSV
}

func readAllInstructions(paths []string) ([]flowtocsv.Instructions, error) {
	files, err := expand(paths)
	if err != nil {
		return nil, err
	}
	var all []flowtocsv.Instructions
	for _, p := range files {
		if filepath.Ext(p) != ".json" {
			continue
		}
		fh, err := os.Open(p)
		if err != nil {
			return nil, err
		}
		in, err := flowtocsv.ReadInstructions(fh)
		fh.Close()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		all = append(all, in)
	}
	if len(all) == 0 {
		return nil, errors.New("convert: no instruction files (*.json) found")
	}
	return all, nil
}

// readInputs reads every input, dropping files already recorded in st and
// files whose content repeats an earlier input in this run.
func readInputs(paths []string, st *state, stderr io.Writer) ([]*input, error) {
	files, err := expand(paths)
	if err != nil {
		return nil, err
	}
	seen := map[string]string{}
	var inputs []*input
	for _, p := range files {
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(b)
		h := hex.EncodeToString(sum[:])
		if prev, ok := st.Files[h]; ok {
			fmt.Fprintf(stderr, "%s: skipped, already converted as %s on %s\n", p, prev.Name, prev.Converted.Format("2006-01-02 15:04"))
			continue
		}
		if prev, ok := seen[h]; ok {
			fmt.Fprintf(stderr, "%s: skipped, same content as %s\n", p, prev)
			continue
		}
		seen[h] = p
		inputs = append(inputs, &input{path: p, name: filepath.Base(p), hash: h, data: b, parsed: map[string]*flowtocsv.File{}})
	}
	return inputs, nil
}

// expand replaces directories with the regular files directly inside them.
func expand(paths []string) ([]string, error) {
	var out []string
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			out = append(out, p)
			continue
		}
		entries, err := os.ReadDir(p)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if e.Type().IsRegular() {
				out = append(out, filepath.Join(p, e.Name()))
			}
		}
	}
	return out, nil
}

// writeFile writes via a temp file and rename, so a failed run never leaves
// a half-written file where a loader might pick it up.
func writeFile(path string, write func(io.Writer) error) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := write(tmp); err != nil {
		tmp.Close()
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
