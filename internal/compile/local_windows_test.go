package compile

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// On Windows .APM is .apm and .apm\Instructions is .apm\instructions, so a
// link of another case gets the first level of .apm\instructions as main
// reads it (issue #131 comments 6286949791 and 6286973860).
func TestCollectInstructions_LinkedAPMOfAnotherCase(t *testing.T) {
	for _, tc := range []struct{ link, below string }{
		{".APM", "instructions/"},
		{`.apm\Instructions`, ""},
	} {
		for _, kind := range []string{"junction", "symlink"} {
			t.Run(tc.link+" "+kind, func(t *testing.T) {
				target := t.TempDir()
				mkFile(t, target, tc.below+"top.instructions.md", "Linked top.\n")
				mkFile(t, target, tc.below+"sub/deep.instructions.md", "Linked deep.\n")
				dir := t.TempDir()
				link := filepath.Join(dir, tc.link)
				if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
					t.Fatal(err)
				}
				if kind == "junction" {
					if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
						t.Fatalf("mklink /J %s %s: %v: %s", link, target, err, out)
					}
				} else if err := os.Symlink(target, link); err != nil {
					t.Skipf("symlink creation unsupported/unprivileged: %v", err)
				}
				if !HasCompilableContent(dir) {
					t.Error("HasCompilableContent = false, want true")
				}
				bodiesInOrder(t, renderLocal(t, dir), []string{"Linked top."}, []string{"Linked deep."})
			})
		}
	}
}
