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

var SignalWhitelist = []TargetSignal{
	{".claude/", true, "claude"},
	{"CLAUDE.md", false, "claude"},
	{".github/copilot-instructions.md", false, "copilot"},
	{".github/instructions/", true, "copilot"},
	{".github/agents/", true, "copilot"},
	{".github/prompts/", true, "copilot"},
	{".github/hooks/", true, "copilot"},
	{".codex/", true, "codex"},
	{".opencode/", true, "opencode"},
	{"GEMINI.md", false, "antigravity"},
	{"AGENTS.md", false, "antigravity"},
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
