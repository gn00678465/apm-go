package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The exit code is part of gatetool's contract with gate.sh (0 pass, 1
// threshold missed, 2 checker refused), so the tests drive the built binary.
var gatetoolBin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "gatetool-test")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	gatetoolBin = filepath.Join(dir, "gatetool")
	if runtime.GOOS == "windows" {
		gatetoolBin += ".exe"
	}
	if out, err := exec.Command("go", "build", "-o", gatetoolBin, ".").CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build gatetool: %v\n%s", err, out)
		os.Exit(2)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

const testModule = "example.test/m"

// coveredSrc has one executable line (4); coveredBlock is the profile block
// that maps and covers it.
const coveredSrc = "package a\n\nfunc F() int {\n\treturn 1\n}\n"

func coveredBlock(path string) string {
	return testModule + "/" + path + ":3.14,5.2 1 1\n"
}

type repo struct {
	t   *testing.T
	dir string
}

func isolatedEnv() []string {
	return append(os.Environ(),
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_CONFIG_SYSTEM="+os.DevNull,
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.test",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.test",
	)
}

// newRepo returns a repository whose tag "base" holds only go.mod.
func newRepo(t *testing.T) *repo {
	t.Helper()
	r := &repo{t: t, dir: t.TempDir()}
	r.git("init", "-q")
	r.write("go.mod", "module "+testModule+"\n\ngo 1.21\n")
	r.commit()
	r.git("tag", "base")
	return r
}

