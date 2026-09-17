package authoring

import (
	"strings"
	"testing"
)

// SPEC marketplace-check-outdated-fixes §A (SC-F1): a lowercase 40-hex pin
// is matched against the commit column only; a ref whose *name* equals the
// SHA but points at another commit must not spare the probe.

type countingProber struct {
	calls int
	next  CommitProber
	found bool
}

func (p *countingProber) HasCommit(source, sha string) (bool, error) {
	p.calls++
	if p.next != nil {
		return p.next.HasCommit(source, sha)
	}
	return p.found, nil
}

// absentSHA is a lowercase 40-hex that no fixture repository contains.
var absentSHA = "0123456789abcdef0123456789abcdef01234567"

func TestCheckPackages_ShaPin_RefNamedLikeSha_DifferentCommit_Probes(t *testing.T) {
	dir := t.TempDir()
	initGitRepoWithTags(t, dir)
	commitC := gitCmd(t, dir, "rev-parse", "HEAD")
	gitCmd(t, dir, "branch", absentSHA, commitC)

	prober := &countingProber{}
	deps := CheckDeps{Lister: gitRefLister{}, Prober: prober, Manifest: panicManifestFetcher{}}
	r := singleResult(t, dir, PackageEntry{Name: "tool", Source: dir, Ref: absentSHA}, deps)

	if prober.calls != 1 {
		t.Errorf("probe calls = %d, want 1: a name-only match must not spare the probe", prober.calls)
	}
	if r.Err == nil || r.Err.Error() != "Ref '"+absentSHA+"' not found" || r.RefOK || !r.Reachable {
		t.Errorf("result = %+v, want Ref '%s' not found, Reachable, !RefOK", r, absentSHA)
	}

	t.Run("WithVersion", func(t *testing.T) {
		prober := &countingProber{}
		deps := CheckDeps{Lister: gitRefLister{}, Prober: prober, Manifest: gitManifestVersionFetcher{}}
		r := singleResult(t, dir, PackageEntry{Name: "tool", Source: dir, Ref: absentSHA, Version: "1.0.0"}, deps)

		if r.Err == nil || r.Err.Error() != "Ref '"+absentSHA+"' not found" || !r.Reachable {
			t.Errorf("result = %+v, want Ref '%s' not found with Reachable", r, absentSHA)
		}
		if r.Err != nil && strings.Contains(r.Err.Error(), "vanished") {
			t.Errorf("err = %v, want the probe verdict, not a manifest fetch failure", r.Err)
		}
	})

	t.Run("Exists", func(t *testing.T) {
		dir := t.TempDir()
		initGitRepoWithTags(t, dir)
		orphan := gitCmd(t, dir, "rev-parse", "HEAD")
		tip := addCommit(t, dir, "second")
		// The SHA is read before the same-named branch exists; rev-parse on
		// it afterwards would print an ambiguity warning into the value.
		gitCmd(t, dir, "branch", orphan, tip)

		prober := &countingProber{next: gitCommitProber{}}
		deps := CheckDeps{Lister: gitRefLister{}, Prober: prober, Manifest: panicManifestFetcher{}}
		wantPass(t, singleResult(t, dir, PackageEntry{Name: "tool", Source: dir, Ref: orphan}, deps))
		if prober.calls != 1 {
			t.Errorf("probe calls = %d, want 1", prober.calls)
		}
	})
}
