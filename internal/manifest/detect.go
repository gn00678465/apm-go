package manifest

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type TargetSignal struct {
	Path   string
	IsDir  bool
	Target string
}

// SignalWhitelist deliberately has NO antigravity entry: GEMINI.md and
// AGENTS.md are cross-tool files (also read by opencode/agent-skills
// tooling), so their presence must not auto-enable antigravity. It is
// explicit-only (user decision 2026-07-05, aligning with Python apm_cli's
// EXPLICIT_ONLY_TARGETS) -- select it via --target antigravity (alias agy)
// or apm.yml target:.
var SignalWhitelist = []TargetSignal{
	{".claude/", true, "claude"},
	{"CLAUDE.md", false, "claude"},
	{".github/copilot-instructions.md", false, "copilot"},
	{".codex/", true, "codex"},
	{".opencode/", true, "opencode"},
}

func DetectTargets(projectDir string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, sig := range SignalWhitelist {
		p := filepath.Join(projectDir, sig.Path)
		var exists bool
		if sig.IsDir {
			info, err := os.Stat(p)
			exists = err == nil && info.IsDir()
		} else {
			_, err := os.Stat(p)
			exists = err == nil
		}
		if exists && !seen[sig.Target] {
			seen[sig.Target] = true
			result = append(result, sig.Target)
		}
	}
	return result
}

func DetectAuthor() string {
	out, err := exec.Command("git", "config", "user.name").Output()
	if err != nil {
		return "Developer"
	}
	name := strings.TrimSpace(string(out))
	if name == "" {
		return "Developer"
	}
	return name
}
