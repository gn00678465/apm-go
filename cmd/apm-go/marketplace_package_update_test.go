package main

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/apm-go/apm/internal/marketplace/authoring"
	"github.com/apm-go/apm/internal/semver"
	"github.com/spf13/pflag"
)

// `marketplace package update` (issue #25) at the command layer: every
// output line and the exit code. The remote is a canned RefLister; no test
// here starts git.

const (
	updShaA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	updShaB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	updShaC = "cccccccccccccccccccccccccccccccccccccccc"
)

type cannedRefLister struct {
	refs map[string][]semver.TagInfo
	errs map[string]error
}

func (c cannedRefLister) ListRefs(source string) ([]semver.TagInfo, error) {
	if err := c.errs[source]; err != nil {
		return nil, err
	}
	return c.refs[source], nil
}

func withCannedRefLister(t *testing.T, lister cannedRefLister) {
	t.Helper()
	orig := authoring.DefaultRefLister
	authoring.DefaultRefLister = lister
	t.Cleanup(func() { authoring.DefaultRefLister = orig })
}

// cannedSubdirObjects answers the subdir comparison of issue #39 per commit:
// an id from ids, and "no such path" for a commit not in it.
type cannedSubdirObjects struct{ ids map[string]string }

func (c cannedSubdirObjects) SubdirObjectID(_, commit, _ string) (string, bool, error) {
	id, ok := c.ids[commit]
	return id, ok, nil
}

func withCannedSubdirObjects(t *testing.T, ids map[string]string) {
	t.Helper()
	orig := authoring.DefaultSubdirObjectReader
	authoring.DefaultSubdirObjectReader = cannedSubdirObjects{ids: ids}
	t.Cleanup(func() { authoring.DefaultSubdirObjectReader = orig })
}

func updTag(name, sha string) semver.TagInfo {
	return semver.TagInfo{Name: name, Commit: sha, Ref: "refs/tags/" + name}
}

func updHead(sha string) semver.TagInfo {
	return semver.TagInfo{Name: "HEAD", Commit: sha, Ref: "HEAD"}
}

// updateFixture is three entries: a SHA pin with a version, a SHA pin with
// no version, and a local package.
const updateFixture = "    - name: tool\n      source: owner/tool\n      version: 1.0.0\n      ref: " + updShaA + " # release\n" +
	"    - name: tip\n      source: owner/tip\n      ref: " + updShaA + "\n" +
	"    - name: local\n      source: ./pkgs/local\n"

const updateFixtureHeader = "name: demo\nversion: 1.0.0\nmarketplace:\n  owner:\n    name: acme\n  packages:\n"

func updateRemote() cannedRefLister {
	return cannedRefLister{refs: map[string][]semver.TagInfo{
		"owner/tool": {updHead(updShaC), updTag("v1.0.0", updShaA), updTag("v2.0.0", updShaB)},
		"owner/tip":  {updHead(updShaC)},
	}}
}

