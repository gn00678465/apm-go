package deploy

import "testing"

func TestTargetsGovernPath(t *testing.T) {
	const local, dep = LocalBucket, DependencyBucket
	all := []string{"claude", "codex", "copilot", "antigravity", "opencode", "agent-skills"}
	tests := []struct {
		name    string
		targets []string
		bucket  DeployBucket
		path    string
		want    bool
	}{
		{"claude governs its skills", []string{"claude"}, local, ".claude/skills/a/x.md", true},
		{"claude governs a dependency's skills", []string{"claude"}, dep, ".claude/skills/a/x.md", true},
		{"claude governs its rules", []string{"claude"}, local, ".claude/rules/x.md", true},
		{"codex does not govern claude", []string{"codex"}, local, ".claude/skills/a/x.md", false},
		{"codex governs shared skills", []string{"codex"}, local, ".agents/skills/a/x.md", true},
		{"codex governs a dependency's shared skills", []string{"codex"}, dep, ".agents/skills/a/x.md", true},
		{"codex does not govern antigravity rules", []string{"codex"}, local, ".agents/rules/x.md", false},
		{"codex does not govern antigravity bundles", []string{"codex"}, dep, ".agents/plugins/pkg/skills/a/x.md", false},
		{"codex governs its own root", []string{"codex"}, local, ".codex/agents/a.toml", true},
		{"copilot governs shared skills", []string{"copilot"}, dep, ".agents/skills/a/x.md", true},
		{"copilot governs .github", []string{"copilot"}, local, ".github/prompts/a.prompt.md", true},
		{"copilot does not govern antigravity hooks", []string{"copilot"}, local, ".agents/hooks.json", false},
		{"opencode governs shared skills", []string{"opencode"}, dep, ".agents/skills/a/x.md", true},
		{"opencode governs its own root", []string{"opencode"}, local, ".opencode/commands/a.md", true},
		{"agent-skills governs shared skills", []string{"agent-skills"}, local, ".agents/skills/a/x.md", true},
		{"agent-skills does not govern antigravity agents", []string{"agent-skills"}, local, ".agents/agents/a/agent.md", false},
		{"claude does not govern shared skills", []string{"claude"}, local, ".agents/skills/a/x.md", false},

		{"antigravity local: skills", []string{"antigravity"}, local, ".agents/skills/a/x.md", true},
		{"antigravity local: rules", []string{"antigravity"}, local, ".agents/rules/x.md", true},
		{"antigravity local: agents", []string{"antigravity"}, local, ".agents/agents/a/agent.md", true},
		{"antigravity local: hooks file", []string{"antigravity"}, local, ".agents/hooks.json", true},
		{"antigravity local: not plugin bundles", []string{"antigravity"}, local, ".agents/plugins/pkg/x.md", false},
		{"antigravity local: not other .agents files", []string{"antigravity"}, local, ".agents/hooks.json.bak", false},
		{"antigravity dependency: plugin bundle skill", []string{"antigravity"}, dep, ".agents/plugins/pkg/skills/a/x.md", true},
		{"antigravity dependency: plugin manifest", []string{"antigravity"}, dep, ".agents/plugins/pkg/plugin.json", true},
		{"antigravity dependency: not shared skills", []string{"antigravity"}, dep, ".agents/skills/a/x.md", false},
		{"antigravity dependency: not flat rules", []string{"antigravity"}, dep, ".agents/rules/x.md", false},
		{"antigravity dependency: not flat hooks", []string{"antigravity"}, dep, ".agents/hooks.json", false},
		{"codex with antigravity governs a dependency's shared skills", []string{"antigravity", "codex"}, dep, ".agents/skills/a/x.md", true},

		{"any listed target is enough", []string{"codex", "claude"}, local, ".claude/skills/a/x.md", true},
		{"prefix match stops at a separator", all, local, ".claudex/a.md", false},
		{"skills prefix stops at a separator", []string{"codex"}, local, ".agents/skillsx/a.md", false},
		{"unknown location", all, dep, "docs/a.md", false},
		{"backslash path is normalized", []string{"claude"}, local, `.claude\skills\a\x.md`, true},
		{"dot segments are cleaned", []string{"claude"}, local, "./.claude/skills/../skills/a/x.md", true},
		{"escape through a governed root", []string{"claude"}, local, ".claude/../outside.md", false},
		{"unknown target", []string{"cursor"}, local, ".claude/skills/a/x.md", false},
		{"no targets", nil, local, ".claude/skills/a/x.md", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TargetsGovernPath(tt.targets, tt.bucket, tt.path); got != tt.want {
				t.Errorf("TargetsGovernPath(%v, %v, %q) = %v, want %v", tt.targets, tt.bucket, tt.path, got, tt.want)
			}
		})
	}
}

func TestTargetsGovernPath_EveryAdapterHasGovernedPaths(t *testing.T) {
	for name := range Adapters {
		roots := governedPaths[name]
		if len(roots.local) == 0 || len(roots.dependency) == 0 {
			t.Errorf("target %q lacks governedPaths for a bucket kind: its stale deployed files would never be cleaned", name)
		}
	}
}

func TestIsMCPConfigPath(t *testing.T) {
	for _, p := range []string{".mcp.json", ".codex/config.toml", ".github/mcp-config.json", ".agents/mcp_config.json", "opencode.json"} {
		if !IsMCPConfigPath(p) {
			t.Errorf("IsMCPConfigPath(%q) = false, want true", p)
		}
	}
	for _, p := range []string{`.codex\config.toml`, "./.mcp.json"} {
		if !IsMCPConfigPath(p) {
			t.Errorf("IsMCPConfigPath(%q) = false, want true (normalized form is an MCP config file)", p)
		}
	}
	for _, p := range []string{".claude/skills/a/x.md", ".codex/hooks.json", ".mcp.json.bak", "sub/.mcp.json"} {
		if IsMCPConfigPath(p) {
			t.Errorf("IsMCPConfigPath(%q) = true, want false", p)
		}
	}
}
