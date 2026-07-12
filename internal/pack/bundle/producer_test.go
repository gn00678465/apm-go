package bundle

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/yamlcore"
)

func baseOpts(t *testing.T, projectRoot, outputDir string) ProduceOptions {
	t.Helper()
	doc, err := yamlcore.SafeLoad([]byte("name: demo\nversion: 1.0.0\n"))
	if err != nil {
		t.Fatal(err)
	}
	return ProduceOptions{
		ProjectRoot: projectRoot,
		OutputDir:   outputDir,
		PkgName:     "demo",
		PkgVersion:  "1.0.0",
		Target:      "all",
		ApmYMLNode:  doc.Content[0],
	}
}

func TestProduce_LocalDepGuard_Errors(t *testing.T) {
	projectRoot := t.TempDir()
	opts := baseOpts(t, projectRoot, filepath.Join(projectRoot, "build"))
	opts.HasLocalDep = true
	var buf bytes.Buffer
	_, err := Produce(&buf, opts)
	if err == nil {
		t.Fatal("expected an error for a local dependency guard")
	}
}

func TestProduce_MinimalBundle_WritesPluginJSON(t *testing.T) {
	projectRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(projectRoot, ".apm", "agents", "foo.md"), "content")
	outputDir := filepath.Join(projectRoot, "build")
	opts := baseOpts(t, projectRoot, outputDir)

	var buf bytes.Buffer
	result, err := Produce(&buf, opts)
	if err != nil {
		t.Fatal(err)
	}
	if result.BundleDir != filepath.Join(outputDir, "demo-1.0.0") {
		t.Errorf("BundleDir = %s, want build/demo-1.0.0", result.BundleDir)
	}
	if _, statErr := os.Stat(filepath.Join(result.BundleDir, "agents", "foo.md")); statErr != nil {
		t.Errorf("expected agents/foo.md to be copied: %v", statErr)
	}
	data, rerr := os.ReadFile(filepath.Join(result.BundleDir, "plugin.json"))
	if rerr != nil {
		t.Fatal(rerr)
	}
	if !strings.Contains(string(data), `"name": "demo"`) {
		t.Errorf("plugin.json = %s, want synthesized name field", data)
	}
	if strings.HasSuffix(string(data), "\n") {
		t.Error("bundle's own plugin.json must NOT have a trailing newline (unlike write_plugin_manifest's standalone copy)")
	}
}

func TestProduce_OutputFiles_SortedAndAlwaysIncludesPluginJSON(t *testing.T) {
	projectRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(projectRoot, ".apm", "agents", "zebra.md"), "z")
	mustWriteFile(t, filepath.Join(projectRoot, ".apm", "agents", "apple.md"), "a")
	opts := baseOpts(t, projectRoot, filepath.Join(projectRoot, "build"))

	var buf bytes.Buffer
	result, err := Produce(&buf, opts)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"agents/apple.md", "agents/zebra.md", "plugin.json"}
	if len(result.Files) != len(want) {
		t.Fatalf("Files = %v, want %v", result.Files, want)
	}
	for i, w := range want {
		if result.Files[i] != w {
			t.Errorf("Files[%d] = %s, want %s (files must be sorted, plugin.json always last)", i, result.Files[i], w)
		}
	}
}

func TestProduce_DryRun_WritesNothingAndNeverScans(t *testing.T) {
	projectRoot := t.TempDir()
	// Embed a critical hidden character (bidi override) that would trigger
	// a scan warning if the scanner ran.
	mustWriteFile(t, filepath.Join(projectRoot, ".apm", "agents", "foo.md"), "safe‮text")
	outputDir := filepath.Join(projectRoot, "build")
	opts := baseOpts(t, projectRoot, outputDir)
	opts.DryRun = true

	var buf bytes.Buffer
	result, err := Produce(&buf, opts)
	if err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(outputDir); !os.IsNotExist(statErr) {
		t.Errorf("--dry-run must not create the output directory (stat err = %v)", statErr)
	}
	if len(result.Files) == 0 {
		t.Error("dry-run must still return the projected file list")
	}
	if strings.Contains(buf.String(), "hidden character") {
		t.Errorf("output = %q, dry-run must skip the security scan entirely (zero scanner invocations)", buf.String())
	}
}

