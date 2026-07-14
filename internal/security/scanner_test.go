package security

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanText_EmptyContentReturnsNil(t *testing.T) {
	if findings := ScanText("", "f.md"); findings != nil {
		t.Errorf("empty content should return nil, got %v", findings)
	}
}

func TestScanText_PureASCIIFastPathReturnsNil(t *testing.T) {
	content := "hello world, this is a normal ASCII file.\nSecond line.\n"
	if findings := ScanText(content, "f.md"); findings != nil {
		t.Errorf("pure ASCII content should return nil, got %v", findings)
	}
}

// TestIsASCII exercises the isascii() fast-path routing helper directly
// (Review Gate A), rather than only asserting ScanText's emergent
// behavior -- an all-ASCII string with no suspicious codepoints would
// return nil either way, which would not actually prove the fast path ran.
func TestIsASCII(t *testing.T) {
	if !isASCII("hello world 123 !@#$%^&*()") {
		t.Error("plain ASCII should be detected as ASCII")
	}
	if !isASCII("") {
		t.Error("empty string is trivially ASCII")
	}
	if isASCII("café") {
		t.Error("non-ASCII (é) should not be detected as ASCII")
	}
	if isASCII(string(rune(0x200B))) {
		t.Error("zero-width space should not be detected as ASCII")
	}
	if isASCII(string(rune(0x7F + 1))) {
		t.Error("first codepoint past ASCII range (0x80) should not be detected as ASCII")
	}
	if !isASCII(string(rune(0x7F))) {
		t.Error("DEL (0x7F, last ASCII codepoint) should be detected as ASCII")
	}
}

// TestScanText_SuspiciousCategories covers every category in
// suspiciousRanges with at least one representative codepoint, asserting
// severity/category classification (Review Gate A).
func TestScanText_SuspiciousCategories(t *testing.T) {
	tests := []struct {
		name     string
		ch       rune
		severity string
		category string
	}{
		{"tag-character", 0xE0041, SeverityCritical, "tag-character"},
		{"bidi-override-LRE", 0x202A, SeverityCritical, "bidi-override"},
		{"bidi-override-RLE", 0x202B, SeverityCritical, "bidi-override"},
		{"bidi-override-PDF", 0x202C, SeverityCritical, "bidi-override"},
		{"bidi-override-LRO", 0x202D, SeverityCritical, "bidi-override"},
		{"bidi-override-RLO", 0x202E, SeverityCritical, "bidi-override"},
		{"bidi-override-LRI", 0x2066, SeverityCritical, "bidi-override"},
		{"bidi-override-RLI", 0x2067, SeverityCritical, "bidi-override"},
		{"bidi-override-FSI", 0x2068, SeverityCritical, "bidi-override"},
		{"bidi-override-PDI", 0x2069, SeverityCritical, "bidi-override"},
		{"variation-selector-smp", 0xE0100, SeverityCritical, "variation-selector"},
		{"zero-width-space", 0x200B, SeverityWarning, "zero-width"},
		{"zero-width-non-joiner", 0x200C, SeverityWarning, "zero-width"},
		{"word-joiner", 0x2060, SeverityWarning, "zero-width"},
		{"variation-selector-cjk", 0xFE00, SeverityWarning, "variation-selector"},
		{"text-presentation-selector", 0xFE0E, SeverityWarning, "variation-selector"},
		{"soft-hyphen", 0x00AD, SeverityWarning, "invisible-formatting"},
		{"bidi-mark-lrm", 0x200E, SeverityWarning, "bidi-mark"},
		{"bidi-mark-rlm", 0x200F, SeverityWarning, "bidi-mark"},
		{"bidi-mark-alm", 0x061C, SeverityWarning, "bidi-mark"},
		{"invisible-function-application", 0x2061, SeverityWarning, "invisible-formatting"},
		{"invisible-times", 0x2062, SeverityWarning, "invisible-formatting"},
		{"invisible-separator", 0x2063, SeverityWarning, "invisible-formatting"},
		{"invisible-plus", 0x2064, SeverityWarning, "invisible-formatting"},
		{"annotation-anchor", 0xFFF9, SeverityWarning, "annotation-marker"},
		{"annotation-separator", 0xFFFA, SeverityWarning, "annotation-marker"},
		{"annotation-terminator", 0xFFFB, SeverityWarning, "annotation-marker"},
		{"deprecated-formatting", 0x206A, SeverityWarning, "deprecated-formatting"},
		{"emoji-presentation-selector", 0xFE0F, SeverityInfo, "variation-selector"},
		{"nbsp", 0x00A0, SeverityInfo, "unusual-whitespace"},
		{"en-space", 0x2002, SeverityInfo, "unusual-whitespace"},
		{"medium-math-space", 0x205F, SeverityInfo, "unusual-whitespace"},
		{"ideographic-space", 0x3000, SeverityInfo, "unusual-whitespace"},
		{"mongolian-vowel-separator", 0x180E, SeverityInfo, "unusual-whitespace"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content := "a" + string(tt.ch) + "b"
			findings := ScanText(content, "f.md")
			if len(findings) != 1 {
				t.Fatalf("expected exactly 1 finding, got %d: %+v", len(findings), findings)
			}
			f := findings[0]
			if f.Severity != tt.severity {
				t.Errorf("severity = %q, want %q", f.Severity, tt.severity)
			}
			if f.Category != tt.category {
				t.Errorf("category = %q, want %q", f.Category, tt.category)
			}
			if f.Line != 1 || f.Column != 2 {
				t.Errorf("line/col = %d/%d, want 1/2", f.Line, f.Column)
			}
			if f.Codepoint == "" {
				t.Error("codepoint should be populated")
			}
		})
	}
}