func readApmYML(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("apm.yml")
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestMarketplacePackageUpdate_UpdatesEveryUpgradableEntry(t *testing.T) {
	chdirTemp(t)
	writeOutdatedFixture(t, updateFixture)
	withCannedRefLister(t, updateRemote())

	out, err := runMarketplaceCmd(t, "package", "update")

	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	want := " + Updated package 'tool': version 1.0.0 -> 2.0.0, ref aaaaaaaaaaaa -> bbbbbbbbbbbb\n" +
		" + Updated package 'tip': ref aaaaaaaaaaaa -> cccccccccccc\n" +
		" i 2 package(s) updated\n"
	if out != want {
		t.Errorf("output =\n%s\nwant\n%s", out, want)
	}
	wantFile := updateFixtureHeader +
		"    - name: tool\n      source: owner/tool\n      version: 2.0.0\n      ref: " + updShaB + " # release\n" +
		"    - name: tip\n      source: owner/tip\n      ref: " + updShaC + "\n" +
		"    - name: local\n      source: ./pkgs/local\n"
	if got := readApmYML(t); got != wantFile {
		t.Errorf("apm.yml =\n%s\nwant\n%s", got, wantFile)
	}
}

func TestMarketplacePackageUpdate_DryRun_PrintsThePlanAndLeavesTheFile(t *testing.T) {
	chdirTemp(t)
	writeOutdatedFixture(t, updateFixture)
	withCannedRefLister(t, updateRemote())

	out, err := runMarketplaceCmd(t, "package", "update", "--dry-run")

	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	want := " i Would update package 'tool': version 1.0.0 -> 2.0.0, ref aaaaaaaaaaaa -> bbbbbbbbbbbb\n" +
		" i Would update package 'tip': ref aaaaaaaaaaaa -> cccccccccccc\n" +
		" i 2 package(s) would be updated\n"
	if out != want {
		t.Errorf("output =\n%s\nwant\n%s", out, want)
	}
	if got := readApmYML(t); got != updateFixtureHeader+updateFixture {
		t.Errorf("--dry-run changed apm.yml:\n%s", got)
	}
}

func TestMarketplacePackageUpdate_NothingToUpdate(t *testing.T) {
	chdirTemp(t)
	writeOutdatedFixture(t, updateFixture)
	withCannedRefLister(t, cannedRefLister{refs: map[string][]semver.TagInfo{
		"owner/tool": {updHead(updShaA), updTag("v1.0.0", updShaA)},
		"owner/tip":  {updHead(updShaA)},
	}})

	out, err := runMarketplaceCmd(t, "package", "update")

	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if want := " i All packages are up to date\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	if got := readApmYML(t); got != updateFixtureHeader+updateFixture {
		t.Errorf("apm.yml changed:\n%s", got)
	}
}

func TestMarketplacePackageUpdate_NamedEntries_SkipsAreReported(t *testing.T) {
	chdirTemp(t)
	writeOutdatedFixture(t, updateFixture)
	withCannedRefLister(t, cannedRefLister{refs: map[string][]semver.TagInfo{
		"owner/tool": {updHead(updShaC), updTag("v1.0.0", updShaA)},
		"owner/tip":  {updHead(updShaC)},
	}})

	out, err := runMarketplaceCmd(t, "package", "update", "local", "TOOL")

	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	want := " i Skipped package 'tool': already up to date\n" +
		" i Skipped package 'local': local package; skipped\n" +
		" i All packages are up to date\n"
	if out != want {
		t.Errorf("output =\n%s\nwant\n%s", out, want)
	}
	if got := readApmYML(t); got != updateFixtureHeader+updateFixture {
		t.Errorf("apm.yml changed (the unnamed 'tip' entry must stay):\n%s", got)
	}
}

func TestMarketplacePackageUpdate_ResolutionFailure_ExitsCode2_NothingWritten(t *testing.T) {
	chdirTemp(t)
	writeOutdatedFixture(t, updateFixture)
	withCannedRefLister(t, cannedRefLister{errs: map[string]error{
		"owner/tool": errors.New("git ls-remote https://github.com/owner/tool: denied"),
		"owner/tip":  errors.New("git ls-remote https://github.com/owner/tip: denied"),
	}})

	out, err := runMarketplaceCmd(t, "package", "update")

	want := " x cannot update: package 'tool': git ls-remote https://github.com/owner/tool: denied\n" +
		" x cannot update: package 'tip': git ls-remote https://github.com/owner/tip: denied\n"
	// This harness has no root error handler, so cobra prints the returned
	// error after the command's own lines; the real root prints nothing for
	// a silent exit.
	if err != nil {
		want += "Error: " + err.Error() + "\n"
	}
	if out != want {
		t.Errorf("output =\n%s\nwant\n%s", out, want)
	}
	if got := exitCodeOf(err); got != 2 || !isSilentExit(err) {
		t.Errorf("exit = %d silent=%v, want a silent exit 2 (the lines above are the whole message)", got, isSilentExit(err))
	}
	if got := readApmYML(t); got != updateFixtureHeader+updateFixture {
		t.Errorf("apm.yml changed:\n%s", got)
	}
}

func TestMarketplacePackageUpdate_UnknownName_ExitsCode2(t *testing.T) {
	chdirTemp(t)
	writeOutdatedFixture(t, updateFixture)
	withCannedRefLister(t, updateRemote())

	_, err := runMarketplaceCmd(t, "package", "update", "no-such-package")

	if err == nil || err.Error() != `package "no-such-package" not found` {
		t.Fatalf("err = %v, want package \"no-such-package\" not found", err)
	}
	if got := exitCodeOf(err); got != 2 {
		t.Errorf("exitCodeOf(err) = %d, want 2", got)
	}
	if got := readApmYML(t); got != updateFixtureHeader+updateFixture {
		t.Errorf("apm.yml changed:\n%s", got)
	}
}

