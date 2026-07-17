package yamlcore

import (
	"bytes"
	"fmt"
	"io"
	"strings"

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
	var buf bytes.Buffer
	// yaml.NewEncoder applies WithV3Defaults(), which sets an 80-column
	// WithLineWidth: re-encoding an existing document then wraps any flow
	// content wider than 80 columns, corrupting hand-formatted multi-line
	// flow sequences/mappings that were never touched by the caller's edit.
	// Disable wrapping (WithLineWidth(-1)) so only the caller's actual
	// mutation changes the byte output.
	dumper, err := yaml.NewDumper(&buf, yaml.WithV3Defaults(), yaml.WithLineWidth(-1), yaml.WithIndent(2))
	if err != nil {
		return nil, fmt.Errorf("YAML encoder init error: %w", err)
	}
	if err := dumper.Dump(doc); err != nil {
		return nil, fmt.Errorf("YAML encode error: %w", err)
	}
	if err := dumper.Close(); err != nil {
		return nil, fmt.Errorf("YAML encoder close error: %w", err)
	}
	return buf.Bytes(), nil
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
