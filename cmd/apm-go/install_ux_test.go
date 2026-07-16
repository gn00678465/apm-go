package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/resolver"
)

// captureInstallStdout redirects os.Stdout for the duration of fn and
// returns everything written to it, mirroring
// uninstall_local_survivor_test.go's captureUninstallStdout so install's ux
// output (written directly to os.Stdout, not cmd.OutOrStdout()) can be
// inspected.
func captureInstallStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	fn()

	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// TestRunInstall_SummaryDistinguishesNewFromExisting is the R9/R10c
// regression: the "Installed N dependencies" summary must mark an
// already-declared dependency as such (textually, not just by color -- P4-10
// requires the distinction to survive ANSI stripping) while leaving a
// genuinely new positional addition unmarked.
func TestRunInstall_SummaryDistinguishesNewFromExisting(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\ndependencies:\n  apm:\n    - org/monorepo/skills/a\n"), 0644)

	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &mockInstallLoader{}}

	out := captureInstallStdout(t, func() {
		if err := runInstall(deps, false, true, "claude", nil, []string{"org/monorepo/skills/b"}); err != nil {
			t.Fatalf("runInstall: %v", err)
		}
	})

	foundExisting := false
	foundNew := false
	for _, line := range strings.Split(out, "\n") {
		// Only the final "Installed N dependencies" bullet list carries "
		// (depth N)"; the earlier "Resolving <dep>..." spinner-update lines
		// also mention the dep name and would otherwise false-match.
		if !strings.Contains(line, "(depth") {
			continue
		}
		switch {
		case strings.Contains(line, "org/monorepo/skills/a"):
			foundExisting = true
			if !strings.Contains(line, "(already in apm.yml)") {
				t.Errorf("already-declared dep line must be marked (already in apm.yml), got: %q", line)
			}
		case strings.Contains(line, "org/monorepo/skills/b"):
			foundNew = true
			if strings.Contains(line, "(already in apm.yml)") {
				t.Errorf("newly-requested dep line must NOT be marked (already in apm.yml), got: %q", line)
			}
		}
	}
	if !foundExisting {
		t.Errorf("expected org/monorepo/skills/a in summary output, got: %s", out)
	}
	if !foundNew {
		t.Errorf("expected org/monorepo/skills/b in summary output, got: %s", out)
	}
}

// TestRunInstall_SummaryBareInstall_AllExistingDepsMuted covers a bare
// `apm-go install` (no positional packages): every resolved dep comes purely
// from an already-populated apm.yml, so requestedKeys is empty and every
// summary line should carry the "(already in apm.yml)" marker (R10c: install
// still resolves/deploys the full manifest -- this is presentation only).
func TestRunInstall_SummaryBareInstall_AllExistingDepsMuted(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\ndependencies:\n  apm:\n    - acme/foo\n"), 0644)

	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &mockInstallLoader{}}

	out := captureInstallStdout(t, func() {
		if err := runInstall(deps, false, true, "claude", nil, nil); err != nil {
			t.Fatalf("runInstall: %v", err)
		}
	})

	found := false
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "acme/foo") && strings.Contains(line, "(depth") {
			found = true
			if !strings.Contains(line, "(already in apm.yml)") {
				t.Errorf("bare install of an already-declared dep must be marked (already in apm.yml), got: %q", line)
			}
		}
	}
	if !found {
		t.Errorf("expected acme/foo in summary output, got: %s", out)
	}
}

func TestDepVersionLabel(t *testing.T) {
	tests := []struct {
		name string
		dep  resolver.ResolvedDep
		want string
	}{
		{
			name: "tag takes priority over ref and commit",
			dep:  resolver.ResolvedDep{ResolvedTag: "v1.0.0", ResolvedRef: "main", Commit: "e9fcdf9512345678"},
			want: "@v1.0.0",
		},
		{
			name: "ref used when tag absent",
			dep:  resolver.ResolvedDep{ResolvedRef: "main", Commit: "e9fcdf9512345678"},
			want: "@main",
		},
		{
			name: "short commit fallback when tag and ref both empty",
			dep:  resolver.ResolvedDep{Commit: "e9fcdf9512345678"},
			want: "@e9fcdf95",
		},
		{
			name: "commit shorter than 8 chars is used as-is, no panic",
			dep:  resolver.ResolvedDep{Commit: "abc"},
			want: "@abc",
		},
		{
			name: "no tag, ref, or commit yields no suffix",
			dep:  resolver.ResolvedDep{},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := depVersionLabel(tt.dep); got != tt.want {
				t.Errorf("depVersionLabel(%+v) = %q, want %q", tt.dep, got, tt.want)
			}
		})
	}
}