func TestMarketplacePackageUpdate_ConfigErrors_FollowOutdated(t *testing.T) {
	t.Run("schema error exits 2", func(t *testing.T) {
		chdirTemp(t)
		writeOutdatedFixture(t, "    - name: bare\n      source: owner/repo\n")
		_, err := runMarketplaceCmd(t, "package", "update")
		_, wantErr := runMarketplaceCmd(t, "outdated")
		if err == nil || wantErr == nil || err.Error() != wantErr.Error() || !strings.HasPrefix(err.Error(), "marketplace config error: ") {
			t.Fatalf("err = %v, want outdated's %v", err, wantErr)
		}
		if got := exitCodeOf(err); got != 2 {
			t.Errorf("exitCodeOf(err) = %d, want 2", got)
		}
	})
	t.Run("no config exits 1", func(t *testing.T) {
		chdirTemp(t)
		_, err := runMarketplaceCmd(t, "package", "update")
		if got := exitCodeOf(err); err == nil || got != 1 {
			t.Errorf("err = %v exit = %d, want an error with exit 1", err, got)
		}
	})
}

func TestMarketplacePackageUpdate_LegacyConfig_Warns(t *testing.T) {
	chdirTemp(t)
	legacy := "name: demo\nversion: 1.0.0\nowner:\n  name: acme\npackages:\n  - name: tip\n    source: owner/tip\n    ref: "
	if err := os.WriteFile("marketplace.yml", []byte(legacy+updShaA+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	withCannedRefLister(t, updateRemote())

	out, err := runMarketplaceCmd(t, "package", "update")

	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	want := " ! reading legacy marketplace.yml; run 'apm-go marketplace migrate' to fold it into apm.yml\n" +
		" + Updated package 'tip': ref aaaaaaaaaaaa -> cccccccccccc\n" +
		" i 1 package(s) updated\n"
	if out != want {
		t.Errorf("output =\n%s\nwant\n%s", out, want)
	}
	data, err := os.ReadFile("marketplace.yml")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != legacy+updShaC+"\n" {
		t.Errorf("marketplace.yml =\n%s", data)
	}
}

func TestMarketplacePackageHelp_ListsUpdate(t *testing.T) {
	out, err := runMarketplaceCmd(t, "package", "--help")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "\n  update ") {
		t.Errorf("`marketplace package --help` does not list update:\n%s", out)
	}
}

func TestMarketplacePackageUpdateCmd_Flags(t *testing.T) {
	cmd := marketplacePackageUpdateCmd()
	got := map[string]string{}
	cmd.Flags().VisitAll(func(f *pflag.Flag) { got[f.Name] = f.Usage })
	if len(got) != 2 || got["dry-run"] == "" {
		t.Errorf("flags = %v, want exactly --dry-run and --include-prerelease", got)
	}
	if want := marketplaceOutdatedCmd().Flags().Lookup("include-prerelease").Usage; got["include-prerelease"] != want {
		t.Errorf("--include-prerelease usage = %q, want outdated's %q", got["include-prerelease"], want)
	}
}

func TestMarketplacePackageUpdate_EntryCannotBeEditedInPlace_ExitsCode2(t *testing.T) {
	chdirTemp(t)
	fixture := "    - {name: tip, source: owner/tip, ref: " + updShaA + "}\n"
	writeOutdatedFixture(t, fixture)
	withCannedRefLister(t, updateRemote())

	_, err := runMarketplaceCmd(t, "package", "update")

	if err == nil || !strings.Contains(err.Error(), "cannot update package 'tip' in place") {
		t.Fatalf("err = %v, want the in-place error for 'tip'", err)
	}
	if got := exitCodeOf(err); got != 2 || isSilentExit(err) {
		t.Errorf("exit = %d silent=%v, want a printed exit 2", got, isSilentExit(err))
	}
	if got := readApmYML(t); got != updateFixtureHeader+fixture {
		t.Errorf("apm.yml changed:\n%s", got)
	}
}

func TestMarketplacePackageUpdate_DryRun_ReportsWhatARealRunWouldRefuse(t *testing.T) {
	chdirTemp(t)
	fixture := "    - {name: tip, source: owner/tip, ref: " + updShaA + "}\n"
	writeOutdatedFixture(t, fixture)
	old := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := os.Chtimes("apm.yml", old, old); err != nil {
		t.Fatal(err)
	}
	withCannedRefLister(t, updateRemote())

	out, err := runMarketplaceCmd(t, "package", "update", "--dry-run")

	want := "Error: cannot update package 'tip' in place: its ref is not a single-line value of a block mapping in apm.yml; edit it by hand\n"
	if out != want {
		t.Errorf("--dry-run output =\n%s\nwant\n%s", out, want)
	}
	if got := exitCodeOf(err); err == nil || got != 2 {
		t.Errorf("--dry-run err = %v exit = %d, want an error with exit 2", err, got)
	}
	realOut, realErr := runMarketplaceCmd(t, "package", "update")
	if realOut != want || exitCodeOf(realErr) != 2 {
		t.Errorf("real run output = %q exit = %d, want the same %q and 2", realOut, exitCodeOf(realErr), want)
	}
	if got := readApmYML(t); got != updateFixtureHeader+fixture {
		t.Errorf("apm.yml changed:\n%s", got)
	}
	info, statErr := os.Stat("apm.yml")
	if statErr != nil {
		t.Fatal(statErr)
	}
	if !info.ModTime().Equal(old) {
		t.Errorf("apm.yml mtime = %v, want %v (the file was written)", info.ModTime(), old)
	}
}

func TestMarketplacePackageUpdate_SymlinkedConfig_RefusedByDryRunAndRealRun(t *testing.T) {
	chdirTemp(t)
	writeOutdatedFixture(t, updateFixture)
	if err := os.Rename("apm.yml", "shared.yml"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("shared.yml", "apm.yml"); err != nil {
		t.Skipf("cannot create a symbolic link here: %v", err)
	}
	withCannedRefLister(t, updateRemote())

	want := "Error: cannot update apm.yml in place: it is a symbolic link; edit the file it points to by hand\n"
	for _, args := range [][]string{{"package", "update", "--dry-run"}, {"package", "update"}} {
		out, err := runMarketplaceCmd(t, args...)
		if out != want {
			t.Errorf("%v output =\n%s\nwant\n%s", args, out, want)
		}
		if got := exitCodeOf(err); err == nil || got != 2 {
			t.Errorf("%v err = %v exit = %d, want an error with exit 2", args, err, got)
		}
	}

	if target, err := os.Readlink("apm.yml"); err != nil || target != "shared.yml" {
		t.Errorf("Readlink(apm.yml) = %q, %v; want the link left as it was", target, err)
	}
	data, err := os.ReadFile("shared.yml")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != updateFixtureHeader+updateFixture {
		t.Errorf("shared.yml changed:\n%s", data)
	}
}

// Issue #39: a SHA pin with a subdir whose content is the same at the moved
// tip is not an update.
const subdirUnchangedFixture = "    - name: mono\n      source: owner/mono\n      ref: " + updShaA + "\n      subdir: plugins/mono\n"

func withUnchangedSubdirRemote(t *testing.T) {
	t.Helper()
	withCannedRefLister(t, cannedRefLister{refs: map[string][]semver.TagInfo{"owner/mono": {updHead(updShaC)}}})
	withCannedSubdirObjects(t, map[string]string{updShaA: "tree1", updShaC: "tree1"})
}

func TestMarketplacePackageUpdate_SubdirUnchangedAtMovedTip_WritesNothing(t *testing.T) {
	for name, tc := range map[string]struct {
		args []string
		want string
	}{
		"AllPackages": {[]string{"package", "update"}, " i All packages are up to date\n"},
		"DryRun":      {[]string{"package", "update", "--dry-run"}, " i All packages are up to date\n"},
		"Named": {[]string{"package", "update", "mono"},
			" i Skipped package 'mono': already up to date\n i All packages are up to date\n"},
		"NamedDryRun": {[]string{"package", "update", "--dry-run", "mono"},
			" i Skipped package 'mono': already up to date\n i All packages are up to date\n"},
	} {
		t.Run(name, func(t *testing.T) {
			chdirTemp(t)
			writeOutdatedFixture(t, subdirUnchangedFixture)
			withUnchangedSubdirRemote(t)

			out, err := runMarketplaceCmd(t, tc.args...)

			if err != nil {
				t.Fatalf("err = %v, want nil", err)
			}
			if out != tc.want {
				t.Errorf("output = %q, want %q", out, tc.want)
			}
			if got := readApmYML(t); got != updateFixtureHeader+subdirUnchangedFixture {
				t.Errorf("apm.yml changed:\n%s", got)
			}
		})
	}
}

func TestMarketplacePackageUpdate_SubdirChangedAtMovedTip_WritesTheTip(t *testing.T) {
	chdirTemp(t)
	writeOutdatedFixture(t, subdirUnchangedFixture)
	withCannedRefLister(t, cannedRefLister{refs: map[string][]semver.TagInfo{"owner/mono": {updHead(updShaC)}}})
	withCannedSubdirObjects(t, map[string]string{updShaA: "tree1", updShaC: "tree2"})

	out, err := runMarketplaceCmd(t, "package", "update")

	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if want := " + Updated package 'mono': ref aaaaaaaaaaaa -> cccccccccccc\n i 1 package(s) updated\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	want := updateFixtureHeader + "    - name: mono\n      source: owner/mono\n      ref: " + updShaC + "\n      subdir: plugins/mono\n"
	if got := readApmYML(t); got != want {
		t.Errorf("apm.yml =\n%s\nwant\n%s", got, want)
	}
}

func TestMarketplacePackageUpdate_SubdirMissingAtTip_ExitsCode2_NothingWritten(t *testing.T) {
	chdirTemp(t)
	writeOutdatedFixture(t, subdirUnchangedFixture)
	withCannedRefLister(t, cannedRefLister{refs: map[string][]semver.TagInfo{"owner/mono": {updHead(updShaC)}}})
	withCannedSubdirObjects(t, map[string]string{updShaA: "tree1"})

	out, err := runMarketplaceCmd(t, "package", "update")

	if got := exitCodeOf(err); got != 2 || !isSilentExit(err) {
		t.Errorf("exit = %d silent=%v, want a silent exit 2", got, isSilentExit(err))
	}
	// runMarketplaceCmd leaves cobra's own "Error:" echo on; the root command silences it.
	if want := " x cannot update: package 'mono': Subdir 'plugins/mono' not found at default branch tip\n"; !strings.HasPrefix(out, want) {
		t.Errorf("output = %q, want it to start with %q", out, want)
	}
	if got := readApmYML(t); got != updateFixtureHeader+subdirUnchangedFixture {
		t.Errorf("apm.yml changed:\n%s", got)
	}
}

func TestMarketplaceOutdated_SubdirUnchangedAtMovedTip_UpToDate(t *testing.T) {
	chdirTemp(t)
	writeOutdatedFixture(t, subdirUnchangedFixture)
	withUnchangedSubdirRemote(t)

	out, err := runMarketplaceCmd(t, "outdated")

	if err != nil {
		t.Fatalf("err = %v, want nil (exit 0)\n%s", err, out)
	}
	var cells []string
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, "mono") {
			continue
		}
		for _, cell := range strings.Split(strings.Trim(line, "│"), "│") {
			cells = append(cells, strings.TrimSpace(cell))
		}
	}
	want := []string{"+", "mono", "aaaaaaaaaaaa", "--", "--", "cccccccccccc", "Tip moved; 'plugins/mono' unchanged"}
	if strings.Join(cells, "|") != strings.Join(want, "|") {
		t.Errorf("row cells = %q, want %q\n%s", cells, want, out)
	}
	if !strings.HasSuffix(out, " i All packages are up to date\n") {
		t.Errorf("output = %q, want it to end with the up-to-date summary", out)
	}
}

func TestMarketplacePackageUpdate_Help_StatesTheSubdirRule(t *testing.T) {
	out, err := runMarketplaceCmd(t, "package", "update", "--help")

	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	for _, want := range []string{"subdir", "nothing is written"} {
		if !strings.Contains(out, want) {
			t.Errorf("--help does not contain %q:\n%s", want, out)
		}
	}
}
