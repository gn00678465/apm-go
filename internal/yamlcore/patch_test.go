package yamlcore

import (
	"strings"
	"testing"

	"go.yaml.in/yaml/v4"
)

// TestPatchMappingPath_AppendToExistingFlowSeq_LeavesRestByteExact guards
// against a real bug found against a hand-formatted apm.yml: appending one
// entry to dependencies.mcp must not touch the byte content of any other
// top-level key (e.g. dependencies.apm), even though both were re-parsed
// through the same *yaml.Node tree.
func TestPatchMappingPath_AppendToExistingFlowSeq_LeavesRestByteExact(t *testing.T) {
	src := []byte(`author: Madao
dependencies:
  apm:
    [
      { git: "https://github.com/getsentry/skills", skills: [skill-writer] },
      { git: "https://github.com/getsentry/skills", skills: [skill-writer] },
    ]
  mcp:
    [
      { name: io.github.github/github-mcp-server, transport: http },
      { name: my-api, registry: false, transport: http, url: "https://mcp.example.com" },
    ]
description: APM project for demo
name: demo
version: 1.0.0
`)
	node, err := SafeLoad(src)
	if err != nil {
		t.Fatal(err)
	}
	root := node.Content[0]
	deps := findMappingChildForTest(t, root, "dependencies")
	mcp := findMappingChildForTest(t, deps, "mcp")

	newEntry := &yaml.Node{
		Kind: yaml.MappingNode,
		Tag:  "!!map",
		Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Value: "name", Tag: "!!str"},
			{Kind: yaml.ScalarNode, Value: "filesystem", Tag: "!!str"},
			{Kind: yaml.ScalarNode, Value: "registry", Tag: "!!str"},
			{Kind: yaml.ScalarNode, Value: "false", Tag: "!!bool"},
			{Kind: yaml.ScalarNode, Value: "transport", Tag: "!!str"},
			{Kind: yaml.ScalarNode, Value: "stdio", Tag: "!!str"},
			{Kind: yaml.ScalarNode, Value: "command", Tag: "!!str"},
			{Kind: yaml.ScalarNode, Value: "npx", Tag: "!!str"},
		},
	}
	mcp.Content = append(mcp.Content, newEntry)

	out, ok, err := PatchMappingPath(src, node, []string{"dependencies", "mcp"})
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("PatchMappingPath returned ok=false, expected a successful patch")
	}

	outStr := string(out)
	t.Logf("patched output:\n%s", outStr)

	// The untouched dependencies.apm block must survive byte-for-byte,
	// including its original one-entry-per-line formatting.
	const untouchedApmBlock = `  apm:
    [
      { git: "https://github.com/getsentry/skills", skills: [skill-writer] },
      { git: "https://github.com/getsentry/skills", skills: [skill-writer] },
    ]
`
	if !strings.Contains(outStr, untouchedApmBlock) {
		t.Errorf("dependencies.apm block was reformatted; want it byte-exact:\n%s", untouchedApmBlock)
	}

	// Untouched trailing keys must also survive verbatim.
	const untouchedTail = "description: APM project for demo\nname: demo\nversion: 1.0.0\n"
	if !strings.HasSuffix(outStr, untouchedTail) {
		t.Errorf("trailing keys were reformatted; want suffix:\n%s\ngot:\n%s", untouchedTail, outStr)
	}

	// The new entry must actually be present and the doc must still parse.
	if !strings.Contains(outStr, "filesystem") {
		t.Errorf("new mcp entry not found in output")
	}
	reparsed, err := SafeLoad(out)
	if err != nil {
		t.Fatalf("patched output does not parse as valid YAML: %v", err)
	}
	_ = reparsed
}

// TestPatchMappingPath_CreatesMissingMCPKey covers the case where
// dependencies exists but dependencies.mcp does not yet.
func TestPatchMappingPath_CreatesMissingMCPKey(t *testing.T) {
	src := []byte(`author: Madao
dependencies:
  apm:
    [
      { git: "https://example.com/x", skills: [a] },
    ]
name: demo
version: 1.0.0
`)
	node, err := SafeLoad(src)
	if err != nil {
		t.Fatal(err)
	}
	root := node.Content[0]
	deps := findMappingChildForTest(t, root, "dependencies")
	mcpSeq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	deps.Content = append(deps.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: "mcp", Tag: "!!str"}, mcpSeq)
	mcpSeq.Content = append(mcpSeq.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: "filesystem", Tag: "!!str"})

	out, ok, err := PatchMappingPath(src, node, []string{"dependencies", "mcp"})
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}
	outStr := string(out)
	t.Logf("patched output:\n%s", outStr)

	const untouchedApmBlock = `  apm:
    [
      { git: "https://example.com/x", skills: [a] },
    ]
`
	if !strings.Contains(outStr, untouchedApmBlock) {
		t.Errorf("dependencies.apm block was reformatted; want it byte-exact:\n%s\ngot:\n%s", untouchedApmBlock, outStr)
	}
	if !strings.Contains(outStr, "mcp:") || !strings.Contains(outStr, "filesystem") {
		t.Errorf("new mcp key not found in output:\n%s", outStr)
	}
	if !strings.HasSuffix(outStr, "name: demo\nversion: 1.0.0\n") {
		t.Errorf("trailing keys were reformatted, got:\n%s", outStr)
	}
	if _, err := SafeLoad(out); err != nil {
		t.Fatalf("patched output does not parse: %v", err)
	}
}

