package marketplace

import (
	"encoding/json"
	"os"
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
	var rows []pythonReprConformanceRow
	if err := json.Unmarshal(data, &rows); err != nil {
		t.Fatalf("parsing spec/conformance/python-repr.json: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("spec/conformance/python-repr.json is empty")
	}

	for _, row := range rows {
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
}
