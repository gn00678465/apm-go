package manifest

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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
	for _, sig := range foundSignals(projectDir) {
		if !seen[sig.Target] {
			seen[sig.Target] = true
			result = append(result, sig.Target)
		}
	}
	sort.Strings(result)
	return result
}

// DetectSignals names, sorted, each signal found under projectDir, a
// directory with its trailing "/": the source the Oracle names for an
// auto-detected target (core/target_detection.py:1070-1080,1138-1151).
func DetectSignals(projectDir string) []string {
	var paths []string
	for _, sig := range foundSignals(projectDir) {
		paths = append(paths, sig.Path)
	}
	sort.Strings(paths)
	return paths
}

func foundSignals(projectDir string) []TargetSignal {
	var found []TargetSignal
	for _, sig := range SignalWhitelist {
		info, err := os.Stat(filepath.Join(projectDir, sig.Path))
		if err == nil && (!sig.IsDir || info.IsDir()) {
			found = append(found, sig)
		}
	}
	return found
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