// TestPatchMappingPath_CreatesMissingDependenciesKey covers a document
// with no dependencies key at all yet.
func TestPatchMappingPath_CreatesMissingDependenciesKey(t *testing.T) {
	src := []byte(`author: Madao
name: demo
version: 1.0.0
`)
	node, err := SafeLoad(src)
	if err != nil {
		t.Fatal(err)
	}
	root := node.Content[0]
	deps := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	root.Content = append(root.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: "dependencies", Tag: "!!str"}, deps)
	mcpSeq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	deps.Content = append(deps.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: "mcp", Tag: "!!str"}, mcpSeq)
	mcpSeq.Content = append(mcpSeq.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: "filesystem", Tag: "!!str"})

	out, ok, err := PatchMappingPath(src, node, []string{"dependencies", "mcp"})
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}
	outStr := string(out)
	t.Logf("patched output:\n%s", outStr)
	if !strings.HasPrefix(outStr, "author: Madao\nname: demo\nversion: 1.0.0\n") {
		t.Errorf("existing top-level keys were reformatted, got:\n%s", outStr)
	}
	if !strings.Contains(outStr, "dependencies:") || !strings.Contains(outStr, "mcp:") || !strings.Contains(outStr, "filesystem") {
		t.Errorf("new dependencies.mcp block not found:\n%s", outStr)
	}
	if _, err := SafeLoad(out); err != nil {
		t.Fatalf("patched output does not parse: %v", err)
	}
}

// TestPatchMappingPath_CRLFDocument_KeepsCRLFThroughout guards against a
// real bug found during review: the yaml dumper (via renderKeyValue) always
// emits bare "\n" line endings. Spliced verbatim into a CRLF-authored
// apm.yml (common on Windows), that leaves the touched span using "\n"
// while every untouched line still uses "\r\n" -- a mixed-line-ending file.
func TestPatchMappingPath_CRLFDocument_KeepsCRLFThroughout(t *testing.T) {
	src := []byte("author: Madao\r\n" +
		"dependencies:\r\n" +
		"  apm:\r\n" +
		"    [\r\n" +
		"      { git: \"https://example.com/x\" },\r\n" +
		"    ]\r\n" +
		"  mcp:\r\n" +
		"    [\r\n" +
		"      { name: foo },\r\n" +
		"    ]\r\n" +
		"name: demo\r\n")
	node, err := SafeLoad(src)
	if err != nil {
		t.Fatal(err)
	}
	root := node.Content[0]
	deps := findMappingChildForTest(t, root, "dependencies")
	mcp := findMappingChildForTest(t, deps, "mcp")
	mcp.Content = append(mcp.Content, &yaml.Node{
		Kind: yaml.MappingNode, Tag: "!!map",
		Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Value: "name", Tag: "!!str"},
			{Kind: yaml.ScalarNode, Value: "bar", Tag: "!!str"},
		},
	})

	out, ok, err := PatchMappingPath(src, node, []string{"dependencies", "mcp"})
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}

	if strings.Contains(strings.ReplaceAll(string(out), "\r\n", ""), "\n") {
		t.Errorf("patched output has a bare LF not paired with CR (mixed line endings):\n%q", out)
	}
	if !strings.HasPrefix(string(out), "author: Madao\r\ndependencies:\r\n  apm:\r\n") {
		t.Errorf("untouched CRLF prefix was altered, got:\n%q", out)
	}
	if !strings.Contains(string(out), "bar") {
		t.Errorf("new mcp entry not found in output:\n%q", out)
	}
	if _, err := SafeLoad(out); err != nil {
		t.Fatalf("patched output does not parse: %v", err)
	}
}

// TestPatchMappingPath_PreservesCommentsNearMCPKey guards against a real
// bug found during review: a comment on the "mcp:" key's own line, or a
// standalone trailing comment between the mcp value and the next sibling
// key, both live on the key node (not the value node) per the yaml parser.
// A naive re-render of just the value silently dropped both.
func TestPatchMappingPath_PreservesCommentsNearMCPKey(t *testing.T) {
	src := []byte("author: Madao\n" +
		"dependencies:\n" +
		"  mcp: # servers list\n" +
		"    [\n" +
		"      { name: foo },\n" +
		"    ]\n" +
		"  # trailing note about the mcp block\n" +
		"name: demo\n")
	node, err := SafeLoad(src)
	if err != nil {
		t.Fatal(err)
	}
	root := node.Content[0]
	deps := findMappingChildForTest(t, root, "dependencies")
	mcp := findMappingChildForTest(t, deps, "mcp")
	mcp.Content = append(mcp.Content, &yaml.Node{
		Kind: yaml.MappingNode, Tag: "!!map",
		Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Value: "name", Tag: "!!str"},
			{Kind: yaml.ScalarNode, Value: "bar", Tag: "!!str"},
		},
	})

	out, ok, err := PatchMappingPath(src, node, []string{"dependencies", "mcp"})
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}
	outStr := string(out)
	t.Logf("patched output:\n%s", outStr)

	if !strings.Contains(outStr, "# servers list") {
		t.Errorf("line comment on the mcp: key was dropped, got:\n%s", outStr)
	}
	if !strings.Contains(outStr, "# trailing note about the mcp block") {
		t.Errorf("trailing comment before the next sibling key was dropped, got:\n%s", outStr)
	}
	if _, err := SafeLoad(out); err != nil {
		t.Fatalf("patched output does not parse: %v", err)
	}
}

func findMappingChildForTest(t *testing.T, m *yaml.Node, key string) *yaml.Node {
	t.Helper()
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	t.Fatalf("key %q not found", key)
	return nil
}
