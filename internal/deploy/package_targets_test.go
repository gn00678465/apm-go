package deploy

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/resolver"
)

// Issue #126 item 25. G2-or: o/b declares o/a, whose apm.yml has
// targets: [claude]; Oracle 0.33.0 deployed o/b to both targets and o/a to
// claude alone. G3-or: o/d, whose entry allows claude, declares copilot.
func TestRun_PackageTargets_TransitiveAndSkipped(t *testing.T) {
	dir := t.TempDir()
	for _, p := range []struct{ name, yml string }{
		{"a", "name: a\nversion: 1.0.0\ntargets: [claude]\n"},
		{"b", "name: b\nversion: 1.0.0\n"},
		{"d", "name: d\nversion: 1.0.0\ntargets: [copilot]\n"},
	} {
		module := filepath.Join(dir, "apm_modules", "o", p.name)
		mkFile(t, module, ".apm/agents/"+p.name+"agent.agent.md", "---\nname: "+p.name+"agent\ndescription: d\n---\nx\n")
		mkFile(t, module, "apm.yml", p.yml)
	}
	m := &manifest.Manifest{Name: "proj", Version: "1.0.0", ParsedDeps: []*manifest.DependencyReference{
		{RepoURL: "o/b", Owner: "o", Repo: "b", Source: "git"},
		{RepoURL: "o/d", Owner: "o", Repo: "d", Source: "git", TargetSubset: []string{"claude"}},
	}}
	resolved := &resolver.ResolutionResult{Deps: []resolver.ResolvedDep{
		{Key: "o/b", RepoURL: "o/b", Depth: 1},
		{Key: "o/d", RepoURL: "o/d", Depth: 1, TargetSubset: []string{"claude"}},
		{Key: "o/a", RepoURL: "o/a", Depth: 2},
	}}

	r, err := Run([]string{"claude", "copilot"}, dir, m, resolved, nil, "", false, nil)
	if err != nil {
		t.Fatal(err)
	}

	if got := depFiles(r, "o/b"); !slices.Equal(got, []string{".claude/agents/bagent.md", ".github/agents/bagent.agent.md"}) {
		t.Errorf("o/b files = %q", got)
	}
	if got := depFiles(r, "o/a"); !slices.Equal(got, []string{".claude/agents/aagent.md"}) {
		t.Errorf("o/a files = %q", got)
	}
	if got := depFiles(r, "o/d"); got != nil {
		t.Errorf("o/d files = %q, want none", got)
	}
	want := []SkippedDep{{Key: "o/d", Targets: []string{"copilot"}, ByPackage: true}}
	if !slices.EqualFunc(r.SkippedDeps, want, func(a, b SkippedDep) bool {
		return a.Key == b.Key && a.ByPackage == b.ByPackage && slices.Equal(a.Targets, b.Targets)
	}) {
		t.Errorf("SkippedDeps = %+v, want %+v", r.SkippedDeps, want)
	}
}
