package main

import (
	"os"
	"testing"
	"time"

	"charm.land/huh/v2"

	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/ux"
	"github.com/spf13/cobra"
)

// interactiveCapture records what the stubbed prompt seams observed during
// one driven run of an interactive init command (consumer or plugin), for
// the Form/MultiSelect/Confirm-branch tests below (AC38/AC41), plus the
// ordered clack transcript call sequence (AC52; see clackEvents' own
// comment on driveInteractiveInit below for why this replaced matching
// text markers in captured output).
type interactiveCapture struct {
	formFields   []ux.Field
	selectOpts   []ux.Option
	confirmCalls int
	runErr       error
	clackEvents  []string
}

// driveInteractiveInit runs cmd (initCmd() or pluginInitCmd()) end to end
// with the TTY seam forced true (ux.SetTTYSeamsForTest, an existing
// AC-L0 seam from 07-29-targets-init-shape) and every prompt seam stubbed:
// MultiSelect always returns []string{"claude"} (non-empty, so
// interactiveTargetSelect resolves on its first pass) and Confirm always
// returns confirmAnswer. It runs the command in a goroutine and applies a
// timeout so an unstubbed prompt that would otherwise block forever fails
// the test instead of hanging it.
func driveInteractiveInit(t *testing.T, cmd *cobra.Command, args []string, confirmAnswer bool) *interactiveCapture {
	t.Helper()

	dir := t.TempDir()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { os.Chdir(origDir) })

	t.Setenv("CI", "")
	restoreTTY := ux.SetTTYSeamsForTest(true, true, true)
	t.Cleanup(restoreTTY)

	cap := &interactiveCapture{}
	// A-MAJOR-1 (external audit round 6, 2026-07-31): records the ordered
	// clack transcript call sequence directly via ux.SetClackEventHookForTest
	// (AC52), rather than inferring it from literal text substrings matched
	// in captured stderr -- see that seam's own doc comment (internal/ux/
	// clackhook_shim.go) for why the text-marker approach is fragile (wording
	// drift, and Banner in particular never appearing on a non-Unicode
	// terminal even though it was genuinely called).
	//
	// A-MINOR-1 (external audit round 7, 2026-07-31 follow-up):
	// ux.SetClackEventHookForTest itself now only exists in a
	// `-tags apm_test_hooks` build (that seam's own doc comment explains
	// why). Calling it directly here would make this whole file -- all 6
	// tests, not just TestInitVsPluginInit_ClackSequenceParity below, which
	// is the only one that actually asserts on cap.clackEvents -- fail to
	// compile under a plain, untagged `go test ./...`. installClackEvent
	// Recorder is the indirection that avoids that: it has two build-tag
	// variant implementations (plugin_init_clackhook_enabled_test.go,
	// plugin_init_clackhook_disabled_test.go), so this file itself carries
	// no build tag and every test below it still runs untagged -- only with
	// clack-event recording silently disabled (cap.clackEvents stays empty),
	// which is exactly why TestInitVsPluginInit_ClackSequenceParity is split
	// into its own tagged file (plugin_init_clacksequence_test.go) instead of
	// living here.
	restoreClackEvents := installClackEventRecorder(cap)
	t.Cleanup(restoreClackEvents)
	restorePrompt := ux.SetPromptSeamsForTest(
		func(theme huh.Theme, prompt string, def bool) (bool, error) {
			cap.confirmCalls++
			return confirmAnswer, nil
		},
		func(theme huh.Theme, title, description string, showHelp bool, opts []ux.Option) ([]string, error) {
			cap.selectOpts = opts
			return []string{"claude"}, nil
		},
		func(theme huh.Theme, title string, showHelp bool, fields []ux.Field) (map[string]string, error) {
			cap.formFields = fields
			values := make(map[string]string, len(fields))
			for _, f := range fields {
				values[f.Key] = f.Default
			}
			return values, nil
		},
	)
	t.Cleanup(restorePrompt)

	done := make(chan error, 1)
	go func() {
		cmd.SetArgs(args)
		done <- cmd.Execute()
	}()
	select {
	case err := <-done:
		cap.runErr = err
	case <-time.After(5 * time.Second):
		t.Fatal("interactive run did not return within timeout; it likely blocked on an unstubbed prompt")
	}
	return cap
}

func formFieldDefault(fields []ux.Field, key string) string {
	for _, f := range fields {
		if f.Key == key {
			return f.Default
		}
	}
	return ""
}

// ── AC41: Form/MultiSelect/Confirm each get an independent assertion ──
// (not just a total-count check that three interactive-branch tests exist).

// TestPluginInitInteractive_Form_DefaultsMatchModeAndPrefill drives plugin
// init's Form branch and asserts on the actual field defaults it builds:
// the name is prefilled from the resolved project directory, and -- R3.3.b
// -- the version default stays "1.0.0" in the interactive form even though
// plugin mode's --yes default is "0.1.0" (mode.defaultYesVer must not leak
// into the interactive form's Default).
func TestPluginInitInteractive_Form_DefaultsMatchModeAndPrefill(t *testing.T) {
	cap := driveInteractiveInit(t, pluginInitCmd(), []string{"my-plugin"}, true)
	if cap.runErr != nil {
		t.Fatalf("plugin init failed: %v", cap.runErr)
	}
	if cap.formFields == nil {
		t.Fatal("inputFormWith seam was never reached; Form branch not exercised")
	}

	if got := formFieldDefault(cap.formFields, "name"); got != "my-plugin" {
		t.Errorf("Form name default = %q, want %q (prefilled from the resolved project directory)", got, "my-plugin")
	}
	if got := formFieldDefault(cap.formFields, "version"); got != "1.0.0" {
		t.Errorf("Form version default = %q, want %q (R3.3.b: interactive default is 1.0.0 for both modes, unlike --yes's 0.1.0)", got, "1.0.0")
	}
}

