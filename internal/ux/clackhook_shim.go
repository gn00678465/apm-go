//go:build apm_test_hooks

package ux

// This file is gated behind the apm_test_hooks build tag (A-MINOR-1,
// external audit round 7, 2026-07-31 follow-up): SetClackEventHookForTest is
// an exported function whose sole purpose is to let test code in ANOTHER
// package (cmd/apm-go) install a callback that overrides clackEventHook
// (clack.go), an unexported package-level var. Prior to this split, it lived
// in testhooks.go -- a plain, non-"_test.go" file, compiled unconditionally
// as part of the ordinary `ux` package -- purely because Go's own visibility
// rules require that: a symbol declared in a "_test.go" file is never part
// of a package's normally-built archive at all, so it could never be
// referenced from a DIFFERENT package's tests (only from tests within the
// SAME package, or an "_test" external test package linked into that same
// package's own test binary). That meant SetClackEventHookForTest shipped in
// every ordinary `go build ./...` release binary, reachable by nothing (it
// is under internal/, so no code outside this module can even import this
// package -- see the module-boundary note below), but present as unused
// dead weight and API-surface bloat regardless.
//
// This build tag closes that gap for the actual `apm build ./...`/AGENTS.md
// release commands (none of which pass -tags): built without
// `-tags apm_test_hooks`, this file -- and therefore
// SetClackEventHookForTest itself -- is not part of the compiled `ux`
// package at all, so the symbol is entirely absent from the resulting
// binary (verified directly: `go tool nm` on a plain `go build ./cmd/apm-go`
// binary finds no such symbol; see 07-29-plugin-init/verify.ps1's
// A-MINOR-1 gate).
//
// cmd/apm-go's own consumer (plugin_init_interactive_test.go's
// driveInteractiveInit) does not call this function directly either: doing
// so would require the SAME build tag on every _test.go file that
// transitively uses driveInteractiveInit, which would silently exclude five
// unrelated tests (AC38/AC41's Form/MultiSelect/Confirm/version-default
// coverage) from a plain `go test ./...` run whenever the tag is not passed.
// Instead, driveInteractiveInit calls a small package-private indirection,
// installClackEventRecorder, implemented in TWO build-tag-variant _test.go
// files in cmd/apm-go (plugin_init_clackhook_enabled_test.go, tagged the
// same way, calls this function; plugin_init_clackhook_disabled_test.go,
// tagged !apm_test_hooks, is a no-op) -- so an ordinary, untagged
// `go test ./...` still compiles and runs those five tests (with clack-event
// recording silently disabled), while only
// TestInitVsPluginInit_ClackSequenceParity itself (the one test that
// actually asserts on the recorded sequence, AC52) is gated the same way, in
// its own tagged file (plugin_init_clacksequence_test.go), since asserting
// against an empty recording with the tag absent would be a meaningless
// false either way.
//
// Real-world exploitability of NOT gating this (honestly assessed, not just
// asserted): this package's own import path is
// "github.com/apm-go/apm/internal/ux" -- Go's language-level "internal/"
// import-visibility rule (https://go.dev/doc/go1.4#internalpackages)
// already forbids ANY package outside this module's own tree
// ("github.com/apm-go/apm/...") from importing it at all, at compile time,
// regardless of what the shipped binary contains. So even before this
// build-tag split, no external caller could ever have reached this function
// by importing this package as a library; the only route within reach was
// building or forking this exact repository, which already grants full
// source access. This is why the finding is MINOR, not a genuine
// vulnerability: the fix here is about binary-surface hygiene (a smaller,
// more auditable release artifact), not closing an externally reachable
// hole.
func SetClackEventHookForTest(hook func(name string)) (restore func()) {
	prev := clackEventHook
	clackEventHook = hook
	return func() { clackEventHook = prev }
}
