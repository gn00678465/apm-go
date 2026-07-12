// Package security ports Python apm_cli's hidden-Unicode content scanner
// (security/content_scanner.py) and its policy gate (security/gate.py).
// It detects invisible Unicode characters that could embed hidden
// instructions in prompt/instruction/rules files -- these characters are
// invisible to humans but LLMs tokenize them individually, so a model can
// process instructions a human reviewer never sees on screen.
//
// This package is intentionally dependency-free (no apm-go internals)
// mirroring the Python module's own "no APM internals" design so it can be
// tested and reused independently (e.g. by both `pack` and `audit`).
package security

import (
	"fmt"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ScanFinding is a single suspicious character found during a scan.
type ScanFinding struct {
	File        string
	Line        int
	Column      int
	Char        string // Go-quoted representation, safe to print (invisible chars are otherwise unreadable in a terminal)
	Codepoint   string // hex, e.g. "U+200B"
	Severity    string // "critical", "warning", "info"
	Category    string // e.g. "tag-character", "bidi-override", "zero-width"
	Description string
}

const (
	SeverityCritical = "critical"
	SeverityWarning  = "warning"
	SeverityInfo     = "info"
)

type rangeEntry struct {
	start       rune
	end         rune
	severity    string
	category    string
	description string
}

// suspiciousRanges mirrors content_scanner.py:33-108 (_SUSPICIOUS_RANGES)
// entry-for-entry. range end is inclusive.
var suspiciousRanges = []rangeEntry{
	// ── Critical: no legitimate use in prompt/instruction files ──
	{0xE0001, 0xE007F, SeverityCritical, "tag-character", "Unicode tag character (invisible ASCII mapping)"},
	{0x202A, 0x202A, SeverityCritical, "bidi-override", "Left-to-right embedding (LRE)"},
	{0x202B, 0x202B, SeverityCritical, "bidi-override", "Right-to-left embedding (RLE)"},
	{0x202C, 0x202C, SeverityCritical, "bidi-override", "Pop directional formatting (PDF)"},
	{0x202D, 0x202D, SeverityCritical, "bidi-override", "Left-to-right override (LRO)"},
	{0x202E, 0x202E, SeverityCritical, "bidi-override", "Right-to-left override (RLO)"},
	{0x2066, 0x2066, SeverityCritical, "bidi-override", "Left-to-right isolate (LRI)"},
	{0x2067, 0x2067, SeverityCritical, "bidi-override", "Right-to-left isolate (RLI)"},
	{0x2068, 0x2068, SeverityCritical, "bidi-override", "First strong isolate (FSI)"},
	{0x2069, 0x2069, SeverityCritical, "bidi-override", "Pop directional isolate (PDI)"},
	// Variation selectors -- Glassworm supply-chain attack vector. These
	// attach to visible characters, embedding invisible payload bytes that
	// AST-based tools skip entirely. Sequences of variation selectors can
	// encode arbitrary hidden data/instructions.
	{0xE0100, 0xE01EF, SeverityCritical, "variation-selector", "Variation selector (SMP) — no legitimate use in prompt files"},
	// ── Warning: common copy-paste debris but can hide instructions ──
	{0x200B, 0x200B, SeverityWarning, "zero-width", "Zero-width space"},
	{0x200C, 0x200C, SeverityWarning, "zero-width", "Zero-width non-joiner (ZWNJ)"},
	{0x200D, 0x200D, SeverityWarning, "zero-width", "Zero-width joiner (ZWJ)"},
	{0x2060, 0x2060, SeverityWarning, "zero-width", "Word joiner"},
	// BMP variation selectors -- uncommon in prompt files
	{0xFE00, 0xFE0D, SeverityWarning, "variation-selector", "Variation selector (CJK typography variant)"},
	{0xFE0E, 0xFE0E, SeverityWarning, "variation-selector", "Text presentation selector"},
	{0x00AD, 0x00AD, SeverityWarning, "invisible-formatting", "Soft hyphen"},
	// Bidirectional marks -- invisible, no legitimate use in prompt files
	{0x200E, 0x200E, SeverityWarning, "bidi-mark", "Left-to-right mark (LRM)"},
	{0x200F, 0x200F, SeverityWarning, "bidi-mark", "Right-to-left mark (RLM)"},
	{0x061C, 0x061C, SeverityWarning, "bidi-mark", "Arabic letter mark (ALM)"},
	// Invisible math operators -- zero-width, no use in prompt files
	{0x2061, 0x2061, SeverityWarning, "invisible-formatting", "Function application (invisible operator)"},
	{0x2062, 0x2062, SeverityWarning, "invisible-formatting", "Invisible times"},
	{0x2063, 0x2063, SeverityWarning, "invisible-formatting", "Invisible separator"},
	{0x2064, 0x2064, SeverityWarning, "invisible-formatting", "Invisible plus"},
	// Interlinear annotation markers -- can hide text between delimiters
	{0xFFF9, 0xFFF9, SeverityWarning, "annotation-marker", "Interlinear annotation anchor"},
	{0xFFFA, 0xFFFA, SeverityWarning, "annotation-marker", "Interlinear annotation separator"},
	{0xFFFB, 0xFFFB, SeverityWarning, "annotation-marker", "Interlinear annotation terminator"},
	// Deprecated formatting -- invisible, deprecated since Unicode 3.0
	{0x206A, 0x206F, SeverityWarning, "deprecated-formatting", "Deprecated formatting character"},
	// FEFF as mid-file BOM is handled separately in scan logic
	// ── Info: unusual whitespace, mostly harmless ──
	{0xFE0F, 0xFE0F, SeverityInfo, "variation-selector", "Emoji presentation selector"},
	{0x00A0, 0x00A0, SeverityInfo, "unusual-whitespace", "Non-breaking space"},
	{0x2000, 0x200A, SeverityInfo, "unusual-whitespace", "Unicode whitespace character"},
	{0x205F, 0x205F, SeverityInfo, "unusual-whitespace", "Medium mathematical space"},
	{0x3000, 0x3000, SeverityInfo, "unusual-whitespace", "Ideographic space"},
	{0x180E, 0x180E, SeverityInfo, "unusual-whitespace", "Mongolian vowel separator"},
}

type charInfo struct {
	severity    string
	category    string
	description string
}

// charLookup is a pre-built O(1) per-character classification table,
// mirroring content_scanner.py's module-level _CHAR_LOOKUP.
var charLookup = buildCharLookup()

func buildCharLookup() map[rune]charInfo {
	m := make(map[rune]charInfo)
	for _, r := range suspiciousRanges {
		for cp := r.start; cp <= r.end; cp++ {
			m[cp] = charInfo{r.severity, r.category, r.description}
		}
	}
	return m
}

// isEmojiChar reports whether ch is an emoji base character (Unicode
// category So, Symbol-other), mirroring _is_emoji_char.
func isEmojiChar(ch rune) bool {
	return unicode.Is(unicode.So, ch)
}

// zwjInEmojiContext reports whether the rune at idx sits between two
// emoji-like characters, mirroring _zwj_in_emoji_context. It looks backward
// past VS16 (U+FE0F) and skin-tone modifiers (U+1F3FB-1F3FF) because emoji
// ZWJ sequences frequently interpose these between the base character and
// the joiner, e.g. woman-astronaut = woman + skin-tone + ZWJ + rocket.
func zwjInEmojiContext(runes []rune, idx int) bool {
	prev := idx - 1
	for prev >= 0 {
		cp := runes[prev]
		if cp == 0xFE0F || (cp >= 0x1F3FB && cp <= 0x1F3FF) {
			prev--
			continue
		}
		break
	}
	prevOK := prev >= 0 && isEmojiChar(runes[prev])

	next := idx + 1
	nextOK := next < len(runes) && isEmojiChar(runes[next])

	return prevOK && nextOK
}

// isASCII reports whether s contains only ASCII bytes, mirroring Python's
// str.isascii() fast path: it runs at byte speed and lets ScanText skip the
// rune-level loop for the vast majority of files that are plain ASCII.
func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] > unicode.MaxASCII {
			return false
		}
	}
	return true
}