func (r *repo) git(args ...string) {
	r.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = r.dir
	cmd.Env = isolatedEnv()
	if out, err := cmd.CombinedOutput(); err != nil {
		r.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func (r *repo) write(path, content string) {
	r.t.Helper()
	full := filepath.Join(r.dir, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

// rebase moves the tag "base" to the current commit.
func (r *repo) rebase() {
	r.t.Helper()
	r.git("tag", "-f", "base")
}

func (r *repo) commit() {
	r.t.Helper()
	r.git("add", "-A")
	r.git("commit", "-q", "-m", "c")
}

// run executes "gatetool <sub> -base base ... roots" against a profile made
// of the given block lines and returns stdout, stderr and the exit code.
func (r *repo) run(sub, profileBlocks string, roots ...string) (string, string, int) {
	r.t.Helper()
	profile := filepath.Join(r.t.TempDir(), "cover.out")
	if err := os.WriteFile(profile, []byte("mode: set\n"+profileBlocks), 0o644); err != nil {
		r.t.Fatal(err)
	}
	args := append([]string{sub, "-base", "base", "-module", testModule, "-profile", profile}, roots...)
	cmd := exec.Command(gatetoolBin, args...)
	cmd.Dir = r.dir
	cmd.Env = isolatedEnv()
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	code := 0
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			r.t.Fatalf("run gatetool: %v", err)
		}
		code = ee.ExitCode()
	}
	return stdout.String(), stderr.String(), code
}

func wantCode(t *testing.T, got, want int, stdout, stderr string) {
	t.Helper()
	if got != want {
		t.Fatalf("exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
	}
}

func wantLacks(t *testing.T, out, unwanted string) {
	t.Helper()
	if strings.Contains(out, unwanted) {
		t.Errorf("output holds %q\noutput:\n%s", unwanted, out)
	}
}

func wantContains(t *testing.T, out string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("output lacks %q\noutput:\n%s", w, out)
		}
	}
}

func TestCoverage_OnlyFilesOutsideRoots_PassesAndListsThem(t *testing.T) {
	r := newRepo(t)
	r.write("tools/x/x.go", coveredSrc)
	r.commit()

	out, errOut, code := r.run("coverage", "", "cmd", "internal")

	wantCode(t, code, 0, out, errOut)
	wantContains(t, out,
		"changed-line coverage: covered=0 exec_mapped=0 (0.0%) unmapped=0 platform_excluded=0 nonexec=0 files=0 unmeasured=1 no_lines=0 pkg_init=0\n",
		"no measured file in the diff (measured roots: cmd internal)\n",
		"UNMEASURED files (1), outside the measured roots:\n  tools/x/x.go\n",
	)
}

func TestUnits_OnlyFilesOutsideRoots_PassesAndListsThem(t *testing.T) {
	r := newRepo(t)
	r.write("tools/x/x.go", coveredSrc)
	r.commit()

	out, errOut, code := r.run("units", "", "cmd", "internal")

	wantCode(t, code, 0, out, errOut)
	wantContains(t, out,
		"# unmeasured: tools/x/x.go\n",
		"# units: 0, unmeasured files: 1, measured roots: cmd internal, granularity:",
	)
}

func TestCoverage_FilesInsideAndOutsideRoots_MeasuresInsideListsOutside(t *testing.T) {
	r := newRepo(t)
	r.write("cmd/a/a.go", coveredSrc)
	r.write("tools/x/x.go", coveredSrc)
	r.commit()

	out, errOut, code := r.run("coverage", coveredBlock("cmd/a/a.go"), "cmd", "internal")

	wantCode(t, code, 0, out, errOut)
	wantContains(t, out,
		"changed-line coverage: covered=1 exec_mapped=1 (100.0%) unmapped=0 platform_excluded=0 nonexec=4 files=1 unmeasured=1 no_lines=0 pkg_init=0\n",
		"UNMEASURED files (1), outside the measured roots:\n  tools/x/x.go\n",
	)
}

func TestUnits_FilesInsideAndOutsideRoots_MeasuresInsideListsOutside(t *testing.T) {
	r := newRepo(t)
	r.write("cmd/a/a.go", coveredSrc)
	r.write("tools/x/x.go", coveredSrc)
	r.commit()

	out, errOut, code := r.run("units", coveredBlock("cmd/a/a.go"), "cmd", "internal")

	wantCode(t, code, 0, out, errOut)
	wantContains(t, out,
		"F\tcmd/a/a.go\t1/1\t(no direct textual reference; coverage only)\n",
		"# unmeasured: tools/x/x.go\n",
		"unmeasured files: 1,",
	)
	if strings.Contains(out, "\ttools/x/x.go\t") {
		t.Errorf("a file outside the roots was reported as a unit\noutput:\n%s", out)
	}
}

func TestCoverage_EmptyDiff_Refused(t *testing.T) {
	r := newRepo(t)

	out, errOut, code := r.run("coverage", "", "cmd", "internal")

	wantCode(t, code, 2, out, errOut)
	wantContains(t, errOut, "coverage: the diff is empty (fail closed)")
}

func TestUnits_EmptyDiff_Refused(t *testing.T) {
	r := newRepo(t)

	out, errOut, code := r.run("units", "", "cmd", "internal")

	wantCode(t, code, 2, out, errOut)
	wantContains(t, errOut, "units: the diff is empty (fail closed)")
}

func TestCoverage_UnprofiledExecutableLineInsideRoot_Fails(t *testing.T) {
	r := newRepo(t)
	r.write("cmd/a/a.go", coveredSrc)
	r.commit()

	out, errOut, code := r.run("coverage", "", "cmd", "internal")

	wantCode(t, code, 1, out, errOut)
	wantContains(t, out,
		"changed-line coverage: covered=0 exec_mapped=0 (0.0%) unmapped=1 platform_excluded=0 nonexec=4 files=1 unmeasured=0 no_lines=0 pkg_init=0\n",
		"UNMAPPED executable lines (1):\n  cmd/a/a.go:4\n",
	)
}

func TestUnits_UnprofiledExecutableLineInsideRoot_ReportedUncovered(t *testing.T) {
	r := newRepo(t)
	r.write("cmd/a/a.go", coveredSrc)
	r.commit()

	out, errOut, code := r.run("units", "", "cmd", "internal")

	wantCode(t, code, 0, out, errOut)
	wantContains(t, out,
		"F\tcmd/a/a.go\t0/1\t(no direct textual reference; coverage only)\n",
		"unmeasured files: 0,",
	)
}

func TestCoverage_RootMatchesOnDirectoryBoundary(t *testing.T) {
	r := newRepo(t)
	r.write("cmdx/y.go", coveredSrc)
	r.commit()

	out, errOut, code := r.run("coverage", "", "cmd")

	wantCode(t, code, 0, out, errOut)
	wantContains(t, out,
		"files=0 unmeasured=1 no_lines=0 pkg_init=0\n",
		"UNMEASURED files (1), outside the measured roots:\n  cmdx/y.go\n",
	)
}

func TestUnits_RootMatchesOnDirectoryBoundary(t *testing.T) {
	r := newRepo(t)
	r.write("cmdx/y.go", coveredSrc)
	r.commit()

	out, errOut, code := r.run("units", "", "cmd")

	wantCode(t, code, 0, out, errOut)
	wantContains(t, out, "# unmeasured: cmdx/y.go\n", "# units: 0, unmeasured files: 1,")
}

func TestCoverage_OnlyNonGoChange_PassesWithNoMeasuredFile(t *testing.T) {
	r := newRepo(t)
	r.write("README.md", "x\n")
	r.commit()

	out, errOut, code := r.run("coverage", "", "cmd", "internal")

	wantCode(t, code, 0, out, errOut)
	wantContains(t, out,
		"files=0 unmeasured=0 no_lines=0 pkg_init=0\n",
		"no measured file in the diff (measured roots: cmd internal)\n",
	)
}

func renamedInsideRoot(t *testing.T) *repo {
	t.Helper()
	r := newRepo(t)
	r.write("cmd/a/a.go", coveredSrc)
	r.commit()
	r.rebase()
	r.git("mv", "cmd/a/a.go", "cmd/a/b.go")
	r.commit()
	return r
}

func TestCoverage_PureRenameInsideRoot_ListedUnderNewPath(t *testing.T) {
	r := renamedInsideRoot(t)

	out, errOut, code := r.run("coverage", "", "cmd", "internal")

	wantCode(t, code, 0, out, errOut)
	wantContains(t, out,
		"changed-line coverage: covered=0 exec_mapped=0 (0.0%) unmapped=0 platform_excluded=0 nonexec=0 files=0 unmeasured=0 no_lines=1 pkg_init=0\n",
		"files with no line to measure (1), inside the measured roots:\n  cmd/a/b.go (renamed from cmd/a/a.go, no changed line)\n",
	)
	wantLacks(t, out, "no measured file in the diff")
}

func TestUnits_PureRenameInsideRoot_ListedUnderNewPath(t *testing.T) {
	r := renamedInsideRoot(t)

	out, errOut, code := r.run("units", "", "cmd", "internal")

	wantCode(t, code, 0, out, errOut)
	wantContains(t, out, "# no line to measure: cmd/a/b.go (renamed from cmd/a/a.go, no changed line)\n")
}

func TestCoverage_DeletedFileInsideRoot_Listed(t *testing.T) {
	r := newRepo(t)
	r.write("cmd/a/keep.go", coveredSrc)
	r.write("cmd/a/gone.go", "package a\n\nfunc G() int {\n\treturn 2\n}\n")
	r.commit()
	r.rebase()
	r.git("rm", "-q", "cmd/a/gone.go")
	r.commit()

	out, errOut, code := r.run("coverage", "", "cmd", "internal")

	wantCode(t, code, 0, out, errOut)
	wantContains(t, out,
		"files=0 unmeasured=0 no_lines=1 pkg_init=0\n",
		"files with no line to measure (1), inside the measured roots:\n  cmd/a/gone.go (deleted)\n",
	)
	wantLacks(t, out, "no measured file in the diff")
}

func renamedOutsideRoot(t *testing.T) *repo {
	t.Helper()
	r := newRepo(t)
	r.write("tools/x/x.go", coveredSrc)
	r.commit()
	r.rebase()
	r.git("mv", "tools/x/x.go", "tools/x/y.go")
	r.commit()
	return r
}

func TestCoverage_PureRenameOutsideRoot_Unmeasured(t *testing.T) {
	r := renamedOutsideRoot(t)

	out, errOut, code := r.run("coverage", "", "cmd", "internal")

	wantCode(t, code, 0, out, errOut)
	wantContains(t, out,
		"files=0 unmeasured=1 no_lines=0 pkg_init=0\n",
		"UNMEASURED files (1), outside the measured roots:\n  tools/x/y.go\n",
	)
}

func TestUnits_PureRenameOutsideRoot_Unmeasured(t *testing.T) {
	r := renamedOutsideRoot(t)

	out, errOut, code := r.run("units", "", "cmd", "internal")

	wantCode(t, code, 0, out, errOut)
	wantContains(t, out, "# unmeasured: tools/x/y.go\n", "# units: 0, unmeasured files: 1,")
}

const nonASCIIPath = "cmd/a/檢查.go"

func TestCoverage_NonASCIIPath_UnprofiledLineFails(t *testing.T) {
	r := newRepo(t)
	r.write(nonASCIIPath, coveredSrc)
	r.commit()

	out, errOut, code := r.run("coverage", "", "cmd", "internal")

	wantCode(t, code, 1, out, errOut)
	wantContains(t, out,
		"changed-line coverage: covered=0 exec_mapped=0 (0.0%) unmapped=1 platform_excluded=0 nonexec=4 files=1 unmeasured=0 no_lines=0 pkg_init=0\n",
		"UNMAPPED executable lines (1):\n  cmd/a/檢查.go:4\n",
	)
	wantLacks(t, out, "no line to measure")
}

func TestCoverage_NonASCIIPath_ProfiledLineCounted(t *testing.T) {
	r := newRepo(t)
	r.write(nonASCIIPath, coveredSrc)
	r.commit()

	out, errOut, code := r.run("coverage", coveredBlock(nonASCIIPath), "cmd", "internal")

	wantCode(t, code, 0, out, errOut)
	wantContains(t, out,
		"changed-line coverage: covered=1 exec_mapped=1 (100.0%) unmapped=0 platform_excluded=0 nonexec=4 files=1 unmeasured=0 no_lines=0 pkg_init=0\n",
	)
}

func TestCoverage_PathGitStillQuotes_Refused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows file names cannot hold a double quote")
	}
	r := newRepo(t)
	r.write(`cmd/a/q"x.go`, coveredSrc)
	r.commit()

	out, errOut, code := r.run("coverage", "", "cmd", "internal")

	wantCode(t, code, 2, out, errOut)
	wantContains(t, errOut, `cmd/a/q"x.go`)
}

