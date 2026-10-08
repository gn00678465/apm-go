package authoring

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/apm-go/apm/internal/semver"
)

// `marketplace package update` (issue #25): the plan comes from outdated's
// own verdict, the write replaces two scalar values and nothing else.

var (
	shaD = strings.Repeat("d", 40)
	shaE = strings.Repeat("e", 40)
)

// sourceLister answers ListRefs per source and counts the calls.
type sourceLister struct {
	refs  map[string][]semver.TagInfo
	errs  map[string]error
	calls map[string]int
}

func (s *sourceLister) ListRefs(source string) ([]semver.TagInfo, error) {
	if s.calls == nil {
		s.calls = map[string]int{}
	}
	s.calls[source]++
	if err := s.errs[source]; err != nil {
		return nil, err
	}
	return s.refs[source], nil
}

func peeledRef(name, sha string) semver.TagInfo {
	return semver.TagInfo{Name: name + "^{}", Commit: sha, Ref: "refs/tags/" + name + "^{}"}
}

func apmYML(packages string) string {
	return "name: demo\nversion: 0.1.0\nmarketplace:\n  owner:\n    name: me\n  packages:\n" + packages
}

// updatePackages runs the command's two steps against dir.
func updatePackages(t *testing.T, dir string, names []string, includePrerelease bool, lister RefLister) ([]PackageUpdate, error) {
	t.Helper()
	cfg, _, err := LoadAuthoringConfig(dir)
	if err != nil {
		t.Fatalf("LoadAuthoringConfig: %v", err)
	}
	updates, err := PlanPackageUpdates(cfg, names, includePrerelease, lister)
	if err != nil {
		return nil, err
	}
	return updates, ApplyPackageUpdates(dir, updates, false)
}

