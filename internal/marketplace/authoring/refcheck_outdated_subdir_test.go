package authoring

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/apm-go/apm/internal/semver"
)

// Issue #39: a SHA pin with a subdir and no version is upgradable only when
// that path's git object id at the default-branch tip differs from the one
// at the pinned commit. The expected rows are the rule table of the issue's
// brief, not values derived from the implementation.

const subdirPath = "plugins/tool"

// fakeSubdirObjects answers per commit: an id from ids, an error from errs,
// and "no such path" for a commit in neither.
type fakeSubdirObjects struct {
	ids   map[string]string
	errs  map[string]error
	calls []string
}

func (f *fakeSubdirObjects) SubdirObjectID(source, commit, subdir string) (string, bool, error) {
	f.calls = append(f.calls, commit+":"+subdir)
	if err := f.errs[commit]; err != nil {
		return "", false, err
	}
	id, ok := f.ids[commit]
	return id, ok, nil
}

type panicSubdirObjects struct{}

func (panicSubdirObjects) SubdirObjectID(source, commit, subdir string) (string, bool, error) {
	panic("SubdirObjectID must not be called for this entry")
}

func subdirPin() PackageEntry {
	return PackageEntry{Name: "tool", Source: "owner/repo", Ref: shaA, Subdir: subdirPath}
}

func tipMoved() mapRefLister {
	return mapRefLister{refs: []semver.TagInfo{headRef(shaB)}}
}

func outdatedRowWith(t *testing.T, pkg PackageEntry, deps OutdatedDeps, offline bool) OutdatedRow {
	t.Helper()
	rows := OutdatedPackagesWith(&AuthoringConfig{Packages: []PackageEntry{pkg}}, deps, offline, false, nil)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	return rows[0]
}

func assertTargetRef(t *testing.T, r OutdatedRow, want string) {
	t.Helper()
	if r.TargetRef != want || r.TargetVersion != "" {
		t.Errorf("TargetRef = %q, TargetVersion = %q, want %q and \"\"", r.TargetRef, r.TargetVersion, want)
	}
}

func assertCallCount(t *testing.T, f *fakeSubdirObjects, min, max int) {
	t.Helper()
	if n := len(f.calls); n < min || n > max {
		t.Errorf("SubdirObjectID calls = %v, want between %d and %d", f.calls, min, max)
	}
}

// ── rule 2 ───────────────────────────────────────────────────────────────

func TestOutdatedSubdirPin_AtTip_UpToDateWithoutReadingObjects(t *testing.T) {
	deps := OutdatedDeps{Lister: mapRefLister{refs: []semver.TagInfo{headRef(shaA)}}, Objects: panicSubdirObjects{}}

	r := outdatedRowWith(t, subdirPin(), deps, false)

	assertRow(t, r, shaA[:12], "--", shaA[:12], "[+]", "", false)
	assertTargetRef(t, r, "")
}

// ── rule 3 ───────────────────────────────────────────────────────────────

func TestOutdatedSubdirPin_InvalidSubdir_IconXWithoutReadingObjects(t *testing.T) {
	pkg := subdirPin()
	pkg.Subdir = "../x"

	r := outdatedRowWith(t, pkg, OutdatedDeps{Lister: tipMoved(), Objects: panicSubdirObjects{}}, false)

	assertRow(t, r, shaA[:12], "--", shaB[:12], "[x]", `invalid subdir "../x": segment ".." is a traversal sequence`, false)
	assertTargetRef(t, r, "")
}

// ── rule 4 ───────────────────────────────────────────────────────────────

func TestOutdatedSubdirPin_ReadFails_IconX(t *testing.T) {
	long := "git fetch https://github.com/owner/repo.git: timed out after 30s"
	for name, tc := range map[string]struct {
		errs map[string]error
		note string
	}{
		"PinnedCommit": {map[string]error{shaA: errors.New("git fetch: connection reset")}, "git fetch: connection reset"},
		"Tip":          {map[string]error{shaB: errors.New("git fetch: connection reset")}, "git fetch: connection reset"},
		"NoteCutTo60":  {map[string]error{shaA: errors.New(long), shaB: errors.New(long)}, "git fetch https://github.com/owner/repo.git: timed out after"},
	} {
		t.Run(name, func(t *testing.T) {
			objects := &fakeSubdirObjects{ids: map[string]string{shaA: "tree1", shaB: "tree2"}, errs: tc.errs}

			r := outdatedRowWith(t, subdirPin(), OutdatedDeps{Lister: tipMoved(), Objects: objects}, false)

			assertRow(t, r, shaA[:12], "--", shaB[:12], "[x]", tc.note, false)
			assertTargetRef(t, r, "")
			assertCallCount(t, objects, 1, 2)
		})
	}
}

