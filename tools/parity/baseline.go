//go:build unix

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// BaselineEntry is one exact path in tools/parity/baseline.json: an
// Oracle-only global-state artefact that the Oracle writes as a side effect
// of running (e.g. under $HOME) regardless of which case is executing, that
// apm-go deliberately does not mirror. Unlike waivers.json's per-case
// tree_paths, a baseline entry applies uniformly across EVERY case: the path
// is excluded from tree-diff comparison everywhere it appears, not just for
// one named case id (ticket 12).
type BaselineEntry struct {
	Path      string `json:"path"`
	Reason    string `json:"reason"`
	OracleRef string `json:"oracle_ref"`
}

// baselineValidationError marks a baseline.json validation failure -- an
// inexact (globbed/empty) path, an empty reason, or a duplicate path entry.
// Like *waiverValidationError, this fails the run closed with exit 2, before
// any real case executes.
type baselineValidationError struct{ err error }

func (e *baselineValidationError) Error() string { return e.err.Error() }
func (e *baselineValidationError) Unwrap() error { return e.err }
func (e *baselineValidationError) ExitCode() int { return 2 }

// loadBaseline reads path as a JSON array of BaselineEntry. A missing file
// means no baseline exclusions apply -- most ad hoc case directories
// (including this package's own tests) have none, and that is not itself an
// error.
func loadBaseline(path string) ([]BaselineEntry, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	var entries []BaselineEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return entries, nil
}

// validateBaseline fails closed on any entry that isn't a precise,
// accountable exemption: an empty or globbed path, an empty reason, or a
// path listed more than once (each path gets exactly one documented
// reason).
func validateBaseline(entries []BaselineEntry) error {
	seen := make(map[string]bool, len(entries))
	for _, e := range entries {
		if e.Path == "" || e.Path == "*" || strings.ContainsAny(e.Path, "*?[") {
			return &baselineValidationError{fmt.Errorf("baseline.json: entry %q is not an exact path", e.Path)}
		}
		if e.Reason == "" {
			return &baselineValidationError{fmt.Errorf("baseline.json: path %q: reason is empty", e.Path)}
		}
		if seen[e.Path] {
			return &baselineValidationError{fmt.Errorf("baseline.json: path %q: duplicate entry", e.Path)}
		}
		seen[e.Path] = true
	}
	return nil
}

// loadAndValidateBaseline reads baseline.json and validates it BEFORE any
// real case executes, mirroring loadAndValidateWaivers. Baseline entries
// live at baselinePath if given, otherwise default to a "baseline.json"
// sibling of casesDir (tools/parity/cases -> tools/parity/baseline.json).
func loadAndValidateBaseline(casesDir, baselinePath string) ([]BaselineEntry, error) {
	path := baselinePath
	if path == "" {
		path = filepath.Join(filepath.Dir(casesDir), "baseline.json")
	}

	entries, err := loadBaseline(path)
	if err != nil {
		return nil, err
	}
	if err := validateBaseline(entries); err != nil {
		return nil, err
	}
	return entries, nil
}

// baselinePathSet indexes entries by Path for O(1) tree-diff exclusion
// lookups.
func baselinePathSet(entries []BaselineEntry) map[string]bool {
	m := make(map[string]bool, len(entries))
	for _, e := range entries {
		m[e.Path] = true
	}
	return m
}
