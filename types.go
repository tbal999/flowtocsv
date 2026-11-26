package flowtocsv

import (
	"fmt"
	"regexp"
	"strconv"
	"time"
)

// Column types. Columns without an entry in Instructions.Types are text.
const (
	TypeDate     = "date"     // YYYYMMDD in the flow, written as YYYY-MM-DD
	TypeDateTime = "datetime" // YYYYMMDDHHMMSS, written as YYYY-MM-DD HH:MM:SS
	TypeDecimal  = "decimal"  // a number, written unchanged
)

const (
	flowDate     = "20060102"
	flowDateTime = "20060102150405"
	isoDate      = "2006-01-02"
	isoDateTime  = "2006-01-02 15:04:05"
)

var (
	digits8      = regexp.MustCompile(`^\d{8}$`)
	digits14     = regexp.MustCompile(`^\d{14}$`)
	decimalPoint = regexp.MustCompile(`^-?\d+\.\d+$`)
)

func knownType(t string) bool {
	return t == TypeDate || t == TypeDateTime || t == TypeDecimal
}

// normalize checks v against typ and converts it to its output form. Empty
// values stay empty (NULL when bulk loaded).
func normalize(typ, v string) (string, error) {
	if v == "" {
		return v, nil
	}
	switch typ {
	case TypeDate:
		t, err := time.Parse(flowDate, v)
		if err != nil || !digits8.MatchString(v) {
			return "", fmt.Errorf("%q is not a YYYYMMDD date", v)
		}
		return t.Format(isoDate), nil
	case TypeDateTime:
		t, err := time.Parse(flowDateTime, v)
		if err != nil || !digits14.MatchString(v) {
			return "", fmt.Errorf("%q is not a YYYYMMDDHHMMSS timestamp", v)
		}
		return t.Format(isoDateTime), nil
	case TypeDecimal:
		if _, err := strconv.ParseFloat(v, 64); err != nil {
			return "", fmt.Errorf("%q is not a number", v)
		}
	}
	return v, nil
}

// inferType returns the type every non-empty value fits, or "" for text.
// Decimals need a decimal point, so integer-looking codes stay text.
func inferType(values []string) string {
	fits := func(ok func(string) bool) bool {
		seen := false
		for _, v := range values {
			if v == "" {
				continue
			}
			if !ok(v) {
				return false
			}
			seen = true
		}
		return seen
	}
	parses := func(typ string) func(string) bool {
		return func(v string) bool { _, err := normalize(typ, v); return err == nil }
	}
	switch {
	case fits(parses(TypeDate)):
		return TypeDate
	case fits(parses(TypeDateTime)):
		return TypeDateTime
	case fits(decimalPoint.MatchString):
		return TypeDecimal
	}
	return ""
}

func inferTypes(samples []*File, hdr, ftr []string, groups map[string][]string) map[string]string {
	values := map[string][]string{}
	add := func(names, fields []string) {
		for i, v := range fields {
			values[names[i]] = append(values[names[i]], v)
		}
	}
	for _, s := range samples {
		add(hdr, s.Header.Fields)
		add(ftr, s.Footer.Fields)
		for _, r := range s.Records {
			add(groups[r.ID], r.Fields)
		}
	}
	types := map[string]string{}
	for name, vs := range values {
		if t := inferType(vs); t != "" {
			types[name] = t
		}
	}
	if len(types) == 0 {
		return nil
	}
	return types
}
