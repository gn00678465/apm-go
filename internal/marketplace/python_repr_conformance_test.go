package marketplace

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// pythonReprConformanceRow mirrors one row of
// spec/conformance/python-repr.json -- see
// tools/depref_conformance_gen.py's own doc comment for how the file is
// produced (a one-shot run of CPython's own builtin repr() against
// json.loads(row.JSON), NOT a re-derivation of what this package's port
// believes repr() does) and AGENTS.md's "Schema sync tests... depend on
// conformance spec files under spec/conformance/... runtime inputs
// tracked in git, not generated" convention this test follows.
type pythonReprConformanceRow struct {
	JSON string `json:"json"`
	Repr string `json:"repr"`
}

// pythonReprWhitespaceRow is one code point of the fixture's exhaustive
// isspace() sweep: `is_space` is CPython's own `chr(cp).isspace()` answer,
// asserted 1:1 against pyIsSpace (ticket 11 attempt 6: Go's
// unicode.IsSpace misses Python's U+001C-U+001F, so the boundary is locked
// per-code-point rather than trusted to any one library's table).
type pythonReprWhitespaceRow struct {
	Codepoint int  `json:"codepoint"`
	IsSpace   bool `json:"is_space"`
}

// pythonReprConformanceDoc mirrors spec/conformance/python-repr.json's
// top-level shape; OracleCommit pins the generating Oracle checkout to the
// SAME commit the parity suite is gated on (tools/parity/oracle.pin), the
// identical provenance rule depref_conformance_test.go enforces.
type pythonReprConformanceDoc struct {
	OracleCommit    string                     `json:"oracle_commit"`
	Rows            []pythonReprConformanceRow `json:"rows"`
	WhitespaceSweep []pythonReprWhitespaceRow  `json:"whitespace_sweep"`
}

// TestPythonReprValue_OracleConformance is ticket 11 attempt 5's PART 2:
// after four evaluator round-trips of "fix the reproducer, find another
// valid-JSON repr gap" (lone surrogates, float lexeme overflow, integer
// -0, duplicate object keys), this asserts pythonReprValue/pythonReprPyStr
// against a table of every JSON value shape those functions are meant to
// reproduce Python's repr() for -- scalars, quoting/escaping (including
// surrogate pairs, lone surrogates, non-printable non-ASCII), numeric
// int-vs-float/overflow/negative-zero, nested lists/dicts, and duplicate
// dict keys -- generated directly from CPython's own repr(), not
// recomputed the way pythonReprValue itself computes it.
func TestPythonReprValue_OracleConformance(t *testing.T) {
	data, err := os.ReadFile("../../spec/conformance/python-repr.json")
	if err != nil {
		t.Fatalf("reading spec/conformance/python-repr.json: %v", err)
	}
	var doc pythonReprConformanceDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parsing spec/conformance/python-repr.json: %v", err)
	}
	if len(doc.Rows) == 0 {
		t.Fatal("spec/conformance/python-repr.json is empty")
	}

	pinData, err := os.ReadFile("../../tools/parity/oracle.pin")
	if err != nil {
		t.Fatalf("reading tools/parity/oracle.pin: %v", err)
	}
	wantPin := strings.TrimSpace(string(pinData))
	if doc.OracleCommit != wantPin {
		t.Fatalf("python-repr.json's oracle_commit = %q, want %q (tools/parity/oracle.pin) -- "+
			"the fixture was generated against a different Oracle commit than the parity suite is pinned to; "+
			"regenerate it with tools/depref_conformance_gen.py --oracle-commit %s",
			doc.OracleCommit, wantPin, wantPin)
	}

	for _, row := range doc.Rows {
		t.Run(row.JSON, func(t *testing.T) {
			v, err := decodeOrderedJSON(json.RawMessage(row.JSON))
			if err != nil {
				t.Fatalf("decodeOrderedJSON(%s): %v", row.JSON, err)
			}
			if got := pythonReprValue(v); got != row.Repr {
				t.Errorf("pythonReprValue(%s) = %q, want %q (Oracle repr())", row.JSON, got, row.Repr)
			}
		})
	}

	// The isspace() sweep: every code point CPython classifies (or refuses
	// to classify) as whitespace must agree with pyIsSpace, so
	// pyStrTrimSpace's strip boundary can never silently drift from
	// Python's str.strip() again.
	if len(doc.WhitespaceSweep) == 0 {
		t.Fatal("python-repr.json has no whitespace_sweep -- regenerate the fixture")
	}
	for _, ws := range doc.WhitespaceSweep {
		if got := pyIsSpace(rune(ws.Codepoint)); got != ws.IsSpace {
			t.Errorf("pyIsSpace(U+%04X) = %v, want %v (CPython chr(cp).isspace())",
				ws.Codepoint, got, ws.IsSpace)
		}
	}
}
