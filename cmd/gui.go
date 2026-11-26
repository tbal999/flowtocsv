package main

import (
	"archive/zip"
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"github.com/tbal999/flowtocsv"
)

//go:embed gui/index.html
var indexHTML []byte

const (
	maxUpload   = 1 << 30 // per request
	previewRows = 100     // rows per table returned by /api/convert
)

func gui(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("gui", flag.ContinueOnError)
	fs.SetOutput(stderr)
	addr := fs.String("addr", "127.0.0.1:8765", "address to listen on (keep it on localhost)")
	dir := fs.String("dir", "instructions", "instructions directory to list and save into")
	open := fs.Bool("open", true, "open the page in the default browser")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := os.MkdirAll(*dir, 0o755); err != nil {
		return err
	}
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		return err
	}
	url := "http://" + ln.Addr().String()
	fmt.Fprintf(stdout, "flowtocsv GUI at %s (instructions in %s). Press Ctrl+C to stop.\n", url, *dir)
	if *open {
		openBrowser(url)
	}
	return http.Serve(ln, newGUI(*dir))
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start() // best effort; the URL is printed anyway
}

type guiServer struct{ dir string }

func newGUI(dir string) http.Handler {
	s := &guiServer{dir: dir}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.index)
	mux.HandleFunc("GET /api/config", s.config)
	mux.HandleFunc("GET /api/instructions", s.list)
	mux.HandleFunc("GET /api/instructions/{name}", s.get)
	mux.HandleFunc("PUT /api/instructions/{name}", s.put)
	mux.HandleFunc("POST /api/learn", s.learn)
	mux.HandleFunc("POST /api/convert", s.convert)
	mux.HandleFunc("POST /api/export", s.export)
	return localOnly(mux)
}

// localOnly rejects requests that don't come from a page on this machine:
// the Host must be a loopback name (stops DNS rebinding) and state-changing
// requests must not come from another origin (stops cross-site requests).
func localOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil {
			host = r.Host
		}
		if host != "localhost" && host != "127.0.0.1" && host != "::1" {
			http.Error(w, "flowtocsv GUI only answers on localhost", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if o := r.Header.Get("Origin"); o != "" && o != "http://"+r.Host {
				http.Error(w, "cross-origin request refused", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *guiServer) index(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self' 'unsafe-inline'; img-src 'self' data:")
	w.Write(indexHTML)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func (s *guiServer) config(w http.ResponseWriter, r *http.Request) {
	dir, err := filepath.Abs(s.dir)
	if err != nil {
		dir = s.dir
	}
	writeJSON(w, http.StatusOK, map[string]string{"instructions_dir": dir})
}

var safeName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

func (s *guiServer) path(name string) (string, error) {
	if !safeName.MatchString(name) || strings.Contains(name, "..") {
		return "", fmt.Errorf("invalid name %q: use letters, digits, '_', '-' and '.'", name)
	}
	return filepath.Join(s.dir, name+".json"), nil
}

type listEntry struct {
	Name   string `json:"name"`
	Flow   string `json:"flow,omitempty"`
	Groups int    `json:"groups,omitempty"`
	Error  string `json:"error,omitempty"`
}

func (s *guiServer) list(w http.ResponseWriter, r *http.Request) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	out := []listEntry{}
	for _, e := range entries {
		if !e.Type().IsRegular() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		le := listEntry{Name: strings.TrimSuffix(e.Name(), ".json")}
		if in, err := readInstructionsFile(filepath.Join(s.dir, e.Name())); err != nil {
			le.Error = err.Error()
		} else {
			le.Flow, le.Groups = in.Flow, len(in.Groups)
		}
		out = append(out, le)
	}
	writeJSON(w, http.StatusOK, out)
}

func readInstructionsFile(path string) (flowtocsv.Instructions, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return flowtocsv.Instructions{}, err
	}
	return flowtocsv.ReadInstructions(bytes.NewReader(b))
}

func (s *guiServer) get(w http.ResponseWriter, r *http.Request) {
	path, err := s.path(r.PathValue("name"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	in, err := readInstructionsFile(path)
	if errors.Is(err, os.ErrNotExist) {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err)
		return
	}
	writeJSON(w, http.StatusOK, in)
}

func (s *guiServer) put(w http.ResponseWriter, r *http.Request) {
	path, err := s.path(r.PathValue("name"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	in, err := flowtocsv.ReadInstructions(http.MaxBytesReader(w, r.Body, 16<<20))
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err)
		return
	}
	if err := writeFile(path, in.WriteJSON); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"saved": path})
}

// uploads reads the multipart "files" field into inputs.
func uploads(w http.ResponseWriter, r *http.Request) ([]*input, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload)
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		return nil, fmt.Errorf("reading upload: %w", err)
	}
	var inputs []*input
	for _, fh := range r.MultipartForm.File["files"] {
		f, err := fh.Open()
		if err != nil {
			return nil, err
		}
		b, err := io.ReadAll(f)
		f.Close()
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, newInput(filepath.Base(fh.Filename), b))
	}
	if len(inputs) == 0 {
		return nil, errors.New("no files uploaded")
	}
	return inputs, nil
}

