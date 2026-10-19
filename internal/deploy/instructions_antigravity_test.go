package deploy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/manifest"
)

// agRuleCases are the instructions of skex/scripts/mkag.sh and the rules
// Oracle 0.33.0 wrote for them (skex/ag1/or/proj/.agents/rules/).
var agRuleCases = []struct{ name, src, want string }{
	{"brace", "---\napplyTo: \"**/*.{ts,tsx}, docs/**\"\n---\n# brace\n", "---\ntrigger: glob\nglobs:\n  - \"**/*.{ts,tsx}\"\n  - \"docs/**\"\n---\n\n# brace\n"},
	{"crlf", "---\r\napplyTo: \"x/**\"\r\n---\r\n\r\n# crlf\r\nline\r\n", "---\ntrigger: glob\nglobs: \"x/**\"\n---\n\n# crlf\nline\n"},
	{"desc", "---\ndescription: \"Python style\"\napplyTo: \"**/*.py\"\n---\n\n# desc\n", "---\ntrigger: glob\nglobs: \"**/*.py\"\n---\n\n# desc\n"},
	{"descnoapply", "---\ndescription: only desc\n---\n\n\n# descnoapply\n", "# descnoapply\n"},
	{"emptyapply", "---\napplyTo: \"\"\n---\nempty\n", "empty\n"},
	{"esc", "---\napplyTo: \"a\\\\,b/**, c\\\"d\"\n---\nesc\n", "---\ntrigger: glob\nglobs:\n  - \"a,b/**\"\n  - \"c\\\"d\"\n---\n\nesc\n"},
	{"leadcr", "---\napplyTo: \"q/**\"\n---\n\r\n\r\nlead\n", "---\ntrigger: glob\nglobs: \"q/**\"\n---\n\nlead\n"},
	{"list", "---\napplyTo:\n  - \"a/**\"\n  - b/*.md\n---\nlist\n", "---\ntrigger: glob\nglobs:\n  - \"a/**\"\n  - \"b/*.md\"\n---\n\nlist\n"},
	{"multi", "---\napplyTo: \"src/**/*.py, tests/**/*.py\"\n---\n# multi\n", "---\ntrigger: glob\nglobs:\n  - \"src/**/*.py\"\n  - \"tests/**/*.py\"\n---\n\n# multi\n"},
	{"newline", "---\napplyTo: |\n  m/**,\n  n/**\n---\nnl\n", "---\ntrigger: glob\nglobs:\n  - \"m/**\"\n  - \"n/**\"\n---\n\nnl\n"},
	{"none", "# none\nno frontmatter\n", "# none\nno frontmatter\n"},
	{"single", "---\napplyTo: \"src/**/*.py\"\n---\n# single\nbody\n", "---\ntrigger: glob\nglobs: \"src/**/*.py\"\n---\n\n# single\nbody\n"},
}

