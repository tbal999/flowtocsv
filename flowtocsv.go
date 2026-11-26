// Package flowtocsv flattens UK energy industry dataflows (MRASCO / DTC style
// flat files such as D0150 or D0300) into tables that load cleanly into SQL.
//
// A dataflow is a header line (e.g. ZHV), group records, and a footer line
// (e.g. ZPT). Every record starts with a group ID ("288", "290", ...) and
// groups nest: a 293 register belongs to a 290 meter, which belongs to a 288
// MPAN. Instructions describe that structure. Learn infers Instructions from
// sample files; Convert uses them to turn a parsed File into a Table with one
// row per combination of records.
//
// Typical use:
//
//	f, _ := flowtocsv.Parse(r, "|")
//	ins, _ := flowtocsv.Learn("D0150", f)  // once; save with ins.WriteJSON
//	t, _ := ins.Convert(f, "D0150001.txt") // every file
//	_ = t.WriteCSV(w)                      // or json.Marshal(t)
package flowtocsv

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// Metadata columns added by Convert and ConvertTables. Fields may not use
// these names.
const (
	ColumnSourceFile = "source_file" // the source name passed to Convert
	ColumnRowIndex   = "row_index"   // wide: 1-based row number within the source
	ColumnID         = "id"          // tables: "<source_file>:<line>", the record's key
	ColumnParentID   = "parent_id"   // tables: the parent record's id
	ColumnLine       = "line"        // tables: the record's line in the source
)

var reservedColumns = map[string]bool{
	ColumnSourceFile: true, ColumnRowIndex: true, ColumnID: true, ColumnParentID: true, ColumnLine: true,
}

// Layout selects the shape of converted output.
type Layout string

const (
	// LayoutWide is one table per flow with one row per combination of
	// records (see Convert).
	LayoutWide Layout = "wide"
	// LayoutTables is a file table plus one table per group, linked by
	// primary and foreign keys (see ConvertTables).
	LayoutTables Layout = "tables"
)

// recordID is the primary key of a group record in LayoutTables.
func recordID(source string, line int) string {
	return fmt.Sprintf("%s:%d", source, line)
}

// EmptyTables returns the tables a layout produces, without rows, in output
// order. For LayoutTables the file table comes first, then one table per
// group in instruction order.
func (in *Instructions) EmptyTables(l Layout) []*Table {
	base := sanitize(in.Flow)
	if l == LayoutWide {
		return []*Table{{
			Name:       base,
			PrimaryKey: []string{ColumnSourceFile, ColumnRowIndex},
			Columns:    in.Columns(),
		}}
	}

	fileTable := &Table{
		Name:       base + "_file",
		PrimaryKey: []string{ColumnSourceFile},
		Columns:    append(append([]string{ColumnSourceFile}, in.Header.Fields...), in.Footer.Fields...),
	}
	tables := []*Table{fileTable}
	byGroup := map[string]string{}
	for i, g := range in.Groups {
		t := &Table{
			Name:        base + "_" + sanitize(g.ID),
			PrimaryKey:  []string{ColumnID},
			ForeignKeys: []ForeignKey{{Column: ColumnSourceFile, Table: fileTable.Name, References: ColumnSourceFile}},
		}
		meta := []string{ColumnID, ColumnSourceFile, ColumnLine}
		if i > 0 {
			meta = []string{ColumnID, ColumnParentID, ColumnSourceFile, ColumnLine}
			t.ForeignKeys = append(t.ForeignKeys, ForeignKey{Column: ColumnParentID, Table: byGroup[g.Parent], References: ColumnID})
		}
		t.Columns = append(meta, g.Fields...)
		byGroup[g.ID] = t.Name
		tables = append(tables, t)
	}
	return tables
}

// Instructions describe how to flatten one dataflow. They are stored as JSON.
// After Learn generates them you can rename any field (column) to something
// meaningful, and fix a group's Parent if the sample didn't show the nesting.
type Instructions struct {
	// Flow identifies the dataflow; a file matches when one of the first three
	// header fields contains it (e.g. "D0150" matches "D0150001").
	Flow      string  `json:"flow"`
	Delimiter string  `json:"delimiter"`
	Header    Segment `json:"header"`
	Footer    Segment `json:"footer"`
	// Groups are in file order. The first group is the root: each root record
	// starts a new block of rows. Every other group names its Parent.
	Groups []Group `json:"groups"`
	// Types maps column names to TypeDate, TypeDateTime or TypeDecimal.
	// Unlisted columns are text.
	Types map[string]string `json:"types,omitempty"`
}

