//go:build !apm_test_hooks

package main

// installClackEventRecorder is the ordinary (untagged) build's no-op
// counterpart to plugin_init_clackhook_enabled_test.go: ux.SetClackEventHookForTest
// only exists in a `-tags apm_test_hooks` build (A-MINOR-1, external audit
// round 7, 2026-07-31 follow-up), so this variant does nothing --
// cap.clackEvents stays empty for the duration of driveInteractiveInit's
// caller. This keeps plugin_init_interactive_test.go's other 5 tests
// (AC38/AC41's Form/MultiSelect/Confirm/version-default coverage, none of
// which touch cap.clackEvents) compiling and passing under a plain, untagged
// `go test ./...`; TestInitVsPluginInit_ClackSequenceParity (the one test
// that DOES assert on cap.clackEvents, AC52) lives in its own
// apm_test_hooks-tagged file (plugin_init_clacksequence_test.go) precisely
// so it is absent -- not silently false -- from an untagged run.
func installClackEventRecorder(cap *interactiveCapture) (restore func()) {
	return func() {}
}
