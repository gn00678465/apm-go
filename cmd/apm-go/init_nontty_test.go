package main

import (
	"os"
	"testing"

	"github.com/apm-go/apm/internal/ux"
)

// TestInitCmd_NonTTYStdin_NoApmYml_SucceedsWithAutoDetectedDefaults pins the
// HIGH regression fix: init.go used to gate its interactive branches on a
// local isInteractive() built on os.ModeCharDevice, which a redirected
// non-terminal stdin (e.g. /dev/null, or a CI pipe) also satisfies -- so a
// non-interactive invocation without --yes was wrongly treated as
// interactive. With every branch now gated on ux.CanPrompt() (real
// term.IsTerminal), a forced non-TTY session must behave exactly like
// --yes: auto-detected metadata/targets, no prompt, apm.yml written.
func TestInitCmd_NonTTYStdin_NoApmYml_SucceedsWithAutoDetectedDefaults(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	restore := ux.SetTTYSeamsForTest(false, false)
	defer restore()

	// Act
	cmd := initCmd()
	cmd.SetArgs([]string{"--target", "claude"})
	err := cmd.Execute()

	// Assert
	if err != nil {
		t.Fatalf("init with non-TTY stdin (no --yes) failed: %v", err)
	}
	if _, statErr := os.Stat("apm.yml"); statErr != nil {
		t.Fatalf("apm.yml was not created: %v", statErr)
	}
}

// TestInitCmd_NonTTYStdin_ExistingApmYml_RequiresYes pins the same fix for
// the overwrite-confirmation branch: with a forced non-TTY session and an
// existing apm.yml, init must require --yes (init.go's non-interactive
// "else" branch) rather than attempting an unanswerable overwrite
// confirmation.
func TestInitCmd_NonTTYStdin_ExistingApmYml_RequiresYes(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	if err := os.WriteFile("apm.yml", []byte("existing"), 0644); err != nil {
		t.Fatal(err)
	}

	restore := ux.SetTTYSeamsForTest(false, false)
	defer restore()

	// Act
	cmd := initCmd()
	cmd.SetArgs([]string{})
	err := cmd.Execute()

	// Assert
	if err == nil {
		t.Fatal("init with non-TTY stdin and existing apm.yml returned no error, want the requires --yes error")
	}
	yml, readErr := os.ReadFile("apm.yml")
	if readErr != nil {
		t.Fatalf("apm.yml missing after rejected init: %v", readErr)
	}
	if string(yml) != "existing" {
		t.Fatalf("apm.yml was modified despite the requires --yes error: %s", yml)
	}
}
