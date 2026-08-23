//go:build unix

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Case is one case.json manifest: a single invocation to run against both the
// Oracle and the Target, plus an optional fixture/ tree materialised into the
// run cwd before execution. Normalisation, diffing, and waiver gating consume
// ExpectedTaxonomy and Waiver in a later ticket; this runner only carries them
// through unmodified.
type Case struct {
	ID                string            `json:"id"`
	Argv              []string          `json:"argv"`
	Stdin             string            `json:"stdin"`
	Env               map[string]string `json:"env"`
	RewriteBinaryName bool              `json:"rewrite_binary_name"`
	ExpectedTaxonomy  []string          `json:"expected_taxonomy"`
	Waiver            string            `json:"waiver"`

	// Dir is the absolute path to the case directory (not part of case.json).
	Dir string `json:"-"`
}

// FixtureDir returns the case's optional fixture/ tree, or "" if none exists.
func (c Case) FixtureDir() string {
	dir := filepath.Join(c.Dir, "fixture")
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		return dir
	}
	return ""
}

// LoadCases globs every immediate subdirectory of casesDir containing a
// case.json and parses it. Cases are returned sorted by directory name so a
// run is deterministic and reproducible across invocations.
func LoadCases(casesDir string) ([]Case, error) {
	entries, err := os.ReadDir(casesDir)
	if err != nil {
		return nil, fmt.Errorf("reading cases dir %s: %w", casesDir, err)
	}

	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	var cases []Case
	seenIDs := make(map[string]string, len(names))
	for _, name := range names {
		dir := filepath.Join(casesDir, name)
		manifestPath := filepath.Join(dir, "case.json")
		data, err := os.ReadFile(manifestPath)
		if os.IsNotExist(err) {
			continue // not a case directory
		}
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", manifestPath, err)
		}

		var c Case
		if err := json.Unmarshal(data, &c); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", manifestPath, err)
		}
		if c.ID == "" {
			return nil, fmt.Errorf("%s: case.json missing required \"id\"", manifestPath)
		}
		// Evidence is keyed purely by ID (<out>/<side>/<id>/...), so a
		// duplicate would silently overwrite an earlier case's evidence
		// instead of erroring.
		if prevDir, ok := seenIDs[c.ID]; ok {
			return nil, fmt.Errorf("%s: duplicate case id %q also used by %s", manifestPath, c.ID, prevDir)
		}
		seenIDs[c.ID] = dir
		c.Dir = dir
		cases = append(cases, c)
	}
	return cases, nil
}
