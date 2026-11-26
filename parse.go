package flowtocsv

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Record is one line of a dataflow: its group ID and the fields after it.
type Record struct {
	ID     string
	Fields []string
	Line   int // 1-based line number in the source
}

// File is a parsed dataflow.
type File struct {
	Delimiter string
	Header    Record
	Records   []Record
	Footer    Record
}

// Parse reads a dataflow. The first non-blank line is the header and the last
// is the footer. Lines may end in CRLF or LF, and one trailing delimiter per
// line is ignored ("288|a|b|" has fields a, b). Double quotes are stripped.
func Parse(r io.Reader, delimiter string) (*File, error) {
	if delimiter == "" {
		return nil, errors.New("parse: delimiter is empty")
	}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)

	var recs []Record
	for line := 1; sc.Scan(); line++ {
		text := strings.TrimSuffix(sc.Text(), "\r")
		if strings.TrimSpace(text) == "" {
			continue
		}
		recs = append(recs, splitRecord(text, delimiter, line))
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	if len(recs) < 2 {
		return nil, errors.New("parse: a dataflow needs at least a header and a footer line")
	}
	return &File{
		Delimiter: delimiter,
		Header:    recs[0],
		Records:   recs[1 : len(recs)-1],
		Footer:    recs[len(recs)-1],
	}, nil
}

func splitRecord(text, delimiter string, line int) Record {
	parts := strings.Split(strings.TrimSuffix(text, delimiter), delimiter)
	for i := range parts {
		parts[i] = strings.ReplaceAll(parts[i], `"`, "")
	}
	return Record{ID: parts[0], Fields: parts[1:], Line: line}
}
