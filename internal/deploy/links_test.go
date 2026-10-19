package deploy

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Each wanted value below is what Oracle 0.33.0's UnifiedLinkResolver, or the
// Python 3.12 function it calls, returns for the same input
// (compilation/link_resolver.py, run with the Oracle's venv).
func TestLinkFilters_AsTheOracle(t *testing.T) {
	for _, tc := range []struct {
		link               string
		external, relative bool
		path, suffix       string
	}{
		{"https://example.com/a.md", true, false, "https://example.com/a.md", ""},
		{"http:docs/d.md", false, false, "http:docs/d.md", ""},
		{"https://[x/a.context.md", false, false, "https://[x/a.context.md", ""},
		{"http://[::1]/a", true, false, "http://[::1]/a", ""},
		{"http://[1.2.3.4]/a", false, false, "http://[1.2.3.4]/a", ""},
		{"http://a[b]/x", false, false, "http://a[b]/x", ""},
		{"http://[v1.x]/y", true, false, "http://[v1.x]/y", ""},
		{"http://[vz.x]/y", false, false, "http://[vz.x]/y", ""},
		{"HTTPS://Ex.com", true, false, "HTTPS://Ex.com", ""},
		{"C:/docs/d.md", false, false, "C:/docs/d.md", ""},
		{"file:../x", false, false, "file:../x", ""},
		{"mailto:a@b.c", false, false, "mailto:a@b.c", ""},
		{"//example.com/x", false, false, "//example.com/x", ""},
		{"#top", false, false, "", "#top"},
		{"/abs", false, false, "/abs", ""},
		{" ../../docs/d.md", false, true, " ../../docs/d.md", ""},
		{"\t../x", false, true, "\t../x", ""},
		{"../x?y#z", false, true, "../x", "?y#z"},
		{"doc.md?x=1#sec", false, true, "doc.md", "?x=1#sec"},
		{"x#a?b", false, true, "x", "#a?b"},
		{"   ", false, false, "   ", ""},
		{"\x00//h/x", false, true, "\x00//h/x", ""},
		{"1abc:x", false, true, "1abc:x", ""},
		{"a+b.c-d:x", false, false, "a+b.c-d:x", ""},
		{"ab c:x", false, true, "ab c:x", ""},
		{"../é:x", false, true, "../é:x", ""},
	} {
		if got := isExternalURL(tc.link); got != tc.external {
			t.Errorf("isExternalURL(%q) = %v, want %v", tc.link, got, tc.external)
		}
		if got := rewritableRelativeLink(tc.link); got != tc.relative {
			t.Errorf("rewritableRelativeLink(%q) = %v, want %v", tc.link, got, tc.relative)
		}
		if p, s := splitLinkTarget(tc.link); p != tc.path || s != tc.suffix {
			t.Errorf("splitLinkTarget(%q) = %q, %q, want %q, %q", tc.link, p, s, tc.path, tc.suffix)
		}
	}
}

func setWindowsPaths(t *testing.T, on bool) {
	t.Helper()
	old := windowsPaths
	windowsPaths = on
	t.Cleanup(func() { windowsPaths = old })
}

// PurePosixPath(link).name and PureWindowsPath(link).name.
func TestPyPathName(t *testing.T) {
	for _, tc := range []struct{ link, posix, windows string }{
		{"a/b.context.md", "b.context.md", "b.context.md"},
		{"a/b/", "b", "b"},
		{"", "", ""},
		{"./", "", ""},
		{"a/./", "a", "a"},
		{`..\x\c.context.md`, `..\x\c.context.md`, "c.context.md"},
		{"C:c.context.md", "C:c.context.md", "c.context.md"},
		{"/", "", ""},
	} {
		setWindowsPaths(t, false)
		if got := pyPathName(tc.link); got != tc.posix {
			t.Errorf("posix pyPathName(%q) = %q, want %q", tc.link, got, tc.posix)
		}
		setWindowsPaths(t, true)
		if got := pyPathName(tc.link); got != tc.windows {
			t.Errorf("windows pyPathName(%q) = %q, want %q", tc.link, got, tc.windows)
		}
	}
}

// rglob("*.md") matches without regard to case on Windows alone.
func TestSkillMarkdown(t *testing.T) {
	setWindowsPaths(t, false)
	if !skillMarkdown("a.md") || skillMarkdown("UP.MD") || skillMarkdown("a.mdx") {
		t.Error("posix: want a.md alone")
	}
	setWindowsPaths(t, true)
	if !skillMarkdown("a.md") || !skillMarkdown("UP.MD") || skillMarkdown("a.mdx") {
		t.Error("windows: want a.md and UP.MD")
	}
}

