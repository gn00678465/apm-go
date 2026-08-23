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

	// SetupArgv is zero or more argv lists run, in order, against the same
	// binary/sandbox/env BEFORE Argv, to seed state Argv's own case depends
	// on (e.g. `["marketplace", "add", "./fixture", "--name", "skills"]` to
	// register a marketplace before a `search` case queries it). Each
	// setup step must exit 0 -- a nonzero exit fails the case as a runner
	// error (the fixture never got seeded, so the real case's evidence
	// would be meaningless). Setup runs are not captured as evidence and
	// are excluded from the pre-run tree snapshot, so they never appear as
	// a spurious "added" tree diff against Argv's own run.
	SetupArgv [][]string `json:"setup_argv"`

	// PathPrepend is a case-relative directory (e.g. "path") whose absolute
	// path is prepended to PATH, ahead of everything else, for both sides
	// (ticket 08's fault-injection mechanism: a fixture `git` shell script
	// placed there shadows the real one so a case can script git's
	// behaviour -- ok/missing/nonzero/hang/etc -- deterministically).
	// Applied on top of buildEnv's own PATH (env.go's allow-listed
	// inherited value); empty means PATH is left as the runner's own value.
	PathPrepend string `json:"path_prepend"`

	// Dir is the absolute path to the case directory (not part of case.json).
	// LoadCases guarantees this via filepath.Abs -- a relative -cases flag
	// (the normal CLI shape) must not leave Dir relative, because
	// runCaseSide joins it into PATH (PathPrepend) while the subprocess's
	// cwd is its own sandbox, not this process's cwd: a relative Dir would
	// resolve against the wrong directory and PathPrepend's fixture binary
	// would never shadow the real one (ticket 10 attempt 3, eval-ticket-10-r2.md
	// §4).
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
		absDir, err := filepath.Abs(dir)
		if err != nil {
			return nil, fmt.Errorf("resolving absolute path for %s: %w", dir, err)
		}
		dir = absDir
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
