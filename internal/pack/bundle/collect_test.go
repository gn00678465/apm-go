package bundle

import (
	"os"
	"path/filepath"
	"testing"
)

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func outputRels(cs []Component) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.OutputRel
	}
	return out
}

func containsRel(cs []Component, rel string) bool {
	for _, c := range cs {
		if c.OutputRel == rel {
			return true
		}
	}
	return false
}

func TestCollectAPMComponents_AgentsFlat(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, ".apm", "agents", "foo.md"), "x")
	got := CollectAPMComponents(filepath.Join(dir, ".apm"))
	if !containsRel(got, "agents/foo.md") {
		t.Errorf("got = %v, want agents/foo.md", outputRels(got))
	}
}

func TestCollectAPMComponents_SkillsRecursive(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, ".apm", "skills", "demo", "SKILL.md"), "x")
	mustWriteFile(t, filepath.Join(dir, ".apm", "skills", "demo", "assets", "img.png"), "x")
	got := CollectAPMComponents(filepath.Join(dir, ".apm"))
	if !containsRel(got, "skills/demo/SKILL.md") || !containsRel(got, "skills/demo/assets/img.png") {
		t.Errorf("got = %v, want skills/demo/SKILL.md and skills/demo/assets/img.png", outputRels(got))
	}
}

func TestCollectAPMComponents_PromptsRenamedToCommands(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, ".apm", "prompts", "foo.prompt.md"), "x")
	got := CollectAPMComponents(filepath.Join(dir, ".apm"))
	if !containsRel(got, "commands/foo.md") {
		t.Errorf("got = %v, want commands/foo.md (renamed from foo.prompt.md)", outputRels(got))
	}
}

func TestCollectAPMComponents_InstructionsCommandsExtensions(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, ".apm", "instructions", "a.instructions.md"), "x")
	mustWriteFile(t, filepath.Join(dir, ".apm", "commands", "b.md"), "x")
	mustWriteFile(t, filepath.Join(dir, ".apm", "extensions", "c.json"), "x")
	got := CollectAPMComponents(filepath.Join(dir, ".apm"))
	for _, want := range []string{"instructions/a.instructions.md", "commands/b.md", "extensions/c.json"} {
		if !containsRel(got, want) {
			t.Errorf("got = %v, want %s", outputRels(got), want)
		}
	}
}

func TestCollectAPMComponents_MissingDir_ReturnsNil(t *testing.T) {
	got := CollectAPMComponents(filepath.Join(t.TempDir(), "nonexistent"))
	if len(got) != 0 {
		t.Errorf("got = %v, want empty", got)
	}
}

func TestCollectRootPluginComponents(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "agents", "foo.md"), "x")
	mustWriteFile(t, filepath.Join(dir, "skills", "demo", "SKILL.md"), "x")
	got := CollectRootPluginComponents(dir)
	if !containsRel(got, "agents/foo.md") || !containsRel(got, "skills/demo/SKILL.md") {
		t.Errorf("got = %v", outputRels(got))
	}
}

func TestCollectBareSkill_UsesVirtualPathSlug(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "SKILL.md"), "# skill")
	mustWriteFile(t, filepath.Join(dir, "helper.py"), "x")
	got := CollectBareSkill(dir, "frontend-design", "acme/skills", nil)
	if !containsRel(got, "skills/frontend-design/SKILL.md") || !containsRel(got, "skills/frontend-design/helper.py") {
		t.Errorf("got = %v", outputRels(got))
	}
}

func TestCollectBareSkill_FallsBackToRepoURLLastSegment(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "SKILL.md"), "# skill")
	got := CollectBareSkill(dir, "", "acme/my-skill", nil)
	if !containsRel(got, "skills/my-skill/SKILL.md") {
		t.Errorf("got = %v, want skills/my-skill/SKILL.md", outputRels(got))
	}
}

func TestCollectBareSkill_ExcludesManifestFiles(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "SKILL.md"), "# skill")
	mustWriteFile(t, filepath.Join(dir, "apm.yml"), "name: x")
	mustWriteFile(t, filepath.Join(dir, "apm.lock.yaml"), "lockfile_version: '1'")
	mustWriteFile(t, filepath.Join(dir, "plugin.json"), "{}")
	got := CollectBareSkill(dir, "", "acme/skill", nil)
	for _, excluded := range []string{"apm.yml", "apm.lock.yaml", "plugin.json"} {
		for _, c := range got {
			if filepath.Base(c.Source) == excluded {
				t.Errorf("bare skill collection included excluded file %s", excluded)
			}
		}
	}
}

func TestCollectBareSkill_NoSKILLMD_ReturnsNil(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "readme.md"), "x")
	got := CollectBareSkill(dir, "", "acme/skill", nil)
	if len(got) != 0 {
		t.Errorf("got = %v, want nil (no SKILL.md at root)", got)
	}
}

func TestCollectBareSkill_SkippedWhenSkillsAlreadyCollected(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "SKILL.md"), "# skill")
	existing := []Component{{OutputRel: "skills/other/SKILL.md"}}
	got := CollectBareSkill(dir, "", "acme/skill", existing)
	if len(got) != 0 {
		t.Errorf("got = %v, want nil (skills/ already collected via .apm/skills or root skills/)", got)
	}
}
