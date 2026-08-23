//go:build unix

package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadWaivers_MissingFileReturnsEmptyNotError(t *testing.T) {
	waivers, err := loadWaivers(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if err != nil {
		t.Fatalf("loadWaivers: %v", err)
	}
	if len(waivers) != 0 {
		t.Errorf("waivers = %v, want none", waivers)
	}
}

func TestLoadWaivers_ParsesEntries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "waivers.json")
	writeJSON(t, path, `[{"id":"version","fields":["stdout"],"taxonomy":"negative-control","oracle_commit":"abc","reason":"r","owner":"o","eval_plan_ref":"ref"}]`)

	waivers, err := loadWaivers(path)
	if err != nil {
		t.Fatalf("loadWaivers: %v", err)
	}
	if len(waivers) != 1 {
		t.Fatalf("waivers = %v, want 1 entry", waivers)
	}
	w := waivers[0]
	if w.ID != "version" || w.OracleCommit != "abc" || w.Reason != "r" || w.Owner != "o" || w.EvalPlanRef != "ref" {
		t.Errorf("waiver = %+v", w)
	}
	if !fieldsEqual(w.Fields, []string{"stdout"}) {
		t.Errorf("Fields = %v, want [stdout]", w.Fields)
	}
}

func writeJSON(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

// versionCase is the one real case whose manifest declares
// expected_taxonomy: ["negative-control"] -- the only case a waiver may use
// that reserved taxonomy against (see TestValidateWaivers_NegativeControl*).
var versionCase = Case{ID: "version", ExpectedTaxonomy: []string{"negative-control"}}

func casesByID(cases ...Case) map[string]Case {
	m := make(map[string]Case, len(cases))
	for _, c := range cases {
		m[c.ID] = c
	}
	return m
}

func TestValidateWaivers_UnknownIDFailsExit2(t *testing.T) {
	err := validateWaivers([]Waiver{{ID: "ghost", Fields: []string{"stdout"}, Reason: "r", OracleCommit: "pin"}},
		casesByID(versionCase), "pin")
	assertWaiverValidationError(t, err)
}

func TestValidateWaivers_EmptyReasonFailsExit2(t *testing.T) {
	err := validateWaivers([]Waiver{{ID: "version", Fields: []string{"stdout"}, Reason: "", OracleCommit: "pin"}},
		casesByID(versionCase), "pin")
	assertWaiverValidationError(t, err)
}

func TestValidateWaivers_EmptyFieldsFailsExit2(t *testing.T) {
	err := validateWaivers([]Waiver{{ID: "version", Fields: nil, Reason: "r", OracleCommit: "pin"}},
		casesByID(versionCase), "pin")
	assertWaiverValidationError(t, err)
}

func TestValidateWaivers_WildcardFieldFailsExit2(t *testing.T) {
	err := validateWaivers([]Waiver{{ID: "version", Fields: []string{"*"}, Reason: "r", OracleCommit: "pin"}},
		casesByID(versionCase), "pin")
	assertWaiverValidationError(t, err)
}

func TestValidateWaivers_OracleCommitMismatchFailsExit2(t *testing.T) {
	err := validateWaivers([]Waiver{{ID: "version", Fields: []string{"stdout"}, Reason: "r", OracleCommit: "wrong-pin"}},
		casesByID(versionCase), "pin")
	assertWaiverValidationError(t, err)
}

func TestValidateWaivers_ValidPasses(t *testing.T) {
	err := validateWaivers([]Waiver{{ID: "version", Fields: []string{"stdout"}, Reason: "r", OracleCommit: "pin"}},
		casesByID(versionCase), "pin")
	if err != nil {
		t.Errorf("validateWaivers: %v, want nil", err)
	}
}

// TestValidateWaivers_NegativeControlOnProductCaseFailsExit2 proves ticket
// 02 attempt 2's W3 fix: a case that isn't on runnerOwnedNegativeControlIDs
// can never be waived with the reserved negative-control taxonomy, however
// well-formed the rest of the waiver is (eval-ticket-02.md's W3 finding: a
// "product" case waiver using negative-control was previously accepted and
// exited 0).
func TestValidateWaivers_NegativeControlOnProductCaseFailsExit2(t *testing.T) {
	productCase := Case{ID: "doctor-help"} // no expected_taxonomy at all
	err := validateWaivers([]Waiver{{
		ID: "doctor-help", Fields: []string{"stdout", "exit_code"}, Taxonomy: "negative-control",
		Reason: "product negative-control probe", OracleCommit: "pin",
	}}, casesByID(productCase), "pin")
	assertWaiverValidationError(t, err)
}

// TestValidateWaivers_NegativeControlAllowedForRunnerOwnedID proves
// eligibility is runner-owned (runnerOwnedNegativeControlIDs), not read
// from the case's own manifest: "version" has no expected_taxonomy
// declared here at all, yet is still allowed, because runner-owned
// eligibility doesn't depend on the manifest field in the first place
// (ticket 02 attempt 3).
func TestValidateWaivers_NegativeControlAllowedForRunnerOwnedID(t *testing.T) {
	plainVersionCase := Case{ID: "version"} // deliberately no expected_taxonomy
	err := validateWaivers([]Waiver{{
		ID: "version", Fields: []string{"stdout"}, Taxonomy: "negative-control",
		Reason: "proves the diff pipeline detects a real difference", OracleCommit: "pin",
	}}, casesByID(plainVersionCase), "pin")
	if err != nil {
		t.Errorf("validateWaivers: %v, want nil (\"version\" is runner-owned regardless of its manifest)", err)
	}
}

// TestValidateWaivers_ProductCaseCannotSelfDeclareNegativeControl is the
// evaluator's "product-negative" reproducer (eval-ticket-02-r2.md, ticket
// 02 attempt 3, item 3): a product case whose OWN manifest declares
// expected_taxonomy: ["negative-control"] must still be rejected when
// waived with that taxonomy, because eligibility comes from the runner's
// own id allow-list, not from anything a case.json can assert about
// itself. Accepting this would let any product case self-authorize hiding
// a real gap under the one taxonomy that's supposed to be unforgeable.
func TestValidateWaivers_ProductCaseCannotSelfDeclareNegativeControl(t *testing.T) {
	selfDeclared := Case{ID: "product-negative", ExpectedTaxonomy: []string{"negative-control"}}
	err := validateWaivers([]Waiver{{
		ID: "product-negative", Fields: []string{"stdout"}, Taxonomy: "negative-control",
		Reason: "self-declared negative control", OracleCommit: "pin",
	}}, casesByID(selfDeclared), "pin")
	assertWaiverValidationError(t, err)
}

func assertWaiverValidationError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("validateWaivers: expected an error, got nil")
	}
	var we *waiverValidationError
	if !errors.As(err, &we) {
		t.Fatalf("error %v is not a *waiverValidationError, so main() would not exit 2", err)
	}
	if we.ExitCode() != 2 {
		t.Errorf("ExitCode() = %d, want 2", we.ExitCode())
	}
}

