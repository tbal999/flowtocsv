package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tbal999/flowtocsv"
)

const sample = "ZHV|1|D0150001|M|A|X|B|20240101|\n" +
	"288|M1|\n290|m1|\n293|r1|\n293|r2|\n290|m2|\n293|r3|\n08A|x|\n" +
	"ZPT|1|6|\n"

func writeT(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runT(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := run(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func readCSV(t *testing.T, path string) *flowtocsv.Table {
	t.Helper()
	fh, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer fh.Close()
	tb, err := flowtocsv.ReadCSV(fh)
	if err != nil {
		t.Fatal(err)
	}
	return tb
}

// setup learns D0150 from sample and returns the instructions path.
func setup(t *testing.T, dir string) string {
	t.Helper()
	writeT(t, filepath.Join(dir, "learn", "D0150.txt"), sample)
	instr := filepath.Join(dir, "instr", "D0150.json")
	if err := os.MkdirAll(filepath.Dir(instr), 0o755); err != nil {
		t.Fatal(err)
	}
	if code, _, stderr := runT(t, "learn", "-o", instr, filepath.Join(dir, "learn", "D0150.txt")); code != 0 {
		t.Fatalf("learn: %s", stderr)
	}
	return instr
}

func TestLearnThenConvertWide(t *testing.T) {
	dir := t.TempDir()
	instr := setup(t, dir)
	writeT(t, filepath.Join(dir, "in", "a.txt"), sample)
	writeT(t, filepath.Join(dir, "in", "b.txt"), strings.Replace(sample, "M1", "M2", 1))
	writeT(t, filepath.Join(dir, "in", "other.txt"), strings.Replace(sample, "D0150001", "D0010002", 1))

	out := filepath.Join(dir, "out")
	code, stdout, stderr := runT(t, "convert", "-i", filepath.Dir(instr), "-o", out, filepath.Join(dir, "in"))
	if code != 0 {
		t.Fatalf("convert exit %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "D0150: 2 files") || !strings.Contains(stdout, "(6 rows)") {
		t.Errorf("stdout = %q", stdout)
	}
	if !strings.Contains(stderr, "other.txt: skipped, matches no instructions") {
		t.Errorf("stderr = %q", stderr)
	}
	tb := readCSV(t, filepath.Join(out, "d0150.csv"))
	if len(tb.Rows) != 6 || tb.Columns[0] != "source_file" || tb.Rows[3][0] != "b.txt" {
		t.Errorf("table: %d rows, columns %v, row 4 %v", len(tb.Rows), tb.Columns, tb.Rows[3])
	}

	code, _, stderr = runT(t, "convert", "-format", "json", "-i", instr, "-o", out, filepath.Join(dir, "in", "a.txt"))
	if code != 0 {
		t.Fatalf("convert json: %s", stderr)
	}
	b, err := os.ReadFile(filepath.Join(out, "d0150.json"))
	if err != nil {
		t.Fatal(err)
	}
	var jt flowtocsv.Table
	if err := json.Unmarshal(b, &jt); err != nil || len(jt.Rows) != 3 || jt.Name != "d0150" {
		t.Errorf("json output: %d rows, name %q, err %v", len(jt.Rows), jt.Name, err)
	}
}

func TestConvertTablesLayout(t *testing.T) {
	dir := t.TempDir()
	instr := setup(t, dir)
	writeT(t, filepath.Join(dir, "in", "a.txt"), sample)
	out := filepath.Join(dir, "out")
	if code, _, stderr := runT(t, "convert", "-layout", "tables", "-i", instr, "-o", out, filepath.Join(dir, "in")); code != 0 {
		t.Fatalf("convert: %s", stderr)
	}
	rows := map[string]int{"d0150_file": 1, "d0150_288": 1, "d0150_290": 2, "d0150_293": 3, "d0150_08a": 1}
	for name, n := range rows {
		tb := readCSV(t, filepath.Join(out, name+".csv"))
		if len(tb.Rows) != n {
			t.Errorf("%s: %d rows, want %d", name, len(tb.Rows), n)
		}
	}
	reg := readCSV(t, filepath.Join(out, "d0150_293.csv"))
	if strings.Join(reg.Columns[:4], ",") != "id,parent_id,source_file,line" || reg.Rows[2][1] != "a.txt:6" {
		t.Errorf("293 table: %v %v", reg.Columns, reg.Rows)
	}
}

func TestStateSkipsConvertedFiles(t *testing.T) {
	dir := t.TempDir()
	instr := setup(t, dir)
	in := filepath.Join(dir, "in")
	out := filepath.Join(dir, "out")
	state := filepath.Join(dir, "state.json")
	writeT(t, filepath.Join(in, "a.txt"), sample)
	convert := func() (int, string, string) {
		return runT(t, "convert", "-state", state, "-i", instr, "-o", out, in)
	}

	if code, _, stderr := convert(); code != 0 {
		t.Fatal(stderr)
	}
	if n := len(readCSV(t, filepath.Join(out, "d0150.csv")).Rows); n != 3 {
		t.Fatalf("first run: %d rows", n)
	}

	// Day 2: a new file arrives, plus a re-delivery of a.txt under a new name.
	writeT(t, filepath.Join(in, "b.txt"), strings.Replace(sample, "M1", "M2", 1))
	writeT(t, filepath.Join(in, "a-resent.txt"), sample)
	code, _, stderr := convert()
	if code != 0 {
		t.Fatal(stderr)
	}
	tb := readCSV(t, filepath.Join(out, "d0150.csv"))
	if len(tb.Rows) != 3 || tb.Rows[0][0] != "b.txt" {
		t.Errorf("second run should hold only b.txt: %v", tb.Rows)
	}
	if !strings.Contains(stderr, "a.txt: skipped, already converted") || !strings.Contains(stderr, "a-resent.txt: skipped, already converted as a.txt") {
		t.Errorf("stderr = %q", stderr)
	}

	// Day 3: nothing new. Output must be emptied, not left holding b.txt.
	if code, _, stderr := convert(); code != 0 {
		t.Fatal(stderr)
	}
	if n := len(readCSV(t, filepath.Join(out, "d0150.csv")).Rows); n != 0 {
		t.Errorf("third run: %d rows, want 0", n)
	}
}

func TestStateDoesNotRecordFailures(t *testing.T) {
	dir := t.TempDir()
	instr := setup(t, dir)
	state := filepath.Join(dir, "state.json")
	bad := filepath.Join(dir, "in", "bad.txt")
	writeT(t, bad, strings.Replace(sample, "08A|x|", "999|?|", 1))
	if code, _, _ := runT(t, "convert", "-state", state, "-i", instr, "-o", filepath.Join(dir, "out"), bad); code != exitPartial {
		t.Fatalf("exit %d, want 3", code)
	}
	if _, err := os.Stat(state); err == nil {
		t.Error("state written although nothing converted")
	}
}

func TestDuplicateContentInOneRun(t *testing.T) {
	dir := t.TempDir()
	instr := setup(t, dir)
	writeT(t, filepath.Join(dir, "in", "a.txt"), sample)
	writeT(t, filepath.Join(dir, "in", "b.txt"), sample)
	code, _, stderr := runT(t, "convert", "-i", instr, "-o", filepath.Join(dir, "out"), filepath.Join(dir, "in"))
	if code != 0 || !strings.Contains(stderr, "b.txt: skipped, same content as") {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
}

func TestConvertReportsBadFile(t *testing.T) {
	dir := t.TempDir()
	instr := setup(t, dir)
	writeT(t, filepath.Join(dir, "bad.txt"), strings.Replace(sample, "08A|x|", "999|?|", 1))
	writeT(t, filepath.Join(dir, "good.txt"), sample)
	code, stdout, stderr := runT(t, "convert", "-i", instr, "-o", filepath.Join(dir, "out"),
		filepath.Join(dir, "bad.txt"), filepath.Join(dir, "good.txt"))
	if code != exitPartial {
		t.Errorf("exit = %d, want 3", code)
	}
	if !strings.Contains(stderr, `bad.txt: line 8: group "999"`) || !strings.Contains(stdout, "1 files") || !strings.Contains(stdout, "(3 rows)") {
		t.Errorf("stdout %q\nstderr %q", stdout, stderr)
	}
}

func TestUsageErrors(t *testing.T) {
	for _, args := range [][]string{
		{"nope"}, {"learn"}, {"convert"},
		{"convert", "-format", "xml", "-i", "x", "y"},
		{"convert", "-layout", "star", "-i", "x", "y"},
	} {
		if code, _, _ := runT(t, args...); code != 2 {
			t.Errorf("%v: exit %d, want 2", args, code)
		}
	}
}