// TestPluginInitInteractive_MultiSelect_OffersFullSupportedTargetsMenu
// drives plugin init's MultiSelect branch and asserts the option set it
// builds has exactly one entry per manifest.PromptTargets -- proving plugin
// mode reuses the same target menu as consumer init rather than a
// bespoke/truncated one. manifest.PromptTargets (not the full
// manifest.SupportedTargets) is the correct comparison set as of 2026-08-02:
// explicit-only targets (antigravity, agent-skills) are --target-selectable
// but deliberately excluded from the interactive menu, parity with Python
// apm_cli's EXPLICIT_ONLY_TARGETS filter (commands/init.py:629, v0.26.0).
func TestPluginInitInteractive_MultiSelect_OffersFullSupportedTargetsMenu(t *testing.T) {
	cap := driveInteractiveInit(t, pluginInitCmd(), []string{"my-plugin"}, true)
	if cap.runErr != nil {
		t.Fatalf("plugin init failed: %v", cap.runErr)
	}
	if cap.selectOpts == nil {
		t.Fatal("multiSelectWith seam was never reached; MultiSelect branch not exercised")
	}
	if len(cap.selectOpts) != len(manifest.PromptTargets) {
		t.Errorf("MultiSelect offered %d options, want %d (one per manifest.PromptTargets)",
			len(cap.selectOpts), len(manifest.PromptTargets))
	}
	for _, o := range cap.selectOpts {
		if manifest.ExplicitOnlyTargets[o.Value] {
			t.Errorf("MultiSelect offered explicit-only target %q; plugin init must exclude it same as consumer init", o.Value)
		}
	}
}

// TestPluginInitInteractive_Confirm_DeclineAbortsWithoutWritingFiles drives
// plugin init's final "Is this OK?" Confirm branch with a declined answer
// and asserts the run is a no-op: no error (Aborted. is not a failure), and
// neither apm.yml nor plugin.json get written -- proving the Confirm gate
// is load-bearing, not a call that's made but ignored.
func TestPluginInitInteractive_Confirm_DeclineAbortsWithoutWritingFiles(t *testing.T) {
	cap := driveInteractiveInit(t, pluginInitCmd(), []string{"my-plugin"}, false)
	if cap.runErr != nil {
		t.Fatalf("plugin init returned an error on declined confirmation, want nil (Aborted. is not an error): %v", cap.runErr)
	}
	if cap.confirmCalls == 0 {
		t.Fatal("confirmWith seam was never reached; Confirm branch not exercised")
	}
	// runInitCore has already chdir'd into "my-plugin" by this point
	// (Phase 1 runs before the Phase 5 Confirm gate), so these paths are
	// relative to that directory.
	if _, err := os.Stat("apm.yml"); err == nil {
		t.Error("apm.yml was written despite the final confirmation being declined")
	}
	if _, err := os.Stat("plugin.json"); err == nil {
		t.Error("plugin.json was written despite the final confirmation being declined")
	}
}

// ── AC38: interactive version default is 1.0.0 for BOTH modes ──
// (a universal claim -- each mode gets its own dedicated test, not one test
// that only checks one side).

// TestInitInteractiveVersionDefault is AC38's consumer-mode half.
func TestInitInteractiveVersionDefault(t *testing.T) {
	cap := driveInteractiveInit(t, initCmd(), nil, true)
	if cap.runErr != nil {
		t.Fatalf("init failed: %v", cap.runErr)
	}
	if got := formFieldDefault(cap.formFields, "version"); got != "1.0.0" {
		t.Errorf("consumer init interactive version default = %q, want %q", got, "1.0.0")
	}
}

// TestPluginInitInteractiveVersionDefault is AC38's plugin-mode half: the
// interactive form's version default must stay "1.0.0" even though plugin
// mode's --yes default is "0.1.0" (mode.defaultYesVer must not leak into
// the Field.Default used for the interactive form, R3.3.b).
func TestPluginInitInteractiveVersionDefault(t *testing.T) {
	cap := driveInteractiveInit(t, pluginInitCmd(), []string{"my-plugin"}, true)
	if cap.runErr != nil {
		t.Fatalf("plugin init failed: %v", cap.runErr)
	}
	if got := formFieldDefault(cap.formFields, "version"); got != "1.0.0" {
		t.Errorf("plugin init interactive version default = %q, want %q (must stay 1.0.0 even though --yes defaults to 0.1.0)", got, "1.0.0")
	}
}

// ── AC52: plugin init walks the same clack transcript sequence as consumer
// init (R11.1/R11.2, design.md D12). See
// plugin_init_clacksequence_test.go for TestInitVsPluginInit_
// ClackSequenceParity itself -- split into its own apm_test_hooks-tagged
// file (A-MINOR-1, external audit round 7, 2026-07-31 follow-up: it is the
// only test in this package that actually asserts on
// interactiveCapture.clackEvents, which stays empty without that tag). ──
