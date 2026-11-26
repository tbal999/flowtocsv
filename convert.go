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

// Convert flattens f into a Table whose columns are in.Columns(). source is
// written to every row's source_file column (typically the file name).
//
// Each root record starts a block. Within a block, child records of the same
// group become alternative rows, and different child groups are combined
// with each other, so a group that appears once is copied onto every row of
// its parent. Groups with no record in a block are left empty.
//
// Convert fails on records the instructions don't know about, on records with
// more non-empty fields than the instructions allow, and on records that
// appear without their parent, so data is never silently dropped.
func (in *Instructions) Convert(f *File, source string) (*Table, error) {
	if !in.Matches(f) {
		return nil, fmt.Errorf("%s: %w %s", source, ErrNoMatch, in.Flow)
	}
	c := newConverter(in)
	header, err := fit(f.Header, len(in.Header.Fields))
	if err != nil {
		return nil, err
	}
	footer, err := fit(f.Footer, len(in.Footer.Fields))
	if err != nil {
		return nil, err
	}
	if f.Footer.ID != in.Footer.ID {
		return nil, fmt.Errorf("line %d: last line is %q, want footer %q (truncated file?)", f.Footer.Line, f.Footer.ID, in.Footer.ID)
	}
	roots, err := c.buildTree(f.Records)
	if err != nil {
		return nil, err
	}

	t := &Table{Columns: in.Columns()}
	for _, root := range roots {
		for _, part := range c.expand(root) {
			row := make([]string, 0, len(t.Columns))
			row = append(row, source, strconv.Itoa(len(t.Rows)+1))
			row = append(row, header...)
			for gi, g := range in.Groups {
				if part[gi] != nil {
					row = append(row, part[gi]...)
				} else {
					row = append(row, make([]string, len(g.Fields))...)
				}
			}
			t.Rows = append(t.Rows, append(row, footer...))
		}
	}
	return t, nil
}

type converter struct {
	in       *Instructions
	index    map[string]int // group ID -> position in in.Groups
	children [][]int        // group position -> child group positions, in order
}

type node struct {
	group  int
	fields []string
	kids   []*node
}

// partial holds one row's fields per group position; nil means no record.
type partial [][]string

func newConverter(in *Instructions) *converter {
	c := &converter{in: in, index: map[string]int{}, children: make([][]int, len(in.Groups))}
	for i, g := range in.Groups {
		c.index[g.ID] = i
		if i > 0 {
			p := c.index[g.Parent]
			c.children[p] = append(c.children[p], i)
		}
	}
	return c
}

func (c *converter) buildTree(recs []Record) ([]*node, error) {
	var roots, stack []*node
	for _, r := range recs {
		gi, ok := c.index[r.ID]
		if !ok {
			return nil, fmt.Errorf("line %d: group %q is not in the %s instructions (re-learn with a sample that contains it)", r.Line, r.ID, c.in.Flow)
		}
		fields, err := fit(r, len(c.in.Groups[gi].Fields))
		if err != nil {
			return nil, err
		}
		n := &node{group: gi, fields: fields}
		if gi == 0 {
			roots = append(roots, n)
			stack = append(stack[:0], n)
			continue
		}
		parent := c.in.Groups[gi].Parent
		for len(stack) > 0 && c.in.Groups[stack[len(stack)-1].group].ID != parent {
			stack = stack[:len(stack)-1]
		}
		if len(stack) == 0 {
			return nil, fmt.Errorf("line %d: group %s appears outside a %s group", r.Line, r.ID, parent)
		}
		top := stack[len(stack)-1]
		top.kids = append(top.kids, n)
		stack = append(stack, n)
	}
	return roots, nil
}

// expand returns the partial rows for n and its descendants: n's fields
// combined with every choice of one row from each child group.
func (c *converter) expand(n *node) []partial {
	own := make(partial, len(c.in.Groups))
	own[n.group] = n.fields
	result := []partial{own}
	for _, cg := range c.children[n.group] {
		var options []partial
		for _, k := range n.kids {
			if k.group == cg {
				options = append(options, c.expand(k)...)
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

// fit pads r's fields to width, or trims trailing empty fields beyond it.
func fit(r Record, width int) ([]string, error) {
	if len(r.Fields) > width {
		for _, v := range r.Fields[width:] {
			if v != "" {
				return nil, fmt.Errorf("line %d: %s has %d fields, instructions allow %d", r.Line, r.ID, len(r.Fields), width)
			}
		}
		return r.Fields[:width], nil
	}
	out := make([]string, width)
	copy(out, r.Fields)
	return out, nil
}
