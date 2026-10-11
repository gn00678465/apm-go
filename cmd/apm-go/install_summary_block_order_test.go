package main

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Issue #47: the summary prints one block per dependency and one for local
// content. Their order is the resolver's order with local content last.

const summaryOrderRuns = 20

var summaryOrderKeyHash = regexp.MustCompile(`-[0-9a-f]{8}$`)

// summaryBlockLabels returns the label of each summary block in out, in
// printed order, without the hash a local-path dependency key ends with.
func summaryBlockLabels(out string) []string {
	var labels []string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "_local/") || strings.HasPrefix(line, "<project root>") {
			labels = append(labels, summaryOrderKeyHash.ReplaceAllString(line, ""))
		}
	}
	return labels
}

func assertSummaryBlockOrder(t *testing.T, run int, out string, want []string) {
	t.Helper()
	if got := summaryBlockLabels(out); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("run %d: summary blocks = %q, want %q\n%s", run, got, want, out)
	}
}

func summaryOrderSkill(name string) string {
	return "---\nname: " + name + "\ndescription: d\n---\nskill\n"
}

func TestInstallGlobal_SummaryBlockOrder_DependencyThenLocalContent(t *testing.T) {
	home, src := globalStaleScope(t, map[string]string{
		".apm/skills/s1/SKILL.md": summaryOrderSkill("s1"),
	})
	writeGlobalStaleFile(t, filepath.Join(home, ".apm", ".apm", "skills", "L1", "SKILL.md"), summaryOrderSkill("L1"))
	want := []string{"_local/dep", "<project root> (local)"}

	assertSummaryBlockOrder(t, 0, globalStaleInstall(t, "claude", src), want)
	for i := 1; i <= summaryOrderRuns; i++ {
		assertSummaryBlockOrder(t, i, globalStaleInstall(t, "claude"), want)
	}
}

func TestInstallGlobal_SummaryBlockOrder_DependenciesInDeclaredOrder(t *testing.T) {
	home, _ := globalStaleScope(t, nil)
	writeGlobalStaleFile(t, filepath.Join(home, ".apm", ".apm", "skills", "L1", "SKILL.md"), summaryOrderSkill("L1"))
	vendor := t.TempDir()
	manifestYAML := "name: global\nversion: 0.0.0\ndependencies:\n  apm:\n"
	for _, name := range []string{"zeta", "alpha", "mid"} {
		dir := filepath.Join(vendor, name)
		writeGlobalStaleFile(t, filepath.Join(dir, "apm.yml"), "name: "+name+"\nversion: 1.0.0\n")
		writeGlobalStaleFile(t, filepath.Join(dir, ".apm", "skills", "s-"+name, "SKILL.md"), summaryOrderSkill("s-"+name))
		manifestYAML += "    - " + filepath.ToSlash(dir) + "\n"
	}
	writeGlobalStaleFile(t, filepath.Join(home, ".apm", "apm.yml"), manifestYAML)
	want := []string{"_local/zeta", "_local/alpha", "_local/mid", "<project root> (local)"}

	for i := 0; i <= summaryOrderRuns; i++ {
		assertSummaryBlockOrder(t, i, globalStaleInstall(t, "claude"), want)
	}
}

func TestInstall_SummaryBlockOrder_DependencyThenLocalContent(t *testing.T) {
	manifestYAML := "name: p\nversion: 1.0.0\ntargets:\n  - claude\ndependencies:\n  apm:\n    - ./vendor/dep\n  mcp: []\n"
	staleCleanupProject(t, manifestYAML, map[string]string{
		".apm/skills/L1/SKILL.md":            summaryOrderSkill("L1"),
		"vendor/dep/apm.yml":                 "name: dep\nversion: 1.0.0\n",
		"vendor/dep/.apm/skills/s1/SKILL.md": summaryOrderSkill("s1"),
	})
	want := []string{"_local/dep", "<project root> (local)"}

	for i := 0; i <= summaryOrderRuns; i++ {
		assertSummaryBlockOrder(t, i, staleCleanupInstall(t, ""), want)
	}
}

func TestInstall_SummaryBlockOrder_DependenciesInDeclaredOrder(t *testing.T) {
	manifestYAML := "name: p\nversion: 1.0.0\ntargets:\n  - claude\ndependencies:\n  apm:\n" +
		"    - ./vendor/zeta\n    - ./vendor/alpha\n    - ./vendor/mid\n  mcp: []\n"
	files := map[string]string{".apm/skills/L1/SKILL.md": summaryOrderSkill("L1")}
	for _, name := range []string{"zeta", "alpha", "mid"} {
		files["vendor/"+name+"/apm.yml"] = "name: " + name + "\nversion: 1.0.0\n"
		files["vendor/"+name+"/.apm/skills/s-"+name+"/SKILL.md"] = summaryOrderSkill("s-" + name)
	}
	staleCleanupProject(t, manifestYAML, files)
	want := []string{"_local/zeta", "_local/alpha", "_local/mid", "<project root> (local)"}

	for i := 0; i <= summaryOrderRuns; i++ {
		assertSummaryBlockOrder(t, i, staleCleanupInstall(t, ""), want)
	}
}
