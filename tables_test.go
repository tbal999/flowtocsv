package flowtocsv

import (
	"reflect"
	"strings"
	"testing"
)

func TestConvertTables(t *testing.T) {
	in := mustLearn(t, "D0150", twoMeters)
	tables, err := in.ConvertTables(mustParse(t, twoMeters), "f.txt")
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]*Table{}
	var names []string
	for _, tb := range tables {
		byName[tb.Name] = tb
		names = append(names, tb.Name)
		for _, row := range tb.Rows {
			if len(row) != len(tb.Columns) {
				t.Fatalf("%s: row width %d != %d columns", tb.Name, len(row), len(tb.Columns))
			}
		}
	}
	wantNames := []string{"d0150_file", "d0150_288", "d0150_289", "d0150_290", "d0150_293", "d0150_08a"}
	if !reflect.DeepEqual(names, wantNames) {
		t.Fatalf("tables = %v, want %v", names, wantNames)
	}

	file := byName["d0150_file"]
	if file.Rows[0][0] != "f.txt" || file.Columns[1] != "hdr_1" || len(file.Rows) != 1 {
		t.Errorf("file table = %v %v", file.Columns, file.Rows)
	}

	// twoMeters lines: 1 ZHV, 2 288, 3 289, 4 290 m1, 5 293 r1, 6 293 r2,
	// 7 290 m2, 8 293 r3, 9 08A, 10 ZPT.
	want := map[string][][]string{
		"d0150_288": {{"f.txt:2", "f.txt", "2", "M1"}},
		"d0150_289": {{"f.txt:3", "f.txt:2", "f.txt", "3", "a"}},
		"d0150_290": {{"f.txt:4", "f.txt:2", "f.txt", "4", "m1"}, {"f.txt:7", "f.txt:2", "f.txt", "7", "m2"}},
		"d0150_293": {{"f.txt:5", "f.txt:4", "f.txt", "5", "r1"}, {"f.txt:6", "f.txt:4", "f.txt", "6", "r2"}, {"f.txt:8", "f.txt:7", "f.txt", "8", "r3"}},
		"d0150_08a": {{"f.txt:9", "f.txt:2", "f.txt", "9", "x"}},
	}
	for name, rows := range want {
		if got := byName[name].Rows; !reflect.DeepEqual(got, rows) {
			t.Errorf("%s rows\n got  %v\n want %v", name, got, rows)
		}
	}

	root := byName["d0150_288"]
	if !reflect.DeepEqual(root.Columns[:3], []string{"id", "source_file", "line"}) {
		t.Errorf("root columns = %v", root.Columns)
	}
	if !reflect.DeepEqual(root.PrimaryKey, []string{"id"}) ||
		!reflect.DeepEqual(root.ForeignKeys, []ForeignKey{{"source_file", "d0150_file", "source_file"}}) {
		t.Errorf("root keys = %v %v", root.PrimaryKey, root.ForeignKeys)
	}
	reg := byName["d0150_293"]
	wantFK := []ForeignKey{{"source_file", "d0150_file", "source_file"}, {"parent_id", "d0150_290", "id"}}
	if !reflect.DeepEqual(reg.ForeignKeys, wantFK) {
		t.Errorf("293 foreign keys = %v", reg.ForeignKeys)
	}
	if !reflect.DeepEqual(file.PrimaryKey, []string{"source_file"}) {
		t.Errorf("file primary key = %v", file.PrimaryKey)
	}
}

// Every foreign key value must exist in the referenced table.
func TestConvertTablesReferentialIntegrity(t *testing.T) {
	in := mustLearn(t, "D0150", twoMeters)
	file := hdr + "288|M1|\n290|m1|\n293|r1|\n288|M2|\n289|b|\n290|m2|\n293|r2|\n293|r3|\n08A|y|\n288|M3|\n" + ftr
	tables, err := in.ConvertTables(mustParse(t, file), "f")
	if err != nil {
		t.Fatal(err)
	}
	keys := map[string]map[string]bool{}
	col := func(tb *Table, name string) int {
		for i, c := range tb.Columns {
			if c == name {
				return i
			}
		}
		t.Fatalf("%s has no column %s", tb.Name, name)
		return -1
	}
	for _, tb := range tables {
		pk := col(tb, tb.PrimaryKey[0])
		keys[tb.Name] = map[string]bool{}
		for _, row := range tb.Rows {
			if keys[tb.Name][row[pk]] {
				t.Errorf("%s: duplicate key %q", tb.Name, row[pk])
			}
			keys[tb.Name][row[pk]] = true
		}
	}
	for _, tb := range tables {
		for _, fk := range tb.ForeignKeys {
			c := col(tb, fk.Column)
			for _, row := range tb.Rows {
				if !keys[fk.Table][row[c]] {
					t.Errorf("%s.%s = %q not found in %s", tb.Name, fk.Column, row[c], fk.Table)
				}
			}
		}
	}
}

func TestLearnInfersTypes(t *testing.T) {
	s := "ZHV|1|D0150001|20240131093000|\n" +
		"288|0158|20240101|9.00|1.5|x|\n" +
		"288|0042|20240202||2|y|\n" +
		"ZPT|1|\n"
	in := mustLearn(t, "D0150", s)
	want := map[string]string{
		"hdr_3":  TypeDateTime,
		"g288_2": TypeDate,
		"g288_3": TypeDecimal,
	}
	if !reflect.DeepEqual(in.Types, want) {
		t.Errorf("types = %v, want %v", in.Types, want)
	}

	tb, err := in.Convert(mustParse(t, s), "f")
	if err != nil {
		t.Fatal(err)
	}
	got := pick(t, tb, "hdr_3", "g288_1", "g288_2", "g288_3")
	if !reflect.DeepEqual(got[0], []string{"2024-01-31 09:30:00", "0158", "2024-01-01", "9.00"}) || got[1][3] != "" {
		t.Errorf("rows = %v", got)
	}
}

func TestConvertRejectsBadTypedValue(t *testing.T) {
	s := hdr + "288|20240101|\n" + ftr
	in := mustLearn(t, "D0150", s)
	_, err := in.Convert(mustParse(t, hdr+"288|20241399|\n"+ftr), "f")
	if err == nil || !strings.Contains(err.Error(), "g288_1") || !strings.Contains(err.Error(), "not a YYYYMMDD date") {
		t.Errorf("err = %v", err)
	}
}

func TestValidateTypesAndReserved(t *testing.T) {
	base := mustLearn(t, "D0150", twoMeters)
	cases := map[string]func(in *Instructions){
		"type on missing column": func(in *Instructions) { in.Types = map[string]string{"nope": TypeDate} },
		"unknown type":           func(in *Instructions) { in.Types = map[string]string{"g288_1": "money"} },
		"reserved id":            func(in *Instructions) { in.Groups[1].Fields[0] = "id" },
		"reserved parent_id":     func(in *Instructions) { in.Header.Fields[0] = "parent_id" },
		"table name collision":   func(in *Instructions) { in.Groups[1].ID = "08a" },
	}
	for name, mutate := range cases {
		in := base
		in.Header.Fields = append([]string(nil), base.Header.Fields...)
		in.Groups = append([]Group(nil), base.Groups...)
		for i := range in.Groups {
			in.Groups[i].Fields = append([]string(nil), base.Groups[i].Fields...)
		}
		mutate(&in)
		if err := in.Validate(); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}