// ── rule 5 ───────────────────────────────────────────────────────────────

func TestOutdatedSubdirPin_PinnedCommitNotOnRemote_IconX(t *testing.T) {
	objects := &fakeSubdirObjects{
		ids:  map[string]string{shaB: "tree2"},
		errs: map[string]error{shaA: fmt.Errorf("probe: %w", errRefNotOnRemote)},
	}

	r := outdatedRowWith(t, subdirPin(), OutdatedDeps{Lister: tipMoved(), Objects: objects}, false)

	assertRow(t, r, shaA[:12], "--", shaB[:12], "[x]", "Ref 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa' not found", false)
	assertTargetRef(t, r, "")
	assertCallCount(t, objects, 1, 2)
}

// ── rule 6 ───────────────────────────────────────────────────────────────

func TestOutdatedSubdirPin_SubdirMissingAtTip_IconX(t *testing.T) {
	objects := &fakeSubdirObjects{ids: map[string]string{shaA: "tree1"}}

	r := outdatedRowWith(t, subdirPin(), OutdatedDeps{Lister: tipMoved(), Objects: objects}, false)

	assertRow(t, r, shaA[:12], "--", shaB[:12], "[x]", "Subdir 'plugins/tool' not found at default branch tip", false)
	assertTargetRef(t, r, "")
	assertCallCount(t, objects, 1, 2)
}

// ── rule 7 ───────────────────────────────────────────────────────────────

func TestOutdatedSubdirPin_SubdirMissingAtPinnedCommit_IconX(t *testing.T) {
	objects := &fakeSubdirObjects{ids: map[string]string{shaB: "tree2"}}

	r := outdatedRowWith(t, subdirPin(), OutdatedDeps{Lister: tipMoved(), Objects: objects}, false)

	assertRow(t, r, shaA[:12], "--", shaB[:12], "[x]", "Subdir 'plugins/tool' not found at ref", false)
	assertTargetRef(t, r, "")
	assertCallCount(t, objects, 1, 2)
}

// ── rule 8 ───────────────────────────────────────────────────────────────

func TestOutdatedSubdirPin_SameObject_UpToDate(t *testing.T) {
	objects := &fakeSubdirObjects{ids: map[string]string{shaA: "tree1", shaB: "tree1"}}

	r := outdatedRowWith(t, subdirPin(), OutdatedDeps{Lister: tipMoved(), Objects: objects}, false)

	assertRow(t, r, shaA[:12], "--", shaB[:12], "[+]", "Tip moved; 'plugins/tool' unchanged", false)
	assertTargetRef(t, r, "")
	assertCallCount(t, objects, 2, 2)

	t.Run("NoteIsNotCut", func(t *testing.T) {
		pkg := subdirPin()
		pkg.Subdir = "plugins/" + strings.Repeat("n", 70)
		objects := &fakeSubdirObjects{ids: map[string]string{shaA: "tree1", shaB: "tree1"}}

		r := outdatedRowWith(t, pkg, OutdatedDeps{Lister: tipMoved(), Objects: objects}, false)

		if want := "Tip moved; 'plugins/" + strings.Repeat("n", 70) + "' unchanged"; r.Note != want {
			t.Errorf("Note = %q, want %q", r.Note, want)
		}
	})
}

// ── rule 9 ───────────────────────────────────────────────────────────────

func TestOutdatedSubdirPin_DifferentObject_Upgradable(t *testing.T) {
	objects := &fakeSubdirObjects{ids: map[string]string{shaA: "tree1", shaB: "tree2"}}

	r := outdatedRowWith(t, subdirPin(), OutdatedDeps{Lister: tipMoved(), Objects: objects}, false)

	assertRow(t, r, shaA[:12], "--", shaB[:12], "[!]", "Default branch tip moved", true)
	assertTargetRef(t, r, shaB)
	assertCallCount(t, objects, 2, 2)
}

// ── rows that never read an object ───────────────────────────────────────

