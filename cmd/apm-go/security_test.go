package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const integrityDir = "../../conformance/conformance-kit/oracle/integrity"

func integrityFixture(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join(integrityDir, name)
	if _, err := os.Stat(p); err != nil {
		t.Skipf("oracle not present (%s)", p)
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

func sha256Envelope(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func copyInto(t *testing.T, src, dst string) {
	t.Helper()
	in, err := os.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		t.Fatal(err)
	}
}

// chTemp switches into a fresh temp dir for the duration of the test.
func chTemp(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	orig, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(orig) })
	return dir
}

func newDeps() *installDeps {
	return &installDeps{tags: &mockInstallTagLister{}, loader: &mockInstallLoader{}}
}

// --- req-sc-001 / req-lk-017: deployed-file integrity (audit + frozen) ---

func TestFrozen_DeployedFileMismatch(t *testing.T) {
	abs := integrityFixture(t, "deployed-file-mismatch.frozen.yaml")
	wsFile := integrityFixture(t, "deployed-file-mismatch.workspace/.github/instructions/demo.instructions.md")
	dir := chTemp(t)

	// Lay down the tampered workspace file and the frozen lockfile.
	target := filepath.Join(dir, ".github", "instructions")
	os.MkdirAll(target, 0o755)
	copyInto(t, wsFile, filepath.Join(target, "demo.instructions.md"))
	copyInto(t, abs, filepath.Join(dir, "apm.lock.yaml"))

	err := runInstall(newDeps(), true, false, "", nil, nil)
	if err == nil {
		t.Fatal("expected fail-closed on deployed-file mismatch")
	}
	if !strings.Contains(err.Error(), ".github/instructions/demo.instructions.md") {
		t.Errorf("diagnostic must name the tampered path, got %v", err)
	}
}

func TestAudit_DeployedFileMismatch(t *testing.T) {
	abs := integrityFixture(t, "deployed-file-mismatch.frozen.yaml")
	wsFile := integrityFixture(t, "deployed-file-mismatch.workspace/.github/instructions/demo.instructions.md")
	dir := chTemp(t)

	target := filepath.Join(dir, ".github", "instructions")
	os.MkdirAll(target, 0o755)
	copyInto(t, wsFile, filepath.Join(target, "demo.instructions.md"))
	copyInto(t, abs, filepath.Join(dir, "apm.lock.yaml"))

	cmd := auditCmd()
	var errBuf bytes.Buffer
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected audit to fail on content-integrity violation")
	}
	const wantPath = ".github/instructions/demo.instructions.md"
	// Pin the per-violation diagnostic loop specifically: "observed " only appears
	// in that stderr line, not in the returned error string — so this fails if the
	// loop is removed, independent of the err-string check below.
	if !strings.Contains(errBuf.String(), wantPath) || !strings.Contains(errBuf.String(), "observed ") {
		t.Errorf("audit stderr must emit the per-violation line naming the path, got %q", errBuf.String())
	}
	if !strings.Contains(err.Error(), wantPath) {
		t.Errorf("audit error must name the tampered path, got %v", err)
	}
}

// --- req-lk-013: registry archive hash verified before extract ---

func TestFrozen_HashMismatch_NoExtract(t *testing.T) {
	lock := integrityFixture(t, "hash-mismatch.frozen.yaml")
	good := integrityFixture(t, "good.tar.gz")
	dir := chTemp(t)

	copyInto(t, lock, filepath.Join(dir, "apm.lock.yaml"))
	copyInto(t, good, filepath.Join(dir, "good.tar.gz")) // repo_url .../demo/good -> good.tar.gz

	err := runInstall(newDeps(), true, false, "", nil, nil)
	if err == nil {
		t.Fatal("expected fail-closed on archive hash mismatch")
	}
	msg := err.Error()
	if !strings.Contains(msg, "expected") || !strings.Contains(msg, "actual") {
		t.Errorf("diagnostic must contain expected+actual, got %v", err)
	}
	// must_not_extract: no payload extracted under apm_modules
	assertNoExtract(t, dir)
}

func TestFrozen_Good_VerifiesAndExtracts(t *testing.T) {
	lock := integrityFixture(t, "good.frozen.yaml")
	good := integrityFixture(t, "good.tar.gz")
	dir := chTemp(t)

	copyInto(t, lock, filepath.Join(dir, "apm.lock.yaml"))
	copyInto(t, good, filepath.Join(dir, "good.tar.gz"))

	if err := runInstall(newDeps(), true, false, "", nil, nil); err != nil {
		t.Fatalf("good registry frozen install should succeed: %v", err)
	}
	extracted := filepath.Join(dir, "apm_modules", "registry.example.com", "demo", "good", "skill", "SKILL.md")
	if _, err := os.Stat(extracted); err != nil {
		t.Errorf("good archive should extract to %s: %v", extracted, err)
	}
}