const twoFuncSrc = "package a\n\nfunc F() int {\n\treturn 1\n}\n\nfunc G() int {\n\treturn 2\n}\n"

func removedFunction(t *testing.T) *repo {
	t.Helper()
	r := newRepo(t)
	r.write("cmd/a/a.go", twoFuncSrc)
	r.commit()
	r.rebase()
	r.write("cmd/a/a.go", coveredSrc)
	r.commit()
	return r
}

func TestCoverage_OnlyRemovedLines_NoLineToMeasure(t *testing.T) {
	r := removedFunction(t)

	out, errOut, code := r.run("coverage", "", "cmd", "internal")

	wantCode(t, code, 0, out, errOut)
	wantContains(t, out,
		"changed-line coverage: covered=0 exec_mapped=0 (0.0%) unmapped=0 platform_excluded=0 nonexec=0 files=0 unmeasured=0 no_lines=1 pkg_init=0\n",
		"files with no line to measure (1), inside the measured roots:\n  cmd/a/a.go (only removed lines)\n",
	)
	wantLacks(t, out, "no measured file in the diff")
}

func TestUnits_RemovedFunction_ListedOnceAsDeletedUnit(t *testing.T) {
	r := removedFunction(t)

	out, errOut, code := r.run("units", "", "cmd", "internal")

	wantCode(t, code, 0, out, errOut)
	wantContains(t, out, "deleted: G\tcmd/a/a.go\t-\tbuild+suite green (no remaining reference)\n")
	wantLacks(t, out, "# no line to measure")
}

