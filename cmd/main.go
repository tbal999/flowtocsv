// Command flowtocsv learns dataflow structures from samples and converts
// dataflow files into CSV or JSON tables.
//
//	flowtocsv learn   [-d DELIM] [-flow NAME] [-o FILE] SAMPLE...
//	flowtocsv convert -i INSTRUCTIONS [-i ...] [-layout wide|tables]
//	                  [-format csv|json] [-state FILE] [-o DIR] INPUT...
//	flowtocsv gui     [-addr HOST:PORT] [-dir INSTRUCTIONS_DIR] [-open=false]
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
	"time"

	"github.com/tbal999/flowtocsv"
)

const usage = `usage:
  flowtocsv learn   [-d DELIM] [-flow NAME] [-o FILE] SAMPLE...
  flowtocsv convert -i INSTRUCTIONS [-i ...] [-layout wide|tables]
                    [-format csv|json] [-state FILE] [-o DIR] INPUT...
  flowtocsv gui     [-addr HOST:PORT] [-dir INSTRUCTIONS_DIR] [-open=false]

gui      opens a local web page to learn and edit instructions (saved in
         INSTRUCTIONS_DIR, default "instructions") and to preview and
         download conversions. It only accepts connections from this machine.
learn   infers instructions (JSON) for one dataflow from sample files.
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

var (
	errUsage = errors.New("usage")
	// errPartial means some inputs were rejected but everything else was
	// converted and written.
	errPartial = errors.New("some files were rejected")
)

// Exit codes.
const (
	exitOK      = 0
	exitFatal   = 1
	exitUsage   = 2
	exitPartial = 3
)

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		args = []string{"gui"} // double-clicked: open the GUI
	}
	var err error
	switch args[0] {
	case "learn":
		err = learn(args[1:], stdout, stderr)
	case "convert":
		err = convert(args[1:], stdout, stderr)
	case "gui":
		err = gui(args[1:], stdout, stderr)
	case "-h", "-help", "--help", "help":
		fmt.Fprint(stdout, usage)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n%s", args[0], usage)
		return exitUsage
	}
	switch {
	case err == nil, errors.Is(err, flag.ErrHelp):
		return exitOK
	case errors.Is(err, errUsage):
		return exitUsage
	case errors.Is(err, errPartial):
		fmt.Fprintln(stderr, "flowtocsv:", err)
		return exitPartial
	default:
		fmt.Fprintln(stderr, "flowtocsv:", err)
		return exitFatal
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
	dups             []string                   // other paths with identical content
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

func convert(args []string, stdout, stderr io.Writer) (err error) {
	fs := flag.NewFlagSet("convert", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var instr multiFlag
	fs.Var(&instr, "i", "instructions file or directory of *.json (repeatable)")
	layout := fs.String("layout", string(flowtocsv.LayoutWide), "output layout: wide or tables")
	format := fs.String("format", "csv", "output format: csv or json")
	statePath := fs.String("state", "", "state file recording converted files; inputs already in it are skipped")
	outDir := fs.String("o", ".", "output directory")
	archive := fs.String("archive", "", "move converted (and already-converted) input files here after the run")
	rejected := fs.String("rejected", "", "move input files that failed to convert here after the run")
	logPath := fs.String("log", "", "append a timestamped log of the run to this file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *logPath != "" {
		lf, openErr := os.OpenFile(*logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if openErr != nil {
			return fmt.Errorf("opening log: %w", openErr)
		}
		defer lf.Close()
		lw := &timestamped{w: lf}
		stdout, stderr = io.MultiWriter(stdout, lw), io.MultiWriter(stderr, lw)
		fmt.Fprintf(lw, "run started: %s\n", strings.Join(args, " "))
		defer func() {
			switch {
			case err == nil:
				fmt.Fprintln(lw, "run finished: ok")
			default:
				fmt.Fprintf(lw, "run finished: %v\n", err)
			}
		}()
	}
	return convertRun(convertOptions{
		instructions: instr, inputs: fs.Args(), layout: *layout, format: *format,
		statePath: *statePath, outDir: *outDir, archive: *archive, rejected: *rejected,
	}, stdout, stderr)
}

type convertOptions struct {
	instructions, inputs                                 []string
	layout, format, statePath, outDir, archive, rejected string
}

func convertRun(o convertOptions, stdout, stderr io.Writer) error {
	layout, format, statePath, outDir := &o.layout, &o.format, &o.statePath, &o.outDir
	instr := o.instructions
	switch {
	case *format != "csv" && *format != "json":
		fmt.Fprintf(stderr, "convert: unknown format %q\n", *format)
		return errUsage
	case *layout != string(flowtocsv.LayoutWide) && *layout != string(flowtocsv.LayoutTables):
		fmt.Fprintf(stderr, "convert: unknown layout %q\n", *layout)
		return errUsage
	case len(instr) == 0 || len(o.inputs) == 0:
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
	inputs, alreadyDone, err := readInputs(o.inputs, st, stderr)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		return err
	}

	claimed := map[*input]bool{}
	failed := map[*input]bool{}
	converted := map[string]stateEntry{}
	for _, ins := range all {
		tables, results := convertInputs(&ins, flowtocsv.Layout(*layout), inputs)
		files := 0
		for _, r := range results {
			if !r.matched {
				continue
			}
			claimed[r.input] = true
			if r.err != nil {
				fmt.Fprintf(stderr, "%s: %v\n", r.input.path, r.err)
				failed[r.input] = true
				continue
			}
			converted[r.input.hash] = stateEntry{Name: r.input.name, Flow: ins.Flow}
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

	// Output and state are safe on disk; now clear the inbox.
	var toArchive, toReject []string
	toArchive = append(toArchive, alreadyDone...)
	for _, inp := range inputs {
		switch {
		case failed[inp]:
			toReject = append(append(toReject, inp.path), inp.dups...)
		case claimed[inp]:
			toArchive = append(append(toArchive, inp.path), inp.dups...)
		}
	}
	if err := moveAll(toArchive, o.archive, stdout); err != nil {
		return err
	}
	if err := moveAll(toReject, o.rejected, stdout); err != nil {
		return err
	}

	if len(failed) > 0 {
		return fmt.Errorf("%w: %d failed to convert", errPartial, len(failed))
	}
	return nil
}

// moveAll moves files into dir (if dir is set), never overwriting: a name
// already taken gets a numeric suffix.
func moveAll(paths []string, dir string, log io.Writer) error {
	if dir == "" || len(paths) == 0 {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, p := range paths {
		dest, err := freeName(dir, filepath.Base(p))
		if err != nil {
			return err
		}
		if err := moveFile(p, dest); err != nil {
			return fmt.Errorf("moving %s: %w", p, err)
		}
		fmt.Fprintf(log, "moved %s -> %s\n", p, dest)
	}
	return nil
}

func freeName(dir, name string) (string, error) {
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 0; i < 10000; i++ {
		candidate := filepath.Join(dir, name)
		if i > 0 {
			candidate = filepath.Join(dir, fmt.Sprintf("%s.%d%s", stem, i, ext))
		}
		if _, err := os.Lstat(candidate); errors.Is(err, os.ErrNotExist) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("no free name for %s in %s", name, dir)
}

// moveFile renames src to dest, falling back to copy-and-delete when they
// are on different drives.
func moveFile(src, dest string) error {
	if err := os.Rename(src, dest); err == nil {
		return nil
	}
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := writeFile(dest, func(w io.Writer) error { _, err := w.Write(b); return err }); err != nil {
		return err
	}
	return os.Remove(src)
}

// timestamped prefixes every write (one log line) with the time.
type timestamped struct{ w io.Writer }

func (t *timestamped) Write(p []byte) (int, error) {
	if _, err := fmt.Fprintf(t.w, "%s %s", time.Now().Format("2006-01-02 15:04:05"), p); err != nil {
		return 0, err
	}
	return len(p), nil
}

// fileResult is the outcome of converting one input with one instruction set.
type fileResult struct {
	input   *input
	matched bool  // the input is an instance of the flow
	err     error // conversion failed; the input contributed no rows
}

// convertInputs converts every input that matches ins into the layout's
// tables, which are always returned (possibly without rows).
func convertInputs(ins *flowtocsv.Instructions, layout flowtocsv.Layout, inputs []*input) ([]*flowtocsv.Table, []fileResult) {
	tables := ins.EmptyTables(layout)
	results := make([]fileResult, len(inputs))
	for i, inp := range inputs {
		results[i].input = inp
		f := inp.parse(ins.Delimiter)
		if f == nil || !ins.Matches(f) {
			continue
		}
		results[i].matched = true
		got, err := convertOne(ins, f, inp.name, layout)
		if err != nil {
			results[i].err = err
			continue
		}
		for j, t := range got {
			tables[j].Rows = append(tables[j].Rows, t.Rows...)
		}
	}
	return tables, results
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
// readInputs reads every input. Files already recorded in st are returned
// separately as done. A file whose content repeats an earlier input in this
// run is attached to that input (dups) and shares its fate.
func readInputs(paths []string, st *state, stderr io.Writer) (inputs []*input, done []string, err error) {
	files, err := expand(paths)
	if err != nil {
		return nil, nil, err
	}
	seen := map[string]*input{}
	for _, p := range files {
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, nil, err
		}
		inp := newInput(p, b)
		if prev, ok := st.Files[inp.hash]; ok {
			fmt.Fprintf(stderr, "%s: skipped, already converted as %s on %s\n", p, prev.Name, prev.Converted.Format("2006-01-02 15:04"))
			done = append(done, p)
			continue
		}
		if prev, ok := seen[inp.hash]; ok {
			fmt.Fprintf(stderr, "%s: skipped, same content as %s\n", p, prev.path)
			prev.dups = append(prev.dups, p)
			continue
		}
		seen[inp.hash] = inp
		inputs = append(inputs, inp)
	}
	return inputs, done, nil
}

func newInput(path string, data []byte) *input {
	sum := sha256.Sum256(data)
	return &input{
		path:   path,
		name:   filepath.Base(path),
		hash:   hex.EncodeToString(sum[:]),
		data:   data,
		parsed: map[string]*flowtocsv.File{},
	}
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
