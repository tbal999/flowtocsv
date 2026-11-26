// Command flowtocsv learns dataflow structures from samples and converts
// dataflow files into CSV or JSON tables.
//
//	flowtocsv learn   [-d DELIM] [-flow NAME] [-o FILE] SAMPLE...
//	flowtocsv convert -i INSTRUCTIONS [-i ...] [-format csv|json] [-o DIR] INPUT...
package main

import (
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
  flowtocsv convert -i INSTRUCTIONS [-i ...] [-format csv|json] [-o DIR] INPUT...

learn    infers instructions (JSON) for one dataflow from sample files.
convert  flattens every input that matches an instruction set into
         DIR/<flow>.csv (or .json), one output file per flow.

INSTRUCTIONS and INPUT may be files or directories (not searched recursively).
Run "flowtocsv learn -h" or "flowtocsv convert -h" for flags.
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

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
	case err == nil:
		return 0
	case errors.Is(err, flag.ErrHelp):
		return 0
	case errors.Is(err, errUsage):
		return 2
	default:
		fmt.Fprintln(stderr, "flowtocsv:", err)
		return 1
	}
}

var errUsage = errors.New("usage")

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
		f, err := parseFile(p, *delim)
		if err != nil {
			return err
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
	return writeFile(*out, in.WriteJSON, func() {
		fmt.Fprintf(stdout, "learned %s: %d groups, %d columns -> %s\n", in.Flow, len(in.Groups), len(in.Columns()), *out)
	})
}

type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

func convert(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("convert", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var instr multiFlag
	fs.Var(&instr, "i", "instructions file or directory of *.json (repeatable)")
	format := fs.String("format", "csv", "output format: csv or json")
	outDir := fs.String("o", ".", "output directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *format != "csv" && *format != "json" {
		fmt.Fprintf(stderr, "convert: unknown format %q\n", *format)
		return errUsage
	}
	if len(instr) == 0 || fs.NArg() == 0 {
		fmt.Fprintln(stderr, "convert: need at least one -i and one input")
		return errUsage
	}

	instrPaths, err := expand(instr)
	if err != nil {
		return err
	}
	var all []flowtocsv.Instructions
	for _, p := range instrPaths {
		if filepath.Ext(p) != ".json" {
			continue
		}
		in, err := readInstructions(p)
		if err != nil {
			return err
		}
		all = append(all, in)
	}
	if len(all) == 0 {
		return errors.New("convert: no instruction files (*.json) found")
	}
	inputs, err := expand(fs.Args())
	if err != nil {
		return err
	}
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		return err
	}

	// Read every input once; parse once per distinct delimiter.
	contents := map[string][]byte{}
	for _, p := range inputs {
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		contents[p] = b
	}
	parsed := map[[2]string]*flowtocsv.File{}
	parse := func(path, delim string) (*flowtocsv.File, error) {
		key := [2]string{path, delim}
		if f, ok := parsed[key]; ok {
			return f, nil
		}
		f, err := flowtocsv.Parse(strings.NewReader(string(contents[path])), delim)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		parsed[key] = f
		return f, nil
	}

	var failures int
	matchedAny := map[string]bool{}
	for _, in := range all {
		table := &flowtocsv.Table{Columns: in.Columns()}
		files := 0
		for _, p := range inputs {
			f, err := parse(p, in.Delimiter)
			if err != nil {
				// Not every file is a dataflow for every delimiter; only
				// report it if nothing ends up claiming the file.
				continue
			}
			if !in.Matches(f) {
				continue
			}
			matchedAny[p] = true
			t, err := in.Convert(f, filepath.Base(p))
			if err != nil {
				fmt.Fprintf(stderr, "%s: %v\n", p, err)
				failures++
				continue
			}
			if err := table.Append(t); err != nil {
				return err
			}
			files++
		}
		if files == 0 {
			continue
		}
		out := filepath.Join(*outDir, in.Flow+"."+*format)
		write := table.WriteCSV
		if *format == "json" {
			write = func(w io.Writer) error {
				enc := json.NewEncoder(w)
				return enc.Encode(table)
			}
		}
		if err := writeFile(out, write, func() {
			fmt.Fprintf(stdout, "%s: %d files, %d rows -> %s\n", in.Flow, files, len(table.Rows), out)
		}); err != nil {
			return err
		}
	}

	for _, p := range inputs {
		if !matchedAny[p] {
			fmt.Fprintf(stderr, "%s: skipped, matches no instructions\n", p)
		}
	}
	if failures > 0 {
		return fmt.Errorf("%d file(s) failed to convert", failures)
	}
	return nil
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

func parseFile(path, delim string) (*flowtocsv.File, error) {
	fh, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	f, err := flowtocsv.Parse(fh, delim)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return f, nil
}

func readInstructions(path string) (flowtocsv.Instructions, error) {
	fh, err := os.Open(path)
	if err != nil {
		return flowtocsv.Instructions{}, err
	}
	defer fh.Close()
	in, err := flowtocsv.ReadInstructions(fh)
	if err != nil {
		return flowtocsv.Instructions{}, fmt.Errorf("%s: %w", path, err)
	}
	return in, nil
}

// writeFile writes via a temp file and rename, so a failed run never leaves
// a half-written output where a loader might pick it up.
func writeFile(path string, write func(io.Writer) error, done func()) error {
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
	if err := os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	done()
	return nil
}
