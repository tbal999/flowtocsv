package flowtocsv

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Learn infers Instructions for a dataflow from one or more sample files.
//
// Groups are listed in the order they first appear, and each gets as many
// fields as the widest record seen. Column names are generated (hdr_1,
// g288_1, ftr_1, ...) and are meant to be renamed in the saved JSON.
//
// Nesting is inferred conservatively. Group C is made a child of an earlier
// group A only when a sample shows A, C, A within one parent, which proves C
// records belong to individual A records. Otherwise C hangs off the root
// group, so a single C is copied onto every row of its block. Samples where
// parent groups repeat (e.g. two meters, each with registers) learn better.
func Learn(flow string, samples ...*File) (Instructions, error) {
	if flow == "" {
		return Instructions{}, errors.New("learn: flow name is empty")
	}
	if len(samples) == 0 {
		return Instructions{}, errors.New("learn: no samples")
	}

	first := samples[0]
	var (
		order      []string
		widths     = map[string]int{}
		seqs       = make([][]string, len(samples))
		hdrW, ftrW int
	)
	for i, s := range samples {
		if s.Delimiter != first.Delimiter || s.Header.ID != first.Header.ID || s.Footer.ID != first.Footer.ID {
			return Instructions{}, fmt.Errorf("learn %s: sample %d has a different delimiter, header or footer from sample 1", flow, i+1)
		}
		hdrW = max(hdrW, len(s.Header.Fields))
		ftrW = max(ftrW, len(s.Footer.Fields))
		for _, r := range s.Records {
			if _, ok := widths[r.ID]; !ok {
				order = append(order, r.ID)
			}
			widths[r.ID] = max(widths[r.ID], len(r.Fields))
			seqs[i] = append(seqs[i], r.ID)
		}
	}
	if len(order) == 0 {
		return Instructions{}, fmt.Errorf("learn %s: samples have no records between header and footer", flow)
	}

	parents := inferParents(order, seqs)
	in := Instructions{
		Flow:      flow,
		Delimiter: first.Delimiter,
		Header:    Segment{ID: first.Header.ID, Fields: fieldNames("hdr", hdrW)},
		Footer:    Segment{ID: first.Footer.ID, Fields: fieldNames("ftr", ftrW)},
	}
	for _, id := range order {
		in.Groups = append(in.Groups, Group{
			ID:     id,
			Parent: parents[id],
			Fields: fieldNames("g"+sanitize(id), widths[id]),
		})
	}
	if err := in.Validate(); err != nil {
		return Instructions{}, fmt.Errorf("learn: %w", err)
	}
	return in, nil
}

// inferParents assigns each group (after the root, order[0]) a parent. The
// candidates are the previous group and its ancestors, deepest first; the
// first with interleaving evidence wins, falling back to the root.
func inferParents(order []string, seqs [][]string) map[string]string {
	parent := map[string]string{}
	ancestors := func(g string) []string { // g, parent(g), ..., root
		var out []string
		for ; g != ""; g = parent[g] {
			out = append(out, g)
		}
		return out
	}
	root := order[0]
	for k := 1; k < len(order); k++ {
		g := order[k]
		parent[g] = root
		for _, t := range ancestors(order[k-1]) {
			if t == root {
				break
			}
			if interleaved(seqs, t, g, ancestors(parent[t])) {
				parent[g] = t
				break
			}
		}
	}
	return parent
}

// interleaved reports whether some sequence contains t, g, t with no group
// from bounds (t's ancestors) in between.
func interleaved(seqs [][]string, t, g string, bounds []string) bool {
	isBound := map[string]bool{}
	for _, b := range bounds {
		isBound[b] = true
	}
	for _, seq := range seqs {
		const none, sawT, sawTG = 0, 1, 2
		state := none
		for _, id := range seq {
			switch {
			case isBound[id]:
				state = none
			case id == t:
				if state == sawTG {
					return true
				}
				state = sawT
			case id == g && state == sawT:
				state = sawTG
			}
		}
	}
	return false
}

func fieldNames(prefix string, n int) []string {
	names := make([]string, n)
	for i := range names {
		names[i] = prefix + "_" + strconv.Itoa(i+1)
	}
	return names
}

// sanitize lowercases id and replaces anything but [a-z0-9] with '_', so
// generated column names are valid unquoted SQL identifiers.
func sanitize(id string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			return r
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		}
		return '_'
	}, id)
}
