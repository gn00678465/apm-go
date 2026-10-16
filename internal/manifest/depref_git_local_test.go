package manifest

import (
	"runtime"
	"slices"
	"strings"
	"testing"
)

// Issue #114: a `git:` entry whose value is a local path is the local package
// that path names. Each row is what Oracle 0.33.0's
// DependencyReference.parse_from_dict gave for the same entry.
func TestParseDepDict_GitLocalPathIsLocalPackage(t *testing.T) {
	for _, tc := range []struct {
		entry, localPath, ref, alias string
		skills, targets              []string
	}{
		{entry: "git: ./repo", localPath: "./repo"},
		{entry: "git: ../x", localPath: "../x"},
		{entry: "git: ~/x", localPath: "~/x"},
		{entry: "git: /abs/x", localPath: "/abs/x"},
		{entry: `git: .\repo`, localPath: `.\repo`},
		{entry: `git: ..\x`, localPath: `..\x`},
		{entry: `git: ~\x`, localPath: `~\x`},
		{entry: `git: C:\work\x`, localPath: `C:\work\x`},
		{entry: "git: ' ./repo '", localPath: "./repo"},
		{entry: "git: ./repo\nref: v1.0.0", localPath: "./repo", ref: "v1.0.0"},
		{entry: "git: ./repo\nref: ^1.0.0", localPath: "./repo", ref: "^1.0.0"},
		// The Oracle keeps virtual_path pkg, and copies the whole ./mono.
		{entry: "git: ./mono\npath: pkg", localPath: "./mono"},
		{entry: "git: ./repo\nalias: al", localPath: "./repo", alias: "al"},
		{entry: "git: ./repo\nskills: [b, a]", localPath: "./repo", skills: []string{"a", "b"}},
		{entry: "git: ./repo\ntargets: [Claude, copilot]", localPath: "./repo", targets: []string{"claude", "copilot"}},
	} {
		t.Run(tc.entry, func(t *testing.T) {
			// A drive letter is an absolute path on Windows alone, as for the
			// string form (TestParseDepString_AbsolutePath).
			if strings.HasPrefix(tc.localPath, "C:") && runtime.GOOS != "windows" {
				t.Skip("drive-letter path outside Windows")
			}
			d, err := parseDepEntry(t, tc.entry+"\n")
			if err != nil {
				t.Fatalf("ParseDepDict: %v", err)
			}
			if !d.IsLocal || d.Source != "local" || d.LocalPath != tc.localPath || d.RepoURL != "" {
				t.Errorf("IsLocal %v, Source %q, LocalPath %q, RepoURL %q; want true, local, %q, empty", d.IsLocal, d.Source, d.LocalPath, d.RepoURL, tc.localPath)
			}
			if d.Reference != tc.ref || d.Alias != tc.alias || d.VirtualPath != "" {
				t.Errorf("Reference %q, Alias %q, VirtualPath %q; want %q, %q, empty", d.Reference, d.Alias, d.VirtualPath, tc.ref, tc.alias)
			}
			if !slices.Equal(d.SkillSubset, tc.skills) || !slices.Equal(d.TargetSubset, tc.targets) {
				t.Errorf("SkillSubset %q, TargetSubset %q; want %q, %q", d.SkillSubset, d.TargetSubset, tc.skills, tc.targets)
			}
		})
	}
}

func TestParseDepDict_GitLocalPathWithNoName(t *testing.T) {
	for _, p := range []string{"./", "../"} {
		_, err := parseDepEntry(t, "git: "+p+"\n")
		want := "Local path '" + p + "' does not resolve to a named directory. Use a path that ends with a directory name (e.g., './my-package' instead of './')."
		if err == nil || !strings.HasSuffix(err.Error(), want) {
			t.Errorf("git: %s: err %v, want it to end with %q", p, err, want)
		}
	}
}

func TestParseDepDict_GitRemoteKeepsPath(t *testing.T) {
	d, err := parseDepEntry(t, "git: owner/repo\npath: sub\nref: v1\n")
	if err != nil {
		t.Fatal(err)
	}
	if d.IsLocal || d.Source != "git" || d.RepoURL != "owner/repo" || d.VirtualPath != "sub" || d.Reference != "v1" {
		t.Errorf("IsLocal %v, Source %q, RepoURL %q, VirtualPath %q, Reference %q; want false, git, owner/repo, sub, v1",
			d.IsLocal, d.Source, d.RepoURL, d.VirtualPath, d.Reference)
	}
}
