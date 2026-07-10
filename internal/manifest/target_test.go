package manifest

import (
	"strings"
	"testing"
)

func TestValidateTarget(t *testing.T) {
	tests := []struct {
		input   string
		want    string
		wantErr string // empty = no error; non-empty = error must contain this
	}{
		// canonical
		{"claude", "claude", ""},
		{"copilot", "copilot", ""},
		{"codex", "codex", ""},
		{"opencode", "opencode", ""},
		{"antigravity", "antigravity", ""},
		{"cursor", "cursor", ""},
		{"gemini", "gemini", ""},
		{"windsurf", "windsurf", ""},
		{"agent-skills", "agent-skills", ""},
		{"all", "all", ""},

		// aliases
		{"vscode", "copilot", ""},
		{"agents", "copilot", ""},
		{"agy", "antigravity", ""},

		// x-vendor (tg-004)
		{"x-acme-tool", "x-acme-tool", ""},
		{"x-my-custom", "x-my-custom", ""},
		{"x-a0-b1", "x-a0-b1", ""},

		// minimal rejected (mf-005)
		{"minimal", "", "minimal"},

		// unknown
		{"notarealtool", "", "notarealtool"},
		{"vim", "", "vim"},

		// x-vendor bad format (needs two segments)
		{"x-a", "", "unknown target"},
		{"x-", "", "unknown target"},
		{"x-acme", "", "unknown target"},

		// not x-vendor pattern
		{"X-acme-tool", "", "unknown target"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ValidateTarget(tt.input)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if got != tt.want {
					t.Errorf("got %q, want %q", got, tt.want)
				}
			} else {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.wantErr)
				} else if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error %q should contain %q", err.Error(), tt.wantErr)
				}
			}
		})
	}
}

func TestHasAdapter(t *testing.T) {
	tests := []struct {
		target string
		want   bool
	}{
		{"claude", true},
		{"codex", true},
		{"copilot", true},
		{"opencode", true},
		{"antigravity", true},
		{"agent-skills", true},
		{"gemini", false},
		{"cursor", false},
		{"windsurf", false},
		{"x-acme-tool", false},
	}
	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			if got := HasAdapter(tt.target); got != tt.want {
				t.Errorf("HasAdapter(%q) = %v, want %v", tt.target, got, tt.want)
			}
		})
	}
}
