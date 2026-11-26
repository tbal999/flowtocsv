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

func TestLearnThenConvert(t *testing.T) {
	dir := t.TempDir()
	writeT(t, filepath.Join(dir, "learn", "D0150.txt"), sample)
	writeT(t, filepath.Join(dir, "in", "a.txt"), sample)
	writeT(t, filepath.Join(dir, "in", "b.txt"), strings.Replace(sample, "M1", "M2", 1))
	writeT(t, filepath.Join(dir, "in", "other.txt"), strings.Replace(sample, "D0150001", "D0010002", 1))
	instr := filepath.Join(dir, "instr")
	if err := os.Mkdir(instr, 0o755); err != nil {
		t.Fatal(err)
	}

	code, _, stderr := runT(t, "learn", "-o", filepath.Join(instr, "D0150.json"), filepath.Join(dir, "learn", "D0150.txt"))
	if code != 0 {
		t.Fatalf("learn exit %d: %s", code, stderr)
	}

	out := filepath.Join(dir, "out")
	code, stdout, stderr := runT(t, "convert", "-i", instr, "-o", out, filepath.Join(dir, "in"))
	if code != 0 {
		t.Fatalf("convert exit %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "D0150: 2 files, 6 rows") {
		t.Errorf("stdout = %q", stdout)
	}
	if !strings.Contains(stderr, "other.txt: skipped") {
		t.Errorf("stderr should report the unmatched file: %q", stderr)
	}

	fh, err := os.Open(filepath.Join(out, "D0150.csv"))
	if err != nil {
		t.Fatal(err)
	}
	defer fh.Close()
	tb, err := flowtocsv.ReadCSV(fh)
	if err != nil {
		t.Fatal(err)
	}
	if len(tb.Rows) != 6 || tb.Columns[0] != "source_file" || tb.Rows[3][0] != "b.txt" {
		t.Errorf("unexpected table: %v rows, columns %v, row 4 %v", len(tb.Rows), tb.Columns, tb.Rows[3])
	}

	code, _, stderr = runT(t, "convert", "-format", "json", "-i", instr, "-o", out, filepath.Join(dir, "in", "a.txt"))
	if code != 0 {
		t.Fatalf("convert json exit %d: %s", code, stderr)
	}
	b, err := os.ReadFile(filepath.Join(out, "D0150.json"))
	if err != nil {
		t.Fatal(err)
	}
	var jt flowtocsv.Table
	if err := json.Unmarshal(b, &jt); err != nil || len(jt.Rows) != 3 {
		t.Errorf("json output: %v rows, err %v", len(jt.Rows), err)
	}
}

func TestConvertReportsBadFile(t *testing.T) {
	dir := t.TempDir()
	writeT(t, filepath.Join(dir, "s.txt"), sample)
	writeT(t, filepath.Join(dir, "bad.txt"), strings.Replace(sample, "08A|x|", "999|?|", 1))
	writeT(t, filepath.Join(dir, "good.txt"), sample)
	instr := filepath.Join(dir, "D0150.json")
	if code, _, e := runT(t, "learn", "-flow", "D0150", "-o", instr, filepath.Join(dir, "s.txt")); code != 0 {
		t.Fatal(e)
	}
	code, stdout, stderr := runT(t, "convert", "-i", instr, "-o", filepath.Join(dir, "out"),
		filepath.Join(dir, "bad.txt"), filepath.Join(dir, "good.txt"))
	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if !strings.Contains(stderr, `bad.txt: line 8: group "999"`) || !strings.Contains(stdout, "1 files, 3 rows") {
		t.Errorf("stdout %q\nstderr %q", stdout, stderr)
	}
}

func TestUsageErrors(t *testing.T) {
	for _, args := range [][]string{nil, {"nope"}, {"learn"}, {"convert"}, {"convert", "-format", "xml", "-i", "x", "y"}} {
		if code, _, _ := runT(t, args...); code != 2 {
			t.Errorf("%v: exit %d, want 2", args, code)
		}
	}
}
