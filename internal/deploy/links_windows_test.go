package deploy

import (
	"path/filepath"
	"strings"
	"testing"
)

// link/win/out-links126.txt, Oracle 0.33.0 on Windows: "\" separates, the
// output is written with "/", a name is spelled as the disk spells it, a
// skill's UP.MD is rewritten, and a drive or a rooted path is kept.
func TestRun_LinksOnWindows(t *testing.T) {
	dir := t.TempDir()
	mod := filepath.Join(dir, "apm_modules", "_local", "dep")
	body := "[fwd](../../docs/d.md)\n[bs](..\\..\\docs\\d.md)\n[mix](..\\../docs/d.md)\n[case](../../DOCS/D.MD)\n[drive](C:/x.md)\n[rooted](\\docs\\d.md)\n[ctxbs](..\\context\\c.context.md)\n[ctxcase](../context/C.CONTEXT.MD)\n[q](..\\..\\docs\\d.md#s)\n"
	mkFile(t, mod, "apm.yml", "name: dep\nversion: 1.0.0\n")
	mkFile(t, mod, "docs/d.md", "# d\n")
	mkFile(t, mod, ".apm/context/c.context.md", "# c\n")
	mkFile(t, mod, ".apm/prompts/p.prompt.md", "---\ndescription: p\n---\n"+body)
	mkFile(t, mod, ".apm/skills/s/SKILL.md", "---\nname: s\ndescription: s\n---\n[bs](..\\..\\..\\docs\\d.md)\n[in](ref\\r.md)\n")
	mkFile(t, mod, ".apm/skills/s/ref/r.md", "[up](..\\..\\..\\..\\docs\\d.md)\n")
	mkFile(t, mod, ".apm/skills/s/ref/UP.MD", "[up](../../../../docs/d.md)\n")
	m, resolved := depManifest("_local/dep")
	if _, err := Run([]string{"copilot", "claude"}, dir, m, resolved, nil, "", false, nil); err != nil {
		t.Fatal(err)
	}
	want := "[fwd](../../apm_modules/_local/dep/docs/d.md)\n[bs](../../apm_modules/_local/dep/docs/d.md)\n[mix](../../apm_modules/_local/dep/docs/d.md)\n[case](../../apm_modules/_local/dep/docs/d.md)\n[drive](C:/x.md)\n[rooted](\\docs\\d.md)\n[ctxbs](../../apm_modules/_local/dep/.apm/context/c.context.md)\n[ctxcase](../../apm_modules/_local/dep/.apm/context/c.context.md)\n[q](../../apm_modules/_local/dep/docs/d.md#s)\n"
	if got := readDeployed(t, dir, ".github/prompts/p.prompt.md"); !strings.HasSuffix(got, "---\n"+want) {
		t.Errorf("p.prompt.md:\n%s\nwant the links:\n%s", got, want)
	}
	for path, want := range map[string]string{
		".claude/skills/s/SKILL.md":  "---\nname: s\ndescription: s\n---\n[bs](../../../apm_modules/_local/dep/docs/d.md)\n[in](ref\\r.md)\n",
		".claude/skills/s/ref/r.md":  "[up](../../../../apm_modules/_local/dep/docs/d.md)\n",
		".claude/skills/s/ref/UP.MD": "[up](../../../../apm_modules/_local/dep/docs/d.md)\n",
	} {
		if got := readDeployed(t, dir, path); got != want {
			t.Errorf("%s = %q, want %q", path, got, want)
		}
	}
}

// portable_link_relpath refuses a path on another drive.
func TestPortableLinkRelpath_OtherDrive(t *testing.T) {
	if _, ok := portableLinkRelpath(`D:\pkg\docs\d.md`, `C:\proj\.github\prompts`); ok {
		t.Error("a path on another drive was made relative")
	}
	if got, ok := portableLinkRelpath(`C:\proj\apm_modules\x\d.md`, `C:\proj\.github\prompts`); !ok || got != "../../apm_modules/x/d.md" {
		t.Errorf("same drive = %q, %v", got, ok)
	}
}