// TestLoadAndValidateWaivers_UsesOracleCommitPassedIn proves the validator
// checks waivers.json against the oracleCommit the caller passed in
// directly (this run's in-memory preflight.OracleCommit) -- ticket 02
// attempt 2's W2 fix means this can no longer be read back from run.json,
// since run.json isn't written until after this validation passes.
func TestLoadAndValidateWaivers_UsesOracleCommitPassedIn(t *testing.T) {
	casesDir := filepath.Join(t.TempDir(), "cases")
	if err := os.MkdirAll(casesDir, 0o755); err != nil {
		t.Fatalf("mkdir cases: %v", err)
	}
	waiversPath := filepath.Join(filepath.Dir(casesDir), "waivers.json")
	writeJSON(t, waiversPath, `[{"id":"version","fields":["stdout"],"reason":"r","oracle_commit":"from-caller"}]`)

	waivers, err := loadAndValidateWaivers(casesDir, "", "from-caller", casesByID(versionCase))
	if err != nil {
		t.Fatalf("loadAndValidateWaivers: %v", err)
	}
	if len(waivers) != 1 {
		t.Fatalf("waivers = %v, want 1", waivers)
	}

	// A waiver pinned to a DIFFERENT commit than the caller passed in must
	// fail -- proves the check is against the passed-in value, not just
	// "any commit value".
	writeJSON(t, waiversPath, `[{"id":"version","fields":["stdout"],"reason":"r","oracle_commit":"stale-commit"}]`)
	_, err = loadAndValidateWaivers(casesDir, "", "from-caller", casesByID(versionCase))
	assertWaiverValidationError(t, err)
}

func TestLoadAndValidateWaivers_ExplicitPathOverridesDefault(t *testing.T) {
	explicit := filepath.Join(t.TempDir(), "custom-waivers.json")
	writeJSON(t, explicit, `[{"id":"version","fields":["stdout"],"reason":"r","oracle_commit":"pin"}]`)

	waivers, err := loadAndValidateWaivers("/nonexistent/cases", explicit, "pin", casesByID(versionCase))
	if err != nil {
		t.Fatalf("loadAndValidateWaivers: %v", err)
	}
	if len(waivers) != 1 {
		t.Errorf("waivers = %v, want 1", waivers)
	}
}

func TestValidateWaivers_TreeWaiverRequiresExactTreePaths(t *testing.T) {
	known := map[string]Case{"c1": {ID: "c1"}}
	base := Waiver{ID: "c1", Taxonomy: "F09", OracleCommit: "deadbeef", Reason: "r", Owner: "o", EvalPlanRef: "x"}

	noPaths := base
	noPaths.Fields = []string{"tree"}
	if err := validateWaivers([]Waiver{noPaths}, known, "deadbeef"); err == nil {
		t.Error("tree waiver without tree_paths must be rejected")
	}

	glob := base
	glob.Fields = []string{"tree"}
	glob.TreePaths = []string{"home/*"}
	if err := validateWaivers([]Waiver{glob}, known, "deadbeef"); err == nil {
		t.Error("tree_paths glob must be rejected")
	}

	stray := base
	stray.Fields = []string{"stdout"}
	stray.TreePaths = []string{"home/.apm/config.json"}
	if err := validateWaivers([]Waiver{stray}, known, "deadbeef"); err == nil {
		t.Error("tree_paths without tree in fields must be rejected")
	}

	ok := base
	ok.Fields = []string{"tree"}
	ok.TreePaths = []string{"home/.apm/config.json"}
	if err := validateWaivers([]Waiver{ok}, known, "deadbeef"); err != nil {
		t.Errorf("exact tree_paths must validate: %v", err)
	}
}