func TestScanText_ZWJBetweenEmojiIsInfo(t *testing.T) {
	// MAN + ZWJ + WOMAN: minimal 2-emoji ZWJ sequence.
	content := string(rune(0x1F468)) + string(rune(0x200D)) + string(rune(0x1F469))
	findings := ScanText(content, "f.md")
	if len(findings) != 1 {
		t.Fatalf("expected exactly 1 finding (the ZWJ), got %d: %+v", len(findings), findings)
	}
	f := findings[0]
	if f.Severity != SeverityInfo {
		t.Errorf("ZWJ between emoji should downgrade to info, got %q", f.Severity)
	}
	if f.Description != "Zero-width joiner (emoji sequence)" {
		t.Errorf("description = %q", f.Description)
	}
}

func TestScanText_ZWJSkipsSkinToneModifierWhenLookingBackward(t *testing.T) {
	// WOMAN + skin-tone modifier + ZWJ + ROCKET (astronaut emoji sequence):
	// exercises the backward-skip-past-VS16/skin-tone-modifier branch of
	// zwjInEmojiContext, not just a plain adjacent-emoji case.
	content := string(rune(0x1F469)) + string(rune(0x1F3FD)) + string(rune(0x200D)) + string(rune(0x1F680))
	findings := ScanText(content, "f.md")
	if len(findings) != 1 {
		t.Fatalf("expected exactly 1 finding (the ZWJ; skin-tone modifier is not itself in the range table), got %d: %+v", len(findings), findings)
	}
	if findings[0].Severity != SeverityInfo {
		t.Errorf("ZWJ after skin-tone modifier between emoji should be info, got %q", findings[0].Severity)
	}
}

func TestScanText_ZWJOutsideEmojiContextIsWarning(t *testing.T) {
	content := "a" + string(rune(0x200D)) + "b"
	findings := ScanText(content, "f.md")
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Severity != SeverityWarning {
		t.Errorf("ZWJ outside emoji context should stay warning, got %q", findings[0].Severity)
	}
	if findings[0].Category != "zero-width" {
		t.Errorf("category = %q, want zero-width", findings[0].Category)
	}
}

func TestScanText_BOMAtFileStartIsInfo(t *testing.T) {
	content := string(rune(0xFEFF)) + "hello"
	findings := ScanText(content, "f.md")
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	f := findings[0]
	if f.Severity != SeverityInfo || f.Category != "bom" {
		t.Errorf("leading BOM should be info/bom, got %q/%q", f.Severity, f.Category)
	}
	if f.Line != 1 || f.Column != 1 {
		t.Errorf("line/col = %d/%d, want 1/1", f.Line, f.Column)
	}
}

func TestScanText_BOMMidLineIsWarning(t *testing.T) {
	content := "hello " + string(rune(0xFEFF)) + "world"
	findings := ScanText(content, "f.md")
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	f := findings[0]
	if f.Severity != SeverityWarning || f.Category != "zero-width" {
		t.Errorf("mid-file BOM should be warning/zero-width, got %q/%q", f.Severity, f.Category)
	}
}