// ScanText scans a string for suspicious Unicode characters, mirroring
// ContentScanner.scan_text. Returns one finding per suspicious character,
// with 1-based line/column positions.
func ScanText(content string, filename string) []ScanFinding {
	if content == "" {
		return nil
	}
	if isASCII(content) {
		return nil
	}

	var findings []ScanFinding
	lines := strings.Split(content, "\n")

	for lineIdx, lineText := range lines {
		runes := []rune(lineText)
		for colIdx, ch := range runes {
			cp := ch

			// Special case: BOM (U+FEFF) at the very start of the file is
			// standard practice; mid-file is suspicious.
			if cp == 0xFEFF {
				if lineIdx == 0 && colIdx == 0 {
					findings = append(findings, ScanFinding{
						File: filename, Line: 1, Column: 1,
						Char: fmt.Sprintf("%q", ch), Codepoint: "U+FEFF",
						Severity: SeverityInfo, Category: "bom",
						Description: "Byte order mark at start of file",
					})
				} else {
					findings = append(findings, ScanFinding{
						File: filename, Line: lineIdx + 1, Column: colIdx + 1,
						Char: fmt.Sprintf("%q", ch), Codepoint: "U+FEFF",
						Severity: SeverityWarning, Category: "zero-width",
						Description: "Byte order mark in middle of file (possible hidden content)",
					})
				}
				continue
			}

			entry, ok := charLookup[cp]
			if !ok {
				continue
			}
			sev, cat, desc := entry.severity, entry.category, entry.description
			// ZWJ between emoji is legitimate (e.g. family emoji sequences).
			if cp == 0x200D && zwjInEmojiContext(runes, colIdx) {
				sev = SeverityInfo
				desc = "Zero-width joiner (emoji sequence)"
			}
			findings = append(findings, ScanFinding{
				File: filename, Line: lineIdx + 1, Column: colIdx + 1,
				Char: fmt.Sprintf("%q", ch), Codepoint: fmt.Sprintf("U+%04X", cp),
				Severity: sev, Category: cat, Description: desc,
			})
		}
	}

	return findings
}

