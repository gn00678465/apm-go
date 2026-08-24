package ux

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// richWrapConformanceRow mirrors one row of spec/conformance/rich-wrap.json
// -- see tools/rich_wrap_conformance_gen.py's own doc comment for how the
// file is produced (a one-shot run of the pinned Oracle's own installed
// `rich` package, through a REAL Console(width=...).print pipeline -- hard-
// newline split, divide_line, rstrip_end, truncate -- not a re-derivation
// of what wrap.go's port believes Rich does) and AGENTS.md's "Schema sync
// tests... depend on conformance spec files under spec/conformance/...
// runtime inputs tracked in git, not generated" convention this follows.
type richWrapConformanceRow struct {
	Name    string `json:"name"`
	Text    string `json:"text"`
	Width   int    `json:"width"`
	Wrapped string `json:"wrapped"`
}

type richWrapConformanceDoc struct {
	OracleCommit string                   `json:"oracle_commit"`
	Rows         []richWrapConformanceRow `json:"rows"`
}

// TestWrapOracleText_ConformsToRichFixture is ticket 14 attempt 2's lock on
// the whole wrap surface: eval-ticket-14.md ruled the attempt-1 port was
// "not byte-faithful" across four corner classes (long-word data loss,
// COLUMNS, CJK cell width, hard-newline reset) discovered only by hand-
// probing the Oracle one reproducer at a time. This asserts wrapOracleText
// against a checked-in table generated directly from the pinned Oracle's
// own rich library instead, covering those same four classes plus the two
// real apm-go marketplace messages and a COLUMNS-equivalent width override,
// so a future regression on any of them fails a single table-driven test
// rather than requiring another live-probing round trip.
func TestWrapOracleText_ConformsToRichFixture(t *testing.T) {
	data, err := os.ReadFile("../../spec/conformance/rich-wrap.json")
	if err != nil {
		t.Fatalf("reading spec/conformance/rich-wrap.json: %v", err)
	}
	var doc richWrapConformanceDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parsing spec/conformance/rich-wrap.json: %v", err)
	}
	if len(doc.Rows) == 0 {
		t.Fatal("spec/conformance/rich-wrap.json is empty")
	}

	pinData, err := os.ReadFile("../../tools/parity/oracle.pin")
	if err != nil {
		t.Fatalf("reading tools/parity/oracle.pin: %v", err)
	}
	wantPin := strings.TrimSpace(string(pinData))
	if doc.OracleCommit != wantPin {
		t.Fatalf("rich-wrap.json's oracle_commit = %q, want %q (tools/parity/oracle.pin) -- "+
			"the fixture was generated against a different Oracle commit than the parity suite is pinned to; "+
			"regenerate it with tools/rich_wrap_conformance_gen.py --oracle-commit %s",
			doc.OracleCommit, wantPin, wantPin)
	}

	for _, row := range doc.Rows {
		t.Run(row.Name, func(t *testing.T) {
			got := wrapOracleText(row.Text, row.Width)
			if got != row.Wrapped {
				t.Errorf("wrapOracleText(_, %d) =\n%q\nwant (Oracle rich.Console)\n%q", row.Width, got, row.Wrapped)
			}
		})
	}
}
