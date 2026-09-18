package deploy

import (
	"os"
	"path/filepath"
	"testing"
)

func setSymlinkMode(t *testing.T, on bool) {
	t.Helper()
	old := activeDeployMode.symlink
	activeDeployMode.symlink = on
	t.Cleanup(func() { activeDeployMode.symlink = old })
}

func makeSkillSource(t *testing.T, dir, name string, files map[string]string) string {
	t.Helper()
	skillDir := filepath.Join(dir, name)
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}
	for f, content := range files {
		p := filepath.Join(skillDir, f)
		os.MkdirAll(filepath.Dir(p), 0755)
		if err := os.WriteFile(p, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return skillDir
}

func TestDeploySkillTo_SymlinkMode(t *testing.T) {
	setSymlinkMode(t, true)

	srcDir := t.TempDir()
	skillSrc := makeSkillSource(t, srcDir, "my-skill", map[string]string{
		"SKILL.md": "# My Skill",
		"lib.md":   "helper",
	})

	projectDir := t.TempDir()
	p := Primitive{Name: "my-skill", Type: TypeSkills, SrcPath: skillSrc}

	files, err := deploySkillTo(p, projectDir, ".claude/skills")
	if err != nil {
		t.Fatalf("deploySkillTo: %v", err)
	}

	dest := filepath.Join(projectDir, ".claude", "skills", "my-skill")
	info, err := os.Lstat(dest)
	if err != nil {
		t.Fatalf("Lstat: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("destination should be a symlink")
	}

	data, err := os.ReadFile(filepath.Join(dest, "SKILL.md"))
	if err != nil {
		t.Fatalf("read through symlink: %v", err)
	}
	if string(data) != "# My Skill" {
		t.Errorf("content = %q, want %q", data, "# My Skill")
	}

	if len(files) != 2 {
		t.Errorf("deployed files count = %d, want 2", len(files))
	}
}

func TestDeploySkillTo_CopyMode(t *testing.T) {
	setSymlinkMode(t, false)

	srcDir := t.TempDir()
	skillSrc := makeSkillSource(t, srcDir, "my-skill", map[string]string{
		"SKILL.md": "# Copy Skill",
	})

	projectDir := t.TempDir()
	p := Primitive{Name: "my-skill", Type: TypeSkills, SrcPath: skillSrc}

	files, err := deploySkillTo(p, projectDir, ".agents/skills")
	if err != nil {
		t.Fatalf("deploySkillTo: %v", err)
	}

	dest := filepath.Join(projectDir, ".agents", "skills", "my-skill")
	info, err := os.Lstat(dest)
	if err != nil {
		t.Fatalf("Lstat: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Error("destination should NOT be a symlink in copy mode")
	}
	if !info.IsDir() {
		t.Error("destination should be a regular directory")
	}

	data, err := os.ReadFile(filepath.Join(dest, "SKILL.md"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != "# Copy Skill" {
		t.Errorf("content = %q", data)
	}

	if len(files) != 1 {
		t.Errorf("deployed files count = %d, want 1", len(files))
	}
}

func TestDeployFileToPath_SymlinkMode(t *testing.T) {
	setSymlinkMode(t, true)

	srcDir := t.TempDir()
	srcFile := filepath.Join(srcDir, "rules.md")
	if err := os.WriteFile(srcFile, []byte("rule content"), 0644); err != nil {
		t.Fatal(err)
	}

	projectDir := t.TempDir()
	p := Primitive{Name: "rules", Type: TypeInstructions, SrcPath: srcFile}

	files, err := deployFileToPath(p, ".claude/rules/rules.md", projectDir)
	if err != nil {
		t.Fatalf("deployFileToPath: %v", err)
	}

	dest := filepath.Join(projectDir, ".claude", "rules", "rules.md")
	info, err := os.Lstat(dest)
	if err != nil {
		t.Fatalf("Lstat: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("destination should be a symlink")
	}

	target, err := os.Readlink(dest)
	if err != nil {
		t.Fatalf("Readlink: %v", err)
	}
	if !filepath.IsAbs(target) {
		t.Errorf("symlink target should be absolute, got %q", target)
	}

	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read through symlink: %v", err)
	}
	if string(data) != "rule content" {
		t.Errorf("content = %q", data)
	}

	if len(files) != 1 || files[0] != ".claude/rules/rules.md" {
		t.Errorf("files = %v", files)
	}
}

func TestDeployFileToPath_CopyMode(t *testing.T) {
	setSymlinkMode(t, false)

	srcDir := t.TempDir()
	srcFile := filepath.Join(srcDir, "agent.md")
	if err := os.WriteFile(srcFile, []byte("agent content"), 0644); err != nil {
		t.Fatal(err)
	}

	projectDir := t.TempDir()
	p := Primitive{Name: "helper", Type: TypeAgents, SrcPath: srcFile}

	files, err := deployFileToPath(p, ".agents/agents/helper.md", projectDir)
	if err != nil {
		t.Fatalf("deployFileToPath: %v", err)
	}

	dest := filepath.Join(projectDir, ".agents", "agents", "helper.md")
	info, err := os.Lstat(dest)
	if err != nil {
		t.Fatalf("Lstat: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Error("destination should NOT be a symlink in copy mode")
	}

	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != "agent content" {
		t.Errorf("content = %q", data)
	}

	if len(files) != 1 || files[0] != ".agents/agents/helper.md" {
		t.Errorf("files = %v", files)
	}
}