func readFile(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func assertFile(t *testing.T, dir, name, want string) {
	t.Helper()
	if got := readFile(t, dir, name); got != want {
		t.Errorf("%s =\n%s\nwant\n%s", name, got, want)
	}
}

// backdate gives dir/name an old mtime, so a later write shows.
func backdate(t *testing.T, dir, name string) time.Time {
	t.Helper()
	old := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := os.Chtimes(filepath.Join(dir, name), old, old); err != nil {
		t.Fatal(err)
	}
	return old
}

func assertNotWritten(t *testing.T, dir, name, content string, mtime time.Time) {
	t.Helper()
	assertFile(t, dir, name, content)
	info, err := os.Stat(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(mtime) {
		t.Errorf("%s mtime = %v, want %v (the file was written)", name, info.ModTime(), mtime)
	}
	assertNoTempFiles(t, dir)
}

func TestPackageUpdate_ShaPinWithVersion_ReplacesOnlyTheTwoValues(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", `name: demo
version: 0.1.0
# the catalog
marketplace:
  owner:
    name: me
  packages:
    # pinned by hand
    - ref: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa   # the release commit
      x-note: keep me
      name: tool
      # shown in the catalog
      version: 1.0.0 # display only
      source: owner/tool
      tags: [a, b]

    - name: other
      source: owner/other
      ref: main
`)
	lister := &sourceLister{refs: map[string][]semver.TagInfo{
		"owner/tool": {headRef(shaC), tagRef("v1.0.0", shaA), tagRef("v1.1.0", shaB), tagRef("v2.0.0", shaC)},
	}}

	updates, err := updatePackages(t, dir, nil, false, lister)
	if err != nil {
		t.Fatalf("update: %v", err)
	}

	assertFile(t, dir, "apm.yml", `name: demo
version: 0.1.0
# the catalog
marketplace:
  owner:
    name: me
  packages:
    # pinned by hand
    - ref: cccccccccccccccccccccccccccccccccccccccc   # the release commit
      x-note: keep me
      name: tool
      # shown in the catalog
      version: 2.0.0 # display only
      source: owner/tool
      tags: [a, b]

    - name: other
      source: owner/other
      ref: main
`)
	if len(updates) != 2 {
		t.Fatalf("len(updates) = %d, want 2", len(updates))
	}
	if u := updates[0]; u.Action != UpdateApply || u.NewVersion != "2.0.0" || u.NewRef != shaC {
		t.Errorf("updates[0] = %+v, want apply 2.0.0 / %s", u, shaC)
	}
	if u := updates[1]; u.Action != UpdateSkip || u.Note != "Pinned to ref; skipped" {
		t.Errorf("updates[1] = %+v, want skip 'Pinned to ref; skipped'", u)
	}
	if lister.calls["owner/tool"] != 1 || lister.calls["owner/other"] != 0 {
		t.Errorf("ListRefs calls = %v, want one for owner/tool and none for owner/other", lister.calls)
	}
}

func TestPackageUpdate_AnnotatedTag_WritesThePeeledCommit(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", apmYML("    - name: tool\n      source: owner/tool\n      version: 1.0.0\n      ref: "+shaA+"\n"))
	lister := &sourceLister{refs: map[string][]semver.TagInfo{
		"owner/tool": {headRef(shaC), tagRef("v1.0.0", shaA), tagRef("v2.0.0", shaB), peeledRef("v2.0.0", shaC)},
	}}

	if _, err := updatePackages(t, dir, nil, false, lister); err != nil {
		t.Fatalf("update: %v", err)
	}

	assertFile(t, dir, "apm.yml", apmYML("    - name: tool\n      source: owner/tool\n      version: 2.0.0\n      ref: cccccccccccccccccccccccccccccccccccccccc\n"))
}

func TestPackageUpdate_LightweightTag_WritesTheTagCommit(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", apmYML("    - name: tool\n      source: owner/tool\n      version: 1.0.0\n      ref: "+shaA+"\n"))
	// The peeled entry of another tag must not be picked up.
	lister := &sourceLister{refs: map[string][]semver.TagInfo{
		"owner/tool": {headRef(shaD), tagRef("v1.0.0", shaA), tagRef("v1.5.0", shaE), peeledRef("v1.5.0", shaD), tagRef("v2.0.0", shaB)},
	}}

	if _, err := updatePackages(t, dir, nil, false, lister); err != nil {
		t.Fatalf("update: %v", err)
	}

	assertFile(t, dir, "apm.yml", apmYML("    - name: tool\n      source: owner/tool\n      version: 2.0.0\n      ref: bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\n"))
}

func TestPackageUpdate_ShaPinWithoutVersion_WritesTheFullTipAndNoVersion(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", apmYML("    - name: tool\n      source: owner/tool\n      ref: "+shaA+"\n      subdir: skills/tool\n"))
	lister := &sourceLister{refs: map[string][]semver.TagInfo{
		"owner/tool": {headRef(shaB), tagRef("v9.0.0", shaC)},
	}}

	updates, err := updatePackages(t, dir, nil, false, lister)
	if err != nil {
		t.Fatalf("update: %v", err)
	}

	assertFile(t, dir, "apm.yml", apmYML("    - name: tool\n      source: owner/tool\n      ref: bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\n      subdir: skills/tool\n"))
	if u := updates[0]; u.Action != UpdateApply || u.NewVersion != "" || u.NewRef != shaB {
		t.Errorf("updates[0] = %+v, want apply with no version / %s", u, shaB)
	}
}

func TestPackageUpdate_VPrefixedVersion_KeepsThePrefix(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", apmYML("    - name: tool\n      source: owner/tool\n      version: v1.2.0\n      ref: "+shaA+"\n"))
	lister := &sourceLister{refs: map[string][]semver.TagInfo{
		"owner/tool": {headRef(shaB), tagRef("v1.2.0", shaA), tagRef("v1.3.0", shaB)},
	}}

	if _, err := updatePackages(t, dir, nil, false, lister); err != nil {
		t.Fatalf("update: %v", err)
	}

	assertFile(t, dir, "apm.yml", apmYML("    - name: tool\n      source: owner/tool\n      version: v1.3.0\n      ref: bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\n"))
}

func TestPackageUpdate_Prerelease(t *testing.T) {
	refs := map[string][]semver.TagInfo{
		"owner/tool": {headRef(shaC), tagRef("v1.0.0", shaA), tagRef("v1.1.0", shaB), tagRef("v2.0.0-rc.1", shaC)},
	}
	entry := "    - name: tool\n      source: owner/tool\n      version: 1.0.0\n      ref: " + shaA + "\n"
	stable := "    - name: tool\n      source: owner/tool\n      version: 1.1.0\n      ref: bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\n"
	pre := "    - name: tool\n      source: owner/tool\n      version: 2.0.0-rc.1\n      ref: cccccccccccccccccccccccccccccccccccccccc\n"

	t.Run("left out by default", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "apm.yml", apmYML(entry))
		if _, err := updatePackages(t, dir, nil, false, &sourceLister{refs: refs}); err != nil {
			t.Fatalf("update: %v", err)
		}
		assertFile(t, dir, "apm.yml", apmYML(stable))
	})
	t.Run("included by the flag", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "apm.yml", apmYML(entry))
		if _, err := updatePackages(t, dir, nil, true, &sourceLister{refs: refs}); err != nil {
			t.Fatalf("update: %v", err)
		}
		assertFile(t, dir, "apm.yml", apmYML(pre))
	})
	t.Run("included by the entry", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "apm.yml", apmYML(entry+"      include_prerelease: true\n"))
		if _, err := updatePackages(t, dir, nil, false, &sourceLister{refs: refs}); err != nil {
			t.Fatalf("update: %v", err)
		}
		assertFile(t, dir, "apm.yml", apmYML(pre+"      include_prerelease: true\n"))
	})
}

func TestPackageUpdate_InferredTagLayout(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", apmYML("    - name: tool\n      source: owner/mono\n      version: 4.1.2\n      ref: "+shaA+"\n"))
	// The default pattern "v{version}" matches none of these tags.
	lister := &sourceLister{refs: map[string][]semver.TagInfo{
		"owner/mono": {headRef(shaC), tagRef("tool-v4.1.2", shaA), tagRef("tool-v4.4.0", shaB), tagRef("zzz-v9.0.0", shaC)},
	}}

	if _, err := updatePackages(t, dir, nil, false, lister); err != nil {
		t.Fatalf("update: %v", err)
	}

	assertFile(t, dir, "apm.yml", apmYML("    - name: tool\n      source: owner/mono\n      version: 4.4.0\n      ref: bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\n"))
}

func TestPackageUpdate_SkippedEntries_NoListRefsNoWrite(t *testing.T) {
	dir := t.TempDir()
	content := apmYML(`    - name: local
      source: ./pkgs/local
    - name: named
      source: owner/named
      ref: v1.0.0
    - name: sha-range
      source: owner/sha-range
      ref: ` + shaA + `
      version: ^1.0.0
    - name: range
      source: owner/range
      version: ^1.0.0
`)
	writeFile(t, dir, "apm.yml", content)
	mtime := backdate(t, dir, "apm.yml")
	lister := &sourceLister{}

	updates, err := updatePackages(t, dir, nil, false, lister)
	if err != nil {
		t.Fatalf("update: %v", err)
	}

	assertNotWritten(t, dir, "apm.yml", content, mtime)
	wantNotes := []string{"local package; skipped", "Pinned to ref; skipped", "Pinned to ref; skipped", "No ref pin; skipped"}
	if len(updates) != len(wantNotes) {
		t.Fatalf("len(updates) = %d, want %d", len(updates), len(wantNotes))
	}
	for i, want := range wantNotes {
		if updates[i].Action != UpdateSkip || updates[i].Note != want {
			t.Errorf("updates[%d] = {Action:%v Note:%q}, want skip %q", i, updates[i].Action, updates[i].Note, want)
		}
	}
	if len(lister.calls) != 0 {
		t.Errorf("ListRefs calls = %v, want none", lister.calls)
	}
}

func TestPackageUpdate_UpToDateAndNoMatchingTags_Skipped(t *testing.T) {
	dir := t.TempDir()
	content := apmYML("    - name: current\n      source: owner/current\n      version: 1.1.0\n      ref: " + shaB + "\n" +
		"    - name: at-tip\n      source: owner/at-tip\n      ref: " + shaA + "\n" +
		"    - name: untagged\n      source: owner/untagged\n      version: 1.0.0\n      ref: " + shaA + "\n")
	writeFile(t, dir, "apm.yml", content)
	mtime := backdate(t, dir, "apm.yml")
	lister := &sourceLister{refs: map[string][]semver.TagInfo{
		"owner/current":  {headRef(shaB), tagRef("v1.0.0", shaA), tagRef("v1.1.0", shaB)},
		"owner/at-tip":   {headRef(shaA)},
		"owner/untagged": {headRef(shaB)},
	}}

	updates, err := updatePackages(t, dir, nil, false, lister)
	if err != nil {
		t.Fatalf("update: %v", err)
	}

	assertNotWritten(t, dir, "apm.yml", content, mtime)
	wantNotes := []string{"already up to date", "already up to date", "No matching tags found"}
	for i, want := range wantNotes {
		if updates[i].Action != UpdateSkip || updates[i].Note != want {
			t.Errorf("updates[%d] = {Action:%v Note:%q}, want skip %q", i, updates[i].Action, updates[i].Note, want)
		}
	}
}

func TestPackageUpdate_OneResolutionFails_NothingWritten(t *testing.T) {
	dir := t.TempDir()
	content := apmYML("    - name: first\n      source: owner/first\n      ref: " + shaA + "\n" +
		"    - name: second\n      source: owner/second\n      ref: " + shaA + "\n" +
		"    - name: third\n      source: owner/third\n      ref: " + shaA + "\n")
	writeFile(t, dir, "apm.yml", content)
	mtime := backdate(t, dir, "apm.yml")
	lister := &sourceLister{
		refs: map[string][]semver.TagInfo{"owner/first": {headRef(shaB)}, "owner/third": {tagRef("v1.0.0", shaB)}},
		errs: map[string]error{"owner/second": errors.New("git ls-remote https://github.com/owner/second: not found")},
	}

	_, err := updatePackages(t, dir, nil, false, lister)

	want := "cannot update: package 'second': git ls-remote https://github.com/owner/second: not found\n" +
		"cannot update: package 'third': Remote advertised no HEAD"
	if err == nil || err.Error() != want {
		t.Fatalf("error = %v\nwant %s", err, want)
	}
	assertNotWritten(t, dir, "apm.yml", content, mtime)
}

func TestPackageUpdate_NameFilter(t *testing.T) {
	entries := "    - name: first\n      source: owner/first\n      ref: " + shaA + "\n" +
		"    - name: Second\n      source: owner/second\n      ref: " + shaA + "\n"
	refs := map[string][]semver.TagInfo{"owner/first": {headRef(shaB)}, "owner/second": {headRef(shaC)}}

	t.Run("only the named entry, matched without case", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "apm.yml", apmYML(entries))
		lister := &sourceLister{refs: refs}

		updates, err := updatePackages(t, dir, []string{"second", "SECOND"}, false, lister)
		if err != nil {
			t.Fatalf("update: %v", err)
		}

		assertFile(t, dir, "apm.yml", apmYML("    - name: first\n      source: owner/first\n      ref: "+shaA+"\n"+
			"    - name: Second\n      source: owner/second\n      ref: cccccccccccccccccccccccccccccccccccccccc\n"))
		if len(updates) != 1 || updates[0].Package.Name != "Second" {
			t.Errorf("updates = %+v, want the one entry 'Second'", updates)
		}
		if lister.calls["owner/first"] != 0 || lister.calls["owner/second"] != 1 {
			t.Errorf("ListRefs calls = %v, want one for owner/second only", lister.calls)
		}
	})
	t.Run("an unknown name is an error before any ListRefs", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "apm.yml", apmYML(entries))
		mtime := backdate(t, dir, "apm.yml")
		lister := &sourceLister{refs: refs}

		_, err := updatePackages(t, dir, []string{"first", "nope"}, false, lister)

		if err == nil || err.Error() != `package "nope" not found` {
			t.Fatalf("error = %v, want package \"nope\" not found", err)
		}
		assertNotWritten(t, dir, "apm.yml", apmYML(entries), mtime)
		if len(lister.calls) != 0 {
			t.Errorf("ListRefs calls = %v, want none", lister.calls)
		}
	})
}

func TestPackageUpdate_QuotedValues_KeepTheirQuotes(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", apmYML("    - name: tool\n      source: owner/tool\n      version: \"1.0.0\"\n      ref: '"+shaA+"'\n"))
	lister := &sourceLister{refs: map[string][]semver.TagInfo{"owner/tool": {headRef(shaB), tagRef("v1.0.0", shaA), tagRef("v1.1.0", shaB)}}}

	if _, err := updatePackages(t, dir, nil, false, lister); err != nil {
		t.Fatalf("update: %v", err)
	}

	assertFile(t, dir, "apm.yml", apmYML("    - name: tool\n      source: owner/tool\n      version: \"1.1.0\"\n      ref: 'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb'\n"))
}

func TestPackageUpdate_CRLFFile_KeepsCRLF(t *testing.T) {
	dir := t.TempDir()
	crlf := func(s string) string { return strings.ReplaceAll(s, "\n", "\r\n") }
	writeFile(t, dir, "apm.yml", crlf(apmYML("    - name: tool\n      source: owner/tool\n      version: 1.0.0 # shown\n      ref: "+shaA+"\n")))
	lister := &sourceLister{refs: map[string][]semver.TagInfo{"owner/tool": {headRef(shaB), tagRef("v1.0.0", shaA), tagRef("v1.1.0", shaB)}}}

	if _, err := updatePackages(t, dir, nil, false, lister); err != nil {
		t.Fatalf("update: %v", err)
	}

	assertFile(t, dir, "apm.yml", crlf(apmYML("    - name: tool\n      source: owner/tool\n      version: 1.1.0 # shown\n      ref: bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\n")))
}

func TestPackageUpdate_LegacyMarketplaceYML(t *testing.T) {
	dir := t.TempDir()
	legacy := func(ref string) string {
		return "# legacy\nname: demo\nversion: 0.1.0\nowner:\n  name: me\npackages:\n  - name: tool\n    source: owner/tool\n    ref: " + ref + " # tip\n"
	}
	writeFile(t, dir, "marketplace.yml", legacy(shaA))
	lister := &sourceLister{refs: map[string][]semver.TagInfo{"owner/tool": {headRef(shaB)}}}

	if _, err := updatePackages(t, dir, nil, false, lister); err != nil {
		t.Fatalf("update: %v", err)
	}

	assertFile(t, dir, "marketplace.yml", legacy(shaB))
}

func TestPackageUpdate_FlowMappingEntry_ErrorNothingWritten(t *testing.T) {
	dir := t.TempDir()
	content := apmYML("    - name: block\n      source: owner/block\n      ref: " + shaA + "\n" +
		"    - {name: flow, source: owner/flow, ref: " + shaA + "}\n")
	writeFile(t, dir, "apm.yml", content)
	mtime := backdate(t, dir, "apm.yml")
	lister := &sourceLister{refs: map[string][]semver.TagInfo{"owner/block": {headRef(shaB)}, "owner/flow": {headRef(shaB)}}}

	_, err := updatePackages(t, dir, nil, false, lister)

	if err == nil || !strings.Contains(err.Error(), "package 'flow'") || !strings.Contains(err.Error(), "ref") {
		t.Fatalf("error = %v, want one that names package 'flow' and its ref", err)
	}
	assertNotWritten(t, dir, "apm.yml", content, mtime)
}

func TestPackageUpdate_ValidationFails_NothingWritten(t *testing.T) {
	dir := t.TempDir()
	content := apmYML("    - name: tool\n      source: owner/tool\n      ref: " + shaA + "\n")
	writeFile(t, dir, "apm.yml", content)
	mtime := backdate(t, dir, "apm.yml")
	lister := &sourceLister{refs: map[string][]semver.TagInfo{"owner/tool": {headRef(shaB)}}}

	origValidate := packageEditValidate
	packageEditValidate = func(out []byte, prefix []string) error {
		return fmt.Errorf("forced validation failure for test")
	}
	t.Cleanup(func() { packageEditValidate = origValidate })

	_, err := updatePackages(t, dir, nil, false, lister)

	if err == nil || !strings.Contains(err.Error(), "forced validation failure") {
		t.Fatalf("error = %v, want the forced validation failure", err)
	}
	assertNotWritten(t, dir, "apm.yml", content, mtime)
}

func TestPackageUpdate_ConfigChangedAfterThePlan_NothingWritten(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", apmYML("    - name: tool\n      source: owner/tool\n      ref: "+shaA+"\n"))
	cfg, _, err := LoadAuthoringConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	updates, err := PlanPackageUpdates(cfg, nil, false, &sourceLister{refs: map[string][]semver.TagInfo{"owner/tool": {headRef(shaB)}}})
	if err != nil {
		t.Fatal(err)
	}
	changed := apmYML("    - name: tool\n      source: owner/tool\n      ref: " + shaC + "\n")
	writeFile(t, dir, "apm.yml", changed)
	mtime := backdate(t, dir, "apm.yml")

	err = ApplyPackageUpdates(dir, updates, false)

	if err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("error = %v, want one that says the config changed", err)
	}
	assertNotWritten(t, dir, "apm.yml", changed, mtime)
}

// The new fields are the only thing update adds to outdated's rows.
func TestOutdatedPackages_TargetFields_OnlyOnUpgradableShaPins(t *testing.T) {
	lister := mapRefLister{refs: []semver.TagInfo{headRef(shaB), tagRef("v1.0.0", shaA), tagRef("v1.1.0", shaB)}}

	up := outdatedRow(t, nil, PackageEntry{Name: "tool", Source: "owner/repo", Ref: shaA, Version: "1.0.0"}, lister, false)
	if up.TargetVersion != "1.1.0" || up.TargetRef != shaB {
		t.Errorf("upgradable row target = %q / %q, want 1.1.0 / %s", up.TargetVersion, up.TargetRef, shaB)
	}
	current := outdatedRow(t, nil, PackageEntry{Name: "tool", Source: "owner/repo", Ref: shaB, Version: "1.1.0"}, lister, false)
	if current.TargetVersion != "" || current.TargetRef != "" {
		t.Errorf("up-to-date row target = %q / %q, want both empty", current.TargetVersion, current.TargetRef)
	}
	ranged := outdatedRow(t, nil, PackageEntry{Name: "tool", Source: "owner/repo", Version: "^1.0.0"}, lister, false)
	if !ranged.Upgradable || ranged.TargetVersion != "" || ranged.TargetRef != "" {
		t.Errorf("range row = {Upgradable:%v target %q / %q}, want upgradable with both empty", ranged.Upgradable, ranged.TargetVersion, ranged.TargetRef)
	}
}

// The refs here are what `git ls-remote -- <repo> HEAD refs/tags/* refs/heads/*`
// printed for a repository with a lightweight v1.0.0 and an annotated v1.1.0.
func TestPackageUpdate_RealLsRemoteOutput_AnnotatedTagPeels(t *testing.T) {
	refs := parseRefsOutput("8174134659c035fc0677a8c8ec13fe1ef255144e\tHEAD\n" +
		"8174134659c035fc0677a8c8ec13fe1ef255144e\trefs/heads/main\n" +
		"d4f51cbcf7cfeb311be807aab00b8650fb751025\trefs/tags/v1.0.0\n" +
		"635ead2fd2bf483ab1dd6611f7cea22cb743e5ee\trefs/tags/v1.1.0\n" +
		"eb26e8b300d82f3d0aac062dfa27940ef125440e\trefs/tags/v1.1.0^{}\n")
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", apmYML("    - name: tool\n      source: owner/tool\n      version: 1.0.0\n      ref: d4f51cbcf7cfeb311be807aab00b8650fb751025\n"))

	if _, err := updatePackages(t, dir, nil, false, &sourceLister{refs: map[string][]semver.TagInfo{"owner/tool": refs}}); err != nil {
		t.Fatalf("update: %v", err)
	}

	assertFile(t, dir, "apm.yml", apmYML("    - name: tool\n      source: owner/tool\n      version: 1.1.0\n      ref: eb26e8b300d82f3d0aac062dfa27940ef125440e\n"))
}

// ApplyPackageUpdates reads the file again; whatever it finds there that it
// cannot edit safely is an error, and the file stays as it is.
func TestApplyPackageUpdates_ConfigUnusableSinceThePlan_NothingWritten(t *testing.T) {
	plan := []PackageUpdate{{
		Index:   0,
		Package: PackageEntry{Name: "tool", Source: "owner/tool", Ref: shaA},
		Action:  UpdateApply,
		NewRef:  shaB,
	}}
	cases := []struct {
		name, file, content, wantErr string
	}{
		{"no config", "", "", "no marketplace"},
		{"not YAML", "marketplace.yml", "owner: [\n", "parse "},
		{"empty", "marketplace.yml", "", "parse "},
		{"not a mapping", "marketplace.yml", "- a\n", "no packages sequence"},
		{"schema error", "marketplace.yml", "name: demo\nowner:\n  name: me\npackages:\n  - name: tool\n", "source"},
		{"no packages", "marketplace.yml", "name: demo\nversion: 0.1.0\nowner:\n  name: me\n", "no packages sequence"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.file != "" {
				writeFile(t, dir, tc.file, tc.content)
			}

			err := ApplyPackageUpdates(dir, plan, false)

			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want one that contains %q", err, tc.wantErr)
			}
			if tc.file != "" {
				assertFile(t, dir, tc.file, tc.content)
			}
		})
	}
}

func TestApplyPackageUpdates_UnreadableConfig_Error(t *testing.T) {
	if os.Geteuid() == 0 || runtime.GOOS == "windows" {
		t.Skip("needs a file the process cannot read")
	}
	dir := t.TempDir()
	writeFile(t, dir, "marketplace.yml", "name: demo\n")
	if err := os.Chmod(filepath.Join(dir, "marketplace.yml"), 0o200); err != nil {
		t.Fatal(err)
	}

	err := ApplyPackageUpdates(dir, []PackageUpdate{{Action: UpdateApply, NewRef: shaB}}, false)

	if err == nil || !strings.Contains(err.Error(), "read ") {
		t.Fatalf("error = %v, want a read error", err)
	}
}

// The schema trims a value, so one with outer white space reads back as a
// different string than the plan holds.
func TestApplyPackageUpdates_ValueDoesNotReadBackAsPlanned_NothingWritten(t *testing.T) {
	dir := t.TempDir()
	content := apmYML("    - name: tool\n      source: owner/tool\n      ref: " + shaA + "\n")
	writeFile(t, dir, "apm.yml", content)
	mtime := backdate(t, dir, "apm.yml")

	err := ApplyPackageUpdates(dir, []PackageUpdate{{
		Index:   0,
		Package: PackageEntry{Name: "tool", Source: "owner/tool", Ref: shaA},
		Action:  UpdateApply,
		NewRef:  shaB + " ",
	}}, false)

	if err == nil || !strings.Contains(err.Error(), "did not produce exactly the planned values") {
		t.Fatalf("error = %v, want the planned-values mismatch", err)
	}
	assertNotWritten(t, dir, "apm.yml", content, mtime)
}

func TestPackageUpdate_KeepsTheConfigFileMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no permission bits for Chmod to set beyond read-only")
	}
	for _, mode := range []os.FileMode{0o644, 0o600, 0o664} {
		t.Run(mode.String(), func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, "apm.yml", apmYML("    - name: tool\n      source: owner/tool\n      ref: "+shaA+"\n"))
			path := filepath.Join(dir, "apm.yml")
			if err := os.Chmod(path, mode); err != nil {
				t.Fatal(err)
			}
			lister := &sourceLister{refs: map[string][]semver.TagInfo{"owner/tool": {headRef(shaB)}}}

			if _, err := updatePackages(t, dir, nil, false, lister); err != nil {
				t.Fatalf("update: %v", err)
			}

			assertFile(t, dir, "apm.yml", apmYML("    - name: tool\n      source: owner/tool\n      ref: "+shaB+"\n"))
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if got := info.Mode().Perm(); got != mode {
				t.Errorf("mode after the update = %v, want %v", got, mode)
			}
			assertNoTempFiles(t, dir)
		})
	}
}

func assertNoTempFiles(t *testing.T, dir string) {
	t.Helper()
	left, err := filepath.Glob(filepath.Join(dir, "*.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Errorf("temp files left in %s: %v", dir, left)
	}
}

// A dry run goes through every step of a real run but the write, so it
// refuses what a real run refuses, with the same message.
func TestApplyPackageUpdates_DryRun_SameErrorsAsARealRun(t *testing.T) {
	block := apmYML("    - name: tool\n      source: owner/tool\n      ref: " + shaA + "\n")
	plan := []PackageUpdate{{
		Index:   0,
		Package: PackageEntry{Name: "tool", Source: "owner/tool", Ref: shaA},
		Action:  UpdateApply,
		NewRef:  shaB,
	}}
	cases := []struct {
		name, content, wantErr string
		validate               func([]byte, []string) error
	}{
		{
			name:    "entry cannot be edited in place",
			content: apmYML("    - {name: tool, source: owner/tool, ref: " + shaA + "}\n"),
			wantErr: "cannot update package 'tool' in place: its ref is not a single-line value of a block mapping in ",
		},
		{
			name:    "config changed after the plan",
			content: apmYML("    - name: tool\n      source: owner/tool\n      ref: " + shaC + "\n"),
			wantErr: "changed while the update was resolved; run the command again",
		},
		{
			name:     "validation fails",
			content:  block,
			wantErr:  "edit produced an invalid config, aborting without writing: forced validation failure for test",
			validate: func([]byte, []string) error { return fmt.Errorf("forced validation failure for test") },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, "apm.yml", tc.content)
			mtime := backdate(t, dir, "apm.yml")
			if tc.validate != nil {
				origValidate := packageEditValidate
				packageEditValidate = tc.validate
				t.Cleanup(func() { packageEditValidate = origValidate })
			}

			dryErr := ApplyPackageUpdates(dir, plan, true)
			realErr := ApplyPackageUpdates(dir, plan, false)

			if dryErr == nil || !strings.Contains(dryErr.Error(), tc.wantErr) {
				t.Errorf("dry-run error = %v, want one that contains %q", dryErr, tc.wantErr)
			}
			if realErr == nil || dryErr == nil || dryErr.Error() != realErr.Error() {
				t.Errorf("dry-run error = %v, real-run error = %v; want the same text", dryErr, realErr)
			}
			assertNotWritten(t, dir, "apm.yml", tc.content, mtime)
		})
	}
}

func TestApplyPackageUpdates_DryRun_ValidPlan_NothingWritten(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no permission bits for Chmod to set beyond read-only")
	}
	dir := t.TempDir()
	content := apmYML("    - name: tool\n      source: owner/tool\n      ref: " + shaA + "\n")
	writeFile(t, dir, "apm.yml", content)
	path := filepath.Join(dir, "apm.yml")
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	mtime := backdate(t, dir, "apm.yml")

	err := ApplyPackageUpdates(dir, []PackageUpdate{{
		Index:   0,
		Package: PackageEntry{Name: "tool", Source: "owner/tool", Ref: shaA},
		Action:  UpdateApply,
		NewRef:  shaB,
	}}, true)

	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	assertNotWritten(t, dir, "apm.yml", content, mtime)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o640 {
		t.Errorf("mode after the dry run = %v, want -rw-r-----", got)
	}
}

func TestWriteConfigKeepingModeAndOwner_Failures_LeaveNoTempFile(t *testing.T) {
	t.Run("the target does not exist", func(t *testing.T) {
		dir := t.TempDir()
		err := writeConfigKeepingModeAndOwner(filepath.Join(dir, "apm.yml"), []byte("x"))
		if err == nil || !strings.HasPrefix(err.Error(), "stat ") {
			t.Fatalf("error = %v, want a stat error", err)
		}
		assertNoTempFiles(t, dir)
	})
	t.Run("the temp file cannot be created", func(t *testing.T) {
		// A name this long exists, but the temp name built from it is past
		// the 255-byte limit of a file name.
		dir := t.TempDir()
		name := strings.Repeat("n", 250)
		writeFile(t, dir, name, "old")
		err := writeConfigKeepingModeAndOwner(filepath.Join(dir, name), []byte("new"))
		if err == nil || !strings.HasPrefix(err.Error(), "create temp file for ") {
			t.Fatalf("error = %v, want a create-temp error", err)
		}
		assertFile(t, dir, name, "old")
	})
	t.Run("the rename fails", func(t *testing.T) {
		// A directory with an entry cannot be replaced by a file.
		dir := t.TempDir()
		target := filepath.Join(dir, "apm.yml")
		if err := os.Mkdir(target, 0o755); err != nil {
			t.Fatal(err)
		}
		writeFile(t, target, "keep", "x")
		err := writeConfigKeepingModeAndOwner(target, []byte("new"))
		if err == nil || !strings.HasPrefix(err.Error(), "commit write to ") {
			t.Fatalf("error = %v, want a commit error", err)
		}
		assertNoTempFiles(t, dir)
		assertFile(t, target, "keep", "x")
	})
}