func TestOutdatedSubdirPin_ObjectsNeverReadOutsideTheRule(t *testing.T) {
	versioned := subdirPin()
	versioned.Version = "1.0.0"
	noSubdir := subdirPin()
	noSubdir.Subdir = ""

	for name, tc := range map[string]struct {
		pkg     PackageEntry
		lister  RefLister
		offline bool
		status  string
		note    string
	}{
		"Offline":        {subdirPin(), panicLister{}, true, "[x]", "Offline mode: no cached refs for 'owner/repo' (package 'tool"},
		"ListRefsFails":  {subdirPin(), mapRefLister{err: errors.New("denied")}, false, "[x]", "denied"},
		"NoHead":         {subdirPin(), mapRefLister{refs: []semver.TagInfo{tagRef("v1.0.0", shaC)}}, false, "[x]", "Remote advertised no HEAD"},
		"NoSubdir":       {noSubdir, tipMoved(), false, "[!]", "Default branch tip moved"},
		"DisplayVersion": {versioned, mapRefLister{refs: []semver.TagInfo{headRef(shaB), tagRef("v1.0.0", shaA), tagRef("v1.1.0", shaB)}}, false, "[!]", ""},
	} {
		t.Run(name, func(t *testing.T) {
			r := outdatedRowWith(t, tc.pkg, OutdatedDeps{Lister: tc.lister, Objects: panicSubdirObjects{}}, tc.offline)

			if r.Status != tc.status || r.Note != tc.note {
				t.Errorf("row = {Status:%q Note:%q}, want {Status:%q Note:%q}", r.Status, r.Note, tc.status, tc.note)
			}
		})
	}
}

// ── package update ───────────────────────────────────────────────────────

const subdirPinYML = "    - name: tool\n      source: owner/repo\n      ref: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n      subdir: plugins/tool\n"

func planWith(t *testing.T, dir string, objects SubdirObjectReader) ([]PackageUpdate, error) {
	t.Helper()
	cfg, _, err := LoadAuthoringConfig(dir)
	if err != nil {
		t.Fatalf("LoadAuthoringConfig: %v", err)
	}
	updates, err := PlanPackageUpdatesWith(cfg, nil, false, OutdatedDeps{Lister: tipMoved(), Objects: objects})
	if err != nil {
		return nil, err
	}
	return updates, ApplyPackageUpdates(dir, updates, false)
}

func writeApmYML(t *testing.T, packages string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "apm.yml"), []byte(apmYML(packages)), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestPlanPackageUpdates_SubdirPin_SameObject_SkipsAndWritesNothing(t *testing.T) {
	dir := writeApmYML(t, subdirPinYML)

	updates, err := planWith(t, dir, &fakeSubdirObjects{ids: map[string]string{shaA: "tree1", shaB: "tree1"}})

	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if len(updates) != 1 || updates[0].Action != UpdateSkip || updates[0].Note != "already up to date" || updates[0].NewRef != "" {
		t.Errorf("updates = %+v, want one UpdateSkip \"already up to date\" with no NewRef", updates)
	}
	assertFile(t, dir, "apm.yml", apmYML(subdirPinYML))
}

func TestPlanPackageUpdates_SubdirPin_DifferentObject_WritesTheTip(t *testing.T) {
	dir := writeApmYML(t, subdirPinYML)

	updates, err := planWith(t, dir, &fakeSubdirObjects{ids: map[string]string{shaA: "tree1", shaB: "tree2"}})

	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if len(updates) != 1 || updates[0].Action != UpdateApply || updates[0].NewRef != shaB {
		t.Errorf("updates = %+v, want one UpdateApply to %s", updates, shaB)
	}
	assertFile(t, dir, "apm.yml", apmYML("    - name: tool\n      source: owner/repo\n      ref: bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\n      subdir: plugins/tool\n"))
}