// TestDeployedFilesTree_AggregatesSkillsByKindAndRoot is the R10b regression:
// 22 skills each mirrored to both the canonical .agents/skills/ root and the
// Claude-specific .claude/skills/ root must collapse into a single "skill"
// tree entry naming both roots, not one line per skill subdirectory.
func TestDeployedFilesTree_AggregatesSkillsByKindAndRoot(t *testing.T) {
	var files []string
	for i := 0; i < 22; i++ {
		name := fmt.Sprintf("skill-%02d", i)
		files = append(files,
			".agents/skills/"+name+"/SKILL.md",
			".claude/skills/"+name+"/SKILL.md",
		)
	}

	node := deployedFilesTree("owner/pkg", files)

	if node.Text != "owner/pkg" {
		t.Fatalf("root label = %q, want %q", node.Text, "owner/pkg")
	}
	if len(node.Children) != 1 {
		t.Fatalf("expected exactly 1 aggregated child, got %d: %+v", len(node.Children), node.Children)
	}
	want := "22 skills -> .agents/skills/, .claude/skills/"
	if node.Children[0].Text != want {
		t.Errorf("child = %q, want %q", node.Children[0].Text, want)
	}
}

// TestDeployedFilesTree_MixedKindsProduceOneLineEach covers multiple
// primitive kinds in one dep's file list, verifying each kind aggregates to
// exactly one line and singular/plural wording still depends on the
// deduplicated primitive count (not the raw file count).
func TestDeployedFilesTree_MixedKindsProduceOneLineEach(t *testing.T) {
	files := []string{
		".claude/agents/reviewer.md",
		".github/agents/reviewer.agent.md",
		".claude/commands/deploy.md",
		".github/instructions/style.instructions.md",
	}

	node := deployedFilesTree("owner/pkg", files)

	byText := make(map[string]bool, len(node.Children))
	for _, c := range node.Children {
		byText[c.Text] = true
	}

	// "reviewer" was mirrored to two roots under two different filename
	// conventions (.md vs .agent.md) -- it must still count as ONE agent.
	if !byText["1 agent -> .claude/agents/, .github/agents/"] {
		t.Errorf("expected deduplicated single-agent line, got children: %+v", node.Children)
	}
	if !byText["1 command -> .claude/commands/"] {
		t.Errorf("expected single-command line, got children: %+v", node.Children)
	}
	if !byText["1 instruction -> .github/instructions/"] {
		t.Errorf("expected single-instruction line, got children: %+v", node.Children)
	}
	if len(node.Children) != 3 {
		t.Errorf("expected exactly 3 aggregated lines (agent/command/instruction), got %d: %+v", len(node.Children), node.Children)
	}
}

// TestDeployAndFinalize_LocalNodeLabel is the R14 regression: the deploy
// tree's local-primitives bucket must be labeled with an unambiguous
// "<project root> (local)" instead of a bare "(local)".
func TestRunInstall_LocalDeployTreeLabel(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\n"), 0644)
	os.MkdirAll(dir+"/.apm/agents", 0755)
	os.WriteFile(dir+"/.apm/agents/demo.md", []byte("# demo agent"), 0644)

	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &mockInstallLoader{}}

	out := captureInstallStdout(t, func() {
		if err := runInstall(deps, false, true, "claude", nil, nil); err != nil {
			t.Fatalf("runInstall: %v", err)
		}
	})

	if !strings.Contains(out, "<project root> (local)") {
		t.Errorf("expected deploy tree to label the local bucket \"<project root> (local)\", got: %s", out)
	}
	if strings.Contains(out, "  (local)\n") || strings.Contains(out, "● (local)") {
		t.Errorf("bare \"(local)\" label should have been replaced, got: %s", out)
	}
}
