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
		"changed-line coverage: covered=0 exec_mapped=0 (0.0%) unmapped=0 platform_excluded=0 nonexec=0 files=0 unmeasured=1\n",
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
		"changed-line coverage: covered=1 exec_mapped=1 (100.0%) unmapped=0 platform_excluded=0 nonexec=4 files=1 unmeasured=1\n",
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
		"changed-line coverage: covered=0 exec_mapped=0 (0.0%) unmapped=1 platform_excluded=0 nonexec=4 files=1 unmeasured=0\n",
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
		"files=0 unmeasured=1\n",
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
		"files=0 unmeasured=0\n",
		"no measured file in the diff (measured roots: cmd internal)\n",
	)
}