func TestUnits_RemovedLineInsideLivingFunction_FileListed(t *testing.T) {
	r := newRepo(t)
	r.write("cmd/a/a.go", "package a\n\nfunc F() int {\n\tprintln(1)\n\treturn 1\n}\n")
	r.commit()
	r.rebase()
	r.write("cmd/a/a.go", coveredSrc)
	r.commit()

	out, errOut, code := r.run("units", "", "cmd", "internal")

	wantCode(t, code, 0, out, errOut)
	wantContains(t, out, "# no line to measure: cmd/a/a.go (only removed lines)\n")
}

func TestCoverage_PathWithSpace_UnprofiledLineFails(t *testing.T) {
	r := newRepo(t)
	r.write("cmd/a/with space.go", coveredSrc)
	r.commit()

	out, errOut, code := r.run("coverage", "", "cmd", "internal")

	wantCode(t, code, 1, out, errOut)
	wantContains(t, out,
		"unmapped=1 platform_excluded=0 nonexec=4 files=1 unmeasured=0 no_lines=0 pkg_init=0\n",
		"UNMAPPED executable lines (1):\n  cmd/a/with space.go:4\n",
	)
}

const pkgInitHeader = "package-level initializer lines"

const pkgInitVarCallSrc = "package a\n\nvar V = calc()\n\nfunc calc() int {\n\treturn 1\n}\n"

