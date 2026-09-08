package yamlcore

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v4"
)

// SafeLoad parses YAML data under the OpenAPM v0.1 safe subset (req-mf-020):
//   - (b) rejects &anchor / *alias constructs
//   - (c) rejects custom (non-!!) tags
//
// Rejects multi-document YAML streams (only single documents are valid for
// manifest, lockfile, and policy files).
//
// Clauses (a) and (d) are enforced by typed accessor functions in later phases;
// the Node tree preserves implicit tags for round-trip fidelity.
func SafeLoad(data []byte) (*yaml.Node, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))

	var doc yaml.Node
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("YAML parse error: %w", err)
	}

	var extra yaml.Node
	if err := dec.Decode(&extra); err == nil {
		return nil, fmt.Errorf("multi-document YAML streams are not allowed")
	} else if err != io.EOF {
		return nil, fmt.Errorf("YAML parse error in trailing content: %w", err)
	}

	if err := validateNode(&doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

// SafeDump re-serializes a validated yaml.Node to bytes.
// The output is byte-equivalent to the original input for conforming documents
// (req-ext-001, req-mf-006, req-cf-001).
func SafeDump(doc *yaml.Node) ([]byte, error) {
	return safeDump(doc, false)
}

// SafeDumpManifest serializes a newly generated apm.yml using the Oracle's
// PyYAML-compatible cosmetics: UTF-8 scalars, single quotes when quoting is
// required, and indentless block sequences. Existing user-edited YAML keeps
// SafeDump's historical v3-compatible formatting so round-trip patching does
// not rewrite unrelated documents.
func SafeDumpManifest(doc *yaml.Node) ([]byte, error) {
	return safeDump(doc, true)
}

func safeDump(doc *yaml.Node, manifestStyle bool) ([]byte, error) {
	var buf bytes.Buffer
	// yaml.NewEncoder applies WithV3Defaults(), which sets an 80-column
	// WithLineWidth: re-encoding an existing document then wraps any flow
	// content wider than 80 columns, corrupting hand-formatted multi-line
	// flow sequences/mappings that were never touched by the caller's edit.
	// Disable wrapping (WithLineWidth(-1)) so only the caller's actual
	// mutation changes the byte output.
	options := []yaml.Option{yaml.WithV3Defaults(), yaml.WithLineWidth(-1), yaml.WithIndent(2)}
	if manifestStyle {
		options = append(options, yaml.WithUnicode(true), yaml.WithQuotePreference(yaml.QuoteSingle), yaml.WithCompactSeqIndent(true))
	}
	dumper, err := yaml.NewDumper(&buf, options...)
	if err != nil {
		return nil, fmt.Errorf("YAML encoder init error: %w", err)
	}
	if err := dumper.Dump(doc); err != nil {
		return nil, fmt.Errorf("YAML encode error: %w", err)
	}
	if err := dumper.Close(); err != nil {
		return nil, fmt.Errorf("YAML encoder close error: %w", err)
	}
	if manifestStyle {
		return normalizeAstralScalars(buf.Bytes()), nil
	}
	return buf.Bytes(), nil
}

// normalizeAstralScalars repairs a limitation in go-yaml's printable-rune
// table: it treats four-byte UTF-8 runes as non-printable and therefore emits
// them as \U escapes even with WithUnicode(true). PyYAML (the Oracle) emits
// those runes as UTF-8, and emits them as plain scalars whenever YAML permits.
// Restrict the repair to inline double-quoted scalar values containing the
// emitter's \UXXXXXXXX form; all other SafeDump formatting remains owned by
// go-yaml.
func normalizeAstralScalars(data []byte) []byte {
	lines := strings.SplitAfter(string(data), "\n")
	for i, line := range lines {
		lines[i] = normalizeAstralScalarLine(line)
	}
	return []byte(strings.Join(lines, ""))
}

func normalizeAstralScalarLine(line string) string {
	colon := strings.Index(line, ": ")
	if colon < 0 {
		return line
	}
	prefix := line[:colon+2]
	value := line[colon+2:]
	if !strings.HasPrefix(value, "\"") {
		return line
	}
	close := quotedScalarEnd(value)
	if close < 0 || !strings.Contains(value[:close], "\\U") {
		return line
	}
	decoded, ok := decodeAstralEscapes(value[1:close])
	if !ok {
		return line
	}
	if plainScalarSafe(decoded) {
		return prefix + decoded + value[close+1:]
	}
	return prefix + `"` + decoded + `"` + value[close+1:]
}

func quotedScalarEnd(value string) int {
	for i := 1; i < len(value); i++ {
		if value[i] != '\\' {
			if value[i] == '"' {
				return i
			}
			continue
		}
		i++
	}
	return -1
}

func decodeAstralEscapes(value string) (string, bool) {
	var out strings.Builder
	changed := false
	for i := 0; i < len(value); {
		if value[i] != '\\' {
			out.WriteByte(value[i])
			i++
			continue
		}
		if i+1 < len(value) && value[i+1] == 'U' && i+9 <= len(value) {
			raw := value[i+2 : i+10]
			r, err := strconv.ParseUint(raw, 16, 32)
			if err == nil && r >= 0x10000 && r <= utf8.MaxRune {
				out.WriteRune(rune(r))
				i += 10
				changed = true
				continue
			}
		}
		out.WriteByte(value[i])
		i++
	}
	return out.String(), changed
}

func plainScalarSafe(value string) bool {
	if value == "" || strings.TrimSpace(value) != value || strings.ContainsAny(value, "\r\n\t\\") {
		return false
	}
	if strings.HasPrefix(value, "-") || strings.HasPrefix(value, "?") || strings.HasPrefix(value, ":") || strings.HasPrefix(value, "#") || strings.HasPrefix(value, "!") || strings.HasPrefix(value, "&") || strings.HasPrefix(value, "*") || strings.HasPrefix(value, "{") || strings.HasPrefix(value, "[") || strings.HasPrefix(value, ",") || strings.HasPrefix(value, ">") || strings.HasPrefix(value, "|") || strings.HasPrefix(value, "%") || strings.HasPrefix(value, "@") || strings.HasPrefix(value, "`") {
		return false
	}
	for i := 0; i < len(value); i++ {
		if value[i] == ':' && (i+1 == len(value) || value[i+1] == ' ' || value[i+1] == '\t') {
			return false
		}
		if value[i] == '#' && i > 0 && (value[i-1] == ' ' || value[i-1] == '\t') {
			return false
		}
	}
	switch value {
	case "~", "null", "Null", "NULL", "true", "True", "TRUE", "false", "False", "FALSE", "yes", "Yes", "YES", "no", "No", "NO", "on", "On", "ON", "off", "Off", "OFF":
		return false
	}
	return true
}

// maxNodeDepth bounds validateNode's recursion over the parsed node tree.
// Manifest/lockfile/policy documents are shallow (a handful of levels); a
// pathologically deep document (e.g. thousands of nested flow sequences) is
// rejected outright so neither validateNode nor the downstream typed accessors
// can be driven into stack exhaustion. Generous enough never to reject a real
// document.
const maxNodeDepth = 100

func validateNode(n *yaml.Node) error {
	return validateNodeDepth(n, 0)
}

func validateNodeDepth(n *yaml.Node, depth int) error {
	if depth > maxNodeDepth {
		return fmt.Errorf("YAML nesting exceeds the maximum depth of %d", maxNodeDepth)
	}
	if n.Anchor != "" {
		return fmt.Errorf("YAML anchors are not allowed (line %d)", n.Line)
	}
	if n.Alias != nil {
		return fmt.Errorf("YAML aliases are not allowed (line %d)", n.Line)
	}
	tag := n.ShortTag()
	if tag != "" && !strings.HasPrefix(tag, "!!") {
		return fmt.Errorf("custom YAML tag %q is not allowed (line %d)", tag, n.Line)
	}
	for _, c := range n.Content {
		if err := validateNodeDepth(c, depth+1); err != nil {
			return err
		}
	}
	return nil
}