// resolve_links' count, len(set(original) - set(resolved)).
func TestRewrittenCount(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want int
	}{
		{"[a](x) [b](y)", "[a](y) [b](x)", 0},
		{"[a](x) ](z)", "[a](q) ](z)", 1},
		{"[a](x) [b](x)", "[a](q) [b](q)", 1},
	} {
		if got := rewrittenCount(tc.a, tc.b); got != tc.want {
			t.Errorf("rewrittenCount(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

// discover_primitives + register_contexts of the same tree
// (~/.cache/apm-go-verify/issue126/impl/G-reg-niQC): of two contexts of one
// name the first walked is kept, a directory of DEFAULT_SKIP_DIRS or with a
// .git, a symlink and a frontmatter that does not load are left out.
func TestContextRegistry_AsTheOracle(t *testing.T) {
	root := t.TempDir()
	for rel, content := range map[string]string{
		".apm/context/a.context.md": "# a\n",
		"docs/a.context.md":         "# a2\n",
		"docs/a.memory.md":          "# am\n",
		"build/b.context.md":        "# b\n",
		"nested/n.context.md":       "# n\n",
		"docs/bad.context.md":       "---\nx: [\n---\n# bad\n",
		"top.memory.md":             "# top\n",
		".apm/memory/notes.md":      "# mm\n",
		"zz/a.context.md":           "# z\n",
	} {
		mkFile(t, root, rel, content)
	}
	mkdir(t, filepath.Join(root, "nested", ".git"))
	if err := os.Symlink("../docs/a.context.md", filepath.Join(root, "zz", "l.context.md")); err != nil {
		t.Fatal(err)
	}
	real, _ := filepath.EvalSymlinks(root)
	got := map[string]string{}
	for k, v := range contextRegistry([]string{root}) {
		rel, _ := filepath.Rel(real, v)
		got[k] = filepath.ToSlash(rel)
	}
	want := map[string]string{"a.context.md": ".apm/context/a.context.md", "top.memory.md": "top.memory.md"}
	if !maps.Equal(got, want) {
		t.Errorf("registry = %v, want %v", got, want)
	}
}

// link/g1out whole-or: a dependency's symlinks are names in the Oracle's
// apm_modules, which holds none, so a link is read from the symlink's own
// directory and names what it passes through as it is written. An agent that
// is a symlink is named after the symlink in its warning too.
func TestRun_LinksOfADependencyWithSymlinks(t *testing.T) {
	dir := t.TempDir()
	mod := filepath.Join(dir, "apm_modules", "o", "whole")
	mkFile(t, mod, "apm.yml", "name: whole\nversion: 1.0.0\n")
	mkFile(t, mod, "docs/real/d.md", "# d\n")
	mkFile(t, mod, ".apm/prompts/p.prompt.md", "---\ndescription: p\ncolor: green\n---\n[d](../../docs/real/d.md)\n[link](../../docs/link.md)\n[alias](../../docs/alias/d.md)\n[aliasup](../../docs/alias/../real/d.md)\n")
	for link, target := range map[string]string{"docs/alias": "real", "docs/link.md": "real/d.md", ".apm/agents/sym.agent.md": "../prompts/p.prompt.md"} {
		mkdir(t, filepath.Dir(filepath.Join(mod, link)))
		if err := os.Symlink(target, filepath.Join(mod, link)); err != nil {
			t.Fatal(err)
		}
	}
	m, resolved := depManifest("o/whole")
	result, err := Run([]string{"claude", "copilot", "opencode"}, dir, m, resolved, nil, "", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	links := "[d](../../apm_modules/o/whole/docs/real/d.md)\n[link](../../apm_modules/o/whole/docs/link.md)\n[alias](../../apm_modules/o/whole/docs/alias/d.md)\n[aliasup](../../apm_modules/o/whole/docs/real/d.md)\n"
	for path, want := range map[string]string{
		".github/prompts/p.prompt.md": "---\ndescription: p\ncolor: green\n---\n" + links,
		".github/agents/sym.agent.md": "---\ndescription: p\ncolor: green\n---\n" + links,
		".claude/agents/sym.md":       "---\ndescription: p\ncolor: green\n---\n" + links,
		".claude/commands/p.md":       "---\ndescription: p\n---\n\n" + strings.TrimSuffix(links, "\n"),
	} {
		if got := readDeployed(t, dir, path); got != want {
			t.Errorf("%s:\n%s\nwant:\n%s", path, got, want)
		}
	}
	var shown []string
	for _, n := range result.CommandNotes {
		if strings.HasPrefix(n.Text, "OpenCode agent") {
			shown = append(shown, n.Text[:strings.Index(n.Text, " has")])
		}
	}
	if want := []string{"OpenCode agent 'whole/sym.agent.md'"}; !slices.Equal(shown, want) {
		t.Errorf("OpenCode warnings name %q, want %q", shown, want)
	}
}

// Issue #126 comment 6267470396 item 2: a file with no relative link is
// written as before, through oracleText, and a skill file is copied as it
// is.
func TestRun_NoLinkKeepsTheBytes(t *testing.T) {
	dir := t.TempDir()
	mod := filepath.Join(dir, "apm_modules", "acme", "pkg")
	mkFile(t, mod, ".apm/agents/a.agent.md", "---\r\nname: a\r\n---\r\nno link [x] (y)\r\n")
	mkFile(t, mod, ".apm/instructions/i.instructions.md", "---\r\napplyTo: '**'\r\n---\r\nsee https://example.com/a.md\r")
	mkFile(t, mod, ".apm/skills/s/SKILL.md", "---\r\nname: s\r\n---\r\n[in](ref/r.md)\r\n")
	mkFile(t, mod, ".apm/skills/s/ref/r.md", "[out](https://example.com)\r\n")
	m, resolved := depManifest("acme/pkg")
	if _, err := Run([]string{"copilot"}, dir, m, resolved, nil, "", false, nil); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{
		".github/agents/a.agent.md":              "---\nname: a\n---\nno link [x] (y)\n",
		".github/instructions/i.instructions.md": "---\napplyTo: '**'\n---\nsee https://example.com/a.md\n",
		".agents/skills/s/SKILL.md":              "---\r\nname: s\r\n---\r\n[in](ref/r.md)\r\n",
		".agents/skills/s/ref/r.md":              "[out](https://example.com)\r\n",
	} {
		if got := readDeployed(t, dir, path); got != want {
			t.Errorf("%s = %q, want %q", path, got, want)
		}
	}
}
