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
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return nil, fmt.Errorf("YAML encode error: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("YAML encoder close error: %w", err)
	}
	return buf.Bytes(), nil
}

func validateNode(n *yaml.Node) error {
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
		if err := validateNode(c); err != nil {
			return err
		}
	}
	return nil
}