func TestProduce_NonDryRun_CriticalCharWarnsButSucceeds(t *testing.T) {
	projectRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(projectRoot, ".apm", "agents", "foo.md"), "safe‮text")
	opts := baseOpts(t, projectRoot, filepath.Join(projectRoot, "build"))

	var buf bytes.Buffer
	_, err := Produce(&buf, opts)
	if err != nil {
		t.Fatalf("a hidden character must never block pack (WARN_POLICY): %v", err)
	}
	if !strings.Contains(buf.String(), "hidden character") {
		t.Errorf("output = %q, want a hidden-character warning", buf.String())
	}
}

func TestProduce_Force_HasNoEffectOnScan(t *testing.T) {
	projectRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(projectRoot, ".apm", "agents", "foo.md"), "safe‮text")
	opts := baseOpts(t, projectRoot, filepath.Join(projectRoot, "build"))
	opts.Force = true

	var buf bytes.Buffer
	_, err := Produce(&buf, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "hidden character") {
		t.Errorf("output = %q, --force must not suppress the scan warning", buf.String())
	}
}

func TestProduce_SanitizeBundleName_TraversalBecomesUnnamed(t *testing.T) {
	projectRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(projectRoot, ".apm", "agents", "foo.md"), "x")
	outputDir := filepath.Join(projectRoot, "build")
	opts := baseOpts(t, projectRoot, outputDir)
	opts.PkgName = "../../etc"
	opts.PkgVersion = "../../passwd"

	var buf bytes.Buffer
	result, err := Produce(&buf, opts)
	if err != nil {
		t.Fatal(err)
	}
	if result.BundleDir != filepath.Join(outputDir, "unnamed-unnamed") {
		t.Errorf("BundleDir = %s, want build/unnamed-unnamed (traversal name sanitized)", result.BundleDir)
	}
}

func TestProduce_DepVsRoot_FileMapCollision_DepWins(t *testing.T) {
	projectRoot := t.TempDir()
	depDir := t.TempDir()
	mustWriteFile(t, filepath.Join(depDir, ".apm", "agents", "foo.md"), "from dep")
	mustWriteFile(t, filepath.Join(projectRoot, ".apm", "agents", "foo.md"), "from root")

	opts := baseOpts(t, projectRoot, filepath.Join(projectRoot, "build"))
	opts.Deps = []DepSource{{Name: "acme/dep", InstallPath: depDir}}

	var buf bytes.Buffer
	result, err := Produce(&buf, opts)
	if err != nil {
		t.Fatal(err)
	}
	data, rerr := os.ReadFile(filepath.Join(result.BundleDir, "agents", "foo.md"))
	if rerr != nil {
		t.Fatal(rerr)
	}
	if string(data) != "from dep" {
		t.Errorf("agents/foo.md = %q, want the dependency's file to win over root (no --force)", data)
	}
	if !strings.Contains(buf.String(), "collision") {
		t.Errorf("output = %q, want a collision warning", buf.String())
	}
}

func TestProduce_HooksMerge_RootWinsOverDep(t *testing.T) {
	projectRoot := t.TempDir()
	depDir := t.TempDir()
	mustWriteFile(t, filepath.Join(depDir, ".apm", "hooks", "h.json"), `{"PreToolUse":"dep-hook"}`)
	mustWriteFile(t, filepath.Join(projectRoot, ".apm", "hooks", "h.json"), `{"PreToolUse":"root-hook"}`)

	opts := baseOpts(t, projectRoot, filepath.Join(projectRoot, "build"))
	opts.Deps = []DepSource{{Name: "acme/dep", InstallPath: depDir}}

	var buf bytes.Buffer
	result, err := Produce(&buf, opts)
	if err != nil {
		t.Fatal(err)
	}
	data, rerr := os.ReadFile(filepath.Join(result.BundleDir, "hooks.json"))
	if rerr != nil {
		t.Fatal(rerr)
	}
	if !strings.Contains(string(data), "root-hook") || strings.Contains(string(data), "dep-hook") {
		t.Errorf("hooks.json = %s, want root's hook to win over the dependency's (opposite of file_map)", data)
	}
}

