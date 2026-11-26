package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"time"
)

// state records which input files have been converted, keyed by the SHA-256
// of their content, so a re-delivered or renamed copy is also skipped.
type state struct {
	Files map[string]stateEntry `json:"files"`
}

type stateEntry struct {
	Name      string    `json:"name"`
	Flow      string    `json:"flow"`
	Converted time.Time `json:"converted"`
}

// loadState reads path; a missing file is an empty state.
func loadState(path string) (*state, error) {
	st := &state{Files: map[string]stateEntry{}}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, st); err != nil {
		return nil, fmt.Errorf("state file %s: %w", path, err)
	}
	if st.Files == nil {
		st.Files = map[string]stateEntry{}
	}
	return st, nil
}

func (s *state) add(entries map[string]stateEntry) {
	now := time.Now().UTC().Truncate(time.Second)
	for h, e := range entries {
		e.Converted = now
		s.Files[h] = e
	}
}

func (s *state) write(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(s)
}
