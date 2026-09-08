//go:build apm_test_hooks

package main

import "github.com/apm-go/apm/internal/ux"

// installClackEventRecorder is the apm_test_hooks build's implementation of
// clack-event recording for driveInteractiveInit
// (plugin_init_interactive_test.go): wires ux.SetClackEventHookForTest
// (internal/ux/clackhook_shim.go, itself gated behind the same build tag --
// see its own doc comment for why) so cap.clackEvents actually gets
// populated. See plugin_init_clackhook_disabled_test.go for the untagged
// build's no-op counterpart.
func installClackEventRecorder(cap *interactiveCapture) (restore func()) {
	return ux.SetClackEventHookForTest(func(name string) {
		cap.clackEvents = append(cap.clackEvents, name)
	})
}
