package compile

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func inst(relPath, applyTo, body string) SourcedInstruction {
	return SourcedInstruction{
		RelPath:           relPath,
		ParsedInstruction: ParsedInstruction{ApplyTo: applyTo, Body: body},
	}
}

// TestRender_RawApplyToCommaAndBrace covers CMP-05: applyTo values with an
// embedded comma or brace alternation must produce exactly ONE heading each,
// verbatim -- compile groups by the raw applyTo string, never install's
// comma-splitting parseApplyTo (design.md §3 note; oracle probe 2026-07-11).
func TestRender_RawApplyToCommaAndBrace(t *testing.T) {
	instructions := []SourcedInstruction{
		inst(".apm/instructions/comma.instructions.md", "**/src/**, **/api/**", "Comma body."),
		inst(".apm/instructions/brace.instructions.md", "**/*.{css,scss}", "Brace body."),
	}
	got := renderInstructionsContent(instructions)

	if strings.Count(got, "## Files matching") != 2 {
		t.Fatalf("expected exactly 2 headings, got content:\n%s", got)
	}
	if !strings.Contains(got, "## Files matching `**/src/**, **/api/**`") {
		t.Errorf("comma pattern not kept verbatim as one heading:\n%s", got)
	}
	if !strings.Contains(got, "## Files matching `**/*.{css,scss}`") {
		t.Errorf("brace pattern not kept literal:\n%s", got)
	}
}

// TestRender_DeterministicGroupingAndSorting covers CMP-06: Global always
// first, pattern groups sorted lexically by pattern, each group's
// instructions sorted by RelPath -- independent of input order, run
// repeatedly to catch any accidental map-iteration nondeterminism.
func TestRender_DeterministicGroupingAndSorting(t *testing.T) {
	instructions := []SourcedInstruction{
		inst("apm_modules/z/.apm/instructions/z.instructions.md", "**/*.py", "Z body."),
		inst(".apm/instructions/b.instructions.md", "", "B global."),
		inst(".apm/instructions/a.instructions.md", "**/*.go", "A body."),
		inst(".apm/instructions/y.instructions.md", "**/*.py", "Y body."),
		inst(".apm/instructions/c.instructions.md", "", "C global."),
	}

	var first string
	for i := 0; i < 25; i++ {
		got := RenderAgentsMD(instructions, "test-version")
		if i == 0 {
			first = got
			continue
		}
		if got != first {
			t.Fatalf("output not deterministic across runs (iteration %d)", i)
		}
	}

	wantOrder := []string{
		"## Global Instructions",
		".apm/instructions/b.instructions.md",
		".apm/instructions/c.instructions.md",
		"## Files matching `**/*.go`",
		".apm/instructions/a.instructions.md",
		"## Files matching `**/*.py`",
		".apm/instructions/y.instructions.md",
		"apm_modules/z/.apm/instructions/z.instructions.md",
	}
	lastIdx := -1
	for _, marker := range wantOrder {
		idx := strings.Index(first, marker)
		if idx == -1 {
			t.Fatalf("marker %q not found in output:\n%s", marker, first)
		}
		if idx <= lastIdx {
			t.Fatalf("marker %q out of order (idx=%d, previous=%d):\n%s", marker, idx, lastIdx, first)
		}
		lastIdx = idx
	}
}

