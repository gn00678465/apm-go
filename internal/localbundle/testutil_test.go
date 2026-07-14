package localbundle

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/pack/bundle"
	"github.com/apm-go/apm/internal/yamlcore"
)

// createJunction creates an NTFS junction (mount point) at link pointing at
// target, via `mklink /J` -- unlike a real symlink (which needs an
// elevated/Developer-Mode privilege on Windows), a junction requires no
// special privilege, which is exactly why it's the vector Gate 6b's B2
// finding used to demonstrate a bundle escaping VerifyBundleIntegrity's
// symlink sweep (codex-verify-gate6b-fix.md).
func createJunction(t *testing.T, link, target string) {
	t.Helper()
	out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput()
	if err != nil {
		t.Fatalf("mklink /J %s %s failed: %v: %s", link, target, err, out)
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// buildTestBundle produces a real plugin-format bundle via bundle.Produce --
// the same BundleProducer `apm-go pack` uses -- with an embedded,
// integrity-checked apm.lock.yaml (pack.bundle_files, bare hex), so this
// package's tests exercise DetectLocalBundle/VerifyBundleIntegrity/
// IntegrateLocalBundle against oracle-shaped output rather than a
// hand-rolled fixture that could silently diverge from what pack actually
// produces.
func buildTestBundle(t *testing.T) string {
	t.Helper()
	projectRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(projectRoot, ".apm", "agents", "foo.md"), "# agent foo")
	mustWriteFile(t, filepath.Join(projectRoot, ".apm", "skills", "bar", "SKILL.md"), "# skill bar")
	mustWriteFile(t, filepath.Join(projectRoot, ".apm", "commands", "greet.md"), "# command greet")
	mustWriteFile(t, filepath.Join(projectRoot, ".apm", "instructions", "baz.instructions.md"), "---\napplyTo: \"**/*.go\"\n---\n\ninstruction body")
	mustWriteFile(t, filepath.Join(projectRoot, ".apm", "hooks", "h.json"), `{"PreToolUse":[{"matcher":"*","hooks":[{"type":"command","command":"echo hi"}]}]}`)
	mustWriteFile(t, filepath.Join(projectRoot, ".mcp.json"), `{"mcpServers":{"demo":{"type":"stdio","command":"demo-server","args":["--flag"]}}}`)

	doc, err := yamlcore.SafeLoad([]byte("name: demo\nversion: 1.0.0\n"))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	result, err := bundle.Produce(&buf, bundle.ProduceOptions{
		ProjectRoot: projectRoot,
		OutputDir:   filepath.Join(projectRoot, "build"),
		PkgName:     "demo",
		PkgVersion:  "1.0.0",
		Target:      "claude",
		ApmYMLNode:  doc.Content[0],
		Lockfile:    &lockfile.Lockfile{Version: "1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return result.BundleDir
}

// bundleTestPackMeta re-detects bundleDir (produced by buildTestBundle) to
// obtain the *bundle.PackMetadata IntegrateLocalBundle expects -- exercising
// the same DetectLocalBundle path cmd/apm-go/install.go's real caller uses,
// rather than hand-constructing a PackMetadata that could silently diverge.
func bundleTestPackMeta(t *testing.T, bundleDir string) *bundle.PackMetadata {
	t.Helper()
	info, err := DetectLocalBundle(bundleDir)
	if err != nil {
		t.Fatal(err)
	}
	if info == nil || !info.HasPackMeta {
		t.Fatal("expected buildTestBundle's output to carry pack: metadata")
	}
	return &info.PackMeta
}

// zipDir packs dir's contents (relative paths preserved, dir itself as the
// zip root -- no extra nesting level) into a .zip archive and returns its
// path.
func zipDir(t *testing.T, dir string) string {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	err := filepath.Walk(dir, func(p string, info os.FileInfo, walkErr error) error {
		if walkErr != nil || info.IsDir() {
			return walkErr
		}
		rel, rerr := filepath.Rel(dir, p)
		if rerr != nil {
			return rerr
		}
		w, cerr := zw.Create(filepath.ToSlash(rel))
		if cerr != nil {
			return cerr
		}
		data, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		_, werr := w.Write(data)
		return werr
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "bundle.zip")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// tarGzDir packs dir's contents into a .tar.gz archive nested under a
// single top-level "<base>/" directory, mirroring `apm pack --archive`'s
// arcname=<bundle-name> convention (findExtractedRoot's "single directory
// child" branch).
func tarGzDir(t *testing.T, dir string) string {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	base := filepath.Base(dir)
	err := filepath.Walk(dir, func(p string, info os.FileInfo, walkErr error) error {
		if walkErr != nil || info.IsDir() {
			return walkErr
		}
		rel, rerr := filepath.Rel(dir, p)
		if rerr != nil {
			return rerr
		}
		data, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		hdr := &tar.Header{Name: base + "/" + filepath.ToSlash(rel), Mode: 0o644, Size: int64(len(data))}
		if werr := tw.WriteHeader(hdr); werr != nil {
			return werr
		}
		_, werr := tw.Write(data)
		return werr
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "bundle.tar.gz")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
