package ux

import (
	"testing"

	"charm.land/huh/v2"
)

// These tests exercise SetPromptSeamsForTest and SetTTYSeamsForTest
// themselves, from inside internal/ux. Both are consumed extensively by
// cmd/apm-go (e.g. AC2/AC3's interactiveTargetSelect coverage), but that
// consumption never proves the seam-swap mechanism itself is correct --
// specifically that restore() actually reverts to the production
// implementation. Without a test at this level, a leaked stub would
// silently disarm every later interactive test in the same process with no
// red anywhere (terminal-ux-contract.md Common Mistakes: Spinner's
// non-idempotent finish() is the same class of bug -- a missing restore
// guarantee that only shows up as a hard-to-diagnose failure downstream).

// TestSetPromptSeamsForTest_OverridesAndSeamRestoreRevertsToProduction is
// the AC-L3 guard for the confirm/multiSelect/inputForm prompt seams:
// overriding must bypass the production runField/runMultiSelectField/
// runForm path entirely, and restore() must bring that production path
// back -- not just "some" behavior, but the exact seam that was in place
// before SetPromptSeamsForTest was called.
func TestSetPromptSeamsForTest_OverridesAndSeamRestoreRevertsToProduction(t *testing.T) {
	// Arrange
	setRichMode(t, true)

	var fieldCalls, multiSelectCalls, formCalls int
	stubRunField(t, func(huh.Field) error { fieldCalls++; return nil })
	stubRunMultiSelectField(t, func(huh.Field) error { multiSelectCalls++; return nil })
	stubRunForm(t, func(*huh.Form) error { formCalls++; return nil })

	// Baseline: before any SetPromptSeamsForTest override, the public
	// Confirm/MultiSelect/InputForm functions run through the production
	// confirmWith/multiSelectWith/inputFormWith implementations, which in
	// turn call runField/runMultiSelectField/runForm.
	if _, err := Confirm("proceed?", true); err != nil {
		t.Fatalf("Confirm() err = %v, want nil", err)
	}
	if _, err := MultiSelect("targets", []Option{{Label: "a", Value: "a"}}); err != nil {
		t.Fatalf("MultiSelect() err = %v, want nil", err)
	}
	if _, err := InputForm("meta", []Field{{Key: "k", Default: "d"}}); err != nil {
		t.Fatalf("InputForm() err = %v, want nil", err)
	}
	if fieldCalls != 1 || multiSelectCalls != 1 || formCalls != 1 {
		t.Fatalf("baseline calls = confirm:%d multiSelect:%d form:%d, want 1/1/1", fieldCalls, multiSelectCalls, formCalls)
	}

	// Act: override all three seams with stubs that never touch
	// runField/runMultiSelectField/runForm.
	var confirmStubCalled, multiSelectStubCalled, inputFormStubCalled bool
	restore := SetPromptSeamsForTest(
		func(theme huh.Theme, prompt string, def bool) (bool, error) {
			confirmStubCalled = true
			return false, nil
		},
		func(theme huh.Theme, title, description string, showHelp bool, opts []Option) ([]string, error) {
			multiSelectStubCalled = true
			return []string{"stub"}, nil
		},
		func(theme huh.Theme, title string, showHelp bool, fields []Field) (map[string]string, error) {
			inputFormStubCalled = true
			return map[string]string{"stub": "value"}, nil
		},
	)
	// 2026-07-30 codex Tier 2 M4: restore() is also called explicitly below
	// (this test asserts on behavior both before and after restoring), but
	// t.Cleanup is registered as a safety net -- any t.Fatalf between here
	// and the explicit restore() call further down would otherwise skip it
	// entirely and leak the stubbed seams into every later test in this
	// package (and, transitively, every cmd/apm-go test that exercises
	// ux.Confirm/MultiSelect/InputForm), with no red anywhere to point at
	// the cause.
	t.Cleanup(restore)

	gotConfirm, err := Confirm("proceed?", true)
	if err != nil {
		t.Fatalf("Confirm() err = %v, want nil", err)
	}
	gotMultiSelect, err := MultiSelect("targets", []Option{{Label: "a", Value: "a"}})
	if err != nil {
		t.Fatalf("MultiSelect() err = %v, want nil", err)
	}
	gotInputForm, err := InputForm("meta", []Field{{Key: "k", Default: "d"}})
	if err != nil {
		t.Fatalf("InputForm() err = %v, want nil", err)
	}

	// Assert: overridden behavior took effect...
	if !confirmStubCalled || !multiSelectStubCalled || !inputFormStubCalled {
		t.Fatalf("stub called = confirm:%v multiSelect:%v inputForm:%v, want all true",
			confirmStubCalled, multiSelectStubCalled, inputFormStubCalled)
	}
	if gotConfirm != false {
		t.Errorf("Confirm() = %v, want stubbed false", gotConfirm)
	}
	if len(gotMultiSelect) != 1 || gotMultiSelect[0] != "stub" {
		t.Errorf("MultiSelect() = %v, want [stub]", gotMultiSelect)
	}
	if gotInputForm["stub"] != "value" {
		t.Errorf("InputForm() = %v, want stub:value", gotInputForm)
	}
	// ...and the production runField/runMultiSelectField/runForm path was
	// not touched again while overridden.
	if fieldCalls != 1 || multiSelectCalls != 1 || formCalls != 1 {
		t.Fatalf("calls while overridden = confirm:%d multiSelect:%d form:%d, want unchanged 1/1/1",
			fieldCalls, multiSelectCalls, formCalls)
	}

	// Act: restore() must revert every seam back to production.
	restore()

	if _, err := Confirm("proceed?", true); err != nil {
		t.Fatalf("Confirm() err = %v, want nil", err)
	}
	if _, err := MultiSelect("targets", []Option{{Label: "a", Value: "a"}}); err != nil {
		t.Fatalf("MultiSelect() err = %v, want nil", err)
	}
	if _, err := InputForm("meta", []Field{{Key: "k", Default: "d"}}); err != nil {
		t.Fatalf("InputForm() err = %v, want nil", err)
	}

	// Assert: production path is reached again post-restore.
	if fieldCalls != 2 || multiSelectCalls != 2 || formCalls != 2 {
		t.Fatalf("calls after restore() = confirm:%d multiSelect:%d form:%d, want 2/2/2 "+
			"(production seams must be back in place)", fieldCalls, multiSelectCalls, formCalls)
	}
}