// TestRender_FiltersEmptyBodies covers CMP-07: an instruction whose body is
// blank (or only frontmatter) produces no Source/End-source block, but a
// valid sibling in the same group still renders normally. NOTE: the
// group's HEADING itself still appears even when every member is empty --
// this is a confirmed oracle quirk (scratch probe 2026-07-11: an
// isolated empty-body global/pattern instruction still emits an orphan
// "## Global Instructions" / "## Files matching ..." heading with nothing
// under it), mirrored here for oracle parity rather than the more
// "intuitive" no-orphan-heading behavior.
func TestRender_FiltersEmptyBodies(t *testing.T) {
	t.Run("empty sibling is dropped, valid one still renders", func(t *testing.T) {
		instructions := []SourcedInstruction{
			inst(".apm/instructions/empty.instructions.md", "", ""),
			inst(".apm/instructions/valid.instructions.md", "", "Valid body."),
		}
		got := renderInstructionsContent(instructions)
		if strings.Contains(got, "empty.instructions.md") {
			t.Errorf("empty-body instruction must not appear in output:\n%s", got)
		}
		if !strings.Contains(got, "<!-- Source: .apm/instructions/valid.instructions.md -->") {
			t.Errorf("valid sibling missing Source comment:\n%s", got)
		}
		if !strings.Contains(got, "Valid body.") {
			t.Errorf("valid sibling body missing:\n%s", got)
		}
	})

	t.Run("orphan heading for all-empty global group (oracle quirk)", func(t *testing.T) {
		instructions := []SourcedInstruction{
			inst(".apm/instructions/onlyempty.instructions.md", "", ""),
		}
		got := renderInstructionsContent(instructions)
		if !strings.Contains(got, globalHeading) {
			t.Errorf("expected orphan Global Instructions heading, got:\n%s", got)
		}
		if strings.Contains(got, "<!-- Source:") {
			t.Errorf("no Source comment should appear for an all-empty group:\n%s", got)
		}
	})

	t.Run("orphan heading for all-empty pattern group (oracle quirk)", func(t *testing.T) {
		instructions := []SourcedInstruction{
			inst(".apm/instructions/onlyempty.instructions.md", "**/*.md", ""),
		}
		got := renderInstructionsContent(instructions)
		if !strings.Contains(got, "## Files matching `**/*.md`") {
			t.Errorf("expected orphan pattern heading, got:\n%s", got)
		}
		if strings.Contains(got, "<!-- Source:") {
			t.Errorf("no Source comment should appear for an all-empty group:\n%s", got)
		}
	})
}

// TestRender_OracleTemplate asserts the full document shape line-by-line
// against the oracle-captured template (design.md §4; oracle:
// compilation/template_builder.py:153-167,189-224; scratch probe
// 2026-07-11 byte-for-byte capture).
func TestRender_OracleTemplate(t *testing.T) {
	instructions := []SourcedInstruction{
		inst(".apm/instructions/global.instructions.md", "", "Global body [G1]."),
		inst(".apm/instructions/gostyle.instructions.md", "**/*.go", "Go body [S1]."),
	}
	got := RenderAgentsMD(instructions, "0.21.0")
	got = StabilizeBuildID(got)

	lines := strings.Split(got, "\n")
	want := []string{
		"# AGENTS.md",
		"<!-- Generated by APM CLI from .apm/ primitives -->",
	}
	for i, w := range want {
		if lines[i] != w {
			t.Fatalf("line %d = %q, want %q\nfull output:\n%s", i, lines[i], w, got)
		}
	}
	if !strings.HasPrefix(lines[2], "<!-- Build ID: ") || !strings.HasSuffix(lines[2], " -->") {
		t.Fatalf("line 2 (Build ID) malformed: %q", lines[2])
	}
	if lines[3] != "<!-- APM Version: 0.21.0 -->" {
		t.Fatalf("line 3 (APM Version) = %q", lines[3])
	}

	wantTail := []string{
		"",
		"## Global Instructions",
		"",
		"<!-- Source: .apm/instructions/global.instructions.md -->",
		"Global body [G1].",
		"<!-- End source: .apm/instructions/global.instructions.md -->",
		"",
		"## Files matching `**/*.go`",
		"",
		"<!-- Source: .apm/instructions/gostyle.instructions.md -->",
		"Go body [S1].",
		"<!-- End source: .apm/instructions/gostyle.instructions.md -->",
		"",
		"---",
		"*This file was generated by APM CLI. Do not edit manually.*",
		"*To regenerate: `apm compile`*",
		"",
	}
	gotTail := lines[4:]
	if len(gotTail) != len(wantTail) {
		t.Fatalf("tail line count = %d, want %d\nfull output:\n%s", len(gotTail), len(wantTail), got)
	}
	for i, w := range wantTail {
		if gotTail[i] != w {
			t.Errorf("tail line %d = %q, want %q", i, gotTail[i], w)
		}
	}

	if strings.Contains(got, "distributed") {
		t.Errorf("single-file output must not contain a distributed-mode marker:\n%s", got)
	}
}