// Segment is the header or footer line of a dataflow.
type Segment struct {
	ID     string   `json:"id"`
	Fields []string `json:"fields"`
}

// Group is one record type within a dataflow.
type Group struct {
	ID     string   `json:"id"`
	Parent string   `json:"parent,omitempty"`
	Fields []string `json:"fields"`
}

// ReadInstructions decodes and validates JSON instructions.
func ReadInstructions(r io.Reader) (Instructions, error) {
	var in Instructions
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return Instructions{}, fmt.Errorf("decoding instructions: %w", err)
	}
	if err := in.Validate(); err != nil {
		return Instructions{}, err
	}
	return in, nil
}

// WriteJSON writes the instructions as indented JSON.
func (in *Instructions) WriteJSON(w io.Writer) error {
	b, err := json.MarshalIndent(in, "", "  ")
	if err != nil {
		return err
	}
	_, err = w.Write(append(b, '\n'))
	return err
}

// Validate checks the instructions are internally consistent.
func (in *Instructions) Validate() error {
	switch {
	case in.Flow == "":
		return errors.New("instructions: flow is empty")
	case in.Delimiter == "":
		return fmt.Errorf("instructions %s: delimiter is empty", in.Flow)
	case in.Header.ID == "" || in.Footer.ID == "":
		return fmt.Errorf("instructions %s: header and footer need an id", in.Flow)
	case len(in.Groups) == 0:
		return fmt.Errorf("instructions %s: no groups", in.Flow)
	}

	seen := map[string]bool{}
	for i, g := range in.Groups {
		switch {
		case g.ID == "":
			return fmt.Errorf("instructions %s: group %d has no id", in.Flow, i+1)
		case seen[g.ID]:
			return fmt.Errorf("instructions %s: group %s listed twice", in.Flow, g.ID)
		case i == 0 && g.Parent != "":
			return fmt.Errorf("instructions %s: first group %s is the root and cannot have a parent", in.Flow, g.ID)
		case i > 0 && !seen[g.Parent]:
			return fmt.Errorf("instructions %s: group %s has parent %q, which must be a group listed before it", in.Flow, g.ID, g.Parent)
		}
		seen[g.ID] = true
	}

	cols := map[string]bool{}
	for _, c := range in.fieldNames() {
		switch {
		case c == "":
			return fmt.Errorf("instructions %s: empty column name", in.Flow)
		case reservedColumns[c]:
			return fmt.Errorf("instructions %s: column name %q is reserved", in.Flow, c)
		case cols[c]:
			return fmt.Errorf("instructions %s: duplicate column name %q", in.Flow, c)
		}
		cols[c] = true
	}
	for c, t := range in.Types {
		if !cols[c] {
			return fmt.Errorf("instructions %s: types lists %q, which is not a column (renamed?)", in.Flow, c)
		}
		if !knownType(t) {
			return fmt.Errorf("instructions %s: column %q has unknown type %q (want %s, %s or %s)", in.Flow, c, t, TypeDate, TypeDateTime, TypeDecimal)
		}
	}
	tables := map[string]bool{}
	for _, t := range in.EmptyTables(LayoutTables) {
		if tables[t.Name] {
			return fmt.Errorf("instructions %s: two groups give the same table name %q", in.Flow, t.Name)
		}
		tables[t.Name] = true
	}
	return nil
}

// fieldNames returns the header, group and footer field names, in order.
func (in *Instructions) fieldNames() []string {
	names := append([]string(nil), in.Header.Fields...)
	for _, g := range in.Groups {
		names = append(names, g.Fields...)
	}
	return append(names, in.Footer.Fields...)
}

// Columns returns the column names of the table produced by Convert, in
// order: source_file, row_index, header fields, each group's fields, footer
// fields.
func (in *Instructions) Columns() []string {
	return append([]string{ColumnSourceFile, ColumnRowIndex}, in.fieldNames()...)
}
