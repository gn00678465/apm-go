package main

import (
	"os"
	"testing"
	"time"

	"charm.land/huh/v2"

	"github.com/apm-go/apm/internal/ux"
)

// TestInteractiveTargetSelect_PreselectsExistingTargets is AC2/AC3: running
// interactive `apm-go init` against an apm.yml that already has a target
// selection (either the singular target: key or the plural targets: key)
// must pre-select those targets in the MultiSelect prompt. The only place
// that preselection exists is the opts slice passed into multiSelectWith,
// so this drives the real init command end to end with the ux prompt seams
// stubbed (SetPromptSeamsForTest, AC-L0) and captures the opts that reached
// the (would-be) MultiSelect field.
func TestInteractiveTargetSelect_PreselectsExistingTargets(t *testing.T) {
	tests := []struct {
		name        string
		apmYML      string
		wantTargets []string
	}{
		{
			name:        "singular target key",
			apmYML:      "name: p\nversion: \"1.0.0\"\ntarget:\n  - claude\n  - copilot\n",
			wantTargets: []string{"claude", "copilot"},
		},
		{
			name:        "plural targets key",
			apmYML:      "name: p\nversion: \"1.0.0\"\ntargets:\n  - claude\n  - copilot\n",
			wantTargets: []string{"claude", "copilot"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			dir := t.TempDir()
			origDir, _ := os.Getwd()
			if err := os.Chdir(dir); err != nil {
				t.Fatalf("chdir: %v", err)
			}
			t.Cleanup(func() { os.Chdir(origDir) })

			if err := os.WriteFile("apm.yml", []byte(tt.apmYML), 0644); err != nil {
				t.Fatal(err)
			}

			t.Setenv("CI", "")
			restoreTTY := ux.SetTTYSeamsForTest(true, true, true)
			t.Cleanup(restoreTTY)

			var capturedOpts []ux.Option
			restorePrompt := ux.SetPromptSeamsForTest(
				// Always accept: answers both the overwrite-confirmation and
				// the final "Is this OK?" confirmation.
				func(theme huh.Theme, prompt string, def bool) (bool, error) {
					return true, nil
				},
				func(theme huh.Theme, title, description string, showHelp bool, opts []ux.Option) ([]string, error) {
					capturedOpts = opts
					// Return a non-empty selection so interactiveTargetSelect
					// returns on its first pass instead of looping on the
					// "no targets selected" branch.
					return []string{"claude"}, nil
				},
				func(theme huh.Theme, title string, showHelp bool, fields []ux.Field) (map[string]string, error) {
					values := make(map[string]string, len(fields))
					for _, f := range fields {
						values[f.Key] = f.Default
					}
					return values, nil
				},
			)
			t.Cleanup(restorePrompt)

			// Act
			done := make(chan error, 1)
			go func() {
				cmd := initCmd()
				cmd.SetArgs([]string{})
				done <- cmd.Execute()
			}()

			select {
			case err := <-done:
				if err != nil {
					t.Fatalf("init failed: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("init did not return within timeout; it likely blocked on an unstubbed prompt")
			}

			// Assert
			if capturedOpts == nil {
				t.Fatal("multiSelectWith was never called; MultiSelect seam not reached")
			}
			selected := make(map[string]bool)
			for _, o := range capturedOpts {
				if o.Selected {
					selected[o.Value] = true
				}
			}
			for _, want := range tt.wantTargets {
				if !selected[want] {
					t.Errorf("target %q from existing apm.yml was not preselected in opts: %+v", want, capturedOpts)
				}
			}
		})
	}
}