const pkgInitVarCallBlock = testModule + "/cmd/a/a.go:5.17,7.2 1 1\n"

func TestCoverage_PackageVarCall_ListedAndPasses(t *testing.T) {
	r := newRepo(t)
	r.write("cmd/a/a.go", pkgInitVarCallSrc)
	r.commit()

	out, errOut, code := r.run("coverage", pkgInitVarCallBlock, "cmd", "internal")

	wantCode(t, code, 0, out, errOut)
	wantContains(t, out,
		"changed-line coverage: covered=1 exec_mapped=1 (100.0%) unmapped=0 platform_excluded=0 nonexec=5 files=1 unmeasured=0 no_lines=0 pkg_init=1\n",
		"package-level initializer lines (1), go cover emits no block for them; listed, not a failure:\n"+
			"  cmd/a/a.go:3  var V  direct-tests: NONE (no direct textual reference)\n",
	)
}

func TestCoverage_PackageVarCall_NamesDirectTest(t *testing.T) {
	r := newRepo(t)
	r.write("cmd/a/a.go", pkgInitVarCallSrc)
	r.write("cmd/a/a_test.go", "package a\n\nimport \"testing\"\n\nfunc TestV(t *testing.T) { _ = V }\n")
	r.commit()

	out, errOut, code := r.run("coverage", pkgInitVarCallBlock, "cmd", "internal")

	wantCode(t, code, 0, out, errOut)
	wantContains(t, out,
		"changed-line coverage: covered=1 exec_mapped=1 (100.0%) unmapped=0 platform_excluded=0 nonexec=5 files=1 unmeasured=0 no_lines=0 pkg_init=1\n",
		"  cmd/a/a.go:3  var V  direct-tests: a_test.go::TestV\n",
	)
}

