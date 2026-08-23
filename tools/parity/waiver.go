//go:build unix

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Waiver is one entry in waivers.json: an explicit, narrow exemption for a
// known case/field difference against the pinned Oracle baseline. "id"
// matches a Case.ID directly -- there is no separate waiver-key indirection
// through Case.Waiver, which ticket 01 carries through case.json unmodified
// but this ticket does not otherwise use.
type Waiver struct {
	ID     string   `json:"id"`
	Fields []string `json:"fields"`
	// TreePaths is REQUIRED whenever Fields contains "tree": the exact
	// evidence paths (e.g. "home/.apm/config.json") this waiver covers. A
	// tree diff is waived only when every differing path is listed -- scope
	// must be machine-checked, never carried in Reason prose alone
	// (eval-ticket-02-r3.md, ticket-review §E3).
	TreePaths    []string `json:"tree_paths,omitempty"`
	Taxonomy     string   `json:"taxonomy"`
	OracleCommit string   `json:"oracle_commit"`
	Reason       string   `json:"reason"`
	Owner        string   `json:"owner"`
	EvalPlanRef  string   `json:"eval_plan_ref"`
}

// waiverValidationError marks a waivers.json validation failure -- unknown
// case id, empty reason, empty/wildcard fields, an oracle_commit that
// doesn't match this run's pinned baseline, or a reserved-taxonomy misuse.
// Like *preflightError, this fails the run closed with exit 2, before any
// real case executes.
type waiverValidationError struct{ err error }

func (e *waiverValidationError) Error() string { return e.err.Error() }
func (e *waiverValidationError) Unwrap() error { return e.err }
func (e *waiverValidationError) ExitCode() int { return 2 }

// loadWaivers reads path as a JSON array of Waiver. A missing file means no
// waivers apply -- most ad hoc case directories (including this package's
// own tests) have none, and that is not itself an error.
func loadWaivers(path string) ([]Waiver, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	var waivers []Waiver
	if err := json.Unmarshal(data, &waivers); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return waivers, nil
}

// negativeControlTaxonomy is the taxonomy tag reserved for a runner case
// that exists purely to prove the diff pipeline detects a real difference
// (a negative control), never to hide an unexplained product gap.
const negativeControlTaxonomy = "negative-control"

// runnerOwnedNegativeControlIDs is the fixed, runner-owned allow-list of
// case ids a waiver may tag negative-control against. Eligibility is
// intentionally NOT read from a case's own manifest (case.json's
// expected_taxonomy): a case.json lives under tools/parity/cases and is
// otherwise just product-authored input to the runner, so letting a case
// self-declare "I am a negative control" would let a product case
// self-authorize the one taxonomy that is supposed to be unforgeable
// (ticket 02 attempt 3: eval-ticket-02-r2.md's "product-negative"
// reproducer -- a product case that adds
// expected_taxonomy: ["negative-control"] to its own manifest must still be
// rejected at validation). "version" is the only case that currently earns
// this: its whole purpose is proving --version diffs and the pipeline
// catches it.
var runnerOwnedNegativeControlIDs = map[string]bool{
	"version": true,
}

// validateWaivers fails closed on any waiver that isn't a precise,
// accountable exemption: an id that doesn't match a loaded case, fields
// that are empty or a wildcard, an empty reason, an oracle_commit that
// doesn't match oracleCommit, or the reserved negative-control taxonomy
// applied to an id outside runnerOwnedNegativeControlIDs. casesByID is
// every case LoadCases loaded for this run, keyed by ID -- what makes a
// waiver's id "known".
func validateWaivers(waivers []Waiver, casesByID map[string]Case, oracleCommit string) error {
	for _, w := range waivers {
		if w.ID == "" {
			return &waiverValidationError{fmt.Errorf("waivers.json: entry with empty id")}
		}
		if _, ok := casesByID[w.ID]; !ok {
			return &waiverValidationError{fmt.Errorf("waivers.json: case %q: unknown case id", w.ID)}
		}
		if len(w.Fields) == 0 {
			return &waiverValidationError{fmt.Errorf("waivers.json: case %q: fields must be explicit and non-empty", w.ID)}
		}
		for _, f := range w.Fields {
			if f == "" || f == "*" {
				return &waiverValidationError{fmt.Errorf("waivers.json: case %q: field %q is not an explicit field name", w.ID, f)}
			}
		}
		if hasField(w.Fields, "tree") {
			if len(w.TreePaths) == 0 {
				return &waiverValidationError{fmt.Errorf("waivers.json: case %q: a tree waiver must list tree_paths (the exact evidence paths it covers)", w.ID)}
			}
			for _, p := range w.TreePaths {
				if p == "" || p == "*" || strings.ContainsAny(p, "*?[") {
					return &waiverValidationError{fmt.Errorf("waivers.json: case %q: tree_paths entry %q is not an exact path", w.ID, p)}
				}
			}
		} else if len(w.TreePaths) > 0 {
			return &waiverValidationError{fmt.Errorf("waivers.json: case %q: tree_paths given but \"tree\" is not in fields", w.ID)}
		}
		if w.Reason == "" {
			return &waiverValidationError{fmt.Errorf("waivers.json: case %q: reason is empty", w.ID)}
		}
		if w.OracleCommit != oracleCommit {
			return &waiverValidationError{fmt.Errorf(
				"waivers.json: case %q: oracle_commit %q does not match this run's pinned baseline %q",
				w.ID, w.OracleCommit, oracleCommit)}
		}
		if w.Taxonomy == negativeControlTaxonomy && !runnerOwnedNegativeControlIDs[w.ID] {
			return &waiverValidationError{fmt.Errorf(
				"waivers.json: case %q: taxonomy %q is reserved for the runner-owned negative-control case ids %v; a case.json cannot self-declare eligibility",
				w.ID, negativeControlTaxonomy, runnerOwnedNegativeControlIDs)}
		}
	}
	return nil
}

// loadAndValidateWaivers reads waivers.json and validates it against
// casesByID and oracleCommit BEFORE any real case executes (ticket 02
// attempt 2, W2): oracleCommit is the in-memory preflight.OracleCommit the
// caller already computed, not a value read back from run.json -- run.json
// is not written until after this validation passes (runCases calls this
// before captureRun), so there is nothing on disk yet to read it from.
// Waivers live at waiversPath if given, otherwise default to a
// "waivers.json" sibling of casesDir (tools/parity/cases -> tools/parity/waivers.json).
func loadAndValidateWaivers(casesDir, waiversPath, oracleCommit string, casesByID map[string]Case) ([]Waiver, error) {
	path := waiversPath
	if path == "" {
		path = filepath.Join(filepath.Dir(casesDir), "waivers.json")
	}

	waivers, err := loadWaivers(path)
	if err != nil {
		return nil, err
	}
	if err := validateWaivers(waivers, casesByID, oracleCommit); err != nil {
		return nil, err
	}
	return waivers, nil
}
