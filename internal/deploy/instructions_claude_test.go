package deploy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/manifest"
)

// applyToSource is a representative .instructions.md with an applyTo
// frontmatter, shared by the transform/byte-copy deployment tests below so
// they provably exercise the SAME source bytes.
const applyToSource = "---\napplyTo: \"**/*.go\"\n---\n\n# Go rules\n\nUse gofmt.\n"

// Oracle: Python instruction_integrator.py _convert_to_claude_rules
// (applyTo -> paths: YAML list, body preserved).
const applyToClaudeWant = "---\npaths:\n  - \"**/*.go\"\n---\n\n# Go rules\n\nUse gofmt.\n"

func readDeployed(t *testing.T, dir, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read deployed file %s: %v", rel, err)
	}
	return string(data)
}

func collectSoleInstruction(t *testing.T, dir string) Primitive {
	t.Helper()
	prims := CollectLocalPrimitives(dir)
	if len(prims) != 1 || prims[0].Type != TypeInstructions {
		t.Fatalf("expected exactly 1 instructions primitive, got %v", prims)
	}
	return prims[0]
}

// PRD req #1: deploying an applyTo-bearing instruction to claude must
// transform the frontmatter to paths:, not byte-copy it (Python
// _convert_to_claude_rules parity).
func TestDeployClaude_InstructionsApplyToTransformed(t *testing.T) {
	dir := t.TempDir()
	mkFile(t, dir, ".apm/instructions/demo.instructions.md", applyToSource)
	p := collectSoleInstruction(t, dir)

	files, err := (&claudeAdapter{}).DeployPrimitive(p, dir)
	if err != nil {
		t.Fatalf("deploy: %v", err)
	}
	if len(files) != 1 || files[0] != ".claude/rules/demo.md" {
		t.Fatalf("expected [.claude/rules/demo.md], got %v", files)
	}

	got := readDeployed(t, dir, ".claude/rules/demo.md")
	if got != applyToClaudeWant {
		t.Errorf("claude rules output not transformed\n got: %q\nwant: %q", got, applyToClaudeWant)
	}
}

// PRD req #1 scope guard: the SAME source stays byte-identical for copilot
// (applyTo is copilot-native) and antigravity (documented deviation:
// byte-copy, 07-05-antigravity-research).
func TestDeployOtherTargets_InstructionsStayByteIdentical(t *testing.T) {
	dir := t.TempDir()
	mkFile(t, dir, ".apm/instructions/demo.instructions.md", applyToSource)
	p := collectSoleInstruction(t, dir)

	if _, err := (&copilotAdapter{}).DeployPrimitive(p, dir); err != nil {
		t.Fatalf("copilot deploy: %v", err)
	}
	if got := readDeployed(t, dir, ".github/instructions/demo.instructions.md"); got != applyToSource {
		t.Errorf("copilot output must be byte-identical to source\n got: %q\nwant: %q", got, applyToSource)
	}

	if _, err := (&antigravityAdapter{}).DeployPrimitive(p, dir); err != nil {
		t.Fatalf("antigravity deploy: %v", err)
	}
	if got := readDeployed(t, dir, ".agents/rules/demo.md"); got != applyToSource {
		t.Errorf("antigravity output must be byte-identical to source\n got: %q\nwant: %q", got, applyToSource)
	}
}

