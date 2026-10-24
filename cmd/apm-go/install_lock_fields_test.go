package main

import (
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/semver"
)

// Issue #124 item 82: a git entry records the content_hash of its directory
// below apm_modules and neither `source: git` nor a constraint for a branch
// ref, as Oracle 0.33.0's lock does (measured, A/124-82/r3: the files and
// hashes below are that run's); a semver range keeps its constraint.
func TestInstall_GitDependency_LockContentHashNoSourceNoConstraint(t *testing.T) {
	cases := []struct {
		name, dep, key, want string
		files                map[string]string
	}{
		{
			name: "subdirectory package", dep: "acme/mono/pkg#main", key: "acme/mono/pkg",
			files: map[string]string{
				"apm.yml":                             "name: monopkg\nversion: 3.1.0\n",
				".apm/instructions/m.instructions.md": "---\napplyTo: \"**\"\n---\n\nRule m.\n",
			},
			want: "sha256:1451ccba8759ac8fb7ca4eb239debbd84d992723e50a1bbcbf00e64bca7a965d",
		},
		{
			name: "whole repository", dep: "acme/whole#main", key: "acme/whole",
			files: map[string]string{
				"apm.yml":                             "name: wholepkg\nversion: 2.0.0\n",
				".apm/instructions/w.instructions.md": "---\napplyTo: \"**\"\n---\n\nRule w.\n",
			},
			want: "sha256:50c2ea3b55758c682c81a9d45c0b0ecea070e86f5183c6a3dd2bbc32173d4127",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lock, _, _, _ := gitVersionInstall(t, tc.dep, tc.key, tc.files, nil)
			if !strings.Contains(lock, "content_hash: "+tc.want) {
				t.Errorf("lock has no content_hash %s\n%s", tc.want, lock)
			}
			for _, absent := range []string{"source: git", "constraint:"} {
				if strings.Contains(lock, absent) {
					t.Errorf("lock has %q\n%s", absent, lock)
				}
			}
		})
	}
	t.Run("semver range", func(t *testing.T) {
		files := map[string]string{"apm.yml": "name: foo\nversion: 9.9.9\n", ".apm/instructions/x.instructions.md": "# x\n"}
		lock, _, _, _ := gitVersionInstall(t, "acme/foo#^1.0.0", "acme/foo", files,
			map[string][]semver.TagInfo{"acme/foo": {{Name: "v1.5.0"}}})
		if !strings.Contains(lock, "constraint: ^1.0.0") || strings.Contains(lock, "source: git") {
			t.Errorf("semver lock wants constraint ^1.0.0 and no source\n%s", lock)
		}
	})
}
