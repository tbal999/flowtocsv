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

// Metadata columns that start every row produced by Convert.
const (
	ColumnSourceFile = "source_file" // the source name passed to Convert
	ColumnRowIndex   = "row_index"   // 1-based row number within that source
)

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
	for _, c := range in.Columns() {
		if c == "" {
			return fmt.Errorf("instructions %s: empty column name", in.Flow)
		}
		if cols[c] {
			return fmt.Errorf("instructions %s: duplicate column name %q", in.Flow, c)
		}
		cols[c] = true
	}
	return nil
}

// Columns returns the column names of tables produced by Convert, in order:
// the metadata columns, header fields, each group's fields, footer fields.
func (in *Instructions) Columns() []string {
	cols := []string{ColumnSourceFile, ColumnRowIndex}
	cols = append(cols, in.Header.Fields...)
	for _, g := range in.Groups {
		cols = append(cols, g.Fields...)
	}
	return append(cols, in.Footer.Fields...)
}
