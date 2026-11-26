package flowtocsv

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ErrNoMatch is returned by Convert when a file is not an instance of the
// instructions' dataflow.
var ErrNoMatch = errors.New("file does not match instructions")

// Matches reports whether f is an instance of this dataflow: same delimiter
// and header ID, and one of the first three header fields contains Flow.
func (in *Instructions) Matches(f *File) bool {
	if f.Delimiter != in.Delimiter || f.Header.ID != in.Header.ID {
		return false
	}
	for _, v := range f.Header.Fields[:min(3, len(f.Header.Fields))] {
		if strings.Contains(v, in.Flow) {
			return true
		}
	}
	return false
}

// Convert flattens f into one wide Table (LayoutWide) whose columns are
// in.Columns(). source is written to every row's source_file column
// (typically the file name).
//
// Each root record starts a block. Within a block, child records of the same
// group become alternative rows, and different child groups are combined
// with each other, so a group that appears once is copied onto every row of
// its parent. Groups with no record in a block are left empty.
//
// Convert fails, rather than dropping or guessing at data, on records the
// instructions don't know about, records with more non-empty fields than
// the instructions allow, records that appear without their parent, values
// that don't fit their column type, and files without the expected footer.
func (in *Instructions) Convert(f *File, source string) (*Table, error) {
	p, err := in.prepare(f, source)
	if err != nil {
		return nil, err
	}
	t := in.EmptyTables(LayoutWide)[0]
	for _, root := range p.roots {
		for _, part := range p.expand(root) {
			row := make([]string, 0, len(t.Columns))
			row = append(row, source, strconv.Itoa(len(t.Rows)+1))
			row = append(row, p.header...)
			for gi, g := range in.Groups {
				if part[gi] != nil {
					row = append(row, part[gi]...)
				} else {
					row = append(row, make([]string, len(g.Fields))...)
				}
			}
			t.Rows = append(t.Rows, append(row, p.footer...))
		}
	}
	return t, nil
}

// ConvertTables converts f into normalised tables (LayoutTables), one row per
// record with nothing copied or multiplied:
//
//   - "<flow>_file": one row per file with the header and footer fields.
//     Primary key source_file.
//   - "<flow>_<group>" for each group, in instruction order. Primary key id
//     ("<source>:<line>"); source_file references the file table; for every
//     group but the root, parent_id references the parent group's id. line is
//     the record's line in the source.
//
// Every key is a single column, so the tables relate directly in BI tools.
// source must be unique across everything loaded together, since all keys
// derive from it. Each Table lists its keys. It fails in the same cases as
// Convert.
func (in *Instructions) ConvertTables(f *File, source string) ([]*Table, error) {
	p, err := in.prepare(f, source)
	if err != nil {
		return nil, err
	}
	tables := in.EmptyTables(LayoutTables)
	fileRow := append([]string{source}, p.header...)
	tables[0].Rows = [][]string{append(fileRow, p.footer...)}

	var walk func(n *node, parentID string)
	walk = func(n *node, parentID string) {
		id := recordID(source, n.line)
		meta := []string{id, source, strconv.Itoa(n.line)}
		if n.group > 0 {
			meta = []string{id, parentID, source, strconv.Itoa(n.line)}
		}
		t := tables[n.group+1]
		t.Rows = append(t.Rows, append(meta, n.fields...))
		for _, k := range n.kids {
			walk(k, id)
		}
	}
	for _, root := range p.roots {
		walk(root, "")
	}
	return tables, nil
}

type node struct {
	group  int
	line   int
	fields []string
	kids   []*node
}

// partial holds one row's fields per group position; nil means no record.
type partial [][]string

// prepared is a validated file: typed header and footer values plus the
// record tree.
type prepared struct {
	in             *Instructions
	children       [][]int // group position -> child group positions, in order
	header, footer []string
	roots          []*node
}

func (in *Instructions) prepare(f *File, source string) (*prepared, error) {
	if !in.Matches(f) {
		return nil, fmt.Errorf("%s: %w %s", source, ErrNoMatch, in.Flow)
	}
	if f.Footer.ID != in.Footer.ID {
		return nil, fmt.Errorf("line %d: last line is %q, want footer %q (truncated file?)", f.Footer.Line, f.Footer.ID, in.Footer.ID)
	}
	p := &prepared{in: in, children: make([][]int, len(in.Groups))}
	index := map[string]int{}
	for i, g := range in.Groups {
		index[g.ID] = i
		if i > 0 {
			parent := index[g.Parent]
			p.children[parent] = append(p.children[parent], i)
		}
	}

	var err error
	if p.header, err = in.values(f.Header, in.Header.Fields); err != nil {
		return nil, err
	}
	if p.footer, err = in.values(f.Footer, in.Footer.Fields); err != nil {
		return nil, err
	}

	var stack []*node
	for _, r := range f.Records {
		gi, ok := index[r.ID]
		if !ok {
			return nil, fmt.Errorf("line %d: group %q is not in the %s instructions (re-learn with a sample that contains it)", r.Line, r.ID, in.Flow)
		}
		fields, err := in.values(r, in.Groups[gi].Fields)
		if err != nil {
			return nil, err
		}
		n := &node{group: gi, line: r.Line, fields: fields}
		if gi == 0 {
			p.roots = append(p.roots, n)
			stack = append(stack[:0], n)
			continue
		}
		parent := in.Groups[gi].Parent
		for len(stack) > 0 && in.Groups[stack[len(stack)-1].group].ID != parent {
			stack = stack[:len(stack)-1]
		}
		if len(stack) == 0 {
			return nil, fmt.Errorf("line %d: group %s appears outside a %s group", r.Line, r.ID, parent)
		}
		top := stack[len(stack)-1]
		top.kids = append(top.kids, n)
		stack = append(stack, n)
	}
	return p, nil
}

// values fits r's fields to names (padding, or trimming trailing empty
// fields) and converts typed columns to their output form.
func (in *Instructions) values(r Record, names []string) ([]string, error) {
	width := len(names)
	if len(r.Fields) > width {
		for _, v := range r.Fields[width:] {
			if v != "" {
				return nil, fmt.Errorf("line %d: %s has %d fields, instructions allow %d", r.Line, r.ID, len(r.Fields), width)
			}
		}
	}
	out := make([]string, width)
	copy(out, r.Fields)
	for i, name := range names {
		typ, ok := in.Types[name]
		if !ok {
			continue
		}
		v, err := normalize(typ, out[i])
		if err != nil {
			return nil, fmt.Errorf("line %d: %s column %s: %v (change its type in the instructions if this is valid)", r.Line, r.ID, name, err)
		}
		out[i] = v
	}
	return out, nil
}

// expand returns the partial rows for n and its descendants: n's fields
// combined with every choice of one row from each child group.
func (p *prepared) expand(n *node) []partial {
	own := make(partial, len(p.in.Groups))
	own[n.group] = n.fields
	result := []partial{own}
	for _, cg := range p.children[n.group] {
		var options []partial
		for _, k := range n.kids {
			if k.group == cg {
				options = append(options, p.expand(k)...)
			}
		}
		if len(options) == 0 {
			continue
		}
		next := make([]partial, 0, len(result)*len(options))
		for _, a := range result {
			for _, b := range options {
				merged := append(partial(nil), a...)
				for gi, f := range b {
					if f != nil {
						merged[gi] = f
					}
				}
				next = append(next, merged)
			}
		}
		result = next
	}
	return result
}