// TestSetPromptSeamsForTest_NilArgsLeaveOtherSeamsUntouched proves the
// documented "a nil argument leaves that seam untouched" contract: passing
// nil for multiSelect/inputForm while overriding only confirm must not
// disturb the other two seams' production wiring.
func TestSetPromptSeamsForTest_NilArgsLeaveOtherSeamsUntouched(t *testing.T) {
	// Arrange
	setRichMode(t, true)

	var multiSelectCalls, formCalls int
	stubRunMultiSelectField(t, func(huh.Field) error { multiSelectCalls++; return nil })
	stubRunForm(t, func(*huh.Form) error { formCalls++; return nil })

	var confirmStubCalled bool
	restore := SetPromptSeamsForTest(
		func(theme huh.Theme, prompt string, def bool) (bool, error) {
			confirmStubCalled = true
			return true, nil
		},
		nil,
		nil,
	)
	t.Cleanup(restore)

	// Act
	if _, err := Confirm("proceed?", true); err != nil {
		t.Fatalf("Confirm() err = %v, want nil", err)
	}
	if _, err := MultiSelect("targets", []Option{{Label: "a", Value: "a"}}); err != nil {
		t.Fatalf("MultiSelect() err = %v, want nil", err)
	}
	if _, err := InputForm("meta", []Field{{Key: "k", Default: "d"}}); err != nil {
		t.Fatalf("InputForm() err = %v, want nil", err)
	}

	// Assert
	if !confirmStubCalled {
		t.Fatal("confirm override was not invoked")
	}
	if multiSelectCalls != 1 || formCalls != 1 {
		t.Fatalf("multiSelect/inputForm calls = %d/%d, want 1/1 (nil args must leave "+
			"those seams on the production path, not silently disable them)", multiSelectCalls, formCalls)
	}
}

// TestSetTTYSeamsForTest_OverridesAndSeamRestoreRevertsToProduction is the
// AC-L3 guard for the stdin/stdout/stderr TTY-detection seams: overriding
// must change CanPrompt()'s answer, and restore() must bring back the exact
// pre-override state (here, a known non-TTY baseline pinned via the
// package-internal with*TTY helpers) rather than some hardcoded default.
func TestSetTTYSeamsForTest_OverridesAndSeamRestoreRevertsToProduction(t *testing.T) {
	// Arrange: pin a known baseline so this test doesn't depend on whatever
	// TTY state the test runner's own stdio happens to have.
	withStdinTTY(t, false)
	withStdoutTTY(t, false)
	withStderrTTY(t, false)
	t.Setenv("CI", "")

	if CanPrompt() {
		t.Fatal("baseline CanPrompt() = true, want false (stdin/stderr stubbed non-TTY)")
	}
	if stdoutIsTTY() {
		t.Fatal("baseline stdoutIsTTY() = true, want false")
	}

	// Act: override via the exported SetTTYSeamsForTest.
	restore := SetTTYSeamsForTest(true, true, true)

	// Assert: overridden state took effect.
	if !CanPrompt() {
		t.Fatal("after SetTTYSeamsForTest(true,true,true), CanPrompt() = false, want true")
	}
	if !stdoutIsTTY() {
		t.Fatal("after SetTTYSeamsForTest(true,true,true), stdoutIsTTY() = false, want true")
	}

	// Act: restore() must revert to the pre-override (baseline-stubbed)
	// state -- a leaked override here would silently make CanPrompt()
	// report true for every later test with no red anywhere.
	restore()

	// Assert
	if CanPrompt() {
		t.Fatal("after restore(), CanPrompt() = true, want false (baseline must be restored)")
	}
	if stdoutIsTTY() {
		t.Fatal("after restore(), stdoutIsTTY() = true, want false (baseline must be restored)")
	}
}