// ScanFile reads a file and scans its content, mirroring
// ContentScanner.scan_file. Returns nil if the file cannot be read or
// cannot be decoded as UTF-8 (binary files, etc.) -- errors are handled
// gracefully rather than propagated.
func ScanFile(path string) []ScanFinding {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	if !utf8.Valid(data) {
		return nil
	}
	return ScanText(string(data), path)
}

// HasCritical reports whether any finding has critical severity.
func HasCritical(findings []ScanFinding) bool {
	for _, f := range findings {
		if f.Severity == SeverityCritical {
			return true
		}
	}
	return false
}

// Summarize returns counts by severity level.
func Summarize(findings []ScanFinding) map[string]int {
	counts := map[string]int{SeverityCritical: 0, SeverityWarning: 0, SeverityInfo: 0}
	for _, f := range findings {
		counts[f.Severity]++
	}
	return counts
}

// Classify combines HasCritical and Summarize in a single pass.
func Classify(findings []ScanFinding) (bool, map[string]int) {
	critical := false
	counts := map[string]int{SeverityCritical: 0, SeverityWarning: 0, SeverityInfo: 0}
	for _, f := range findings {
		counts[f.Severity]++
		if f.Severity == SeverityCritical {
			critical = true
		}
	}
	return critical, counts
}

// StripDangerous removes critical- and warning-level characters from
// content, mirroring ContentScanner.strip_dangerous.
//
// Info-level characters (emoji selectors, non-breaking spaces, unusual
// whitespace) are preserved -- they are legitimate and stripping them would
// break content. ZWJ between emoji characters is treated as info
// (preserved) to keep compound emoji sequences intact.
func StripDangerous(content string) string {
	runes := []rune(content)
	result := make([]rune, 0, len(runes))
	for i, ch := range runes {
		cp := ch
		entry, ok := charLookup[cp]
		if ok {
			sev := entry.severity
			// ZWJ between emoji is info-level -- preserve it.
			if cp == 0x200D && zwjInEmojiContext(runes, i) {
				result = append(result, ch)
				continue
			}
			if sev == SeverityCritical || sev == SeverityWarning {
				continue // strip it
			}
		} else if cp == 0xFEFF {
			if i != 0 {
				continue // mid-file BOM is warning-level -- strip
			}
			// leading BOM is info-level -- fall through to append
		}
		result = append(result, ch)
	}
	return string(result)
}
