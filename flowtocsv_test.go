package flowtocsv

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

const (
	hdr = "ZHV|99999|D0150001|M|LBSL|X|NEEB|20179568182352||||TR01|\n"
	ftr = "ZPT|99999|6||1|20179568182352|\n"
)

// d0150 is a single-meter sample: nothing proves any nesting, so every group
// hangs off 288 and singletons are copied onto each 293 row.
const d0150 = hdr +
	"288|127227343996|20179568|D|x|\n" +
	"289|0158|20179568|Z||\n" +
	"290|XXXTEST011|||40|F|L&G CL27|LBSL|||||||||||K|20179568|20179568|20200101|||H|20179568|\n" +
	"293|L|C|AI|9.00||9||DE|\n" +
	"293|N|C|AI|9.00||7|||\n" +
	"08A|KXXA 12387|20179568|LBSL|\n" +
	ftr

// twoMeters shows 290, 293, 290: registers belong to individual meters.
const twoMeters = hdr +
	"288|M1|\n" +
	"289|a|\n" +
	"290|m1|\n" +
	"293|r1|\n" +
	"293|r2|\n" +
	"290|m2|\n" +
	"293|r3|\n" +
	"08A|x|\n" +
	ftr

func mustParse(t *testing.T, s string) *File {
	t.Helper()
	f, err := Parse(strings.NewReader(s), "|")
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func mustLearn(t *testing.T, flow string, samples ...string) Instructions {
	t.Helper()
	var files []*File
	for _, s := range samples {
		files = append(files, mustParse(t, s))
	}
	in, err := Learn(flow, files...)
	if err != nil {
		t.Fatal(err)
	}
	return in
}

// pick returns the named columns of every row, for compact assertions.
func pick(t *testing.T, tb *Table, cols ...string) [][]string {
	t.Helper()
	pos := map[string]int{}
	for i, c := range tb.Columns {
		pos[c] = i
	}
	var out [][]string
	for _, row := range tb.Rows {
		if len(row) != len(tb.Columns) {
			t.Fatalf("row width %d != %d columns", len(row), len(tb.Columns))
		}
		var r []string
		for _, c := range cols {
			i, ok := pos[c]
			if !ok {
				t.Fatalf("no column %q in %v", c, tb.Columns)
			}
			r = append(r, row[i])
		}
		out = append(out, r)
	}
	return out
}

func TestParse(t *testing.T) {
	in := "\r\nZHV|1|D0150001|\r\n\r\n288|\"a\"|b||\r\n293|\r\nZPT|2|"
	f, err := Parse(strings.NewReader(in), "|")
	if err != nil {
		t.Fatal(err)
	}
	want := &File{
		Delimiter: "|",
		Header:    Record{ID: "ZHV", Fields: []string{"1", "D0150001"}, Line: 2},
		Records: []Record{
			{ID: "288", Fields: []string{"a", "b", ""}, Line: 4},
			{ID: "293", Fields: []string{}, Line: 5},
		},
		Footer: Record{ID: "ZPT", Fields: []string{"2"}, Line: 6},
	}
	if !reflect.DeepEqual(f, want) {
		t.Errorf("got  %+v\nwant %+v", f, want)
	}
}

func TestParseErrors(t *testing.T) {
	if _, err := Parse(strings.NewReader("ZHV|1|\n"), "|"); err == nil {
		t.Error("single line: want error")
	}
	if _, err := Parse(strings.NewReader(d0150), ""); err == nil {
		t.Error("empty delimiter: want error")
	}
}

func TestLearn(t *testing.T) {
	in := mustLearn(t, "D0150", d0150)
	if in.Header.ID != "ZHV" || len(in.Header.Fields) != 11 || in.Footer.ID != "ZPT" || len(in.Footer.Fields) != 5 {
		t.Errorf("header/footer = %+v / %+v", in.Header, in.Footer)
	}
	type g struct {
		id, parent string
		width      int
	}
	var got []g
	for _, x := range in.Groups {
		got = append(got, g{x.ID, x.Parent, len(x.Fields)})
	}
	want := []g{{"288", "", 4}, {"289", "288", 4}, {"290", "288", 25}, {"293", "288", 8}, {"08A", "288", 3}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("groups\n got  %v\n want %v", got, want)
	}
	if in.Groups[4].Fields[0] != "g08a_1" || in.Header.Fields[0] != "hdr_1" || in.Footer.Fields[0] != "ftr_1" {
		t.Errorf("unexpected generated names: %v %v %v", in.Groups[4].Fields, in.Header.Fields, in.Footer.Fields)
	}
}

func TestLearnInfersNesting(t *testing.T) {
	in := mustLearn(t, "D0150", twoMeters)
	parents := map[string]string{}
	for _, g := range in.Groups {
		parents[g.ID] = g.Parent
	}
	want := map[string]string{"288": "", "289": "288", "290": "288", "293": "290", "08A": "288"}
	if !reflect.DeepEqual(parents, want) {
		t.Errorf("got %v want %v", parents, want)
	}
}

func TestLearnNestingNeedsSameParentInstance(t *testing.T) {
	// 290 and 293 alternate only across different 288s, which says nothing
	// about 293 belonging to 290.
	s := hdr + "288|A|\n290|m|\n293|r|\n288|B|\n290|m|\n293|r|\n" + ftr
	in := mustLearn(t, "D0150", s)
	if p := in.Groups[2].Parent; p != "288" {
		t.Errorf("293 parent = %q, want 288", p)
	}
}

func TestLearnMergesSamples(t *testing.T) {
	a := hdr + "288|M1|\n290|m1|\n293|r1|\n" + ftr
	b := hdr + "288|M1|\n290|m1|\n293|r1|\n290|m2|wide|\n293|r2|\n" + ftr
	in := mustLearn(t, "D0150", a, b)
	if in.Groups[1].Parent != "288" || in.Groups[2].Parent != "290" || len(in.Groups[1].Fields) != 2 {
		t.Errorf("groups = %+v", in.Groups)
	}
}

func TestLearnRejectsMixedSamples(t *testing.T) {
	a := mustParse(t, d0150)
	b, _ := Parse(strings.NewReader(strings.ReplaceAll(d0150, "|", ",")), ",")
	if _, err := Learn("D0150", a, b); err == nil {
		t.Error("want error for mixed delimiters")
	}
}

func TestConvertCopiesSingletons(t *testing.T) {
	in := mustLearn(t, "D0150", d0150)
	tb, err := in.Convert(mustParse(t, d0150), "f.txt")
	if err != nil {
		t.Fatal(err)
	}
	got := pick(t, tb, "source_file", "row_index", "hdr_1", "g288_1", "g289_1", "g290_1", "g293_1", "g293_6", "g08a_1", "ftr_2")
	want := [][]string{
		{"f.txt", "1", "99999", "127227343996", "0158", "XXXTEST011", "L", "9", "KXXA 12387", "6"},
		{"f.txt", "2", "99999", "127227343996", "0158", "XXXTEST011", "N", "7", "KXXA 12387", "6"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %v\nwant %v", got, want)
	}
}

func TestConvertNested(t *testing.T) {
	in := mustLearn(t, "D0150", twoMeters)
	tb, err := in.Convert(mustParse(t, twoMeters), "f")
	if err != nil {
		t.Fatal(err)
	}
	got := pick(t, tb, "g288_1", "g289_1", "g290_1", "g293_1", "g08a_1")
	want := [][]string{
		{"M1", "a", "m1", "r1", "x"},
		{"M1", "a", "m1", "r2", "x"},
		{"M1", "a", "m2", "r3", "x"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %v\nwant %v", got, want)
	}
}

func TestConvertBlocksAndMissingGroups(t *testing.T) {
	in := mustLearn(t, "D0150", twoMeters)
	file := hdr +
		"288|M1|\n290|m1|\n293|r1|\n" + // no 289, no 08A
		"288|M2|\n289|b|\n290|m2|\n" + // meter without registers
		"288|M3|\n" + // MPAN only
		ftr
	tb, err := in.Convert(mustParse(t, file), "f")
	if err != nil {
		t.Fatal(err)
	}
	got := pick(t, tb, "row_index", "g288_1", "g289_1", "g290_1", "g293_1", "g08a_1")
	want := [][]string{
		{"1", "M1", "", "m1", "r1", ""},
		{"2", "M2", "b", "m2", "", ""},
		{"3", "M3", "", "", "", ""},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %v\nwant %v", got, want)
	}
}

func TestConvertCRLF(t *testing.T) {
	in := mustLearn(t, "D0150", d0150)
	tb, err := in.Convert(mustParse(t, strings.ReplaceAll(d0150, "\n", "\r\n")), "f")
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range tb.Rows {
		for _, v := range row {
			if strings.ContainsRune(v, '\r') {
				t.Fatalf("carriage return in %q", v)
			}
		}
	}
}

func TestConvertErrors(t *testing.T) {
	in := mustLearn(t, "D0150", twoMeters)
	cases := map[string]struct {
		file string
		want string
	}{
		"unknown group":   {hdr + "288|M1|\n999|?|\n" + ftr, `group "999" is not in`},
		"too many fields": {hdr + "288|M1|extra|\n" + ftr, "288 has 2 fields, instructions allow 1"},
		"orphan child":    {hdr + "293|r1|\n" + ftr, "293 appears outside a 290 group"},
		"truncated":       {hdr + "288|M1|\n290|m1|\n", "want footer"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := in.Convert(mustParse(t, c.file), "f")
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("err = %v, want containing %q", err, c.want)
			}
		})
	}
}

func TestConvertToleratesTrailingEmptyFields(t *testing.T) {
	in := mustLearn(t, "D0150", twoMeters)
	if _, err := in.Convert(mustParse(t, hdr+"288|M1|||\n"+ftr), "f"); err != nil {
		t.Error(err)
	}
}

func TestConvertNoMatch(t *testing.T) {
	in := mustLearn(t, "D0150", d0150)
	other := mustParse(t, strings.Replace(d0150, "D0150001", "D0010002", 1))
	if in.Matches(other) {
		t.Error("D0010 should not match D0150 instructions")
	}
	if _, err := in.Convert(other, "f"); !errors.Is(err, ErrNoMatch) {
		t.Errorf("err = %v, want ErrNoMatch", err)
	}
}

func TestInstructionsJSONRoundTrip(t *testing.T) {
	in := mustLearn(t, "D0150", twoMeters)
	var buf bytes.Buffer
	if err := in.WriteJSON(&buf); err != nil {
		t.Fatal(err)
	}
	back, err := ReadInstructions(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back, in) {
		t.Errorf("round trip changed instructions:\n%+v\n%+v", back, in)
	}
}

func TestReadInstructionsRejectsBadInput(t *testing.T) {
	cases := map[string]string{
		"old format":     `{"Dataflow":"150","Delimiter":"|","DataItems":["288"],"Spaces":[4],"Headers":[],"Outputname":"x"}`,
		"unknown parent": `{"flow":"D","delimiter":"|","header":{"id":"ZHV","fields":[]},"footer":{"id":"ZPT","fields":[]},"groups":[{"id":"1","fields":["a"]},{"id":"2","parent":"9","fields":["b"]}]}`,
		"root parent":    `{"flow":"D","delimiter":"|","header":{"id":"ZHV","fields":[]},"footer":{"id":"ZPT","fields":[]},"groups":[{"id":"1","parent":"1","fields":["a"]}]}`,
		"dup column":     `{"flow":"D","delimiter":"|","header":{"id":"ZHV","fields":["a"]},"footer":{"id":"ZPT","fields":[]},"groups":[{"id":"1","fields":["a"]}]}`,
		"reserved col":   `{"flow":"D","delimiter":"|","header":{"id":"ZHV","fields":["row_index"]},"footer":{"id":"ZPT","fields":[]},"groups":[{"id":"1","fields":["a"]}]}`,
	}
	for name, js := range cases {
		if _, err := ReadInstructions(strings.NewReader(js)); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestTableRoundTrips(t *testing.T) {
	in := mustLearn(t, "D0150", d0150)
	tb, err := in.Convert(mustParse(t, d0150), `a "quoted", file`)
	if err != nil {
		t.Fatal(err)
	}

	var csvBuf bytes.Buffer
	if err := tb.WriteCSV(&csvBuf); err != nil {
		t.Fatal(err)
	}
	fromCSV, err := ReadCSV(&csvBuf)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fromCSV.Columns, tb.Columns) || !reflect.DeepEqual(fromCSV.Rows, tb.Rows) {
		t.Error("CSV round trip changed the columns or rows")
	}

	js, err := json.Marshal(tb)
	if err != nil {
		t.Fatal(err)
	}
	var fromJSON Table
	if err := json.Unmarshal(js, &fromJSON); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(&fromJSON, tb) {
		t.Error("JSON round trip changed the table")
	}
}

func TestTableAppend(t *testing.T) {
	var all Table
	a := &Table{Columns: []string{"x"}, Rows: [][]string{{"1"}}}
	if err := all.Append(a); err != nil {
		t.Fatal(err)
	}
	if err := all.Append(a); err != nil {
		t.Fatal(err)
	}
	if len(all.Rows) != 2 {
		t.Errorf("rows = %d", len(all.Rows))
	}
	if err := all.Append(&Table{Columns: []string{"y"}}); err == nil {
		t.Error("want error for mismatched columns")
	}
}