// TestRender_UTF8LFAndTrailingNewline covers CMP-09: UTF-8 bytes survive
// round-trip, no CR anywhere in the output (documented LF deviation --
// design.md §4/§7; oracle writes CRLF on Windows, Go intentionally does
// not), and the file ends with exactly one trailing "\n".
func TestRender_UTF8LFAndTrailingNewline(t *testing.T) {
	instructions := []SourcedInstruction{
		inst(".apm/instructions/unicode.instructions.md", "", "中文內容 and émoji 🎉."),
	}
	got := StabilizeBuildID(RenderAgentsMD(instructions, "0.1.0"))

	if strings.ContainsRune(got, '\r') {
		t.Errorf("output must not contain any CR byte:\n%q", got)
	}
	if !strings.Contains(got, "中文內容 and émoji 🎉.") {
		t.Errorf("UTF-8 body not preserved verbatim:\n%s", got)
	}
	if !strings.HasSuffix(got, "\n") || strings.HasSuffix(got, "\n\n") {
		t.Errorf("output must end with exactly one trailing newline, got suffix %q", got[len(got)-3:])
	}
}

// TestBuildID_OracleAlgorithm covers CMP-10: the placeholder line is
// removed (not blanked) before hashing, the rest joined by LF, SHA256'd,
// first 12 hex chars kept -- oracle: compilation/build_id.py:22-39.
func TestBuildID_OracleAlgorithm(t *testing.T) {
	content := "# AGENTS.md\n<!-- Generated by APM CLI from .apm/ primitives -->\n" +
		buildIDPlaceholder + "\n<!-- APM Version: 0.1.0 -->\n\nBody.\n"

	got := StabilizeBuildID(content)

	if strings.Contains(got, buildIDPlaceholder) {
		t.Fatalf("placeholder must not survive stabilization:\n%s", got)
	}

	// Recompute expected hash the same way build_id.py does: placeholder
	// line REMOVED (not blanked), remaining lines LF-joined, sha256, first
	// 12 hex chars.
	lines := []string{
		"# AGENTS.md",
		"<!-- Generated by APM CLI from .apm/ primitives -->",
		"<!-- APM Version: 0.1.0 -->",
		"",
		"Body.",
	}
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	wantID := hex.EncodeToString(sum[:])[:12]
	wantLine := "<!-- Build ID: " + wantID + " -->"

	if !strings.Contains(got, wantLine) {
		t.Errorf("Build ID line = missing %q, got:\n%s", wantLine, got)
	}

	// Idempotent: re-stabilizing already-stabilized content is a no-op.
	again := StabilizeBuildID(got)
	if again != got {
		t.Errorf("re-stabilizing changed content:\nfirst:  %q\nsecond: %q", got, again)
	}

	// Content changes -> Build ID changes.
	changed := strings.Replace(content, "Body.", "Body changed.", 1)
	gotChanged := StabilizeBuildID(changed)
	if gotChanged == got {
		t.Errorf("changed body did not change Build ID")
	}

	// Trailing newline preserved.
	if !strings.HasSuffix(got, "\n") {
		t.Errorf("trailing newline not preserved")
	}

	// No placeholder in input -> content returned unchanged.
	noPlaceholder := "# AGENTS.md\nno placeholder here\n"
	if out := StabilizeBuildID(noPlaceholder); out != noPlaceholder {
		t.Errorf("content without placeholder must be returned unchanged, got %q", out)
	}
}

// TestVersionLine_IsOnlyVersionSpecificTemplateDifference covers CMP-11:
// Go writes its own non-empty APM Version; after normalizing the Build ID
// and APM Version lines, the rest of the template bytes are identical
// regardless of which version string was supplied.
func TestVersionLine_IsOnlyVersionSpecificTemplateDifference(t *testing.T) {
	instructions := []SourcedInstruction{
		inst(".apm/instructions/a.instructions.md", "", "Body."),
	}
	a := StabilizeBuildID(RenderAgentsMD(instructions, "0.1.0"))
	b := StabilizeBuildID(RenderAgentsMD(instructions, "9.9.9-different"))

	normalize := func(s string) string {
		lines := strings.Split(s, "\n")
		for i, l := range lines {
			if strings.HasPrefix(l, "<!-- Build ID: ") {
				lines[i] = "<!-- Build ID: NORMALIZED -->"
			}
			if strings.HasPrefix(l, "<!-- APM Version: ") {
				lines[i] = "<!-- APM Version: NORMALIZED -->"
			}
		}
		return strings.Join(lines, "\n")
	}

	if normalize(a) != normalize(b) {
		t.Errorf("only Build ID / APM Version lines should differ:\na: %q\nb: %q", normalize(a), normalize(b))
	}
	if !strings.Contains(a, "<!-- APM Version: 0.1.0 -->") {
		t.Errorf("expected non-empty APM Version line in a")
	}
}