func TestPlanPackageUpdates_SubdirPin_Unreadable_FailsTheWholeBatch(t *testing.T) {
	// A second entry with no subdir is upgradable on its own; it must not be
	// written when the subdir pin beside it cannot be resolved.
	const other = "    - name: other\n      source: owner/other\n      ref: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n"

	for name, tc := range map[string]struct {
		subdir  string
		objects *fakeSubdirObjects
		line    string
	}{
		"InvalidSubdir": {"../x", &fakeSubdirObjects{},
			`cannot update: package 'tool': invalid subdir "../x": segment ".." is a traversal sequence`},
		"ReadFails": {subdirPath, &fakeSubdirObjects{errs: map[string]error{shaA: errors.New("git fetch: connection reset")}},
			"cannot update: package 'tool': git fetch: connection reset"},
		"CommitNotOnRemote": {subdirPath, &fakeSubdirObjects{errs: map[string]error{shaA: errRefNotOnRemote}},
			"cannot update: package 'tool': Ref 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa' not found"},
		"MissingAtTip": {subdirPath, &fakeSubdirObjects{ids: map[string]string{shaA: "tree1"}},
			"cannot update: package 'tool': Subdir 'plugins/tool' not found at default branch tip"},
		"MissingAtPinnedCommit": {subdirPath, &fakeSubdirObjects{ids: map[string]string{shaB: "tree2"}},
			"cannot update: package 'tool': Subdir 'plugins/tool' not found at ref"},
	} {
		t.Run(name, func(t *testing.T) {
			packages := "    - name: tool\n      source: owner/repo\n      ref: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n      subdir: " + tc.subdir + "\n" + other
			dir := writeApmYML(t, packages)

			updates, err := planWith(t, dir, tc.objects)

			var unresolved *UpdateResolutionError
			if !errors.As(err, &unresolved) {
				t.Fatalf("err = %v (updates %+v), want *UpdateResolutionError", err, updates)
			}
			if len(unresolved.Lines) != 1 || unresolved.Lines[0] != tc.line {
				t.Errorf("Lines = %q, want [%q]", unresolved.Lines, tc.line)
			}
			assertFile(t, dir, "apm.yml", apmYML(packages))
		})
	}
}

// ── the production reader against a local repository ─────────────────────

// subdirRepo has three commits: base, one that changes a file outside
// plugins/tool, and one that changes a file inside it.
func subdirRepo(t *testing.T) (dir, base, outside, inside string) {
	t.Helper()
	dir = t.TempDir()
	initGitRepoWithTags(t, dir)
	writeManifest(t, dir, "plugins/tool/SKILL.md", "v1")
	base = addCommit(t, dir, "base")
	outside = addCommit(t, dir, "outside")
	writeManifest(t, dir, "plugins/tool/SKILL.md", "v2")
	inside = addCommit(t, dir, "inside")
	return dir, base, outside, inside
}

func assertNoScratchLeft(t *testing.T, scratch string) {
	t.Helper()
	entries, err := os.ReadDir(scratch)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("%d scratch repositories left behind", len(entries))
	}
}

func TestGitSubdirObjectReader_AgainstLocalRepository(t *testing.T) {
	scratch := t.TempDir()
	orig := scratchTempRoot
	scratchTempRoot = scratch
	t.Cleanup(func() { scratchTempRoot = orig })

	dir, base, outside, inside := subdirRepo(t)
	reader := gitSubdirObjectReader{}
	read := func(t *testing.T, commit, subdir string) (string, bool) {
		t.Helper()
		id, found, err := reader.SubdirObjectID(dir, commit, subdir)
		if err != nil {
			t.Fatalf("SubdirObjectID(%s, %q): %v", commit, subdir, err)
		}
		return id, found
	}

	t.Run("SameTree", func(t *testing.T) {
		want := gitCmd(t, dir, "rev-parse", base+":plugins/tool")
		for _, commit := range []string{base, outside} {
			if id, found := read(t, commit, subdirPath); !found || id != want {
				t.Errorf("id at %s = %q found=%v, want %q", commit, id, found, want)
			}
		}
	})

	t.Run("DifferentTree", func(t *testing.T) {
		want := gitCmd(t, dir, "rev-parse", inside+":plugins/tool")
		id, found := read(t, inside, subdirPath)
		if !found || id != want {
			t.Errorf("id = %q found=%v, want %q", id, found, want)
		}
		if before, _ := read(t, base, subdirPath); before == id {
			t.Errorf("id %q is the same before and after a change inside the subdir", id)
		}
	})

	t.Run("FileHasAnID", func(t *testing.T) {
		want := gitCmd(t, dir, "rev-parse", base+":plugins/tool/SKILL.md")
		if id, found := read(t, base, "plugins/tool/SKILL.md"); !found || id != want {
			t.Errorf("id = %q found=%v, want %q", id, found, want)
		}
	})

	t.Run("PathMissing", func(t *testing.T) {
		// "hooks" exists on disk inside the bare scratch repository; git
		// words that case differently from a path that exists nowhere.
		for _, subdir := range []string{"plugins/absent", "hooks"} {
			if id, found := read(t, base, subdir); found || id != "" {
				t.Errorf("%q: id = %q found=%v, want \"\" false", subdir, id, found)
			}
		}
	})

	t.Run("CommitMissing", func(t *testing.T) {
		_, found, err := reader.SubdirObjectID(dir, strings.Repeat("0", 39)+"1", subdirPath)
		if !errors.Is(err, errRefNotOnRemote) || found {
			t.Errorf("err = %v found=%v, want errRefNotOnRemote", err, found)
		}
	})

	assertNoScratchLeft(t, scratch)
}

