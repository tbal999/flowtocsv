package flowtocsv

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
)

// Table is flattened dataflow output. It marshals to JSON as
// {"columns": [...], "rows": [[...], ...]} and to CSV as a header line
// followed by the rows; both forms read back into an identical Table.
type Table struct {
	Columns []string   `json:"columns"`
	Rows    [][]string `json:"rows"`
}

// Append adds other's rows to t. Both tables must have the same columns.
func (t *Table) Append(other *Table) error {
	if len(t.Columns) == 0 && len(t.Rows) == 0 {
		t.Columns = other.Columns
	} else if !equal(t.Columns, other.Columns) {
		return errors.New("append: tables have different columns")
	}
	t.Rows = append(t.Rows, other.Rows...)
	return nil
}

// WriteCSV writes the column names then every row.
func (t *Table) WriteCSV(w io.Writer) error {
	cw := csv.NewWriter(w)
	if err := cw.Write(t.Columns); err != nil {
		return err
	}
	if err := cw.WriteAll(t.Rows); err != nil {
		return err
	}
	return cw.Error()
}

// ReadCSV reads a table written by WriteCSV. Every row must be as wide as
// the header.
func ReadCSV(r io.Reader) (*Table, error) {
	recs, err := csv.NewReader(r).ReadAll()
	if err != nil {
		return nil, fmt.Errorf("read csv: %w", err)
	}
	if len(recs) == 0 {
		return nil, errors.New("read csv: no header row")
	}
	return &Table{Columns: recs[0], Rows: recs[1:]}, nil
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