// A dependency's instructions and the project's own are written as the
// Oracle writes an antigravity rule (skex/ag1), each in the place apm-go
// gives it: the project's in .agents/rules/, a dependency's in its bundle.
func TestRun_AntigravityRulesAsTheOracleWritesThem(t *testing.T) {
	dir := t.TempDir()
	depDir := filepath.Join(dir, "apm_modules", "acme", "tool")
	for _, c := range agRuleCases {
		mkFile(t, depDir, ".apm/instructions/"+c.name+".instructions.md", c.src)
	}
	mkFile(t, dir, ".apm/instructions/own.instructions.md", "---\napplyTo: \"**/*.py\"\ntitle: x\n---\nown\n")
	mkFile(t, dir, ".apm/instructions/ownplain.instructions.md", "own plain\n")
	m := &manifest.Manifest{Name: "proj", Version: "1.0.0", ParsedDeps: []*manifest.DependencyReference{
		{RepoURL: "acme/tool", Owner: "acme", Repo: "tool", Source: "git"},
	}}

	if _, err := Run([]string{"antigravity"}, dir, m, nil, nil, "", false, &Managed{Root: dir}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, c := range agRuleCases {
		if got := readDeployed(t, dir, ".agents/plugins/tool/rules/"+c.name+".md"); got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}
	if got := readDeployed(t, dir, ".agents/rules/own.md"); got != "---\ntrigger: glob\nglobs: \"**/*.py\"\n---\n\nown\n" {
		t.Errorf("own.md = %q", got)
	}
	if got := readDeployed(t, dir, ".agents/rules/ownplain.md"); got != "own plain\n" {
		t.Errorf("ownplain.md = %q", got)
	}
}

// The Oracle writes an antigravity rule over a file the user put there
// (skex/ag4), and adopts one that holds the rule (skex/ag2).
func TestRun_AntigravityRuleWritesOverAUserFileAndAdoptsItsOwn(t *testing.T) {
	dir := t.TempDir()
	mkFile(t, dir, ".apm/instructions/own.instructions.md", "---\napplyTo: \"**/*.py\"\ntitle: x\n---\nown\n")
	mkFile(t, dir, ".agents/rules/own.md", "user file\n")
	m := &manifest.Manifest{Name: "proj", Version: "1.0.0"}

	result, err := Run([]string{"antigravity"}, dir, m, nil, nil, "", false, &Managed{Root: dir})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	const want = "---\ntrigger: glob\nglobs: \"**/*.py\"\n---\n\nown\n"
	if got := readDeployed(t, dir, ".agents/rules/own.md"); got != want {
		t.Errorf("own.md = %q, want the rule over the user's file", got)
	}
	if len(result.Unmanaged[""]) != 0 || result.Adopted[".agents/rules/own.md"] {
		t.Errorf("Unmanaged = %v, Adopted = %v, want the rule integrated", result.Unmanaged, result.Adopted)
	}

	result, err = Run([]string{"antigravity"}, dir, m, nil, nil, "", false, &Managed{Root: dir})
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if !result.Adopted[".agents/rules/own.md"] {
		t.Errorf("Adopted = %v, want .agents/rules/own.md adopted on the second run", result.Adopted)
	}
}

// An antigravity rule whose frontmatter is not YAML fails alone, as a claude
// rule does (issue #126 comment 6267289737, item 3.1; Oracle 0.33.0 ends the
// install with exit 1, skex/ag5): the old rule stays, the bucket is not
// cleaned, and the other instructions deploy.
func TestRun_AntigravityRuleWithInvalidFrontmatterFails(t *testing.T) {
	t.Chdir(t.TempDir())
	mkFile(t, ".", ".apm/instructions/bad.instructions.md", "---\napplyTo: \"a/**\"\nfoo: [unclosed\n---\nbad\n")
	mkFile(t, ".", ".apm/instructions/good.instructions.md", "---\napplyTo: \"b/**\"\n---\ngood\n")
	mkFile(t, ".", ".agents/rules/bad.md", "old rule\n")
	m := &manifest.Manifest{Name: "proj", Version: "1.0.0"}

	result, err := Run([]string{"antigravity"}, ".", m, nil, nil, "", false, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := "deploy bad to antigravity failed: .apm/instructions/bad.instructions.md: frontmatter: YAML parse error: "
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
	if got := readDeployed(t, ".", ".agents/rules/bad.md"); got != "old rule\n" {
		t.Errorf("bad.md = %q, want the old rule kept", got)
	}
	if got := readDeployed(t, ".", ".agents/rules/good.md"); got != "---\ntrigger: glob\nglobs: \"b/**\"\n---\n\ngood\n" {
		t.Errorf("good.md = %q", got)
	}
}

// A --global deploy links an antigravity instruction as before, since the
// Oracle deploys none at user scope (integration/targets.py:770-773).
func TestRun_GlobalAntigravityInstructionStaysALink(t *testing.T) {
	defer SetSymlinkProbeForTest(func() bool { return true })()
	dir := t.TempDir()
	mkFile(t, dir, ".apm/instructions/own.instructions.md", "---\napplyTo: \"**/*.py\"\n---\nown\n")
	m := &manifest.Manifest{Name: "proj", Version: "1.0.0"}

	if _, err := Run([]string{"antigravity"}, dir, m, nil, nil, "", true, nil); err != nil {
		t.Fatalf("Run: %v", err)
	}
	info, err := os.Lstat(filepath.Join(dir, ".agents", "rules", "own.md"))
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("own.md: %v %v, want a symlink to the source", info, err)
	}
}
