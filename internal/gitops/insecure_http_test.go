package gitops

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/manifest"
)

// recordGit puts a git on PATH that logs GIT_ALLOW_PROTOCOL and its
// arguments and fails, and a URL rewrite probe that logs before it; it
// returns the log.
func recordGit(t *testing.T) func() []string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the recording git is a shell script")
	}
	bin := t.TempDir()
	log := filepath.Join(bin, "log")
	script := "#!/bin/sh\nprintf '%s %s\\n' \"$GIT_ALLOW_PROTOCOL\" \"$*\" >> " + log + "\nexit 1\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	restore := SetConfigProbeForTest(func(context.Context, ConfigProbe) ([]byte, int, error) {
		f, _ := os.OpenFile(log, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		f.WriteString("probe\n")
		f.Close()
		return nil, 0, nil
	})
	t.Cleanup(restore)
	return func() []string {
		data, _ := os.ReadFile(log)
		return strings.Split(strings.TrimSpace(string(data)), "\n")
	}
}

// TestCloneAllowsHTTPOnlyWhenApproved: an http:// dependency's clone and tag
// listing get http in GIT_ALLOW_PROTOCOL only when install's policy approved
// it, the URL rewrite check runs first, and every other git command keeps
// the default list.
func TestCloneAllowsHTTPOnlyWhenApproved(t *testing.T) {
	cases := []struct {
		name, url string
		approved  bool
		want      string
	}{
		{"http not approved", "http://127.0.0.1/o/pkg.git", false, "https:ssh:git"},
		{"http approved", "http://127.0.0.1/o/pkg.git", true, "https:ssh:git:http"},
		{"https approved", "https://127.0.0.1/o/pkg.git", true, "https:ssh:git"},
	}
	for _, tc := range cases {
		for _, gitRef := range []string{"main", strings.Repeat("a", 40)} {
			t.Run(tc.name+" "+gitRef[:4], func(t *testing.T) {
				lines := recordGit(t)
				ref, err := manifest.ParseDepString(tc.url)
				if err != nil {
					t.Fatal(err)
				}
				ref.HTTPApproved = tc.approved
				loader := &RealPackageLoader{ModulesDir: t.TempDir()}
				if _, err := loader.LoadPackage(ref, gitRef); err == nil {
					t.Fatal("the recording git fails every command")
				}
				got := lines()
				if len(got) < 2 || got[0] != "probe" {
					t.Fatalf("the rewrite check did not run before git: %q", got)
				}
				if !strings.HasPrefix(got[1], tc.want+" -c core.autocrlf=false clone ") {
					t.Errorf("clone got %q, want GIT_ALLOW_PROTOCOL=%s", got[1], tc.want)
				}
			})
		}
		t.Run(tc.name+" tags", func(t *testing.T) {
			lines := recordGit(t)
			ref, _ := manifest.ParseDepString(tc.url)
			ref.HTTPApproved = tc.approved
			(&RealTagLister{}).ListTagsForRef(ref)
			if got := lines(); len(got) != 2 || !strings.HasPrefix(got[1], tc.want+" ls-remote ") {
				t.Errorf("ls-remote got %q, want GIT_ALLOW_PROTOCOL=%s", got, tc.want)
			}
		})
	}
	t.Run("other git commands", func(t *testing.T) {
		lines := recordGit(t)
		ResolveCommit(t.TempDir())
		(&RealTagLister{}).ListTags("http://127.0.0.1/o/pkg.git")
		for _, l := range lines() {
			if l != "probe" && !strings.HasPrefix(l, "https:ssh:git ") {
				t.Errorf("%q has another protocol list", l)
			}
		}
	})
}
