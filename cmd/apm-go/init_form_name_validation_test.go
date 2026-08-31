package main

import (
	"os"
	"strings"
	"testing"
	"time"

	"charm.land/huh/v2"

	"github.com/apm-go/apm/internal/ux"
	"github.com/spf13/cobra"
)

// driveInteractiveInitWithNameOverride is driveInteractiveInit
// (plugin_init_interactive_test.go) with one difference: the stubbed Form
// seam returns nameOverride for the "name" field instead of that field's
// Default, simulating a user editing the prefilled project name in the
// interactive form.
//
// A-MAJOR-1 (external audit): Phase 1 of runInitCore (init.go) only
// validates the resolved name when it comes from the explicit PROJECT-NAME
// arg, or (plugin mode) the current directory's basename when the arg is
// omitted. It never validated a name typed into THIS form -- so
// `apm-go plugin init my-plugin` (a valid arg) followed by editing the
// form's prefilled "my-plugin" to "My_Plugin" wrote an invalid plugin name
// straight into apm.yml/plugin.json with no error. These tests drive that
// exact path.
func driveInteractiveInitWithNameOverride(t *testing.T, cmd *cobra.Command, args []string, nameOverride string) *interactiveCapture {
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
	restoreClackEvents := installClackEventRecorder(cap)
	t.Cleanup(restoreClackEvents)
	restorePrompt := ux.SetPromptSeamsForTest(
		func(theme huh.Theme, prompt string, def bool) (bool, error) {
			cap.confirmCalls++
			return true, nil
		},
		func(theme huh.Theme, title, description string, showHelp bool, opts []ux.Option) ([]string, error) {
			cap.selectOpts = opts
			return []string{"claude"}, nil
		},
		func(theme huh.Theme, title string, showHelp bool, fields []ux.Field) (map[string]string, error) {
			cap.formFields = fields
			values := make(map[string]string, len(fields))
			for _, f := range fields {
				if f.Key == "name" {
					values[f.Key] = nameOverride
					continue
				}
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

// TestPluginInitInteractive_Form_InvalidNameRejected covers A-MAJOR-1: a user
// who edits the prefilled project name in plugin init's interactive form to
// an invalid (non-kebab-case) value must be rejected with the same
// "invalid plugin name" error Phase 1's arg/cwd-basename validation returns
// -- not silently written into apm.yml/plugin.json.
func TestPluginInitInteractive_Form_InvalidNameRejected(t *testing.T) {
	cap := driveInteractiveInitWithNameOverride(t, pluginInitCmd(), []string{"my-plugin"}, "My_Plugin")
	if cap.runErr == nil {
		t.Fatal("plugin init returned no error for an invalid form-entered name (My_Plugin), want an error")
	}
	if !strings.Contains(cap.runErr.Error(), "invalid plugin name") {
		t.Errorf("error = %q, want it to contain %q", cap.runErr.Error(), "invalid plugin name")
	}
	// runInitCore has already chdir'd into "my-plugin" by this point
	// (Phase 1 runs before the Phase 3 form), so these paths are relative
	// to that directory.
	if _, err := os.Stat("apm.yml"); err == nil {
		t.Error("apm.yml was written despite the form-entered name failing validation")
	}
	if _, err := os.Stat("plugin.json"); err == nil {
		t.Error("plugin.json was written despite the form-entered name failing validation")
	}
}

// TestInitInteractive_Form_ConsumerNameStillAcceptsUnderscore is the
// consumer-mode counterpart proving R3.3.a's guarantee holds through this new
// validation call too: consumerValidateName only rejects "/", "\\", and ".."
// (AC32/AC37), so a form-entered name with an underscore must still succeed
// -- the new mode.validateName(name) call must not tighten consumer mode's
// existing, looser rule.
func TestInitInteractive_Form_ConsumerNameStillAcceptsUnderscore(t *testing.T) {
	cap := driveInteractiveInitWithNameOverride(t, initCmd(), nil, "My_Project")
	if cap.runErr != nil {
		t.Fatalf("consumer init failed for a form-entered name with an underscore: %v", cap.runErr)
	}
	if _, err := os.Stat("apm.yml"); err != nil {
		t.Errorf("apm.yml was not written for a valid consumer-mode form-entered name: %v", err)
	}
}

// TestInitInteractive_Form_ConsumerNameRejectsPathSeparator covers R3.3.a's
// other half: consumer mode's existing "/" rejection (AC37) must also apply
// to a name typed into the interactive form, not just the PROJECT-NAME arg.
func TestInitInteractive_Form_ConsumerNameRejectsPathSeparator(t *testing.T) {
	cap := driveInteractiveInitWithNameOverride(t, initCmd(), nil, "a/b")
	if cap.runErr == nil {
		t.Fatal("consumer init returned no error for a form-entered name containing '/', want an error")
	}
	if !strings.Contains(cap.runErr.Error(), "invalid project name") {
		t.Errorf("error = %q, want it to contain %q", cap.runErr.Error(), "invalid project name")
	}
}
