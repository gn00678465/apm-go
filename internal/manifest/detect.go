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

// HarnessSignals is the Oracle's signal table, by which install and update
// detect a target (core/target_detection.py:1009-1027). It names targets
// apm-go has no adapter for (cursor, gemini, grok-build, windsurf, kiro):
// such a signal still makes a second harness.
var HarnessSignals = []TargetSignal{
	{".claude/", true, "claude"},
	{"CLAUDE.md", false, "claude"},
	{".cursor/", true, "cursor"},
	{".cursorrules", false, "cursor"},
	{".github/copilot-instructions.md", false, "copilot"},
	{".github/mcp.json", false, "copilot"},
	{".github/instructions/", true, "copilot"},
	{".github/agents/", true, "copilot"},
	{".github/prompts/", true, "copilot"},
	{".github/hooks/", true, "copilot"},
	{".codex/", true, "codex"},
	{".gemini/", true, "gemini"},
	{"GEMINI.md", false, "gemini"},
	{".grok/", true, "grok-build"},
	{".opencode/", true, "opencode"},
	{".windsurf/", true, "windsurf"},
	{".kiro/", true, "kiro"},
}

// DetectHarnesses names, sorted, each target a signal of HarnessSignals
// under projectDir detects.
func DetectHarnesses(projectDir string) []string {
	return uniqueTargets(foundHarnessSignals(projectDir))
}

func DetectTargets(projectDir string) []string {
	return uniqueTargets(foundSignals(projectDir, SignalWhitelist))
}

func uniqueTargets(signals []TargetSignal) []string {
	seen := make(map[string]bool)
	var result []string
	for _, sig := range signals {
		if !seen[sig.Target] {
			seen[sig.Target] = true
			result = append(result, sig.Target)
		}
	}
	sort.Strings(result)
	return result
}

// DetectSignals names, sorted, each signal of HarnessSignals found under
// projectDir, a directory with its trailing "/": the source the Oracle names
// for an auto-detected target (core/target_detection.py:1070-1080,1138-1151).
func DetectSignals(projectDir string) []string {
	var paths []string
	for _, sig := range foundHarnessSignals(projectDir) {
		paths = append(paths, sig.Path)
	}
	sort.Strings(paths)
	return paths
}

// foundHarnessSignals is foundSignals over HarnessSignals, where a file signal
// has to be a file, as detect_signals takes it with is_file.
func foundHarnessSignals(projectDir string) []TargetSignal {
	var found []TargetSignal
	for _, sig := range foundSignals(projectDir, HarnessSignals) {
		if info, err := os.Stat(filepath.Join(projectDir, sig.Path)); err == nil && (sig.IsDir || info.Mode().IsRegular()) {
			found = append(found, sig)
		}
	}
	return found
}

func foundSignals(projectDir string, signals []TargetSignal) []TargetSignal {
	var found []TargetSignal
	for _, sig := range signals {
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