func TestCoverage_PackageVarCall_DoesNotChangeFailureOfUnmappedLine(t *testing.T) {
	r := newRepo(t)
	r.write("cmd/a/a.go", pkgInitVarCallSrc)
	r.commit()

	out, errOut, code := r.run("coverage", "", "cmd", "internal")

	wantCode(t, code, 1, out, errOut)
	wantContains(t, out,
		"changed-line coverage: covered=0 exec_mapped=0 (0.0%) unmapped=1 platform_excluded=0 nonexec=5 files=1 unmeasured=0 no_lines=0 pkg_init=1\n",
		"  cmd/a/a.go:3  var V  direct-tests: NONE (no direct textual reference)\n",
		"UNMAPPED executable lines (1):\n  cmd/a/a.go:6\n",
	)
	section, unmapped := strings.Index(out, pkgInitHeader), strings.Index(out, "UNMAPPED executable lines")
	if section < 0 || unmapped < 0 || section > unmapped {
		t.Errorf("the initializer section (at %d) must precede the UNMAPPED section (at %d)\noutput:\n%s", section, unmapped, out)
	}
}

func TestCoverage_PackageConst_Listed(t *testing.T) {
	r := newRepo(t)
	r.write("cmd/a/a.go", "package a\n\nconst C = 1 + 2\n")
	r.commit()

	out, errOut, code := r.run("coverage", "", "cmd", "internal")

	wantCode(t, code, 0, out, errOut)
	wantContains(t, out,
		"changed-line coverage: covered=0 exec_mapped=0 (0.0%) unmapped=0 platform_excluded=0 nonexec=2 files=1 unmeasured=0 no_lines=0 pkg_init=1\n",
		"package-level initializer lines (1), go cover emits no block for them; listed, not a failure:\n"+
			"  cmd/a/a.go:3  const C  direct-tests: NONE (no direct textual reference)\n",
	)
}

func TestCoverage_PackageCompositeLiteral_ListedAsOneRange(t *testing.T) {
	r := newRepo(t)
	r.write("cmd/a/a.go", "package a\n\nvar M = map[string]int{\n\t\"a\": 1,\n\t\"b\": len(\"xx\"),\n}\n")
	r.commit()

	out, errOut, code := r.run("coverage", "", "cmd", "internal")

	wantCode(t, code, 0, out, errOut)
	wantContains(t, out,
		"changed-line coverage: covered=0 exec_mapped=0 (0.0%) unmapped=0 platform_excluded=0 nonexec=3 files=1 unmeasured=0 no_lines=0 pkg_init=3\n",
		"package-level initializer lines (3), go cover emits no block for them; listed, not a failure:\n"+
			"  cmd/a/a.go:3-5  var M  direct-tests: NONE (no direct textual reference)\n",
	)
}

func TestCoverage_PackageVarBlock_ListsOnlySpecWithValue(t *testing.T) {
	r := newRepo(t)
	r.write("cmd/a/a.go", "package a\n\nvar (\n\tA = calc()\n\tB int\n)\n\nfunc calc() int {\n\treturn 1\n}\n")
	r.commit()

	out, errOut, code := r.run("coverage", testModule+"/cmd/a/a.go:8.17,10.2 1 1\n", "cmd", "internal")

	wantCode(t, code, 0, out, errOut)
	wantContains(t, out,
		"changed-line coverage: covered=1 exec_mapped=1 (100.0%) unmapped=0 platform_excluded=0 nonexec=8 files=1 unmeasured=0 no_lines=0 pkg_init=1\n",
		"package-level initializer lines (1), go cover emits no block for them; listed, not a failure:\n"+
			"  cmd/a/a.go:4  var A  direct-tests: NONE (no direct textual reference)\n",
	)
	wantLacks(t, out, "var B")
}

