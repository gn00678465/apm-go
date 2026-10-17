package gitops

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// On Windows a symlink of a local package is resolved as Python 3.12's
// ntpath.realpath(strict=True) resolves it for Oracle 0.33.0
// (install/phases/local_content.py:139-148), so a relative target resolves
// and a failure names the symlink itself. The expected reasons are Oracle
// 0.33.0's output in a Windows sandbox (issue #127, cases L6-L9) with the
// test root replaced by <ROOT>, or by <ROOT2> where repr() doubles its
// backslashes. A Windows error's message is in the system
// language, so for those the test checks the form Python gives it: the code,
// a message with no trailing dot or space, and repr() of the symlink.
func TestMaterializeLocalCopy_RefusesSymlinkItCannotDereferenceWindows(t *testing.T) {
	const dangling = ". Remove the dangling symlink or restore its target. Local install aborted."
	tests := []struct {
		name    string
		links   [][2]string // link under dep, its target; <DEP> is dep
		want    string      // the whole reason, when winCode is 0
		winCode int         // the [WinError <n>] of a broken link
	}{
		{
			name:  "file outside",
			links: [][2]string{{`.apm\agents\outer.agent.md`, `..\..\..\outside\outer.agent.md`}},
			want:  `Symlink '<ROOT>\dep\.apm\agents\outer.agent.md' resolves to '<ROOT>\outside\outer.agent.md', which escapes the package root '<ROOT>\dep'. Only in-package symlinks are dereferenced. Remove the symlink or replace it with a real file copy. Local install aborted.`,
		},
		{
			name:  "relative directory outside",
			links: [][2]string{{`.apm\agents`, `..\..\outside`}},
			want:  `Symlink '<ROOT>\dep\.apm\agents' resolves to '<ROOT>\outside', which escapes the package root '<ROOT>\dep'. Only in-package symlinks are dereferenced. Remove the symlink or replace it with a real file copy. Local install aborted.`,
		},
		{
			name:    "dangling, absolute",
			links:   [][2]string{{`.apm\agents\x.agent.md`, `<DEP>\.apm\agents\missing`}},
			winCode: 2,
		},
		{
			name:    "dangling, relative",
			links:   [][2]string{{`.apm\agents\x.agent.md`, `missing`}},
			winCode: 2,
		},
		{
			name: "two links to each other",
			links: [][2]string{
				{`.apm\agents\a.agent.md`, `<DEP>\.apm\agents\b.agent.md`},
				{`.apm\agents\b.agent.md`, `<DEP>\.apm\agents\a.agent.md`},
			},
			want: `Symlink loop from '<ROOT2>\\dep\\.apm\\agents\\a.agent.md'`,
		},
		{
			name:  "link to itself",
			links: [][2]string{{`.apm\agents\x.agent.md`, `x.agent.md`}},
			want:  `Symlink loop from '<ROOT2>\\dep\\.apm\\agents\\x.agent.md'`,
		},
		{
			name:  "directory cycle",
			links: [][2]string{{`self`, `.`}},
			want:  `Circular symlink detected: '<ROOT>\dep\self' resolves to '<ROOT>\dep', which was already visited during this install. Break the cycle by replacing one symlink with a real directory copy. Local install aborted.`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, dep := symlinkDep(t)
			writeAgent(t, filepath.Join(root, "outside", "outer.agent.md"))
			for _, l := range tt.links {
				symlink(t, strings.ReplaceAll(l[1], "<DEP>", dep), filepath.Join(dep, l[0]))
			}
			stale := filepath.Join("apm_modules", "_local", "dep")
			writePkg(t, stale)

			r := &RealPackageLoader{ModulesDir: "apm_modules"}
			_, err := r.LoadPackage(localRef("../dep", dep), "")
			if err == nil {
				t.Fatal("LoadPackage succeeded, want the copy refused")
			}
			got, ok := strings.CutPrefix(err.Error(), "copy local dependency "+dep+": ")
			if !ok {
				t.Fatalf("error %q does not start with the copy prefix", err)
			}
			if tt.winCode == 0 {
				// <ROOT2> is the root as repr() prints it, each backslash doubled.
				want := strings.NewReplacer("<ROOT>", root, "<ROOT2>", strings.ReplaceAll(root, `\`, `\\`)).Replace(tt.want)
				if got != want {
					t.Errorf("reason\n%s\nwant\n%s", got, want)
				}
			} else {
				link := filepath.Join(dep, tt.links[0][0])
				prefix := "Symlink '" + link + "' has a broken or unresolvable target: [WinError " + strconv.Itoa(tt.winCode) + "] "
				suffix := ": " + pyRepr(link) + dangling
				msg, ok := strings.CutPrefix(got, prefix)
				if ok {
					msg, ok = strings.CutSuffix(msg, suffix)
				}
				if !ok || msg == "" || strings.HasSuffix(msg, ".") || strings.TrimRight(msg, " \r\n\t") != msg {
					t.Errorf("reason\n%s\nwant\n%s<system message, no trailing dot or space>%s", got, prefix, suffix)
				}
			}
			if _, err := os.Lstat(stale); !os.IsNotExist(err) {
				t.Errorf("%s exists after a refused copy (lstat error %v)", stale, err)
			}
		})
	}
}
