package flowtocsv

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
)

// Table is converted dataflow output. JSON carries everything:
// {"name", "primary_key", "foreign_keys", "columns", "rows"}. CSV carries the
// columns and rows only; the table name is the file name, and the keys
// follow the naming rules in ConvertTables.
type Table struct {
	Name        string       `json:"name,omitempty"` // e.g. "d0150" or "d0150_290"
	PrimaryKey  []string     `json:"primary_key,omitempty"`
	ForeignKeys []ForeignKey `json:"foreign_keys,omitempty"`
	Columns     []string     `json:"columns"`
	Rows        [][]string   `json:"rows"`
}

// ForeignKey says Column holds values of the References column of Table.
type ForeignKey struct {
	Column     string `json:"column"`
	Table      string `json:"table"`
	References string `json:"references"`
}

// Append adds other's rows to t. An empty t takes other's name, keys and
// columns; otherwise the columns must match.
func (t *Table) Append(other *Table) error {
	if len(t.Columns) == 0 && len(t.Rows) == 0 {
		t.Name, t.PrimaryKey, t.ForeignKeys, t.Columns = other.Name, other.PrimaryKey, other.ForeignKeys, other.Columns
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
