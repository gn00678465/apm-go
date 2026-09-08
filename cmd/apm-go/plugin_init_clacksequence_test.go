//go:build apm_test_hooks

package main

import (
	"reflect"
	"testing"
)

// This file is gated behind the apm_test_hooks build tag (A-MINOR-1,
// external audit round 7, 2026-07-31 follow-up): TestInitVsPluginInit_
// ClackSequenceParity is the only test in this package that asserts on
// interactiveCapture.clackEvents, which driveInteractiveInit
// (plugin_init_interactive_test.go) only populates via
// installClackEventRecorder's apm_test_hooks-tagged variant
// (plugin_init_clackhook_enabled_test.go) -- under an untagged
// `go test ./...`, clackEvents stays permanently empty (the disabled
// variant's no-op), so this test is excluded here rather than asserting
// against an always-empty slice, which would be a meaningless tautological
// pass, not a real regression gate.
//
// Run this test explicitly with: go test -tags apm_test_hooks -run
// TestInitVsPluginInit_ClackSequenceParity ./cmd/apm-go/

// wantClackSequence is the full, mode-independent clack transcript call
// order both consumer init and plugin init must walk.
var wantClackSequence = []string{"Banner", "Intro", "Form", "MultiSelect", "Note", "Confirm", "Outro"}

// TestInitVsPluginInit_ClackSequenceParity is AC52: `plugin init` must walk
// the same clack transcript sequence as consumer `init` -- Banner, Intro,
// Form, MultiSelect, Note, Confirm, Outro -- rather than a bespoke
// non-interactive flow.
//
// A-MAJOR-1 (external audit round 6, 2026-07-31): this used to reconstruct
// the call sequence by matching literal text substrings (e.g. "Is this
// OK?") in captured stderr, which is fragile in two independent ways: (1) it
// silently breaks if a message is ever reworded, since nothing keeps the
// marker string and the production string in sync; (2) it conflates "was
// this method called" with "did it print something" -- Banner in particular
// only prints on a Unicode-capable terminal (banner.go), so the old
// text-marker recorder omitted Banner from BOTH sequences whenever
// supportsUnicode() was false in the test environment, silently reducing
// the assertion's power without ever going red. This now uses
// ux.SetClackEventHookForTest to record each Clack method call directly, at
// its own entry point, decoupled from whatever it renders -- and asserts
// against wantClackSequence explicitly (not just "the two sides match each
// other"), so a genuinely missing event (e.g. Banner never firing at all)
// is caught even if it happens to vanish from both sides identically.
//
// Falsifiability (required by AC52's own text): if plugin init were
// rewritten as an independent non-interactive command, the clack-event hook
// this test installs would never fire for it, so pluginCap.clackEvents
// would come back empty while consumerCap.clackEvents stays populated,
// and the DeepEqual checks below would fail. This was verified directly:
// temporarily short-circuiting pluginInitCmd's RunE to `return nil` before
// calling runInitCore made this test fail with consumerCap.clackEvents
// non-empty and pluginCap.clackEvents empty, confirming the assertion is
// load-bearing and not a tautology.
func TestInitVsPluginInit_ClackSequenceParity(t *testing.T) {
	consumerCap := driveInteractiveInit(t, initCmd(), nil, true)
	if consumerCap.runErr != nil {
		t.Fatalf("consumer init failed: %v", consumerCap.runErr)
	}
	pluginCap := driveInteractiveInit(t, pluginInitCmd(), []string{"my-plugin"}, true)
	if pluginCap.runErr != nil {
		t.Fatalf("plugin init failed: %v", pluginCap.runErr)
	}

	if !reflect.DeepEqual(consumerCap.clackEvents, wantClackSequence) {
		t.Errorf("consumer init clack call sequence = %v, want %v", consumerCap.clackEvents, wantClackSequence)
	}
	if !reflect.DeepEqual(pluginCap.clackEvents, wantClackSequence) {
		t.Errorf("plugin init clack call sequence = %v, want %v", pluginCap.clackEvents, wantClackSequence)
	}
	if !reflect.DeepEqual(consumerCap.clackEvents, pluginCap.clackEvents) {
		t.Errorf("clack call sequence differs between init and plugin init:\n  init:        %v\n  plugin init: %v", consumerCap.clackEvents, pluginCap.clackEvents)
	}
}
