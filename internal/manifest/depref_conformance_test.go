package manifest

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// depRefConformanceRow mirrors one row of spec/conformance/depref-accept.json
// -- see tools/depref_conformance_gen.py's own doc comment for how the file
// is produced (a one-shot run of the PINNED Oracle's own
// DependencyReference.parse) and AGENTS.md's "Schema sync tests... depend
// on conformance spec files under spec/conformance/... runtime inputs
// tracked in git, not generated" convention this test follows.
type depRefConformanceRow struct {
	Input         string `json:"input"`
	Category      string `json:"category"`
	Accepted      bool   `json:"accepted"`
	IsLocal       bool   `json:"is_local"`
	ErrorType     string `json:"error_type"`
	KnownGap      string `json:"known_gap"`
	ApmgoAccepted bool   `json:"apmgo_accepted"`
	ApmgoIsLocal  bool   `json:"apmgo_is_local"`
}

type depRefConformanceDoc struct {
	OracleCommit string                 `json:"oracle_commit"`
	Rows         []depRefConformanceRow `json:"rows"`
}

// TestParseDepString_OracleConformance is ticket 11 attempt 5's PART 2: a
// strategy change after four evaluator round-trips of "fix the reproducer,
// find another corner" -- instead of chasing individual eval-supplied
// examples, this asserts ParseDepString's accept/reject boundary against a
// table generated directly from the pinned Oracle's own
// DependencyReference.parse across every category reachable from a
// marketplace source coordinate (shorthand, https/http/ssh/SCP, mixed-case
// schemes, users, ports, queries/fragments, percent-encoding, GitLab/ADO
// shapes, IPv6/odd hosts, traversal, control characters, empty/whitespace).
//
// Ticket 11 attempt 6's "Standards" fixes:
//   - The fixture's oracle_commit must match tools/parity/oracle.pin -- the
//     SAME pin the parity suite itself is gated on -- so a fixture silently
//     regenerated against a drifted Oracle checkout is caught here, not
//     discovered later as an unexplained diff.
//   - A KnownGap row is no longer just logged: it asserts ApmgoAccepted/
//     ApmgoIsLocal (the CURRENT, documented, deliberately-diverging apm-go
//     behavior recorded in the fixture) against the live implementation.
//     This locks BOTH directions of a known gap -- an accidental future
//     change to apm-go that either closes the gap (starts matching the
//     Oracle) or widens it further (diverges in some new way) now fails
//     this test, instead of silently drifting unnoticed.
func TestParseDepString_OracleConformance(t *testing.T) {
	data, err := os.ReadFile("../../spec/conformance/depref-accept.json")
	if err != nil {
		t.Fatalf("reading spec/conformance/depref-accept.json: %v", err)
	}
	var doc depRefConformanceDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parsing spec/conformance/depref-accept.json: %v", err)
	}
	if len(doc.Rows) == 0 {
		t.Fatal("spec/conformance/depref-accept.json is empty")
	}

	pinData, err := os.ReadFile("../../tools/parity/oracle.pin")
	if err != nil {
		t.Fatalf("reading tools/parity/oracle.pin: %v", err)
	}
	wantPin := strings.TrimSpace(string(pinData))
	if doc.OracleCommit != wantPin {
		t.Fatalf("depref-accept.json's oracle_commit = %q, want %q (tools/parity/oracle.pin) -- "+
			"the fixture was generated against a different Oracle commit than the parity suite is pinned to; "+
			"regenerate it with tools/depref_conformance_gen.py --oracle-commit %s",
			doc.OracleCommit, wantPin, wantPin)
	}

	knownGaps := 0
	for _, row := range doc.Rows {
		t.Run(row.Category+"/"+row.Input, func(t *testing.T) {
			ref, err := ParseDepString(row.Input)
			gotAccepted := err == nil

			if row.KnownGap != "" {
				knownGaps++
				t.Logf("known gap: %s", row.KnownGap)
				if gotAccepted != row.ApmgoAccepted {
					t.Errorf("ParseDepString(%q): accepted = %v (err=%v), want accepted = %v "+
						"(documented apm-go behavior for this known_gap row -- if this changed "+
						"deliberately, update apmgo_accepted in the fixture)",
						row.Input, gotAccepted, err, row.ApmgoAccepted)
					return
				}
				if gotAccepted && ref.IsLocal != row.ApmgoIsLocal {
					t.Errorf("ParseDepString(%q): IsLocal = %v, want %v (documented apm-go behavior)",
						row.Input, ref.IsLocal, row.ApmgoIsLocal)
				}
				return
			}

			if gotAccepted != row.Accepted {
				t.Errorf("ParseDepString(%q): accepted = %v (err=%v), want accepted = %v (Oracle)",
					row.Input, gotAccepted, err, row.Accepted)
				return
			}
			if gotAccepted && ref.IsLocal != row.IsLocal {
				t.Errorf("ParseDepString(%q): IsLocal = %v, want %v (Oracle)", row.Input, ref.IsLocal, row.IsLocal)
			}
		})
	}
	if knownGaps == 0 {
		t.Log("no known-gap rows in this fixture revision")
	}
}