func TestNewRevParseCmd_ShapeAndSecureEnv(t *testing.T) {
	cmd := newRevParseCmd(context.Background(), "/scratch", subdirPath)

	if got, want := strings.Join(cmd.Args, " "), "git -C /scratch rev-parse FETCH_HEAD:plugins/tool"; got != want {
		t.Errorf("args = %q, want %q", got, want)
	}
	if cmd.WaitDelay != subprocessWaitDelay {
		t.Errorf("WaitDelay = %v, want %v", cmd.WaitDelay, subprocessWaitDelay)
	}
	for _, want := range []string{"GIT_TERMINAL_PROMPT=0", "LC_ALL=C"} {
		if !slices.Contains(cmd.Env, want) {
			t.Errorf("env is missing %q", want)
		}
	}
}

func TestObjectIDAtFetchHead_GitFailure_IsAnErrorNotAMissingPath(t *testing.T) {
	// A bare repository that never fetched has no FETCH_HEAD to resolve.
	dir := t.TempDir()
	gitCmd(t, dir, "init", "-q", "--bare")

	id, found, err := objectIDAtFetchHead(dir, subdirPath)

	if err == nil || !strings.HasPrefix(err.Error(), "git rev-parse plugins/tool: ") || strings.Contains(err.Error(), "timed out") {
		t.Errorf("err = %v, want git's own failure behind \"git rev-parse plugins/tool: \"", err)
	}
	if id != "" || found {
		t.Errorf("id = %q found=%v, want \"\" false", id, found)
	}
}

func TestObjectIDAtFetchHead_TimesOut(t *testing.T) {
	fakeGitDir := buildFakeGit(t)
	t.Setenv("PATH", fakeGitDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKEGIT_SLEEP_MS", "5000")
	orig := listRefsTimeout
	listRefsTimeout = 200 * time.Millisecond
	t.Cleanup(func() { listRefsTimeout = orig })

	start := time.Now()
	id, found, err := objectIDAtFetchHead(t.TempDir(), subdirPath)

	if err == nil || err.Error() != "git rev-parse plugins/tool: timed out after 200ms" {
		t.Errorf("err = %v, want \"git rev-parse plugins/tool: timed out after 200ms\"", err)
	}
	if id != "" || found {
		t.Errorf("id = %q found=%v, want \"\" false", id, found)
	}
	if time.Since(start) > 3*time.Second {
		t.Errorf("took %s; the timeout did not fire", time.Since(start))
	}
}

// The whole rule through the production seams: one row per commit pair.
func TestOutdatedSubdirPin_ProductionSeams(t *testing.T) {
	dir, base, outside, inside := subdirRepo(t)
	deps := OutdatedDeps{Lister: gitRefLister{}, Objects: gitSubdirObjectReader{}}
	pkg := PackageEntry{Name: "tool", Source: dir, Ref: base, Subdir: subdirPath}

	gitCmd(t, dir, "reset", "-q", "--hard", outside)
	r := outdatedRowWith(t, pkg, deps, false)
	assertRow(t, r, base[:12], "--", outside[:12], "[+]", "Tip moved; 'plugins/tool' unchanged", false)
	assertTargetRef(t, r, "")

	gitCmd(t, dir, "reset", "-q", "--hard", inside)
	r = outdatedRowWith(t, pkg, deps, false)
	assertRow(t, r, base[:12], "--", inside[:12], "[!]", "Default branch tip moved", true)
	assertTargetRef(t, r, inside)
}
