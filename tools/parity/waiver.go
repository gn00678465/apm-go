//go:build unix

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Waiver is one entry in waivers.json: an explicit, narrow exemption for a
// known case/field difference against the pinned Oracle baseline. "id"
// matches a Case.ID directly -- there is no separate waiver-key indirection
// through Case.Waiver, which ticket 01 carries through case.json unmodified
// but this ticket does not otherwise use.
type Waiver struct {
	ID           string   `json:"id"`
	Fields       []string `json:"fields"`
	Taxonomy     string   `json:"taxonomy"`
	OracleCommit string   `json:"oracle_commit"`
	Reason       string   `json:"reason"`
	Owner        string   `json:"owner"`
	EvalPlanRef  string   `json:"eval_plan_ref"`
}

// waiverValidationError marks a waivers.json validation failure -- unknown
// case id, empty reason, empty/wildcard fields, or an oracle_commit that
// doesn't match this run's pinned baseline. Like *preflightError, this
// fails the run closed with exit 2, before any real case executes.
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

// validateWaivers fails closed on any waiver that isn't a precise,
// accountable exemption: an id that doesn't match a loaded case, fields
// that are empty or a wildcard, an empty reason, or an oracle_commit that
// doesn't match oracleCommit (the value this run actually recorded in
// run.json's preflight.oracle_commit -- see loadAndValidateWaivers).
func validateWaivers(waivers []Waiver, knownIDs map[string]bool, oracleCommit string) error {
	for _, w := range waivers {
		if w.ID == "" {
			return &waiverValidationError{fmt.Errorf("waivers.json: entry with empty id")}
		}
		if !knownIDs[w.ID] {
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
		if w.Reason == "" {
			return &waiverValidationError{fmt.Errorf("waivers.json: case %q: reason is empty", w.ID)}
		}
		if w.OracleCommit != oracleCommit {
			return &waiverValidationError{fmt.Errorf(
				"waivers.json: case %q: oracle_commit %q does not match this run's pinned baseline %q",
				w.ID, w.OracleCommit, oracleCommit)}
		}
	}
	return nil
}

// loadAndValidateWaivers reads outDir/run.json back from disk -- not the
// in-memory Preflight the caller already computed -- so validation always
// checks against the oracle_commit this run actually wrote as evidence,
// not merely what was in memory a moment earlier. Waivers live at
// waiversPath if given, otherwise default to a "waivers.json" sibling of
// casesDir (tools/parity/cases -> tools/parity/waivers.json).
func loadAndValidateWaivers(outDir, casesDir, waiversPath string, knownIDs map[string]bool) ([]Waiver, error) {
	header, err := readRunHeader(outDir)
	if err != nil {
		return nil, fmt.Errorf("reading run.json for waiver validation: %w", err)
	}

	path := waiversPath
	if path == "" {
		path = filepath.Join(filepath.Dir(casesDir), "waivers.json")
	}

	waivers, err := loadWaivers(path)
	if err != nil {
		return nil, err
	}
	if err := validateWaivers(waivers, knownIDs, header.Preflight.OracleCommit); err != nil {
		return nil, err
	}
	return waivers, nil
}

func readRunHeader(outDir string) (runHeader, error) {
	data, err := os.ReadFile(filepath.Join(outDir, "run.json"))
	if err != nil {
		return runHeader{}, err
	}
	var h runHeader
	if err := json.Unmarshal(data, &h); err != nil {
		return runHeader{}, fmt.Errorf("unmarshalling run.json: %w", err)
	}
	return h, nil
}
