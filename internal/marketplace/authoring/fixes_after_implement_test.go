package authoring

import (
	"context"
	"slices"
	"testing"

	"github.com/apm-go/apm/internal/gitops"
	"github.com/apm-go/apm/internal/semver"
)

// SPEC marketplace-check-outdated-fixes v2, after-implement squad: a local
// package keeps its published Current (SC-F20, D-4), a leading "v" does not
// change the upgrade verdict (SC-F8 build-tag, D-3), and the `git show`
// command is exactly the specified one under the full secure environment
// (SC-F16).

func TestOutdatedPackages_LocalShaPin_KeepsCurrentMap(t *testing.T) {
	for _, version := range []string{"", "1.0.0"} {
		t.Run("Version="+version, func(t *testing.T) {
			pkg := PackageEntry{Name: "tool", Source: "./pkgs/tool", Ref: shaA, Version: version}
			r := outdatedRowWithCurrent(t, pkg, panicLister{}, false, map[string]string{"tool": "v0.9.0"})
			if r.Status != "[i]" || r.Note != "local package; skipped" || r.Current != "v0.9.0" {
				t.Errorf("row = %+v, want [i] local skip keeping Current v0.9.0", r)
			}
		})
	}
}

func TestOutdatedPackages_ShaPinWithVersion_LeadingV_SameAsBare_BuildTag(t *testing.T) {
	lister := mapRefLister{refs: []semver.TagInfo{headRef(shaB), tagRef("v1.0.0", shaA), tagRef("v1.0.0+build", shaB)}}

	bare := outdatedRow(t, nil, PackageEntry{Name: "tool", Source: "owner/repo", Ref: shaA, Version: "1.0.0"}, lister, false)
	prefixed := outdatedRow(t, nil, PackageEntry{Name: "tool", Source: "owner/repo", Ref: shaA, Version: "v1.0.0"}, lister, false)

	if bare.Status != prefixed.Status || bare.Upgradable != prefixed.Upgradable {
		t.Errorf("version 1.0.0 -> %s upgradable=%v, v1.0.0 -> %s upgradable=%v; want the same verdict",
			bare.Status, bare.Upgradable, prefixed.Status, prefixed.Upgradable)
	}
}

func TestNewShowCmd_ExactCommandAndSecureEnv(t *testing.T) {
	dir := t.TempDir()
	cmd := newShowCmd(context.Background(), dir, ".claude-plugin/plugin.json")

	want := []string{"git", "-C", dir, "show", "FETCH_HEAD:.claude-plugin/plugin.json"}
	if !slices.Equal(cmd.Args, want) {
		t.Errorf("args = %q, want exactly %q", cmd.Args, want)
	}
	for _, e := range append(gitops.SecureGitEnv(), "LC_ALL=C", "LANGUAGE=C") {
		if !slices.Contains(cmd.Env, e) {
			t.Errorf("env lacks %q", e)
		}
	}
}
