package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/marketplace/authoring"
)

func writeLocalPackage(t *testing.T, projectRoot, rel, name, version string) {
	t.Helper()
	dir := filepath.Join(projectRoot, rel)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "name: " + name + "\n"
	if version != "" {
		content += "version: " + version + "\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "apm.yml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCheckVersionAlignment_Lockstep_Match(t *testing.T) {
	root := t.TempDir()
	writeLocalPackage(t, root, "pkgs/a", "pkg-a", "1.0.0")
	cfg := &authoring.AuthoringConfig{
		Version:    "1.0.0",
		Versioning: authoring.Versioning{Strategy: "lockstep"},
		Packages:   []authoring.PackageEntry{{Name: "pkg-a", Source: "./pkgs/a"}},
	}

	report := CheckVersionAlignment(cfg, root)
	if !report.OK {
		t.Fatalf("report.OK = false, want true; packages=%+v", report.Packages)
	}
	if report.Expected != "1.0.0" {
		t.Errorf("report.Expected = %q, want %q", report.Expected, "1.0.0")
	}
	if len(report.Packages) != 1 || report.Packages[0].Reason != "matches" {
		t.Errorf("packages = %+v, want a single matching row", report.Packages)
	}
}

func TestCheckVersionAlignment_Lockstep_Drift(t *testing.T) {
	root := t.TempDir()
	writeLocalPackage(t, root, "pkgs/a", "pkg-a", "2.0.0")
	cfg := &authoring.AuthoringConfig{
		Version:    "1.0.0",
		Versioning: authoring.Versioning{Strategy: "lockstep"},
		Packages:   []authoring.PackageEntry{{Name: "pkg-a", Source: "./pkgs/a"}},
	}

	report := CheckVersionAlignment(cfg, root)
	if report.OK {
		t.Fatal("report.OK = true, want false")
	}
	if report.Packages[0].Reason != "drift:expected=1.0.0" {
		t.Errorf("reason = %q, want %q", report.Packages[0].Reason, "drift:expected=1.0.0")
	}
	msgs := report.ErrorMessages()
	if len(msgs) != 1 || msgs[0] != "pkgs/a: expected 1.0.0, found 2.0.0" {
		t.Errorf("ErrorMessages() = %v, want exactly one matching message", msgs)
	}
}

func TestCheckVersionAlignment_PerPackage_NeverDrifts(t *testing.T) {
	root := t.TempDir()
	writeLocalPackage(t, root, "pkgs/a", "pkg-a", "9.9.9")
	cfg := &authoring.AuthoringConfig{
		Version:    "1.0.0",
		Versioning: authoring.Versioning{Strategy: "per_package"},
		Packages:   []authoring.PackageEntry{{Name: "pkg-a", Source: "./pkgs/a"}},
	}

	report := CheckVersionAlignment(cfg, root)
	if !report.OK {
		t.Fatalf("per_package must never enforce equality; packages=%+v", report.Packages)
	}
	if report.Expected != "" {
		t.Errorf("report.Expected = %q, want empty (only lockstep sets it)", report.Expected)
	}
}

func TestCheckVersionAlignment_TagPattern_Match(t *testing.T) {
	root := t.TempDir()
	writeLocalPackage(t, root, "pkgs/a", "pkg-a", "1.2.3")
	cfg := &authoring.AuthoringConfig{
		Version:    "1.0.0",
		Versioning: authoring.Versioning{Strategy: "tag_pattern"},
		Build:      authoring.Build{TagPattern: "{name}-v{version}"},
		Packages:   []authoring.PackageEntry{{Name: "pkg-a", Source: "./pkgs/a"}},
	}

	report := CheckVersionAlignment(cfg, root)
	if !report.OK {
		t.Fatalf("report.OK = false, want true; packages=%+v", report.Packages)
	}
	if report.Packages[0].RenderedTag != "pkg-a-v1.2.3" {
		t.Errorf("RenderedTag = %q, want %q", report.Packages[0].RenderedTag, "pkg-a-v1.2.3")
	}
}

func TestCheckVersionAlignment_TagPattern_CollisionFlipsBothRows(t *testing.T) {
	root := t.TempDir()
	writeLocalPackage(t, root, "pkgs/a", "pkg-a", "1.0.0")
	writeLocalPackage(t, root, "pkgs/b", "pkg-b", "1.0.0")
	cfg := &authoring.AuthoringConfig{
		Version:    "1.0.0",
		Versioning: authoring.Versioning{Strategy: "tag_pattern"},
		// A pattern that ignores {name} entirely -- both packages render
		// the SAME tag from the SAME version, forcing a collision.
		Build: authoring.Build{TagPattern: "v{version}"},
		Packages: []authoring.PackageEntry{
			{Name: "pkg-a", Source: "./pkgs/a"},
			{Name: "pkg-b", Source: "./pkgs/b"},
		},
	}

	report := CheckVersionAlignment(cfg, root)
	if report.OK {
		t.Fatal("report.OK = true, want false (duplicate rendered tag)")
	}
	if len(report.Packages) != 2 {
		t.Fatalf("packages = %+v, want 2 rows", report.Packages)
	}
	for _, row := range report.Packages {
		if row.OK {
			t.Errorf("row %q OK = true, want both rows flipped to drift on collision", row.Path)
		}
	}
}

func TestCheckVersionAlignment_MissingApmYML_FallsBackToPluginJSON(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "pkgs", "a")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(`{"name":"pkg-a","version":"1.0.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &authoring.AuthoringConfig{
		Version:    "1.0.0",
		Versioning: authoring.Versioning{Strategy: "lockstep"},
		Packages:   []authoring.PackageEntry{{Name: "pkg-a", Source: "./pkgs/a"}},
	}

	report := CheckVersionAlignment(cfg, root)
	if !report.OK {
		t.Fatalf("report.OK = false, want true (plugin.json fallback); packages=%+v", report.Packages)
	}
	if report.Packages[0].Version != "1.0.0" {
		t.Errorf("Version = %q, want the plugin.json version", report.Packages[0].Version)
	}
}

func TestCheckVersionAlignment_NoManifestAtAll(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "pkgs", "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := &authoring.AuthoringConfig{
		Version:    "1.0.0",
		Versioning: authoring.Versioning{Strategy: "lockstep"},
		Packages:   []authoring.PackageEntry{{Name: "pkg-a", Source: "./pkgs/a"}},
	}

	report := CheckVersionAlignment(cfg, root)
	if report.OK {
		t.Fatal("report.OK = true, want false (no apm.yml or plugin.json at all)")
	}
	if report.Packages[0].Reason != "no_apm_yml" {
		t.Errorf("reason = %q, want %q", report.Packages[0].Reason, "no_apm_yml")
	}
	msgs := report.ErrorMessages()
	if len(msgs) != 1 || msgs[0] != "pkgs/a: no apm.yml or plugin.json found" {
		t.Errorf("ErrorMessages() = %v", msgs)
	}
}

func TestCheckVersionAlignment_MissingVersionField(t *testing.T) {
	root := t.TempDir()
	writeLocalPackage(t, root, "pkgs/a", "pkg-a", "")
	cfg := &authoring.AuthoringConfig{
		Version:    "1.0.0",
		Versioning: authoring.Versioning{Strategy: "lockstep"},
		Packages:   []authoring.PackageEntry{{Name: "pkg-a", Source: "./pkgs/a"}},
	}

	report := CheckVersionAlignment(cfg, root)
	if report.OK {
		t.Fatal("report.OK = true, want false")
	}
	if report.Packages[0].Reason != "missing_version" {
		t.Errorf("reason = %q, want %q", report.Packages[0].Reason, "missing_version")
	}
}

func TestCheckVersionAlignment_SkipsRemotePackages(t *testing.T) {
	root := t.TempDir()
	cfg := &authoring.AuthoringConfig{
		Version:    "1.0.0",
		Versioning: authoring.Versioning{Strategy: "lockstep"},
		Packages:   []authoring.PackageEntry{{Name: "remote-pkg", Source: "owner/repo"}},
	}

	report := CheckVersionAlignment(cfg, root)
	if !report.OK {
		t.Fatal("report.OK = false, want true (no local packages to check)")
	}
	if len(report.Packages) != 0 {
		t.Errorf("packages = %+v, want none (remote sources are out of scope)", report.Packages)
	}
}

func TestVersionAlignmentReport_ErrorMessages_DuplicateTag(t *testing.T) {
	root := t.TempDir()
	writeLocalPackage(t, root, "pkgs/a", "pkg-a", "1.0.0")
	writeLocalPackage(t, root, "pkgs/b", "pkg-b", "1.0.0")
	cfg := &authoring.AuthoringConfig{
		Version:    "1.0.0",
		Versioning: authoring.Versioning{Strategy: "tag_pattern"},
		Build:      authoring.Build{TagPattern: "v{version}"},
		Packages: []authoring.PackageEntry{
			{Name: "pkg-a", Source: "./pkgs/a"},
			{Name: "pkg-b", Source: "./pkgs/b"},
		},
	}

	report := CheckVersionAlignment(cfg, root)
	for _, msg := range report.ErrorMessages() {
		if !strings.Contains(msg, "rendered tag collides with") {
			t.Errorf("ErrorMessages() entry = %q, want it to name the collision", msg)
		}
	}
}
