package main

import (
	"errors"
	"os"
	"strings"
	"testing"

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