func TestProduce_EmbedsPackLockfile_WhenLockfileProvided(t *testing.T) {
	projectRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(projectRoot, ".apm", "agents", "foo.md"), "x")
	opts := baseOpts(t, projectRoot, filepath.Join(projectRoot, "build"))
	opts.Lockfile = &lockfile.Lockfile{Version: "1"}
	opts.Target = "claude"

	var buf bytes.Buffer
	result, err := Produce(&buf, opts)
	if err != nil {
		t.Fatal(err)
	}
	data, rerr := os.ReadFile(filepath.Join(result.BundleDir, "apm.lock.yaml"))
	if rerr != nil {
		t.Fatal(rerr)
	}
	text := string(data)
	if !strings.HasPrefix(text, "pack:") {
		t.Fatalf("apm.lock.yaml = %s, want it to start with the pack: section", text)
	}
	if !strings.Contains(text, "target: claude") {
		t.Errorf("apm.lock.yaml = %s, want target: claude", text)
	}
	if !strings.Contains(text, "bundle_files:") {
		t.Errorf("apm.lock.yaml = %s, want a bundle_files manifest", text)
	}
	if strings.Contains(text, "sha256:") {
		t.Errorf("apm.lock.yaml = %s, bundle_files hashes must be bare hex (no sha256: envelope)", text)
	}
}

func TestProduce_NoLockfile_NoEmbeddedLockfile(t *testing.T) {
	projectRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(projectRoot, ".apm", "agents", "foo.md"), "x")
	opts := baseOpts(t, projectRoot, filepath.Join(projectRoot, "build"))

	var buf bytes.Buffer
	result, err := Produce(&buf, opts)
	if err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(filepath.Join(result.BundleDir, "apm.lock.yaml")); !os.IsNotExist(statErr) {
		t.Errorf("apm.lock.yaml must not be written when no lockfile was found (stat err = %v)", statErr)
	}
}

func TestProduce_ExistingPluginJSON_StripsSchemaInvalidKeys(t *testing.T) {
	projectRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(projectRoot, ".apm", "agents", "foo.md"), "x")
	mustWriteFile(t, filepath.Join(projectRoot, "plugin.json"),
		`{"name":"demo","skills":["./extra.md"],"commands":["./cmd.md"]}`)
	opts := baseOpts(t, projectRoot, filepath.Join(projectRoot, "build"))

	var buf bytes.Buffer
	result, err := Produce(&buf, opts)
	if err != nil {
		t.Fatal(err)
	}
	data, rerr := os.ReadFile(filepath.Join(result.BundleDir, "plugin.json"))
	if rerr != nil {
		t.Fatal(rerr)
	}
	if strings.Contains(string(data), "skills") || strings.Contains(string(data), "commands") {
		t.Errorf("plugin.json = %s, want schema-invalid keys (skills/commands) stripped", data)
	}
	if !strings.Contains(buf.String(), "Stripped schema-invalid keys") {
		t.Errorf("output = %q, want a strip warning naming the removed keys", buf.String())
	}
}

func TestProduce_RootLevelHooksJSON_Merged(t *testing.T) {
	projectRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(projectRoot, ".apm", "agents", "foo.md"), "x")
	mustWriteFile(t, filepath.Join(projectRoot, "hooks.json"), `{"PreToolUse":"root-level-hook"}`)
	opts := baseOpts(t, projectRoot, filepath.Join(projectRoot, "build"))

	var buf bytes.Buffer
	result, err := Produce(&buf, opts)
	if err != nil {
		t.Fatal(err)
	}
	data, rerr := os.ReadFile(filepath.Join(result.BundleDir, "hooks.json"))
	if rerr != nil {
		t.Fatal(rerr)
	}
	if !strings.Contains(string(data), "root-level-hook") {
		t.Errorf("hooks.json = %s, want the root-level (non-.apm) hooks.json merged in", data)
	}
}

func TestProduce_AuthorField_RenderedInPluginJSON(t *testing.T) {
	projectRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(projectRoot, ".apm", "agents", "foo.md"), "x")
	doc, err := yamlcore.SafeLoad([]byte("name: demo\nversion: 1.0.0\nauthor: Jane Doe\nkeywords: [a, b]\n"))
	if err != nil {
		t.Fatal(err)
	}
	opts := baseOpts(t, projectRoot, filepath.Join(projectRoot, "build"))
	opts.ApmYMLNode = doc.Content[0]

	var buf bytes.Buffer
	result, err := Produce(&buf, opts)
	if err != nil {
		t.Fatal(err)
	}
	data, rerr := os.ReadFile(filepath.Join(result.BundleDir, "plugin.json"))
	if rerr != nil {
		t.Fatal(rerr)
	}
	text := string(data)
	if !strings.Contains(text, `"author"`) || !strings.Contains(text, `"Jane Doe"`) {
		t.Errorf("plugin.json = %s, want author.name rendered", text)
	}
	if !strings.Contains(text, `"keywords"`) || !strings.Contains(text, `"a"`) {
		t.Errorf("plugin.json = %s, want keywords array rendered", text)
	}
}
