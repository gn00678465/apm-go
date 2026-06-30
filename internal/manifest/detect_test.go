package manifest

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestDetectTargets_AllSignals(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(dir string)
		expected []string
	}{
		{"claude dir", func(d string) { os.MkdirAll(filepath.Join(d, ".claude"), 0755) }, []string{"claude"}},
		{"CLAUDE.md file", func(d string) { os.WriteFile(filepath.Join(d, "CLAUDE.md"), []byte(""), 0644) }, []string{"claude"}},
		{"codex dir", func(d string) { os.MkdirAll(filepath.Join(d, ".codex"), 0755) }, []string{"codex"}},
		{"opencode dir", func(d string) { os.MkdirAll(filepath.Join(d, ".opencode"), 0755) }, []string{"opencode"}},
		{"copilot instructions file", func(d string) {
			os.MkdirAll(filepath.Join(d, ".github"), 0755)
			os.WriteFile(filepath.Join(d, ".github", "copilot-instructions.md"), []byte(""), 0644)
		}, []string{"copilot"}},
		{"copilot instructions dir", func(d string) {
			os.MkdirAll(filepath.Join(d, ".github", "instructions"), 0755)
		}, []string{"copilot"}},
		{"copilot agents dir", func(d string) {
			os.MkdirAll(filepath.Join(d, ".github", "agents"), 0755)
		}, []string{"copilot"}},
		{"copilot prompts dir", func(d string) {
			os.MkdirAll(filepath.Join(d, ".github", "prompts"), 0755)
		}, []string{"copilot"}},
		{"copilot hooks dir", func(d string) {
			os.MkdirAll(filepath.Join(d, ".github", "hooks"), 0755)
		}, []string{"copilot"}},
		{"antigravity GEMINI.md", func(d string) {
			os.WriteFile(filepath.Join(d, "GEMINI.md"), []byte(""), 0644)
		}, []string{"antigravity"}},
		{"antigravity AGENTS.md", func(d string) {
			os.WriteFile(filepath.Join(d, "AGENTS.md"), []byte(""), 0644)
		}, []string{"antigravity"}},
		{"no signals", func(d string) {}, nil},
		{"bare .github dir does NOT trigger copilot", func(d string) {
			os.MkdirAll(filepath.Join(d, ".github"), 0755)
		}, nil},
		{"multiple signals", func(d string) {
			os.MkdirAll(filepath.Join(d, ".claude"), 0755)
			os.MkdirAll(filepath.Join(d, ".codex"), 0755)
		}, []string{"claude", "codex"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			tt.setup(dir)
			got := DetectTargets(dir)
			// Copy before sorting to avoid mutating the test table (S-006).
			gotSorted := slices.Clone(got)
			expSorted := slices.Clone(tt.expected)
			slices.Sort(gotSorted)
			slices.Sort(expSorted)
			if !slices.Equal(gotSorted, expSorted) {
				t.Errorf("expected %v, got %v", tt.expected, got)
			}
		})
	}
}