// TestConvertToClaudeRules covers the frontmatter split and the body of the
// Python oracle _convert_to_claude_rules (instruction_integrator.py:784-805);
// applyToOracleCases covers how applyTo is read.
func TestConvertToClaudeRules(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{
			"single glob",
			"---\napplyTo: \"**/*.go\"\n---\nbody\n",
			"---\npaths:\n  - \"**/*.go\"\n---\n\nbody\n",
		},
		{
			"single-quoted value",
			"---\napplyTo: '**/*.ts'\n---\nbody\n",
			"---\npaths:\n  - \"**/*.ts\"\n---\n\nbody\n",
		},
		{
			"no frontmatter passthrough",
			"# Title\n\nbody\n",
			"# Title\n\nbody\n",
		},
		{
			"no frontmatter leading blank lines stripped",
			"\n\n# Title\n",
			"# Title\n",
		},
		{
			"unclosed frontmatter treated as plain body",
			"---\napplyTo: \"**/*.go\"\nbody\n",
			"---\napplyTo: \"**/*.go\"\nbody\n",
		},
		{
			"crlf source: LF frontmatter, body bytes preserved",
			"---\r\napplyTo: \"**/*.go\"\r\n---\r\n\r\nbody\r\n",
			"---\npaths:\n  - \"**/*.go\"\n---\n\nbody\r\n",
		},
		{
			"empty content",
			"",
			"",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := convertToClaudeRules([]byte(tt.in))
			if err != nil {
				t.Fatalf("convertToClaudeRules(%q): %v", tt.in, err)
			}
			if string(got) != tt.want {
				t.Errorf("convertToClaudeRules(%q)\n got: %q\nwant: %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestDeployClaudeInstructions_MissingSourceErrors(t *testing.T) {
	dir := t.TempDir()
	p := Primitive{
		Name:    "ghost",
		Type:    TypeInstructions,
		Source:  "local",
		SrcPath: filepath.Join(dir, ".apm", "instructions", "ghost.instructions.md"),
	}
	if _, err := (&claudeAdapter{}).DeployPrimitive(p, dir); err == nil {
		t.Fatal("expected error for missing source file")
	}
}

// PRD req #3: only *.instructions.md is collected from .apm/instructions/
// (Python find_instruction_files parity); plain .md is ignored.
func TestCollectInstructions_OnlyInstructionsMDCollected(t *testing.T) {
	dir := t.TempDir()
	mkFile(t, dir, ".apm/instructions/keep.instructions.md", "# keep")
	mkFile(t, dir, ".apm/instructions/plain.md", "# plain")

	inst := groupByType(CollectLocalPrimitives(dir))[TypeInstructions]
	if len(inst) != 1 || inst[0].Name != "keep" {
		t.Errorf("expected only [keep] collected, got %v", inst)
	}
}

// An instruction whose frontmatter is not YAML (an unquoted ** is an alias)
// fails alone: the rule is not written, the old one stays, the bucket is not
// cleaned, and the other instructions deploy.
func TestRun_ClaudeInstructionWithInvalidFrontmatterFails(t *testing.T) {
	dir := t.TempDir()
	mkFile(t, dir, ".apm/instructions/bad.instructions.md", "---\napplyTo: **/*.go\n---\nbad\n")
	mkFile(t, dir, ".apm/instructions/good.instructions.md", "---\napplyTo:\n  - \"a/**\"\n---\ngood\n")
	mkFile(t, dir, ".claude/rules/bad.md", "old rule\n")
	m := &manifest.Manifest{Name: "test", Version: "1.0.0"}

	result, err := Run([]string{"claude"}, dir, m, nil, nil, "", false)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	src := filepath.Join(dir, ".apm", "instructions", "bad.instructions.md")
	want := "deploy bad to claude failed: " + src + ": frontmatter: YAML parse error: "
	found := false
	for _, d := range result.Diags {
		found = found || strings.HasPrefix(d, want)
	}
	if !found {
		t.Errorf("Diags = %q, want one starting with %q", result.Diags, want)
	}
	if !result.FailedBuckets[""] {
		t.Errorf("FailedBuckets = %v, want the local bucket", result.FailedBuckets)
	}
	if got := readDeployed(t, dir, ".claude/rules/bad.md"); got != "old rule\n" {
		t.Errorf(".claude/rules/bad.md = %q, want the old rule kept", got)
	}
	if got := readDeployed(t, dir, ".claude/rules/good.md"); got != "---\npaths:\n  - \"a/**\"\n---\n\ngood\n" {
		t.Errorf(".claude/rules/good.md = %q", got)
	}
}
