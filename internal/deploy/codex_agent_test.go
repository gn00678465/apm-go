package deploy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

// parseCodexAgentTOML parses raw TOML bytes into a generic map, asserting
// the parse itself succeeds -- callers assert further on the returned map.
func parseCodexAgentTOML(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := toml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("output is not valid TOML: %v\n%s", err, data)
	}
	return doc
}

func assertExactCodexAgentKeys(t *testing.T, doc map[string]any) {
	t.Helper()
	want := map[string]bool{"name": true, "description": true, "developer_instructions": true}
	if len(doc) != len(want) {
		t.Fatalf("expected exactly 3 keys %v, got %v", want, doc)
	}
	for k := range doc {
		if !want[k] {
			t.Errorf("unexpected key in TOML output: %s", k)
		}
	}
}

// TestCodexAgentTransform covers every boundary of the Python oracle
// _write_codex_agent (agent_integrator.py:302-335): symlink rejection, name
// fallback, frontmatter override, malformed-YAML silence, description
// default, body trimming and exact key set, and the no-frontmatter case.
func TestCodexAgentTransform(t *testing.T) {
	t.Run("rejects symlink", func(t *testing.T) {
		dir := t.TempDir()
		real := filepath.Join(dir, "real.agent.md")
		if err := os.WriteFile(real, []byte("# real"), 0644); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(dir, "linked.agent.md")
		if err := os.Symlink(real, link); err != nil {
			t.Skipf("symlink creation not permitted in this environment: %v", err)
		}

		if _, err := transformCodexAgent(link); err == nil {
			t.Fatal("expected error for symlink source, got nil")
		}
	})

	t.Run("name fallback strips agent", func(t *testing.T) {
		dir := t.TempDir()
		src := filepath.Join(dir, "accessibility-runtime-tester.agent.md")
		if err := os.WriteFile(src, []byte("plain body, no frontmatter"), 0644); err != nil {
			t.Fatal(err)
		}

		doc, err := transformCodexAgent(src)
		if err != nil {
			t.Fatalf("transformCodexAgent: %v", err)
		}
		if doc.Name != "accessibility-runtime-tester" {
			t.Errorf("Name = %q, want %q (no trailing .agent)", doc.Name, "accessibility-runtime-tester")
		}
	})

	t.Run("frontmatter overrides", func(t *testing.T) {
		dir := t.TempDir()
		src := filepath.Join(dir, "fm.agent.md")
		content := "---\n" +
			"name: Frontmatter Name\n" +
			"description: Frontmatter Description\n" +
			"model: ignored\n" +
			"tools: [ignored]\n" +
			"---\n\n" +
			"  BODY EDGE  \n"
		if err := os.WriteFile(src, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		doc, err := transformCodexAgent(src)
		if err != nil {
			t.Fatalf("transformCodexAgent: %v", err)
		}
		if doc.Name != "Frontmatter Name" {
			t.Errorf("Name = %q, want %q", doc.Name, "Frontmatter Name")
		}
		if doc.Description != "Frontmatter Description" {
			t.Errorf("Description = %q, want %q", doc.Description, "Frontmatter Description")
		}
		if doc.DeveloperInstructions != "BODY EDGE" {
			t.Errorf("DeveloperInstructions = %q, want %q", doc.DeveloperInstructions, "BODY EDGE")
		}
		if strings.Contains(doc.DeveloperInstructions, "model") || strings.Contains(doc.DeveloperInstructions, "---") {
			t.Errorf("frontmatter leaked into body: %q", doc.DeveloperInstructions)
		}

		data, err := toml.Marshal(doc)
		if err != nil {
			t.Fatalf("toml.Marshal: %v", err)
		}
		parsed := parseCodexAgentTOML(t, data)
		assertExactCodexAgentKeys(t, parsed)
		if parsed["model"] != nil || parsed["tools"] != nil {
			t.Errorf("extra frontmatter keys leaked into TOML output: %v", parsed)
		}
	})

	t.Run("malformed yaml is silent", func(t *testing.T) {
		dir := t.TempDir()
		src := filepath.Join(dir, "bad-yaml.agent.md")
		content := "---\n" +
			"name: \"unterminated\n" +
			"---\n\n" +
			"bad body\n"
		if err := os.WriteFile(src, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		doc, err := transformCodexAgent(src)
		if err != nil {
			t.Fatalf("transformCodexAgent must not error on malformed frontmatter YAML: %v", err)
		}
		if doc.Name != "bad-yaml" {
			t.Errorf("Name = %q, want fallback %q", doc.Name, "bad-yaml")
		}
		if doc.Description != "" {
			t.Errorf("Description = %q, want empty default", doc.Description)
		}
		if doc.DeveloperInstructions != "bad body" {
			t.Errorf("DeveloperInstructions = %q, want %q (frontmatter still cut)", doc.DeveloperInstructions, "bad body")
		}
	})

	t.Run("description defaults empty", func(t *testing.T) {
		dir := t.TempDir()
		tests := []struct {
			name, content string
		}{
			{"no-frontmatter", "just plain body\n"},
			{"empty-frontmatter", "---\nname: x\n---\n\nbody\n"},
			{"malformed-frontmatter", "---\nname: \"unterminated\n---\n\nbody\n"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				src := filepath.Join(dir, tt.name+".agent.md")
				if err := os.WriteFile(src, []byte(tt.content), 0644); err != nil {
					t.Fatal(err)
				}
				doc, err := transformCodexAgent(src)
				if err != nil {
					t.Fatalf("transformCodexAgent: %v", err)
				}
				if doc.Description != "" {
					t.Errorf("Description = %q, want empty string default", doc.Description)
				}
				data, err := toml.Marshal(doc)
				if err != nil {
					t.Fatalf("toml.Marshal: %v", err)
				}
				parsed := parseCodexAgentTOML(t, data)
				desc, ok := parsed["description"]
				if !ok {
					t.Fatal("description key missing from parsed TOML")
				}
				if desc != "" {
					t.Errorf("parsed description = %v, want empty string", desc)
				}
			})
		}
	})

	t.Run("body is trimmed and keys exact", func(t *testing.T) {
		dir := t.TempDir()
		src := filepath.Join(dir, "spacing.agent.md")
		content := "\n\n  Line one.\n\n  Line two.  \n\n\n"
		if err := os.WriteFile(src, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		doc, err := transformCodexAgent(src)
		if err != nil {
			t.Fatalf("transformCodexAgent: %v", err)
		}
		want := "Line one.\n\n  Line two."
		if doc.DeveloperInstructions != want {
			t.Errorf("DeveloperInstructions = %q, want %q (only outer whitespace trimmed)", doc.DeveloperInstructions, want)
		}

		data, err := toml.Marshal(doc)
		if err != nil {
			t.Fatalf("toml.Marshal: %v", err)
		}
		assertExactCodexAgentKeys(t, parseCodexAgentTOML(t, data))
	})

	t.Run("no frontmatter uses full body", func(t *testing.T) {
		dir := t.TempDir()
		src := filepath.Join(dir, "plain.agent.md")
		content := "# No Frontmatter\n\nplain body\n"
		if err := os.WriteFile(src, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		doc, err := transformCodexAgent(src)
		if err != nil {
			t.Fatalf("transformCodexAgent: %v", err)
		}
		want := "# No Frontmatter\n\nplain body"
		if doc.DeveloperInstructions != want {
			t.Errorf("DeveloperInstructions = %q, want %q", doc.DeveloperInstructions, want)
		}

		// Negative fixture: a leading blank line before "---...---" means the
		// frontmatter regex (anchored at file start) must NOT match -- the
		// whole thing stays body.
		srcNeg := filepath.Join(dir, "leading-blank.agent.md")
		negContent := "\n---\ntitle: X\n---\n\nbody\n"
		if err := os.WriteFile(srcNeg, []byte(negContent), 0644); err != nil {
			t.Fatal(err)
		}
		negDoc, err := transformCodexAgent(srcNeg)
		if err != nil {
			t.Fatalf("transformCodexAgent: %v", err)
		}
		wantNeg := "---\ntitle: X\n---\n\nbody"
		if negDoc.DeveloperInstructions != wantNeg {
			t.Errorf("DeveloperInstructions = %q, want %q (leading blank line must prevent frontmatter match)", negDoc.DeveloperInstructions, wantNeg)
		}
		if negDoc.Name != "leading-blank" || negDoc.Description != "" {
			t.Errorf("leading-blank fixture must fall back to defaults: name=%q description=%q", negDoc.Name, negDoc.Description)
		}
	})
}

// TestDeployCodexAgentTOML asserts the deployment layer: only the expected
// path is written, and its content parses as TOML with exactly the three
// documented keys (prd.md acceptance criteria).
func TestDeployCodexAgentTOML(t *testing.T) {
	dir := t.TempDir()
	mkFile(t, dir, ".apm/agents/helper.agent.md", "---\nname: Helper\ndescription: A helper agent\n---\n\nDo the thing.\n")

	prims := CollectLocalPrimitives(dir)
	if len(prims) != 1 || prims[0].Type != TypeAgents {
		t.Fatalf("expected exactly 1 agent primitive, got %+v", prims)
	}

	files, err := (&codexAdapter{}).DeployPrimitive(prims[0], dir)
	if err != nil {
		t.Fatalf("DeployPrimitive: %v", err)
	}
	if len(files) != 1 || files[0] != ".codex/agents/helper.toml" {
		t.Fatalf("expected [.codex/agents/helper.toml], got %v", files)
	}

	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(files[0])))
	if err != nil {
		t.Fatalf("read deployed file: %v", err)
	}
	doc := parseCodexAgentTOML(t, data)
	assertExactCodexAgentKeys(t, doc)
	if doc["name"] != "Helper" {
		t.Errorf("name = %v, want %q", doc["name"], "Helper")
	}
	if doc["description"] != "A helper agent" {
		t.Errorf("description = %v, want %q", doc["description"], "A helper agent")
	}
	if doc["developer_instructions"] != "Do the thing." {
		t.Errorf("developer_instructions = %v, want %q", doc["developer_instructions"], "Do the thing.")
	}
}

// TestOtherTargetsAgentCopyUnchanged is the parity guard for prd.md's
// non-goal: claude/opencode/copilot agents deployment must stay byte-copy
// while codex converts to TOML -- from the SAME source bytes.
func TestOtherTargetsAgentCopyUnchanged(t *testing.T) {
	dir := t.TempDir()
	const source = "---\nname: Frontmatter Name\ndescription: Frontmatter Description\n---\n\nbody\n"
	mkFile(t, dir, ".apm/agents/fm.agent.md", source)

	prims := CollectLocalPrimitives(dir)
	if len(prims) != 1 {
		t.Fatalf("expected exactly 1 agent primitive, got %+v", prims)
	}
	p := prims[0]

	cases := []struct {
		adapter TargetAdapter
		relPath string
	}{
		{&claudeAdapter{}, ".claude/agents/fm.md"},
		{&opencodeAdapter{}, ".opencode/agents/fm.md"},
		{&copilotAdapter{}, ".github/agents/fm.agent.md"},
	}
	for _, tc := range cases {
		t.Run(tc.adapter.Name(), func(t *testing.T) {
			if _, err := tc.adapter.DeployPrimitive(p, dir); err != nil {
				t.Fatalf("deploy: %v", err)
			}
			if got := readDeployed(t, dir, tc.relPath); got != source {
				t.Errorf("output must be byte-identical to source\n got: %q\nwant: %q", got, source)
			}
		})
	}

	if _, err := (&codexAdapter{}).DeployPrimitive(p, dir); err != nil {
		t.Fatalf("codex deploy: %v", err)
	}
	if got := readDeployed(t, dir, ".codex/agents/fm.toml"); got == source {
		t.Error("codex output must NOT be byte-identical to source (must be transformed TOML)")
	}
}