func (s *guiServer) learn(w http.ResponseWriter, r *http.Request) {
	inputs, err := uploads(w, r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	delim := r.FormValue("delimiter")
	if delim == "" {
		delim = "|"
	}
	var files []*flowtocsv.File
	for _, inp := range inputs {
		f, err := flowtocsv.Parse(bytes.NewReader(inp.data), delim)
		if err != nil {
			writeErr(w, http.StatusUnprocessableEntity, fmt.Errorf("%s: %w", inp.name, err))
			return
		}
		files = append(files, f)
	}
	flow := strings.TrimSpace(r.FormValue("flow"))
	if flow == "" {
		flow = strings.TrimSuffix(inputs[0].name, filepath.Ext(inputs[0].name))
	}
	in, err := flowtocsv.Learn(flow, files...)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"instructions": in, "samples": sampleValues(&in, files)})
}

// sampleValues maps each column to its first non-empty value in the
// samples, to help whoever renames the columns.
func sampleValues(in *flowtocsv.Instructions, files []*flowtocsv.File) map[string]string {
	out := map[string]string{}
	set := func(names, values []string) {
		for i, v := range values {
			if i < len(names) && v != "" {
				if _, ok := out[names[i]]; !ok {
					out[names[i]] = v
				}
			}
		}
	}
	groups := map[string][]string{}
	for _, g := range in.Groups {
		groups[g.ID] = g.Fields
	}
	for _, f := range files {
		set(in.Header.Fields, f.Header.Fields)
		set(in.Footer.Fields, f.Footer.Fields)
		for _, r := range f.Records {
			set(groups[r.ID], r.Fields)
		}
	}
	return out
}

type convertRequest struct {
	ins     flowtocsv.Instructions
	layout  flowtocsv.Layout
	tables  []*flowtocsv.Table
	results []fileResult
}

func runConvert(w http.ResponseWriter, r *http.Request) (*convertRequest, bool) {
	inputs, err := uploads(w, r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return nil, false
	}
	ins, err := flowtocsv.ReadInstructions(strings.NewReader(r.FormValue("instructions")))
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err)
		return nil, false
	}
	layout := flowtocsv.Layout(r.FormValue("layout"))
	if layout != flowtocsv.LayoutTables {
		layout = flowtocsv.LayoutWide
	}
	c := &convertRequest{ins: ins, layout: layout}
	c.tables, c.results = convertInputs(&c.ins, layout, inputs)
	return c, true
}

type fileStatus struct {
	Name   string `json:"name"`
	Status string `json:"status"` // converted, failed, skipped
	Error  string `json:"error,omitempty"`
}

func statuses(results []fileResult, flow string) []fileStatus {
	var out []fileStatus
	for _, r := range results {
		st := fileStatus{Name: r.input.name, Status: "converted"}
		switch {
		case !r.matched:
			st.Status, st.Error = "skipped", "not a "+flow+" file"
		case r.err != nil:
			st.Status, st.Error = "failed", r.err.Error()
		}
		out = append(out, st)
	}
	return out
}

func (s *guiServer) convert(w http.ResponseWriter, r *http.Request) {
	c, ok := runConvert(w, r)
	if !ok {
		return
	}
	type preview struct {
		*flowtocsv.Table
		TotalRows int `json:"total_rows"`
	}
	var tables []preview
	for _, t := range c.tables {
		p := preview{Table: &flowtocsv.Table{
			Name: t.Name, PrimaryKey: t.PrimaryKey, ForeignKeys: t.ForeignKeys,
			Columns: t.Columns, Rows: t.Rows[:min(len(t.Rows), previewRows)],
		}, TotalRows: len(t.Rows)}
		if p.Rows == nil {
			p.Rows = [][]string{}
		}
		tables = append(tables, p)
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": statuses(c.results, c.ins.Flow), "tables": tables})
}

func (s *guiServer) export(w http.ResponseWriter, r *http.Request) {
	c, ok := runConvert(w, r)
	if !ok {
		return
	}
	format := r.FormValue("format")
	if format != "json" {
		format = "csv"
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, t := range c.tables {
		fw, err := zw.Create(t.Name + "." + format)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		if err := writer(t, format)(fw); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
	}
	var problems []string
	for _, st := range statuses(c.results, c.ins.Flow) {
		if st.Status != "converted" {
			problems = append(problems, fmt.Sprintf("%s: %s: %s", st.Name, st.Status, st.Error))
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		fw, _ := zw.Create("_not_converted.txt")
		io.WriteString(fw, strings.Join(problems, "\n")+"\n")
	}
	if err := zw.Close(); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	name := fmt.Sprintf("%s-%s-%s.zip", strings.ToLower(c.ins.Flow), c.layout, format)
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, name))
	w.Write(buf.Bytes())
}