func TestCoverage_PackageFuncLiteralBody_StaysUnderCoverage(t *testing.T) {
	r := newRepo(t)
	r.write("cmd/a/a.go", "package a\n\nvar F = func() int {\n\treturn 1\n}\n")
	r.commit()

	out, errOut, code := r.run("coverage", testModule+"/cmd/a/a.go:3.20,5.2 1 0\n", "cmd", "internal")

	wantCode(t, code, 1, out, errOut)
	wantContains(t, out,
		"changed-line coverage: covered=0 exec_mapped=1 (0.0%) unmapped=0 platform_excluded=0 nonexec=4 files=1 unmeasured=0 no_lines=0 pkg_init=0\n",
		"UNCOVERED executable lines (1):\n  cmd/a/a.go:4\n",
	)
	wantLacks(t, out, pkgInitHeader)
}

func TestCoverage_PackageVarWithNoValue_NotListed(t *testing.T) {
	r := newRepo(t)
	r.write("cmd/a/a.go", "package a\n\nvar B int\n")
	r.commit()

	out, errOut, code := r.run("coverage", "", "cmd", "internal")

	wantCode(t, code, 0, out, errOut)
	wantContains(t, out,
		"changed-line coverage: covered=0 exec_mapped=0 (0.0%) unmapped=0 platform_excluded=0 nonexec=3 files=1 unmeasured=0 no_lines=0 pkg_init=0\n",
	)
	wantLacks(t, out, pkgInitHeader)
}

const twoNameVarSrc = "package a\n\nvar A, B = 1, 2\n"

const testReadingB = "package a\n\nimport \"testing\"\n\nfunc TestB(t *testing.T) { _ = B }\n"

const testUsingOnlyBlank = "func TestOther(t *testing.T) {\n\tfor _, x := range []int{1} {\n\t\tprintln(x)\n\t}\n}\n"

func TestCoverage_PackageVarTwoNames_NamesTestOfSecondName(t *testing.T) {
	r := newRepo(t)
	r.write("cmd/a/a.go", twoNameVarSrc)
	r.write("cmd/a/a_test.go", testReadingB)
	r.commit()

	out, errOut, code := r.run("coverage", "", "cmd", "internal")

	wantCode(t, code, 0, out, errOut)
	wantContains(t, out, "  cmd/a/a.go:3  var A,B  direct-tests: a_test.go::TestB\n")
}

func TestUnits_PackageVarTwoNames_StillSearchesFirstNameOnly(t *testing.T) {
	r := newRepo(t)
	r.write("cmd/a/a.go", twoNameVarSrc)
	r.write("cmd/a/a_test.go", testReadingB)
	r.commit()

	out, errOut, code := r.run("units", "", "cmd", "internal")

	wantCode(t, code, 0, out, errOut)
	wantContains(t, out, "var A,B\tcmd/a/a.go\t0/0\t(no direct textual reference; coverage only)\n")
}

func TestCoverage_PackageBlankVar_HasNoDirectTest(t *testing.T) {
	r := newRepo(t)
	r.write("cmd/a/a.go", "package a\n\ntype I interface{}\n\ntype T struct{}\n\nvar _ I = (*T)(nil)\n")
	r.write("cmd/a/a_test.go", "package a\n\nimport \"testing\"\n\n"+testUsingOnlyBlank)
	r.commit()

	out, errOut, code := r.run("coverage", "", "cmd", "internal")

	wantCode(t, code, 0, out, errOut)
	wantContains(t, out, "  cmd/a/a.go:7  var _  direct-tests: NONE (no direct textual reference)\n")
}

func TestCoverage_PackageVarBlankAndName_SearchesOnlyTheName(t *testing.T) {
	r := newRepo(t)
	r.write("cmd/a/a.go", "package a\n\nvar _, C = 3, 4\n")
	r.write("cmd/a/a_test.go", "package a\n\nimport \"testing\"\n\nfunc TestC(t *testing.T) { _ = C }\n\n"+testUsingOnlyBlank)
	r.commit()

	out, errOut, code := r.run("coverage", "", "cmd", "internal")

	wantCode(t, code, 0, out, errOut)
	wantContains(t, out, "  cmd/a/a.go:3  var _,C  direct-tests: a_test.go::TestC\n")
	wantLacks(t, out, "TestOther")
}