// --- END-TO-END EXTRACT PROOF (advisor #2): malicious tarballs flow through the
// real install registry path container->hash->SafeExtract->limits ---

func TestFrozen_RegistryExtract_EndToEnd(t *testing.T) {
	cases := []struct {
		name       string
		fixture    string
		maxEntries int
		wantSubstr string
	}{
		{"zip-slip", "zip-slip.tar.gz", 0, ".."},
		{"symlink-escape", "symlink-escape.tar.gz", 0, "link"},
		{"four-entry", "four-entry.tar.gz", 3, "entry count"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fx := integrityFixture(t, tc.fixture)
			hash := sha256Envelope(t, fx)
			dir := chTemp(t)

			// basename(repo_url) drives the offline archive name: x/<name> -> <name>.tar.gz
			copyInto(t, fx, filepath.Join(dir, tc.fixture))
			lock := fmt.Sprintf("lockfile_version: \"2\"\ndependencies:\n  - repo_url: x/%s\n    source: registry\n    resolved_url: https://r.example/%s\n    resolved_hash: %q\n    version: \"1.0.0\"\n    depth: 1\n",
				strings.TrimSuffix(tc.fixture, ".tar.gz"),
				tc.fixture, hash)
			os.WriteFile(filepath.Join(dir, "apm.lock.yaml"), []byte(lock), 0o644)

			deps := newDeps()
			deps.maxEntries = tc.maxEntries

			err := runInstall(deps, true, false, "", nil, nil)
			if err == nil {
				t.Fatalf("%s must fail closed through the install extract path", tc.name)
			}
			if !strings.Contains(err.Error(), tc.wantSubstr) {
				t.Errorf("%s: diagnostic must contain %q, got %v", tc.name, tc.wantSubstr, err)
			}
			assertNoExtract(t, dir)
		})
	}
}

// TestFrozen_RepoURLTraversal_FailsClosed proves the extraction destination
// cannot escape apm_modules via a tampered lockfile repo_url (the HIGH defect
// found in review). A registry entry with repo_url "../../escape" plus a
// hash-matching local archive must fail closed and write nothing outside.
func TestFrozen_RepoURLTraversal_FailsClosed(t *testing.T) {
	good := integrityFixture(t, "good.tar.gz")
	parent := t.TempDir()
	project := filepath.Join(parent, "project")
	os.MkdirAll(project, 0o755)
	orig, _ := os.Getwd()
	os.Chdir(project)
	t.Cleanup(func() { os.Chdir(orig) })

	// repo_url "../../escape" -> basename "escape" -> escape.tar.gz in CWD.
	copyInto(t, good, filepath.Join(project, "escape.tar.gz"))
	hash := sha256Envelope(t, filepath.Join(project, "escape.tar.gz"))
	lock := fmt.Sprintf("lockfile_version: \"2\"\ndependencies:\n  - repo_url: ../../escape\n    source: registry\n    resolved_url: https://r.example/escape.tar.gz\n    resolved_hash: %q\n    version: \"1.0.0\"\n    depth: 1\n", hash)
	os.WriteFile(filepath.Join(project, "apm.lock.yaml"), []byte(lock), 0o644)

	err := runInstall(newDeps(), true, false, "", nil, nil)
	if err == nil {
		t.Fatal("expected fail-closed on repo_url path traversal")
	}
	if !strings.Contains(err.Error(), "..") {
		t.Errorf("diagnostic should flag the traversal, got %v", err)
	}
	// Nothing may be written outside the project dir (sibling escape/ etc.).
	for _, p := range []string{
		filepath.Join(parent, "escape"),
		filepath.Join(parent, "escape", "skill", "SKILL.md"),
	} {
		if _, statErr := os.Stat(p); statErr == nil {
			t.Errorf("payload escaped containment: %s exists", p)
		}
	}
}

// assertNoExtract verifies no archive payload leaked under apm_modules.
func assertNoExtract(t *testing.T, dir string) {
	t.Helper()
	modules := filepath.Join(dir, "apm_modules")
	var leaked []string
	filepath.WalkDir(modules, func(p string, d os.DirEntry, err error) error {
		if err == nil && d != nil && !d.IsDir() {
			leaked = append(leaked, p)
		}
		return nil
	})
	if len(leaked) > 0 {
		t.Errorf("fail-closed must not leave extracted files, found: %v", leaked)
	}
}