func TestScanText_BOMAtSecondLineStartIsStillWarning(t *testing.T) {
	// Only line_idx==0 && col_idx==0 counts as "file start" -- a BOM at
	// column 0 of line 2 is mid-file, not a leading BOM.
	content := "hello\n" + string(rune(0xFEFF)) + "world"
	findings := ScanText(content, "f.md")
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Severity != SeverityWarning {
		t.Errorf("BOM at start of line 2 should be warning (not file start), got %q", findings[0].Severity)
	}
}

func TestHasCritical(t *testing.T) {
	findings := []ScanFinding{{Severity: SeverityWarning}, {Severity: SeverityInfo}}
	if HasCritical(findings) {
		t.Error("expected false, no critical findings present")
	}
	findings = append(findings, ScanFinding{Severity: SeverityCritical})
	if !HasCritical(findings) {
		t.Error("expected true, a critical finding is present")
	}
}

func TestSummarizeAndClassify(t *testing.T) {
	findings := []ScanFinding{
		{Severity: SeverityCritical}, {Severity: SeverityCritical},
		{Severity: SeverityWarning},
		{Severity: SeverityInfo}, {Severity: SeverityInfo}, {Severity: SeverityInfo},
	}
	counts := Summarize(findings)
	if counts[SeverityCritical] != 2 || counts[SeverityWarning] != 1 || counts[SeverityInfo] != 3 {
		t.Errorf("Summarize = %v", counts)
	}
	hasCritical, classified := Classify(findings)
	if !hasCritical {
		t.Error("Classify should report hasCritical=true")
	}
	if classified[SeverityCritical] != 2 || classified[SeverityWarning] != 1 || classified[SeverityInfo] != 3 {
		t.Errorf("Classify counts = %v", classified)
	}
}

func TestStripDangerous_RemovesCriticalAndWarning(t *testing.T) {
	content := "a" + string(rune(0x202E)) + "b" + string(rune(0x200B)) + "c"
	got := StripDangerous(content)
	if want := "abc"; got != want {
		t.Errorf("StripDangerous = %q, want %q", got, want)
	}
}

func TestStripDangerous_PreservesInfoLevelChars(t *testing.T) {
	content := "a" + string(rune(0x00A0)) + "b"
	if got := StripDangerous(content); got != content {
		t.Errorf("info-level nbsp should be preserved, got %q want %q", got, content)
	}
}

func TestStripDangerous_PreservesEmojiZWJSequenceIntact(t *testing.T) {
	family := string(rune(0x1F468)) + string(rune(0x200D)) + string(rune(0x1F469))
	if got := StripDangerous(family); got != family {
		t.Errorf("emoji ZWJ sequence should be preserved intact, got %q want %q", got, family)
	}
}

func TestStripDangerous_LeadingBOMPreservedMidFileBOMStripped(t *testing.T) {
	content := string(rune(0xFEFF)) + "a" + string(rune(0xFEFF)) + "b"
	got := StripDangerous(content)
	want := string(rune(0xFEFF)) + "ab"
	if got != want {
		t.Errorf("StripDangerous BOM handling = %q, want %q", got, want)
	}
}

func TestScanFile_NonUTF8ReturnsNil(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "binary.dat")
	if err := os.WriteFile(p, []byte{0xFF, 0xFE, 0x00, 0x01}, 0o644); err != nil {
		t.Fatal(err)
	}
	if findings := ScanFile(p); findings != nil {
		t.Errorf("non-UTF8 file should return nil, got %v", findings)
	}
}

func TestScanFile_MissingFileReturnsNil(t *testing.T) {
	if findings := ScanFile(filepath.Join(t.TempDir(), "missing.md")); findings != nil {
		t.Errorf("missing file should return nil, got %v", findings)
	}
}

func TestScanFile_DetectsSuspiciousContent(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "prompt.md")
	content := "safe" + string(rune(0x202E)) + "text"
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	findings := ScanFile(p)
	if len(findings) != 1 || findings[0].Severity != SeverityCritical {
		t.Fatalf("ScanFile should find 1 critical finding, got %+v", findings)
	}
	if findings[0].File != p {
		t.Errorf("finding.File = %q, want %q", findings[0].File, p)
	}
}
